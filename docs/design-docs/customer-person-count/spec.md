# Feature：顾客端「入座先选就餐人数」——茶位费准确性设计

**作者**：CodeBuddy（代码调研 + 起草）
**日期**：2026-10-18
**状态**：Draft（待评审）
**关联代码**：
`frontend/apps/customer/src/{OrderPage.vue,order/CartDrawer.vue,order/__tests__/OrderPage.test.ts}`、
`frontend/packages/customer-core/src/{index.ts,useCart.ts}`、
`frontend/packages/shared/src/sessionStorage.ts`、
`frontend/apps/miniprogram/src/{types.ts,app.ts}`、
`frontend/apps/miniprogram/src/store/{session.ts,cart.ts,cartPersist.ts}`、
`frontend/apps/miniprogram/src/pages/{index/index.ts,index/index.wxml,index/index.wxss,cart/index.ts,order/index.ts}`、
`backend/application/service/{service.go,customer_service.go,order_service.go}`

> 一句话：把「用餐人数」从购物车抽屉里的**可选字段**，前置为茶位费门店进入点餐前的**必过步骤**；以「菜单 + 半透明蒙层 + **贴底小面板**」呈现，面板**只选人数**，不展示茶位费——选完后茶位费按现状出现在购物车明细里即可；H5 与小程序两端统一。

---

## 0. 决策记录（本轮结论）

1. **仅茶位费开启时触发**：`seat_fee_enabled === '1' && seat_fee > 0`。未开启茶位费的门店保持现状，不增加额外步骤。
2. **H5 与小程序同步**：两端都加同一道进入关卡，交互与文案对齐。
3. **贴底小面板而非独立页/居中弹窗**：选人数以「菜单上方的半透明蒙层 + **底部停靠面板**」呈现（形态与购物车抽屉 `CartDrawer` 一致：顶部圆角、贴底、safe-area 适配）。菜单在蒙层下**可见但不可操作**——顾客扫码后不会面对白屏或陌生页面，且视线自然落在菜单底部，不会遮挡菜品的浏览感。
4. **面板只做一件事：选人数**。不展示茶位费预览、不写解释文案——茶位费按现状只出现在购物车明细（「茶位费（N人）」行）里，进入面板不替购物车说话。
5. **首次必须显式选择**：不预选人数，确认按钮在未选前禁用，避免「默认 1 人」在多人桌继续产生错误茶位费。
6. **已选过则跳过**：H5 用 `sessionStorage`（按桌隔离）；小程序用 `App.session` 标记 + 本地存储（按桌码，60 分钟窗口）。刷新 / 冷启动不重复打扰。
7. **不改后端接口与账务**：人数本来就随 `createOrder` 提交，后端 `RecalcAmount(personCount, items)` 按人重算茶位费；本设计只解决「进入时人数不对」。
8. **不改现有购物车抽屉与订单页改人数入口**：它们是下单前/下单后的修正通道，本设计在其前面补一道「初始值」关卡。

---

## 1. 需求背景

### 1.1 问题与场景

顾客扫码进入 H5 后，直接看到菜单并开始加菜。茶位费 = **人数 × 每人茶位费**，但人数默认是 1：

- 4 人桌的顾客不知道要先改人数，按默认 1 人结算 → **少收 3 人茶位费**；
- 底部结算栏在选菜后就会显示含茶位费的合计，**没打开购物车抽屉时合计已经是错的**；
- 顾客刷新页面（H5 是单页应用，误触刷新很常见）后人数回到 1，购物车却恢复了 → **菜在、人数没了**；
- 小程序同样只有购物车页能改人数，且冷启动后人数会随购物车恢复（有车时）或回 1（无车时），口径不稳定。

### 1.2 现状分析（代码调研事实）

| # | 位置 | 现状 |
|---|---|---|
| 1 | `frontend/apps/customer/src/OrderPage.vue:755` | `const personCount = ref(1)` —— H5 人数写死默认 1，且不持久化 |
| 2 | `frontend/apps/customer/src/OrderPage.vue:646` | 人数通过 `v-model:person-count` 只绑给 `CartDrawer`（购物车抽屉） |
| 3 | `frontend/apps/customer/src/order/CartDrawer.vue:118-183` | 人数控件只在「首次下单」态展示；加菜态是另一套精简控件（`:214-265`） |
| 4 | `frontend/apps/customer/src/OrderPage.vue:853-865` | `seatFee = personCount × seat_fee`；`totalPrice` 在 `dishTotal > 0` 时即含茶位费 |
| 5 | `frontend/apps/customer/src/composables/useCartPersist.ts:75-87` | 购物车持久化只存 `items`，**不存人数**，刷新后人数归 1 |
| 6 | `frontend/apps/miniprogram/src/store/session.ts:68-72` | `setPersonCount` 收敛 1..20；人数在 `App.session`，无「是否已显式选择」标记 |
| 7 | `frontend/apps/miniprogram/src/pages/cart/index.ts:158-164` | 小程序人数也只能在购物车页改 |
| 8 | `frontend/apps/miniprogram/src/store/cartPersist.ts:194` | 只有「有车可恢复」时才顺带恢复人数；无车的冷启动人数为 1 |
| 9 | `backend/application/service/service.go:243-264` | 后端 `RecalcAmount`：茶位费 = `personCount × seat_fee`，按配置开关 |
| 10 | `backend/application/service/customer_service.go:220-225` | 下单时人数 `< 1` 兜到 1，`> 100` 拒绝；两端前端上限 20 |

**结论**：问题不在金额计算，而在**进入时的人数初始值**。人数一旦正确，前后端现有计价链路都是对的。茶位费的**展示**也已由购物车承担（`CartDrawer.vue:189-194` 的「茶位费（N人）」行、小程序 `cart/index.wxml:105-111`），面板无需重复。

