<template>
  <t-drawer
    :visible="visible"
    placement="bottom"
    size="86%"
    :show-header="false"
    :footer="false"
    @update:visible="(v: boolean) => emit('update:visible', v)"
  >
    <div class="cart-sheet">
      <div class="sheet-head">
        <div class="sheet-title">
          已选菜品 <span v-if="totalCount">({{ totalCount }}件)</span>
        </div>
        <button
          v-if="cart.length"
          class="clear-btn"
          @click="clearCart"
        >
          清空
        </button>
      </div>

      <div class="cart-list">
        <div
          v-for="item in cart"
          :key="item.key"
          class="cart-item"
        >
          <div class="ci-info">
            <div class="ci-name">
              {{ item.dishName }}<em v-if="item.specName">（{{ item.specName }}）</em>
            </div>
            <div
              class="ci-remark"
              :class="{ add: !item.remark }"
              @click="openRemark(item)"
            >
              <svg
                v-if="item.remark"
                viewBox="0 0 24 24"
                width="12"
                height="12"
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
              <svg
                v-else
                viewBox="0 0 24 24"
                width="12"
                height="12"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="round"
              >
                <path d="M12 5v14M5 12h14" />
              </svg>
              <span>{{ item.remark || '添加备注' }}</span>
            </div>
          </div>
          <div class="ci-right">
            <div class="ci-price">¥{{ (item.price * item.quantity).toFixed(2) }}</div>
            <div class="qty-ctl">
              <button
                class="qty-btn minus"
                @click="changeCartQty(item, -1)"
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
                  <path d="M5 12h14" />
                </svg>
              </button>
              <span class="qty-num">{{ item.quantity }}</span>
              <button
                class="qty-btn plus"
                @click="changeCartQty(item, 1)"
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
              </button>
            </div>
          </div>
        </div>
        <div
          v-if="!cart.length"
          class="cart-empty"
        >
          购物车空空如也
        </div>
      </div>

      <template v-if="!canAppend">
        <div class="sheet-divider">用餐信息</div>
        <div class="dine-form">
          <div class="form-row">
            <span class="fr-label">用餐人数</span>
            <div class="qty-ctl">
              <button
                class="qty-btn minus"
                @click="personCount > 1 && emit('update:personCount', personCount - 1)"
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
                  <path d="M5 12h14" />
                </svg>
              </button>
              <span class="qty-num big">{{ personCount }}</span>
              <button
                class="qty-btn plus"
                @click="personCount < 20 && emit('update:personCount', personCount + 1)"
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
              </button>
            </div>
          </div>
          <div class="form-row col">
            <span class="fr-label">整单备注</span>
            <input
              v-model="remarkModel"
              class="remark-input"
              placeholder="如：不要辣、尽快上菜"
              maxlength="100"
            />
          </div>
        </div>

        <div class="fee-lines">
          <div class="fee-line">
            <span>菜品金额</span><span>¥{{ dishTotal.toFixed(2) }}</span>
          </div>
          <div
            v-if="seatFee > 0"
            class="fee-line"
          >
            <span>餐位费（{{ personCount }}人）</span><span>¥{{ seatFee.toFixed(2) }}</span>
          </div>
          <div
            v-if="discount > 0"
            class="fee-line off"
          >
            <span>满减优惠</span><span>-¥{{ discount.toFixed(2) }}</span>
          </div>
        </div>
      </template>
      <div
        v-else
        class="append-hint"
      >
        本次加菜将累加到订单 #{{ currentOrder?.orderNo }}，最终金额以订单为准
      </div>

      <div class="sheet-submit">
        <div class="ss-total">
          {{ canAppend ? '本次加菜' : '合计' }}
          <span class="money"><i>¥</i>{{ canAppend ? dishTotal.toFixed(2) : totalPrice }}</span>
        </div>
        <button
          class="submit-btn"
          :disabled="submitting || !cart.length"
          @click="emit('submit')"
        >
          <span
            v-if="submitting"
            class="spin small light"
          ></span>
          {{ canAppend ? '确认加菜' : '确认下单' }}
        </button>
      </div>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { OrderDetail } from '../../types/entities';
import type { CartItem } from '../../composables/useCart';

