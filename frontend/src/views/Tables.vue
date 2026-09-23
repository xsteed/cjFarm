<template>
  <div class="page-card">
    <div class="toolbar">
      <div class="toolbar-left">
        <t-input
          v-model="query.tableName"
          placeholder="搜索桌台名称"
          clearable
          style="width: 220px"
          @enter="load"
        />
        <t-button
          theme="primary"
          @click="load"
        >
          查询
        </t-button>
      </div>
      <t-space>
        <t-radio-group
          v-model="viewMode"
          variant="default"
        >
          <t-radio-button value="table"> 列表 </t-radio-button>
          <t-radio-button value="card"> 卡片 </t-radio-button>
        </t-radio-group>
        <t-button
          variant="outline"
          @click="openBatch"
        >
          <template #icon>
            <qrcode-icon />
          </template>
          批量导出 / 打印
        </t-button>
        <t-button
          v-if="canEdit"
          theme="primary"
          @click="openAdd"
        >
          <template #icon>
            <add-icon />
          </template>
          新增桌台
        </t-button>
      </t-space>
    </div>

    <TableStatBar :summary="summary" />

    <div
      v-if="h5BaseWarn"
      class="h5-warn"
    >
      <b>二维码地址提示：</b>{{ h5BaseWarn }}
    </div>

    <div class="tip-bar">
      桌台二维码由「H5 访问地址 + 点餐码」生成，点餐码在桌台创建时即固定、永不变更，可放心印制成桌牌。
      <br />
      点击「占用」中的桌台行（或窄屏卡片）可直接查看该桌当前订单详情，并在弹窗内改单、制作、收款、补打。
    </div>

    <TableCardGrid
      :list="list"
      :loading="loading"
      :show-card="showCard"
      :pagination="pagination"
      :can-edit="canEdit"
      :can-view-order="canViewOrder"
      @page-change="onPageChange"
      @open-order="openTableOrder"
      @edit="openEdit"
      @delete="onDelete"
      @qr="openQr"
    />

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.tableId ? '编辑桌台' : '新增桌台'"
      width="480px"
      :confirm-btn="saveConfirmBtn"
      @confirm="save"
    >
      <t-form
        :data="form"
        label-width="90px"
      >
        <t-form-item
          label="桌号"
          name="tableNo"
        >
          <t-input
            v-model="form.tableNo"
            placeholder="如 01"
          />
        </t-form-item>
        <t-form-item
          label="桌台名称"
          name="tableName"
        >
          <t-input
            v-model="form.tableName"
            placeholder="如 大厅01桌"
          />
        </t-form-item>
        <t-form-item
          label="容纳人数"
          name="capacity"
        >
          <t-input-number
            v-model="form.capacity"
            :min="1"
            :max="50"
          />
        </t-form-item>
        <t-form-item
          label="排序"
          name="sortOrder"
        >
          <t-input-number
            v-model="form.sortOrder"
            :min="0"
          />
        </t-form-item>
      </t-form>
    </t-dialog>

    <SingleQrDialog
      v-model:visible="qrVisible"
      :row="qrRow"
      :h5-base="h5Base"
      :shop-logo="shopLogo"
      :shop-title="shopTitle"
      @copy="copyText"
    />

    <BatchQrDialog
      v-model:visible="batchVisible"
      :shop-logo="shopLogo"
      :shop-title="shopTitle"
      :h5-base="h5Base"
      :all-count="allCount"
      :list-count="list.length"
      :query-table-name="query.tableName"
    />

    <!-- 点击桌台查看当前订单详情：与「订单管理」页共用同一个组件，避免两处各写一套 -->
    <OrderDetailDialog
      ref="orderDetailRef"
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
      @changed="load"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import { AddIcon, QrcodeIcon } from 'tdesign-icons-vue-next';
import { listTables, saveTable, updateTable, deleteTable, getConfig, listOrders } from '../api';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';
import { isActiveStatus } from '../constants/orderStatus';
import { LOOPBACK_RE, PAGE_ON_LOOPBACK, normalizeBaseUrl } from '../utils/h5Base';
import OrderDetailDialog from '../components/OrderDetailDialog.vue';
import TableStatBar from './tables/TableStatBar.vue';
import TableCardGrid from './tables/TableCardGrid.vue';
import SingleQrDialog from './tables/SingleQrDialog.vue';
import BatchQrDialog from './tables/BatchQrDialog.vue';
import type { PageInfo, RadioValue } from 'tdesign-vue-next';
import type { TableInfo, TablePayload } from '../types/entities';

