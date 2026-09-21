<!-- 订单详情弹窗（共享组件）
     桌台管理页「点击桌台」与订单管理页「详情」共用同一份实现，
     避免两处各写一套详情/收款/退款逻辑后长期跑偏。

     版式按收银场景定稿：标题「xx号桌 · 订单详情」→ 两行关键信息 →
     菜品明细(菜品+规格小标签 / 备注 / 单价 / 数量 / 小计) → 右对齐金额行 →
     两排操作按钮（主操作 + 补打），条件性操作（完成/核销挂账/撤销结算/退款）另起一行。

     收款、挂账核销、退款三个子弹窗也收在本组件内，并通过 defineExpose 暴露
     openSettle/openRefund/openCredit 给「订单管理」列表行的快捷按钮复用 ——
     这样可以保证每种操作全局只有一份实现。 -->
<template>
  <t-dialog
    :visible="visible"
    :header="dialogTitle"
    width="880px"
    :footer="false"
    @update:visible="(v) => emit('update:visible', v)"
    @closed="onClosed"
  >
    <div v-if="detail" class="od">
      <!-- 关键信息：订单号/状态、桌号/人数/下单时间 -->
      <div class="od-info">
        <div class="od-row">
          <span class="od-k">订单号：</span>
          <span class="od-v mono">{{ detail.orderNo }}</span>
          <span class="od-k od-gap">状态：</span>
          <span class="od-v">
            <t-tag :theme="ORDER_STATUS[detail.orderStatus]?.theme" variant="light">
              {{ ORDER_STATUS[detail.orderStatus]?.label }}
            </t-tag>
          </span>
        </div>
        <div class="od-row">
          <span class="od-k">桌号：</span>
          <span class="od-v">{{ detail.tableNo }}号</span>
          <span class="od-k od-gap">人数：</span>
          <span class="od-v">{{ detail.personCount }}人</span>
          <span class="od-k od-gap">下单时间：</span>
          <span class="od-v">{{ detail.createTime }}</span>
        </div>
      </div>

      <div v-if="detail.pendingUrge" class="urge-banner">
        <span>顾客已催菜，请优先跟进</span>
        <t-button theme="danger" size="small" @click="onHandleUrge">标记已处理</t-button>
      </div>

      <t-table :data="detail.items" :columns="itemCols" row-key="itemId" size="small" :max-height="isMobile ? 220 : 320">
        <template #dishName="{ row }">
          <span class="od-dish">{{ row.dishName }}</span>
          <span v-if="row.specName" class="od-spec">{{ row.specName }}</span>
        </template>
        <template #itemRemark="{ row }">
          <span class="od-iremark">{{ row.itemRemark || '-' }}</span>
        </template>
        <template #price="{ row }">¥{{ $money(row.price) }}</template>
        <template #amount="{ row }"><span class="money">¥{{ $money(row.amount) }}</span></template>
      </t-table>

      <div class="od-total">
        <span class="od-t-item"><span class="od-k">菜品</span><b>¥{{ $money(detail.dishAmount) }}</b></span>
        <span class="od-t-item"><span class="od-k">餐位费</span><b>¥{{ $money(detail.seatFee) }}</b></span>
        <span v-if="detail.discountAmount > 0" class="od-t-item">
          <span class="od-k">优惠</span><b>-¥{{ $money(detail.discountAmount) }}</b>
        </span>
        <span class="od-t-item od-t-sum"><span class="od-k">合计</span><b>¥{{ $money(detail.totalAmount) }}</b></span>
      </div>

      <div v-if="detail.payStatus === 1" class="od-settle-line">
        <span>结算：{{ payText(detail) }}</span>
        <span>实收 <b>¥{{ $money(detail.paidAmount) }}</b></span>
        <span v-if="detail.refundAmount > 0" class="od-refunded">已退款 ¥{{ $money(detail.refundAmount) }}</span>
      </div>

      <div v-if="detail.settleType === 'free' && detail.settleRemark" class="od-note">
        免单原因：{{ detail.settleRemark }}（操作人 {{ detail.settleOperator || '-' }}）
      </div>
      <div v-else-if="detail.settleType === 'credit' && detail.settleRemark" class="od-note">
        挂账人/备注：{{ detail.settleRemark }}（操作人 {{ detail.settleOperator || '-' }}）
      </div>
      <div v-if="detail.orderRemark" class="od-note">备注：{{ detail.orderRemark }}</div>
      <div v-if="detail.cancelReason" class="od-note">取消原因：{{ detail.cancelReason }}</div>

      <!-- 主操作：改单 / 推进状态 / 收款 / 结账 -->
      <div class="od-actions">
        <button type="button" class="ab ab-edit" :disabled="!canEditOrder" @click="openEdit">手动改单</button>
        <button type="button" class="ab ab-make" :disabled="!canMake" @click="setStatus(2)">开始制作</button>
        <button type="button" class="ab ab-serve" :disabled="!canServe" @click="setStatus(3)">上齐/用餐中</button>
        <button type="button" class="ab ab-pay" :disabled="!canPay" @click="openSettle(detail, 'pay')">确认收款</button>
        <button type="button" class="ab ab-settle" :disabled="!canPay" @click="openSettle(detail, 'settle')">收款并结束用餐</button>
      </div>

      <!-- 补打：两排按钮是两条**互不相干**的通道，别合并。
           第一行走打印服务出真实热敏纸（小票机在线时用这个）；
           第二行 (A4) 走浏览器打印页，由人手动出纸或另存 PDF ——
           适用于小票机坏了、需要留档、挂账要顾客签字等场景。 -->
      <div class="od-actions">
        <button type="button" class="ab ab-re" :disabled="!canReprint" @click="onReprint('kitchen')">重打厨房单</button>
        <button type="button" class="ab ab-re" :disabled="!canReprint" @click="onReprint('guest')">重打小票</button>
      </div>
      <div class="od-actions">
        <button type="button" class="ab ab-a4" :disabled="!detail" @click="onPrintA4('kitchen')">重打厨房单(A4)</button>
        <button type="button" class="ab ab-a4" :disabled="!detail" @click="onPrintA4('guest')">重打小票(A4)</button>
        <button type="button" class="ab ab-cancel" :disabled="!canCancelOrder" @click="onCancel">取消订单</button>
      </div>

      <!-- 条件性操作：只有满足条件才出现，平时不占位置 -->
      <div v-if="hasExtraActions" class="od-actions od-actions-extra">
        <span class="od-extra-label">更多操作</span>
        <button v-if="canFinish" type="button" class="ab ab-more" @click="onFinish">完成订单</button>
        <button v-if="canCreditSettleOrder" type="button" class="ab ab-more" @click="openCredit(detail)">核销挂账</button>
        <button v-if="canCancelSettleOrder" type="button" class="ab ab-more" @click="onSettleCancel">撤销结算</button>
        <button v-if="canRefundOrder" type="button" class="ab ab-more" @click="openRefund(detail)">退款</button>
      </div>

      <div v-if="detail.payStatus === 1 && canRefundView" class="refund-block">
        <div class="refund-title">
          退款记录
          <t-button size="small" variant="text" @click="loadRefunds(detail.orderId)">刷新</t-button>
        </div>
        <t-table v-if="refunds.length" :data="refunds" :columns="refundCols" row-key="refundId" size="small">
          <template #amount="{ row }"><span class="money">¥{{ $money(row.amount) }}</span></template>
          <template #status="{ row }">
            <t-tag :theme="REFUND_STATUS[row.status]?.theme" variant="light">{{ REFUND_STATUS[row.status]?.label }}</t-tag>
          </template>
          <template #op="{ row }">
            <t-button v-if="row.status === 0" size="small" variant="text" @click="syncRefund(row)">同步状态</t-button>
          </template>
        </t-table>
        <div v-else class="refund-empty">暂无退款记录</div>
      </div>
    </div>
  </t-dialog>

  <!-- 手动改单 -->
  <OrderEditDialog v-model:visible="editVisible" :order="detail" @saved="refresh" />

  <!-- 退款 -->
  <t-dialog v-model:visible="refundVisible" header="订单退款" width="480px" :confirm-btn="confirmBtn" @confirm="submitRefund">
    <t-form v-if="refundForm" label-width="90px">
      <t-form-item label="订单号">{{ refundForm.orderNo }}</t-form-item>
      <t-form-item label="可退金额"><span class="money">¥{{ $money(refundForm.maxAmount) }}</span></t-form-item>
      <t-form-item label="退款金额">
        <t-input-number v-model="refundForm.amount" :min="0.01" :max="refundForm.maxAmount" :step="0.01" :decimal-places="2" theme="normal" />
        <t-button size="small" variant="text" @click="refundForm.amount = refundForm.maxAmount">全额</t-button>
      </t-form-item>
      <t-form-item label="退款原因">
        <t-input v-model="refundForm.reason" placeholder="如：顾客取消、菜品售罄" />
      </t-form-item>
    </t-form>
  </t-dialog>

  <!-- 收款 / 结账：正常收款、免单、挂账三种结算方式 -->
  <t-dialog
    v-model:visible="settleVisible"
    :header="settleForm.mode === 'pay' ? '订单收款' : '订单结算'"
    width="480px"
    :confirm-btn="{ content: '确认', theme: 'primary', loading: settleSubmitting }"
    @confirm="submitSettle"
  >
    <div class="settle-box">
      <div class="st-row">
        <span class="st-label">应收金额</span>
        <span class="st-amount">¥{{ $money(settleForm.total) }}</span>
      </div>

      <div v-if="settleForm.mode === 'settle'" class="st-field">
        <div class="st-label">结算方式</div>
        <div class="st-types">
          <div
            v-for="t in settleTypes"
            :key="t.value"
            class="st-type"
            :class="{ on: settleForm.settleType === t.value }"
            @click="settleForm.settleType = t.value"
          >
            <div class="st-t">{{ t.label }}</div>
            <div class="st-d">{{ t.desc }}</div>
          </div>
        </div>
      </div>

      <div v-if="settleForm.settleType === 'normal'" class="st-field">
        <div class="st-label">支付方式</div>
        <t-select v-model="settleForm.payType" style="width: 100%">
          <t-option v-for="p in payTypes" :key="p" :value="p" :label="p" />
        </t-select>
      </div>

      <div v-if="settleForm.settleType === 'free'" class="st-field">
        <div class="st-label">免单原因 <span class="req">*</span></div>
        <t-input v-model="settleForm.remark" placeholder="如：老客户招待 / 菜品问题补偿" :maxlength="60" />
        <div class="st-hint">免单后实收 ¥0，不计入营业额，仅统计为让利金额。</div>
      </div>

      <div v-if="settleForm.settleType === 'credit'" class="st-field">
        <div class="st-label">挂账人 / 备注 <span class="req">*</span></div>
        <t-input v-model="settleForm.remark" placeholder="如：王总（公司月结）" :maxlength="60" />
        <div class="st-hint">挂账后订单即可完成、桌台释放，欠款记入挂账，收款后再核销。</div>
      </div>
    </div>
  </t-dialog>

  <!-- 挂账核销 -->
  <t-dialog
    v-model:visible="creditVisible"
    header="挂账核销"
    width="420px"
    :confirm-btn="{ content: '确认收款', theme: 'primary', loading: creditSubmitting }"
    @confirm="submitCredit"
  >
    <div class="settle-box">
      <div class="st-row">
        <span class="st-label">待收金额</span>
        <span class="st-amount">¥{{ $money(creditForm.amount) }}</span>
      </div>
      <div class="st-field">
        <div class="st-label">收款方式</div>
        <t-select v-model="creditForm.payType" style="width: 100%">
          <t-option v-for="p in payTypes" :key="p" :value="p" :label="p" />
        </t-select>
      </div>
      <div class="st-hint">核销后该笔金额计入营业额。</div>
    </div>
  </t-dialog>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import {
  getOrder, changeOrderStatus, payOrder, settleOrder, settleCreditOrder, cancelSettle,
  finishOrder, cancelOrder, refundPay, queryRefund, listRefunds, handleUrge, reprintOrder,
  getPublicConfig, ORDER_STATUS, REFUND_STATUS
} from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'
import { printOrderA4 } from '../utils/printA4'
import OrderEditDialog from './OrderEditDialog.vue'

