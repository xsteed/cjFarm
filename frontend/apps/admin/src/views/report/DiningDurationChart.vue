<template>
  <div class="page-card chart-card">
    <div class="chart-head">
      <div>
        <div class="chart-title">用餐时长分析</div>
        <div class="chart-sub">
          统计口径为已完成订单（完成时间 − 下单时间）<template v-if="stats.orderCount">
            · 样本 <b>{{ stats.orderCount }}</b> 单 · 人次
            <b>{{ stats.guestCount || 0 }}</b></template
          >
        </div>
      </div>
      <div
        v-if="stats.orderCount"
        class="dur-stats"
      >
        <span>平均 <b>{{ durationText(stats.avgMinutes) }}</b></span>
        <span>中位 <b>{{ durationText(stats.medianMinutes) }}</b></span>
        <span>最长 <b>{{ durationText(stats.maxMinutes) }}</b></span>
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
import type { DiningDurationStats } from '@dining/types';
import { durationText } from '@dining/shared';
import { useIsMobile } from '../../utils/useMobile';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import { AXIS_COLOR, AXIS_LINE, SPLIT_LINE, BRAND_DEEP, pickTooltipParam } from './reportShared';

const props = defineProps<{
  stats: DiningDurationStats;
}>();

const el = ref<HTMLElement | null>(null);
const { isMobile } = useIsMobile();

function render(): void {
  const c = chart('diningDuration', el.value);
  if (!c) return;
  c.clear();
  const list = props.stats.buckets || [];
  if (!list.length || !props.stats.orderCount) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row = list[p.dataIndex] || {};
        const n = Number(row.count || 0);
        const total = props.stats.orderCount || 1;
        return `${row.label}<br/><b>${n}</b> 单（占 ${(n / total * 100).toFixed(1)}%）`;
      }
    },
    grid: { left: 8, right: 16, top: 20, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      data: list.map(b => b.label as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: '#4A4A4A', fontSize: 11, interval: 0 }
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11 }
    },
    series: [
      {
        name: '订单数',
        type: 'bar',
        barMaxWidth: 34,
        data: list.map(b => Number(b.count || 0)),
        itemStyle: {
          borderRadius: [6, 6, 0, 0],
          color: {
            type: 'linear',
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: '#FF8A50' },
              { offset: 1, color: BRAND_DEEP }
            ]
          }
        },
        label: {
          show: true,
          position: 'top',
          color: AXIS_COLOR,
          fontSize: 11
        }
      }
    ]
  };
  c.setOption(option);
}

onMounted(render);
watch(() => props.stats, render, { deep: true });
// 断点变化时重绘
watch(isMobile, render);
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

/* 汇总指标(平均/中位/最长):横排小指标,数字比图更常被店员引用 */
.dur-stats {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--ink-3);
}

.dur-stats b {
  font-weight: 700;
  color: var(--brand-deep);
}

.chart-canvas {
  height: 260px;
  width: 100%;
}
</style>