const list = ref<TableInfo[]>([]);
const loading = ref(false);
// 全量桌台(忽略分页/搜索),用于顶部总览统计;load() 内拉取,最多 500 条。
const allRows = ref<TableInfo[]>([]);
const query = reactive<{ tableName: string | number }>({ tableName: '' });
const pagination = reactive({ current: 1, pageSize: 10, total: 0 });
const { isMobile } = useIsMobile();
// 桌台展示模式:列表(t-table) / 卡片(响应式网格)。
// 卡片视图在「手动选卡片」或「移动端窄屏」时启用;其余(桌面且选列表)用表格。
// 默认卡片:楼层平面图式铺开更贴合前台日常看桌况的直觉。
const viewMode = ref<RadioValue>('card');
const showCard = computed(() => viewMode.value === 'card' || isMobile.value);
const dialogVisible = ref(false);
const saveConfirmBtn = { content: '保存', theme: 'primary' as const };

interface TableForm {
  tableId: number | null;
  tableNo: string | number;
  tableName: string | number;
  capacity: number | string;
  sortOrder: number | string;
}

const form = reactive<TableForm>({ tableId: null, tableNo: '', tableName: '', capacity: 10, sortOrder: 0 });

// 无 table:edit 时隐藏「新增桌台」「编辑」「删除」;「二维码」是只读展示,所有人保留。
const canEdit = computed(() => hasPerm('table:edit'));
// 二维码/批量导出要读 h5_base_url,而该接口属 config:view。收银员(有 table:view 无 config:view)
// 也需要印桌牌,因此读不到配置时退回「当前访问地址」——后台与顾客端同源,这个默认值通常是对的。
const canReadConfig = computed(() => hasPerm('config:view'));

const shopLogo = ref('');
const shopTitle = ref('');
const h5Base = ref('');
// 配置里的 H5 地址不可用(如仍是出厂的 localhost)时的提示文案,空串表示无需提示。
const h5BaseWarn = ref('');
const allCount = ref(0);

// 顶部总览:基于全量桌台统计(忽略分页/搜索),反映整层楼面桌况
const summary = computed(() => {
  const rows = allRows.value;
  const total = rows.length;
  const occupied = rows.filter(r => r.status === 1).length;
  const idle = total - occupied;
  const rate = total ? Math.round((occupied / total) * 100) : 0;
  const capacity = rows.reduce((s, r) => s + (Number(r.capacity) || 0), 0);
  return { total, occupied, idle, rate, capacity };
});

// ---- 点击桌台 → 当前订单详情 ----
// 弹窗与「订单管理」页共用 components/OrderDetailDialog.vue（详情 + 改单 + 收款/结算 + 补打）。
const orderDetailRef = ref<InstanceType<typeof OrderDetailDialog> | null>(null);
const detailVisible = ref(false);
const detailOrderId = ref(0);
// 只有能看订单的人才能点开详情，否则点下去只会拿到 403。
const canViewOrder = computed(() => hasPerm('order:view'));

// openTableOrder 按桌号反查该桌「进行中」的订单（1 已下单 / 2 制作中 / 3 已上齐）。
// 复用 /order/list 而不是 order/board：看板会一次性拉全部桌台及其订单明细，
// 单桌点开只需一页，且能直接复用 order:view 权限。
async function openTableOrder(row: TableInfo): Promise<void> {
  if (!canViewOrder.value) {
    MessagePlugin.warning('没有查看订单的权限');
    return;
  }
  if (row.status !== 1) {
    MessagePlugin.info('【' + row.tableName + '】当前没有进行中的订单');
    return;
  }
  try {
    const res = await listOrders({ tableNo: row.tableNo, pageNum: 1, pageSize: 20 });
    // 列表按 order_id 倒序，第一条命中即为该桌最新的一单
    const active = (res.items || []).find(o => isActiveStatus(o.orderStatus));
    if (!active) {
      MessagePlugin.info('【' + row.tableName + '】当前没有进行中的订单');
      return;
    }
    detailOrderId.value = active.orderId ?? 0;
    detailVisible.value = true;
  } catch {
    /* 失败原因由请求拦截器统一提示 */
  }
}

// 全量桌台(供批量导出与顶部总览统计):带短 TTL 缓存,
// 翻页/搜索不再重复拉全量,只在首次与增删改后强制刷新。
// 后端单页上限 500,超出时循环翻页拉全,避免静默少算桌台。
const ALL_TTL_MS = 30_000;
const FULL_PAGE_SIZE = 500;
let allLoadedAt = 0;

async function fetchAllTables(): Promise<TableInfo[]> {
  const rows: TableInfo[] = [];
  for (let page = 1; ; page++) {
    const res = await listTables({ pageNum: page, pageSize: FULL_PAGE_SIZE });
    rows.push(...(res.items || []));
    if (rows.length >= res.total) break;
  }
  return rows;
}

