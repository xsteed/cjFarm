# 后端代码审查报告（2026-10-17）

- **范围**：`backend/` 全部 Go 代码（application / domain / infrastructure / interfaces / main.go，约 256 个文件）及相关的顾客端前端调用点
- **方法**：`go build ./...`、`go vet ./...`、`go test ./...` 全绿（17 个包）后，按模块读码审查；所列问题均于 2026-10-17 逐条读码确认。**危害较高的 10 项（4 高危 + 6 中危）已于当日修复完毕**，修复后复查结论见文末。
- **总体评价**：代码质量很高——SQL 全参数化、金额全程 `int64` 分、双库占位符统一、核心并发路径（占台/库存/手工退款）有行锁与事务防护、认证链路 fail-closed。此前的问题多为「已 hardening 的路径上遗漏的同类模式」或边界条件。

> 说明：正文中的行号是**修复前**的定位；修复后的代码位置以「修复」段落的文件名为准。

---

## 修复总览

| 编号 | 问题 | 严重度 | 状态 |
|---|---|---|---|
| H-1 | 重复支付自动退回：查重与落单无互斥，可并发退两次 | 高 | ✅ 已修复 |
| H-2 | 微信退款同步 SUCCESS 时不入账，钱退账未记 | 高 | ✅ 已修复 |
| H-3 | 先付模式并发通知出两张厨单 | 高 | ✅ 已修复 |
| H-4 | `PayCreate` 金额快照竞态，可生成旧金额二维码 | 高 | ✅ 已修复 |
| M-1 | 顾客端按单号可遍历查询任意订单（IDOR） | 中 | ✅ 已修复 |
| M-2 | 登录接口可枚举有效用户名（文案 + 时序） | 中 | ✅ 已修复 |
| M-3 | 微信未知退款状态被默认成「明确失败」，释放可退额度 | 中 | ✅ 已修复 |
| M-4 | 配置逐项落库失败仍返回「保存成功」 | 中 | ✅ 已修复 |
| M-5 | 跨日自动清台 check-then-act，可误清正在用餐的桌 | 中 | ✅ 已修复 |
| M-6 | 删除分类无菜品守卫，菜品静默消失却仍可下单 | 中 | ✅ 已修复 |
| L-1 ~ L-13 | 见第三节（健壮性与口径类） | 低 | ✅ 已逐条复核确认属实，⬜ 未修复（本轮范围外） |
| L-14 | 登录 `UsernameExists` 查询错误被吞 | 低 | ✅ 已随 M-2 修复 |

### 本轮改动文件

后端：
- `backend/application/service/payment_service.go`（H-1/H-3/H-4）
- `backend/application/service/customer_service.go`（M-1）
- `backend/application/service/auth_service.go`（M-2/L-14）
- `backend/application/service/setting_service.go`（M-4）
- `backend/application/service/stale_service.go`（M-5）
- `backend/application/service/menu_service.go`（M-6）
- `backend/infrastructure/store/dao/refund.go`（H-1：`GetPendingDuplicateRefundTx`）
- `backend/infrastructure/store/dao/payment.go`（H-4：`InsertPaymentTx`/`GetOrderAmountTx`）
- `backend/infrastructure/store/dao/table.go`（M-5：`TouchTableTx`）
- `backend/infrastructure/store/dao/category.go`（M-6：`CountActiveDishes`）
- `backend/infrastructure/pay/wxpay.go`（M-3）
- `backend/interfaces/handler/pay.go`（H-1/H-2/H-3/H-4）
- `backend/interfaces/handler/customer.go`（M-1）
- `backend/interfaces/handler/auth.go`（M-2）
- `backend/interfaces/handler/setting.go`（M-4）
- `backend/interfaces/middleware/ratelimit.go`（**新增**，M-1 限流）
- `backend/infrastructure/conf/conf.go`（限流配置常量）
- `backend/main.go`（M-1 路由挂限流）
- 测试：`payment_service_test.go`、`customer_service_test.go`、`setting_service_test.go`、`menu_service_test.go`

前端（M-1 配套传参）：
- `frontend/packages/api/src/customer.ts`
- `frontend/apps/customer/src/OrderPage.vue`
- `frontend/packages/customer-core/src/useOrderPolling.ts`
- `frontend/apps/customer/src/PrintTicket.vue`
- `frontend/apps/customer/src/order/__tests__/OrderPage.test.ts`

