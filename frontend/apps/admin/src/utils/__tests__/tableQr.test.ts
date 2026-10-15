import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  CARD_PRESETS,
  QR_PIXEL_PRESETS,
  buildCardImagesSheetHtml,
  buildPrintSheetHtml,
  buildPrintToolbar,
  openPrintWindow,
  preparePrintWindow,
  writePrintWindow,
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

/** openPrintWindow 用的是 window.open 出来的真实 Window，这里做一个够用的替身。 */
function createPrintWindowStub() {
  const btn = document.createElement('button');
  btn.setAttribute('data-print', '');
  const listeners: Record<string, Array<() => void>> = {};
  const doc = {
    readyState: 'loading',
    write: vi.fn(),
    open: vi.fn(),
    close: vi.fn(),
    querySelector: vi.fn((sel: string) => (sel === '[data-print]' ? btn : null)),
    images: [] as unknown as HTMLCollectionOf<HTMLImageElement>
  };
  const w = {
    closed: false,
    document: doc,
    print: vi.fn(),
    focus: vi.fn(),
    addEventListener: vi.fn((type: string, fn: () => void) => {
      (listeners[type] ||= []).push(fn);
    })
  };
  return { w, btn, doc, fireLoad: () => (listeners.load || []).forEach(fn => fn()) };
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

  // 生产 CSP 是 script-src 'self'（无 'unsafe-inline'），而 about:blank 会继承 opener 的
  // CSP —— 打印页里一旦有内联 <script> 或 onclick，Chromium 会直接拦掉，
  // 表现为「页面正常渲染，但右上角打印按钮点了没反应」。这两条用来钉死这个约束。
  it('打印页不含内联脚本与 onclick，只留 data-print 钩子', () => {
    const card = { tableNo: 'A1', tableName: '大厅桌', dataUrl: 'data:image/png;base64,xxx' };

    for (const html of [
      buildPrintSheetHtml([card], { cutLine: true }),
      buildCardImagesSheetHtml([{ dataUrl: card.dataUrl, name: 'A1_大厅桌' }], {})
    ]) {
      expect(html).toContain('data-print');
      expect(html).not.toContain('onclick');
      expect(html).not.toContain('<script');
    }
  });

  it('buildPrintToolbar 只输出 data-print 钩子并转义文案', () => {
    const html = buildPrintToolbar('店 & <b>', '纸张 A4');

    expect(html).toContain('data-print');
    expect(html).not.toContain('onclick');
    expect(html).toContain('店 &amp; &lt;b&gt;');
  });

  it('preparePrintWindow 先开窗口写占位页，writePrintWindow 覆盖后再绑定打印', () => {
    const stub = createPrintWindowStub();
    vi.spyOn(window, 'open').mockReturnValue(stub.w as unknown as Window);

    // 点击时先把窗口开出来(保住用户激活态),此时内容还没生成
    const w = preparePrintWindow();
    expect(w).toBe(stub.w);
    expect(stub.doc.write).toHaveBeenCalledTimes(1);
    expect(stub.doc.write.mock.calls[0][0]).toContain('正在生成打印页');
    // 占位页阶段不该挂打印动作,否则会对着一个空页面 print()
    expect(stub.w.addEventListener).not.toHaveBeenCalled();

    const html = buildCardImagesSheetHtml([{ dataUrl: 'data:image/png;base64,xxx' }], {});
    writePrintWindow(stub.w as unknown as Window, html);
    expect(stub.doc.open).toHaveBeenCalled();
    expect(stub.doc.write).toHaveBeenCalledWith(html);

    stub.fireLoad();
    stub.btn.click();
    expect(stub.w.print).toHaveBeenCalledTimes(1);
  });

  it('A4 网格预留页眉高度，卡片不会溢出纸张', () => {
    const cards = Array.from({ length: 20 }, () => ({ dataUrl: 'data:image/png;base64,xxx' }));

    // A4 可用高 277mm，扣掉 8mm 页眉后剩 269mm。50×50 原先排 5 行(274mm + 页眉 ≈
    // 282mm)会溢出，Chrome 于是每页后面多打一张空白页；现在应收敛到 4 行。
    expect(buildCardImagesSheetHtml(cards, { cardW: 50, cardH: 50, columns: 3 })).toContain('每页 3 × 4');
    // 常用的 60×80 不受影响，仍是 3 列 3 行
    expect(buildCardImagesSheetHtml(cards, { cardW: 60, cardH: 80, columns: 3 })).toContain('每页 3 × 3');
  });

  it('openPrintWindow 由 opener 绑定打印按钮并自动唤起打印', async () => {
    const stub = createPrintWindowStub();
    vi.spyOn(window, 'open').mockReturnValue(stub.w as unknown as Window);
    const html = buildCardImagesSheetHtml([{ dataUrl: 'data:image/png;base64,xxx' }], {});

    expect(openPrintWindow(html)).toBe(true);
    expect(stub.doc.write).toHaveBeenCalledWith(html);

    stub.fireLoad();
    // 点击按钮 → 弹窗 print()
    stub.btn.click();
    expect(stub.w.print).toHaveBeenCalledTimes(1);

    // 图片解码完成后自动打印一次（与按钮共用同一个 print）
    await new Promise(r => setTimeout(r, 400));
    expect(stub.w.print).toHaveBeenCalledTimes(2);
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
