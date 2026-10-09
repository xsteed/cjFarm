import { onBeforeUnmount, onMounted, ref } from 'vue';
import type { Ref } from 'vue';

interface PullRefreshOptions {
  /** 触发刷新的下拉距离阈值(px) */
  threshold?: number;
  /** 最大可视下拉距离(px) */
  maxDistance?: number;
  /** 自定义滚动容器;默认取后台布局里的 .content */
  getScroller?: () => HTMLElement | null;
}

/** 阻尼系数:手指位移映射到可视下拉距离的比例 */
const PULL_DAMPING = 0.45;
/** 触发刷新的松手判定比例(距离达到阈值的一定比例即可) */
const TRIGGER_RATIO = 0.7;

/** 手指下拉位移(px)→ 可视下拉距离(px),带阻尼与上限。纯函数,便于单测。 */
export function pullDistanceFor(dy: number, maxDistance: number, damping = PULL_DAMPING): number {
  if (!(dy > 0)) return 0;
  return Math.min(maxDistance, dy * damping);
}

/** 松手时是否达到触发刷新的距离。纯函数,便于单测。 */
export function shouldTriggerRefresh(distance: number, threshold: number, ratio = TRIGGER_RATIO): boolean {
  return distance >= threshold * ratio;
}

interface PullRefresh {
  /** 当前下拉距离(px),绑定到指示器高度即可实现「内容被拉下」的效果 */
  distance: Ref<number>;
  /** 是否正在刷新 */
  refreshing: Ref<boolean>;
  /** 是否处于手指下拉过程中(用于关掉 height 过渡,避免跟手时卡顿) */
  pulling: Ref<boolean>;
}

/**
 * 移动端下拉刷新。
 *
 * 监听后台滚动容器(.content)的触摸事件:当容器已在顶部、且用户向下拖动时,
 * 按阻尼把位移映射成 distance,由调用方的指示器渲染出「下拉/松开/刷新中」三态。
 *
 * 用 passive 监听,不阻止原生滚动/回弹 —— 后台页面本身可用,不做过度接管。
 */
export function usePullRefresh(
  onRefresh: () => Promise<unknown> | unknown,
  options: PullRefreshOptions = {}
): PullRefresh {
  const threshold = options.threshold ?? 66;
  const maxDistance = options.maxDistance ?? 92;

  const distance = ref(0);
  const refreshing = ref(false);
  const pulling = ref(false);

  let startY = 0;
  let scroller: HTMLElement | null = null;

  const resolveScroller = (): HTMLElement | null =>
    options.getScroller?.() ?? (document.querySelector('.content') as HTMLElement | null);

  function onStart(e: TouchEvent): void {
    if (refreshing.value) return;
    scroller = resolveScroller();
    if (!scroller || scroller.scrollTop > 0) {
      pulling.value = false;
      return;
    }
    startY = e.touches[0].clientY;
    pulling.value = true;
    distance.value = 0;
  }

  function onMove(e: TouchEvent): void {
    if (!pulling.value || refreshing.value) return;
    if (!scroller || scroller.scrollTop > 0) return;
    const dy = e.touches[0].clientY - startY;
    // 阻尼:越往下越钝,避免一拉到底
    distance.value = pullDistanceFor(dy, maxDistance);
  }

  async function onEnd(): Promise<void> {
    if (!pulling.value) return;
    pulling.value = false;
    if (!shouldTriggerRefresh(distance.value, threshold)) {
      distance.value = 0;
      return;
    }
    refreshing.value = true;
    distance.value = threshold * 0.6;
    try {
      await onRefresh();
    } catch {
      /* 刷新失败由请求层统一提示,这里只负责收尾,避免未处理的拒绝 */
    } finally {
      refreshing.value = false;
      distance.value = 0;
    }
  }

  onMounted(() => {
    scroller = resolveScroller();
    if (!scroller) return;
    scroller.addEventListener('touchstart', onStart, { passive: true });
    scroller.addEventListener('touchmove', onMove, { passive: true });
    scroller.addEventListener('touchend', onEnd);
    scroller.addEventListener('touchcancel', onEnd);
  });

  onBeforeUnmount(() => {
    if (!scroller) return;
    scroller.removeEventListener('touchstart', onStart);
    scroller.removeEventListener('touchmove', onMove);
    scroller.removeEventListener('touchend', onEnd);
    scroller.removeEventListener('touchcancel', onEnd);
  });

  return { distance, refreshing, pulling };
}
