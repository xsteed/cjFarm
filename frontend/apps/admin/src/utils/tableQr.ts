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
  { key: '60x80', label: '60 × 80 mm　竖版桌牌', w: 60, h: 80 },
  { key: '80x80', label: '80 × 80 mm　方形', w: 80, h: 80 },
  { key: '100x70', label: '100 × 70 mm　横版', w: 100, h: 70 },
  { key: '90x130', label: '90 × 130 mm　大立牌', w: 90, h: 130 },
  { key: '50x50', label: '50 × 50 mm　小方标', w: 50, h: 50 }
];

/**
 * 从系统配置的「店铺名称」提取品牌名。
 * 配置值可能是「品牌 · 门店标语」的组合(顾客端 H5/小程序顶部整体展示),
 * 桌牌二维码顶部只放品牌名,取分隔符前的部分。
 */
export function shopBrandName(raw: string): string {
  return String(raw || '')
    .split(/[·•]/)[0]
    .trim();
}

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
  // 中心元素的「参考长边占比」:Logo 会按自身宽高比归一化到该视觉面积,
  // 横版/方形/圆形因此占据一致的重量,而不是都被塞进同一个正方形。
  logoRatio: 0.2,
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

/** 把像素尺寸向上取整到「偶数个二维码模块」,保证中心底板落在模块网格上、边缘干净。 */
function snapToModules(px: number, modulePx: number): number {
  const m = Math.max(4, Math.round(px / modulePx));
  const even = m % 2 === 0 ? m : m + 1;
  return even * modulePx;
}

/**
 * 扫描 Logo 的透明包围盒,裁掉其自带的透明/白边,使不同来源的 Logo 拥有相同的「内容重量」。
 * 跨域(无 CORS)或读图失败时回退为整图(不裁剪)。
 */
async function measureLogo(img: HTMLImageElement): Promise<{ sx: number; sy: number; sw: number; sh: number; rw: number; rh: number }> {
  try {
    const maxScan = 240;
    const scale = Math.min(1, maxScan / Math.max(img.width, img.height));
    const cw = Math.max(1, Math.round(img.width * scale));
    const ch = Math.max(1, Math.round(img.height * scale));
    const off = document.createElement('canvas');
    off.width = cw;
    off.height = ch;
    const c = off.getContext('2d', { willReadFrequently: true })!;
    c.drawImage(img, 0, 0, cw, ch);
    const d = c.getImageData(0, 0, cw, ch).data;
    let minX = cw, minY = ch, maxX = -1, maxY = -1;
    for (let y = 0; y < ch; y++) {
      for (let x = 0; x < cw; x++) {
        if (d[(y * cw + x) * 4 + 3] > 16) {
          if (x < minX) minX = x;
          if (x > maxX) maxX = x;
          if (y < minY) minY = y;
          if (y > maxY) maxY = y;
        }
      }
    }
    if (maxX < 0) return { sx: 0, sy: 0, sw: img.width, sh: img.height, rw: img.width, rh: img.height };
    const sx = Math.max(0, Math.floor(minX / scale));
    const sy = Math.max(0, Math.floor(minY / scale));
    const ex = Math.ceil(maxX / scale);
    const ey = Math.ceil(maxY / scale);
    const sw = Math.min(img.width - sx, ex - sx);
    const sh = Math.min(img.height - sy, ey - sy);
    return { sx, sy, sw, sh, rw: sw, rh: sh };
  } catch {
    return { sx: 0, sy: 0, sw: img.width, sh: img.height, rw: img.width, rh: img.height };
  }
}

