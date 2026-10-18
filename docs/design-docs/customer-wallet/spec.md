# Feature：顾客储值（小程序「我的钱包」+ 余额支付）

**作者**：CodeBuddy（设计起草 + 实施）
**日期**：2026-10-19
**状态**：已实施（合规改造：本金 / 赠金分桶、规则确认、限额、退余额、注销闸门、充值补账）
**关联代码**：`backend/domain/po/wallet.go`、`backend/infrastructure/store/dao/wallet.go`、
`backend/application/service/wallet_service.go`、`backend/interfaces/handler/wallet.go`、
`frontend/apps/miniprogram/src/pages/wallet/*`、`frontend/apps/admin/src/views/Members.vue`
**合规配套**：[`../../compliance/prepaid-card.md`](../../compliance/prepaid-card.md)
**前置**：[`../customer-member/spec.md`](../customer-member/spec.md)（会员一期）、
[`../customer-member-identity/spec.md`](../customer-member-identity/spec.md)（身份层）

---

## 0. 需求与一句话方案

**需求**：会员能在小程序里给账户储值，结账时用储值余额抵扣；门店能调账、能退余额。

**一句话方案**：储值账户挂在会员（`customer_id`）上，本金与赠金**分两个桶**；
充值走微信 JSAPI、回调入账；消费在**同一个事务**里完成「扣余额 + 落支付流水 + 订单入账」；
退款原路退回本金；退余额与注销各有硬闸门。

**这不是什么**：不是积分、不是优惠券、不是会员等级。会员系统的其余部分见 §8。

---

## 1. 三个产品决策

| # | 决策 | 结论 | 理由 |
|---|---|---|---|
| D1 | 储值是否绑定会员 | **是**，储值账户 1:1 挂在 `tb_customer` | 余额是顾客的财产，必须有明确归属；匿名单在支付时被认领（见 §4.2） |
| D2 | 本金与赠金是否分桶 | **分**（两个余额列） | 不分桶就无法回答「这笔钱该不该退给顾客」，只能一刀切全退 → 可被套现 |
| D3 | 自助充值是否默认开 | **默认关**（`wallet_recharge_enabled=0`） | 动钱的入口 fail-closed；关闭只隐藏充值入口，不影响已有余额消费与退款 |

---

## 2. 数据模型

```
tb_customer ──1:1── tb_wallet            (本金 balance / 赠金 balance_gift, 单位:分)
                ├── tb_wallet_txn        (每一笔变动一条, 记 balance_after 与 gift_amount)
                └── tb_wallet_agreement  (《储值规则》同意留痕: 版本 + 时间)

tb_recharge                              (充值单: R 开头, status 0 待支付 / 2 已关闭)
tb_order.pay_channel = 'wallet'          (余额支付的订单, 复用渠道流水语义)
```

### 2.1 `tb_wallet`

```sql
balance       INTEGER DEFAULT 0,  -- 本金(分):顾客真金白银充进来的,可退
balance_gift  INTEGER DEFAULT 0,  -- 赠金(分):赠送得来,可消费、不可退现
total_recharge INTEGER DEFAULT 0, -- 累计充值(统计口径,不用于对账)
total_consume  INTEGER DEFAULT 0, -- 累计消费
total_gift     INTEGER DEFAULT 0  -- 累计赠送
```

**三条不变式**（`dao/wallet.go` 顶部有同一份说明）：

1. 余额只在**事务内**增减，`UPDATE` 必带 `balance + ? >= 0` 守卫 —— 并发下「先查再扣」必超扣；
2. 每一次变动都落一条 `tb_wallet_txn`，`balance_after` 由同一事务回读算出，逐行累加等于当前余额；
3. 充值入账用条件更新（`tb_recharge.status 0→1`）做幂等，渠道重发通知不会重复加钱。

### 2.2 流水类型（`biz_type`）

| 类型 | 含义 | 动哪个桶 | 是否计入累计列 |
|---|---|---|---|
| `recharge` | 充值（回调入账） | 本金 | `total_recharge` |
| `consume` | 消费（余额支付订单） | 先赠金后本金 | `total_consume` |
| `refund` | 订单退款退回 | 本金 | 否 |
| `gift` | 后台赠送 | 赠金 | `total_gift` |
| `gift_void` | 赠金作废（退余额时清零赠金） | 赠金 | 否 |
| `gift_back` | 赠金退回（撤销未完成的混合支付） | 赠金 | 否 |
| `adjust` | 后台扣减 / 退款回滚 | 本金 | 否 |
| `withdraw` | 退余额（本金清零） | 本金 | 否 |

