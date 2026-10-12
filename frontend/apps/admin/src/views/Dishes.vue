<template>
  <div class="page-card">
    <!-- 桌面端工具栏 -->
    <div
      v-if="!isMobile"
      class="toolbar"
    >
      <div class="toolbar-left">
        <t-input
          v-model="query.dishName"
          placeholder="搜索菜品名称"
          clearable
          style="width: 220px"
          @enter="search"
        >
          <template #prefix-icon>
            <search-icon />
          </template>
        </t-input>
        <t-select
          v-model="query.categoryId"
          placeholder="全部分类"
          clearable
          style="width: 170px"
        >
          <t-option
            v-for="c in categories"
            :key="c.categoryId"
            :value="String(c.categoryId)"
            :label="c.categoryName"
          />
        </t-select>
        <t-button
          theme="primary"
          @click="search"
        >
          <template #icon>
            <search-icon />
          </template>
          查询
        </t-button>
      </div>
      <t-space>
        <div
          v-if="!isMobile"
          class="view-mode-switch"
        >
          <t-radio-group v-model="viewMode">
            <t-radio-button value="table"> 列表 </t-radio-button>
            <t-radio-button value="card"> 卡片 </t-radio-button>
          </t-radio-group>
        </div>
        <t-button
          v-if="canEdit"
          theme="primary"
          @click="openAdd"
        >
          <template #icon>
            <add-icon />
          </template>
          新增菜品
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

    <!-- 移动端 App 式搜索栏 + 分类药丸 -->
    <div
      v-if="isMobile"
      class="m-bar"
    >
      <div class="m-search">
        <t-input
          v-model="query.dishName"
          placeholder="搜索菜品名称"
          clearable
          @enter="search"
        >
          <template #prefix-icon>
            <search-icon />
          </template>
        </t-input>
      </div>
      <div class="m-seg">
        <button
          type="button"
          class="m-seg-item"
          :class="{ on: !query.categoryId }"
          @click="onCategoryTab('')"
        >
          全部
        </button>
        <button
          v-for="c in categories"
          :key="c.categoryId"
          type="button"
          class="m-seg-item"
          :class="{ on: String(query.categoryId) === String(c.categoryId) }"
          @click="onCategoryTab(String(c.categoryId))"
        >
          {{ c.categoryName }}
        </button>
      </div>
    </div>

    <!-- 移动端悬浮「新增菜品」(可拖拽,避免挡住操作) -->
    <MobileFab
      v-if="isMobile && canEdit"
      @click="openAdd"
    >
      <span class="m-fab-ic"><add-icon /></span>
      新增
    </MobileFab>

    <!-- 移动端:图片列表卡(横向),编辑/删除直接铺在卡片右侧,一键直达 -->
    <template v-if="isMobile">
      <div class="m-list">
        <div
          v-if="!loading && !list.length"
          class="m-empty"
        >
          暂无菜品
        </div>
        <div
          v-for="d in list"
          :key="d.dishId"
          class="m-row"
          :class="{ 'm-row-tap': canEdit }"
          :role="canEdit ? 'button' : undefined"
          :tabindex="canEdit ? 0 : undefined"
          @click="canEdit && openEdit(d)"
          @keydown.enter="canEdit && openEdit(d)"
          @keydown.space.prevent="canEdit && openEdit(d)"
        >
          <div class="m-row-pic">
            <img
              v-if="d.dishImage"
              class="m-row-img"
              :class="{ 'is-off': d.status !== 1 }"
              :src="d.dishImage"
              :alt="d.dishName"
            />
            <div
              v-else
              class="m-row-ph"
            >
              无图
            </div>
            <!-- 招牌角标:图片左上角,与顾客端同视觉,店长一眼确认哪些菜设了招牌 -->
            <span
              v-if="d.signature === 1"
              class="sig-tag"
            >招牌</span>
            <!-- 蒙版:下架优先于售罄(顾客端完全看不到 vs 临时沽清),同一套视觉 -->
            <span
              v-if="d.status !== 1"
              class="m-row-out is-off"
              >已下架</span
            >
            <span
              v-else-if="d.soldOut === 1"
              class="m-row-out"
              >售罄</span
            >
          </div>
          <div class="m-row-main">
            <div class="m-row-head">
              <div class="m-row-name">{{ d.dishName }}</div>
              <label
                v-if="canEdit"
                class="m-row-shelf"
                @click.stop
              >
                <t-switch
                  :value="Number(d.status)"
                  :custom-value="[1, 0]"
                  size="small"
                  :disabled="statusBusyIds.has(Number(d.dishId))"
                  @change="(val: string | number | boolean) => toggleStatus(d, val)"
                />
                <span
                  class="m-row-shelf-txt"
                  :class="d.status === 1 ? 'is-on' : 'is-off'"
                  >{{ d.status === 1 ? '上架中' : '已下架' }}</span
                >
              </label>
            </div>
            <div class="m-row-sub">
              {{ d.categoryName || '未分类' }} ·
              {{ d.soldOut === 1 ? '售罄' : d.status === 1 ? '上架中' : '已下架' }}
            </div>
            <div class="m-row-foot">
              <div class="m-row-price">¥{{ minPrice(d) }}<small v-if="d.specs && d.specs.length > 1"> 起</small></div>
              <div
                v-if="canEdit"
                class="m-row-ops"
                @click.stop
              >
                <t-button
                  theme="primary"
                  variant="text"
                  size="small"
                  @click="openEdit(d)"
                  >编辑</t-button
                >
                <t-button
                  theme="danger"
                  variant="text"
                  size="small"
                  @click="onDelete(d)"
                  >删除</t-button
                >
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="pagination.total > pagination.pageSize"
          class="m-pager"
        >
          <t-pagination
            :current="pagination.current"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            @current-change="onPageChange"
          />
        </div>
      </div>
    </template>

    <!-- 桌面端列表视图 -->
    <div
      v-else-if="!showCard"
      class="table-scroll"
    >
      <t-table
        :data="list"
        :columns="columns"
        row-key="dishId"
        :loading="loading"
        :pagination="tablePagination"
        empty="暂无菜品"
        @page-change="onTablePageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #dishImage="{ row }">
          <div class="thumb-box">
            <img
              v-if="row.dishImage"
              class="thumb"
              :class="{ 'is-off': row.status !== 1 }"
              :src="row.dishImage"
              :alt="row.dishName"
            />
            <span
              v-else
              class="thumb thumb-ph"
              >无图</span
            >
            <!-- 招牌角标:缩略图左上角,与顾客端同视觉 -->
            <span
              v-if="row.signature === 1"
              class="sig-tag"
            >招牌</span>
            <!-- 蒙版:下架优先于售罄,缩略图太小直接在图上压字,一眼可辨 -->
            <span
              v-if="row.status !== 1"
              class="thumb-out is-off"
              >已下架</span
            >
            <span
              v-else-if="row.soldOut === 1"
              class="thumb-out"
              >售罄</span
            >
          </div>
        </template>
        <template #dishName="{ row }">
          <div class="cell-name">
            {{ row.dishName }}
          </div>
          <div
            v-if="row.description"
            class="cell-desc"
          >
            {{ row.description }}
          </div>
        </template>
        <template #categoryName="{ row }">
          {{ row.categoryName || '未分类' }}
        </template>
        <template #specs="{ row }">
          <div
            v-if="row.specs && row.specs.length"
            class="spec-list"
          >
            <span
              v-for="(s, i) in row.specs"
              :key="s.specId ?? i"
              class="spec-chip"
              :class="{ 'spec-chip-out': Number(s.stock) === 0 }"
            >
              {{ s.specName }}
              <b>¥{{ Number(s.price || 0).toFixed(2) }}</b>
              <em v-if="Number(s.stock) === 0">售罄</em>
              <em v-else-if="Number(s.stock) > 0">余 {{ s.stock }}</em>
            </span>
          </div>
          <span v-else>-</span>
        </template>
        <template #status="{ row }">
          <t-tag
            v-if="row.soldOut === 1"
            theme="warning"
            variant="light"
          >
            售罄
          </t-tag>
          <t-tag
            v-else
            :theme="row.status === 1 ? 'success' : 'default'"
            variant="light"
          >
            {{ row.status === 1 ? '上架中' : '已下架' }}
          </t-tag>
        </template>
        <template #op="{ row }">
          <t-space size="small">
            <t-button
              theme="primary"
              variant="text"
              size="small"
              @click="openEdit(row)"
            >
              编辑
            </t-button>
            <t-button
              theme="danger"
              variant="text"
              size="small"
              @click="onDelete(row)"
            >
              删除
            </t-button>
          </t-space>
        </template>
      </t-table>
    </div>

    <!-- 网格卡片 -->
    <template v-else>
      <div
        v-if="!loading && list.length"
        class="dish-grid"
      >
        <div
          v-for="d in list"
          :key="d.dishId"
          class="dish-card"
        >
          <div class="pic">
            <img
              v-if="d.dishImage"
              :class="{ 'is-off': d.status !== 1 }"
              :src="d.dishImage"
              :alt="d.dishName"
            />
            <div
              v-else
              class="pic-ph"
            >
              无图
            </div>
            <!-- 招牌角标:图片左上角(右上角已被状态 flag 占用),与顾客端同视觉 -->
            <div
              v-if="d.signature === 1"
              class="sig-tag"
            >
              招牌
            </div>
            <!-- 蒙版:下架优先于售罄,与移动端行卡同一套视觉 -->
            <div
              v-if="d.status !== 1"
              class="pic-mask is-off"
            >
              <span>已下架</span>
            </div>
            <div
              v-else-if="d.soldOut === 1"
              class="pic-mask"
            >
              <span>售罄</span>
            </div>
            <div
              class="flag"
              :class="d.soldOut === 1 ? 'soldout' : d.status !== 1 ? 'off' : ''"
            >
              {{ d.soldOut === 1 ? '售罄' : d.status === 1 ? '上架中' : '已下架' }}
            </div>
          </div>
          <div class="info">
            <div class="n">
              {{ d.dishName }}
            </div>
            <div class="p"><i>¥</i>{{ minPrice(d) }}<small v-if="d.specs && d.specs.length > 1"> 起</small></div>
            <div class="meta">
              {{ d.categoryName || '未分类' }}
            </div>
            <div
              v-if="canEdit"
              class="ops"
            >
              <t-button
                theme="primary"
                variant="text"
                size="small"
                @click="openEdit(d)"
                >编辑</t-button
              >
              <t-button
                theme="danger"
                variant="text"
                size="small"
                @click="onDelete(d)"
                >删除</t-button
              >
            </div>
          </div>
        </div>
      </div>
      <div
        v-else-if="!loading && !list.length"
        class="empty"
      >
        暂无菜品
      </div>

      <div
        v-if="pagination.total > pagination.pageSize"
        class="pager"
      >
        <t-pagination
          :current="pagination.current"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          show-page-size
          :page-size-options="[12, 24, 48]"
          @current-change="onPageChange"
          @page-size-change="onPageSizeChange"
        />
      </div>
    </template>

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.dishId ? '编辑菜品' : '新增菜品'"
      width="640px"
      class="dish-dialog"
      :confirm-btn="{ content: '保存', theme: 'primary', loading: saving }"
      @confirm="save"
    >
      <t-form
        :data="form"
        label-width="90px"
      >
        <t-form-item
          label="所属分类"
          name="categoryId"
        >
          <t-select
            v-model="form.categoryId"
            placeholder="请选择分类"
          >
            <t-option
              v-for="c in categories"
              :key="c.categoryId"
              :value="c.categoryId"
              :label="c.categoryName"
            />
          </t-select>
        </t-form-item>
        <t-form-item
          label="菜品名称"
          name="dishName"
        >
          <t-input
            v-model="form.dishName"
            placeholder="如 凉拌青瓜"
          />
        </t-form-item>
        <t-form-item
          label="菜品图片"
          name="dishImage"
        >
          <t-upload
            v-model="form.imageFiles"
            :auto-upload="false"
            accept="image/*"
            theme="image"
            :max="1"
          />
        </t-form-item>
        <t-form-item
          label="菜品描述"
          name="description"
        >
          <t-textarea
            v-model="form.description"
            placeholder="如 清爽开胃"
            :maxlength="100"
          />
        </t-form-item>
        <t-form-item
          label="规格价格"
          name="specs"
        >
          <div class="spec-editor">
            <div
              v-for="(s, i) in form.specs"
              :key="i"
              class="spec-row"
            >
              <t-input
                v-model="s.specName"
                class="spec-name"
                placeholder="规格名，如 份/小份"
              />
              <t-input-number
                v-model="s.price"
                class="spec-price"
                :min="0"
                :decimal-places="2"
                placeholder="价格"
              />
              <t-input-number
                v-model="s.stock"
                class="spec-stock"
                :min="-1"
                :decimal-places="0"
                placeholder="库存"
              />
              <t-button
                class="spec-del"
                theme="danger"
                variant="text"
                @click="removeSpec(i)"
              >
                删除
              </t-button>
            </div>
            <div class="spec-hint">价格单位「元」；库存填 -1 表示不限量</div>
            <t-button
              variant="dashed"
              block
              @click="addSpec"
            >
              <template #icon>
                <add-icon />
              </template>
              添加规格
            </t-button>
          </div>
        </t-form-item>
        <t-form-item
          label="状态"
          name="status"
        >
          <t-switch
            v-model="form.status"
            :custom-value="[1, 0]"
          />
          <span style="margin-left: 8px; color: #999">{{ form.status === 1 ? '上架' : '下架' }}</span>
        </t-form-item>
        <t-form-item
          label="售罄"
          name="soldOut"
        >
          <t-switch
            v-model="form.soldOut"
            :custom-value="[1, 0]"
          />
          <span style="margin-left: 8px; color: #999">今日沽清（顾客端仍能看到，但不可下单）</span>
        </t-form-item>
        <t-form-item
          label="招牌"
          name="signature"
        >
          <t-switch
            v-model="form.signature"
            :custom-value="[1, 0]"
          />
          <span style="margin-left: 8px; color: #999">设为招牌菜（顾客端图片左上角显示「招牌」角标）</span>
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
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import { listDishes, saveDish, updateDish, deleteDish, listCategories, uploadFile } from '@dining/api';
import { hasPerm } from '@dining/auth';
import { useViewMode } from '../composables/useViewMode';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import PullRefresh from '../components/PullRefresh.vue';
import MobileFab from '../components/MobileFab.vue';
import type { UploadFile, PageInfo } from 'tdesign-vue-next';
import type { Dish, MenuCategory, DishPayload } from '@dining/types';

