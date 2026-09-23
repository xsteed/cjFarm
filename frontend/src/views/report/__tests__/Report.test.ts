import { mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import Report from '../../Report.vue';
import {
  getDailyTrend,
  getDishRank,
  getHourlyReport,
  getMonthlyTrend,
  getReportSummary,
  getSettleMix
} from '../../../api';
import type {
  DailyTrendRow,
  DishRankRow,
  HourlyRow,
  MonthlyTrendRow,
  ReportSummary,
  SettleMixRow
} from '../../../types/entities';
import { disposeAll, EMPTY_OPTION } from '../../../composables/useEChart';
import { flushPromises, stubResizeObserver } from '../../../test/mocks';

// jsdom 的 Blob 未实现 text(),用 FileReader 补一个等价实现(测试内 shim)
if (typeof Blob !== 'undefined' && !Blob.prototype.text) {
  Blob.prototype.text = function (): Promise<string> {
    return new Promise(resolve => {
      const fr = new FileReader();
      fr.onload = () => resolve(String(fr.result));
      fr.readAsText(this);
    });
  };
}

type ChartInstance = {
  setOption: ReturnType<typeof vi.fn>;
  clear: ReturnType<typeof vi.fn>;
  resize: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};

type ChartSeries = {
  name?: string;
  data?: unknown[];
};

type ChartOption = {
  series?: ChartSeries[];
  [key: string]: unknown;
};

const echartsMock = vi.hoisted(() => ({
  init: vi.fn(() => ({
    setOption: vi.fn(),
    clear: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn()
  })),
  use: vi.fn()
}));

// useEChart 已改为 echarts/core 按需引入,mock 路径同步跟上
vi.mock('echarts/core', () => echartsMock);
vi.mock('echarts/charts', () => ({ LineChart: {}, BarChart: {}, PieChart: {} }));
vi.mock('echarts/components', () => ({
  TitleComponent: {},
  TooltipComponent: {},
  GridComponent: {},
  LegendComponent: {}
}));
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }));

vi.mock('../../../api', () => ({
  getReportSummary: vi.fn(),
  getDailyTrend: vi.fn(),
  getMonthlyTrend: vi.fn(),
  getDishRank: vi.fn(),
  getHourlyReport: vi.fn(),
  getSettleMix: vi.fn()
}));

vi.mock('vue-router', () => ({
  RouterLink: defineComponent({
    name: 'RouterLink',
    props: {
      to: { type: [String, Object], default: '' }
    },
    setup(props, { slots }) {
      return () => h('a', { href: typeof props.to === 'string' ? props.to : '' }, slots.default?.() ?? null);
    }
  })
}));

vi.mock('tdesign-icons-vue-next', () => ({
  FileIcon: defineComponent({
    name: 'FileIcon',
    setup: () => () => h('i', { class: 'file-icon' })
  }),
  RefreshIcon: defineComponent({
    name: 'RefreshIcon',
    setup: () => () => h('i', { class: 'refresh-icon' })
  })
}));

vi.mock('tdesign-vue-next', () => ({
  Button: defineComponent({
    name: 'TButton',
    props: {
      disabled: { type: Boolean, default: false },
      loading: { type: Boolean, default: false },
      theme: { type: String, default: '' }
    },
    setup(props, { slots }) {
      return () =>
        h('button', { class: 't-button', disabled: props.disabled || props.loading }, [
          slots.icon?.() ?? null,
          slots.default?.() ?? null
        ]);
    }
  }),
  Loading: defineComponent({
    name: 'TLoading',
    props: {
      loading: { type: Boolean, default: false }
    },
    setup(_, { slots }) {
      return () => h('div', { class: 't-loading' }, slots.default?.() ?? null);
    }
  }),
  DateRangePicker: defineComponent({
    name: 'TDateRangePicker',
    props: {
      modelValue: { type: Array, default: () => [] }
    },
    emits: ['change', 'update:modelValue'],
    setup(_, { emit }) {
      const onChange = () => {
        const next = ['2025-01-01', '2025-01-10'];
        emit('update:modelValue', next);
        emit('change', next);
      };
      return () => h('div', { class: 'date-range-picker', onClick: onChange }, '日期区间');
    }
  }),
  RadioGroup: defineComponent({
    name: 'TRadioGroup',
    emits: ['change'],
    setup(_, { emit }) {
      return () =>
        h('div', { class: 'radio-group' }, [
          h('button', { class: 'radio-qty', onClick: () => emit('change', 'qty') }, '按销量'),
          h('button', { class: 'radio-amount', onClick: () => emit('change', 'amount') }, '按销售额')
        ]);
    }
  }),
  RadioButton: defineComponent({
    name: 'TRadioButton',
    setup(_, { slots }) {
      return () => h('span', { class: 'radio-button' }, slots.default?.() ?? null);
    }
  })
}));

