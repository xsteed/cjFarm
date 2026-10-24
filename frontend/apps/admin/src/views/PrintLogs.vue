<template>
  <div class="page-card">
    <!-- 移动端下拉刷新 -->
    <PullRefresh
      v-if="isMobile"
      :distance="refreshDistance"
      :refreshing="refreshRefreshing"
      :pulling="refreshPulling"
    />

    <div class="toolbar">
      <span class="tip">
        每向一台打印机发一次单据都会记一条。小票没出来时先看这里：是没发出去、还是打印机离线、还是内容被平台拒了。
        失败和成功都会记录，可随时人工补打。
      </span>
    </div>

    <div
      v-if="!isMobile"
      class="filters"
    >
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
        <template #icon>
          <search-icon />
        </template>
        查询
      </t-button>
      <t-button
        variant="outline"
        @click="resetQuery"
      >
        重置
      </t-button>
    </div>

    <!-- 移动端 App 式搜索栏 -->
    <div
      v-if="isMobile"
      class="m-bar"
    >
      <div class="m-search">
        <t-input
          v-model="query.orderNo"
          placeholder="搜索订单号 / 短号"
          clearable
          @enter="reload"
        >
          <template #prefix-icon>
            <search-icon />
          </template>
        </t-input>
        <t-button
          theme="primary"
          variant="outline"
          class="m-filter-btn"
          aria-label="筛选"
          @click="filterSheetVisible = true"
        >
          <template #icon>
            <filter-icon />
          </template>
        </t-button>
      </div>
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
            <!-- 桌面端表格是「短号 + 完整单号」两行,移动端补齐同样的完整单号行,
                 否则手机上无法凭卡片对回订单(短号只够口头报读)。 -->
            <div
              v-if="row.shortNo && row.orderNo"
              class="mcard-sub tip mono"
            >
              {{ row.orderNo }}
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
            <div class="v">
              {{ row.tableNo }}<template v-if="row.tableName"> · {{ row.tableName }}</template>
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">单据</div>
            <div class="v">
              {{ printDocLabel(row.docType, row.triggerBy) || row.docType }}
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
          {{ row.status === 0 ? '失败原因' : '说明' }}：<span
            v-if="ticketLabel(row)"
            class="ticket-label"
            >{{ ticketLabel(row) }}</span
          >{{ row.detail || '—' }}
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
            :disabled="reprintingIds.has(Number(row.printId))"
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
          <!-- 刷新:与桌面操作列同口径 —— 常显,不可用时置灰并给出原因 -->
          <t-tooltip
            :content="refreshTip(row)"
            :disabled="canRefresh(row)"
            placement="top"
          >
            <t-button
              theme="warning"
              variant="text"
              size="small"
              :disabled="!canRefresh(row) || refreshingIds.has(Number(row.printId))"
              @click="onRefresh(row)"
            >
              刷新
            </t-button>
          </t-tooltip>
          <span
            v-if="(row.orderId ?? 0) <= 0"
            class="tip"
            >测试页请到「打印机管理 → 票据预览」查看样式</span
          >
        </div>
      </div>
      <!-- 单页时也显示:给出「共 N 项」与每页条数选择,不再按 total>pageSize 隐藏 -->
      <div class="m-pager">
        <t-pagination
          v-model="query.pageNum"
          v-model:page-size="query.pageSize"
          :total="total"
          :page-size-options="[10, 20, 50]"
          size="small"
          @change="load"
          @page-size-change="reload"
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
            {{ printDocLabel(row.docType, row.triggerBy) || row.docType }}
          </t-tag>
        </template>
        <template #printer="{ row }">
          <t-tooltip
            :content="row.printerName || ''"
            :disabled="!row.printerName"
            placement="top-left"
          >
            <div class="ell">{{ row.printerName }}</div>
          </t-tooltip>
          <div class="tip ell">{{ providerLabel(row.provider) }} · {{ row.copies }} 份</div>
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
          <!-- 说明只够放一行:列宽让给订单/桌台/时间,全文(含失败原因)靠 hover 看。
               maxWidth + pre-wrap 让长文案在浮层里换行,不会又被截一次。 -->
          <t-tooltip
            :content="detailTip(row)"
            :disabled="!detailTip(row)"
            :overlay-inner-style="tipOverlayStyle"
            placement="top-left"
          >
            <div class="detail-cell">
              <span class="tip ell"
                ><span
                  v-if="ticketLabel(row)"
                  class="ticket-label"
                  >{{ ticketLabel(row) }}</span
                >{{ row.detail || '—' }}</span
              >
              <div
                v-if="row.provider === 'agent' && queueCostText(row.costMs)"
                class="tip ell queue-cost"
                :class="{ 'queue-warn': Number(row.costMs) > 60000 }"
              >
                排队耗时 {{ queueCostText(row.costMs) }}
              </div>
            </div>
          </t-tooltip>
        </template>
        <template #op="{ row }">
          <div class="op-cell">
            <t-button
              v-if="(row.orderId ?? 0) > 0"
              theme="primary"
              variant="text"
              size="small"
              :disabled="reprintingIds.has(Number(row.printId))"
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
            <!-- 刷新:始终显示,不可刷新时置灰并 hover 给出原因(移动端卡片同口径) -->
            <t-tooltip
              :content="refreshTip(row)"
              :disabled="canRefresh(row)"
              placement="top-left"
            >
              <t-button
                theme="warning"
                variant="text"
                size="small"
                :disabled="!canRefresh(row) || refreshingIds.has(Number(row.printId))"
                @click="onRefresh(row)"
              >
                刷新
              </t-button>
            </t-tooltip>
          </div>
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

    <!-- 票据预览:回放这条记录当时存下的票面快照,等宽展示即与出纸 1:1
         (升级前的老记录没有快照,后端回落重渲染并由 fromSnapshot=false 标出) -->
    <TicketPreviewDialog
      v-model:visible="previewVisible"
      :title="previewTitle"
      :loading="previewLoading"
      :chunks="previewData?.chunks"
      :line-width="previewData?.lineWidth ?? 48"
      :copies="previewData?.copies"
      :from-snapshot="previewData?.fromSnapshot"
      :approximate="previewData?.approximate"
    />

    <!-- 移动端筛选弹层 -->
    <BottomSheet
      v-model="filterSheetVisible"
      title="筛选打印记录"
    >
      <div class="ff-list">
        <label class="ff">
          <span class="ff-k">结果</span>
          <t-select
            v-model="query.status"
            :options="statusOptions"
            clearable
            placeholder="全部结果"
          />
        </label>
        <label class="ff">
          <span class="ff-k">单据</span>
          <t-select
            v-model="query.docType"
            :options="docOptions"
            clearable
            placeholder="全部单据"
          />
        </label>
        <label class="ff">
          <span class="ff-k">通道</span>
          <t-select
            v-model="query.provider"
            :options="providerOptions"
            clearable
            placeholder="全部通道"
          />
        </label>
      </div>
      <template #footer>
        <t-button
          theme="default"
          @click="resetFilters"
        >
          重置
        </t-button>
        <t-button
          theme="primary"
          @click="applyFilters"
        >
          <template #icon>
            <search-icon />
          </template>
          查询
        </t-button>
      </template>
    </BottomSheet>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import {
  listPrintLogs,
  reprintLog,
  refreshPrintLog,
  previewPrintLog,
  PRINT_DOC_TYPE,
  PRINT_STATUS,
  PRINT_TRIGGER,
  PRINTER_PROVIDER,
  printDocLabel
} from '@dining/api';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import { hasPerm } from '@dining/auth';
import TicketPreviewDialog from '../components/TicketPreviewDialog.vue';
import BottomSheet from '../components/BottomSheet.vue';
import PullRefresh from '../components/PullRefresh.vue';
import type { PrintLog, TicketPreview, PageQuery } from '@dining/types';

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

