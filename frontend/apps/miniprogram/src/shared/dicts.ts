// 小程序只需要订单状态字典(订单页标状态、出餐进度用)。
//
// 其余管理端字典(桌台可用性 / 结算方式 / 挂账状态 / 退款状态 / 打印机通道与类型 /
// 打印台账 / 操作日志)此前是从管理端整段拷过来的,小程序一个都没用 ——
// 已删除,避免两端字典各自漂移时还要同步一份没人读的副本。
export type DictTheme = 'default' | 'primary' | 'success' | 'warning' | 'danger';

export type DictOption = {
  label: string;
  theme: DictTheme;
};

export const ORDER_STATUS: Record<number, DictOption> = {
  1: { label: '已下单', theme: 'primary' },
  2: { label: '制作中', theme: 'warning' },
  // 不写「已上齐」:状态由商家手动维护、可能滞后于实际上菜,「用餐中」是不打包票的阶段词
  3: { label: '用餐中', theme: 'success' },
  4: { label: '已完成', theme: 'success' },
  5: { label: '已取消', theme: 'danger' }
};
