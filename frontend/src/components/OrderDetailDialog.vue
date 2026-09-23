<!-- 订单详情弹窗（共享组件）
     桌台管理页「点击桌台」与订单管理页「详情」共用同一份实现，
     避免两处各写一套详情/收款/退款逻辑后长期跑偏。

     本文件只做「薄壳」：数据加载与订单操作集中在此，展示区抽到
     components/order/OrderInfoSection.vue，收款/挂账核销/退款三个子弹窗
     各自成组件，按钮权限矩阵统一收在 composables/useOrderActions.ts。 -->
<template>
  <t-dialog
    :visible="visible"
    :header="dialogTitle"
    width="880px"
    :footer="false"
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @closed="onClosed"
  >
    <OrderInfoSection
      :detail="detail"
      :refunds="refunds"
      @handle-urge="onHandleUrge"
      @set-status="setStatus"
      @edit="openEdit"
      @settle="onSectionSettle"
      @reprint="onReprint"
      @print-a4="onPrintA4"
      @cancel="onCancel"
      @finish="onFinish"
      @credit-settle="onSectionCredit"
      @cancel-settle="onSettleCancel"
      @refund="onSectionRefund"
      @refresh-refunds="loadRefunds"
      @sync-refund="syncRefund"
    />
  </t-dialog>

  <!-- 手动改单 -->
  <OrderEditDialog
    v-model:visible="editVisible"
    :order="detail"
    @saved="refresh"
  />

  <!-- 收款 / 结账 -->
  <SettleDialog
    ref="settleDialogRef"
    @changed="refresh"
  />

  <!-- 挂账核销 -->
  <CreditSettleDialog
    ref="creditDialogRef"
    @changed="refresh"
  />

  <!-- 退款 -->
  <RefundDialog
    ref="refundDialogRef"
    @changed="refresh"
  />
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import {
  getOrder,
  changeOrderStatus,
  cancelSettle,
  finishOrder,
  cancelOrder,
  handleUrge,
  reprintOrder,
  getPublicConfig,
  listRefunds,
  queryRefund
} from '../api';
import { printOrderA4 } from '../utils/printA4';
import type { OrderDetail, OrderSummary, Refund } from '../types/entities';
import OrderInfoSection from './order/OrderInfoSection.vue';
import OrderEditDialog from './OrderEditDialog.vue';
import SettleDialog from './order/SettleDialog.vue';
import CreditSettleDialog from './order/CreditSettleDialog.vue';
import RefundDialog from './order/RefundDialog.vue';

type SettleMode = 'pay' | 'settle';
type DocType = 'kitchen' | 'guest';

const props = withDefaults(
  defineProps<{
    visible: boolean;
    orderId: number | null;
  }>(),
  {
    visible: false,
    orderId: 0
  }
);

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
  (e: 'changed'): void;
}>();

const detail = ref<OrderDetail | null>(null);
const refunds = ref<Refund[]>([]);
const editVisible = ref(false);

const settleDialogRef = ref<InstanceType<typeof SettleDialog> | null>(null);
const creditDialogRef = ref<InstanceType<typeof CreditSettleDialog> | null>(null);
const refundDialogRef = ref<InstanceType<typeof RefundDialog> | null>(null);

const dialogTitle = computed(() => (detail.value?.tableNo ? `${detail.value.tableNo}号桌 · 订单详情` : '订单详情'));

// 请求序号:请求在途时弹窗被关闭、或被切换到另一单,旧响应不得写回已清空/新订单的状态。
let reqSeq = 0;

async function loadRefunds(orderId: number, seq: number): Promise<void> {
  try {
    const rf = await listRefunds(orderId);
    if (seq === reqSeq) refunds.value = rf;
  } catch {
    if (seq === reqSeq) refunds.value = [];
  }
}

/** 拉取详情与退款记录(带序号守卫)。
 *  failClose=true(首次打开):详情拉取失败时关闭弹窗,避免留下一个空壳;
 *  false(refresh):失败时保留旧数据,错误提示由拦截器统一弹出。 */
async function fetchDetail(id: number, failClose: boolean): Promise<void> {
  const seq = ++reqSeq;
  try {
    const d = await getOrder(id);
    if (seq !== reqSeq || !props.visible) return;
    detail.value = d;
  } catch {
    if (failClose && seq === reqSeq && props.visible) {
      emit('update:visible', false);
    }
    return;
  }
  await loadRefunds(id, seq);
}

/** 重新拉取详情与退款记录，并通知父级刷新列表。
 *  传 orderId 可强制刷新指定订单（列表页快捷操作时，详情显示的未必就是被操作的那一单）。 */
async function refresh(orderId?: number): Promise<void> {
  const id = orderId || detail.value?.orderId;
  if (id) {
    await fetchDetail(id, false);
  }
  emit('changed');
}

/** 打开弹窗/切换订单：父级只需给出 orderId，详情与退款记录由本组件拉取。 */
async function open(orderId?: number | null): Promise<void> {
  const id = orderId || props.orderId;
  if (!id) return;
  await fetchDetail(id, true);
}

