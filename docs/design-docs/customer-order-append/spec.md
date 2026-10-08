# Feature：顾客端「下单 / 加菜 / 结账」订单生命周期设计

**作者**：CodeBuddy（代码调研 + 起草）
**日期**：2026-10-07
**状态**：Draft（待评审）
**关联代码**：`frontend/apps/customer/src/{OrderPage.vue,order/*,composables/useScrollSpy.ts}`、`frontend/packages/customer-core/src/*`、`frontend/packages/api/src/customer.ts`、`frontend/packages/types/src/entities.ts`、`backend/internal/service/customer_service.go`、`backend/internal/service/order_service.go`、`backend/internal/service/table_service.go`、`backend/internal/store/dao/order.go`、`backend/internal/handler/customer.go`、`backend/internal/handler/order.go`

> 路径按前端 workspaces 重构后的位置书写（跨端可复用层在 `frontend/packages/`，顾客端页面在 `frontend/apps/customer/`）。

---

## 0. 决策记录（本轮讨论结论）

1. **加菜按钮在"未结账"期间常驻**：订单进行中且未付款时，「加菜」按钮始终可用，菜品累加到同一笔订单。
2. **付款 = 顾客侧（结账维度）结束**：顾客付完款，这一单对顾客而言就闭环了。
3. **出餐进度是商家维度**：付款时若出餐尚未结束，出餐流程**继续**，直到商家推进到"完成"。
4. **付款后同一桌可以再点新单**：因为对顾客来说那是"新的一单"。

由此得出核心模型：**两个维度解耦 + 一桌多单**。

---

## 1. 需求背景

### 1.1 问题与场景

顾客扫码点餐后常见"吃到一半还想再点"。早年实现里，这一诉求与"付款""后厨出餐"纠缠在同一个订单状态上，导致若干死路：

- 顾客付了款还想加菜 → 加菜入口消失，重下单又被拦；
- 订单完成/取消后，页面没有任何出口；
- 加菜时改用餐人数，茶位费补不上。

### 1.2 现状分析（代码调研事实）

**（1）两个维度其实已经分开了**——现状把资金与出餐拆成两个独立字段，只是顾客端的"结束语义"没有对齐：

| 维度 | 现有字段 | 语义 |
|---|---|---|
| 结账 / 资金 | `tb_order.pay_status` | 0 未支付 / 1 已支付 |
| 出餐进度 | `tb_order.order_status` | 1 已下单 / 2 制作中 / 3 已上齐 / 4 已完成 / 5 已取消（`service.go:41-47`） |

**（2）加菜窗口本来就绑在"未支付"上**（与决策 1 一致，结构上已具备）：

```272:272:frontend/apps/customer/src/OrderPage.vue
const canAppend = computed(() => isActive.value && Number(currentOrder.value?.payStatus) === 0);
```

**（3）但顾客端"结束"被绑到了商家维度**——付款后顾客只能被动等商家确认：

```217:223:frontend/apps/customer/src/order/OrderTrackView.vue
      <div
        v-else-if="view === 'order' && currentOrder.payStatus === 1"
        class="paid-badge"
      >
        已支付，等待商家确认用餐结束
      </div>
```

**（4）系统各处都写死了"一桌一单"假设**（本设计的改造重点）：

| 位置 | 现状假设 |
|---|---|
| `customer_service.go:215-221` | 该桌有进行中订单(1/2/3)即拒绝新单（`ErrActiveOrderDuplicate`） |
| `customer_service.go:59-65` | 顾客扫码只返回"最近一笔进行中订单" |
| `dao/order.go:85-91` | `GetActiveOrderByTable` 取 `ORDER BY order_id DESC LIMIT 1` |
| `dao/order.go:341-345` | `CountActiveOrdersTx` 统计 `order_status IN (1,2,3)` |
| `order_service.go:98-104` | 看板每桌只保留 `order_id` 最大的一笔 |
| `table_service.go:66-70` | 删桌台前只检查"是否存在进行中订单" |
| `order_service.go:178-182,309,441,476` | 任一订单完成/取消即把桌台置空闲(`SetTableStatusTx(...,0)`) |

