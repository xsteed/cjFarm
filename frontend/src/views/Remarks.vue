<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">备注选项将展示在食客点餐页，可为每道菜选择（如：加辣、少盐）</span>
      <t-button v-if="canEdit" theme="primary" @click="openAdd">
        <template #icon><add-icon /></template>
        新增备注
      </t-button>
    </div>

    <!-- 窄屏:改用卡片,避免 ID/排序/操作三列把表格挤到横向滚动 -->
    <div v-if="isMobile" class="m-list">
      <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无备注选项' }}</div>
      <div v-for="row in list" :key="row.remarkId" class="mcard">
        <div class="mcard-hd">
          <span class="mcard-no">{{ row.optionName }}</span>
          <span class="mcard-sub" style="margin: 0">排序 {{ row.sortOrder }}</span>
        </div>
        <div v-if="canEdit" class="mcard-ft">
          <t-button theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </div>
      </div>
    </div>

    <t-table v-else :data="list" :columns="columns" row-key="remarkId" :loading="loading">
      <template #op="{ row }">
        <t-space>
          <t-button theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
        </t-space>
      </template>
    </t-table>

    <t-dialog v-model:visible="dialogVisible" :header="form.remarkId ? '编辑备注' : '新增备注'" width="420px" :confirm-btn="{ content: '保存', theme: 'primary' }" @confirm="save">
      <t-form :data="form" label-width="90px">
        <t-form-item label="备注名称" name="optionName">
          <t-input v-model="form.optionName" placeholder="如 加辣" />
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
import { listRemarkOptions, saveRemark, updateRemark, deleteRemark } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { hasPerm } from '../utils/perm'

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = reactive({ remarkId: null, optionName: '', sortOrder: 0 })
const { isMobile } = useIsMobile()

// 无 remark:edit 时隐藏「新增/编辑/删除」——只读浏览。后端接口同样会 403。
const canEdit = computed(() => hasPerm('remark:edit'))

const columns = computed(() => {
  const cols = [
    { colKey: 'remarkId', title: 'ID', width: 90 },
    { colKey: 'optionName', title: '备注名称' },
    { colKey: 'sortOrder', title: '排序', width: 120 }
  ]
  if (canEdit.value) cols.push({ colKey: 'op', title: '操作', width: 150 })
  return cols
})

async function load() {
  loading.value = true
  try {
    const res = await listRemarkOptions()
    list.value = res.rows
  } finally {
    loading.value = false
  }
}

function openAdd() {
  Object.assign(form, { remarkId: null, optionName: '', sortOrder: list.value.length + 1 })
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, { remarkId: row.remarkId, optionName: row.optionName, sortOrder: row.sortOrder })
  dialogVisible.value = true
}

async function save() {
  if (!form.optionName) {
    MessagePlugin.warning('请填写备注名称')
    return
  }
  if (form.remarkId) {
    await updateRemark(form)
  } else {
    await saveRemark(form)
  }
  MessagePlugin.success('保存成功')
  dialogVisible.value = false
  load()
}

function onDelete(row) {
  DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除备注【${row.optionName}】？`,
    onConfirm: async () => {
      await deleteRemark(row.remarkId)
      MessagePlugin.success('删除成功')
      load()
    }
  })
}

onMounted(load)
</script>
