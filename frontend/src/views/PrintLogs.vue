<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        每向一台打印机发一次单据都会记一条。小票没出来时先看这里：是没发出去、还是打印机离线、还是内容被平台拒了。
        失败和成功都会记录，可随时人工补打。
      </span>
    </div>

    <div class="filters">
      <t-select
        v-model="query.status"
        :options="statusOptions"
        clearable
        placeholder="全部结果"
        style="width: 130px"
        @change="reload"
      />
      <t-select
        v-model="query.docType"
        :options="docOptions"
        clearable
        placeholder="全部单据"
        style="width: 130px"
        @change="reload"
      />
      <t-select
        v-model="query.provider"
        :options="providerOptions"
        clearable
        placeholder="全部通道"
        style="width: 130px"
        @change="reload"
      />
      <t-input
        v-model="query.orderNo"
        placeholder="订单号 / 短号"
        style="width: 200px"
        @enter="reload"
      />
      <t-button
        theme="primary"
        @click="reload"
      >
        查询
      </t-button>
      <t-button
        variant="outline"
        @click="resetQuery"
      >
        重置
      </t-button>
    </div>

    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : '暂无打印记录' }}
      </div>
      <div
        v-for="row in list"
        :key="row.printId"
        class="mcard"
      >
        <div class="mcard-hd">
          <div style="min-width: 0">
            <div class="mcard-no">
              {{ row.shortNo || row.orderNo || '—' }}
            </div>
            <div class="mcard-sub">
              {{ row.createTime }}
            </div>
          </div>
          <span class="hd-tags">
            <t-tag
              :theme="PRINT_STATUS[row.status]?.theme"
              variant="light"
            >
              {{ PRINT_STATUS[row.status]?.label }}
            </t-tag>
            <t-tag
              v-if="isExpired(row)"
              theme="danger"
              variant="outline"
              size="small"
              style="margin-left: 4px"
            >
              过期
            </t-tag>
          </span>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">桌号</div>
            <div class="v">{{ row.tableNo }} · {{ row.tableName }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">单据</div>
            <div class="v">
              {{ PRINT_DOC_TYPE[row.docType]?.label || row.docType }}
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">打印机</div>
            <div class="v">{{ row.printerName }}（{{ providerLabel(row.provider) }}）</div>
          </div>
          <div class="mcard-cell">
            <div class="k">触发 / 份数</div>
            <div class="v">{{ PRINT_TRIGGER[row.triggerBy] || row.triggerBy }} · {{ row.copies }} 份</div>
          </div>
        </div>

        <div
          class="mcard-sub"
          style="margin-top: 8px"
        >
          {{ row.status === 0 ? '失败原因' : '说明' }}：{{ row.detail || '—' }}
        </div>
        <div
          v-if="row.provider === 'agent' && queueCostText(row.costMs)"
          class="mcard-sub queue-cost"
          :class="{ 'queue-warn': Number(row.costMs) > 60000 }"
        >
          排队耗时 {{ queueCostText(row.costMs) }}
        </div>

        <div
          v-if="canReprint"
          class="mcard-ft"
        >
          <t-button
            v-if="(row.orderId ?? 0) > 0"
            theme="primary"
            variant="text"
            size="small"
            @click="onReprint(row)"
          >
            补打这张
          </t-button>
          <t-button
            v-if="(row.orderId ?? 0) > 0"
            theme="default"
            variant="text"
            size="small"
            @click="onPreview(row)"
          >
            预览
          </t-button>
          <span
            v-if="(row.orderId ?? 0) <= 0"
            class="tip"
            >测试记录不可补打</span
          >
        </div>
      </div>
      <div
        v-if="total > query.pageSize"
        class="m-pager"
      >
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
      <t-table
        :data="list"
        :columns="columns"
        row-key="printId"
        :loading="loading"
      >
        <template #orderNo="{ row }">
          <div class="mono strong">
            {{ row.shortNo || '—' }}
          </div>
          <div class="tip mono">
            {{ row.orderNo || '（非订单单据）' }}
          </div>
        </template>
        <template #table="{ row }">
          <span
            >{{ row.tableNo
            }}<span
              v-if="row.tableName"
              class="tip"
            >
              · {{ row.tableName }}</span
            ></span
          >
        </template>
        <template #docType="{ row }">
          <t-tag
            :theme="PRINT_DOC_TYPE[row.docType]?.theme || 'default'"
            variant="light"
          >
            {{ PRINT_DOC_TYPE[row.docType]?.label || row.docType }}
          </t-tag>
        </template>
        <template #printer="{ row }">
          <div>{{ row.printerName }}</div>
          <div class="tip">{{ providerLabel(row.provider) }} · {{ row.copies }} 份</div>
        </template>
        <template #triggerBy="{ row }">
          <span>{{ PRINT_TRIGGER[row.triggerBy] || row.triggerBy }}</span>
        </template>
        <template #status="{ row }">
          <t-tag
            :theme="PRINT_STATUS[row.status]?.theme"
            variant="light"
          >
            {{ PRINT_STATUS[row.status]?.label }}
          </t-tag>
          <t-tag
            v-if="isExpired(row)"
            theme="danger"
            variant="outline"
            size="small"
            style="margin-left: 2px"
          >
            过期
          </t-tag>
        </template>
        <template #detail="{ row }">
          <div>
            <span
              class="tip ell"
              :title="row.detail"
              >{{ row.detail || '—' }}</span
            >
            <div
              v-if="row.provider === 'agent' && queueCostText(row.costMs)"
              class="tip queue-cost"
              :class="{ 'queue-warn': Number(row.costMs) > 60000 }"
            >
              排队耗时 {{ queueCostText(row.costMs) }}
            </div>
          </div>
        </template>
        <template #op="{ row }">
          <t-button
            v-if="(row.orderId ?? 0) > 0"
            theme="primary"
            variant="text"
            size="small"
            @click="onReprint(row)"
          >
            补打
          </t-button>
          <t-button
            v-if="(row.orderId ?? 0) > 0"
            theme="default"
            variant="text"
            size="small"
            @click="onPreview(row)"
          >
            预览
          </t-button>
          <span
            v-if="(row.orderId ?? 0) <= 0"
            class="tip"
            >—</span
          >
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

    <!-- 票据预览:后端同一份渲染代码重放,等宽展示即与出纸 1:1 -->
    <TicketPreviewDialog
      v-model:visible="previewVisible"
      :title="previewTitle"
      :loading="previewLoading"
      :chunks="previewData?.chunks"
      :line-width="previewData?.lineWidth ?? 48"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import {
  listPrintLogs,
  reprintLog,
  previewPrintLog,
  PRINT_DOC_TYPE,
  PRINT_STATUS,
  PRINT_TRIGGER,
  PRINTER_PROVIDER
} from '../api';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';
import TicketPreviewDialog from '../components/TicketPreviewDialog.vue';
import type { PrintLog, TicketPreview } from '../types/entities';
import type { PageQuery } from '../types/api';

// 列表行在模板里要拿 printId/status/docType/triggerBy 当字典下标或传给补打接口,
// PrintLog 字段全可选,这里收紧为必填。
type PrintLogRow = PrintLog & { printId: number; status: number; docType: string; triggerBy: string };

const list = ref<PrintLogRow[]>([]);
const total = ref<number>(0);
const loading = ref(false);
const { isMobile } = useIsMobile();

// 补打会真的向打印机发一次任务,属写操作 → 只有 printer:edit 才显示。
// 本页本身归 printer:view,所以只读账号仍能看到失败原因,只是不能补打。
const canReprint = computed(() => hasPerm('printer:edit'));

interface PrintLogQuery {
  status: string;
  docType: string;
  provider: string;
  orderNo: string;
  pageNum: number;
  pageSize: number;
  [key: string]: string | number;
}

const query = reactive<PrintLogQuery>({ status: '', docType: '', provider: '', orderNo: '', pageNum: 1, pageSize: 10 });

// 通道中文名统一取自 api 枚举,避免这里再硬编码一遍导致两处漂移。
const providerLabel = (p?: string): string => PRINTER_PROVIDER[p || '']?.label || '网络直连';

// agent 通道排队耗时(入队到送出的毫秒数)。缺省或 <=0 不显示;不足 1 分钟按秒,否则按「x 分 y 秒」。
function queueCostText(ms?: number): string {
  const n = Number(ms);
  if (!n || n <= 0) return '';
  const sec = Math.round(n / 1000);
  if (sec < 60) return `${sec} 秒`;
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return s ? `${m} 分 ${s} 秒` : `${m} 分钟`;
}

// 失败且详情带「已过期」时,状态旁追加「过期」小标,便于从失败列表里一眼区分。
function isExpired(row: PrintLogRow): boolean {
  return row.status === 0 && (row.detail || '').includes('已过期');
}

const statusOptions = [
  { label: '已送出', value: '1' },
  { label: '排队中', value: '2' },
  { label: '失败', value: '0' }
];
const docOptions = [
  { label: '厨房单', value: 'kitchen' },
  { label: '食客小票', value: 'guest' },
  { label: '测试页', value: 'test' }
];
const providerOptions = [
  { label: '网络直连', value: 'tcp' },
  { label: '飞鹅云', value: 'feie' },
  { label: '本地代理', value: 'agent' }
];

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
  ];
  // 操作列(补打/预览)只对 printer:edit 展示:预览返回完整票据(含金额),
  // 与补打同为写权限级别;printer:view 的只读用户看列表与失败原因即可。
  if (canReprint.value) cols.push({ colKey: 'op', title: '操作', width: 112 });
  return cols;
});

