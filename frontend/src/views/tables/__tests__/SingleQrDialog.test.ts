import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import SingleQrDialog from '../SingleQrDialog.vue';
import { renderQrDataUrl } from '../../../utils/tableQr';
import { flushPromises } from '../../../test/mocks';
import type { TableInfo } from '../../../types/entities';

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

vi.mock('../../../utils/tableQr', () => ({
  QR_PIXEL_PRESETS: [
    { key: 360, label: '360 px（预览 / 网页）' },
    { key: 640, label: '640 px（常规打印）' },
    { key: 900, label: '900 px（高清打印）' },
    { key: 1200, label: '1200 px（大幅面）' }
  ],
  renderQrDataUrl: vi.fn(),
  buildPrintSheetHtml: vi.fn(),
  openPrintWindow: vi.fn()
}));

const renderQrDataUrlMock = vi.mocked(renderQrDataUrl);

const rowFixture: TableInfo = {
  tableId: 1,
  tableNo: '01',
  tableName: '大厅01桌',
  tableCode: 'C001',
  capacity: 4,
  status: 1,
  sortOrder: 1
};

function mountDialog(overrides: Record<string, unknown> = {}) {
  return mount(SingleQrDialog, {
    props: {
      visible: false,
      row: rowFixture,
      h5Base: 'http://example.com',
      shopLogo: 'http://cdn.test/logo.png',
      shopTitle: '测试餐厅',
      ...overrides
    },
    global: {
      plugins: [TDesign],
      stubs: { teleport: false },
      config: { errorHandler: () => undefined }
    }
  });
}

beforeEach(() => {
  renderQrDataUrlMock.mockResolvedValue('data:image/png;base64,mock');
});

describe('SingleQrDialog 单张桌台二维码', () => {
  it('打开后调用 renderQrDataUrl 并渲染二维码预览 img', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    const img = wrapper.find('.qr-img');
    expect(img.exists()).toBe(true);
    expect(img.attributes('src')).toBe('data:image/png;base64,mock');
    expect(renderQrDataUrlMock).toHaveBeenCalledWith(
      'http://example.com/order/C001',
      expect.objectContaining({ size: 640, logo: 'http://cdn.test/logo.png', centerText: '' })
    );
    wrapper.unmount();
  });

  it('切换中心 Logo / 桌号选项触发 watch 防抖重渲染', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();
    expect(renderQrDataUrlMock).toHaveBeenCalledTimes(1);

    const checkboxes = wrapper.findAll('input[type=checkbox]');
    expect(checkboxes).toHaveLength(2);
    // 先取消「中心显示 Logo」，再勾选「中心显示桌号」
    await checkboxes[0].setChecked(false);
    await checkboxes[1].setChecked(true);

    // watch 联动有 180ms 防抖，用真实计时器等待后再排空微任务
    await new Promise(resolve => setTimeout(resolve, 220));
    await flushPromises();

    expect(renderQrDataUrlMock).toHaveBeenCalledTimes(2);
    expect(renderQrDataUrlMock).toHaveBeenLastCalledWith(
      'http://example.com/order/C001',
      expect.objectContaining({ logo: '', centerText: '01' })
    );
    wrapper.unmount();
  });
});
