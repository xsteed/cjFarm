// 顾客会员登录态（小程序「注册成为会员」）。
//
// 设计与令牌策略见 docs/design-docs/customer-member/spec.md：
//   ・登录是「自愿」的 —— 未登录顾客的点餐链路完全不经过本模块；
//   ・令牌 2h 有效，过期/401 时**静默重登一次**（wx.login 免费无感），顾客无感知；
//   ・注册动作 = 点按钮 → wx.login（静默）→ POST /customer/auth/login，免表单免密码。
//
// 状态本体挂在 App 实例上（跨页共享的原因见 src/types.ts 顶部注释）；
// 令牌另存本地存储，冷启动后恢复，避免「每次进小程序都要重新注册」的错觉。
//
// ⚠️ 这里直接 import '@dining/api' 而不是 '../api/client' 是有意的：
// 本模块要注册 401 策略，被 api/client.ts 反过来引入；若再从 client 导入会形成环。
// 模块加载期不做任何请求（只定义函数），因此加载顺序不影响正确性。
import { http, setUnauthorizedHandler } from '@dining/api';
import { CUSTOMER_TOKEN_KEY } from '../config';
import type { GlobalData, MemberInfo, MemberOrderRow, MemberState } from '../types';

/** 取全局唯一的会员登录态（可读可写，写入后调用方自行 setData）。 */
export function memberState(): MemberState {
  return getApp<GlobalData>().member;
}

/** 当前顾客令牌（未登录为空串）。供 api/client.ts 的请求拦截器读取。 */
export function currentCustomerToken(): string {
  try {
    return String(wx.getStorageSync(CUSTOMER_TOKEN_KEY) || '');
  } catch {
    return '';
  }
}

function saveToken(token: string): void {
  try {
    if (token) wx.setStorageSync(CUSTOMER_TOKEN_KEY, token);
    else wx.removeStorageSync(CUSTOMER_TOKEN_KEY);
  } catch {
    /* 存储不可用（磁盘满/隐私限制）：会员态退化为「本次会话内有效」，静默降级 */
  }
}

function clearMember(): void {
  const s = memberState();
  s.customerId = 0;
  s.info = null;
  s.pending = false;
  saveToken('');
}

/** 冷启动恢复：把本地令牌还原进 App 实例（档案信息由「我的」页按需拉取）。 */
export function restoreMember(): void {
  if (currentCustomerToken()) memberState().customerId = -1; // 有令牌但档案未知，等首次接口调用补全
}

/**
 * 微信一键注册/登录（「注册成为会员」按钮的落地动作）。
 * 全程无授权弹窗：wx.login 是静默的。isNew 区分「🎉 注册成功」与「欢迎回来」。
 */
let loginInflight: Promise<{ info: MemberInfo; isNew: boolean }> | null = null;

export function memberLogin(): Promise<{ info: MemberInfo; isNew: boolean }> {
  // **单飞（single-flight）**：并发调用（用户双击、401 兜底与请求包装同时触发重登）
  // 共用同一次登录。没有这个约定，「请求包装里的重登」会撞上「401 兜底已先开始的
  // 重登」，顾客会看到「正在登录，请稍候」的误报（单测抓到过）。
  if (loginInflight) return loginInflight;
  const s = memberState();
  s.pending = true;
  loginInflight = (async () => {
    const code = await new Promise<string>((resolve, reject) => {
      wx.login({
        success: r => resolve(r.code),
        fail: () => reject(new Error('微信登录失败，请重试'))
      });
    });
    const res = await http.post<unknown, { token: string; isNew: boolean; customer: MemberInfo }>(
      '/customer/auth/login',
      { code }
    );
    saveToken(String(res.token || ''));
    s.customerId = Number(res.customer?.customerId) || 0;
    s.info = res.customer || null;
    return { info: s.info, isNew: !!res.isNew };
  })().finally(() => {
    loginInflight = null;
    s.pending = false;
  });
  return loginInflight;
}

/**
 * 确保已登录（会员专属接口调用前使用）：已有令牌直接放行，否则静默登录。
 * 失败时抛出的错误文案可直接展示（来自后端或 wx.login 的失败）。
 */
export async function memberEnsureLogin(): Promise<void> {
  if (currentCustomerToken()) return;
  await memberLogin();
}

/** 退出登录：清本地令牌与档案。令牌无状态（2h 自动过期），后端无需接口。 */
export function memberLogout(): void {
  clearMember();
}

/**
 * 会员专属请求的包装：先确保登录，遇 401（令牌过期）静默重登一次并重放。
 * 重放仍 401 才把错误抛给调用方（此时后端文案已能解释原因）。
 */
async function memberedGet<T>(url: string, params?: Record<string, unknown>): Promise<T> {
  // 响应拦截器已把 {code,msg,data} 解包,运行时拿到的是业务数据;
  // axios 的类型在此处与泛型 T 推导不合,与 http.ts 同样用断言桥接。
  const once = (): Promise<T> => http.get(url, { params }) as unknown as Promise<T>;
  try {
    await memberEnsureLogin();
    return await once();
  } catch (e) {
    const status = (e as { response?: { status?: number } })?.response?.status;
    if (status !== 401) throw e;
    saveToken('');
    await memberLogin();
    return await once();
  }
}

/** 拉取会员卡信息（登录后档案以服务端为准）。 */
export async function memberProfile(): Promise<MemberInfo> {
  const info = await memberedGet<MemberInfo>('/customer/auth/profile');
  const s = memberState();
  s.info = info;
  s.customerId = Number(info.customerId) || s.customerId;
  return info;
}

/** 我的订单（跨桌历史订单，分页）。 */
export async function memberOrders(
  pageNum: number,
  pageSize = 10
): Promise<{ total: number; items: MemberOrderRow[] }> {
  return memberedGet<{ total: number; items: MemberOrderRow[] }>('/customer/order/mine', {
    pageNum,
    pageSize
  });
}

/**
 * 会员功能是否开启（公开配置下发；未开启时「我的」页隐藏登录入口，只留说明）。
 * 配置还没拉到时按「开启」处理：误点了也会拿到后端的明确文案，不会误伤真开店的场景。
 */
export function memberEnabled(): boolean {
  return String(getApp<GlobalData>().session.config?.member_enabled ?? '1') !== '0';
}

/**
 * 静默重登（api/client.ts 的 401 兜底调用）：
 * 只在有令牌（说明是会员，只是过期了）时尝试，且失败不打扰用户 ——
 * 顾客真正的诉求（点餐）不依赖登录态，静默失败等下次进「我的」页再试即可。
 */
export function memberSilentRelogin(): void {
  if (!currentCustomerToken()) return;
  void memberLogin().catch(() => {
    /* 静默失败：下次需要会员数据时再走完整登录 */
  });
}

// 401 策略注册。被 client.ts 在装配完成后调用（而不是模块加载期注册），
// 保证注册时提示/传输层都已就绪。
export function installMemberUnauthorizedHandler(): void {
  setUnauthorizedHandler(() => {
    memberSilentRelogin();
  });
}
