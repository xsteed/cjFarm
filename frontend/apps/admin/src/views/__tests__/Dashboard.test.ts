// 首页看板「未结账超时告警」的回归用例。
//
// 这道提醒的价值全在「开关由后端说了算」:后端开关关着时根本不下发条目,界面必须干净。
// 一旦前端自作主张去读系统配置,就会出现「配置页开着、看板不显示」的错配 ——
// 因为告警开关属 config:edit,而看板只要 order:view 就能进。
import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Dashboard from '../Dashboard.vue';
import { getOrderBoard, getReportSummary, getStaleOrders } from '@dining/api';
import { setAuth } from '@dining/auth';
import { flushPromises, stubMatchMedia, stubResizeObserver, clearAllStorage } from '@shell/test/mocks.ts';
import type { TableInfo } from '@dining/types';

// vue-router 必须在这里顶层 mock:vi.mock 会被提升到 import 之前,
// 放进 helper 函数里调用会因为「Dashboard 已先导入真实实现」而完全不生效。
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}));

vi.mock('@dining/api', () => ({
  // @dining/auth 在模块加载时会注册它(见 perm.ts 末尾),漏了会让整个套件加载失败
  onAuthCleared: vi.fn(),
  getReportSummary: vi.fn(),
  getOrderBoard: vi.fn(),
  getStaleOrders: vi.fn(),
  getProfile: vi.fn()
}));

const boardFixture: TableInfo[] = [
  { tableId: 1, tableNo: '01', tableName: '大厅01桌', status: 1, orders: [] },
  { tableId: 2, tableNo: '02', tableName: '大厅02桌', status: 0, orders: [] }
];

let restoreMatchMedia: () => void;
let restoreResizeObserver: () => void;

beforeEach(() => {
  restoreMatchMedia = stubMatchMedia();
  restoreResizeObserver = stubResizeObserver();
  clearAllStorage();
  setAuth({ token: 't', username: 'u', perms: ['order:view', 'report:view'] });
  vi.mocked(getReportSummary).mockResolvedValue({});
  vi.mocked(getOrderBoard).mockResolvedValue(boardFixture);
  vi.mocked(getStaleOrders).mockResolvedValue({ enabled: false, hours: 4, items: [] });
});

afterEach(() => {
  restoreMatchMedia();
  restoreResizeObserver();
});

function mountDashboard() {
  return mount(Dashboard, {
    global: {
      plugins: [TDesign],
      // router-link 在真实应用里由 router 插件提供;这里只需要它能渲染出文字供断言。
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
        PullRefresh: true
      }
    }
  });
}

describe('Dashboard · 未结账超时告警', () => {
  it('开关开启且命中时，把超时未结账的桌台列出来', async () => {
    vi.mocked(getStaleOrders).mockResolvedValue({
      enabled: true,
      hours: 4,
      items: [
        {
          orderId: 9,
          orderNo: 'NO000009',
          shortNo: '009',
          tableId: 1,
          tableNo: '01',
          tableName: '大厅01桌',
          minutes: 200,
          totalAmount: 128
        }
      ]
    });

    const w = mountDashboard();
    await flushPromises();

    expect(w.find('.stale-bar').exists()).toBe(true);
    expect(w.text()).toContain('1 笔订单还没结账');
    expect(w.text()).toContain('大厅01桌');
    // 200 分钟要说成「3 小时 20 分」,店员扫一眼就知道开了多久
    expect(w.text()).toContain('已 3 小时 20 分');
    expect(w.text()).toContain('¥128');
    // 当天超时的单不是遗留单,不该带「跨日」标记
    expect(w.find('.stale-line .lg').exists()).toBe(false);
    w.unmount();
  });

  // 跨日遗留单的桌台已被自动清空:看板上完全看不到它,只有这里能找到。
  // 不标出「跨日」的话,店家会对着一个空桌发懵 ——「桌上没人,怎么还有单」。
  it('跨日遗留单带「跨日」标记', async () => {
    vi.mocked(getStaleOrders).mockResolvedValue({
      enabled: true,
      hours: 4,
      items: [
        {
          orderId: 11,
          orderNo: 'NO000011',
          shortNo: '011',
          tableId: 1,
          tableNo: '01',
          tableName: '大厅01桌',
          minutes: 1500,
          totalAmount: 88,
          legacy: true
        }
      ]
    });

    const w = mountDashboard();
    await flushPromises();

    expect(w.text()).toContain('1 笔订单还没结账');
    expect(w.find('.stale-line .lg').text()).toBe('跨日');
    // 25 小时不该说成「25 小时」以外的形态
    expect(w.text()).toContain('已 1 天 1 小时');
    w.unmount();
  });

  it('后端开关关闭（不下发条目）时不展示告警区', async () => {
    const w = mountDashboard();
    await flushPromises();

    expect(w.find('.stale-bar').exists()).toBe(false);
    w.unmount();
  });

  it('告警接口失败不影响看板其余部分', async () => {
    vi.mocked(getStaleOrders).mockRejectedValue(new Error('没有操作权限'));

    const w = mountDashboard();
    await flushPromises();

    expect(w.find('.stale-bar').exists()).toBe(false);
    // 桌台看板照常渲染:提醒拿不到不该让整页空掉(瓦片显示的是桌号,不是桌名)
    expect(w.find('.table-grid').exists()).toBe(true);
    expect(w.text()).toContain('桌台看板');
    w.unmount();
  });
});
