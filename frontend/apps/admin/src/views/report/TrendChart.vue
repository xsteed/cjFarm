<template>
  <div class="page-card chart-card">
    <div class="chart-head">
      <div>
        <div class="chart-title">营业额与订单趋势</div>
        <div class="chart-sub">
          区间合计 <b>¥{{ money(total.amount) }}</b> · {{ total.orders }} 单 · 客流 {{ total.guests }} 人次 ·
          点击数据点看当日订单
        </div>
      </div>
      <Button
        size="small"
        variant="outline"
        :disabled="!rows.length"
        @click="emit('drill', undefined)"
      >
        区间订单明细
      </Button>
    </div>
    <div
      ref="el"
      class="chart-canvas"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { Button } from 'tdesign-vue-next';
import type { DailyTrendRow } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import {
  AXIS_COLOR,
  AXIS_LINE,
  SPLIT_LINE,
  BRAND,
  BRAND_DEEP,
  GREEN,
  axisYuan,
  yuan,
  money,
  pickTooltipParam
} from './reportShared';

const props = defineProps<{
  rows: DailyTrendRow[];
  total: { amount: number; orders: number; guests: number };
}>();

// 下钻:点击数据点带出该营业日,表头按钮不带 date 表示整个报表区间
const emit = defineEmits<{
  drill: [date?: string];
}>();

const el = ref<HTMLElement | null>(null);
const { isMobile } = useIsMobile();

function render(): void {
  const c = chart('daily', el.value);
  if (!c) return;
  c.clear();
  const list = props.rows || [];
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const mobile = isMobile.value;
  const showSymbol = list.length <= 30;
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      // 手机上把浮层限制在画布内,避免贴边时溢出到屏幕外
      confine: true,
      axisPointer: { type: 'cross', label: { backgroundColor: BRAND_DEEP } },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: DailyTrendRow = list[p.dataIndex] || {};
        return `${p.axisValue}<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单<br/>客流 <b>${row.guestCount || 0}</b> 人次`;
      }
    },
    legend: {
      data: ['营业额', '订单数'],
      right: 0,
      top: 0,
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: AXIS_COLOR, fontSize: 12 }
    },
    grid: { left: 8, right: 8, top: mobile ? 34 : 44, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: list.map(d => d.date as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, hideOverlap: true }
    },
    yAxis: [
      {
        type: 'value',
        // 窄屏去掉轴名,给绘图区让出横向空间(图例已能区分双轴)
        name: mobile ? undefined : '营业额',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        splitLine: { lineStyle: { color: SPLIT_LINE } },
        axisLabel: { color: AXIS_COLOR, fontSize: 11, formatter: axisYuan }
      },
      {
        type: 'value',
        name: mobile ? undefined : '订单',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        minInterval: 1,
        splitLine: { show: false },
        axisLabel: { color: AXIS_COLOR, fontSize: 11 }
      }
    ],
    series: [
      {
        name: '营业额',
        type: 'line',
        smooth: true,
        showSymbol,
        symbol: 'circle',
        symbolSize: 6,
        data: list.map(d => Number(d.amount || 0)),
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
        name: '订单数',
        type: 'line',
        smooth: true,
        yAxisIndex: 1,
        showSymbol,
        symbol: 'circle',
        symbolSize: 6,
        data: list.map(d => Number(d.orderCount || 0)),
        itemStyle: { color: GREEN },
        lineStyle: { color: GREEN, width: 2, type: 'dashed' }
      }
    ]
  };
  c.setOption(option);
  // 点击数据点下钻当日订单。off+on 重绑而不是只绑一次:实例按 key 复用,
  // 每次 render 的闭包各自捕获当次 rows,只绑一次会拿到过期数据。
  // 测试的 echarts mock 没有 on/off,做了能力探测。
  if (typeof c.on === 'function' && typeof c.off === 'function') {
    c.off('click');
    c.on('click', (p: { componentType?: string; dataIndex?: number }) => {
      if (p?.componentType !== 'series') return;
      const row: DailyTrendRow | undefined = list[p.dataIndex ?? -1];
      if (row?.date) emit('drill', row.date);
    });
  }
}

onMounted(render);
watch(() => props.rows, render);
// 切换手机/桌面宽度时用不同的 option 重绘
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
