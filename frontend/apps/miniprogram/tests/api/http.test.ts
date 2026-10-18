// 接口传输层（wx.request 封装）的单测。
//
// 这层是所有接口的必经之路，语义钉住八件事：
//   1) 业务码解包：code=200 交出 data，仅消息结果原样返回；
//   2) 业务失败转 ApiError：中文文案 + 保留原始响应体（催菜冷却读 err.response.data.data）；
//   3) silent 请求不弹全局错误提示；
//   4) HTTP 错误：业务 msg 优先，无 msg 走状态码映射；
//   5) 网络失败 / 超时给出可读文案；
//   6) 401 触发注册的处理器（会员模块的静默重登挂在这里）；
//   7) /customer/ 路径自动带顾客令牌，其余路径不带；
//   8) GET 查询参数拼进 URL，空值丢弃。
import { beforeEach, describe, expect, it } from 'vitest';
import { ApiError, configureHttp, httpGet, httpPost, setUnauthorizedHandler } from '../../src/api/http';
import { setUiNotifier } from '../../src/shared/ui';
import { wxMock } from '../../test/mock-wx';

const notified: string[] = [];
let unauthorizedCount = 0;

beforeEach(() => {
  wxMock.reset();
  wxMock.storage.clear();
  notified.length = 0;
  unauthorizedCount = 0;
  configureHttp({ baseURL: 'https://api.test/api', timeoutMs: 5000 });
  // 提示与 401 都用本文件自己的记录器：断言「有没有提示」而不是依赖 toast 实现
  setUiNotifier({
    success: m => notified.push(`s:${m}`),
    warning: m => notified.push(`w:${m}`),
    error: m => notified.push(`e:${m}`)
  });
  setUnauthorizedHandler(() => {
    unauthorizedCount += 1;
  });
});

async function rejection(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (e) {
    return e as ApiError;
  }
  throw new Error('应当 reject 但 resolve 了');
}

describe('接口传输层 · 解包与错误', () => {
  it('code=200 交出 data；仅消息结果（无 data 字段）原样返回', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: { menu: 1 } });
    await expect(httpGet('/customer/menu')).resolves.toEqual({ menu: 1 });

    wxMock.reply({ code: 200, msg: '保存成功' });
    await expect(httpPost('/customer/remarks')).resolves.toEqual({ code: 200, msg: '保存成功' });
  });

  it('业务失败转 ApiError：中文文案 + 保留响应体（催菜冷却读 err.response.data.data）', async () => {
    wxMock.reply({ code: 400, msg: '操作过于频繁', data: { cooldown: 96 } });

    const err = await rejection(httpPost('/customer/order/urge', { orderNo: 'D1', tableId: 3 }));

    expect(err.friendlyMessage).toBe('操作过于频繁');
    expect(err.status).toBe(200);
    expect((err.response?.data as { data?: { cooldown?: number } })?.data?.cooldown).toBe(96);
    // 非 silent：弹了全局错误提示
    expect(notified).toEqual(['e:操作过于频繁']);
  });

  it('silent 请求失败时不弹全局错误提示（埋点 / 冷却由调用方自行处置）', async () => {
    wxMock.reply({ code: 400, msg: '操作过于频繁' });

    const err = await rejection(httpPost('/customer/order/urge', {}, { silent: true }));

    expect(err.friendlyMessage).toBe('操作过于频繁');
    expect(notified).toEqual([]);
  });

  it('HTTP 错误：业务 msg 优先；无 msg 走状态码映射', async () => {
    wxMock.reply({ code: 500, msg: '库存不足' }, 500);
    const withMsg = await rejection(httpGet('/customer/menu'));
    expect(withMsg.friendlyMessage).toBe('库存不足');

    // 网关错误页等拿不到业务 msg 的场景：走状态码映射，不把英文技术文案抛给用户
    wxMock.reply({}, 502);
    const mapped = await rejection(httpGet('/customer/menu'));
    expect(mapped.friendlyMessage).toBe('服务正在重启，请稍后重试');
  });

  it('网络失败与超时给出可读文案', async () => {
    wxMock.replyFail('request:fail');
    const net = await rejection(httpGet('/customer/menu'));
    expect(net.friendlyMessage).toBe('网络连接失败，请检查网络后重试');

    wxMock.replyFail('request:fail timeout');
    const timeout = await rejection(httpGet('/customer/menu'));
    expect(timeout.friendlyMessage).toBe('请求超时，请检查网络后重试');
  });

  it('401 触发注册的处理器（会员模块的静默重登挂在这里）', async () => {
    wxMock.reply({ code: 200, msg: 'ok' }, 401);

    const err = await rejection(httpGet('/customer/auth/profile'));

    expect(unauthorizedCount).toBe(1);
    expect(err.friendlyMessage).toBe('登录状态已过期，请重新登录');
  });
});

describe('接口传输层 · 令牌与 URL', () => {
  it('/customer/ 路径自动携带顾客令牌；其余路径不带', async () => {
    wxMock.storage.set('dining_customer_token', 'tok-1');

    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await httpGet('/customer/menu');
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await httpGet('/other/endpoint');

    const [customer, other] = wxMock.requests;
    expect(customer.header?.Authorization).toBe('Bearer tok-1');
    expect(other.header?.Authorization).toBeUndefined();
  });

  it('GET 查询参数拼进 URL：空值丢弃、做了编码', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await httpGet('/customer/order/mine', { pageNum: 2, pageSize: 10, empty: '', skip: undefined });

    expect(wxMock.requests[0].url).toContain('/customer/order/mine?');
    expect(wxMock.requests[0].url).toContain('pageNum=2');
    expect(wxMock.requests[0].url).toContain('pageSize=10');
    expect(wxMock.requests[0].url).not.toContain('empty=');
    expect(wxMock.requests[0].url).not.toContain('skip=');
  });
});
