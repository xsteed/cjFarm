<template>
  <!-- 卡片视图:桌面/移动端统一,响应式网格铺排,像楼层平面图;
       占用桌台暖橙描边+浅底、空闲绿描边,前台一眼分辨。移动端窄屏仍单列(见 scoped media 查询)。 -->
  <div
    v-if="showCard"
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

  <t-table
    v-else
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
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { PageInfo, RowEventContext } from 'tdesign-vue-next';
import type { TableInfo } from '../../types/entities';

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

function rowClassName({ row }: { row: TableInfo }): string {
  return tapable(row) ? 'row-clickable' : '';
}

function handlePageChange(page: PageInfo): void {
  emit('page-change', page);
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
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 20px 16px;
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
  /* 移动端屏窄,卡片视图仍单列,避免 180px 格子被压太窄、内部信息挤作一团 */
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
</style>
