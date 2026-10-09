/**
 * 移动端 t-dialog 弹层的「横杠」手势。
 *
 * style.css 的移动端断点里,所有 t-dialog 被改造成贴底弹层,
 * header 顶部有一条 ::before 画的抓取横杠——但它只是装饰,
 * TDesign Dialog 本身不提供下拉/点击关闭手势。
 *
 * 这里用 document 级事件委托补上,覆盖所有弹窗(包括
 * DialogPlugin.confirm 这类命令式弹窗),无需逐个改造页面:
 *   - 点击 header 顶部横杠区域 → 关闭;
 *   - 从顶部 56px 内按下并下拉超过阈值 → 跟手平移,松手滑出并关闭,否则回弹。
 *
 * 关闭动作通过模拟 Escape keydown 实现:TDesign Dialog 在 document 上
 * 监听 keydown,顶层弹窗收到 Escape 且未禁用 closeOnEscKeydown 时
 * 会走标准的 emitCloseEvent(update:visible: false),与点关闭按钮一致。
 * 与 BottomSheet 的 useSheetSwipe 保持同一组手感参数。
 */

/** 与 style.css 的移动端弹窗断点保持一致 */
const MOBILE_QUERY = '(width <= 767px)';
/** 只有从弹层顶部这个高度内开始的触摸才可拖拽(避开内容区滚动) */
const HANDLE_HEIGHT = 56;
/** 下拉超过该距离即关闭弹层(px) */
const DISMISS_THRESHOLD = 90;
/** 点击关闭的横杠热区高度(px),覆盖 ::before 横杠及其上方留白 */
const TAP_STRIP_HEIGHT = 28;
/** 松手后的滑出/回弹动画时长(ms) */
const SETTLE_MS = 220;

interface DialogDrag {
  panel: HTMLElement;
  startY: number;
  dy: number;
}

let installed = false;
let drag: DialogDrag | null = null;

function isMobile(): boolean {
  return window.matchMedia(MOBILE_QUERY).matches;
}

function findPanel(target: EventTarget | null): HTMLElement | null {
  if (!(target instanceof Element)) return null;
  return target.closest('.t-dialog');
}

function closeTopDialog(): void {
  document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Escape', key: 'Escape' }));
}

function onTouchStart(e: TouchEvent): void {
  if (!isMobile() || drag) return;
  const panel = findPanel(e.target);
  if (!panel) return;
  const startY = e.touches[0].clientY;
  if (startY - panel.getBoundingClientRect().top > HANDLE_HEIGHT) return;
  drag = { panel, startY, dy: 0 };
  // 仅拖拽期间挂 move/end,避免常驻非 passive 监听拖慢整页滚动
  document.addEventListener('touchmove', onTouchMove, { passive: false });
  document.addEventListener('touchend', onTouchEnd);
  document.addEventListener('touchcancel', onTouchEnd);
}

function onTouchMove(e: TouchEvent): void {
  if (!drag) return;
  const dy = Math.max(0, e.touches[0].clientY - drag.startY);
  drag.dy = dy;
  if (dy > 0) {
    e.preventDefault();
    drag.panel.style.transition = 'none';
    drag.panel.style.transform = `translateY(${dy}px)`;
  } else {
    drag.panel.style.transform = '';
  }
}

function onTouchEnd(): void {
  if (!drag) return;
  const { panel, dy } = drag;
  drag = null;
  document.removeEventListener('touchmove', onTouchMove);
  document.removeEventListener('touchend', onTouchEnd);
  document.removeEventListener('touchcancel', onTouchEnd);

  if (dy > DISMISS_THRESHOLD) {
    // 立即触发关闭:离场动画(tDialogSlideDown)的起点取当前 transform,
    // 会从拖拽到的位置继续平滑滑出,遮罩同步淡出;内联样式延后清理
    panel.style.transition = `transform ${SETTLE_MS}ms ease`;
    window.setTimeout(() => {
      panel.style.transition = '';
      panel.style.transform = '';
    }, SETTLE_MS + 300);
    closeTopDialog();
  } else {
    panel.style.transition = `transform ${SETTLE_MS}ms ease`;
    panel.style.transform = '';
    window.setTimeout(() => {
      panel.style.transition = '';
    }, SETTLE_MS + 50);
  }
}

function onClick(e: MouseEvent): void {
  if (!isMobile()) return;
  const panel = findPanel(e.target);
  if (!panel) return;
  // 右上角关闭按钮有自己的点击逻辑,别抢
  if (e.target instanceof Element && e.target.closest('.t-dialog__close')) return;
  if (e.clientY - panel.getBoundingClientRect().top <= TAP_STRIP_HEIGHT) closeTopDialog();
}

/** 在应用入口调用一次,全局生效。重复调用是空操作。 */
export function setupDialogSheetGesture(): void {
  if (installed) return;
  installed = true;
  document.addEventListener('touchstart', onTouchStart, { passive: true });
  document.addEventListener('click', onClick);
}
