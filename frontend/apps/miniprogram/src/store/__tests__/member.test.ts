// 会员登录态的单测。
//
// 重点不在「函数能跑」，而在三条不变式：
//   ・匿名链路不受影响 —— 未登录时任何请求都不带 Authorization 头；
//   ・令牌过期静默自愈 —— 401 → 重登一次 → 重放，顾客无感知；
//   ・令牌全局可见 —— 存本地存储、状态在 App 实例上（跨页共享）。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { CUSTOMER_TOKEN_KEY } from '../../config';
import {
  currentCustomerToken,
  memberLogin,
  memberLogout,
  memberProfile,
  memberState,
  restoreMember
} from '../member';

const MEMBER = {
  customerId: 7,
  nickname: '微信用户',
  avatarUrl: '',
  memberSince: '2026-10-08',
  orderCount: 0
};

function replyLoginOK(token = 'tok-1'): void {
  wxMock.reply({ code: 200, data: { token, isNew: true, customer: MEMBER } });
}

describe('会员登录态', () => {
  beforeEach(async () => {
    await bootApp();
  });

  it('一键注册：wx.login 的 code 换令牌，令牌落本地存储、档案进全局状态', async () => {
    replyLoginOK();
    const res = await memberLogin();
    await flush();

    expect(res.isNew).toBe(true);
    expect(res.info.customerId).toBe(7);
    // 登录请求体带 wx.login 的 code
    const loginReq = wxMock.requests.find(r => r.url.includes('/customer/auth/login'));
    expect(loginReq?.data).toEqual({ code: 'code-abc' });
    // 令牌持久化（冷启动恢复用）
    expect(wxMock.storage.get(CUSTOMER_TOKEN_KEY)).toBe('tok-1');
    // 状态进 App 实例（跨页共享）
    expect(memberState().customerId).toBe(7);
    expect(memberState().info?.nickname).toBe('微信用户');
  });

  it('登录后的请求自动带 Authorization 头', async () => {
    replyLoginOK();
    await memberLogin();
    await flush();

    wxMock.reply({ code: 200, data: MEMBER });
    await memberProfile();
    await flush();

    expect(wxMock.lastRequest?.url).toContain('/customer/auth/profile');
    expect(wxMock.lastRequest?.header?.Authorization).toBe('Bearer tok-1');
  });

  it('未登录时请求不带 Authorization（匿名链路不受影响）', async () => {
    expect(currentCustomerToken()).toBe('');
    wxMock.reply({ code: 200, data: MEMBER });
    await memberProfile().catch(() => undefined);
    await flush();
    // 未登录会先触发静默登录；这里断言「第一次请求」没有令牌头
    const first = wxMock.requests[0];
    expect(first?.header?.Authorization).toBeUndefined();
  });

  it('wx.login 失败时不落任何令牌', async () => {
    wxMock.loginCode = '';
    await expect(memberLogin()).rejects.toThrow('微信登录失败');
    expect(currentCustomerToken()).toBe('');
    expect(memberState().customerId).toBe(0);
    expect(wxMock.requests).toHaveLength(0);
  });

  it('后端拒绝（会员功能未开启）时把文案透出', async () => {
    wxMock.reply({ code: 400, msg: '会员功能未开启' }, 400);
    await expect(memberLogin()).rejects.toThrow('会员功能未开启');
    expect(currentCustomerToken()).toBe('');
  });

  it('退出登录清掉令牌与档案，后续请求不再带头', async () => {
    replyLoginOK();
    await memberLogin();
    await flush();

    memberLogout();
    expect(currentCustomerToken()).toBe('');
    expect(memberState().customerId).toBe(0);

    wxMock.reply({ code: 200, data: MEMBER });
    await memberProfile().catch(() => undefined);
    await flush();
    expect(wxMock.lastRequest?.header?.Authorization).toBeUndefined();
  });

  it('冷启动恢复：本地有令牌时 restoreMember 置为「档案待补全」', async () => {
    wxMock.storage.set(CUSTOMER_TOKEN_KEY, 'cold-token');
    restoreMember();
    expect(currentCustomerToken()).toBe('cold-token');
    expect(memberState().customerId).toBe(-1);
  });

  it('令牌过期：401 → 静默重登 → 重放成功，且重放带新令牌', async () => {
    replyLoginOK('old-token');
    await memberLogin();
    await flush();

    // 队列：第一次 profile 401 → 重登成功 → 重放成功
    wxMock.reply({ code: 401, msg: '请先登录' }, 401);
    replyLoginOK('new-token');
    wxMock.reply({ code: 200, data: { ...MEMBER, orderCount: 3 } });

    const info = await memberProfile();
    await flush();

    expect(info.orderCount).toBe(3);
    expect(currentCustomerToken()).toBe('new-token');
    const profileReqs = wxMock.requests.filter(r => r.url.includes('/customer/auth/profile'));
    expect(profileReqs).toHaveLength(2);
    expect(profileReqs[0].header?.Authorization).toBe('Bearer old-token');
    expect(profileReqs[1].header?.Authorization).toBe('Bearer new-token');
    // 中间确有一次重新登录
    expect(wxMock.requests.some(r => r.url.includes('/customer/auth/login'))).toBe(true);
  });
});