---

## 2. 目标与非目标

### 2.1 目标

1. **（P0）H5 进入关卡**：茶位费门店的新桌顾客，进入菜单后先在**底部面板**选人数；确认后即可点菜。**菜单在蒙层下始终可见**。
2. **（P0）小程序对齐**：同一流程在小程序落地，交互与文案一致。
3. **（P0）从第一道菜起合计正确**：底部结算栏的茶位费与最终下单金额一致；购物车明细照现状展示「茶位费（N人）」。
4. **（P0）不重复打扰**：同一会话刷新、同一次用餐的冷启动，不再次弹面板。
5. **（P1）口径统一**：人数范围 1..20、选择值在菜单页/购物车页/订单页三处一致。
6. **（P1）可回归**：现有「已有订单 / 加菜 / 锁定桌台 / 浏览模式」路径行为不变。

### 2.2 非目标

- 不改后端下单 / 加菜 / 改人数接口与账务模型。
- 不做「按人头自动识别」（摄像头/座位传感器）等硬件方案。
- **面板不做茶位费科普/预览**：金额展示维持现状（购物车明细 + 底部结算栏合计）。
- 不重构购物车抽屉里的人数控件（它仍是下单前的最后修正入口）。
- 不提供「跳过 / 稍后再选」入口（会重新引入默认 1 人的漏选问题；若产品后续要求，见 §12）。
- 不在本次补齐「H5 订单页未支付时改人数」的入口（小程序有 `/customer/order/person`，H5 目前没有；属独立小需求，见 §12）。

---

## 3. 交互设计

### 3.1 H5 进入流程

```
扫码进入 /order/:tableRef
  │
  ├─ load() 并发拉 config / table / menu / remarks
  │
  ├─ 本桌已有未结账订单 ──────────▶ resume →（是我的）order /（不是）浏览菜单
  ├─ 桌台停用 / 预约 ─────────────▶ locked
  │
  └─ 无订单 + 桌台正常 ─▶ menu（菜单、hero、底部导航照常渲染）
        │
        ├─ 茶位费未开启 ─────────────▶ 直接可点（保持现状）
        └─ 茶位费已开启
              ├─ 本会话已选过人数（sessionStorage 有值）─▶ 用该值，无蒙层
              └─ 未选过 ───────────▶ personGate=true（蒙层 + 贴底面板）
                                       顾客看得到菜单，但点不到
                                       │ 在底部面板上确认
                                       ▼
                                personCount = N
                                sessionStorage 落盘
                                personGate=false → 直接点菜
```

**关键点**：
- 菜单**始终先渲染**，蒙层与面板只是盖在上面 —— 不会出现「先看一个人数页、再进菜单」的跳变；
- 面板**贴底停靠**（与购物车抽屉同形态），不挡菜单主体；蒙层半透明，菜品仍然可见；
- 关卡只挡「新桌首单」路径；已有订单意味着人数已经写在订单里，不重问。

### 3.2 选人数面板 UI 规格（H5）

| 元素 | 规格 |
|---|---|
| 呈现形式 | 半透明蒙层（`rgba(15,15,20,0.45)`）+ **贴底面板**：顶部圆角 `18px`、贴底停靠、`safe-area-inset-bottom` 适配——与 `CartDrawer` 的抽屉形态一致；菜单在蒙层上方区域可见 |
| 面板内容 | **只有「选人数」一件事**：标题 + 一行中性提示 + 快捷人数格子 + 步进器 + 确认按钮。**无茶位费预览**（提示只交代下一步，不涉及金额；高度约 200~240rpx，几乎不挡菜单） |
| 标题 | 「本桌用餐几位？」 |
| 辅助提示 | 「选好人数，即刻点餐～」（面板盖住菜单时，顾客需要知道下一步是什么；刻意不提茶位费） |
| 快捷人数 | `1~10` 五列两行等宽格子，单选高亮；点击即选中 |
| 步进器 | `−  N  +`，范围 `1..20`；点数字可切换为输入框直接输入（覆盖 11~20 的桌况） |
| 主按钮 | 「开始点餐」，与步进器同行（左步进器、右按钮）；未选人数时禁用（文案「请先选择人数」）；选中后「开始点餐（4人）」 |
| 关闭 | 无。面板不提供关闭入口，确认后才可点菜（保证人数被显式选择）；关闭页面 = 放弃点餐 |
| 覆盖范围 | 蒙层全屏 fixed，盖住 hero、菜单、底部结算栏与底部导航（z-index 高于 90/88，低于抽屉 1500） |
| 茶位费展示 | **面板内不展示**。确认后按现状：购物车抽屉明细有「茶位费（N人）」行，底部结算栏合计含茶位费 |

> **为什么默认不预选**：预选 1 人时，多人桌顾客仍可能直接点「开始点餐」；不预选 + 按钮禁用能保证这是一次**有意识的选择**。代价是单人顾客多点一次，但换来的是茶位费的确定性。

### 3.3 小程序等价流程

```
onLoad（扫码 / 最近使用） → loadAll → 拉 config / table / menu / remarks
  │
  ├─ 本桌有未结账订单 ─▶ 归属确认 → 订单页 / 等待清台
  ├─ 桌台锁定 ─────────▶ 错误态
  └─ 无订单 + 正常 ─▶ 菜单页照常渲染（hero、分类、菜品、结算栏都在）
        │
        ├─ 茶位费未开启 ─────────────────▶ 直接可点
        └─ 茶位费已开启
              ├─ session.personChosen 或本地存储有有效值 ─▶ 直接可点
              └─ 都没有 ─▶ needPerson=true（蒙层 + 贴底面板）
                              顾客看得到菜单，点不到
                              │ 在底部面板上确认
                              ▼
                       setPersonCount(N)
                       markPersonChosen()
                       本地存储落盘
                       needPerson=false → 直接点菜
```