const getReportSummaryMock = vi.mocked(getReportSummary);
const getDailyTrendMock = vi.mocked(getDailyTrend);
const getMonthlyTrendMock = vi.mocked(getMonthlyTrend);
const getDishRankMock = vi.mocked(getDishRank);
const getHourlyReportMock = vi.mocked(getHourlyReport);
const getSettleMixMock = vi.mocked(getSettleMix);

const summaryFixture: ReportSummary = {
  todayAmount: 12888.5,
  yesterdayAmount: 12000,
  todayOrderCount: 128,
  yesterdayOrderCount: 120,
  todayGuestCount: 205,
  yesterdayGuestCount: 190,
  todayAvgAmount: 100.69,
  monthAmount: 320000,
  lastMonthSamePeriodAmount: 300000,
  monthOrderCount: 3200,
  monthGuestCount: 5100,
  todayFreeAmount: 88,
  creditPendingAmount: 5600,
  creditPendingCount: 6,
  todayCreditAmount: 1200,
  todayCreditSettledAmount: 2400,
  todayCreditSettledCount: 4,
  todayRefundAmount: 66,
  todayCancelCount: 2,
  tableCount: 20,
  freeTableCount: 8,
  activeOrderCount: 12
};

const dailyTrendFixture: DailyTrendRow[] = [
  { date: '2025-01-01', amount: 1000, orderCount: 20, guestCount: 32 },
  { date: '2025-01-02', amount: 1200, orderCount: 24, guestCount: 38 },
  { date: '2025-01-03', amount: 980, orderCount: 18, guestCount: 30 },
  { date: '2025-01-04', amount: 1500, orderCount: 30, guestCount: 45 },
  { date: '2025-01-05', amount: 1700, orderCount: 33, guestCount: 50 },
  { date: '2025-01-06', amount: 1600, orderCount: 31, guestCount: 47 },
  { date: '2025-01-07', amount: 1900, orderCount: 36, guestCount: 55 }
];

const hourlyFixture: HourlyRow[] = [
  { hour: '10', amount: 520, orderCount: 12, guestCount: 20 },
  { hour: '11', amount: 980, orderCount: 20, guestCount: 33 },
  { hour: '12', amount: 1200, orderCount: 25, guestCount: 40 }
];

const mixFixture: SettleMixRow[] = [
  { settleType: 'normal', label: '正常收款', amount: 8800, paidAmount: 8600, orderCount: 180 },
  { settleType: 'free', label: '免单', amount: 320, paidAmount: 0, orderCount: 5 },
  { settleType: 'credit', label: '挂账', amount: 1500, paidAmount: 0, orderCount: 12 }
];

const monthlyFixture: MonthlyTrendRow[] = [
  { month: '2025-01', amount: 280000, orderCount: 2800, guestCount: 4600 },
  { month: '2025-02', amount: 320000, orderCount: 3200, guestCount: 5100 }
];

const rankFixture: DishRankRow[] = [
  { dishName: '宫保鸡丁', quantity: 520, amount: 19760 },
  { dishName: '红烧肉', quantity: 410, amount: 18860 },
  { dishName: '米饭', quantity: 900, amount: 1800 }
];

function chartInstances(): ChartInstance[] {
  return echartsMock.init.mock.results.map(r => r.value as ChartInstance);
}

function allSetOptionOptions(): ChartOption[] {
  return chartInstances().flatMap(inst => inst.setOption.mock.calls.map(call => call[0] as ChartOption));
}

function mountReport() {
  return mount(Report);
}

let restoreResizeObserver: (() => void) | undefined;

beforeEach(() => {
  restoreResizeObserver = stubResizeObserver();
  disposeAll();

  getReportSummaryMock.mockResolvedValue(summaryFixture);
  getDailyTrendMock.mockResolvedValue(dailyTrendFixture);
  getMonthlyTrendMock.mockResolvedValue(monthlyFixture);
  getDishRankMock.mockResolvedValue(rankFixture);
  getHourlyReportMock.mockResolvedValue(hourlyFixture);
  getSettleMixMock.mockResolvedValue(mixFixture);
});

afterEach(() => {
  restoreResizeObserver?.();
  restoreResizeObserver = undefined;
});

