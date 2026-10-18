// ============================================================================
// 小程序端安全的键值存储(wx.storage 直接封装)
//
// 小程序没有 localStorage / window,存储后端固定是 wx.storage。所有读写包一层
// try/catch:旧版基础库或「存储被禁用 / 已满」时读写会抛错,这些调用遍布令牌存取
// 与购物车持久化,漏一处就是白屏。读失败视为无值、写/删失败静默,
// 最坏结果只是拿不到令牌 → 后端 401 → 正常触发静默重登,行为可预期。
// ============================================================================

export interface StorageBackend {
  get(key: string): string | null;
  set(key: string, value: string): void;
  remove(key: string): void;
}

/**
 * 小程序存储实现(wx.storage 同步 API)。
 *
 * getStorageSync 在 key 不存在时返回 '' 而非 null,这里统一归一成 null,
 * 与「无值」的语义对齐(调用方用 `if (v)` 判断即可,不必区分 '' 和 null)。
 */
const mpStorage: StorageBackend = {
  get(key: string): string | null {
    try {
      const v = wx.getStorageSync(key);
      return v === '' || v == null ? null : String(v);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      wx.setStorageSync(key, value);
    } catch {
      /* 存储被禁用/已满:登录态退化为本次会话内可用 */
    }
  },
  remove(key: string): void {
    try {
      wx.removeStorageSync(key);
    } catch {
      /* 同上 */
    }
  }
};

/** 安全的键值存储:调用方无需关心底层实现,也无需再包 try/catch。 */
export const safeStorage: StorageBackend = mpStorage;
