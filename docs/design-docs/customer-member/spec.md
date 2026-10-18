# Feature：顾客会员体系（小程序「注册成为会员」）

**作者**：CodeBuddy（设计起草）
**日期**：2026-10-08
**状态**：已实施（2026-10-08，按 §9 的推荐方案落地）
**关联代码**：`frontend/apps/miniprogram/src/*`、`backend/internal/weapp/*`、`backend/internal/handler/customer*.go`、`backend/internal/service/*`、`backend/internal/store/{schema.go,dao/*}`、`backend/internal/service/auth_service.go`（令牌机制）

> **后续演进（2026-10-18）**：身份层已拆成「账号 / 登录方式」两张表
> （`tb_customer` + `tb_customer_identity`），为 App、网站接入预留；
> 详见 [`../customer-member-identity/spec.md`](../customer-member-identity/spec.md)。
> 本文档中「`openid` 直接做 `tb_customer` 唯一键」「登录请求体只有 `{code}`」的描述
> 已被那次改造取代，其余（令牌机制、会员卡字段、我的订单、前端交互）仍然有效。

---

## 0. 需求与一句话方案

**需求**：客人在小程序里可以「登录」，登录后显示为「注册成为了会员」。

**一句话方案**：微信一键授权静默注册 —— 顾客点「微信一键注册成为会员」，小程序拿 `wx.login`
的 code 到后端换 `openid`；这个 openid 第一次出现就建会员档案并提示
「🎉 注册成功，已加入会员」，之后再来就是「欢迎回来」。
**全程免填表单、免密码、无授权弹窗**（`wx.login` 本身是静默的，用户无感知）。

**「会员」一期是什么**：

- 一个身份（openid）+ 一张会员卡（入会日期、累计订单数、昵称头像可选填写）
- 「我的订单」：跨桌的历史订单列表
- 为小程序内支付（JSAPI）备好 openid —— 这是支付的硬前置（`payer.openid`），见 `docs/miniprogram.md` 4.1

**「会员」一期不是什么**：积分、储值、优惠券、会员等级、手机号绑定。全部列入二期（见 §8）。

---

## 1. 三个产品决策（待拍板，先给推荐）

| # | 决策 | 推荐 | 理由 |
|---|---|---|---|
| D1 | 强制登录还是自愿登录 | **自愿**：未登录仍可完整点餐，现有体验零变化 | 到店客人的主诉求是快点下单；登录打断下单会把人挡在门外。会员入口放「我的」tab 与下单成功页 |
| D2 | 「我的」是否新增第三个底部 tab | **是**（点餐 / 订单 / 我的） | 会员需要常驻入口；tab-bar 与 nav 已是组件化实现，加一档改动小 |
| D3 | 一期是否收集手机号 | **否** | 小程序获取手机号需**企业认证主体**且按次收费；openid 已满足「身份 + 支付」需求。手机号放二期（届时同步更新隐私指引） |

---

## 2. 身份与会话设计

### 2.1 会员档案：新表 `tb_customer`

```sql
CREATE TABLE IF NOT EXISTS tb_customer (
	customer_id {{PK}},
	openid VARCHAR(64) NOT NULL,             -- 小程序 openid，唯一
	nickname VARCHAR(64) DEFAULT '',         -- 用户主动填写，不强制
	avatar_url VARCHAR(255) DEFAULT '',
	status INTEGER DEFAULT 1,                -- 1 正常 / 0 停用
	order_count INTEGER DEFAULT 0,           -- 冗余：累计下单次数（会员卡展示）
	last_order_time VARCHAR(32) DEFAULT '',
	create_time VARCHAR(32) DEFAULT '',      -- 即「入会日期」
	last_login_time VARCHAR(32) DEFAULT '',
	remark VARCHAR(255) DEFAULT ''
){{OPTS}};
CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_openid ON tb_customer(openid);
```

- **openid 存明文**：它只是「在这个小程序里的用户标识」，不是密钥；但**不对外输出**
  （接口只回 customerId / 昵称 / 头像 / 入会日期）。
