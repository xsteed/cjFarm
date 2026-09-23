<template>
  <t-dialog
    v-model:visible="creditVisible"
    header="挂账核销"
    width="420px"
    :confirm-btn="{ content: '确认收款', theme: 'primary', loading: creditSubmitting }"
    @confirm="submitCredit"
  >
    <div class="settle-box">
      <div class="st-row">
        <span class="st-label">待收金额</span>
        <span class="st-amount">¥{{ money(creditForm.amount) }}</span>
      </div>
      <div class="st-field">
        <div class="st-label">收款方式</div>
        <t-select
          v-model="creditForm.payType"
          style="width: 100%"
        >
          <t-option
            v-for="p in payTypes"
            :key="p"
            :value="p"
            :label="p"
          />
        </t-select>
      </div>
      <div class="st-hint">核销后该笔金额计入营业额。</div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { settleCreditOrder } from '../../api';

interface CreditForm {
  orderId: number;
  amount: number;
  payType: string;
}

interface CreditTarget {
  orderId?: number;
  creditAmount?: number;
  totalAmount?: number;
}

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const creditVisible = ref(false);
const creditSubmitting = ref(false);
const creditForm = reactive<CreditForm>({ orderId: 0, amount: 0, payType: '现金' });
const payTypes = ['现金', '微信', '支付宝', '银行卡', '其他'];

const money = (v: unknown): string => Number(v || 0).toFixed(2);

function open(row: CreditTarget): void {
  creditForm.orderId = Number(row.orderId);
  creditForm.amount = Number(row.creditAmount || row.totalAmount);
  creditForm.payType = '现金';
  creditVisible.value = true;
}

async function submitCredit(): Promise<void> {
  creditSubmitting.value = true;
  try {
    const res = await settleCreditOrder({ orderId: creditForm.orderId, payType: creditForm.payType });
    MessagePlugin.success(res?.msg || '挂账已核销');
    creditVisible.value = false;
    emit('changed');
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留供重试 */
  } finally {
    creditSubmitting.value = false;
  }
}

defineExpose({ open });
</script>

<style scoped>
.settle-box {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.st-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 12px 14px;
  border-radius: 10px;
  background: #fff7f2;
}

.st-label {
  font-size: 13px;
  color: #666;
}

.st-amount {
  font-size: 22px;
  font-weight: 800;
  color: #f0481f;
}

.st-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.st-hint {
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}
</style>