describe('Report', () => {
  it('挂载后渲染 5 个图表容器并初始化 echarts 实例,setOption 收到非空 option', async () => {
    const wrapper = mountReport();
    await flushPromises();

    expect(wrapper.findAll('.chart-canvas')).toHaveLength(5);
    expect(echartsMock.init).toHaveBeenCalledTimes(5);

    for (const inst of chartInstances()) {
      const hasRealOption = inst.setOption.mock.calls.some(call => call[0] !== EMPTY_OPTION);
      expect(hasRealOption).toBe(true);
    }

    wrapper.unmount();
  });

  it('dailyTrend 映射为 series 数据,长度与 fixture 天数一致', async () => {
    const wrapper = mountReport();
    await flushPromises();

    const dailyOption = allSetOptionOptions().find(
      opt => opt.series?.[0]?.name === '营业额' && opt.series?.[0]?.data?.length === dailyTrendFixture.length
    );
    expect(dailyOption).toBeDefined();

    wrapper.unmount();
  });

  it('切换排行维度为销售额时重新按 amount 加载排行', async () => {
    const wrapper = mountReport();
    await flushPromises();

    expect(getDishRankMock).toHaveBeenCalledWith({ limit: 10, sort: 'qty' });

    await wrapper.find('.radio-amount').trigger('click');
    await flushPromises();

    expect(getDishRankMock).toHaveBeenCalledWith({ limit: 10, sort: 'amount' });
    expect(getDishRankMock).toHaveBeenCalledTimes(2);

    wrapper.unmount();
  });

  it('接口全空时各图表渲染 EMPTY_OPTION', async () => {
    getReportSummaryMock.mockResolvedValue({});
    getDailyTrendMock.mockResolvedValue([]);
    getMonthlyTrendMock.mockResolvedValue([]);
    getDishRankMock.mockResolvedValue([]);
    getHourlyReportMock.mockResolvedValue([]);
    getSettleMixMock.mockResolvedValue([]);

    const wrapper = mountReport();
    await flushPromises();

    expect(echartsMock.init).toHaveBeenCalledTimes(5);
    for (const inst of chartInstances()) {
      const calls = inst.setOption.mock.calls;
      expect(calls.length).toBeGreaterThan(0);
      expect(calls[calls.length - 1][0]).toBe(EMPTY_OPTION);
    }

    wrapper.unmount();
  });

  it('切换日期区间只重新拉取三个区间相关接口', async () => {
    const wrapper = mountReport();
    await flushPromises();

    expect(getDailyTrendMock).toHaveBeenCalledTimes(1);
    expect(getHourlyReportMock).toHaveBeenCalledTimes(1);
    expect(getSettleMixMock).toHaveBeenCalledTimes(1);

    await wrapper.find('.date-range-picker').trigger('click');
    await flushPromises();

    expect(getDailyTrendMock).toHaveBeenCalledTimes(2);
    expect(getHourlyReportMock).toHaveBeenCalledTimes(2);
    expect(getSettleMixMock).toHaveBeenCalledTimes(2);
    expect(getDailyTrendMock).toHaveBeenLastCalledWith({ start: '2025-01-01', end: '2025-01-10' });

    expect(getReportSummaryMock).toHaveBeenCalledTimes(1);
    expect(getMonthlyTrendMock).toHaveBeenCalledTimes(1);
    expect(getDishRankMock).toHaveBeenCalledTimes(1);

    wrapper.unmount();
  });

  it('导出明细生成包含表头与合计的 CSV', async () => {
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => 'blob:mock-url');
    const revokeObjectURL = vi.fn();
    const prevCreate = URL.createObjectURL;
    const prevRevoke = URL.revokeObjectURL;
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    URL.createObjectURL = createObjectURL as unknown as typeof URL.createObjectURL;
    URL.revokeObjectURL = revokeObjectURL as unknown as typeof URL.revokeObjectURL;

    try {
      const wrapper = mountReport();
      await flushPromises();

      const exportButton = wrapper.findAll('button').find(btn => btn.text().includes('导出明细'));
      if (!exportButton) throw new Error('未找到导出明细按钮');
      await exportButton.trigger('click');

      expect(createObjectURL).toHaveBeenCalledTimes(1);
      const blob = createObjectURL.mock.calls[0][0];
      const text = await blob.text();
      const body = text.replace(/^\uFEFF/, '');
      const lines = body.split('\r\n');

      // 表头 1 行 + 7 天明细 + 合计 1 行
      expect(lines).toHaveLength(9);
      expect(body).toContain('日期');
      expect(body).toContain('营业额(元)');
      expect(body).toContain('合计');
      expect(clickSpy).toHaveBeenCalledTimes(1);

      wrapper.unmount();
    } finally {
      URL.createObjectURL = prevCreate;
      URL.revokeObjectURL = prevRevoke;
      clickSpy.mockRestore();
    }
  });
});
