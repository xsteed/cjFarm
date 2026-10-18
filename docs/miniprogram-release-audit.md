# 小程序上线运营审计

> 审计对象：`frontend/apps/miniprogram`（微信扫码点餐顾客端，`appid: wx0f4ab661c6cbe4eb`）
> 审计日期：2026-10-19
> 结论：**代码质量高，但提审前仍有必须人工完成的平台侧动作**。代码能修的已全部修掉，详见第二节。

---

## 一、整体结论

| 类别 | 数量 | 状态 |
|---|---|---|
| 代码侧安全缺陷 | 1 | ✅ 已修（订单可被凭单号遍历） |
| 代码侧硬缺陷 | 1 | ✅ 已修（静默重登死链路） |
| 代码侧合规缺失 | 4 | ✅ 已修（隐私授权、协议入口、协议可查阅、更新管理） |
| 健壮性 / 记账缺陷 | 2 | ✅ 已修（购物车 key 残留、`round2` 负数偏置） |
| 构建配置优化 | 2 | ✅ 已修（压缩、禁传 sourceMap） |
| 环境配置 | 2 | ✅ 已改造（域名按 envVersion 自动切换、兜底桌台码不再需清空） |
| 平台侧强制动作 | 6 | ⬜ 待办，**代码无法替代** |
| 运营 / 体验缺口 | 6 | ⬜ 待决策 |

`npm run verify`（typecheck + lint + audit + 236 个单测 + 冒烟）当前全绿。

> 审计过程中反复核对过、确认**没有问题**的几处，也一并记下，省得下次重查：
> 定时器回收（订单页倒计时 / 菜单页滚动锁与飞球动画 / 支付页延时跳转都有 `onUnload` 兜底）；
> 支付与登录的并发（pay 的 `querying` 锁、member 的 single-flight）；
> 菜单页用 `Promise.all` 刻意 fail-fast —— 公开配置拉不到时应拒绝展示金额，
> 悄悄降级反而会让前端少算茶位费、与后端实收对不上。

---

## 二、代码侧已修复的问题

按轮次排列。第一、二轮的改动都在 `npm run verify` 里守着。

### 2.0 🔴🔴 `getOrderByNo` 从不传 tableId → 订单可被人凭单号遍历（第二轮）

后端在 `CustomerOrderByNo` 里明确写了归属校验，注释也很直白：

```go
// 归属校验:调用方(扫码进来的桌台)带 tableId 时校验订单归属,防止凭单号
// 遍历查询他人订单(单号会出现在小票/照片/群聊里)。老二维码链接未带桌台
// 参数时退化为仅按单号查询,由本路由上的 IP 限流兜底(见 main.go)。
tableID, _ := strconv.Atoi(c.Query("tableId"))
```

```go
// application/service/customer_service.go:389
if tableID > 0 && o.TableID != tableID {
    return dto.Order{}, errors.New("订单不存在或已结束")
}
```

而前端 `api/client.ts:64` 声明了这个可选参数，可 **5 个调用点一个都没传**。这等于把后端准备好的防线拆掉：订单号是小票上的短号/顺序号，任何人拼一个 URL 就能读到任意订单的桌台、明细、金额、**顾客写的备注**（还有专门的 IP 限流中间件兜底，说明后端团队清楚这个接口的风险）。

修复：5 处全部带上当前桌台。

| 位置 | 说明 |
|---|---|
| `pages/order/index.ts` `recover` | 本桌订单回拉 |
| `pages/order/index.ts` `tick` | 3s 轮询。只改 recover 等于给高频请求留一扇敞开的门 |
| `pages/pay/index.ts` `recover` | 结账页回拉 |
| `pages/pay/index.ts` `checkPaid` 成功后回拉 | 支付成功后的详情刷新 |
| `pages/cart/index.ts` 下单后回拉 | 该单刚由本桌创建，必定匹配，不存在误伤 |

`history-detail` 用的是 `getMyOrderDetail`（走会员令牌），不是这条路径，**不在此列也不该改**。

**为什么不会有误伤**：后端只在 `tableID > 0` 时校验，会话还没拿到桌台详情时为 0，自动退化成原来「不限桌」的行为，顾客不会因为「刚进来还在加载」就看到订单不存在。

