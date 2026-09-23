import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Tables from '../../Tables.vue';
import TableCardGrid from '../TableCardGrid.vue';
import { getConfig, listOrders, listTables } from '../../../api';
import { PAGE_ON_LOOPBACK } from '../../../utils/h5Base';
import { setAuth } from '../../../utils/perm';
import { flushPromises, stubMatchMedia, stubResizeObserver } from '../../../test/mocks';
import type { TableInfo } from '../../../types/entities';

// 与 Tables.vue / BatchQrDialog / perm 共享同一个 src/api 模块，统一 mock 全部导出，
// 避免真实 http(axios) 被加载或发起请求。
vi.mock('../../../api', () => ({
  listTables: vi.fn(),
  saveTable: vi.fn(),
  updateTable: vi.fn(),
  deleteTable: vi.fn(),
  getConfig: vi.fn(),
  listOrders: vi.fn(),
  getProfile: vi.fn()
}));

// h5Base 的 LOOPBACK_RE / normalizeBaseUrl 保留真实实现，只把「当前页面是否回环」换成可控 mock。
vi.mock('../../../utils/h5Base', async importOriginal => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, PAGE_ON_LOOPBACK: vi.fn() };
});

// OrderDetailDialog 依赖树深且包含订单操作，用轻量 stub 承接，仅断言被打开与 orderId 透传。
vi.mock('../../../components/OrderDetailDialog.vue', async () => {
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
        return () =>
          props.visible ? h('div', { class: 'order-detail-stub', 'data-order-id': String(props.orderId) }) : null;
      }
    })
  };
});

// 单码/批量弹窗的内部交互在各自测试文件中覆盖，这里仅断言被打开与参数透传。
vi.mock('../SingleQrDialog.vue', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    default: defineComponent({
      name: 'SingleQrDialog',
      props: {
        visible: { type: Boolean, default: false },
        row: { type: Object, default: null },
        h5Base: { type: String, default: '' },
        shopLogo: { type: String, default: '' },
        shopTitle: { type: String, default: '' }
      },
      emits: ['update:visible', 'copy'],
      setup(props) {
        return () =>
          props.visible
            ? h('div', { class: 'single-qr-stub', 'data-table-no': String(props.row?.tableNo ?? '') })
            : null;
      }
    })
  };
});

vi.mock('../BatchQrDialog.vue', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    default: defineComponent({
      name: 'BatchQrDialog',
      props: {
        visible: { type: Boolean, default: false },
        shopLogo: { type: String, default: '' },
        shopTitle: { type: String, default: '' },
        h5Base: { type: String, default: '' },
        allCount: { type: Number, default: 0 },
        listCount: { type: Number, default: 0 },
        queryTableName: { type: [String, Number], default: '' }
      },
      emits: ['update:visible'],
      setup(props) {
        return () => (props.visible ? h('div', { class: 'batch-qr-stub' }) : null);
      }
    })
  };
});

// 保留真实 TDesign 组件（供 global.plugins 使用），只把 MessagePlugin 换成 mock，
// 避免测试里真实弹 toast 产生 DOM 残留与定时器。
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

const listTablesMock = vi.mocked(listTables);
const getConfigMock = vi.mocked(getConfig);
const listOrdersMock = vi.mocked(listOrders);
const pageOnLoopbackMock = vi.mocked(PAGE_ON_LOOPBACK);

// 4 张桌：2 占用 + 2 空闲，容量合计 28，上座率 50%。
const tablesFixture: TableInfo[] = [
  { tableId: 1, tableNo: '01', tableName: '大厅01桌', tableCode: 'C001', capacity: 4, status: 1, sortOrder: 1 },
  { tableId: 2, tableNo: '02', tableName: '大厅02桌', tableCode: 'C002', capacity: 6, status: 1, sortOrder: 2 },
  { tableId: 3, tableNo: '03', tableName: '包间03桌', tableCode: 'C003', capacity: 8, status: 0, sortOrder: 3 },
  { tableId: 4, tableNo: '04', tableName: '包间04桌', tableCode: 'C004', capacity: 10, status: 0, sortOrder: 4 }
];

let restoreMatchMedia: () => void;
let restoreResizeObserver: () => void;

beforeEach(() => {
  restoreMatchMedia = stubMatchMedia();
  restoreResizeObserver = stubResizeObserver();
  setAuth({ token: 't', username: 'u', perms: ['table:edit', 'order:view', 'config:view'] });

  // load() 会拉两次：分页列表 + 全量(500)统计；本测试数据量小，两处返回同一批即可。
  listTablesMock.mockResolvedValue({ total: tablesFixture.length, items: tablesFixture });
  getConfigMock.mockResolvedValue({
    shop_name: '测试餐厅',
    shop_logo: 'http://cdn.test/logo.png',
    h5_base_url: 'http://example.com'
  });
  listOrdersMock.mockResolvedValue({ total: 1, items: [{ orderId: 100, orderNo: 'NO100', orderStatus: 1 }] });
  pageOnLoopbackMock.mockReturnValue(false);
});

