// 会话态：本次进入的桌台、当前订单、店铺公开配置。
//
// 状态本体挂在 App 实例的 globalData 上（理由见 src/types.ts 顶部注释）；
// 本模块只提供「读写这份状态」的函数，页面照旧 `import { sessionState } from '../store/session'`。
import { STORAGE_KEY } from '../config';
import { clampPerson, isValidPerson } from '../core/person';
import type { GlobalData, SessionState } from '../types';
import type { OrderDetail, PublicConfig, TableInfo } from '../types';

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
  // 顶部展示「店铺名称 · 标语」(标语为系统配置的独立字段,留空则只显示名称)
  s.shopName = [config.shop_name, config.shop_slogan].filter(Boolean).join(' · ') || '扫码点餐';
}

/**
 * 更新就餐人数（1..20），返回收敛后的值。
 * 菜单页与购物车页都读它算茶位费，保证两页合计一致。
 * 区间常量与收敛规则收在 core/person.ts，与后端注释同一口径。
 */
export function setPersonCount(n: number): number {
  const next = clampPerson(n);
  sessionState().personCount = next;
  return next;
}

// ---- 入座选人数（茶位费门店） ----
//
// 「选人数」面板只在「本桌无订单 + 茶位费开启 + 本会话没选过」时出现（见菜单页
// renderMenu）。「已选」的记忆分两层：
//   ・personChosen：本次运行（切页不丢，冷启动归零）；
//   ・dining_mp_person:{桌台码}：跨冷启动，60 分钟窗口 —— 与「未开单购物车」
//     的恢复窗口（store/cartPersist）一致：宁可多问一次，也不沿用陌生人的数。

/** 人数记忆的本地存储 key 前缀（按桌台码隔离，换桌不会串人数）。 */
const PERSON_KEY_PREFIX = 'dining_mp_person';
/** 未开单的人数记忆窗口：与「未开单购物车」一致（store/cartPersist）。 */
const PERSON_TTL_MS = 60 * 60 * 1000;

/** 顾客显式选过人数（面板确认 / 购物车步进 / 订单页改人数 / 恢复购物车）。 */
export function markPersonChosen(): void {
  sessionState().personChosen = true;
}

/** 把本次选择落盘，供冷启动恢复（60 分钟内同一桌有效）。 */
export function persistPersonChoice(): void {
  const s = sessionState();
  if (!s.tableCode) return;
  try {
    wx.setStorageSync(`${PERSON_KEY_PREFIX}:${s.tableCode}`, {
      count: s.personCount,
      at: Date.now()
    });
  } catch {
    /* 存储不可用（磁盘满 / 隐私限制）：退化为本次运行内有效 */
  }
}

/**
 * 冷启动恢复上次的选择：有效且未过期 → 写回会话并标记已选，返回 true。
 * 换桌 / 过期 / 非法值一律视为「没选过」，交给面板重新问。
 */
export function restorePersonChoice(): boolean {
  const s = sessionState();
  if (!s.tableCode) return false;
  let data: { count?: unknown; at?: unknown } | '' | undefined;
  try {
    data = wx.getStorageSync(`${PERSON_KEY_PREFIX}:${s.tableCode}`) as typeof data;
  } catch {
    return false;
  }
  if (!data || typeof data !== 'object') return false;
  const n = Math.floor(Number(data.count) || 0);
  const at = Number(data.at);
  if (!isValidPerson(n) || !Number.isFinite(at) || Date.now() - at > PERSON_TTL_MS) return false;
  setPersonCount(n);
  markPersonChosen();
  return true;
}

/**
 * 采纳同桌共享的入座人数：桌台接口带 pendingPersonCount（别的设备已选过）时，
 * 本机直接沿用并标记已选，不再弹「选人数」面板。返回是否采纳成功。
 *
 * 共享值只是预填默认：权威值是下单请求里的 personCount，
 * 所以「本机已选过」的优先级高于共享值（restorePersonChoice 先判断）。
 */
export function adoptPendingPerson(table: TableInfo | null): boolean {
  const n = Math.floor(Number(table?.pendingPersonCount) || 0);
  if (!isValidPerson(n)) return false;
  setPersonCount(n);
  markPersonChosen();
  return true;
}

/**
 * 本桌还有「已付款但出餐未完成」的订单 = 这桌人正在吃，茶位费已随那些单收过：
 * 人数沿最近一笔，不再重复问。返回是否采纳成功。
 */
export function adoptServingPerson(table: TableInfo | null): boolean {
  const serving = table?.servingOrders || [];
  if (!serving.length) return false;
  // 后端按订单倒序返回，首笔即最近一笔
  const n = Math.floor(Number(serving[0]?.personCount) || 0);
  if (!isValidPerson(n)) return false;
  setPersonCount(n);
  markPersonChosen();
  return true;
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

// ---- 未结账订单的归属认领 ----
//
// 扫码进来时本桌那笔「未结账订单」很可能是上一桌留下的（店家忘了点收款,订单就一直
// 挂在桌上）。不问一句直接跳订单页，等于把上一桌的账单摊给新客人。认领一次后记在本
// 地，本次使用不再重复打扰。
const MINE_KEY = 'dining_mp_mine';

function mineKey(tableCode: string, orderNo: string): string {
  return `${MINE_KEY}:${tableCode}:${orderNo}`;
}

/** 取认领结果：'1' 是我的 / 'no' 不是我的 / '' 还没问过。 */
export function ownOrderMark(tableCode: string, orderNo: string): string {
  try {
    return String(wx.getStorageSync(mineKey(tableCode, orderNo)) || '');
  } catch {
    return '';
  }
}

export function markOwnOrder(tableCode: string, orderNo: string, mine: boolean): void {
  try {
    wx.setStorageSync(mineKey(tableCode, orderNo), mine ? '1' : 'no');
  } catch {
    /* 存储不可用：退化为每次进入都问一次，不会误认他人的单 */
  }
}
