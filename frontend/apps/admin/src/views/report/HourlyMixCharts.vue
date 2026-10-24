<template>
  <!-- 时段分布 + 结算构成 -->
  <div class="duo">
    <div class="page-card chart-card">
      <div class="chart-head">
        <div>
          <div class="chart-title">经营时段分布</div>
          <div class="chart-sub">柱为订单数，线为营业额；点击柱条看该时段订单</div>
        </div>
      </div>
      <div
        ref="hourlyEl"
        class="chart-canvas"
      ></div>
    </div>

    <div class="page-card chart-card">
      <div class="chart-head">
        <div>
          <div class="chart-title">结算方式构成</div>
          <div class="chart-sub">按订单应收金额，合计 ¥{{ money(mixTotal) }} · 点击扇区看该类订单</div>
        </div>
      </div>
      <div
        ref="mixEl"
        class="chart-canvas"
      ></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import type { HourlyRow, SettleMixRow } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import {
  AXIS_COLOR,
  AXIS_LINE,
  SPLIT_LINE,
  BRAND,
  BLUE,
  AMBER,
  axisYuan,
  yuan,
  money,
  pickTooltipParam,
  bindChartClick
} from './reportShared';

const props = defineProps<{
  hourly: HourlyRow[];
  mix: SettleMixRow[];
  mixTotal: number;
}>();

// 下钻:时段柱条 → 该小时的订单;结算扇区 → 该结算方式的订单
const emit = defineEmits<{
  'drill-hour': [hour: string];
  'drill-mix': [settleType: string, label: string];
}>();

const hourlyEl = ref<HTMLElement | null>(null);
const mixEl = ref<HTMLElement | null>(null);
const { isMobile } = useIsMobile();

function renderHourly(): void {
  const c = chart('hourly', hourlyEl.value);
  if (!c) return;
  c.clear();
  const list = props.hourly || [];
  if (!list.length || !list.some(h => (h.orderCount || 0) > 0)) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const mobile = isMobile.value;
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: HourlyRow = list[p.dataIndex] || {};
        return `${row.hour}:00 - ${row.hour}:59<br/>订单 <b>${row.orderCount || 0}</b> 单<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>客流 <b>${row.guestCount || 0}</b> 人次`;
      }
    },
    legend: {
      data: ['订单数', '营业额'],
      right: 0,
      top: 0,
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: AXIS_COLOR, fontSize: 12 }
    },
    grid: { left: 8, right: 8, top: 44, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      data: list.map(h => h.hour as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      // 24 个档位在窄屏会挤成一团:移动端隔两个显示,桌面端隔一个
      axisLabel: { color: AXIS_COLOR, fontSize: 11, interval: mobile ? 2 : 1 }
    },
    yAxis: [
      {
        type: 'value',
        // 窄屏去掉轴名,给绘图区让出横向空间
        name: mobile ? undefined : '订单',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        minInterval: 1,
        splitLine: { lineStyle: { color: SPLIT_LINE } },
        axisLabel: { color: AXIS_COLOR, fontSize: 11 }
      },
      {
        type: 'value',
        name: mobile ? undefined : '营业额',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        splitLine: { show: false },
        axisLabel: { color: AXIS_COLOR, fontSize: 11, formatter: axisYuan }
      }
    ],
    series: [
      {
        name: '订单数',
        type: 'bar',
        barMaxWidth: 22,
        data: list.map(h => Number(h.orderCount || 0)),
        itemStyle: {
          borderRadius: [4, 4, 0, 0],
          color: {
            type: 'linear',
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: '#FF8A50' },
              { offset: 1, color: '#FFC7AC' }
            ]
          }
        },
        emphasis: { itemStyle: { color: BRAND } }
      },
      {
        name: '营业额',
        type: 'line',
        smooth: true,
        yAxisIndex: 1,
        symbol: 'circle',
        symbolSize: 5,
        data: list.map(h => Number(h.amount || 0)),
        itemStyle: { color: BLUE },
        lineStyle: { color: BLUE, width: 2 }
      }
    ]
  };
  c.setOption(option);
  bindChartClick(c, p => {
    if (p?.componentType !== 'series') return;
    const row: HourlyRow | undefined = list[p.dataIndex ?? -1];
    if (row?.hour != null) emit('drill-hour', String(row.hour));
  });
}

function renderMix(): void {
  const c = chart('mix', mixEl.value);
  if (!c) return;
  c.clear();
  const list = (props.mix || []).filter(m => Number(m.amount || 0) > 0);
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const colorOf: Record<string, string> = { normal: BRAND, free: AMBER, credit: BLUE, other: '#C7C7C7' };
  const option: EChartOption = {
    tooltip: {
      trigger: 'item',
      confine: true,
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: SettleMixRow = list[p.dataIndex] || {};
        return `${row.label}<br/>订单金额 <b>¥${yuan(row.amount)}</b><br/>实收 <b>¥${yuan(row.paidAmount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单`;
      }
    },
    legend: {
      bottom: 0,
      icon: 'circle',
      itemWidth: 8,
      itemHeight: 8,
      textStyle: { color: AXIS_COLOR, fontSize: 12 }
    },
    series: [
      {
        type: 'pie',
        radius: ['52%', '74%'],
        center: ['50%', '44%'],
        avoidLabelOverlap: true,
        itemStyle: { borderColor: '#fff', borderWidth: 2 },
        label: { show: false },
        labelLine: { show: false },
        data: list.map(m => ({
          name: m.label as string,
          value: Number(m.amount || 0),
          itemStyle: { color: colorOf[m.settleType ?? 'other'] || '#C7C7C7' }
        }))
      }
    ]
  };
  c.setOption(option);
  // 饼图的 series 点击参数没有 dataIndex 之外的行标识,name 即 label,
  // 用它回查原始行拿 settleType(label 是后端下发文案,直接透传给弹窗标题)。
  bindChartClick(c, p => {
    if (p?.componentType !== 'series') return;
    const row = list.find(m => m.label === (p as { name?: string }).name);
    if (row?.settleType) emit('drill-mix', row.settleType, row.label || row.settleType);
  });
}

onMounted(() => {
  renderHourly();
  renderMix();
});
watch(() => props.hourly, renderHourly);
watch(() => props.mix, renderMix);
// 断点变化时重绘时段图(其 option 依赖是否窄屏)
watch(isMobile, renderHourly);
</script>

<style scoped>
.duo {
  display: grid;
  grid-template-columns: 1.4fr 1fr;
  gap: 16px;
  margin-top: 16px;
}

.duo .chart-card {
  margin-top: 0;
}

.chart-card {
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

.chart-canvas {
  height: 300px;
  width: 100%;
}

@media (width <= 1200px) {
  .duo {
    grid-template-columns: 1fr;
  }
}

@media (width <= 767px) {
  .chart-canvas {
    height: 260px;
  }
}
</style>
