// 会话态：本次进入的桌台、当前订单、店铺公开配置。
//
// 状态本体挂在 App 实例的 globalData 上（理由见 src/types.ts 顶部注释）；
// 本模块只提供「读写这份状态」的函数，页面照旧 `import { sessionState } from '../store/session'`。
import { STORAGE_KEY } from '../config';
import type { GlobalData, SessionState } from '../types';
import type { OrderDetail, PublicConfig, TableInfo } from '@dining/types';

/** 取到全局唯一的会话态对象（可读可写，写入后调用方自行 setData）。 */
export function sessionState(): SessionState {
  return getApp<GlobalData>().session;
}

interface Persisted {
  tableCode: string;
  orderNo: string;
}

function safeGet(key: string, fallback: Persisted): Persisted {
  try {
    const v = wx.getStorageSync(key) as Persisted | '' | undefined;
    if (!v || typeof v !== 'object') return fallback;
    return { tableCode: String(v.tableCode || ''), orderNo: String(v.orderNo || '') };
  } catch {
    return fallback;
  }
}

function safeSet(key: string, value: Persisted): void {
  try {
    wx.setStorageSync(key, value);
  } catch {
    /* 存储不可用（磁盘满 / 隐私限制）：只影响「下次进入能否恢复」，静默降级 */
  }
}

/** 上次进入的桌台码与订单号：用户从「最近使用」进来时没有 scene 参数，用它兜底。 */
export function readPersisted(): Persisted {
  return safeGet(STORAGE_KEY, { tableCode: '', orderNo: '' });
}

/** 记住本次桌台与订单，便于中途退出小程序后恢复。 */
export function persistSession(tableCode: string, orderNo: string): void {
  safeSet(STORAGE_KEY, { tableCode, orderNo });
}

/** 用桌台详情刷新会话（扫码进入后调用一次）。 */
export function applyTable(table: TableInfo): void {
  const s = sessionState();
  s.table = table;
  s.tableId = Number(table.tableId) || 0;
  s.tableCode = String(table.tableCode || s.tableCode || '');
}

/** 用公开配置刷新会话。 */
export function applyConfig(config: PublicConfig): void {
  const s = sessionState();
  s.config = config;
  s.shopName = String(config.shop_name || '') || '扫码点餐';
}

/**
 * 更新就餐人数（1..20），返回收敛后的值。
 * 菜单页与购物车页都读它算茶位费，保证两页合计一致。
 * 上限 20 与 H5 的 CartDrawer 对齐：茶位费按人数算，两端口径不一致会产生异常大额。
 */
export function setPersonCount(n: number): number {
  const next = Math.max(1, Math.min(20, Math.floor(Number(n) || 1)));
  sessionState().personCount = next;
  return next;
}

/** 把当前订单写回会话，并同步持久化（供下次按 orderNo 恢复）。 */
export function applyOrder(order: OrderDetail | null): void {
  const s = sessionState();
  s.currentOrder = order;
  persistSession(s.tableCode, order ? String(order.orderNo || '') : '');
}

/** 仅清掉内存里的当前订单，保留持久化的 orderNo（本桌还有已付款订单在出餐时用）。 */
export function clearCurrentOrder(): void {
  sessionState().currentOrder = null;
}

/** 订单结束（完成/取消/再点一单）：连持久化一起清掉，避免下次误进旧单。 */
export function resetOrder(): void {
  sessionState().currentOrder = null;
  persistSession(sessionState().tableCode, '');
}
