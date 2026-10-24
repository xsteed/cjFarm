import { mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import QRCode from 'qrcode';
import OrderPage from '../../OrderPage.vue';
import {
  createOrder,
  createPay,
  getMenu,
  getOrderByNo,
  getPublicConfig,
  getRemarks,
  getReviewStatus,
  getTable,
  reportVisit,
  setTablePerson,
  urgeOrder
} from '@dining/api';
import type { MenuCategory, OrderDetail, PublicConfig, RemarkOption, TableInfo } from '@dining/types';
import { flushPromises } from '@shell/test/mocks.ts';

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { tableId: '1' }, query: {}, hash: '', path: '/order/1', name: 'OrderPage', meta: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}));

vi.mock('tdesign-vue-next', () => ({
  MessagePlugin: {
    success: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    close: vi.fn()
  },
  // 清空购物车会先弹确认：这里记下最后一次配置，测试据此触发 onConfirm
  DialogPlugin: {
    confirm: vi.fn(() => ({ destroy: vi.fn() }))
  }
}));

vi.mock('qrcode', () => ({
  default: {
    toDataURL: vi.fn(() => Promise.resolve('data:image/png;base64,mock'))
  }
}));

vi.mock('@dining/api', () => ({
  // @dining/auth 在模块加载时会注册它(见 perm.ts 末尾),漏了会让整个套件加载失败
  onAuthCleared: vi.fn(),
  getTable: vi.fn(),
  getMenu: vi.fn(),
  getRemarks: vi.fn(),
  getPublicConfig: vi.fn(),
  createOrder: vi.fn(),
  appendOrder: vi.fn(),
  getOrderByNo: vi.fn(),
  urgeOrder: vi.fn(),
  createPay: vi.fn(),
  queryPay: vi.fn(),
  setTablePerson: vi.fn(),
  getReviewStatus: vi.fn(() => Promise.resolve({ reviewed: false })),
  // 站点访问统计打点(挂载即上报一次,失败静默)
  reportVisit: vi.fn(() => Promise.resolve())
}));

const getTableMock = vi.mocked(getTable);
const getMenuMock = vi.mocked(getMenu);
const getRemarksMock = vi.mocked(getRemarks);
const getPublicConfigMock = vi.mocked(getPublicConfig);
const createOrderMock = vi.mocked(createOrder);
const getOrderByNoMock = vi.mocked(getOrderByNo);
const urgeOrderMock = vi.mocked(urgeOrder);
const createPayMock = vi.mocked(createPay);
const reportVisitMock = vi.mocked(reportVisit);
const setTablePersonMock = vi.mocked(setTablePerson);
const getReviewStatusMock = vi.mocked(getReviewStatus);

// CartDrawer 只依赖 t-drawer 一个 TDesign 组件,用同名全局组件按 visible 渲染默认插槽即可稳定驱动内部交互。
const TDrawerStub = defineComponent({
  name: 'TDrawer',
  props: {
    visible: { type: Boolean, default: false }
  },
  setup(props, { slots }) {
    return () => (props.visible ? h('div', { class: 'tdrawer-stub' }, slots.default?.()) : null);
  }
});

const configFixture: PublicConfig = {
  shop_name: '测试餐厅',
  seat_fee_enabled: '0',
  seat_fee: '0',
  promotion_enabled: '0',
  promotion_threshold: '0',
  promotion_discount: '0',
  pay_qr_wx: '',
  pay_qr_ali: '',
  wxpay_enabled: '1',
  alipay_enabled: '1'
};

const tableNoOrder: TableInfo = { tableId: 1, tableNo: 'A1', tableName: 'A1', capacity: 4 };

const menuFixture: MenuCategory[] = [
  {
    categoryId: 1,
    categoryName: '热菜',
    dishes: [
      {
        dishId: 101,
        dishName: '宫保鸡丁',
        description: '微辣',
        specs: [
          { specId: 1, specName: '小份', price: 28 },
          { specId: 2, specName: '大份', price: 38 }
        ]
      },
      {
        dishId: 102,
        dishName: '米饭',
        specs: [{ specId: 3, specName: '碗', price: 2 }]
      }
    ]
  }
];

const remarksFixture: RemarkOption[] = [{ remarkId: 1, optionName: '不要辣' }];

