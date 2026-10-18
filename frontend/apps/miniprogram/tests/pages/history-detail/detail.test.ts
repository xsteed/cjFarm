// 历史订单详情页：只读渲染 + 「再来一单」。
//
// 「再来一单」不是把订单原样复制一份，而是把上次点的菜按**当前菜单**复核后
// 并入当前购物车。几条硬约束：
//   ・没有桌台就无处下单（后端强制 tableId）→ 只提示，不拉菜单、不跳转；
//   ・赠菜不复制（商家送的，不该被顾客自助再点一份）；
//   ・下架/售罄的菜丢掉；全丢光就只提示，不能把顾客送进空购物车；
//   ・并入而不是替换（顾客自己已经点的菜不能被冲掉）。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { CUSTOMER_TOKEN_KEY } from '../../../src/config';

function order(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    orderNo: 'D001',
    shortNo: '001',
    tableId: 7,
    tableName: '大厅01桌',
    orderStatus: 4,
    payStatus: 1,
    personCount: 3,
    dishAmount: 116,
    seatFee: 6,
    discountAmount: 0,
    totalAmount: 122,
    createTime: '2026-10-18T12:03:07',
    items: [
      {
        itemId: 1,
        dishId: 101,
        specId: 1,
        dishName: '红烧肉',
        specName: '大份',
        quantity: 2,
        amount: 116,
        itemRemark: '不要辣',
        isGift: 0
      }
    ],
    ...overrides
  };
}

const menu = [
  {
    categoryId: 1,
    categoryName: '热菜',
    dishes: [
      {
        dishId: 101,
        dishName: '红烧肉',
        dishImage: '/uploads/101.png',
        soldOut: 0,
        specs: [{ specId: 1, dishName: 101, specName: '大份', price: 58, stock: -1 }]
      },
      {
        dishId: 102,
        dishName: '青菜',
        dishImage: '/uploads/102.png',
        soldOut: 1,
        specs: [{ specId: 2, dishName: 102, specName: '小份', price: 12, stock: -1 }]
      }
    ]
  }
];

async function mount(): Promise<PageInstance> {
  const page = await mountPage('history-detail', () => import('../../../src/pages/history-detail/index'));
  return page;
}

