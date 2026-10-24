<template>
  <div
    ref="rootRef"
    class="report"
  >
    <!-- 移动端下拉刷新当前区间报表 -->
    <PullRefresh
      v-if="isMobile"
      :distance="refreshDistance"
      :refreshing="refreshRefreshing"
      :pulling="refreshPulling"
    />

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
            <DownloadIcon />
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
      <!-- 站点访问统计:PV/UV/新访客趋势 + 渠道构成 + 扫码入口 Top -->
      <VisitChart
        :rows="visitRows"
        :channels="visitChannels"
        :entries="visitEntries"
        :devices="visitDevices"
        :oses="visitOses"
        :browsers="visitBrowsers"
        :total="visitTotal"
      />
      <HourlyMixCharts
        :hourly="hourlyRows"
        :mix="mixRows"
        :mix-total="mixTotal"
      />
      <DiningDurationChart :stats="durationStats" />
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
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { Button, DateRangePicker, Loading } from 'tdesign-vue-next';
import type { DateRangeValue } from 'tdesign-vue-next';
import { DownloadIcon, RefreshIcon } from 'tdesign-icons-vue-next';
import {
  getDailyTrend,
  getDiningDuration,
  getDishRank,
  getHourlyReport,
  getMonthlyTrend,
  getReportSummary,
  getSettleMix,
  getVisitStats
} from '@dining/api';
import type {
  DailyTrendRow,
  DiningDurationStats,
  DishRankRow,
  HourlyRow,
  MonthlyTrendRow,
  PageQuery,
  ReportSummary,
  SettleMixRow,
  VisitChannelRow,
  VisitDailyRow,
  VisitDistRow,
  VisitEntryRow,
  VisitStats
} from '@dining/types';

import { useEChart } from '../composables/useEChart';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import PullRefresh from '../components/PullRefresh.vue';
import { yuan, type Deltas } from './report/reportShared';
import KpiCards from './report/KpiCards.vue';
import MetaStrip from './report/MetaStrip.vue';
import TrendChart from './report/TrendChart.vue';
import HourlyMixCharts from './report/HourlyMixCharts.vue';
import MonthlyChart from './report/MonthlyChart.vue';
import DishRankChart from './report/DishRankChart.vue';
import DiningDurationChart from './report/DiningDurationChart.vue';
import VisitChart from './report/VisitChart.vue';

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
const durationStats = ref<DiningDurationStats>({});
// 站点访问统计(与经营报表同一区间;口径独立 —— PV/UV 不受订单影响)
const visitRows = ref<VisitDailyRow[]>([]);
const visitChannels = ref<VisitChannelRow[]>([]);
const visitEntries = ref<VisitEntryRow[]>([]);
const visitDevices = ref<VisitDistRow[]>([]);
const visitOses = ref<VisitDistRow[]>([]);
const visitBrowsers = ref<VisitDistRow[]>([]);
const visitSummary = ref<VisitStats['summary']>({});
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

// 访问图表的区间合计:用后端口径(区间 UV 是明细去重值,不能由逐日 UV 相加得出)
const visitTotal = computed(() => ({
  pv: Number(visitSummary.value?.pv || 0),
  uv: Number(visitSummary.value?.uv || 0),
  newUv: Number(visitSummary.value?.newUv || 0)
}));

// ---------- 数据加载 ----------
// 两个加载入口(全量 / 切区间)共享一份「加载中」计数:各自置 true/false 会让
// 先结束的那个提前灭掉另一个请求的 loading(刷新按钮的转圈中途停住)。
let loadingCount = 0;
function beginLoad(): void {
  loadingCount++;
  loading.value = true;
}
function endLoad(): void {
  loadingCount = Math.max(0, loadingCount - 1);
  if (loadingCount === 0) loading.value = false;
}

