import { onBeforeUnmount, ref } from 'vue';
import type { Ref } from 'vue';

// ============================================================================
// 列表拖拽排序(桌面端拖行 / 移动端拖卡片,同一套逻辑)
//
// 不用 HTML5 DnD:表格行没法逐行挂 draggable(组件内部渲染),触屏上 HTML5 DnD
// 也根本不触发。这里统一用 Pointer Events —— 鼠标、触摸、触控笔走同一条代码路径。
//
// 交互约定:
//   - 鼠标:在行/卡片上按下后移动超过阈值即起拖(点按钮不误触,阈值内松手仍是一次点击)
//   - 触摸:点把手立即起拖(把手上 touch-action:none,不会被系统滚动抢走);
//     不点把手则要长按,避免一碰卡片就把页面滚动吞掉
//   - 起拖后被拖元素跟手平移,落点用插入指示线标出,松手回调 onDrop
// ============================================================================

/** 判定落位所需的矩形投影(只取四个字段,便于单测直接构造)。 */
export interface SlotRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

/**
 * 计算插入槽位:返回 0..rects.length,表示「插入到第 slot 项之前」
 * (slot === rects.length 即插到末尾)。rects 必须是同一分组内、按展示顺序排列、
 * **且不含被拖项**的元素矩形。
 *
 * 单列(表格行、移动端竖排卡片)按中线逐项判断;多列网格(桌面卡片墙)先找
 * 中心最近的一项,再按纵向越界/同行左右决定插在它前面还是后面。
 */
export function slotFromPoint(rects: SlotRect[], x: number, y: number): number {
  if (!rects.length) return 0;
  const first = rects[0];
  const singleColumn = rects.every(r => Math.abs(r.left - first.left) < Math.max(r.width, 1) * 0.5);
  if (singleColumn) {
    for (let i = 0; i < rects.length; i++) {
      const r = rects[i];
      if (y < r.top + r.height / 2) return i;
    }
    return rects.length;
  }
  let best = 0;
  let bestDist = Number.POSITIVE_INFINITY;
  for (let i = 0; i < rects.length; i++) {
    const r = rects[i];
    const dx = x - (r.left + r.width / 2);
    const dy = y - (r.top + r.height / 2);
    const dist = dx * dx + dy * dy;
    if (dist < bestDist) {
      bestDist = dist;
      best = i;
    }
  }
  const r = rects[best];
  const cy = r.top + r.height / 2;
  // 纵向明显落在该行上/下 → 以纵向为准(换行到上一行/下一行)
  if (y < cy - r.height * 0.3) return best;
  if (y > cy + r.height * 0.3) return best + 1;
  // 同一行内:左半插到它前面,右半插到它后面
  return x < r.left + r.width / 2 ? best : best + 1;
}

export interface DragSortOptions {
  /** 当前渲染的可排序元素,顺序要与数据顺序一致 */
  items: () => HTMLElement[];
  /** 元素所属分组(如分类 ID):只有同组元素之间才允许调整顺序 */
  groupOf: (index: number) => string | number;
  /** 松手落位回调:先删 from,再把该项插到 to(移动端/桌面端同一语义) */
  onDrop: (from: number, to: number) => void;
  /** 是否允许拖拽(如无编辑权限时关闭) */
  enabled?: () => boolean;
  /** 拖动把手选择器:指针落在把手内立即起拖,无需长按 */
  handleSelector?: string;
  /** 触摸端长按起拖的等待时间(毫秒) */
  longPressDelay?: number;
}

/** 起拖判定的最小位移(px):小于它视为点击,不拦截原生的点击/滚动。 */
const MOVE_THRESHOLD = 4;
/** 起拖期间挂在 body 上的类名:统一光标并禁止选中文本。 */
const BODY_CLASS = 'drag-sorting';
/** 自动滚动触发边距(px)与每帧滚动量(px)。 */
const SCROLL_EDGE = 64;
const SCROLL_STEP = 14;

interface Session {
  pointerId: number;
  pointerType: string;
  startX: number;
  startY: number;
  lastX: number;
  lastY: number;
  index: number;
  el: HTMLElement;
  /** 按下点相对元素顶部的偏移,用于跟手平移 */
  grabOffset: number;
  /** 已经施加的位移,用来从 getBoundingClientRect 里扣掉自身位移 */
  appliedY: number;
  timer: ReturnType<typeof setTimeout> | null;
  active: boolean;
  /** 起拖时算好的滚动容器,自动滚动每帧都要用,不必重复向上遍历 DOM */
  scroller: HTMLElement | null;
}

