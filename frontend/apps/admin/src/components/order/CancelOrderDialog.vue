<!-- 取消订单弹窗（共享组件）
     订单管理列表卡片/桌面表格行 与 订单详情弹窗 的「取消订单」共用这一份实现。

     为什么不再用一句 DialogPlugin.confirm：
     取消是唯一「点下去就收不回」的订单动作（收款/完成都还能改单或补救），
     收银需要在下手前看清 —— 金额多少、桌台会不会放掉、会不会影响后厨，以及
     先付单等太久未收款时「取消其实才是正确操作」这层业务含义。
     提示块沿用「确认收款」弹窗（SettleDialog .st-hint）的视觉语言。 -->
<template>
  <t-dialog
    v-model:visible="cancelVisible"
    header="取消订单"
    width="480px"
    :confirm-btn="confirmBtn"
    :cancel-btn="{ content: '返回' }"
    @confirm="submitCancel"
  >
    <div class="cancel-box">
      <div class="ct-sum">
        <div class="ct-sum-main">
          <span class="ct-label">应收金额</span>
          <span class="ct-amount">¥{{ money(form.totalAmount) }}</span>
        </div>
        <div class="ct-sum-sub">订单号 {{ form.orderNo }}</div>
      </div>

      <!-- 等太久的先付新单：先付单在「确认收款」前不会出厨房单，
           等这么久多半已经线下处理或顾客已走 —— 取消不会浪费菜品，反而是正确收尾 -->
      <div
        v-if="form.waitedMinutes >= STALE_MIN"
        class="ct-hint ct-hint-stale"
      >
        <span class="ct-strong">已等待 {{ durationText(form.waitedMinutes) }} 未确认收款</span>
        <span>后厨还没有出这一单，顾客可能已离开或已线下处理，取消不会造成菜品浪费。</span>
      </div>
      <div
        v-else-if="form.waitedMinutes >= WARN_MIN"
        class="ct-hint ct-hint-wait"
      >
        <span class="ct-strong">已等待 {{ durationText(form.waitedMinutes) }} 未确认收款</span>
        <span>后厨还没有出这一单。如果顾客仍在等，请返回点「确认收款」；确认取消则这一单不再出菜。</span>
      </div>

      <div class="ct-hint ct-hint-warn">
        <span class="ct-strong">取消后不可恢复</span>
        <!-- 先付模式下「取消」可点就意味着还没收款，厨房单必然还没出（printKitchenOnPaid），
             不能套用后付那句「菜品无法撤回」，否则会吓住本该取消的单 -->
        <span v-if="kitchenNotPrinted"
          >订单不再计入营业额，桌台立即释放为空闲；这一单还没有出厨房单，取消不会产生菜品浪费。</span
        >
        <span v-else>订单不再计入营业额，桌台立即释放为空闲；后厨可能已经出单，菜品无法撤回，请先与后厨确认。</span>
      </div>

      <div class="ct-hint">
        取消原因记为「商家取消」，操作人与时间会写入操作日志；顾客若要重新下单，请重新扫码点餐。
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { cancelOrder } from '@dining/api';
import { durationText } from '@dining/shared';
import { NEW_ORDER_STALE_MIN, NEW_ORDER_WARN_MIN } from '../../utils/newOrderWait';
import { usePayFirst } from '../../composables/usePayFirst';

interface CancelTarget {
  orderId?: number;
  orderNo?: string;
  totalAmount?: number;
  payStatus?: number;
}

interface CancelForm {
  orderId: number;
  orderNo: string;
  totalAmount: number;
  payStatus: number;
  /** 该单已等待的分钟数（仅先付未收款新单会传，0 表示不提示等待时长） */
  waitedMinutes: number;
}

const emit = defineEmits<{
  (e: 'changed'): void;
}>();

const WARN_MIN = NEW_ORDER_WARN_MIN;
const STALE_MIN = NEW_ORDER_STALE_MIN;

// 先付模式开关(模块级单例,与订单管理/详情弹窗同源)
const payFirst = usePayFirst();

const cancelVisible = ref(false);
const form = reactive<CancelForm>({ orderId: 0, orderNo: '', totalAmount: 0, payStatus: 0, waitedMinutes: 0 });

// 厨房单还没出:先付模式下厨房单只在首次收款成功时补出(printKitchenOnPaid),
// 而未支付的单才允许取消 —— 所以「先付 + 未支付」必然没出厨房单,不存在菜已下锅的问题。
const kitchenNotPrinted = computed(() => payFirst.value && form.payStatus === 0);
// 确认按钮必须用 reactive，否则 loading 变化不会触发视图更新
const confirmBtn = reactive<{ content: string; theme: 'danger'; loading: boolean }>({
  content: '确认取消',
  theme: 'danger',
  loading: false
});

const money = (v: unknown): string => Number(v || 0).toFixed(2);

function open(row: CancelTarget, waitedMinutes = 0): void {
  form.orderId = Number(row.orderId) || 0;
  form.orderNo = row.orderNo || '';
  form.totalAmount = Number(row.totalAmount) || 0;
  form.payStatus = Number(row.payStatus) || 0;
  form.waitedMinutes = Math.max(0, Number(waitedMinutes) || 0);
  cancelVisible.value = true;
}

async function submitCancel(): Promise<void> {
  confirmBtn.loading = true;
  try {
    await cancelOrder({ orderId: form.orderId, cancelReason: '商家取消' });
    MessagePlugin.success('订单已取消');
    cancelVisible.value = false;
    emit('changed');
  } catch {
    /* 失败已由拦截器统一 toast，弹窗保留供复核 */
  } finally {
    confirmBtn.loading = false;
  }
}

defineExpose({ open });
</script>

<style scoped>
/* ---- 取消订单弹窗 ---- */
.cancel-box {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ct-sum {
  padding: 12px 14px;
  border-radius: 10px;
  background: #fff7f2;
}

.ct-sum-main {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.ct-label {
  font-size: 13px;
  color: #666;
}

.ct-amount {
  font-size: 22px;
  font-weight: 800;
  color: #f0481f;
}

/* 订单号弱化一级：取消前核对的是金额与桌台，单号只用来对账 */
.ct-sum-sub {
  margin-top: 4px;
  font-size: 12px;
  color: #999;
  word-break: break-all;
}

.ct-hint {
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}

/* 说明块统一样式：加粗小标题单独成行，正文跟在下一行，手机上不长成一大坨 */
.ct-hint .ct-strong {
  display: block;
  font-weight: 700;
}

/* 取消后果：与「确认收款」弹窗的出纸提示同款主色浅底牌 —— 都是「点下去会发生什么」的关键信息，
   收银在确认前必须看到（与 SettleDialog .st-hint-print 同一视觉语言） */
.ct-hint-warn {
  padding: 8px 12px;
  border: 1px solid #ffd5c4;
  border-radius: 8px;
  background: var(--brand-ghost, #fff8f5);
  color: var(--brand-deep, #f0481f);
}

/* 已超时未收款：红底牌说明「为什么这时候取消是对的」，语气比主色提示更紧迫 */
.ct-hint-stale {
  padding: 8px 12px;
  border: 1px solid #ffd0d0;
  border-radius: 8px;
  background: var(--danger-soft, #feeeee);
  color: var(--danger, #f53f3f);
}

/* 刚过预警线：仍是主色，语气是「提醒」而非「催你取消」 */
.ct-hint-wait {
  padding: 8px 12px;
  border: 1px solid #ffd5c4;
  border-radius: 8px;
  background: var(--warning-soft, #fff6e5);
  color: #a35b00;
}
</style>
