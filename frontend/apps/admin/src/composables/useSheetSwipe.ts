import { computed, onBeforeUnmount, ref, watch } from 'vue';
import type { CSSProperties, Ref } from 'vue';

/** 下拉超过该距离即关闭弹层(px) */
export const SHEET_DISMISS_THRESHOLD = 90;
/** 滑出/回弹动画时长(ms),需与组件里的 transition 时长一致 */
const SETTLE_MS = 220;
/** 只有从弹层顶部这个高度内开始的触摸才可拖拽(避开内容区滚动) */
const HANDLE_HEIGHT = 56;
/** 量不到面板高度时的兜底(px):足够把面板移出视口 */
const FALLBACK_PANEL_HEIGHT = 400;

/** 是否达到「松手关闭」的距离。纯函数,便于单测。 */
export function shouldDismissSheet(dragY: number, threshold = SHEET_DISMISS_THRESHOLD): boolean {
  return dragY > threshold;
}

export interface SheetSwipe {
  /** 面板下行位移(px) */
  dragY: Ref<number>;
  /** 是否正在拖拽 */
  dragging: Ref<boolean>;
  /** 绑到面板上的样式:位移 + 松手过渡 */
  panelStyle: Ref<CSSProperties>;
}

/**
 * 底部弹层的「下滑关闭」手势。
 *
 * - 仅当触摸起点落在弹层顶部 HANDLE_HEIGHT 内时才接管(内容区滚动不受影响);
 * - 跟手平移面板,松手超过阈值滑出并关闭,否则回弹;
 * - 通过内联 transform/transition 驱动,不与 Vue 的进入/离开过渡冲突。
 */
export function useSheetSwipe(
  panelRef: Ref<HTMLElement | null>,
  isOpen: Ref<boolean>,
  onClose: () => void
): SheetSwipe {
  const dragY = ref(0);
  const dragging = ref(false);

  let startY = 0;
  let closing = false;
  /** 滑出后触发 onClose 的定时器:组件在 220ms 内被销毁要清掉,避免打到已卸载组件。
   *  显式写 number:本仓 @types/node 在场,`setTimeout` 的 ReturnType 会被推成 Timeout。 */
  let closeTimer: number | null = null;

  const panelStyle = computed<CSSProperties>(() => {
    const style: CSSProperties = {};
    if (dragY.value) style.transform = `translateY(${dragY.value}px)`;
    // 非拖拽态给一个过渡,用于松手后的滑出/回弹
    if (!dragging.value) style.transition = `transform ${SETTLE_MS}ms ease`;
    return style;
  });

  function onStart(e: TouchEvent): void {
    if (closing || dragging.value) return;
    const el = panelRef.value;
    if (!el) return;
    const top = el.getBoundingClientRect().top;
    if (e.touches[0].clientY - top > HANDLE_HEIGHT) return;
    startY = e.touches[0].clientY;
    dragging.value = true;
    dragY.value = 0;
  }

  function onMove(e: TouchEvent): void {
    if (!dragging.value) return;
    dragY.value = Math.max(0, e.touches[0].clientY - startY);
    // 接管期间阻止原生滚动:否则从把手向下拖时内容区会同时滚动,
    // 手势被原生滚动抢走一半(与 usePullRefresh 的做法一致)。
    if (e.cancelable) e.preventDefault();
  }

  function onEnd(): void {
    if (!dragging.value) return;
    dragging.value = false;
    const height = panelRef.value?.getBoundingClientRect().height ?? FALLBACK_PANEL_HEIGHT;
    if (shouldDismissSheet(dragY.value)) {
      closing = true;
      dragY.value = height; // 滑出屏幕
      closeTimer = window.setTimeout(() => {
        closeTimer = null;
        closing = false;
        onClose();
      }, SETTLE_MS);
    } else {
      dragY.value = 0; // 回弹
    }
  }

  function unbind(el: HTMLElement | null): void {
    if (!el) return;
    el.removeEventListener('touchstart', onStart);
    el.removeEventListener('touchmove', onMove);
    el.removeEventListener('touchend', onEnd);
    el.removeEventListener('touchcancel', onEnd);
  }

  function bind(el: HTMLElement | null): void {
    if (!el) return;
    el.addEventListener('touchstart', onStart, { passive: true });
    // passive: false 才能在拖拽期间 preventDefault,阻止内容区跟着滚
    el.addEventListener('touchmove', onMove, { passive: false });
    el.addEventListener('touchend', onEnd);
    el.addEventListener('touchcancel', onEnd);
  }

  // 面板由 v-if 渲染:元素挂载/卸载时动态绑定监听
  watch(panelRef, (el, oldEl) => {
    unbind(oldEl ?? null);
    bind(el);
  });

  // 重新打开时复位,避免残留上一轮的位移;定时器两个分支都要清 ——
  // 否则「下滑 dismiss 后 220ms 内重新打开」会让旧定时器到点执行 onClose(),
  // 把刚重开的弹窗又关掉。
  watch(isOpen, open => {
    if (closeTimer) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
    if (open) {
      dragY.value = 0;
      closing = false;
    }
  });

  onBeforeUnmount(() => {
    if (closeTimer) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
    unbind(panelRef.value);
  });

  return { dragY, dragging, panelStyle };
}