const list = ref<Dish[]>([]);
const categories = ref<MenuCategory[]>([]);
const loading = ref(false);
const query = reactive({ dishName: '', categoryId: '' });
const pagination = reactive({ current: 1, pageSize: 12, total: 0 });
const dialogVisible = ref(false);
// 保存在途标记:确认按钮据此 loading 并禁用,防连点重复保存(含图片上传)
const saving = ref(false);

// 展示模式:卡片(默认,图片墙式铺开) / 列表(表格,一屏看更多行的价格与库存)。
// 与桌台管理页保持一致:手动切换后桌面和移动端都尊重选择,不按屏宽强制改视图。
// 选择按「用户名 + 页面」缓存在本地(见 useViewMode),下次进来直接是上次的视图。
const viewMode = useViewMode('dishes');
const { isMobile } = useIsMobile();
// 移动端下拉刷新当前菜品列表
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);
// 移动端强制卡片视图(2 列图片墙),不再走定宽横向滚动的「列表」表格,杜绝窄屏破版。
// 桌面端仍尊重用户手动选择的 列表/卡片。
const showCard = computed(() => (isMobile.value ? true : viewMode.value === 'card'));

interface SpecForm {
  specId?: number;
  specName: string;
  price: number;
  stock: number;
}

interface DishForm {
  dishId: number | null;
  categoryId: number | undefined;
  dishName: string;
  dishImage: string;
  description: string;
  status: number;
  soldOut: number;
  signature: number;
  sortOrder: number;
  specs: SpecForm[];
  imageFiles: UploadFile[];
}

