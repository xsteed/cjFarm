<template>
  <div
    class="remark-mask"
    @click.self="emit('save')"
  >
    <div class="remark-sheet">
      <div class="remark-head">
        <span>菜品备注</span>
        <span class="remark-target">{{ dishName }}</span>
      </div>
      <div class="remark-options">
        <span
          v-for="r in remarks"
          :key="r.remarkId"
          class="remark-chip"
          :class="{ active: selected.includes(r.optionName || '') }"
          @click="emit('toggle', r.optionName || '')"
          >{{ r.optionName }}</span
        >
      </div>
      <input
        :value="custom"
        class="remark-custom"
        placeholder="其他备注（选填）"
        maxlength="50"
        @input="onCustomInput"
      />
      <button
        class="remark-confirm"
        @click="emit('save')"
      >
        确定
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { RemarkOption } from '../../types/entities';

defineProps<{
  dishName: string;
  selected: string[];
  custom: string;
  remarks: RemarkOption[];
}>();

const emit = defineEmits<{
  toggle: [name: string];
  'update:custom': [value: string];
  save: [];
}>();

function onCustomInput(e: Event): void {
  emit('update:custom', (e.target as HTMLInputElement).value);
}
</script>

<style scoped>
/* ---------- 单项备注弹窗 ---------- */
.remark-mask {
  position: fixed;
  inset: 0;
  background: rgb(0 0 0 / 50%);
  z-index: 200;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.remark-sheet {
  width: 100%;
  max-width: 480px;
  background: #fff;
  border-radius: var(--r-lg) var(--r-lg) 0 0;
  padding: 20px 18px calc(20px + env(safe-area-inset-bottom));
}

.remark-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
}

.remark-target {
  font-size: 12.5px;
  font-weight: 400;
  color: var(--ink-3);
}

.remark-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.remark-chip {
  padding: 7px 18px;
  border-radius: var(--r-pill);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.12s;
  border: 1.5px solid var(--line);
  background: #fafafa;
  color: var(--ink-2);
}

.remark-chip.active {
  border-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}

.remark-custom {
  width: 100%;
  box-sizing: border-box;
  margin-top: 14px;
  padding: 10px 14px;
  border: 1.5px solid var(--line);
  border-radius: var(--r-md);
  font-size: 13.5px;
  color: var(--ink);
  outline: none;
}

.remark-custom:focus {
  border-color: var(--brand);
}

.remark-confirm {
  width: 100%;
  height: 46px;
  margin-top: 16px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15.5px;
  font-weight: 600;
  cursor: pointer;
}
</style>