// ---- 移动端:下拉刷新 + 筛选弹层 ----
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);
const filterSheetVisible = ref(false);

function applyFilters(): void {
  filterSheetVisible.value = false;
  reload();
}

function resetFilters(): void {
  // 与桌面端 resetQuery 同一口径:orderNo 搜索框在弹层之外,漏清会让重置后关键词仍旧
  // 生效;页码不回 1、不重新加载,列表仍是旧数据。
  resetQuery();
}

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

// 说明列要补显示的票据标识。
//
// 只在它比「单据」列多带信息时返回:拆单厨房单的「【厨房单】1/4」必须看得见 ——
// 「一菜一单」一次下单会写多条台账,不写第几张就分不清丢的是哪一张;
// 而顾客小票的标识就是票种名本身,再打一遍纯属噪音。
// 后端把它放在独立字段(label)而非拼进 detail,因为代理通道的回执/重试/作废
// 会整段改写 detail,拼进去的标识留不住。
function ticketLabel(row: PrintLogRow): string {
  const label = (row.label || '').trim();
  if (!label) return '';
  return label === (printDocLabel(row.docType, row.triggerBy) || row.docType) ? '' : label;
}

// 说明列只放得下一行,全文交给 hover 浮层;浮层限宽并允许换行,
// 长失败原因不会被再截一次。空说明不弹浮层。
const tipOverlayStyle = { maxWidth: '420px', whiteSpace: 'pre-wrap', wordBreak: 'break-all' };

