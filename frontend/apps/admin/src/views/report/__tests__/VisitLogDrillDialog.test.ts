import { mount } from '@vue/test-utils';
import { flushPromises } from '@shell/test/mocks.ts';
import VisitLogDrillDialog from '../VisitLogDrillDialog.vue';
import { getVisitLogs } from '@dining/api';

vi.mock('@dining/api', () => ({
  onAuthCleared: vi.fn(),
  getVisitLogs: vi.fn()
}));

const getVisitLogsMock = vi.mocked(getVisitLogs);

beforeEach(() => {
  getVisitLogsMock.mockReset();
  getVisitLogsMock.mockResolvedValue({
    range: { start: '2025-01-01', end: '2025-01-03' },
    total: 5,
    visitors: 3,
    items: [
      { time: '2025-01-01 10:00:00', channel: 'h5', channelLabel: '扫码点餐', device: '手机', os: 'iOS', isNew: true },
      { time: '2025-01-01 11:00:00', channel: 'admin', channelLabel: '管理后台', device: '', os: '', isNew: false }
    ]
  });
});

function mountDialog() {
  return mount(VisitLogDrillDialog, {
    props: {
      visible: false,
      title: '手机访问明细',
      query: { start: '2025-01-01', end: '2025-01-03', device: '手机' },
      hint: '2025-01-01 ~ 2025-01-03'
    }
  });
}

describe('VisitLogDrillDialog', () => {
  it('visible=false 不发请求', async () => {
    const wrapper = mountDialog();
    await flushPromises();
    expect(getVisitLogsMock).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it('打开时按传入筛选参数请求第 1 页,头部展示次数与去重访客数', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    expect(getVisitLogsMock).toHaveBeenCalledTimes(1);
    expect(getVisitLogsMock).toHaveBeenCalledWith({
      start: '2025-01-01',
      end: '2025-01-03',
      device: '手机',
      pageNum: 1,
      pageSize: 10
    });
    const sub = wrapper.find('.drill-sub').text();
    expect(sub).toContain('共 5 次访问');
    expect(sub).toContain('3 位访客');
    expect(sub).toContain('2025-01-01 ~ 2025-01-03');

    wrapper.unmount();
  });

  it('筛选参数变化时回到第 1 页重新查询', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    await wrapper.setProps({ query: { start: '2025-01-02', end: '2025-01-02', channel: 'admin' } });
    await flushPromises();

    expect(getVisitLogsMock).toHaveBeenCalledTimes(2);
    expect(getVisitLogsMock).toHaveBeenLastCalledWith({
      start: '2025-01-02',
      end: '2025-01-02',
      channel: 'admin',
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

    expect(getVisitLogsMock).toHaveBeenCalledTimes(2);

    wrapper.unmount();
  });
});
