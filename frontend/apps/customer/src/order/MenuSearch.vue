<template>
  <!-- 展开态:白卡最顶部(不随菜单滚动)。入口收进 hero 头部,是否显示由页面层控制 -->
  <div
    v-if="open"
    class="op-search"
  >
    <span class="search-ico"></span>
    <input
      ref="inputRef"
      :value="keyword"
      class="search-input"
      placeholder="搜索菜名 / 口味，如「五指毛桃鸡」"
      enterkeyhint="search"
      @input="onInput"
    />
    <!-- 用 button 而不是 span:可聚焦、可键盘触发,读屏也能读出「清空搜索」 -->
    <button
      type="button"
      class="search-clear"
      aria-label="清空搜索"
      @click="onClear"
    >
      ×
    </button>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';

const props = defineProps<{
  keyword: string;
  /** 搜索框展开态:由页面层持有(hero 头部的搜索按钮控制展开) */
  open: boolean;
}>();
const emit = defineEmits<{
  'update:keyword': [value: string];
  'update:open': [value: boolean];
}>();

const inputRef = ref<HTMLInputElement>();

// 打开时聚焦输入框
watch(
  () => props.open,
  v => {
    if (v) nextTick(() => inputRef.value?.focus());
  }
);

// 有词(历史输入)时保持展开,避免「框收起了但还在搜」的错位
watch(
  () => props.keyword,
  v => {
    if (v && !props.open) emit('update:open', true);
  }
);

function onInput(e: Event): void {
  emit('update:keyword', (e.target as HTMLInputElement).value);
}

function onClear(): void {
  if (props.keyword) {
    emit('update:keyword', '');
    nextTick(() => inputRef.value?.focus());
    return;
  }
  // 空词点 ×:收起搜索框
  emit('update:open', false);
}
</script>

<style scoped>
.op-search {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
  height: 36px;
  margin: 10px 12px 8px;
  padding: 0 12px;
  border-radius: var(--r-pill);
  background: #f5f5f5;
}

/* 放大镜:圆圈 + 45° 短柄 */
.search-ico {
  position: relative;
  flex-shrink: 0;
  width: 12px;
  height: 12px;
  border: 1.5px solid var(--ink-3);
  border-radius: 50%;
  box-sizing: border-box;
}

.search-ico::after {
  position: absolute;
  right: -4px;
  bottom: -2px;
  width: 6px;
  height: 1.5px;
  border-radius: 1px;
  background: var(--ink-3);
  transform: rotate(45deg);
  content: '';
}

.search-input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--ink);
  font-size: 13.5px;
  outline: none;
}

.search-input::placeholder {
  color: var(--ink-4);
}

/* ×:有词清词,空词收起,可点热区放宽 */
.search-clear {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  margin-right: -8px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: none;
  color: var(--ink-3);
  font-family: inherit;
  font-size: 18px;
  line-height: 24px;
  text-align: center;
  cursor: pointer;
}
</style>