> `gift_void` 不复用 `gift` 的负数：复用会让 `total_gift`（累计赠送）被减掉，
> 「一共送了多少」变成净额；顾客端也会看到「赠送 -20.00」这种像扣错账的流水。

退回与纠错性扣减**不动累计列** —— 否则「退回一笔消费」会把 `total_consume` 减成负数，报表口径就乱了。

---

## 3. 充值流程

```
小程序: wx.login → code
  → POST /customer/wallet/recharge {amount, code}          ← 四道闸门在这里
      → 建 tb_recharge(status=0) + JSAPI 下单
  → wx.requestPayment
  → 微信回调 /pay/notify/{channel}（按单号前缀 R 分流）
      → service.WalletRechargeCredit  (金额一致 + 幂等)
  → 小程序轮询 /recharge/status（最多 5 秒），没等到就调 /recharge/reconcile 补账
```

### 3.1 四道合规闸门（`service.CreateWalletRecharge`）

| 闸门 | 不满足时 | 配置 |
|---|---|---|
| 门店开放自助充值 | 拒绝 | `wallet_recharge_enabled` |
| 已同意当前版本《储值规则》 | 拒绝并提示去同意 | `wallet_rules_version` + `tb_wallet_agreement` |
| 已实名（绑手机号） | 拒绝并提示去绑定 | `wallet_realname_required` |
| 单笔区间 + 账户余额上限 | 拒绝 | `wallet_recharge_min/max`、`wallet_balance_max[_named]` |

余额上限按**本金 + 赠金合计**判定；记名与否决定走 `wallet_balance_max` 还是 `wallet_balance_max_named`。

### 3.2 入账顺序：先抢占，再动钱

`WalletRechargeCredit` 必须在加钱**之前**执行条件更新（`status 0→1`）：

```go
changed, _ := dao.MarkRechargePaidTx(tx, ...) // 先抢占（持行锁）
if !changed { return nil }                    // 并发回调已入账 → 幂等成功
dao.AdjustWalletTx(...)                       // 再动钱
```

反过来的写法（先加钱、后置已入账）在并发回调下会加两遍钱：两个事务都读到
`status=0`、各自加一次，最后条件更新只有一个成功 —— 但钱已经多了。
`TestWalletRechargeCreditConcurrent` 守住这个顺序。

### 3.3 回调丢失的自救（本轮新增）

订单支付有 `PayQuery` 主动查单兜底，充值单**原本只有回调一条路** ——
回调一丢就是「顾客已扣款、余额永远不到账」，且无法自愈。

新增两条通道：

- `POST /customer/wallet/recharge/reconcile`：把本人 24h 内 `status=0` 的充值单
  逐个向渠道查单，成功即补入账。小程序**进钱包页就调一次**，充值轮询落空时也调一次。
- `dao.ExpirePendingRecharges`：启动与每 6 小时关掉超过 24 小时仍未支付的单（远超
  `prepay_id` 的 2 小时有效期）。已关闭的单回调到达时返回 `ErrRechargeClosed`，
  应答渠道失败让它停止重试。

---

## 4. 消费（余额支付）

### 4.1 一个事务做完三件事

```
service.PayOrderByWallet:
  1. 订单行锁 + 状态/金额复核（与渠道支付同一套防线）
  2. 落支付流水(待支付) → 扣余额 → 置流水已支付 → 订单入账
```

**为什么必须在同一事务**：渠道支付有回调重试兜底，余额扣掉之后**没有任何重试机制**。
拆成多步，中间任一步失败都会留下「钱扣了、单没付」且无法自愈，只能人工对账。

### 4.2 归属与认领

- 只能付**自己的单**；他人订单直接拒绝；
- **匿名单**（同桌他人下的、`customer_id=0`）由本次支付认领归属 ——
  否则「我的订单」里查不到这单，对账口径也断了；
- 小程序端在扣款前有一次 `wx.showModal` 二次确认（微信支付有收银台这一步，余额支付没有）。

### 4.3 扣减顺序：先赠金后本金

```go
useGift, principal := splitDelta(deltaCents, balance, gift)  // dao/wallet.go
```

消费时优先消耗赠金（赠金先过期 / 先消耗对门店更有利，也符合顾客预期），
流水里记下 `gift_amount` 让顾客看得懂「扣的是赠金还是本金」。

### 4.4 混合支付（余额抵扣 + 微信补差）

顾客余额 10 元、订单 28 元时，不必先充值：可「余额扣 10 + 微信付 18」。

