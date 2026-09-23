// 订单状态码唯一来源(与后端 tb_order.order_status 语义一致)。
//
// 展示层文案/主题在 constants/dicts.ts 的 ORDER_STATUS,这里的职责是给逻辑判断
// 提供命名常量:数字散落在轮询、按钮矩阵、顾客端视图等多处,各自维护极易漏改
// (如「加菜允许 1/2/3」「完成判定 4」「取消判定 5」三处语义不同却长得一样)。
export const ORDER_STATUS_CODE = {
  PLACED: 1, // 已下单
  MAKING: 2, // 制作中
  SERVED: 3, // 已上齐(用餐中)
  DONE: 4, // 已完成
  CANCELED: 5 // 已取消
} as const;

// 进行中:1/2/3 —— 可加菜(未支付时)、可催菜、占用桌台
export const ACTIVE_STATUSES: readonly number[] = [
  ORDER_STATUS_CODE.PLACED,
  ORDER_STATUS_CODE.MAKING,
  ORDER_STATUS_CODE.SERVED
];

export function isActiveStatus(s: number | undefined | null): boolean {
  return s != null && ACTIVE_STATUSES.includes(s);
}
