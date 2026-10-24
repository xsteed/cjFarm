/**
 * 先付新单的等待时长与两档阈值（唯一来源）。
 *
 * 先付单的厨房单不在下单时出，而是商家点「确认收款」那一刻才补出（printKitchenOnPaid），
 * 所以「下单到现在」的时长就等于顾客干等、后厨没单的时长。
 * 订单管理卡片上的「超时」角标与取消订单弹窗里的说明共用这一份阈值，
 * 避免两处说法不一致（一处说 20 分钟该取消，另一处还写 15 分钟）。
 */

/** 超过该时长：角标由「新」转红「超时」，提示尽快确认收款 */
export const NEW_ORDER_WARN_MIN = 5;

/** 超过该时长：大概率已线下处理/顾客已走，文案升级为引导取消订单 */
export const NEW_ORDER_STALE_MIN = 20;

// 后端下发 'YYYY-MM-DD HH:mm:ss'，Safari 不认这种写法，统一斜杠化（与看板同口径）。
// 时间缺失/非法时返回 0，调用方按「未超时」处理，不会误报。
export function newOrderWaitMinutes(createTime?: string, now: number = Date.now()): number {
  const t = new Date(String(createTime || '').replace(/-/g, '/')).getTime();
  if (!Number.isFinite(t)) return 0;
  return Math.max(0, Math.floor((now - t) / 60_000));
}

/**
 * 是否属于「等收款的新单」：已下单(1) + 未支付(0) + 商家从未处理过(isNew)。
 * 后付模式的厨房单在下单时就出了，不会因为没收款而不出菜，调用方不要再给它加超时提示。
 * 是否先付模式由调用方叠加判断（本工具不依赖配置单例，便于复用）。
 */
export function isPendingPayNewOrder(row: { isNew?: boolean; orderStatus?: number; payStatus?: number }): boolean {
  return !!row?.isNew && row.orderStatus === 1 && row.payStatus === 0;
}
