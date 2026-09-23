<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        管理端的每次写操作都会记一条：谁、在什么时间、从哪个 IP、对哪个对象做了什么。
        免单、挂账核销、退款、改单与权限变更还额外记了金额与原因，供事后对账追责。 密码与支付密钥等敏感字段一律不入库。
      </span>
      <t-button
        v-if="canManage"
        theme="danger"
        variant="outline"
        @click="onClean"
      >
        清理过期日志
      </t-button>
    </div>

    <div class="filters">
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
        查询
      </t-button>
      <t-button
        variant="outline"
        @click="resetQuery"
      >
        重置
      </t-button>
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
            <div class="v">{{ row.operIp || '—' }} · {{ row.costMs }}ms</div>
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
          <span
            class="tip ell"
            :title="row.detail || row.errorMsg"
            >{{ row.detail || '—' }}</span
          >
          <div
            v-if="row.status === 0 && row.errorMsg"
            class="err ell"
            :title="row.errorMsg"
          >
            {{ row.errorMsg }}
          </div>
        </template>
        <template #status="{ row }">
          <t-tag
            :theme="OPER_STATUS[row.status]?.theme"
            variant="light"
          >
            {{ OPER_STATUS[row.status]?.label }}
          </t-tag>
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
import { listOperLogs, cleanOperLogs, OPER_TYPE, OPER_STATUS } from '../api';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';
import type { OperLog } from '../types/entities';
import type { PageQuery } from '../types/api';

// 列表行在模板里要拿 status/businessType 当字典下标,OperLog 字段全可选,
// 这里收紧为必填,避免模板报「可能未定义」。
type OperLogRow = OperLog & { status: number; businessType: string };

const list = ref<OperLogRow[]>([]);
const total = ref<number>(0);
const loading = ref(false);
const { isMobile } = useIsMobile();

// 清理会真的删数据，单独收口到 log:manage（只读的 log:view 看不到这个按钮）。
const canManage = computed(() => hasPerm('log:manage'));

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
  const cols = [
    { colKey: 'createTime', title: '时间', width: 152 },
    { colKey: 'operator', title: '操作人 / 角色', width: 130 },
    { colKey: 'action', title: '动作', width: 148 },
    { colKey: 'businessType', title: '类型', width: 84 },
    { colKey: 'target', title: '操作对象', width: 128 },
    { colKey: 'detail', title: '摘要 / 失败原因', minWidth: 200 },
    { colKey: 'status', title: '结果', width: 80 },
    { colKey: 'operIp', title: 'IP', width: 124 },
    { colKey: 'costMs', title: '耗时', width: 78 },
    { colKey: 'param', title: '参数', width: 72 }
  ];
  return cols;
});

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
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
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

.ell {
  display: inline-block;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
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
