<template>
  <!-- 站点访问统计:趋势(全宽) + 渠道构成 / 扫码入口 Top(并排) -->
  <div class="visit-wrap">
    <div class="page-card chart-card">
      <div class="chart-head">
        <div>
          <div class="chart-title">访问趋势</div>
          <div class="chart-sub">
            区间合计 <b>{{ total.pv }}</b> 次访问 · 独立访客 <b>{{ total.uv }}</b> 人 · 新访客
            <b>{{ total.newUv }}</b> 人
            <span class="caliber">UV 按访客去重，匿名访客换设备/换网络会被算作新访客，看趋势即可</span>
          </div>
        </div>
      </div>
      <div
        ref="trendEl"
        class="chart-canvas"
      ></div>
    </div>

    <div class="duo">
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">渠道构成</div>
            <div class="chart-sub">扫码点餐 / 小程序 / 管理后台，PV 与 UV</div>
          </div>
        </div>
        <div
          ref="channelEl"
          class="chart-canvas duo-canvas"
        ></div>
      </div>

      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">扫码入口 Top</div>
            <div class="chart-sub">哪张桌的二维码被扫得最多（扫码点餐 + 小程序，按桌台码统计）</div>
          </div>
        </div>
        <div
          ref="entryEl"
          class="chart-canvas duo-canvas"
        ></div>
      </div>
    </div>

    <!-- 终端构成:设备类型 / 操作系统 / 浏览器三张小图 -->
    <div class="trio">
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">设备类型</div>
            <div class="chart-sub">手机 / 平板 / 电脑</div>
          </div>
        </div>
        <div
          ref="deviceEl"
          class="chart-canvas trio-canvas"
        ></div>
      </div>
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">操作系统</div>
            <div class="chart-sub">小程序为端上直传，H5 由 UA 解析</div>
          </div>
        </div>
        <div
          ref="osEl"
          class="chart-canvas trio-canvas"
        ></div>
      </div>
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">浏览器 / 宿主</div>
            <div class="chart-sub">小程序记为「微信小程序」，H5 能识别微信内打开</div>
          </div>
        </div>
        <div
          ref="browserEl"
          class="chart-canvas trio-canvas"
        ></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch, type Ref } from 'vue';
import type { VisitChannelRow, VisitDailyRow, VisitDistRow, VisitEntryRow } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import { AXIS_COLOR, AXIS_LINE, SPLIT_LINE, BRAND, GREEN, BLUE, AMBER, pickTooltipParam } from './reportShared';

const props = defineProps<{
  rows: VisitDailyRow[];
  channels: VisitChannelRow[];
  entries: VisitEntryRow[];
  devices: VisitDistRow[];
  oses: VisitDistRow[];
  browsers: VisitDistRow[];
  total: { pv: number; uv: number; newUv: number };
}>();

const trendEl = ref<HTMLElement | null>(null);
const channelEl = ref<HTMLElement | null>(null);
const entryEl = ref<HTMLElement | null>(null);
const deviceEl = ref<HTMLElement | null>(null);
const osEl = ref<HTMLElement | null>(null);
const browserEl = ref<HTMLElement | null>(null);
const { isMobile } = useIsMobile();

