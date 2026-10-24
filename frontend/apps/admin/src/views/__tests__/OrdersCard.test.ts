import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Orders from '../Orders.vue';
import { changeOrderStatus, finishOrder, listOrders, cancelOrder } from '@dining/api';
import { setAuth } from '@dining/auth';
import { clearAllStorage, flushPromises, stubResizeObserver } from '@shell/test/mocks.ts';
import type { OrderSummary } from '@dining/types';

// 先付开关用可控桩替掉模块级单例(真实现的单例缓存/失败兜底见 usePayFirst.test.ts):
// usePayFirst 是模块级单例,一次页面生命周期只拉一次配置,本文件要先付/后付两种矩阵
// 都跑,只能把开关提到用例里控制;每个用例在挂载前置 payFirstState.value 即可。
const payFirstState = vi.hoisted(() => ({ value: false }));

vi.mock('../../composables/usePayFirst', () => ({
  usePayFirst: () => payFirstState
}));

// 详情弹窗依赖树深(收款/退款/改单全在组件内),用轻量 stub 承接;
// openSettle/openRefund/refresh 换成 spy,供断言卡片快捷按钮的接线与入参。
const detailSpies = vi.hoisted(() => ({
  openSettle: vi.fn(),
  openRefund: vi.fn(),
  refresh: vi.fn()
}));

vi.mock('../../components/OrderDetailDialog.vue', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    default: defineComponent({
      name: 'OrderDetailDialog',
      props: {
        visible: { type: Boolean, default: false },
        orderId: { type: Number, default: 0 }
      },
      emits: ['update:visible', 'changed'],
      setup() {
        return detailSpies;
      },
      render() {
        return h('div', { class: 'order-detail-stub' });
      }
    })
  };
});

// 与 Orders.vue / usePayFirst / perm 共享同一个 @dining/api 模块,统一 mock 全部导出,
// 避免真实 http(axios) 被加载或发起请求;@dining/auth 模块加载时依赖
// onAuthCleared/getProfile,漏了会让整个套件加载失败。
vi.mock('@dining/api', () => ({
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn(),
  listOrders: vi.fn(),
  changeOrderStatus: vi.fn(),
  cancelOrder: vi.fn(),
  finishOrder: vi.fn(),
  handleUrge: vi.fn(),
  getPublicConfig: vi.fn(() => Promise.resolve({ pay_first_enabled: '0' })),
  // 与 @dining/shared 的 dicts.ts 同口径(页面只用它的 label/theme 渲染标签)
  ORDER_STATUS: {
    1: { label: '已下单', theme: 'primary' },
    2: { label: '制作中', theme: 'warning' },
    3: { label: '已上齐', theme: 'success' },
    4: { label: '已完成', theme: 'success' },
    5: { label: '已取消', theme: 'danger' }
  }
}));

// MessagePlugin 换 mock,避免测试里真实弹 toast 产生 DOM 残留与定时器;
// DialogPlugin 等其余导出保留真实实现(完成订单的二次确认弹窗要真实渲染出来)。
vi.mock('tdesign-vue-next', async importOriginal => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    MessagePlugin: {
      success: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      info: vi.fn(),
      close: vi.fn()
    }
  };
});

// 图标在 main.ts 里全局注册,测试环境没有;逐个 stub 掉避免组件解析告警。
const iconStubs = {
  'chevron-left-icon': { template: '<i />' },
  'chevron-right-icon': { template: '<i />' },
  'search-icon': { template: '<i />' },
  'filter-icon': { template: '<i />' },
  'refresh-icon': { template: '<i />' }
};

const listOrdersMock = vi.mocked(listOrders);
const finishOrderMock = vi.mocked(finishOrder);
const changeOrderStatusMock = vi.mocked(changeOrderStatus);
const cancelOrderMock = vi.mocked(cancelOrder);