const props = defineProps({
  visible: { type: Boolean, default: false },
  orderId: { type: Number, default: 0 }
})
const emit = defineEmits(['update:visible', 'changed'])

// ---- 权限位：前端隐藏/禁用只是体验优化，后端 RequirePerm 仍会 403 ----
const canOperate = computed(() => hasPerm('order:operate')) // 制作 / 上菜 / 完成 / 已催办
const canSettle = computed(() => hasPerm('order:settle')) // 收款 / 结账 / 撤销结算
const canCancelPerm = computed(() => hasPerm('order:cancel')) // 取消订单
const canEditPerm = computed(() => hasPerm('order:edit')) // 手动改单
const canCreditSettle = computed(() => hasPerm('credit:settle')) // 核销挂账
const canRefundOperate = computed(() => hasPerm('refund:operate'))
const canRefundView = computed(() => hasPerm('refund:view'))
const canReprint = computed(() => hasPerm('printer:edit')) // 补打会真的出纸

const { isMobile } = useIsMobile()
const detail = ref(null)
const refunds = ref([])

// 进行中状态：1 已下单 / 2 制作中 / 3 已上齐(用餐中)
const active = computed(() => !!detail.value && [1, 2, 3].includes(detail.value.orderStatus))
const unpaid = computed(() => !!detail.value && detail.value.payStatus === 0)

