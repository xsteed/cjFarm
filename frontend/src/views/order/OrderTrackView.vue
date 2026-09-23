<template>
  <div
    class="track-body"
    :class="{ lifted: showTabbar }"
  >
    <div class="st-steps">
      <div class="st-title">
        <template v-if="view === 'done'">
          <svg
            class="st-title-svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M20 6L9 17l-5-5" />
          </svg>
          用餐愉快，订单已完成！
        </template>
        <template v-else-if="view === 'canceled'">
          <svg
            class="st-title-svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
          订单已取消
        </template>
        <template v-else>
          {{ statusText }}
          <svg
            class="st-title-svg"
            viewBox="0 0 24 24"
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
        </template>
      </div>

      <!-- 四步进度条(仅进行中显示) -->
      <div
        v-if="view === 'order'"
        class="st-line"
      >
        <div
          v-for="s in STEPS"
          :key="s.key"
          class="st-step"
          :class="{ done: currentStep > s.key, cur: currentStep === s.key }"
        >
          <div class="st-dot">
            <svg
              v-if="currentStep > s.key"
              viewBox="0 0 24 24"
              width="12"
              height="12"
              fill="none"
              stroke="currentColor"
              stroke-width="3"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M20 6L9 17l-5-5" />
            </svg>
            <span v-else>{{ s.key }}</span>
          </div>
          <div class="t">
            {{ s.label }}
          </div>
        </div>
      </div>
    </div>

    <div class="st-card">
      <div class="tt">
        菜品明细
        <span :title="'完整订单号 ' + (currentOrder.orderNo || '')"
          >订单号 #{{ currentOrder.shortNo || currentOrder.orderNo }}</span
        >
      </div>
      <div
        v-for="it in currentOrder.items || []"
        :key="it.itemId"
        class="st-item"
      >
        <span class="si-name"
          >{{ it.dishName }}<em v-if="it.specName">（{{ it.specName }}）</em> ×{{ it.quantity }}</span
        >
        <span class="si-amount">¥{{ Number(it.amount).toFixed(2) }}</span>
      </div>
      <div
        v-for="(it, idx) in remarkItems"
        :key="'rk' + idx"
        class="st-item remark"
      >
        <span class="si-remark">{{ it.dishName }}：{{ it.itemRemark }}</span>
      </div>
      <div
        v-if="Number(currentOrder.seatFee) > 0"
        class="st-item"
      >
        <span class="si-name sub">餐位费（{{ currentOrder.personCount }}人）</span>
        <span class="si-amount">¥{{ Number(currentOrder.seatFee).toFixed(2) }}</span>
      </div>
      <div
        v-if="Number(currentOrder.discountAmount) > 0"
        class="st-item discount"
      >
        <span class="si-name sub">满减优惠</span>
        <span class="si-amount">-¥{{ Number(currentOrder.discountAmount).toFixed(2) }}</span>
      </div>
      <div class="st-amt">
        <span>合计</span><span class="v">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</span>
      </div>

      <!-- 整单备注:顾客提交的整体要求(如不要辣),必须回显否则顾客无法确认是否已传达 -->
      <div
        v-if="currentOrder.orderRemark"
        class="st-remark-line"
      >
        <svg
          viewBox="0 0 24 24"
          width="13"
          height="13"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M16 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" />
          <path d="M16 3v6h6" />
          <path d="M8 13h8M8 17h5" />
        </svg>
        <span>整单备注：{{ currentOrder.orderRemark }}</span>
      </div>

      <div
        v-if="view === 'canceled' && currentOrder.cancelReason"
        class="cancel-reason"
      >
        取消原因：{{ currentOrder.cancelReason }}
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

      <div
        v-if="view === 'order' && currentOrder.payStatus === 0"
        class="st-paybtn"
        @click="emit('pay')"
      >
        去支付 ¥{{ Number(currentOrder.totalAmount).toFixed(2) }}
      </div>
      <div
        v-else-if="view === 'order' && currentOrder.payStatus === 1"
        class="paid-badge"
      >
        已支付，等待商家确认用餐结束
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { OrderDetail, OrderItem } from '../../types/entities';
import type { ViewState } from '../../composables/useOrderPolling';

