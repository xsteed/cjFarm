// ============================================================================
// sessionStorage 安全包装（仅 Web）
//
// 与 localStorage 的分工：sessionStorage 随标签页生命周期走 —— 关掉重开就是一次新
// 会话。顾客端靠这个区分「同一位顾客刷新页面」与「换了一批客人重新扫码」。
// 小程序没有这个存储层级，也无此需求（页面 onHide/onShow 天然覆盖），因此不注入。
//
// 读写同样全部 try/catch：禁用站点数据的浏览器里它也抛 SecurityError。
// localStorage 的可插拔版本见 ./storage.ts（三端共用）。
// ============================================================================

export const safeSession = {
  get(key: string): string | null {
    try {
      return window.sessionStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      window.sessionStorage.setItem(key, value);
    } catch {
      /* 存储被禁用：退化为「每次进入都是新会话」，不会误恢复他人数据 */
    }
  }
};
