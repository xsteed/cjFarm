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
      <!-- 说明收成一句话 + 问号浮层:原文三行会把右上角按钮挤到换行/压扁。 -->
      <div class="toolbar-tip">
        <span class="tip">管理端每次写操作都会留痕，敏感字段不入库。</span>
        <t-tooltip
          :content="AUDIT_TIP"
          placement="bottom-left"
          :overlay-inner-style="tipOverlayStyle"
        >
          <info-circle-icon class="tip-icon" />
        </t-tooltip>
      </div>
      <t-button
        v-if="canManage"
        theme="danger"
        variant="outline"
        class="clean-btn"
        @click="onClean"
      >
        <template #icon>
          <delete-icon />
        </template>
        清理过期日志
      </t-button>
    </div>

    <div
      v-if="!isMobile"
      class="filters"
    >
      <t-input
        v-model="query.operator"
        placeholder="操作人"
        style="width: 130px"
        @enter="reload"
      />
      <t-select
        v-model="query.module"
        :options="moduleOptions"
        clearable
        placeholder="全部模块"
        style="width: 140px"
        @change="reload"
      />
      <t-select
        v-model="query.businessType"
        :options="typeOptions"
        clearable
        placeholder="全部类型"
        style="width: 120px"
        @change="reload"
      />
      <t-select
        v-model="query.status"
        :options="statusOptions"
        clearable
        placeholder="全部结果"
        style="width: 120px"
        @change="reload"
      />
      <t-input
        v-model="query.targetId"
        placeholder="对象ID / 订单号"
        style="width: 160px"
        @enter="reload"
      />
      <t-date-range-picker
        v-model="dateRange"
        clearable
        enable-time-picker
        value-type="YYYY-MM-DD HH:mm:ss"
        style="width: 320px"
        @change="reload"
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
          v-model="query.operator"
          placeholder="搜索操作人 / 对象"
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

    <!-- 移动端：卡片流 -->
    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : '暂无操作记录' }}
      </div>
      <div
        v-for="row in list"
        :key="row.logId"
        class="mcard"
      >
        <div class="mcard-hd">
          <div style="min-width: 0">
            <div class="mcard-no">
              {{ row.action }}
            </div>
            <div class="mcard-sub">{{ row.module }} · {{ row.createTime }}</div>
          </div>
          <t-tag
            :theme="OPER_STATUS[row.status]?.theme"
            variant="light"
          >
            {{ OPER_STATUS[row.status]?.label }}
          </t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">操作人</div>
            <div class="v">
              {{ row.operator || '—'
              }}<span
                v-if="row.operatorRole"
                class="tip"
              >
                · {{ row.operatorRole }}</span
              >
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">类型</div>
            <div class="v">
              {{ OPER_TYPE[row.businessType]?.label || row.businessType }}
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">对象</div>
            <div class="v">{{ row.targetType || '—' }}{{ row.targetId ? ' #' + row.targetId : '' }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">IP / 耗时</div>
            <div class="v">{{ row.operIp || '—' }} · {{ costText(row.costMs) }}</div>
          </div>
        </div>

        <div class="mcard-sub detail">
          {{ row.detail || '—' }}
        </div>
        <div
          v-if="row.status === 0 && row.errorMsg"
          class="mcard-sub err"
        >
          失败原因：{{ row.errorMsg }}
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
        row-key="logId"
        :loading="loading"
      >
        <template #action="{ row }">
          <div class="strong">
            {{ row.action }}
          </div>
          <div class="tip">
            {{ row.module }}
          </div>
        </template>
        <template #businessType="{ row }">
          <t-tag
            :theme="OPER_TYPE[row.businessType]?.theme || 'default'"
            variant="light"
          >
            {{ OPER_TYPE[row.businessType]?.label || row.businessType }}
          </t-tag>
        </template>
        <template #operator="{ row }">
          <div>{{ row.operator || '—' }}</div>
          <div class="tip">
            {{ row.operatorRole || '' }}
          </div>
        </template>
        <template #target="{ row }">
          <span class="tip mono">{{ row.targetType || '—' }}{{ row.targetId ? ' #' + row.targetId : '' }}</span>
        </template>
        <template #detail="{ row }">
          <t-tooltip
            :content="detailTip(row)"
            :disabled="!detailTip(row)"
            :overlay-inner-style="tipOverlayStyle"
            placement="top-left"
          >
            <div class="detail-cell">
              <span class="tip ell">{{ row.detail || '—' }}</span>
              <div
                v-if="row.status === 0 && row.errorMsg"
                class="err ell"
              >
                {{ row.errorMsg }}
              </div>
            </div>
          </t-tooltip>
        </template>
        <template #status="{ row }">
          <t-tag
            :theme="OPER_STATUS[row.status]?.theme"
            variant="light"
          >
            {{ OPER_STATUS[row.status]?.label }}
          </t-tag>
        </template>
        <template #costMs="{ row }">
          <span class="mono">{{ costText(row.costMs) }}</span>
        </template>
        <template #param="{ row }">
          <t-button
            v-if="row.operParam"
            theme="primary"
            variant="text"
            size="small"
            @click="onViewParam(row)"
          >
            查看
          </t-button>
          <span
            v-else
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

    <!-- 移动端筛选弹层 -->
    <BottomSheet
      v-model="filterSheetVisible"
      title="筛选操作日志"
    >
      <div class="ff-list">
        <label class="ff">
          <span class="ff-k">模块</span>
          <t-select
            v-model="query.module"
            :options="moduleOptions"
            clearable
            placeholder="全部模块"
          />
        </label>
        <label class="ff">
          <span class="ff-k">类型</span>
          <t-select
            v-model="query.businessType"
            :options="typeOptions"
            clearable
            placeholder="全部类型"
          />
        </label>
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
          <span class="ff-k">对象ID / 订单号</span>
          <t-input
            v-model="query.targetId"
            placeholder="对象ID / 订单号"
            clearable
          />
        </label>
        <label class="ff">
          <span class="ff-k">时间范围</span>
          <t-date-range-picker
            v-model="dateRange"
            clearable
            enable-time-picker
            value-type="YYYY-MM-DD HH:mm:ss"
            style="width: 100%"
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

    <!-- 请求参数(脱敏后) -->
    <t-dialog
      v-model:visible="paramVisible"
      header="请求参数"
      :footer="false"
      width="640px"
    >
      <pre class="param-box">{{ paramText }}</pre>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import type { DateRangeValue } from 'tdesign-vue-next';
import { DeleteIcon, InfoCircleIcon } from 'tdesign-icons-vue-next';
import { listOperLogs, cleanOperLogs, OPER_TYPE, OPER_STATUS } from '@dining/api';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import { hasPerm } from '@dining/auth';
import BottomSheet from '../components/BottomSheet.vue';
import PullRefresh from '../components/PullRefresh.vue';
import type { OperLog, PageQuery } from '@dining/types';

// 列表行在模板里要拿 status/businessType 当字典下标,OperLog 字段全可选,
// 这里收紧为必填,避免模板报「可能未定义」。
type OperLogRow = OperLog & { status: number; businessType: string };

const list = ref<OperLogRow[]>([]);
const total = ref<number>(0);
const loading = ref(false);
const { isMobile } = useIsMobile();

// ---- 移动端:下拉刷新 + 筛选弹层 ----
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);
const filterSheetVisible = ref(false);

