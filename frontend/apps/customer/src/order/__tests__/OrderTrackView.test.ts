import { mount } from '@vue/test-utils';
import OrderTrackView from '../OrderTrackView.vue';
import type { ViewState } from '@dining/customer-core';
import type { OrderDetail } from '@dining/types';

function order(overrides: Partial<OrderDetail> = {}): OrderDetail {
  return {
    orderId: 1,
    orderNo: 'NO001',
    shortNo: '001',
    tableId: 1,
    personCount: 2,
    orderStatus: 1,
    payStatus: 0,
    dishAmount: 28,
    seatFee: 0,
    discountAmount: 0,
    totalAmount: 28,
    items: [
      { itemId: 1, dishId: 101, dishName: '宫保鸡丁', specId: 1, specName: '小份', quantity: 1, price: 28, amount: 28 }
    ],
    ...overrides
  };
}

function mountView(
  overrides: Partial<OrderDetail> = {},
  view: ViewState = 'order',
  payFirst = false,
  canAppend = false
) {
  return mount(OrderTrackView, {
    props: {
      view,
      currentOrder: order(overrides),
      isActive: true,
      canAppend,
      urging: false,
      urgeCooldown: 0,
      showTabbar: true,
      payFirst
    }
  });
}

describe('OrderTrackView', () => {
  it('取消单：没有原因时给标准说明，有原因时带上原因', () => {
    const noReason = mountView({ orderStatus: 5 }, 'canceled');
    expect(noReason.find('.st-tip--cancel').text()).toBe('订单已取消，本单未支付、不会产生任何费用');

    const withReason = mountView({ orderStatus: 5, cancelReason: '菜品售罄' }, 'canceled');
    expect(withReason.find('.st-tip--cancel').text()).toBe('订单已取消：菜品售罄');
  });

  it('已结账：文案是「已结账，后厨正在做啦」', () => {
    const wrapper = mountView(
      { orderStatus: 2, payStatus: 1, payType: '微信', payTime: '2026-10-09 12:03:07' },
      'settled'
    );
    // 状态步骤条已下线,不再渲染
    expect(wrapper.find('.op-steps').exists()).toBe(false);
    expect(wrapper.find('.st-tip').text()).toContain('已结账，后厨正在做啦');
    expect(wrapper.find('.st-info-paid').text()).toBe('已支付（微信） · 2026-10-09 12:03');
  });

  it('未支付：不出现支付凭证行', () => {
    expect(mountView({ payStatus: 0 }).find('.st-info-paid').exists()).toBe(false);
  });

  it('已完成的单：文案是「这顿吃完啦」', () => {
    const wrapper = mountView({ orderStatus: 4, payStatus: 1 }, 'done');
    expect(wrapper.find('.st-tip').text()).toContain('这顿吃完啦');
  });

  it('先付模式：未支付时给出「待付款」引导，后付模式不出现', () => {
    // 后付（默认）：未支付订单不该出现待付款引导
    expect(mountView({ payStatus: 0 }).find('.st-payfirst-tip').exists()).toBe(false);
    const payFirst = mountView({ payStatus: 0 }, 'order', true);
    const tip = payFirst.find('.st-payfirst-tip').text();
    // 引导要同时点明「先付模式」与下一步动作「点下方去支付」；
    // 金额刻意不带 —— 账单末行已给出，重复出现反而像两笔账
    expect(tip).toContain('先付模式');
    expect(tip).toContain('去支付');
    expect(tip).not.toContain('¥');
    // 已支付：引导随之消失（付款已完成，无需再引导）
    expect(mountView({ payStatus: 1 }, 'settled', true).find('.st-payfirst-tip').exists()).toBe(false);
  });

  it('先付模式：未支付的「去支付」按钮带火苗，后付模式不带', () => {
    // 先付：后厨还没收到这颗单，付款是关键动作，火苗给紧迫感
    expect(mountView({ payStatus: 0 }, 'order', true).find('.st-paybtn').text()).toContain('🔥 去支付');
    // 后付：只是结账，不加火苗
    expect(mountView({ payStatus: 0 }, 'order', false).find('.st-paybtn').text()).toBe('去支付');
    // 已支付：按钮本身就不渲染了
    expect(mountView({ payStatus: 1 }, 'settled', true).find('.st-paybtn').exists()).toBe(false);
  });

  it('先付模式：未支付时收掉催菜，只留「加菜」这条出口', () => {
    // 后付（默认）：未支付照样能催
    expect(mountView({ payStatus: 0 }).find('.st-urge').exists()).toBe(true);

    // 先付 + 未支付 + 可加菜：催菜收起，加菜还在（不能把动作区整块抽掉）
    const appendOnly = mountView({ payStatus: 0 }, 'order', true, true);
    expect(appendOnly.find('.st-urge').exists()).toBe(false);
    expect(appendOnly.find('.st-append').exists()).toBe(true);

    // 先付 + 未支付 + 不可加菜：动作区整块不渲染，页面只剩「去支付」
    const payOnly = mountView({ payStatus: 0 }, 'order', true, false);
    expect(payOnly.find('.st-actions').exists()).toBe(false);
    expect(payOnly.find('.st-paybtn').exists()).toBe(true);

    // 先付 + 已支付：后厨已经收到单，催菜回来
    expect(mountView({ orderStatus: 2, payStatus: 1 }, 'order', true).find('.st-urge').exists()).toBe(true);
  });
});
