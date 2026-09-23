<template>
  <t-dialog
    v-model:visible="refundVisible"
    header="订单退款"
    width="480px"
    :confirm-btn="confirmBtn"
    @confirm="submitRefund"
  >
    <t-form
      v-if="refundForm"
      label-width="90px"
    >
      <t-form-item label="订单号">
        {{ refundForm.orderNo }}
      </t-form-item>
      <t-form-item label="可退金额">
        <span class="money">¥{{ money(refundForm.maxAmount) }}</span>
      </t-form-item>
      <t-form-item label="退款金额">
        <t-input-number
          v-model="refundForm.amount"
          :min="0.01"
          :max="refundForm.maxAmount"
          :step="0.01"
          :decimal-places="2"
          theme="normal"
        />
        <t-button
          size="small"
          variant="text"
          @click="refundForm.amount = refundForm.maxAmount"
        >
          全额
        </t-button>
      </t-form-item>
      <t-form-item label="退款原因">
        <t-input
          v-model="refundForm.reason"
          placeholder="如：顾客取消、菜品售罄"
        />
      </t-form-item>
    </t-form>
  </t-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { refundPay } from '../../api';

interface RefundForm {
  orderId: number;
  orderNo: string;
  amount: number;
  maxAmount: number;
  reason: string;
}

interface RefundTarget {
  orderId?: number;
  orderNo?: string;
  totalAmount?: number;
  refundAmount?: number;
}

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const refundVisible = ref(false);
const refundForm = ref<RefundForm | null>(null);
// 确认按钮必须用 reactive，否则 loading 变化不会触发视图更新
const confirmBtn = reactive<{ content: string; theme: 'danger'; loading: boolean }>({
  content: '确认退款',
  theme: 'danger',
  loading: false
});

const money = (v: unknown): string => Number(v || 0).toFixed(2);

function open(row: RefundTarget): void {
  const maxAmount = Number((Number(row.totalAmount || 0) - Number(row.refundAmount || 0)).toFixed(2));
  refundForm.value = {
    orderId: Number(row.orderId),
    orderNo: row.orderNo || '',
    // 默认全额退款；手动改小即为部分退款(组件有最小值，不能靠 0 表示全额)
    amount: maxAmount,
    maxAmount,
    reason: ''
  };
  refundVisible.value = true;
}

async function submitRefund(): Promise<void> {
  const f = refundForm.value;
  if (!f) return;
  // t-input-number 可被清空:未填或非正数不能提交(此前 amount || 0 会把「未填」当成退 0 元发出)。
  const amount = Number(f.amount);
  if (!Number.isFinite(amount) || amount <= 0) {
    MessagePlugin.warning('请输入大于 0 的退款金额');
    return;
  }
  if (amount > f.maxAmount) {
    MessagePlugin.warning(`退款金额不能超过可退金额 ${f.maxAmount.toFixed(2)} 元`);
    return;
  }
  confirmBtn.loading = true;
  try {
    const res = await refundPay({ orderId: f.orderId, amount, reason: f.reason });
    MessagePlugin.success(res?.msg || '退款已提交');
    refundVisible.value = false;
    emit('changed');
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供核对重试 */
  } finally {
    confirmBtn.loading = false;
  }
}

defineExpose({ open });
</script>