**（5）三条历史死路**（本设计要一并消除）：

- **① 先付后吃无法加菜**：付款后加菜按钮消失，重下单被 `ErrActiveOrderDuplicate` 拦 → 死路。
- **② 完成/取消无出口**：`showTabbar` 仅在 `menu|order` 显示（`OrderPage.vue:253`），`done/canceled` 页无任何返回入口。
- **③ 加菜不能改人数**：加菜态隐藏人数控件（`CartDrawer.vue:116`），后端沿用旧 `personCount`（`customer_service.go:328`）。

> 注：本设计**不做**"已支付后差额补付"。原因见附录 A（现有系统存在 `pay_status=1 ⇒ paid_amount = total - refund` 的硬不变式，差额方案会与之冲突并需账务重构）。本模型下"已支付未足额"状态不会出现，从根上规避该问题。

---

## 2. 目标与非目标

### 2.1 目标

1. **（P0）加菜常驻**：未结账期间，「加菜」按钮在订单页始终可用。
2. **（P0）顾客侧以付款结束**：付款成功后，顾客端本单闭环，可继续查看出餐进度或再点新单。
3. **（P0）出餐维度独立**：付款不打断出餐流程；订单归档与桌台释放由商家维度决定。
4. **（P0）一桌多单**：付款后同一桌可再开新单，与仍在出餐的旧单并存。
5. **（P0）结束出口**：订单完成后提供「再点一单」，回菜单开新单。
6. **（P1）加菜可改人数**：加菜请求可携带人数，后端据此重算茶位费。
7. **（P1）幂等**：下单/加菜重复提交不产生重复订单或重复菜品。

### 2.2 非目标

- 不做顾客端退菜/减菜（统一走管理端改单 `EditOrder`）。
- 不做拆单/并桌/转台。
- 不改动账务模型（不引入差额/部分支付）。
- 不重构订单状态机（`order_status` 语义保持）。

---

## 3. 概念模型

### 3.1 两个维度

| 维度 | 归属 | 状态 | 推进者 |
|---|---|---|---|
| **结账 / 资金** | 顾客 | 未结账(0) → **已结账(1)** | 顾客在线支付 或 收银员收款(`PayOrder`) |
| **出餐进度** | 商家 | 1 已下单 → 2 制作中 → 3 已上齐 → 4 完成 / 5 取消 | 后厨 / 收银员 |

- **顾客侧结束** = `pay_status = 1`（付款完成，售出即闭环）。
- **商家侧结束** = `order_status ∈ {4, 5}`（出餐完成或取消，桌台可释放）。
- 两者**互不触发**：付款不归档订单；归档不等于已付款（免单/挂账即为归档但未收款）。

### 3.2 状态与判定表

| 场景 | 判定 | 说明 |
|---|---|---|
| 能否加菜 | `order_status ∈ {1,2,3}` **且** `pay_status = 0` | 未结账期间常驻 |
| 能否付款 | `order_status ∈ {1,2,3}` **且** `pay_status = 0` | 付款后顾客侧结束 |
| 能否开新单（同店同菜） | 该桌**无**未结账单（见 §3.3） | 已付款的旧单不阻断 |
| 顾客端展示"已结账" | `pay_status = 1` 且 `order_status ∈ {1,2,3}` | 可继续看出餐进度 |
| 顾客端展示"已完成" | `order_status ∈ {4,5}` | 商家维度终结 |
| 桌台是否空闲 | 该桌**无**未完成订单（`order_status ∈ {1,2,3}`） | 见 §3.4 |

### 3.3 一桌多单与「未结账单唯一」约束

放开"一桌一单"后会带出新问题：**若一桌同时存在两笔未结账订单，顾客点"加菜"加到哪一单？防重复提交也失去依据。** 因此约束收紧为：

> **同一桌台，最多存在一笔「未结账单」**（`pay_status=0 且 order_status IN (1,2,3)`）；**已结账的订单可以有多笔**（各自出餐）。

- 下新单时：若该桌已有未结账单 → 拒绝并引导"继续加菜或先结账"；否则允许新单。
- 这样既保留"防双击 / 防重复开单"，又实现"付款后再点 = 新单"。

