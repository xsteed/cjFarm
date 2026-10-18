// ============================================================================
// UI 通知适配器(唯一注入点)
//
// 接口层(api/http)只依赖这里的 `ui`;由各端入口决定真正的实现:
//   - 小程序: setUiNotifier({ success: (m) => wx.showToast(...), ... })
// 未注册时退化为「success/warning 静默、error 打 console」,不会抛错。
// ============================================================================

export interface UiNotifier {
  success(message: string): void;
  warning(message: string): void;
  error(message: string): void;
}

const fallback: UiNotifier = {
  // 成功/警告属于「锦上添花」,没有 UI 层时静默即可
  success: () => {},
  warning: () => {},
  // 失败必须留痕,否则线上会变成「点了没反应又查不到线索」
  error: (message: string) => console.error('[ui]', message)
};

let notifier: UiNotifier = fallback;

/**
 * 注册(或局部覆盖)通知实现。可多次调用,未提供的字段沿用当前实现。
 */
export function setUiNotifier(next: Partial<UiNotifier>): void {
  notifier = { ...notifier, ...next };
}

/**
 * 稳定的通知入口对象:方法转发到当前 notifier,
 * 这样各包在模块顶层 `import { ui }` 后,后注册的实现依然生效。
 */
export const ui: UiNotifier = {
  success: (message: string) => notifier.success(message),
  warning: (message: string) => notifier.warning(message),
  error: (message: string) => notifier.error(message)
};