---

## 一、高危（资金安全）——全部已修复

### H-1 重复支付自动退回：查重与落单无互斥，可对同一笔支付并发发起两笔退款 ✅

- **原问题**：`backend/interfaces/handler/pay.go:263-309`（修复前）——`HasPendingDuplicateRefund`（普通 SELECT）与 `RecordDuplicateRefund`（普通 INSERT）之间无锁、无事务；`tb_refund` 仅有 `refund_no` 唯一索引。渠道通知重发 + 前端 `PayQuery` 轮询补单可同时命中 `Duplicate` 分支，各自生成不同 `refundNo` 调两次渠道退款。
- **修复**：
  - `dao/refund.go` 新增 `GetPendingDuplicateRefundTx(tx, orderNo, channel)`；
  - `service.RecordDuplicateRefund` 改为事务内「`TouchOrderTx` 锁订单行 → 查重 → `InsertRefundTx` 落单」，新增哨兵错误 `ErrDuplicateRefundPending`；订单金额也改为在行锁保护下重读；
  - `handler.autoRefundDuplicatePayment` 保留事务外快速路径预查（省一次事务），并在收到 `ErrDuplicateRefundPending` 时跳过（权威判定在事务内）。
  - 与手工退款 `CreateRefund` 的行锁模式一致，消除 check-then-act。
- **验证**：`TestPaymentSvcRecordMismatchedAndDuplicate` 新增两条断言——同订单同渠道第二次登记返回 `ErrDuplicateRefundPending`；换渠道允许登记。

### H-2 微信退款同步返回 SUCCESS 时不做账务入账 ✅

- **原问题**：`backend/interfaces/handler/pay.go:489`、`:517-520`（修复前）；`wxpay.go:155-167`——wxpay 把 `SUCCESS` 映射为 `RefundSuccess`，但入账判据是渠道名（`refundSettled` 仅 alipay 为 true）。微信 SUCCESS 落到「已受理」分支：`refund_amount` 不累加、`paid_amount` 不扣减（营业额虚高）、退款单占用可退额度、文案误导。
- **修复**：`PayRefund` 判据改为 `result.Success && (result.Status == pay.RefundSuccess || refundSettled(channel))`；`autoRefundDuplicatePayment` 的落账判据同样改为按返回状态（`pay.go:315` / `:509`）。`refundSettled` 注释同步说明其仅为补充判据。
- **验证**：编译 + 全量测试通过；`wxpay.Refund` 的 SUCCESS 路径现由 `refundSettled` 分支覆盖。

### H-3 先付模式「首次收款出厨单」check-then-act，并发通知出两张厨单 ✅

- **原问题**：`backend/interfaces/handler/pay.go:237-252`（修复前）——`beforePaid` 在 `ApplyPaymentSuccess` 之前读快照，两个并发通知可都读到 `false`，而 `ApplyPaymentSuccess` 对两者都幂等返回空结果 → `printKitchenOnPaid` 调用两次（该函数无幂等）。
- **修复**：`PaymentSuccessResult` 增加 `FirstTime bool`，由 `MarkOrderPaidByChannel` 条件更新的 `changed` 结果判定；两处首次入账（正常入账 + 掉单补偿）置 `FirstTime=true`；`handler.applyPaymentSuccess` 删除 `beforePaid` 快照，改为 `if res.FirstTime { printKitchenOnPaid(...) }`。
- **验证**：`TestPaymentSvcApplyPaymentSuccess` 新增断言——首次入账 `FirstTime=true`；重复回调 `FirstTime=false`；掉单补偿 `FirstTime=true`。

### H-4 `PayCreate` 金额快照与渠道下单之间存在竞态，可生成旧金额二维码 ✅

- **原问题**：`backend/interfaces/handler/pay.go:85-119`（修复前）——`totalCents` 在事务外读取，出码期间加菜/改单抬价会流出旧金额码；`InsertPayment` 的复活 UPDATE 会无条件重置已关单的占位行。
- **修复**：
  - `service.CreatePayment` 签名改为 `(orderID, orderNo, channel, amountCents, prepayID)`，内部 `store.WithTx`：`TouchOrderTx` 锁行 → `GetOrderAmountTx` 重读金额 → 不一致返回新增哨兵 `ErrOrderAmountChanged` → 一致才 `InsertPaymentTx`；
  - `dao/payment.go` 新增 `InsertPaymentTx`（与 `InsertPayment` 同语义的插入 + 复活 UPDATE）；
  - `PayCreate` 接收 `ValidateOrderPayable` 返回的 `orderID`，命中 `ErrOrderAmountChanged` 时 `provider.Close` 关掉旧码并提示「订单金额已变更，请刷新后重新发起支付」。
