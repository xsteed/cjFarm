<template>
  <div class="page-card">
    <div class="toolbar">
      <div class="toolbar-left">
        <t-input v-model="query.tableName" placeholder="搜索桌台名称" clearable style="width: 220px" @enter="load" />
        <t-button theme="primary" @click="load">查询</t-button>
      </div>
      <t-space>
        <t-radio-group v-model="viewMode" variant="default">
          <t-radio-button value="table">列表</t-radio-button>
          <t-radio-button value="card">卡片</t-radio-button>
        </t-radio-group>
        <t-button variant="outline" @click="openBatch">
          <template #icon><qrcode-icon /></template>
          批量导出 / 打印
        </t-button>
        <t-button v-if="canEdit" theme="primary" @click="openAdd">
          <template #icon><add-icon /></template>
          新增桌台
        </t-button>
      </t-space>
    </div>

    <!-- 顶部总览:基于全量桌台(忽略分页/搜索)统计,一眼看清当前楼面桌况 -->
    <div class="stat-row">
      <div class="stat">
        <div class="label">桌台总数</div>
        <div class="num">{{ summary.total }}</div>
      </div>
      <div class="stat occ">
        <div class="label">占用中</div>
        <div class="num">{{ summary.occupied }}</div>
      </div>
      <div class="stat idle">
        <div class="label">空闲</div>
        <div class="num">{{ summary.idle }}</div>
      </div>
      <div class="stat">
        <div class="label">占用率</div>
        <div class="num">{{ summary.rate }}<span class="unit">%</span></div>
      </div>
      <div class="stat">
        <div class="label">总容纳人数</div>
        <div class="num">{{ summary.capacity }}<span class="unit">人</span></div>
      </div>
    </div>

    <div v-if="h5BaseWarn" class="h5-warn">
      <b>二维码地址提示：</b>{{ h5BaseWarn }}
    </div>

    <div class="tip-bar">
      桌台二维码由「H5 访问地址 + 点餐码」生成，点餐码在桌台创建时即固定、永不变更，可放心印制成桌牌。
      <br />
      点击「占用」中的桌台行（或窄屏卡片）可直接查看该桌当前订单详情，并在弹窗内改单、制作、收款、补打。
    </div>

    <!-- 卡片视图:桌面/移动端统一,响应式网格铺排,像楼层平面图;
         占用桌台暖橙描边+浅底、空闲绿描边,前台一眼分辨。移动端窄屏仍单列(见 scoped media 查询)。 -->
    <div v-if="showCard" class="tcard-grid">
      <div v-if="!list.length" class="m-empty" style="grid-column: 1 / -1">{{ loading ? '加载中…' : '暂无桌台' }}</div>
      <div
        v-for="row in list"
        :key="row.tableId"
        class="mcard"
        :class="{ 'mcard-tap': tapable(row), 'tcard-occ': row.status === 1, 'tcard-idle': row.status === 0 }"
        @click="onCardClick(row, $event)"
      >
        <div class="mcard-hd">
          <div style="min-width: 0">
            <span class="mcard-no">{{ row.tableNo }} 号桌</span>
            <div class="mcard-sub">{{ row.tableName }}</div>
          </div>
          <t-tag :theme="row.status === 0 ? 'success' : 'warning'" variant="light">
            {{ row.status === 0 ? '空闲' : '占用' }}
          </t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">点餐码</div>
            <div class="v"><span class="code-chip">{{ row.tableCode || '-' }}</span></div>
          </div>
          <div class="mcard-cell">
            <div class="k">容纳人数 / 排序</div>
            <div class="v">{{ row.capacity }} 人 · 第 {{ row.sortOrder }} 位</div>
          </div>
        </div>

        <div class="mcard-ft">
          <t-button v-if="tapable(row)" theme="primary" variant="text" size="small" @click="openTableOrder(row)">详情</t-button>
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button variant="text" size="small" @click="openQr(row)">二维码</t-button>
          <t-button v-if="canEdit" theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </div>
      </div>

      <div v-if="pagination.total > pagination.pageSize" class="m-pager" style="grid-column: 1 / -1">
        <t-pagination
          :current="pagination.current"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          @change="onPageChange"
        />
      </div>
    </div>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="tableId"
      :loading="loading"
      :pagination="pagination"
      :row-class-name="rowClassName"
      @page-change="onPageChange"
      @row-click="onRowClick"
    >
      <template #tableCode="{ row }">
        <span class="code-chip">{{ row.tableCode || '-' }}</span>
      </template>
      <template #status="{ row }">
        <t-tag :theme="row.status === 0 ? 'success' : 'warning'" variant="light">
          {{ row.status === 0 ? '空闲' : '占用' }}
        </t-tag>
      </template>
      <template #op="{ row }">
        <t-space>
          <t-button v-if="tapable(row)" theme="primary" variant="text" size="small" @click="openTableOrder(row)">详情</t-button>
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button variant="text" size="small" @click="openQr(row)">二维码</t-button>
          <t-button v-if="canEdit" theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </t-space>
      </template>
    </t-table>

    <t-dialog v-model:visible="dialogVisible" :header="form.tableId ? '编辑桌台' : '新增桌台'" width="480px" :confirm-btn="{ content: '保存', theme: 'primary' }" @confirm="save">
      <t-form :data="form" label-width="90px">
        <t-form-item label="桌号" name="tableNo">
          <t-input v-model="form.tableNo" placeholder="如 01" />
        </t-form-item>
        <t-form-item label="桌台名称" name="tableName">
          <t-input v-model="form.tableName" placeholder="如 大厅01桌" />
        </t-form-item>
        <t-form-item label="容纳人数" name="capacity">
          <t-input-number v-model="form.capacity" :min="1" :max="50" />
        </t-form-item>
        <t-form-item label="排序" name="sortOrder">
          <t-input-number v-model="form.sortOrder" :min="0" />
        </t-form-item>
      </t-form>
    </t-dialog>

    <!-- 单张桌台二维码 -->
    <t-dialog v-model:visible="qrVisible" :header="qrTitle" width="480px" :footer="false" @close="onQrClose">
      <div class="qr-box">
        <div class="qr-img-wrap">
          <img v-if="qrDataUrl" :src="qrDataUrl" class="qr-img" alt="桌台二维码" />
          <div v-else-if="qrLoading" class="qr-placeholder"><span class="spin"></span>生成中...</div>
          <div v-else class="qr-placeholder error">{{ qrError || '二维码生成失败' }}</div>
        </div>

        <div class="qr-meta">
          <div class="qr-table-name">{{ qrTable?.tableName }}（{{ qrTable?.tableNo }}桌）</div>
          <div class="qr-code-line">
            点餐码 <span class="code-chip">{{ qrTable?.tableCode || '-' }}</span>
            <t-button variant="text" size="small" @click="copyText(qrTable?.tableCode)">复制</t-button>
          </div>
          <div class="qr-url" :title="qrUrl">{{ qrUrl }}</div>
          <div v-if="isLocalhost" class="qr-warn">⚠ 当前为本地地址，手机无法访问。请到「系统配置」把 H5 访问地址改成公网域名或服务器 IP。</div>
        </div>

        <div class="qr-opts">
          <t-checkbox v-model="qrUseLogo" :disabled="!shopLogo">中心显示 Logo</t-checkbox>
          <t-checkbox v-model="qrUseText" :disabled="qrUseLogo">中心显示桌号</t-checkbox>
          <t-select v-model="qrSize" :options="pixelOptions" size="small" style="width: 190px" />
        </div>
        <div v-if="!shopLogo" class="qr-hint">
          尚未配置店铺 Logo，可到「系统配置 → 店铺 Logo」上传，二维码中间即可显示店铺标志。
        </div>

        <div class="qr-actions">
          <t-button theme="primary" :disabled="!qrDataUrl" @click="downloadQr">下载 PNG</t-button>
          <t-button variant="outline" :disabled="!qrDataUrl" @click="printSingle">打印</t-button>
          <t-button variant="outline" @click="copyText(qrUrl)">复制链接</t-button>
        </div>
      </div>
    </t-dialog>

    <!-- 批量导出 / 打印 -->
    <t-dialog v-model:visible="batchVisible" header="批量导出桌台点餐码" width="660px" :footer="false">
      <t-form label-width="104px" label-align="right">
        <t-form-item label="导出范围">
          <t-radio-group v-model="batch.scope">
            <t-radio-button value="all">全部桌台（{{ allCount }}）</t-radio-button>
            <t-radio-button value="query">当前查询结果（{{ list.length }}）</t-radio-button>
          </t-radio-group>
        </t-form-item>

        <t-form-item label="排版方式">
          <t-space direction="vertical" style="align-items: flex-start">
            <t-radio-group v-model="batch.mode">
              <t-radio-button value="sheet">A4 网格（打印后裁剪）</t-radio-button>
              <t-radio-button value="label">每页一张（标签机 / 已裁切桌牌）</t-radio-button>
            </t-radio-group>
            <t-space v-if="batch.mode === 'sheet'">
              <span class="mini-label">每行张数</span>
              <t-radio-group v-model="batch.columns" variant="default">
                <t-radio-button :value="2">2</t-radio-button>
                <t-radio-button :value="3">3</t-radio-button>
                <t-radio-button :value="4">4</t-radio-button>
              </t-radio-group>
            </t-space>
          </t-space>
        </t-form-item>

        <t-form-item label="卡片尺寸">
          <t-select v-model="batch.cardKey" :options="cardOptions" style="width: 280px" />
        </t-form-item>

        <t-form-item label="卡片内容">
          <t-checkbox-group v-model="batch.fields" :options="fieldOptions" />
        </t-form-item>

        <t-form-item label="二维码中心">
          <t-space>
            <t-checkbox v-model="batch.useLogo" :disabled="!shopLogo">店铺 Logo</t-checkbox>
            <t-checkbox v-model="batch.useTableNo" :disabled="batch.useLogo">桌号</t-checkbox>
          </t-space>
        </t-form-item>

        <t-form-item label="清晰度">
          <t-select v-model="batch.size" :options="pixelOptions" style="width: 240px" />
        </t-form-item>
      </t-form>

      <div class="batch-tip">
        点餐码与桌台绑定且永不变更，一次印制长期有效。打印时请在浏览器打印对话框中关闭「页眉和页脚」，并按所选尺寸设置纸张。
      </div>
      <div v-if="batch.progress" class="batch-progress">{{ batch.progress }}</div>

      <div class="batch-actions">
        <t-button variant="outline" :loading="batchBusy" @click="runBatch('zip')">打包下载 PNG（ZIP）</t-button>
        <t-button theme="primary" :loading="batchBusy" @click="runBatch('print')">生成打印页</t-button>
      </div>
    </t-dialog>

    <!-- 点击桌台查看当前订单详情：与「订单管理」页共用同一个组件，避免两处各写一套 -->
    <OrderDetailDialog
      ref="orderDetailRef"
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
      @changed="load"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { AddIcon, QrcodeIcon } from 'tdesign-icons-vue-next'
