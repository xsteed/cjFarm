<template>
  <div class="order-page">
    <!-- 加载中:骨架屏按真实布局铺一遍(hero + 搜索 + 左栏 + 菜品行),
         比转圈更接近最终画面,加载完成时也不会整页「跳」一下(与小程序一致) -->
    <div
      v-if="view === 'loading'"
      class="sk-wrap"
    >
      <div class="sk-hero">
        <div class="sk-bar sk-shop"></div>
        <div class="sk-bar sk-table"></div>
      </div>
      <div class="sk-sheet">
        <div class="sk-bar sk-search"></div>
        <div class="sk-body">
          <div class="sk-cats">
            <div
              v-for="n in 6"
              :key="n"
              class="sk-bar sk-cat"
            ></div>
          </div>
          <div class="sk-dishes">
            <div
              v-for="n in 4"
              :key="n"
              class="sk-dish"
            >
              <div class="sk-bar sk-img"></div>
              <div class="sk-main">
                <div class="sk-bar sk-line"></div>
                <div class="sk-bar sk-line sk-line-short"></div>
                <div class="sk-bar sk-price"></div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 加载失败 -->
    <div
      v-else-if="view === 'error'"
      class="state-box"
    >
      <div class="err-icon">!</div>
      <div class="state-text">
        {{ loadError }}
      </div>
      <button
        class="retry-btn"
        @click="load"
      >
        重新加载
      </button>
    </div>

    <!-- 有 hero 的内容视图 -->
    <template v-else>
      <HeroHeader
        :shop-name="shopName"
        :table="table"
      />

      <!-- 白色圆角卡片:承载出餐提示条 + 菜单,整体上移盖住 hero 底部 -->
      <div
        v-if="view === 'menu'"
        class="menu-sheet"
      >
        <!-- 搜索框:放在白卡最顶部(不随菜单滚动),位置与小程序一致 -->
        <MenuSearch v-model:keyword="keyword" />

        <!-- 本桌「已付款但出餐未完成」的订单:顾客可查看进度 -->
        <div
          v-if="servingOrders.length"
          class="serving-banner"
        >
          本桌还有 {{ servingOrders.length }} 笔订单正在出餐
          <span
            v-for="o in servingOrders"
            :key="o.orderId"
            class="serving-chip"
          >
            #{{ o.shortNo || o.orderNo }} · {{ ORDER_STATUS_TEXT[Number(o.orderStatus)] || '处理中' }}
          </span>
        </div>

        <MenuView
          :menu="menu"
          :selected-spec="selectedSpec"
          :img-errs="imgErrs"
          :show-tabbar="showTabbar"
          :active-cat="activeCat"
          :ordered-qty="orderedQty"
          :current-spec="currentSpec"
          :dish-qty="dishQty"
          :select-spec="selectSpec"
          :change-qty="changeQty"
          :on-img-err="onImgErr"
          :set-list-ref="setListRef"
          :set-group-ref="setGroupRef"
          :jump-to="jumpTo"
          :handle-scroll="handleScroll"
          :keyword="keyword"
          :open-sheet="openSheet"
        />
      </div>

      <OrderTrackView
        v-else-if="currentOrder && (view === 'order' || view === 'settled' || view === 'done' || view === 'canceled')"
        :view="view"
        :current-order="currentOrder"
        :is-active="isActive"
        :can-append="canAppend"
        :urging="urging"
        :urge-cooldown="urgeCooldown"
        :show-tabbar="showTabbar"
        @append="startAppend"
        @urge="urge"
        @pay="view = 'pay'"
        @new-order="startNewOrder"
      />

      <!-- 还没下单:订单视图给空态而不是一片空白(导航常驻后进得来) -->
      <div
        v-else-if="view === 'order'"
        class="order-empty"
      >
        没有找到进行中的订单
      </div>

      <PayView
        v-else-if="currentOrder && view === 'pay'"
        :current-order="currentOrder"
        :pay-tab="payTab"
        :online-qr="onlineQr"
        :paying="paying"
        :online-enabled="onlineEnabled"
        :current-pay-qr="currentPayQr"
        @switch-tab="switchPayTab"
        @online-pay="startOnlinePay"
        @back="backFromPay"
      />
    </template>

    <!-- 底部悬浮结算栏 -->
    <CartBar
      v-if="view === 'menu'"
      :show-tabbar="showTabbar"
      :total-count="totalCount"
      :total-price="totalPrice"
      :can-append="canAppend"
      @open="cartVisible = true"
    />

    <!-- 底部导航:点餐 / 订单。已有订单时常驻,加菜时顾客可随时切回核对已下单菜品 -->
    <BottomTabBar
      v-if="showTabbar"
      :view="view"
      @switch="switchView"
    />

    <!-- 购物车抽屉 -->
    <CartDrawer
      v-model:visible="cartVisible"
      v-model:person-count="personCount"
      v-model:order-remark="orderRemark"
      :cart="cart"
      :total-count="totalCount"
      :dish-total="dishTotal"
      :seat-fee="seatFee"
      :discount="discount"
      :total-price="totalPrice"
      :can-append="canAppend"
      :submitting="submitting"
      :current-order="currentOrder"
      :open-remark="openRemark"
      :change-cart-qty="changeCartQty"
      @clear="onClearCart"
      @submit="submit"
    />

    <!-- 菜品单项备注弹窗。层级高于购物车抽屉,详见 RemarkSheet 的 .remark-mask -->
    <RemarkSheet
      v-if="remarkVisible"
      :dish-name="remarkTarget.item?.dishName || ''"
      :selected="remarkTarget.selected"
      :custom="remarkTarget.custom"
      :remarks="remarks"
      :has-remark="!!remarkTarget.item?.remark"
      @toggle="toggleRemark"
      @update:custom="remarkTarget.custom = $event"
      @clear="clearRemark"
      @close="closeRemark"
      @save="onRemarkSave"
    />

    <!-- 规格 / 单项备注弹层:多规格菜点「+」或点菜名时打开(对齐小程序 spec-sheet) -->
    <SpecSheet
      v-if="sheetVisible"
      :dish-name="sheetDish?.dishName || ''"
      :specs="sheetDish?.specs || []"
      :remarks="remarks"
      :selected-spec-id="sheetSpecId"
      :selected="sheetRemark.selected"
      :custom="sheetRemark.custom"
      :in-cart-qty="sheetInCartQty"
      @pick-spec="pickSheetSpec"
      @toggle-remark="toggleSheetRemark"
      @update:custom="sheetRemark.custom = $event"
      @close="closeSheet"
      @confirm="confirmSheet"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import {
  appendOrder,
  createOrder,
  createPay,
  getMenu,
  getOrderByNo,
  getPublicConfig,
  getRemarks,
  getTable
} from '@dining/api';
import QRCode from 'qrcode';
import type { MenuCategory, OrderDetail, OrderSummary, PublicConfig, RemarkOption, TableInfo } from '@dining/types';
import { isActiveStatus, ui } from '@dining/shared';
import { useCart, useOrderPolling, useUrge } from '@dining/customer-core';
import type { ViewState } from '@dining/customer-core';
import { useCartPersist } from './composables/useCartPersist';
import { useScrollSpy } from './composables/useScrollSpy';
import HeroHeader from './order/HeroHeader.vue';
import MenuView from './order/MenuView.vue';
import OrderTrackView from './order/OrderTrackView.vue';
import PayView from './order/PayView.vue';
import CartBar from './order/CartBar.vue';
import BottomTabBar from './order/BottomTabBar.vue';
import CartDrawer from './order/CartDrawer.vue';
import SpecSheet from './order/SpecSheet.vue';
import MenuSearch from './order/MenuSearch.vue';
import RemarkSheet from './order/RemarkSheet.vue';

