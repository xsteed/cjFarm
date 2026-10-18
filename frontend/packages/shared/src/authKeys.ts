// 登录态在 localStorage 里的键名（唯一来源）。
//
// 单独抽成模块，是因为有两处需要用到同一批键：
//   - `@dining/auth` 的 perm.ts      读写完整登录态（登录 / 退出 / 刷新资料）
//   - `@dining/api` 的 http.ts       请求拦截器取令牌、401 时清空登录态
// 两边若各写一份字符串，改名时极易漏改一处 —— 表现为「令牌取不到」
// 或「退出登录后权限缓存还在」。因此收敛到这里。
//
// ⚠️ `admin_token` 这个名字一旦改动，线上所有已登录用户都会掉线
//    （浏览器里存的是旧键），需要重新登录，非必要不要改。

export const LS_TOKEN = 'admin_token';
export const LS_USER = 'admin_user';
export const LS_REALNAME = 'admin_realname';
export const LS_ROLE = 'admin_role';
export const LS_ROLENAME = 'admin_rolename';
export const LS_PERMS = 'admin_perms';

/** 全部登录态键；「清空登录态」按此列表遍历，避免漏清造成脏缓存。 */
export const AUTH_KEYS: string[] = [LS_TOKEN, LS_USER, LS_REALNAME, LS_ROLE, LS_ROLENAME, LS_PERMS];

/**
 * 顾客会员令牌的本地键。
 *
 * ⚠️ 刻意**不**放进 AUTH_KEYS：那个列表的语义是「管理端登出时清空」。
 * 同一个浏览器里管理端与顾客端是两个身份，把顾客令牌塞进去会导致
 * 「管理端一退出，同浏览器的顾客也被踢下线」。
 *
 * 各端共用这一个键名。小程序历史上用的是 dining_mp_customer_token，本次统一后
 * 旧键被弃用：老用户升级后本地令牌读不到，下一次会员请求会静默重登一次
 * （wx.login 免费无感），而令牌本身只有 2h 有效期，实际影响可忽略。
 */
export const LS_CUSTOMER_TOKEN = 'dining_customer_token';
