<template>
  <div ref="rootRef" class="report">
    <!-- 顶部：报表区间 + 导出 / 刷新 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <t-date-range-picker
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
        <span v-if="updatedAt" class="updated">更新于 {{ updatedAt }}</span>
        <t-button theme="default" :disabled="!dailyRows.length" @click="exportCsv">
          <template #icon><file-icon /></template>
          导出明细
        </t-button>
        <t-button theme="primary" :loading="loading" @click="loadAll">
          <template #icon><refresh-icon /></template>
          刷新
        </t-button>
      </div>
    </div>

    <t-loading :loading="loading" show-overlay>
      <!-- 关键指标：今日营业额为主卡，环比基准取昨日 / 上月同期 -->
      <div class="kpi-grid">
        <div class="kpi hero">
          <div class="hero-label">今日营业额</div>
          <div class="hero-value">¥{{ $money(summary.todayAmount) }}</div>
          <div v-if="deltas.todayAmount !== null" class="hero-delta">
            <span class="badge" :class="deltas.todayAmount >= 0 ? 'up' : 'down'">
              {{ arrow(deltas.todayAmount) }} {{ Math.abs(deltas.todayAmount).toFixed(1) }}%
            </span>
            <span class="hero-delta-label">较昨日</span>
          </div>
          <div class="hero-stats">
            <div class="hs">
              <span class="hs-v">{{ summary.todayOrderCount || 0 }}</span>
              <span class="hs-k">订单</span>
              <em v-if="deltas.todayOrder !== null" class="hs-d" :class="deltas.todayOrder >= 0 ? 'up' : 'down'">
                {{ arrow(deltas.todayOrder) }}{{ Math.abs(deltas.todayOrder).toFixed(0) }}%
              </em>
            </div>
            <div class="hs">
              <span class="hs-v">{{ summary.todayGuestCount || 0 }}</span>
              <span class="hs-k">到店人次</span>
              <em v-if="deltas.todayGuest !== null" class="hs-d" :class="deltas.todayGuest >= 0 ? 'up' : 'down'">
                {{ arrow(deltas.todayGuest) }}{{ Math.abs(deltas.todayGuest).toFixed(0) }}%
              </em>
            </div>
            <div class="hs">
              <span class="hs-v">¥{{ $money(summary.todayAvgAmount) }}</span>
              <span class="hs-k">客单价</span>
            </div>
          </div>
          <div class="hero-date">{{ today }}</div>
        </div>

        <div class="kpi">
          <div class="kpi-label">本月营业额</div>
          <div class="kpi-value money">¥{{ $money(summary.monthAmount) }}</div>
          <div class="kpi-sub">
            本月 {{ summary.monthOrderCount || 0 }} 单 · 客流 {{ summary.monthGuestCount || 0 }} 人次
          </div>
          <div v-if="deltas.monthAmount !== null" class="kpi-delta">
            <span class="badge" :class="deltas.monthAmount >= 0 ? 'up' : 'down'">
              {{ arrow(deltas.monthAmount) }} {{ Math.abs(deltas.monthAmount).toFixed(1) }}%
            </span>
            <span class="kpi-delta-label">较上月同期</span>
          </div>
        </div>

        <div class="kpi">
          <div class="kpi-label">今日免单让利</div>
          <div class="kpi-value free">¥{{ $money(summary.todayFreeAmount) }}</div>
          <div class="kpi-sub">让利额不计入营业额</div>
        </div>

        <div class="kpi">
          <div class="kpi-label">挂账待收</div>
          <div class="kpi-value credit">¥{{ $money(summary.creditPendingAmount) }}</div>
          <div class="kpi-sub">
            {{ summary.creditPendingCount || 0 }} 笔未结清 ·
            <router-link to="/dining/credit">去核销 →</router-link>
          </div>
        </div>

        <div class="kpi">
          <div class="kpi-label">今日挂账回款</div>
          <div class="kpi-value">¥{{ $money(summary.todayCreditSettledAmount) }}</div>
          <div class="kpi-sub">
            核销 {{ summary.todayCreditSettledCount || 0 }} 笔 ·
            新增挂账 ¥{{ $money(summary.todayCreditAmount) }}
          </div>
        </div>
      </div>

      <!-- 运营概览：一眼看清在途、空闲与风险项 -->
      <div class="meta-card">
        <div class="meta-item">
          <span class="mk">在途订单</span>
          <span class="mv">{{ summary.activeOrderCount || 0 }}<em>单</em></span>
        </div>
        <div class="meta-item">
          <span class="mk">空闲桌台</span>
          <span class="mv">{{ summary.freeTableCount || 0 }}<em>/ {{ summary.tableCount || 0 }} 桌</em></span>
        </div>
        <div class="meta-item">
          <span class="mk">翻台率</span>
          <span class="mv">{{ turnover }}<em>次/桌</em></span>
        </div>
        <div class="meta-item">
          <span class="mk">今日退款</span>
          <span class="mv" :class="{ warn: (summary.todayRefundAmount || 0) > 0 }">
            ¥{{ $money(summary.todayRefundAmount) }}
          </span>
        </div>
        <div class="meta-item">
          <span class="mk">今日取消</span>
          <span class="mv" :class="{ warn: (summary.todayCancelCount || 0) > 0 }">
            {{ summary.todayCancelCount || 0 }}<em>单</em>
          </span>
        </div>
      </div>

      <!-- 趋势 -->
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">营业额与订单趋势</div>
            <div class="chart-sub">
              区间合计 <b>¥{{ $money(rangeTotal.amount) }}</b> ·
              {{ rangeTotal.orders }} 单 · 客流 {{ rangeTotal.guests }} 人次
            </div>
          </div>
        </div>
        <div ref="dailyRef" class="chart-canvas"></div>
      </div>

      <!-- 时段分布 + 结算构成 -->
      <div class="duo">
        <div class="page-card chart-card">
          <div class="chart-head">
            <div>
              <div class="chart-title">经营时段分布</div>
              <div class="chart-sub">柱为订单数，线为营业额；用于排班与备货</div>
            </div>
          </div>
          <div ref="hourlyRef" class="chart-canvas"></div>
        </div>

        <div class="page-card chart-card">
          <div class="chart-head">
            <div>
              <div class="chart-title">结算方式构成</div>
              <div class="chart-sub">按订单应收金额，合计 ¥{{ $money(mixTotal) }}</div>
            </div>
          </div>
          <div ref="mixRef" class="chart-canvas"></div>
        </div>
      </div>

      <!-- 月度 -->
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">近 12 个月营业额</div>
            <div class="chart-sub">悬停可看当月订单数与客流</div>
          </div>
        </div>
        <div ref="monthlyRef" class="chart-canvas"></div>
      </div>

      <!-- 菜品排行 -->
      <div class="page-card chart-card">
        <div class="chart-head">
          <div>
            <div class="chart-title">菜品排行 TOP 10</div>
            <div class="chart-sub">统计口径为全部历史订单（不含已取消）</div>
          </div>
          <t-radio-group v-model="rankSort" variant="default-filled" size="small" @change="loadRank">
            <t-radio-button value="qty">按销量</t-radio-button>
            <t-radio-button value="amount">按销售额</t-radio-button>
          </t-radio-group>
        </div>
        <div ref="rankRef" class="chart-canvas rank"></div>
      </div>
    </t-loading>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import * as echarts from 'echarts'