const form = reactive<DishForm>({
  dishId: null,
  categoryId: undefined,
  dishName: '',
  dishImage: '',
  description: '',
  status: 1,
  soldOut: 0,
  signature: 0,
  sortOrder: 0,
  specs: [],
  imageFiles: []
});

// 无 dish:edit 时隐藏「新增菜品」入口与卡片上的编辑/删除(只读浏览)。后端接口同样 403。
const canEdit = computed(() => hasPerm('dish:edit'));

function minPrice(d: Dish): string {
  if (!d.specs || !d.specs.length) return '0';
  return Math.min(...d.specs.map(s => Number(s.price) || 0)).toFixed(2);
}

// 列表视图列:图片给了缩略图、规格列直接铺开每个规格的价格与余量,
// 不用像卡片那样只能看到「最低价 起」。只读角色(无 dish:edit)不渲染操作列。
const columns = computed(() => {
  const cols = [
    { colKey: 'dishImage', title: '图片', width: 80 },
    { colKey: 'dishName', title: '菜品名称', width: 200 },
    { colKey: 'categoryName', title: '分类', width: 120 },
    { colKey: 'specs', title: '规格 / 价格', width: 260 },
    { colKey: 'status', title: '状态', width: 100 },
    { colKey: 'sortOrder', title: '排序', width: 80 }
  ];
  if (canEdit.value) cols.push({ colKey: 'op', title: '操作', width: 130 });
  return cols;
});

