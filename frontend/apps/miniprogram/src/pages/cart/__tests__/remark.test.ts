// 购物车页「就地改备注」与「下单成功后跳转」的回归用例。
//
// 覆盖两处真踩过的坑：
//   1) 想改单项备注只能靠菜品弹层「确定」，等于为了改两个字多下一份菜；
//      现在的 showModal 可编辑路径必须「只改文字、不加份数」，且老基础库拿不到
//      输入内容时不能把顾客写的备注清掉。
//   2) 下单成功但紧随的详情回拉失败时，顾客会卡在「购物车还是空的」，
//      订单其实已经生成 —— 极易重复下单。现在必须带上 orderNo 跳订单页兜底。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartAdd, cartChangeQty, cartClear, cartState } from '../../../store/cart';

async function mount(): Promise<PageInstance> {
  return mountPage('cart', () => import('../index'));
}

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

const KEY = '101:2';

describe('购物车页 · 就地改备注', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    app.session.tableId = 7;
    app.session.personCount = 1;
    app.session.config = { shop_name: '长健农场' };
    wxMock.reset();
  });

  it('改备注只改文字，不加份数', async () => {
    seedLine(101, 2, 108, 1, '不要辣');
    const page = await mount();
    page.onShow();

    wxMock.modalInput = '多放葱';
    page.onEditRemark({ currentTarget: { dataset: { key: KEY } } } as never);
    await flush();

    expect(cartState()[0].itemRemark).toBe('多放葱');
    expect(cartState()[0].quantity).toBe(1);
    expect((page.data.rows as { itemRemark: string }[])[0].itemRemark).toBe('多放葱');
  });

  it('旧基础库不返回输入内容时保持原备注，不清空', async () => {
    seedLine(101, 2, 108, 1, '不要辣');
    const page = await mount();
    page.onShow();

    // modalInput 保持 undefined：模拟 < 2.17.1 的 showModal 不带 content
    page.onEditRemark({ currentTarget: { dataset: { key: KEY } } } as never);
    await flush();

    expect(cartState()[0].itemRemark).toBe('不要辣');
  });
});

describe('购物车页 · 下单成功后的跳转', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    app.session.tableId = 7;
    app.session.personCount = 1;
    app.session.config = { shop_name: '长健农场' };
    wxMock.reset();
  });

  it('详情回拉失败：仍带 orderNo 跳订单页，不把顾客留在空购物车', async () => {
    seedLine();
    const page = await mount();
    page.onShow();

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } }); // 提交成功
    wxMock.replyFail('request:fail'); // 详情回拉失败
    await page.onSubmit();
    await flush();

    expect(cartState()).toHaveLength(0);
    expect(wxMock.navigations).toContainEqual({
      type: 'redirectTo',
      url: '/pages/order/index?orderNo=D001'
    });
    expect(wxMock.toasts.map(t => t.title)).toContain('下单成功');
  });

  it('详情回拉成功：走会话直接跳订单页（不多带一次请求的参数）', async () => {
    seedLine();
    const page = await mount();
    page.onShow();

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 1, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    expect(wxMock.navigations).toContainEqual({ type: 'redirectTo', url: '/pages/order/index' });
  });

  it('加菜态：提交走 append 且不带整单备注（后端该接口不收这个字段）', async () => {
    const app = await bootApp();
    app.session.currentOrder = { orderNo: 'D001', orderStatus: 2, payStatus: 0, personCount: 2 };
    seedLine(102, 5, 42);

    const page = await mount();
    page.onShow();
    expect(page.data.appendMode).toBe(true);

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 2, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    expect(post?.data).not.toHaveProperty('orderRemark');
    expect(post?.data).toMatchObject({ orderNo: 'D001' });
  });

  it('订单状态被序列化成字符串时仍判为加菜（判错会开出第二笔订单）', async () => {
    const app = await bootApp();
    // 后端一旦把 order_status 序列化成字符串，不做 Number() 归一就会恒判 false
    app.session.currentOrder = { orderNo: 'D001', orderStatus: '2' as never, payStatus: 0, personCount: 2 };
    seedLine(102, 5, 42);

    const page = await mount();
    page.onShow();
    expect(page.data.appendMode).toBe(true);

    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001' } });
    wxMock.reply({ code: 200, msg: 'ok', data: { orderNo: 'D001', orderStatus: 2, payStatus: 0 } });
    await page.onSubmit();
    await flush();

    const post = wxMock.requests.find(r => r.method === 'POST');
    expect(post?.url).toContain('/customer/order/append');
  });
});