const dialogTitle = computed(() =>
  detail.value?.tableNo ? `${detail.value.tableNo}号桌 · 订单详情` : '订单详情'
)

// ---- 按钮可用性（截图里按钮恒显示，不适用时置灰，避免布局跳动）----
const canEditOrder = computed(() => canEditPerm.value && active.value && unpaid.value)
const canMake = computed(() => canOperate.value && detail.value?.orderStatus === 1)
const canServe = computed(() => canOperate.value && detail.value?.orderStatus === 2)
const canPay = computed(() => canSettle.value && active.value && unpaid.value)
const canCancelOrder = computed(() => canCancelPerm.value && active.value)
const canFinish = computed(() => canOperate.value && detail.value?.orderStatus === 3 && detail.value?.payStatus === 1)
const canCreditSettleOrder = computed(
  () => canCreditSettle.value && detail.value?.settleType === 'credit' && detail.value?.creditStatus === 1
)
const canCancelSettleOrder = computed(
  () =>
    canSettle.value &&
    detail.value?.payStatus === 1 &&
    detail.value?.settleType !== 'normal' &&
    !(detail.value?.settleType === 'credit' && detail.value?.creditStatus === 2)
)
// 仅在线支付(微信/支付宝)且已支付、未全额退款的订单可发起在线退款
const canRefundOrder = computed(
  () =>
    canRefundOperate.value &&
    detail.value?.payStatus === 1 &&
    ['wxpay', 'alipay'].includes(detail.value?.payChannel) &&
    (detail.value?.refundAmount || 0) < detail.value?.totalAmount
)
const hasExtraActions = computed(
  () => canFinish.value || canCreditSettleOrder.value || canCancelSettleOrder.value || canRefundOrder.value
)

