<template>
  <t-dialog
    v-model:visible="settleVisible"
    :header="settleForm.mode === 'pay' ? '订单收款' : '订单结算'"
    width="480px"
    :confirm-btn="{ content: '确认', theme: 'primary', loading: settleSubmitting }"
    @confirm="submitSettle"
  >
    <div class="settle-box">
      <div class="st-row">
        <span class="st-label">应收金额</span>
        <span class="st-amount">¥{{ money(settleForm.total) }}</span>
      </div>

      <div
        v-if="settleForm.mode === 'settle'"
        class="st-field"
      >
        <div class="st-label">结算方式</div>
        <div class="st-types">
          <div
            v-for="t in settleTypes"
            :key="t.value"
            class="st-type"
            :class="{ on: settleForm.settleType === t.value }"
            @click="settleForm.settleType = t.value"
          >
            <div class="st-t">
              {{ t.label }}
            </div>
            <div class="st-d">
              {{ t.desc }}
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="settleForm.settleType === 'normal'"
        class="st-field"
      >
        <div class="st-label">支付方式</div>
        <t-select
          v-model="settleForm.payType"
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

      <div
        v-if="settleForm.settleType === 'free'"
        class="st-field"
      >
        <div class="st-label">免单原因 <span class="req">*</span></div>
        <t-input
          v-model="settleForm.remark"
          placeholder="如：老客户招待 / 菜品问题补偿"
          :maxlength="60"
        />
        <div class="st-hint">免单后实收 ¥0，不计入营业额，仅统计为让利金额。</div>
      </div>

      <div
        v-if="settleForm.settleType === 'credit'"
        class="st-field"
      >
        <div class="st-label">挂账人 / 备注 <span class="req">*</span></div>
        <t-input
          v-model="settleForm.remark"
          placeholder="如：王总（公司月结）"
          :maxlength="60"
        />
        <div class="st-hint">挂账后订单即可完成、桌台释放，欠款记入挂账，收款后再核销。</div>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { payOrder, settleOrder } from '../../api';

type SettleMode = 'pay' | 'settle';
type SettleType = 'normal' | 'free' | 'credit';

interface SettleForm {
  orderId: number;
  total: number;
  mode: SettleMode;
  settleType: SettleType;
  payType: string;
  remark: string;
}

interface SettleTarget {
  orderId?: number;
  totalAmount?: number;
}

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const settleVisible = ref(false);
const settleSubmitting = ref(false);
const settleForm = reactive<SettleForm>({
  orderId: 0,
  total: 0,
  mode: 'settle',
  settleType: 'normal',
  payType: '现金',
  remark: ''
});
const payTypes = ['现金', '微信', '支付宝', '银行卡', '其他'];
const settleTypes: { value: SettleType; label: string; desc: string }[] = [
  { value: 'normal', label: '正常收款', desc: '当场收款，计入营业额' },
  { value: 'free', label: '免单', desc: '商家让利，实收 0' },
  { value: 'credit', label: '挂账', desc: '先记账，后续核销' }
];

const money = (v: unknown): string => Number(v || 0).toFixed(2);

// mode=pay 仅标记收款；mode=settle 可选免单/挂账，并完成订单、释放桌台
function open(row: SettleTarget, mode: SettleMode): void {
  settleForm.orderId = Number(row.orderId);
  settleForm.total = Number(row.totalAmount);
  settleForm.mode = mode;
  settleForm.settleType = 'normal';
  settleForm.payType = '现金';
  settleForm.remark = '';
  settleVisible.value = true;
}

async function submitSettle(): Promise<void> {
  if (settleForm.settleType === 'free' && !settleForm.remark.trim()) {
    MessagePlugin.warning('免单必须填写原因');
    return;
  }
  if (settleForm.settleType === 'credit' && !settleForm.remark.trim()) {
    MessagePlugin.warning('挂账必须填写挂账单位/事由');
    return;
  }
  settleSubmitting.value = true;
  try {
    const id = settleForm.orderId;
    if (settleForm.mode === 'pay') {
      await payOrder({ orderId: id, payType: settleForm.payType });
      MessagePlugin.success('收款成功');
    } else if (settleForm.settleType === 'free') {
      await settleOrder({ orderId: id, settleType: 'free', settleRemark: settleForm.remark.trim() });
      MessagePlugin.success('免单成功，订单已完成');
    } else if (settleForm.settleType === 'credit') {
      await settleOrder({ orderId: id, settleType: 'credit', settleRemark: settleForm.remark.trim() });
      MessagePlugin.success('已记账挂账，订单已完成');
    } else {
      await settleOrder({ orderId: id, settleType: 'normal', payType: settleForm.payType });
      MessagePlugin.success('结账成功');
    }
    settleVisible.value = false;
    emit('changed');
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供核对重试 */
  } finally {
    settleSubmitting.value = false;
  }
}

defineExpose({ open });
</script>

<style scoped>
/* ---- 收款 / 结算 / 核销弹窗 ---- */
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

.req {
  color: #e34d59;
}

.st-types {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}

.st-type {
  border: 1.5px solid #e5e5e5;
  border-radius: 10px;
  padding: 10px;
  cursor: pointer;
  transition: all 0.15s;
}

.st-type:hover {
  border-color: #ffd5c4;
}

.st-type.on {
  border-color: #ff6b35;
  background: #fff7f2;
}

.st-type .st-t {
  font-size: 13px;
  font-weight: 700;
  color: #333;
}

.st-type.on .st-t {
  color: #f0481f;
}

.st-type .st-d {
  font-size: 11px;
  color: #999;
  margin-top: 4px;
  line-height: 1.4;
}

.st-hint {
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}

/* 手机上三张结算卡片各只剩 ~90px，描述文字会挤成一团。
   结算方式选错等于做错账，这里宁可改成竖排让人点准。 */
@media (width <= 767px) {
  .st-types {
    grid-template-columns: 1fr;
  }

  .st-type {
    display: flex;
    align-items: baseline;
    gap: 10px;
  }

  .st-type .st-d {
    margin-top: 0;
  }
}
</style>