// 卡片上的「超时」判定基于「下单到现在」的时长,createTime 必须相对当前时间生成 ——
// 写死时间戳会让用例随日历推移而失效(今天的「刚下单」,几个月后就成了超时单)。
function timeAgo(minutes: number): string {
  const d = new Date(Date.now() - minutes * 60_000);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function order(overrides: Partial<OrderSummary> = {}): OrderSummary {
  return {
    orderId: 1,
    orderNo: 'NO20261017001',
    shortNo: '0001',
    tableNo: '01',
    tableName: '大厅01桌',
    personCount: 2,
    dishAmount: 28,
    seatFee: 0,
    discountAmount: 0,
    totalAmount: 28,
    orderStatus: 1,
    payStatus: 0,
    payChannel: '',
    refundAmount: 0,
    createTime: timeAgo(2),
    items: [],
    ...overrides
  };
}

let restoreMatchMedia: () => void;
let restoreResizeObserver: () => void;

beforeEach(() => {
  // 移动端断点(max-width: 1024px)置真,页面走 App 式订单流(卡片)
  const original = window.matchMedia;
  window.matchMedia = vi.fn().mockImplementation((query: string): MediaQueryList => {
    return {
      matches: query === '(max-width: 1024px)',
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => true)
    } as unknown as MediaQueryList;
  });
  restoreMatchMedia = () => {
    window.matchMedia = original;
  };
  restoreResizeObserver = stubResizeObserver();

  clearAllStorage();
  setAuth({ token: 't', username: 'u', perms: ['order:operate', 'order:settle', 'order:cancel', 'refund:operate'] });
  payFirstState.value = false;
  detailSpies.openSettle.mockClear();
  detailSpies.openRefund.mockClear();
  detailSpies.refresh.mockClear();
  finishOrderMock.mockReset();
  cancelOrderMock.mockReset();
  cancelOrderMock.mockResolvedValue({} as never);
});

afterEach(() => {
  restoreMatchMedia();
  restoreResizeObserver();
  // DialogPlugin 的程序化弹窗挂在 body 下,清掉避免跨用例串味
  document.querySelectorAll('.t-dialog__ctx, .t-dialog__mask, .t-dialog').forEach(el => el.remove());
});

// 注意:不能 stub teleport —— DialogPlugin 的程序化弹窗经 DialogPlugin._context
// 继承本 app 的组件解析,teleport 一旦被换成桩,二次确认弹窗只渲染出
// [object Object],点不到「确认」。
// attachTo:取消订单弹窗是 t-dialog(Teleport 到 body),未挂到文档时内容不会真正渲染,
// 想查 body 里的弹窗 DOM 必须把应用挂到 body 上。
function mountOrders(options: { attach?: boolean } = {}) {
  return mount(Orders, {
    ...(options.attach ? { attachTo: document.body } : {}),
    global: {
      plugins: [TDesign],
      stubs: { ...iconStubs },
      config: { errorHandler: () => undefined }
    }
  });
}

function cardLabels(wrapper: ReturnType<typeof mountOrders>): string[] {
  return wrapper.findAll('.ocard-ft button').map(b => (b.text() || '').trim());
}

