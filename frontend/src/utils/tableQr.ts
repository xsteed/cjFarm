// 桌台二维码生成 / 排版 / 打印工具
//
// 关键点:
// 1. 纠错级别固定 H(30% 冗余),中间才放得下 Logo 或桌号而仍能正常识别;
// 2. Logo 与二维码同源加载(生产由后端托管、开发走 vite 代理),不会污染画布,
//    因此可以正常 toDataURL 导出;若使用外链图片则会附带 crossOrigin 尝试。
// 3. 打印使用 mm 单位排版:A4 网格(亚克力桌牌常见)或每页一张(标签机)。

import QRCode from 'qrcode';

export interface CardPreset {
  key: string;
  label: string;
  w: number;
  h: number;
}

export interface QrPixelPreset {
  key: number;
  label: string;
}

export interface QrCard {
  tableNo?: string | number;
  tableName?: string;
  dataUrl: string;
  url?: string;
}

export interface PrintSheetOptions {
  mode?: 'sheet' | 'label';
  columns?: number;
  cardW?: number;
  cardH?: number;
  gap?: number;
  showShop?: boolean;
  shopName?: string;
  showTableName?: boolean;
  showTableNo?: boolean;
  hint?: string;
  cutLine?: boolean;
}

interface QrOptions {
  size?: number;
  margin?: number;
  ecc?: string;
  logo?: string;
  logoRatio?: number;
  centerText?: string;
  centerTextColor?: string;
}

/** 卡片尺寸预设(单位 mm)。 */
export const CARD_PRESETS: CardPreset[] = [
  { key: '60x90', label: '60 × 90 mm　竖版桌牌', w: 60, h: 90 },
  { key: '80x80', label: '80 × 80 mm　方形', w: 80, h: 80 },
  { key: '100x70', label: '100 × 70 mm　横版', w: 100, h: 70 },
  { key: '90x130', label: '90 × 130 mm　大立牌', w: 90, h: 130 },
  { key: '50x50', label: '50 × 50 mm　小方标', w: 50, h: 50 }
];

/** 二维码画布尺寸预设(像素,导出用)。 */
export const QR_PIXEL_PRESETS: QrPixelPreset[] = [
  { key: 360, label: '360 px（预览 / 网页）' },
  { key: 640, label: '640 px（常规打印）' },
  { key: 900, label: '900 px（高清打印）' },
  { key: 1200, label: '1200 px（大幅面）' }
];

const DEFAULTS: Required<QrOptions> = {
  size: 640,
  margin: 2,
  ecc: 'H',
  logo: '',
  logoRatio: 0.22,
  centerText: '',
  centerTextColor: '#1f2329'
};

/** 加载图片,返回 HTMLImageElement。 */
export function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const abs = /^https?:\/\//i.test(src);
    if (abs && typeof window !== 'undefined' && !src.startsWith(window.location.origin)) {
      img.crossOrigin = 'anonymous';
    }
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error('图片加载失败:' + src));
    img.src = src;
  });
}

function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number): void {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.lineTo(x + w - rr, y);
  ctx.arcTo(x + w, y, x + w, y + rr, rr);
  ctx.lineTo(x + w, y + h - rr);
  ctx.arcTo(x + w, y + h, x + w - rr, y + h, rr);
  ctx.lineTo(x + rr, y + h);
  ctx.arcTo(x, y + h, x, y + h - rr, rr);
  ctx.lineTo(x, y + rr);
  ctx.arcTo(x, y, x + rr, y, rr);
  ctx.closePath();
}

/**
 * 渲染二维码到 Canvas(含中心 Logo 或中心文字)。
 * @param text 二维码内容
 * @param opts 见 DEFAULTS
 */
