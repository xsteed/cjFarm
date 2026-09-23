import { baseURL, http } from './http';
import type { ApiResponse } from '../types/api';
import type {
  ChangePasswordPayload,
  LoginPayload,
  LoginResult,
  Profile,
  RememberLoginPayload,
  RememberSessionsResult
} from '../types/entities';

// 登录
export const login = (data: LoginPayload): Promise<LoginResult> => http.post<unknown, LoginResult>('/auth/login', data);

// 用「记住我」令牌静默换取新登录态(免登录 7/30 天);过期与否由后端查库裁决。
// silent:失败(令牌过期 401 / 弱网超时)不弹全局 toast —— 这是页面打开时的
// 后台尝试,用户还没做任何操作,安静地留在登录页即可。
export const rememberLogin = (data: RememberLoginPayload): Promise<LoginResult> =>
  http.post<unknown, LoginResult>('/auth/remember-login', data, { silent: true });

// 吊销「记住我」令牌(退出登录 / 取消记住时调用)。
// 刻意不走上面的 axios 实例:该请求不依赖登录态,也不该被全局拦截器接住 ——
// 拦截器的 401 处理会强制跳转登录页,会干扰退出流程。fetch + keepalive
// 保证页面跳转后请求仍能送达;失败由调用方(utils/remember.js 的 revokeRemember)兜底。
export const logoutRemember = (token: string): Promise<Response> =>
  fetch(`${baseURL}/auth/logout`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rememberToken: token }),
    keepalive: true
  });

// 修改密码
export const changePassword = (data: ChangePasswordPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/auth/password', data);

// 我的信息与权限(登录态刷新用:返回 username / realName / roleKey / roleName / perms)
export const getProfile = (): Promise<Profile> => http.get<unknown, Profile>('/admin/auth/profile');

// 「记住我」会话(登录设备页):列出当前账号仍有效的免登录设备。
// 令牌只回传前 8 位(识别本机用),完整令牌不出服务端。
export const listRememberSessions = (): Promise<RememberSessionsResult> =>
  http.get<unknown, RememberSessionsResult>('/admin/auth/remember/sessions');

// 吊销一条「记住我」会话(服务端校验属主,只能吊销自己的设备)。
export const revokeRememberSession = (data: { tokenId: number }): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/auth/remember/revoke', data);
