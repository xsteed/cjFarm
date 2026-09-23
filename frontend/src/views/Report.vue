<template>
  <div
    ref="rootRef"
    class="report"
  >
    <!-- 顶部：报表区间 + 导出 / 刷新 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <DateRangePicker
          v-model="range"
          :presets="rangePresets"
          value-type="YYYY-MM-DD"
          :clearable="false"
          style="width: 260px"
          @change="loadRange"
        />
        <span class="hint">口径：营业额按「实收金额」统计，免单与未核销挂账不计入</span>
      </div>
      <div class="toolbar-acts">
        <span
          v-if="updatedAt"
          class="updated"
          >更新于 {{ updatedAt }}</span
        >
        <Button
          theme="default"
          :disabled="!dailyRows.length"
          @click="exportCsv"
        >
          <template #icon>
            <FileIcon />
          </template>
          导出明细
        </Button>
        <Button
          theme="primary"
          :loading="loading"
          @click="loadAll"
        >
          <template #icon>
            <RefreshIcon />
          </template>
          刷新
        </Button>
      </div>
    </div>

    <Loading
      :loading="loading"
      show-overlay
    >
      <KpiCards
        :summary="summary"
        :deltas="deltas"
        :today="today"
      />
      <MetaStrip
        :summary="summary"
        :turnover="turnover"
      />
      <TrendChart
        :rows="dailyRows"
        :total="rangeTotal"
      />
      <HourlyMixCharts
        :hourly="hourlyRows"
        :mix="mixRows"
        :mix-total="mixTotal"
      />
      <MonthlyChart :rows="monthlyRows" />
      <DishRankChart
        v-model:sort="rankSort"
        :rows="rankRows"
        @change="loadRank"
      />
    </Loading>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Button, DateRangePicker, Loading } from 'tdesign-vue-next';
import type { DateRangeValue } from 'tdesign-vue-next';
import { FileIcon, RefreshIcon } from 'tdesign-icons-vue-next';
import { getDailyTrend, getDishRank, getHourlyReport, getMonthlyTrend, getReportSummary, getSettleMix } from '../api';
import type {
  DailyTrendRow,
  DishRankRow,
  HourlyRow,
  MonthlyTrendRow,
  ReportSummary,
  SettleMixRow
} from '../types/entities';
import type { PageQuery } from '../types/api';
import { useEChart } from '../composables/useEChart';
import { yuan, type Deltas } from './report/reportShared';
import KpiCards from './report/KpiCards.vue';
import MetaStrip from './report/MetaStrip.vue';
import TrendChart from './report/TrendChart.vue';
import HourlyMixCharts from './report/HourlyMixCharts.vue';
import MonthlyChart from './report/MonthlyChart.vue';
import DishRankChart from './report/DishRankChart.vue';

// ---------- 日期工具 ----------
function fmtDay(d: Date): string {
  const p = (n: number): string => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}
function shiftDays(n: number): Date {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return d;
}

const rootRef = ref<HTMLElement | null>(null);
const summary = ref<ReportSummary>({});
const dailyRows = ref<DailyTrendRow[]>([]);
const hourlyRows = ref<HourlyRow[]>([]);
const mixRows = ref<SettleMixRow[]>([]);
const monthlyRows = ref<MonthlyTrendRow[]>([]);
const rankRows = ref<DishRankRow[]>([]);
const loading = ref(false);
const updatedAt = ref('');
const rankSort = ref<'qty' | 'amount'>('qty');

// 报表区间默认近 7 天。presets 用函数是为了每次展开面板时都按「当前日期」重算，
// 而不是在组件初始化那一刻就把相对日期算死。
const range = ref<DateRangeValue>([fmtDay(shiftDays(-6)), fmtDay(new Date())]);
const rangePresets: Record<string, () => [string, string]> = {
  近7天: () => [fmtDay(shiftDays(-6)), fmtDay(new Date())],
  近30天: () => [fmtDay(shiftDays(-29)), fmtDay(new Date())],
  近90天: () => [fmtDay(shiftDays(-89)), fmtDay(new Date())],
  本月: () => {
    const d = new Date();
    return [fmtDay(new Date(d.getFullYear(), d.getMonth(), 1)), fmtDay(d)];
  },
  上月: () => {
    const d = new Date();
    // new Date(y, m, 0) 是「上个月最后一天」，省去自己处理大小月
    return [fmtDay(new Date(d.getFullYear(), d.getMonth() - 1, 1)), fmtDay(new Date(d.getFullYear(), d.getMonth(), 0))];
  }
};

const today = computed(() => fmtDay(new Date()));