// 列表视图的分页交给 t-table 自带分页条(卡片视图仍用页面底部的 .pager)。
// pageSizeOptions 必须包含默认 12,否则下拉里会缺项、显示成游离值。
const tablePagination = computed(() => ({
  current: pagination.current,
  pageSize: pagination.pageSize,
  total: pagination.total,
  showPageSize: true,
  pageSizeOptions: [12, 24, 48, 96]
}));

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listDishes({
      pageNum: pagination.current,
      pageSize: pagination.pageSize,
      dishName: query.dishName,
      categoryId: query.categoryId
    });
    // 缺 items 时兜底成空数组:否则 list.value=undefined 会让模板的 !list.length 抛
    // TypeError 白屏(同仓库其它列表页都做了这层兜底)。
    list.value = res.items || [];
    pagination.total = res.total;
  } finally {
    loading.value = false;
  }
}

async function loadCategories(): Promise<void> {
  // 分类下拉依赖 category:view(跨模块读)。内置角色要么两者都有、要么都没有,
  // 但自定义角色可能只给 dish:view —— 那时跳过请求,而不是让它弹一次 403。
  if (!hasPerm('category:view')) return;
  const res = await listCategories();
  categories.value = res.items || [];
}

// 查询 / 搜索框回车:必须先回到第 1 页再查。
// 不重置页码时,从第 N 页发起筛选会带着旧页码请求,命中变少就落在空页;
// 移动端分页器还带 v-if(total > pageSize),total 变小后直接消失,无法再翻回。
function search(): void {
  pagination.current = 1;
  load();
}

