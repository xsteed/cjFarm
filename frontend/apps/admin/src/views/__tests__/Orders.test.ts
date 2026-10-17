import { mount } from '@vue/test-utils';
import TDesign, { Pagination as TPagination } from 'tdesign-vue-next';
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
  // 移动端断点(max-width: 1024px)置真，页面走 App 式订单流(卡片 + 底部分页器)
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

describe('Orders 订单管理页 · 移动端底部分页器', () => {
  it('多页时在卡片列表底部渲染分页器(与菜品页同口径)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    const pager = wrapper.find('.m-pager');
    expect(pager.exists()).toBe(true);
    expect(pager.findComponent(TPagination).exists()).toBe(true);
    expect(pager.findComponent(TPagination).props('total')).toBe(TOTAL);
    // 只此一个分页器，且位于订单卡片之后(底部)
    expect(wrapper.findAll('.m-pager')).toHaveLength(1);
    expect(pager.element.compareDocumentPosition(wrapper.find('.ocard').element)).toBe(
      Node.DOCUMENT_POSITION_PRECEDING
    );
    wrapper.unmount();
  });

  it('只有一页时也渲染分页器(共 N 条数据 · 1 页)，0 单才隐藏', async () => {
    listOrdersMock.mockResolvedValue({ total: 8, items: orderPage(1).slice(0, 8) });
    const onePage = mountOrders();
    await flushPromises();
    // 一页也常驻:此时仍可把「10 条/页」切成 5 条,一页变两页浏览
    const pager = onePage.find('.m-pager');
    expect(pager.exists()).toBe(true);
    expect(pager.findComponent(TPagination).props('total')).toBe(8);
    expect(pager.findComponent(TPagination).props('current')).toBe(1);
    onePage.unmount();

    listOrdersMock.mockResolvedValue({ total: 0, items: [] });
    const empty = mountOrders();
    await flushPromises();
    expect(empty.find('.m-pager').exists()).toBe(false);
    empty.unmount();
  });

  it('改每页条数后回第 1 页重拉(page-size-change)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    // 先跳到第 2 页,再改每页条数:必须回第 1 页,否则可能越过新的总页数拿到空列表
    const pager = wrapper.find('.m-pager').findComponent(TPagination);
    const emit = (event: string, ...args: unknown[]) =>
      (pager.vm as unknown as { $emit: (e: string, ...a: unknown[]) => void }).$emit(event, ...args);
    emit('current-change', 2);
    await flushPromises();
    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 2 }));

    emit('page-size-change', 5);
    await flushPromises();

    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 1, pageSize: 5 }));
    expect(wrapper.find('.m-pager').findComponent(TPagination).props('pageSize')).toBe(5);
    wrapper.unmount();
  });

  it('点分页器页码即跳转(请求对应页并保留原筛选条件)', async () => {
    const wrapper = mountOrders();
    await flushPromises();

    // current-change 首参就是新页码数字(见 t-pagination 事件签名)
    const pagination = wrapper.find('.m-pager').findComponent(TPagination);
    (pagination.vm as unknown as { $emit: (event: string, ...args: unknown[]) => void }).$emit('current-change', 3);
    await flushPromises();

    expect(listOrdersMock).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 3, pageSize: PAGE_SIZE }));
    // 跳页后仍保留原筛选条件：两次请求只有 pageNum 不同
    const firstCall = listOrdersMock.mock.calls[0][0];
    const lastCall = listOrdersMock.mock.calls.at(-1)?.[0];
    expect(lastCall).toEqual({ ...firstCall, pageNum: 3 });
    // 分页器当前页同步为第 3 页
    expect(wrapper.find('.m-pager').findComponent(TPagination).props('current')).toBe(3);
    wrapper.unmount();
  });
});
