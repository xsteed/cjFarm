<template>
  <div class="op-hero">
    <!-- 单行布局:店名靠左(过长省略),桌号胶囊与搜索小圆钮成组靠右 -->
    <div class="op-row">
      <div class="op-shop">
        {{ shopName || '扫码点餐' }}
      </div>
      <div class="op-right">
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
          {{ tableLabel }}
        </div>
        <!-- 搜索入口:紧跟桌号胶囊的小圆钮,点开进独立搜索页 -->
        <button
          v-if="showSearch"
          class="op-searchbtn"
          aria-label="搜索菜品"
          @click="emit('search')"
        >
          <span class="ico"></span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { TableInfo } from '@dining/types';

const props = defineProps<{
  shopName: string;
  table: TableInfo;
  /** 是否显示搜索入口(仅菜单视图) */
  showSearch?: boolean;
}>();

const emit = defineEmits<{
  search: [];
}>();

const tableLabel = computed(() => props.table.tableName || (props.table.tableNo ? `${props.table.tableNo}号桌` : ''));
</script>

<style scoped>
/* ---------- 顶部 hero:单行紧凑布局 ---------- */
.op-hero {
  background: linear-gradient(135deg, #ff8a50 0%, #ff6b35 55%, #f0481f 100%);

  /* 安全区(刘海/灵动岛/独立窗口)保留。16px 顶 / 28px 底:白卡上移 16px 后内容行在可见橙色区域居中 */
  padding: calc(16px + env(safe-area-inset-top, 0px)) 18px 28px;
  position: relative;
  overflow: hidden;
  flex-shrink: 0;
}

.op-hero::after {
  pointer-events: none; /* 装饰圆不拦点击,否则会盖住右上角的搜索按钮 */
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
  pointer-events: none;
  content: '';
  position: absolute;
  left: -36px;
  bottom: -52px;
  width: 110px;
  height: 110px;
  border-radius: 50%;
  background: rgb(255 255 255 / 8%);
}

/* 单行:店名靠左、桌号胶囊与搜索钮成组靠右,两端锚定 */
.op-row {
  position: relative;
  z-index: 1; /* 压过右上角装饰圆,避免视觉与点击都被盖 */
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.op-shop {
  position: relative;
  z-index: 1;
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

/* 右侧组:桌号胶囊 + 搜索小圆钮并排 */
.op-right {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
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

/* 搜索入口:与桌号胶囊同高的小圆钮,放大镜用纯 CSS(与 MenuSearch 同款) */
.op-searchbtn {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 27px;
  height: 27px;
  padding: 0;
  border: 1px solid rgb(255 255 255 / 35%);
  border-radius: 50%;
  background: rgb(255 255 255 / 20%);
  cursor: pointer;
  flex-shrink: 0;
}

.op-searchbtn:active {
  background: rgb(255 255 255 / 32%);
}

.op-searchbtn .ico {
  position: relative;
  width: 11px;
  height: 11px;
  border: 1.5px solid rgb(255 255 255 / 90%);
  border-radius: 50%;
  box-sizing: border-box;
}

.op-searchbtn .ico::after {
  position: absolute;
  right: -4px;
  bottom: -2px;
  width: 6px;
  height: 1.5px;
  border-radius: 1px;
  background: rgb(255 255 255 / 90%);
  transform: rotate(45deg);
  content: '';
}
</style>