### 3.4 桌台占用与释放

- **占用**：桌台创建订单时置忙（沿用 `OccupyTableTx`，`dao/table.go:40`）。
- **释放**：仅当该桌**没有任何未完成订单**（`order_status ∈ {1,2,3}`）时才置空闲。
- 现状"任一订单完成/取消即置空闲"在多单场景下会**误释放**，必须改为按桌判定。

---

## 4. 需求

### 4.1 功能性需求

- **FR1 加菜常驻**：订单进行中且未结账时，订单页展示「加菜」按钮（与催菜并排）；点击进入菜单选菜，保留「已点 N」角标与轮询。
- **FR2 加菜提交**：加菜复用**该桌唯一未结账簿**，合并明细、重算金额；只打印新增菜品厨房单。
- **FR3 顾客侧结束**：付款成功后，顾客端显示"已结账"，本单对顾客结束；仍可查看出餐进度。
- **FR4 出餐独立**：付款/收款**不**改变 `order_status`；后厨继续推进 1→2→3→4。
- **FR5 一桌多单**：付款后同一桌可再开新单；新单与旧单互不影响。
- **FR6 结束出口**：`order_status ∈ {4,5}` 的顾客页面提供「再点一单」，回菜单并清空购物车。
- **FR7 加菜改人数**：加菜请求可携带 `personCount`，后端据此重算茶位费（缺省沿用订单现值）。
- **FR8 幂等**：下单/加菜请求携带 `clientRequestId`，服务端去重。

### 4.2 非功能性需求

- **NFR1 资金安全**：金额一律由后端依据 DB 重算（`ResolveOrderItems` + `RecalcAmount`），客户端只传 `dishId/specId/quantity`。
- **NFR2 并发正确**：加菜与改单/收款/取消在同一订单行上串行化（`TouchOrderTx`）。
- **NFR3 兼容**：现有接口契约向后兼容；管理端看板接口的字段调整为**破坏性变更**，需前后端同步发布。
- **NFR4 可回滚**：不改账务、不改状态机，按 PR 粒度可回滚。

---

## 5. 详细设计

### 5.1 顾客侧流程与视图

```
扫码进入 (/order/:tableId)
  │
  ├─ 该桌有「未结账单」 ─▶ 进入该单（view='order'）
  │        ├─ 可加菜（常驻）
  │        ├─ 可催菜
  │        └─ 可付款（view='pay'）
  │
  └─ 无「未结账单」 ─▶ 菜单（view='menu'），可开新单
           └─ 顶部/订单 Tab 可查看本桌「出餐中」的已结账订单

付款成功
  └─▶ 顾客侧结束（view='settled'）
        ├─ 展示"已结账" + 出餐进度（商家维度 1/2/3）
        └─ 提供「再点一单」→ 清空订单/购物车 → view='menu'（新单）

出餐完成(4) / 取消(5)
  └─▶ view='done' / 'canceled' + 「再点一单」
```

视图状态机在现有 `ViewState`（`useOrderPolling.ts:6`）基础上**新增 `settled`**：
`loading | error | menu | order | settled | done | canceled | pay`

### 5.2 加菜（未结账期间）

- 入口、抽屉文案、菜单角标**保持现状**（已实现且正确）。
- 后端归属校验、事务行锁、明细合并、金额重算**保持现状**（`customer_service.go:271-356`）。
- 新增：请求可携带 `personCount`（FR7）；携带 `clientRequestId`（FR8）。

### 5.3 付款与顾客侧结束

- **触发点**：顾客在线支付成功（`ApplyPaymentSuccess` → `MarkOrderPaidByChannel`）或收银员收款（`PayOrder` → `MarkOrderPaid`）。
- **后端**：**不改**——两条路径本来就只置 `pay_status=1`、不归档订单（`dao/order.go:369-378`、`:524-532`）。
- **前端**：把 `pay_status=1` 作为顾客侧结束的依据，切换到 `settled` 视图，而不是停留在 `order` 的"等待商家确认"。
- **轮询**：`useOrderPolling` 在 `pay_status=1` 时不再把视图锁死在 `order`；继续轮询出餐进度（1/2/3→4），供 `settled` 视图更新。

