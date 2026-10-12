<template>
  <div class="page-card">
    <!-- 桌面端工具栏 -->
    <div
      v-if="!isMobile"
      class="toolbar tables-toolbar"
    >
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
          <template #icon>
            <search-icon />
          </template>
          查询
        </t-button>
      </div>
      <t-space>
        <div class="view-mode-switch">
          <t-radio-group v-model="viewMode">
            <t-radio-button value="table"> 列表 </t-radio-button>
            <t-radio-button value="card"> 卡片 </t-radio-button>
          </t-radio-group>
        </div>
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

    <!-- 移动端下拉刷新 -->
    <PullRefresh
      v-if="isMobile"
      :distance="refreshDistance"
      :refreshing="refreshRefreshing"
      :pulling="refreshPulling"
    />

    <!-- 移动端 App 式搜索栏 -->
    <div
      v-if="isMobile"
      class="m-bar"
    >
      <div class="m-search">
        <t-input
          v-model="query.tableName"
          placeholder="搜索桌台名称"
          clearable
          @enter="load"
        >
          <template #prefix-icon>
            <search-icon />
          </template>
        </t-input>
        <t-button
          theme="primary"
          variant="outline"
          class="m-filter-btn"
          aria-label="批量导出 / 打印"
          @click="openBatch"
        >
          <template #icon>
            <qrcode-icon />
          </template>
        </t-button>
      </div>
    </div>

    <!-- 移动端悬浮「新增桌台」 -->
    <MobileFab
      v-if="isMobile && canEdit"
      @click="openAdd"
    >
      <span class="m-fab-ic"><add-icon /></span>
      新增
    </MobileFab>

    <TableStatBar :summary="summary" />

    <div
      v-if="h5BaseWarn"
      class="h5-warn"
    >
      <b>二维码地址提示：</b>{{ h5BaseWarn }}
    </div>

    <div
      class="tip-bar"
      :class="{ collapsed: !tipExpanded }"
      @click="tipExpanded = !tipExpanded"
    >
      <div class="tip-body">
        桌台二维码由「H5 访问地址 + 点餐码」生成，点餐码在桌台创建时即固定、永不变更，可放心印制成桌牌。
        <br />
        点击「占用」中的桌台行（或窄屏卡片）可直接查看该桌当前订单详情，并在弹窗内改单、制作、收款、补打。
        <br />
        可用「状态」把桌台设为「停用（不可用）」或「预约」：此时顾客扫码无法下单，避免被其他客人占用；客人到店后再改回「正常」即可。
        <br />
        忘了点「收款」导致桌台一直占用时，用「更多 →
        强制清台」收走本桌订单并恢复空闲（会记入操作日志，需选择补结账或作废）。
      </div>
      <span class="tip-toggle">{{ tipExpanded ? '收起 ▲' : '展开 ▼' }}</span>
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
      @lock="openLock"
      @release="openRelease"
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

    <t-dialog
      v-model:visible="lockVisible"
      header="桌台状态"
      width="440px"
      :confirm-btn="lockConfirmBtn"
      @confirm="saveLock"
    >
      <t-form
        :data="lockForm"
        label-width="90px"
      >
        <t-form-item label="桌台">
          <span>{{ lockForm.tableName }}（{{ lockForm.tableNo }} 号桌）</span>
        </t-form-item>
        <t-form-item label="状态">
          <t-radio-group v-model="lockForm.lockStatus">
            <t-radio :value="0"> 正常 </t-radio>
            <t-radio :value="1"> 停用（不可用） </t-radio>
            <t-radio :value="2"> 预约 </t-radio>
          </t-radio-group>
        </t-form-item>
        <t-form-item
          v-if="lockForm.lockStatus !== 0"
          label="备注"
        >
          <t-input
            v-model="lockForm.lockRemark"
            :maxlength="60"
            placeholder="如 设备维修 / 张先生 18:00"
          />
        </t-form-item>
        <div class="lock-hint">设为「停用」或「预约」后，顾客扫码将无法下单；客人到店后改回「正常」即可继续点餐。</div>
      </t-form>
    </t-dialog>

    <t-dialog
      v-model:visible="releaseVisible"
      header="强制清台"
      width="480px"
      :confirm-btn="releaseConfirmBtn"
      @confirm="saveRelease"
    >
      <t-form
        :data="releaseForm"
        label-width="90px"
      >
        <t-form-item label="桌台">
          <span>{{ releaseForm.tableName }}（{{ releaseForm.tableNo }} 号桌）</span>
        </t-form-item>
        <t-form-item label="处理方式">
          <t-radio-group v-model="releaseForm.action">
            <t-radio value="settle"> 补结账（钱已收过） </t-radio>
            <t-radio value="cancel"> 作废订单（不计收入） </t-radio>
          </t-radio-group>
        </t-form-item>
      </t-form>
      <div class="release-hint">
        本桌进行中的订单会被按所选方式全部结束，桌台随即恢复空闲。<br />
        <template v-if="releaseForm.action === 'settle'">
          选「补结账」：订单按正常收款入账（实收 = 应收），适用于钱已在店里收过、只是系统漏点了收款。
        </template>
        <template v-else> 选「作废订单」：订单直接取消、不计收入，适用于客人跑单或误开单。 </template>
        <br />
        操作会记入操作日志，且不可撤销。若本桌其实还在用餐，请先走正常的「收款 / 结账」流程。
      </div>
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
import {
  listTables,
  saveTable,
  updateTable,
  deleteTable,
  lockTable,
  releaseTable,
  getConfig,
  listOrders
} from '@dining/api';
import { hasPerm } from '@dining/auth';
import { isActiveStatus, LOOPBACK_RE, PAGE_ON_LOOPBACK, normalizeBaseUrl } from '@dining/shared';
import { shopBrandName } from '../utils/tableQr';
import OrderDetailDialog from '../components/OrderDetailDialog.vue';
import TableStatBar from './tables/TableStatBar.vue';
import TableCardGrid from './tables/TableCardGrid.vue';
import SingleQrDialog from './tables/SingleQrDialog.vue';
import BatchQrDialog from './tables/BatchQrDialog.vue';
import { useViewMode } from '../composables/useViewMode';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import PullRefresh from '../components/PullRefresh.vue';
import MobileFab from '../components/MobileFab.vue';
import type { PageInfo } from 'tdesign-vue-next';
import type { TableInfo, TablePayload } from '@dining/types';

