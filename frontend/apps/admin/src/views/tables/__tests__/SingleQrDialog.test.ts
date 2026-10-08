import { mount } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import SingleQrDialog from '../SingleQrDialog.vue';
import { getTableWeappCode } from '@dining/api';
import { renderQrDataUrl } from '../../../utils/tableQr';
import { flushPromises } from '@shell/test/mocks.ts';
import type { TableInfo } from '@dining/types';

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

vi.mock('@dining/api', () => ({
  getTableWeappCode: vi.fn()
}));

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
const getTableWeappCodeMock = vi.mocked(getTableWeappCode);

// 按文案点选单选按钮:TDesign 的 radio 切换绑在 label 的 click 上(非 input change),
// 与 BatchQrDialog.test.ts / Tables.test.ts 保持同一做法。
async function pickRadio(wrapper: ReturnType<typeof mountDialog>, text: string): Promise<void> {
  const btn = wrapper.findAll('.t-radio-button').find(b => b.text().includes(text));
  if (!btn) throw new Error(`找不到文案为「${text}」的单选按钮`);
  await btn.trigger('click');
  await flushPromises();
}

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
  getTableWeappCodeMock.mockResolvedValue({
    dataUrl: 'data:image/jpeg;base64,weapp',
    cached: false,
    envVersion: 'release',
    width: 640
  });
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
    // 先取消「中心显示 Logo」，再勾选「中心显示桌号」。
    // 这里用 setValue 而不是 setChecked：两者运行时是**同一条路径**
    //（@vue/test-utils 的 setValue 对 input[type=checkbox] 就是 `return this.setChecked(value)`），
    // 但 2.5.1 的 domWrapper.d.ts 把 setChecked 误标成了 private，直接调用会报 TS2341。
    await checkboxes[0].setValue(false);
    await checkboxes[1].setValue(true);

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

  it('默认仍是 H5 二维码（存量桌牌行为不变）', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    expect(renderQrDataUrlMock).toHaveBeenCalledTimes(1);
    expect(getTableWeappCodeMock).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it('切到小程序码后改由后端出码，中心选项被禁用', async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    await pickRadio(wrapper, '小程序码');
    // 切换码类型走的是同一个 180ms 防抖
    await new Promise(resolve => setTimeout(resolve, 220));
    await flushPromises();

    // 用桌台稳定码请求后端(后端带 access_token 换取,前端不接触令牌)
    expect(getTableWeappCodeMock).toHaveBeenCalledWith('C001', 640);
    expect(wrapper.find('.qr-img').attributes('src')).toBe('data:image/jpeg;base64,weapp');
    // 小程序码的中心由微信固定为「小程序头像」,两个中心选项都不该再能勾选
    const checkboxes = wrapper.findAll('input[type=checkbox]');
    expect(checkboxes).toHaveLength(2);
    expect(checkboxes.every(c => (c.element as HTMLInputElement).disabled)).toBe(true);
    // 没有链接可展示,改为说明码指向哪个版本
    expect(wrapper.text()).toContain('pages/index/index');
    wrapper.unmount();
  });

  it('出的是体验版码时给出醒目提醒，避免误印到桌牌', async () => {
    getTableWeappCodeMock.mockResolvedValue({
      dataUrl: 'data:image/jpeg;base64,trial',
      cached: false,
      envVersion: 'trial',
      width: 640
    });
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    await pickRadio(wrapper, '小程序码');
    await new Promise(resolve => setTimeout(resolve, 220));
    await flushPromises();

    // 体验版码只有体验成员能扫开;印到桌牌上等于顾客全扫不开,必须显式拦一下
    expect(wrapper.text()).toContain('体验版');
    expect(wrapper.text()).toContain('只有体验成员能扫开');
    wrapper.unmount();
  });

  it('小程序码出码失败时把后端的中文提示显示出来', async () => {
    getTableWeappCodeMock.mockRejectedValue(
      new Error('未配置小程序 AppID / AppSecret，请先到「系统配置 → 小程序桌牌码」填写')
    );
    const wrapper = mountDialog();
    await wrapper.setProps({ visible: true });
    await flushPromises();

    await pickRadio(wrapper, '小程序码');
    await new Promise(resolve => setTimeout(resolve, 220));
    await flushPromises();

    // 提示要能直接指向去哪配,而不是笼统的「二维码生成失败」
    expect(wrapper.find('.qr-placeholder.error').text()).toContain('AppSecret');
    wrapper.unmount();
  });
});
