// ============================================================================
// 401 分流回归防护
//
// 管理端与顾客端共用同一个 axios 实例，但它们是**两个身份**：
//   - 顾客令牌只有 2h 有效期，过期是常态（后端 CustomerAuthOptional/Required）；
//   - 若 401 不分路径一律按管理端处置，一次过期的 /customer/* 请求就会清掉
//     admin_token/admin_perms，并把顾客甩到管理端 /login。
// 本文件锁住「按路径分流」这条不变式，避免以后又被合并回同一套处置。
// ============================================================================
import { AxiosError } from 'axios';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AUTH_KEYS, LS_CUSTOMER_TOKEN, LS_PERMS, LS_TOKEN, safeStorage, setUiNotifier } from '@dining/shared';
import { http, setCustomerUnauthorizedHandler, setUnauthorizedHandler } from '../http';

type Rejected = (e: unknown) => Promise<unknown>;

/** 取出响应拦截器的 rejected 分支（axios 内部结构，仅测试使用）。 */
function rejectedOf(): Rejected {
  const manager = http.interceptors.response as unknown as {
    handlers: Array<{ rejected: Rejected }>;
  };
  return manager.handlers[0].rejected;
}

/** 构造带响应体的 AxiosError。 */
function httpError(url: string, status: number, msg: string): AxiosError {
  const config = { url, headers: {} } as never;
  const response = { status, statusText: String(status), headers: {}, config, data: { msg } } as never;
  return new AxiosError(
    `Request failed with status code ${status}`,
    AxiosError.ERR_BAD_REQUEST,
    config,
    {},
    response
  );
}

describe('HTTP 401 按身份分流', () => {
  const adminUnauthorized = vi.fn();
  const customerUnauthorized = vi.fn();

  beforeEach(() => {
    adminUnauthorized.mockReset();
    customerUnauthorized.mockReset();
    setUnauthorizedHandler(adminUnauthorized);
    setCustomerUnauthorizedHandler(customerUnauthorized);
    // 静默全局 toast（默认实现会打 console.error），不影响断言
    setUiNotifier({ error: () => {}, warning: () => {}, success: () => {} });
    for (const k of [...AUTH_KEYS, LS_CUSTOMER_TOKEN]) safeStorage.remove(k);
  });

  it('顾客端 401：只清顾客令牌，不碰管理端登录态、不触发管理端跳转', async () => {
    safeStorage.set(LS_TOKEN, 'admin-token');
    safeStorage.set(LS_PERMS, '["order:view"]');
    safeStorage.set(LS_CUSTOMER_TOKEN, 'customer-token');

    await expect(rejectedOf()(httpError('/customer/order/query', 401, '请先登录'))).rejects.toBeTruthy();

    expect(safeStorage.get(LS_CUSTOMER_TOKEN)).toBeNull();
    expect(safeStorage.get(LS_TOKEN)).toBe('admin-token');
    expect(safeStorage.get(LS_PERMS)).toBe('["order:view"]');
    expect(customerUnauthorized).toHaveBeenCalledTimes(1);
    expect(adminUnauthorized).not.toHaveBeenCalled();
  });

  it('管理端 401：清空管理端登录态并跳转，但不牵连顾客令牌', async () => {
    safeStorage.set(LS_TOKEN, 'admin-token');
    safeStorage.set(LS_PERMS, '["order:view"]');
    safeStorage.set(LS_CUSTOMER_TOKEN, 'customer-token');

    await expect(rejectedOf()(httpError('/admin/order/list', 401, '未登录或登录已过期'))).rejects.toBeTruthy();

    expect(safeStorage.get(LS_TOKEN)).toBeNull();
    expect(safeStorage.get(LS_PERMS)).toBeNull();
    // 与 shared/authKeys.ts 的既有约定一致：管理端退出不踢同浏览器的顾客
    expect(safeStorage.get(LS_CUSTOMER_TOKEN)).toBe('customer-token');
    expect(adminUnauthorized).toHaveBeenCalledTimes(1);
    expect(customerUnauthorized).not.toHaveBeenCalled();
  });

  it('非 401 错误不触发任何登录态处置', async () => {
    safeStorage.set(LS_CUSTOMER_TOKEN, 'customer-token');

    await expect(rejectedOf()(httpError('/customer/order/query', 500, '服务繁忙'))).rejects.toBeTruthy();

    expect(safeStorage.get(LS_CUSTOMER_TOKEN)).toBe('customer-token');
    expect(adminUnauthorized).not.toHaveBeenCalled();
    expect(customerUnauthorized).not.toHaveBeenCalled();
  });
});