- **验证**：`TestPaymentSvcPaymentFlow` 更新——金额一致可复用流水；金额不一致（5001 vs 5000）返回 `ErrOrderAmountChanged`。

---

## 二、中危（安全与数据一致性）——全部已修复

### M-1 顾客端可按单号遍历查询任意他人订单（IDOR）✅

- **原问题**：`backend/main.go:67`、`handler/customer.go:135-142`、`service/customer_service.go:368-374`（修复前）——路由无鉴权、无限流，service 只按单号查后全量返回，不校验桌台归属；泄露菜品/金额/挂账人姓名等。与项目自身「数字 ID 可被枚举，不能作为订单能力凭据」的口径不一致。
- **修复（分级防护）**：
  1. **归属校验**：`service.CustomerOrderByNo(orderNo, tableID)` 增加 `tableID` 参数，`tableID > 0` 时校验 `o.TableID != tableID` → 返回「订单不存在或已结束」（不泄露「单号存在但不属于你」）；`handler` 从 query 读取 `tableId` 传入。
  2. **限流兜底**：新增 `interfaces/middleware/ratelimit.go`（进程内单 IP 分钟窗计数，429 + `Retry-After`），仅挂在 `/api/customer/order/no/:orderNo` 路由；默认 300 次/分钟（覆盖门店 NAT 下约 15 桌 3s 轮询，35bit 随机段穷举需数十年），`CUSTOMER_ORDER_QUERY_RATE_LIMIT` 可配、<=0 关闭。
  3. **前端配套**：`getOrderByNo(no, tableId?)` 加可选参数；`OrderPage.vue` 传当前 `tableId.value`；`useOrderPolling.ts` 用 `currentOrder.tableId` 轮询；`PrintTicket.vue` 读取 `route.query.tableId`。老链接（未带桌台）退化为仅按单号查询 + 限流，存量二维码不失效。
- **验证**：`TestCustomerSvcOrderByNo` 新增归属用例（错误桌台拒绝、正确桌台放行）；`OrderPage.test.ts` 断言更新为 `toHaveBeenCalledWith('NO001', 1)`；前端 vitest 256/256、`vue-tsc` 通过。

### M-2 登录接口可枚举有效用户名（文案 + 时序双侧信道）✅

- **原问题**：`service/auth_service.go:443-469`、`handler/auth.go:133-140`（修复前）——锁定账号分支不执行 bcrypt 且回不同文案；`UsernameExists` 错误被 `_` 吞掉（L-14）。
- **修复**：
  - 锁定分支补 `store.WastePasswordVerify(password)`，与「账号不存在」分支耗时对齐；
  - `handler` 对 `LoginFailUserLocked` 回统一文案「用户名或密码错误」，真实原因仅进 `WriteLoginLog`（管理员可查）；
  - `UsernameExists` 错误改为显式留痕 `logger.CtxWarnf`，DB 故障时按「不存在」处理并告警（L-14 一并修复）。
- **验证**：编译 + 全量测试通过；登录限流测试（`auth_more_test.go`）不受影响。

### M-3 微信退款无法识别的 status 被默认成「明确失败」 ✅

- **原问题**：`wxpay.go:155-161`（`Refund` 默认 `RefundFail`）、`QueryRefund` 默认 `RefundFail`（修复前）——HTTP 200 但 status 缺失/新增状态码时会把退款单置失败、释放可退额度。
- **修复**：`Refund` 的默认值改为 `RefundProcessing`，仅 `CLOSED`/`ABNORMAL` 显式映射为失败；`QueryRefund` 同样只把 `CLOSED`/`ABNORMAL` 判失败，未知状态 fail-safe 到处理中，等查单兜底。
- **验证**：`infrastructure/pay` 测试通过；`pay.go:505` 的失败分支只会在渠道明确拒绝时命中。

### M-4 系统配置逐项落库失败只打 warning，接口仍返回「保存成功」✅