### 5.4 商家侧：出餐与看板

- 出餐流程**不动**：后厨按 `order_status` 推进。
- **看板改为支持一桌多单**：`BoardTable` 由单笔 `Order *BoardOrder` 改为多笔 `Orders []BoardOrder`。
- 一桌多单后，桌台卡片内按时间列出多笔订单，各自可收款/改单/完成。

### 5.5 桌台占用与释放规则（关键）

- 释放改为"按桌判定"：仅当该桌无未完成订单时置空闲。
- 封装统一函数，替换现有四处"任一单完成即释放"的写法。

### 5.6 幂等

- 请求体可选 `clientRequestId`（UUID，前端点击生成、超时重试复用）。
- 服务端在"桌台 + requestId"范围内去重：重复请求返回首次结果，不重复插单/插明细。
- 存储：新增 `tb_order_request(request_id VARCHAR PK, table_id, order_id, create_time)`。

---

## 6. 数据模型

### 6.1 无需新增业务列

两个维度字段（`pay_status` / `order_status`）已存在，本设计**不新增业务字段**。

### 6.2 建议新增索引（性能）

```sql
-- 支撑「未结账单」判定与看板查询
CREATE INDEX idx_order_table_unpaid ON tb_order(table_id, pay_status, order_status);
```

### 6.3 幂等表（P1，可选）

```sql
CREATE TABLE tb_order_request (
  request_id  VARCHAR(64) PRIMARY KEY,
  table_id    INTEGER NOT NULL,
  order_id    INTEGER NOT NULL,
  create_time VARCHAR(32)
);
```

迁移脚本按 `backend/migrations/` 约定生成 SQLite / MySQL 双份。

---

## 7. 接口设计

### 7.1 顾客端

| 接口 | 变更 |
|---|---|
| `GET /customer/table/:ref` | `currentOrder` 语义改为返回**未结账单**（`pay_status=0 且 1/2/3`）；可选新增 `servingOrders[]`（本桌出餐中的已结账订单，供查看进度） |
| `POST /customer/order` | 校验条件由"该桌无进行中订单"改为"该桌无未结账单"；新增可选 `clientRequestId` |
| `POST /customer/order/append` | 新增可选 `personCount`、`clientRequestId` |
| `GET /customer/order/no/:orderNo` | 无变更（`payStatus` 已含） |
| `POST /customer/pay/create` | 无变更 |

### 7.2 管理端

| 接口 | 变更 |
|---|---|
| `GET /admin/order/board` | **破坏性**：每桌由 `order`（单笔）改为 `orders`（数组） |
| 其余（list/get/status/pay/settle/finish/cancel） | 无变更（仍按单操作） |

### 7.3 错误语义

- `该桌台已有未结账的订单，请继续加菜或先结账`（替代原 `ErrActiveOrderDuplicate` 文案）。
- `订单不属于当前桌台` / `当前订单状态不可加菜` / `订单状态已变更，请刷新后重试`。

---

## 8. 实现方案（文件级改动清单）

### 8.1 后端 · 判定与查询

| # | 文件 | 改动 |
|---|---|---|
| B1 | `backend/internal/store/dao/order.go` | 新增 `CountUnpaidOrdersTx(tx, tableID)`：`WHERE table_id=? AND pay_status=0 AND order_status IN (1,2,3)` |
| B2 | 同上 | 新增 `GetUnpaidOrderByTable(tableID)`：按 `pay_status=0 AND order_status IN (1,2,3)` 取 `ORDER BY order_id DESC LIMIT 1` |
| B3 | 同上 | 新增 `CountUnfinishedOrdersTx(tx, tableID)`：`WHERE table_id=? AND order_status IN (1,2,3)`（桌台释放判定用） |
| B4 | `backend/internal/service/customer_service.go:215-221` | `CountActiveOrdersTx` → `CountUnpaidOrdersTx`；拒绝文案调整 |
| B5 | `backend/internal/service/customer_service.go:59-65` | `CustomerTable` 改用 `GetUnpaidOrderByTable`；去掉对 `t.Status == 1` 的强依赖（存在已付款未出餐单时桌台仍为忙） |
| B6 | `backend/internal/service/customer_service.go:31` | `ErrActiveOrderDuplicate` 文案更新为"已有未结账订单…" |

