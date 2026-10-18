# Feature：顾客会员身份层（账号 / 登录方式解耦）

**作者**：CodeBuddy（设计起草 + 实施）
**日期**：2026-10-18
**状态**：P0 + P2 + 会员运营闭环已实施（2026-10-18）
**关联代码**：`backend/domain/po/customer.go`、`backend/infrastructure/store/{schema.go,dao/customer.go}`、`backend/application/service/{customer_member_service.go,auth_service.go}`、`backend/interfaces/handler/customer_auth.go`、`backend/infrastructure/weapp/auth.go`、`frontend/packages/{shared/src/safeStorage.ts,api/src/{http.ts,customer.ts}}`、`frontend/apps/miniprogram/src/{store/member.ts,api/client.ts}`

**前置**：[`../customer-member/spec.md`](../customer-member/spec.md)（会员一期，已实施）

---

## 0. 需求与一句话方案

**需求**：会员功能本期**只在小程序上线**，但身份层要为 App、网站预留好，后续接入时不返工。

**一句话方案**：把「账号」与「登录方式」拆成两张表 —— `tb_customer` 只存档案，
新增 `tb_customer_identity` 存「哪种登录方式 + 该方式下的用户标识」。
本期只实现 `provider = wx_mp`（微信小程序 openid），其余方式按需追加实现，
**接口契约、令牌、订单归属、中间件全部不动**。

**本期交付的只是地基**：管理端会员页、手机号、积分储值一律不在范围内（见 §10）。

---

## 1. 现状与耦合点（代码调研事实）

### 1.1 已经与平台无关的部分（本期不动）

| 层 | 现状 | 为什么通用 |
|---|---|---|
| 账号主键 | `tb_customer.customer_id` | 与登录方式无关；`tb_order.customer_id` 已按它归属 |
| 令牌 | `customer\|<id>\|<exp>` + HMAC-SHA256 | 载荷无平台痕迹，App/网站可直接复用同一套 |
| 中间件 | `CustomerAuthOptional` / `CustomerAuthRequired` | 只解析令牌并查档案，不关心身份来源 |
| 接口路径 | `/api/customer/auth/login`、`/auth/profile`、`/order/mine` | 与平台无关 |

**结论：这四层是现有设计做对的地方，本期一行不改。**

### 1.2 真正的硬耦合（本期要拆的）