- **原问题**：`setting_service.go:311-314`、`:338-341`、`:350-353`（修复前）——`dao.SetSetting` 失败仅告警不上抛。
- **修复**：`SaveSettings` 签名改为 `(detail string, conflict bool, err error)`；普通项、敏感项、清空代理令牌三处的失败键收集到 `failedKeys`，任一项失败返回 `fmt.Errorf("以下配置项保存失败: ...")` 且不生成审计摘要；`handler.SettingSave` 改为 HTTP 500 + 可操作提示，错误原因进日志。
- **验证**：新增 `TestSaveSettingsPersistError`（关库后保存必须返回 error、不产生摘要、错误含失败键）；原有 9 处测试调用与冲突用例全部通过。

### M-5 跨日自动清台 check-then-act，可能误清正在用餐的桌 ✅

- **原问题**：`stale_service.go:144-186`（修复前）——事务外「读快照 → 内存判定 → `SetTableStatus` 无条件置空闲」，窗口期内顾客新开的单会被抹成空闲桌。
- **修复**：清台改为 `store.WithTx` + 新增的 `dao.TouchTableTx`（自赋值 UPDATE 取桌台行写锁，把并发下单的 `OccupyTableTx` 挡在锁外）+ `CountUnfinishedOrdersTx(tx, id, today)`：本营业日（含 `biz_date` 为空，与快照口径一致）仍有进行中订单则不清，为 0 才 `SetTableStatusTx`。事务外快照仅保留作日志计数与「已空闲跳过」优化。
- **验证**：全量测试通过（`stale_service` 相关用例）；逻辑与原「空 biz_date 保守不清」「一桌多单有本营业日单不清」语义一致。

### M-6 删除分类无菜品守卫，菜品静默消失却仍可被下单 ✅

- **原问题**：`menu_service.go:121-123`（修复前）——`DeleteCategory` 直接软删，不检查分类下菜品；顾客端菜单按分类分组会静默丢弃菜品，而下单校验只查菜品自身 `del_flag`。
- **修复**：`dao/category.go` 新增 `CountActiveDishes(categoryID)`；`service.DeleteCategory` 删除前计数，`>0` 时拒绝并提示「该分类下还有 N 道菜品，请先移动或删除菜品后再删除分类」，与 `DeleteTable` 同口径。
- **验证**：`TestCategoryCRUD` 新增用例——分类下有菜品时删除被拒绝；移走菜品后删除成功。

---

## 三、低危（13 项：2026-10-18 逐条复核确认属实，本轮未修复）

> 复核方式：逐条读码确认（按函数名定位，避免行号漂移）。L-14 已随 M-2 修复，不在此列。

### L-1 `EditOrder` 无用餐人数上限 ✅ 确认属实
- **位置**：`application/service/order_service.go:588-590`（`EditOrder`）
- **确认依据**：仅 `if personCount < 1 { personCount = 1 }`，无上限；而 `CustomerCreateOrder`（`:218`）与 `CustomerAppendOrder`（`:399`）都有 `> 100` 拒绝。
- **影响**：茶位费开启时管理端改单传 `personCount=100000`，`seatFee = 人数 × 单价` 会算出巨额应收。
- **建议**：补同一上限（>100 拒绝或夹紧到 100）。

### L-2 满减用浮点除法取整，整除边界可少算一档 ✅ 确认属实
- **位置**：`application/service/service.go:269-271`（`RecalcAmount`）
- **确认依据**：`discount = po.Round2(math.Min(math.Floor(dishAmount/threshold)*d, dishAmount))`，`dishAmount/threshold` 是 float64 除法；`0.3/0.1 == 2.9999999999999996` 时 `floor` 少一档。金额已 `Round2`，但阈值/金额含小数且恰好整除时仍可触发。
- **影响**：该享受的满减没享受到（顾客少优惠，不会多收）。
- **建议**：改为 int64 分整数运算：`dishCents/thresholdCents*discountCents`。

### L-3 `AgentAck` 响应计数违反自身不变式 ✅ 确认属实
- **位置**：`interfaces/handler/agent.go:262-296`（`AgentAck`）
- **确认依据**：`accepted++` 在 `:267` 无条件执行；`r.Ok == false` 且 `AckAgentJob` 成功时（`:289-295`），`failed/done/skipped` 均不增。而 `:298-299` 注释声称 `accepted = done+rejected+skipped`。
- **影响**：代理程序若用 `accepted` 对账会平不了账（仅协议/排障层面，不影响打印结果）。
- **建议**：增加 `retried` 计数，或把「上报失败」计入 `rejected`。

