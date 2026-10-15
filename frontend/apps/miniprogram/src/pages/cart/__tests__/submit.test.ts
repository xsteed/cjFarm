// 下单页（购物车）的单测：这是顾客端唯一写数据的入口，也是最贵的一条链路。
//
// 覆盖：首次下单 / 加菜判别、幂等键、成功后清空购物车并跳订单页、
// 业务错误时「不跳转 + 不丢购物车 + 保留幂等键」、空车提交、人数与合计联动。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartAdd, cartChangeQty, cartClear, cartState } from '../../../store/cart';
import { sessionState } from '../../../store/session';
import { API_BASE_URL } from '../../../config';

function seedLine(dishId = 101, specId = 2, price = 108, qty = 1, remark = '不要辣'): void {
  cartAdd({
    dishId,
    dishName: `菜${dishId}`,
    specId,
    specName: '大份',
    price,
    image: '',
    itemRemark: remark
  });
  for (let i = 1; i < qty; i++) cartChangeQty(`${dishId}:${specId}`, 1);
}

async function mount(): Promise<PageInstance> {
  return mountPage('cart', () => import('../index'));
}

describe('购物车页 · 下单', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.tableId = 7;
    app.session.tableCode = 'K7M3PQ9X';
    app.session.currentOrder = null;
    app.session.personCount = 1;
    app.session.config = { shop_name: '长健农场', seat_fee_enabled: '1', seat_fee: '2' };
    wxMock.reset();
  });

  it('空车提交：只提示，不发请求', async () => {
    const page = await mount();
    page.onShow();
    await page.onSubmit();
    expect(wxMock.lastToast).toBe('先挑几道想吃的菜吧～');
    expect(wxMock.requests).toHaveLength(0);
  });

  it('首次下单：POST /customer/order，成功后清空购物车并跳订单页', async () => {
    seedLine(101, 2, 108, 2);
    const page = await mount();
    page.onShow();

    expect(page.data.rows).toHaveLength(1);
    expect(page.data.dishTotalText).toBe('216.00');
    expect(page.data.seatFeeText).toBe('2.00'); // 1 人 × 2 元
    expect(page.data.totalText).toBe('218.00');

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', shortNo: '8001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 1, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    // 跟随 config.ts 的 API_ORIGIN，不写死域名（本地联调会把它指到 127.0.0.1）
    expect(post?.url).toBe(`${API_BASE_URL}/customer/order`);
    expect(post?.header?.['Content-Type']).toBe('application/json');
    expect(post?.data).toMatchObject({
      tableId: 7,
      personCount: 1,
      items: [{ dishId: 101, specId: 2, quantity: 2, itemRemark: '不要辣' }]
    });
    // 幂等键带上并且是字符串（后端按它去重）
    expect(typeof (post?.data as { clientRequestId: unknown }).clientRequestId).toBe('string');

    expect(cartState()).toHaveLength(0);
    expect(sessionState().currentOrder?.orderNo).toBe('D001');
    expect(wxMock.lastToast).toBe('下单成功，冲呀');
    expect(wxMock.navigations).toContainEqual({ type: 'redirectTo', url: '/pages/order/index' });
  });

  it('本桌有进行中未支付订单时走加菜：POST /customer/order/append 带 orderNo，人数沿用订单值', async () => {
    const app = await bootApp();
    app.session.currentOrder = { orderNo: 'D001', orderStatus: 2, payStatus: 0, personCount: 3 };
    seedLine(102, 5, 42);

    const page = await mount();
    page.onShow();
    expect(page.data.appendMode).toBe(true);
    expect(page.data.submitText).toBe('提交加菜');
    expect(page.data.personCount).toBe(3);
    expect(page.data.totalText).toBe('48.00'); // 42 + 3 人 × 2 元茶位费

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 2, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    expect(post?.url).toBe(`${API_BASE_URL}/customer/order/append`);
    expect(post?.data).toMatchObject({ orderNo: 'D001', personCount: 3 });
    expect(wxMock.lastToast).toBe('加菜成功，冲呀');
  });

  it('业务错误：完整提示后端原因，停留本页、保留购物车与幂等键', async () => {
    seedLine();
    const page = await mount();
    page.onShow();

    wxMock.reply({ code: 400, msg: '该桌台已有未结账的订单，请扫描桌上的新二维码' });
    await page.onSubmit();
    await flush();

    expect(wxMock.lastToast).toBe('该桌台已有未结账的订单，请扫描桌上的新二维码');
    expect(wxMock.navigations).toHaveLength(0);
    expect(cartState()).toHaveLength(1);
    // 重试时要复用同一个幂等键，避免网络抖动导致重复下单
    expect(page.pendingRequestId).toBeTruthy();
    const first = page.pendingRequestId;

    // 重试：仍失败则键不变
    wxMock.reply({ code: 400, msg: '仍然失败' });
    await page.onSubmit();
    await flush();
    expect(page.pendingRequestId).toBe(first);
  });

  it('提交中重复点击不会发第二次请求', async () => {
    seedLine();
    const page = await mount();
    page.onShow();
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 1, payStatus: 0 } });

    const first = page.onSubmit();
    await page.onSubmit(); // 第二次点击：submitting 已置位，应被直接挡掉
    await first;
    await flush();

    expect(wxMock.requests.filter(r => r.method === 'POST')).toHaveLength(1);
  });

  it('人数增减会写回会话，并同步重算茶位费与合计', async () => {
    seedLine(101, 2, 50);
    const page = await mount();
    page.onShow();
    expect(page.data.totalText).toBe('52.00'); // 50 + 1×2

    page.onPerson({ currentTarget: { dataset: { delta: '1' } } } as never);
    expect(page.data.personCount).toBe(2);
    expect(sessionState().personCount).toBe(2);
    expect(page.data.totalText).toBe('54.00');

    // 下限是 1
    page.onPerson({ currentTarget: { dataset: { delta: '-5' } } } as never);
    expect(page.data.personCount).toBe(1);
    expect(page.data.totalText).toBe('52.00');
  });

  it('移除菜品需要确认，确认后从购物车消失', async () => {
    seedLine();
    const page = await mount();
    page.onShow();
    expect(page.data.rows).toHaveLength(1);

    page.onRemove({ currentTarget: { dataset: { key: '101:2' } } } as never);
    await flush();
    expect(wxMock.modals).toHaveLength(1);
    expect(cartState()).toHaveLength(0);
    expect(page.data.rows).toHaveLength(0);
  });
});

