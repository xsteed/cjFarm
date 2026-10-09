import { computed, onBeforeUnmount, ref, watch } from 'vue';
import type { CSSProperties, Ref } from 'vue';

/** 下拉超过该距离即关闭弹层(px) */
export const SHEET_DISMISS_THRESHOLD = 90;
/** 滑出/回弹动画时长(ms),需与组件里的 transition 时长一致 */
const SETTLE_MS = 220;
/** 只有从弹层顶部这个高度内开始的触摸才可拖拽(避开内容区滚动) */
const HANDLE_HEIGHT = 56;

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
  }

  function onEnd(): void {
    if (!dragging.value) return;
    dragging.value = false;
    const height = panelRef.value?.getBoundingClientRect().height ?? 400;
    if (shouldDismissSheet(dragY.value)) {
      closing = true;
      dragY.value = height; // 滑出屏幕
      window.setTimeout(() => {
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
    el.addEventListener('touchmove', onMove, { passive: true });
    el.addEventListener('touchend', onEnd);
    el.addEventListener('touchcancel', onEnd);
  }

  // 面板由 v-if 渲染:元素挂载/卸载时动态绑定监听
  watch(panelRef, (el, oldEl) => {
    unbind(oldEl ?? null);
    bind(el);
  });

  // 重新打开时复位,避免残留上一轮的位移
  watch(isOpen, open => {
    if (open) {
      dragY.value = 0;
      closing = false;
    }
  });

  onBeforeUnmount(() => unbind(panelRef.value));

  return { dragY, dragging, panelStyle };
}
