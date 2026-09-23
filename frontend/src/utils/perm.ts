import { ref } from 'vue';
import { getProfile } from '../api';
import { LS_TOKEN, LS_USER, LS_REALNAME, LS_ROLE, LS_ROLENAME, LS_PERMS, AUTH_KEYS } from './authKeys';
import { safeStorage } from './safeStorage';
import type { LoginResult, Profile } from '../types/entities';

// ============================================================================
// 前端登录态与权限
//
// 权限码的权威定义在后端(internal/store/permission.go),前端只做两件事:
//   1. 把登录/刷新接口返回的 perms 缓存到 localStorage;
//   2. 据此控制「菜单是否显示、按钮是否可点、路由是否放行」。
//
// ⚠️ 前端隐藏按钮只是体验优化,**不是安全边界**。所有接口在后端都由
//    AdminAuth(401) + RequirePerm(403) 强制校验,前端漏改最多是「看到按钮点了报错」,
//    不会出现越权操作。
//
// 一个容易踩的坑:必须区分「未加载(null)」与「已加载但为空([])」。
//   前者要先调接口补载,后者表示该账号确实没有任何权限(应展示无权限提示页)。
//   若把两者当成同一件事,新登录用户会因为「还没拉到权限」而被瞬间判定为无权限。
// ============================================================================

/**
 * 权限集合缓存。null = 尚未加载;Set = 已加载(可能为空集)。
 *
 * 用 ref 而不是普通变量,是为了让组件里的 computed 能追踪到它:
 * 各页面的 `canEdit` / 侧栏菜单都是在 computed 里调 hasPerm() 的,
 * 若底层是普通变量,computed 首次求值后就被永久缓存,
 * refreshProfile() 拿到新权限时不会触发重算 —— 菜单和按钮会停在旧状态。
 */
const permSet = ref<Set<string> | null>(null);

function readJSON(key: string): string[] | null {
  const raw = safeStorage.get(key);
  if (!raw) return null;
  try {
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) ? (v as string[]) : null;
  } catch {
    return null;
  }
}

/** 从 localStorage 恢复权限缓存(页面刷新后调用一次)。 */
export function restorePerms(): void {
  const cached = readJSON(LS_PERMS);
  permSet.value = cached ? new Set(cached) : null;
}

/** 登录成功后写入完整登录态。 */
export function setAuth(data: LoginResult): void {
  const perms = Array.isArray(data?.perms) ? (data.perms ?? []) : [];
  safeStorage.set(LS_TOKEN, data?.token || '');
  safeStorage.set(LS_USER, data?.username || '');
  safeStorage.set(LS_REALNAME, data?.realName || data?.username || '');
  safeStorage.set(LS_ROLE, data?.roleKey || '');
  safeStorage.set(LS_ROLENAME, data?.roleName || '');
  safeStorage.set(LS_PERMS, JSON.stringify(perms));
  permSet.value = new Set(perms);
}

/** 清空登录态(退出登录 / 401 时调用)。 */
export function clearAuth(): void {
  for (const k of AUTH_KEYS) {
    safeStorage.remove(k);
  }
  permSet.value = null;
}

/** 是否已有令牌。 */
export function hasToken(): boolean {
  return !!safeStorage.get(LS_TOKEN);
}

/**
 * 本地令牌是否已过期(乐观预判,不验签)。
 *
 * 令牌格式为 base64url("user|uid|version|exp").sig(见后端 handler/auth.go),
 * 解出第 4 段 exp(Unix 秒)即可在不打后端的情况下识别「显然已过期」的令牌:
 * 路由守卫据此直接去登录页,省掉「进页面 → 发请求撞 401 → 整页刷新弹回」的绕路。
 * 篡改 exp 骗过本判断没有意义 —— 安全校验仍在后端验签,伪造/篡改的令牌照样 401。
 * 解析失败一律视为已过期,交给后端兜底。
 */
export function tokenExpired(): boolean {
  const t = safeStorage.get(LS_TOKEN);
  if (!t) return true;
  try {
    // RawURLEncoding 无 padding,atob 要求长度为 4 的倍数,需补 '='。
    // 用户名含中文也没关系:'|'(0x7C) 不会出现在 UTF-8 多字节序列(字节均 >= 0x80)里,
    // atob 的逐字节输出不影响按 '|' 定位第 4 段。
    const raw = t.split('.')[0].replace(/-/g, '+').replace(/_/g, '/');
    const b64 = raw + '='.repeat((4 - (raw.length % 4)) % 4);
    const exp = Number(atob(b64).split('|')[3]);
    return !Number.isFinite(exp) || exp * 1000 <= Date.now();
  } catch {
    return true;
  }
}

/** 权限是否已加载(区分 null 与空集)。 */
export function permsLoaded(): boolean {
  return permSet.value !== null;
}

/** 是否拥有某个权限码。未加载时一律返回 false(等补载后再判定)。 */
export function hasPerm(code: string): boolean {
  if (!code) return true;
  if (!permSet.value) return false;
  return permSet.value.has(code);
}

/** 是否拥有其中任意一个权限码(用于「菜单是否显示」这类聚合判断)。 */
export function hasAnyPerm(codes: string[]): boolean {
  if (!codes || !codes.length) return true;
  return codes.some(c => hasPerm(c));
}

export function getUsername(): string {
  return safeStorage.get(LS_USER) || '';
}

/** 顶栏展示名:优先中文姓名。 */
export function getDisplayName(): string {
  return safeStorage.get(LS_REALNAME) || getUsername() || '未登录';
}

export function getRoleName(): string {
  return safeStorage.get(LS_ROLENAME) || '';
}

export function getRoleKey(): string {
  return safeStorage.get(LS_ROLE) || '';
}

/**
 * 重新拉取自己的身份与权限并覆盖缓存。
 *
 * 必须在每次「进入管理端」时调用一次:商户在后台改了某人的角色后,
 * 该员工浏览器里缓存的是旧权限,不刷新就会看到不该看到的菜单/按钮。
 * 这里刷新后,前端展示与后端判定立即一致。
 */
export async function refreshProfile(): Promise<Profile> {
  const data = await getProfile();
  const perms = Array.isArray(data?.perms) ? (data.perms ?? []) : [];
  safeStorage.set(LS_USER, data?.username || '');
  safeStorage.set(LS_REALNAME, data?.realName || data?.username || '');
  safeStorage.set(LS_ROLE, data?.roleKey || '');
  safeStorage.set(LS_ROLENAME, data?.roleName || '');
  safeStorage.set(LS_PERMS, JSON.stringify(perms));
  permSet.value = new Set(perms);
  return data;
}

// 页面加载即恢复一次缓存,避免路由守卫首次判定时拿到 null。
restorePerms();
