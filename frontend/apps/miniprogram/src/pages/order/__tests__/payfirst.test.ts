// 先付模式（门店在管理端「营业设置」里开启）在订单页的两处收紧：
//   1) 未支付时不出现「催菜」—— 后厨此刻还没收到厨单，催了只会得到「已催」的假反馈，
//      顾客反而以为菜在做；
//   2) 未支付时多一句「付款后才开做」，与底部「去支付」按钮形成因果。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { STORAGE_KEY } from '../../../config';

function order(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    orderNo: 'D001',
    shortNo: '001',
    orderStatus: 1,
    payStatus: 0,
    personCount: 2,
    dishAmount: 28,
    seatFee: 0,
    discountAmount: 0,
    totalAmount: 28,
    items: [],
    ...overrides
  };
}

describe('订单页 · 先付模式', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    wxMock.reset();
    // onLoad 会用本地记下的订单号回拉一次，先备一个回包免得它走错误分支
    wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo: 'D001' });
    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page = await mountPage('order', () => import('../index'));
    await flush();
    page.onUnload();
  });

  afterEach(() => {
    page?.clearUrgeTimer();
    page?.onUnload();
    page = null;
  });

  it('先付 + 未支付：提示「付款后才开做」，并收掉催菜', async () => {
    const app = await bootApp();
    app.session.config = { shop_name: '长健农场', pay_first_enabled: '1' };

    page?.render(order({ payStatus: 0 }));

    expect(page?.data.showPayFirstTip).toBe(true);
    expect(page?.data.canUrge).toBe(false);
    // 「去支付」仍在：提示与按钮指向同一件事，且带火苗给紧迫感
    expect(page?.data.canPay).toBe(true);
    expect(page?.data.payBtnText).toBe('🔥 去支付');
  });

  it('先付 + 已支付：可以催菜，提示收起', async () => {
    const app = await bootApp();
    app.session.config = { shop_name: '长健农场', pay_first_enabled: '1' };

    page?.render(order({ orderStatus: 2, payStatus: 1 }));

    expect(page?.data.showPayFirstTip).toBe(false);
    expect(page?.data.canUrge).toBe(true);
  });

  it('后付（默认）：未支付照样能催菜，不出现先付提示', async () => {
    const app = await bootApp();
    app.session.config = { shop_name: '长健农场', pay_first_enabled: '0' };

    page?.render(order({ payStatus: 0 }));

    expect(page?.data.showPayFirstTip).toBe(false);
    expect(page?.data.canUrge).toBe(true);
    // 后付只是结账，不是「不付就不开工」，按钮不加火苗
    expect(page?.data.payBtnText).toBe('去支付');
  });
});
