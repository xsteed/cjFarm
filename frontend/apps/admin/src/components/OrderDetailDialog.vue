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
      :busy="acting"
      @handle-urge="onHandleUrge"
      @set-status="setStatus"
      @edit="openEdit"
      @settle="onSectionSettle"
      @reprint="onReprint"
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

  <!-- 取消订单:与订单管理页卡片/表格行的取消共用同一个弹窗(含后果与超时说明) -->
  <CancelOrderDialog
    ref="cancelDialogRef"
    @changed="refresh"
  />
</template>

<script setup lang="ts">
import { ref, computed, watch, h } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import {
  getOrder,
  changeOrderStatus,
  cancelSettle,
  finishOrder,
  handleUrge,
  reprintOrder,
  listRefunds,
  queryRefund
} from '@dining/api';
import type { OrderDetail, OrderSummary, Refund } from '@dining/types';
import OrderInfoSection from './order/OrderInfoSection.vue';
import OrderEditDialog from './OrderEditDialog.vue';
import SettleDialog from './order/SettleDialog.vue';
import CreditSettleDialog from './order/CreditSettleDialog.vue';
import RefundDialog from './order/RefundDialog.vue';
import CancelOrderDialog from './order/CancelOrderDialog.vue';
import { usePayFirst } from '../composables/usePayFirst';
import { isPendingPayNewOrder, newOrderWaitMinutes } from '../utils/newOrderWait';

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
// 在途标记:详情里的直接请求操作(改状态/催办/补打)进行中时禁止再次触发,
// 尤其「补打」会真的出纸 —— 连点会打出多张重复小票。
const acting = ref(false);

const settleDialogRef = ref<InstanceType<typeof SettleDialog> | null>(null);
const creditDialogRef = ref<InstanceType<typeof CreditSettleDialog> | null>(null);
const refundDialogRef = ref<InstanceType<typeof RefundDialog> | null>(null);
const cancelDialogRef = ref<InstanceType<typeof CancelOrderDialog> | null>(null);

// 先付模式开关(模块级单例,与订单管理/结算弹窗同源):用于「完成订单」确认弹窗
// 追加不出小票的提示,见 onFinish。
const payFirst = usePayFirst();

// 标题桌台优先展示名称(收银认的是「大厅01桌」而非桌号),名称为空的历史单回退桌号;与信息行/列表同口径
const dialogTitle = computed(() => {
  const d = detail.value;
  const table = d?.tableName || (d?.tableNo ? `${d.tableNo} 号桌` : '');
  return table ? `${table} · 订单详情` : '订单详情';
});

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
// immediate:父级以「visible 一开始就是 true」挂载时(如从路由直接打开)首帧也要拉取,
// 否则弹窗是个空壳。父级常规写法 visible=false 起步,immediate 不会多发请求。
watch(
  [() => props.visible, () => props.orderId],
  ([visible, id]) => {
    if (visible && id) void open(id);
  },
  { immediate: true }
);

function onClosed(): void {
  detail.value = null;
  refunds.value = [];
}

async function setStatus(status: number): Promise<void> {
  if (!detail.value || acting.value) return;
  acting.value = true;
  try {
    await changeOrderStatus({ orderId: detail.value.orderId, orderStatus: status });
    MessagePlugin.success('操作成功');
    await refresh();
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    acting.value = false;
  }
}

// onHandleUrge 消解该订单全部待处理催菜(已加急/已解释)，角标随之消失
async function onHandleUrge(): Promise<void> {
  if (!detail.value || acting.value) return;
  acting.value = true;
  try {
    await handleUrge({ orderId: detail.value.orderId });
    MessagePlugin.success('催菜已标记为已处理');
    await refresh();
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    acting.value = false;
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
  // 免单订单的操作名与按钮保持一致:撤销免单;挂账仍称撤销结算
  const isFree = row.settleType === 'free';
  const name = isFree ? '免单' : '挂账';
  const dlg = DialogPlugin.confirm({
    header: isFree ? '撤销免单' : '撤销结算',
    body: `确认撤销订单【${row.orderNo}】的${name}？撤销后订单恢复为未支付并回到「已上齐」，需重新结算。`,
    onConfirm: async () => {
      try {
        await cancelSettle({ orderId: row.orderId, reason: '撤销' + name });
        MessagePlugin.success(isFree ? '已撤销免单' : '已撤销结算');
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
  // 先付单的小票已在「确认收款」时出过(printKitchenOnPaid),完成订单只是归档兜底,
  // 不再出纸。提示单独成块排版,与订单管理卡片 onFinish 完全同一份渲染;牌子样式与
  // 「确认收款」弹窗(SettleDialog .st-hint-print)一致,避免收银为「补小票」重复操作。
  const finishBody = payFirst.value
    ? () =>
        h('div', { style: 'display:flex;flex-direction:column;gap:10px;' }, [
          h('div', '确认后订单将归档并计入今日统计，桌台释放为空闲。'),
          h(
            'div',
            {
              style:
                'padding:8px 12px;border:1px solid #ffd5c4;border-radius:8px;background:var(--brand-ghost,#fff8f5);color:var(--brand-deep,#f0481f);font-size:12px;line-height:1.5;'
            },
            [h('strong', '先付模式下不会打印小票'), h('span', '，仅归档并释放桌台。')]
          )
        ])
    : '确认后订单将归档并计入今日统计，桌台释放为空闲。';
  const dlg = DialogPlugin.confirm({
    header: '确认完成订单，释放桌台',
    body: finishBody,
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

// 取消订单走共享弹窗(带金额/后果/超时说明)。先付单等太久未确认收款时,
// 把已等待时长传进去,弹窗会解释「后厨还没出这一单,取消不会浪费」。
function onCancel(): void {
  const row = detail.value;
  if (!row) return;
  const waited = payFirst.value && isPendingPayNewOrder(row) ? newOrderWaitMinutes(row.createTime) : 0;
  cancelDialogRef.value?.open(row, waited);
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
  if (!detail.value || acting.value) return;
  acting.value = true;
  const label = docType === 'kitchen' ? '厨房单' : '顾客小票';
  try {
    const res = await reprintOrder({ orderId: detail.value.orderId, docType });
    MessagePlugin.success(res?.msg || `${label}补打指令已发送`);
  } catch {
    /* 失败原因(如「没有启用中的厨房单打印机」)由请求拦截器统一提示 */
  } finally {
    acting.value = false;
  }
}

// 供「订单管理」列表行的快捷按钮调用：每种操作全局只有一份实现
defineExpose({ open, refresh, openSettle, openRefund, openCredit, openEdit });
</script>