const list = ref<TableInfo[]>([]);
const loading = ref(false);
// 全量桌台(忽略分页/搜索),用于顶部总览统计;load() 内拉取,最多 500 条。
const allRows = ref<TableInfo[]>([]);
const query = reactive<{ tableName: string | number }>({ tableName: '' });
const pagination = reactive({ current: 1, pageSize: 10, total: 0 });
// 桌台展示模式:列表(t-table) / 卡片(响应式网格)。
// 桌面与移动端都尊重手动切换:默认卡片(楼层平面图式铺开更贴合前台看桌况直觉),
// 选「列表」时切到表格;窄屏下的表格由 TableCardGrid 内 .table-scroll 横向滚动兜底,
// 不再强制锁死卡片视图。选择按「用户名 + 页面」缓存在本地(见 useViewMode),
// 下次进来直接是上次的视图。
const viewMode = useViewMode('tables');
const { isMobile } = useIsMobile();
// 移动端下拉刷新当前桌台列表
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);
// 移动端强制卡片(楼层平面图式铺开、单列大卡),不再走定宽横向滚动的「列表」表格。
// 桌面端仍尊重用户手动选择的 列表/卡片。
const showCard = computed(() => (isMobile.value ? true : viewMode.value === 'card'));
const dialogVisible = ref(false);
// 保存在途标记:确认按钮据此 loading 并禁用,防连点重复建桌
const saving = ref(false);
const saveConfirmBtn = computed(() => ({ content: '保存', theme: 'primary' as const, loading: saving.value }));

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
// 使用说明提示默认只显示一行,点击展开看完整内容。
const tipExpanded = ref(false);
const allCount = ref(0);

// 顶部总览:基于全量桌台统计(忽略分页/搜索),反映整层楼面桌况。
// 「停用 / 预约」是人工设置的不可用状态,不算空闲(放不进客人),需从 idle 里剔除。
const summary = computed(() => {
  const rows = allRows.value;
  const total = rows.length;
  const occupied = rows.filter(r => r.status === 1).length;
  const disabled = rows.filter(r => Number(r.lockStatus) === 1).length;
  const reserved = rows.filter(r => Number(r.lockStatus) === 2).length;
  const idle = Math.max(0, total - occupied - disabled - reserved);
  const rate = total ? Math.round((occupied / total) * 100) : 0;
  const capacity = rows.reduce((s, r) => s + (Number(r.capacity) || 0), 0);
  return { total, occupied, idle, disabled, reserved, rate, capacity };
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
  if (saving.value) return;
  saving.value = true;
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
  } finally {
    saving.value = false;
  }
}

