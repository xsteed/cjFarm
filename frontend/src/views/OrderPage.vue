<template>
  <div class="order-page">
    <!-- 加载中 -->
    <div
      v-if="view === 'loading'"
      class="state-box"
    >
      <div class="spin"></div>
      <div class="state-text">正在进入点餐...</div>
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

      <MenuView
        v-if="view === 'menu'"
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
      />

      <OrderTrackView
        v-else-if="currentOrder && (view === 'order' || view === 'done' || view === 'canceled')"
        :view="view"
        :current-order="currentOrder"
        :status-text="statusText"
        :current-step="currentStep"
        :remark-items="remarkItems"
        :is-active="isActive"
        :can-append="canAppend"
        :urging="urging"
        :urge-cooldown="urgeCooldown"
        :show-tabbar="showTabbar"
        @append="startAppend"
        @urge="urge"
        @pay="view = 'pay'"
      />

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
      :ordered-count="orderedCount"
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
      :clear-cart="clearCart"
      :open-remark="openRemark"
      :change-cart-qty="changeCartQty"
      @submit="submit"
    />

    <!-- 菜品单项备注弹窗 -->
    <RemarkSheet
      v-if="remarkVisible"
      :dish-name="remarkTarget.item?.dishName || ''"
      :selected="remarkTarget.selected"
      :custom="remarkTarget.custom"
      :remarks="remarks"
      @toggle="toggleRemark"
      @update:custom="remarkTarget.custom = $event"
      @save="saveRemark"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { MessagePlugin } from 'tdesign-vue-next';
import {
  appendOrder,
  createOrder,
  createPay,
  getMenu,
  getOrderByNo,
  getPublicConfig,
  getRemarks,
  getTable
} from '../api';
import QRCode from 'qrcode';
import type { MenuCategory, OrderDetail, PublicConfig, RemarkOption, TableInfo } from '../types/entities';
import { isActiveStatus } from '../constants/orderStatus';
import { useCart } from '../composables/useCart';
import { useOrderPolling } from '../composables/useOrderPolling';
import type { ViewState } from '../composables/useOrderPolling';
import { useScrollSpy } from '../composables/useScrollSpy';
import { useUrge } from '../composables/useUrge';
import HeroHeader from './order/HeroHeader.vue';
import MenuView from './order/MenuView.vue';
import OrderTrackView from './order/OrderTrackView.vue';
import PayView from './order/PayView.vue';
import CartBar from './order/CartBar.vue';
import BottomTabBar from './order/BottomTabBar.vue';
import CartDrawer from './order/CartDrawer.vue';
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
const payTab = ref<'wx' | 'ali'>('wx');
const onlineQr = ref(''); // 在线支付二维码(dataURL)
const paying = ref(false);
const imgErrs = ref(new Set<number>());

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
  saveRemark
} = useCart(remarks);

const { urging, urgeCooldown, urge, clearUrgeTimer } = useUrge(currentOrder, tableId);
const { startPolling, stopPolling } = useOrderPolling({ view, currentOrder, onlineQr, clearUrgeTimer });
const { activeCat, setListRef, setGroupRef, jumpTo, handleScroll } = useScrollSpy(menu);

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
// 未选菜时不展示餐位费(合计为 0),选菜后才计入餐位费与优惠
const totalPrice = computed(() => (dishTotal.value > 0 ? round2(dishTotal.value + seatFee.value - discount.value) : 0));

const statusText = computed(() => ORDER_STATUS_TEXT[currentOrder.value?.orderStatus as number] || '处理中');
const currentStep = computed(() => {
  const s = Number(currentOrder.value?.orderStatus) || 1;
  return Math.max(1, Math.min(4, s));
});
const currentPayQr = computed(() => (payTab.value === 'wx' ? config.value.pay_qr_wx : config.value.pay_qr_ali));
// 该渠道是否已启用在线支付(未申请 key 前为 false,前端自动降级为码牌收款)
const onlineEnabled = computed(() =>
  payTab.value === 'wx' ? config.value.wxpay_enabled === '1' : config.value.alipay_enabled === '1'
);
const remarkItems = computed(() => (currentOrder.value?.items || []).filter(i => i.itemRemark));

// 是否已存在当前订单。底部导航与结算栏的定位都依赖它。
const hasOrder = computed(() => !!currentOrder.value);
// 底部 tabbar 显示条件:已有订单且停留在点餐/订单视图。
// 首次扫码尚未下单时不显示(只有一个 tab 没有意义);支付页需专注,也不显示。
const showTabbar = computed(() => hasOrder.value && (view.value === 'menu' || view.value === 'order'));
// 已下单份数(按 dishId 聚合,同菜多规格累加)。tabbar 角标与菜单「已点」标记共用同一份数据。
const orderedMap = computed<Record<number, number>>(() => {
  const m: Record<number, number> = {};
  for (const it of currentOrder.value?.items || []) {
    const id = it.dishId as number;
    m[id] = (m[id] || 0) + Number(it.quantity || 0);
  }
  return m;
});
const orderedCount = computed(() => Object.values(orderedMap.value).reduce((s, n) => s + n, 0));
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

function startAppend(): void {
  // 加菜期间保持轮询:后厨状态与已上齐菜品会持续刷新,菜单里的「已点」标记才不会过期
  cart.value = [];
  orderRemark.value = '';
  view.value = 'menu';
}

// 底部 tabbar 切页。加菜时随时切回订单页核对已下单菜品,购物车内容保留不丢。
function switchView(v: 'menu' | 'order'): void {
  if (view.value === v || !currentOrder.value) return;
  if (v === 'order') {
    view.value = 'order';
    startPolling();
  } else {
    view.value = 'menu';
  }
}

async function submit(): Promise<void> {
  if (!cart.value.length) {
    MessagePlugin.warning('请先选择菜品');
    return;
  }
  submitting.value = true;
  try {
    const items = cart.value.map(i => ({
      dishId: i.dishId,
      specId: i.specId,
      quantity: i.quantity,
      itemRemark: i.remark
    }));
    if (canAppend.value) {
      const res = await appendOrder({ tableId: tableId.value, orderNo: currentOrder.value?.orderNo, items });
      MessagePlugin.success('加菜成功，已通知后厨');
      currentOrder.value = await getOrderByNo(res.orderNo);
    } else {
      const res = await createOrder({
        tableId: tableId.value,
        personCount: personCount.value,
        orderRemark: orderRemark.value,
        items
      });
      MessagePlugin.success('下单成功，已通知后厨');
      currentOrder.value = await getOrderByNo(res.orderNo);
    }
    cart.value = [];
    cartVisible.value = false;
    view.value = 'order';
    startPolling();
  } catch {
    /* 失败已由拦截器统一提示,停留当前视图供重试 */
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
    shopName.value = cfg.shop_name || '扫码点餐';
    table.value = tb;
    tableId.value = tb.tableId ?? 0;
    menu.value = m;
    remarks.value = r;
    // 默认选中每道菜第一个规格
    m.forEach(c =>
      c.dishes?.forEach(d => {
        if (d.specs && d.specs.length) selectedSpec[d.dishId as number] = d.specs[0].specId as number;
      })
    );
    if (m.length) activeCat.value = m[0].categoryId ?? null;
    // 桌台已有进行中订单 → 直接进入订单状态
    if (tb && tb.currentOrder) {
      currentOrder.value = tb.currentOrder;
      view.value = 'order';
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
</style>
