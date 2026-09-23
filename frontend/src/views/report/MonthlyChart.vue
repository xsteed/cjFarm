<template>
  <div class="page-card chart-card">
    <div class="chart-head">
      <div>
        <div class="chart-title">近 12 个月营业额</div>
        <div class="chart-sub">悬停可看当月订单数与客流</div>
      </div>
    </div>
    <div
      ref="el"
      class="chart-canvas"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import type { MonthlyTrendRow } from '../../types/entities';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import { AXIS_COLOR, AXIS_LINE, SPLIT_LINE, BRAND, BRAND_DEEP, axisYuan, yuan, pickTooltipParam } from './reportShared';

const props = defineProps<{
  rows: MonthlyTrendRow[];
}>();

const el = ref<HTMLElement | null>(null);

function render(): void {
  const c = chart('monthly', el.value);
  if (!c) return;
  c.clear();
  const list = props.rows || [];
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: MonthlyTrendRow = list[p.dataIndex] || {};
        return `${p.axisValue}<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单<br/>客流 <b>${row.guestCount || 0}</b> 人次`;
      }
    },
    grid: { left: 8, right: 8, top: 20, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      data: list.map(m => m.month as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, hideOverlap: true }
    },
    yAxis: {
      type: 'value',
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, formatter: axisYuan }
    },
    series: [
      {
        name: '营业额',
        type: 'bar',
        barMaxWidth: 30,
        data: list.map(m => Number(m.amount || 0)),
        itemStyle: {
          borderRadius: [6, 6, 0, 0],
          color: {
            type: 'linear',
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: BRAND },
              { offset: 1, color: '#FFB08A' }
            ]
          }
        },
        emphasis: { itemStyle: { color: BRAND_DEEP } }
      }
    ]
  };
  c.setOption(option);
}

onMounted(render);
watch(() => props.rows, render);
</script>

<style scoped>
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

@media (width <= 767px) {
  .chart-canvas {
    height: 260px;
  }
}
</style>
