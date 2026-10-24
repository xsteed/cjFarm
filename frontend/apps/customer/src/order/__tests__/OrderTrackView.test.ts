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
    expect(wrapper.find('.st-tip').text()).toContain('已结账，后厨正在做啦');
    expect(wrapper.find('.st-info-paid').text()).toBe('已支付（微信） · 2026-10-09 12:03');
  });

  it('状态卡：大字说清走到哪一步，进度按后端状态点亮（不承诺时间）', () => {
    // 已下单：第一步亮、第二步未亮
    const placed = mountView({ orderStatus: 1, payStatus: 1 }, 'settled');
    expect(placed.find('.sh-title').text()).toBe('已下单，等待接单');
    const dots = placed.findAll('.step');
    expect(dots).toHaveLength(3);
    expect(dots.map(d => d.classes('on'))).toEqual([true, false, false]);
    // 已等时长来自订单 createTime；fixture 没给时间就不编造
    expect(placed.find('.sh-sub').text()).toBe('订单已提交');

    // 制作中：两步亮，当前节点标 now
    const cooking = mountView({ orderStatus: 2, payStatus: 1 }, 'settled');
    expect(cooking.find('.sh-title').text()).toBe('后厨正在制作');
    expect(cooking.findAll('.step').map(d => d.classes('on'))).toEqual([true, true, false]);
    expect(cooking.findAll('.step')[1].classes('now')).toBe(true);

    // 已完成：三步全亮，进度不再标 now（单子已经走完）
    const finished = mountView({ orderStatus: 4, payStatus: 1 }, 'done');
    expect(finished.find('.sh-title').text()).toBe('本单已完成');
    expect(finished.findAll('.step').map(d => d.classes('on'))).toEqual([true, true, true]);
    expect(finished.findAll('.step').some(d => d.classes('now'))).toBe(false);
  });

  it('状态卡：取消单不出进度（没有「到哪一步」可言）', () => {
    const wrapper = mountView({ orderStatus: 5 }, 'canceled');
    expect(wrapper.find('.st-steps').exists()).toBe(false);
    expect(wrapper.find('.sh-title').text()).toBe('订单已取消');
  });

  it('状态卡：先付未付款时不说「已下单」，改成等付款且不画进度', () => {
    // 后厨还没收到这颗单：画进度会让人以为菜已经在做
    const wrapper = mountView({ orderStatus: 1, payStatus: 0 }, 'order', true);
    expect(wrapper.find('.sh-title').text()).toBe('等待付款');
    expect(wrapper.find('.sh-sub').text()).toBe('付款后后厨即刻开工');
    expect(wrapper.find('.st-steps').exists()).toBe(false);

    // 付完款：回到正常出餐进度
    const paid = mountView({ orderStatus: 1, payStatus: 1 }, 'settled', true);
    expect(paid.find('.sh-title').text()).toBe('已下单，等待接单');
    expect(paid.find('.st-steps').exists()).toBe(true);
  });

  it('菜品明细：整单件数在卡头，明细行是纯文字（不放菜品图）', () => {
    const wrapper = mountView();
    expect(wrapper.find('.st-count').text()).toBe('共 1 件');
    // 下单后是来「核对点了什么」的,不放图片,也就不该出现任何缩略图容器
    expect(wrapper.find('.si-thumb').exists()).toBe(false);
    expect(wrapper.find('.st-item img').exists()).toBe(false);
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

  it('催菜不是按钮：常态是账单下一行小字，催过之后整句换成安抚文案', () => {
    const idle = mountView();
    expect(idle.find('.st-urge-line').text()).toBe('菜等好久还没来？催一下后厨');
    // 「加菜」按钮区里不再有催菜，两者不并排
    expect(idle.find('.st-actions').exists()).toBe(false);

    const props = {
      view: 'order' as ViewState,
      currentOrder: order(),
      isActive: true,
      canAppend: false,
      showTabbar: true,
      payFirst: false
    };
    // 请求中：链接收起，避免连点
    const urging = mount(OrderTrackView, { props: { ...props, urging: true, urgeCooldown: 0 } });
    expect(urging.find('.st-urge').exists()).toBe(false);
    expect(urging.find('.st-urge-line').text()).toBe('正在通知后厨…');

    // 冷却中：只给一句安抚文案，不给倒计时（倒计时会让人盯着数等下一次催）
    const cooling = mount(OrderTrackView, { props: { ...props, urging: false, urgeCooldown: 120 } });
    expect(cooling.find('.st-urge').exists()).toBe(false);
    expect(cooling.find('.st-urge-line').text()).toBe('已通知后厨加急，马上就来～');
  });

  it('催菜那行小字排在按钮下方，不压在「加菜 / 去支付」上面', () => {
    const wrapper = mountView({ payStatus: 0 }, 'order', false, true);
    const html = wrapper.html();
    expect(html).toContain('st-urge-line');
    // DOM 顺序：加菜 → 去支付 → 催菜小字
    expect(html.indexOf('st-append')).toBeLessThan(html.indexOf('st-paybtn'));
    expect(html.indexOf('st-paybtn')).toBeLessThan(html.indexOf('st-urge-line'));
  });
});