import {
  getDailyTrend,
  getDishRank,
  getHourlyReport,
  getMonthlyTrend,
  getReportSummary,
  getSettleMix
} from '../api'

const AXIS_COLOR = '#8C8C8C'
const AXIS_LINE = '#EEEEEE'
const SPLIT_LINE = '#F5F5F5'
const BRAND = '#FF6B35'
const BRAND_DEEP = '#F0481F'
const GREEN = '#1FC97E'
const BLUE = '#2B6CD4'
const AMBER = '#FFB020'

// 无数据时不渲染空坐标轴，给一句居中提示，比光秃秃的网格更清楚
const EMPTY_OPTION = {
  title: {
    text: '暂无数据',
    left: 'center',
    top: 'middle',
    textStyle: { color: '#C7C7C7', fontSize: 13, fontWeight: 400 }
  }
}

/**
 * 金额轴标签做「万」单位收敛，否则 5 位数会把左侧留白撑得很宽，
 * 手机窄屏下几乎挤没了绘图区。
 */
function axisYuan(v) {
  const n = Number(v || 0)
  if (n >= 10000) return (n / 10000).toFixed(n >= 100000 ? 0 : 1) + '万'
  return String(n)
}

const yuan = (v) => Number(v || 0).toFixed(2)

// ---------- 日期工具 ----------
function fmtDay(d) {
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function shiftDays(n) {
  const d = new Date()
  d.setDate(d.getDate() + n)
  return d
}

const rootRef = ref(null)
const dailyRef = ref(null)
const hourlyRef = ref(null)
const mixRef = ref(null)
const monthlyRef = ref(null)
const rankRef = ref(null)

const summary = ref({})
const dailyRows = ref([])
const hourlyRows = ref([])
const mixRows = ref([])
const loading = ref(false)
const updatedAt = ref('')
const rankSort = ref('qty')

// 报表区间默认近 7 天。presets 用函数是为了每次展开面板时都按「当前日期」重算，
// 而不是在组件初始化那一刻就把相对日期算死。
const range = ref([fmtDay(shiftDays(-6)), fmtDay(new Date())])
const rangePresets = {
  近7天: () => [fmtDay(shiftDays(-6)), fmtDay(new Date())],
  近30天: () => [fmtDay(shiftDays(-29)), fmtDay(new Date())],
  近90天: () => [fmtDay(shiftDays(-89)), fmtDay(new Date())],
  本月: () => {
    const d = new Date()
    return [fmtDay(new Date(d.getFullYear(), d.getMonth(), 1)), fmtDay(d)]
  },
  上月: () => {
    const d = new Date()
    // new Date(y, m, 0) 是「上个月最后一天」，省去自己处理大小月
    return [fmtDay(new Date(d.getFullYear(), d.getMonth() - 1, 1)), fmtDay(new Date(d.getFullYear(), d.getMonth(), 0))]
  }
}

const today = computed(() => fmtDay(new Date()))

/** 区间入参：正常返回 start/end，控件被清空时回退到近 7 天。 */
function rangeParams() {
  const r = range.value
  if (Array.isArray(r) && r.length === 2 && r[0] && r[1]) {
    return { start: r[0], end: r[1] }
  }
  return { days: 7 }
}

// ---------- 环比 ----------
/** 涨跌幅(%),基准为 0 或缺失时返回 null(不显示「+∞%」这种噪音)。 */
function pct(cur, prev) {
  const c = Number(cur || 0)
  const p = Number(prev || 0)
  if (p <= 0) return null
  return ((c - p) / p) * 100
}
const arrow = (v) => (v >= 0 ? '↑' : '↓')

const deltas = computed(() => ({
  todayAmount: pct(summary.value.todayAmount, summary.value.yesterdayAmount),
  todayOrder: pct(summary.value.todayOrderCount, summary.value.yesterdayOrderCount),
  todayGuest: pct(summary.value.todayGuestCount, summary.value.yesterdayGuestCount),
  // 本月至今对比「上月同期」，而不是上月整月 —— 后者在月中必然显示负增长，没有参考价值
  monthAmount: pct(summary.value.monthAmount, summary.value.lastMonthSamePeriodAmount)
}))

const turnover = computed(() => {
  const tables = Number(summary.value.tableCount || 0)
  if (!tables) return '—'
  return (Number(summary.value.todayOrderCount || 0) / tables).toFixed(2)
})

const rangeTotal = computed(() => {
  let amount = 0
  let orders = 0
  let guests = 0
  for (const d of dailyRows.value) {
    amount += Number(d.amount || 0)
    orders += Number(d.orderCount || 0)
    guests += Number(d.guestCount || 0)
  }
  return { amount, orders, guests }
})

const mixTotal = computed(() =>
  (mixRows.value || []).reduce((sum, m) => sum + Number(m.amount || 0), 0)
)

// ---------- 图表实例 ----------
// 按 key 复用：首次 init，之后只 clear + setOption，避免每次刷新都新建实例造成泄漏
const charts = {}
function chart(key, el) {
  if (!el) return null
  if (!charts[key]) charts[key] = echarts.init(el)
  return charts[key]
}

function renderDaily(rows) {
  const c = chart('daily', dailyRef.value)
  if (!c) return
  c.clear()
  const list = rows || []
  if (!list.length) {
    c.setOption(EMPTY_OPTION)
    return
  }
  const showSymbol = list.length <= 30
  c.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross', label: { backgroundColor: BRAND_DEEP } },
      formatter: (ps) => {
        const row = list[ps[0].dataIndex] || {}
        return `${ps[0].axisValue}<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单<br/>客流 <b>${row.guestCount || 0}</b> 人次`
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
    grid: { left: 8, right: 8, top: 44, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: list.map((d) => d.date),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, hideOverlap: true }
    },
    yAxis: [
      {
        type: 'value',
        name: '营业额',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        splitLine: { lineStyle: { color: SPLIT_LINE } },
        axisLabel: { color: AXIS_COLOR, fontSize: 11, formatter: axisYuan }
      },
      {
        type: 'value',
        name: '订单',
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
        data: list.map((d) => Number(d.amount || 0)),
        itemStyle: { color: BRAND },
        lineStyle: { color: BRAND, width: 3 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
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
        data: list.map((d) => Number(d.orderCount || 0)),
        itemStyle: { color: GREEN },
        lineStyle: { color: GREEN, width: 2, type: 'dashed' }
      }
    ]
  })
}