小程序没有 `sessionStorage`，用 `App.session.personChosen` 覆盖「本次运行」，用 `wx.setStorageSync` 覆盖「冷启动」。窗口 60 分钟，与「未开单购物车」的恢复窗口一致（`cartPersist.ts:28`）。

### 3.4 文案（两端一致）

| 位置 | 文案 |
|---|---|
| 标题 | 本桌用餐几位？ |
| 辅助提示 | 选好人数，即刻点餐～ |
| 主按钮 | 开始点餐（N人） / 请先选择人数 |
| 校验提示 | 用餐人数需在 1~20 人之间 |

> 面板刻意**不出现**「茶位费」三个字：金额与说明是购物车明细的职责（现状已展示），面板保持一步即走的极简。

---

## 4. 触发判定与取值

### 4.1 触发判定表

| 场景 | 是否弹选人数面板 | 说明 |
|---|---|---|
| 茶位费关闭（`seat_fee_enabled != '1'` 或 `seat_fee <= 0`） | 否 | 保持现状 |
| 茶位费开启 + 本桌无订单 + 本会话未选过 | **是（蒙层 + 贴底面板）** | 本次设计的核心路径 |
| 茶位费开启 + 本桌有未结账订单（resume/order） | 否 | 人数在订单里 |
| 茶位费开启 + 加菜（appendMode） | 否 | `startAppend` 已同步 `currentOrder.personCount` |
| 茶位费开启 + 锁定桌台 | 否 | 不进菜单 |
| 茶位费开启 + 「不是本人的单」浏览模式 | 否 | 提交被拦截，人数不参与金额 |
| 茶位费开启 + 「再点一单」 | 否 | 沿用上一单人数（见 §5.1.5） |
| 茶位费开启 + 同会话刷新 | 否 | sessionStorage / session 标记命中 |

### 4.2 人数来源优先级

**H5**（仅「无订单」路径需要）：

1. `sessionStorage[dining:person:{tableId}]`（本次会话已选值）
2. 用户在面板上显式选择
3. `1`（兜底，仅在茶位费关闭或异常时使用）

**小程序**：

1. `App.session.personChosen ? App.session.personCount : 无`
2. 本地存储 `dining_mp_person:{tableCode}`（`at` 在 60 分钟内）
3. 用户在面板上显式选择
4. `1`（兜底）

> 有订单时（resume / 加菜 / 再点一单），人数以订单 `personCount` 为准，不走上表。

### 4.3 记忆与失效

| 端 | 存储 | Key | 生命周期 |
|---|---|---|---|
| H5 | `sessionStorage` | `dining:person:{tableId}` | 标签页会话；换桌不同 key；关掉重开即失效 |
| 小程序 | `App.session.personChosen` | — | 本次运行（含页面切换） |
| 小程序 | `wx.setStorageSync` | `dining_mp_person:{tableCode}`，值 `{ count, at }` | 60 分钟；换桌不同 key；同一部手机 = 同一位顾客 |

**与现有隔离语义对齐**：
- H5 的 `sessionStorage` 已经用于「订单归属认领」（`dining:mine:{tableId}:{orderNo}`）与购物车会话起点，本设计复用同一层语义：**同一标签页 = 同一批客人**。
- 小程序没有会话级存储，冷启动就是新会话；用桌台码 + 60 分钟窗口隔离，与 `cartPersist` 的「未开单只保留 60 分钟」完全一致。

---

## 5. 详细设计

### 5.1 H5

#### 5.1.1 呈现方式：菜单上方的蒙层 + 贴底面板（不改 `ViewState`）

**不新增 `ViewState`**。`view` 保持 `'menu'`，`HeroHeader`、`MenuView`、底部导航、结算栏照常渲染；在其上叠加「蒙层 + 贴底面板」，由 `personGate` 控制：

```ts
const personGate = ref(false); // 选人数蒙层：true = 盖在菜单上方（可见不可点）
```

菜单在蒙层下「可见不可点」：蒙层是全屏 fixed 元素，天然拦截点击；面板贴底停靠（复用 `CartDrawer` 的视觉语言），确认后 `personGate = false`，顾客无缝继续点菜，**不需要重新渲染菜单**，也不会经历「先看人数页、再进菜单」的跳变。

#### 5.1.2 状态与常量

在 `OrderPage.vue` 中：

```ts
// 选人数蒙层：true = 覆盖在菜单上方（顾客看得到菜单但点不到）
const personGate = ref(false);
// 底部面板上的「草稿值」：0 = 还没选（与真实 personCount 分开，
// 避免未选时被默认值混过去）
const personDraft = ref(0);

const PERSON_MIN = 1;
const PERSON_MAX = 20;

function personKey(): string {
  return `dining:person:${tableId.value}`;
}

/** 本会话为这张桌选过的人数；没选过返回 0。 */
function rememberedPerson(): number {
  const v = Math.floor(Number(safeSession.get(personKey())) || 0);
  return v >= PERSON_MIN && v <= PERSON_MAX ? v : 0;
}

/** 茶位费是否开启（进入关卡只看它）。 */
function seatFeeOn(cfg: PublicConfig): boolean {
  return cfg.seat_fee_enabled === '1' && Number(cfg.seat_fee) > 0;
}
```

> 建议把 `PERSON_MIN/PERSON_MAX` 抽到 `frontend/packages/customer-core/src/person.ts`，由 H5 `OrderPage` / `CartDrawer` 与小程序 `session.ts` 共用，避免三处各写一份 1/20。`customer-core` 的 `exports` 已支持 `@dining/customer-core/*` 子路径（`package.json:8-11`），小程序现在就在用 `@dining/customer-core/spec`。

