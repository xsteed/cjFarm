<template>
  <div>
    <!-- 顶部统计来自 report/summary(report:view)。收银员有 credit:view 但没有 report:view,
         照旧调用会稳定 403,所以整块按权限降级 —— 列表本身仍可正常使用。 -->
    <div v-if="canReport" class="stat-grid">
      <div class="stat-card acc">
        <div class="stat-label">挂账待收总额</div>
        <div class="stat-value money">¥{{ $money(pendingTotal) }}</div>
        <div class="stat-sub">共 {{ pendingCount || 0 }} 笔未结清</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">今日新增挂账</div>
        <div class="stat-value money">¥{{ $money(summary.todayCreditAmount) }}</div>
        <div class="stat-sub">今日记账金额</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">今日挂账回款</div>
        <div class="stat-value money">¥{{ $money(summary.todayCreditSettledAmount) }}</div>
        <div class="stat-sub">核销 {{ summary.todayCreditSettledCount || 0 }} 笔</div>
      </div>
    </div>

    <div class="page-card" :style="canReport ? 'margin-top: 16px' : ''">
      <div class="toolbar">
        <div class="toolbar-left">
          <t-radio-group v-model="tab" variant="default-filled" @change="onTabChange">
            <t-radio-button value="pending">待收款</t-radio-button>
            <t-radio-button value="settled">已结清</t-radio-button>
            <t-radio-button value="all">全部挂账</t-radio-button>
          </t-radio-group>
          <t-input v-model="keyword" placeholder="挂账人 / 订单号" clearable style="width: 200px" @enter="onSearch" />
          <t-button theme="primary" @click="onSearch">查询</t-button>
        </div>
        <t-button theme="primary" @click="load">
          <template #icon><refresh-icon /></template>
          刷新
        </t-button>
      </div>

      <!-- 窄屏:8 列表格合计 1060px 只能横向拖;改为卡片流 -->
      <div v-if="isMobile" class="m-list">
        <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无挂账记录' }}</div>
        <div v-for="row in list" :key="row.orderId" class="mcard">
          <div class="mcard-hd">
            <div style="min-width: 0">
              <span class="mcard-no">#{{ row.shortNo || (row.orderNo || '').slice(-6) }}</span>
              <div class="mcard-sub">{{ row.tableNo }} 号桌 · {{ row.settleRemark || '未填写挂账人' }}</div>
            </div>
            <t-tag :theme="row.creditStatus === 1 ? 'warning' : 'success'" variant="light">
              {{ row.creditStatus === 1 ? '待收款' : '已结清' }}
            </t-tag>
          </div>

          <div class="mcard-grid">
            <div class="mcard-cell">
              <div class="k">挂账金额</div>
              <div class="v"><span class="money">¥{{ $money(row.creditAmount) }}</span></div>
            </div>
            <div class="mcard-cell">
              <div class="k">挂账时间</div>
              <div class="v">{{ row.settleTime || '-' }}</div>
            </div>
            <div v-if="row.creditStatus !== 1" class="mcard-cell">
              <div class="k">核销时间</div>
              <div class="v">{{ row.creditSettleTime || '-' }}</div>
            </div>
          </div>

          <div class="mcard-ft">
            <t-button v-if="row.creditStatus === 1 && canSettle" theme="primary" variant="text" size="small" @click="onSettle(row)">核销收款</t-button>
            <t-button theme="default" variant="text" size="small" @click="openDetail(row)">明细</t-button>
          </div>
        </div>

        <div v-if="pagination.total > pagination.pageSize" class="m-pager">
          <t-pagination
            :current="pagination.current"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            @change="onPageChange"
          />
        </div>
      </div>

      <t-table v-else :data="list" :columns="columns" row-key="orderId" :loading="loading" :pagination="pagination" @page-change="onPageChange">
        <template #creditStatus="{ row }">
          <t-tag :theme="row.creditStatus === 1 ? 'warning' : 'success'" variant="light">
            {{ row.creditStatus === 1 ? '待收款' : '已结清' }}
          </t-tag>
        </template>
        <template #creditAmount="{ row }">
          <span class="money">¥{{ $money(row.creditAmount) }}</span>
        </template>
        <template #settleRemark="{ row }">
          {{ row.settleRemark || '-' }}
        </template>
        <template #op="{ row }">
          <t-space>
            <t-button v-if="row.creditStatus === 1 && canSettle" theme="primary" variant="text" size="small" @click="onSettle(row)">核销收款</t-button>
            <t-button theme="default" variant="text" size="small" @click="openDetail(row)">明细</t-button>
          </t-space>
        </template>
        <template #empty>
          <div class="empty">暂无挂账记录</div>
        </template>
      </t-table>
    </div>

    <!-- 挂账核销 -->
    <t-dialog
      v-model:visible="settleVisible"
      header="挂账核销"
      width="420px"
      :confirm-btn="{ content: '确认收款', theme: 'primary', loading: submitting }"
      @confirm="submitSettle"
    >
      <div class="settle-box">
        <div class="st-row">
          <span class="st-label">待收金额</span>
          <span class="st-amount">¥{{ $money(form.amount) }}</span>
        </div>
        <div v-if="form.remark" class="st-hint">挂账人：{{ form.remark }}</div>
        <div class="st-field">
          <div class="st-label">收款方式</div>
          <t-select v-model="form.payType" style="width: 100%">
            <t-option v-for="p in payTypes" :key="p" :value="p" :label="p" />
          </t-select>
        </div>
        <div class="st-hint">核销后该笔金额计入营业额。</div>
      </div>
    </t-dialog>

    <!-- 订单明细 -->
    <t-dialog v-model:visible="detailVisible" header="挂账订单明细" width="680px" :footer="false">
      <div v-if="detail">
        <t-descriptions :column="isMobile ? 1 : 2" bordered>
          <t-descriptions-item label="订单号">{{ detail.orderNo }}</t-descriptions-item>
          <t-descriptions-item label="桌台">{{ detail.tableName }}（{{ detail.tableNo }}号）</t-descriptions-item>
          <t-descriptions-item label="挂账金额">¥{{ $money(detail.creditAmount) }}</t-descriptions-item>
          <t-descriptions-item label="状态">{{ detail.creditStatus === 1 ? '待收款' : '已结清' }}</t-descriptions-item>
          <t-descriptions-item label="挂账时间">{{ detail.settleTime || detail.createTime }}</t-descriptions-item>
          <t-descriptions-item label="操作人">{{ detail.settleOperator || '-' }}</t-descriptions-item>
          <t-descriptions-item label="挂账人/备注">{{ detail.settleRemark || '-' }}</t-descriptions-item>
          <t-descriptions-item label="核销时间">{{ detail.creditSettleTime || '-' }}</t-descriptions-item>
        </t-descriptions>
        <t-table :data="detail.items" :columns="itemCols" row-key="itemId" size="small" style="margin-top: 16px">
          <template #price="{ row }">¥{{ $money(row.price) }}</template>
          <template #amount="{ row }"><span class="money">¥{{ $money(row.amount) }}</span></template>
        </t-table>
        <div class="amount-summary">
          <div>应收金额：<span class="money">¥{{ $money(detail.totalAmount) }}</span></div>
          <div class="total">实收金额：<span class="money">¥{{ $money(detail.paidAmount) }}</span></div>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { listOrders, getOrder, settleCreditOrder, getReportSummary } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'

