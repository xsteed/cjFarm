import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Orders from '../Orders.vue';
import { finishOrder, listOrders } from '@dining/api';
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
    createTime: '2026-10-17 12:00:00',
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
function mountOrders() {
  return mount(Orders, {
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

  it('后付模式:卡片保持 详情 / 开始制作 / 后付款结账 / 取消订单', async () => {
    payFirstState.value = false;
    listOrdersMock.mockResolvedValue({ total: 1, items: [order({ orderStatus: 1, payStatus: 0 })] });
    const wrapper = mountOrders();
    await flushPromises();
    expect(cardLabels(wrapper)).toEqual(['订单详情', '开始制作', '后付款结账', '取消订单']);
    wrapper.unmount();
  });
});