import { listTables, saveTable, updateTable, deleteTable, getConfig, listOrders } from '../api'
import {
  renderQrDataUrl, buildPrintSheetHtml, openPrintWindow,
  CARD_PRESETS, QR_PIXEL_PRESETS
} from '../utils/tableQr'
import { downloadZip, dataUrlToBytes } from '../utils/zip'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'
import OrderDetailDialog from '../components/OrderDetailDialog.vue'

const list = ref([])
const loading = ref(false)
// 全量桌台(忽略分页/搜索),用于顶部总览统计;load() 内拉取,最多 500 条。
const allRows = ref([])
const query = reactive({ tableName: '' })
const pagination = reactive({ current: 1, pageSize: 10, total: 0 })
const { isMobile } = useIsMobile()
// 桌台展示模式:列表(t-table) / 卡片(响应式网格)。
// 卡片视图在「手动选卡片」或「移动端窄屏」时启用;其余(桌面且选列表)用表格。
// 默认卡片:楼层平面图式铺开更贴合前台日常看桌况的直觉。
const viewMode = ref('card')
const showCard = computed(() => viewMode.value === 'card' || isMobile.value)
const dialogVisible = ref(false)
const form = reactive({ tableId: null, tableNo: '', tableName: '', capacity: 10, sortOrder: 0 })