```461:473:backend/infrastructure/store/schema.go
`CREATE TABLE IF NOT EXISTS tb_customer (
		customer_id      {{PK}},
		openid           VARCHAR(64)  NOT NULL,
```

| # | 耦合点 | 后果 |
|---|---|---|
| 1 | `tb_customer.openid NOT NULL` | 建档必须带 openid；App/网站用户没有 openid，**根本建不了档** |
| 2 | `idx_customer_openid` 唯一索引（`schema.go:718`） | 一个账号只能绑一个 openid，同一个人的小程序/公众号/App 三个 openid 无法共用一个档案 |
| 3 | `CustomerLogin(ctx, code, ip)`（`customer_member_service.go:40`） | 业务函数签名直接吃 `code`，登录方式写死在函数体里 |
| 4 | 登录请求体只有 `{code}`（`customer_auth.go:84-95`） | 契约里没有「登录方式」这一维度 |

### 1.3 顺手发现的两个问题

**(a) 一条注释与实现不一致**

```194:197:backend/application/service/auth_service.go
// 短有效期换来「停用会员」更快生效 —— 令牌无状态,不查库就无法感知 status 变化。
const customerTokenTTL = 2 * time.Hour
```

但 `AuthenticateCustomerToken` **每次请求都查一次库**：

```258:273:backend/application/service/auth_service.go
func AuthenticateCustomerToken(tok string) (*po.Customer, CustomerAuthzStatus) {
	...
	c, err := dao.GetCustomerByID(id)
	...
	if c.Status != po.CustomerStatusEnabled {
		return nil, CustomerAuthzDisabled
```

即「停用立即生效」并不依赖短 TTL。这个结论很关键：**Web/App 用长令牌不会削弱停用能力**，
不需要引入 refresh token 那套复杂度（见 §3.3）。

**(b) 会员开关的口径绑死了微信配置**

`service.MemberEnabled()` 现在等价于「小程序 AppID + AppSecret 已配置」。
一旦接上手机号登录，没配微信的部署也应该能开会员 —— 该开关需要改成
「任一 provider 可用即开启」，或直接用一个独立的配置项（见 §12 待拍板 D5）。

---

## 2. 产品与技术决策

| # | 决策 | 推荐 | 理由 |
|---|---|---|---|
| D1 | 本期实现范围 | **只做 `wx_mp`** | 小程序是当前唯一入口，其余 provider 只留结构不写实现 |
| D2 | 表结构何时到位 | **现在就拆**（本期） | 会员刚上线、数据量极小；等接 App 再拆要连带订单归属/令牌/全部顾客接口回归 |
| D3 | 是否设 identity 级状态 | **否** | 账号可用性只在 `tb_customer.status` 一处裁决，避免「两个 status 谁生效」；解绑=物理删行 |
| D4 | 登录接口是否改路径 | **否**，只加 `provider` 字段且缺省 `wx_mp` | 小程序端**零改动**，契约已通用 |
| D5 | 会员开关口径 | 改为「任一 provider 可用即开启」 | 否则接手机号后，未配微信的部署无法开会员 |
| D6 | 是否引入 refresh token | **否** | §1.3(a)：停用靠查库而非短 TTL，长令牌 + 过期重登已够 |
| D7 | unionid | **预留列，本期不写逻辑** | 成本近零；真要跨端合并时再说（见 §3.2） |

---

## 3. 目标模型

### 3.1 新表 `tb_customer_identity`

```sql
CREATE TABLE IF NOT EXISTS tb_customer_identity (
	identity_id     {{PK}},
	customer_id     INTEGER      NOT NULL,   -- 指向 tb_customer.customer_id
	provider        VARCHAR(32)  NOT NULL,   -- 登录方式：wx_mp / wx_oa / wx_app / phone / password / apple
	external_id     VARCHAR(128) NOT NULL,   -- 该方式下的用户标识：openid / 手机号 / 登录名 / apple sub
	credential      VARCHAR(255) DEFAULT '', -- 仅 password 用（bcrypt 60 字符）；wx_* 为空
	unionid         VARCHAR(64)  DEFAULT NULL,
	last_login_time {{TSTYPE}} DEFAULT NULL,
	create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
	update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
){{OPTS}};
```

索引：

| 索引 | 定义 | 作用 |
|---|---|---|
| `idx_identity_provider_external` | UNIQUE(`provider`, `external_id`) | 登录查档主路径；**同时兜底并发首登**（替代原 `idx_customer_openid` 的这层职责） |
| `idx_identity_customer` | (`customer_id`) | 查「这个人绑了哪几种登录方式」（后续解绑/合并要用） |
| `idx_identity_unionid` | (`unionid`)，**非唯一** | 同一人的多个 openid 共享 unionid，唯一索引会直接冲突 |

几点约定：

- **不设 `status` 列**：见 D3。
- **`external_id` 用 `VARCHAR(128)`** 而非 64：openid 通常 28 字符，但手机号/邮箱/登录名都要装得下，留一次余量。
- **`unionid` 允许 NULL**：两库的唯一/普通索引都把 NULL 视为不同值，多个 NULL 不冲突。
- **无种子数据** → `SeedCounts()` 与 `sqlgen_test.go` 无需登记（同 `tb_customer`）。
- 表含 `update_time` → 会被 `updateTimeTables` 自动推导，SQLite 的 `trg_tb_customer_identity_touch`
  由 `touchTriggerStatements` **自动生成**，不需要手工维护（`schema.go:631` 是按 DDL 推导的）。

### 3.2 provider 规划

| provider | external_id | 场景 | 排期 |
|---|---|---|---|
| `wx_mp` | 小程序 openid | 小程序（**本期**） | P0 |
| `wx_oa` | 公众号 openid | 网站走微信网页授权 | 需要网站会员时 |
| `wx_app` | App 端 openid | App 微信登录 | 需要 App 时 |
| `phone` | 手机号 | App/网站验证码登录，**跨端唯一可靠锚点** | P2 |
| `password` | 登录名 | 网站账号密码（复用 `store/password.go` 的 bcrypt） | 可选 |
| `apple` | Apple `sub` | iOS 含第三方登录时上架强制要求 | 上架前 |

**枚举值现在就定死**，未实现的 provider 统一返回「暂不支持该登录方式」。
这样前端/接口契约先稳定，后面加端只加一个校验器实现。

**关于 unionid 的边界**：只有把小程序 + 公众号 + App 绑到**同一个微信开放平台账号**下才有值。
它能免手机号识别同一人，但覆盖不了「网页扫码用微信、App 用手机号」这类混搭。
真正稳定的跨端锚点仍是手机号 —— 所以 §3.1 的 `phone` provider 不是可选项，是终局。

### 3.3 令牌：TTL 按端区分，载荷不动

`exp` 本身就是唯一的时效字段，**签发时按 provider 传不同 TTL 即可，载荷形态不用改**：

| 端 | TTL | 理由 |
|---|---|---|
| 小程序 | 2h | `wx.login` 静默免费，401 自动重登，顾客无感知 |
| 网站 | 7d | 没有静默重登，2h 会被用户骂 |
| App | 30d | 同上，且 App 通常「一次登录长期有效」 |

依据 §1.3(a)：停用由每次请求查库裁决，与 TTL 长短无关。
代价是**每个顾客请求一次主键查询** —— 现在小程序量级无所谓，接网站前
建议加一层 30~60s 的进程内缓存挡一下（注意：加了缓存后停用会延迟到缓存过期，
届时必须在注释里写清这个取舍）。

---

## 4. 接口契约（向后兼容）

```http
POST /api/customer/auth/login
Content-Type: application/json

{ "provider": "wx_mp", "code": "<wx.login 的 code>" }   // provider 缺省 = wx_mp
```

响应体**完全不变**：

```json
{ "code": 200, "data": { "token": "...", "isNew": true,
  "customer": { "customerId": 1, "nickname": "微信用户", "avatarUrl": "",
                "memberSince": "2026-10-18 10:00:00", "orderCount": 0 } } }
```

后续 provider 的请求体（本期不实现，仅登记契约）：

| provider | 请求体 | 备注 |
|---|---|---|
| `phone` | `{ "provider":"phone", "phone":"138...", "smsCode":"123456" }` | 验证码只在服务端生成存储，**绝不回传前端** |
| `password` | `{ "provider":"password", "account":"...", "password":"..." }` | bcrypt 校验，复用 `store/password.go` |

其余接口（`/auth/profile`、`/order/mine`）与下单/加菜的 `CustomerAuthOptional` **一律不动**。

### 4.1 会员运营闭环（P2 功能，已实施）

| 方法与路径 | 鉴权 | 说明 |
|---|---|---|
| `POST /api/customer/auth/profile` | 顾客令牌 | 改昵称（≤32 字，去首尾空格）；头像原样保留 |
| `DELETE /api/customer/auth/account` | 顾客令牌 | 注销：事务内删档案 + 删登录方式 + 订单归属置 0 |
| `GET /api/admin/member/list` | `member:view` | 关键词（昵称）+ 状态筛选 + 分页；聚合登录方式 |
| `POST /api/admin/member/status` | `member:edit` | 启停用；停用即让该会员的现有令牌失效 |

**后台刻意不提供新建 / 删除会员**：档案必须由顾客自助注册产生（要带一条登录方式），
后台代建只会留下谁也登不进去的孤儿档案；删除是顾客自己的权利（「我的」页有入口）。

为什么注销要把订单归属置 0 而不是删订单：订单是门店的经营数据与报表口径来源，
删掉会让营业额凭空减少；置 0 后它变回匿名单，历史账目不受影响。

---

## 5. 登录流程（含并发首登）

```
loginByProvider(ctx, provider, payload, ip)
  ├─ 限流：按 provider 走各自的窗口（见 §5.2）
  ├─ extID, unionid, err := verify(provider, payload)
  │     wx_mp => weapp.Code2Session(ctx, code)
  ├─ 快路径：cust := dao.GetCustomerByIdentity(provider, extID)
  │     └─ 命中 → 校验 tb_customer.status → TouchCustomerLogin → 签发令牌
  └─ 慢路径（首次出现）：事务内建档 + 绑定
        BEGIN
          id := INSERT tb_customer(...)                        -- 只写档案，不含任何凭据
          INSERT tb_customer_identity(id, provider, extID, unionid)
        COMMIT
        └─ 唯一冲突（并发首登）→ ROLLBACK → 按 (provider, extID) 重查 → 登录分支
```

### 5.1 事务是必须的（本期最容易写错的一点）

现在的实现靠 `tb_customer.openid` 的唯一索引挡并发：

```66:82:backend/application/service/customer_member_service.go
	id, insErr := dao.InsertCustomer(po.Customer{OpenID: openid})
	if insErr != nil {
		// 并发首登:另一请求刚建档(唯一索引冲突)。转按 openid 重查走登录分支。
		cust, err = dao.GetCustomerByOpenid(openid)
```

拆表后唯一约束落在 `tb_customer_identity` 上。如果「插档案」和「绑身份」分两个独立事务，
并发首登会留下一条**孤儿 `tb_customer` 行**（档案建好了，身份绑定失败回滚，没有登录方式指向它）。
必须把两条 INSERT 放进**同一个事务**，冲突时整体回滚再重查。

> 验收方式见 §9 的「并发首登只产生一个会员」用例，这条必须覆盖。

### 5.2 限流按 provider 分开

现有限流 `allowCustomerLogin(ip)` 是 30 次/分/IP，只管 `code2session`。
新增 provider 时**必须各做一套**，不能共用：

| provider | 维度 | 建议阈值 | 防的是什么 |
|---|---|---|---|
| `wx_mp` | IP | 30 次/分（现状） | 刷微信接口额度 |
| `phone` | 手机号 + IP 双维度 | 单号 60s/次、5 次/天；单 IP 20 次/分 | **短信轰炸 + 费用** |
| `password` | 账号 + IP | 同管理端的 5 次失败锁 15 分钟 | 撞库 |

---

## 6. 前端通用化

### 6.1 改造前：`safeStorage` 是 Web 专属的（已修）

改造前它直接读 `window.localStorage`，小程序只能另写一套 `wx.getStorageSync`：

小程序现在靠 `apps/miniprogram/src/api/client.ts` 单独注册拦截器绕过去。要三端通用，
照 `setUiNotifier`（`packages/shared/src/ui.ts`）的既有模式加一个存储后端注入口：

```ts
// @dining/shared/src/storage.ts
export interface StorageBackend {
  get(key: string): string | null;
  set(key: string, value: string): void;
  remove(key: string): void;
}

let backend: StorageBackend = webBackend;          // 默认 localStorage 实现
export function setStorageBackend(b: StorageBackend): void { backend = b; }
export const safeStorage = { /* 转发到 backend，读写失败静默 */ };
```

小程序在 `client.ts` 里注入 `wx.getStorageSync` / `setStorageSync` / `removeStorageSync`。

> ⚠️ 改动 `safeStorage` 会同时影响 `visitor.ts` 的访客 ID 存储，改完必须回归
> 「同一设备两次扫码仍是同一访客」。

### 6.2 令牌注入按 URL 前缀分流（已实施）

改造前 `packages/api/src/http.ts` 的请求拦截器**只注入管理端令牌**（`LS_TOKEN`），
小程序的顾客令牌靠 `api/client.ts` 里另注册一个拦截器补上。现在按前缀选择：

```ts
// packages/api/src/http.ts
const token = safeStorage.get(isCustomerPath(config.url) ? LS_CUSTOMER_TOKEN : LS_TOKEN);
```

`apps/miniprogram/src/api/client.ts` 里那段独立的顾客令牌拦截器已删除，
三端共用同一份注入逻辑。

> ⚠️ `LS_CUSTOMER_TOKEN` **不要**加进 `authKeys.ts` 的 `AUTH_KEYS` ——
> 那个列表是「管理端登出时清空」，把顾客令牌放进去会导致管理端一退出，顾客也被踢下线
> （同一浏览器里管理端和顾客端是两个身份）。

### 6.3 会员接口下沉到 `packages/api`（已实施）

改造前 `memberLogin / memberProfile / memberOrders` 全写在小程序 `store/member.ts` 里
直接 `http.post('/customer/auth/login')`。现已下沉到 `packages/api/src/customer.ts`，
小程序只保留「拿凭据」（`wx.login`）这一段；网站/App 接进来时换成短信验证码即可，
页面代码不用重写。

---

## 7. 迁移方案（分两步，不要一步到位）

### 7.1 P0（本期）：只加表 + 搬数据，不动老列

```
1. CREATE TABLE tb_customer_identity + 3 个索引
2. INSERT ... SELECT customer_id, 'wx_mp', openid FROM tb_customer WHERE openid <> ''
3. 代码改为读 tb_customer_identity
4. tb_customer.openid 保留且继续写入（NOT NULL + 唯一索引仍在）
```

**为什么 P0 不删 `openid` / 不删唯一索引**：本期只实现 `wx_mp`，openid 始终有值，
老约束不碍事；保留它还能在过渡期多一道保险。少动一次表结构，少一次回归风险。

### 7.2 P2（已实施 2026-10-18）：直接删列

**结论先行**：不需要整表重建。SQLite 3.35+ 与 MySQL 5.7+ 都支持
`ALTER TABLE ... DROP COLUMN`（项目内置 SQLite 3.46，且 incr 30 号已有删列先例），
所以只要**先删索引再删列**即可：

| 步骤 | SQLite | MySQL |
|---|---|---|
| 1. 搬迁（兼容跳过 P0 的库） | `INSERT ... SELECT` `tb_customer.openid` → `tb_customer_identity` | 同左 |
| 2. 删唯一索引 | `DROP INDEX idx_customer_openid` | `DROP INDEX idx_customer_openid ON tb_customer` |
| 3. 删列 | `ALTER TABLE tb_customer DROP COLUMN openid` | 同左 |

> ⚠️ 顺序不能反：SQLite 不允许删除**仍被索引引用**的列。
>
> ⚠️ 不建议「把列放开可空」那条路 —— 只删唯一索引而不删列，多行空串虽然不再冲突，
> 但会留下一列永远没人读的死数据，且 `NOT NULL` 仍要求建档时写一个假值。
> 直接删列最干净。

**启动迁移（关键）**：`deploy.sh` 不跑 `incr/` 脚本，老库升级靠的是后端启动时的
`store.migrateCustomerOpenid()`（`schema.go`）。它做三件事且完全幂等：

1. 列不存在 → 直接返回（新库 / 已升级过的库）；
2. 列存在 → 先搬迁（`NOT EXISTS` 守卫，兼容从未跑过 P0 的库），再删索引、删列；
3. 搬迁或删索引失败 → `Fatalf` 中止启动，**绝不在数据未搬完时删列**。

删列失败也中止启动：否则建档会因 `openid` 缺失而失败，线上表现是「注册不了会员」，
比启动失败更难排查。

### 7.3 脚本与生成物

| 动作 | 命令 / 位置 |
|---|---|
| 改表结构定义 | `backend/infrastructure/store/schema.go`（`schemaTemplate` + `indexDefs`） |
| 重新生成 full 脚本 | `cd backend && go run ./cmd/gensql` |
| 手工补 incr 脚本 | `migrations/incr/{sqlite,mysql}/20261018000000_tb_customer_identity_create.sql`（P0）、`20261018010000_tb_customer_drop_openid.sql`（P2） |
| 校验一致性 | `go run ./cmd/gensql -check`（CI 等价；`sqlgen_test.go` 会在 `go test` 时自动跑） |

`migrations/README.md` 的「表清单 / 索引清单」已同步为 **26 张 / 41 个**
（P0 时是 42 个，P2 删掉了 `idx_customer_openid`）。

---

## 8. 文件级改动清单

### 后端

| 文件 | 改动 |
|---|---|
| `domain/po/customer.go` | 新增 `CustomerIdentity` 结构；P2 已删除 `Customer.OpenID` |
| `infrastructure/store/schema.go` | `schemaTemplate` 加表；`indexDefs` 加 3 个索引；P2 删除 `tb_customer.openid` 列与 `idx_customer_openid`，并新增启动迁移 `migrateCustomerOpenid()` |
| `infrastructure/store/dao/customer.go` | 新增 `GetCustomerByIdentity` / `InsertIdentityTx` / `TouchIdentityLogin` / `CreateCustomerWithIdentity`；P2 删除 `GetCustomerByOpenid` 与所有 `openid` 读写 |
| `application/service/customer_member_service.go` | `CustomerLogin` 改为按 provider 分派；`customerLoginByIdentity` 承接查档/建档/签发；建档+绑定改为单事务 |
| `interfaces/handler/customer_auth.go` | login 收 `provider`（缺省 `wx_mp`），未实现的 provider 返回「暂不支持该登录方式」 |
| `application/service/customer_service.go` | `memberEnabledFlag()` 改口径（D5） |
| `infrastructure/weapp/auth.go` | **不动**（它就是 `wx_mp` 的实现） |
| `main.go` | **不动**（无新增路由） |

### 前端

| 文件 | 改动 |
|---|---|
| `packages/shared/src/storage.ts`（新） | `setStorageBackend` + `safeStorage` 转发；默认 Web 后端（localStorage） |
| `packages/shared/src/sessionStorage.ts`（新，取代 `safeStorage.ts`） | 只剩 Web 专属的 `safeSession`（随标签页生命周期，三端不通用） |
| `packages/shared/src/authKeys.ts` | 加 `LS_CUSTOMER_TOKEN`（**不进** `AUTH_KEYS`） |
| `packages/api/src/http.ts` | 按 URL 前缀注入顾客/管理端令牌 |
| `packages/api/src/customer.ts` | 新增 `memberLogin / memberProfile / memberOrders` |
| `apps/miniprogram/src/api/client.ts` | 注入存储后端；删掉独立的顾客令牌拦截器 |
| `apps/miniprogram/src/store/member.ts` | 改调 `@dining/api`，登录逻辑（单飞/401 重登）保持不变 |

---

## 9. 测试与验收

| 层 | 验收点 |
|---|---|
| 数据迁移 | 老库执行 incr 脚本后，`tb_customer` 每行都能在 identity 表找到唯一一条 `wx_mp` 映射；重复执行零变更。**P2**：启动迁移后 `openid` 列与 `idx_customer_openid` 均已消失，且能新增无 openid 的档案（`TestMigrateCustomerOpenidFromLegacyDB`） |
| P2 解锁 | 非微信 provider（phone / password / apple）可各自独立建档（`TestNonWechatProviderCanRegister`）—— 这是 P0 阶段被旧唯一索引挡住的能力 |
| 登录语义 | 新客建档（`isNew=true`）、老客复登（`isNew=false`）、停用会员被拒 |
| **并发首登** | 同一 openid 并发登录，**只产生一个 `tb_customer` 行**、一个 identity 行（§5.1 的孤儿行回归） |
| 契约兼容 | 请求体不带 `provider` 时按 `wx_mp` 处理；现有小程序端代码不改也能登录 |
| 未实现 provider | `provider=phone` 返回明确业务提示，不 500、不落库 |
| 令牌回归 | `TestCustomerTokenLifecycle` 保持绿（载荷未变）；与管理端令牌仍互拒 |
| 匿名链路 | `routes_test.go` 的 `TestCustomerRoutesStayPublic` 保持绿；无令牌下单 `customer_id=0` |
| 生成物一致性 | `go run ./cmd/gensql -check` 通过；`sqlgen_test.go` / `schema_dedup_test.go` 绿 |
| 前端 | 会员三接口从 `@dining/api` 调用；小程序 `npm run mp:verify` 六道门全过；访客 ID 跨端仍一致 |

**不改的测试**：`customer_member_test.go` 里「openid 绝不外发」的断言必须继续通过
（新增 identity 表后，openid 更不能出现在任何响应里）。

---

## 10. 风险与明确不做的事

### 风险

1. **并发首登的孤儿行**（§5.1）—— 本期最容易写错的地方，必须单测覆盖。
2. **`safeStorage` 改动波及访客 ID**（§6.1）—— 回归「同一设备两次扫码仍是同一访客」。
3. **令牌被管理端登出清掉** —— `LS_CUSTOMER_TOKEN` 绝不能进 `AUTH_KEYS`（§6.2）。
4. **P2 的整表重建** —— SQLite 不能 `ALTER COLUMN`，靠启动迁移做不了，必须落脚本。
5. **每请求一次主键查询** —— 接网站前评估，别等到压测才发现。

### 不做

| 项 | 原因 |
|---|---|
| 后台新建 / 删除会员 | 档案由顾客自助注册与注销；后台代建会留下没有登录方式的孤儿档案 |
| 顾客自助上传头像 | 现有上传接口要管理端权限，给顾客开一条通道是另一个功能（还要防滥用） |
| 手机号 / 密码 provider 的实现 | 只登记枚举，实现放后续 |
| 积分 / 储值 / 优惠券 | 要新增账户表 + 流水表 + 退款冲正，等运营方案定了再动表结构 |
| H5 扫码点餐接入会员 | 保持匿名，`CustomerAuthOptional` 继续放行无令牌请求 |
| refresh token | D6：停用靠查库，长令牌已够 |

---

## 11. 分期

| 期 | 内容 | 成本 |
|---|---|---|
| **P0（本 spec）** | 拆 `tb_customer_identity`、只接 `wx_mp`、接口加 `provider` 缺省值、前端存储/令牌通用化 | 1 张表 + 1 次搬迁 + DAO 局部改造，**小程序端零改动** |
| **P2 结构解耦** | ✅ 已完成：删 `idx_customer_openid` + 删 `tb_customer.openid` 列，登录完全走 identity 表 | 1 次启动迁移 + 2 个 incr 脚本 |
| **会员运营闭环** | ✅ 已完成：管理端会员页（列表/搜索/启停用，`member:view` / `member:edit`）、顾客自助改昵称（`POST /customer/auth/profile`）、自助注销（`DELETE /customer/auth/account`） | 2 个接口 + 1 个权限模块 + 1 个管理页 |
| P1 | JSAPI 支付 + 订阅消息（openid 已备好，见 `docs/miniprogram.md` 4.1 / 4.3） | 中 |
| P2 功能 | 手机号验证码（App/网站）—— 唯一还缺的对外能力 | 中大 |
| P3 | 积分 / 储值 / 优惠券，挂 `customer_id` | 大 |

---

## 12. 决策记录（P0 + P2 已按推荐执行）

1. **D1** 本期只实现 `wx_mp`，其余 provider 仅登记枚举 ✅
2. **D3** identity 表不设 `status`，账号可用性只由 `tb_customer.status` 裁决 ✅
3. **D5** 会员开关改为「任一 provider 可用即开启」（`service.MemberEnabled` 遍历
   `customerProviderEnabled`），不再直接判断微信配置 ✅
4. **令牌 TTL 分端**：本期只有小程序，维持 2h 不变；网站 7d / App 30d 的值等到接端时
   随 provider 一起定（签发文签名不变，只改 TTL 参数即可）⏸ P2
5. **管理端会员总闸** ✅ 已落地：`tb_config` 增加 `member_enabled`（出厂默认 "1"，
   「默认开」语义：只有明确 "0" 才关），由管理端「营业设置 → 会员功能」控制。
   它是唯一的显隐/放行口径 —— 公开配置下发的 `member_enabled` 直接取库值，
   **不再与「任一 provider 可用」取与**（显隐归商家运营决定，不被 AppSecret 等
   技术配置顺手决定）；登录与静默重登在 `CustomerLoginByProvider` 入口同受总闸约束。
6. **P2 采用「直接删列」而不是「放开可空」** ✅：`DROP COLUMN` 两库都支持（SQLite 3.46），
   先删索引再删列即可，无需整表重建；只放开可空会留下一列永远没人读的死数据。
   老库由启动迁移 `migrateCustomerOpenid()` 自动完成（不依赖运维跑脚本）。