async function load(): Promise<void> {
  loading.value = true;
  try {
    // 空串不传,避免后端把空值当成有效筛选条件。
    const params: PageQuery = { pageNum: query.pageNum, pageSize: query.pageSize };
    for (const k of ['status', 'docType', 'provider', 'orderNo']) {
      if (query[k]) params[k] = query[k];
    }
    const res = await listPrintLogs(params);
    list.value = (res.items || []) as PrintLogRow[];
    total.value = res.total || 0;
  } finally {
    loading.value = false;
  }
}

function reload(): void {
  query.pageNum = 1;
  load();
}

function resetQuery(): void {
  Object.assign(query, { status: '', docType: '', provider: '', orderNo: '', pageNum: 1 });
  load();
}

async function onReprint(row: PrintLogRow): Promise<void> {
  try {
    const res = await reprintLog(row.printId);
    MessagePlugin.success(res?.msg || '已重新发送');
    load();
  } catch {
    // 失败原因由请求拦截器统一弹出
    load();
  }
}

// ============ 票据预览 ============

const previewVisible = ref(false);
const previewLoading = ref(false);
const previewData = ref<TicketPreview | null>(null);

const previewTitle = computed(() => {
  const d = previewData.value;
  if (!d) return '票据预览';
  const doc = PRINT_DOC_TYPE[d.docType || '']?.label || d.docType || '单据';
  return `票据预览 · ${doc}${d.orderNo ? ` · ${d.orderNo}` : ''}`;
});

async function onPreview(row: PrintLogRow): Promise<void> {
  previewVisible.value = true;
  previewLoading.value = true;
  previewData.value = null;
  try {
    previewData.value = await previewPrintLog(row.printId);
  } catch {
    // 失败原因由请求拦截器统一弹出
    previewVisible.value = false;
  } finally {
    previewLoading.value = false;
  }
}

onMounted(load);
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

.hd-tags {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}

.ell {
  display: inline-block;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.queue-cost {
  margin-top: 2px;
}

.queue-warn {
  color: #ed7b2f;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
