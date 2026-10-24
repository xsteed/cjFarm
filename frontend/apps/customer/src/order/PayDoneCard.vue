<template>
  <div
    class="pd-mask"
    @click.self="emit('close')"
  >
    <div class="pd-card">
      <span class="pd-check">
        <svg
          viewBox="0 0 24 24"
          width="26"
          height="26"
          fill="none"
          stroke="currentColor"
          stroke-width="3"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M5 12.5l4.6 4.6L19 7.4" />
        </svg>
      </span>
      <div class="pd-title">支付成功</div>
      <div class="pd-amt">¥{{ amount.toFixed(2) }}</div>
      <div
        v-if="sub"
        class="pd-sub"
      >
        {{ sub }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// 付款成功反馈:轮询发现「刚变成已支付」时弹一下,把全流程的情绪高点补回来。
// 纯展示 + 自动消失(父组件定时关闭),点击遮罩可立即关掉,不挡后续操作。
withDefaults(
  defineProps<{
    amount: number;
    sub?: string;
  }>(),
  { sub: '' }
);

const emit = defineEmits<{
  close: [];
}>();
</script>

<style scoped>
.pd-mask {
  position: fixed;
  inset: 0;
  z-index: 120;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(26 26 26 / 28%);
  animation: pd-fade 0.18s ease-out both;
}

.pd-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  min-width: 190px;
  padding: 24px 28px 20px;
  border-radius: var(--r-xl);
  background: #fff;
  box-shadow: var(--shadow-2);
  animation: pd-pop 0.28s cubic-bezier(0.2, 1.3, 0.4, 1) both;
}

.pd-check {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  border-radius: 50%;
  background: var(--success-soft);
  color: var(--success);
}

/* 勾选描边动画:stroke-dasharray 覆盖整条路径,由 offset 拉入 */
.pd-check svg {
  stroke-dasharray: 30;
  stroke-dashoffset: 30;
  animation: pd-draw 0.4s 0.12s ease-out forwards;
}

.pd-title {
  margin-top: 6px;
  color: var(--ink);
  font-size: 15px;
  font-weight: 700;
}

.pd-amt {
  color: var(--brand-deep);
  font-size: 24px;
  font-weight: 800;
  line-height: 1.15;
}

.pd-sub {
  color: var(--ink-3);
  font-size: 12px;
}

@keyframes pd-fade {
  from {
    opacity: 0;
  }
}

@keyframes pd-pop {
  from {
    opacity: 0;
    transform: scale(0.86);
  }
}

@keyframes pd-draw {
  to {
    stroke-dashoffset: 0;
  }
}

/* 关掉动效偏好时只留结果,不做位移/描边 */
@media (prefers-reduced-motion: reduce) {
  .pd-mask,
  .pd-card,
  .pd-check svg {
    animation: none;
  }

  .pd-check svg {
    stroke-dashoffset: 0;
  }
}
</style>
