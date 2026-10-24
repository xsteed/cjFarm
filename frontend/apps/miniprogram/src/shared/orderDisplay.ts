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

// ============================================================================
// 加菜批次标签
//
// 后端在 tb_order_item.batch_no 记录加菜批次(1=首次下单,每次加菜 +1)。
// 顾客端(H5/小程序)与管理端订单详情共用同一展示口径:
//   - 只有「本单加过菜」(存在多个批次)才逐行打标,单批次订单不打,避免满屏标签;
//   - 出现顺序编号而不是直接用 batchNo:后端批次号万一跳号(1、3),文案也不会错位。
// ============================================================================

/** 只依赖 batchNo 字段,OrderItem / 任意带批次号的行对象均可直接传入。 */
export interface BatchNoItem {
  batchNo?: number | null;
}

/**
 * 按明细顺序计算每行的批次标签:首单 → 「首单」,第 N 次加菜 → 「加菜N」。
 * 返回数组与入参 items 一一对应;单批次订单每项都是空串(调用方据此不打标)。
 */
export function batchLabels<T extends BatchNoItem>(items: readonly T[] | null | undefined): string[] {
  const list = items || [];
  const seqOf = new Map<number, number>();
  for (const it of list) {
    const b = Number(it.batchNo) || 1;
    if (!seqOf.has(b)) seqOf.set(b, seqOf.size + 1);
  }
  const multiBatch = seqOf.size > 1;
  return list.map(it => {
    if (!multiBatch) return '';
    const seq = seqOf.get(Number(it.batchNo) || 1) || 1;
    return seq <= 1 ? '首单' : `加菜${seq - 1}`;
  });
}

// ============================================================================
// 用餐 / 等餐时长文案
//
// 订单页状态头的「已等 X 分钟」与管理端桌台列表的「已用餐 X」同一套口径:
// 按分钟取整,跨小时说到小时,跨日说到天。入参只收分钟数,时间解析留给调用方。
// ============================================================================

/** 把分钟数说成人话:0 分钟、64 分钟、25 小时各说各的,不出现「90 分钟」。 */
export function durationText(minutes?: number | null): string {
  const m = Math.max(0, Number(minutes) || 0);
  if (m < 60) return `${m} 分钟`;
  const h = Math.floor(m / 60);
  const restMin = m % 60;
  if (h < 24) return restMin ? `${h} 小时 ${restMin} 分` : `${h} 小时`;
  const days = Math.floor(h / 24);
  const restHour = h % 24;
  return restHour ? `${days} 天 ${restHour} 小时` : `${days} 天`;
}
