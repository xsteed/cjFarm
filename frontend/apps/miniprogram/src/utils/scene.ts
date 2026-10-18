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

/** 安全解码：非法百分号编码时按原值返回（decodeURIComponent 会抛）。 */
function tryDecode(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

/**
 * 解析「小程序内 wx.scanCode 扫到的内容」里的桌台码（首页与菜单页的「扫码点餐」共用）。
 *
 * 桌牌上可能贴两种码：
 *   1) H5 点餐码（管理端默认）：`https://<域名>/order/<桌台码>`；
 *      顾客也可能扫到带参数的分享链接（`?tableCode=` / `?scene=`）。
 *   2) 小程序码：微信在**部分基础库**下会把码里携带的页面放进 `path`
 *      （形如 `pages/index/index?scene=K7M3PQ9X`），这里一并兼容。
 *
 * 扫到无关内容（官网、付款码、商品条码）返回空串 —— 调用方据此提示
 * 「请扫描桌面上的点餐码」，而不是拿一串网址去请求桌台接口。
 */
export function parseScannedTableCode(text: string | undefined | null, path?: string): string {
  const link = String(text || '').trim();
  const pagePath = String(path || '').trim();

  // 小程序码的 path：?scene= / ?tableCode=
  const pageQuery = /[?&](?:table_?code|table|scene)=([^&#]+)/i.exec(pagePath);
  if (pageQuery) {
    const code = decodeScene(tryDecode(pageQuery[1]));
    if (code) return code;
  }

  if (!link) return '';

  // H5 点餐码：/order/<桌台码>（存量码里是数字桌号，路径格式相同）
  const orderPath = /\/order\/([A-Za-z0-9_-]{1,32})(?:[?#/]|$)/.exec(link);
  if (orderPath) return orderPath[1];

  // 带参数的链接：?tableCode= / ?table= / ?scene=
  const linkQuery = /[?&](?:table_?code|table|scene)=([^&#]+)/i.exec(link);
  if (linkQuery) {
    const code = decodeScene(tryDecode(linkQuery[1]));
    if (code) return code;
  }

  // 是链接但不是点餐码：宁可让顾客重扫，也不要拼出一个假码去请求接口
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(link)) return '';

  // 裸码值（小程序码被解析出来 / 手输调试码的形态）
  return /^[A-Za-z0-9_-]{1,32}$/.test(link) ? link : '';
}

/**
 * 汇总 **App.onLaunch** 的启动参数，得到本次进入对应的桌台码。
 *
 * 为什么不能直接用 resolveTableCode(options)：
 *   - 页面 onLoad 里 `options.scene` 就是小程序码携带的字符串；
 *   - App.onLaunch 里 `options.scene` 是**场景值数字**（如 1047 = 扫小程序码），
 *     真正的码值在 `options.query.scene`。把 1047 当桌台码解析会得到 "1047"
 *     这种假入口，把后台「扫码入口 Top」污染掉 —— 所以这里只接受字符串形态。
 *
 * 两种形态都覆盖：扫小程序码（query.scene）与扫普通二维码/分享进入（query.tableCode）。
 * 什么都没带的「直接打开」返回空串，属正常情况：那次访问确实不是从桌台码进来的。
 */
export function resolveLaunchTableCode(options?: {
  scene?: unknown;
  query?: Record<string, unknown>;
}): string {
  const q = options?.query || {};
  const scene = typeof q.scene === 'string' ? q.scene : typeof options?.scene === 'string' ? options.scene : '';
  const tableCode = typeof q.tableCode === 'string' ? q.tableCode : typeof q.table === 'string' ? q.table : '';
  return resolveTableCode({ scene, tableCode });
}