async function loadAll(): Promise<void> {
  beginLoad();
  try {
    // 各接口互不依赖，并行拉取；单块失败只丢那一块，其余图表照常显示 ——
    // 全有或全无的 Promise.all 会让任一失败都把整页留在旧数据/空白，
    // 与「任一失败由拦截器统一提示」的承诺不是一回事（提示照弹，页面不该白）。
    const [s, d, h, m, mo, r, du, v] = await Promise.allSettled([
      getReportSummary(),
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams()),
      getMonthlyTrend(),
      getDishRank({ limit: 10, sort: rankSort.value }),
      getDiningDuration(rangeParams()),
      getVisitStats(rangeParams())
    ]);
    summary.value = (s.status === 'fulfilled' && s.value) || {};
    dailyRows.value = (d.status === 'fulfilled' && d.value) || [];
    hourlyRows.value = (h.status === 'fulfilled' && h.value) || [];
    mixRows.value = (m.status === 'fulfilled' && m.value) || [];
    monthlyRows.value = (mo.status === 'fulfilled' && mo.value) || [];
    rankRows.value = (r.status === 'fulfilled' && r.value) || [];
    durationStats.value = (du.status === 'fulfilled' && du.value) || {};
    if (v.status === 'fulfilled') applyVisitStats(v.value);
    updatedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  } finally {
    endLoad();
  }
}

/** 切换区间只重取「区间相关」的接口，汇总与月度、排行不受影响。 */
async function loadRange(): Promise<void> {
  // 切区间也要有加载反馈:此前只有工具栏「刷新」会点亮 loading,
  // 换日期区间时页面毫无反应,弱网下像是没生效。
  beginLoad();
  try {
    // 与 loadAll 同一策略:单块失败不影响其余图表（错误提示仍由请求层弹出）。
    const [d, h, m, du, v] = await Promise.allSettled([
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams()),
      getDiningDuration(rangeParams()),
      getVisitStats(rangeParams())
    ]);
    dailyRows.value = (d.status === 'fulfilled' && d.value) || [];
    hourlyRows.value = (h.status === 'fulfilled' && h.value) || [];
    mixRows.value = (m.status === 'fulfilled' && m.value) || [];
    durationStats.value = (du.status === 'fulfilled' && du.value) || {};
    if (v.status === 'fulfilled') applyVisitStats(v.value);
  } finally {
    endLoad();
  }
}

/** 把访问统计接口出参落到三个图表的数据源(interface 内字段全可选,统一下兜底)。 */
function applyVisitStats(v: VisitStats | undefined): void {
  visitRows.value = v?.daily || [];
  visitChannels.value = v?.channels || [];
  visitEntries.value = v?.entries || [];
  visitDevices.value = v?.devices || [];
  visitOses.value = v?.oses || [];
  visitBrowsers.value = v?.browsers || [];
  visitSummary.value = v?.summary || {};
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
/** 导出 CSV 的 object URL 与其延迟回收定时器(卸载时清理,见 onBeforeUnmount) */
let exportUrl: string | null = null;
let exportRevokeTimer: ReturnType<typeof setTimeout> | null = null;

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
  // 延迟回收:Firefox/Safari 对立即 revoke 的 blob URL 可能中断下载(参照 utils/zip.ts 的做法)。
  // 句柄记下来,卸载时清掉并立即回收,避免组件已卸载后还留一个待执行的定时器与无人释放的 URL。
  exportUrl = url;
  exportRevokeTimer = setTimeout(() => {
    URL.revokeObjectURL(url);
    exportUrl = null;
    exportRevokeTimer = null;
  }, 4000);
}

// ---------- 移动端 ----------
const { isMobile } = useIsMobile();
// 下拉刷新:重新拉取当前区间的全部报表数据
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(loadAll);

// ---------- 生命周期 ----------
const { observeRoot } = useEChart();

onMounted(() => {
  loadAll();
  observeRoot(rootRef.value);
});

onBeforeUnmount(() => {
  // 导出 CSV 的 object URL:清掉待执行的回收定时器并立即回收
  if (exportRevokeTimer) {
    clearTimeout(exportRevokeTimer);
    exportRevokeTimer = null;
  }
  if (exportUrl) {
    URL.revokeObjectURL(exportUrl);
    exportUrl = null;
  }
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
  /* 「更新于」独占一行,导出 / 刷新两个按钮等宽平分下一行,拇指好点 */
  .toolbar-acts {
    width: 100%;
    flex-wrap: wrap;
    gap: 8px;
    justify-content: flex-start;
  }

  .updated {
    flex: 1 1 100%;
  }

  .toolbar-acts .t-button {
    flex: 1;
  }
}
</style>