// 无 table:edit 时隐藏「新增桌台」「编辑」「删除」;「二维码」是只读展示,所有人保留。
const canEdit = computed(() => hasPerm('table:edit'))
// 二维码/批量导出要读 h5_base_url,而该接口属 config:view。收银员(有 table:view 无 config:view)
// 也需要印桌牌,因此读不到配置时退回「当前访问地址」——后台与顾客端同源,这个默认值通常是对的。
const canReadConfig = computed(() => hasPerm('config:view'))

const shopLogo = ref('')
const h5Base = ref('')
// 配置里的 H5 地址不可用(如仍是出厂的 localhost)时的提示文案,空串表示无需提示。
const h5BaseWarn = ref('')
const allCount = ref(0)

// 顶部总览:基于全量桌台统计(忽略分页/搜索),反映整层楼面桌况
const summary = computed(() => {
  const rows = allRows.value
  const total = rows.length
  const occupied = rows.filter((r) => r.status === 1).length
  const idle = total - occupied
  const rate = total ? Math.round((occupied / total) * 100) : 0
  const capacity = rows.reduce((s, r) => s + (Number(r.capacity) || 0), 0)
  return { total, occupied, idle, rate, capacity }
})

// ---- 点击桌台 → 当前订单详情 ----
// 弹窗与「订单管理」页共用 components/OrderDetailDialog.vue（详情 + 改单 + 收款/结算 + 补打）。
const orderDetailRef = ref(null)
const detailVisible = ref(false)
const detailOrderId = ref(0)
// 只有能看订单的人才能点开详情，否则点下去只会拿到 403。
const canViewOrder = computed(() => hasPerm('order:view'))

