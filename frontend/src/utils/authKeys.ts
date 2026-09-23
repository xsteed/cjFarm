// 登录态在 localStorage 里的键名（唯一来源）。
//
// 单独抽成模块，是因为有两处需要用到同一批键：
//   - `utils/perm.js`  读写完整登录态（登录 / 退出 / 刷新资料）
//   - `api/http.ts`    请求拦截器取令牌、401 时清空登录态
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