// current-change 首参就是新页码数字(与 page-size-change 的首参语义一致,见 t-pagination 事件签名)
function onPageChange(page: number): void {
  pagination.current = page;
  load();
}
function onPageSizeChange(size: number): void {
  pagination.pageSize = size;
  pagination.current = 1;
  load();
}

// t-table 的 page-change 给的是 PageInfo(current + pageSize),语义与底部 .pager 的两个事件合并成一个
function onTablePageChange(pageInfo: PageInfo): void {
  pagination.current = pageInfo.current;
  pagination.pageSize = pageInfo.pageSize;
  load();
}

// 移动端卡片上的上下架开关。后端 /admin/dish/update 强校验分类/名称/规格必填,
// 不能只传 status,因此用行内已有数据组装完整 payload;specId 原样带回以增量更新、保留库存。
// 先乐观更新开关状态,请求失败再回滚(错误提示由拦截器统一 toast)。
// 逐行在途标记(用 Set 而非单个值):一行在途时不应把其它行的开关一起锁死 ——
// 全局锁在弱网下的表现就是「点哪一行都没反应」。模板据此逐行 disabled。
const statusBusyIds = ref<Set<number>>(new Set());

async function toggleStatus(d: Dish, val: string | number | boolean): Promise<void> {
  const next = Number(val) === 1 ? 1 : 0;
  const id = Number(d.dishId) || 0;
  if (Number(d.status) === next || statusBusyIds.value.has(id)) return;
  statusBusyIds.value.add(id);
  const previous = Number(d.status) === 1 ? 1 : 0;
  d.status = next;
  try {
    await updateDish({
      dishId: d.dishId,
      categoryId: d.categoryId,
      dishName: d.dishName ?? '',
      dishImage: d.dishImage ?? '',
      description: d.description ?? '',
      status: next,
      soldOut: Number(d.soldOut) === 1 ? 1 : 0,
      signature: Number(d.signature) === 1 ? 1 : 0,
      sortOrder: d.sortOrder ?? 0,
      specs: (d.specs ?? []).map(s => ({
        specId: s.specId,
        specName: s.specName ?? '',
        price: s.price ?? 0,
        stock: s.stock ?? -1
      }))
    });
    MessagePlugin.success(next === 1 ? '已上架' : '已下架');
  } catch {
    d.status = previous;
  } finally {
    statusBusyIds.value.delete(id);
  }
}

// t-upload 的回显项:只带 url(没有 raw)时组件会按普通图片渲染缩略图,
// 但不会触发上传 —— 保存时按 raw 是否存在区分「沿用旧图」与「上传新图」。
function imageFileList(url: string): UploadFile[] {
  if (!url) return [];
  return [{ url, name: url.split('/').pop() || 'dish-image', status: 'success' }];
}

function openAdd(): void {
  Object.assign(form, {
    dishId: null,
    categoryId: categories.value[0]?.categoryId,
    dishName: '',
    dishImage: '',
    description: '',
    status: 1,
    soldOut: 0,
    signature: 0,
    sortOrder: 0,
    specs: [{ specName: '份', price: 0, stock: -1 }],
    imageFiles: []
  });
  dialogVisible.value = true;
}