function applyFilters(): void {
  filterSheetVisible.value = false;
  reload();
}

function resetFilters(): void {
  query.module = '';
  query.businessType = '';
  query.status = '';
  query.targetId = '';
  dateRange.value = [];
}

// 清理会真的删数据，单独收口到 log:manage（只读的 log:view 看不到这个按钮）。
const canManage = computed(() => hasPerm('log:manage'));

// 顶部说明的完整版：工具栏里只留一句话，其余挂在问号浮层上。
const AUDIT_TIP = [
  '管理端的每次写操作都会记一条：谁、在什么时间、从哪个 IP、对哪个对象做了什么。',
  '免单、挂账核销、退款、改单与权限变更还额外记了金额与原因，供事后对账追责。',
  '密码与支付密钥等敏感字段一律不入库。'
].join('\n');

// 模块取值与后端 handler/audit.go 的 routeAudit 保持一致 —— 改了后端记得同步这里。
const MODULES: string[] = [
  '登录账号',
  '系统安全',
  '员工管理',
  '角色权限',
  '桌台管理',
  '分类管理',
  '菜品管理',
  '备注管理',
  '打印机管理',
  '打印记录',
  '系统配置',
  '订单管理',
  '挂账管理',
  '退款管理',
  '操作日志'
];

interface OperLogQuery {
  operator: string;
  module: string;
  businessType: string;
  status: string;
  targetId: string;
  beginTime: string;
  endTime: string;
  pageNum: number;
  pageSize: number;
  [key: string]: string | number;
}

