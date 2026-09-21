<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        每向一台打印机发一次单据都会记一条。小票没出来时先看这里：是没发出去、还是打印机离线、还是内容被平台拒了。
        失败和成功都会记录，可随时人工补打。
      </span>
    </div>

    <div class="filters">
      <t-select v-model="query.status" :options="statusOptions" clearable placeholder="全部结果" style="width: 130px" @change="reload" />
      <t-select v-model="query.docType" :options="docOptions" clearable placeholder="全部单据" style="width: 130px" @change="reload" />
      <t-select v-model="query.provider" :options="providerOptions" clearable placeholder="全部通道" style="width: 130px" @change="reload" />
      <t-input v-model="query.orderNo" placeholder="订单号 / 短号" style="width: 200px" @enter="reload" />
      <t-button theme="primary" @click="reload">查询</t-button>
      <t-button variant="outline" @click="resetQuery">重置</t-button>
    </div>

    <div v-if="isMobile" class="m-list">
      <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无打印记录' }}</div>
      <div v-for="row in list" :key="row.printId" class="mcard">
        <div class="mcard-hd">
          <div style="min-width: 0">
            <div class="mcard-no">{{ row.shortNo || row.orderNo || '—' }}</div>
            <div class="mcard-sub">{{ row.createTime }}</div>
          </div>
          <t-tag :theme="PRINT_STATUS[row.status]?.theme" variant="light">
            {{ PRINT_STATUS[row.status]?.label }}
          </t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">桌号</div>
            <div class="v">{{ row.tableNo }} · {{ row.tableName }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">单据</div>
            <div class="v">{{ PRINT_DOC_TYPE[row.docType]?.label || row.docType }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">打印机</div>
            <div class="v">{{ row.printerName }}{{ row.provider === 'feie' ? '（飞鹅云）' : '' }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">触发 / 份数</div>
            <div class="v">{{ PRINT_TRIGGER[row.triggerBy] || row.triggerBy }} · {{ row.copies }} 份</div>
          </div>
        </div>

        <div class="mcard-sub" style="margin-top: 8px">
          {{ row.status === 1 ? '说明' : '失败原因' }}：{{ row.detail || '—' }}
        </div>

        <div v-if="canReprint" class="mcard-ft">
          <t-button
            v-if="row.orderId > 0"
            theme="primary"
            variant="text"
            size="small"
            @click="onReprint(row)"
          >补打这张</t-button>
          <span v-else class="tip">测试记录不可补打</span>
        </div>
      </div>
      <div v-if="total > query.pageSize" class="m-pager">
        <t-pagination
          v-model="query.pageNum"
          :total="total"
          :page-size="query.pageSize"
          size="small"
          @change="load"
        />
      </div>
    </div>

    <template v-else>
      <t-table :data="list" :columns="columns" row-key="printId" :loading="loading">
        <template #orderNo="{ row }">
          <div class="mono strong">{{ row.shortNo || '—' }}</div>
          <div class="tip mono">{{ row.orderNo || '（非订单单据）' }}</div>
        </template>
        <template #table="{ row }">
          <span>{{ row.tableNo }}<span class="tip" v-if="row.tableName"> · {{ row.tableName }}</span></span>
        </template>
        <template #docType="{ row }">
          <t-tag :theme="PRINT_DOC_TYPE[row.docType]?.theme || 'default'" variant="light">
            {{ PRINT_DOC_TYPE[row.docType]?.label || row.docType }}
          </t-tag>
        </template>
        <template #printer="{ row }">
          <div>{{ row.printerName }}</div>
          <div class="tip">{{ row.provider === 'feie' ? '飞鹅云' : '网络直连' }} · {{ row.copies }} 份</div>
        </template>
        <template #triggerBy="{ row }">
          <span>{{ PRINT_TRIGGER[row.triggerBy] || row.triggerBy }}</span>
        </template>
        <template #status="{ row }">
          <t-tag :theme="PRINT_STATUS[row.status]?.theme" variant="light">
            {{ PRINT_STATUS[row.status]?.label }}
          </t-tag>
        </template>
        <template #detail="{ row }">
          <span class="tip ell" :title="row.detail">{{ row.detail || '—' }}</span>
        </template>
        <template #op="{ row }">
          <t-button
            v-if="row.orderId > 0"
            theme="primary"
            variant="text"
            size="small"
            @click="onReprint(row)"
          >补打</t-button>
          <span v-else class="tip">—</span>
        </template>
      </t-table>

      <div class="pager">
        <t-pagination
          v-model="query.pageNum"
          v-model:page-size="query.pageSize"
          :total="total"
          :page-size-options="[10, 20, 50]"
          @change="load"
          @page-size-change="reload"
        />
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { listPrintLogs, reprintLog, PRINT_DOC_TYPE, PRINT_STATUS, PRINT_TRIGGER } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'

const list = ref([])
const total = ref(0)
const loading = ref(false)
const { isMobile } = useIsMobile()

// 补打会真的向打印机发一次任务,属写操作 → 只有 printer:edit 才显示。
// 本页本身归 printer:view,所以只读账号仍能看到失败原因,只是不能补打。
const canReprint = computed(() => hasPerm('printer:edit'))

const query = reactive({ status: '', docType: '', provider: '', orderNo: '', pageNum: 1, pageSize: 10 })

const statusOptions = [
  { label: '已送出', value: '1' },
  { label: '失败', value: '0' }
]
const docOptions = [
  { label: '厨房单', value: 'kitchen' },
  { label: '食客小票', value: 'guest' },
  { label: '测试页', value: 'test' }
]
const providerOptions = [
  { label: '网络直连', value: 'tcp' },
  { label: '飞鹅云', value: 'feie' }
]

const columns = computed(() => {
  // 列宽按「1440 屏内容区实测约 1107px」标定。原先合计 1170px 会把最右边的
  // 「操作/补打」挤出可视区 —— 1440 屏下也要横向拖才能补打,等于功能被藏起来。
  const cols = [
    { colKey: 'orderNo', title: '订单', width: 150 },
    { colKey: 'table', title: '桌台', width: 104 },
    { colKey: 'docType', title: '单据', width: 94 },
    { colKey: 'printer', title: '打印机 / 份数', width: 158 },
    { colKey: 'triggerBy', title: '触发', width: 88 },
    { colKey: 'status', title: '结果', width: 84 },
    { colKey: 'detail', title: '说明', minWidth: 150 },
    { colKey: 'createTime', title: '时间', width: 152 }
  ]
  if (canReprint.value) cols.push({ colKey: 'op', title: '操作', width: 76 })
  return cols
})

async function load() {
  loading.value = true
  try {
    // 空串不传,避免后端把空值当成有效筛选条件。
    const params = { pageNum: query.pageNum, pageSize: query.pageSize }
    for (const k of ['status', 'docType', 'provider', 'orderNo']) {
      if (query[k]) params[k] = query[k]
    }
    const res = await listPrintLogs(params)
    list.value = res.rows || []
    total.value = res.total || 0
  } finally {
    loading.value = false
  }
}

function reload() {
  query.pageNum = 1
  load()
}

function resetQuery() {
  Object.assign(query, { status: '', docType: '', provider: '', orderNo: '', pageNum: 1 })
  load()
}

async function onReprint(row) {
  try {
    const res = await reprintLog(row.printId)
    MessagePlugin.success(res?.msg || '已重新发送')
    load()
  } catch {
    // 失败原因由请求拦截器统一弹出
    load()
  }
}

onMounted(load)
</script>

<style scoped>
.tip {
  color: #999;
  font-size: 12px;
}
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.mono {
  font-family: 'Courier New', monospace;
  font-size: 12px;
}
.strong {
  font-weight: 600;
  color: #333;
}
.ell {
  display: inline-block;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
