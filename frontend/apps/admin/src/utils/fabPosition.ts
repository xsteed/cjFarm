/**
 * 可拖拽悬浮按钮(FAB)定位相关的纯函数。
 *
 * 抽成纯函数的目的有二:
 *   1. 组件里的边界/吸附计算只有一处实现,便于维护;
 *   2. 可以脱离 DOM 单测(见 __tests__/fabPosition.test.ts)。
 */

export const FAB_MARGIN = 12; // 距屏幕左右的最小间距
export const FAB_TOP_RESERVE = 60; // 顶部栏预留
export const FAB_BOTTOM_RESERVE = 62; // 底部 tabbar(含安全区)兜底预留

export interface FabLayout {
  width: number;
  height: number;
  viewportWidth: number;
  viewportHeight: number;
  margin?: number;
  topReserve?: number;
  bottomReserve?: number;
}

export interface FabPoint {
  x: number;
  y: number;
}

/** 把坐标夹到可视区内:左右留 margin,顶部避开顶栏,底部避开 tabbar/安全区。 */
export function clampFabPosition(point: FabPoint, layout: FabLayout): FabPoint {
  const margin = layout.margin ?? FAB_MARGIN;
  const top = layout.topReserve ?? FAB_TOP_RESERVE;
  const bottom = layout.bottomReserve ?? FAB_BOTTOM_RESERVE;
  const maxX = Math.max(margin, layout.viewportWidth - layout.width - margin);
  const maxY = Math.max(top, layout.viewportHeight - layout.height - bottom);
  return {
    x: Math.min(Math.max(point.x, margin), maxX),
    y: Math.min(Math.max(point.y, top), maxY)
  };
}

/** 吸附到某条边后的 x 坐标。 */
export function snappedFabX(layout: FabLayout, side: 'left' | 'right'): number {
  const margin = layout.margin ?? FAB_MARGIN;
  if (side === 'left') return margin;
  return Math.max(margin, layout.viewportWidth - layout.width - margin);
}

/** 当前横坐标更靠近哪条边(用于松手吸附)。 */
export function nearestFabSide(x: number, width: number, viewportWidth: number): 'left' | 'right' {
  return x + width / 2 < viewportWidth / 2 ? 'left' : 'right';
}