defineProps<{
  view: ViewState;
  currentOrder: OrderDetail;
  statusText: string;
  currentStep: number;
  remarkItems: OrderItem[];
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
}>();

const STEPS = [
  { key: 1, label: '已下单' },
  { key: 2, label: '制作中' },
  { key: 3, label: '已上齐' },
  { key: 4, label: '已完成' }
];
</script>

<style scoped>
/* 订单状态 / 完成:同样上移压住 hero 底部,需要定位层否则卡片上沿被橙色横幅裁掉 */
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

.st-steps {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 18px 16px;

  /* 原为 -22px(想压住 hero 底边),但 track-body 是滚动容器,超出上边界的部分会被裁掉,
     结果只露出被切平的直角边。改成 -8px 正好抵掉容器 8px 上内边距,卡片贴住横幅且圆角完整 */
  margin-top: -8px;
  box-shadow: var(--shadow-1);
}

.st-title {
  font-size: 15px;
  font-weight: 700;
  text-align: center;
  margin-bottom: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
}

.st-title-svg {
  color: var(--brand);
  width: 16px;
  height: 16px;
}

.st-line {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  position: relative;
}

.st-line::before {
  content: '';
  position: absolute;
  top: 11px;
  left: 14px;
  right: 14px;
  height: 2px;
  background: var(--line);
}

.st-step {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  width: 56px;
}

.st-dot {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: #fff;
  border: 2px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--ink-4);
}

.st-step.done .st-dot {
  background: var(--success);
  border-color: var(--success);
  color: #fff;
}

.st-step.cur .st-dot {
  background: var(--brand);
  border-color: var(--brand);
  color: #fff;
  box-shadow: 0 0 0 4px var(--brand-soft);
}

.st-step .t {
  font-size: 10px;
  color: var(--ink-3);
  text-align: center;
}

.st-step.done .t,
.st-step.cur .t {
  color: var(--ink);
  font-weight: 600;
}

.st-card {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 16px;
  margin-top: 12px;
  box-shadow: var(--shadow-1);
}

.st-card .tt {
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 8px;
}

.st-card .tt span {
  font-weight: 400;
  color: var(--ink-4);
  font-size: 11px;
}

.st-item {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 10px;
  padding: 7px 0;
  border-bottom: 1px solid #f5f5f5;
  font-size: 12px;
}

.st-item:last-of-type {
  border-bottom: none;
}

.si-name {
  color: var(--ink);
}

.si-name em {
  font-style: normal;
  font-size: 11px;
  color: var(--ink-3);
}

.si-name.sub {
  color: var(--ink-3);
}

.si-amount {
  flex-shrink: 0;
  color: var(--ink);
}

.st-item.discount .si-amount {
  color: var(--brand-deep);
}

.st-item.remark .si-remark {
  font-size: 11px;
  color: var(--ink-3);
}

.st-amt {
  display: flex;
  justify-content: space-between;
  padding-top: 10px;
  font-size: 12.5px;
  font-weight: 700;
}

.st-amt .v {
  color: var(--brand-deep);
  font-size: 16px;
}

.st-paybtn {
  width: 100%;
  height: 44px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 14px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 12px;
  box-shadow: var(--shadow-3);
  cursor: pointer;
}

.paid-badge {
  display: flex;
  justify-content: center;
  margin-top: 12px;
  padding: 10px 22px;
  border-radius: var(--r-pill);
  background: var(--success-soft);
  color: var(--success);
  font-size: 13.5px;
  font-weight: 600;
}

/* 整单备注回显:顾客提交的整体要求(不要辣等),下单后必须能看到 */
.st-remark-line {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 10px;
  padding: 9px 12px;
  border-radius: var(--r-md);
  background: var(--brand-ghost);
  color: var(--ink-2);
  font-size: 12px;
  line-height: 1.6;
}

.st-remark-line svg {
  color: var(--brand);
  flex-shrink: 0;
  margin-top: 1px;
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

.cancel-reason {
  margin-top: 10px;
  padding: 8px 12px;
  border-radius: var(--r-md);
  background: #fff3f2;
  color: var(--danger);
  font-size: 12px;
}
</style>
