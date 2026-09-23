<template>
  <div class="op-tabbar">
    <div
      class="tb-item"
      :class="{ on: view === 'menu' }"
      @click="emit('switch', 'menu')"
    >
      <span class="tb-icon">
        <svg
          viewBox="0 0 24 24"
          width="21"
          height="21"
          fill="none"
          stroke="currentColor"
          stroke-width="1.9"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M4 6h16M4 12h16M4 18h10" />
        </svg>
      </span>
      <span class="tb-text">点餐</span>
    </div>
    <div
      class="tb-item"
      :class="{ on: view === 'order' }"
      @click="emit('switch', 'order')"
    >
      <span class="tb-icon">
        <svg
          viewBox="0 0 24 24"
          width="21"
          height="21"
          fill="none"
          stroke="currentColor"
          stroke-width="1.9"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2" />
          <rect
            x="9"
            y="3"
            width="6"
            height="4"
            rx="1"
          />
          <path d="M9 13h6M9 17h4" />
        </svg>
        <span
          v-if="orderedCount > 0"
          class="tb-badge"
          >{{ orderedCount }}</span
        >
      </span>
      <span class="tb-text">订单</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { ViewState } from '../../composables/useOrderPolling';

defineProps<{
  view: ViewState;
  orderedCount: number;
}>();

const emit = defineEmits<{
  switch: [view: 'menu' | 'order'];
}>();
</script>

<style scoped>
/* ---------- 底部导航(点餐 / 订单) ---------- */
.op-tabbar {
  position: fixed;
  left: 50%;
  bottom: 0;
  transform: translateX(-50%);
  width: 100%;
  max-width: 480px;
  min-height: 58px;
  padding-bottom: env(safe-area-inset-bottom);
  background: #fff;
  border-top: 1px solid var(--line);
  box-shadow: 0 -2px 12px rgb(0 0 0 / 5%);
  display: flex;
  z-index: 88;
}

.tb-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  padding: 8px 0;
  color: var(--ink-4);
  font-size: 10.5px;
  cursor: pointer;
  transition: color 0.15s;
}

.tb-item.on {
  color: var(--brand-deep);
  font-weight: 700;
}

.tb-icon {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 21px;
}

.tb-badge {
  position: absolute;
  top: -5px;
  right: -10px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: var(--r-pill);
  background: var(--brand);
  border: 1.5px solid #fff;
  color: #fff;
  font-size: 9px;
  font-weight: 700;
  line-height: 13px;
  text-align: center;
}
</style>
