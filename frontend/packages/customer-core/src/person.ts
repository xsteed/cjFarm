// 就餐人数的统一口径（H5 / 小程序共用）。
//
// 茶位费 = 人数 × 每位茶位费：两端人数口径不一致，同一张桌就会算出两个价，
// 也可能被异常输入打穿成大额应收。上限与后端的 1..100 防线
// （customer_service.go / order_service.go）对齐，前端收得更紧 —— 20 人
// 已覆盖绝大多数桌况，且与 CartDrawer / 订单页弹窗的原有约束一致。
export const PERSON_MIN = 1;
export const PERSON_MAX = 20;

/**
 * 收敛到合法区间，非法输入兜底为 1。
 * 与小程序 setPersonCount 的原 clamp 规则一致（Number(n) || 0 → floor → 夹紧）。
 */
export function clampPerson(n: unknown): number {
  const v = Math.floor(Number(n) || 0);
  return Math.max(PERSON_MIN, Math.min(PERSON_MAX, v));
}

/** 是否为一次合法的显式选择（0 / 小数 / 超范围 / 非数字都算「没选」）。 */
export function isValidPerson(n: unknown): boolean {
  const v = Math.floor(Number(n) || 0);
  return v >= PERSON_MIN && v <= PERSON_MAX;
}