function openEdit(row: Dish): void {
  Object.assign(form, {
    dishId: row.dishId ?? null,
    categoryId: row.categoryId,
    dishName: row.dishName ?? '',
    dishImage: row.dishImage ?? '',
    description: row.description ?? '',
    status: Number(row.status) === 1 ? 1 : 0,
    soldOut: Number(row.soldOut) === 1 ? 1 : 0,
    signature: Number(row.signature) === 1 ? 1 : 0,
    sortOrder: row.sortOrder ?? 0,
    specs: (row.specs ?? []).map(s => ({
      specId: s.specId,
      specName: s.specName ?? '',
      price: s.price ?? 0,
      stock: s.stock ?? -1
    })),
    imageFiles: imageFileList(row.dishImage ?? '')
  });
  dialogVisible.value = true;
}

function addSpec(): void {
  form.specs.push({ specName: '', price: 0, stock: -1 });
}

function removeSpec(i: number): void {
  form.specs.splice(i, 1);
}

async function save(): Promise<void> {
  if (!form.categoryId || !form.dishName) {
    MessagePlugin.warning('请选择分类并填写菜品名称');
    return;
  }
  if (!form.specs.length) {
    MessagePlugin.warning('请至少添加一个规格');
    return;
  }
  if (saving.value) return;
  saving.value = true;
  try {
    // 图片处理:仅有 raw(本次新选的文件)才需要上传;只带 url 的是编辑回显的旧图,跳过。
    const picked = form.imageFiles[0];
    if (picked?.raw) {
      try {
        const res = await uploadFile(picked.raw);
        form.dishImage = res.url;
      } catch {
        return; // 上传失败:拦截器已弹提示,保持弹窗以便重试
      }
    } else if (!picked) {
      // 用户在编辑态删除了原图且未重新选择 → 明确清空,否则旧路径残留
      form.dishImage = '';
    }

    // imageFiles 是上传组件的本地状态,不是后端字段,提交前剔除
    const payload: DishPayload = {
      dishId: form.dishId ?? undefined,
      categoryId: form.categoryId,
      dishName: form.dishName,
      dishImage: form.dishImage,
      description: form.description,
      status: form.status,
      soldOut: form.soldOut,
      signature: form.signature,
      sortOrder: form.sortOrder,
      // specId 必须带上：后端据此增量更新（保留库存），否则每次都重建规格、库存清零
      specs: form.specs
        .filter(s => s.specName)
        .map(s => ({ specId: s.specId, specName: s.specName, price: s.price, stock: s.stock }))
    };
    try {
      if (form.dishId) {
        await updateDish(payload);
      } else {
        await saveDish(payload);
      }
    } catch {
      return; // 保存失败同样保持弹窗,避免用户重填
    }
    MessagePlugin.success('保存成功');
    dialogVisible.value = false;
    load();
  } finally {
    saving.value = false;
  }
}