// 明细表：窄屏只留「菜品/数量/小计」，规格与单价在收银看单时不是必需，
// 少了它们才不需要横向拖动。
const itemCols = computed(() =>
  isMobile.value
    ? [
        { colKey: 'dishName', title: '菜品', ellipsis: true },
        { colKey: 'quantity', title: '数量', width: 46 },
        { colKey: 'amount', title: '小计', width: 74 }
      ]
    : [
        { colKey: 'dishName', title: '菜品' },
        { colKey: 'itemRemark', title: '备注', width: 160 },
        { colKey: 'price', title: '单价', width: 100 },
        { colKey: 'quantity', title: '数量', width: 70 },
        { colKey: 'amount', title: '小计', width: 100 }
      ]
)

const refundCols = computed(() => {
  const cols = [
    { colKey: 'refundNo', title: '退款单号', ellipsis: true },
    { colKey: 'channel', title: '渠道', width: 90 },
    { colKey: 'amount', title: '金额', width: 90 },
    { colKey: 'status', title: '状态', width: 90 },
    { colKey: 'reason', title: '原因', ellipsis: true },
    { colKey: 'createTime', title: '时间', width: 160 }
  ]
  if (canRefundOperate.value) cols.push({ colKey: 'op', title: '操作', width: 90 })
  return cols
})

// 结算状态展示：未支付 / 已支付 / 免单 / 挂账待收 / 挂账已结
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

async function loadRefunds(orderId) {
  try {
    refunds.value = await listRefunds(orderId)
  } catch (e) {
    refunds.value = []
  }
}

/** 重新拉取详情与退款记录，并通知父级刷新列表。
 *  传 orderId 可强制刷新指定订单（列表页快捷操作时，详情显示的未必就是被操作的那一单）。 */
