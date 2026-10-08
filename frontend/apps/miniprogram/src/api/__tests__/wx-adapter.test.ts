// wx-adapter 的单测：这是整个小程序复用 Web 接口层的唯一接缝，也是最容易出错的地方。
//
// 分两层：
//   1) 直接测 wxAdapter —— 报文组装（URL / params / header / method / timeout）与
//      状态码判定（validateStatus、AxiosError 的 code 与 response 是否带上）
//   2) 走真实 axios 实例（src/api/client）跑一遍 —— 验证「响应拦截器解包 {code,msg,data}」
//      与「业务错误码 → 中文提示」在 wx.request 之上同样成立
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AxiosError } from 'axios';
import type { InternalAxiosRequestConfig } from 'axios';
import { wxAdapter } from '../wx-adapter';
import { API_BASE_URL } from '../../config';
import { flush, wxMock } from '../../../test/mock-wx';

/**
 * 断言请求打到了「配置的站点源 + 指定路径」。
 * 刻意不写死域名：`src/config.ts` 的 API_ORIGIN 是文档要求按环境修改的值
 *（本地联调会指向 127.0.0.1），写死会让「改配置」直接打红单测。
 * 两条断言合起来才非平凡：既要绝对地址、又要落在配置的站点源下。
 */
function expectUrl(url: string | undefined, path: string): void {
  expect(url).toMatch(/^https?:\/\/[^/]+\/api\//);
  expect(url).toBe(`${API_BASE_URL}${path}`);
}

/** 构造一份最小可用的 axios 内部配置（baseURL 只作固定夹具，与 config.ts 解耦）。 */
function cfg(over: Partial<InternalAxiosRequestConfig> = {}): InternalAxiosRequestConfig {
  return {
    url: '/customer/menu',
    method: 'get',
    headers: {},
    baseURL: 'https://dining.example.com/api',
    validateStatus: (s: number) => s >= 200 && s < 300,
    ...over
  } as InternalAxiosRequestConfig;
}

describe('wxAdapter · 报文组装', () => {
  beforeEach(() => wxMock.reset());

  it('拼出 baseURL + url，并大写 method、透传 timeout', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: [] });
    await wxAdapter(cfg({ url: '/customer/table/K7M3PQ9X', method: 'get', timeout: 15000 }));

    expect(wxMock.requests).toHaveLength(1);
    expect(wxMock.lastRequest?.url).toBe('https://dining.example.com/api/customer/table/K7M3PQ9X');
    expect(wxMock.lastRequest?.method).toBe('GET');
    expect(wxMock.lastRequest?.timeout).toBe(15000);
  });

  it('绝对 URL 不再拼 baseURL', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: null });
    await wxAdapter(cfg({ url: 'https://other.example.com/x' }));
    expect(wxMock.lastRequest?.url).toBe('https://other.example.com/x');
  });

  it('params 序列化：编码特殊字符，跳过 undefined / null / 空串', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: null });
    await wxAdapter(
      cfg({
        url: '/customer/pay/query',
        params: { orderNo: 'D2026 1008/1', empty: '', nil: null, undef: undefined, n: 0, no: false }
      } as Partial<InternalAxiosRequestConfig>)
    );
    const url = wxMock.lastRequest?.url ?? '';
    expect(url).toContain('orderNo=D2026%201008%2F1');
    expect(url).toContain('n=0');
    expect(url).toContain('no=false');
    expect(url).not.toContain('empty=');
    expect(url).not.toContain('nil=');
    expect(url).not.toContain('undef=');
  });

  it('已带 query 的 url 用 & 追加', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: null });
    await wxAdapter(cfg({ url: '/x?a=1', params: { b: 2 } } as Partial<InternalAxiosRequestConfig>));
    expect(wxMock.lastRequest?.url).toBe('https://dining.example.com/api/x?a=1&b=2');
  });

  it('对象请求体在没有 content-type 时补 application/json', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await wxAdapter(cfg({ url: '/customer/order', method: 'post', data: { tableId: 7 } }));
    expect(wxMock.lastRequest?.header?.['content-type']).toBe('application/json');
    // 请求体保持对象，交给 wx.request 自己按 content-type 序列化
    expect(wxMock.lastRequest?.data).toEqual({ tableId: 7 });
  });

  it('已有 content-type 时不覆盖（大小写不敏感）', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await wxAdapter(cfg({ method: 'post', data: { a: 1 }, headers: { 'Content-Type': 'application/json' } as never }));
    const header = wxMock.lastRequest?.header ?? {};
    expect(header['Content-Type']).toBe('application/json');
    expect(header['content-type']).toBeUndefined();
  });

  it('AxiosHeaders（有 toJSON）能被归一化，且丢掉 null/undefined 值', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    const headers = {
      toJSON: () => ({ Accept: 'application/json', 'X-Empty': null, 'X-Missing': undefined })
    };
    await wxAdapter(cfg({ headers: headers as never }));
    expect(wxMock.lastRequest?.header).toEqual({ Accept: 'application/json' });
  });

  it('GET 无请求体时不补 content-type（避免给读请求带上无意义头）', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await wxAdapter(cfg());
    expect(wxMock.lastRequest?.header?.['content-type']).toBeUndefined();
  });
});