const route = useRoute();
// 路由参数既可能是「桌台稳定码」(如 K7M3PQ9X),也可能是老的数字桌台 ID(如 1)。
// 先用它换取桌台详情,拿到数字 tableId 后再用于下单等接口。
const tableRef = String(route.params.tableId || '');
const tableId = ref(0);

const view = ref<ViewState>('loading'); // loading | error | menu | order | done | canceled | pay
const loadError = ref('');
const table = ref<TableInfo>({});
const menu = ref<MenuCategory[]>([]);
const remarks = ref<RemarkOption[]>([]);
const config = ref<PublicConfig>({});
const shopName = ref('扫码点餐');
const cartVisible = ref(false);
const personCount = ref(1);
const orderRemark = ref('');
const submitting = ref(false);
const currentOrder = ref<OrderDetail | null>(null);
const servingOrders = ref<OrderSummary[]>([]);
// 幂等键:同一次提交(含超时重试)复用同一 ID,后端据此去重。
let pendingRequestId = '';
const payTab = ref<'wx' | 'ali'>('wx');
const onlineQr = ref(''); // 在线支付二维码(dataURL)
const paying = ref(false);
const imgErrs = ref(new Set<number>());
// 菜单搜索关键词:放在页面层,搜索框与菜单列表共用(搜索框在白卡顶部,菜单里面只做过滤)
const keyword = ref('');