const list = ref([])
const loading = ref(false)
const tab = ref('pending')
const keyword = ref('')
const pagination = reactive({ current: 1, pageSize: 10, total: 0 })
const summary = ref({})
const { isMobile } = useIsMobile()

const settleVisible = ref(false)
const submitting = ref(false)
const form = reactive({ orderId: 0, amount: 0, payType: '现金', remark: '' })
const payTypes = ['现金', '微信', '支付宝', '银行卡', '其他']

const detailVisible = ref(false)
const detail = ref(null)

const columns = [
  { colKey: 'orderNo', title: '订单号', width: 180 },
  { colKey: 'tableNo', title: '桌号', width: 80 },
  { colKey: 'settleRemark', title: '挂账人/备注' },
  { colKey: 'creditAmount', title: '挂账金额', width: 120 },
  { colKey: 'settleTime', title: '挂账时间', width: 170 },
  { colKey: 'creditStatus', title: '状态', width: 100 },
  { colKey: 'creditSettleTime', title: '核销时间', width: 170 },
  { colKey: 'op', title: '操作', width: 140 }
]

// 明细表在弹窗里只有 ~255px 宽(92vw 再扣内边距),窄屏只保留「菜品/数量/小计」三列
const itemCols = computed(() => isMobile.value
  ? [
      { colKey: 'dishName', title: '菜品', ellipsis: true },
      { colKey: 'quantity', title: '数量', width: 46 },
      { colKey: 'amount', title: '小计', width: 70 }
    ]
  : [
      { colKey: 'dishName', title: '菜品' },
      { colKey: 'specName', title: '规格', width: 120 },
      { colKey: 'price', title: '单价', width: 90 },
      { colKey: 'quantity', title: '数量', width: 70 },
      { colKey: 'amount', title: '小计', width: 90 }
    ])