describe('wxAdapter · 状态码判定', () => {
  beforeEach(() => wxMock.reset());

  it('2xx 解析成功，并把 wx 的 data/statusCode/header 映射到 AxiosResponse', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: { a: 1 } }, 200);
    const res = await wxAdapter(cfg());
    expect(res.status).toBe(200);
    expect(res.data).toEqual({ code: 200, msg: 'ok', data: { a: 1 } });
  });

  it('4xx 拒绝，且错误对象上带着 response（催菜冷却读 data.data.cooldown 依赖它）', async () => {
    wxMock.reply({ code: 400, msg: '催菜太频繁', data: { cooldown: 96 } }, 400);
    await expect(wxAdapter(cfg())).rejects.toMatchObject({
      code: AxiosError.ERR_BAD_REQUEST,
      response: { status: 400, data: { code: 400, msg: '催菜太频繁', data: { cooldown: 96 } } }
    });
  });

  it('5xx 用 ERR_BAD_RESPONSE', async () => {
    wxMock.reply({ code: 500, msg: '炸了' }, 500);
    await expect(wxAdapter(cfg())).rejects.toMatchObject({ code: AxiosError.ERR_BAD_RESPONSE });
  });

  it('自定义 validateStatus 可以把 404 当成功', async () => {
    wxMock.reply({}, 404);
    const res = await wxAdapter(cfg({ validateStatus: s => s === 404 } as Partial<InternalAxiosRequestConfig>));
    expect(res.status).toBe(404);
  });

  it('validateStatus 缺失时一律放行（真实链路上 axios 一定会提供它，这里只是明确兜底语义）', async () => {
    wxMock.reply({ code: 500 }, 500);
    const res = await wxAdapter(cfg({ validateStatus: undefined } as Partial<InternalAxiosRequestConfig>));
    expect(res.status).toBe(500);
  });

  it('网络层失败 → ERR_NETWORK，且把 wx 的 errMsg 作为文案', async () => {
    wxMock.replyFail('request:fail url not in domain list');
    await expect(wxAdapter(cfg())).rejects.toMatchObject({
      code: AxiosError.ERR_NETWORK,
      message: 'request:fail url not in domain list'
    });
  });
});

describe('装配后的真实 axios 实例（经 wx.request）', () => {
  beforeEach(() => wxMock.reset());

  it('响应拦截器解包 {code,msg,data}，业务成功直接拿到 data', async () => {
    const { getMenu } = await import('../client');
    wxMock.reply({ code: 200, msg: 'ok', data: [{ categoryId: 1, categoryName: '招牌' }] });

    const menu = await getMenu();
    expect(menu).toEqual([{ categoryId: 1, categoryName: '招牌' }]);
    expectUrl(wxMock.lastRequest?.url, '/customer/menu');
  });

  it('业务错误码 → 抛错 + 弹全局提示（文案用后端 msg）', async () => {
    const { createOrder } = await import('../client');
    wxMock.reply({ code: 400, msg: '该桌台已有未结账的订单' });

    await expect(createOrder({ tableId: 7, items: [] })).rejects.toThrow('该桌台已有未结账的订单');
    expect(wxMock.lastToast).toBe('该桌台已有未结账的订单');
  });

  it('silent 配置下不弹提示，但错误照常抛出', async () => {
    const { urgeOrder } = await import('../client');
    wxMock.reply({ code: 400, msg: '催菜太频繁', data: { cooldown: 96 } }, 400);

    await expect(urgeOrder('D001', 7, { silent: true })).rejects.toMatchObject({
      response: { data: { data: { cooldown: 96 } } }
    });
    expect(wxMock.toasts).toHaveLength(0);
  });

  it('401 会走 setUnauthorizedHandler 注入的策略', async () => {
    const { setUnauthorizedHandler } = await import('@dining/api');
    const onUnauthorized = vi.fn();
    setUnauthorizedHandler(onUnauthorized);
    try {
      wxMock.reply({ code: 401, msg: '登录状态已过期' }, 401);
      const { getPublicConfig } = await import('../client');
      await expect(getPublicConfig()).rejects.toBeTruthy();
      await flush();
      expect(onUnauthorized).toHaveBeenCalledTimes(1);
    } finally {
      // 还原成默认策略，避免影响同文件后续用例
      setUnauthorizedHandler(() => {});
    }
  });
});