async function refresh(orderId) {
  const id = orderId || detail.value?.orderId
  if (id) {
    detail.value = await getOrder(id)
    await loadRefunds(id)
  }
  emit('changed')
}

/** 打开弹窗/切换订单：父级只需给出 orderId，详情与退款记录由本组件拉取。 */
async function open(orderId) {
  const id = orderId || props.orderId
  if (!id) return
  detail.value = await getOrder(id)
  await loadRefunds(id)
}

watch(
  () => props.visible,
  async (v) => {
    if (v) await open(props.orderId)
  }
)

// 弹窗已打开时切换订单（如桌台页连续点不同桌台）也要重新拉取
watch(
  () => props.orderId,
  async (id) => {
    if (props.visible && id) await open(id)
  }
)

function onClosed() {
  detail.value = null
  refunds.value = []
}

async function setStatus(status) {
  await changeOrderStatus({ orderId: detail.value.orderId, orderStatus: status })
  MessagePlugin.success('操作成功')
  await refresh()
}

// onHandleUrge 消解该订单全部待处理催菜(已加急/已解释)，角标随之消失
async function onHandleUrge() {
  await handleUrge({ orderId: detail.value.orderId })
  MessagePlugin.success('催菜已标记为已处理')
  await refresh()
}

// ---- 手动改单 ----
const editVisible = ref(false)
function openEdit() {
  editVisible.value = true
}

// ---- 收款 / 结算 ----
const settleVisible = ref(false)
const settleSubmitting = ref(false)
const settleForm = reactive({ orderId: 0, total: 0, mode: 'settle', settleType: 'normal', payType: '现金', remark: '' })
const payTypes = ['现金', '微信', '支付宝', '银行卡', '其他']
const settleTypes = [
  { value: 'normal', label: '正常收款', desc: '当场收款，计入营业额' },
  { value: 'free', label: '免单', desc: '商家让利，实收 0' },
  { value: 'credit', label: '挂账', desc: '先记账，后续核销' }
]

// mode=pay 仅标记收款；mode=settle 可选免单/挂账，并完成订单、释放桌台
function openSettle(row, mode) {
  settleForm.orderId = row.orderId
  settleForm.total = row.totalAmount
  settleForm.mode = mode
  settleForm.settleType = 'normal'
  settleForm.payType = '现金'
  settleForm.remark = ''
  settleVisible.value = true
}

async function submitSettle() {
  if (settleForm.settleType === 'free' && !settleForm.remark.trim()) {
    MessagePlugin.warning('免单必须填写原因')
    return
  }
  if (settleForm.settleType === 'credit' && !settleForm.remark.trim()) {
    MessagePlugin.warning('挂账必须填写挂账单位/事由')
    return
  }
  settleSubmitting.value = true
  try {
    const id = settleForm.orderId
    if (settleForm.mode === 'pay') {
      await payOrder({ orderId: id, payType: settleForm.payType })
      MessagePlugin.success('收款成功')
    } else if (settleForm.settleType === 'free') {
      await settleOrder({ orderId: id, settleType: 'free', settleRemark: settleForm.remark.trim() })
      MessagePlugin.success('免单成功，订单已完成')
    } else if (settleForm.settleType === 'credit') {
      await settleOrder({ orderId: id, settleType: 'credit', settleRemark: settleForm.remark.trim() })
      MessagePlugin.success('已记账挂账，订单已完成')
    } else {
      await settleOrder({ orderId: id, settleType: 'normal', payType: settleForm.payType })
      MessagePlugin.success('结账成功')
    }
    settleVisible.value = false
    await refresh()
  } finally {
    settleSubmitting.value = false
  }
}

// ---- 挂账核销 ----
const creditVisible = ref(false)
const creditSubmitting = ref(false)
const creditForm = reactive({ orderId: 0, amount: 0, payType: '现金' })

function openCredit(row) {
  creditForm.orderId = row.orderId
  creditForm.amount = row.creditAmount || row.totalAmount
  creditForm.payType = '现金'
  creditVisible.value = true
}

async function submitCredit() {
  creditSubmitting.value = true
  try {
    const res = await settleCreditOrder({ orderId: creditForm.orderId, payType: creditForm.payType })
    MessagePlugin.success(res?.msg || '挂账已核销')
    creditVisible.value = false
    await refresh()
  } finally {
    creditSubmitting.value = false
  }
}

