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
 * H5（@dining/customer-core 的 useCart）与小程序（store/cart）共用同一套口径;
 * 小程序侧是自包含副本(不能依赖工作区包),两处实现必须逐字一致。
 * 两端此前各写一份且实现漂移(一边加了 Number.EPSILON、一边没加),同一个购物车
 * 在两端会显示出相差 1 分的合计金额 —— 顾客会认为其中一端算错了。
 * 最终金额仍以后端重算为准(见 README「安全与生产要点」),这里只保证展示一致。
 */
export function round2(v: number): number {
  if (!Number.isFinite(v)) return 0;
  // 修正量必须跟着符号走。无条件加 EPSILON 等于对**负数**反向推了一把：
  // round2(-0.145) 会落到 -0.14 而不是对称的 -0.15。当前用到 round2 的地方
  // （菜品合计、茶位费、满减、合计）都非负，所以现在看不出来；但只要哪天有负数
  // 进场（退款、差额调整），两端就会各自给出差 1 分的结果。
  const bias = v >= 0 ? Number.EPSILON : -Number.EPSILON;
  return Math.round((v + bias) * 100) / 100;
}
