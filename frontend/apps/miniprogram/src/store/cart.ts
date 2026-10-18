// 购物车。
//
// 与 Web 端顾客端（Vue useCart）是「同一套规则的另一实现」：
// 那边基于 Vue 的 ref/computed，这里基于「globalData 里的普通数组 + 页面手动 setData」。
// 之所以没有直接复用：原生小程序没有 Vue 运行时，computed 无法工作
// （详见 docs/miniprogram.md 5.1 节）。
// 两端**计价规则**保持一致，但最终金额仍以后端重算为准。
import type { GlobalData, CartLine } from '../types';
import type { OrderCreateItem, PublicConfig } from '../types';
import { round2 } from '../shared/money';
import { saveCart } from './cartPersist';

/** 购物车数组本体（跨页共享，见 src/types.ts 顶部注释）。 */
export function cartState(): CartLine[] {
  return getApp<GlobalData>().cart;
}

/**
 * 两位小数取整，避免浮点误差累积。
 *
 * 实现收在 shared/money.ts（唯一来源）：多处各留一份时曾经漂移（一边加 EPSILON、一边没加），
 * 同一个购物车在 H5 与小程序会显示出相差 1 分的合计。
 */
export { round2 };

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
export function cartAdd(
  line: Omit<CartLine, 'key' | 'quantity' | 'itemRemark'> & { itemRemark?: string },
  quantity = 1
): void {
  // 数量必须被收敛成正整数：弹层传上来的值坏掉时按 1 份兜底，不静默丢加购
  const qty = Math.max(1, Math.floor(Number(quantity) || 1));
  const key = `${line.dishId}:${line.specId}`;
  const cart = cartState();
  const exist = cart.find(l => l.key === key);
  if (exist) {
    exist.quantity += qty;
    if (line.itemRemark !== undefined) exist.itemRemark = line.itemRemark;
    saveCart();
    return;
  }
  cart.push({ ...line, key, quantity: qty, itemRemark: line.itemRemark ?? '' });
  saveCart();
}

/**
 * 批量并入若干行（「再来一单」用）：同一 dish+spec 合并份数，整批只落盘一次。
 * 与循环调用 cartAdd 的差别只是「一次写完」，避免几十次同步写 storage。
 * 已经点过的菜不会被覆盖 —— 顾客自己加的量不该被历史订单冲掉。
 */
export function cartMergeLines(lines: CartLine[]): void {
  if (!lines.length) return;
  const cart = cartState();
  for (const line of lines) {
    const exist = cart.find(l => l.key === line.key);
    if (exist) {
      exist.quantity += line.quantity;
      if (line.itemRemark) exist.itemRemark = line.itemRemark;
      continue;
    }
    cart.push({ ...line });
  }
  saveCart();
}

/** 增减份数；减到 0 自动移除该行。 */
export function cartChangeQty(key: string, delta: number): void {
  const cart = cartState();
  const idx = cart.findIndex(l => l.key === key);
  if (idx < 0) return;
  const next = cart[idx].quantity + delta;
  if (next <= 0) cart.splice(idx, 1);
  else cart[idx].quantity = next;
  saveCart();
}

export function cartSetRemark(key: string, itemRemark: string): void {
  const line = cartState().find(l => l.key === key);
  if (!line) return;
  line.itemRemark = itemRemark;
  saveCart();
}

export function cartRemove(key: string): void {
  const cart = cartState();
  const idx = cart.findIndex(l => l.key === key);
  if (idx >= 0) cart.splice(idx, 1);
  saveCart();
}

export function cartClear(): void {
  cartState().length = 0;
  saveCart();
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
