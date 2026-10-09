<template>
  <!-- 移动端:App 式桌台瓦片网格(2 列),占用暖橙 / 空闲薄荷绿,点瓦片看订单、右上角「⋯」出操作面板 -->
  <div
    v-if="isMobile"
    class="m-tiles"
  >
    <div
      v-if="!list.length"
      class="m-empty"
      style="grid-column: 1 / -1"
    >
      {{ loading ? '加载中…' : '暂无桌台' }}
    </div>
    <div
      v-for="row in list"
      :key="rowKey(row)"
      class="m-tile"
      :class="row.status === 1 ? 'busy' : 'free'"
      role="button"
      tabindex="0"
      @click="onTileClick(row, $event)"
      @keydown.enter="onTileKey(row)"
      @keydown.space.prevent="onTileKey(row)"
    >
      <div class="m-tile-no">{{ row.tableNo }}</div>
      <div class="m-tile-sub">{{ row.tableName }}</div>
      <div class="m-tile-sub">{{ row.status === 1 ? '占用中' : '空闲' }} · {{ row.capacity }} 人</div>
      <button
        type="button"
        class="m-tile-more"
        aria-label="桌台操作"
        @click.stop="openTableSheet(row)"
      >
        <ellipsis-icon />
      </button>
    </div>

    <div
      v-if="pagination.total > pagination.pageSize"
      class="m-pager"
      style="grid-column: 1 / -1"
    >
      <t-pagination
        :current="pagination.current"
        :page-size="pagination.pageSize"
        :total="pagination.total"
        @change="handlePageChange"
      />
    </div>
  </div>

  <!-- 卡片视图:桌面/移动端统一,响应式网格铺排,像楼层平面图;
       占用桌台暖橙描边+浅底、空闲绿描边,前台一眼分辨。移动端窄屏仍单列(见 scoped media 查询)。 -->
  <div
    v-else-if="showCard"
    class="tcard-grid"
  >
    <div
      v-if="!list.length"
      class="m-empty"
      style="grid-column: 1 / -1"
    >
      {{ loading ? '加载中…' : '暂无桌台' }}
    </div>
    <div
      v-for="row in list"
      :key="rowKey(row)"
      class="mcard"
      :class="{ 'mcard-tap': tapable(row), 'tcard-occ': row.status === 1, 'tcard-idle': row.status === 0 }"
      @click="onCardClick(row, $event)"
    >
      <div class="mcard-hd">
        <div style="min-width: 0">
          <span class="mcard-no">{{ row.tableNo }} 号桌</span>
          <div class="mcard-sub">
            {{ row.tableName }}
          </div>
        </div>
        <t-tag
          :theme="row.status === 0 ? 'success' : 'warning'"
          variant="light"
        >
          {{ row.status === 0 ? '空闲' : '占用' }}
        </t-tag>
      </div>

      <div class="mcard-grid">
        <div class="mcard-cell">
          <div class="k">点餐码</div>
          <div class="v">
            <span class="code-chip">{{ row.tableCode || '-' }}</span>
          </div>
        </div>
        <div class="mcard-cell">
          <div class="k">容纳人数 / 排序</div>
          <div class="v">{{ row.capacity }} 人 · 第 {{ row.sortOrder }} 位</div>
        </div>
      </div>

      <div class="mcard-ft">
        <!-- 移动端:行内小按钮收成单个「操作」,点开底部面板(大按钮、单手可点) -->
        <t-button
          v-if="isMobile"
          theme="primary"
          variant="outline"
          size="small"
          block
          @click="openTableSheet(row)"
          >操作</t-button
        >
        <template v-else>
          <t-button
            v-if="tapable(row)"
            theme="primary"
            variant="text"
            size="small"
            @click="openOrder(row)"
          >
            详情
          </t-button>
          <t-button
            v-if="canEdit"
            theme="primary"
            variant="text"
            size="small"
            @click="emit('edit', row)"
          >
            编辑
          </t-button>
          <t-button
            variant="text"
            size="small"
            @click="emit('qr', row)"
          >
            二维码
          </t-button>
          <t-button
            v-if="canEdit"
            theme="danger"
            variant="text"
            size="small"
            @click="emit('delete', row)"
          >
            删除
          </t-button>
        </template>
      </div>
    </div>

    <div
      v-if="pagination.total > pagination.pageSize"
      class="m-pager"
      style="grid-column: 1 / -1"
    >
      <t-pagination
        :current="pagination.current"
        :page-size="pagination.pageSize"
        :total="pagination.total"
        @change="handlePageChange"
      />
    </div>
  </div>

  <div
    v-else
    class="table-scroll"
  >
    <t-table
      :data="list"
      :columns="columns"
      row-key="tableId"
      :loading="loading"
      :pagination="pagination"
      :row-class-name="rowClassName"
      @page-change="handlePageChange"
      @row-click="onRowClick"
    >
      <template #tableCode="{ row }">
        <span class="code-chip">{{ row.tableCode || '-' }}</span>
      </template>
      <template #status="{ row }">
        <t-tag
          :theme="row.status === 0 ? 'success' : 'warning'"
          variant="light"
        >
          {{ row.status === 0 ? '空闲' : '占用' }}
        </t-tag>
      </template>
      <template #op="{ row }">
        <t-space>
          <t-button
            v-if="tapable(row)"
            theme="primary"
            variant="text"
            size="small"
            @click="openOrder(row)"
          >
            详情
          </t-button>
          <t-button
            v-if="canEdit"
            theme="primary"
            variant="text"
            size="small"
            @click="emit('edit', row)"
          >
            编辑
          </t-button>
          <t-button
            variant="text"
            size="small"
            @click="emit('qr', row)"
          >
            二维码
          </t-button>
          <t-button
            v-if="canEdit"
            theme="danger"
            variant="text"
            size="small"
            @click="emit('delete', row)"
          >
            删除
          </t-button>
        </t-space>
      </template>
    </t-table>
  </div>

  <!-- 移动端桌台卡片的「操作」底部面板:订单详情 / 编辑 / 二维码 / 删除 -->
  <ActionSheet
    v-model="tableSheetVisible"
    title="桌台操作"
    :actions="tableActions"
    @pick="onTablePick"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import type { PageInfo, RowEventContext } from 'tdesign-vue-next';