const query = reactive<OperLogQuery>({
  operator: '',
  module: '',
  businessType: '',
  status: '',
  targetId: '',
  beginTime: '',
  endTime: '',
  pageNum: 1,
  pageSize: 10
});
const dateRange = ref<DateRangeValue>([]);

const moduleOptions = MODULES.map(m => ({ label: m, value: m }));
const typeOptions = Object.entries(OPER_TYPE).map(([k, v]) => ({ label: v.label, value: k }));
const statusOptions = [
  { label: '成功', value: '1' },
  { label: '失败', value: '0' }
];

const paramVisible = ref(false);
const paramText = ref('');

const columns = computed(() => {
  // 列宽按「1440 屏内容区约 1107px」标定:固定列合计约 1043px,剩余宽度全给
  // 「摘要 / 失败原因」(minWidth),不够时表格自身横向滚动 —— 这列内容最长且要读全文,
  // 压窄了等于把关键信息藏起来。
  // 时间列 185px:后端 conf.TimeLayout = "2006-01-02 15:04:05" 共 19 个半角字符,
  // 14px 字号下文字约 145px,加单元格左右各 16px 内边距约 177px —— 原 152px 会折成两行。
  // 耗时列 92px:要放得下「1234 ms」这种带单位的文本。
  const cols = [
    { colKey: 'createTime', title: '时间', width: 185 },
    { colKey: 'operator', title: '操作人 / 角色', width: 148 },
    { colKey: 'action', title: '动作', width: 138 },
    { colKey: 'businessType', title: '类型', width: 78 },
    { colKey: 'target', title: '操作对象', width: 128 },
    { colKey: 'detail', title: '摘要 / 失败原因', minWidth: 260 },
    { colKey: 'status', title: '结果', width: 76 },
    { colKey: 'operIp', title: 'IP', width: 130 },
    { colKey: 'costMs', title: '耗时', width: 92 },
    { colKey: 'param', title: '参数', width: 68 }
  ];
  return cols;
});

// 耗时统一带单位:不足 1 秒按毫秒,超过按秒(保留 1 位),避免「1234」这种裸数字看不出量级。
function costText(ms?: number): string {
  const n = Number(ms);
  if (!Number.isFinite(n) || n < 0) return '—';
  if (n < 1000) return `${Math.round(n)} ms`;
  return `${(n / 1000).toFixed(1)} s`;
}

// 摘要 + 失败原因一起作为悬浮提示正文:列里只放得下一行,全文靠 hover 看。
function detailTip(row: OperLogRow): string {
  const parts: string[] = [];
  if (row.detail) parts.push(row.detail);
  if (row.status === 0 && row.errorMsg) parts.push(`失败原因:${row.errorMsg}`);
  return parts.join('\n');
}

// 提示浮层限宽并允许换行,长摘要不会被截断成一行。
const tipOverlayStyle = { maxWidth: '420px', whiteSpace: 'pre-wrap', wordBreak: 'break-all' };