```
POST /customer/wallet/pay-mixed {orderNo, code, walletAmount}
  ① 事务内扣余额、落一条已支付的储值流水
  ② 余额付清 → 直接置订单已支付，返回 {paid:true}
  ③ 否则返回 wx.requestPayment 参数，顾客补齐差额 → 微信回调入账
```

**为什么先扣余额再拉起收银台**：反过来一旦余额不足，钱已经进了商户号，只能再发起退款。

**余额被占用的三条释放路径**（先扣钱必然要回答「顾客跑了怎么办」）：

| 路径 | 时机 | 动作 |
|---|---|---|
| `pay-cancel` | 顾客在收银台点取消 | **先关渠道单**（关单成功才说明不可能再付成功）→ 退回余额 |
| 支付成功 | 微信回调 | 补齐后订单入账，占用转为正常消费 |
| 超时巡检 | 15 分钟未付 | 同「取消」：关单 → 退回余额 |

**金额防线**：渠道回调只覆盖差额，判据从「本笔 == 订单总额」改为
「本笔 + 其它渠道已入账 == 订单总额」（`ApplyPaymentSuccess` 前校验）。

**退款**：按支付流水分摊（最近付的先退），微信退该笔支付的原始金额、
储值退回钱包；赠金部分仍不退。

---

## 5. 退款与退余额

| 场景 | 行为 | 代码 |
|---|---|---|
| 余额支付订单的订单退款 | 原路退回**本金**（不向渠道发起退款，钱本来就在店里） | `service.WalletRefundForOrder` |
| 退款入账失败 | 立刻把刚退回的钱扣回（`adjust` 流水），防止「余额已加、退款单卡在处理中」 | `service.WalletRefundRollback` |
| 顾客要求退卡 / 退余额 | 本金清零 + 赠金作废，门店线下付款；审计 + `withdraw` 流水 | `service.AdminWalletWithdraw` |

**退款金额有上限**：一笔消费里含赠金的部分不退。
`WalletRefundForOrder` 会按订单的消费构成算出可退本金，
超过即拒绝并提示「含赠金 X 元（赠金不退），最多可退本金 Y 元」。

---

## 6. 注销会员（本轮修复的 P0）

此前 `DeleteCustomerAccount` 只清订单归属 + 删身份 + 删档案，**完全没碰 `tb_wallet`**：

- 不删 → 余额成孤儿数据（会员没了、钱还在账上，永远消费不掉也退不出）；
- 删 → 直接吞掉顾客的预付款。

现在的行为：

1. 事务内先读余额，`balance > 0 || balance_gift > 0` 即返回 `ErrWalletBalanceRemaining`；
2. 通过后**连储值账户、规则同意记录一起删除**，不留孤儿行；
3. 小程序端在点注销前先查一次余额，有余额就弹「请先到门店办理退余额」而不是等后端报错。

---

## 7. 配置项

| 键 | 默认 | 说明 |
|---|---|---|
| `wallet_recharge_enabled` | `0` | 自助充值总闸 |
| `wallet_recharge_min` / `wallet_recharge_max` | `1` / `1000` | 单笔充值区间（元，整数） |
| `wallet_balance_max` | `1000` | 未记名账户余额上限（元） |
| `wallet_balance_max_named` | `5000` | 已记名账户余额上限（元） |
| `wallet_realname_required` | `1` | 充值前是否强制实名 |
| `wallet_adjust_max` | `1000` | 后台单次调账上限（元），防误操作 |
| `wallet_rules_version` | `2026.1` | 《储值规则》版本；改正文必须改版本 |
| `wallet_rules_text` | 出厂文案 | 《储值规则》正文，留空回落到默认 |

权限点：`wallet:view`（查余额 / 流水）、`wallet:edit`（调账 / 退余额）。

---

## 8. 与会员系统的衔接（二期）

储值是为会员系统打底，后续几件事**必须在现有结构上长**，不要另起一套：

| 二期能力 | 依赖的现有结构 | 需要注意 |
|---|---|---|
| 充值赠送（充 100 送 10） | 赠金桶已就位 | 赠送走 `biz_type=gift`，直接入 `balance_gift`；退款上限逻辑不用改 |
| 会员等级折扣 | 订单金额 | 折扣改的是订单金额，余额按折后价扣，无需改动储值 |
| 积分抵现 | 混合支付已就位 | 一单可由「余额 + 一个渠道」组合支付（见 §4.4）；积分若要参与分摊，照 `splitDelta` 增加一个桶即可 |
| 会员注销 / 合并 | 余额闸门 | 合并两个会员前必须先把其中一个的余额退掉或转出 |