import type { TableInfo } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import ActionSheet from '../../components/ActionSheet.vue';
import type { SheetAction } from '../../components/ActionSheet.vue';

const props = defineProps<{
  list: TableInfo[];
  loading: boolean;
  showCard: boolean;
  pagination: { current: number; pageSize: number; total: number };
  canEdit: boolean;
  canViewOrder: boolean;
}>();

const emit = defineEmits<{
  'page-change': [page: PageInfo];
  'open-order': [row: TableInfo];
  edit: [row: TableInfo];
  delete: [row: TableInfo];
  qr: [row: TableInfo];
}>();

function rowKey(row: TableInfo): string | number {
  return row.tableId ?? row.tableNo ?? '';
}

// 可点开 = 有 order:view 权限且桌台处于占用状态。空闲桌台点了也只能提示
// 「没有进行中的订单」，干脆不让它看起来可点。
function tapable(row: TableInfo): boolean {
  return props.canViewOrder && row.status === 1;
}

// 行内按钮/输入框的点击不该顺带打开详情，靠事件源判定排除。
function fromControl(e?: Event | null): boolean {
  const el = e?.target as HTMLElement | null | undefined;
  return !!(el && typeof el.closest === 'function' && el.closest('button, a, input, textarea, select'));
}

function openOrder(row: TableInfo): void {
  emit('open-order', row);
}

function onRowClick({ row, e }: RowEventContext<TableInfo>): void {
  if (fromControl(e)) return;
  openOrder(row);
}

function onCardClick(row: TableInfo, e: Event): void {
  if (fromControl(e)) return;
  openOrder(row);
}

