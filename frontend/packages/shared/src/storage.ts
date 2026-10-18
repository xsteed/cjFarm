// ============================================================================
// 可插拔的键值存储
//
// 「令牌存在哪」是各端唯一需要替换的部分：Web 用 localStorage、小程序用
// wx.storage、未来的 App 用原生存档；而「读写失败怎么办」的策略必须一致 ——
// 任何一处裸调用在 Safari 隐私模式 / 禁用站点数据 / 磁盘满时都会抛错，
// 而这些读写遍布请求拦截器与路由守卫，漏一处就是白屏。
//
// 因此照 @dining/shared/ui 的既有模式：包只定义接口与兜底语义，
// 由各端入口注入实现（小程序见 apps/miniprogram/src/api/client.ts）。
//
// 不注入任何实现时默认走 Web 的 localStorage —— 管理端与 H5 顾客端无需额外配置。
// ============================================================================

export interface StorageBackend {
  get(key: string): string | null;
  set(key: string, value: string): void;
  remove(key: string): void;
}

/**
 * Web 默认实现（localStorage）。
 *
 * 全部 try/catch：旧版 Safari 隐私浏览与「禁用站点数据」的浏览器里，
 * localStorage 的读写会直接抛 SecurityError。读失败视为无值、写/删失败静默，
 * 最坏结果只是拿不到令牌 → 后端 401 → 正常回登录页，行为可预期。
 */
const webStorage: StorageBackend = {
  get(key: string): string | null {
    try {
      return window.localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      /* 存储被禁用/已满：登录态退化为本次会话内可用 */
    }
  },
  remove(key: string): void {
    try {
      window.localStorage.removeItem(key);
    } catch {
      /* 同上 */
    }
  }
};

let backend: StorageBackend = webStorage;

/** 注入本端的存储实现（各端入口装配阶段调用一次；未注入时用 Web 默认实现）。 */
export function setStorageBackend(next: StorageBackend): void {
  backend = next;
}

/**
 * 安全的键值存储。调用方无需关心底层是 localStorage 还是 wx.storage，
 * 也无需再包一层 try/catch —— 兜底语义由注入的实现保证。
 */
export const safeStorage: StorageBackend = {
  get: (key: string) => backend.get(key),
  set: (key: string, value: string) => backend.set(key, value),
  remove: (key: string) => backend.remove(key)
};
