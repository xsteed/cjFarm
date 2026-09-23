import { mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import QRCode from 'qrcode';
import OrderPage from '../../OrderPage.vue';
import {
  createOrder,
  createPay,
  getMenu,
  getOrderByNo,
  getPublicConfig,
  getRemarks,
  getTable,
  urgeOrder
} from '../../../api';
import type { MenuCategory, OrderDetail, PublicConfig, RemarkOption, TableInfo } from '../../../types/entities';
import { flushPromises } from '../../../test/mocks';

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
  }
}));

vi.mock('qrcode', () => ({
  default: {
    toDataURL: vi.fn(() => Promise.resolve('data:image/png;base64,mock'))
  }
}));

vi.mock('../../../api', () => ({
  getTable: vi.fn(),
  getMenu: vi.fn(),
  getRemarks: vi.fn(),
  getPublicConfig: vi.fn(),
  createOrder: vi.fn(),
  appendOrder: vi.fn(),
  getOrderByNo: vi.fn(),
  urgeOrder: vi.fn(),
  createPay: vi.fn(),
  queryPay: vi.fn()
}));

const getTableMock = vi.mocked(getTable);
const getMenuMock = vi.mocked(getMenu);
const getRemarksMock = vi.mocked(getRemarks);
const getPublicConfigMock = vi.mocked(getPublicConfig);
const createOrderMock = vi.mocked(createOrder);
const getOrderByNoMock = vi.mocked(getOrderByNo);
const urgeOrderMock = vi.mocked(urgeOrder);
const createPayMock = vi.mocked(createPay);

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
  it('挂载后先展示 loading,再进入菜单视图', async () => {
    const wrapper = mountPage();
    expect(wrapper.text()).toContain('正在进入点餐');

    await flushPromises();
    expect(wrapper.text()).toContain('宫保鸡丁');
    expect(wrapper.text()).toContain('去结算');
    expect(getTableMock).toHaveBeenCalledWith('1');
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
    expect(wrapper.text()).toContain('已下单，后厨正在接单');

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

    expect(createOrderMock).toHaveBeenCalledWith({
      tableId: 1,
      personCount: 2,
      orderRemark: '不要辣',
      items: [{ dishId: 101, specId: 1, quantity: 1, itemRemark: '' }]
    });
    expect(wrapper.text()).toContain('已下单，后厨正在接单');
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
      expect(wrapper.text()).toContain('已下单，后厨正在接单');

      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();
      expect(getOrderByNoMock).toHaveBeenCalledWith('NO001');
      expect(wrapper.text()).toContain('餐品已上齐，请慢用');

      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('催菜成功后进入冷却倒计时,倒计时结束后恢复可点', async () => {
    vi.useFakeTimers();
    try {
      const order = makeOrder({ orderStatus: 2, payStatus: 0 });
      getTableMock.mockResolvedValue({ ...tableNoOrder, currentOrder: order });
      getOrderByNoMock.mockResolvedValue(order);

      const wrapper = mountPage();
      await drainMicrotasks();
      expect(wrapper.text()).toContain('正在制作中，请稍候');

      await wrapper.find('.st-urge').trigger('click');
      await drainMicrotasks();
      expect(urgeOrderMock).toHaveBeenCalledWith('NO001', 1, { silent: true });
      expect(wrapper.find('.st-urge').text()).toContain('s 后可再催');
      expect(wrapper.find('.st-urge').classes()).toContain('disabled');

      await vi.advanceTimersByTimeAsync(3000);
      await drainMicrotasks();
      expect(wrapper.find('.st-urge').text()).toContain('催菜');
      expect(wrapper.find('.st-urge').classes()).not.toContain('disabled');

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
});