const ORDER_STATUS_TEXT: Record<number, string> = {
  1: '已下单，后厨正在接单',
  2: '正在制作中，请稍候',
  3: '餐品已上齐，请慢用',
  4: '订单已完成',
  5: '订单已取消'
};

const {
  cart,
  selectedSpec,
  remarkVisible,
  remarkTarget,
  totalCount,
  dishTotal,
  round2,
  currentSpec,
  dishQty,
  selectSpec,
  changeQty,
  changeCartQty,
  clearCart,
  openRemark,
  toggleRemark,
  closeRemark,
  clearRemark,
  saveRemark,
  sheetVisible,
  sheetDish,
  sheetSpecId,
  sheetRemark,
  sheetInCartQty,
  openSheet,
  closeSheet,
  pickSheetSpec,
  toggleSheetRemark,
  confirmSheet
} = useCart(remarks);

const { urging, urgeCooldown, urge, clearUrgeTimer } = useUrge(currentOrder, tableId);
const { startPolling, stopPolling } = useOrderPolling({ view, currentOrder, onlineQr, clearUrgeTimer });
const { activeCat, setListRef, setGroupRef, jumpTo, handleScroll } = useScrollSpy(menu);
// 刷新/重进不丢购物车:菜单到位后再按当前菜单复核恢复
const { restore: restoreCart } = useCartPersist(cart, tableId);

const seatFeeEnabled = computed(() => config.value.seat_fee_enabled === '1' && Number(config.value.seat_fee) > 0);
const seatFeePer = computed(() => Number(config.value.seat_fee) || 0);

const seatFee = computed(() => (seatFeeEnabled.value ? round2(personCount.value * seatFeePer.value) : 0));
const discount = computed(() => {
  const enabled = config.value.promotion_enabled === '1';
  const t = Number(config.value.promotion_threshold);
  const d = Number(config.value.promotion_discount);
  if (!enabled || !(t > 0) || !(d > 0) || dishTotal.value < t) return 0;
  return round2(Math.min(Math.floor(dishTotal.value / t) * d, dishTotal.value));
});
// 未选菜时不展示茶位费(合计为 0),选菜后才计入茶位费与优惠
const totalPrice = computed(() => (dishTotal.value > 0 ? round2(dishTotal.value + seatFee.value - discount.value) : 0));

const currentPayQr = computed(() => (payTab.value === 'wx' ? config.value.pay_qr_wx : config.value.pay_qr_ali));
// 该渠道是否已启用在线支付(未申请 key 前为 false,前端自动降级为码牌收款)
const onlineEnabled = computed(() =>
  payTab.value === 'wx' ? config.value.wxpay_enabled === '1' : config.value.alipay_enabled === '1'
);
// 是否已存在当前订单（订单视图能不能出内容、能不能催菜都看它）。
const hasOrder = computed(() => !!currentOrder.value);
// 底部 tabbar 常驻(点餐/订单两档):与小程序一致 —— 还没下单也能进订单页看空态,
// 不必先点菜才知道「有没有单」。支付页需要专注,不显示。
const showTabbar = computed(() => view.value === 'menu' || view.value === 'order');
// 已下单份数(按 dishId 聚合,同菜多规格累加)。菜单「已点」标记用这份数据。
const orderedMap = computed<Record<number, number>>(() => {
  const m: Record<number, number> = {};
  for (const it of currentOrder.value?.items || []) {
    const id = it.dishId as number;
    m[id] = (m[id] || 0) + Number(it.quantity || 0);
  }
  return m;
});
const orderedQty = (dishId: number | undefined) => orderedMap.value[dishId as number] || 0;