// 打开弹窗、或弹窗开着时切换订单(如桌台页连续点不同桌台)都要重新拉取。
// 同时侦听 [visible, orderId]:父级在同一同步块里赋两个 prop(打开弹窗的标准写法)时
// 只触发一次拉取 —— 此前的双 watch 会让每次打开都把详情+退款各拉两遍。
watch([() => props.visible, () => props.orderId], ([visible, id]) => {
  if (visible && id) void open(id);
});

function onClosed(): void {
  detail.value = null;
  refunds.value = [];
}

async function setStatus(status: number): Promise<void> {
  if (!detail.value) return;
  try {
    await changeOrderStatus({ orderId: detail.value.orderId, orderStatus: status });
    MessagePlugin.success('操作成功');
    await refresh();
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

// onHandleUrge 消解该订单全部待处理催菜(已加急/已解释)，角标随之消失
async function onHandleUrge(): Promise<void> {
  if (!detail.value) return;
  try {
    await handleUrge({ orderId: detail.value.orderId });
    MessagePlugin.success('催菜已标记为已处理');
    await refresh();
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

// ---- 手动改单 ----
function openEdit(): void {
  editVisible.value = true;
}

// ---- 收款 / 结算 / 挂账核销 / 退款：转发给子弹窗，保证每种操作全局只有一份实现 ----
function openSettle(row: OrderSummary | null, mode: SettleMode): void {
  if (!row) return;
  settleDialogRef.value?.open(row, mode);
}

function openCredit(row: OrderSummary | null): void {
  if (!row) return;
  creditDialogRef.value?.open(row);
}

function openRefund(row: OrderSummary | null): void {
  if (!row) return;
  refundDialogRef.value?.open(row);
}

function onSectionSettle(mode: SettleMode): void {
  openSettle(detail.value, mode);
}

function onSectionCredit(): void {
  openCredit(detail.value);
}

function onSectionRefund(): void {
  openRefund(detail.value);
}

// ---- 撤销免单 / 挂账 ----
function onSettleCancel(): void {
  const row = detail.value;
  if (!row) return;
  const name = row.settleType === 'free' ? '免单' : '挂账';
  const dlg = DialogPlugin.confirm({
    header: '撤销结算',
    body: `确认撤销订单【${row.orderNo}】的${name}？撤销后订单恢复为未支付并回到「已上齐」，需重新结算。`,
    onConfirm: async () => {
      try {
        await cancelSettle({ orderId: row.orderId, reason: '撤销' + name });
        MessagePlugin.success('已撤销结算');
        dlg.hide();
        await refresh();
      } catch {
        dlg.hide();
      }
    }
  });
}

function onFinish(): void {
  const row = detail.value;
  if (!row) return;
  const dlg = DialogPlugin.confirm({
    header: '确认完成',
    body: '确认后订单将归档并计入今日统计，桌台释放为空闲。',
    onConfirm: async () => {
      try {
        await finishOrder({ orderId: row.orderId });
        MessagePlugin.success('订单已完成');
        dlg.hide();
        await refresh();
      } catch {
        dlg.hide();
      }
    }
  });
}

function onCancel(): void {
  const row = detail.value;
  if (!row) return;
  const dlg = DialogPlugin.confirm({
    header: '确认取消',
    body: `确认取消订单【${row.orderNo}】？`,
    onConfirm: async () => {
      try {
        await cancelOrder({ orderId: row.orderId, cancelReason: '商家取消' });
        MessagePlugin.success('订单已取消');
        dlg.hide();
        await refresh();
      } catch {
        dlg.hide();
      }
    }
  });
}

async function syncRefund(row: Refund): Promise<void> {
  try {
    const res = await queryRefund({ refundId: row.refundId });
    MessagePlugin.success(res?.msg || '已同步');
    await refresh();
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

// onReprint 人工补打：纸没装好、打歪了、打印机离线导致没出纸时用。
// 不传 printerId，由后端自动挑一台该单据类型的启用打印机。
async function onReprint(docType: DocType): Promise<void> {
  if (!detail.value) return;
  const label = docType === 'kitchen' ? '厨房单' : '食客小票';
  try {
    const res = await reprintOrder({ orderId: detail.value.orderId, docType });
    MessagePlugin.success(res?.msg || `${label}补打指令已发送`);
  } catch {
    /* 失败原因(如「没有启用中的厨房单打印机」)由请求拦截器统一提示 */
  }
}

// ** A4 通道 **：不走打印机，生成浏览器打印页，服务员手动出纸或另存 PDF。
// 与小票机互不干扰，所以即使小票机离线也能出纸质单据。
async function onPrintA4(docType: DocType): Promise<void> {
  if (!detail.value) return;
  const ok = await printOrderA4(detail.value, docType, { getPublicConfig });
  if (!ok) MessagePlugin.warning('打印页被浏览器拦截，请允许本站点弹出窗口后重试');
}

// 供「订单管理」列表行的快捷按钮调用：每种操作全局只有一份实现
defineExpose({ open, refresh, openSettle, openRefund, openCredit, openEdit });
</script>