回归测试：`tests/pages/order/ownership.test.ts`，两个用例分别锁住「带 tableId」与「tableId=0 时不带」。已做反向验证 —— 把修复删掉测试立刻失败。

### 2.0.1 🔴 购物车持久化 key 残留（第二轮）

`dining_mp_cart:{桌台码}:{订单号|new}` —— key 里带订单号，而 restoreCart 要求「同一桌 + 同一订单号」同时成立。同一个订单号不会第二次出现在同一张桌上，所以一旦开了单（`:new` → `:{orderNo}`），旧 key 就成了**永远读不到的孤儿**，在 10MB storage 里持续沉淀。

最典型的一步就是下单成功：`cartClear()` 时 key 已经变成 `:{orderNo}`，清的是新 key，`:new` 那份原封不动留着。

修复：在 `saveCart` 里感知 key 变化、显式删掉旧记录。选写入侧是因为 saveCart 是所有写操作的唯一出口，在这里感知最可靠；判断放在「清空」分支之前，正好覆盖上面那一步。

### 2.0.2 🟡 `round2` 对负数的偏置方向错了（第二轮）

```ts
return Math.round((v + Number.EPSILON) * 100) / 100;   // 无条件加
```

修正量必须跟着符号走，否则 `round2(-0.145)` 落在 `-0.14` 而不是对称的 `-0.15`。当前用到 round2 的地方（菜品合计、茶位费、满减、合计）都非负，所以现在看不出来；但只要有负数进场（退款、差额调整），H5 与小程序就会各自给出差 1 分的结果 —— 正是这函数被造出来要消灭的那类漂移。

### 2.1 🔴 静默重登是死链路（第一轮，真实缺陷）

`api/http.ts` 的 401 分支是**先清令牌、再回调**：

```ts
// src/api/http.ts:146-147
safeStorage.remove(LS_CUSTOMER_TOKEN);
onUnauthorized();
```

而 `store/member.ts` 的原本判据是「本地是否还有令牌」：

```ts
if (!currentCustomerToken()) return;   // 走到这里时令牌恒为空 → 永远 return
```

结果：**静默重登一次都没有真正执行过**。只有 `memberedGet` 包装的 4 个接口（会员档案、历史订单）能靠自己的 401 重试恢复；而钱包余额、绑定手机号、充值、注销、修改昵称这些直接调 `api/client` 的接口，401 后不会自动恢复。

线上表现：令牌 2h 过期后，顾客进「我的」看不到储值余额，结账页的「余额支付」按钮凭空消失，且一直到他完全退出小程序重进才恢复。

**修法**：判据改为「本次会话是不是会员」（`memberState().customerId !== 0`）。该字段由 `restoreMember`/`memberLogin` 置为非 0、`memberLogout` 清 0，因此「匿名顾客不会被自动注册成会员」这条边界仍然成立。

### 2.2 🔴 隐私合规：一套完整补上

这是提审最可能被驳回的一项。代码使用了受《小程序用户隐私保护规范》列管的手机号接口：

```xml
<!-- src/pages/profile/index.wxml -->
<button open-type="getPhoneNumber" bindgetphonenumber="onGetPhoneNumber">
```

而此前全仓库**零命中** `__usePrivacyCheck__`、`wx.getPrivacySetting`、`wx.requirePrivacyAuthorize`、`wx.onNeedPrivacyAuthorization`。后果有两个：新基础库上 `getPhoneNumber` 直接拿不到 code（「绑定手机号」点了没反应，`profile/index.ts` 静默 return），以及提审驳回。

新增/改动：

| 文件 | 作用 |
|---|---|
| `src/store/privacy.ts`（新） | 集中管理一次性 resolver。取走即失效，杜绝同一 resolver 被兑现两次 |
| `src/pages/privacy/*`（新） | 隐私授权页。展示采集清单 + 用户权利，同意/不同意兑现回基础库 |
| `src/pages/agreement/*`（新） | 《用户服务协议》+《隐私政策》，`?type=terms\|privacy` 切换 |
| `src/shared/privacy-text.ts`（新） | 条款正文的唯一来源，避免授权页摘要与协议页正文漂移 |
| `src/app.ts` | 注册 `wx.onNeedPrivacyAuthorization`；新增版本更新管理器 |
| `src/app.json` | `__usePrivacyCheck__: true`；注册两个新页面 |
| `src/pages/profile/*` | 注册前协议告知（可点击查阅）、昵称修改入口接线 |