// 趋势:PV(面积线)/ UV / 新访客三条线共用「次数」量纲，不做双轴。
function renderTrend(): void {
  const c = chart('visitTrend', trendEl.value);
  if (!c) return;
  c.clear();
  const list = props.rows || [];
  if (!list.length || !list.some(d => (d.pv || 0) > 0)) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const showSymbol = list.length <= 30;
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'line', label: { backgroundColor: BRAND } },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: VisitDailyRow = list[p.dataIndex] || {};
        const parts = [
          `${p.axisValue}`,
          `PV <b>${row.pv || 0}</b>`,
          `UV <b>${row.uv || 0}</b>`,
          `新访客 <b>${row.newUv || 0}</b>`
        ];
        // 有分渠道数据时补一行构成，省得再点开渠道图对时间
        const h5 = row.h5Pv || 0;
        const mp = row.mpPv || 0;
        const admin = row.adminPv || 0;
        if (h5 + mp + admin > 0) parts.push(`扫码 ${h5} · 小程序 ${mp} · 后台 ${admin}`);
        return parts.join('<br/>');
      }
    },
    legend: {
      data: ['PV', 'UV', '新访客'],
      right: 0,
      top: 0,
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: AXIS_COLOR, fontSize: 12 }
    },
    grid: { left: 8, right: 8, top: isMobile.value ? 34 : 44, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: list.map(d => d.date as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, hideOverlap: true }
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    series: [
      {
        name: 'PV',
        type: 'line',
        smooth: true,
        showSymbol,
        symbol: 'circle',
        symbolSize: 6,
        data: list.map(d => Number(d.pv || 0)),
        itemStyle: { color: BRAND },
        lineStyle: { color: BRAND, width: 3 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(255,107,53,0.26)' },
              { offset: 1, color: 'rgba(255,107,53,0.02)' }
            ]
          }
        }
      },
      {
        name: 'UV',
        type: 'line',
        smooth: true,
        showSymbol,
        symbol: 'circle',
        symbolSize: 6,
        data: list.map(d => Number(d.uv || 0)),
        itemStyle: { color: GREEN },
        lineStyle: { color: GREEN, width: 2 }
      },
      {
        name: '新访客',
        type: 'line',
        smooth: true,
        showSymbol,
        symbol: 'circle',
        symbolSize: 5,
        data: list.map(d => Number(d.newUv || 0)),
        itemStyle: { color: BLUE },
        lineStyle: { color: BLUE, width: 2, type: 'dashed' }
      }
    ]
  };
  c.setOption(option);
}

// 渠道构成:横向条形图（PV / UV 成对），渠道少时比饼图更易读。
function renderChannels(): void {
  const c = chart('visitChannel', channelEl.value);
  if (!c) return;
  c.clear();
  const list = (props.channels || []).filter(x => (x.pv || 0) > 0 || (x.uv || 0) > 0);
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  // ECharts 的类目轴自下而上排列，先反转再渲染，最大的渠道出现在最上方。
  const ordered = [...list].reverse();
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: VisitChannelRow = ordered[p.dataIndex] || {};
        return `${row.label || row.channel || ''}<br/>PV <b>${row.pv || 0}</b><br/>UV <b>${row.uv || 0}</b>`;
      }
    },
    legend: {
      data: ['PV', 'UV'],
      right: 0,
      top: 0,
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: AXIS_COLOR, fontSize: 12 }
    },
    grid: { left: 8, right: 8, top: 34, bottom: 4, containLabel: true },
    xAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    yAxis: {
      type: 'category',
      data: ordered.map(x => x.label || x.channel || ''),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    series: [
      {
        name: 'PV',
        type: 'bar',
        barMaxWidth: 14,
        data: ordered.map(x => Number(x.pv || 0)),
        itemStyle: { color: BRAND, borderRadius: [0, 4, 4, 0] }
      },
      {
        name: 'UV',
        type: 'bar',
        barMaxWidth: 14,
        data: ordered.map(x => Number(x.uv || 0)),
        itemStyle: { color: GREEN, borderRadius: [0, 4, 4, 0] }
      }
    ]
  };
  c.setOption(option);
}