function renderHourly(rows) {
  const c = chart('hourly', hourlyRef.value)
  if (!c) return
  c.clear()
  const list = rows || []
  if (!list.length || !list.some((h) => (h.orderCount || 0) > 0)) {
    c.setOption(EMPTY_OPTION)
    return
  }
  c.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (ps) => {
        const row = list[ps[0].dataIndex] || {}
        return `${row.hour}:00 - ${row.hour}:59<br/>订单 <b>${row.orderCount || 0}</b> 单<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>客流 <b>${row.guestCount || 0}</b> 人次`
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
      data: list.map((h) => h.hour),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: AXIS_LINE } },
      axisLabel: { color: AXIS_COLOR, fontSize: 11, interval: 1 }
    },
    yAxis: [
      {
        type: 'value',
        name: '订单',
        nameTextStyle: { color: '#C7C7C7', fontSize: 11 },
        minInterval: 1,
        splitLine: { lineStyle: { color: SPLIT_LINE } },
        axisLabel: { color: AXIS_COLOR, fontSize: 11 }
      },
      {
        type: 'value',
        name: '营业额',
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
        data: list.map((h) => Number(h.orderCount || 0)),
        itemStyle: {
          borderRadius: [4, 4, 0, 0],
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
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
        data: list.map((h) => Number(h.amount || 0)),
        itemStyle: { color: BLUE },
        lineStyle: { color: BLUE, width: 2 }
      }
    ]
  })
}