async function loadAll(force = false): Promise<void> {
  if (!force && Date.now() - allLoadedAt < ALL_TTL_MS) return;
  try {
    const rows = await fetchAllTables();
    allCount.value = rows.length;
    allRows.value = rows;
    allLoadedAt = Date.now();
  } catch {
    /* 失败已由拦截器统一 toast,沿用旧全量数据 */
  }
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listTables({
      pageNum: pagination.current,
      pageSize: pagination.pageSize,
      tableName: query.tableName
    });
    list.value = res.items;
    pagination.total = res.total;
    if (!query.tableName) allCount.value = res.total;
    await loadAll();
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    loading.value = false;
  }
}

function onPageChange(page: PageInfo): void {
  pagination.current = page.current;
  pagination.pageSize = page.pageSize;
  load();
}

function openAdd(): void {
  Object.assign(form, { tableId: null, tableNo: '', tableName: '', capacity: 10, sortOrder: 0 });
  dialogVisible.value = true;
}

function openEdit(row: TableInfo): void {
  Object.assign(form, {
    tableId: row.tableId ?? null,
    tableNo: row.tableNo ?? '',
    tableName: row.tableName ?? '',
    capacity: row.capacity ?? 10,
    sortOrder: row.sortOrder ?? 0
  });
  dialogVisible.value = true;
}

async function save(): Promise<void> {
  if (!form.tableNo || !form.tableName) {
    MessagePlugin.warning('请填写桌号和桌台名称');
    return;
  }
  const payload: TablePayload = {
    tableId: form.tableId ?? undefined,
    tableNo: String(form.tableNo),
    tableName: String(form.tableName),
    capacity: Number(form.capacity) || 10,
    sortOrder: Number(form.sortOrder) || 0
  };
  try {
    if (form.tableId) {
      await updateTable(payload);
    } else {
      await saveTable(payload);
    }
    MessagePlugin.success('保存成功');
    dialogVisible.value = false;
    void loadAll(true);
    load();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供修改重试 */
  }
}

function onDelete(row: TableInfo): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除桌台【${row.tableName}】？删除后已印制该桌的二维码将失效（点餐码不再可用）。`,
    onConfirm: async () => {
      try {
        await deleteTable(row.tableId ?? 0);
        MessagePlugin.success('删除成功');
        dlg.hide();
        void loadAll(true);
        load();
      } catch {
        dlg.hide();
      }
    }
  });
}

async function copyText(text?: string): Promise<void> {
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    MessagePlugin.success('已复制');
  } catch {
    MessagePlugin.warning('复制失败，请手动选择复制');
  }
}

// ---- 单张二维码 ----
const qrVisible = ref(false);
const qrRow = ref<TableInfo | null>(null);

function openQr(row: TableInfo): void {
  qrRow.value = row;
  qrVisible.value = true;
}

// ---- 批量导出 / 打印 ----
const batchVisible = ref(false);

function openBatch(): void {
  batchVisible.value = true;
}

async function loadConfig(): Promise<void> {
  // 顾客端与后台同源部署,先用当前地址兜底:即便读不到配置,二维码也能扫。
  h5Base.value = window.location.origin.replace(/\/+$/, '');
  h5BaseWarn.value = '';
  if (!canReadConfig.value) return;
  try {
    const cfg = await getConfig();
    shopLogo.value = cfg.shop_logo || '';
    shopTitle.value = cfg.shop_name || '';
    const configured = normalizeBaseUrl(cfg.h5_base_url);
    if (!configured) return;
    // 出厂默认的 H5 地址是 http://localhost:8080。生产环境照搬,二维码就会指向
    // 服务器自己,手机扫出来必然「访问不通」。回环地址只在「当前页面也是本机
    // 打开」(本地开发)时才有意义;其余情况一律忽略它,退回当前访问地址
    // (顾客端与后台同源部署,这个默认值通常就是对的),并给出醒目提示。
    if (LOOPBACK_RE.test(configured) && !PAGE_ON_LOOPBACK()) {
      h5BaseWarn.value =
        `系统配置中的「H5 访问地址」是 ${configured}，属于本机地址，手机扫码会访问不通。` +
        `已临时改用当前访问地址 ${h5Base.value}；请到「系统配置 → H5 访问地址」改成手机能访问到的公网域名或服务器 IP。`;
      return;
    }
    h5Base.value = configured;
  } catch {
    /* 配置读取失败不阻断列表展示,沿用手上的兜底地址 */
  }
}

onMounted(async () => {
  await loadConfig();
  load();
});
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
</style>