**混合支付已在一期内落地**（§4.4）：`tb_payment` 天然支持「一单多条流水」，
退款按流水分摊原路退回。仍未支持的是「同一单多个渠道补差」（如微信 + 支付宝同时分摊），
以及积分参与分摊 —— 两者都需要再拆一层分摊模型，届时再出 spec。

---

## 9. 接口

| 方法与路径 | 说明 | 鉴权 |
|---|---|---|
| `GET /customer/wallet` | 余额（本金 / 赠金）+ 规则 + 限额 + 是否已同意 | 会员令牌 |
| `GET /customer/wallet/txns` | 储值流水（分页） | 会员令牌 |
| `POST /customer/wallet/recharge` | 建充值单 + JSAPI 调起参数 | 会员令牌 |
| `GET /customer/wallet/recharge/status` | 轮询入账状态 | 会员令牌 |
| `POST /customer/wallet/recharge/reconcile` | 充值补账（回调丢失自救） | 会员令牌 |
| `POST /customer/wallet/rules/agree` | 同意《储值规则》 | 会员令牌 |
| `POST /customer/wallet/pay` | 余额支付订单（余额足够付全款） | 会员令牌 |
| `POST /customer/wallet/pay-mixed` | 混合支付：余额抵扣 + 微信补差 | 会员令牌 |
| `POST /customer/wallet/pay-cancel` | 取消混合支付，立刻退回已扣余额 | 会员令牌 |
| `POST /customer/auth/phone` | 绑定手机号（实名） | 会员令牌 |
| `GET /admin/member/wallet` | 某会员的余额与流水 | `wallet:view` |
| `POST /admin/member/wallet/adjust` | 调账（正数赠送 / 负数扣减，原因必填） | `wallet:edit` |
| `POST /admin/member/wallet/withdraw` | 退余额（本金清零 + 赠金作废） | `wallet:edit` |
| `DELETE /customer/auth/account` | 注销会员（余额未清零则拒绝） | 会员令牌 |

---

## 10. 测试与验收

后端 `backend/application/service/wallet_service_test.go`：

| 用例 | 守住的不变式 |
|---|---|
| `TestWalletRechargeCredit` | 重复回调不重复加钱；金额不一致绝不入账 |
| `TestWalletRechargeCreditConcurrent` | **并发**回调只能入账一次（守住「先抢占再动钱」的顺序） |
| `TestNormalizeYuanIntValue` | 储值限额必须落成整数（存成小数会被读取侧静默忽略） |
| `TestWalletPayAndRefund` | 扣款 / 流水 / 订单入账原子；退款原路；他人订单不可用我的余额 |
| `TestWalletInsufficientAndConcurrency` | 并发消费不超扣 |
| `TestWalletGiftBucketConsumeFirst` | 先扣赠金；含赠金的消费不得全额退成本金 |
| `TestWalletRechargeComplianceGates` | 规则 / 实名 / 单笔上限 / 余额上限四道闸门 |
| `TestWalletClosedRechargeNeverCredits` | 已关闭的充值单回调不得入账 |
| `TestAdminWalletWithdrawAndDeleteAccount` | 退余额清零两个桶；带余额注销被拒；清零后可注销且不留孤儿 |
| `TestWalletConcurrentConsumeUsesFreshGift` | 并发消费按**锁定后**的赠金拆分（不会明明有钱却报余额不足） |
| `TestMixedPayWalletPlusChannel` | 余额 + 渠道补差分摊正确；补齐后订单入账、实收=总额 |
| `TestMixedPayWalletCoversAll` | 余额够付全款时收敛到差额、不多扣，直接付清 |
| `TestAbortMixedPayRestoresBuckets` | 撤销混合支付后本金/赠金**原样**回到各自的桶 |
| `TestRefundPartsSplitAcrossChannels` | 混合支付退款按流水分摊，钱各回各家，渠道退款 total 传该笔金额 |

小程序 `npm run mp:verify`（typecheck + lint + WXML/WXSS 审计 + 单测 + 冒烟）。

---

## 11. 运维

- **对账**：`tb_wallet_txn` 逐行累加必须等于 `tb_wallet.balance`；对不上以流水为准。
- **告警**：日志里搜 `[wallet][告警]`，涉及退款回滚失败、退款入账失败等需人工核对的场景。
- **迁移**：改表结构后跑 `go run ./cmd/gensql` 重新生成 `migrations/full/*`，
  `TestGenSQLCheck` 会强制校验。
