import { mount } from '@vue/test-utils';
import TDesign, { Select as TSelect } from 'tdesign-vue-next';
import Orders from '../Orders.vue';
import { listOrders } from '@dining/api';
import { setAuth } from '@dining/auth';
import { flushPromises, clearAllStorage } from '@shell/test/mocks.ts';
import type { OrderSummary } from '@dining/types';

// 与 Orders.vue / usePayFirst / perm 共享同一个 @dining/api 模块，统一 mock 全部导出，
// 避免真实 http(axios) 被加载或发起请求。
vi.mock('@dining/api', () => ({
  // @dining/auth 在模块加载时会注册它(见 perm.ts 末尾)，漏了会让整个套件加载失败
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn(),
  listOrders: vi.fn(),
  changeOrderStatus: vi.fn(),
  cancelOrder: vi.fn(),
  finishOrder: vi.fn(),
  handleUrge: vi.fn(),
  // 先付开关(pay_first_enabled)：统一按后付返回，卡片按钮矩阵不是本套件的关注点
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

// 详情弹窗依赖树深且包含订单操作，用轻量 stub 承接。
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

// 图标在 main.ts 里全局注册，测试环境没有；逐个 stub 掉避免组件解析告警。
const iconStubs = {
  'chevron-left-icon': { template: '<i />' },
  'chevron-right-icon': { template: '<i />' },
  'search-icon': { template: '<i />' },
  'filter-icon': { template: '<i />' },
  'refresh-icon': { template: '<i />' }
};

const TOTAL = 25;
const PAGE_SIZE = 10;

// 按页码造一页订单：内容随页码变化，翻页后能顺带看出列表真的换了数据。
function orderPage(pageNum: number): OrderSummary[] {
  return Array.from({ length: PAGE_SIZE }, (_, i) => ({
    orderId: (pageNum - 1) * PAGE_SIZE + i + 1,
    orderNo: `NO${pageNum}-${i}`,
    shortNo: String((pageNum - 1) * PAGE_SIZE + i + 1),
    orderStatus: 1,
    payStatus: 0,
    totalAmount: 100
  }));
}

const listOrdersMock = vi.mocked(listOrders);

let restoreMatchMedia: () => void;

beforeEach(() => {
  // 移动端断点(max-width: 1024px)置真，页面走 App 式订单流(卡片 + 顶部页码选择器)
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
    } as MediaQueryList;
  });
  restoreMatchMedia = () => {
    window.matchMedia = original;
  };

  clearAllStorage();
  setAuth({ token: 't', username: 'u', perms: ['order:view', 'order:operate', 'order:settle', 'order:cancel'] });

  listOrdersMock.mockImplementation((params?: { pageNum?: number }) =>
    Promise.resolve({ total: TOTAL, items: orderPage(params?.pageNum ?? 1) })
  );
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

describe('Orders 订单管理页 · 移动端顶部页码选择器', () => {
  it('多页时在卡片列表上方渲染页码选择器，显示当前页/总页数与总单数', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const pager = wrapper.find('.m-toppager');
    expect(pager.exists()).toBe(true);
    // 选择器位于订单卡片列表之前(顶部)，而不是只在列表底部
    expect(pager.element.compareDocumentPosition(wrapper.find('.m-list').element))
      .toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(pager.text()).toContain('第 1 / 3 页');
    expect(pager.text()).toContain(`共 ${TOTAL} 单`);
    // 第 1 页时「上一页」禁用、「下一页」可点
    expect(pager.findAll('.m-toppager-nav')[0].attributes('disabled')).toBeDefined();
    expect(pager.findAll('.m-toppager-nav')[1].attributes('disabled')).toBeUndefined();
    wrapper.unmount();
  });

  it('只有一页时不渲染顶部页码选择器', async () => {
    listOrdersMock.mockResolvedValue({ total: 8, items: orderPage(1).slice(0, 8) });
    const wrapper = mountOrders();
    await flushPromises();

    expect(wrapper.find('.m-toppager').exists()).toBe(false);
    wrapper.unmount();
  });

  it('点「下一页」带着当前筛选请求第 2 页，并更新页码显示', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    await wrapper.findAll('.m-toppager-nav')[1].trigger('click');
    await flushPromises();

    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 2, pageSize: PAGE_SIZE }));
    expect(wrapper.find('.m-toppager').text()).toContain('第 2 / 3 页');
    wrapper.unmount();
  });

  it('页码下拉列出全部页，选中即跳转(直接请求选中页)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const select = wrapper.findComponent(TSelect);
    expect(select.exists()).toBe(true);
    // 折叠态显示「第 X / Y 页」，下拉项逐页可点(即分页选择 + 跳转)
    expect(wrapper.find('.m-toppager').text()).toContain('第 1 / 3 页');
    expect(select.props('options')).toEqual([
      { label: '第 1 页', value: 1 },
      { label: '第 2 页', value: 2 },
      { label: '第 3 页', value: 3 }
    ]);

    (select.vm as unknown as { $emit: (event: string, ...args: unknown[]) => void }).$emit('change', 3);
    await flushPromises();

    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 3 }));
    expect(wrapper.find('.m-toppager').text()).toContain('第 3 / 3 页');
    // 末页时「下一页」禁用，「上一页」恢复可点
    expect(wrapper.findAll('.m-toppager-nav')[1].attributes('disabled')).toBeDefined();
    expect(wrapper.findAll('.m-toppager-nav')[0].attributes('disabled')).toBeUndefined();
    wrapper.unmount();
  });

  it('跳页后仍保留原筛选条件(页号变了，查询参数不变)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    await wrapper.findAll('.m-toppager-nav')[1].trigger('click');
    await flushPromises();

    // 首屏与翻页两次请求的查询条件完全一致，只有 pageNum 不同
    const firstCall = listOrdersMock.mock.calls[0][0];
    const secondCall = listOrdersMock.mock.calls.at(-1)?.[0];
    expect(secondCall).toEqual({ ...firstCall, pageNum: 2 });
    wrapper.unmount();
  });
});