// ---- 桌台人工状态:停用(不可用) / 预约 / 恢复正常 ----
// 与订单占用状态正交:占用由下单/结账自动流转,这里只改「能否被扫码点餐」。
const lockVisible = ref(false);
const lockSaving = ref(false);
const lockConfirmBtn = computed(() => ({ content: '确定', theme: 'primary' as const, loading: lockSaving.value }));
const lockForm = reactive<{
  tableId: number;
  tableNo: string;
  tableName: string;
  lockStatus: number;
  lockRemark: string;
}>({
  tableId: 0,
  tableNo: '',
  tableName: '',
  lockStatus: 0,
  lockRemark: ''
});

function openLock(row: TableInfo): void {
  Object.assign(lockForm, {
    tableId: row.tableId ?? 0,
    tableNo: row.tableNo ?? '',
    tableName: row.tableName ?? '',
    lockStatus: Number(row.lockStatus) || 0,
    lockRemark: row.lockRemark ?? ''
  });
  lockVisible.value = true;
}

async function saveLock(): Promise<void> {
  if (!lockForm.tableId || lockSaving.value) return;
  lockSaving.value = true;
  try {
    await lockTable({
      tableId: lockForm.tableId,
      lockStatus: Number(lockForm.lockStatus) || 0,
      // 恢复「正常」时清空备注,避免残留旧信息误导。
      lockRemark: lockForm.lockStatus === 0 ? '' : String(lockForm.lockRemark || '')
    });
    MessagePlugin.success('桌台状态已更新');
    lockVisible.value = false;
    void loadAll(true);
    load();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供修改重试 */
  } finally {
    lockSaving.value = false;
  }
}

// ---- 强制清台 ----
// 店家漏点「收款」时桌台会一直占用,下一桌扫码会被直接带进上一桌的账单;
// 而常规编辑接口不改占用状态,所以这里提供唯一的人工补救入口。
// 因为会动钱,处理方式必须由店家显式选择(补结账 / 作废),不做默认。
const releaseVisible = ref(false);
const releaseSaving = ref(false);
const releaseConfirmBtn = computed(() => ({
  content: '确认清台',
  theme: 'danger' as const,
  loading: releaseSaving.value
}));
const releaseForm = reactive<{
  tableId: number;
  tableNo: string;
  tableName: string;
  action: string;
}>({
  tableId: 0,
  tableNo: '',
  tableName: '',
  action: 'settle'
});

function openRelease(row: TableInfo): void {
  Object.assign(releaseForm, {
    tableId: row.tableId ?? 0,
    tableNo: row.tableNo ?? '',
    tableName: row.tableName ?? '',
    action: 'settle'
  });
  releaseVisible.value = true;
}

async function saveRelease(): Promise<void> {
  if (!releaseForm.tableId || releaseSaving.value) return;
  releaseSaving.value = true;
  try {
    await releaseTable({
      tableId: releaseForm.tableId,
      action: releaseForm.action
    });
    MessagePlugin.success('已清台，桌台恢复空闲');
    releaseVisible.value = false;
    void loadAll(true);
    load();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留供重试 */
  } finally {
    releaseSaving.value = false;
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
    // 「店铺名称」可能是「品牌 · 标语」组合(顾客端顶部整体展示);桌牌二维码顶部只放品牌名。
    shopTitle.value = shopBrandName(cfg.shop_name || '');
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
  display: flex;
  align-items: baseline;
  gap: 8px;
  background: #fff7f3;
  border: 1px solid #ffe0d2;
  color: #a0401c;
  font-size: 12px;
  line-height: 1.6;
  padding: 8px 12px;
  border-radius: 8px;
  margin-bottom: 12px;
  cursor: pointer;
  user-select: none;
}

/* 折叠态:正文压成一行,超出省略 */
.tip-body {
  flex: 1;
  min-width: 0;
}

.tip-bar.collapsed .tip-body {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 1;
  overflow: hidden;
}

.tip-toggle {
  flex-shrink: 0;
  color: #c06a3a;
  white-space: nowrap;
}

.lock-hint {
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-3);
  background: #fff8f5;
  border: 1px solid #ffe0d2;
  border-radius: 8px;
  padding: 6px 10px;
  margin-top: 4px;
}

/* 强制清台:会动钱且不可撤销,用告警色把后果讲清楚 */
.release-hint {
  font-size: 12px;
  line-height: 1.7;
  color: #a0401c;
  background: #fff3ef;
  border: 1px solid #ffd5c4;
  border-radius: 8px;
  padding: 8px 10px;
  margin-top: 10px;
}

/* 移动端搜索栏右侧的图标按钮:正方形 */
.m-filter-btn {
  width: 40px;
  height: 40px;
  flex-shrink: 0;
  padding: 0;
}
</style>
