// 购物车。
//
// 与 Web 端 `@dining/customer-core` 的 useCart 是「同一套规则的另一实现」：
// 那边基于 Vue 的 ref/computed，这里基于「globalData 里的普通数组 + 页面手动 setData」。
// 之所以没有直接复用：原生小程序没有 Vue 运行时，computed 无法工作
// （详见 docs/miniprogram.md 5.1 节）。
// 两端**计价规则**保持一致，但最终金额仍以后端重算为准。
import type { GlobalData, CartLine } from '../types';
import type { OrderCreateItem, PublicConfig } from '@dining/types';

/** 购物车数组本体（跨页共享，见 src/types.ts 顶部注释）。 */
export function cartState(): CartLine[] {
  return getApp<GlobalData>().cart;
}

/** 两位小数取整，避免浮点误差累积（与 H5 的 round2 一致）。 */
export function round2(n: number): number {
  return Math.round((n + Number.EPSILON) * 100) / 100;
}

export function cartCount(): number {
  return cartState().reduce((sum, l) => sum + l.quantity, 0);
}

export function cartDishTotal(): number {
  return round2(cartState().reduce((sum, l) => sum + l.price * l.quantity, 0));
}

/** 某道菜在购物车里的份数（菜单页角标用）。 */
export function cartDishQty(dishId: number): number {
  return cartState()
    .filter(l => l.dishId === dishId)
    .reduce((sum, l) => sum + l.quantity, 0);
}

/** 加一份；同一 dish+spec 合并成一行。 */
export function cartAdd(line: Omit<CartLine, 'key' | 'quantity' | 'itemRemark'> & { itemRemark?: string }): void {
  const key = `${line.dishId}:${line.specId}`;
  const cart = cartState();
  const exist = cart.find(l => l.key === key);
  if (exist) {
    exist.quantity += 1;
    if (line.itemRemark !== undefined) exist.itemRemark = line.itemRemark;
    return;
  }
  cart.push({ ...line, key, quantity: 1, itemRemark: line.itemRemark ?? '' });
}

/** 增减份数；减到 0 自动移除该行。 */
export function cartChangeQty(key: string, delta: number): void {
  const cart = cartState();
  const idx = cart.findIndex(l => l.key === key);
  if (idx < 0) return;
  const next = cart[idx].quantity + delta;
  if (next <= 0) cart.splice(idx, 1);
  else cart[idx].quantity = next;
}

export function cartSetRemark(key: string, itemRemark: string): void {
  const line = cartState().find(l => l.key === key);
  if (line) line.itemRemark = itemRemark;
}

export function cartRemove(key: string): void {
  const cart = cartState();
  const idx = cart.findIndex(l => l.key === key);
  if (idx >= 0) cart.splice(idx, 1);
}

export function cartClear(): void {
  cartState().length = 0;
}

/** 转成下单接口的 items 结构。 */
export function cartToItems(): OrderCreateItem[] {
  return cartState().map(l => ({
    dishId: l.dishId,
    specId: l.specId,
    quantity: l.quantity,
    itemRemark: l.itemRemark
  }));
}

export interface CartSettle {
  dishTotal: number;
  seatFee: number;
  discount: number;
  total: number;
}

/**
 * 结算明细（仅用于展示）。
 * ⚠️ 最终金额以后端按数据库价格重算的结果为准，这里不参与下单金额。
 */
export function cartSettle(config: PublicConfig | null, personCount: number): CartSettle {
  const dishTotal = cartDishTotal();
  const cfg = config ?? {};
  const seatFeeEnabled = cfg.seat_fee_enabled === '1' && Number(cfg.seat_fee) > 0;
  const seatFeePer = Number(cfg.seat_fee) || 0;
  const seatFee = seatFeeEnabled ? round2(Math.max(1, personCount) * seatFeePer) : 0;

  const promoOn = cfg.promotion_enabled === '1';
  const threshold = Number(cfg.promotion_threshold);
  const minus = Number(cfg.promotion_discount);
  const discount =
    promoOn && threshold > 0 && minus > 0 && dishTotal >= threshold
      ? round2(Math.min(Math.floor(dishTotal / threshold) * minus, dishTotal))
      : 0;

  // seatFee 与 H5 一样「按人数照算」；视图层只在购物车非空时才渲染金额明细，
  // 所以空车不会看到它。但合计在未选菜时固定为 0，避免「没点菜却有金额」的观感。
  const total = dishTotal > 0 ? round2(dishTotal + seatFee - discount) : 0;
  return { dishTotal, seatFee, discount, total };
}
