// 订单 Tab 的「出餐中订单」列表回归。
//
// 场景：本桌订单已付款、还在出餐（session.currentOrder 不含它们）。此时点底部
// 「订单」tab 应该列出这些单、点一条看进度，而不是甩一句「没有找到进行中的订单」。
// 另一个方向：桌上没有出餐中的单时，维持原有的错误态不变。
//
// 注意：脚手架只造实例、不跑生命周期，onLoad 由用例手动触发（与 person.test 一致）。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage, setCurrentPages } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { STORAGE_KEY } from '../../../config';

function servingOrder(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    orderNo: 'D009',
    shortNo: '009',
    orderStatus: 2,
    payStatus: 1,
    personCount: 5,
    dishAmount: 28,
    seatFee: 10,
    discountAmount: 0,
    totalAmount: 38,
    items: [],
    ...overrides
  };
}

describe('订单页 · 出餐中订单列表', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    app.session.currentOrder = null;
    // 菜单页拉到的桌台详情：本桌有一笔已付款出餐中的订单
    app.session.table = { tableId: 7, tableCode: 'A1', servingOrders: [servingOrder()] };
    wxMock.reset();
    // 没有 session 订单、也没有本地订单号 → 走「列出餐中订单」分支
    wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo: '' });
    page = await mountPage('order', () => import('../index'));
    page.onLoad({});
    await flush();
  });

  afterEach(() => {
    page?.onUnload();
    page = null;
  });

  it('没有进行中订单但有出餐中的单：列出而不是报错', () => {
    expect(page?.data.showServingList).toBe(true);
    expect(page?.data.error).toBe('');
    const rows = page?.data.servingRows as { orderNo: string; label: string }[];
    expect(rows).toHaveLength(1);
    expect(rows[0].orderNo).toBe('D009');
    expect(rows[0].label).toContain('009');
  });

  it('点一条：按单号回拉详情并进入进度视图', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: servingOrder() });

    page?.onOpenServing({ currentTarget: { dataset: { no: 'D009' } } } as never);
    await flush();
    await flush();

    expect(page?.data.showServingList).toBe(false);
    expect(page?.data.orderNo).toBe('D009');
    expect(page?.data.statusText).toBe('制作中');
    expect(wxMock.requests.some(r => String(r.url).includes('/customer/order/no/D009'))).toBe(true);
  });

  it('从菜单页回来（onShow）且桌上出现了出餐中的单：错误态让位给列表', async () => {
    // 先进来时桌上什么都没有 → 原有错误态
    const app = await bootApp();
    app.session.table = { tableId: 7, tableCode: 'A1', servingOrders: [] };
    const empty = await mountPage('order', () => import('../index'));
    empty.onLoad({});
    await flush();
    expect(empty.data.showServingList).toBe(false);
    expect(empty.data.error).toBeTruthy();

    // 去过菜单页后桌台详情带上了出餐中的单：回到订单 tab 应切到列表
    app.session.table = { tableId: 7, tableCode: 'A1', servingOrders: [servingOrder()] };
    empty.onShow();
    expect(empty.data.showServingList).toBe(true);
    expect(empty.data.error).toBe('');
    empty.onUnload();
  });

  it('没有出餐中的单：维持原有的错误态', async () => {
    const app = await bootApp();
    app.session.table = { tableId: 7, tableCode: 'A1', servingOrders: [] };
    const p2 = await mountPage('order', () => import('../index'));
    p2.onLoad({});
    await flush();

    expect(p2.data.showServingList).toBe(false);
    expect(p2.data.error).toBeTruthy();
    p2.onUnload();
  });
});

// 左上角返回按钮：tab 切页走 redirectTo（栈深 1）没有原生返回箭头，需要自绘；
// 从菜单页 navigateTo 进来时原生箭头已在，不再重复画。
describe('订单页 · 左上角返回', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    app.session.currentOrder = null;
    app.session.table = { tableId: 7, tableCode: 'A1', servingOrders: [] };
    wxMock.reset();
    wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo: '' });
  });

  afterEach(() => {
    page?.onUnload();
    page = null;
  });

  it('栈深 1（tab 切页）：显示自绘返回按钮', async () => {
    setCurrentPages([{ route: 'pages/order/index' }]);
    page = await mountPage('order', () => import('../index'));
    page.onLoad({});
    await flush();
    expect(page?.data.showBack).toBe(true);
  });

  it('栈里有上一层（菜单页 navigateTo 进来）：不画，交给原生返回箭头', async () => {
    setCurrentPages([{ route: 'pages/index/index' }, { route: 'pages/order/index' }]);
    page = await mountPage('order', () => import('../index'));
    page.onLoad({});
    await flush();
    expect(page?.data.showBack).toBe(false);
  });

  it('从餐中订单列表点进来的详情：返回先回列表', async () => {
    const app = await bootApp();
    app.session.table = {
      tableId: 7,
      tableCode: 'A1',
      servingOrders: [servingOrder()]
    };
    setCurrentPages([{ route: 'pages/order/index' }]);
    page = await mountPage('order', () => import('../index'));
    page.onLoad({});
    await flush();

    wxMock.reply({ code: 200, msg: 'ok', data: servingOrder() });
    page.onOpenServing({ currentTarget: { dataset: { no: 'D009' } } } as never);
    await flush();
    await flush();
    expect(page?.data.showServingList).toBe(false);

    page.onBack();
    expect(page?.data.showServingList).toBe(true);
    expect(page?.data.fromServingList).toBe(false);
  });

  it('自己的订单：返回回点餐页', async () => {
    const app = await bootApp();
    app.session.currentOrder = servingOrder({ orderNo: 'D001', payStatus: 0, orderStatus: 1 });
    setCurrentPages([{ route: 'pages/order/index' }]);
    page = await mountPage('order', () => import('../index'));
    page.onLoad({});
    await flush();

    page.onBack();
    // goToMenu：栈里没有菜单页 → redirectTo 打开（带 append=1）
    expect(wxMock.navigations.some(n => n.type === 'redirectTo' && n.url === '/pages/index/index?append=1')).toBe(true);
  });
});
