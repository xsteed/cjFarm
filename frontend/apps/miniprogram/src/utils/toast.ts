// 全局轻提示封装。
//
// 它是 shared/ui 通知接口在小程序端的实现（见 api/client.ts 的 setUiNotifier）：
// 逻辑层（api/http）只负责「该提示什么」，不关心用什么弹。
//
// ⚠️ 只在**带图标**时才截断文案：微信对 `icon: 'success'` 的标题限制约 7 个汉字，
// 但 `icon: 'none'` 允许两行、也不截断。错误原因（如后端返回的「桌台已被占用，请扫码后重试」）
// 恰恰是最需要完整展示的内容，早期版本无差别截断过一次，务必保持现在这个区分。

/** 带图标 toast 的标题上限（约 7 个汉字）。 */
const ICON_MAX_LEN = 7;

function show(title: string, icon: 'success' | 'none', duration: number): void {
  const text = String(title || '').trim();
  if (!text) return;
  // 截断后连省略号一起不超过上限：slice(0, 7) + '…' 是 8 个字符，
  // 微信会再截一次，省略号反而被吃掉。
  const clipped = icon !== 'none' && text.length > ICON_MAX_LEN ? `${text.slice(0, ICON_MAX_LEN - 1)}…` : text;
  wx.showToast({ title: clipped, icon, duration, mask: false });
}

export const toast = {
  success(title: string): void {
    show(title, 'success', 1800);
  },
  warning(title: string): void {
    show(title, 'none', 2200);
  },
  error(title: string): void {
    show(title, 'none', 2600);
  },
  /** 需要完整展示较长文案时使用（如业务校验失败原因）。 */
  info(title: string): void {
    show(title, 'none', 2600);
  }
};
