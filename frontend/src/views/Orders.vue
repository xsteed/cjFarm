<template>
  <div class="page-card">
    <div class="toolbar">
      <div class="toolbar-left">
        <t-input v-model="query.orderNo" placeholder="订单号 / 短号后6位" clearable style="width: 180px" />
        <t-input v-model="query.tableNo" placeholder="桌号" clearable style="width: 120px" />
        <t-select v-model="query.orderStatus" placeholder="订单状态" clearable style="width: 140px">
          <t-option v-for="(v, k) in ORDER_STATUS" :key="k" :value="String(k)" :label="v.label" />
        </t-select>
        <t-select v-model="query.payStatus" placeholder="支付状态" clearable style="width: 140px">
          <t-option value="0" label="未支付" />
          <t-option value="1" label="已支付" />
        </t-select>
        <t-select v-model="query.settleFilter" placeholder="结算方式" clearable style="width: 150px">
          <t-option value="normal" label="正常收款" />
          <t-option value="free" label="免单" />
          <t-option value="creditPending" label="挂账待收" />
          <t-option value="creditSettled" label="挂账已结" />
        </t-select>
        <t-button theme="primary" @click="onSearch">查询</t-button>
      </div>
      <t-button theme="primary" @click="load">
        <template #icon><refresh-icon /></template>
        刷新
      </t-button>
    </div>

    <!-- 窄屏:表格列宽合计 900px+(操作列独占 290px),只能横向拖;改为卡片流 -->
    <div v-if="isMobile" class="m-list">
      <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无订单' }}</div>
      <div v-for="row in list" :key="row.orderId" class="mcard">
        <div class="mcard-hd">
          <div style="min-width: 0">
            <span class="mcard-no">#{{ row.shortNo || row.orderNo.slice(-6) }}</span>
            <t-tag v-if="row.pendingUrge" theme="danger" variant="light" size="small" class="urgent-mini">催菜</t-tag>
            <div class="mcard-sub">{{ row.tableNo }} 号桌 · {{ row.personCount }} 人 · {{ row.createTime }}</div>
          </div>
          <t-tag :theme="ORDER_STATUS[row.orderStatus]?.theme" variant="light">{{ ORDER_STATUS[row.orderStatus]?.label }}</t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">应收金额</div>
            <div class="v">
              <span class="money">¥{{ $money(row.totalAmount) }}</span>
              <span v-if="row.settleType === 'free'" class="settle-flag free">免单</span>
              <span v-else-if="row.settleType === 'credit'" class="settle-flag credit">
                {{ row.creditStatus === 1 ? '挂账' : '挂账已结' }}
              </span>
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">支付状态</div>
            <div class="v">
              <t-tag v-if="row.refundAmount > 0" :theme="row.refundAmount >= row.totalAmount ? 'danger' : 'warning'" variant="light">
                {{ row.refundAmount >= row.totalAmount ? '已退款' : '部分退款' }} ¥{{ $money(row.refundAmount) }}
              </t-tag>
              <t-tag v-else :theme="payTheme(row)" variant="light">{{ payText(row) }}</t-tag>
            </div>
          </div>
        </div>

        <div class="mcard-ft">
          <t-button theme="primary" variant="text" size="small" @click="openDetail(row)">详情</t-button>
          <t-button v-if="row.pendingUrge && canOperate" theme="danger" variant="text" size="small" @click="onHandleUrge(row)">已催办</t-button>
          <t-button v-if="row.orderStatus === 1 && canOperate" theme="warning" variant="text" size="small" @click="setStatus(row, 2)">制作</t-button>
          <t-button v-if="row.orderStatus === 2 && canOperate" theme="warning" variant="text" size="small" @click="setStatus(row, 3)">上菜</t-button>
          <t-button
            v-if="[1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canSettle"
            theme="success" variant="text" size="small" @click="openSettle(row, 'settle')"
          >结算</t-button>
          <t-button v-if="[1, 2, 3].includes(row.orderStatus) && canCancel" theme="danger" variant="text" size="small" @click="onCancel(row)">取消</t-button>
          <t-button v-if="canRefund(row)" theme="danger" variant="text" size="small" @click="openRefund(row)">退款</t-button>
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
      <template #orderNo="{ row }">
        <span class="ono" :title="'完整订单号 ' + row.orderNo">{{ row.shortNo || row.orderNo.slice(-6) }}</span>
        <t-tag v-if="row.pendingUrge" theme="danger" variant="light" size="small" style="margin-left: 6px">催菜</t-tag>
      </template>
      <template #orderStatus="{ row }">
        <t-tag :theme="ORDER_STATUS[row.orderStatus]?.theme" variant="light">{{ ORDER_STATUS[row.orderStatus]?.label }}</t-tag>
      </template>
      <template #payStatus="{ row }">
        <t-tag v-if="row.refundAmount > 0" :theme="row.refundAmount >= row.totalAmount ? 'danger' : 'warning'" variant="light">
          {{ row.refundAmount >= row.totalAmount ? '已退款' : '部分退款' }} ¥{{ $money(row.refundAmount) }}
        </t-tag>
        <t-tag v-else :theme="payTheme(row)" variant="light">{{ payText(row) }}</t-tag>
      </template>
      <template #dishAmount="{ row }"><span class="money">¥{{ $money(row.dishAmount) }}</span></template>
      <template #seatFee="{ row }"><span class="money">¥{{ $money(row.seatFee) }}</span></template>
      <template #totalAmount="{ row }">
        <span class="money">¥{{ $money(row.totalAmount) }}</span>
        <span v-if="row.settleType === 'free'" class="settle-flag free">免单</span>
        <span v-else-if="row.settleType === 'credit'" class="settle-flag credit">
          {{ row.creditStatus === 1 ? '挂账' : '挂账已结' }}
        </span>
      </template>
      <template #op="{ row }">
        <t-space>
          <t-button theme="primary" variant="text" size="small" @click="openDetail(row)">详情</t-button>
          <t-button v-if="row.pendingUrge && canOperate" theme="danger" variant="text" size="small" @click="onHandleUrge(row)">已催办</t-button>
          <t-button v-if="row.orderStatus === 1 && canOperate" theme="warning" variant="text" size="small" @click="setStatus(row, 2)">制作</t-button>
          <t-button v-if="row.orderStatus === 2 && canOperate" theme="warning" variant="text" size="small" @click="setStatus(row, 3)">上菜</t-button>
          <t-button
            v-if="[1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canSettle"
            theme="success" variant="text" size="small" @click="openSettle(row, 'settle')"
          >结算</t-button>
          <t-button v-if="[1, 2, 3].includes(row.orderStatus) && canCancel" theme="danger" variant="text" size="small" @click="onCancel(row)">取消</t-button>
          <t-button v-if="canRefund(row)" theme="danger" variant="text" size="small" @click="openRefund(row)">退款</t-button>
        </t-space>
      </template>
    </t-table>

    <!-- 订单详情：与「桌台管理」页点击桌台共用同一个组件，收款/退款/改单等子弹窗都在组件内，
         避免两处各写一套后长期跑偏。列表行的快捷按钮通过 ref 调用组件暴露的方法。 -->
    <OrderDetailDialog
      ref="detailRef"
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
      @changed="load"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { listOrders, changeOrderStatus, cancelOrder, handleUrge, ORDER_STATUS } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'