// 待收统计:仅统计当前列表为待收时的总额;用于顶部 KPI 展示。
const pendingTotal = computed(() => summary.value.creditPendingAmount || 0)
const pendingCount = computed(() => summary.value.creditPendingCount || 0)

// 核销是写操作(会改订单状态与收款方式),只有 credit:settle 才显示按钮;
// 「明细」是纯读取(order:view),所有能看到本页的人都保留。
const canSettle = computed(() => hasPerm('credit:settle'))
// 顶部三张统计卡的数据来自 report/summary,需 report:view;没有就不发这个请求。
const canReport = computed(() => hasPerm('report:view'))

async function load() {
  loading.value = true
  try {
    const params = { pageNum: pagination.current, pageSize: pagination.pageSize, settleType: 'credit' }
    if (tab.value === 'pending') params.creditStatus = 1
    if (tab.value === 'settled') params.creditStatus = 2
    const res = await listOrders(params)
    const rows = res.rows || []
    // 关键词在前端做二次过滤(挂账人为自由文本,后端仅支持订单号/桌号精确匹配)
    const kw = keyword.value.trim()
    list.value = kw
      ? rows.filter((r) => (r.settleRemark || '').includes(kw) || (r.orderNo || '').includes(kw))
      : rows
    pagination.total = res.total
    if (canReport.value) summary.value = await getReportSummary()
  } finally {
    loading.value = false
  }
}

function onSearch() {
  pagination.current = 1
  load()
}

function onTabChange() {
  pagination.current = 1
  load()
}

function onPageChange(page) {
  pagination.current = page.current
  pagination.pageSize = page.pageSize
  load()
}

function onSettle(row) {
  form.orderId = row.orderId
  form.amount = row.creditAmount || row.totalAmount
  form.payType = '现金'
  form.remark = row.settleRemark || ''
  settleVisible.value = true
}

async function submitSettle() {
  submitting.value = true
  try {
    const res = await settleCreditOrder({ orderId: form.orderId, payType: form.payType })
    MessagePlugin.success(res?.msg || '挂账已核销')
    settleVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

async function openDetail(row) {
  detail.value = await getOrder(row.orderId)
  detailVisible.value = true
}

onMounted(load)
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
.stat-card.acc {
  background: var(--grad-brand);
  border: none;
}
.stat-card.acc .stat-label,
.stat-card.acc .stat-sub {
  color: rgba(255, 255, 255, 0.85);
}
.stat-card.acc .stat-value {
  color: #fff;
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
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.empty {
  padding: 30px 0;
  text-align: center;
  color: var(--ink-4);
  font-size: 13px;
}
.settle-box {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.st-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 12px 14px;
  border-radius: 10px;
  background: #fff7f2;
}
.st-label {
  font-size: 13px;
  color: #666;
}
.st-amount {
  font-size: 22px;
  font-weight: 800;
  color: #f0481f;
}
.st-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.st-hint {
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}
.amount-summary {
  margin-top: 16px;
  text-align: right;
  font-size: 14px;
  color: #666;
}
.amount-summary .total {
  font-size: 16px;
  font-weight: 600;
  margin-top: 6px;
}
@media (max-width: 900px) {
  .stat-grid {
    grid-template-columns: 1fr;
  }
}
</style>
