<template>
  <div
    class="track-body"
    :class="{ lifted: showTabbar }"
  >
    <!-- 菜品明细卡:顾客最关心「点了什么、多少钱」,所以放第一张。
         订单状态收进卡头,单号 / 人数 / 时间挪到卡后的「订单信息」卡,顶部不再堆概要。 -->
    <div class="st-card st-card-first">
      <div class="st-card-head">
        <div class="tt">菜品明细</div>
        <div
          class="st-tag"
          :class="'st-tag--' + statusTheme"
        >
          {{ statusLabel }}
        </div>
      </div>

      <div
        v-for="it in renderItems"
        :key="it.itemId"
        class="st-item"
      >
        <div class="si-main">
          <span class="si-name"
            ><b
              v-if="it.batchLabel"
              class="si-batch"
              >{{ it.batchLabel }}</b
            ><b
              v-if="it.isGift"
              class="si-gift"
              >赠 </b
            >{{ it.dishName
            }}<em
              v-if="it.specName"
              class="si-spec"
              >{{ it.specName }}</em
            ></span
          >
          <span
            v-if="it.itemRemark"
            class="si-remark"
            >{{ it.itemRemark }}</span
          >
        </div>
        <span class="si-qty">×{{ it.quantity }}</span>
        <span
          class="si-amount"
          :class="{ 'si-gift': it.isGift }"
          >{{ it.isGift ? '赠' : '¥' + Number(it.amount).toFixed(2) }}</span
        >
      </div>

      <!-- 整单备注:顾客提交的整体要求(如不要辣),位置与小程序一致(明细之后、账单之前) -->
      <div
        v-if="currentOrder.orderRemark"
        class="st-order-remark"
      >
        订单备注：{{ currentOrder.orderRemark }}
      </div>

      <!-- 账单明细:菜品小计 + 加价项,与小程序一致摊开。
           末行给出「合计 + 支付状态」:合计只在这里出现一次,与上面几行前因后果对得上。 -->
      <div class="st-bill">
        <div class="st-bill-row">
          <span>菜品小计</span>
          <span>¥{{ Number(currentOrder.dishAmount || 0).toFixed(2) }}</span>
        </div>
        <div
          v-if="Number(currentOrder.seatFee) > 0"
          class="st-bill-row"
        >
          <span>茶位费（{{ currentOrder.personCount }}人）</span>
          <span>¥{{ Number(currentOrder.seatFee).toFixed(2) }}</span>
        </div>
        <div
          v-if="Number(currentOrder.discountAmount) > 0"
          class="st-bill-row st-discount"
        >
          <span>满减优惠</span>
          <span>-¥{{ Number(currentOrder.discountAmount).toFixed(2) }}</span>
        </div>
        <div class="st-bill-row st-bill-total">
          <span>合计</span>
          <span class="st-bill-total-right">
            <span
              class="st-tag"
              :class="'st-tag--' + payTheme"
              >{{ payText }}</span
            >
            <span class="st-bill-total-value">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</span>
          </span>
        </div>
      </div>

      <!-- 进行中的订单:可加菜、可催菜 -->
      <div
        v-if="view === 'order' && isActive"
        class="st-actions"
      >
        <div
          v-if="canAppend"
          class="st-append"
          @click="emit('append')"
        >
          <svg
            viewBox="0 0 24 24"
            width="14"
            height="14"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
            stroke-linecap="round"
          >
            <path d="M12 5v14M5 12h14" />
          </svg>
          加菜
        </div>
        <div
          class="st-urge"
          :class="{ disabled: urgeCooldown > 0 || urging }"
          @click="emit('urge')"
        >
          <svg
            viewBox="0 0 24 24"
            width="14"
            height="14"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <circle
              cx="12"
              cy="12"
              r="9"
            />
            <path d="M12 7v5l3 2" />
          </svg>
          {{ urgeCooldown > 0 ? urgeCooldown + 's 后可再催' : urging ? '催菜中…' : '催菜' }}
        </div>
      </div>

      <!-- 按钮只给动作，金额由上方账单末行的「合计」承担，避免同屏出现两次 -->
      <div
        v-if="view === 'order' && currentOrder.payStatus === 0"
        class="st-paybtn"
        @click="emit('pay')"
      >
        去支付
      </div>
      <div
        v-if="view === 'settled' || view === 'done' || view === 'canceled'"
        class="st-neworder"
        @click="emit('newOrder')"
      >
        {{ view === 'canceled' ? '重新点菜' : '再点一单' }}
      </div>
    </div>

    <!-- 状态提示:已完成 / 已取消 / 已结账,与小程序 sec 卡一致(白色卡片 + 内嵌彩色提示块) -->
    <div
      v-if="done || canceled || settled"
      class="st-card"
    >
      <div
        class="st-tip"
        :class="{ 'st-tip--cancel': canceled }"
      >
        <template v-if="canceled">{{ cancelTip }}</template>
        <template v-else-if="done">🎉 这顿吃完啦，欢迎下次光临</template>
        <template v-else-if="settled">已结账，后厨正在做啦～出餐进度会自动更新</template>
      </div>
    </div>

    <!-- 订单信息:单号 / 人数 / 时间 / 支付凭证,顾客核对单据时才看,放菜品之后不再挤在首屏 -->
    <div class="st-info">
      <div class="st-info-no">单号 {{ currentOrder.shortNo || currentOrder.orderNo }}</div>
      <div class="st-info-meta">
        <span>{{ currentOrder.personCount }} 人用餐</span>
        <span v-if="currentOrder.createTime">{{ currentOrder.createTime }}</span>
      </div>
      <!-- 支付凭证行:结账后留一条可核对的记录(渠道 + 时间),不再只有一个「已结账」标签 -->
      <div
        v-if="Number(currentOrder.payStatus) === 1"
        class="st-info-paid"
      >
        <span class="st-info-paid-dot"></span>{{ payText }}<template v-if="paidAtText"> · {{ paidAtText }}</template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { OrderDetail, OrderItem } from '@dining/types';