// 可点开 = 有 order:view 权限且桌台处于占用状态。空闲桌台点了也只能提示
// 「没有进行中的订单」，干脆不让它看起来可点。
function tapable(row) {
  return canViewOrder.value && row.status === 1
}

// 行内按钮/输入框的点击不该顺带打开详情，靠事件源判定排除。
function fromControl(e) {
  const el = e?.target
  return !!(el && el.closest && el.closest('button, a, input, textarea, select'))
}

function onRowClick({ row, e }) {
  if (fromControl(e)) return
  openTableOrder(row)
}

function onCardClick(row, e) {
  if (fromControl(e)) return
  openTableOrder(row)
}

function rowClassName({ row }) {
  return tapable(row) ? 'row-clickable' : ''
}

// openTableOrder 按桌号反查该桌「进行中」的订单（1 已下单 / 2 制作中 / 3 已上齐）。
// 复用 /order/list 而不是 order/board：看板会一次性拉全部桌台及其订单明细，
// 单桌点开只需一页，且能直接复用 order:view 权限。
async function openTableOrder(row) {
  if (!canViewOrder.value) {
    MessagePlugin.warning('没有查看订单的权限')
    return
  }
  if (row.status !== 1) {
    MessagePlugin.info('【' + row.tableName + '】当前没有进行中的订单')
    return
  }
  try {
    const res = await listOrders({ tableNo: row.tableNo, pageNum: 1, pageSize: 20 })
    // 列表按 order_id 倒序，第一条命中即为该桌最新的一单
    const active = (res.rows || []).find((o) => [1, 2, 3].includes(o.orderStatus))
    if (!active) {
      MessagePlugin.info('【' + row.tableName + '】当前没有进行中的订单')
      return
    }
    detailOrderId.value = active.orderId
    detailVisible.value = true
  } catch (e) {
    /* 失败原因由请求拦截器统一提示 */
  }
}

