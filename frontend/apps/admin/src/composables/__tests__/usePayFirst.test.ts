import { describe, it, expect, beforeEach, vi } from 'vitest';
import { flushPromises } from '@shell/test/mocks.ts';

// @dining/api 真实模块会创建 axios 实例并注册拦截器,这里整体 mock 掉,
// 只保留 usePayFirst 用到的 getPublicConfig。
vi.mock('@dining/api', () => ({
  getPublicConfig: vi.fn()
}));

// usePayFirst 是模块级单例(一次页面生命周期只拉一次配置),每个用例前
// resetModules + 动态 import,拿到干净的模块实例与配套的 api mock。
// 必须在使用前重新 import '@dining/api' 并配置:resetModules 之后静态 import
// 拿到的绑定可能与 usePayFirst 内部看到的不是同一实例。
async function loadUsePayFirst() {
  vi.resetModules();
  const api = await import('@dining/api');
  const getPublicConfig = vi.mocked(api.getPublicConfig);
  getPublicConfig.mockReset();
  const { usePayFirst } = await import('../usePayFirst');
  return { usePayFirst, getPublicConfig };
}

describe('usePayFirst 先付模式开关', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it("pay_first_enabled='1' 时拉取后为 true", async () => {
    const { usePayFirst, getPublicConfig } = await loadUsePayFirst();
    getPublicConfig.mockResolvedValue({ pay_first_enabled: '1' });
    // 请求在途时先按后付渲染:按钮显示「结算」(改动前的既有行为),不错配
    expect(usePayFirst().value).toBe(false);
    await flushPromises();
    expect(usePayFirst().value).toBe(true);
  });

  it("pay_first_enabled='0' 或缺省时为 false", async () => {
    const { usePayFirst, getPublicConfig } = await loadUsePayFirst();
    getPublicConfig.mockResolvedValue({ pay_first_enabled: '0' });
    await flushPromises();
    expect(usePayFirst().value).toBe(false);

    const second = await loadUsePayFirst();
    second.getPublicConfig.mockResolvedValue({});
    await flushPromises();
    expect(second.usePayFirst().value).toBe(false);
  });

  it('请求失败时保持 false(安全默认,不把后付门店错配成先付)', async () => {
    const { usePayFirst, getPublicConfig } = await loadUsePayFirst();
    getPublicConfig.mockRejectedValue(new Error('network down'));
    await flushPromises();
    expect(usePayFirst().value).toBe(false);
  });

  it('同一模块内多次调用只发一次请求', async () => {
    const { usePayFirst, getPublicConfig } = await loadUsePayFirst();
    getPublicConfig.mockResolvedValue({ pay_first_enabled: '1' });
    usePayFirst();
    usePayFirst();
    await flushPromises();
    expect(getPublicConfig).toHaveBeenCalledTimes(1);
    expect(usePayFirst().value).toBe(true);
  });
});
