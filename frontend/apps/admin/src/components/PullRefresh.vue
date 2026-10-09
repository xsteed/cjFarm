<template>
  <div
    class="m-ptr"
    :class="{ 'm-ptr-anim': !pulling }"
    :style="{ height: distance + 'px' }"
  >
    <span class="m-ptr-txt">
      {{ refreshing ? '正在刷新…' : distance > 0 ? (distance >= 46 ? '松开立即刷新' : '下拉刷新') : '' }}
    </span>
  </div>
</template>

<script setup lang="ts">
// 下拉刷新指示器:高度由 usePullRefresh 的 distance 驱动,随手指下拉把内容「拉下来」,
// 松手后刷新中保持一段高度、加载结束再收回。
defineProps<{
  distance: number;
  refreshing: boolean;
  pulling: boolean;
}>();
</script>

<style scoped>
.m-ptr {
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  font-size: 12.5px;
  color: var(--ink-3);
}

/* 手指跟随时关掉过渡,避免高度滞后于手指;松手后再平滑收回 */
.m-ptr-anim {
  transition: height 0.24s ease;
}

.m-ptr-txt {
  white-space: nowrap;
}
</style>