export async function renderQrCanvas(text: string, opts: QrOptions = {}): Promise<HTMLCanvasElement> {
  const o = { ...DEFAULTS, ...opts } as Required<QrOptions>;
  const canvas = document.createElement('canvas');
  await QRCode.toCanvas(canvas, String(text), {
    width: o.size,
    margin: o.margin,
    errorCorrectionLevel: o.ecc,
    color: { dark: '#000000', light: '#FFFFFF' }
  });
  const ctx = canvas.getContext('2d')!;
  const S = canvas.width;

  if (o.logo) {
    const img = await loadImage(o.logo);
    const box = Math.round(S * o.logoRatio);
    const pad = Math.max(2, Math.round(box * 0.1));
    const bx = (S - box) / 2;
    // 白色圆角底板,避免 Logo 与码点直接叠加影响识别
    ctx.fillStyle = '#ffffff';
    roundRect(ctx, bx, bx, box, box, box * 0.24);
    ctx.fill();
    const inner = box - pad * 2;
    const scale = Math.min(inner / img.width, inner / img.height);
    const w = img.width * scale;
    const h = img.height * scale;
    ctx.drawImage(img, (S - w) / 2, (S - h) / 2, w, h);
  } else if (o.centerText) {
    const box = Math.round(S * o.logoRatio);
    const bx = (S - box) / 2;
    ctx.fillStyle = '#ffffff';
    roundRect(ctx, bx, bx, box, box, box * 0.24);
    ctx.fill();
    const txt = String(o.centerText);
    // 字号随字数自适应,保证不溢出白底
    let fs = Math.round(box * (txt.length <= 2 ? 0.62 : txt.length <= 4 ? 0.44 : 0.3));
    fs = Math.max(10, fs);
    ctx.fillStyle = o.centerTextColor;
    ctx.font = `bold ${fs}px "PingFang SC","Microsoft YaHei",system-ui,sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(txt, S / 2, S / 2 + fs * 0.04);
  }
  return canvas;
}

/** 生成二维码 PNG dataURL。 */
export async function renderQrDataUrl(text: string, opts: QrOptions = {}): Promise<string> {
  const canvas = await renderQrCanvas(text, opts);
  try {
    return canvas.toDataURL('image/png');
  } catch {
    throw new Error('Logo 图片跨域导致无法导出，请把 Logo 上传到系统内的「店铺 Logo」');
  }
}

// ============ 打印排版 ============

const HTML_ESCAPE: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;'
};

const esc = (s: unknown): string => String(s == null ? '' : s).replace(/[&<>"]/g, c => HTML_ESCAPE[c]);

function cardInner(card: QrCard, o: Required<PrintSheetOptions>): string {
  const horizontal = o.cardW > o.cardH;
  const qrSize = horizontal ? Math.round(o.cardH * 0.68) : Math.round(Math.min(o.cardW, o.cardH) * 0.64);
  const qrHtml = `<img src="${card.dataUrl}" style="width:${qrSize}mm;height:${qrSize}mm;display:block" alt="二维码">`;

  const lines: string[] = [];
  if (o.showShop && o.shopName) {
    lines.push(
      `<div style="font-size:3.1mm;font-weight:600;color:#F0481F;letter-spacing:.2mm">${esc(o.shopName)}</div>`
    );
  }
  if (o.showTableName && card.tableName) {
    lines.push(
      `<div style="font-size:4.4mm;font-weight:700;color:#1f2329;line-height:1.2">${esc(card.tableName)}</div>`
    );
  }
  if (o.showTableNo && card.tableNo) {
    lines.push(`<div style="font-size:3.2mm;color:#5b6270">桌号 ${esc(card.tableNo)}</div>`);
  }
  if (o.hint) {
    lines.push(`<div style="font-size:2.7mm;color:#9299a6;line-height:1.35">${esc(o.hint)}</div>`);
  }

  return `<div style="display:flex;flex-direction:${horizontal ? 'row' : 'column'};align-items:center;justify-content:${horizontal ? 'flex-start' : 'center'};gap:${horizontal ? '4mm' : '2.4mm'};width:100%;height:100%">
    ${qrHtml}
    <div style="display:flex;flex-direction:column;align-items:${horizontal ? 'flex-start' : 'center'};justify-content:center;gap:1.6mm;text-align:${horizontal ? 'left' : 'center'};overflow:hidden">
      ${lines.join('')}
    </div>
  </div>`;
}

function cardHtml(card: QrCard, o: Required<PrintSheetOptions>): string {
  const border = o.cutLine ? 'border:0.3mm dashed #cfd4dc;' : '';
  return `<div style="box-sizing:border-box;width:${o.cardW}mm;height:${o.cardH}mm;padding:4mm;background:#fff;${border}display:flex;align-items:center;justify-content:center;break-inside:avoid;page-break-inside:avoid">
    ${cardInner(card, o)}
  </div>`;
}

/**
 * 生成打印用 HTML(新窗口直接打印或另存为 PDF)。
 * @param cards 桌台二维码卡片
 * @param opts mode(sheet|label) / columns / cardW / cardH / gap / showShop / shopName / showTableName / showTableNo / hint / cutLine
 */
export function buildPrintSheetHtml(cards: QrCard[], opts: PrintSheetOptions = {}): string {
  const o = {
    mode: 'sheet',
    columns: 3,
    cardW: 60,
    cardH: 90,
    gap: 6,
    showShop: true,
    shopName: '',
    showTableName: true,
    showTableNo: true,
    hint: '微信扫码 · 自助点餐',
    cutLine: true,
    ...opts
  } as Required<PrintSheetOptions>;
  const A4W = 210;
  const A4H = 297;
  const MARGIN = 10;
  const usableW = A4W - MARGIN * 2;
  const usableH = A4H - MARGIN * 2;

  let cols: number;
  let rows: number;
  let gap = o.gap;
  if (o.mode === 'label') {
    cols = 1;
    rows = 1;
  } else {
    // 卡片宽度固定,列数按纸张可用宽度自适应:
    // 优先满足用户选择的列数,通过压缩间距让它们排下(最小 2mm);
    // 若即使最小间距也排不下,则退回到实际能放下的最多列数,避免溢出到纸张外。
    const minGap = 2;
    const maxCols = Math.max(1, Math.floor((usableW + minGap) / (o.cardW + minGap)));
    cols = Math.max(1, Math.min(o.columns, maxCols));
    if (cols > 1) {
      const maxGap = (usableW - cols * o.cardW) / (cols - 1);
      gap = Math.max(minGap, Math.min(o.gap, maxGap));
    }
    rows = Math.max(1, Math.floor((usableH + gap) / (o.cardH + gap)));
  }
  const perPage = cols * rows;
  const pages: QrCard[][] = [];
  for (let i = 0; i < cards.length; i += perPage) pages.push(cards.slice(i, i + perPage));

  const pageCss =
    o.mode === 'label'
      ? `@page { size: ${o.cardW}mm ${o.cardH}mm; margin: 0; }`
      : `@page { size: A4; margin: ${MARGIN}mm; }`;

  const pageHtml = pages
    .map((items, pi) => {
      const isLast = pi === pages.length - 1;
      const head =
        o.mode === 'label'
          ? ''
          : `<div class="phead"><span>${esc(o.shopName || '')}&emsp;每页 ${cols} × ${rows}</span><span>第 ${pi + 1} / ${pages.length} 页&emsp;共 ${cards.length} 张</span></div>`;
      const grid = `<div class="grid" style="grid-template-columns:repeat(${cols},${o.cardW}mm);gap:${gap}mm">${items
        .map(c => cardHtml(c, o))
        .join('')}</div>`;
      // 末页需显式关闭分页,否则浏览器会多打一张空白页(末页并非 body 的 last-child)
      return `<section class="page${isLast ? ' last' : ''}">${head}${grid}</section>`;
    })
    .join('');

  return `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>桌台点餐码 ${o.shopName ? '· ' + esc(o.shopName) : ''}</title>
<style>
  * { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  html,body { margin:0; padding:0; background:#f3f4f6; font-family:"PingFang SC","Microsoft YaHei",system-ui,sans-serif; }
  ${pageCss}
  .page { background:#fff; box-sizing:border-box; break-after:page; page-break-after:always; }
  .page.last { break-after:auto; page-break-after:auto; }
  .phead { display:flex; justify-content:space-between; font-size:9pt; color:#9299a6; margin-bottom:4mm; }
  .grid { display:grid; justify-content:center; align-content:start; }
  .toolbar { position:fixed; left:0; right:0; top:0; background:#fff; border-bottom:1px solid #e6e8eb; padding:10px 16px;
    display:flex; align-items:center; gap:12px; z-index:9; box-shadow:0 2px 8px rgba(0,0,0,.06); }
  .toolbar b { font-size:14px; color:#1f2329; }
  .toolbar span { font-size:12px; color:#8a919f; }
  .toolbar button { margin-left:auto; background:#F0481F; color:#fff; border:0; border-radius:6px; padding:8px 18px; font-size:13px; cursor:pointer; }
  .spacer { height:52px; }
  @media print {
    html,body { background:#fff; }
    .toolbar, .spacer { display:none !important; }
    ${o.mode === 'label' ? '.page { padding:0 }' : '.page { padding:0 }'}
  }
</style></head>
<body>
  <div class="toolbar">
    <b>桌台点餐码 · 共 ${cards.length} 张</b>
    <span>打印时请关闭「页眉页脚」并选择实际纸张（${o.mode === 'label' ? o.cardW + '×' + o.cardH + 'mm' : 'A4'}）</span>
    <button onclick="window.print()">打印 / 另存为 PDF</button>
  </div>
  <div class="spacer"></div>
  ${pageHtml}
  <script>
    // 等图片解码完再唤起打印,避免打出空白二维码
    window.addEventListener('load', function () {
      var imgs = Array.prototype.slice.call(document.images);
      Promise.all(imgs.map(function (i) { return i.decode ? i.decode().catch(function(){}) : Promise.resolve(); }))
        .then(function () { setTimeout(function () { window.print(); }, 350); });
    });
  </script>
</body></html>`;
}

/** 在新窗口打开打印页。返回是否成功打开(被拦截返回 false)。 */
export function openPrintWindow(html: string): boolean {
  const w = window.open('', '_blank');
  if (!w) return false;
  w.document.write(html);
  w.document.close();
  return true;
}
