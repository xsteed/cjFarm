// 报表拆分后各子组件共享的图表配色与格式化工具，避免重复实现。
import { formatMoney } from '../../utils/money';

export const AXIS_COLOR = '#8C8C8C';
export const AXIS_LINE = '#EEEEEE';
export const SPLIT_LINE = '#F5F5F5';
export const BRAND = '#FF6B35';
export const BRAND_DEEP = '#F0481F';
export const GREEN = '#1FC97E';
export const BLUE = '#2B6CD4';
export const AMBER = '#FFB020';

/**
 * 金额轴标签做「万」单位收敛，否则 5 位数会把左侧留白撑得很宽，
 * 手机窄屏下几乎挤没了绘图区。
 */
export function axisYuan(v: unknown): string {
  const n = Number(v || 0);
  if (n >= 10000) return (n / 10000).toFixed(n >= 100000 ? 0 : 1) + '万';
  return String(n);
}

export const yuan = (v: unknown): string => Number(v || 0).toFixed(2);

// 全局 $money 的本地替代：与 utils/money 共享同一实现。
export const money = formatMoney;

export interface Deltas {
  todayAmount: number | null;
  todayOrder: number | null;
  todayGuest: number | null;
  monthAmount: number | null;
}

// ECharts tooltip formatter 参数在 axis 触发时是数组、item 触发时是单对象；
// 统一归一成单对象，方便各图表复用同一套取数逻辑。
export interface TooltipParam {
  dataIndex: number;
  name: string;
  value?: unknown;
  axisValue?: string | number;
}

export type TooltipParams = TooltipParam | TooltipParam[];

export function pickTooltipParam(ps: TooltipParams): TooltipParam {
  return Array.isArray(ps) ? ps[0] : ps;
}