import type { ViewState } from '@dining/customer-core';
import { payStatusText, payStatusTheme, batchLabels } from '@dining/shared/orderDisplay';

const props = defineProps<{
  view: ViewState;
  currentOrder: OrderDetail;
  isActive: boolean;
  canAppend: boolean;
  urging: boolean;
  urgeCooldown: number;
  showTabbar: boolean;
}>();

const emit = defineEmits<{
  append: [];
  urge: [];
  pay: [];
  newOrder: [];
}>();

// 订单状态标签:短文案 + 主题,与小程序 ORDER_STATUS 字典一致(挂在菜品明细卡头右侧)
const STATUS_LABEL: Record<number, string> = { 1: '已下单', 2: '制作中', 3: '已上齐', 4: '已完成', 5: '已取消' };
const STATUS_THEME: Record<number, string> = { 1: 'primary', 2: 'warning', 3: 'success', 4: 'default', 5: 'danger' };
const orderStatus = computed(() => Number(props.currentOrder.orderStatus) || 1);
const statusLabel = computed(() => STATUS_LABEL[orderStatus.value] || '处理中');
const statusTheme = computed(() => STATUS_THEME[orderStatus.value] || 'primary');

// 结束态:与小程序 order 页的 settled / done / canceled 三态一致
const done = computed(() => props.view === 'done');
const canceled = computed(() => props.view === 'canceled');
const settled = computed(() => props.view === 'settled');

// 支付状态文案/主题:复用共享实现,与小程序同源
const payText = computed(() => payStatusText(props.currentOrder));
const payTheme = computed(() => payStatusTheme(props.currentOrder));

// 付款时间:后端是「2026-10-09 12:03:07」这类字符串,截到分钟即可,不当时间解析用
const paidAtText = computed(() => {
  const t = props.currentOrder.payTime;
  return t ? String(t).replace('T', ' ').slice(0, 16) : '';
});

// 取消提示:后端已对取消原因做可展示性收口(通用标签「商家取消」会置空),
// 有真实原因就带上,没有就给标准说明。取消只可能发生在未支付订单上(后端强制 pay_status=0),
// 所以能明确告诉顾客「不产生费用」,不必让顾客去猜钱到底扣没扣。
const cancelTip = computed(() =>
  props.currentOrder.cancelReason
    ? `订单已取消：${props.currentOrder.cancelReason}`
    : '订单已取消，本单未支付、不会产生任何费用'
);