function makeOrder(overrides: Partial<OrderDetail> = {}): OrderDetail {
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

function mountPage() {
  return mount(OrderPage, {
    global: {
      // t-drawer 未在测试里 app.use(TDesign),用同名全局组件承接,按 visible 渲染默认插槽。
      components: { TDrawer: TDrawerStub },
      // 组件 submit 失败路径无 catch,失败会经 Vue 事件处理器上报;测试里吞掉避免噪声。
      config: { errorHandler: () => undefined }
    }
  });
}

// 假定时器下 flushPromises(setTimeout) 不会推进,用微任务排空模拟 load/pollOnce 的异步链。
async function drainMicrotasks() {
  for (let i = 0; i < 10; i++) {
    await Promise.resolve();
  }
}

beforeEach(() => {
  // 默认把本桌那笔未结账订单当作「顾客自己刚点的」:本文件既有用例测的都是本人订单页。
  // 归属确认另有一组用例专门覆盖(见文末「未结账订单归属确认」)。
  sessionStorage.clear();
  sessionStorage.setItem('dining:mine:1:NO001', '1');
  getTableMock.mockResolvedValue(tableNoOrder);
  getMenuMock.mockResolvedValue(menuFixture);
  getRemarksMock.mockResolvedValue(remarksFixture);
  getPublicConfigMock.mockResolvedValue(configFixture);
  getOrderByNoMock.mockResolvedValue(makeOrder());
  urgeOrderMock.mockResolvedValue({ orderNo: 'NO001', cooldown: 3 });
  createOrderMock.mockResolvedValue({ orderNo: 'NO001', orderId: 1, shortNo: '001' });
  createPayMock.mockResolvedValue({ codeUrl: 'weixin://wxpay/mock' });
});

describe('OrderPage', () => {
  it('挂载即上报一次访问统计（h5 渠道，带桌台码）', async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(reportVisitMock).toHaveBeenCalledTimes(1);
    const payload = reportVisitMock.mock.calls[0][0] as { channel?: string; entry?: string; visitorId?: string };
    expect(payload.channel).toBe('h5');
    expect(payload.entry).toBe('1');
    expect(payload.visitorId).toBeTruthy();
    wrapper.unmount();
  });

  it('挂载后先展示 loading,再进入菜单视图', async () => {
    const wrapper = mountPage();
    expect(wrapper.find('.sk-wrap').exists()).toBe(true);

    await flushPromises();
    expect(wrapper.text()).toContain('宫保鸡丁');
    // 一道菜都还没点时结算栏不出现,避免首屏一条空栏压住菜单
    expect(wrapper.find('.op-checkout').exists()).toBe(false);
    expect(getTableMock).toHaveBeenCalledWith('1');
    wrapper.unmount();
  });

  it('清空购物车先弹二次确认:点了「清空」不直接清,确认后才清', async () => {
    const wrapper = mountPage();
    await flushPromises();

    // 加一道菜，打开购物车抽屉
    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    await nextTick();
    expect(wrapper.find('.clear-btn').exists()).toBe(true);

    await wrapper.find('.clear-btn').trigger('click');
    const dialog = vi.mocked(DialogPlugin.confirm);
    expect(dialog).toHaveBeenCalledTimes(1);
    // 还没确认：菜应该还在（角标 1）
    expect(wrapper.find('.op-cartbtn .b').text()).toBe('1');

    const opts = dialog.mock.calls[0][0] as { onConfirm?: () => void };
    opts.onConfirm?.();
    await nextTick();
    expect(wrapper.find('.op-cartbtn .b').exists()).toBe(false);
    wrapper.unmount();
  });

  it('底部结算栏整条都能展开购物车:点金额区(非图标)也能打开抽屉', async () => {
    const wrapper = mountPage();
    await flushPromises();

    // 加一道菜,让购物车非空
    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await nextTick();

    // 点「合计 ¥xx」这一块(图标与去结算按钮之外),抽屉应当展开
    await wrapper.find('.op-total').trigger('click');
    await nextTick();
    expect(wrapper.find('.clear-btn').exists()).toBe(true);
    wrapper.unmount();
  });

  it('结算栏按钮文案随购物车状态切换:选菜后「去结算」,清空后回到「请选择菜品」', async () => {
    const wrapper = mountPage();
    await flushPromises();
    // 从未点过菜:结算栏还没出现
    expect(wrapper.find('.op-checkout').exists()).toBe(false);

    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await nextTick();
    expect(wrapper.find('.op-checkout').text()).toBe('去结算');

    // 清空后结算栏常驻,文案回到空车提示(与小程序 cart-bar 一致)
    await wrapper.find('.op-cartarea').trigger('click');
    await nextTick();
    await wrapper.find('.clear-btn').trigger('click');
    const opts = vi.mocked(DialogPlugin.confirm).mock.calls[0][0] as { onConfirm?: () => void };
    opts.onConfirm?.();
    await nextTick();
    expect(wrapper.find('.op-checkout').text()).toBe('请选择菜品');
    wrapper.unmount();
  });

  it('清空购物车后仍能展开抽屉:空车热区点击也打开,并展示空态', async () => {
    const wrapper = mountPage();
    await flushPromises();

    // 加菜 → 打开抽屉 → 清空(确认弹窗)后抽屉保持空态;此时关掉再点结算栏,应能重新展开
    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await nextTick();
    await wrapper.find('.op-cartarea').trigger('click');
    await nextTick();

    await wrapper.find('.clear-btn').trigger('click');
    const dialog = vi.mocked(DialogPlugin.confirm);
    const opts = dialog.mock.calls[0][0] as { onConfirm?: () => void };
    opts.onConfirm?.();
    await nextTick();
    // 清空后抽屉自动收起
    expect(wrapper.find('.tdrawer-stub').exists()).toBe(false);

    // 收起后,空车状态下点金额区热区仍应能重新展开并展示空态
    await wrapper.find('.op-cartarea').trigger('click');
    await nextTick();
    expect(wrapper.text()).toContain('购物车空空如也');
    wrapper.unmount();
  });

  it('底部导航常驻:还没下单也能进订单页看空态', async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.op-tabbar').exists()).toBe(true);
    await wrapper.findAll('.tb-item')[1].trigger('click'); // 订单
    expect(wrapper.text()).toContain('上桌啦，点起来吧');
    expect(wrapper.text()).toContain('去点餐');
    wrapper.unmount();
  });

  it('搜索:点 hero 搜索按钮进独立搜索页,输入过滤结果,收起后回到菜单', async () => {
    const wrapper = mountPage();
    await flushPromises();

    // 装饰伪元素曾拦住按钮点击,这里钉住入口可达
    await wrapper.find('.op-searchbtn').trigger('click');
    await nextTick();
    expect(wrapper.find('.search-layer').exists()).toBe(true);
    expect(wrapper.find('.menu-sheet').exists()).toBe(false);

    // 输入关键词:命中菜名
    const input = wrapper.find('.search-input');
    (input.element as HTMLInputElement).value = '宫保';
    await input.trigger('input');
    await nextTick();
    expect(wrapper.text()).toContain('宫保鸡丁');

    // 清词后 × 收起搜索页,回到菜单(左栏恢复)
    (input.element as HTMLInputElement).value = '';
    await input.trigger('input');
    await nextTick();
    await wrapper.find('.search-clear').trigger('click');
    await nextTick();
    expect(wrapper.find('.search-layer').exists()).toBe(false);
    expect(wrapper.find('.menu-sheet').exists()).toBe(true);
    expect(wrapper.find('.op-side').exists()).toBe(true);
    wrapper.unmount();
  });

  it('getTable 失败展示错误视图,点击重试后恢复菜单', async () => {
    getTableMock.mockRejectedValueOnce({ message: '桌号不存在' });

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('桌号不存在，请重新扫描餐桌上的二维码');

    await wrapper.find('.retry-btn').trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('宫保鸡丁');
    wrapper.unmount();
  });

  it('桌台被停用/预约时不进菜单,展示锁定提示', async () => {
    getTableMock.mockResolvedValue({ ...tableNoOrder, lockStatus: 2 });

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('该桌台已被预订，如需用餐请联系店员');
    // 锁定态不渲染菜单与结算栏,顾客点不出菜品
    expect(wrapper.find('.op-body').exists()).toBe(false);
    expect(wrapper.find('.op-checkout').exists()).toBe(false);

    // 店员改回「正常」后,顾客点「刷新状态」即可正常点餐
    getTableMock.mockResolvedValue(tableNoOrder);
    await wrapper.find('.retry-btn').trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('宫保鸡丁');
    wrapper.unmount();
  });

  it('菜单为空数组时不渲染分类和菜品', async () => {
    getMenuMock.mockResolvedValue([]);

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find('.op-body').exists()).toBe(true);
    expect(wrapper.findAll('.op-cat')).toHaveLength(0);
    expect(wrapper.findAll('.op-dish')).toHaveLength(0);
    wrapper.unmount();
  });

  it('分类下无菜品时展示暂无菜品空态', async () => {
    getMenuMock.mockResolvedValue([{ categoryId: 1, categoryName: '热菜', dishes: [] }]);

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('暂无菜品');
    wrapper.unmount();
  });

  it('切换规格后加菜,结算栏金额更新', async () => {
    const wrapper = mountPage();
    await flushPromises();

    const bigSpec = wrapper.findAll('.op-spec').find(s => s.text() === '大份');
    if (!bigSpec) throw new Error('未找到大份规格');
    await bigSpec.trigger('click');
    await wrapper.findAll('.op-add')[0].trigger('click');

    expect(wrapper.find('.op-total .v').text()).toContain('38');
    wrapper.unmount();
  });

  it('加菜时菜单显示已点份数标记', async () => {
    const activeOrder = makeOrder({
      orderStatus: 1,
      payStatus: 0,
      items: [
        {
          itemId: 1,
          dishId: 101,
          dishName: '宫保鸡丁',
          specId: 1,
          specName: '小份',
          quantity: 2,
          price: 28,
          amount: 56
        }
      ]
    });
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: activeOrder });

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('已下单');

    await wrapper.find('.st-append').trigger('click');
    expect(wrapper.find('.op-ordered').text()).toBe('已点 2');
    wrapper.unmount();
  });

  it('减到 0 时移除菜品,结算栏归零', async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll('.op-add')[0].trigger('click');
    expect(wrapper.find('.op-stepper').exists()).toBe(true);
    expect(wrapper.find('.op-total .v').text()).toContain('28');

    await wrapper.find('.op-stepper .m').trigger('click');
    expect(wrapper.find('.op-stepper').exists()).toBe(false);
    expect(wrapper.findAll('.op-add')).toHaveLength(2);
    expect(wrapper.find('.op-cartbtn .b').exists()).toBe(false);
    wrapper.unmount();
  });

  it('下单成功后切换到订单视图', async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll('.op-add')[0].trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    expect(wrapper.find('.cart-sheet').exists()).toBe(true);

    await wrapper.find('.dine-form .qty-btn.plus').trigger('click');
    await wrapper.find('.remark-input').setValue('不要辣');
    await wrapper.find('.submit-btn').trigger('click');
    await flushPromises();

    expect(createOrderMock).toHaveBeenCalledWith(
      expect.objectContaining({
        tableId: 1,
        personCount: 2,
        orderRemark: '不要辣',
        items: [{ dishId: 101, specId: 1, quantity: 1, itemRemark: '' }]
      })
    );
    expect(wrapper.text()).toContain('已下单');
    wrapper.unmount();
  });

  it('先付模式：下单成功后落到订单明细页，并由待付款引导去支付', async () => {
    getPublicConfigMock.mockResolvedValue({ ...configFixture, pay_first_enabled: '1' });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll('.op-add')[0].trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    await wrapper.find('.submit-btn').trigger('click');
    await flushPromises();

    // 落点是订单明细页：顾客要能看见自己点了什么、多少钱
    expect(wrapper.find('.pay-wrap').exists()).toBe(false);
    expect(wrapper.find('.st-paybtn').exists()).toBe(true);
    // 待付款引导必须点明「先付模式」与动作，且与「去支付」按钮同时出现
    const tip = wrapper.find('.st-payfirst-tip');
    expect(tip.exists()).toBe(true);
    expect(tip.text()).toContain('先付模式');
    expect(tip.text()).toContain('去支付');
    // 金额不写在引导里：账单末行已给出，重复出现反而像两笔账
    expect(tip.text()).not.toContain('¥');
    // 下单成功的 toast 不能再宣称「大厨已收到」—— 先付模式下后厨还没收到这颗单
    expect(MessagePlugin.success).toHaveBeenCalledWith(expect.stringContaining('还没付款'));
    wrapper.unmount();
  });

  it('下单失败时停留在抽屉并提示', async () => {
    createOrderMock.mockImplementation(() => {
      MessagePlugin.error('下单失败，请重试');
      return Promise.reject(new Error('下单失败，请重试'));
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll('.op-add')[0].trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    await wrapper.find('.submit-btn').trigger('click');
    await flushPromises();

    expect(MessagePlugin.error).toHaveBeenCalled();
    expect(wrapper.find('.cart-sheet').exists()).toBe(true);
    expect(wrapper.find('.submit-btn').attributes('disabled')).toBeUndefined();
    wrapper.unmount();
  });

  it('进入订单视图后每 3 秒轮询订单状态并推进视图', async () => {
    vi.useFakeTimers();
    try {
      const initial = makeOrder({ orderStatus: 1 });
      const updated = makeOrder({ orderStatus: 3 });
      getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: initial });
      getOrderByNoMock.mockResolvedValue(updated);

      const wrapper = mountPage();
      await drainMicrotasks();
      expect(wrapper.text()).toContain('已下单');

      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();
      expect(getOrderByNoMock).toHaveBeenCalledWith('NO001', 1);
      expect(wrapper.text()).toContain('已上齐');

      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('轮询发现刚支付成功时弹一次成功卡,1.8s 后自动收起', async () => {
    vi.useFakeTimers();
    try {
      const unpaid = makeOrder({ orderStatus: 2, payStatus: 0 });
      const paid = makeOrder({ orderStatus: 2, payStatus: 1, payType: '微信', payTime: '2026-10-09 12:03:07' });
      getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: unpaid });
      getOrderByNoMock.mockResolvedValue(paid);

      const wrapper = mountPage();
      await drainMicrotasks();
      expect(wrapper.find('.pd-card').exists()).toBe(false);

      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();

      // 成功卡:金额 + 渠道,并且订单已切到已结账视图
      expect(wrapper.find('.pd-card').exists()).toBe(true);
      expect(wrapper.find('.pd-amt').text()).toContain('28.00');
      expect(wrapper.text()).toContain('微信支付');
      expect(wrapper.text()).toContain('已结账，后厨正在做啦');

      await vi.advanceTimersByTimeAsync(1800);
      expect(wrapper.find('.pd-card').exists()).toBe(false);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('进入已结账订单(刷新进来)不重复弹成功卡', async () => {
    vi.useFakeTimers();
    try {
      const paid = makeOrder({ orderStatus: 2, payStatus: 1, payType: '微信' });
      getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: paid });
      getOrderByNoMock.mockResolvedValue(paid);

      const wrapper = mountPage();
      await drainMicrotasks();
      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();

      expect(wrapper.find('.pd-card').exists()).toBe(false);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('催菜成功后收起链接只留安抚文案,冷却结束后链接自动恢复', async () => {
    vi.useFakeTimers();
    try {
      const order = makeOrder({ orderStatus: 2, payStatus: 0 });
      getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: order });
      getOrderByNoMock.mockResolvedValue(order);

      const wrapper = mountPage();
      await drainMicrotasks();
      expect(wrapper.text()).toContain('制作中');

      await wrapper.find('.st-urge').trigger('click');
      await drainMicrotasks();
      expect(urgeOrderMock).toHaveBeenCalledWith('NO001', 1, { silent: true });
      // 冷却中链接收起,只留一句安抚文案:不给倒计时,免得顾客盯着秒数等下一次催
      expect(wrapper.find('.st-urge').exists()).toBe(false);
      expect(wrapper.find('.st-urge-line').text()).toContain('已通知后厨加急');

      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();
      // 冷却结束后链接自己回来,文案回到「催一下后厨」
      expect(wrapper.find('.st-urge').text()).toBe('催一下后厨');

      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('在线支付生成二维码并展示', async () => {
    const order = makeOrder({ orderStatus: 3, payStatus: 0, totalAmount: 28 });
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: order });
    getOrderByNoMock.mockResolvedValue(order);
    vi.mocked(QRCode.toDataURL).mockResolvedValue('data:image/png;base64,mock');

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('去支付');

    await wrapper.find('.st-paybtn').trigger('click');
    expect(wrapper.text()).toContain('在线支付');

    await wrapper.find('.online-pay-btn').trigger('click');
    await flushPromises();

    expect(createPayMock).toHaveBeenCalledWith({ orderNo: 'NO001', channel: 'wxpay' });
    expect(wrapper.find('.pay-qr img').attributes('src')).toBe('data:image/png;base64,mock');
    wrapper.unmount();
  });

  it('从出餐中列表点进已付款订单：直接进「已结账」，评价状态当场就问，不等这一拍轮询', async () => {
    vi.useFakeTimers();
    try {
      getTableMock.mockResolvedValue({
        ...tableNoOrder,
        servingOrders: [{ orderId: 9, orderNo: 'NO009', shortNo: '009', personCount: 5, orderStatus: 2, payStatus: 1 }]
      });
      getOrderByNoMock.mockResolvedValue(makeOrder({ orderNo: 'NO009', shortNo: '009', orderStatus: 2, payStatus: 1 }));

      const wrapper = mountPage();
      await drainMicrotasks();
      await wrapper.findAll('.tb-item')[1].trigger('click');
      await wrapper.find('.sl-item').trigger('click');
      await drainMicrotasks();

      // 一点定时器都不推:视图已是「已结账」,评价状态也已经查过
      expect(wrapper.text()).toContain('已结账');
      const reviewCall = getReviewStatusMock.mock.calls[0] ?? [];
      expect(reviewCall[0]).toBe('NO009');
      expect(reviewCall[1]).toBe(1);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('切回订单 Tab 时立刻补一拍，不等 3s 的第一次轮询', async () => {
    const order = makeOrder({ orderStatus: 2, payStatus: 0 });
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: order });
    getOrderByNoMock.mockResolvedValue(order);

    const wrapper = mountPage();
    await flushPromises();
    getOrderByNoMock.mockClear();

    // 点餐 → 订单：手上的订单可能是几分钟前的快照,切回来必须马上刷一次
    await wrapper.findAll('.tb-item')[0].trigger('click');
    await nextTick();
    await wrapper.findAll('.tb-item')[1].trigger('click');
    await drainMicrotasks();

    expect(getOrderByNoMock).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it('菜单请求还没回来时，订单卡已经先渲染出来', async () => {
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: makeOrder() });
    getOrderByNoMock.mockResolvedValue(makeOrder());
    // 菜单(最大的那份包)挂住不返回:订单卡不该陪它一起等
    let releaseMenu: (v: MenuCategory[]) => void = () => {};
    getMenuMock.mockReturnValue(
      new Promise<MenuCategory[]>(resolve => {
        releaseMenu = resolve;
      })
    );

    const wrapper = mountPage();
    await drainMicrotasks();

    expect(wrapper.find('.sk-wrap').exists()).toBe(false);
    expect(wrapper.find('.st-card').exists()).toBe(true);
    expect(wrapper.text()).toContain('菜品明细');

    // 菜单补齐后正常进数据,不把已渲染的卡顶掉
    releaseMenu(menuFixture);
    await drainMicrotasks();
    expect(wrapper.find('.st-card').exists()).toBe(true);
    wrapper.unmount();
  });
});

// 店家忘了点「收款」时，桌台会一直挂着一笔下过单未结账的订单。下一桌扫码如果不问
// 一句就进订单页，等于把上一桌的账单直接摊给新客人 —— 这组用例守住这道确认。
describe('未结账订单归属确认', () => {
  const tableWithOrder: TableInfo = { ...tableNoOrder, currentOrder: makeOrder({ orderStatus: 3 }) };

  /** 新客人场景：没认领过任何订单（beforeEach 里的认领标记在用例内清掉）。 */
  function asNewGuest(): void {
    sessionStorage.clear();
  }

  it('本桌有未结账订单时，先让顾客认领，不直接进订单页', async () => {
    asNewGuest();
    getTableMock.mockResolvedValue(tableWithOrder);

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain('本桌已有一笔未结账的订单');
    expect(wrapper.text()).toContain('是我的，继续');
    // 没认领前不该看到订单的操作入口（去支付 / 加菜）
    expect(wrapper.find('.st-paybtn').exists()).toBe(false);
    wrapper.unmount();
  });

  it('点「是我的」后进订单页，且同一会话内重进不再重复问', async () => {
    asNewGuest();
    const order = makeOrder({ orderStatus: 3 });
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: order });
    getOrderByNoMock.mockResolvedValue(order);

    const wrapper = mountPage();
    await flushPromises();
    await wrapper.findAll('.resume-btn')[0].trigger('click');
    await flushPromises();
    expect(wrapper.find('.st-paybtn').exists()).toBe(true);
    wrapper.unmount();

    // 认领结果记在会话里：重新进入不应再回到确认页
    const again = mountPage();
    await flushPromises();
    expect(again.text()).not.toContain('本桌已有一笔未结账的订单');
    expect(again.find('.st-paybtn').exists()).toBe(true);
    again.unmount();
  });

  it('点「不是我的订单」后给出等服务员清台的出口', async () => {
    asNewGuest();
    getTableMock.mockResolvedValue(tableWithOrder);

    const wrapper = mountPage();
    await flushPromises();
    await wrapper.findAll('.resume-btn')[1].trigger('click');
    await flushPromises();

    expect(wrapper.text()).toContain('上一笔订单还未结账，服务员清台后即可点餐');
    expect(wrapper.text()).not.toContain('去支付');
    wrapper.unmount();
  });

  it('已付款的订单只看出餐进度，不需要认领', async () => {
    asNewGuest();
    const paid = makeOrder({ orderStatus: 3, payStatus: 1 });
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: paid });

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).not.toContain('本桌已有一笔未结账的订单');
    wrapper.unmount();
  });
});

