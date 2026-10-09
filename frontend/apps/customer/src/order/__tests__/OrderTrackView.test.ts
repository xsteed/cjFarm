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

function mountView(overrides: Partial<OrderDetail> = {}, view: ViewState = 'order') {
  return mount(OrderTrackView, {
    props: {
      view,
      currentOrder: order(overrides),
      isActive: true,
      canAppend: false,
      urging: false,
      urgeCooldown: 0,
      showTabbar: true
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
});