- **不存 `session_key`**：一期不解密手机号等敏感数据，按最小化原则不落库（二期接手机号再评估）。
- **无 `del_flag`**：会员不做软删除；「注销会员」直接物理删（见 2.6）。
- 建表风格对齐 `store/schema.go` 的 `{{PK}}/{{OPTS}}` 占位符写法（参考 `tb_user`，schema.go:371）。
- `tb_customer` **无种子数据** → `SeedCounts()` 与 `sqlgen_test.go` 都**不用**登记
  （无种子的表不参与校验，如同 tb_user）。

### 2.2 订单归属：`tb_order` 加一列

```sql
ALTER TABLE tb_order ADD COLUMN customer_id INTEGER DEFAULT 0;  -- 0 = 匿名单
CREATE INDEX IF NOT EXISTS idx_order_customer ON tb_order(customer_id, create_time);
```

- 用 `DEFAULT 0` 表示匿名（历史单与未登录单都是 0），不用 NULL：两方言下的补列迁移更简单，
  po 里 `CustomerID int` 直接可扫。
- `dao.InsertOrderTx(...)` 增加 `customerID` 参数；加菜（append）路径同步。
- **现有订单不回填**，保持 0。

### 2.3 顾客令牌：复用现有 HMAC 基建、独立形态

管理端令牌是无状态 HMAC（`auth_service.GenToken`：`username|uid|tokenVersion|exp` +
HMAC-SHA256，密钥来自 `data/auth.key`）。顾客令牌**用同一套基础设施、独立形态**：

```
payload = "customer|" + customerID + "|" + exp        (exp = 签发时间 + 2h)
token   = base64url(payload) + "." + HMAC-SHA256(payload, authKey)
```

- **TTL 2 小时**（与管理端默认一致），**不做「记住我」**：因为 `wx.login` 静默免费，
  小程序在令牌过期 / 401 时**自动重新登录**，顾客无感知。换来两个好处：
  ① 无状态，不建会话表；② 停用会员即时生效。
- 新增 `service.GenCustomerToken` / `AuthenticateCustomerToken`（同用 `infra.LoadAuthKey()`）。
- **与管理端令牌互不通用**：payload 前缀不同，管理端 token 拿到顾客接口会被拒（反之亦然）。

### 2.4 接口设计（全部在公开组 `/api/customer/*`）

| 方法与路径 | 说明 |
|---|---|
| `POST /api/customer/auth/login` | body `{code}`；服务端 code2session 换 openid → 不存在则建档（响应 `isNew:true`）→ 签发 token。响应 `{token, isNew, customer:{customerId, nickname, avatarUrl, createTime, orderCount}}` |
| `GET /api/customer/auth/profile` | 需顾客 token；返回会员卡信息 |
| `GET /api/customer/order/mine?pageNum=&pageSize=` | 需顾客 token；**跨桌**历史订单（单号/桌名/状态/金额/时间），分页结构复用现有 `{total, items}` |
| `POST /customer/order`、`/customer/order/append` | **可选**携带 `Authorization: Bearer <顾客token>`：有则把 customer_id 写进订单，无则 0。请求体**不带** customer_id（不可信，一律以 token 为准） |

- 现有 `GET /customer/order/no/:orderNo` **维持现状**（凭 8 位单号可查）：同桌其他人可能用
  H5 看同一单，不因会员收紧。
- **退出登录无后端接口**（令牌无状态、TTL 2h 自动失效），前端清本地 storage 即可。
- `/auth/login` 加**进程内限流**（同 IP 每分钟 ≤ 30 次），防刷 `code2session`；
  微信侧另有频控（45011）。

### 2.5 code2session（`backend/internal/weapp/auth.go`，新增）

```
GET https://api.weixin.qq.com/sns/jscode2session
    ?appid=APPID&secret=SECRET&js_code=JSCODE&grant_type=authorization_code
→ 成功 { openid, session_key?, unionid? } / 失败 { errcode, errmsg }
```