// 先付模式下「本单还没付款」是顾客必须马上处理的事：后厨压根没收到厨单。
// 但下单后的落点仍是订单明细页 —— 顾客得先看清自己点了什么、多少钱，
// 再由页面上的待付款引导（点明先付模式 + 指向「去支付」按钮）带他去付款。
describe('先付模式 · 页面落点与待付款引导', () => {
  const payFirstConfig: PublicConfig = { ...configFixture, pay_first_enabled: '1' };
  const unpaidOrder = () => makeOrder({ orderStatus: 1, payStatus: 0 });

  it('已认领且未支付：落订单明细页，并给出待付款引导', async () => {
    getPublicConfigMock.mockResolvedValue(payFirstConfig);
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: unpaidOrder() });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.pay-wrap').exists()).toBe(false);
    const tip = wrapper.find('.st-payfirst-tip');
    expect(tip.exists()).toBe(true);
    expect(tip.text()).toContain('去支付');
    wrapper.unmount();
  });

  it('未认领：先确认归属，认领后同样落订单明细页', async () => {
    sessionStorage.clear();
    getPublicConfigMock.mockResolvedValue(payFirstConfig);
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: unpaidOrder() });

    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).toContain('本桌已有一笔未结账的订单');

    await wrapper.findAll('.resume-btn')[0].trigger('click');
    await flushPromises();
    expect(wrapper.find('.pay-wrap').exists()).toBe(false);
    expect(wrapper.find('.st-payfirst-tip').exists()).toBe(true);
    wrapper.unmount();
  });

  it('后付（默认）：同样场景仍落订单页，行为不变', async () => {
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: unpaidOrder() });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.pay-wrap').exists()).toBe(false);
    expect(wrapper.find('.st-paybtn').exists()).toBe(true);
    wrapper.unmount();
  });

  it('先付但已支付：进已结账视图看出餐进度，不再催付款', async () => {
    sessionStorage.clear();
    getPublicConfigMock.mockResolvedValue(payFirstConfig);
    getTableMock.mockResolvedValue({
      ...tableNoOrder,
      currentOrder: makeOrder({ orderStatus: 2, payStatus: 1, payType: '微信' })
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.pay-wrap').exists()).toBe(false);
    expect(wrapper.find('.st-payfirst-tip').exists()).toBe(false);
    wrapper.unmount();
  });
});