设计取舍说明：

- **做成独立页面而不是每页塞弹窗组件**：8 个页面各插一个 `<privacy-popup/>`，漏一个就是那个页面的隐私按钮失效，而且以后每加一个页面都要记得补。一个页面 + 一个全局监听覆盖全部调用点。
- **`onUnload` 兜底 `resolve({ event: 'disagree' })`**：顾客用左上角返回离开时若不兑现，resolver 会永久挂在内存里，此后每次点隐私按钮都不再回调，按钮彻底失灵且无法恢复。
- **`buttonId` 必须与 WXML 里按钮的 `id` 一致**（`agree-btn`），基础库会校验这个按钮真的被点过。

### 2.3 🟠 昵称修改此前是死代码

`profile/index.ts` 里的 `onEditNickname()` 全仓库**没有任何 UI 引用**，顾客永远碰不到，昵称一直停在后端占位值「微信用户」。已在会员卡上补「改名」入口（用 `catchtap` 避免与整卡的未登录点击冲突）。

### 2.4 🟠 顾客会长期停在旧版本

`app.ts` 原先没有 `wx.getUpdateManager()`。小程序顾客用的就是本地缓存的旧包，不加这段，修完线上 bug 后仍会有一批人停留在旧版本好几天。已加上：检测到新版本下载完成后弹一次确认框，「稍后」可被推迟到下次冷启动。

### 2.5 🟠 构建配置

| 项 | 原值 | 现值 | 理由 |
|---|---|---|---|
| `uploadWithSourceMap` | `true` | `false` | sourceMap 随包上传等于把源码交出去 |
| `minified` / `minifyWXSS` / `minifyWXML` | `false` | `true` | 上线版本应压缩，减小包体与注入耗时 |

### 2.6 🟡 预检脚本增强

`scripts/precheck-release.mjs` 增加隐私链路自查（缺 `__usePrivacyCheck__` / 缺隐私页 / 缺协议页 / 未注册监听 → 报错），并在通过后列出 5 项「代码无法自查、必须人工确认」的平台侧事项。

同时补上了文档里引用但 monorepo 根实际不存在的 `mp:precheck` 别名（`frontend/package.json`）。

> 提醒：这套检查**不在** `npm run verify` 里（开发时本机值才是正确的），提审前需单独跑 `npm run mp:precheck`。

---

## 三、⬜ 平台侧待办（代码无法替代，必须人工完成）

这 6 项做不到 = 提审被驳回或线上不可用。`npm run mp:precheck` 通过后会把它们打印出来。

1. **小程序备案（工信部）** —— 2023-09 起，未履行备案手续不得开展互联网信息服务。这一步比提审更前置。
2. **用户隐私保护指引** —— 公众平台「设置 → 服务内容声明 → 用户隐私保护指引」勾选并提交审核。指引没配，`pages/privacy` 根本不会被唤起，隐私接口直接 fail。
3. **服务器域名白名单** —— 「开发管理 → 服务器域名」配同一域名到**两处**：
   - `request` 合法域名 ← `/api/customer/*`
   - `downloadFile` 合法域名 ← `<image>` 加载菜品图与门店收款码
     漏后者表现为全站图片空白（`config.ts` 的 `mediaUrl()` 会把 `/uploads/xxx` 拼到 `API_ORIGIN`）。
4. **类目与实际服务一致** —— 扫码点餐属餐饮类目，需营业执照、食品经营许可证。类目不符是驳回高频原因。
5. **主体认证 + 微信支付** —— `wx.requestPayment` 依赖已认证主体 + 商户号与 AppID 绑定。代码侧链路本身是完整的（如下三点做得很好，不用改），资质全在后台：
   - 以**后端查单结果**而非 `requestPayment.success` 判定入账（`pay/index.ts:222-255`）；
   - 入口由 `wxpay_jsapi_enabled` 控制，未开通时按钮根本不出现（fail-closed）；
   - 未开通时降级到「收款码 + 前台结账」并有明确提示。
