// 菜品「能不能点 / 怎么点」的判定(与 H5 共用一个口径)。
//
// 纯函数,不依赖 Vue 运行时,小程序端可直接 import。
import type { Dish } from '../types';

/**
 * 行内最多放几个规格 chip。
 * 上限取 3:菜品行的文本列窄(扣掉左栏与图片后约 340rpx),
 * 3 个 chip 基本放得下;再多会换行把行高撑乱,不如统一进弹层。
 */
export const INLINE_SPEC_MAX = 3;

/** 售罄 = 人工沽清，或所有规格库存都卖完。 */
export function isSoldOut(d: Dish): boolean {
  const specs = d.specs || [];
  return Number(d.soldOut) === 1 || (specs.length > 0 && specs.every(s => Number(s.stock) === 0));
}

/** 可点 = 至少有一个规格（无规格是历史脏数据，后端也下不了单）。 */
export function isOrderable(d: Dish): boolean {
  return (d.specs || []).length > 0;
}

/**
 * 规格里的最小「有效」价格（> 0）。全部缺失/为零时返回 0。
 *
 * 直接 Math.min(...prices) 会被 0/缺失价污染:招牌菜与「起」价会显示 ¥0.00,
 * 点进去价格却完全不同。首页与菜单页共用这一份。
 */
export function minPositivePrice(specs: Array<{ price?: number | null }>): number {
  const prices = specs.map(s => Number(s.price) || 0).filter(p => p > 0);
  return prices.length ? Math.min(...prices) : 0;
}
