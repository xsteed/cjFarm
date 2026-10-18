// 订单页 · 修改就餐人数。
//
// 人数决定茶位费，改完金额就变，所以三条边界最要紧：
//   ・只有「进行中且未支付」才给入口（已支付改金额会对不上实收）；
//   ・金额一律用后端返回的订单刷新，前端不自己算茶位费；
//   ・非法输入 / 取消 / 老基础库拿不到输入内容时，一个请求都不许发。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';

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

/** 等两轮微任务：showModal 的回调 → submitPerson 里的请求回包。 */
async function settle(): Promise<void> {
  await flush();
  await flush();
  await flush();
}

describe('订单页 · 修改人数', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    wxMock.reset();
    // 只造实例，不跑 onLoad（脚手架不会自动调生命周期）：本用例里的请求全靠
    // 各测试自己 reply，避免预置回包被别的请求错取。
    page = await mountPage('order', () => import('../../../src/pages/order/index'));
    page.onUnload();
  });

  afterEach(() => {
    page?.clearUrgeTimer();
    page?.onUnload();
    page = null;
  });

  it('只有「进行中且未支付」才给入口', () => {
    page?.render(order({ orderStatus: 1, payStatus: 0 }));
    expect(page?.data.canEditPerson).toBe(true);

    page?.render(order({ orderStatus: 2, payStatus: 1 })); // 已支付
    expect(page?.data.canEditPerson).toBe(false);

    page?.render(order({ orderStatus: 4, payStatus: 0 })); // 已完成
    expect(page?.data.canEditPerson).toBe(false);

    page?.render(order({ orderStatus: 5, payStatus: 0 })); // 已取消
    expect(page?.data.canEditPerson).toBe(false);
  });

  it('确认后提交人数，并用后端返回的订单刷新金额（茶位费不在前端算）', async () => {
    const app = await bootApp();
    page?.render(order({ personCount: 2 }));
    wxMock.modalInput = '5';
    wxMock.modalConfirm = true;
    wxMock.reply({
      code: 200,
      msg: 'ok',
      data: order({ personCount: 5, seatFee: 10, totalAmount: 38 })
    });

    page?.onEditPerson();
    await settle();

    expect(wxMock.lastRequest?.url).toContain('/customer/order/person');
    expect(wxMock.lastRequest?.data).toMatchObject({ tableId: 7, orderNo: 'D001', personCount: 5 });
    expect(page?.data.personCount).toBe(5);
    expect(page?.data.totalText).toBe('38.00');
    expect(page?.data.seatFeeText).toBe('10.00');
    // 会话人数同步：购物车页/菜单页的茶位费都读它，不同步会出现两个价
    expect(app.session.personCount).toBe(5);
    expect(wxMock.lastToast).toContain('5');
  });

  it('输入不是 1~20 的整数：只提示，不发请求', async () => {
    page?.render(order({ personCount: 2 }));
    for (const bad of ['0', '21', 'abc', '-1']) {
      wxMock.modalInput = bad;
      page?.onEditPerson();
      await settle();
      expect(wxMock.requests.filter(r => r.url.includes('/order/person'))).toHaveLength(0);
    }
    expect(wxMock.lastToast).toContain('1~20');
  });

  it('取消、或老基础库不回 content：不发请求也不改人数', async () => {
    page?.render(order({ personCount: 2 }));

    wxMock.modalConfirm = false;
    wxMock.modalInput = '5';
    page?.onEditPerson();
    await settle();
    expect(wxMock.requests.filter(r => r.url.includes('/order/person'))).toHaveLength(0);

    wxMock.modalConfirm = true;
    wxMock.modalInput = undefined; // 旧基础库：editable 模式不返回 content
    page?.onEditPerson();
    await settle();
    expect(wxMock.requests.filter(r => r.url.includes('/order/person'))).toHaveLength(0);
    expect(page?.data.personCount).toBe(2);
  });

  it('人数没变：不发请求', async () => {
    page?.render(order({ personCount: 3 }));
    wxMock.modalInput = '3';
    page?.onEditPerson();
    await settle();
    expect(wxMock.requests.filter(r => r.url.includes('/order/person'))).toHaveLength(0);
  });

  it('提交中再次点击不重复请求', async () => {
    page?.render(order({ personCount: 2 }));
    wxMock.modalInput = '4';

    page?.onEditPerson();
    // 请求还没回包时再点一次
    page?.onEditPerson();
    await settle();

    expect(wxMock.requests.filter(r => r.url.includes('/order/person'))).toHaveLength(1);
  });

  it('失败时保留原人数，不写入错误值', async () => {
    page?.render(order({ personCount: 2 }));
    wxMock.modalInput = '6';
    wxMock.reply({ code: 400, msg: '订单已支付，不可修改人数', data: null });

    page?.onEditPerson();
    await settle();

    expect(page?.data.personCount).toBe(2);
    expect(page?.data.updatingPerson).toBe(false);
  });
});
