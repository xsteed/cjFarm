<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">菜品分类将展示在食客点餐页的左侧菜单栏</span>
      <t-button v-if="canEdit" theme="primary" @click="openAdd">
        <template #icon><add-icon /></template>
        新增分类
      </t-button>
    </div>

    <!-- 窄屏:改用卡片,避免 ID/排序/操作三列把表格挤到横向滚动 -->
    <div v-if="isMobile" class="m-list">
      <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无分类' }}</div>
      <div v-for="row in list" :key="row.categoryId" class="mcard">
        <div class="mcard-hd">
          <span class="mcard-no">{{ row.categoryName }}</span>
          <span class="mcard-sub" style="margin: 0">排序 {{ row.sortOrder }}</span>
        </div>
        <div v-if="canEdit" class="mcard-ft">
          <t-button theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </div>
      </div>
    </div>

    <t-table v-else :data="list" :columns="columns" row-key="categoryId" :loading="loading">
      <template #op="{ row }">
        <t-space>
          <t-button theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </t-space>
      </template>
    </t-table>

    <t-dialog v-model:visible="dialogVisible" :header="form.categoryId ? '编辑分类' : '新增分类'" width="420px" :confirm-btn="{ content: '保存', theme: 'primary' }" @confirm="save">
      <t-form :data="form" label-width="90px">
        <t-form-item label="分类名称" name="categoryName">
          <t-input v-model="form.categoryName" placeholder="如 荤菜" />
        </t-form-item>
        <t-form-item label="排序" name="sortOrder">
          <t-input-number v-model="form.sortOrder" :min="0" />
        </t-form-item>
      </t-form>
    </t-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { listCategories, saveCategory, updateCategory, deleteCategory } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = reactive({ categoryId: null, categoryName: '', sortOrder: 0 })
const { isMobile } = useIsMobile()

// 无 category:edit 时隐藏「新增/编辑/删除」——只读浏览。后端同样会 403，这里只是体验层。
const canEdit = computed(() => hasPerm('category:edit'))

// 只读账号不渲染「操作」列,否则会出现一整列空白单元格。
const columns = computed(() => {
  const cols = [
    { colKey: 'categoryId', title: 'ID', width: 90 },
    { colKey: 'categoryName', title: '分类名称' },
    { colKey: 'sortOrder', title: '排序', width: 120 }
  ]
  if (canEdit.value) cols.push({ colKey: 'op', title: '操作', width: 150 })
  return cols
})

async function load() {
  loading.value = true
  try {
    const res = await listCategories()
    list.value = res.rows
  } finally {
    loading.value = false
  }
}

function openAdd() {
  Object.assign(form, { categoryId: null, categoryName: '', sortOrder: list.value.length + 1 })
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, { categoryId: row.categoryId, categoryName: row.categoryName, sortOrder: row.sortOrder })
  dialogVisible.value = true
}

async function save() {
  if (!form.categoryName) {
    MessagePlugin.warning('请填写分类名称')
    return
  }
  if (form.categoryId) {
    await updateCategory(form)
  } else {
    await saveCategory(form)
  }
  MessagePlugin.success('保存成功')
  dialogVisible.value = false
  load()
}

function onDelete(row) {
  DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除分类【${row.categoryName}】？`,
    onConfirm: async () => {
      await deleteCategory(row.categoryId)
      MessagePlugin.success('删除成功')
      load()
    }
  })
}

onMounted(load)
</script>