// ---- 撤销免单 / 挂账 ----
function onSettleCancel() {
  const row = detail.value
  const name = row.settleType === 'free' ? '免单' : '挂账'
  DialogPlugin.confirm({
    header: '撤销结算',
    body: `确认撤销订单【${row.orderNo}】的${name}？撤销后订单恢复为未支付并回到「已上齐」，需重新结算。`,
    onConfirm: async () => {
      await cancelSettle({ orderId: row.orderId, reason: '撤销' + name })
      MessagePlugin.success('已撤销结算')
      await refresh()
    }
  })
}

function onFinish() {
  DialogPlugin.confirm({
    header: '确认完成',
    body: '确认后订单将归档并计入今日统计，桌台释放为空闲。',
    onConfirm: async () => {
      await finishOrder({ orderId: detail.value.orderId })
      MessagePlugin.success('订单已完成')
      await refresh()
    }
  })
}

function onCancel() {
  DialogPlugin.confirm({
    header: '确认取消',
    body: `确认取消订单【${detail.value.orderNo}】？`,
    onConfirm: async () => {
      await cancelOrder({ orderId: detail.value.orderId, cancelReason: '商家取消' })
      MessagePlugin.success('订单已取消')
      await refresh()
    }
  })
}

// ---- 退款 ----
const refundVisible = ref(false)
const refundForm = ref(null)
// 确认按钮必须用 reactive，否则 loading 变化不会触发视图更新
const confirmBtn = reactive({ content: '确认退款', theme: 'danger', loading: false })

function openRefund(row) {
  const maxAmount = Number((row.totalAmount - (row.refundAmount || 0)).toFixed(2))
  refundForm.value = {
    orderId: row.orderId,
    orderNo: row.orderNo,
    // 默认全额退款；手动改小即为部分退款(组件有最小值，不能靠 0 表示全额)
    amount: maxAmount,
    maxAmount,
    reason: ''
  }
  refundVisible.value = true
}

async function submitRefund() {
  const f = refundForm.value
  if (!f) return
  confirmBtn.loading = true
  try {
    const res = await refundPay({ orderId: f.orderId, amount: f.amount || 0, reason: f.reason })
    MessagePlugin.success(res?.msg || '退款已提交')
    refundVisible.value = false
    await refresh()
  } finally {
    confirmBtn.loading = false
  }
}

async function syncRefund(row) {
  const res = await queryRefund({ refundId: row.refundId })
  MessagePlugin.success(res?.msg || '已同步')
  await refresh()
}

// onReprint 人工补打：纸没装好、打歪了、打印机离线导致没出纸时用。
// 不传 printerId，由后端自动挑一台该单据类型的启用打印机。
async function onReprint(docType) {
  const label = docType === 'kitchen' ? '厨房单' : '食客小票'
  try {
    const res = await reprintOrder({ orderId: detail.value.orderId, docType })
    MessagePlugin.success(res?.msg || `${label}补打指令已发送`)
  } catch (e) {
    /* 失败原因(如「没有启用中的厨房单打印机」)由请求拦截器统一提示 */
  }
}

// ** A4 通道 **：不走打印机，生成浏览器打印页，服务员手动出纸或另存 PDF。
// 与小票机互不干扰，所以即使小票机离线也能出纸质单据。
async function onPrintA4(docType) {
  const ok = await printOrderA4(detail.value, docType, { getPublicConfig })
  if (!ok) MessagePlugin.warning('打印页被浏览器拦截，请允许本站点弹出窗口后重试')
}

// 供「订单管理」列表行的快捷按钮调用：每种操作全局只有一份实现
defineExpose({ open, refresh, openSettle, openRefund, openCredit, openEdit })
</script>

