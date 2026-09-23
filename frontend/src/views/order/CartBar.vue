<template>
  <div
    class="op-bar"
    :class="{ lifted: showTabbar }"
  >
    <div
      class="op-cartbtn"
      :class="{ has: totalCount > 0 }"
      @click="totalCount > 0 && emit('open')"
    >
      <svg
        viewBox="0 0 24 24"
        width="22"
        height="22"
        fill="#fff"
      >
        <path
          d="M7 18a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm10 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM3 2h2.6l.9 2H21a1 1 0 0 1 .94 1.34l-2.6 7.3A2 2 0 0 1 17.46 14H8.4l-.4 2H19v2H6.72a1 1 0 0 1-.98-1.2L6.6 12 4.4 4H3a1 1 0 0 1 0-2z"
        />
      </svg>
      <span
        v-if="totalCount > 0"
        class="b"
        >{{ totalCount }}</span
      >
    </div>
    <div class="op-total">
      <div class="l">
        {{ canAppend ? '本次加菜' : '合计' }}
      </div>
      <div class="v"><small>¥</small>{{ totalPrice }}</div>
    </div>
    <div
      class="op-checkout"
      :class="{ disabled: !totalCount }"
      @click="totalCount > 0 && emit('open')"
    >
      去结算
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  showTabbar: boolean;
  totalCount: number;
  totalPrice: number;
  canAppend: boolean;
}>();

const emit = defineEmits<{
  open: [];
}>();
</script>

<style scoped>
/* ---------- 底部悬浮结算栏 ---------- */
.op-bar {
  position: fixed;
  left: 50%;
  bottom: calc(12px + env(safe-area-inset-bottom));
  transform: translateX(-50%);
  width: calc(100% - 20px);
  max-width: 460px;
  height: 54px;
  background: #fff;
  border-radius: var(--r-pill);
  box-shadow: var(--shadow-2);
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 0 7px;
  z-index: 90;
}

/* 底部有 tabbar 时结算栏整体上移,避免压住导航 */
.op-bar.lifted {
  bottom: calc(70px + env(safe-area-inset-bottom));
}

.op-cartbtn {
  position: relative;
  width: 42px;
  height: 42px;
  border-radius: 50%;
  background: #e5e5e5;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
  cursor: pointer;
  transition: background 0.2s;
}

.op-cartbtn.has {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
}

.op-cartbtn .b {
  position: absolute;
  top: -3px;
  right: -3px;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  border-radius: var(--r-pill);
  background: var(--ink);
  border: 2px solid #fff;
  color: #fff;
  font-size: 9px;
  line-height: 13px;
  font-weight: 700;
  text-align: center;
}

.op-total {
  flex: 1;
}

.op-total .l {
  font-size: 9.5px;
  color: var(--ink-4);
}

.op-total .v {
  font-size: 18px;
  font-weight: 800;
  color: var(--ink);
  line-height: 1.1;
}

.op-total .v small {
  font-size: 11px;
}

.op-checkout {
  height: 42px;
  padding: 0 22px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  cursor: pointer;
  transition: opacity 0.2s;
}

.op-checkout.disabled {
  background: #d8d8d8;
  cursor: not-allowed;
}

@media (width <= 360px) {
  .op-checkout {
    padding: 0 18px;
  }
}
</style>
