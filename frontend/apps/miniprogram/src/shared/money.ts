// 全局金额格式化唯一实现。
//
// main.ts 注册的全局属性 $money 与各页面「TS 模板消费不到全局属性」而写的本地替代
// 函数共用这一份,避免多处各自复制一份实现、改一处漏一处(此前 4 处各写了一遍)。
export function formatMoney(v: unknown): string {
  const n = Number(v || 0);
  return Number.isFinite(n) ? n.toFixed(2) : '0.00';
}

/** 供组件直接 import 使用(模板里可用),行为与全局 $money 完全一致。 */
export const money = formatMoney;

/**
 * 金额四舍五入到两位小数(按分取整),用于购物车的逐项累加与优惠计算。
 *
 * H5 顾客端与小程序端（store/cart）共用同一套计价口径，实现各自独立维护。
 * 两端此前各写一份且实现漂移(一边加了 Number.EPSILON、一边没加),同一个购物车
 * 在两端会显示出相差 1 分的合计金额 —— 顾客会认为其中一端算错了。
 * 最终金额仍以后端重算为准(见 README「安全与生产要点」),这里只保证展示一致。
 */
export function round2(v: number): number {
  if (!Number.isFinite(v)) return 0;
  return Math.round((v + Number.EPSILON) * 100) / 100;
}