### 8.2 后端 · 看板多单

| # | 文件 | 改动 |
|---|---|---|
| B7 | `backend/internal/service/order_service.go:64-135` | `BoardTable.Order *BoardOrder` → `Orders []BoardOrder`；`OrderBoard` 由"每桌取最新一笔"改为"收集全部进行中订单" |
| B8 | `backend/internal/handler/order.go:82-90` | 出参结构调整（`order` → `orders`） |
| B9 | `backend/internal/service/order_service_test.go` / `handler/order_test.go` | 断言更新 |

### 8.3 后端 · 桌台释放（按桌判定）

| # | 文件 | 改动 |
|---|---|---|
| B10 | `backend/internal/service/order_service.go` | 新增 `releaseTableIfIdle(tx, tableID)`：`CountUnfinishedOrdersTx == 0` 时 `SetTableStatusTx(tx, tableID, 0)`（**须在当前订单已更新为终态之后调用**） |
| B11 | `order_service.go:178-182`（`ChangeOrderStatus`） | 完成/取消分支改用 `releaseTableIfIdle` |
| B12 | `order_service.go:309`（`SettleOrder`） | 同上 |
| B13 | `order_service.go:441`（`FinishOrder`） | 同上 |
| B14 | `order_service.go:476`（`CancelOrder`） | 同上 |

### 8.4 后端 · 幂等（P1）

| # | 文件 | 改动 |
|---|---|---|
| B15 | `schema.go` + `migrations/` | 新增 `tb_order_request` 表 |
| B16 | `customer_service.go`(`CustomerCreateOrder`/`CustomerAppendOrder`) | 事务内校验并写入 `request_id`，重复直接返回首次结果 |
| B17 | `handler/customer.go:137-166` 等 | 请求体透传 `clientRequestId` |

### 8.5 前端 · 顾客端

| # | 文件 | 改动 |
|---|---|---|
| F1 | `frontend/packages/customer-core/src/useOrderPolling.ts` | `ViewState` 新增 `'settled'`；`payStatus=1` 时切 `settled` 而非停留 `order`；出餐完成(4)切 `done` |
| F2 | `frontend/apps/customer/src/OrderPage.vue` | 新增 `settled` 视图分支；新增 `startNewOrder()`（清 `currentOrder`/购物车 → `view='menu'`）；`canAppend` 保持 `isActive && payStatus===0` |
| F3 | `frontend/apps/customer/src/order/OrderTrackView.vue` | 付款后徽章由"等待商家确认"改为"已结账"；新增「再点一单」按钮（`settled`/`done`/`canceled` 可见） |
| F4 | `frontend/apps/customer/src/order/CartDrawer.vue` | 加菜态保留精简人数控件（FR7） |
| F5 | `frontend/packages/api/src/customer.ts` | `appendOrder`/`createOrder` payload 扩展 `personCount`/`clientRequestId`；`getTable` 返回类型加 `servingOrders` |
| F6 | `frontend/packages/types/src/entities.ts` | `TableInfo` 增加 `servingOrders`；`OrderCreatePayload` 扩展 |

### 8.6 前端 · 管理端

| # | 文件 | 改动 |
|---|---|---|
| F7 | `frontend/packages/types/src/entities.ts` | `TableInfo.order` → `orders: OrderSummary[]` |
| F8 | `frontend/packages/api/src/order.ts` | `getOrderBoard` 返回类型同步 |
| F9 | `frontend/apps/admin/src/views/Dashboard.vue` | 桌台卡片由单笔订单改为多笔渲染；`activeOrders` 展平多单 |
| F10 | `frontend/apps/admin/src/views/Tables.vue` | `openTableOrder` 适配一桌多单 |

---

## 9. 并发与边界

