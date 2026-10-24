import { mount } from '@vue/test-utils';
import { defineComponent } from 'vue';
import OrderDrillDialog from '../OrderDrillDialog.vue';
import { listOrders } from '@dining/api';
import type { OrderSummary } from '@dining/types';
import { flushPromises } from '@shell/test/mocks.ts';

// 弹窗内嵌的订单详情组件会级联引入收款/退款/改单一整条组件链,测试只关心
// 「下钻列表」本身,整只 stub 掉(与 Report.test.ts 同一策略)。
vi.mock('../../../components/OrderDetailDialog.vue', () => ({
  default: defineComponent({
    name: 'OrderDetailDialog',
    props: { visible: { type: Boolean, default: false }, orderId: { type: Number, default: 0 } },
    emits: ['update:visible', 'changed'],
    setup() {
      return () => null;
    }
  })
}));

vi.mock('@dining/api', () => ({
  onAuthCleared: vi.fn(),
  listOrders: vi.fn(),
  ORDER_STATUS: {
    1: { label: '已下单', theme: 'primary' },
    4: { label: '已完成', theme: 'success' },
    5: { label: '已取消', theme: 'danger' }
  }
}));

const listOrdersMock = vi.mocked(listOrders);

const rows: OrderSummary[] = [
  {
    orderId: 1,
    orderNo: 'ORD20250101001',
    shortNo: 'A1001',
    tableNo: '01',
    tableName: '大厅01桌',
    orderStatus: 4,
    personCount: 3,
    totalAmount: 12800,
    payStatus: 1,
    settleType: 'normal',
    createTime: '2025-01-01 12:30:00'
  }
];

beforeEach(() => {
  listOrdersMock.mockReset();
  listOrdersMock.mockResolvedValue({ total: 3, items: rows });
});

function mountDialog() {
  return mount(OrderDrillDialog, {
    props: {
      visible: false,
      title: '2025-01-01 营业日订单',
      query: { hideCanceled: 1, start: '2025-01-01', end: '2025-01-01' },
      hint: '营业日 2025-01-01 · 不含已取消'
    }
  });
}

describe('OrderDrillDialog', () => {
  it('visible=false 不发请求', async () => {
    const wrapper = mountDialog();
    await flushPromises();
    expect(listOrdersMock).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it('打开时按传入筛选参数请求第 1 页,并把口径提示拼在标题区', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    expect(listOrdersMock).toHaveBeenCalledTimes(1);
    expect(listOrdersMock).toHaveBeenCalledWith({
      hideCanceled: 1,
      start: '2025-01-01',
      end: '2025-01-01',
      pageNum: 1,
      pageSize: 10
    });
    // 「共 N 单」来自接口 total,不是本页条数
    expect(wrapper.find('.drill-sub').text()).toContain('共 3 单');
    expect(wrapper.find('.drill-sub').text()).toContain('不含已取消');

    wrapper.unmount();
  });

  it('筛选参数变化时回到第 1 页重新查询', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    await wrapper.setProps({ query: { hideCanceled: 1, start: '2025-01-02', end: '2025-01-02' } });
    await flushPromises();

    expect(listOrdersMock).toHaveBeenCalledTimes(2);
    expect(listOrdersMock).toHaveBeenLastCalledWith({
      hideCanceled: 1,
      start: '2025-01-02',
      end: '2025-01-02',
      pageNum: 1,
      pageSize: 10
    });

    wrapper.unmount();
  });

  it('关闭后再打开会重查一遍', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();
    await wrapper.setProps({ visible: false });
    await wrapper.setProps({ visible: true });
    await flushPromises();

    expect(listOrdersMock).toHaveBeenCalledTimes(2);

    wrapper.unmount();
  });
});