import OrderDetailDialog from '../components/OrderDetailDialog.vue'

// ---- 权限位:每个写操作按钮都对应后端一个权限点(见 docs/user-permission-design.md 4.3)。
// 前端隐藏只是体验优化,后端 RequirePerm 仍会 403。
const canOperate = computed(() => hasPerm('order:operate'))       // 制作/上菜/已催办
const canSettle = computed(() => hasPerm('order:settle'))         // 收款/结账(弹窗在详情组件内)
const canCancel = computed(() => hasPerm('order:cancel'))         // 取消订单
const canRefundOperate = computed(() => hasPerm('refund:operate')) // 发起退款/同步退款状态

const list = ref([])
const loading = ref(false)
const query = reactive({ orderNo: '', tableNo: '', orderStatus: '', payStatus: '', settleFilter: '' })
const pagination = reactive({ current: 1, pageSize: 10, total: 0 })
const { isMobile } = useIsMobile()

// 详情弹窗已抽成共享组件 components/OrderDetailDialog.vue ——
// 详情展示、收款/结算、挂账核销、退款、手动改单全在组件内实现；
// 列表行的「结算」「退款」快捷按钮通过 detailRef 调用组件暴露的方法，
// 保证每种操作全局只有一份代码(桌台管理页点击桌台也复用同一个组件)。
const detailRef = ref(null)
const detailVisible = ref(false)
const detailOrderId = ref(0)

// 结算状态展示:未支付 / 已支付 / 免单 / 挂账待收 / 挂账已结
function payText(row) {
  if (!row || row.payStatus !== 1) return '未支付'
  if (row.settleType === 'free') return '免单'
  if (row.settleType === 'credit') {
    return row.creditStatus === 1
      ? `挂账待收 ¥${(row.creditAmount || 0).toFixed(2)}`
      : `挂账已结（${row.payType || '-'}）`
  }
  return `已支付（${row.payType || '-'}）`
}

function payTheme(row) {
  if (!row || row.payStatus !== 1) return 'default'
  if (row.settleType === 'free') return 'warning'
  if (row.settleType === 'credit') return row.creditStatus === 1 ? 'warning' : 'success'
  return 'success'
}