// 浮层正文 = 票据标识 + 说明 + 排队耗时(前两者在列里都可能被省略/折行)。
function detailTip(row: PrintLogRow): string {
  const parts: string[] = [];
  const label = ticketLabel(row);
  if (label) parts.push(label);
  const detail = (row.detail || '').trim();
  if (detail) parts.push(detail);
  const cost = queueCostText(row.costMs);
  if (row.provider === 'agent' && cost) parts.push(`排队耗时 ${cost}`);
  return parts.join('\n');
}

const statusOptions = [
  { label: '已送出', value: '1' },
  { label: '排队中', value: '2' },
  { label: '失败', value: '0' }
];
const docOptions = [
  { label: '厨房单', value: 'kitchen' },
  // guest 单据按触发分两种形态(点菜单/结账小票),台账里同为一个 doc_type,筛选用。
  { label: '顾客小票', value: 'guest' },
  { label: '加菜小票', value: 'guest_append' },
  { label: '测试页', value: 'test' }
];
const providerOptions = [
  { label: '网络直连', value: 'tcp' },
  { label: '飞鹅云', value: 'feie' },
  { label: '本地代理', value: 'agent' }
];

const columns = computed(() => {
  // 列宽按「1440 屏内容区实测约 1107px」标定:除「说明」外的固定列合计 992px
  // (含操作列),余下全给「说明」(minWidth)。原先合计 1170px 会把最右边的
  // 「操作/补打」挤出可视区 —— 1440 屏下也要横向拖才能补打,等于功能被藏起来。
  //
  // 三列原本都在折行,已按内容实宽重新标定(与 OperLogs 同口径):
  //   时间 176:后端 TimeLayout = "2006-01-02 15:04:05",19 个半角字符 14px 下约 145px,
  //             加单元格左右各 16px 内边距约 177px —— 原 152px 会折成两行。
  //   订单 184:列名改「短号/订单号」后再放宽一档;orderNo 是 18 位雪花号
  //            (Courier 12px 约 135px)+ 内边距 —— 原 150px 折行。
  //   桌台 124:「7 · 包间牡丹厅」这类「桌号 · 名称」—— 原 104px 折成两行。
  // 「打印机 / 份数」从 158 压到 146:8 字名称(后厨厨房单打印机)仍能完整显示,
  // 更长的省略号 + hover 看全(比折行整齐),省下的宽度留给「说明」——
  // 失败原因才是这页最要读的内容,列里只放一行,完整文案由 hover 浮层给出。
  // 「说明」列 minWidth 200:失败原因可多显示约一行,完整文案仍由 hover 浮层兜底。
  // 代价:1440 屏(内容区约 1107px)下固定列 992 + 200 会略超,出现少量横向滚动。
  const cols = [
    { colKey: 'orderNo', title: '短号/订单号', width: 184 },
    { colKey: 'table', title: '桌台', width: 124 },
    { colKey: 'docType', title: '单据', width: 94 },
    { colKey: 'printer', title: '打印机 / 份数', width: 146 },
    { colKey: 'triggerBy', title: '触发', width: 88 },
    { colKey: 'status', title: '结果', width: 84 },
    { colKey: 'detail', title: '说明', minWidth: 200 },
    { colKey: 'createTime', title: '时间', width: 176 }
  ];
  // 操作列(补打/预览/刷新)只对 printer:edit 展示:预览返回完整票据(含金额),
  // 与补打同为写权限级别;printer:view 的只读用户看列表与失败原因即可。
  // 刷新按钮常显(不可用时置灰),宽度按三个文字按钮单行不折行标定:
  // 3 × 约40px + flex 间距 8px + 单元格左右内边距 32px。
  if (canReprint.value) cols.push({ colKey: 'op', title: '操作', width: 172 });
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
  } catch {
    // 拦截器已统一 toast;这里吞掉异常避免 onMounted/分页回调抛 unhandled rejection。
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

// 逐行在途标记:补打会真的出纸,连点会打出多张重复小票。用 Set 逐行去重,
// 不误伤其它行的补打按钮(模板据此 disabled)。
const reprintingIds = ref<Set<number>>(new Set());
async function onReprint(row: PrintLogRow): Promise<void> {
  const id = Number(row.printId);
  if (reprintingIds.value.has(id)) return;
  reprintingIds.value.add(id);
  try {
    const res = await reprintLog(row.printId);
    MessagePlugin.success(res?.msg || '已重新发送');
    load();
  } catch {
    // 失败原因由请求拦截器统一弹出
    load();
  } finally {
    reprintingIds.value.delete(id);
  }
}

// ============ 刷新失败记录(飞鹅云端回查) ============

// 仅「失败 + 飞鹅通道」的记录可刷新:回查飞鹅该次推送的最新结果,确认已打印时
// 后端把失败回写为成功。直连/代理通道没有云端单号可追问,不显示。
// 不再要求 remoteId 非空:推送当时未受理的记录(发送即失败)没有云端单号,
// 点刷新时后端会明确报「没有云端单号,无法回查」—— 按钮可见但给出解释,
// 好过按钮不出现让商家以为功能缺失。
function canRefresh(row: PrintLogRow): boolean {
  return row.status === 0 && row.provider === 'feie';
}

// 刷新按钮置灰时的提示:说明为什么这条不能刷新,而不是让按钮哑着。
// 可刷新时返回空串,浮层随之禁用。
function refreshTip(row: PrintLogRow): string {
  if (row.status !== 0) return '仅失败状态的记录支持刷新';
  if (row.provider !== 'feie') return '仅飞鹅云通道的记录支持刷新';
  return '';
}

// 逐行在途标记:回查要请求飞鹅接口(有状态缓存),连点拿不到新结果,纯浪费限流额度。
const refreshingIds = ref<Set<number>>(new Set());
async function onRefresh(row: PrintLogRow): Promise<void> {
  const id = Number(row.printId);
  if (refreshingIds.value.has(id)) return;
  refreshingIds.value.add(id);
  try {
    const res = await refreshPrintLog(row.printId);
    MessagePlugin.success(res?.msg || '已回查云端结果');
    load();
  } catch {
    // 失败原因由请求拦截器统一弹出。失败也要刷新:后端可能已回写部分状态
    // (如云端已打印但回写失败),不刷新会停在旧状态。与 onReprint 同口径。
    load();
  } finally {
    refreshingIds.value.delete(id);
  }
}

// ============ 票据预览 ============

const previewVisible = ref(false);
const previewLoading = ref(false);
const previewData = ref<TicketPreview | null>(null);

const previewTitle = computed(() => {
  const d = previewData.value;
  if (!d) return '票据预览';
  const doc = printDocLabel(d.docType, d.triggerBy) || d.docType || '单据';
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

/* 说明列:撑满整列后单行截断,不再写死 max-width —— 写死会在宽屏下留白、
   窄屏下溢出。全文由 t-tooltip 在 hover 时给出。 */
.detail-cell {
  min-width: 0;
}

.ell {
  display: block;
  width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 操作列:文字按钮横向排布、禁止折行 —— 列宽(172px)已按三个按钮标定,
   这里兜底保证任何字体度量误差下都不会把「刷新」挤到第二行。 */
.op-cell {
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}

.queue-cost {
  margin-top: 2px;
}

/* 说明列里的票据标识(【厨房单】1/4):一条订单会出多张时,它是唯一能区分
   这几条记录的字段,颜色上要与后面的结果文案区分开。 */
.ticket-label {
  color: #0052d9;
  margin-right: 4px;
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
