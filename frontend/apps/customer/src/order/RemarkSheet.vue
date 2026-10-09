<template>
  <div
    class="remark-mask"
    @click.self="emit('close')"
  >
    <div class="remark-sheet">
      <div class="remark-head">
        <span class="remark-title">菜品备注</span>
        <!-- 右上角的关闭必须与「确定」严格区分:点它是「不改了」,不能写回购物车 -->
        <span
          class="remark-close"
          @click="emit('close')"
          >×</span
        >
      </div>
      <div class="remark-target">{{ dishName }}</div>
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
      <div class="remark-foot">
        <button
          v-if="hasRemark"
          class="remark-clear"
          @click="emit('clear')"
        >
          清除
        </button>
        <button
          class="remark-confirm"
          @click="emit('save')"
        >
          确定
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { RemarkOption } from '@dining/types';

defineProps<{
  dishName: string;
  selected: string[];
  custom: string;
  remarks: RemarkOption[];
  /** 该行是否已有备注:只有已有备注时「清除」才有意义 */
  hasRemark: boolean;
}>();

const emit = defineEmits<{
  toggle: [name: string];
  'update:custom': [value: string];
  clear: [];
  close: [];
  save: [];
}>();

function onCustomInput(e: Event): void {
  emit('update:custom', (e.target as HTMLInputElement).value);
}
</script>

<style scoped>
/* ---------- 单项备注弹窗 ---------- */
/* 层级必须高过购物车抽屉:TDesign 的 .t-drawer 是 z-index 1500,
   低于它的话弹层会整个被抽屉盖住,表现为「点了添加备注没反应」。 */
.remark-mask {
  position: fixed;
  inset: 0;
  background: rgb(0 0 0 / 50%);
  z-index: 1600;
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
}

.remark-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
}

/* 关闭按钮:放大点击热区到 32px,手指点得中 */
.remark-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  margin-right: -6px;
  color: var(--ink-4);
  font-size: 20px;
  line-height: 1;
  cursor: pointer;
}

.remark-target {
  margin-top: 2px;
  font-size: 12.5px;
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

/* 底部两个按钮:清除是次要操作,不抢「确定」的视线 */
.remark-foot {
  display: flex;
  gap: 10px;
  margin-top: 16px;
}

.remark-clear {
  flex-shrink: 0;
  height: 46px;
  padding: 0 18px;
  border: 1.5px solid var(--line);
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--ink-3);
  font-size: 15px;
  cursor: pointer;
}

.remark-confirm {
  flex: 1;
  height: 46px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15.5px;
  font-weight: 600;
  cursor: pointer;
}
</style>