function renderMix(rows) {
  const c = chart('mix', mixRef.value)
  if (!c) return
  c.clear()
  const list = (rows || []).filter((m) => Number(m.amount || 0) > 0)
  if (!list.length) {
    c.setOption(EMPTY_OPTION)
    return
  }
  const colorOf = { normal: BRAND, free: AMBER, credit: BLUE, other: '#C7C7C7' }
  c.setOption({
    tooltip: {
      trigger: 'item',
      formatter: (p) => {
        const row = list[p.dataIndex] || {}
        return `${row.label}<br/>订单金额 <b>¥${yuan(row.amount)}</b><br/>实收 <b>¥${yuan(row.paidAmount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单`
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
        data: list.map((m) => ({
          name: m.label,
          value: Number(m.amount || 0),
          itemStyle: { color: colorOf[m.settleType] || '#C7C7C7' }
        }))
      }
    ]
  })
}

function renderMonthly(rows) {
  const c = chart('monthly', monthlyRef.value)
  if (!c) return
  c.clear()
  const list = rows || []
  if (!list.length) {
    c.setOption(EMPTY_OPTION)
    return
  }
  c.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (ps) => {
        const row = list[ps[0].dataIndex] || {}
        return `${ps[0].axisValue}<br/>营业额 <b>¥${yuan(row.amount)}</b><br/>订单 <b>${row.orderCount || 0}</b> 单<br/>客流 <b>${row.guestCount || 0}</b> 人次`
      }
    },
    grid: { left: 8, right: 8, top: 20, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      data: list.map((m) => m.month),
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
        data: list.map((m) => Number(m.amount || 0)),
        itemStyle: {
          borderRadius: [6, 6, 0, 0],
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: BRAND },
              { offset: 1, color: '#FFB08A' }
            ]
          }
        },
        emphasis: { itemStyle: { color: BRAND_DEEP } }
      }
    ]
  })
}

