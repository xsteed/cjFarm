// 桌台码解析。
//
// 进入小程序有两条路径，拿到的参数形态不同：
//   1) 扫「小程序码」：参数在 options.scene 里（微信要求 ≤32 字符，通常形如
//      `tableCode=K7M3PQ9X`，也可能被 urlencode）；微信开发者工具的编译模式
//      里 scene 是原始值。
//   2) 扫「普通二维码」跳小程序 / 公众号菜单 / 直接分享页：参数是普通 query，
//      即 options.tableCode。
//
// 小程序运行环境没有 URL/URLSearchParams（JSCore 不保证提供），所以这里手写解析。

/** 从 scene 串中取出桌台码：支持 `tableCode=X`、`k=X`、裸 `X` 三种形态。 */
export function decodeScene(raw: string | undefined | null): string {
  const input = String(raw || '').trim();
  if (!input) return '';
  let s = input;
  try {
    s = decodeURIComponent(input);
  } catch {
    /* 非法百分号编码：按原值处理，宁可解析失败也不要抛错 */
  }
  const kv = /(?:^|&)table_?code=([^&]+)/i.exec(s);
  if (kv) return kv[1].trim();
  const first = s.indexOf('=');
  if (first >= 0)
    return s
      .slice(first + 1)
      .replace(/&.*$/, '')
      .trim();
  return s;
}

/** 汇总页面 onLoad 的 options，得到本次进入对应的桌台码。 */
export function resolveTableCode(options: Record<string, string | undefined> | undefined): string {
  if (!options) return '';
  return decodeScene(options.scene) || String(options.tableCode || options.table || '').trim();
}
