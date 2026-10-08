import { mount } from '@vue/test-utils';
import TDesign, { MessagePlugin } from 'tdesign-vue-next';
import BatchQrDialog from '../BatchQrDialog.vue';
import { getTableWeappCode, listTables } from '@dining/api';
import { buildPrintSheetHtml, openPrintWindow, renderQrDataUrl } from '../../../utils/tableQr';
import { dataUrlToBytes, downloadZip } from '../../../utils/zip';
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
  listTables: vi.fn(),
  getTableWeappCode: vi.fn()
}));

vi.mock('../../../utils/tableQr', () => ({
  CARD_PRESETS: [
    { key: '60x90', label: '60 × 90 mm　竖版桌牌', w: 60, h: 90 },
    { key: '80x80', label: '80 × 80 mm　方形', w: 80, h: 80 },
    { key: '100x70', label: '100 × 70 mm　横版', w: 100, h: 70 }
  ],
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

vi.mock('../../../utils/zip', () => ({
  dataUrlToBytes: vi.fn(() => new Uint8Array([1, 2, 3])),
  downloadZip: vi.fn(() => Promise.resolve('mock.zip'))
}));

const listTablesMock = vi.mocked(listTables);
const getTableWeappCodeMock = vi.mocked(getTableWeappCode);
const renderQrDataUrlMock = vi.mocked(renderQrDataUrl);
const buildPrintSheetHtmlMock = vi.mocked(buildPrintSheetHtml);
const openPrintWindowMock = vi.mocked(openPrintWindow);
const dataUrlToBytesMock = vi.mocked(dataUrlToBytes);
const downloadZipMock = vi.mocked(downloadZip);

const tablesFixture: TableInfo[] = [
  { tableId: 1, tableNo: '01', tableName: '大厅01桌', tableCode: 'C001', capacity: 4, status: 1, sortOrder: 1 },
  { tableId: 2, tableNo: '02', tableName: '大厅02桌', tableCode: 'C002', capacity: 6, status: 1, sortOrder: 2 }
];

function mountDialog(overrides: Record<string, unknown> = {}) {
  return mount(BatchQrDialog, {
    props: {
      visible: true,
      shopLogo: 'http://cdn.test/logo.png',
      shopTitle: '测试餐厅',
      h5Base: 'http://example.com',
      allCount: 2,
      listCount: 2,
      queryTableName: '',
      ...overrides
    },
    global: {
      plugins: [TDesign],
      stubs: { teleport: false },
      config: { errorHandler: () => undefined }
    }
  });
}

// 按文案点选单选按钮。
//
// 两个坑:
//  1. 不能按索引选 —— 前面还有「导出范围」「排版方式」两组单选,顺序一变就错位;
//  2. 不能 setValue 原生 input —— TDesign 的 radio 切换逻辑绑在 label 的 click 上,
//     而非 input 的 change(与 Tables.test.ts 里的做法一致)。
async function pickRadio(wrapper: ReturnType<typeof mountDialog>, text: string): Promise<void> {
  const btn = wrapper.findAll('.t-radio-button').find(b => b.text().includes(text));
  if (!btn) throw new Error(`找不到文案为「${text}」的单选按钮`);
  await btn.trigger('click');
  await flushPromises();
}

beforeEach(() => {
  listTablesMock.mockResolvedValue({ total: tablesFixture.length, items: tablesFixture });
  renderQrDataUrlMock.mockResolvedValue('data:image/png;base64,mock');
  getTableWeappCodeMock.mockResolvedValue({
    dataUrl: 'data:image/jpeg;base64,weapp',
    cached: false,
    envVersion: 'release',
    width: 900
  });
  buildPrintSheetHtmlMock.mockReturnValue('<html>print</html>');
  openPrintWindowMock.mockReturnValue(true);
  dataUrlToBytesMock.mockReturnValue(new Uint8Array([1, 2, 3]));
  downloadZipMock.mockResolvedValue('mock.zip');
});

describe('BatchQrDialog 批量导出 / 打印', () => {
  it('渲染导出范围、排版、卡片尺寸与清晰度选项', () => {
    const wrapper = mountDialog();
    const text = wrapper.text();

    expect(text).toContain('导出范围');
    expect(text).toContain('全部桌台');
    expect(text).toContain('当前查询结果');
    expect(text).toContain('排版方式');
    expect(text).toContain('A4 网格');
    expect(text).toContain('每页一张');
    expect(text).toContain('卡片尺寸');
    // TDesign 的 t-select 把选中值渲染在只读 input 的 value 上,而非文本节点,故断言 input value。
    const cardSizeInput = wrapper.find<HTMLInputElement>('.t-select input.t-input__inner');
    expect(cardSizeInput.element.value).toContain('60 × 90 mm');
    expect(text).toContain('清晰度');
    wrapper.unmount();
  });

  it('打印分支并发生成二维码并调用打印页构建与打开', async () => {
    const resolvers: Array<(value: string) => void> = [];
    renderQrDataUrlMock.mockImplementation(
      () =>
        new Promise<string>(resolve => {
          resolvers.push(resolve);
        })
    );

    const wrapper = mountDialog();
    const printBtn = wrapper.findAll('button').find(b => b.text().includes('生成打印页'));
    expect(printBtn).toBeTruthy();
    await printBtn!.trigger('click');
    await flushPromises();

    // 并发池:两行数据同时进入生成(而非串行一张张来),进度在各自完成后推进
    expect(resolvers).toHaveLength(2);

    resolvers[0]('data:image/png;base64,1');
    await flushPromises();
    expect(wrapper.find('.batch-progress').text()).toContain('正在生成 1 / 2');

    resolvers[1]('data:image/png;base64,2');
    await flushPromises();

    expect(buildPrintSheetHtmlMock).toHaveBeenCalledTimes(1);
    expect(openPrintWindowMock).toHaveBeenCalledTimes(1);
    expect(openPrintWindowMock).toHaveBeenCalledWith('<html>print</html>');
    expect(wrapper.find('.batch-progress').text()).toContain('已生成 2 张二维码');
    wrapper.unmount();
  });

  it('下载分支打包 PNG 并调用 downloadZip', async () => {
    const wrapper = mountDialog();
    const zipBtn = wrapper.findAll('button').find(b => b.text().includes('打包下载 PNG'));
    expect(zipBtn).toBeTruthy();
    await zipBtn!.trigger('click');
    await flushPromises();

    expect(downloadZipMock).toHaveBeenCalledTimes(1);
    const [files, zipName] = downloadZipMock.mock.calls[0];
    expect(files).toHaveLength(2);
    expect(files[0].name).toBe('01_大厅01桌.png');
    expect(files[1].name).toBe('02_大厅02桌.png');
    expect(zipName).toMatch(/^桌台点餐码_\d{8}\.zip$/);
    expect(dataUrlToBytesMock).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it('切到小程序码后改走后端出码，不再本地渲染，且文件扩展名为 jpg', async () => {
    const wrapper = mountDialog();
    await pickRadio(wrapper, '小程序码');
    expect(wrapper.text()).toContain('小程序码由微信生成');

    const zipBtn = wrapper.findAll('button').find(b => b.text().includes('打包下载'));
    expect(zipBtn).toBeTruthy();
    await zipBtn!.trigger('click');
    await flushPromises();

    // 小程序码由微信生成:不再走本地 renderQrDataUrl,改为逐个桌台请求后端
    // (后端带 access_token 换取并缓存,前端不接触令牌)。
    expect(renderQrDataUrlMock).not.toHaveBeenCalled();
    expect(getTableWeappCodeMock).toHaveBeenCalledTimes(2);
    expect(getTableWeappCodeMock).toHaveBeenCalledWith('C001', 900);
    expect(getTableWeappCodeMock).toHaveBeenCalledWith('C002', 900);

    const [files] = downloadZipMock.mock.calls[0];
    // 微信回的是 JPEG,扩展名必须跟着真实类型走,否则看图工具会报损坏。
    expect(files[0].name).toBe('01_大厅01桌.jpg');
    expect(files[1].name).toBe('02_大厅02桌.jpg');
    wrapper.unmount();
  });

  it('小程序码的打印提示语与 H5 二维码区分开', async () => {
    const wrapper = mountDialog();
    await pickRadio(wrapper, '小程序码');

    const printBtn = wrapper.findAll('button').find(b => b.text().includes('生成打印页'));
    await printBtn!.trigger('click');
    await flushPromises();

    // 顾客看到「自助点餐」并不知道会跳去哪;小程序码明确写「进小程序点餐」。
    expect(buildPrintSheetHtmlMock.mock.calls[0][1]?.hint).toBe('微信扫一扫 · 进小程序点餐');
    wrapper.unmount();
  });

  it('小程序码出码失败时把后端的中文提示直接抛给用户', async () => {
    getTableWeappCodeMock.mockRejectedValue(new Error('出码失败：小程序页面 pages/index/index 不存在。'));
    const wrapper = mountDialog();
    await pickRadio(wrapper, '小程序码');

    const printBtn = wrapper.findAll('button').find(b => b.text().includes('生成打印页'));
    await printBtn!.trigger('click');
    await flushPromises();

    // 这类错误几乎都是「页面没发布」，必须原样透出而不是吞成「生成失败」
    expect(MessagePlugin.error).toHaveBeenCalledWith(expect.stringContaining('小程序页面 pages/index/index 不存在'));
    expect(openPrintWindowMock).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