describe('Orders 订单管理页 · 移动端订单卡片按钮矩阵', () => {
  it('先付模式·未支付单:卡片依次为 详情 / 确认收款 / 取消订单', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({ total: 1, items: [order({ orderStatus: 1, payStatus: 0 })] });
    const wrapper = mountOrders();
    await flushPromises();
    expect(cardLabels(wrapper)).toEqual(['订单详情', '确认收款', '取消订单']);
    wrapper.unmount();
  });

  it('先付模式·已支付单:卡片依次为 详情 / 完成订单(在线已付单也不出退款键)', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 2, payStatus: 1, payChannel: 'wxpay', payType: '微信支付' })]
    });
    const wrapper = mountOrders();
    await flushPromises();
    expect(cardLabels(wrapper)).toEqual(['订单详情', '完成订单']);
    wrapper.unmount();
  });

  it('先付模式:制作/上齐/催办/退款不上卡片(一律进详情弹窗)', async () => {
    payFirstState.value = true;
    // 已下单 + 顾客催菜 + 未支付:后付模式下这张卡会有「已催办 / 开始制作」
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, pendingUrge: true })]
    });
    const wrapper = mountOrders();
    await flushPromises();
    expect(cardLabels(wrapper)).toEqual(['订单详情', '确认收款', '取消订单']);
    wrapper.unmount();
  });

  it('先付模式:点「确认收款」走 pay 模式(只标记收款,后厨才会补出厨单)', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({ total: 1, items: [order({ orderStatus: 1, payStatus: 0 })] });
    const wrapper = mountOrders();
    await flushPromises();

    const btn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('确认收款'));
    expect(btn).toBeTruthy();
    await btn!.trigger('click');
    expect(detailSpies.openSettle).toHaveBeenCalledWith(expect.objectContaining({ orderId: 1 }), 'pay');
    wrapper.unmount();
  });

  it('先付模式:点「完成订单」二次确认后调用 /order/finish 归档', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({ total: 1, items: [order({ orderStatus: 2, payStatus: 1, payType: '微信支付' })] });
    const wrapper = mountOrders();
    await flushPromises();

    const btn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('完成订单'));
    expect(btn).toBeTruthy();
    await btn!.trigger('click');
    await flushPromises();

    // DialogPlugin.confirm 是程序化弹窗,挂在 body 下;确认按钮类名见 tdesign useAction 的 className
    const confirmBtn = document.querySelector('.t-dialog__confirm') as HTMLElement | null;
    expect(confirmBtn).toBeTruthy();
    confirmBtn!.click();
    await flushPromises();
    expect(finishOrderMock).toHaveBeenCalledWith({ orderId: 1 });
    wrapper.unmount();
  });

  it('先付模式·已收款待完成:右上角「待完成」badge + 整卡高亮,点完成后消失', async () => {
    payFirstState.value = true;
    listOrdersMock
      .mockResolvedValueOnce({
        total: 1,
        items: [order({ orderId: 1, orderStatus: 2, payStatus: 1, payType: '微信支付', needFinish: true })]
      })
      .mockResolvedValueOnce({
        total: 1,
        items: [order({ orderId: 1, orderStatus: 2, payStatus: 1, payType: '微信支付', needFinish: false })]
      });
    const wrapper = mountOrders();
    await flushPromises();

    // badge + 整卡高亮(is-need-finish 驱动浅成功底与绿描边),与「新」单同款样式
    const badge = wrapper.find('.ocard-finish');
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toBe('待完成');
    expect(wrapper.find('.ocard').classes()).toContain('is-need-finish');

    // 点「完成订单」→ 二次确认 → 归档 → 列表刷新:badge 与整卡高亮一并消失
    const btn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('完成订单'));
    await btn!.trigger('click');
    await flushPromises();
    const confirmBtn = document.querySelector('.t-dialog__confirm') as HTMLElement | null;
    expect(confirmBtn).toBeTruthy();
    confirmBtn!.click();
    await flushPromises();
    expect(finishOrderMock).toHaveBeenCalledWith({ orderId: 1 });
    expect(wrapper.find('.ocard-finish').exists()).toBe(false);
    expect(wrapper.find('.ocard').classes()).not.toContain('is-need-finish');
    wrapper.unmount();
  });

  it('已收款的单不会再被标成「新」:needFinish 与 isNew 互斥渲染', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 2,
      items: [
        order({ orderId: 1, orderStatus: 2, payStatus: 1, payType: '微信支付', isNew: false, needFinish: true }),
        order({ orderId: 2, orderNo: 'NO20261017002', orderStatus: 1, payStatus: 0, isNew: true, needFinish: false })
      ]
    });
    const wrapper = mountOrders();
    await flushPromises();

    const cards = wrapper.findAll('.ocard');
    expect(cards[0].classes()).toContain('is-need-finish');
    expect(cards[0].classes()).not.toContain('is-new');
    expect(cards[1].classes()).toContain('is-new');
    expect(cards[1].classes()).not.toContain('is-need-finish');
    expect(wrapper.findAll('.ocard-finish')).toHaveLength(1);
    expect(wrapper.findAll('.ocard-new')).toHaveLength(1);
    wrapper.unmount();
  });

  it('新单在卡片左上角显示「新」,仅点开详情查看不消解', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({
      total: 2,
      items: [
        order({ orderId: 1, isNew: true }),
        order({ orderId: 2, orderNo: 'NO20261017002', orderStatus: 2, isNew: false })
      ]
    });
    const wrapper = mountOrders();
    await flushPromises();

    const badges = wrapper.findAll('.ocard-new');
    expect(badges).toHaveLength(1);
    expect(badges[0].text()).toBe('新');

    // 「新」= 未操作过:仅打开详情查看不清除,角标保留
    await wrapper.findAll('.ocard')[0].trigger('click');
    await flushPromises();
    expect(wrapper.findAll('.ocard-new')).toHaveLength(1);
    expect(wrapper.findAll('.ocard')[0].classes()).toContain('is-new');
    wrapper.unmount();
  });

  it('操作后消解:点「开始制作」刷新列表,该卡「新」角标消失', async () => {
    payFirstState.value = false;
    listOrdersMock
      .mockResolvedValueOnce({ total: 1, items: [order({ orderId: 1, isNew: true })] })
      .mockResolvedValueOnce({ total: 1, items: [order({ orderId: 1, isNew: false })] });
    const wrapper = mountOrders();
    await flushPromises();
    expect(wrapper.findAll('.ocard-new')).toHaveLength(1);

    const btn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('开始制作'));
    expect(btn).toBeTruthy();
    await btn!.trigger('click');
    await flushPromises();
    expect(changeOrderStatusMock).toHaveBeenCalledWith({ orderId: 1, orderStatus: 2 });
    expect(wrapper.findAll('.ocard-new')).toHaveLength(0);
    wrapper.unmount();
  });

  it('多张新单互不影响:点开其中一张,其余新单的角标与高亮保留', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({
      total: 2,
      items: [
        order({ orderId: 1, isNew: true }),
        order({ orderId: 2, orderNo: 'NO20261017002', isNew: true })
      ]
    });
    const wrapper = mountOrders();
    await flushPromises();
    expect(wrapper.findAll('.ocard.is-new')).toHaveLength(2);
    expect(wrapper.findAll('.ocard-new')).toHaveLength(2);

    // 仅查看不消解:点第一张后,两张的角标与高亮都保留
    await wrapper.findAll('.ocard')[0].trigger('click');
    await flushPromises();
    expect(wrapper.findAll('.ocard.is-new')).toHaveLength(2);
    expect(wrapper.findAll('.ocard-new')).toHaveLength(2);
    expect(wrapper.findAll('.ocard')[1].classes()).toContain('is-new');
    wrapper.unmount();
  });

  it('筛选:移动端「新单」tab 请求带 newOnly=1,切到状态 tab 自动取消', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({ total: 0, items: [] });
    const wrapper = mountOrders();
    await flushPromises();

    // tab 顺序:全部 / 新单 / 已下单 / 制作中 / ...
    const tabs = wrapper.findAll('.m-seg .m-seg-item');
    expect(tabs.length).toBeGreaterThanOrEqual(3);
    expect(tabs[1].text()).toBe('新单');

    await tabs[1].trigger('click');
    await flushPromises();
    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ newOnly: '1' }));
    expect(tabs[1].classes()).toContain('on');

    // 切到「已下单」状态 tab:取消只看新单,回到状态筛选
    await tabs[2].trigger('click');
    await flushPromises();
    const last = listOrdersMock.mock.calls.at(-1)![0] as Record<string, unknown>;
    expect(last.newOnly).toBeUndefined();
    expect(last.orderStatus).toBe('1');
    expect(tabs[2].classes()).toContain('on');
    wrapper.unmount();
  });

  it('后付模式:卡片保持 详情 / 开始制作 / 后付款结账 / 取消订单', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({ total: 1, items: [order({ orderStatus: 1, payStatus: 0 })] });
    const wrapper = mountOrders();
    await flushPromises();
    expect(cardLabels(wrapper)).toEqual(['订单详情', '开始制作', '后付款结账', '取消订单']);
    wrapper.unmount();
  });
});