export function useDragSort(opts: DragSortOptions) {
  const dragging = ref(false);
  /** 被拖项在当前数据里的下标,-1 表示没有拖拽 */
  const fromIndex = ref(-1);
  /** 插入指示线:画在该下标元素的上方 */
  const dropBefore = ref(-1);
  /** 插入指示线:画在该下标元素的下方(落点是分组末尾时用) */
  const dropAfter = ref(-1);

  let session: Session | null = null;
  /** 当前落位(移除 from 之后的插入下标),-1 表示还没算出来 */
  let slot = -1;
  let rafId = 0;
  /** 拖拽结束后的那次 click 要吞掉(否则移动端松手就跳编辑弹窗) */
  let suppressClick = false;
  let suppressTimer: ReturnType<typeof setTimeout> | null = null;

  function list(): HTMLElement[] {
    try {
      return opts.items();
    } catch {
      return [];
    }
  }

  function allowed(): boolean {
    return opts.enabled ? opts.enabled() : true;
  }

  /** 从事件目标向上找可排序元素(点击落在行内按钮上时不会被当成拖拽起点)。 */
  function itemAt(target: EventTarget | null): { el: HTMLElement; index: number } | null {
    const els = list();
    if (!els.length) return null;
    let node = target as HTMLElement | null;
    while (node) {
      const i = els.indexOf(node);
      if (i >= 0) return { el: node, index: i };
      node = node.parentElement;
    }
    return null;
  }

  function scrollParent(el: HTMLElement): HTMLElement | null {
    let node = el.parentElement;
    while (node && node !== document.body) {
      const overflowY = getComputedStyle(node).overflowY;
      if (/(auto|scroll)/.test(overflowY) && node.scrollHeight > node.clientHeight) return node;
      node = node.parentElement;
    }
    const doc = (document.scrollingElement as HTMLElement | null) ?? null;
    return doc && doc.scrollHeight > doc.clientHeight ? doc : null;
  }

  function beginDrag(): void {
    if (!session || session.active) return;
    const el = session.el;
    const rect = el.getBoundingClientRect();
    session.active = true;
    session.grabOffset = session.startY - rect.top;
    session.appliedY = 0;
    session.scroller = scrollParent(el);
    dragging.value = true;
    fromIndex.value = session.index;
    el.style.transition = 'none';
    el.style.position = 'relative';
    el.style.zIndex = '30';
    el.style.pointerEvents = 'none';
    el.style.willChange = 'transform';
    document.body.classList.add(BODY_CLASS);
    tick();
  }

  function resetElement(el: HTMLElement): void {
    el.style.transition = '';
    el.style.position = '';
    el.style.zIndex = '';
    el.style.pointerEvents = '';
    el.style.willChange = '';
    el.style.transform = '';
  }

  /** 跟手平移 + 重算落位。 */
  function updatePointer(x: number, y: number): void {
    if (!session || !session.active) return;
    const el = session.el;
    // 元素自身已被 transform 过,rect.top 含位移:扣掉已施加量得到「自然位置」,
    // 这样容器滚动后也能自愈,不会越拖越偏。
    const naturalTop = el.getBoundingClientRect().top - session.appliedY;
    const dy = y - session.grabOffset - naturalTop;
    session.appliedY = dy;
    el.style.transform = `translateY(${Math.round(dy)}px)`;

    const els = list();
    const from = session.index;
    if (from < 0 || from >= els.length) return;
    const group = String(opts.groupOf(from));
    const inGroup: number[] = [];
    for (let i = 0; i < els.length; i++) {
      if (i !== from && String(opts.groupOf(i)) === group) inGroup.push(i);
    }
    if (!inGroup.length) {
      slot = 0;
      dropBefore.value = -1;
      dropAfter.value = -1;
      return;
    }
    const rects = inGroup.map(i => els[i].getBoundingClientRect());
    const next = slotFromPoint(rects, x, y);
    slot = next;
    if (next < inGroup.length) {
      dropBefore.value = inGroup[next];
      dropAfter.value = -1;
    } else {
      dropBefore.value = -1;
      dropAfter.value = inGroup[inGroup.length - 1];
    }
  }

  /** 拖到视口/滚动容器边缘时自动滚动,否则长列表里拖不到屏幕外的位置。 */
  function tick(): void {
    rafId = 0;
    if (!session || !session.active) return;
    const sp = session.scroller;
    if (sp) {
      const isDoc = sp === document.scrollingElement;
      const top = isDoc ? 0 : sp.getBoundingClientRect().top;
      const bottom = isDoc ? window.innerHeight : sp.getBoundingClientRect().bottom;
      const y = session.lastY;
      let delta = 0;
      if (y < top + SCROLL_EDGE) delta = -Math.min(SCROLL_STEP, Math.ceil((top + SCROLL_EDGE - y) / 4));
      else if (y > bottom - SCROLL_EDGE) delta = Math.min(SCROLL_STEP, Math.ceil((y - (bottom - SCROLL_EDGE)) / 4));
      if (delta) {
        sp.scrollTop += delta;
        updatePointer(session.lastX, session.lastY);
      }
    }
    rafId = requestAnimationFrame(tick);
  }

  function clearState(): void {
    const el = session?.el;
    if (el) resetElement(el);
    if (session?.timer) clearTimeout(session.timer);
    session = null;
    slot = -1;
    dragging.value = false;
    fromIndex.value = -1;
    dropBefore.value = -1;
    dropAfter.value = -1;
    document.body.classList.remove(BODY_CLASS);
    if (rafId) {
      cancelAnimationFrame(rafId);
      rafId = 0;
    }
  }

  function stopListeners(): void {
    window.removeEventListener('pointermove', onPointerMove);
    window.removeEventListener('pointerup', onPointerUp);
    window.removeEventListener('pointercancel', onPointerCancel);
    window.removeEventListener('keydown', onKeyDown);
    window.removeEventListener('touchmove', onTouchMove);
    window.removeEventListener('blur', onWindowBlur);
  }

  function finish(commit: boolean): void {
    const active = !!session?.active;
    const from = session?.index ?? -1;
    const to = slot;
    if (session?.el) {
      try {
        session.el.releasePointerCapture?.(session.pointerId);
      } catch {
        /* 未捕获/已释放时释放会抛,忽略即可 */
      }
    }
    stopListeners();
    clearState();
    if (!active) return;
    // 拖完浏览器会补发一次 click(移动端卡片整张可点=编辑),吞掉它
    suppressClick = true;
    if (suppressTimer) clearTimeout(suppressTimer);
    suppressTimer = setTimeout(() => {
      suppressClick = false;
    }, 300);
    if (commit && from >= 0 && to >= 0 && to !== from) opts.onDrop(from, to);
  }

  function onTouchMove(e: TouchEvent): void {
    if (dragging.value) e.preventDefault();
  }

  function onPointerMove(e: PointerEvent): void {
    if (!session || e.pointerId !== session.pointerId) return;
    session.lastX = e.clientX;
    session.lastY = e.clientY;
    if (!session.active) {
      if (Math.hypot(e.clientX - session.startX, e.clientY - session.startY) < MOVE_THRESHOLD) return;
      // 长按计时还没到就移开 = 用户在滚动页面,不抢手势
      if (session.timer) {
        clearTimeout(session.timer);
        session.timer = null;
        finish(false);
        return;
      }
      beginDrag();
    }
    if (!session) return;
    if (e.cancelable) e.preventDefault();
    updatePointer(e.clientX, e.clientY);
  }

  function onPointerUp(e: PointerEvent): void {
    if (!session || e.pointerId !== session.pointerId) return;
    finish(true);
  }

  function onPointerCancel(e: PointerEvent): void {
    if (!session || e.pointerId !== session.pointerId) return;
    finish(false);
  }

  function onKeyDown(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return;
    finish(false);
  }

  /** 切走窗口时指针可能再也收不到 up(比如鼠标在浏览器外松开),主动收尾。 */
  function onWindowBlur(): void {
    finish(false);
  }

  function onClickCapture(e: MouseEvent): void {
    if (!suppressClick) return;
    suppressClick = false;
    e.preventDefault();
    e.stopPropagation();
  }

  /** 绑定到列表容器(事件委托):行/卡片上的 pointerdown 都走这里。 */
  function onPointerDown(e: PointerEvent): void {
    if (session || !allowed()) return;
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    const target = e.target as HTMLElement | null;
    // 行内交互控件(按钮/开关/输入框)不参与拖拽,否则「点删除」会被拖拽拦掉
    if (target?.closest?.('button, a, input, textarea, select, [data-drag-ignore]')) return;
    const hit = itemAt(e.target);
    if (!hit) return;
    const inHandle = !!opts.handleSelector && !!target?.closest?.(opts.handleSelector);
    session = {
      pointerId: e.pointerId,
      pointerType: e.pointerType,
      startX: e.clientX,
      startY: e.clientY,
      lastX: e.clientX,
      lastY: e.clientY,
      index: hit.index,
      el: hit.el,
      grabOffset: 0,
      appliedY: 0,
      timer: null,
      active: false,
      scroller: null
    };
    // 指针捕获:鼠标在窗口外松开时也能收到 up,不至于一直卡在拖拽态
    try {
      hit.el.setPointerCapture?.(e.pointerId);
    } catch {
      /* 个别环境不支持(如 jsdom),窗口级监听已能覆盖绝大多数情况 */
    }
    window.addEventListener('pointermove', onPointerMove, { passive: false });
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerCancel);
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('touchmove', onTouchMove, { passive: false });
    window.addEventListener('blur', onWindowBlur);
    if (e.pointerType !== 'mouse' && !inHandle) {
      // 触摸端:长按才起拖,避免手指一碰卡片就吞掉页面滚动
      session.timer = setTimeout(() => {
        if (!session) return;
        session.timer = null;
        beginDrag();
        if (session) updatePointer(session.lastX, session.lastY);
      }, opts.longPressDelay ?? 320);
    }
  }

  window.addEventListener('click', onClickCapture, true);

  onBeforeUnmount(() => {
    stopListeners();
    clearState();
    window.removeEventListener('click', onClickCapture, true);
    if (suppressTimer) clearTimeout(suppressTimer);
  });

  return {
    dragging: dragging as Ref<boolean>,
    fromIndex: fromIndex as Ref<number>,
    dropBefore: dropBefore as Ref<number>,
    dropAfter: dropAfter as Ref<number>,
    onPointerDown
  };
}
