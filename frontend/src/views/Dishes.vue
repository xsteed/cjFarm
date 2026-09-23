<template>
  <div class="page-card">
    <div class="toolbar">
      <div class="toolbar-left">
        <t-input
          v-model="query.dishName"
          placeholder="搜索菜品名称"
          clearable
          style="width: 220px"
          @enter="load"
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
          @click="load"
        >
          查询
        </t-button>
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
    </div>

    <!-- 网格卡片 -->
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
            :src="d.dishImage"
            :alt="d.dishName"
          />
          <div
            v-else
            class="pic-ph"
          >
            无图
          </div>
          <div
            class="flag"
            :class="{ off: d.status !== 1 }"
          >
            {{ d.status === 1 ? '上架中' : '已下架' }}
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
            <span
              class="edit"
              @click="openEdit(d)"
              >编辑</span
            >
            <span
              class="del"
              @click="onDelete(d)"
              >删除</span
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
        @current-change="onPageChange"
        @page-size-change="onPageSizeChange"
      />
    </div>

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.dishId ? '编辑菜品' : '新增菜品'"
      width="640px"
      :confirm-btn="{ content: '保存', theme: 'primary' }"
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
                placeholder="规格名，如 份/小份"
                style="width: 160px"
              />
              <t-input-number
                v-model="s.price"
                :min="0"
                :decimal-places="2"
                placeholder="价格"
                style="width: 130px"
              />
              <t-button
                theme="danger"
                variant="text"
                @click="removeSpec(i)"
              >
                删除
              </t-button>
            </div>
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
import { listDishes, saveDish, updateDish, deleteDish, listCategories, uploadFile } from '../api';
import { hasPerm } from '../utils/perm';
import type { UploadFile } from 'tdesign-vue-next';
import type { Dish, MenuCategory, DishPayload } from '../types/entities';

const list = ref<Dish[]>([]);
const categories = ref<MenuCategory[]>([]);
const loading = ref(false);
const query = reactive({ dishName: '', categoryId: '' });
const pagination = reactive({ current: 1, pageSize: 12, total: 0 });
const dialogVisible = ref(false);

interface DishForm {
  dishId: number | null;
  categoryId: number | undefined;
  dishName: string;
  dishImage: string;
  description: string;
  status: number;
  sortOrder: number;
  specs: { specName: string; price: number }[];
  imageFiles: UploadFile[];
}

const form = reactive<DishForm>({
  dishId: null,
  categoryId: undefined,
  dishName: '',
  dishImage: '',
  description: '',
  status: 1,
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

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listDishes({
      pageNum: pagination.current,
      pageSize: pagination.pageSize,
      dishName: query.dishName,
      categoryId: query.categoryId
    });
    list.value = res.items;
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
  categories.value = res.items;
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
    sortOrder: 0,
    specs: [{ specName: '份', price: 0 }],
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
    sortOrder: row.sortOrder ?? 0,
    specs: (row.specs ?? []).map(s => ({ specName: s.specName ?? '', price: s.price ?? 0 })),
    imageFiles: imageFileList(row.dishImage ?? '')
  });
  dialogVisible.value = true;
}

function addSpec(): void {
  form.specs.push({ specName: '', price: 0 });
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
    sortOrder: form.sortOrder,
    specs: form.specs.filter(s => s.specName)
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

onMounted(() => {
  loadCategories();
  load();
});
</script>

<style scoped>
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

  /* 规格编辑行:规格名(160px)+价格(130px)+删除按钮在 92vw 弹窗里会溢出,窄屏允许折行 */
  .spec-row {
    flex-wrap: wrap;
  }

  .spec-row .t-input-number {
    max-width: 140px;
  }
}

/* 超窄屏(320~360px):2 列卡片每列不足 140px,改单列舒展 */
@media (width <= 360px) {
  .dish-grid {
    grid-template-columns: 1fr;
  }
}
</style>