| 场景 | 处理 |
|---|---|
| 加菜 vs 收款/取消/改单并发 | 订单行锁（`TouchOrderTx`）+ 事务内 `GetOrderStateTx` 重读，状态已变则拒绝 |
| 同秒双击下单/加菜 | 前端 `submitting` + 后端 `clientRequestId` 幂等（P1） |
| 付款与开新单并发 | 新单校验放在事务内，基于同一桌台行锁（`OccupyTableTx`）串行化 |
| 多单桌台释放竞态 | `releaseTableIfIdle` 在事务内、订单已置终态后计数，避免误释放/漏释放 |
| 已付款订单再改单/取消 | 现有守卫已拒绝（`UpdateOrderForEditTx`/`CancelOrderTx` 要求 `pay_status=0`），无需改动 |
| 免单/挂账 | 属"结账"动作，直接归档(`order_status=4`)；不参与"未结账单"判定 |
| 旧二维码（纯数字桌台号） | 保持现状：不带订单能力，避免遍历枚举 |

---

## 10. 测试计划

- **单元测试**：未结账单判定（付款前/后、免单/挂账、取消）；桌台按桌释放（单/多单）；加菜改人数重算茶位费。
- **并发测试**：并发下单只允许一笔未结账单；加菜 vs 收款/取消。
- **回归**：`order_service_test.go`、`handler/orderflow_test.go`、`customer_service_test.go`。
- **接口测试**：`GET /admin/order/board` 一桌多单出参。
- **前端测试**：`OrderPage.test.ts` 补 `settled` 视图、`再点一单`、加菜改人数。

---

## 11. 分期实施

| 阶段 | 内容 | 风险 |
|---|---|---|
| **Phase 1（P0，最小闭环）** | B1/B2/B4/B5/B6（未结账单判定 + 顾客进入）+ F1/F2/F3（顾客侧结束 + 再点一单） | 低 |
| **Phase 2（P0，一桌多单）** | B3/B7/B8/B10~B14（看板多单 + 按桌释放）+ F7~F10（管理端适配） | 中（看板为破坏性变更，需前后端同步发布） |
| **Phase 3（P1）** | B15~B17 幂等 + F4/F5/F6 加菜改人数 | 低 |

**建议**：Phase 1 先行（顾客端闭环立即可见），Phase 2 因涉及管理端接口破坏性变更，单独 PR 与发布窗口。

---

## 12. 风险与取舍

1. **看板接口为破坏性变更**：`order` → `orders` 需前后端同版本发布；若需灰度，可先**新增** `orders` 字段并保留 `order`（取最新一笔）一版，再择机移除。
2. **桌台释放口径变化**：由"单完成"改"桌空闲"，需回归所有完成/取消/结账路径，避免桌台永久占用或提前释放。
3. **免单/挂账与出餐解耦**：当前结账即归档（`order_status=4`），出餐若未完成会被提前终结。本设计**保持现状**；若后续需要"挂账后仍出餐"，再单列议题。
4. **不加差额**：本模型下"已支付未足额"不会出现，规避了账务重构（见附录 A）。

---

## 附录 A：为什么不采用"已支付后差额补付"

现有系统存在一条被**每次启动强制回填**的不变式：

```607:609:backend/internal/store/schema.go
	// 历史数据回填:老订单没有实收金额,按「已支付即全额实收」补齐,保证报表口径连续。
	DB.Exec(`UPDATE tb_order SET paid_amount = total_amount - COALESCE(refund_amount,0)
		WHERE pay_status=1 AND (paid_amount IS NULL OR paid_amount=0) AND (settle_type IS NULL OR settle_type='normal')`)
```

即 **`pay_status=1` ⇒ `paid_amount = total_amount - refund`（已支付 = 已足额）**。而在线支付回写 `MarkOrderPaidByChannel` 本身**不写 `paid_amount`**（`dao/order.go:524-532`），实收依赖上述回填补齐。

若允许"已支付后再加菜"产生差额，会同时破坏：启动回填、报表口径（`dao/report.go`）、退款额度、结账校验。要闭环需重构账务模型，风险面大。

本设计选择"**付款即顾客侧结束、之后再点 = 新单**"，从根本上不产生"已支付未足额"状态，因此**不需要任何账务改造**。