#### 5.1.3 `load()` 分支

`OrderPage.vue:1240-1262` 的「无订单且桌台正常」分支改为：

```ts
if (tb && tb.currentOrder) {
  // ...现有 resume / order / settled 逻辑不变...
} else if (lockStatus.value) {
  view.value = 'locked';
} else {
  // 菜单照常进：就算要选人数，顾客也应该先看到菜单（蒙层形式）
  const remembered = rememberedPerson();
  if (remembered) personCount.value = remembered;
  view.value = 'menu';
  // 全新桌 + 茶位费开启 + 本会话没选过：菜单上方盖蒙层 + 贴底面板
  personGate.value = seatFeeOn(cfg) && !remembered;
  if (personGate.value) personDraft.value = 0;
}
```

注意：`cfg` 是 `load()` 里 `Promise.all` 拿到的公开配置，直接用局部变量，不依赖 computed 的时序。

#### 5.1.4 确认与持久化

```ts
function confirmPerson(): void {
  const n = Math.floor(Number(personDraft.value) || 0);
  if (n < PERSON_MIN || n > PERSON_MAX) return;
  personCount.value = n;
  safeSession.set(personKey(), String(n));
  // view 本来就是 'menu'：面板一收，菜单已渲染好，顾客直接点菜
  personGate.value = false;
}
```

`personCount` 的变化统一落盘（购物车抽屉改人数、加菜同步、再点一单都覆盖）：

```ts
watch(personCount, v => {
  if (tableId.value <= 0) return;
  const n = Math.floor(Number(v) || 0);
  if (n >= PERSON_MIN && n <= PERSON_MAX) safeSession.set(personKey(), String(n));
});
```

> 这样刷新后即使不弹面板，`personCount` 也能从 `sessionStorage` 恢复到上次的值；解决 §1.1 的「菜在、人数没了」。

#### 5.1.5 「再点一单」沿用上一单人数

`OrderPage.vue:1011-1020` 目前把 `personCount` 重置为 1。改为在清空订单前读取旧单人数：

```ts
function startNewOrder(): void {
  // 「再点一单」是同一批客人继续吃：人数沿用上一单，避免新单按 1 人收茶位费。
  // 旧单没有人数（理论上不会）时退回本会话记忆值，再退回 1。
  const prev = Number(currentOrder.value?.personCount) || rememberedPerson() || 1;
  currentOrder.value = null;
  cart.value = [];
  orderRemark.value = '';
  personCount.value = Math.max(PERSON_MIN, Math.min(PERSON_MAX, prev));
  view.value = 'menu';
  stopPolling();
}
```

不再重复弹面板：顾客刚在同一批人里点完，重问一次没有信息增量。

#### 5.1.6 模板与样式

面板作为独立弹层加在 `OrderPage.vue` 模板末尾（与 `CartDrawer`、`SpecSheet` 等弹层并列）：

```html
<!-- 选人数面板：蒙层盖在菜单之上（z-index 100 > 结算栏 90 / 底部导航 88，< 抽屉 1500），
     面板贴底停靠（与 CartDrawer 同形态）。菜单在蒙层下可见 ——
     顾客扫码后不会面对陌生页面，只是暂时点不了。
     面板只有「选人数」一件事：不展示茶位费，金额照旧在购物车明细里 -->
<div
  v-if="personGate"
  class="person-mask"
>
  <div class="person-sheet">
    <div class="ps-title">本桌用餐几位？</div>
    <div class="ps-hint">选好人数，即刻点餐～</div>

    <div class="pg-chips">
      <button
        v-for="n in PERSON_CHIPS"
        :key="n"
        class="pg-chip"
        :class="{ on: personDraft === n }"
        @click="personDraft = n"
      >{{ n }}</button>
    </div>

    <div class="ps-row">
      <div class="pg-stepper">
        <button
          class="pg-step"
          :disabled="personDraft <= PERSON_MIN"
          @click="personDraft = Math.max(PERSON_MIN, personDraft - 1)"
        >−</button>
        <input
          v-model.number="personDraft"
          class="pg-num"
          type="number"
          inputmode="numeric"
          :min="PERSON_MIN"
          :max="PERSON_MAX"
          placeholder="—"
        />
        <button
          class="pg-step"
          :disabled="personDraft >= PERSON_MAX"
          @click="personDraft = Math.min(PERSON_MAX, personDraft + 1)"
        >+</button>
      </div>
      <button
        class="pg-confirm"
        :disabled="personDraft < PERSON_MIN || personDraft > PERSON_MAX"
        @click="confirmPerson"
      >
        {{ personDraft >= PERSON_MIN ? `开始点餐（${personDraft}人）` : '请先选择人数' }}
      </button>
    </div>
  </div>
</div>
```

常量：`const PERSON_CHIPS = [1, 2, 3, 4, 5, 6, 8, 10];`

样式要点（视觉语言对齐 `CartDrawer` 的抽屉）：

```css
.person-mask {
  position: fixed;
  inset: 0;
  z-index: 100; /* 结算栏 90 / 底部导航 88 之上，抽屉 1500 之下 */
  background: rgba(15, 15, 20, 0.45);
  display: flex;
  align-items: flex-end; /* 面板贴底 */
  justify-content: center;
  touch-action: none; /* 拦截蒙层下的背景滚动（iOS Safari 兜底） */
}

.person-sheet {
  width: 100%;
  padding: 18px 18px calc(16px + env(safe-area-inset-bottom));
  border-radius: 18px 18px 0 0; /* 顶部圆角，与购物车抽屉一致 */
  background: #fff;
  animation: pg-up 0.24s ease-out; /* 自底部滑入，观感同 CartDrawer */
}

@keyframes pg-up {
  from { transform: translateY(40px); opacity: 0; }
  to   { transform: translateY(0);    opacity: 1; }
}

/* 步进器（左）与确认按钮（右）同一行：面板保持极简紧凑 */
.ps-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
}

.pg-confirm {
  flex: 1;
  height: 44px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15px;
  font-weight: 600;
}
```

