// ============================================================================
// 访客 ID(站点访问统计的匿名去重键)
//
// 后端 UV 口径:登录会员用 customer_id,其余用「前端生成并持久化的随机 ID」。
// 小程序端用 wx.storage 持久化(见 safeStorage),生成规则与 Web 端一致。
// ============================================================================

import { safeStorage } from './storage';

/** 访客 ID 在本地存储里的键名。 */
export const LS_VISITOR_ID = 'dining_visitor_id';

/** randomUUID 不可用时的降级长度(32 位十六进制,与 uuid v4 的熵同级)。 */
const FALLBACK_LEN = 32;

/**
 * 生成一个新的随机访客 ID(32 位十六进制)。
 *
 * 小程序没有 WebCrypto 的 randomUUID 保证,走 getRandomValues 降级;
 * 再不行用 Math.random 兜底(访客 ID 不承担安全职责,可预测的唯一代价是 UV 精度)。
 */
export function generateVisitorId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  const bytes = new Uint8Array(FALLBACK_LEN / 2);
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
}

/**
 * 取本机访客 ID:没有则现场生成并落盘,之后同一设备永远拿到同一个值。
 * 只读不回写不行 —— 打点接口是 fire-and-forget,若每次都生成新 ID,UV 会退化成 PV。
 */
export function visitorId(): string {
  const existing = safeStorage.get(LS_VISITOR_ID);
  if (existing) return existing;
  const id = generateVisitorId();
  safeStorage.set(LS_VISITOR_ID, id);
  return id;
}
