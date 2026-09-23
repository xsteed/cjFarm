// ============================================================================
// localStorage 安全包装
//
// Safari 隐私浏览(旧版)与「禁用站点数据」的浏览器里,localStorage 的读写会直接
// 抛 SecurityError;登录态读写遍布请求拦截器、路由守卫与权限模块,任何一处裸
// 调用都会让页面在加载阶段白屏。统一在这里兜底:读失败视为无值、写/删失败静默,
// 最坏结果只是拿不到令牌 → 后端 401 → 正常回登录页,行为可预期。
// ============================================================================

export const safeStorage = {
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
      /* 存储被禁用/已满:静默降级,登录态退化为本次会话内可用 */
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