const columns = computed(() => {
  const cols = [
    { colKey: 'tableNo', title: '桌号', width: 90 },
    { colKey: 'tableName', title: '桌台名称' },
    { colKey: 'tableCode', title: '点餐码', width: 130 },
    { colKey: 'capacity', title: '容纳人数', width: 100 },
    { colKey: 'status', title: '状态', width: 90 },
    { colKey: 'sortOrder', title: '排序', width: 80 }
  ]
  // 只读时操作列只剩「二维码」,宽度收窄;占用中的桌台会多一个「详情」按钮。
  const opWidth = 100 + (canEdit.value ? 110 : 0) + (canViewOrder.value ? 60 : 0)
  cols.push({ colKey: 'op', title: '操作', width: opWidth })
  return cols
})

const cardOptions = CARD_PRESETS.map((c) => ({ value: c.key, label: c.label }))
const pixelOptions = QR_PIXEL_PRESETS.map((p) => ({ value: p.key, label: p.label }))
const fieldOptions = [
  { value: 'shop', label: '店铺名称' },
  { value: 'tableName', label: '桌台名称' },
  { value: 'tableNo', label: '桌号' },
  { value: 'hint', label: '底部提示语' },
  { value: 'cutLine', label: '裁剪虚线' }
]

async function load() {
  loading.value = true
  try {
    const res = await listTables({ pageNum: pagination.current, pageSize: pagination.pageSize, tableName: query.tableName })
    list.value = res.rows
    pagination.total = res.total
    if (!query.tableName) allCount.value = res.total
    // 一次拉全量用于批量导出(上限 500),同时给顶部总览统计用
    const all = await listTables({ pageNum: 1, pageSize: 500 })
    allCount.value = all.total
    allRows.value = all.rows || []
  } finally {
    loading.value = false
  }
}

function onPageChange(page) {
  pagination.current = page.current
  pagination.pageSize = page.pageSize
  load()
}

function openAdd() {
  Object.assign(form, { tableId: null, tableNo: '', tableName: '', capacity: 10, sortOrder: 0 })
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, { tableId: row.tableId, tableNo: row.tableNo, tableName: row.tableName, capacity: row.capacity, sortOrder: row.sortOrder })
  dialogVisible.value = true
}

async function save() {
  if (!form.tableNo || !form.tableName) {
    MessagePlugin.warning('请填写桌号和桌台名称')
    return
  }
  if (form.tableId) {
    await updateTable(form)
  } else {
    await saveTable(form)
  }
  MessagePlugin.success('保存成功')
  dialogVisible.value = false
  load()
}

