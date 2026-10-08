// 极简 ZIP 打包器(store 模式,不做压缩)。
// PNG 本身已是压缩格式,再套 DEFLATE 收益极低,因此只做容器封装,
// 好处是零依赖、体积小、兼容所有解压工具(Windows 资源管理器 / macOS 归档工具)。

const CRC_TABLE: Uint32Array = (() => {
  const t = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[i] = c >>> 0;
  }
  return t;
})();

function crc32(bytes: Uint8Array): number {
  let c = 0xffffffff;
  for (let i = 0; i < bytes.length; i++) c = CRC_TABLE[(c ^ bytes[i]) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

// DOS 时间/日期(1980 基准),ZIP 头使用该格式。
function dosDateTime(d: Date): { time: number; date: number } {
  const time = ((d.getHours() & 0x1f) << 11) | ((d.getMinutes() & 0x3f) << 5) | ((d.getSeconds() / 2) & 0x1f);
  const date = (((d.getFullYear() - 1980) & 0x7f) << 9) | (((d.getMonth() + 1) & 0x0f) << 5) | (d.getDate() & 0x1f);
  return { time: time & 0xffff, date: date & 0xffff };
}

const enc = (s: string): Uint8Array => new TextEncoder().encode(s);

export interface ZipFileInput {
  name: string;
  data: Uint8Array | ArrayBuffer | Blob;
}

interface ZipEntry {
  name: string;
  bytes: Uint8Array;
}

/**
 * 打包下载为 ZIP。
 * @param files 待打包文件
 * @param zipName 下载文件名(自动补 .zip)
 */
export async function downloadZip(files: ZipFileInput[], zipName = 'export.zip'): Promise<string> {
  const entries: ZipEntry[] = [];
  for (const f of files) {
    let bytes: Uint8Array;
    const d = f.data;
    if (d instanceof Uint8Array) bytes = d;
    else if (d instanceof ArrayBuffer) bytes = new Uint8Array(d);
    else if (d && typeof d.arrayBuffer === 'function') bytes = new Uint8Array(await d.arrayBuffer());
    else bytes = new Uint8Array();
    entries.push({ name: f.name, bytes });
  }
  const blob = buildZip(entries);
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = zipName.endsWith('.zip') ? zipName : zipName + '.zip';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 4000);
  return a.download;
}

/** 生成 ZIP Blob。 */
export function buildZip(entries: ZipEntry[]): Blob {
  const { time, date } = dosDateTime(new Date());
  const chunks: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;

  for (const e of entries) {
    const nameBytes = enc(e.name);
    const crc = crc32(e.bytes);
    const size = e.bytes.length;

    // ---- 本地文件头 ----
    const head = new Uint8Array(30 + nameBytes.length);
    const hv = new DataView(head.buffer);
    hv.setUint32(0, 0x04034b50, true); // signature
    hv.setUint16(4, 20, true); // version needed
    hv.setUint16(6, 0x0800, true); // flags: UTF-8 文件名
    hv.setUint16(8, 0, true); // method: store
    hv.setUint16(10, time, true);
    hv.setUint16(12, date, true);
    hv.setUint32(14, crc, true);
    hv.setUint32(18, size, true); // compressed size
    hv.setUint32(22, size, true); // uncompressed size
    hv.setUint16(26, nameBytes.length, true);
    hv.setUint16(28, 0, true); // extra len
    head.set(nameBytes, 30);

    chunks.push(head, e.bytes);

    // ---- 中央目录项 ----
    const cd = new Uint8Array(46 + nameBytes.length);
    const cv = new DataView(cd.buffer);
    cv.setUint32(0, 0x02014b50, true);
    cv.setUint16(4, 20, true); // version made by
    cv.setUint16(6, 20, true); // version needed
    cv.setUint16(8, 0x0800, true);
    cv.setUint16(10, 0, true);
    cv.setUint16(12, time, true);
    cv.setUint16(14, date, true);
    cv.setUint32(16, crc, true);
    cv.setUint32(20, size, true);
    cv.setUint32(24, size, true);
    cv.setUint16(28, nameBytes.length, true);
    cv.setUint16(30, 0, true); // extra
    cv.setUint16(32, 0, true); // comment
    cv.setUint16(34, 0, true); // disk start
    cv.setUint16(36, 0, true); // internal attr
    cv.setUint32(38, 0, true); // external attr
    cv.setUint32(42, offset, true); // 本地头偏移
    cd.set(nameBytes, 46);
    central.push(cd);

    offset += head.length + size;
  }

  const cdSize = central.reduce((n, c) => n + c.length, 0);

  const end = new Uint8Array(22);
  const ev = new DataView(end.buffer);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(4, 0, true); // disk
  ev.setUint16(6, 0, true); // cd start disk
  ev.setUint16(8, entries.length, true);
  ev.setUint16(10, entries.length, true);
  ev.setUint32(12, cdSize, true);
  ev.setUint32(16, offset, true);
  ev.setUint16(20, 0, true); // comment len

  return new Blob([...chunks, ...central, end], { type: 'application/zip' });
}

/** dataURL → Uint8Array(用于把 PNG dataURL 放进 ZIP)。 */
export function dataUrlToBytes(dataUrl: string): Uint8Array {
  const base64 = String(dataUrl).split(',')[1] || '';
  const bin = atob(base64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