### L-4 `SetTableLock` 无删除守卫、不查影响行数 ✅ 确认属实
- **位置**：`infrastructure/store/dao/table.go:197-201`（`SetTableLock`）
- **确认依据**：`UPDATE tb_table SET lock_status=?, lock_remark=?, update_time=? WHERE table_id=?`——无 `AND del_flag='0'`，也未检查 `RowsAffected`；对照 `SetTableStatusTx`（`:33`）带守卫。
- **影响**：可给已软删桌台加锁并返回成功，给管理员「已更新」的错误反馈。
- **建议**：补 `del_flag='0'` 守卫并检查影响行数。

### L-5 `newDeliveryID` 兜底值可预测且同秒撞号 ✅ 确认属实
- **位置**：`infrastructure/store/dao/printjob.go:197-204`（`newDeliveryID`）
- **确认依据**：crypto/rand 失败时 `return hex.EncodeToString([]byte(store.Now()))`；`store.Now()` 是 `2006-01-02 15:04:05`（秒级，见 `conf.TimeLayout`），注释却称「纳秒时间戳」。同秒入队的两条任务会得到相同 delivery_id，且值可从时间推断。
- **影响**：破坏代理侧「回执丢失 → 凭 delivery_id 去重」的幂等语义（两张不同票据被判同一张而跳过打印）。仅在 crypto/rand 失败时可达（概率极低），但仍违背函数自身文档承诺的 128 位随机性。
- **建议**：rand 失败时返回 error/让入队失败重试，不回退可预测值；或至少用 `time.Now().UnixNano()` + 进程内计数器。

### L-6 列表查询扫描失败静默丢行、不查 `rows.Err()` ✅ 确认属实
- **位置**（12 处，均为 `if err == nil` 丢行且无 `rows.Err()`）：
  - `dao/printjob.go`：`ListPrinters`（`:43`）、`ListEnabledPrinters`（`:66`）、`ClaimPrintJobs` 的 `pick`（`:272-284`，`return nil`）
  - `dao/printagent.go`：`ListPrintAgents`（`:101`）
  - `dao/dish.go`：`ListDishes`（`:67`）、`ListEnabledDishes`（`:84`）
  - `dao/category.go`：`ListCategories`（`:21`）、`CategoryNameMapByIDs`（`:74`）
  - `dao/remark.go`：`ListRemarks`（`:21`）
  - `dao/user.go`：`ListRoles`（`:463`）
  - `dao/order.go`：`ListServingOrdersByTable`（`:198`）、`CountOrderBatches`（`:896`）
- **确认依据**：同文件的 `CancelPrintJobsByPrinter`（`:458-468`）与 `ExpireOverduePrintJobs` 的 `collect`（`:507-511`）都正确检查了 `rows.Err()` 并在注释申明口径，说明这是漏网而非设计。
- **影响**：驱动级扫描错误时静默少返回数据且无日志——打印机被漏掉即该台**静默不打印**；取单候选被丢弃即少取任务。
- **建议**：统一改为 `return nil, err` + `rows.Err()`；`ScanDish` 等可空列（`create_by/update_by` 未声明 NOT NULL）改用 `TS()`/`sql.NullString` 兜底。

### L-7 LIKE 通配符 `%`/`_` 未转义 ✅ 确认属实
- **位置**：`dao/table.go:132-133`（`ListTables` 的 `table_name`）、`dao/dish.go:44-45`（`dish_name`）、`dao/order.go:326-327/337-338/353-354`（`order_no`/`table_name`/`settle_remark`）、`dao/audit.go:72-73`（`operator`）
- **确认依据**：均为 `"%"+输入+"%"` 直接绑定，无 `ESCAPE` 子句。非 SQL 注入（已参数化），是通配符语义问题。
- **影响**：用户输入 `%` 可列出全表、`_` 匹配任意单字符，筛选结果与预期不符。
- **建议**：对 `\`、`%`、`_` 转义并加 `ESCAPE '\'`。

