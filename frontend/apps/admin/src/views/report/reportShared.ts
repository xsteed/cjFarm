// 报表拆分后各子组件共享的图表配色与格式化工具，避免重复实现。
import { formatMoney } from '@dining/shared';

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

// 全局 $money 的本地替代：与 @dining/shared 的 money 共享同一实现。
export const money = formatMoney;

/**
 * 明细时间列的人类可读格式:当年显示「MM-DD HH:mm」,跨年带上年份;
 * 秒对报表没有参考价值,一律去掉(省出的宽度还给数据列)。
 * 入参是后端 'YYYY-MM-DD HH:mm:ss' 形态的本地时间字符串,按字符位直接截取、
 * 不经 Date 时区换算 —— 既避免 iOS Safari 对该形态 new Date 解析的兼容问题,
 * 也保证显示的就是落库时间。空返回 '—',畸形串原样返回(不吞信息)。
 */
export function shortTime(v: unknown, now = new Date()): string {
  const s = String(v || '').trim();
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(s);
  if (!m) return s || '—';
  const [, y, mo, d, h, mi] = m;
  if (Number(y) === now.getFullYear()) return `${mo}-${d} ${h}:${mi}`;
  return `${y}-${mo}-${d} ${h}:${mi}`;
}

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

// 访问统计下钻的负载:图表点击 → 访问明细弹窗的标题与筛选参数。
// device/os/browser 传「未知」时后端匹配空值行,与分布图归一口径一致。
export interface VisitDrill {
  title: string;
  hint?: string;
  query: {
    start: string;
    end: string;
    channel?: string;
    entry?: string;
    device?: string;
    os?: string;
    browser?: string;
  };
}

// ECharts 点击事件统一 off+on 重绑:实例按 key 复用,每次 render 的闭包各自
// 捕获当次数据,只绑一次会拿到上一轮的列表。测试的 echarts mock 没有 on/off,
// 做能力探测跳过。
export function bindChartClick(
  c: { on?: unknown; off?: unknown },
  handler: (p: { componentType?: string; dataIndex?: number; name?: string }) => void
): void {
  if (typeof c.on !== 'function' || typeof c.off !== 'function') return;
  c.off('click');
  c.on('click', handler);
}