// 移动端瓦片:占用桌台点一下直接看订单详情;空闲桌台点一下直接进编辑(最常用动作);
// 无编辑权限或需要二维码/删除时走右上角「⋯」的操作面板。
function onTileClick(row: TableInfo, e: Event): void {
  if (fromControl(e)) return;
  onTileKey(row);
}

// 键盘(Enter/Space)触发:与点击同样「占用看订单、空闲编辑」的语义
function onTileKey(row: TableInfo): void {
  if (tapable(row)) openOrder(row);
  else if (props.canEdit) emit('edit', row);
  else openTableSheet(row);
}

function rowClassName({ row }: { row: TableInfo }): string {
  return tapable(row) ? 'row-clickable' : '';
}

function handlePageChange(page: PageInfo): void {
  emit('page-change', page);
}

// ---- 移动端桌台卡片的「操作」底部面板 ----
const { isMobile } = useIsMobile();
const tableSheetVisible = ref(false);
const tableSheetRow = ref<TableInfo | null>(null);

const tableActions = computed<SheetAction[]>(() => {
  const row = tableSheetRow.value;
  if (!row) return [];
  const items: SheetAction[] = [];
  if (tapable(row)) items.push({ key: 'order', label: '查看订单详情', theme: 'primary' });
  if (props.canEdit) items.push({ key: 'edit', label: '编辑桌台', theme: 'primary' });
  items.push({ key: 'qr', label: '查看二维码', theme: 'default' });
  if (props.canEdit) items.push({ key: 'delete', label: '删除桌台', theme: 'danger' });
  return items;
});

function openTableSheet(row: TableInfo): void {
  tableSheetRow.value = row;
  tableSheetVisible.value = true;
}

function onTablePick(key: string): void {
  const row = tableSheetRow.value;
  tableSheetVisible.value = false;
  if (!row) return;
  if (key === 'order') emit('open-order', row);
  else if (key === 'edit') emit('edit', row);
  else if (key === 'qr') emit('qr', row);
  else if (key === 'delete') emit('delete', row);
}

const columns = computed(() => {
  const cols = [
    { colKey: 'tableNo', title: '桌号', width: 90 },
    { colKey: 'tableName', title: '桌台名称' },
    { colKey: 'tableCode', title: '点餐码', width: 130 },
    { colKey: 'capacity', title: '容纳人数', width: 100 },
    { colKey: 'status', title: '状态', width: 90 },
    { colKey: 'sortOrder', title: '排序', width: 80 }
  ];
  // 只读时操作列只剩「二维码」,宽度收窄;占用中的桌台会多一个「详情」按钮。
  const opWidth = 100 + (props.canEdit ? 110 : 0) + (props.canViewOrder ? 60 : 0);
  cols.push({ colKey: 'op', title: '操作', width: opWidth });
  return cols;
});
</script>

<style scoped>
/* 列表视图在窄屏(表格列宽合计约 600px+)会超出视口,外层包一层横向滚动,
   让表格在卡片内滑动而非把整页撑出横向滚动条。桌面宽屏不受影响。 */
.table-scroll {
  width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}

/* 给滚动容器一个最小宽度,确保列不被无限压窄、文字不会换行挤成一团 */
.table-scroll :deep(.t-table) {
  min-width: 640px;
}

/* 占用中的桌台行可点开订单详情。scoped 样式进不到 t-table 内部，需要 :deep。 */
::deep(.row-clickable) {
  cursor: pointer;
}

::deep(.row-clickable:hover > td) {
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
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 24px 20px;
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

@media (width <= 767px) {
  /* 移动端屏窄,卡片视图仍单列,避免格子被压太窄、内部信息挤作一团 */
  .tcard-grid {
    grid-template-columns: 1fr;
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

/* 移动端瓦片右上角的「⋯」操作按钮 */
.m-tile-more {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: rgb(255 255 255 / 70%);
  color: var(--ink-2);
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.m-tile-more:active {
  background: #fff;
}
</style>