function renderRank(rows) {
  const c = chart('rank', rankRef.value)
  if (!c) return
  c.clear()
  // 横向条形图从下往上画，反转一次让第 1 名排在最上面
  const list = (rows || []).slice().reverse()
  if (!list.length) {
    c.setOption(EMPTY_OPTION)
    return
  }
  const byAmount = rankSort.value === 'amount'
  const values = list.map((r) => Number((byAmount ? r.amount : r.quantity) || 0))
  c.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (ps) => {
        const row = list[ps[0].dataIndex] || {}
        return `${row.dishName}<br/>销量 <b>${row.quantity || 0}</b><br/>销售额 <b>¥${yuan(row.amount)}</b>`
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
      data: list.map((r) => r.dishName),
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
            x: 0, y: 0, x2: 1, y2: 0,
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
          formatter: byAmount ? (p) => '¥' + axisYuan(p.value) : undefined
        }
      }
    ]
  })
}

// ---------- 数据加载 ----------
function applyDaily(rows) {
  dailyRows.value = rows || []
  renderDaily(dailyRows.value)
}
function applyHourly(rows) {
  hourlyRows.value = rows || []
  renderHourly(hourlyRows.value)
}
function applyMix(rows) {
  mixRows.value = rows || []
  renderMix(mixRows.value)
}

async function loadAll() {
  loading.value = true
  try {
    // 六个接口互不依赖，并行拉取；任一失败由 axios 拦截器统一提示
    const [s, d, h, m, mo, r] = await Promise.all([
      getReportSummary(),
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams()),
      getMonthlyTrend(),
      getDishRank({ limit: 10, sort: rankSort.value })
    ])
    summary.value = s || {}
    applyDaily(d)
    applyHourly(h)
    applyMix(m)
    renderMonthly(mo)
    renderRank(r)
    updatedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } catch {
    // 错误提示已由请求层统一处理，这里只需保证 loading 收尾
  } finally {
    loading.value = false
  }
}

/** 切换区间只重取三个「区间相关」的接口，汇总与月度、排行不受影响。 */
async function loadRange() {
  try {
    const [d, h, m] = await Promise.all([
      getDailyTrend(rangeParams()),
      getHourlyReport(rangeParams()),
      getSettleMix(rangeParams())
    ])
    applyDaily(d)
    applyHourly(h)
    applyMix(m)
  } catch {
    /* 同上 */
  }
}

async function loadRank() {
  try {
    renderRank(await getDishRank({ limit: 10, sort: rankSort.value }))
  } catch {
    /* 同上 */
  }
}