### L-8 关闭期后台任务与 `store.DB.Close()` 竞态 ✅ 确认属实
- **位置**：`backend/main.go:417-431`（`srv.Shutdown` 后直接 `store.DB.Close()`）vs `infrastructure/print/agent.go:559-588`（`agentCleanup` 的 `time.Sleep` 常驻循环）、`application/service/stale_service.go:261-270`（`StartStaleOrderGuard` 同款循环）
- **确认依据**：两个后台循环都无退出信号/`WaitGroup`；HTTP 关闭不等待它们。
- **影响**：仅限进程退出过程——DB 关闭后残留循环抛一轮 "sql: database is closed"（agentCleanup 有 recover 吞 panic，但错误仍进日志），形成无头错误日志。
- **建议**：关闭时经 context/WaitGroup 通知后台循环退出后再 `Close()`。

### L-9 `HourlyStats` 与其他报表口径矛盾 ✅ 确认属实
- **位置**：`dao/report.go:283-306`（`HourlyStats`：`create_time` 取小时 + `SUM(paid_amount)`）
- **确认依据**：同文件 `RangeAmount`（`:44-56`）与 `DailyTrendStats`（`:76-137`）为资金流口径（`pay_time` / `credit_settle_time` / 退款 `update_time`），`SettleMixStats`（`:309-316`）为应收口径。`HourlyStats` 既不含挂账核销回款、也不扣退款。
- **影响**：分时图各小时金额合计 ≠ 今日营业额（含挂账/退款时必然对不上），店长对账困惑。
- **建议**：改为资金流口径（三路 UNION 按小时分组），或在接口/前端明确标注口径不同、不可相加。

### L-10 按份拆单两处除零 panic ✅ 确认属实
- **位置**：`infrastructure/print/print.go:746`（`splitItemPortions`：`unit := it.Amount / int64(it.Quantity)`）、`:1388`（按份拆单：`one.Amount = it.Amount / int64(it.Quantity)`）
- **确认依据**：两处均未判 `Quantity <= 0`；正常写入路径已把 `Quantity<1` 归一为 1（`service.go:360-362`），但老库/手工插入 `quantity=0` 的历史行可达。
- **影响**：预览链路在 HTTP handler 内直接调用，只能靠 gin recovery 变成 500；打印链路被 `runJobRecovered` 吞成「该票静默丢失、无台账」。
- **建议**：`Quantity<=0` 时直接返回空/单份并记 Warn。

### L-11 `UpdateDish` 对不属本菜品的 SpecID 静默 no-op ✅ 确认属实
- **位置**：`dao/dish.go:146-152`（`UpdateDish`）
- **确认依据**：`UPDATE tb_spec ... WHERE spec_id=? AND dish_id=?` 的 `RowsAffected` 未检查，且 `keep[s.SpecID] = true` 在 UPDATE 之前设置。
- **影响**：传入别的菜品的 `specId` 时，该规格更新被静默丢弃且不会被删除；若提交的所有 SpecID 都不属本菜品，菜品最终 0 规格，而 `menu_service.go:76-78` 只校验 `len>=1` 拦不住。
- **建议**：`RowsAffected()==0` 时返回「规格不存在或不属于该菜品」；或更新前按 `dish_id` 批量校验归属。

### L-12 `AuditRetentionDays` 语义与调用方约定冲突 ✅ 确认属实
- **位置**：`dao/audit.go:175-181`（`AuditRetentionDays`：解析失败或 `n<0` 返回 365）vs `application/service/audit_service.go:82-93`（用 `days<=0` 判「禁用清理」）
- **确认依据**：配置 `AUDIT_RETENTION_DAYS=-1` 或非数字时，dao 归一为 365，service 的「禁用」分支不可达，会按 365 天真删数据；同时 `dao.CleanExpiredOperLogs`（`:142-153`）只被测试引用，生产走 service 版，属死代码。
- **影响**：误配置负值/非法值时审计日志被清理而非按预期禁用；语义两处不一致，后续维护易踩坑。
- **建议**：非法/负值返回 0 并统一「`<=0` = 不清理」；删除 dao 版死代码。