chip 用品牌色高亮，`disabled` 态降低透明度；`.pg-chips` 单行排列（8 个 chip 在 480px 宽度内放得下）。整体面板高度约 180~200px。

> 两个实现细节：
> 1. `input` 直接 `v-model.number` 时输入 `abc` 会变成空串；`confirmPerson` 里再做一次 `Math.floor(Number(...))` 与范围校验，非法值不会写入。
> 2. 个别浏览器 fixed 蒙层仍可能带动背景滚动，必要时在 `personGate` 为真期间给 `.order-page` 临时加 `overflow: hidden`（`watch(personGate)` 同步增删）。

### 5.2 小程序

#### 5.2.1 会话字段

`frontend/apps/miniprogram/src/types.ts` 的 `SessionState` 增加：

```ts
/**
 * 是否已显式选择过就餐人数（茶位费门店进入时必须选一次）。
 * 与 personCount 分开：personCount 可能来自购物车恢复/订单，
 * 不能据此判断「顾客是否已经做过选择」。
 */
personChosen: boolean;
```

`app.ts:22` 初始化 `personChosen: false`。

#### 5.2.2 `session.ts` 工具函数

```ts
import { PERSON_MAX, PERSON_MIN, clampPerson } from '@dining/customer-core/person';

const PERSON_KEY_PREFIX = 'dining_mp_person';
/** 未开单的人数记忆窗口：与「未开单购物车」一致（cartPersist 60min）。 */
const PERSON_TTL_MS = 60 * 60 * 1000;

export function setPersonCount(n: number): number {
  const next = clampPerson(n);
  sessionState().personCount = next;
  return next;
}

/** 顾客显式选过人数（面板确认 / 购物车步进 / 订单页改人数 / 恢复购物车）。 */
export function markPersonChosen(): void {
  sessionState().personChosen = true;
}

interface PersistedPerson { count: number; at: number; }

export function persistPersonChoice(): void {
  const s = sessionState();
  if (!s.tableCode) return;
  try {
    wx.setStorageSync(`${PERSON_KEY_PREFIX}:${s.tableCode}`, {
      count: s.personCount,
      at: Date.now()
    } satisfies PersistedPerson);
  } catch { /* 存储不可用：退化为本次运行内有效 */ }
}

/** 冷启动恢复：换桌 / 过期 / 非法值一律视为「没选过」。 */
export function restorePersonChoice(): boolean {
  const s = sessionState();
  if (!s.tableCode) return false;
  let data: PersistedPerson | '' | undefined;
  try {
    data = wx.getStorageSync(`${PERSON_KEY_PREFIX}:${s.tableCode}`) as PersistedPerson | '' | undefined;
  } catch { return false; }
  if (!data || typeof data !== 'object') return false;
  const n = Math.floor(Number(data.count) || 0);
  const at = Number(data.at);
  if (n < PERSON_MIN || n > PERSON_MAX || !Number.isFinite(at) || Date.now() - at > PERSON_TTL_MS) return false;
  setPersonCount(n);
  markPersonChosen();
  return true;
}
```

> `personChosen` 不随 `resetOrder()` 清零：「再点一单」是同一批客人继续，沿用人数（与 H5 一致）。

#### 5.2.3 菜单页入口（`pages/index/index.ts`）

**`renderMenu(info)`** 菜单**照常完整渲染**，渲染完成后决定是否盖蒙层：

```ts
renderMenu(info: TableInfo) {
  // 先恢复购物车：恢复出人数本身就是一次「已选」证据
  restoreCart(this.menu);

  const s = sessionState();
  const seatFeeOn = s.config?.seat_fee_enabled === '1' && Number(s.config.seat_fee) > 0;
  // 有订单（含加菜）时人数以订单为准，不弹面板
  const needPerson = !s.currentOrder && seatFeeOn && !s.personChosen && !restorePersonChoice();

  // 菜单照常渲染：顾客扫码后立刻看得到菜品与桌号，
  // 蒙层只是盖在上面，不拦截渲染（体验关键，见 §3.3）
  // ...原有 cats / setData / refreshRows 不变...

  if (needPerson) {
    this.setData({ needPerson: true, personDraft: 0 }); // 不预选
  }
}
```

**确认事件**：

```ts
onConfirmPerson(): void {
  const n = Math.floor(Number(this.data.personDraft) || 0);
  if (n < PERSON_MIN || n > PERSON_MAX) {
    toast.info('用餐人数需在 1~20 人之间');
    return;
  }
  setPersonCount(n);
  markPersonChosen();
  persistPersonChoice();
  // 菜单一直在渲染（面板只是盖在上面）：收掉面板即可直接点菜
  this.setData({ needPerson: false });
}
```

**步进 / 直接输入**：`onPersonDraft(e)` 读 `data-delta` 或 chip 的 `data-n`；点数字打开 `wx.showModal({ editable: true })`（与订单页 `onEditPerson` 同款），输入合法后写 `personDraft`。

> `renderMenu` 会被 `loadAll`、`gotoOrder` 失败兜底多次调用。`restoreCart` 幂等（内存已有车时直接返回），`restorePersonChoice` 在 `personChosen` 为真时不再命中，所以面板不会重复出现。

#### 5.2.4 WXML / WXSS