async function load(): Promise<void> {
  loading.value = true;
  try {
    // 空串不传，避免后端把空值当成有效筛选条件。
    const params: PageQuery = { pageNum: query.pageNum, pageSize: query.pageSize };
    for (const k of ['operator', 'module', 'businessType', 'status', 'targetId', 'beginTime', 'endTime']) {
      if (query[k]) params[k] = query[k];
    }
    const res = await listOperLogs(params);
    list.value = (res.items || []) as OperLogRow[];
    total.value = res.total || 0;
  } catch {
    /* 失败已由拦截器统一 toast;必须 catch,否则 onMounted(load)/@change 会抛未处理的 Promise 拒绝 */
  } finally {
    loading.value = false;
  }
}

function reload(): void {
  query.pageNum = 1;
  // 日期区间是数组形式，拆成 beginTime / endTime 两个参数。
  query.beginTime = dateRange.value?.[0] ? String(dateRange.value[0]) : '';
  query.endTime = dateRange.value?.[1] ? String(dateRange.value[1]) : '';
  load();
}

function resetQuery(): void {
  dateRange.value = [];
  Object.assign(query, {
    operator: '',
    module: '',
    businessType: '',
    status: '',
    targetId: '',
    beginTime: '',
    endTime: '',
    pageNum: 1
  });
  load();
}

function onViewParam(row: OperLogRow): void {
  // 后端存的是脱敏后的 JSON，这里只做格式化展示，不再二次解析。
  try {
    paramText.value = JSON.stringify(JSON.parse(row.operParam || ''), null, 2);
  } catch {
    paramText.value = row.operParam || '';
  }
  paramVisible.value = true;
}

async function onClean(): Promise<void> {
  const dlg = DialogPlugin.confirm({
    header: '清理过期操作日志',
    body: '将按后端配置的保留天数删除过期记录，且不可恢复。建议先导出需要的记录。确认继续？',
    confirmBtn: { theme: 'danger', content: '确认清理' },
    onConfirm: async () => {
      try {
        const res = await cleanOperLogs();
        MessagePlugin.success(res?.msg || '已清理');
        dlg.hide();
        load();
      } catch {
        dlg.hide();
      }
    }
  });
}

onMounted(load);
</script>

<style scoped>
/* 说明只留一行,按钮 flex-start 贴到卡片右上角、不参与收缩,排版不再随文字长短漂。 */
.toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.toolbar-tip {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1 1 auto;
  min-width: 0;
}

.tip-icon {
  flex: 0 0 auto;
  color: #bbb;
  font-size: 14px;
  cursor: help;
}

.clean-btn {
  flex-shrink: 0;
}

/* 窄屏(<=1024px)说明文字会占满整行,按钮换行独占一行更好点 —— 通用规则里
   .toolbar > .t-button 已是 width:100%,这里补上换行即可。 */
@media (width <= 1024px) {
  .toolbar {
    flex-wrap: wrap;
  }

  .clean-btn {
    flex-shrink: 1;
    width: 100%;
  }
}

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

/* 摘要 / 失败原因:撑满整列后单行截断,不再写死 320px —— 写死会在宽屏下留白、
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

.err {
  color: #d54941;
  font-size: 12px;
}

.detail {
  margin-top: 8px;
}

.param-box {
  margin: 0;
  max-height: 420px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}

.m-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.m-empty {
  padding: 24px;
  text-align: center;
  color: #999;
  font-size: 13px;
}

.mcard {
  border: 1px solid #eee;
  border-radius: 8px;
  padding: 10px 12px;
}

.mcard-hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.mcard-no {
  font-weight: 600;
  color: #333;
}

.mcard-sub {
  color: #999;
  font-size: 12px;
}

.mcard-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 12px;
  margin-top: 8px;
}

.mcard-cell .k {
  color: #999;
  font-size: 12px;
}

.mcard-cell .v {
  font-size: 13px;
}

.m-pager {
  display: flex;
  justify-content: center;
  margin-top: 12px;
}
</style>
