<template>
  <div>
    <div class="stat-grid">
      <div class="stat-card">
        <div class="stat-label">今日营业额</div>
        <div class="stat-value money">¥{{ $money(summary.todayAmount) }}</div>
        <div class="stat-sub">今日 {{ summary.todayOrderCount || 0 }} 单已完成</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">本月营业额</div>
        <div class="stat-value money">¥{{ $money(summary.monthAmount) }}</div>
        <div class="stat-sub">本月 {{ summary.monthOrderCount || 0 }} 单</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">今日到店</div>
        <div class="stat-value">{{ summary.todayGuestCount || 0 }}</div>
        <div class="stat-sub">人次</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">客单价（今日）</div>
        <div class="stat-value money">¥{{ $money(summary.todayAvgAmount) }}</div>
        <div class="stat-sub">今日订单 {{ summary.todayOrderCount || 0 }} 单</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">今日免单（让利）</div>
        <div class="stat-value money free">¥{{ $money(summary.todayFreeAmount) }}</div>
        <div class="stat-sub">免单不计入营业额</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">挂账待收</div>
        <div class="stat-value money credit">¥{{ $money(summary.creditPendingAmount) }}</div>
        <div class="stat-sub">
          {{ summary.creditPendingCount || 0 }} 笔未结清 ·
          <router-link to="/dining/credit">去核销 →</router-link>
        </div>
      </div>
    </div>

    <div class="page-card" style="margin-top: 16px">
      <div class="chart-title">近 7 日营业额趋势</div>
      <div ref="dailyRef" style="height: 300px"></div>
    </div>

    <div class="page-card" style="margin-top: 16px">
      <div class="chart-title">近 12 个月营业额</div>
      <div ref="monthlyRef" style="height: 300px"></div>
    </div>

    <div class="page-card" style="margin-top: 16px">
      <div class="chart-title">菜品销量排行 TOP 10</div>
      <div ref="rankRef" style="height: 320px"></div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import * as echarts from 'echarts'
import { getReportSummary, getDailyTrend, getMonthlyTrend, getDishRank } from '../api'

const summary = ref({})
const dailyRef = ref(null)
const monthlyRef = ref(null)
const rankRef = ref(null)
const charts = []

function initChart(el, option) {
  const chart = echarts.init(el)
  chart.setOption(option)
  charts.push(chart)
  return chart
}

function resize() {
  charts.forEach((c) => c.resize())
}

async function load() {
  summary.value = await getReportSummary()

  const daily = await getDailyTrend({ days: 7 })
  initChart(dailyRef.value, {
    tooltip: { trigger: 'axis' },
    grid: { left: 50, right: 20, top: 30, bottom: 30 },
    xAxis: { type: 'category', data: daily.map((d) => d.date) },
    yAxis: { type: 'value' },
    series: [
      {
        name: '营业额',
        type: 'line',
        smooth: true,
        data: daily.map((d) => d.amount),
        itemStyle: { color: '#FF6B35' },
        lineStyle: { color: '#FF6B35', width: 3 },
        areaStyle: { color: 'rgba(255,107,53,0.12)' }
      }
    ]
  })

  const monthly = await getMonthlyTrend()
  initChart(monthlyRef.value, {
    tooltip: { trigger: 'axis' },
    grid: { left: 60, right: 20, top: 30, bottom: 30 },
    xAxis: { type: 'category', data: monthly.map((m) => m.month) },
    yAxis: { type: 'value' },
    series: [
      {
        name: '营业额',
        type: 'bar',
        data: monthly.map((m) => m.amount),
        itemStyle: {
          borderRadius: [4, 4, 0, 0],
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: '#FF6B35' },
              { offset: 1, color: '#FFB08A' }
            ]
          }
        }
      }
    ]
  })

  const rank = await getDishRank({ limit: 10 })
  initChart(rankRef.value, {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: 100, right: 40, top: 20, bottom: 30 },
    xAxis: { type: 'value' },
    yAxis: { type: 'category', data: rank.map((r) => r.dishName).reverse() },
    series: [
      {
        name: '销量',
        type: 'bar',
        data: rank.map((r) => r.quantity).reverse(),
        itemStyle: {
          borderRadius: [0, 4, 4, 0],
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 1, y2: 0,
            colorStops: [
              { offset: 0, color: '#FF8A50' },
              { offset: 1, color: '#F0481F' }
            ]
          }
        },
        label: { show: true, position: 'right' }
      }
    ]
  })
}

onMounted(() => {
  load()
  window.addEventListener('resize', resize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resize)
  charts.forEach((c) => c.dispose())
})
</script>

<style scoped>
.stat-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
}
.stat-card {
  background: #fff;
  border-radius: 14px;
  padding: 18px 20px;
  box-shadow: var(--shadow-1);
}
.stat-label {
  font-size: 13px;
  color: var(--ink-3);
  margin-bottom: 10px;
}
.stat-value {
  font-size: 26px;
  font-weight: 800;
  color: var(--ink);
}
.stat-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-top: 6px;
}
.stat-value.free {
  color: #d48a00;
}
.stat-value.credit {
  color: #2b6cd4;
}
.stat-sub a {
  color: var(--brand-deep);
  text-decoration: none;
}
.stat-sub a:hover {
  text-decoration: underline;
}
.chart-title {
  font-size: 15px;
  font-weight: 700;
  margin-bottom: 12px;
  color: var(--ink);
}
@media (max-width: 900px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