// 先付模式（门店在管理端「营业设置」里开启）：付完款后厨才出单。
// 但落点仍是订单明细页 —— 顾客要能先核对菜品与金额，再由页面上的待付款引导去支付。
describe('购物车页 · 先付模式', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.tableId = 7;
    app.session.tableCode = 'K7M3PQ9X';
    app.session.currentOrder = null;
    app.session.personCount = 1;
    app.session.config = { shop_name: '长健农场', pay_first_enabled: '1' };
    wxMock.reset();
  });

  it('首次下单：落订单明细页，提示说明要先付款', async () => {
    seedLine(101, 2, 108);
    const page = await mount();
    page.onShow();

    expect(page.data.payFirst).toBe(true);
    // 提交后仍是明细页：让顾客先核对菜品与金额，再由待付款引导去支付
    expect(page.data.submitText).toBe('提交订单');
    expect(page.data.payHint).toContain('先付模式');

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 1, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    expect(wxMock.navigations).toContainEqual({ type: 'redirectTo', url: '/pages/order/index' });
  });

  it('加菜：同样落订单明细页', async () => {
    const app = await bootApp();
    app.session.currentOrder = { orderNo: 'D001', orderStatus: 2, payStatus: 0, personCount: 3 };
    seedLine(102, 5, 42);

    const page = await mount();
    page.onShow();
    expect(page.data.appendMode).toBe(true);
    expect(page.data.submitText).toBe('提交加菜');

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 2, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    expect(wxMock.navigations).toContainEqual({ type: 'redirectTo', url: '/pages/order/index' });
  });
});
