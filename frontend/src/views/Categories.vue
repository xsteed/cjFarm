<template>
  <SimpleCrudPage
    tip="菜品分类将展示在食客点餐页的左侧菜单栏"
    empty-label="暂无分类"
    name-label="分类名称"
    name-placeholder="如 荤菜"
    add-label="新增分类"
    edit-label="编辑分类"
    delete-noun="分类"
    edit-perm="category:edit"
    id-title="ID"
    :list-api="listApi"
    :save-api="saveApi"
    :delete-api="deleteApi"
  />
</template>

<script setup lang="ts">
// 简单 CRUD 列表页的薄封装:列表/表单/删除的通用逻辑在 components/SimpleCrudPage.vue,
// 本页只负责「分类」数据源到通用行结构(CrudRow)的映射与接口调用。
import SimpleCrudPage from '../components/SimpleCrudPage.vue';
import type { CrudRow } from '../components/SimpleCrudPage.vue';
import { listCategories, saveCategory, updateCategory, deleteCategory } from '../api';
import type { CategoryPayload } from '../types/entities';

async function listApi(): Promise<CrudRow[]> {
  const res = await listCategories();
  return (res.items || []).map(c => ({
    id: c.categoryId ?? null,
    name: c.categoryName || '',
    sortOrder: c.sortOrder ?? 0
  }));
}

async function saveApi(row: CrudRow): Promise<void> {
  const payload: CategoryPayload = {
    categoryId: row.id ?? undefined,
    categoryName: row.name,
    sortOrder: row.sortOrder
  };
  if (row.id) {
    await updateCategory(payload);
  } else {
    await saveCategory(payload);
  }
}

async function deleteApi(id: number): Promise<void> {
  await deleteCategory(id);
}
</script>
