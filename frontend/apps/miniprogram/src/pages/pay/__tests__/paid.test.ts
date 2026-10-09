// 支付页的两个边界：
//   1) 支付成功后必须先落「已付款」再回拉详情 —— 详情失败也不能让顾客看到
//      「还没付款」的假象（轮询此时已停，页面会彻底卡住）。
//   2) 「收款成功」后有 800ms 延时跳转，顾客可能抢先手动返回，不能重复导航。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';

async function mount(): Promise<PageInstance> {
  return mountPage('pay', () => import('../index'));
}

describe('支付页', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.currentOrder = { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 1 };
    app.session.config = { shop_name: '长健农场', pay_qr_wx: '/uploads/pay_wx.png' };
    wxMock.reset();
    page = null;
  });

  afterEach(() => {
    // 支付成功会停轮询，但成功路径之外的定时器仍要清掉
    page?.onUnload();
  });

  it('支付成功先落「已付款」，紧随的详情回拉失败也不影响', async () => {
    page = await mount();
    page.onLoad();
    expect(page.data.orderNo).toBe('D001');

    wxMock.reply({ code: 200, msg: 'ok', data: { payStatus: 1 } }); // queryPay 说已付款
    wxMock.replyFail('request:fail'); // 详情回拉失败
    await page.checkPaid(false);
    await flush();

    expect(page.data.paid).toBe(true);
    expect(wxMock.toasts.map(t => t.title)).toContain('支付成功');
  });

  it('离页导航只发一次（延时跳转与手动返回撞车时不重复导航）', async () => {
    page = await mount();
    page.onLoad();

    page.leaveToOrder();
    page.leaveToOrder();

    expect(wxMock.navigations.filter(n => n.type === 'navigateBack')).toHaveLength(1);
  });
});
