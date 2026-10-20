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
/** 默认阈值;PullRefresh 组件的文案也按这个默认值算「松开立即刷新」的距离 */
export const DEFAULT_PULL_THRESHOLD = 66;
/** 刷新态至少停留的时长(ms):接口很快时也要让用户看见「正在刷新」 */
const MIN_REFRESH_MS = 400;

/** 手指下拉位移(px)→ 可视下拉距离(px),带阻尼与上限。纯函数,便于单测。 */
export function pullDistanceFor(dy: number, maxDistance: number, damping = PULL_DAMPING): number {
  if (!(dy > 0)) return 0;
  return Math.min(maxDistance, dy * damping);
}

/** 松手时是否达到触发刷新的距离。纯函数,便于单测。 */
export function shouldTriggerRefresh(
  distance: number,
  threshold: number = DEFAULT_PULL_THRESHOLD,
  ratio = TRIGGER_RATIO
): boolean {
  return distance >= threshold * ratio;
}

/**
 * 「松开立即刷新」的提示距离 = 真正触发刷新的距离。
 *
 * 指示器文案和手势判定必须共用这一份计算:此前文案写死 46、判定按 66*0.7=46.2,
 * 停在两者之间松手就会出现「已经提示松手、松手却不刷新」。
 */
export function readyDistanceFor(threshold: number = DEFAULT_PULL_THRESHOLD): number {
  return threshold * TRIGGER_RATIO;
}

interface PullRefresh {
  /** 当前下拉距离(px),绑定到指示器高度即可实现「内容被拉下」的效果 */
  distance: Ref<number>;
  /** 是否正在刷新 */
  refreshing: Ref<boolean>;
  /** 是否处于手指下拉过程中(用于关掉 height 过渡,避免跟手时卡顿) */
  pulling: Ref<boolean>;
}

const sleep = (ms: number): Promise<void> => new Promise(resolve => window.setTimeout(resolve, ms));

/**
 * 移动端下拉刷新。
 *
 * 监听后台滚动容器(.content)的触摸事件:当容器已在顶部、且用户向下拖动时,
 * 按阻尼把位移映射成 distance,由调用方的指示器渲染出「下拉/松开/刷新中」三态。
 *
 * 几个关键点(都是「拉下来了、松手却没反应」的成因):
 * - touchmove 必须非 passive:接管下拉后 preventDefault,否则原生滚动/回弹会和指示器
 *   抢手势(安卓 WebView 还会触发浏览器自带的下拉刷新,看起来像整页被重置);
 * - 手势中途也要能接管:列表滚到顶再继续往下拉是同一个手势,不能在 touchstart
 *   时「不在顶部」就整段放弃;
 * - 记录本次手势的最大下拉距离:提示已经出现就一定能触发,不受松手前手指轻微回抬影响;
 * - touchcancel 只收起指示器、不刷新(触摸被浏览器/系统接管时不算用户松手)。
 */
export function usePullRefresh(
  onRefresh: () => Promise<unknown> | unknown,
  options: PullRefreshOptions = {}
): PullRefresh {
  const threshold = options.threshold ?? DEFAULT_PULL_THRESHOLD;
  const maxDistance = options.maxDistance ?? 92;

  const distance = ref(0);
  const refreshing = ref(false);
  const pulling = ref(false);

  /** 本次手势是否已经接管为「下拉刷新」 */
  let armed = false;
  let startY = 0;
  let lastY = 0;
  /** 本次手势的最大下拉距离:松手判定用它,而不是松手瞬间的距离 */
  let peak = 0;
  let scroller: HTMLElement | null = null;
  let boundEl: HTMLElement | null = null;

  const resolveScroller = (): HTMLElement | null =>
    options.getScroller?.() ?? (document.querySelector('.content') as HTMLElement | null);

  function reset(): void {
    armed = false;
    peak = 0;
    pulling.value = false;
    distance.value = 0;
  }

  function onStart(e: TouchEvent): void {
    if (refreshing.value) return;
    const el = resolveScroller();
    if (el && el !== boundEl) {
      unbind();
      boundEl = el;
      bind();
    }
    scroller = el;
    if (!scroller) return;
    startY = e.touches[0].clientY;
    lastY = startY;
    peak = 0;
    // 在顶部就直接接管;不在顶部先挂着,等手势把列表滚回顶部那一刻再接管(onMove)
    armed = scroller.scrollTop <= 0;
    pulling.value = armed;
    if (armed) distance.value = 0;
  }

  function onMove(e: TouchEvent): void {
    if (refreshing.value || !scroller) return;
    const y = e.touches[0].clientY;
    const step = y - lastY;
    lastY = y;

    if (!armed) {
      // 手势前半段在滚列表,滚到顶部后继续下拉:从这一步起接管,已滚过的位移不计入
      if (step > 0 && scroller.scrollTop <= 0) {
        armed = true;
        pulling.value = true;
        startY = y - step;
        distance.value = 0;
      } else {
        return;
      }
    }

    // 接管后被别的滚动(惯性、嵌套滚动)带离顶部:放弃本次下拉
    if (scroller.scrollTop > 0) {
      reset();
      return;
    }

    const next = pullDistanceFor(y - startY, maxDistance);
    distance.value = next;
    if (next > peak) peak = next;
    // 接管期间阻止原生滚动/回弹,否则内容会被「拉两次」、松手判定也不稳
    if (next > 0 && e.cancelable) e.preventDefault();
  }

  async function onEnd(cancel = false): Promise<void> {
    if (!armed) return;
    const reached = !cancel && shouldTriggerRefresh(peak, threshold);
    armed = false;
    pulling.value = false;
    if (!reached) {
      reset();
      return;
    }
    refreshing.value = true;
    distance.value = readyDistanceFor(threshold);
    const startedAt = Date.now();
    try {
      await onRefresh();
    } catch {
      /* 刷新失败由请求层统一提示,这里只负责收尾,避免未处理的拒绝 */
    } finally {
      // 数据没变化时也要给一段可见的「正在刷新」,否则下拉像没生效
      const rest = MIN_REFRESH_MS - (Date.now() - startedAt);
      if (rest > 0) await sleep(rest);
      refreshing.value = false;
      reset();
    }
  }

  const onEndHandler = (): void => void onEnd(false);
  const onCancelHandler = (): void => void onEnd(true);

  function bind(): void {
    const el = boundEl;
    if (!el) return;
    el.addEventListener('touchstart', onStart, { passive: true });
    el.addEventListener('touchmove', onMove, { passive: false });
    el.addEventListener('touchend', onEndHandler);
    el.addEventListener('touchcancel', onCancelHandler);
  }

  function unbind(): void {
    const el = boundEl;
    if (!el) return;
    el.removeEventListener('touchstart', onStart);
    el.removeEventListener('touchmove', onMove);
    el.removeEventListener('touchend', onEndHandler);
    el.removeEventListener('touchcancel', onCancelHandler);
  }

  /** 布局容器可能晚于页面渲染(懒加载/首帧),首次触摸时补绑一次 */
  function ensureBound(): void {
    const el = resolveScroller();
    if (!el || el === boundEl) return;
    unbind();
    boundEl = el;
    bind();
  }

  onMounted(() => {
    ensureBound();
    document.addEventListener('touchstart', ensureBound, { passive: true, capture: true });
  });

  onBeforeUnmount(() => {
    document.removeEventListener('touchstart', ensureBound, { capture: true });
    unbind();
    boundEl = null;
  });

  return { distance, refreshing, pulling };
}
