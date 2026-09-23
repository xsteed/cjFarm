export type DictTheme = 'default' | 'primary' | 'success' | 'warning' | 'danger';

export type DictOption = {
  label: string;
  theme: DictTheme;
};

export const ORDER_STATUS: Record<number, DictOption> = {
  1: { label: '已下单', theme: 'primary' },
  2: { label: '制作中', theme: 'warning' },
  3: { label: '已上齐', theme: 'success' },
  4: { label: '已完成', theme: 'default' },
  5: { label: '已取消', theme: 'danger' }
};

// 结算方式:normal 正常收款 / free 免单 / credit 挂账
export const SETTLE_TYPE: Record<string, DictOption> = {
  normal: { label: '正常收款', theme: 'success' },
  free: { label: '免单', theme: 'warning' },
  credit: { label: '挂账', theme: 'primary' }
};

// 挂账状态:0 非挂账 1 待收款 2 已结清
export const CREDIT_STATUS: Record<number, DictOption> = {
  0: { label: '非挂账', theme: 'default' },
  1: { label: '待收款', theme: 'warning' },
  2: { label: '已结清', theme: 'success' }
};

// 退款单状态
export const REFUND_STATUS: Record<number, DictOption> = {
  0: { label: '退款中', theme: 'warning' },
  1: { label: '已退款', theme: 'success' },
  2: { label: '退款失败', theme: 'danger' }
};

// 打印机接入方式:
//   tcp   网络直连(要求后端与打印机同局域网)
//   feie  飞鹅云打印(打印机自己联网取单,后端在云上也能用)
//   agent 本地打印代理(后端只入队,门店内网的代理程序取单后直发 9100;
//         云部署 + 复用门店已有网络机时用它,不需要装任何打印机驱动)
export const PRINTER_PROVIDER: Record<string, DictOption> = {
  tcp: { label: '网络直连', theme: 'default' },
  feie: { label: '飞鹅云', theme: 'primary' },
  agent: { label: '本地代理', theme: 'warning' }
};

// 打印机类型
export const PRINTER_TYPE: Record<number, DictOption> = {
  1: { label: '厨房单', theme: 'danger' },
  2: { label: '食客小票', theme: 'primary' }
};

// 打印单据类型
export const PRINT_DOC_TYPE: Record<string, DictOption> = {
  kitchen: { label: '厨房单', theme: 'danger' },
  guest: { label: '食客小票', theme: 'primary' },
  test: { label: '测试页', theme: 'default' }
};

// 打印结果:0 失败 / 1 已送出 / 2 排队中(仅本地代理通道:已入队、等待门店代理取单)
export const PRINT_STATUS: Record<number, DictOption> = {
  0: { label: '失败', theme: 'danger' },
  1: { label: '已送出', theme: 'success' },
  2: { label: '排队中', theme: 'warning' }
};

// 打印触发场景
export const PRINT_TRIGGER: Record<string, string> = {
  order: '顾客下单',
  append: '顾客加菜',
  settle: '收银结账',
  test: '测试打印',
  reprint: '人工补打'
};

// 操作日志:操作类型(与后端 model.OperType* 一致)
export const OPER_TYPE: Record<string, DictOption> = {
  insert: { label: '新增', theme: 'primary' },
  update: { label: '修改', theme: 'warning' },
  delete: { label: '删除', theme: 'danger' },
  login: { label: '登录', theme: 'default' },
  grant: { label: '授权', theme: 'danger' },
  print: { label: '打印', theme: 'default' },
  other: { label: '其它', theme: 'default' }
};

// 操作日志:结果(0 失败 / 1 成功)
export const OPER_STATUS: Record<number, DictOption> = {
  0: { label: '失败', theme: 'danger' },
  1: { label: '成功', theme: 'success' }
};