6. **储值 / 预付资质** —— `pages/wallet` 的余额充值属预付资金，涉及《单用途商业预付卡管理办法》，且平台对部分类目有限制。当前默认值是关闭（`wallet/index.ts` 双重 fail-closed）。
   **要开放自助充值，必须先完成 [`../compliance/prepaid-card.md`](../compliance/prepaid-card.md) 第 0 节的 10 项清单**（备案判断、规则发布、限额与实名、退卡通道、微信侧类目资质、隐私指引、发票口径）。
   系统侧已具备的合规能力（限额校验、规则同意留痕、退余额、注销清零、充值补账）见
   [`../design-docs/customer-wallet/spec.md`](../design-docs/customer-wallet/spec.md)。

---

## 四、⬜ 仍需人工决策的运营缺口

| # | 缺口 | 依赖 | 说明 |
|---|---|---|---|
| 1 | **UGC 内容安全审核** | 后端 | 订单备注、单项备注、会员昵称都是用户可控文本，会直接出现在商家端与厨房小票。目前前端只做了长度截断（`cart/index.ts:36`），无内容安全检查。建议在后端落库前接 `msgSecCheck`，并补一个举报入口。 |
| 2 | **没有客服 / 商家联系方式** | 后端 `PublicConfig` 无门店电话字段 | 全仓无 `<button open-type="contact">`、无 `wx.makePhoneCall`。顾客支付异常时无求助路径，点餐类目审核也常查售后渠道是否可见。建议后端补 `shop_phone`，前端在支付页/「我的」展示。 |
| 3 | **无订阅消息触达** | 后端模板 | 零命中 `requestSubscribeMessage`。顾客关掉小程序后完全无法召回（出餐完成、结算均无通知）。 |
| 4 | **无退款 / 发票说明** | 产品 | 支付完成后只有一句「支付成功」，退款规则零感知。协议里已写明退款路径，但支付页最好也有提示。 |
| 5 | **条款正文需替换为真实主体** | 门店 / 法务 | `src/shared/privacy-text.ts` 顶部注明了三处必改：主体名称（现为「本店」）、有效联系方式、储值规则。当前文案是可用的模板，但签署方信息必须真实。 |
| 6 | **`sitemap.json` 全 `disallow`** | 产品决策 | 现状正确（扫码私域入口不该被索引）。若日后希望门店能被微信搜一搜找到，可在通过审核后单独放开首页。 |

---

## 五、已做对的（不用动，保持即可）

- 全仓**无地理位置采集**，`app.json` 不配 `permission`/`requiredPrivateInfos` —— 配置与代码一致，这是对的；
- 定时器均有回收：`store/poller.ts` + 各页 `onUnload` 清理（订单页的催菜倒计时、菜单页的滚动锁与飞球动画、支付页的延时跳转），**没有发现泄漏**；
- 支付并发安全：`querying` 锁由手动点击与静默轮询共用；`memberLogin` 有 single-flight 防重；
- 金额最终以后端重算为准，前端只负责展示一致（`shared/money.ts`）；
- 401 → 静默重登链路的**设计**是对的，只是实现判据写错了（见 2.1）；
- 「上一桌遗留未结账订单」有认领确认（`store/session.ts`），不会把陌生人的账单摊给新客人；
- 四套自研审计脚本（wxml / wxss / smoke / precheck）+ 236 个单测，`npm run verify` 一条命令跑齐。

---

## 六、上线 Checklist

```
阶段 1 · 代码（已完成）
  [x] npm run verify                     全部通过
  [x] 隐私授权链路、协议入口、更新管理器已补

阶段 2 · 平台（人工，不可自动化）
  [ ] 完成小程序备案
  [ ] 公众平台提交《用户隐私保护指引》并审核通过
  [ ] 配 request + downloadFile 两张合法域名白名单
  [ ] 类目 = 餐饮，上传营业执照 + 食品经营许可证
  [ ] 主体认证；确认微信支付商户号已绑定
  [ ] 决定储值自助充值是否开放（开放前按 [`../compliance/prepaid-card.md`](../compliance/prepaid-card.md) 第 0 节逐项确认资质与类目）

阶段 3 · 提审前
  [ ] config.ts 的 PROD_API_ORIGIN 换成已备案 HTTPS 域名（只需填一次，不用再切换）
  [ ] npm run mp:precheck                通过
  [ ] 替换 privacy-text.ts 里的主体名称与联系方式
  [ ] 体验版真机回归：扫码进桌 → 点餐 → 下单 → 支付 → 出餐 → 结账
  [ ] 真机回归：未同意隐私时点「绑定手机号」应弹授权页；同意后能正常绑定
  [ ] 真机回归：老板端改菜品/价格后，顾客端版本更新提示能弹出

阶段 4 · 发布
  [ ] 上传 → 提交审核 → 审核通过后发布
  [ ] 线上验证：图片（含收款码）能正常加载
```