// 订单是否仍在进行中(1已下单/2制作中/3已上齐)。
// 加菜还需未支付;催菜只要求进行中——已付款但迟迟未上齐的订单同样可以催。
const isActive = computed(() => isActiveStatus(currentOrder.value?.orderStatus));

// ---- 下单 / 加菜 ----
// 存在进行中且未支付的订单时走加菜(追加到同一订单),否则首次下单。
const canAppend = computed(() => isActive.value && Number(currentOrder.value?.payStatus) === 0);

// 订单跟踪/支付视图的渲染已由模板上的 `currentOrder &&` 守卫收窄;
// CartDrawer 的 currentOrder prop 声明为可空并内部空值容错。

function onImgErr(id: number | undefined): void {
  const s = new Set(imgErrs.value);
  if (id !== undefined) s.add(id);
  imgErrs.value = s;
}

// 清空前二次确认:抽屉里一键清空太容易误触(与小程序 showModal 同文案)
function onClearCart(): void {
  if (!cart.value.length) return;
  const dlg = DialogPlugin.confirm({
    header: '清空购物车',
    body: '将移除已选的全部菜品，确定吗？',
    confirmBtn: { content: '清空', theme: 'danger' },
    cancelBtn: '再想想',
    onConfirm: () => {
      clearCart();
      dlg.destroy();
    }
  });
}

// 备注写回后给一句反馈:弹层直接消失的话,顾客不确定到底存没存上
function onRemarkSave(): void {
  saveRemark();
  ui.success('备注已保存');
}

function startAppend(): void {
  // 加菜期间保持轮询:后厨状态与已上齐菜品会持续刷新,菜单里的「已点」标记才不会过期
  cart.value = [];
  orderRemark.value = '';
  // 人数同步为订单当前值,加菜时可在抽屉里继续调整。
  personCount.value = currentOrder.value?.personCount || 1;
  view.value = 'menu';
}

// 顾客侧已结束(已结账/完成/取消)后的「再点一单」:清空当前订单与购物车,回菜单开新单。
function startNewOrder(): void {
  currentOrder.value = null;
  cart.value = [];
  orderRemark.value = '';
  personCount.value = 1;
  view.value = 'menu';
}

// 底部 tabbar 切页。加菜时随时切回订单页核对已下单菜品,购物车内容保留不丢。
function switchView(v: 'menu' | 'order'): void {
  if (view.value === v) return;
  if (v === 'order') {
    view.value = 'order';
    // 没单时展示空态即可,起轮询只会白拉接口(与小程序一致:订单页常驻可进)
    if (hasOrder.value) startPolling();
    else stopPolling();
  } else {
    view.value = 'menu';
  }
}

function genRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return 'req-' + Date.now().toString(36) + '-' + Math.random().toString(16).slice(2);
}

async function submit(): Promise<void> {
  if (!cart.value.length) {
    MessagePlugin.warning('请先选择菜品');
    return;
  }
  submitting.value = true;
  // 幂等:同一次提交复用同一 clientRequestId;成功后清空,下次提交重新生成。
  const clientRequestId = pendingRequestId || genRequestId();
  pendingRequestId = clientRequestId;
  try {
    const items = cart.value.map(i => ({
      dishId: i.dishId,
      specId: i.specId,
      quantity: i.quantity,
      itemRemark: i.remark
    }));
    if (canAppend.value) {
      const res = await appendOrder({
        tableId: tableId.value,
        orderNo: currentOrder.value?.orderNo,
        items,
        personCount: personCount.value,
        clientRequestId
      });
      MessagePlugin.success('加菜成功，已通知后厨');
      currentOrder.value = await getOrderByNo(res.orderNo);
    } else {
      const res = await createOrder({
        tableId: tableId.value,
        personCount: personCount.value,
        orderRemark: orderRemark.value,
        items,
        clientRequestId
      });
      MessagePlugin.success('下单成功，已通知后厨');
      currentOrder.value = await getOrderByNo(res.orderNo);
    }
    pendingRequestId = '';
    cart.value = [];
    cartVisible.value = false;
    view.value = 'order';
    startPolling();
  } catch {
    /* 失败已由拦截器统一提示,停留当前视图供重试;pendingRequestId 保留以便重试去重 */
  } finally {
    submitting.value = false;
  }
}