面板加在 `pages/index/index.wxml` 末尾（`tab-bar` 之后、`.page` 内），fixed 定位盖住整页、面板贴底：

```html
<!-- 选人数面板：蒙层盖在整页之上，菜单在下面可见（z-index 需高于 tab-bar / cart-bar），
     面板贴底停靠（与购物车抽屉同形态）。catchtouchmove 拦截背景滚动。
     面板只有「选人数」：不展示茶位费，金额照旧在购物车明细里 -->
<view
  wx:if="{{needPerson}}"
  class="person-mask"
  catchtouchmove="noop"
>
  <view class="person-sheet">
    <view class="ps-title">本桌用餐几位？</view>
    <view class="ps-hint">选好人数，即刻点餐～</view>

    <view class="pg-chips">
      <view
        wx:for="{{personChips}}"
        wx:key="*this"
        class="pg-chip {{personDraft === item ? 'on' : ''}}"
        data-n="{{item}}"
        bindtap="onPickPerson"
      >{{item}}</view>
    </view>

    <view class="ps-row">
      <view class="pg-stepper">
        <view
          class="pg-step {{personDraft <= 1 ? 'off' : ''}}"
          data-delta="-1"
          bindtap="onPersonDraft"
        ></view>
        <view
          class="pg-num"
          bindtap="onInputPerson"
        >{{personDraft || '—'}}</view>
        <view
          class="pg-step {{personDraft >= 20 ? 'off' : ''}}"
          data-delta="1"
          bindtap="onPersonDraft"
        ></view>
      </view>
      <view
        class="btn-primary pg-confirm {{personDraft >= 1 ? '' : 'dim'}}"
        bindtap="onConfirmPerson"
      >
        {{personDraft >= 1 ? '开始点餐（' + personDraft + '人）' : '请先选择人数'}}
      </view>
    </view>
  </view>
</view>
```

页面 `data` 增加：`needPerson: false, personDraft: 0, personChips: [1,2,3,4,5,6,8,10]`。另加一个空的 `noop() {}` 方法供 `catchtouchmove` 使用。**无需** `seatFeePerText` / `personPreviewText`：面板不展示金额。

样式 `pages/index/index.wxss`：

```css
.person-mask {
  position: fixed;
  inset: 0;
  z-index: 120; /* 实现时确认需高于 cart-bar / 自定义 tab-bar 的实际层级 */
  background: rgba(15, 15, 20, 0.45);
  display: flex;
  align-items: flex-end; /* 面板贴底 */
}

.person-sheet {
  width: 100%;
  box-sizing: border-box;
  padding: 24rpx 32rpx calc(24rpx + env(safe-area-inset-bottom));
  border-radius: 36rpx 36rpx 0 0;
  background: #fff;
  animation: pg-up 0.24s ease-out;
}

@keyframes pg-up {
  from { transform: translateY(80rpx); opacity: 0; }
  to   { transform: translateY(0);     opacity: 1; }
}
```

其余 `.pg-chip` / `.pg-stepper` / `.pg-confirm` 与 H5 对齐（品牌橙、chip 单选高亮、步进器左 + 确认按钮右的同一行布局）。

#### 5.2.5 购物车页 / 订单页标记「已选」

- `pages/cart/index.ts:158-164` 的 `onPerson`：`setPersonCount` 后调用 `markPersonChosen()` + `persistPersonChoice()`。
- `pages/order/index.ts:400-411` 的改人数成功回调：已经 `setPersonCount`，补 `markPersonChosen()` + `persistPersonChoice()`。
- `store/cartPersist.ts:194` 恢复人数处：`setPersonCount(...)` 后补 `markPersonChosen()`（有车且带人数 = 之前选过）。

这样「手动改过人数」不会在下次冷启动又被面板拦住。

#### 5.2.6 冷启动恢复顺序

`loadAll` → `applyConfig/applyTable` → `renderMenu`：

1. `restoreCart(menu)`：有车则恢复人数（并标记已选）；
2. `restorePersonChoice()`：无车但有 60 分钟内的选择记录 → 恢复；
3. 两者都没有 → 菜单渲染完成后显示蒙层与面板。

顺序不能反：`restoreCart` 可能把人数恢复成 4，若先判了关卡再恢复，会出现「选了 4 又跳回 1」的闪烁。

---

## 6. 数据模型

### 6.1 数据库

**无变更**。人数已经存在 `tb_order.person_count`，茶位费由 `RecalcAmount` 实时重算；本设计不新增表/列。

### 6.2 H5 `sessionStorage`

| Key | 值 | 说明 |
|---|---|---|
| `dining:person:{tableId}` | `"4"` | 本会话为这张桌选过的人数 |

### 6.3 小程序本地存储

| Key | 值 | 说明 |
|---|---|---|
| `dining_mp_person:{tableCode}` | `{ "count": 4, "at": 1750000000000 }` | 60 分钟窗口；`at` 用于过期判断 |

---

## 7. 接口设计

**顾客端接口无变更。**

| 接口 | 说明 |
|---|---|
| `GET /customer/config`（公开配置） | 已返回 `seat_fee_enabled` / `seat_fee`，前端据此判断是否弹面板 |
| `POST /customer/order` | 已接收 `personCount`，后端 `CustomerCreateOrder` 已做 `1..100` 校验 |
| `POST /customer/order/append` | 已接收 `personCount`，加菜沿用订单人数 |
| `POST /customer/order/person` | 已存在（小程序在用），H5 本次不接 |

> 后端人数上限 100、前端上限 20：前端更严，后端不会拒绝正常请求；两端保持 20 是为了「茶位费 × 人数」不产生异常大额（`customer_service.go:602-606` 的注释也是这个口径）。

---

## 8. 边界与异常