function onDelete(row) {
  DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除桌台【${row.tableName}】？删除后已印制该桌的二维码将失效（点餐码不再可用）。`,
    onConfirm: async () => {
      await deleteTable(row.tableId)
      MessagePlugin.success('删除成功')
      load()
    }
  })
}

async function copyText(text) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    MessagePlugin.success('已复制')
  } catch (e) {
    MessagePlugin.warning('复制失败，请手动选择复制')
  }
}

// ---- 二维码内容 ----
// 优先使用桌台稳定码(table_code):创建时生成、永不变更,且不可被枚举遍历。
const tableQrUrl = (t) => `${h5Base.value}/order/${t.tableCode || t.tableId}`

// ---- 单张二维码 ----
const qrVisible = ref(false)
const qrLoading = ref(false)
const qrDataUrl = ref('')
const qrUrl = ref('')
const qrError = ref('')
const qrTable = ref(null)
const qrUseLogo = ref(true)
const qrUseText = ref(false)
const qrSize = ref(640)

const qrTitle = computed(() => (qrTable.value ? `桌台二维码 - ${qrTable.value.tableName}` : '桌台二维码'))
const isLocalhost = computed(() => /localhost|127\.0\.0\.1|0\.0\.0\.0/.test(qrUrl.value))

async function renderSingle() {
  if (!qrTable.value) return
  qrLoading.value = true
  try {
    qrDataUrl.value = await renderQrDataUrl(qrUrl.value, {
      size: qrSize.value,
      logo: qrUseLogo.value && shopLogo.value ? shopLogo.value : '',
      centerText: !qrUseLogo.value && qrUseText.value ? qrTable.value.tableNo : ''
    })
    qrError.value = ''
  } catch (e) {
    qrDataUrl.value = ''
    qrError.value = e.message || '二维码生成失败'
  } finally {
    qrLoading.value = false
  }
}

async function openQr(row) {
  qrTable.value = row
  qrVisible.value = true
  qrDataUrl.value = ''
  qrError.value = ''
  qrUrl.value = tableQrUrl(row)
  await renderSingle()
}

function onQrClose() {
  qrDataUrl.value = ''
  qrUrl.value = ''
  qrTable.value = null
  qrError.value = ''
}

let renderTimer = null
watch([qrUseLogo, qrUseText, qrSize], () => {
  if (!qrVisible.value) return
  clearTimeout(renderTimer)
  renderTimer = setTimeout(renderSingle, 180)
})
watch(qrUseLogo, (v) => {
  if (v) qrUseText.value = false
})

function downloadQr() {
  if (!qrDataUrl.value) return
  const a = document.createElement('a')
  a.href = qrDataUrl.value
  a.download = `桌台码_${qrTable.value.tableNo}_${qrTable.value.tableName}.png`
  a.click()
}

function printSingle() {
  if (!qrDataUrl.value) return
  const card = { tableNo: qrTable.value.tableNo, tableName: qrTable.value.tableName, dataUrl: qrDataUrl.value }
  const html = buildPrintSheetHtml([card], {
    mode: 'sheet',
    columns: 1,
    cardW: 90,
    cardH: 120,
    showShop: true,
    shopName: shopTitle.value,
    hint: '微信扫码 · 自助点餐'
  })
  if (!openPrintWindow(html)) {
    MessagePlugin.warning('浏览器拦截了弹窗，请改用「下载 PNG」后自行打印')
  }
}

// ---- 批量导出 / 打印 ----
const batchVisible = ref(false)
const batchBusy = ref(false)
const shopTitle = ref('')
const batch = reactive({
  scope: 'all',
  mode: 'sheet',
  columns: 3,
  cardKey: '60x90',
  size: 900,
  useLogo: true,
  useTableNo: false,
  fields: ['shop', 'tableName', 'tableNo', 'hint', 'cutLine'],
  progress: ''
})

function openBatch() {
  batchVisible.value = true
  batch.progress = ''
}

async function collectCards() {
  const params = batch.scope === 'all' ? {} : { tableName: query.tableName }
  const res = await listTables({ pageNum: 1, pageSize: 500, ...params })
  const rows = res.rows || []
  if (!rows.length) {
    MessagePlugin.warning('所选范围内没有桌台')
    return null
  }
  const cards = []
  for (let i = 0; i < rows.length; i++) {
    const t = rows[i]
    batch.progress = `正在生成 ${i + 1} / ${rows.length} …`
    /* eslint-disable no-await-in-loop */
    const dataUrl = await renderQrDataUrl(tableQrUrl(t), {
      size: batch.size,
      logo: batch.useLogo && shopLogo.value ? shopLogo.value : '',
      centerText: !batch.useLogo && batch.useTableNo ? t.tableNo : ''
    })
    cards.push({ tableNo: t.tableNo, tableName: t.tableName, dataUrl })
  }
  batch.progress = `已生成 ${cards.length} 张二维码`
  return cards
}

function sheetOpts() {
  const preset = CARD_PRESETS.find((c) => c.key === batch.cardKey) || CARD_PRESETS[0]
  return {
    mode: batch.mode,
    columns: batch.columns,
    cardW: preset.w,
    cardH: preset.h,
    gap: 6,
    showShop: batch.fields.includes('shop'),
    shopName: shopTitle.value,
    showTableName: batch.fields.includes('tableName'),
    showTableNo: batch.fields.includes('tableNo'),
    hint: batch.fields.includes('hint') ? '微信扫码 · 自助点餐' : '',
    cutLine: batch.fields.includes('cutLine')
  }
}

async function runBatch(kind) {
  batchBusy.value = true
  batch.progress = ''
  try {
    const cards = await collectCards()
    if (!cards) return
    if (kind === 'print') {
      const html = buildPrintSheetHtml(cards, sheetOpts())
      if (!openPrintWindow(html)) {
        MessagePlugin.warning('浏览器拦截了弹窗，请允许弹出窗口后重试，或改用「打包下载 PNG」')
        return
      }
      MessagePlugin.success(`已生成 ${cards.length} 张二维码打印页`)
    } else {
      const files = cards.map((c) => ({
        // 去掉 Windows 文件名不允许的字符
        name: `${c.tableNo}_${c.tableName}.png`.replace(/[\\/:*?"<>|]/g, '_'),
        data: dataUrlToBytes(c.dataUrl)
      }))
      const d = new Date()
      const stamp = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
      await downloadZip(files, `桌台点餐码_${stamp}.zip`)
      MessagePlugin.success(`已打包 ${cards.length} 张二维码`)
    }
  } catch (e) {
    MessagePlugin.error(e.message || '生成失败')
  } finally {
    batchBusy.value = false
  }
}

// 回环地址:localhost / 127.0.0.1 / 0.0.0.0 / ::1。
const LOOPBACK_RE = /^https?:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d+)?(?:\/|$)/i
// 当前页面自身是否就是从回环地址打开的(本地开发)。
const PAGE_ON_LOOPBACK = /^(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])$/i.test(window.location.hostname)

// 归一化配置里的 H5 地址:补协议、去末尾斜杠。
// 少了这一步,「111.230.154.50」这种裸地址会直接进二维码,手机可能识别不出。
function normalizeBaseUrl(raw) {
  let v = String(raw || '').trim().replace(/\/+$/, '')
  if (v && !/^https?:\/\//i.test(v)) v = 'http://' + v
  return v
}

async function loadConfig() {
  // 顾客端与后台同源部署,先用当前地址兜底:即便读不到配置,二维码也能扫。
  h5Base.value = window.location.origin.replace(/\/+$/, '')
  h5BaseWarn.value = ''
  if (!canReadConfig.value) return
  try {
    const cfg = await getConfig()
    shopLogo.value = cfg.shop_logo || ''
    shopTitle.value = cfg.shop_name || ''
    const configured = normalizeBaseUrl(cfg.h5_base_url)
    if (!configured) return
    // 出厂默认的 H5 地址是 http://localhost:8080。生产环境照搬,二维码就会指向
    // 服务器自己,手机扫出来必然「访问不通」。回环地址只在「当前页面也是本机
    // 打开」(本地开发)时才有意义;其余情况一律忽略它,退回当前访问地址
    // (顾客端与后台同源部署,这个默认值通常就是对的),并给出醒目提示。
    if (LOOPBACK_RE.test(configured) && !PAGE_ON_LOOPBACK) {
      h5BaseWarn.value = `系统配置中的「H5 访问地址」是 ${configured}，属于本机地址，手机扫码会访问不通。`
        + `已临时改用当前访问地址 ${h5Base.value}；请到「系统配置 → H5 访问地址」改成手机能访问到的公网域名或服务器 IP。`
      return
    }
    h5Base.value = configured
  } catch (e) {
    /* 配置读取失败不阻断列表展示,沿用手上的兜底地址 */
  }
}

onMounted(async () => {
  await loadConfig()
  load()
})
</script>

<style scoped>
/* H5 地址不可用(仍是 localhost)时的醒目提示:比 tip-bar 更重,避免店员直接印出扫不通的桌牌 */
.h5-warn {
  background: #fff8e6;
  border: 1px solid #ffe1a8;
  color: #a86a00;
  font-size: 12px;
  line-height: 1.6;
  padding: 8px 12px;
  border-radius: 8px;
  margin-bottom: 12px;
  word-break: break-all;
}
.h5-warn b {
  font-weight: 600;
}
.tip-bar {
  background: #fff7f3;
  border: 1px solid #ffe0d2;
  color: #a0401c;
  font-size: 12px;
  line-height: 1.6;
  padding: 8px 12px;
  border-radius: 8px;
  margin-bottom: 12px;
}
/* 顶部总览:一排统计卡,响应式自动折行 */
.stat-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}
.stat {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: var(--r-md, 10px);
  padding: 14px 16px;
}
.stat .label {
  font-size: 12px;
  color: var(--ink-3);
}
.stat .num {
  font-size: 24px;
  font-weight: 700;
  color: var(--ink);
  margin-top: 6px;
  line-height: 1;
}
.stat .num .unit {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-3);
  margin-left: 3px;
}
.stat.occ .num {
  color: #e6772e;
}
.stat.idle .num {
  color: #2ba24a;
}
/* 占用中的桌台行可点开订单详情。scoped 样式进不到 t-table 内部，需要 :deep。 */
:deep(.row-clickable) {
  cursor: pointer;
}
:deep(.row-clickable:hover > td) {
  background: var(--brand-ghost, #fff8f5);
}
.mcard-tap {
  cursor: pointer;
  border-color: #ffd5c4;
}
/* 桌台卡片视图:响应式网格,像楼层平面图一样把桌台铺开。
   上下(row)间距刻意比左右(column)大,保证卡片行与行之间留白清晰、不粘连 */
.tcard-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  row-gap: 20px;
  column-gap: 16px;
  margin-bottom: 16px;
}
/* 卡片在网格里等高靠网格默认的 align-items:stretch(勿再写 height:100% ——
   它在 auto 高的行里会让卡片溢出轨道、把 row-gap 吃掉,行与行重叠)。
   内部 flex 列铺开:内容区 flex:1 撑满,底部操作区统一贴到卡底,上下节奏整齐 */
.tcard-grid .mcard {
  display: flex;
  flex-direction: column;
}
.tcard-grid .mcard-grid {
  flex: 1;
}
/* 状态颜色条:卡片左侧 4px 竖条区分占用(橙)/空闲(绿),比细描边/浅底醒目得多 */
.tcard-occ {
  border-left: 4px solid #e6772e;
  background: #fffaf6;
}
.tcard-idle {
  border-left: 4px solid #2ba24a;
}
@media (max-width: 767px) {
  /* 移动端屏窄,卡片视图仍单列,避免 180px 格子被压太窄、内部信息挤作一团 */
  .tcard-grid {
    grid-template-columns: 1fr;
  }
}
/* 超窄屏(320~360px):顶部总览 5 张统计卡收紧,数字字号下调避免换行 */
@media (max-width: 360px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }
  .stat {
    padding: 12px 12px;
  }
  .stat .num {
    font-size: 20px;
  }
}
.code-chip {
  display: inline-block;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
  letter-spacing: 1px;
  background: #f3f4f6;
  color: #374151;
  border-radius: 4px;
  padding: 2px 8px;
}
.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 8px 0 4px;
}
.qr-img-wrap {
  width: 260px;
  height: 260px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--line);
  border-radius: 10px;
  background: #fff;
  overflow: hidden;
}
.qr-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.qr-placeholder {
  color: var(--ink-3);
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.qr-placeholder.error {
  color: var(--danger);
}
.qr-meta {
  text-align: center;
  width: 100%;
}
.qr-table-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  margin-bottom: 4px;
}
.qr-code-line {
  font-size: 12px;
  color: var(--ink-3);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-bottom: 4px;
}
.qr-url {
  font-size: 12px;
  color: var(--ink-3);
  word-break: break-all;
}
.qr-warn {
  margin-top: 8px;
  font-size: 12px;
  color: #e6a23c;
  background: #fdf6ec;
  border-radius: 6px;
  padding: 6px 10px;
  text-align: left;
  line-height: 1.5;
}
.qr-opts {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  justify-content: center;
}
.qr-hint {
  font-size: 12px;
  color: var(--ink-3);
  text-align: center;
  line-height: 1.6;
}
.qr-actions {
  display: flex;
  gap: 10px;
}
.batch-tip {
  font-size: 12px;
  color: var(--ink-3);
  background: #f7f8fa;
  border-radius: 8px;
  padding: 10px 12px;
  line-height: 1.6;
}
.batch-progress {
  margin-top: 10px;
  font-size: 13px;
  color: var(--brand-deep, #f0481f);
}
.batch-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}
.mini-label {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 32px;
}
.spin {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid var(--line);
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: qrspin 0.8s linear infinite;
}
@keyframes qrspin {
  to { transform: rotate(360deg); }
}
</style>