// 先付单在「确认收款」之前不会补出厨房单,等太久 = 顾客干等、后厨没单。
// 这里钉住两档提示:超时(≥5 分钟)提醒催办、严重超时(≥20 分钟)引导取消。
describe('Orders 订单管理页 · 先付新单超时提示', () => {
  it('等待 ≥5 分钟:角标「新」变「超时」,整卡转危险色并出现提示条', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(6) })]
    });
    const wrapper = mountOrders();
    await flushPromises();

    const card = wrapper.find('.ocard');
    expect(card.classes()).toContain('is-overdue');
    expect(card.classes()).not.toContain('is-stale');

    const badge = wrapper.find('.ocard-new');
    expect(badge.text()).toBe('超时');
    expect(badge.classes()).toContain('is-overdue');

    const hint = wrapper.find('.ocard-overdue');
    expect(hint.exists()).toBe(true);
    expect(hint.classes()).not.toContain('is-stale');
    expect(hint.text()).toContain('未确认收款');
    expect(hint.text()).toContain('后厨尚未出单');

    // 6 分钟还没到「可能已线下处理」的程度,取消键仍是描边
    const cancelBtn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('取消订单'));
    expect(cancelBtn!.classes()).toContain('t-button--variant-outline');
    wrapper.unmount();
  });

  it('等待 ≥20 分钟:提示升级为引导取消,取消键变实心红按钮', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(25) })]
    });
    const wrapper = mountOrders();
    await flushPromises();

    const card = wrapper.find('.ocard');
    expect(card.classes()).toContain('is-overdue');
    expect(card.classes()).toContain('is-stale');

    const hint = wrapper.find('.ocard-overdue');
    expect(hint.classes()).toContain('is-stale');
    expect(hint.text()).toContain('请点「取消订单」');

    const cancelBtn = wrapper.findAll('.ocard-ft button').find(b => (b.text() || '').includes('取消订单'));
    expect(cancelBtn!.classes()).toContain('t-button--variant-base');

    // 引导取消不等于改顺序:确认收款仍在取消之前,避免手快误点
    expect(cardLabels(wrapper)).toEqual(['订单详情', '确认收款', '取消订单']);
    wrapper.unmount();
  });

  it('刚下单(未到阈值)与已支付先付单都不标超时', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 2,
      items: [
        order({ orderId: 1, orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(2) }),
        order({
          orderId: 2,
          orderNo: 'NO20261017002',
          orderStatus: 2,
          payStatus: 1,
          isNew: true,
          createTime: timeAgo(30)
        })
      ]
    });
    const wrapper = mountOrders();
    await flushPromises();
    expect(wrapper.findAll('.ocard.is-overdue')).toHaveLength(0);
    expect(wrapper.findAll('.ocard-overdue')).toHaveLength(0);
    expect(wrapper.findAll('.ocard-new').map(b => b.text())).toEqual(['新', '新']);
    wrapper.unmount();
  });

  it('后付模式不标超时:厨房单在下单时就出了,不会因为未收款而不出菜', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(30) })]
    });
    const wrapper = mountOrders();
    await flushPromises();

    expect(wrapper.find('.ocard').classes()).not.toContain('is-overdue');
    expect(wrapper.find('.ocard-new').text()).toBe('新');
    expect(wrapper.find('.ocard-overdue').exists()).toBe(false);
    wrapper.unmount();
  });
});

