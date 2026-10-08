// ============================================================================
// 页面可见性适配器
//
// 轮询需要在「页面进入后台」时暂停、回到前台时立刻补一轮。Web 端靠
// document 的 visibilitychange 事件；小程序端没有 document，要用
// wx.onAppShow / wx.onAppHide。这里把差异收在一处:
//   - Web 端默认实现即可，无需接线;
//   - 小程序端在入口调 setVisibilityAdapter() 换成自己的订阅。
// ============================================================================

export interface VisibilityAdapter {
  /**
   * 订阅可见性变化。`handler(true)` = 进入后台，`handler(false)` = 回到前台。
   * 返回取消订阅函数。
   */
  subscribe(handler: (hidden: boolean) => void): () => void;
}

const webAdapter: VisibilityAdapter = {
  subscribe(handler) {
    // 非浏览器环境(SSR / 小程序)没有 document:退化为「永不变化」，
    // 最坏结果只是后台期间多轮询几拍，不影响功能正确性。
    if (typeof document === 'undefined') return () => {};
    const listener = () => handler(document.hidden);
    document.addEventListener('visibilitychange', listener);
    return () => document.removeEventListener('visibilitychange', listener);
  }
};

let adapter: VisibilityAdapter = webAdapter;

/** 替换可见性适配器(小程序端在入口调用一次即可)。 */
export function setVisibilityAdapter(next: VisibilityAdapter): void {
  adapter = next;
}

/** 按当前适配器订阅可见性变化，返回取消订阅函数。 */
export function subscribeVisibility(handler: (hidden: boolean) => void): () => void {
  return adapter.subscribe(handler);
}
