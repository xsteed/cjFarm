// ============================================================================
// 「正在拖拽排序」的全局标记
//
// 拖拽和下拉刷新都在「列表已在顶部 + 手指往下走」时感兴趣:拖卡片往下放时,
// 页面本身绝不能被拉下来(否则内容跟着位移、松手还会触发一次刷新)。
// 两边不能互相 import(会绕成一团),所以用 body 上的类名当唯一事实来源:
//   - useDragSort 起拖/收尾时置位与清除(顺带给全局光标与禁选文本用);
//   - usePullRefresh 在动手指前后查一眼,拖拽期间整段手势让路。
// ============================================================================

/** 挂在 body 上的类名(样式见 style.css 的 body.drag-sorting) */
export const DRAG_ACTIVE_CLASS = 'drag-sorting';

/** 标记/取消「拖拽进行中」。 */
export function setDragActive(active: boolean): void {
  document.body.classList.toggle(DRAG_ACTIVE_CLASS, active);
}

/** 当前是否有拖拽在手(下拉刷新等手势据此让路)。 */
export function isDragActive(): boolean {
  return document.body.classList.contains(DRAG_ACTIVE_CLASS);
}
