// 「记住我(免登录)」本地存储
//
// 只存后端签发的不透明令牌(rememberToken),绝不存账号密码。
// 令牌是否过期完全由后端查库裁决(见后端 tb_remember_token.expire_time),
// 前端不做任何本地过期判断 —— 打开页面时拿令牌去 /auth/remember-login 校验,
// 过期/失效后端直接返 401,前端再清掉本地令牌回到登录页。窗口控制权完全在服务端,
// 前端无法伪造或篡改有效期(此前用 localStorage 时间戳本地判断的做法已被取代)。

import { logoutRemember } from '../api';
import { safeStorage } from './safeStorage';

const LS_REMEMBER = 'admin_remember';

export function saveRemember(token: string): void {
  safeStorage.set(LS_REMEMBER, token);
}

export function getRemember(): string | null {
  const t = safeStorage.get(LS_REMEMBER);
  return t && t.length > 0 ? t : null;
}

export function clearRemember(): void {
  safeStorage.remove(LS_REMEMBER);
}

// 吊销本设备的「记住我」令牌:清本地 + 请求后端删除库里那一条(仅这一条,
// 不连坐该账号在其他设备上的令牌)。供「退出登录」与「取消记住」使用。
// 若不吊销,服务端令牌会一直有效到 7/30 天后自然过期 —— 共用电脑场景下,
// 只清本地意味着「退出」止不了损。
// 任何失败都静默:本地已清,最坏结果是服务端残留一条无人持有的旧令牌。
export async function revokeRemember(): Promise<void> {
  const t = getRemember();
  if (!t) return;
  safeStorage.remove(LS_REMEMBER);
  try {
    await logoutRemember(t);
  } catch {
    // 网络失败等:不打扰退出/登录主流程
  }
}
