// 首页看板「未结账超时告警」的回归用例。
//
// 这道提醒的价值全在「开关由后端说了算」:后端开关关着时根本不下发条目,界面必须干净。
// 一旦前端自作主张去读系统配置,就会出现「配置页开着、看板不显示」的错配 ——
// 因为告警开关属 config:edit,而看板只要 order:view 就能进。
import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Dashboard from '../Dashboard.vue';
import { getOrderBoard, getReportSummary, getStaleOrders, getVisitStats, listUrges } from '@dining/api';
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
  getProfile: vi.fn(),
  // 首页「今日访问」卡片的数据源(与经营报表同属 report:view)
  getVisitStats: vi.fn(),
  // 首页「待处理催菜」卡片的数据源(与看板同属 order:view)
  listUrges: vi.fn()
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
  vi.mocked(getVisitStats).mockResolvedValue({
    summary: { todayPv: 66, todayUv: 40, todayNewUv: 5, yesterdayPv: 58, yesterdayUv: 35 }
  });
  vi.mocked(listUrges).mockResolvedValue({ total: 0, items: [] });
});

/** 造一个「N 分钟前」的服务端时间字符串,格式与后端 conf.TimeLayout 一致。 */
function minutesAgo(m: number): string {
  const d = new Date(Date.now() - m * 60_000);
  const p = (n: number): string => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

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

  it('展示「今日访问」卡片：UV 为主、PV 与新访客为副', async () => {
    const w = mountDashboard();
    await flushPromises();

    expect(w.text()).toContain('今日访问');
    // 主数字是独立访客数,副标题是访问次数与新访客
    expect(w.text()).toContain('40');
    expect(w.text()).toContain('66 次访问');
    expect(w.text()).toContain('新访客 5');
    w.unmount();
  });

  it('访问统计接口失败不影响经营 KPI 与桌台看板', async () => {
    vi.mocked(getVisitStats).mockRejectedValue(new Error('没有操作权限'));

    const w = mountDashboard();
    await flushPromises();

    // 卡片仍在(数字退化为 0),桌台看板照常渲染
    expect(w.text()).toContain('今日访问');
    expect(w.find('.table-grid').exists()).toBe(true);
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

describe('Dashboard · 待处理催菜', () => {
  it('有人催菜时列出在催的桌与最久等待', async () => {
    vi.mocked(listUrges).mockResolvedValue({
      total: 2,
      items: [
        {
          urgeId: 1,
          orderId: 101,
          tableId: 1,
          tableNo: '01',
          tableName: '大厅01桌',
          status: 0,
          createTime: minutesAgo(8)
        },
        {
          urgeId: 2,
          orderId: 102,
          tableId: 2,
          tableNo: '02',
          tableName: '大厅02桌',
          status: 0,
          createTime: minutesAgo(3)
        }
      ]
    });

    const w = mountDashboard();
    await flushPromises();

    const card = w.find('.kpi.urge');
    expect(card.exists()).toBe(true);
    // 进入告警态:底色/描边/数字都转成告警色
    expect(card.classes()).toContain('urge-alert');
    expect(card.text()).toContain('待处理催菜');
    expect(card.text()).toContain('2');
    expect(card.text()).toContain('大厅01桌 · 大厅02桌 在催');
    // 取的是「等得最久」那一条,不是最新那一条
    expect(card.text()).toContain('最久 8 分钟');
    w.unmount();
  });

  // 顾客端催菜有 3 分钟冷却,同一桌完全可能连催多次。
  // 按条数算会把一桌的重复催促当成多桌,把 1 桌的毛病报成 3 桌。
  it('同一桌连催多次只算一桌', async () => {
    vi.mocked(listUrges).mockResolvedValue({
      total: 3,
      items: [
        { urgeId: 1, orderId: 101, tableId: 1, tableName: '大厅01桌', status: 0, createTime: minutesAgo(12) },
        { urgeId: 2, orderId: 101, tableId: 1, tableName: '大厅01桌', status: 0, createTime: minutesAgo(8) },
        { urgeId: 3, orderId: 101, tableId: 1, tableName: '大厅01桌', status: 0, createTime: minutesAgo(3) }
      ]
    });

    const w = mountDashboard();
    await flushPromises();

    const card = w.find('.kpi.urge');
    expect(card.find('.v').text()).toContain('1');
    expect(card.text()).toContain('大厅01桌 在催');
    expect(card.text()).not.toContain('大厅01桌 · 大厅01桌');
    w.unmount();
  });

  it('没人催菜时是常态卡片，不抢注意力', async () => {
    const w = mountDashboard();
    await flushPromises();

    const card = w.find('.kpi.urge');
    expect(card.exists()).toBe(true);
    expect(card.classes()).not.toContain('urge-alert');
    expect(card.text()).toContain('暂无催菜');
    w.unmount();
  });

  // 刚催出来不满 1 分钟时,「最久 0 分钟」读起来别扭,说「刚刚」。
  it('刚催出来不满 1 分钟时说「刚刚」', async () => {
    vi.mocked(listUrges).mockResolvedValue({
      total: 1,
      items: [{ urgeId: 1, orderId: 101, tableId: 1, tableName: '大厅01桌', status: 0, createTime: minutesAgo(0) }]
    });

    const w = mountDashboard();
    await flushPromises();

    const card = w.find('.kpi.urge');
    expect(card.text()).toContain('刚刚');
    expect(card.text()).not.toContain('最久 0 分钟');
    w.unmount();
  });

  it('催菜接口失败不影响看板其余部分', async () => {
    vi.mocked(listUrges).mockRejectedValue(new Error('网络异常'));

    const w = mountDashboard();
    await flushPromises();

    // 卡片退化为「无人催菜」,而不是整页空掉
    const card = w.find('.kpi.urge');
    expect(card.exists()).toBe(true);
    expect(card.classes()).not.toContain('urge-alert');
    expect(w.find('.table-grid').exists()).toBe(true);
    expect(w.text()).toContain('桌台看板');
    w.unmount();
  });
});
