// 菜品「能不能点 / 怎么点」的判定。
//
// 单独成一个文件（而不是塞进 useCart）是为了让小程序端也能直接复用：
// useCart 依赖 Vue 运行时，原生小程序用不了；这几个判定是纯函数，谁都能 import。
// 两端共用一个口径，才不会出现「同一道菜 H5 能点、小程序不能点」。
import type { Dish } from '@dining/types';

/**
 * 行内最多放几个规格 chip。
 * 上限取 3：菜品行的文本列窄（扣掉左栏与图片后约 340rpx），
 * 3 个 chip 基本放得下；再多会换行把行高撑乱，不如统一进弹层。
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

/** 规格多到行内放不下 → 进规格弹层，不默默按默认规格下单。 */
export function isManySpecs(d: Dish): boolean {
  return (d.specs || []).length > INLINE_SPEC_MAX;
}