// ---------- 导出 ----------
/** 导出当前区间的逐日明细(带 BOM，Excel 打开中文不乱码)。 */
function exportCsv() {
  const rows = [['日期', '营业额(元)', '订单数', '客流(人次)']]
  for (const d of dailyRows.value) {
    rows.push([d.date, yuan(d.amount), d.orderCount || 0, d.guestCount || 0])
  }
  rows.push(['合计', yuan(rangeTotal.value.amount), rangeTotal.value.orders, rangeTotal.value.guests])

  const csv = rows
    .map((r) => r.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))
    .join('\r\n')
  const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `营业明细_${range.value?.[0] || ''}_${range.value?.[1] || ''}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

// ---------- 生命周期 ----------
/**
 * 统一用 ResizeObserver 监听容器尺寸：窗口缩放、侧栏收起、内容区变化都能覆盖，
 * 比只监听 window.resize 更可靠（桌面端窗口不变但内容宽度变了的情况会漏）。
 */
let ro = null
function resizeAll() {
  Object.values(charts).forEach((c) => c.resize())
}

onMounted(() => {
  loadAll()
  if (typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(resizeAll)
    ro.observe(rootRef.value)
  }
  window.addEventListener('resize', resizeAll)
})

onBeforeUnmount(() => {
  if (ro) ro.disconnect()
  window.removeEventListener('resize', resizeAll)
  Object.values(charts).forEach((c) => c.dispose())
})
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

/* ---------- 指标卡 ---------- */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}
.kpi {
  position: relative;
  background: #fff;
  border-radius: var(--r-lg);
  padding: 18px 20px;
  box-shadow: var(--shadow-1);
}
.kpi-label {
  font-size: 13px;
  color: var(--ink-3);
  margin-bottom: 10px;
}
.kpi-value {
  font-size: 24px;
  font-weight: 800;
  color: var(--ink);
}
.kpi-value.free {
  color: #d48a00;
}
.kpi-value.credit {
  color: #2b6cd4;
}
.kpi-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-top: 6px;
  line-height: 1.5;
}
.kpi-sub a {
  color: var(--brand-deep);
  text-decoration: none;
}
.kpi-sub a:hover {
  text-decoration: underline;
}

/* 今日营业额：品牌渐变主卡，横跨两列压住版面重心 */
.kpi.hero {
  grid-column: span 2;
  background: var(--grad-brand);
  color: #fff;
  box-shadow: 0 10px 24px rgba(240, 72, 31, 0.22);
}
.hero-label {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.85);
}
.hero-value {
  font-size: 34px;
  font-weight: 800;
  line-height: 1.2;
  margin-top: 10px;
  letter-spacing: 0.5px;
}
.hero-delta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}
.hero-delta-label {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.8);
}
.hero-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  margin-top: 16px;
}
.hs {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.hs-v {
  font-size: 17px;
  font-weight: 700;
}
.hs-k {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.8);
}
.hs-d {
  font-style: normal;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.9);
}
.hero-date {
  position: absolute;
  top: 18px;
  right: 20px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.8);
}

/* 环比徽标 */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--r-pill);
  font-size: 11.5px;
  font-weight: 700;
  line-height: 1.6;
}
.badge.up {
  background: rgba(31, 201, 126, 0.16);
  color: #0f9d5c;
}
.badge.down {
  background: rgba(245, 63, 63, 0.14);
  color: #d32f2f;
}
/* 渐变主卡上的徽标改用白色半透明底，保证在橙底上依然可读 */
.kpi.hero .badge.up,
.kpi.hero .badge.down {
  background: rgba(255, 255, 255, 0.22);
  color: #fff;
}
.kpi-delta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
}
.kpi-delta-label {
  font-size: 12px;
  color: var(--ink-4);
}

/* ---------- 运营概览条 ---------- */
.meta-card {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
  background: #fff;
  border-radius: var(--r-lg);
  padding: 14px 20px;
  box-shadow: var(--shadow-1);
}
.meta-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.mk {
  font-size: 12px;
  color: var(--ink-3);
}
.mv {
  font-size: 18px;
  font-weight: 700;
  color: var(--ink);
}
.mv em {
  font-style: normal;
  font-size: 12px;
  font-weight: 500;
  color: var(--ink-4);
  margin-left: 3px;
}
.mv.warn {
  color: var(--danger);
}

/* ---------- 图表 ---------- */
.chart-card {
  margin-top: 16px;
}
.duo {
  display: grid;
  grid-template-columns: 1.4fr 1fr;
  gap: 16px;
  margin-top: 16px;
}
.duo .chart-card {
  margin-top: 0;
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

@media (max-width: 1200px) {
  .kpi-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .meta-card {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    row-gap: 16px;
  }
  .duo {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 767px) {
  .toolbar-acts {
    width: 100%;
    justify-content: space-between;
  }
  .kpi-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
  }
  .kpi.hero {
    grid-column: span 1;
    padding: 16px;
  }
  .hero-value {
    font-size: 28px;
  }
  .hero-stats {
    gap: 16px;
  }
  .kpi {
    padding: 14px 16px;
  }
  .meta-card {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    padding: 14px 16px;
  }
  .chart-canvas {
    height: 260px;
  }
  .chart-canvas.rank {
    height: 320px;
  }
}
/* 超窄屏(320~360px):运营概览条 2 列金额数字收紧,避免 ¥ 金额换行 */
@media (max-width: 360px) {
  .meta-card {
    gap: 8px;
    padding: 12px 12px;
  }
  .mv {
    font-size: 16px;
  }
}
</style>
