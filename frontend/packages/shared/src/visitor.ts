// ============================================================================
// 访客 ID(站点访问统计的匿名去重键)
//
// 后端 UV 口径见 docs/design-docs/visit-analytics/spec.md 第 1 节:
// 登录会员用 customer_id(最可靠),其余用「前端生成并持久化的随机 ID」——
// 同一设备再次扫码/启动能被识别为同一位访客;存储被禁用时后端自动退化为
// IP+UA 哈希兜底,两条路径都不影响点餐。
//
// 为什么放 @dining/shared:H5 用 localStorage,小程序用 wx.storage,
// 两端只是「存储后端」不同,生成规则必须一致(否则同一人在两端各算一个访客,
// 而服务端只认 ID 字符串本身)。
// ============================================================================

import { safeStorage } from './safeStorage';

/** 访客 ID 在本地存储里的键名(与登录态键同级管理,避免散落字符串)。 */
export const LS_VISITOR_ID = 'dining_visitor_id';

/** randomUUID 不可用时的降级长度(32 位十六进制,与 uuid v4 的熵同级)。 */
const FALLBACK_LEN = 32;

/**
 * 生成一个新的随机访客 ID。
 *
 * 单独导出:小程序端用 wx.storage 持久化(见 apps/miniprogram),Web 端用
 * localStorage(见下面的 visitorId)—— 存储后端不同,生成规则必须同一份,
 * 否则同一批设备在两端的 ID 格式不一致,排查时对不上账。
 */
export function generateVisitorId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  const bytes = new Uint8Array(FALLBACK_LEN / 2);
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(bytes);
  } else {
    // 极端老环境:Math.random 兜底。访客 ID 不承担安全职责(不鉴权、不含信息),
    // 可预测的唯一代价是 UV 精度,不会造成越权或数据问题。
    for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
}

/**
 * 取本机访客 ID(Web 端):没有则现场生成并落盘,之后同一设备永远拿到同一个值。
 *
 * 只读不回写是不行的 —— 打点接口是 fire-and-forget,若每次调用都生成新 ID,
 * 「同一访客」就永远无从谈起,UV 会退化成 PV。
 */
export function visitorId(): string {
  const existing = safeStorage.get(LS_VISITOR_ID);
  if (existing) return existing;
  const id = generateVisitorId();
  safeStorage.set(LS_VISITOR_ID, id);
  return id;
}
