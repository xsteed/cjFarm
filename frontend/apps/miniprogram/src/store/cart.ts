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
import { toast } from '../utils/toast';

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

/** 规格剩余份数归一：-1 / 非数字 / 负数视为不限量，>=0 为可购份数（与后端 po.Spec 一致）。 */
function stockCapOf(stock: number | undefined): number {
  const n = Number(stock);
  return Number.isFinite(n) && n >= 0 ? n : Number.POSITIVE_INFINITY;
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
  const cap = stockCapOf(line.stock);
  if (exist) {
    // 快照必须在任何提前 return 之前刷新:售罄分支 return 时若没更新,
    // exist.stock 会停留在旧的 -1(不限量),购物车页的 + 就按 Infinity 放行。
    if (line.stock !== undefined) exist.stock = line.stock;
    if (exist.quantity + qty > cap) {
      // 超出剩余份数:夹到上限并明确告知,别让顾客到提交才被后端拒
      const allowed = Math.max(0, cap - exist.quantity);
      if (allowed <= 0) {
        toast.info(cap > 0 ? `该规格仅剩 ${cap} 份` : '该商品已售罄');
        return;
      }
      toast.info(`该规格仅剩 ${cap} 份，已按上限加入`);
      exist.quantity = cap;
    } else {
      exist.quantity += qty;
    }
    if (line.itemRemark !== undefined) exist.itemRemark = line.itemRemark;
    saveCart();
    return;
  }
  const add = Math.min(qty, cap);
  if (add <= 0) {
    toast.info('该商品已售罄');
    return;
  }
  if (add < qty) toast.info(`该规格仅剩 ${cap} 份，已按上限加入`);
  cart.push({ ...line, key, quantity: add, itemRemark: line.itemRemark ?? '' });
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
      // 快照以「再来一单」复核后的现值为准(rebuild 已按当前库存夹紧过)
      if (line.stock !== undefined) exist.stock = line.stock;
      // 合并也要按库存夹紧:自己 2 份 + 历史 3 份,库存只有 3 份时不能并成 5 份,
      // 否则提交时被后端按 stock >= qty 拒绝。
      const cap = stockCapOf(exist.stock);
      const next = exist.quantity + line.quantity;
      if (next > cap) {
        const allowed = Math.max(0, cap - exist.quantity);
        if (allowed <= 0) {
          toast.info(cap > 0 ? `该规格仅剩 ${cap} 份` : '该商品已售罄');
          continue;
        }
        toast.info(`该规格仅剩 ${cap} 份，已按上限并入`);
        exist.quantity = cap;
      } else {
        exist.quantity = next;
      }
      // 不拿历史单的备注覆盖当前行:那是顾客自己为这道菜写的(如「不要辣」)，
      // 「再来一单」只应补份数。与上面的注释「已经点过的菜不会被覆盖」保持一致。
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
  const line = cart[idx];
  // 加号同样按加购时记下的库存快照夹紧（这里拿不到菜单对象，用快照）
  if (delta > 0) {
    const cap = stockCapOf(line.stock);
    if (line.quantity + delta > cap) {
      toast.info(cap > 0 ? `该规格仅剩 ${cap} 份` : '该商品已售罄');
      return;
    }
  }
  const next = line.quantity + delta;
  if (next <= 0) cart.splice(idx, 1);
  else line.quantity = next;
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