// 取消是唯一不可逆的订单动作(收款/完成都还有改单兜底),弹窗里必须交代清楚
// 「会发生什么」;先付单等太久未收款时,还要说明「取消不会浪费菜品」这层业务含义。
describe('Orders 订单管理页 · 取消订单弹窗', () => {
  function openCancelDialog(wrapper: ReturnType<typeof mountOrders>) {
    return wrapper
      .findAll('.ocard-ft button')
      .find(b => (b.text() || '').includes('取消订单'))!
      .trigger('click');
  }

  it('超时未收款的先付单:弹窗含金额、取消后果与「不会浪费菜品」说明', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(25) })]
    });
    const wrapper = mountOrders({ attach: true });
    await flushPromises();

    await openCancelDialog(wrapper);
    await flushPromises();

    const box = document.querySelector('.cancel-box') as HTMLElement | null;
    expect(box).toBeTruthy();
    const text = box!.textContent || '';
    expect(text).toContain('¥28.00');
    expect(text).toContain('取消后不可恢复');
    expect(text).toContain('桌台立即释放为空闲');
    expect(text).toContain('操作日志');

    // 先付未收款 = 厨房单还没出:说明里不能出现「菜品无法撤回」这种会吓住收银的话
    expect(text).toContain('还没有出厨房单');
    expect(text).not.toContain('请先与后厨确认');

    // 超时说明与卡片同口径(≥20 分钟):红色块 + 已等待时长
    expect(box!.querySelector('.ct-hint-stale')).toBeTruthy();
    expect(text).toContain('未确认收款');
    expect(text).toContain('取消不会造成菜品浪费');
    wrapper.unmount();
  });

  it('刚下单的先付单:只给通用后果说明,不出现超时引导', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(2) })]
    });
    const wrapper = mountOrders({ attach: true });
    await flushPromises();

    await openCancelDialog(wrapper);
    await flushPromises();

    const box = document.querySelector('.cancel-box') as HTMLElement | null;
    expect(box).toBeTruthy();
    expect(box!.querySelector('.ct-hint-stale')).toBeNull();
    expect(box!.querySelector('.ct-hint-wait')).toBeNull();
    expect(box!.textContent || '').toContain('取消后不可恢复');
    expect(box!.textContent || '').toContain('还没有出厨房单');
    wrapper.unmount();
  });

  it('后付模式的取消:厨房单在下单时就出了,提示要收银先与后厨确认', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 2, payStatus: 0, createTime: timeAgo(3) })]
    });
    const wrapper = mountOrders({ attach: true });
    await flushPromises();

    await openCancelDialog(wrapper);
    await flushPromises();

    const box = document.querySelector('.cancel-box') as HTMLElement | null;
    expect(box).toBeTruthy();
    const text = box!.textContent || '';
    expect(text).toContain('后厨可能已经出单');
    expect(text).toContain('请先与后厨确认');
    expect(text).not.toContain('还没有出厨房单');
    wrapper.unmount();
  });

  it('确认取消后调用取消接口并按「商家取消」落库,随后刷新列表', async () => {
    payFirstState.value = true;
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [order({ orderStatus: 1, payStatus: 0, isNew: true, createTime: timeAgo(2) })]
    });
    const wrapper = mountOrders({ attach: true });
    await flushPromises();
    const loadsBefore = listOrdersMock.mock.calls.length;

    await openCancelDialog(wrapper);
    await flushPromises();

    // t-dialog 的确认键在 footer 里,按文案取,避开 TDesign 版本差异导致的类名变化
    const confirmBtn = Array.from(document.querySelectorAll('.t-dialog__footer button')).find(b =>
      (b.textContent || '').includes('确认取消')
    ) as HTMLElement | undefined;
    expect(confirmBtn).toBeTruthy();
    confirmBtn!.click();
    await flushPromises();

    expect(cancelOrderMock).toHaveBeenCalledWith({ orderId: 1, cancelReason: '商家取消' });
    expect(listOrdersMock.mock.calls.length).toBeGreaterThan(loadsBefore);
    wrapper.unmount();
  });
});