/** 从已生成的画布推算二维码模块像素尺寸(失败回退)。 */
function getModulePx(S: number, text: string, o: Required<QrOptions>): number {
  try {
    const qr = QRCode.create(String(text), {
      errorCorrectionLevel: o.ecc,
      margin: o.margin
    } as never);
    const m = (qr.modules as unknown as { size: number }).size;
    if (m > 0) return S / (m + o.margin * 2);
  } catch {
    /* 回退 */
  }
  return S / 45;
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
  const modulePx = getModulePx(S, text, o);

  if (o.logo) {
    const img = await loadImage(o.logo);
    const b = await measureLogo(img);
    const ar = b.rw / b.rh; // 内容宽高比
    // 按面积归一化:不论横版/方形/圆形,覆盖面积接近一致,视觉重量统一。
    const base = S * (o.logoRatio || 0.2);
    const maxSide = S * 0.3; // 单边安全上限(ECC H 仍可识别)
    let drawnW: number;
    let drawnH: number;
    if (ar >= 1) {
      drawnW = Math.min(base * Math.sqrt(ar), maxSide);
      drawnH = drawnW / ar;
    } else {
      drawnH = Math.min(base * Math.sqrt(1 / ar), maxSide);
      drawnW = drawnH * ar;
    }
    // 统一留白:按模块尺寸取呼吸边距,而不是固定百分比,使不同尺寸下一致。
    const pad = Math.max(modulePx * 1.2, S * 0.012);
    let plateW = snapToModules(drawnW + pad * 2, modulePx);
    let plateH = snapToModules(drawnH + pad * 2, modulePx);
    // 极端长条时按单边 ≤ 36% 收口,保证不破坏识别。
    const cap = S * 0.36;
    if (plateW > cap || plateH > cap) {
      const k = cap / Math.max(plateW, plateH);
      plateW = snapToModules(plateW * k, modulePx);
      plateH = snapToModules(plateH * k, modulePx);
      const kk = Math.min(plateW / (drawnW + pad * 2), plateH / (drawnH + pad * 2));
      drawnW *= kk;
      drawnH *= kk;
    }
    const bx = (S - plateW) / 2;
    const by = (S - plateH) / 2;
    // 圆角随短边走:方形→圆角方,横版→胶囊,视觉更协调。
    const radius = Math.min(plateW, plateH) * 0.28;
    // 白色圆角底板,避免 Logo 与码点直接叠加影响识别
    ctx.fillStyle = '#ffffff';
    roundRect(ctx, bx, by, plateW, plateH, radius);
    ctx.fill();
    // 按底板圆角裁剪,Logo 四角与底板形状一致;内容填满内框、四周留白均匀。
    ctx.save();
    roundRect(ctx, bx, by, plateW, plateH, radius);
    ctx.clip();
    const ix = bx + (plateW - drawnW) / 2;
    const iy = by + (plateH - drawnH) / 2;
    ctx.drawImage(img, b.sx, b.sy, b.sw, b.sh, ix, iy, drawnW, drawnH);
    ctx.restore();
  } else if (o.centerText) {
    const box = snapToModules(S * (o.logoRatio || 0.22), modulePx);
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

// ============ 单张桌牌卡片(矩形,含商家名/扫码提醒) ============

export interface QrCardOptions extends QrOptions {
  text?: string; // 二维码内容(H5 点餐链接),与 qrImageSrc 二选一
  width?: number; // 卡片逻辑宽度(px),默认 480
  ratio?: number; // 高/宽,默认 1.5
  scale?: number; // 清晰度倍率,默认 2
  shopName?: string; // 顶部商家名(品牌色)
  title?: string; // 主提示,默认「扫码点餐」
  hint?: string; // 副提示(如「微信扫码 · 自助点餐」)
  tableName?: string;
  tableNo?: string | number;
  accent?: string; // 品牌色,默认 #F0481F
  qrImageSrc?: string; // 直接嵌入已有图片(如微信小程序码),与 text 二选一
}

/**
 * 渲染一张矩形桌牌卡片:圆角白底 + 商家名 + 二维码(含 Logo) + 「扫码点餐」提醒 + 桌号。
 * 返回的画布可直接 toDataURL 导出 PNG,也可嵌入打印页。
 */
export async function renderCardCanvas(params: QrCardOptions): Promise<HTMLCanvasElement> {
  const scale = Math.max(1, params.scale || 2);
  const W = Math.round(params.width || 480);
  const ratio = params.ratio || 1.5;
  const H = Math.round(W * ratio);
  const accent = params.accent || '#F0481F';
  const sans = '"PingFang SC","Microsoft YaHei",system-ui,sans-serif';

  const canvas = document.createElement('canvas');
  canvas.width = W * scale;
  canvas.height = H * scale;
  const ctx = canvas.getContext('2d')!;
  ctx.scale(scale, scale);

  // 外层浅色底,卡片像一张贴纸
  ctx.fillStyle = '#f5f6f8';
  ctx.fillRect(0, 0, W, H);

  // 白色卡片 + 圆角细边框
  const m = 12;
  const cx = m;
  const cy = m;
  const cw = W - m * 2;
  const ch = H - m * 2;
  roundRect(ctx, cx, cy, cw, ch, 20);
  ctx.fillStyle = '#ffffff';
  ctx.fill();
  ctx.lineWidth = 1;
  ctx.strokeStyle = '#eceef1';
  ctx.stroke();

  const pad = 16;
  const innerW = cw - pad * 2;

  // 头部:品牌色块 + 白色店名 + 白色小副题(两侧细线装饰),餐饮桌牌的经典版式
  const shopFont = Math.round(W * 0.052);
  const subFont = Math.round(W * 0.03);
  const shopName = params.shopName;
  const hasShop = !!shopName;
  let headH = 0;
  if (shopName) {
    headH = Math.round(W * 0.15);
    // 顶部两角圆角的色块
    const cr = 20;
    ctx.beginPath();
    ctx.moveTo(cx, cy + headH);
    ctx.lineTo(cx, cy + cr);
    ctx.arcTo(cx, cy, cx + cr, cy, cr);
    ctx.lineTo(cx + cw - cr, cy);
    ctx.arcTo(cx + cw, cy, cx + cw, cy + cr, cr);
    ctx.lineTo(cx + cw, cy + headH);
    ctx.closePath();
    ctx.fillStyle = accent;
    ctx.fill();
    // 店名(白)
    ctx.fillStyle = '#ffffff';
    ctx.font = `700 ${shopFont}px ${sans}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    const nameY = cy + headH * 0.4;
    ctx.fillText(shopName, W / 2, nameY);
    // 副题(白,略透) + 两侧细线
    const headText = params.title || '扫码点餐';
    const subY = nameY + shopFont * 0.62 + subFont;
    ctx.font = `500 ${subFont}px ${sans}`;
    ctx.fillStyle = 'rgba(255,255,255,0.92)';
    ctx.fillText(headText, W / 2, subY);
    const tw2 = ctx.measureText(headText).width;
    const lineLen = Math.round(W * 0.05);
    ctx.strokeStyle = 'rgba(255,255,255,0.55)';
    ctx.lineWidth = 1;
    for (const dir of [-1, 1]) {
      const x1 = W / 2 + dir * (tw2 / 2 + 12);
      ctx.beginPath();
      ctx.moveTo(x1, subY);
      ctx.lineTo(x1 + dir * lineLen, subY);
      ctx.stroke();
    }
  }

  // 底部/侧栏文字尺寸
  const titleFont = Math.round(W * 0.07);
  const tableFont = Math.round(W * 0.038);
  const hintFont = Math.round(W * 0.027);
  const pillH = tableFont + 18;
  const tableLabel = [params.tableName, params.tableNo ? `桌号 ${params.tableNo}` : '']
    .filter(Boolean)
    .join(' · ');
  const horizontal = ratio < 1;

  let footH = 4;
  if (horizontal) {
    footH = 0; // 横版文字放二维码右侧,不占底部
  } else {
    if (!hasShop && params.title) footH += titleFont + 12;
    if (tableLabel) footH += pillH + 8;
    if (params.hint) footH += hintFont;
  }

  const platePad = Math.max(8, Math.round(W * 0.02)); // 白色衬底四周留白
  let plateX: number;
  let plateY: number;
  let qrSide: number;
  if (horizontal) {
    // 横版:左码右文。二维码占左侧正方形,右侧垂直居中排文字。
    const bodyTop = cy + (hasShop ? headH + 4 : pad + 4);
    const bodyBottom = cy + ch - pad;
    const bodyH = Math.max(120, bodyBottom - bodyTop);
    qrSide = Math.max(
      100,
      Math.min(bodyH - platePad * 2, Math.round(cw * 0.5) - platePad * 2)
    );
    plateX = cx + pad;
    plateY = bodyTop + Math.max(0, (bodyH - (qrSide + platePad * 2)) / 2);
  } else {
    // 竖版/方形:居中,二维码按宽度尽量铺满,压掉上下空白。
    const qrTop = cy + (hasShop ? headH + 4 : pad + 4);
    const qrBottom = cy + ch - pad - footH;
    const qrAreaH = Math.max(100, qrBottom - qrTop);
    qrSide = Math.max(
      100,
      Math.min(innerW - platePad * 2, qrAreaH - platePad * 2)
    );
    plateX = cx + (cw - (qrSide + platePad * 2)) / 2;
    plateY = qrTop + (qrAreaH - (qrSide + platePad * 2)) / 2;
  }

  // 生成或嵌入二维码(Logo 由 renderQrCanvas 画在中心)
  let draw: HTMLCanvasElement | HTMLImageElement;
  if (params.qrImageSrc) {
    draw = await loadImage(params.qrImageSrc);
  } else {
    draw = await renderQrCanvas(params.text || '', {
      size: Math.max(300, Math.round(qrSide * scale)),
      logo: params.logo,
      centerText: params.centerText
    });
  }
  // 白色圆角衬底 + 细边框,二维码 contain 居中其中
  const plateSide = qrSide + platePad * 2;
  roundRect(ctx, plateX, plateY, plateSide, plateSide, 12);
  ctx.fillStyle = '#ffffff';
  ctx.fill();
  ctx.lineWidth = 1;
  ctx.strokeStyle = '#eceef1';
  ctx.stroke();
  const iw = (draw as HTMLCanvasElement).width;
  const ih = (draw as HTMLCanvasElement).height;
  const k = Math.min(qrSide / iw, qrSide / ih);
  const dw = iw * k;
  const dh = ih * k;
  ctx.drawImage(
    draw as CanvasImageSource,
    plateX + (plateSide - dw) / 2,
    plateY + (plateSide - dh) / 2,
    dw,
    dh
  );

  if (horizontal) {
    // 右侧文字列:垂直居中
    const colL = plateX + plateSide + 24;
    const colR = cx + cw - pad;
    const colCx = (colL + colR) / 2;
    const th = params.title ? titleFont + 12 : 0;
    const ph2 = tableLabel ? pillH + 8 : 0;
    const hh = params.hint ? hintFont : 0;
    let ey = plateY + Math.max(0, (plateSide - (th + ph2 + hh)) / 2);
    if (params.title) {
      ctx.fillStyle = '#1f2329';
      ctx.font = `700 ${titleFont}px ${sans}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillText(params.title, colCx, ey);
      ey += titleFont + 12;
    }
    if (tableLabel) {
      // 桌号:实心品牌色胶囊 + 白色加粗字,桌与桌之间一眼可辨
      ctx.font = `700 ${tableFont}px ${sans}`;
      const tw = ctx.measureText(tableLabel).width;
      const pw = Math.min(tw + tableFont * 2, colR - colL);
      roundRect(ctx, colCx - pw / 2, ey, pw, pillH, pillH / 2);
      ctx.fillStyle = accent;
      ctx.fill();
      ctx.fillStyle = '#ffffff';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(tableLabel, colCx, ey + pillH / 2 + tableFont * 0.04);
      ey += pillH + 8;
    }
    if (params.hint) {
      ctx.fillStyle = '#9299a6';
      ctx.font = `400 ${hintFont}px ${sans}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillText(params.hint, colCx, ey);
    }
  } else {
    // 底部文字(底对齐到卡片内边距)
    let fy = cy + ch - pad - footH;
    if (!hasShop && params.title) {
      ctx.fillStyle = '#1f2329';
      ctx.font = `700 ${titleFont}px ${sans}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillText(params.title, W / 2, fy);
      fy += titleFont + 12;
    }
    if (tableLabel) {
      // 桌号:实心品牌色胶囊 + 白色加粗字,桌与桌之间一眼可辨
      ctx.font = `700 ${tableFont}px ${sans}`;
      const tw = ctx.measureText(tableLabel).width;
      const pw = tw + tableFont * 2;
      roundRect(ctx, (W - pw) / 2, fy, pw, pillH, pillH / 2);
      ctx.fillStyle = accent;
      ctx.fill();
      ctx.fillStyle = '#ffffff';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(tableLabel, W / 2, fy + pillH / 2 + tableFont * 0.04);
      fy += pillH + 8;
    }
    if (params.hint) {
      ctx.fillStyle = '#9299a6';
      ctx.font = `400 ${hintFont}px ${sans}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'top';
      ctx.fillText(params.hint, W / 2, fy);
    }
  }

  return canvas;
}

/** 生成桌牌卡片 PNG dataURL。 */
export async function renderCardDataUrl(params: QrCardOptions): Promise<string> {
  const canvas = await renderCardCanvas(params);
  try {
    return canvas.toDataURL('image/png');
  } catch {
    throw new Error('图片跨域导致无法导出，请把 Logo 上传到系统内的「店铺 Logo」');
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

/**
 * 打印页顶栏样式(A4 单据、桌台码两种打印页共用,避免多处各自漂移)。
 *
 * 顶栏高度必须稳定在 .spacer 的 52px:button/span 的 flex 属性是为此服务的 ——
 * button 不收缩(否则「打印 / 另存为 PDF」被挤成竖排),span 可收缩并省略超长文案
 * (Windows 微软雅黑下文案更长)。顶栏一旦换行变高就会盖住首页内容。
 */
export const PRINT_TOOLBAR_CSS = `
  .toolbar { position:fixed; left:0; right:0; top:0; background:#fff; border-bottom:1px solid #e6e8eb; padding:10px 16px;
    display:flex; align-items:center; gap:12px; z-index:9; box-shadow:0 2px 8px rgba(0,0,0,.06); }
  .toolbar b { flex:0 0 auto; font-size:14px; color:#1f2329; }
  .toolbar span { flex:1 1 auto; min-width:0; font-size:12px; color:#8a919f;
    overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
  .toolbar button { flex:0 0 auto; white-space:nowrap; background:#F0481F; color:#fff; border:0;
    border-radius:6px; padding:8px 18px; font-size:13px; cursor:pointer; }
  .spacer { height:52px; }
  @media print { .toolbar, .spacer { display:none !important; } }
`;

/**
 * 打印页顶栏。按钮只带 data-print —— 绑定由 opener 完成(见 openPrintWindow):
 * 生产 CSP 是 script-src 'self'(无 'unsafe-inline'),内联 onclick 会被拦掉。
 */
export function buildPrintToolbar(title: string, hint: string): string {
  return `<div class="toolbar">
    <b>${esc(title)}</b>
    <span>${esc(hint)}</span>
    <button
      type="button"
      data-print
    >
      打印 / 另存为 PDF
    </button>
  </div>
  <div class="spacer"></div>`;
}

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
 * A4 网格模式每页顶部那行页眉(「XX店 每页 N × M / 第 x/y 页」)占用的高度(mm)。
 * 9pt 正文约 3.8mm + .phead 的 4mm 下边距,取 8 留余量。
 */
const SHEET_HEAD_H = 8;

/** 计算 A4 / 单页下卡片的行列与间距,供两种打印排版复用。 */
function computeGrid(
  mode: string,
  columns: number,
  cardW: number,
  cardH: number,
  gap: number
): { cols: number; rows: number; gap: number; perPage: number } {
  const A4W = 210;
  const A4H = 297;
  const MARGIN = 10;
  const usableW = A4W - MARGIN * 2;
  // 网格模式每页都带一行页眉,必须从可用高里扣掉:否则网格刚好顶满 277mm 时
  // (如 50×50 排 5 行 = 274mm + 页眉 ≈ 282mm)会溢出纸张,Chrome 会在每页后面
  // 多打一张空白页。单页(label)模式不渲染页眉,且行数恒为 1,不受影响。
  const usableH = A4H - MARGIN * 2 - SHEET_HEAD_H;
  let cols: number;
  let rows: number;
  let g = gap;
  if (mode === 'label') {
    cols = 1;
    rows = 1;
  } else {
    // 卡片宽度固定,列数按纸张可用宽度自适应:
    // 优先满足用户选择的列数,通过压缩间距让它们排下(最小 2mm);
    // 若即使最小间距也排不下,则退回到实际能放下的最多列数,避免溢出到纸张外。
    const minGap = 2;
    const maxCols = Math.max(1, Math.floor((usableW + minGap) / (cardW + minGap)));
    cols = Math.max(1, Math.min(columns, maxCols));
    if (cols > 1) {
      const maxGap = (usableW - cols * cardW) / (cols - 1);
      g = Math.max(minGap, Math.min(gap, maxGap));
    }
    rows = Math.max(1, Math.floor((usableH + g) / (cardH + g)));
  }
  return { cols, rows, gap: g, perPage: cols * rows };
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
  const grid = computeGrid(o.mode, o.columns, o.cardW, o.cardH, o.gap);
  const cols = grid.cols;
  const rows = grid.rows;
  const gap = grid.gap;
  const perPage = grid.perPage;
  const pages: QrCard[][] = [];
  for (let i = 0; i < cards.length; i += perPage) pages.push(cards.slice(i, i + perPage));

  const pageCss =
    o.mode === 'label'
      ? `@page { size: ${o.cardW}mm ${o.cardH}mm; margin: 0; }`
      : `@page { size: A4; margin: 10mm; }`;

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
  ${PRINT_TOOLBAR_CSS}
  @media print {
    html,body { background:#fff; }
    ${o.mode === 'label' ? '.page { padding:0 }' : '.page { padding:0 }'}
  }
</style></head>
<body>
  ${buildPrintToolbar(
    `桌台点餐码 · 共 ${cards.length} 张`,
    `打印时请关闭「页眉页脚」并选择实际纸张（${o.mode === 'label' ? o.cardW + '×' + o.cardH + 'mm' : 'A4'}）`
  )}
  ${pageHtml}
</body></html>`;
}

export interface CardImage {
  dataUrl: string;
  name?: string;
}

export interface CardSheetOptions {
  mode?: 'sheet' | 'label';
  columns?: number;
  cardW?: number;
  cardH?: number;
  gap?: number;
  cutLine?: boolean;
  shopName?: string;
}

/**
 * 把已渲染好的完整桌牌卡片图片(由 renderCardDataUrl 生成)平铺到 A4 / 单页。
 * 卡片图本身已含商家名 / 扫码提醒,这里只负责排版、分页与裁剪线,不再重复叠加文字。
 */
export function buildCardImagesSheetHtml(images: CardImage[], opts: CardSheetOptions = {}): string {
  const o = {
    mode: 'sheet',
    columns: 3,
    cardW: 60,
    cardH: 90,
    gap: 6,
    cutLine: true,
    shopName: '',
    ...opts
  } as Required<CardSheetOptions>;

  const grid = computeGrid(o.mode, o.columns, o.cardW, o.cardH, o.gap);
  const cols = grid.cols;
  const rows = grid.rows;
  const gap = grid.gap;
  const perPage = grid.perPage;
  const pages: CardImage[][] = [];
  for (let i = 0; i < images.length; i += perPage) pages.push(images.slice(i, i + perPage));

  const pageCss =
    o.mode === 'label'
      ? `@page { size: ${o.cardW}mm ${o.cardH}mm; margin: 0; }`
      : `@page { size: A4; margin: 10mm; }`;

  const pageHtml = pages
    .map((items, pi) => {
      const isLast = pi === pages.length - 1;
      const head =
        o.mode === 'label'
          ? ''
          : `<div class="phead"><span>${esc(o.shopName || '')}&emsp;每页 ${cols} × ${rows}</span><span>第 ${pi + 1} / ${pages.length} 页&emsp;共 ${images.length} 张</span></div>`;
      const border = o.cutLine ? 'border:0.3mm dashed #cfd4dc;' : '';
      const gridHtml = `<div class="grid" style="grid-template-columns:repeat(${cols},${o.cardW}mm);gap:${gap}mm">${items
        .map(
          c =>
            `<div style="box-sizing:border-box;width:${o.cardW}mm;height:${o.cardH}mm;${border}display:flex;align-items:center;justify-content:center;overflow:hidden;break-inside:avoid;page-break-inside:avoid">
               <img src="${c.dataUrl}" style="width:100%;height:100%;object-fit:contain;display:block" alt="${esc(c.name || '桌台码')}">
             </div>`
        )
        .join('')}</div>`;
      // 末页需显式关闭分页,否则浏览器会多打一张空白页(末页并非 body 的 last-child)
      return `<section class="page${isLast ? ' last' : ''}">${head}${gridHtml}</section>`;
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
  ${PRINT_TOOLBAR_CSS}
  @media print {
    html,body { background:#fff; }
    .page { padding:0; }
  }
</style></head>
<body>
  ${buildPrintToolbar(
    `桌台点餐码 · 共 ${images.length} 张`,
    `打印时请关闭「页眉页脚」并选择实际纸张（${o.mode === 'label' ? o.cardW + '×' + o.cardH + 'mm' : 'A4'}）`
  )}
  ${pageHtml}
</body></html>`;
}

/** 打印页里打印按钮的统一钩子:HTML 只写 data-print,绑定由 opener 完成(见 openPrintWindow)。 */
const PRINT_BTN_SELECTOR = '[data-print]';
/** 等图片解码的上限:桌台多时解码很慢,超时直接打,不能让用户干等。 */
const IMAGE_DECODE_TIMEOUT = 8000;

/**
 * 在新窗口打开打印页。返回是否成功打开(被拦截返回 false)。
 *
 * 打印页里刻意不放任何脚本(既不内联 <script>、也不用 onclick):
 * 生产 CSP 是 `script-src 'self'`(没有 'unsafe-inline'),而 about:blank 会继承
 * opener 的 CSP,Chromium(Chrome / Edge)因此会拦掉内联脚本与内联事件处理器,
 * 表现为「页面渲染正常,但右上角『打印 / 另存为 PDF』点了没反应」。
 * 绑定与自动打印改由 opener(受信的管理端 JS)执行:addEventListener 传入的是
 * opener realm 里的函数,不受弹窗继承来的 CSP 约束。
 */
export function openPrintWindow(html: string): boolean {
  const w = window.open('', '_blank');
  if (!w) return false;
  focusWindow(w);
  w.document.write(html);
  w.document.close();
  attachPrintActions(w);
  return true;
}

/**
 * 点击时同步把打印窗口开出来,内容稍后再由 writePrintWindow 写入。返回 null 表示被拦截。
 *
 * 渲染几百张二维码要几十秒,而 Chrome 的 transient activation 只保留 5 秒 ——
 * 等渲染完再 window.open 会被当成「非用户触发的弹窗」直接拦掉,根本不会有打印页。
 * 所以必须在仍带用户激活的点击任务里先开窗。
 */
export function preparePrintWindow(pendingText = '正在生成打印页，请稍候…'): Window | null {
  const w = window.open('', '_blank');
  if (!w) return null;
  focusWindow(w);
  // 先写一页占位,否则窗口一片空白,用户会以为卡死
  w.document.write(
    `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>正在生成打印页</title>
<style>html,body{margin:0;height:100%;background:#f3f4f6;display:flex;align-items:center;justify-content:center;
font-family:"PingFang SC","Microsoft YaHei",system-ui,sans-serif;font-size:14px;color:#5b6270}</style></head>
<body>${esc(pendingText)}</body></html>`
  );
  w.document.close();
  return w;
}

/** 把真正的内容写进 preparePrintWindow 开出来的窗口(会覆盖占位页)并挂上打印动作。 */
export function writePrintWindow(w: Window, html: string): void {
  if (w.closed) return;
  // 占位页已经 close 过,重新 open 会清空文档并重置解析器
  w.document.open();
  w.document.write(html);
  w.document.close();
  attachPrintActions(w);
}

/** 抢一下焦点:若浏览器在后台标签打开,打印预览会开在后面,用户会以为没反应。 */
function focusWindow(w: Window): void {
  try {
    w.focus();
  } catch {
    /* 部分环境不允许抢焦点,忽略 */
  }
}

function attachPrintActions(w: Window): void {
  const bind = (): void => {
    if (w.closed) return;
    // 打印页里的按钮只带 data-print;点击行为在这里挂,避免内联 onclick 被 CSP 拦。
    const btn = w.document.querySelector<HTMLElement>(PRINT_BTN_SELECTOR);
    btn?.addEventListener('click', () => w.print());
    void autoPrintWhenReady(w);
  };
  // document.write + close 之后通常还没到 complete,统一等 load 再动手。
  if (w.document.readyState === 'complete') bind();
  else w.addEventListener('load', bind, { once: true });
}

/** 等图片解码完再唤起打印,避免打出空白二维码。 */
async function autoPrintWhenReady(w: Window): Promise<void> {
  try {
    // 图片是 dataURL,load 时已在 DOM 里;decode() 保证位图就绪后再进打印预览。
    await withTimeout(
      Promise.all(
        Array.from(w.document.images).map(img =>
          typeof img.decode === 'function' ? img.decode().catch(() => undefined) : Promise.resolve()
        )
      ),
      IMAGE_DECODE_TIMEOUT
    );
    // 再给一帧让版面稳定,否则打印预览偶尔会漏掉刚解码的图。
    await new Promise<void>(r => setTimeout(r, 300));
    if (!w.closed) w.print();
  } catch {
    /* 窗口已关闭或解码异常:放弃自动打印,用户仍可点工具栏按钮 */
  }
}

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T | void> {
  return Promise.race([p, new Promise<void>(r => setTimeout(r, ms))]);
}