// 茶位费按人数算，人数默认 1 会让多人桌少收。这组用例守住「入座先选人数」：
// 只在「茶位费开启 + 新桌未选过」时弹；菜单必须先渲染（蒙层不是白板）；
// 不预选；同桌共享（pendingPersonCount 沿用 / 确认后上报）。
describe('入座选人数（茶位费门店）', () => {
  const seatFeeConfig: PublicConfig = { ...configFixture, seat_fee_enabled: '1', seat_fee: '2' };

  beforeEach(() => {
    sessionStorage.clear();
    getTableMock.mockResolvedValue(tableNoOrder);
    getPublicConfigMock.mockResolvedValue(seatFeeConfig);
    setTablePersonMock.mockResolvedValue(undefined);
  });

  it('茶位费开启 + 新桌未选过：菜单照常渲染，面板盖在上面且未选不可确认', async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(true);
    // 菜单已渲染：顾客扫码后看到的是「菜单 + 蒙层」，不是白板
    expect(wrapper.text()).toContain('宫保鸡丁');
    const confirm = wrapper.find('.pg-confirm');
    expect(confirm.exists()).toBe(true);
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true);
    wrapper.unmount();
  });

  it('选 4 人确认：面板收起、人数生效、上报桌级共享', async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll('.pg-cell')[3].trigger('click'); // 4
    await wrapper.find('.pg-confirm').trigger('click');
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    expect(setTablePersonMock).toHaveBeenCalledWith({ tableId: 1, personCount: 4 });
    // 合计含 4 人茶位费：加一道 28 元的菜 → 28 + 4×2 = 36
    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    await nextTick();
    expect(wrapper.text()).toContain('茶位费（4人）');
    wrapper.unmount();
  });

  it('点数字直接输入：覆盖 11~20 的桌况', async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('.pg-num').setValue('12');
    await wrapper.find('.pg-confirm').trigger('click');
    await flushPromises();

    expect(setTablePersonMock).toHaveBeenCalledWith({ tableId: 1, personCount: 12 });
    wrapper.unmount();
  });

  it('茶位费关闭：不弹面板，行为与改造前一致', async () => {
    getPublicConfigMock.mockResolvedValue(configFixture);
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    wrapper.unmount();
  });

  it('同桌其他设备已选过（pendingPersonCount）：直接沿用，不弹面板', async () => {
    getTableMock.mockResolvedValue({ ...tableNoOrder, pendingPersonCount: 6 });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    wrapper.unmount();
  });

  it('本会话已选过（刷新重进）：不弹面板且人数恢复', async () => {
    sessionStorage.setItem('dining:person:1', '5');
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    wrapper.unmount();
  });

  it('本桌已有未结账订单：不弹面板（人数以订单为准）', async () => {
    sessionStorage.clear();
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: makeOrder() });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    wrapper.unmount();
  });

  it('本桌有已付款出餐中的订单（茶位费已收过）：不弹面板，人数沿最近一笔', async () => {
    getTableMock.mockResolvedValue({
      ...tableNoOrder,
      servingOrders: [{ orderId: 9, orderNo: 'NO009', shortNo: '009', personCount: 5, orderStatus: 2 }]
    });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('.person-mask').exists()).toBe(false);
    // 出餐提示条：文字与订单标签在同一行（标签区可横向滑动）
    expect(wrapper.find('.serving-banner').exists()).toBe(true);
    expect(wrapper.find('.serving-label').text()).toContain('1 笔订单正在出餐');

    // 人数沿用最近一笔已付款订单：加一道 28 元的菜 → 28 + 5×2 = 38
    await wrapper.findAll('.op-dish')[0].find('.op-add').trigger('click');
    await wrapper.find('.op-checkout').trigger('click');
    await nextTick();
    expect(wrapper.text()).toContain('茶位费（5人）');
    wrapper.unmount();
  });

  it('订单 Tab 列出出餐中的订单，点一条可看进度（不再是空态）', async () => {
    getTableMock.mockResolvedValue({
      ...tableNoOrder,
      servingOrders: [
        {
          orderId: 9,
          orderNo: 'NO009',
          shortNo: '009',
          personCount: 5,
          orderStatus: 2,
          payStatus: 1,
          totalAmount: 92,
          createTime: '2026-10-25 12:03:00',
          items: [
            { itemId: 1, dishId: 101, dishName: '宫保鸡丁', quantity: 1, amount: 38 },
            { itemId: 2, dishId: 102, dishName: '干煸四季豆', quantity: 2, amount: 44 },
            { itemId: 3, dishId: 103, dishName: '米饭', quantity: 2, amount: 14 }
          ]
        }
      ]
    });
    getOrderByNoMock.mockResolvedValue(
      makeOrder({ orderNo: 'NO009', shortNo: '009', orderStatus: 2, payStatus: 1 })
    );
    const wrapper = mountPage();
    await flushPromises();

    // 切到订单 Tab：列表而不是「还没有进行中的订单」
    await wrapper.findAll('.tb-item')[1].trigger('click');
    await nextTick();
    expect(wrapper.find('.serving-list').exists()).toBe(true);
    expect(wrapper.find('.order-empty').exists()).toBe(false);
    expect(wrapper.text()).toContain('本桌正在出餐');

    // 卡片自带摘要:取餐号 + 状态 + 菜品 + 时间人数 + 金额,
    // 顾客在列表页就能认出「这是我的单、钱付过了」,不必逐条点进去核对
    const card = wrapper.find('.sl-item');
    expect(card.find('.sl-no').text()).toBe('#009');
    expect(card.find('.sl-tag').text()).toBe('制作中');
    expect(card.find('.sl-dishes').text()).toBe('宫保鸡丁、干煸四季豆 等 3 道');
    expect(card.find('.sl-meta').text()).toBe('12:03 下单 · 5 人 · 共 5 件');
    expect(card.find('.sl-amt').text()).toBe('¥92.00');

    // 点一条：回拉详情后进入进度视图
    await wrapper.find('.sl-item').trigger('click');
    await flushPromises();
    expect(getOrderByNoMock).toHaveBeenCalledWith('NO009', 1);
    expect(wrapper.find('.serving-list').exists()).toBe(false);
    expect(wrapper.text()).toContain('制作中');
    wrapper.unmount();
  });

  it('订单详情左上角有返回：餐中订单退回列表，自己的订单回点餐页', async () => {
    getTableMock.mockResolvedValue({
      ...tableNoOrder,
      servingOrders: [{ orderId: 9, orderNo: 'NO009', shortNo: '009', personCount: 5, orderStatus: 2, payStatus: 1 }]
    });
    getOrderByNoMock.mockResolvedValue(
      makeOrder({ orderNo: 'NO009', shortNo: '009', orderStatus: 2, payStatus: 1 })
    );
    const wrapper = mountPage();
    await flushPromises();

    // 菜单视图不显示返回按钮
    expect(wrapper.find('.op-backbtn').exists()).toBe(false);

    // 订单 Tab → 列表 → 点一条进详情：返回按钮出现
    await wrapper.findAll('.tb-item')[1].trigger('click');
    await wrapper.find('.sl-item').trigger('click');
    await flushPromises();
    expect(wrapper.find('.op-backbtn').exists()).toBe(true);

    // 餐中订单：返回 → 回列表（不是回菜单）
    await wrapper.find('.op-backbtn').trigger('click');
    await nextTick();
    expect(wrapper.find('.serving-list').exists()).toBe(true);

    wrapper.unmount();

    // 自己的进行中订单：返回 → 回点餐页
    sessionStorage.setItem('dining:mine:1:NO001', '1'); // 认领过：直接进订单页
    getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: makeOrder() });
    getOrderByNoMock.mockResolvedValue(makeOrder());
    const own = mountPage();
    await flushPromises();
    expect(own.find('.op-backbtn').exists()).toBe(true);
    await own.find('.op-backbtn').trigger('click');
    await nextTick();
    // 回点餐页：菜单的搜索入口出现，订单明细卡消失
    expect(own.find('.op-searchbtn').exists()).toBe(true);
    expect(own.find('.st-card').exists()).toBe(false);
    own.unmount();
  });

  it('返回按钮：轮询把餐中订单推到「已结账」后再返回，仍回出餐中列表而不是空白页', async () => {
    vi.useFakeTimers();
    try {
      getTableMock.mockResolvedValue({
        ...tableNoOrder,
        servingOrders: [{ orderId: 9, orderNo: 'NO009', shortNo: '009', personCount: 5, orderStatus: 2, payStatus: 1 }]
      });
      getOrderByNoMock.mockResolvedValue(makeOrder({ orderNo: 'NO009', shortNo: '009', orderStatus: 2, payStatus: 1 }));
      const wrapper = mountPage();
      await drainMicrotasks();

      // 订单 Tab → 列表 → 点一条进详情
      await wrapper.findAll('.tb-item')[1].trigger('click');
      await wrapper.find('.sl-item').trigger('click');
      await drainMicrotasks();
      expect(wrapper.find('.op-backbtn').exists()).toBe(true);

      // 一拍轮询：已付款且仍在出餐 → 视图被推到 settled(3s 内必然发生)
      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();
      expect(wrapper.text()).toContain('已结账');

      await wrapper.find('.op-backbtn').trigger('click');
      await nextTick();

      // 关键：不能只剩一个头部。列表里这条单还在出餐,应当回到列表
      expect(wrapper.find('.serving-list').exists()).toBe(true);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

});
