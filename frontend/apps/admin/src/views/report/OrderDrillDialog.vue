<!-- 报表下钻订单列表弹窗
     报表页各图表(趋势/时段/结算构成/月度/KPI)点击后共用同一份「订单明细」实现,
     只由调用方传入不同的筛选参数(start/end 营业日区间、hour、settleType …),
     避免每个图表各写一套列表+分页后口径跑偏。
     单号可点进 OrderDetailDialog 看完整详情(含收款/退款等操作,权限逻辑不变)。 -->
<template>
  <t-dialog
    :visible="visible"
    :header="title"
    :width="isMobile ? '94vw' : '880px'"
    :footer="false"
    top="8vh"
    @update:visible="(v: boolean) => emit('update:visible', v)"
  >
    <!-- 口径说明:下钻按「下单营业日」归属,金额是订单当前应收/实收快照 ——
         与报表营业额(按到账时间归属)可能存在跨日差异,不说明会被当成对不上账。 -->
    <div class="drill-sub">
      共 <b>{{ pagination.total }}</b> 单<template v-if="hint"> · {{ hint }}</template>
    </div>

    <t-table
      :data="list"
      :columns="columns"
      row-key="orderId"
      :loading="loading"
      :pagination="pagination"
      row-class-name="drill-row"
      @page-change="onPageChange"
    >
      <template #orderNo="{ row }">
        <span
          class="ono"
          role="button"
          tabindex="0"
          :title="'查看 ' + (row.shortNo || row.orderNo) + ' 详情'"
          @click="openDetail(row)"
          @keydown.enter="openDetail(row)"
          @keydown.space.prevent="openDetail(row)"
          >#{{ row.shortNo || row.orderNo?.slice(-6) }}</span
        >
      </template>
      <template #table="{ row }">
        <span>{{ row.tableName || (row.tableNo ? row.tableNo + ' 号桌' : '—') }}</span>
      </template>
      <template #orderStatus="{ row }">
        <t-tag
          size="small"
          :theme="ORDER_STATUS[row.orderStatus]?.theme || 'default'"
          variant="light"
        >
          {{ ORDER_STATUS[row.orderStatus]?.label || '未知' }}
        </t-tag>
      </template>
      <template #totalAmount="{ row }">¥{{ money(row.totalAmount) }}</template>
      <template #payState="{ row }">
        <t-tag
          size="small"
          :theme="payTheme(row)"
          variant="light"
        >
          {{ payText(row) }}
        </t-tag>
      </template>
      <template #createTime="{ row }">
        <!-- 悬停给完整时间(含秒),列内只占人类可读的宽度 -->
        <span
          :title="row.createTime"
          class="time"
          >{{ shortTime(row.createTime) }}</span
        >
      </template>
    </t-table>

    <!-- 订单详情:与订单管理页共用同一个组件,操作按钮权限/逻辑零复制 -->
    <OrderDetailDialog
      ref="detailRef"
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
      @changed="load"
    />
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import type { PageInfo } from 'tdesign-vue-next';
import { listOrders, ORDER_STATUS } from '@dining/api';
import { money, payStatusText, payStatusTheme } from '@dining/shared';
import type { OrderSummary, PageQuery } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import { shortTime } from './reportShared';
import OrderDetailDialog from '../../components/OrderDetailDialog.vue';

const props = defineProps<{
  visible: boolean;
  title: string;
  /** 下钻筛选参数(营业日区间 start/end、hour、settleType …),由触发方按图表口径组装 */
  query: PageQuery;
  /** 口径补充说明,拼在「共 N 单」后(如「营业日 10-24」「时段 18:00-18:59」) */
  hint?: string;
}>();

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
}>();

const { isMobile } = useIsMobile();
// 未支付标签用商家口径「未确认支付」,与「订单管理」页同一份措辞(先付模式下
// 顾客可能已付钱,只是还没确认收款),见 packages/shared orderDisplay 的 unpaidLabel。
const payText = (p: Parameters<typeof payStatusText>[0]) => payStatusText(p, '未确认支付');
const payTheme = payStatusTheme;

const list = ref<OrderSummary[]>([]);
const loading = ref(false);
const pagination = reactive({
  current: 1,
  pageSize: 10,
  total: 0,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50]
});

const columns = computed(() => [
  { colKey: 'orderNo', title: '取餐号/单号', width: 110 },
  { colKey: 'table', title: '桌台', width: 120 },
  { colKey: 'orderStatus', title: '状态', width: 88 },
  { colKey: 'personCount', title: '人数', width: 60 },
  { colKey: 'totalAmount', title: '应收', width: 100, align: 'right' as const },
  { colKey: 'payState', title: '结算状态', width: 150 },
  { colKey: 'createTime', title: '下单时间', width: 110 }
]);

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listOrders({
      ...props.query,
      pageNum: pagination.current,
      pageSize: pagination.pageSize
    });
    list.value = res.items || [];
    pagination.total = res.total || 0;
  } finally {
    loading.value = false;
  }
}

function onPageChange(info: PageInfo): void {
  pagination.current = info.current;
  pagination.pageSize = info.pageSize;
  load();
}

// 打开或筛选条件变化时回到第 1 页重查;仅翻页走 onPageChange 不重置页码。
watch(
  () => [props.visible, props.query] as const,
  ([visible]) => {
    if (!visible) return;
    pagination.current = 1;
    load();
  },
  { immediate: true }
);

// ---------- 详情 ----------
const detailRef = ref<InstanceType<typeof OrderDetailDialog> | null>(null);
const detailVisible = ref(false);
const detailOrderId = ref(0);

function openDetail(row: OrderSummary): void {
  detailOrderId.value = Number(row.orderId || 0);
  detailVisible.value = true;
}
</script>

<style scoped>
.drill-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-bottom: 10px;
  line-height: 1.6;
}

.drill-sub b {
  color: var(--brand-deep);
}

.ono {
  color: var(--brand-deep);
  cursor: pointer;
  font-variant-numeric: tabular-nums;
}

.ono:hover {
  text-decoration: underline;
}

.time {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

:deep(.drill-row) {
  cursor: default;
}
</style>
