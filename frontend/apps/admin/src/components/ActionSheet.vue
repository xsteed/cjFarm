<template>
  <teleport to="body">
    <transition name="sheet-mask">
      <div
        v-if="modelValue"
        class="sheet-mask"
        @click="close"
      ></div>
    </transition>
    <transition name="sheet-panel">
      <div
        v-if="modelValue"
        ref="panelRef"
        class="sheet-panel"
        role="dialog"
        aria-modal="true"
        :aria-label="title || '操作'"
        tabindex="-1"
        :style="panelStyle"
      >
        <div class="sheet-grab"></div>
        <div
          v-if="title"
          class="sheet-title"
        >
          {{ title }}
        </div>
        <div class="as-list">
          <button
            v-for="a in actions"
            :key="a.key"
            type="button"
            class="as-item"
            :class="[a.theme || 'default', { 'is-disabled': a.disabled }]"
            :disabled="a.disabled"
            @click="onPick(a)"
          >
            <span
              v-if="a.icon"
              class="as-ic"
              ><component :is="a.icon"
            /></span>
            <span class="as-label">{{ a.label }}</span>
          </button>
        </div>
        <button
          type="button"
          class="sheet-cancel"
          @click="close"
        >
          取消
        </button>
      </div>
    </transition>
  </teleport>
</template>

<script setup lang="ts">
import { ref, toRef } from 'vue';
import type { Component } from 'vue';
import { useSheetA11y } from '../composables/useSheetA11y';
import { useSheetSwipe } from '../composables/useSheetSwipe';

/** 底部操作面板里的一项操作 */
export interface SheetAction {
  key: string;
  label: string;
  theme?: 'default' | 'primary' | 'success' | 'warning' | 'danger';
  icon?: Component;
  disabled?: boolean;
}

const props = defineProps<{
  modelValue: boolean;
  title?: string;
  actions: SheetAction[];
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  pick: [key: string];
}>();

function close(): void {
  emit('update:modelValue', false);
}

function onPick(a: SheetAction): void {
  if (a.disabled) return;
  emit('pick', a.key);
}

// 焦点陷阱 + Esc 关闭
const panelRef = ref<HTMLElement | null>(null);
useSheetA11y(panelRef, toRef(props, 'modelValue'), close);
// 下拉关闭手势(抓取条)
const { panelStyle } = useSheetSwipe(panelRef, toRef(props, 'modelValue'), close);
</script>

<style scoped>
.as-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
  min-height: 0;
}

/* 操作项:整行大按钮,拇指友好(52px 高) */
.as-item {
  flex-shrink: 0;
  width: 100%;
  height: 52px;
  border: none;
  border-radius: 12px;
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  cursor: pointer;
  transition: background 0.15s;
}

.as-item:active {
  background: #ececec;
}

.as-ic {
  display: inline-flex;
  align-items: center;
  font-size: 18px;
}

.as-item.primary {
  color: var(--brand-deep);
}

.as-item.success {
  color: var(--success);
}

.as-item.warning {
  color: #d48a00;
}

.as-item.danger {
  color: var(--danger);
}

.as-item.is-disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