---

## 七、附录：环境配置的改造说明（第三轮）

### 7.1 为什么改：硬编码 + 手工切环境本身就是风险

最早 `config.ts` 里是一个硬编码的 `API_ORIGIN`，靠「上线前手改 + `precheck` 拦住漏改」来切环境。但这个动作是风险源：

- 改漏一次 = 正式版全站请求失败，真机上没有任何降级路径；
- 来回切会在 git 里留噪音，也容易把生产域名错误地提交回开发分支；
- `WEB 端不需要这个值`（前后端同域，走相对路径 `/api`），所以团队对它天然缺乏肌肉记忆。

**Web 端与小程序的差异决定了这件事不能照搬**：Web 端有同域这条路，小程序必须写死绝对 HTTPS 域名，所以它天然需要环境档位。

### 7.2 改成了什么

```ts
const DEV_API_ORIGIN = 'http://127.0.0.1:8080';
const PROD_API_ORIGIN = 'https://YOUR-DOMAIN.example.com';   // 上线前改这一处

export const API_ORIGIN = isDevelopEnv() ? DEV_API_ORIGIN : PROD_API_ORIGIN;
```

档位取自 `wx.getAccountInfoSync().miniProgram.envVersion` —— 这个判据代码里本来就在用（`devTableCode`），不是新引入的机制：

| envVersion | 连到 |
|---|---|
| `develop`（开发者工具 / 真机调试） | `DEV_API_ORIGIN` |
| `trial` / `release`（体验版 / 正式版） | `PROD_API_ORIGIN` |

> **当前状态：`PROD_API_ORIGIN` 是占位符**（备案域名审批中，域名下来后填一次即可）。
> 这不影响开发联调（开发版走本机），但**不能打体验版**：体验版走生产分支，
> 请求会全部失败。为免排查时只看到一堆 `request:fail`，`app.ts` 的 `onLaunch` 在
> 非开发环境且域名仍是占位符时会打印一条明确的中文告警。

**效果**：从「每次发版前手工改 2 处代码」变成「只填一次生产域名，之后永不切换」。预检从 3 项降到 1 项不可自动化的缺失。

### 7.3 为什么不走 `.env` / 构建注入

技术上最"标准"的方案是 `esbuild define` 或 `.env` 注入，但**这个小程序的构建方式决定了它不适用**：

```json
// project.config.json
"miniprogramRoot": "src/"
```

开发者工具**直接编译 `src/` 下的 TS**，`build.mjs`（esbuild）只服务 CI 的 `dist/` 产物。任何构建期注入都只在 CI 生效，日常开发走工具编译时拿不到 —— 两套行为不一致，比现在更糟。除非把 `miniprogramRoot` 改成 `dist/`（等于要求每次改代码先 build），那是工作流的重大变更，不该为了这个问题做。

所以**运行时判档位是这里唯一零工作流改动的方案**，也是原生小程序里的通行做法。

### 7.4 `DEV_TABLE_CODE` 不再需要清空

它有 `envVersion === 'develop'` 守卫，体验版/正式版恒返回空串，留在仓库里不会泄漏到线上。清空只会白白牺牲开发便利（每次调工具都要配编译模式）。

预检的规则随之调整：从「必须为空」改成「**守卫必须还在**」—— 后者才是真正会出故障的边界（守卫被删 = 正式版默认进某张桌）。

### 7.5 一个刻意保留的不对称

两处「拿不到环境信息」时的兜底方向**相反**，且必须保持相反：

| | 兜底到 | 理由 |
|---|---|---|
| `API_ORIGIN` | `develop`（本机） | 宁可连不上（现象明显），也不能在开发时误连**生产**——后者会往真实账单写测试数据 |
| `devTableCode()` | 不生效 | 宁可让顾客扫码，也不能把他带进**别人的桌**——兜底桌台码误用的代价是点错桌 |

合并成一个 `isDevelopEnv()` 判断会让其中一侧的失效方向变坏。这一条改动时被既有单测抓到过（`tests/pages/index/entry.test.ts`），已在 `config.ts` 里注明。