afterEach(() => {
  restoreMatchMedia();
  restoreResizeObserver();
});

function mountTables() {
  return mount(Tables, {
    global: {
      plugins: [TDesign],
      stubs: { teleport: true },
      config: { errorHandler: () => undefined }
    }
  });
}

describe('Tables 桌台管理页', () => {
  it('挂载后顶部统计数值正确（总数/占用/空闲/上座率/容量）', async () => {
    const wrapper = mountTables();
    await flushPromises();

    const nums = wrapper.findAll('.stat .num').map(n => n.text());
    expect(nums).toHaveLength(5);
    expect(nums[0]).toContain('4');
    expect(nums[1]).toContain('2');
    expect(nums[2]).toContain('2');
    expect(nums[3]).toContain('50');
    expect(nums[4]).toContain('28');
    wrapper.unmount();
  });

  it('默认卡片视图，切换到列表后显示表格', async () => {
    const wrapper = mountTables();
    await flushPromises();

    expect(wrapper.find('.tcard-grid').exists()).toBe(true);
    expect(wrapper.findComponent(TableCardGrid).props('showCard')).toBe(true);

    // TDesign 的 radio 切换逻辑绑定在 label 的 click 上（非 input change），因此直接点击 label。
    const listRadio = wrapper.findAll('.t-radio-button').find(b => b.text().includes('列表'));
    expect(listRadio).toBeTruthy();
    await listRadio!.trigger('click');
    await flushPromises();

    expect(wrapper.find('.tcard-grid').exists()).toBe(false);
    expect(wrapper.findComponent(TableCardGrid).props('showCard')).toBe(false);
    expect(wrapper.find('table').exists()).toBe(true);
    wrapper.unmount();
  });

  it('点击占用桌卡片调用 listOrders 并打开订单详情弹窗', async () => {
    const wrapper = mountTables();
    await flushPromises();

    const occCard = wrapper.findAll('.mcard.tcard-occ')[0];
    await occCard.trigger('click');
    await flushPromises();

    expect(listOrdersMock).toHaveBeenCalledWith({ tableNo: '01', pageNum: 1, pageSize: 20 });
    const stub = wrapper.find('.order-detail-stub');
    expect(stub.exists()).toBe(true);
    expect(stub.attributes('data-order-id')).toBe('100');
    wrapper.unmount();
  });

  it('点击卡片「二维码」按钮打开单码弹窗并传入桌台', async () => {
    const wrapper = mountTables();
    await flushPromises();

    const qrBtn = wrapper
      .findAll('.mcard')[0]
      .findAll('button')
      .find(b => b.text().includes('二维码'));
    expect(qrBtn).toBeTruthy();
    await qrBtn!.trigger('click');

    const stub = wrapper.find('.single-qr-stub');
    expect(stub.exists()).toBe(true);
    expect(stub.attributes('data-table-no')).toBe('01');
    wrapper.unmount();
  });

  it('点击「批量导出 / 打印」按钮打开批量弹窗', async () => {
    const wrapper = mountTables();
    await flushPromises();

    const batchBtn = wrapper.findAll('button').find(b => b.text().includes('批量导出'));
    expect(batchBtn).toBeTruthy();
    await batchBtn!.trigger('click');

    expect(wrapper.find('.batch-qr-stub').exists()).toBe(true);
    wrapper.unmount();
  });

  it('H5 配置为回环地址且页面非本机时显示警告条', async () => {
    pageOnLoopbackMock.mockReturnValue(false);
    getConfigMock.mockResolvedValue({ shop_name: '测试餐厅', shop_logo: '', h5_base_url: 'http://localhost:8080' });

    const wrapper = mountTables();
    await flushPromises();

    const warn = wrapper.find('.h5-warn');
    expect(warn.exists()).toBe(true);
    expect(warn.text()).toContain('本机地址');
    wrapper.unmount();
  });

  it('H5 配置为真实域名时不显示警告条', async () => {
    pageOnLoopbackMock.mockReturnValue(false);
    getConfigMock.mockResolvedValue({ shop_name: '测试餐厅', shop_logo: '', h5_base_url: 'http://example.com' });

    const wrapper = mountTables();
    await flushPromises();

    expect(wrapper.find('.h5-warn').exists()).toBe(false);
    wrapper.unmount();
  });

  it('页面本身为回环地址时（本地开发）不误报警告', async () => {
    pageOnLoopbackMock.mockReturnValue(true);
    getConfigMock.mockResolvedValue({ shop_name: '测试餐厅', shop_logo: '', h5_base_url: 'http://localhost:8080' });

    const wrapper = mountTables();
    await flushPromises();

    expect(wrapper.find('.h5-warn').exists()).toBe(false);
    wrapper.unmount();
  });
});