function onDelete(row: Dish): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除菜品【${row.dishName}】？`,
    onConfirm: async () => {
      try {
        await deleteDish(row.dishId ?? 0);
        MessagePlugin.success('删除成功');
        load();
      } catch {
        /* 失败已由拦截器统一 toast */
      } finally {
        dlg.hide();
      }
    }
  });
}

// 移动端分类药丸:点选即查
function onCategoryTab(value: string): void {
  query.categoryId = value;
  pagination.current = 1;
  load();
}

onMounted(() => {
  loadCategories();
  load();
});
</script>

<style scoped>
/* 列表视图在窄屏(各列定宽合计约 900px)会超出视口,外层包一层横向滚动,
   让表格在卡片内滑动而非把整页撑出横向滚动条。桌面宽屏不受影响。 */
.table-scroll {
  width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}

.table-scroll :deep(.t-table) {
  min-width: 860px;
}

/* 列表里的缩略图:固定 44px 方形,与「无图」占位同尺寸,行高才整齐。
   overflow:hidden + 圆角让内部招牌角标的外角自动跟随缩略图圆角裁切,不会「方角出框」 */
.thumb-box {
  position: relative;
  width: 44px;
  height: 44px;
  border-radius: 6px;
  overflow: hidden;
}

.thumb {
  width: 100%;
  height: 100%;
  border-radius: 6px;
  object-fit: cover;
  display: block;
  background: #f5f5f5;
}

.thumb-ph {
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  color: var(--ink-4);
}

/* 招牌角标:贴图片左上角,品牌色渐变底 + 白字,三处图片(移动端行卡/表格缩略图/卡片视图)共用。
   尺寸基准按最小场景(列表 44px 缩略图)定 —— 原 10px 字号 + 1px 字距在这种小图上会占掉
   近 8 成宽度,像糊了一块色板;收到 9px + 0.5px 字距后只剩内角(BR)做「缎带」缺口,外角(TL)
   交给容器 overflow 裁切,恒等于缩略图圆角。移动端行卡(68px)与卡片视图(130px 高图片)
   篇幅更大,在下面各自放大一档,避免同一个小角标在大图上显得寒酸。 */
.sig-tag {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 1;
  padding: 1px 4px;
  border-radius: 0 0 5px;
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 9px;
  font-weight: 700;
  line-height: 1.5;
  letter-spacing: 0.5px;
  white-space: nowrap;
  pointer-events: none;
}

/* 移动端行卡缩略图 68px:比列表大一档,角标同步放大,读起来才不局促 */
.m-row-pic .sig-tag {
  padding: 2px 6px;
  font-size: 10px;
  border-bottom-right-radius: 6px;
}

/* 卡片视图图片 130px 高:角标按图放大并加宽内角,与顾客端大图上的观感一致 */
.dish-card .sig-tag {
  padding: 3px 9px;
  font-size: 12px;
  letter-spacing: 1px;
  border-bottom-right-radius: 10px;
}

/* 售罄蒙版(列表缩略图):44px 里放不下带描边的胶囊标签,
   索性让蒙版自己兼作标签 —— 压暗 + 居中「售罄」两个字 */
.thumb-out {
  position: absolute;
  inset: 0;
  border-radius: 6px;
  background: rgb(0 0 0 / 55%);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.cell-name {
  font-weight: 600;
  color: var(--ink);
}

.cell-desc {
  margin-top: 2px;
  font-size: 11.5px;
  color: var(--ink-4);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 规格逐个铺开:名称 + 价格,库存有限时补「余 N」,为 0 标红「售罄」 */
.spec-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
}

.spec-chip {
  font-size: 11.5px;
  padding: 2px 7px;
  border-radius: 4px;
  background: #f3f4f6;
  color: #374151;
  white-space: nowrap;
}

.spec-chip b {
  color: var(--brand-deep);
  font-weight: 700;
}

.spec-chip em {
  font-style: normal;
  margin-left: 4px;
  color: var(--ink-4);
}

.spec-chip-out {
  background: var(--danger-soft);
}

.spec-chip-out em {
  color: var(--danger);
}

.dish-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.dish-card {
  background: #fff;
  border-radius: 14px;
  overflow: hidden;
  box-shadow: var(--shadow-1);
  border: 1px solid var(--line);
  transition: all 0.2s;
}

.dish-card:hover {
  box-shadow: var(--shadow-2);
  transform: translateY(-2px);
}

.pic {
  height: 130px;
  overflow: hidden;
  position: relative;
  background: #f5f5f5;
}

.pic img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.pic-ph {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ink-4);
  font-size: 13px;
}

.flag {
  position: absolute;
  top: 8px;
  right: 8px;
  font-size: 10.5px;
  padding: 3px 9px;
  border-radius: var(--r-pill);
  background: var(--success);
  color: #fff;
  font-weight: 600;
}

.flag.off {
  background: rgb(26 26 26 / 55%);
}

/* 售罄：与「下架」区分开，用琥珀色提示「上架中但今天没了」 */
.flag.soldout {
  background: var(--warning);
  color: #7a5200;
}

/* 售罄蒙版:压暗图片 + 居中「售罄」描边标签,一眼看出今天沽清、顾客端不可下单 */
.pic-mask {
  position: absolute;
  inset: 0;
  background: rgb(0 0 0 / 45%);
  display: flex;
  align-items: center;
  justify-content: center;
}

.pic-mask span {
  color: #fff;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 3px;
  padding: 4px 12px;
  border: 1px solid rgb(255 255 255 / 70%);
  border-radius: var(--r-pill);
}

.info {
  padding: 12px 14px 14px;
}

.n {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.p {
  font-size: 17px;
  font-weight: 800;
  color: var(--brand-deep);
  margin-top: 4px;
}

.p i {
  font-style: normal;
  font-size: 12px;
  margin-right: 1px;
}

.p small {
  font-size: 11px;
  font-weight: 400;
  color: var(--ink-4);
}

.meta {
  font-size: 11px;
  color: var(--ink-4);
  margin-top: 3px;
}

.ops {
  margin-top: 10px;
  display: flex;
  gap: 6px;
}

.ops span {
  font-size: 12px;
  padding: 4px 14px;
  border-radius: var(--r-pill);
  cursor: pointer;
  font-weight: 500;
  transition: all 0.15s;
}

.ops .edit {
  color: var(--brand-deep);
  background: var(--brand-soft);
}

.ops .edit:hover {
  background: #ffe3d4;
}

.ops .del {
  color: var(--danger);
  background: var(--danger-soft);
}

.ops .del:hover {
  background: #ffdcdc;
}

.empty {
  padding: 60px 0;
  text-align: center;
  color: var(--ink-4);
  font-size: 14px;
}

.pager {
  margin-top: 18px;
  display: flex;
  justify-content: flex-end;
}

.spec-editor {
  width: 100%;
}

.spec-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

/* 规格名自适应剩余空间(可收缩),价格/库存固定宽度:各控件宽度之和不会
   超过弹窗表单区,避免出现 x 轴滚动条 */
.spec-name {
  flex: 1 1 auto;
  min-width: 0;
}

.spec-price,
.spec-stock,
.spec-del {
  flex: 0 0 auto;
}

.spec-price,
.spec-stock {
  width: 130px;
}

/* 原来跟在每个规格行后的长提示会撑宽整行,改为编辑区下方单行说明 */
.spec-hint {
  margin-bottom: 8px;
  color: var(--ink-4);
  font-size: 12px;
  line-height: 1.5;
}

@media (width <= 1200px) {
  .dish-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (width <= 900px) {
  .dish-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* 手机(iOS 375~430 / 安卓 360~412):收窄间距、降图片高度,让 2 列卡片更紧凑 */
@media (width <= 767px) {
  .dish-grid {
    gap: 10px;
  }

  .pic {
    height: 110px;
  }

  .info {
    padding: 10px 11px 12px;
  }

  /* 新增/编辑菜品弹窗:左对齐标签(固定 90px)+固定宽规格行在 92vw 弹窗里会挤成一团。
     手机上改为「标签在上、控件占满整行」的竖排表单,信息更清晰、点按区更大 */
  .dish-dialog :deep(.t-form__item) {
    flex-direction: column;
    align-items: stretch;
    margin-bottom: 14px;
  }

  .dish-dialog :deep(.t-form__label) {
    width: 100% !important;
    text-align: left;
    margin-bottom: 6px;
    padding-bottom: 0;
  }

  .dish-dialog :deep(.t-form__controls) {
    width: 100%;
    margin-left: 0 !important;
  }

  /* 规格编辑行:改为纵向堆叠、各控件占满整行(需重置 flex,否则 basis
     在纵向排列时会变成高度) */
  .spec-row {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }

  .spec-name,
  .spec-price,
  .spec-stock {
    flex: none;
    width: 100% !important;
  }
}

/* 超窄屏(320~360px):2 列卡片每列不足 140px,改单列舒展 */
@media (width <= 360px) {
  .dish-grid {
    grid-template-columns: 1fr;
  }
}

/* 移动端菜品行卡右侧的操作按钮:竖排,与卡片右侧对齐 */
.m-row-ops {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 0;
}

/* 菜名行:菜名占满,右侧放上下架开关——状态一眼可见 */
.m-row-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.m-row-head .m-row-name {
  flex: 1;
  min-width: 0;
}

/* 上下架开关:开关 + 状态文字,按状态着色(上架中绿 / 已下架灰) */
.m-row-shelf {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  line-height: 1.4;
  cursor: pointer;
}

.m-row-shelf-txt.is-on {
  color: var(--td-success-color, #2ba471);
  font-weight: 500;
}

.m-row-shelf-txt.is-off {
  color: var(--ink-3);
}

/* 底部行:价格在左、编辑/删除在右,与信息区分离,卡片更紧凑 */
.m-row-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 5px;
}

.m-row-foot .m-row-price {
  margin-top: 0;
}

/* 移动端行卡缩略图容器:只为承载售罄蒙版(原先是裸 img,没有可定位的祖先)。
   尺寸与全局 .m-row-img / .m-row-ph 的 68px 保持一致,不改变行卡排版。
   圆角取 10px(与 .m-row-img 相同)并裁切,招牌角标的外角才会贴合图片圆角。 */
.m-row-pic {
  position: relative;
  flex-shrink: 0;
  width: 68px;
  height: 68px;
  border-radius: 10px;
  overflow: hidden;
}

/* 售罄蒙版(移动端行卡):压暗缩略图 + 居中「售罄」,与桌面卡片 .pic-mask 同一套视觉 */
.m-row-out {
  position: absolute;
  inset: 0;
  border-radius: 10px;
  background: rgb(0 0 0 / 55%);
  color: #fff;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 2px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 已下架:图片整体置灰(顾客端完全看不到,区别于售罄的临时沽清);蒙版文字同为居中白字 */
img.is-off {
  filter: grayscale(1);
  opacity: 0.9;
}
</style>