describe('历史订单详情', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    app.session.tableCode = 'A1';
    app.session.currentOrder = null;
    app.cart.length = 0;
    wxMock.reset();
    // 详情接口是会员专属：预置令牌，否则 memberedGet 会先走一遍 wx.login 静默登录，
    // 把预置的回包错取给登录请求。
    wxMock.storage.set(CUSTOMER_TOKEN_KEY, 'test-token');
    page = await mount();
  });

  afterEach(() => {
    page = null;
  });

  it('加载成功：渲染明细、账单与「再来一单」入口', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    expect(wxMock.lastRequest?.url).toContain('/customer/order/mine/D001');
    expect(page?.data.loading).toBe(false);
    expect(page?.data.tableLabel).toBe('大厅01桌');
    expect(page?.data.personCount).toBe(3);
    expect(page?.data.totalText).toBe('122.00');
    expect(page?.data.createTimeText).toBe('2026-10-18 12:03');
    const rows = page?.data.rows as Array<Record<string, unknown>>;
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: '红烧肉', amountText: '¥116.00', isGift: false });
    expect(page?.data.canReorder).toBe(true);
  });

  it('跨桌的历史单不给「去支付」（否则会把旧单费用算到当前桌）', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: order({ tableId: 9, payStatus: 0, orderStatus: 1 }) });
    page?.onLoad({ orderNo: 'D001' });
    await flush();
    expect(page?.data.canPay).toBe(false);
  });

  it('当前桌 + 未支付 + 进行中：给「去支付」', async () => {
    wxMock.reply({ code: 200, msg: 'ok', data: order({ tableId: 7, payStatus: 0, orderStatus: 1 }) });
    page?.onLoad({ orderNo: 'D001' });
    await flush();
    expect(page?.data.canPay).toBe(true);
  });

  it('再来一单：按当前菜单复核后并入购物车，并跳到购物车页', async () => {
    const app = await bootApp();
    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    wxMock.reply({ code: 200, msg: 'ok', data: menu });
    page?.onReorder();
    await flush();
    await flush();

    expect(wxMock.lastRequest?.url).toContain('/customer/menu');
    expect(app.cart).toHaveLength(1);
    expect(app.cart[0]).toMatchObject({
      dishId: 101,
      specId: 1,
      quantity: 2,
      price: 58, // 价格取菜单现值
      itemRemark: '不要辣',
      image: '/uploads/101.png'
    });
    expect(wxMock.navigations.some(n => n.type === 'redirectTo' && n.url === '/pages/cart/index')).toBe(true);
  });

  it('赠菜不复制', async () => {
    const app = await bootApp();
    wxMock.reply({
      code: 200,
      msg: 'ok',
      data: order({
        items: [
          { itemId: 1, dishId: 101, specId: 1, dishName: '红烧肉', specName: '大份', quantity: 1, amount: 58, isGift: 0 },
          { itemId: 2, dishId: 102, specId: 2, dishName: '青菜', specName: '小份', quantity: 1, amount: 0, isGift: 1 }
        ]
      })
    });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    wxMock.reply({ code: 200, msg: 'ok', data: menu });
    page?.onReorder();
    await flush();
    await flush();

    expect(app.cart.map(l => l.dishId)).toEqual([101]);
  });

  it('菜全都下架/售罄：只提示，不跳转也不污染购物车', async () => {
    const app = await bootApp();
    wxMock.reply({
      code: 200,
      msg: 'ok',
      data: order({
        items: [
          { itemId: 1, dishId: 102, specId: 2, dishName: '青菜', specName: '小份', quantity: 1, amount: 12, isGift: 0 }
        ]
      })
    });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    wxMock.reply({ code: 200, msg: 'ok', data: menu }); // 青菜 soldOut=1
    page?.onReorder();
    await flush();
    await flush();

    expect(app.cart).toHaveLength(0);
    expect(wxMock.navigations).toHaveLength(0);
    expect(wxMock.lastToast).toContain('点不了');
  });

  it('没有桌台：提示先扫码，不发任何请求', async () => {
    const app = await bootApp();
    app.session.tableId = 0;
    app.session.tableCode = '';

    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page?.onLoad({ orderNo: 'D001' });
    await flush();
    const before = wxMock.requests.length;

    page?.onReorder();
    await flush();

    expect(wxMock.requests.length).toBe(before);
    expect(wxMock.lastToast).toContain('二维码');
  });

  it('并入而不是替换：顾客自己已点的菜保留，同规格份数累加', async () => {
    const app = await bootApp();
    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    // 先自己点了一份同规格的菜
    const { cartAdd } = await import('../../../src/store/cart');
    cartAdd({ dishId: 101, dishName: '红烧肉', specId: 1, specName: '大份', price: 58, image: '' });

    wxMock.reply({ code: 200, msg: 'ok', data: menu });
    page?.onReorder();
    await flush();
    await flush();

    expect(app.cart).toHaveLength(1);
    expect(app.cart[0].quantity).toBe(3); // 自己 1 份 + 历史 2 份
  });

  it('加载失败：展示错误并可重试', async () => {
    wxMock.reply({ code: 400, msg: '订单不存在或已结束', data: null });
    page?.onLoad({ orderNo: 'D001' });
    await flush();

    expect(page?.data.loading).toBe(false);
    expect(page?.data.error).toBeTruthy();

    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page?.onRetry();
    await flush();
    expect(page?.data.error).toBe('');
    expect(page?.data.rows).toHaveLength(1);
  });

  it('缺少订单号：直接进错误态，不发请求', async () => {
    page?.onLoad({});
    await flush();
    expect(wxMock.requests).toHaveLength(0);
    expect(page?.data.error).toBe('缺少订单号');
  });
});
