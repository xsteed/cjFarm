import { safeStorage } from './storage';
// ============================================================================
// 访客 ID(站点访问统计的匿名去重键)
//
// 后端 UV 口径:登录会员用 customer_id,其余用「前端生成并持久化的随机 ID」。
// 小程序端用 wx.storage 持久化(见 safeStorage),生成规则与 Web 端一致。
// ============================================================================

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

// 取值入口在小程序端自己的 utils/visitor.ts(mpVisitorId):它直接用 wx.getStorageSync,
// 而这里注入的 safeStorage 后端并不覆盖这套用法。此处曾有一份等价的 visitorId(),无调用方,已删。

/**
 * 取本机访客 ID(小程序端):没有则现场生成并落盘,之后同一设备恒定。
 * 与 H5 的 @dining/shared visitorId 同名同语义(平行实现);评价「我是否已评」
 * 按它判定,同桌他人评过不影响本机。
 */
export function visitorId(): string {
  const existing = safeStorage.get(LS_VISITOR_ID);
  if (existing) return existing;
  const id = generateVisitorId();
  safeStorage.set(LS_VISITOR_ID, id);
  return id;
}
