// ============================================================================
// 小程序端访客 ID(站点访问统计的匿名去重键)
//
// 生成规则在 shared/visitor.ts(32 位十六进制),
// 持久化用 wx.storage —— 小程序的存储按设备隔离,同一台手机再次启动
// 能识别为同一位访客;ID 缺失时后端自动退化为 IP+UA 哈希兜底。
//
// 键名与 Web 端保持同一个字符串:两端存储本来就互不相通,统一键名只是为了让
// 「本地存储里哪个键是访客 ID」这件事只有一种答案。
// ============================================================================

import { generateVisitorId, LS_VISITOR_ID } from '../shared/visitor';

/** 取本机访客 ID:没有则生成并落盘。存储不可用时返回一次性 ID(UV 退化,不影响点餐)。 */
export function mpVisitorId(): string {
  try {
    const existing = String(wx.getStorageSync(LS_VISITOR_ID) || '');
    if (existing) return existing;
    const id = generateVisitorId();
    wx.setStorageSync(LS_VISITOR_ID, id);
    return id;
  } catch {
    return generateVisitorId();
  }
}
