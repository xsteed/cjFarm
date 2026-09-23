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