const props = defineProps<{
  visible: boolean;
  cart: CartItem[];
  totalCount: number;
  dishTotal: number;
  seatFee: number;
  discount: number;
  totalPrice: number;
  canAppend: boolean;
  personCount: number;
  orderRemark: string;
  submitting: boolean;
  currentOrder: OrderDetail | null;
  clearCart: () => void;
  openRemark: (item: CartItem) => void;
  changeCartQty: (item: CartItem, delta: number) => void;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  'update:personCount': [value: number];
  'update:orderRemark': [value: string];
  submit: [];
}>();

const remarkModel = computed({
  get: () => props.orderRemark,
  set: v => emit('update:orderRemark', v)
});
</script>

<style scoped>
/* ---------- 购物车抽屉 ---------- */
.cart-sheet {
  padding: 20px 18px calc(16px + env(safe-area-inset-bottom));
  display: flex;
  flex-direction: column;
}

.sheet-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.sheet-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
}

.sheet-title span {
  font-size: 12px;
  font-weight: 400;
  color: var(--ink-3);
  margin-left: 2px;
}

.clear-btn {
  border: none;
  background: none;
  color: var(--ink-3);
  font-size: 12.5px;
  cursor: pointer;
  padding: 4px;
}

.cart-list {
  max-height: 240px;
  overflow-y: auto;
  margin-top: 8px;
}

.cart-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 11px 0;
  border-bottom: 1px solid #f7f5f2;
}

.ci-info {
  min-width: 0;
}

.ci-name {
  font-size: 14.5px;
  font-weight: 500;
  color: var(--ink);
}

.ci-name em {
  font-style: normal;
  font-size: 12px;
  color: var(--ink-3);
}

.ci-remark {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--ink-3);
  margin-top: 4px;
  cursor: pointer;
}

.ci-remark svg {
  flex-shrink: 0;
}

.ci-remark.add {
  color: var(--brand);
}

.ci-right {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}

.ci-price {
  font-size: 14px;
  font-weight: 700;
  color: var(--brand-deep);
}

.qty-ctl {
  display: flex;
  align-items: center;
  gap: 10px;
}

.qty-btn {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  line-height: 1;
}

.qty-btn.minus {
  background: #fff;
  color: var(--brand);
  box-shadow: inset 0 0 0 1.5px #ffd5c4;
}

.qty-btn.plus {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
}

.qty-num {
  min-width: 18px;
  text-align: center;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}

.qty-num.big {
  font-size: 16px;
}

.cart-empty {
  padding: 40px 0;
  text-align: center;
  color: var(--ink-4);
  font-size: 13px;
}

.sheet-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 18px 0 14px;
  color: var(--ink-3);
  font-size: 12px;
  white-space: nowrap;
}

.sheet-divider::before,
.sheet-divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--line);
}

.dine-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.form-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.form-row.col {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.fr-label {
  font-size: 13.5px;
  color: var(--ink-2);
  flex-shrink: 0;
}

.remark-input {
  width: 100%;
  box-sizing: border-box;
  padding: 10px 14px;
  border: 1.5px solid var(--line);
  border-radius: var(--r-md);
  font-size: 13.5px;
  color: var(--ink);
  outline: none;
  background: #fafafa;
  transition: border-color 0.15s;
}

.remark-input:focus {
  border-color: var(--brand);
}

.fee-lines {
  margin-top: 16px;
  padding: 12px 14px;
  background: #fafafa;
  border-radius: var(--r-md);
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.fee-line {
  display: flex;
  justify-content: space-between;
  font-size: 12.5px;
  color: var(--ink-3);
}

.fee-line.off span:last-child {
  color: var(--brand-deep);
  font-weight: 600;
}

.append-hint {
  margin-top: 14px;
  padding: 10px 14px;
  border-radius: var(--r-md);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-size: 12px;
  line-height: 1.6;
}

.sheet-submit {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
}

.ss-total {
  font-size: 13px;
  color: var(--ink-3);
}

.ss-total .money {
  font-size: 22px;
  font-weight: 800;
  color: var(--ink);
  margin-left: 4px;
}

.ss-total .money i {
  font-style: normal;
  font-size: 14px;
}

.submit-btn {
  min-width: 148px;
  height: 46px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15.5px;
  font-weight: 600;
  cursor: pointer;
}

.submit-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

/* 提交按钮内的 loading 小转圈 */
.spin {
  width: 34px;
  height: 34px;
  border: 3px solid #ffe3d4;
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.spin.small {
  width: 14px;
  height: 14px;
  border-width: 2px;
  display: inline-block;
  vertical-align: -2px;
  margin-right: 6px;
}

.spin.light {
  border-color: rgb(255 255 255 / 40%);
  border-top-color: #fff;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
