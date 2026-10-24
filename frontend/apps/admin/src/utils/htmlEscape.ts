// HTML 转义（唯一实现）。
//
// 打印页（A4 票据 printA4.ts / 桌台码 tableQr.ts）都是「拼 HTML 字符串 → 新窗口 write」，
// 顾客输入（备注、桌台名、店名）直接进 HTML，转义规则必须同一份：
// 两处此前各写了一遍，且差了一个单引号 —— 一旦有人只改一处，同一条顾客备注
// 在两种打印页里就会出现不同的转义结果。
const HTML_ESCAPE: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
};

/** 转义 HTML 特殊字符；null / undefined 视为空串。 */
export function escapeHtml(v: unknown): string {
  return String(v == null ? '' : v).replace(/[&<>"']/g, c => HTML_ESCAPE[c]);
}

/** 短名别名：打印模板里出现频率极高，保持 `esc(x)` 的可读性。 */
export const esc = escapeHtml;