| 场景 | 处理 |
|---|---|
| 面板显示期间 | 菜单在蒙层下**完整可见但不可操作**；蒙层拦截点击与滚动（H5 `touch-action:none`，小程序 `catchtouchmove`），确认后无缝继续，不重新渲染菜单 |
| 面板内容 | 只有标题 / chip / 步进器 / 确认按钮，不含任何金额信息；茶位费照现状在购物车明细与底部合计里展示 |
| 茶位费开启但 `seat_fee` 为 0 / 负数 | 视为未开启，不弹面板（与 `cartSettle` / `RecalcAmount` 的 `f > 0` 口径一致） |
| 顾客在面板上刷新页面 | 本会话未选过 → 菜单渲染后重新弹；已选过 → 直接可点 |
| 顾客选了人数但一直没下单，关掉重开（H5） | 新标签页 = 新会话，重新弹（合理：可能是下一批客人） |
| 小程序冷启动超过 60 分钟 | 重新弹（与未开单购物车窗口一致） |
| 小程序换桌 | key 带 `tableCode`，不会串人数 |
| 已有未结账订单 | 不弹；人数以订单为准 |
| 加菜 | 不弹；`startAppend` 同步 `currentOrder.personCount` |
| 「不是本人的单」浏览模式 | 不弹；提交被拦截，人数不参与金额 |
| 锁定 / 预约桌台 | 不弹；不进菜单 |
| 输入非法（空 / `abc` / `0` / `21`） | 不写入 `personCount`；按钮禁用或提示「1~20」 |
| 输入 `3.7` | `Math.floor` 收敛为 3 |
| `sessionStorage` / `wx.storage` 不可用 | 退化为每次进入都弹；不阻断点餐 |
| 「再点一单」 | 沿用上一单 `personCount`，不重弹 |
| 同一桌换一批客人但同一标签页（H5） | 与现有 `dining:mine` / 购物车会话语义一致：关掉重开即可；不额外处理 |
| 下单时人数与页面显示不一致 | 后端 `RecalcAmount` 以请求里的 `personCount` 为准；前端指纹已含 `personCount`，改人数会换幂等键（`OrderPage.vue:1068-1078`） |

---

## 9. 测试计划

### 9.1 H5（`frontend/apps/customer/src/order/__tests__/OrderPage.test.ts`）

1. **茶位费关闭**：`seat_fee_enabled='0'` → 进入即菜单可点，面板不出现（`personGate===false`）。
2. **茶位费开启 + 无订单 + 未选过**：`view==='menu'` 且面板显示（`personGate===true`），**菜单内容已渲染**（菜品行存在）；未选时确认按钮禁用；面板内不含金额文案。
3. **选 4 人确认**：面板收起（`personGate===false`），`personCount===4`，底部合计含 `4 × 单价`（合计来自既有 computed，回归验证）。
4. **同会话刷新**（`sessionStorage` 有值）：不弹面板，`personCount` 恢复为该值。
5. **已有订单**：`tb.currentOrder` 非空 → 走 resume/order，不弹面板。
6. **锁定桌台**：`lockStatus=1` → `locked`，不弹。
7. **非法输入**：`0 / 21 / ''` 不写入。
8. **再点一单**：沿用上一单 `personCount`，不重置为 1。
9. **购物车抽屉改人数**：`sessionStorage` 同步更新。

### 9.2 小程序

1. `pages/index/__tests__/entry.test.ts`：茶位费开启 + 无订单 → 菜单 rows 已渲染且 `needPerson===true`；关闭 → `needPerson===false`。
2. 新增 `pages/index/__tests__/person.test.ts`：
   - 选人数确认 → `needPerson===false`、`personChosen===true`、`personCount===N`；
   - 非法输入 → 提示且不写入；
   - 已有订单 / 加菜 → 不弹；
   - 冷启动 `restorePersonChoice()`：有效值恢复、过期值不恢复、换桌不恢复。
3. `store/__tests__/cartPersist.test.ts`：恢复购物车时带上人数并标记 `personChosen`。
4. `pages/cart/__tests__/submit.test.ts`：人数 4 时下单请求带 `personCount=4`（现有用例已覆盖金额，补进入面板后的链路）。

### 9.3 手动验收

- 真机 H5：扫码后**菜单立刻可见**（半透明蒙层 + 底部面板），选 4 人 + 一道 10 元菜（茶位费 2 元/人），底部合计应为 18 元，购物车明细出现「茶位费（4人）¥8.00」；刷新后仍为 18 元。
- 真机小程序：同上；杀进程重进（60 分钟内）不重复弹，人数仍为 4。
- 面板期间尝试点击/滚动背景菜单：不可操作；确认后立即可点。
- 茶位费关闭的门店：进入无面板，与改造前完全一致。

---

## 10. 文件级改动清单

### 10.1 H5

| # | 文件 | 改动 |
|---|---|---|
| F1 | `frontend/packages/customer-core/src/person.ts`（新增） | `PERSON_MIN/PERSON_MAX/clampPerson/validPerson`，并从 `index.ts` 导出 |
| F2 | `frontend/apps/customer/src/order/CartDrawer.vue` | 改为引用共享的 `PERSON_MIN/PERSON_MAX`（行为不变） |
| F3 | `frontend/apps/customer/src/OrderPage.vue` | `personGate/personDraft`、`personKey/rememberedPerson/seatFeeOn`、`load()` 决定是否显示面板（菜单照常进）、`confirmPerson`、`watch(personCount)`、`startNewOrder` 沿用人数、蒙层 + 贴底面板模板与样式 |
| F4 | `frontend/apps/customer/src/order/__tests__/OrderPage.test.ts` | §9.1 用例 |

### 10.2 小程序

