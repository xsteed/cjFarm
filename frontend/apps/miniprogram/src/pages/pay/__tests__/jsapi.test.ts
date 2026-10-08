// 小程序内支付（JSAPI）的回归用例。
//
// 后端目前走的是 Native 扫码下单（返回 code_url），小程序里不能「自己扫自己」，
// 所以真正的在线支付要另开 JSAPI 通道。前端这一版先把通道就位，并保证后端没开通时：
//   ・顾客不会被卡在支付页 —— 降级提示「用收款码或到前台结账」
//   ・顾客取消收银台 —— 只提示，不误置「已支付」
//   ・拉起收银台的参数原样透传（签名由服务端完成，前端不做任何加工）
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

  it('后端已开通：wx.login → 下单 → 拉起收银台，调起参数原样透传', async () => {
    page = await boot(true);
    expect(page.data.canJsapi).toBe(true);

    wxMock.reply({ code: 200, msg: 'ok', data: PARAMS }); // /customer/pay/jsapi
    await page.onPayOnline();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    expect(post?.url).toBe(`${API_BASE_URL}/customer/pay/jsapi`);
    expect(post?.data).toMatchObject({ orderNo: 'D001', code: 'code-abc' });

    expect(wxMock.payments).toHaveLength(1);
    expect(wxMock.payments[0]).toMatchObject({ timeStamp: '1700000000', package: 'prepay_id=wx1' });

    expect(page.data.paid).toBe(true);
    expect(wxMock.toasts.map(t => t.title)).toContain('支付成功');
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
