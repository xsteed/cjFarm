<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">{{ tip }}</span>
      <t-button
        v-if="canEdit"
        theme="primary"
        @click="openAdd"
      >
        <template #icon>
          <add-icon />
        </template>
        {{ addLabel }}
      </t-button>
    </div>

    <!-- 窄屏:改用卡片,避免 ID/排序/操作三列把表格挤到横向滚动 -->
    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : emptyLabel }}
      </div>
      <div
        v-for="row in list"
        :key="row.id ?? 0"
        class="mcard"
      >
        <div class="mcard-hd">
          <span class="mcard-no">{{ row.name }}</span>
          <span
            class="mcard-sub"
            style="margin: 0"
            >排序 {{ row.sortOrder }}</span
          >
        </div>
        <div
          v-if="canEdit"
          class="mcard-ft"
        >
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
        </div>
      </div>
    </div>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="id"
      :loading="loading"
    >
      <template #op="{ row }">
        <t-space>
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

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.id ? editLabel : addLabel"
      width="420px"
      :confirm-btn="{ content: '保存', theme: 'primary' }"
      @confirm="save"
    >
      <t-form
        :data="form"
        label-width="90px"
      >
        <t-form-item
          :label="nameLabel"
          name="name"
        >
          <t-input
            v-model="form.name"
            :placeholder="namePlaceholder"
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';

/** 简单 CRUD 列表页的通用行结构:ID + 名称 + 排序。 */
export interface CrudRow {
  id: number | null;
  name: string;
  sortOrder: number;
}

const props = defineProps<{
  /** 工具栏提示文案 */
  tip: string;
  /** 列表空态文案 */
  emptyLabel: string;
  /** 表单名称字段标签(如 分类名称/备注名称) */
  nameLabel: string;
  /** 名称输入框 placeholder */
  namePlaceholder: string;
  /** 新增按钮/弹窗标题文案 */
  addLabel: string;
  /** 编辑弹窗标题文案 */
  editLabel: string;
  /** 删除确认文案里的名词(如 分类/备注) */
  deleteNoun: string;
  /** 写操作权限码,无则只读浏览 */
  editPerm: string;
  /** 表格 ID 列标题 */
  idTitle: string;
  /** 拉取列表(由调用方完成数据源到 CrudRow 的映射) */
  listApi: () => Promise<CrudRow[]>;
  /** 保存(新增/编辑由 row.id 区分,由调用方实现) */
  saveApi: (row: CrudRow) => Promise<void>;
  /** 删除 */
  deleteApi: (id: number) => Promise<void>;
}>();

const list = ref<CrudRow[]>([]);
const loading = ref(false);
const dialogVisible = ref(false);
const form = reactive<CrudRow>({ id: null, name: '', sortOrder: 0 });
const { isMobile } = useIsMobile();

// 无写权限时隐藏「新增/编辑/删除」——只读浏览。后端接口同样会 403,这里只是体验层。
const canEdit = computed(() => hasPerm(props.editPerm));

// 只读账号不渲染「操作」列,否则会出现一整列空白单元格。
const columns = computed(() => {
  const cols = [
    { colKey: 'id', title: props.idTitle, width: 90 },
    { colKey: 'name', title: props.nameLabel },
    { colKey: 'sortOrder', title: '排序', width: 120 }
  ];
  if (canEdit.value) cols.push({ colKey: 'op', title: '操作', width: 150 });
  return cols;
});

async function load(): Promise<void> {
  loading.value = true;
  try {
    list.value = await props.listApi();
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    loading.value = false;
  }
}

function openAdd(): void {
  Object.assign(form, { id: null, name: '', sortOrder: list.value.length + 1 });
  dialogVisible.value = true;
}

function openEdit(row: CrudRow): void {
  Object.assign(form, { id: row.id ?? null, name: row.name || '', sortOrder: row.sortOrder ?? 0 });
  dialogVisible.value = true;
}

async function save(): Promise<void> {
  if (!form.name) {
    MessagePlugin.warning(`请填写${props.nameLabel}`);
    return;
  }
  try {
    await props.saveApi({ ...form });
    MessagePlugin.success('保存成功');
    dialogVisible.value = false;
    load();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供修改重试 */
  }
}

function onDelete(row: CrudRow): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除${props.deleteNoun}【${row.name}】？`,
    onConfirm: async () => {
      try {
        await props.deleteApi(row.id ?? 0);
        MessagePlugin.success('删除成功');
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