const columns = computed(() => {
  const cols = [
    { colKey: 'orderNo', title: '订单号', width: 165 },
    { colKey: 'tableNo', title: '桌号', width: 70 },
    { colKey: 'personCount', title: '人数', width: 70 },
    { colKey: 'dishAmount', title: '菜品金额', width: 100 },
    { colKey: 'seatFee', title: '餐位费', width: 90 },
    { colKey: 'totalAmount', title: '应收金额', width: 110 },
    { colKey: 'orderStatus', title: '订单状态', width: 100 },
    { colKey: 'payStatus', title: '支付状态', width: 150 },
    { colKey: 'createTime', title: '下单时间', width: 170 }
  ]
  // 操作列需容纳「详情/已催办/制作/结算/取消」最多 5 个按钮,过窄会被裁切;
  // 只读账号只剩「详情」,宽度收窄避免大片空白。
  const actions = (canOperate.value ? 1 : 0) + (canSettle.value ? 2 : 0) + (canCancel.value ? 1 : 0) + (canRefundOperate.value ? 1 : 0)
  cols.push({ colKey: 'op', title: '操作', width: actions === 0 ? 90 : 290 })
  return cols
})

// canRefund 仅在线支付(微信/支付宝)且已支付、未全额退款的订单可发起在线退款。
// 另需 refund:operate —— 本期该权限只授予超级管理员。
function canRefund(row) {
  return canRefundOperate.value && row && row.payStatus === 1 &&
    ['wxpay', 'alipay'].includes(row.payChannel) &&
    (row.refundAmount || 0) < row.totalAmount
}

async function load() {
  loading.value = true
  try {
    const params = { pageNum: pagination.current, pageSize: pagination.pageSize }
    if (query.orderNo) params.orderNo = query.orderNo
    if (query.tableNo) params.tableNo = query.tableNo
    if (query.orderStatus) params.orderStatus = query.orderStatus
    if (query.payStatus) params.payStatus = query.payStatus
    // 结算方式筛选:挂账需区分待收 / 已结
    if (query.settleFilter === 'creditPending') {
      params.settleType = 'credit'
      params.creditStatus = 1
    } else if (query.settleFilter === 'creditSettled') {
      params.settleType = 'credit'
      params.creditStatus = 2
    } else if (query.settleFilter) {
      params.settleType = query.settleFilter
    }
    const res = await listOrders(params)
    list.value = res.rows
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function onSearch() {
  pagination.current = 1
  load()
}

function onPageChange(page) {
  pagination.current = page.current
  pagination.pageSize = page.pageSize
  load()
}

// openDetail 打开详情弹窗:只给组件 orderId,详情与退款记录由组件自行拉取。
function openDetail(row) {
  detailOrderId.value = row.orderId || row
  detailVisible.value = true
}

// afterRowAction 列表行操作后的收尾:详情弹窗开着就顺带刷新它(组件内部会 emit changed
// 触发本页 load()),否则直接刷列表,避免同一操作触发两次列表请求。
async function afterRowAction(orderId) {
  if (detailVisible.value && detailRef.value) {
    await detailRef.value.refresh(orderId)
  } else {
    load()
  }
}

async function setStatus(row, status) {
  await changeOrderStatus({ orderId: row.orderId, orderStatus: status })
  MessagePlugin.success('操作成功')
  await afterRowAction(row.orderId)
}

// onHandleUrge 消解该订单全部待处理催菜(已加急/已解释),角标随之消失。
async function onHandleUrge(row) {
  await handleUrge({ orderId: row.orderId })
  MessagePlugin.success('催菜已标记为已处理')
  await afterRowAction(row.orderId)
}

// 列表行的「结算」/「退款」直接复用详情组件里的弹窗:
// openSettle mode=pay 仅标记收款,mode=settle 可选择免单/挂账并完成订单。
function openSettle(row, mode) {
  detailRef.value?.openSettle(row, mode)
}

function openRefund(row) {
  detailRef.value?.openRefund(row)
}

function onCancel(row) {
  DialogPlugin.confirm({
    header: '确认取消',
    body: `确认取消订单【${row.orderNo}】？`,
    onConfirm: async () => {
      await cancelOrder({ orderId: row.orderId, cancelReason: '商家取消' })
      MessagePlugin.success('订单已取消')
      await afterRowAction(row.orderId)
    }
  })
}

onMounted(load)
</script>

<style scoped>
/* 列表里的免单 / 挂账角标 */
.settle-flag {
  display: inline-block;
  margin-left: 6px;
  font-size: 10.5px;
  padding: 1px 6px;
  border-radius: 999px;
  font-weight: 600;
  vertical-align: 1px;
}
.settle-flag.free {
  background: #fff3e0;
  color: #d48a00;
}
.settle-flag.credit {
  background: #e8f0ff;
  color: #2b6cd4;
}

/* 短号：列表统一展示可口头报读的短号，避免收银喊单时报错流水号 */
.ono {
  font-weight: 600;
  color: var(--ink, #1a1a1a);
  letter-spacing: 0.4px;
}

/* 移动端卡片里的催菜角标:贴住短号右侧,不另起一行 */
.urgent-mini {
  margin-left: 6px;
  vertical-align: 1px;
}
</style>