// ---- 在线支付 ----
function switchPayTab(tab: 'wx' | 'ali'): void {
  payTab.value = tab;
  onlineQr.value = ''; // 切换渠道时重置在线支付二维码
}

async function startOnlinePay(): Promise<void> {
  if (!currentOrder.value || paying.value) return;
  paying.value = true;
  try {
    const channel = payTab.value === 'wx' ? 'wxpay' : 'alipay';
    const res = await createPay({ orderNo: currentOrder.value.orderNo, channel });
    // 用 qrcode 库把支付串渲染为二维码图片(360px/M级容错)
    onlineQr.value = await QRCode.toDataURL(res.codeUrl, { width: 360, margin: 2, errorCorrectionLevel: 'M' });
    startPolling(); // 轮询支付结果
  } catch {
    /* 错误已由拦截器提示 */
  } finally {
    paying.value = false;
  }
}

function backFromPay(): void {
  onlineQr.value = '';
  view.value = 'order';
}

async function load(): Promise<void> {
  view.value = 'loading';
  loadError.value = '';
  try {
    const [cfg, tb, m, r] = await Promise.all([getPublicConfig(), getTable(tableRef), getMenu(), getRemarks()]);
    config.value = cfg;
    // 顶部展示「店铺名称 · 标语」(标语为系统配置的独立字段,留空则只显示名称)
    shopName.value = [cfg.shop_name, cfg.shop_slogan].filter(Boolean).join(' · ') || '扫码点餐';
    table.value = tb;
    tableId.value = tb.tableId ?? 0;
    servingOrders.value = tb.servingOrders || [];
    menu.value = m;
    remarks.value = r;
    // 默认选中每道菜的「当前可用规格」(与 currentSpec 的回退口径一致)。
    // 不能直接取 specs[0]:若第一个规格恰已售罄,选它会让 dishQty 按售罄规格的 key 计数,
    // 而 changeQty 实际加到 currentSpec 回退出的有货规格上 —— 卡片步进器恒显示 0、与购物车不一致。
    m.forEach(c =>
      c.dishes?.forEach(d => {
        if (d.specs && d.specs.length) selectedSpec[d.dishId as number] = currentSpec(d).specId as number;
      })
    );
    if (m.length) activeCat.value = m[0].categoryId ?? null;
    // 菜单到位后再恢复上次没提交的车:要拿当前菜单复核菜还在不在、有没有售罄
    restoreCart(m);
    // 桌台已有未结账订单 → 进入订单状态;已付款但出餐未完成则进入已结账视图。
    if (tb && tb.currentOrder) {
      currentOrder.value = tb.currentOrder;
      view.value = Number(tb.currentOrder.payStatus) === 1 ? 'settled' : 'order';
      startPolling();
    } else {
      view.value = 'menu';
    }
  } catch (e) {
    view.value = 'error';
    loadError.value = friendlyLoadError(e);
  }
}

// 把加载失败转成顾客能看懂、且知道下一步该做什么的中文提示。
// 拦截器已把 HTTP 错误统一成中文(msg 优先),这里再做一层场景化改写。
function friendlyLoadError(e: unknown): string {
  const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
  const msg = err?.friendlyMessage || err?.message || '';
  if (/桌台|桌号/.test(msg)) return '桌号不存在，请重新扫描餐桌上的二维码';
  if (/Network Error|网络连接失败/i.test(msg)) return '网络连接失败，请检查手机网络后重试';
  if (/超时/.test(msg)) return '网络响应较慢，请稍后重试';
  return msg || '加载失败，请检查网络后重试';
}

onMounted(load);
onUnmounted(() => {
  stopPolling();
  clearUrgeTimer();
});
</script>