- appid / secret 用现有配置 `wxpay_appid` / `wx_app_secret`（已加密存储，桌牌码功能已在用）。
- 与 `internal/weapp/code.go` 同包：共用 `apiBase` 与 httptest 打桩方式，测试容易。
- **AppSecret 未配置时的行为**：`/auth/login` 返回「会员功能未开启」（业务提示而非异常），
  前端据此隐藏登录入口 —— 会员功能可以被配置式地关掉，不炸错误给用户。
- 错误码翻译成可操作的中文：`40029` code 无效（已被用/过期，前端自动重试一次 `wx.login`）、
  `45011` 频率超限、`-1` 系统繁忙。

### 2.6 注销（隐私合规）

「我的」页提供**退出登录**（清本地令牌）。一期**不提供**「注销会员」入口 —— 不采集手机号等
敏感信息，openid 档案不构成可识别的敏感数据。二期若收集手机号，需补「注销会员」：
物理删除档案 + 该顾客历史订单 `customer_id` 置 0。

---

## 3. 小程序端设计

### 3.1 会员入口与「注册成为会员」的时刻

| 位置 | 形态 |
|---|---|
| 「我的」tab（新增） | 未登录：会员卡占位 + 「微信一键注册成为会员」按钮；已登录：会员卡（昵称/头像，未填写显示「微信用户」）+ 入会日期 + 累计订单 + 「我的订单」列表（分页）+ 退出登录。**会员未开启时该 tab 整体隐藏**（导航回到点餐/订单两档，三个页面统一由 `memberEnabled()` 驱动） |
| 下单成功页 | 轻提示条：「注册会员，随时查看历史订单 →」，点按进入一键注册；已登录不显示 |
| 支付页 | 一期不加（支付本身未接，见 `docs/miniprogram.md` 4.1） |

- **登录动作全程无弹窗**：`wx.login` 是静默的。所谓「一键」就是「点一下按钮」。
- 注册成功反馈：toast「🎉 注册成功，已加入会员」+ 会员卡从占位态翻转为实卡（含入会日期）。
- 老会员再次登录：显示「欢迎回来」，不重复提示注册。
- code 失效（40029）自动重试一次 `wx.login`，仍失败才提示。

### 3.2 改动清单（文件级）

| 文件 | 改动 |
|---|---|
| `src/app.json` | `pages` 增加 `pages/profile/index` |
| `src/components/tab-bar/*` + `src/utils/nav.ts` | 2 tab → 3 tab（点餐 / 订单 / 我的）；nav 的「已在目标页不跳 / 在栈里返回 / redirectTo」三分支同样适用 |
| `src/store/member.ts`（新增） | 会员状态挂 App 实例（沿用「模块级单例不可用」约定，见 `docs/miniprogram.md` 5.3）：`login()` / `fetchProfile()` / `logout()` / `ensureToken()` |
| `src/api/client.ts` | 请求自动附顾客 token；**401 时静默重登一次并重放原请求**（仅对顾客接口生效） |
| `src/pages/profile/*`（新增） | 会员卡 + 我的订单（分页）+ 退出登录 |
| `src/test/*` | member 用例：登录建档 / 老客复登 / 401 静默重登重放 / 退出 / 未配置时隐藏入口 |

体积影响：+1 页面约 +40~60KB（自包含 CJS，axios 已在各入口内联），`dist/` 约 460KB → ~520KB，
主包上限 2MB，安全。

---

## 4. 后端实现清单