### L-13 `refundMu` 全局锁横跨渠道退款网络调用 ✅ 确认属实
- **位置**：`interfaces/handler/pay.go:469-470`（`refundMu.Lock(); defer Unlock`）+ `:494`（锁内 `provider.Refund`）；wxpay HTTP 超时 15s（`wxpay.go:290`）
- **确认依据**：锁覆盖「额度校验 → 落退款单 → 渠道退款」全过程，渠道调用失败/超时期间全店其他退款请求被阻塞。
- **影响**：可用性问题（正确性已由 `CreateRefund` 的 DB 行锁兜底），渠道慢时收银员点击退款会「转圈」最长 15s+。
- **建议**：锁范围收缩到「校验 + 落退款单」，渠道调用放到锁外。

### L-14 登录 `UsernameExists` 查询错误被吞 ✅ 已修复（随 M-2）
- **原位置**：`application/service/auth_service.go:445`——`if taken, _ := dao.UsernameExists(...)`；已改为显式 `existsErr` 留痕。

---

## 四、审查中排查过、确认**不存在**的项（留档避免重复审查）

| 类别 | 结论 |
|---|---|
| SQL 注入 | 不存在。dao 层全参数化；唯一 `fmt.Sprintf` 拼 SQL 在 `*_test.go`；`DishRank` 排序列白名单 |
| MySQL/SQLite 占位符兼容 | 不存在问题。统一 `?`（`infrastructure/store/dialect.go:136-138`） |
| 浮点金额 | 核心账务全 `int64` 分（`po.ToCents/Round2/ToYuan`），精度处理正确（L-2 除外） |
| 库存超卖 | 已防护。`DecrementSpecStockTx` 带守卫原子 UPDATE，取消/改单事务内回补 |
| 桌台占用竞态 | 已防护。`OccupyTableTx` 先行锁再计数（M-5 是巡检路径遗漏，已修复） |
| 认证绕过/越权 | 不存在。`AdminAuth → RequirePerm` fail-closed，`routePerms` 全覆盖 `/api/admin/*`（routes_test 守护） |
| token 校验 | HMAC-SHA256 + `hmac.Equal` 恒定时间比对；过期/version/status 逐请求校验 |
| 密码存储 | bcrypt（随机盐），历史 SHA-256+盐用 `subtle.ConstantTimeCompare` 且登录后自动升级 |
| 回调验签/金额校验 | 微信 RSA 验签 + AES-GCM 解密 + 归属校验；支付宝 RSA2 + app_id 校验；回调与查单均有金额一致性防线 |
| 上传安全 | 无路径穿越（服务端生成文件名）；jpg/png/gif 双解码校验 + webp/bmp 手写头部 + 尺寸上限 + `io.LimitReader` |
| 报表时间边界 | 统一 `time.ParseInLocation(time.Local)`、`[start, end+1天)` 左闭右开，跨月/年处理正确 |
| HTTP 客户端 | feie 12s / weapp 10s 超时 + `defer Body.Close` + 大小上限，无 fd 泄漏 |
| goroutine/map 并发 | `loginGuard`、`perAgentSeenNano`、`settingCache`、`imagecache` 均有锁 |

---

## 五、复查记录

### 2026-10-18 复核：修复代码复审 + 低危确认

**1）10 项修复逐条复审：全部正确，无回退**

| 项 | 复审要点 | 结论 |
|---|---|---|
| H-1 | 事务内 `TouchOrderTx` 锁行后查重、落单；并发通知第二个事务会阻塞到第一个提交后命中查重 | ✅ |
| H-2 | 入账判据 `result.Status == RefundSuccess \|\| refundSettled(channel)` 覆盖微信同步 SUCCESS 与支付宝 | ✅ |
| H-3 | `ApplyPaymentSuccess` 全部 11 条 return 路径逐一核对：仅「changed=true 且订单条件更新命中」与「掉单补偿成功」返回 `FirstTime=true`，幂等短路/重复通知均返回 false | ✅ |
| H-4 | `CreatePayment` 事务内先 `TouchOrderTx` 再 `GetOrderAmountTx`，与 `EditOrder` 事务串行化；`ErrOrderAmountChanged` 分支关码并提示重试 | ✅ |
| M-1 | 归属校验 `tableID > 0` 时生效且文案不泄露存在性；限流仅挂目标路由；前端 4 处均已传参 | ✅ |
| M-2 | 锁定/不存在/密码错误三条路径耗时对齐（各一次 bcrypt）；前端文案统一 | ✅ |
| M-3 | 仅 `CLOSED`/`ABNORMAL` 判失败，其余（含未知）为处理中 | ✅ |
| M-4 | 失败键收集覆盖普通项/敏感项/清空令牌；错误返回时不生成审计摘要；`settingSaveMu` 正常释放 | ✅ |
| M-5 | 事务内「`TouchTableTx` 取行锁 → `CountUnfinishedOrdersTx` → `SetTableStatusTx`」顺序正确：MySQL RR 下快照建立于取锁的当前读时刻，并发下单要么被计入、要么被行锁挡到本事务提交后 | ✅ |
| M-6 | 计数仅统计 `del_flag='0'` 菜品；拒绝文案含数量 | ✅ |