<style scoped>
.order-page {
  max-width: 480px;
  margin: 0 auto;
  height: 100vh;
  height: 100dvh;
  background: var(--bg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ---------- 加载骨架屏 ----------
   按真实布局铺一遍（渐变头部 + 搜索 + 左栏 + 菜品行），加载完成时不会整页跳。
   白色区域的灰条用横向流光；头部在渐变底上改用透明度呼吸，避免灰底压在品牌色上。
   尺寸与 HeroHeader / MenuView 的真实布局对齐（1rpx ≈ 0.5px 换算）。 */
.sk-wrap {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.sk-bar {
  background: linear-gradient(90deg, #eef0f2 25%, #f8f9fa 37%, #eef0f2 63%);
  background-size: 400% 100%;
  animation: sk-sweep 1.4s ease infinite;
}

@keyframes sk-sweep {
  from {
    background-position: 100% 50%;
  }

  to {
    background-position: 0 50%;
  }
}

.sk-hero {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;

  /* 与 .op-hero 同一套内边距:加载完成切到真实头部时不跳高 */
  padding: calc(18px + env(safe-area-inset-top, 0px)) 18px 30px;
  background: linear-gradient(135deg, #ff8a50 0%, #ff6b35 55%, #f0481f 100%);
}

.sk-shop,
.sk-table {
  height: 26px;
  border-radius: var(--r-md);
  background: rgb(255 255 255 / 28%);
  animation: sk-pulse 1.3s ease-in-out infinite;
}

.sk-shop {
  width: 130px;
}

.sk-table {
  width: 60px;
  border-radius: var(--r-pill);
}

@keyframes sk-pulse {
  0%,
  100% {
    opacity: 0.55;
  }

  50% {
    opacity: 1;
  }
}

.sk-sheet {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  margin-top: -16px;
  border-radius: 18px 18px 0 0;
  background: #fff;
  overflow: hidden;
}

.sk-search {
  flex-shrink: 0;
  height: 36px;
  margin: 10px 12px;
  border-radius: var(--r-pill);
}

.sk-body {
  display: flex;
  flex: 1;
  min-height: 0;
}

.sk-cats {
  flex-shrink: 0;
  width: 90px;
  padding: 10px 10px 0;
  box-sizing: border-box;
  background: #fafafa;
}

.sk-cat {
  height: 18px;
  margin-bottom: 24px;
  border-radius: var(--r-sm);
}

.sk-dishes {
  flex: 1;
  min-width: 0;
  padding: 10px 12px 0;
  box-sizing: border-box;
}

.sk-dish {
  display: flex;
  gap: 10px;
  padding: 12px 0;
}

.sk-img {
  flex-shrink: 0;
  width: 80px;
  height: 80px;
  border-radius: var(--r-md);
}

.sk-main {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding-top: 4px;
}

.sk-line {
  height: 15px;
  border-radius: var(--r-sm);
}

.sk-line-short {
  width: 60%;
}

.sk-price {
  width: 70px;
  height: 18px;
  margin-top: auto;
  border-radius: var(--r-sm);
}

/* ---------- 加载 / 错误 ---------- */
.state-box {
  flex: 1;
  min-height: 55vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
}

.state-text {
  font-size: 14px;
  color: var(--ink-3);
}

.spin {
  width: 34px;
  height: 34px;
  border: 3px solid #ffe3d4;
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.err-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--danger-soft);
  color: var(--danger);
  font-size: 30px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.retry-btn {
  margin-top: 4px;
  padding: 9px 28px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 14px;
  cursor: pointer;
}

/* 菜单白色卡片:整体上移盖住 hero 底部,圆角与裁切统一在这里提供 */
.menu-sheet {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  margin-top: -16px;
  background: #fff;
  border-radius: 18px 18px 0 0;
  overflow: hidden;
  position: relative;
  z-index: 1;
}

/* 订单空态:还没下单时进「订单」页看到的是它(底部导航常驻后进得来) */
.order-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 24px 80px;
  color: var(--ink-4);
  font-size: 14px;
}

/* 本桌「已付款但出餐未完成」订单提示条:置顶于白色卡片内 */
.serving-banner {
  flex-shrink: 0;
  padding: 10px 16px;
  background: var(--brand-ghost);
  color: var(--ink-2);
  font-size: 12px;
  line-height: 1.7;
  border-bottom: 1px solid var(--line);
}

.serving-chip {
  display: inline-block;
  margin: 2px 6px 0 0;
  padding: 1px 8px;
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--brand-deep);
  font-size: 11px;
}
</style>
