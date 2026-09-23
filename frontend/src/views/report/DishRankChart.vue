<template>
  <div class="page-card chart-card">
    <div class="chart-head">
      <div>
        <div class="chart-title">菜品排行 TOP 10</div>
        <div class="chart-sub">统计口径为全部历史订单（不含已取消）</div>
      </div>
      <RadioGroup
        :value="sort"
        variant="default-filled"
        size="small"
        @change="onSortChange"
      >
        <RadioButton value="qty"> 按销量 </RadioButton>
        <RadioButton value="amount"> 按销售额 </RadioButton>
      </RadioGroup>
    </div>
    <div
      ref="el"
      class="chart-canvas rank"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { RadioButton, RadioGroup } from 'tdesign-vue-next';
import type { DishRankRow } from '../../types/entities';
import { chart, EMPTY_OPTION, type EChartOption } from '../../composables/useEChart';
import { AXIS_COLOR, AXIS_LINE, SPLIT_LINE, BRAND_DEEP, axisYuan, yuan, pickTooltipParam } from './reportShared';

const props = defineProps<{
  rows: DishRankRow[];
  sort: 'qty' | 'amount';
}>();

const emit = defineEmits<{
  'update:sort': [value: 'qty' | 'amount'];
  change: [value: 'qty' | 'amount'];
}>();

const el = ref<HTMLElement | null>(null);

function onSortChange(value: string | number | boolean): void {
  const sort: 'qty' | 'amount' = value === 'amount' ? 'amount' : 'qty';
  emit('update:sort', sort);
  emit('change', sort);
}

function render(): void {
  const c = chart('rank', el.value);
  if (!c) return;
  c.clear();
  // 横向条形图从下往上画，反转一次让第 1 名排在最上面
  const list = (props.rows || []).slice().reverse();
  if (!list.length) {
    c.setOption(EMPTY_OPTION);
    return;
  }
  const byAmount = props.sort === 'amount';
  const values = list.map(r => Number((byAmount ? r.amount : r.quantity) || 0));
  const option: EChartOption = {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: ps => {
        const p = pickTooltipParam(ps);
        const row: DishRankRow = list[p.dataIndex] || {};
        return `${row.dishName}<br/>销量 <b>${row.quantity || 0}</b><br/>销售额 <b>¥${yuan(row.amount)}</b>`;
      }
    },
    grid: { left: 8, right: 56, top: 10, bottom: 4, containLabel: true },
    xAxis: {
      type: 'value',
      minInterval: byAmount ? undefined : 1,
      splitLine: { lineStyle: { color: SPLIT_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, formatter: byAmount ? axisYuan : undefined }
    },
    yAxis: {
      type: 'category',
      data: list.map(r => r.dishName as string),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: '#4A4A4A', fontSize: 12, width: 92, overflow: 'truncate' }
    },
    series: [
      {
        name: byAmount ? '销售额' : '销量',
        type: 'bar',
        barMaxWidth: 16,
        data: values,
        itemStyle: {
          borderRadius: [0, 6, 6, 0],
          color: {
            type: 'linear',
            x: 0,
            y: 0,
            x2: 1,
            y2: 0,
            colorStops: [
              { offset: 0, color: '#FF8A50' },
              { offset: 1, color: BRAND_DEEP }
            ]
          }
        },
        label: {
          show: true,
          position: 'right',
          color: AXIS_COLOR,
          fontSize: 11,
          // 金额标签同样做「万」收敛，避免 5 位数把右侧留白撑爆
          formatter: byAmount ? p => '¥' + axisYuan(p.value) : undefined
        }
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

.chart-canvas.rank {
  height: 340px;
}

@media (width <= 767px) {
  .chart-canvas {
    height: 260px;
  }

  .chart-canvas.rank {
    height: 320px;
  }
}
</style>
