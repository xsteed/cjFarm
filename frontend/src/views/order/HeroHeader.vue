<template>
  <div class="op-hero">
    <div class="op-row">
      <div class="op-shop">
        {{ shopName || '扫码点餐' }}
      </div>
      <div
        v-if="table.tableNo"
        class="op-chip"
      >
        <svg
          viewBox="0 0 24 24"
          width="13"
          height="13"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <circle
            cx="12"
            cy="8"
            r="5"
          />
          <path d="M4 21h16M12 13v8" />
        </svg>
        {{ tableLabel }}<template v-if="table.capacity"> · {{ table.capacity }}人桌 </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { TableInfo } from '../../types/entities';

const props = defineProps<{
  shopName: string;
  table: TableInfo;
}>();

const tableLabel = computed(() => props.table.tableName || (props.table.tableNo ? `${props.table.tableNo}号桌` : ''));
</script>

<style scoped>
/* ---------- 顶部 hero:单行紧凑布局,压缩高度给菜单留更多空间 ---------- */
.op-hero {
  background: linear-gradient(135deg, #ff8a50 0%, #ff6b35 55%, #f0481f 100%);

  /* 顶部预留刘海/灵动岛安全区(iPhone X 及以后),避免店名被状态栏裁切 */
  padding: calc(26px + env(safe-area-inset-top, 0px)) 18px 24px;
  position: relative;
  overflow: hidden;
  flex-shrink: 0;
}

.op-hero::after {
  content: '';
  position: absolute;
  right: -28px;
  top: -34px;
  width: 120px;
  height: 120px;
  border-radius: 50%;
  background: rgb(255 255 255 / 12%);
}

/* 左下第二枚装饰圆,让渐变背景有前后层次而不是一块平色 */
.op-hero::before {
  content: '';
  position: absolute;
  left: -36px;
  bottom: -52px;
  width: 110px;
  height: 110px;
  border-radius: 50%;
  background: rgb(255 255 255 / 8%);
}

/* 店名靠左、桌台胶囊靠右,两端锚定比堆在一侧更稳 */
.op-row {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.op-shop {
  position: relative;
  font-size: 17px;
  font-weight: 800;
  color: #fff;
  letter-spacing: 0.3px;
  flex-shrink: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.op-chip {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border-radius: var(--r-pill);
  background: rgb(255 255 255 / 20%);
  border: 1px solid rgb(255 255 255 / 35%);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  flex-shrink: 0;
  white-space: nowrap;
}
</style>