| 文件 | 改动 |
|---|---|
| `internal/store/schema.go` | 新增 `tb_customer` 与 `idx_customer_openid`；`tb_order` 补列 `customer_id` + `idx_order_customer`。启动时 `migrate()` 的幂等建表/补列自动覆盖老库，**无需手工迁移**；`incr/` 照惯例补一份手工脚本供 DBA 离线升级 |
| `cmd/gensql` 产物 | `go run ./cmd/gensql` 重新生成 `full/*.sql`（`TestGenSQLCheck` 会强制校验） |
| `internal/po/customer.go`（新增） | Customer PO（风格对齐 po/user.go） |
| `internal/store/dao/customer.go`（新增） | `GetByOpenid` / `Insert` / `TouchLogin` / `UpdateProfile` / `SetStatus` / `IncrOrderCount` / `ListOrdersByCustomer` |
| `internal/service/auth_service.go` | `GenCustomerToken` / `AuthenticateCustomerToken`（同 HMAC 基建，独立前缀与 TTL） |
| `internal/weapp/auth.go`（新增） | `Code2Session(code)` + 错误码翻译 |
| `internal/service/customer_member_service.go`（新增） | Login（isNew 判定；并发注册靠唯一索引兜底）/ Profile / MyOrders / 下单时维护 order_count 与 last_order_time |
| `internal/handler/customer_auth.go`（新增） | 3 个 handler + `CustomerAuth`（必选）与 `CustomerAuthOptional`（可选身份）两个中间件 |
| `main.go` | 公开组注册 3 条新路由（customer 组无权限体系，**不需**登记 routePerms，与现有 customer 路由一致） |
| `internal/store/dao/order.go` | `InsertOrderTx` / 加菜路径带 customerID |
| 测试 | code2session 打桩（新客建档/老客复登/微信错误码/未配置）；顾客 token 签发-校验-过期-伪造-与管理端互拒；匿名下单 `customer_id=0` 不受影响；会员下单归属正确；登录限流 |

---

## 5. 与其它模块的关系

| 模块 | 关系 |
|---|---|
| 桌牌小程序码（`docs/miniprogram.md` 二） | 共用 `wxpay_appid` / `wx_app_secret`；会员功能依赖这两项已填，未填则隐藏登录入口 |
| JSAPI 支付（4.1） | 本方案把 openid 备好；支付落地时 `payer.openid = customer.openid`，身份层无需再动 |
| 订阅消息（4.3） | 推送目标 = openid，同样就绪 |
| H5 顾客端 | **不做** H5 会员：H5 无法静默拿 openid（要网页授权，另一套体系）。H5 保持匿名；H5 单与小程序单**不合并**（没有可靠关联键），「我的订单」只含小程序单 |
| 管理端 | 一期不加「会员管理」页面（数据在 `tb_customer`）；二期可加列表/停用/标签 |

---

## 6. 合规

- 公众平台「用户隐私保护指引」新增声明：**openid（用户标识）、昵称、头像（用户主动填写）**
  —— 对应上线硬前置第 7 项，从「可暂缓」变为「上会员前必填」。
- 不收集手机号、位置、通讯录等（最小化收集）。
- 登录无需任何用户授权弹窗；昵称头像只在用户主动「完善资料」时填写。

---

## 7. 测试与验收

| 层 | 验收点 |
|---|---|
| 后端 | code2session 新客建档/老客复登/错误码；顾客 token 签发-校验-过期-伪造-与管理端互拒；**匿名链路回归**：现有 customer 相关测试全绿、下单/加菜/催菜/支付查询行为不变 |
| 小程序 | member 用例 + 现有 13 个用例文件全绿（`npm run mp:verify` 六道门全过）；真机：静默重登无感知、tab 三档切换、我的订单分页 |
| 管理端 | 零改动（不出现在会员功能里） |

---

## 8. 分期与二期候选

| 期 | 内容 | 依赖 |
|---|---|---|
| 一期（本方案） | openid 会员 + 会员卡 + 我的订单 + 为支付备好 openid | AppSecret 已配置 |
| 二期 | 手机号绑定（企业认证主体 + 按次收费）、积分/储值/优惠券、会员等级、管理端会员管理页、「注销会员」入口、订阅消息 | 一期 + 资质/营销方案 |
| 不做 | H5 端会员（无静默 openid 路径） | — |

---

## 9. 待拍板（已确认：全部按推荐执行）

1. **D1 自愿登录**：未登录保持完整点餐能力 ✅
2. **D2 三个底部 tab**：新增「我的」 ✅
3. **D3 一期不收集手机号** ✅
4. **会员权益**：一期只有「身份 + 我的订单」，不做积分/储值 ✅