| # | 文件 | 改动 |
|---|---|---|
| M1 | `frontend/apps/miniprogram/src/types.ts` | `SessionState.personChosen` |
| M2 | `frontend/apps/miniprogram/src/app.ts` | 初始化 `personChosen: false` |
| M3 | `frontend/apps/miniprogram/src/store/session.ts` | `markPersonChosen`、`persistPersonChoice`、`restorePersonChoice`；`setPersonCount` 用共享 `clampPerson` |
| M4 | `frontend/apps/miniprogram/src/pages/index/index.ts` | `needPerson/personDraft/personChips` 等 data；`renderMenu` 渲染后决定是否显示面板；`onConfirmPerson/onPickPerson/onPersonDraft/onInputPerson/noop` |
| M5 | `frontend/apps/miniprogram/src/pages/index/index.wxml` | 页面尾部选人数面板块（`wx:if="{{needPerson}}"`，`catchtouchmove` 拦背景滚动） |
| M6 | `frontend/apps/miniprogram/src/pages/index/index.wxss` | `.person-mask` / `.person-sheet` 等面板样式 |
| M7 | `frontend/apps/miniprogram/src/pages/cart/index.ts` | `onPerson` 后标记已选 + 落盘 |
| M8 | `frontend/apps/miniprogram/src/pages/order/index.ts` | 改人数成功后标记已选 + 落盘 |
| M9 | `frontend/apps/miniprogram/src/store/cartPersist.ts` | 恢复人数后标记已选 |
| M10 | `frontend/apps/miniprogram/src/pages/index/__tests__/person.test.ts`（新增） | §9.2 用例 |

### 10.3 后端

无改动。

---

## 11. 分期与发布

| 阶段 | 内容 | 说明 |
|---|---|---|
| **Phase 1（P0）** | F1~F4：H5 进入面板 + 持久化 + 再点一单 | 独立可发布，立即修正 H5 茶位费 |
| **Phase 2（P0）** | M1~M10：小程序对齐 | 与 Phase 1 同一套交互；可同版本发布 |
| **Phase 3（可选）** | H5 订单页补「未支付改人数」入口（接 `/customer/order/person`）；或面板加「稍后再选」入口（人数按 1 兜底） | 与小程序对齐的独立小需求，不在本次范围 |

两端均为纯前端改动，无接口/DB 变更，可按端独立灰度、独立回滚。

---

## 12. 风险与取舍

1. **强制选择的摩擦**：不预选 + 按钮禁用，单人顾客多点一次。换来的是多人桌茶位费的确定性；若门店反馈摩擦过大，可改成「预选 1 人」并把快捷 chip 做得更醒目（本方案不推荐，因为会重新引入「直接确认」的漏选）。
2. **面板不可关闭**：顾客在选好人数前点不了菜单。菜单在蒙层下可见、面板贴底不挡菜品主体，能大幅降低「什么都看不到」的烦躁（本次设计目标）；若产品仍希望允许先逛后选，可在 Phase 3 加「稍后再选」入口——人数按 1 兜底，下单前在购物车抽屉里仍可改（会在一定程度上重新引入漏选风险）。
3. **面板层级与滚动锁**：`z-index` 必须盖过结算栏（H5 90 / 底部导航 88；小程序实现时确认实际值）与底部导航；背景滚动用 `touch-action:none`（H5）与 `catchtouchmove`（小程序）拦截，避免顾客误点/误滚蒙层下的菜单。面板极简（~200px），小屏也不与蒙层外的内容冲突。
4. **H5 `sessionStorage` 的「同标签页 = 同一批客人」假设**：与现有 `dining:mine` 归属认领、购物车会话起点是同一假设。极端场景（同一标签页里换一批客人扫同一张桌）会沿用上批人数；关掉重开即恢复正确。若未来要更严格，可把 `visitStart` 并入 key。
5. **小程序 60 分钟窗口**：超过窗口重开会再问一次。这是「宁可多问一次，也不沿用陌生人的数」的取舍，与未开单购物车一致。
6. **`personCount` 上限 20 vs 后端 100**：前端 20 是茶位费异常大额的防线（`customer_service.go:602-606` 注释同口径）。大桌（>20）当前就走不通，属既有约束，不在本次放宽。
7. **H5 订单页没有改人数入口**：小程序有 `/customer/order/person`，H5 只能通过加菜携带人数。本设计不补，列 Phase 3；不影响「进入时选对人数」这一核心目标。
8. **`renderMenu` 多次调用**：靠 `restoreCart` 幂等 + `personChosen` 标记避免重复弹面板；实现时需注意「先恢复、后判关卡」的顺序（§5.2.6）。

---

## 13. 验收标准

- [ ] 茶位费开启的门店，H5 新桌进入后**菜单立刻可见**（半透明蒙层 + 底部面板盖在上面），确认人数后面板收起、直接可点。
- [ ] 面板只有「选人数」：无茶位费预览/说明文案；选完后购物车明细出现「茶位费（N人）」行。
- [ ] 未选人数时确认按钮不可点；选 4 人后底部合计 = 菜品 + 4 × 单价。
- [ ] H5 刷新后人数与合计保持，不再回到 1。
- [ ] 小程序同流程、同文案、同范围（1..20）；菜单同样在蒙层下可见、面板贴底。
- [ ] 小程序冷启动（60 分钟内）不重复弹，人数保持。
- [ ] 已有订单 / 加菜 / 锁定桌台 / 浏览模式均不弹面板，行为与改造前一致。
- [ ] 茶位费关闭的门店完全无感。
- [ ] 「再点一单」沿用上一单人数，新单茶位费正确。
- [ ] 两端测试通过：H5 `OrderPage.test.ts`、小程序 `entry/person/cartPersist` 用例。
