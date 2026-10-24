<template>
  <SimpleCrudPage
    tip="备注选项将展示在食客点餐页，可为每道菜选择（如：加辣、少盐）"
    empty-label="暂无备注选项"
    name-label="备注名称"
    name-placeholder="如 加辣"
    add-label="新增备注"
    edit-label="编辑备注"
    delete-noun="备注"
    edit-perm="remark:edit"
    id-title="ID"
    :list-api="listApi"
    :save-api="saveApi"
    :delete-api="deleteApi"
  />
</template>

<script setup lang="ts">
// 简单 CRUD 列表页的薄封装:列表/表单/删除的通用逻辑在 components/SimpleCrudPage.vue,
// 本页只负责「备注」数据源到通用行结构(CrudRow)的映射与接口调用。
import SimpleCrudPage from '../components/SimpleCrudPage.vue';
import type { CrudRow } from '../components/SimpleCrudPage.vue';
import { listRemarkOptions, saveRemark, updateRemark, deleteRemark } from '@dining/api';
import type { RemarkPayload } from '@dining/types';

async function listApi(): Promise<CrudRow[]> {
  const res = await listRemarkOptions();
  return (res.items || []).map(r => ({
    id: r.remarkId ?? null,
    name: r.optionName || '',
    sortOrder: r.sortOrder ?? 0
  }));
}

async function saveApi(row: CrudRow): Promise<void> {
  const payload: RemarkPayload = {
    remarkId: row.id ?? undefined,
    optionName: row.name,
    sortOrder: row.sortOrder
  };
  // id === 0 也是合法主键时才不该走新增;当前自增从 1 起,但用 != null 更稳
  if (row.id != null) {
    await updateRemark(payload);
  } else {
    await saveRemark(payload);
  }
}

async function deleteApi(id: number): Promise<void> {
  await deleteRemark(id);
}
</script>
