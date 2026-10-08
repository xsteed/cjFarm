// 订单结算状态展示:文案与标签主题。
//
// 「订单管理」列表与订单详情两处各写了一遍 payText/payTheme(逐行相同),
// 抽成共享实现;入参只取展示所需字段,OrderSummary / OrderDetail / OrderRow 均可直接传入。
export type PayTagTheme = 'default' | 'primary' | 'success' | 'warning' | 'danger';

export interface PayState {
  payStatus?: number | null;
  settleType?: string | null;
  creditStatus?: number | null;
  creditAmount?: number | null;
  payType?: string | null;
}

/** 结算状态文案:未支付 / 已支付 / 免单 / 挂账待收 / 挂账已结 */
export function payStatusText(p: PayState | null | undefined): string {
  if (!p || p.payStatus !== 1) return '未支付';
  if (p.settleType === 'free') return '免单';
  if (p.settleType === 'credit') {
    return p.creditStatus === 1
      ? `挂账待收 ¥${Number(p.creditAmount || 0).toFixed(2)}`
      : `挂账已结（${p.payType || '-'}）`;
  }
  return `已支付（${p.payType || '-'}）`;
}

export function payStatusTheme(p: PayState | null | undefined): PayTagTheme {
  if (!p || p.payStatus !== 1) return 'default';
  if (p.settleType === 'free') return 'warning';
  if (p.settleType === 'credit') return p.creditStatus === 1 ? 'warning' : 'success';
  return 'success';
}