// 菜品明细行:与小程序一致,给「加过菜」的批次打行内小标签(首单 / 加菜N);单批次不标。
// 编号口径收敛在 @dining/shared 的 batchLabels(与管理端订单详情共用)。
type RenderItem = OrderItem & { batchLabel: string };

const renderItems = computed<RenderItem[]>(() => {
  const list = props.currentOrder.items || [];
  const labels = batchLabels(list);
  return list.map((it, i) => ({ ...it, batchLabel: labels[i] }));
});
</script>

<style scoped>
/* 订单视图滚动区:整页是一张张白卡(菜品明细 → 状态提示 → 订单信息),
   与菜单页「白卡上移压住 hero」不同,这里首张卡与横幅之间留出边距 */
.track-body {
  flex: 1;
  padding: 8px 14px 24px;
  overflow-y: auto;
  position: relative;
  z-index: 1;
}

/* 底部 tabbar 会遮住卡片底部,加菜/催菜按钮需要额外让位 */
.track-body.lifted {
  padding-bottom: 86px;
}

/* 订单信息卡:单号 / 人数 / 时间。放菜品之后,顾客核对单据时才看(桌台由 HeroHeader 展示)。
   内边距与 .st-card 一致,几张卡的留白口径统一 */
.st-info {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 16px;
  margin-top: 12px;
  box-shadow: var(--shadow-1);
}

.st-info-no {
  color: var(--ink-3);
  font-size: 12px;
  letter-spacing: 0.5px;
}

.st-info-meta {
  display: flex;
  gap: 16px;
  margin-top: 6px;
  color: var(--ink-3);
  font-size: 12px;
}

/* 支付凭证:绿点 + 「已支付（微信）· 2026-10-09 12:03」,可核对可回看 */
.st-info-paid {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  color: var(--success);
  font-size: 12px;
}

.st-info-paid-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--success);
}

/* 状态 / 支付标签:对齐小程序 app.wxss 的 .tag 体系 */
.st-tag {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 10px;
  border-radius: var(--r-pill);
  font-size: 11px;
  line-height: 1;
}

.st-tag--primary {
  background: var(--brand-soft);
  color: var(--brand-deep);
}

.st-tag--success {
  background: var(--success-soft);
  color: var(--success);
}

.st-tag--warning {
  background: var(--warning-soft);
  color: #b7791f;
}

.st-tag--danger {
  background: var(--danger-soft);
  color: var(--danger);
}

.st-tag--default {
  background: #f2f2f2;
  color: var(--ink-3);
}

/* 状态提示块:已完成 / 已取消 / 已结账,内嵌于白色 sec 卡,对齐小程序 */
.st-tip {
  padding: 10px 12px;
  border-radius: var(--r-md);
  background: var(--brand-ghost);
  color: var(--ink-2);
  font-size: 12.5px;
  line-height: 1.6;
}

.st-tip--cancel {
  background: var(--danger-soft);
  color: var(--danger);
}

.st-card {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 16px;
  margin-top: 12px;
  box-shadow: var(--shadow-1);
}

/* 首张卡(菜品明细):不再上移压住 hero,与横幅之间留出呼吸感。
   8px(track-body 上内边距) + 6px = 14px,与左右 14px 的页边距对齐 */
.st-card-first {
  margin-top: 6px;
}

/* 卡头:标题 + 右侧状态标签(订单状态挂在最显眼的菜品卡上,不另开概要卡) */
.st-card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.st-card .tt {
  font-size: 14px;
  font-weight: 700;
  margin-bottom: 8px;
}

.st-card-head .tt {
  margin-bottom: 0;
}

/* 加菜批次行内标签(首单 / 加菜N):与小程序 row-batch 一致 */
.si-batch {
  display: inline-block;
  margin-right: 4px;
  padding: 0 4px;
  border-radius: var(--r-sm);
  background: #f2f2f2;
  color: var(--ink-3);
  font-size: 10px;
  font-weight: 400;
  line-height: 1.7;
}

