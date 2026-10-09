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
        :aria-label="title || '筛选'"
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
        <div class="sheet-body">
          <slot />
        </div>
        <div
          v-if="$slots.footer"
          class="bs-foot"
        >
          <slot name="footer" />
        </div>
        <button
          v-else
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
import { useSheetA11y } from '../composables/useSheetA11y';
import { useSheetSwipe } from '../composables/useSheetSwipe';

const props = defineProps<{
  modelValue: boolean;
  title?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

function close(): void {
  emit('update:modelValue', false);
}

// 焦点陷阱 + Esc 关闭
const panelRef = ref<HTMLElement | null>(null);
useSheetA11y(panelRef, toRef(props, 'modelValue'), close);
// 下拉关闭手势(抓取条)
const { panelStyle } = useSheetSwipe(panelRef, toRef(props, 'modelValue'), close);
</script>

<style scoped>
.bs-foot {
  flex-shrink: 0;
  display: flex;
  gap: 10px;
  margin-top: 14px;
}

.bs-foot :deep(.t-button) {
  flex: 1;
}
</style>