/** 区间入参：正常返回 start/end，控件被清空时回退到近 7 天。 */
function rangeParams(): PageQuery {
  const r = range.value;
  if (Array.isArray(r) && r.length === 2 && r[0] && r[1]) {
    return { start: r[0], end: r[1] };
  }
  return { days: 7 };
}

// ---------- 环比 ----------
/** 涨跌幅(%),基准为 0 或缺失时返回 null(不显示「+∞%」这种噪音)。 */
function pct(cur: unknown, prev: unknown): number | null {
  const c = Number(cur || 0);
  const p = Number(prev || 0);
  if (p <= 0) return null;
  return ((c - p) / p) * 100;
}

const deltas = computed<Deltas>(() => ({
  todayAmount: pct(summary.value.todayAmount, summary.value.yesterdayAmount),
  todayOrder: pct(summary.value.todayOrderCount, summary.value.yesterdayOrderCount),
  todayGuest: pct(summary.value.todayGuestCount, summary.value.yesterdayGuestCount),
  // 本月至今对比「上月同期」，而不是上月整月 —— 后者在月中必然显示负增长，没有参考价值
  monthAmount: pct(summary.value.monthAmount, summary.value.lastMonthSamePeriodAmount)
}));

const turnover = computed<string>(() => {
  const tables = Number(summary.value.tableCount || 0);
  if (!tables) return '—';
  return (Number(summary.value.todayOrderCount || 0) / tables).toFixed(2);
});

const rangeTotal = computed(() => {
  let amount = 0;
  let orders = 0;
  let guests = 0;
  for (const d of dailyRows.value) {
    amount += Number(d.amount || 0);
    orders += Number(d.orderCount || 0);
    guests += Number(d.guestCount || 0);
  }
  return { amount, orders, guests };
});

const mixTotal = computed(() => (mixRows.value || []).reduce((sum, m) => sum + Number(m.amount || 0), 0));

// ---------- 数据加载 ----------
async function loadAll(): Promise<void> {
  loading.value = true;
  try {
    // 六个接口互不依赖，并行拉取；任一失败由 axios 拦截器统一提示
    const [s, d, h, m, mo, r] = await Promise.all([
      getReportSummary(),
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams()),
      getMonthlyTrend(),
      getDishRank({ limit: 10, sort: rankSort.value })
    ]);
    summary.value = s || {};
    dailyRows.value = d || [];
    hourlyRows.value = h || [];
    mixRows.value = m || [];
    monthlyRows.value = mo || [];
    rankRows.value = r || [];
    updatedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  } catch {
    // 错误提示已由请求层统一处理，这里只需保证 loading 收尾
  } finally {
    loading.value = false;
  }
}

/** 切换区间只重取三个「区间相关」的接口，汇总与月度、排行不受影响。 */
async function loadRange(): Promise<void> {
  try {
    const [d, h, m] = await Promise.all([
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams())
    ]);
    dailyRows.value = d || [];
    hourlyRows.value = h || [];
    mixRows.value = m || [];
  } catch {
    /* 同上 */
  }
}

async function loadRank(): Promise<void> {
  try {
    rankRows.value = (await getDishRank({ limit: 10, sort: rankSort.value })) || [];
  } catch {
    /* 同上 */
  }
}

// ---------- 导出 ----------
/** 导出当前区间的逐日明细(带 BOM，Excel 打开中文不乱码)。 */
function exportCsv(): void {
  const rows: Array<Array<string | number>> = [['日期', '营业额(元)', '订单数', '客流(人次)']];
  for (const d of dailyRows.value) {
    rows.push([d.date as string, yuan(d.amount), d.orderCount || 0, d.guestCount || 0]);
  }
  rows.push(['合计', yuan(rangeTotal.value.amount), rangeTotal.value.orders, rangeTotal.value.guests]);

  const csv = rows.map(r => r.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(',')).join('\r\n');
  const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `营业明细_${range.value?.[0] || ''}_${range.value?.[1] || ''}.csv`;
  a.click();
  // 延迟回收:Firefox/Safari 对立即 revoke 的 blob URL 可能中断下载(参照 utils/zip.ts 的做法)
  setTimeout(() => URL.revokeObjectURL(url), 4000);
}

// ---------- 生命周期 ----------
const { observeRoot } = useEChart();

onMounted(() => {
  loadAll();
  observeRoot(rootRef.value);
});
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.6;
}

.toolbar-acts {
  display: flex;
  align-items: center;
  gap: 10px;
}

.updated {
  font-size: 12px;
  color: var(--ink-4);
}

@media (width <= 767px) {
  .toolbar-acts {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