**2）复审中发现并修复的 2 个小问题（限流器自身）**

- `ipBucket.windowStart` 注释误写为「整分钟」，实现是「该 IP 首个请求时刻起窗」——注释已修正；
- 过期 key 清理原为**每请求**全表扫描，大量不同 IP 的流量下会让限流器自身成为 O(N²) 放大面——改为按窗口节流（每个窗口最多清理一次，新增 `lastCleanup` 字段）。

修改后 `go build` / `go vet` / `go test ./interfaces/middleware/ ./application/service/ ./interfaces/handler/` 全部通过。

**3）低危 13 项逐条复核：全部确认属实**

详见第三节（每项已标注确认依据、影响与建议）。其中：
- **L-6 的准确清单为 12 处**（原报告中的 `CancelPrintJobsByPrinter` / `ExpireOverduePrintJobs` 实际已正确检查 `rows.Err()`，已从清单排除）；
- **L-5 额外确认**：注释称「纳秒时间戳」但 `store.Now()` 实为秒级（`conf.TimeLayout`），同秒撞号成立；
- **L-13 行号更新**：`refundMu` 现位于 `pay.go:469-470`，渠道调用在 `:494`（锁内）。

### 2026-10-17 修复验证

### 验证结果

| 检查项 | 结果 |
|---|---|
| `go build ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 通过 |
| `go test ./... -count=1` | ✅ 17 个包全部通过 |
| `go test ./application/service/ -race`（支付/配置/顾客/清台用例） | ✅ 通过 |
| 前端 `vitest run` | ✅ 256/256 通过 |
| 前端 `vue-tsc --noEmit` | ✅ 通过 |

### 新增/加强的回归测试

- `TestPaymentSvcPaymentFlow`：金额不一致拒绝出码（H-4）
- `TestPaymentSvcApplyPaymentSuccess`：`FirstTime` 三态（首次/重复/掉单补偿）（H-3）
- `TestPaymentSvcRecordMismatchedAndDuplicate`：并发重复退款被事务内查重拦截（H-1）
- `TestCustomerSvcOrderByNo`：订单归属校验（M-1）
- `TestSaveSettingsPersistError`：落库失败必须返回 error（M-4）
- `TestCategoryCRUD`：分类下仍有菜品时拒绝删除（M-6）

### 复查中确认的连带影响

1. **`CreatePayment` 语义收紧**：现在要求出码金额与订单当前应收一致（这是 H-4 的目的）。原测试「重复发起时金额可任意变化」的用例已改为「金额不一致必须拒绝」。
2. **`SaveSettings` 返回值变化**：调用方需处理第三个返回值；`handler` 已改为失败回 500，不再落「保存成功」审计。
3. **顾客端 `getOrderByNo` 调用点**：`OrderPage` / `useOrderPolling` / `PrintTicket` 均已传 `tableId`；未带桌台的老链接仍可查询（由限流兜底），存量二维码不失效。
4. **限流只作用于按单号查单接口**：不影响菜单、下单、支付等其他公开接口，避免门店 NAT 下误伤。
5. **`OrderPay`（前台手工收款）路径无需改动**：`service.PayOrder` 已有 `MarkOrderPaid` 条件更新守卫，并发收款只有先到者生效。

### 遗留说明

- **低危 13 项未修复**：见第三节，建议按「L-6、L-7、L-10 优先」在后续迭代处理。
- **测试环境偶发**：`TestOldMasterKey` 曾出现一次 `setenv: invalid argument`（单跑与多次重跑均通过，与本次改动无关，属测试进程环境噪声）；`TestVisitRollupDay` 的全包失败经复核为用户并行开发中的 visit 功能测试（未跟踪文件，与本次改动无关），当前已通过。
- **`docs` 中的行号**：低危项仍按修复前行号定位；高危/中危项已随修复更新为文件级定位。
