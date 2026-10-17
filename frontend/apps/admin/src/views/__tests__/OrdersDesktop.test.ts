import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Orders from '../Orders.vue';
import { listOrders } from '@dining/api';
import { setAuth } from '@dining/auth';
import { flushPromises, clearAllStorage } from '@shell/test/mocks.ts';
import type { OrderSummary } from '@dining/types';

// 桌面端场景:useIsMobile 是模块级单例(见 utils/useMobile.ts),与 Orders.test.ts
// (移动端)分开文件,保证本文件首次挂载时断点已按桌面同步。
// 与 Orders.vue 共享同一个 @dining/api 模块,统一 mock 全部导出,
// 避免真实 http(axios) 被加载或发起请求。
vi.mock('@dining/api', () => ({
  // @dining/auth 在模块加载时会注册它(见 perm.ts 末尾),漏了会让整个套件加载失败
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn(),
  listOrders: vi.fn(),
  changeOrderStatus: vi.fn(),
  cancelOrder: vi.fn(),
  finishOrder: vi.fn(),
  handleUrge: vi.fn(),
  getPublicConfig: vi.fn(() => Promise.resolve({ pay_first_enabled: '0' })),
  ORDER_STATUS: {
    1: { label: '已下单', theme: 'primary' },
    2: { label: '制作中', theme: 'warning' },
    3: { label: '已上齐', theme: 'success' },
    4: { label: '已完成', theme: 'success' },
    5: { label: '已取消', theme: 'danger' }
  }
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
      setup(props) {
        return () => (props.visible ? h('div', { class: 'order-detail-stub' }) : null);
      }
    })
  };
});

const iconStubs = {
  'chevron-left-icon': { template: '<i />' },
  'chevron-right-icon': { template: '<i />' },
  'search-icon': { template: '<i />' },
  'filter-icon': { template: '<i />' },
  'refresh-icon': { template: '<i />' }
};

function oneOrder(): OrderSummary[] {
  return [
    {
      orderId: 1,
      orderNo: 'NO1-000001',
      shortNo: '1',
      orderStatus: 1,
      payStatus: 0,
      totalAmount: 100,
      tableNo: '01',
      tableName: '大厅01桌'
    }
  ];
}

const listOrdersMock = vi.mocked(listOrders);

let restoreMatchMedia: () => void;

beforeEach(() => {
  // 桌面端断点:全部媒体查询都不命中
  const original = window.matchMedia;
  window.matchMedia = vi.fn().mockImplementation((query: string): MediaQueryList => {
    return {
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => true)
    } as MediaQueryList;
  });
  restoreMatchMedia = () => {
    window.matchMedia = original;
  };

  clearAllStorage();
  setAuth({ token: 't', username: 'u', perms: ['order:view', 'order:operate', 'order:settle', 'order:cancel'] });

  listOrdersMock.mockResolvedValue({ total: 1, items: oneOrder() });
});

afterEach(() => {
  restoreMatchMedia();
});

function mountOrders() {
  return mount(Orders, {
    global: {
      plugins: [TDesign],
      stubs: { teleport: true, ...iconStubs },
      config: { errorHandler: () => undefined }
    }
  });
}

describe('Orders 订单管理页 · 桌面端桌台筛选', () => {
  it('工具栏在桌号之外提供桌台名称筛选项', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const placeholders = wrapper.findAll('.toolbar input').map(i => i.attributes('placeholder'));
    expect(placeholders).toContain('桌号');
    expect(placeholders).toContain('桌台名称');
    wrapper.unmount();
  });

  it('填写桌台名称后点查询,条件随列表请求下发', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const nameInput = wrapper
      .findAll('.toolbar input')
      .find(i => i.attributes('placeholder') === '桌台名称');
    expect(nameInput).toBeDefined();
    await nameInput!.setValue('大厅');
    await wrapper.findAll('.toolbar button').find(b => b.text() === '查询')!.trigger('click');
    await flushPromises();

    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ tableName: '大厅' }));
    wrapper.unmount();
  });

  it('未填写桌台名称时不带该条件(空串不过滤)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const firstCall = listOrdersMock.mock.calls[0][0] as Record<string, unknown>;
    expect(firstCall.tableName).toBeUndefined();
    wrapper.unmount();
  });
});

describe('Orders 订单管理页 · 桌面端桌台列', () => {
  it('桌号列同时展示桌号与桌台名称', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    // 列标题改为「桌台」:单元格里是「01号桌 · 大厅01桌」两段信息
    expect(wrapper.find('.toolbar').exists()).toBe(true);
    const tableText = wrapper.find('table').text();
    expect(tableText).toContain('桌台');
    expect(tableText).toContain('01号桌');
    expect(tableText).toContain('大厅01桌');
    wrapper.unmount();
  });

  it('桌台名称为空的历史单回退只显示桌号', async () => {
    listOrdersMock.mockResolvedValue({
      total: 1,
      items: [{ ...oneOrder()[0], tableNo: '07', tableName: '' }]
    });
    const wrapper = mountOrders();
    await flushPromises();

    const tableText = wrapper.find('table').text();
    expect(tableText).toContain('07号桌');
    expect(tableText).not.toContain('·');
    wrapper.unmount();
  });
});