// 扫码入口 Top:横向条形图，标签已由后端拼好（桌名 · 桌号）。
function renderEntries(): void {
  const c = chart('visitEntry', entryEl.value);
  if (!c) return;
  c.clear();
  // 先按 PV 降序再截取:直接 slice 是「前 8 条」,后端一旦改了返回顺序就不是 Top 了
  const list = (props.entries || [])
    .filter(x => (x.pv || 0) > 0)
    .sort((a, b) => (b.pv || 0) - (a.pv || 0))
    .slice(0, 8);
  if (!list.length) {
    // 空态分两种:整个区间压根没访问 → 「暂无数据」;有访问却没入口 → 说明顾客
    // 不是从桌台码进来的(或顾客端版本未上报入口)。后者必须说清楚,否则店家
    // 只会看到一块空面板,无从判断是没客人还是功能坏了。
    if ((props.total?.pv || 0) > 0) {
      c.setOption({
        title: {
          text: '暂无扫码入口数据',
          subtext: '顾客需通过桌台二维码进入才会记录入口',
          left: 'center',
          top: 'middle',
          textStyle: { color: '#C7C7C7', fontSize: 13, fontWeight: 400 },
          subtextStyle: { color: '#C7C7C7', fontSize: 12 }
        }
      });
      return;
    }
    c.setOption(EMPTY_OPTION);
    return;
  }
  const ordered = [...list].reverse();
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: VisitEntryRow = ordered[p.dataIndex] || {};
        return `${row.label || row.entry || ''}<br/>扫码 <b>${row.pv || 0}</b> 次`;
      }
    },
    grid: { left: 8, right: 16, top: 10, bottom: 4, containLabel: true },
    xAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    yAxis: {
      type: 'category',
      data: ordered.map(x => x.label || x.entry || ''),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, width: 110, overflow: 'truncate' }
    },
    series: [
      {
        name: '扫码次数',
        type: 'bar',
        barMaxWidth: 14,
        data: ordered.map(x => Number(x.pv || 0)),
        itemStyle: { color: BLUE, borderRadius: [0, 4, 4, 0] },
        label: { show: true, position: 'right', color: AXIS_COLOR, fontSize: 11 }
      }
    ]
  };
  c.setOption(option);
}

// 终端小图的通用渲染:横向条形,Top 6,空数据显示「暂无数据」。
function renderDist(
  key: string,
  el: Ref<HTMLElement | null>,
  rows: VisitDistRow[],
  color: string,
  title: string
): void {
  const c = chart(key, el.value);
  if (!c) return;
  c.clear();
  // 同上:先按 PV 降序再截取 Top 6
  const list = (rows || [])
    .filter(x => (x.pv || 0) > 0)
    .sort((a, b) => (b.pv || 0) - (a.pv || 0))
    .slice(0, 6);
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  // ECharts 的类目轴自下而上排列,先反转让最大值在最上方
  const ordered = [...list].reverse();
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: VisitDistRow = ordered[p.dataIndex] || {};
        return `${row.name || '未知'}<br/><b>${row.pv || 0}</b> 次`;
      }
    },
    grid: { left: 8, right: 24, top: 10, bottom: 4, containLabel: true },
    xAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    yAxis: {
      type: 'category',
      data: ordered.map(x => x.name || '未知'),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, width: 72, overflow: 'truncate' }
    },
    series: [
      {
        name: title,
        type: 'bar',
        barMaxWidth: 14,
        data: ordered.map(x => Number(x.pv || 0)),
        itemStyle: { color, borderRadius: [0, 4, 4, 0] },
        label: { show: true, position: 'right', color: AXIS_COLOR, fontSize: 11 }
      }
    ]
  };
  c.setOption(option);
}

function renderTerminals(): void {
  renderDist('visitDevice', deviceEl, props.devices || [], GREEN, '设备类型');
  renderDist('visitOs', osEl, props.oses || [], BLUE, '操作系统');
  renderDist('visitBrowser', browserEl, props.browsers || [], AMBER, '浏览器');
}

function renderAll(): void {
  renderTrend();
  renderChannels();
  renderEntries();
  renderTerminals();
}

onMounted(renderAll);
watch(() => [props.rows, props.channels, props.entries, props.devices, props.oses, props.browsers], renderAll);
// 切换手机/桌面宽度时用不同的 option 重绘
watch(isMobile, renderAll);
</script>

<style scoped>
.visit-wrap {
  margin-top: 16px;
}

.chart-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.chart-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
}

.chart-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-top: 4px;
  line-height: 1.5;
}

.chart-sub b {
  color: var(--brand-deep);
}

/* 口径提示:与正文同色系但更淡,避免和数字抢注意力 */
.caliber {
  display: block;
  margin-top: 2px;
  color: var(--ink-4);
  font-size: 11px;
}

.chart-canvas {
  height: 300px;
  width: 100%;
}

.duo {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 16px;
}

.trio {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-top: 16px;
}

.duo-canvas {
  height: 260px;
}

@media (width <= 900px) {
  .duo {
    grid-template-columns: 1fr;
  }

  .trio {
    grid-template-columns: 1fr;
  }
}

@media (width <= 767px) {
  .chart-canvas {
    height: 260px;
  }

  .duo {
    gap: 12px;
  }

  .trio {
    gap: 12px;
  }
}
</style>
