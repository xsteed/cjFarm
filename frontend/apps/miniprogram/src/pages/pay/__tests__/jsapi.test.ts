// 小程序内支付（JSAPI）的回归用例。
//
// 链路：wx.login → 后端换 openid + JSAPI 下单 + 签发调起参数 → wx.requestPayment
//      → 查单确认（以服务端为准）。
// 要守住的三件事：
//   ・顾客不会被卡在支付页 —— 未开通/失败时降级提示「用收款码或到前台结账」；
//   ・顾客取消收银台 —— 只提示，不误置「已支付」；
//   ・收银台 success 也要以查单结果为准 —— 回调有延迟，不能谎报支付成功。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { API_BASE_URL } from '../../../config';

async function mount(): Promise<PageInstance> {
  return mountPage('pay', () => import('../index'));
}

const PARAMS = {
  timeStamp: '1700000000',
  nonceStr: 'n1',
  package: 'prepay_id=wx1',
  signType: 'RSA',
  paySign: 'sig'
};

describe('支付页 · 小程序内支付（JSAPI）', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    wxMock.reset();
    page = null;
  });

  afterEach(() => {
    // 支付成功后有 800ms 的延时跳转，onUnload 会把它清掉
    page?.onUnload();
  });

  /** 起一个支付页；jsapiEnabled = 后端是否开通了小程序内支付。 */
  async function boot(jsapiEnabled: boolean): Promise<PageInstance> {
    const app = await bootApp();
    app.session.currentOrder = { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 1 };
    app.session.config = {
      shop_name: '长健农场',
      wxpay_enabled: '1',
      wxpay_jsapi_enabled: jsapiEnabled ? '1' : '0',
      pay_qr_wx: '/uploads/pay_wx.png'
    };
    const p = await mount();
    p.onLoad();
    return p;
  }

  it('后端已开通：wx.login → 下单 → 拉起收银台 → 查单确认后才算支付成功', async () => {
    page = await boot(true);
    expect(page.data.canJsapi).toBe(true);

    wxMock.reply({ code: 200, msg: 'ok', data: PARAMS }); // /customer/pay/jsapi
    wxMock.reply({ code: 200, msg: 'ok', data: { payStatus: 1 } }); // /customer/pay/query
    await page.onPayOnline();
    await flush();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    expect(post?.url).toBe(`${API_BASE_URL}/customer/pay/jsapi`);
    expect(post?.data).toMatchObject({ orderNo: 'D001', code: 'code-abc' });

    expect(wxMock.payments).toHaveLength(1);
    expect(wxMock.payments[0]).toMatchObject({ timeStamp: '1700000000', package: 'prepay_id=wx1' });

    // requestPayment 的 success 之后必须再查一次单：服务端确认收款才置「已支付」
    expect(wxMock.requests.some(r => r.url.includes('/customer/pay/query'))).toBe(true);
    expect(page.data.paid).toBe(true);
    expect(wxMock.toasts.map(t => t.title)).toContain('支付成功');
  });

  it('收银台成功但服务端还没确认到账：不谎报成功，提示「确认中」并继续等轮询', async () => {
    page = await boot(true);

    wxMock.reply({ code: 200, msg: 'ok', data: PARAMS }); // /customer/pay/jsapi
    wxMock.reply({ code: 200, msg: 'ok', data: { payStatus: 0 } }); // 回调还没到
    await page.onPayOnline();
    await flush();
    await flush();

    expect(wxMock.payments).toHaveLength(1);
    expect(page.data.paid).toBe(false);
    expect(wxMock.toasts.map(t => t.title)).not.toContain('支付成功');
    expect(wxMock.lastToast).toContain('确认中');
  });

  it('顾客取消收银台：只提示，不误置「已支付」', async () => {
    page = await boot(true);
    wxMock.paymentResult = 'cancel';
    wxMock.reply({ code: 200, msg: 'ok', data: PARAMS });

    await page.onPayOnline();
    await flush();

    expect(page.data.paid).toBe(false);
    expect(wxMock.lastToast).toBe('支付已取消');
  });

  it('后端未开通 / 下单失败：降级提示，收款码与前台结账仍可用', async () => {
    page = await boot(true);
    wxMock.reply({ code: 400, msg: '小程序支付未开通' });

    await page.onPayOnline();
    await flush();

    expect(page.data.paid).toBe(false);
    expect(wxMock.payments).toHaveLength(0); // 没拉起收银台
    expect(wxMock.lastToast).toContain('收款码');
  });

  it('wx.login 失败时不拉起收银台（拿不到 code 就没必要下单）', async () => {
    page = await boot(true);
    wxMock.loginCode = '';

    await page.onPayOnline();
    await flush();

    expect(wxMock.payments).toHaveLength(0);
    expect(page.data.paid).toBe(false);
  });

  it('后端未开通小程序内支付时，主按钮走降级路径且收款码照常显示', async () => {
    page = await boot(false);

    expect(page.data.canJsapi).toBe(false);
    expect(page.data.hasQr).toBe(true);
  });
});
