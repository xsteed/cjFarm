<template>
  <div
    class="m-ptr"
    :class="{ 'm-ptr-anim': !pulling }"
    :style="{ height: distance + 'px' }"
  >
    <span class="m-ptr-txt">
      {{ refreshing ? '正在刷新…' : distance > 0 ? (ready ? '松开立即刷新' : '下拉刷新') : '' }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { readyDistanceFor } from '../composables/usePullRefresh';

// 下拉刷新指示器:高度由 usePullRefresh 的 distance 驱动,随手指下拉把内容「拉下来」,
// 松手后刷新中保持一段高度、加载结束再收回。
const props = defineProps<{
  distance: number;
  refreshing: boolean;
  pulling: boolean;
  /** 与 usePullRefresh 的 threshold 保持一致;不传即默认阈值(目前所有调用点都用默认值) */
  threshold?: number;
}>();

// 阈值与手势判定共用一处计算:提示「松开立即刷新」的距离就是真正会刷新的距离。
// 必须跟随 usePullRefresh 的 threshold —— 否则调用方一旦自定义阈值,就会出现
// 「提示了松开却不刷新」。
const readyDistance = readyDistanceFor(props.threshold);

// 与 usePullRefresh 同口径按「本次手势的最大距离」出文案:一旦过了阈值就一直提示松开,
// 不会因为松手前手指轻微回抬把提示收回去(那样松手就成了「提示过却不刷新」)
const peak = ref(0);
watch(
  () => props.distance,
  d => {
    if (d <= 0) peak.value = 0;
    else if (d > peak.value) peak.value = d;
  }
);
const ready = computed(() => peak.value >= readyDistance);
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
