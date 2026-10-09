<template>
  <div class="op-search">
    <span class="search-ico"></span>
    <input
      :value="keyword"
      class="search-input"
      placeholder="搜索菜名 / 口味，如「五指毛桃鸡」"
      enterkeyhint="search"
      @input="onInput"
    />
    <span
      v-if="keyword"
      class="search-clear"
      @click="emit('update:keyword', '')"
      >×</span
    >
  </div>
</template>

<script setup lang="ts">
defineProps<{ keyword: string }>();
const emit = defineEmits<{ 'update:keyword': [value: string] }>();

function onInput(e: Event): void {
  emit('update:keyword', (e.target as HTMLInputElement).value);
}
</script>

<style scoped>
/* 搜索框:放在白卡最顶部(不随菜单滚动),菜多的店一眼能看到。位置与小程序一致 */
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

.search-clear {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #d8d8d8;
  color: #fff;
  font-size: 14px;
  line-height: 18px;
  text-align: center;
  cursor: pointer;
}
</style>