<style scoped>
.od {
  display: flex;
  flex-direction: column;
}
.od-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--line);
}
.od-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  font-size: 14px;
  color: var(--ink);
}
.od-k {
  color: var(--ink-3);
  font-size: 13px;
}
.od-v {
  font-weight: 600;
}
.od-gap {
  margin-left: 20px;
}
.mono {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 13px;
  letter-spacing: 0.2px;
}
.od-dish {
  font-weight: 600;
}
/* 规格小标签：截图里规格挂在菜名右侧，而不是单占一列 */
.od-spec {
  display: inline-block;
  margin-left: 8px;
  font-size: 11px;
  line-height: 16px;
  padding: 0 6px;
  border-radius: 4px;
  background: #f2f3f5;
  color: #7c7f85;
  vertical-align: 1px;
}
.od-iremark {
  font-size: 13px;
  color: var(--ink-2);
}
.od-total {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: baseline;
  gap: 8px 22px;
  margin-top: 14px;
  font-size: 14px;
}
.od-t-item .od-k {
  margin-right: 6px;
}
.od-t-sum b {
  font-size: 19px;
  font-weight: 800;
  color: var(--brand-deep);
}
.od-settle-line {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px 20px;
  margin-top: 8px;
  font-size: 13px;
  color: var(--ink-2);
}
.od-settle-line b {
  color: var(--ink);
}
.od-refunded {
  color: #d4380d;
}
.od-note {
  margin-top: 10px;
  padding: 8px 12px;
  background: #fff7e6;
  border-radius: 6px;
  font-size: 13px;
  /* 原来的 var(--brand-deep) #F0481F 落在浅黄底上只有 ~3.4:1，低于 WCAG AA 的 4.5:1，
     收银场景里备注(免单原因/挂账人)又是要看清的重点，这里压深一档。 */
  color: #a8340b;
  font-weight: 600;
}
.urge-banner {
  margin-top: 12px;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--danger-soft, #feeeee);
  color: var(--danger, #f53f3f);
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

/* ---- 操作按钮 ---- */
.od-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}
.od-actions-extra {
  align-items: center;
  margin-top: 10px;
}
.od-extra-label {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--ink-3);
}
.ab {
  flex: 1 1 0;
  min-width: 100px;
  height: 38px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid transparent;
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
  background: #fff;
  transition: filter 0.15s ease, opacity 0.15s ease;
}
.ab:not(:disabled):hover {
  filter: brightness(0.95);
}
.ab:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.ab-edit {
  color: #d97b06;
  border-color: #f3c98a;
  background: #fffaf1;
}
.ab-make {
  color: #fff;
  background: #ff9f26;
}
.ab-serve {
  color: #fff;
  background: #46b662;
}
.ab-pay {
  color: #fff;
  background: #3b8cff;
}
.ab-settle {
  color: #fff;
  background: #f4512e;
}
.ab-re {
  color: var(--ink);
  border-color: #dcdcdc;
}
/* A4 通道：与上面灰色的 ab-re 刻意区分成蓝色，提示「这次不走小票机」 */
.ab-a4 {
  color: #2563eb;
  border-color: #bfd3fe;
  background: #f5f8ff;
}
.ab-a4:not(:disabled):hover {
  border-color: #2563eb;
  background: #eaf0ff;
}
.ab-cancel {
  color: #e34d59;
  border-color: #f7c4c7;
}
.ab-more {
  flex: 0 1 auto;
  min-width: 88px;
  height: 32px;
  font-size: 13px;
  color: var(--ink-2);
  border-color: #dcdcdc;
}

/* ---- 退款记录 ---- */
.refund-block {
  margin-top: 20px;
  padding-top: 12px;
  border-top: 1px dashed #e5e6eb;
}
.refund-title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.refund-empty {
  font-size: 13px;
  color: #999;
}

/* ---- 收款 / 结算 / 核销弹窗 ---- */
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
.req {
  color: #e34d59;
}
.st-types {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.st-type {
  border: 1.5px solid #e5e5e5;
  border-radius: 10px;
  padding: 10px;
  cursor: pointer;
  transition: all 0.15s;
}
.st-type:hover {
  border-color: #ffd5c4;
}
.st-type.on {
  border-color: #ff6b35;
  background: #fff7f2;
}
.st-type .st-t {
  font-size: 13px;
  font-weight: 700;
  color: #333;
}
.st-type.on .st-t {
  color: #f0481f;
}
.st-type .st-d {
  font-size: 11px;
  color: #999;
  margin-top: 4px;
  line-height: 1.4;
}
.st-hint {
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}
/* 手机上三张结算卡片各只剩 ~90px，描述文字会挤成一团。
   结算方式选错等于做错账，这里宁可改成竖排让人点准。 */
@media (max-width: 767px) {
  .st-types {
    grid-template-columns: 1fr;
  }
  .st-type {
    display: flex;
    align-items: baseline;
    gap: 10px;
  }
  .st-type .st-d {
    margin-top: 0;
  }
}
</style>
