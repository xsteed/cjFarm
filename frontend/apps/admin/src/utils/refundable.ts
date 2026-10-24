// 「在线退款」资格判定（唯一实现）。
//
// 订单列表行（Orders.vue）与订单详情弹窗（useOrderActions）各写过一份、逐行相同：
// 仅在线支付（微信/支付宝）且已支付、未全额退款、且没有处理中退款的订单可发起在线退款。
// 两处各自维护极易改一处漏一处 —— 比如只在详情里排除 refundingAmount，
// 列表上的退款按钮就会「可点但被后端拒」。
//
// 权限位（refund:operate）不在这里判断：调用方先看权限，再看这单的支付状态允不允许退。

/** 只依赖下面几个状态字段，OrderRow / OrderSummary / OrderDetail 均可直接传入。 */
export interface RefundableOrder {
  payStatus?: number | null;
  payChannel?: string | null;
  refundAmount?: number | null;
  refundingAmount?: number | null;
  totalAmount?: number | null;
}

export function isRefundable(order: RefundableOrder | null | undefined): boolean {
  if (!order) return false;
  return (
    order.payStatus === 1 &&
    ['wxpay', 'alipay'].includes(order.payChannel ?? '') &&
    // refundingAmount（处理中的退款单）同样占用可退额度，必须排除：
    // 否则退款到账前按钮仍可点，点了会被后端按额度校验拒绝。
    !((order.refundingAmount || 0) > 0) &&
    (order.refundAmount || 0) < (order.totalAmount || 0)
  );
}