/* 菜品行:名称(含备注) | 数量 | 金额 三栏,与小程序一致(每行上边框,首行也有分隔线)。
   字号对齐全站菜品口径(菜单 .op-name 14px / 购物车 14.5px),不再比按钮小一截 */
.st-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 0;
  border-top: 1px solid #f5f5f5;
  font-size: 13.5px;
}

.si-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.si-name {
  color: var(--ink);
  font-weight: 600;
}

.si-spec {
  font-style: normal;
  margin-left: 6px;
  padding: 1px 6px;
  border-radius: var(--r-sm);
  background: #f4f4f4;
  color: var(--ink-3);
  font-size: 11.5px;
  font-weight: 400;
}

.si-remark {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.5;
}

.si-qty {
  flex-shrink: 0;
  color: var(--ink-3);
  font-size: 12.5px;
}

.si-amount {
  flex-shrink: 0;
  min-width: 60px;
  text-align: right;
  color: var(--ink);
  font-size: 13.5px;
  font-weight: 600;
}

.si-gift {
  color: #e8850c;
  font-weight: 700;
}

/* 账单明细:菜品小计 / 茶位费 / 满减,与小程序一致的摊开式呈现 */
.st-bill {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #f5f5f5;
}

.st-bill-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 5px 0;
  color: var(--ink-3);
  font-size: 12.5px;
}

.st-bill-row.st-discount {
  color: var(--brand-deep);
}

/* 合计行:账单收尾,金额与「未支付 / 已支付」标签同排。
   虚线上下的留白对齐(上一行 5px 下内边距 + 7px 外边距 = 12px,下方 12px) */
.st-bill-total {
  margin-top: 7px;
  padding: 12px 0 0;
  border-top: 1px dashed var(--line);
  color: var(--ink);
  font-size: 13.5px;
  font-weight: 600;
}

/* 窄屏兜底:标签文案较长(如「挂账待收 ¥12.00」)时也不把金额挤变形 */
.st-bill-total-right {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
}

.st-bill-total-value {
  color: var(--brand-deep);
  font-size: 17px;
  font-weight: 700;
}

/* 支付按钮:与卡内「加菜 / 催菜」同为 42px,去掉重投影 ——
   主次靠底色(实心品牌渐变 vs 描边)区分,不再靠体积压住菜品明细 */
.st-paybtn {
  width: 100%;
  height: 42px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 14.5px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 12px;
  cursor: pointer;
}

/* 顾客侧已结束(已结账/完成/取消)后的「再点一单」出口:与去支付同尺寸同级 */
.st-neworder {
  width: 100%;
  height: 42px;
  border-radius: var(--r-pill);
  border: 1.5px solid #ffd5c4;
  background: #fff;
  color: var(--brand-deep);
  font-size: 14.5px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 12px;
  cursor: pointer;
}

.st-neworder:active {
  background: var(--brand-soft);
}

/* 整单备注回显:顾客提交的整体要求(不要辣等),下单后必须能看到。
   样式与小程序「订单备注」一致:虚线上方纯文本,不再用图标高亮块 */
.st-order-remark {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #f5f5f5;
  color: var(--ink-2);
  font-size: 12.5px;
  line-height: 1.6;
}

/* 订单卡片操作区:加菜 / 催菜并排 */
.st-actions {
  display: flex;
  gap: 10px;
  margin-top: 12px;
}

.st-append,
.st-urge {
  flex: 1;
  height: 42px;
  border-radius: var(--r-pill);
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  cursor: pointer;
}

.st-append {
  border: 1.5px solid #ffd5c4;
  background: #fff;
  color: var(--brand-deep);
}

.st-append:active {
  background: var(--brand-soft);
}

.st-urge {
  border: 1.5px solid #ffd5c4;
  background: var(--brand-soft);
  color: var(--brand-deep);
}

.st-urge:active {
  background: #ffe3d4;
}

.st-urge.disabled {
  border-color: var(--line);
  background: #f5f5f5;
  color: var(--ink-4);
  cursor: not-allowed;
}

.st-urge.disabled:active {
  background: #f5f5f5;
}
</style>
