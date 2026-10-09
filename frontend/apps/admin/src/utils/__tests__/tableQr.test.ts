import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  CARD_PRESETS,
  QR_PIXEL_PRESETS,
  buildPrintSheetHtml,
  renderQrCanvas,
  renderQrDataUrl,
  renderCardDataUrl,
  shopBrandName
} from '../tableQr';

// qrcode 的 toCanvas 在 jsdom 下不可用，mock 成仅设置画布尺寸。
vi.mock('qrcode', () => ({
  default: {
    toCanvas: vi.fn(async (canvas: HTMLCanvasElement, _text: string, opts?: { width?: number }) => {
      const size = opts?.width ?? 640;
      canvas.width = size;
      canvas.height = size;
    })
  }
}));

function createCtxStub() {
  return {
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 1,
    font: '',
    textAlign: '',
    textBaseline: '',
    beginPath: vi.fn(),
    moveTo: vi.fn(),
    lineTo: vi.fn(),
    arcTo: vi.fn(),
    closePath: vi.fn(),
    fill: vi.fn(),
    stroke: vi.fn(),
    fillRect: vi.fn(),
    scale: vi.fn(),
    save: vi.fn(),
    restore: vi.fn(),
    drawImage: vi.fn(),
    fillText: vi.fn(),
    measureText: vi.fn(() => ({ width: 10 }))
  };
}

describe('tableQr 桌台二维码', () => {
  let ctx: ReturnType<typeof createCtxStub>;

  beforeEach(() => {
    vi.restoreAllMocks();
    ctx = createCtxStub();
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D);
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue('data:image/png;base64,stub');
  });

  it('shopBrandName 从组合店铺名中提取品牌名', () => {
    expect(shopBrandName('长健农场 · 柴火农家土菜')).toBe('长健农场');
    expect(shopBrandName('青山茶舍•分店标语')).toBe('青山茶舍');
    expect(shopBrandName('纯品牌名')).toBe('纯品牌名');
    expect(shopBrandName('  前后空格 · 标语  ')).toBe('前后空格');
    expect(shopBrandName('')).toBe('');
  });

  it('CARD_PRESETS 非空且字段完整', () => {
    expect(CARD_PRESETS.length).toBeGreaterThan(0);
    for (const p of CARD_PRESETS) {
      expect(p.key).toBeTruthy();
      expect(p.label).toBeTruthy();
      expect(p.w).toBeGreaterThan(0);
      expect(p.h).toBeGreaterThan(0);
    }
  });

  it('QR_PIXEL_PRESETS 非空且字段完整', () => {
    expect(QR_PIXEL_PRESETS.length).toBeGreaterThan(0);
    for (const p of QR_PIXEL_PRESETS) {
      expect(p.key).toBeGreaterThan(0);
      expect(p.label).toBeTruthy();
    }
  });

  it('buildPrintSheetHtml 包含卡片名/桌号/裁剪线', () => {
    const html = buildPrintSheetHtml([{ tableNo: 'A1', tableName: '大厅桌', dataUrl: 'data:image/png;base64,xxx' }], {
      shopName: '测试店',
      showTableName: true,
      showTableNo: true,
      cutLine: true
    });

    expect(html).toContain('测试店');
    expect(html).toContain('大厅桌');
    expect(html).toContain('桌号 A1');
    expect(html).toContain('border:0.3mm dashed');
  });

  it('buildPrintSheetHtml 关闭裁剪线时不输出裁剪线', () => {
    const html = buildPrintSheetHtml([{ tableNo: 'A1', tableName: '大厅桌', dataUrl: 'data:image/png;base64,xxx' }], {
      cutLine: false
    });

    expect(html).not.toContain('border:0.3mm dashed');
  });

  it('buildPrintSheetHtml 转义 HTML 特殊字符', () => {
    const html = buildPrintSheetHtml(
      [{ tableNo: '<b>1</b>', tableName: '桌&"名"', dataUrl: 'data:image/png;base64,xxx' }],
      { shopName: '<script>alert(1)</script>', hint: '<提示>' }
    );

    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;');
    expect(html).not.toContain('<script>alert(1)</script>');
    expect(html).toContain('桌&amp;&quot;名&quot;');
    expect(html).toContain('桌号 &lt;b&gt;1&lt;/b&gt;');
  });

  it('renderQrCanvas 基础渲染返回画布', async () => {
    const canvas = await renderQrCanvas('https://x.test/t/1', { size: 100 });

    expect(canvas).toBeInstanceOf(HTMLCanvasElement);
    expect(canvas.width).toBe(100);
    expect(canvas.height).toBe(100);
  });

  it('renderQrCanvas 中心文字分支调用 fillText', async () => {
    const canvas = await renderQrCanvas('https://x.test/t/1', { size: 100, centerText: 'A1' });

    expect(ctx.fillText).toHaveBeenCalled();
    expect(canvas.width).toBe(100);
  });

  it('renderQrDataUrl 成功返回 dataURL', async () => {
    const url = await renderQrDataUrl('x');
    expect(url).toBe('data:image/png;base64,stub');
  });

  it('renderQrDataUrl 导出失败抛出跨域文案', async () => {
    vi.mocked(HTMLCanvasElement.prototype.toDataURL).mockImplementation(() => {
      throw new Error('canvas tainted');
    });

    await expect(renderQrDataUrl('x')).rejects.toThrow('Logo 图片跨域');
  });

  it('renderCardDataUrl 返回含商家名/提醒的卡片 dataURL', async () => {
    const url = await renderCardDataUrl({
      text: 'https://x.test/t/1',
      shopName: '测试店',
      title: '扫码点餐',
      hint: '微信扫码 · 自助点餐',
      tableName: '大厅桌',
      tableNo: 'A1'
    });

    expect(url).toBe('data:image/png;base64,stub');
    expect(ctx.drawImage).toHaveBeenCalled();
    expect(ctx.fillText).toHaveBeenCalled();
  });
});
