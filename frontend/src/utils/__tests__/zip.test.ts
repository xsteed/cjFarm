import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { buildZip, dataUrlToBytes, downloadZip } from '../zip';

describe('zip 极简 ZIP 打包器', () => {
  let createObjectURL: ReturnType<typeof vi.fn>;
  let revokeObjectURL: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    // jsdom 未实现 URL.createObjectURL / revokeObjectURL，测试内局部 stub
    createObjectURL = vi.fn(() => 'blob:mock');
    revokeObjectURL = vi.fn();
    URL.createObjectURL = createObjectURL as unknown as typeof URL.createObjectURL;
    URL.revokeObjectURL = revokeObjectURL as unknown as typeof URL.revokeObjectURL;
    // jsdom 的 Blob 未实现 arrayBuffer,用 FileReader 补一个等价实现(测试内 shim)
    if (typeof Blob !== 'undefined' && !Blob.prototype.arrayBuffer) {
      Blob.prototype.arrayBuffer = function (): Promise<ArrayBuffer> {
        return new Promise(resolve => {
          const fr = new FileReader();
          fr.onload = () => resolve(fr.result as ArrayBuffer);
          fr.readAsArrayBuffer(this);
        });
      };
    }
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('crc32 空串为 0（经 buildZip 本地文件头校验）', async () => {
    const blob = buildZip([{ name: 'empty.txt', bytes: new Uint8Array(0) }]);
    const view = new DataView(await blob.arrayBuffer());

    expect(view.getUint32(0, true)).toBe(0x04034b50);
    expect(view.getUint32(14, true)).toBe(0);
  });

  it('crc32 "123456789" 为 0xCBF43926', async () => {
    const blob = buildZip([{ name: 'a.txt', bytes: new TextEncoder().encode('123456789') }]);
    const view = new DataView(await blob.arrayBuffer());

    expect(view.getUint32(14, true)).toBe(0xcbf43926);
  });

  it('buildZip 生成完整 ZIP 结构（本地头/中央目录/EOCD）', async () => {
    const name = 'a.txt';
    const blob = buildZip([{ name, bytes: new TextEncoder().encode('hi') }]);
    expect(blob.type).toBe('application/zip');

    const view = new DataView(await blob.arrayBuffer());
    expect(view.getUint32(0, true)).toBe(0x04034b50);

    const cdOffset = 30 + name.length + 2;
    expect(view.getUint32(cdOffset, true)).toBe(0x02014b50);
    expect(view.getUint32(view.byteLength - 22, true)).toBe(0x06054b50);
  });

  it('dataUrlToBytes 解析 base64 dataURL', () => {
    const bytes = dataUrlToBytes('data:image/png;base64,aGVsbG8=');
    expect(Array.from(bytes)).toEqual([104, 101, 108, 108, 111]);
  });

  it('dataUrlToBytes 解析纯文本 base64 dataURL', () => {
    const bytes = dataUrlToBytes('data:text/plain;base64,aGVsbG8=');
    expect(new TextDecoder().decode(bytes)).toBe('hello');
  });

  it('dataUrlToBytes 空输入返回空字节', () => {
    expect(dataUrlToBytes('').length).toBe(0);
  });

  it('downloadZip 生成下载并返回补全后的文件名', async () => {
    vi.useFakeTimers();

    const name = await downloadZip([{ name: 'a.png', data: new Uint8Array([1, 2, 3]) }], 'table');

    expect(name).toBe('table.zip');
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).not.toHaveBeenCalled();

    vi.advanceTimersByTime(4000);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock');
  });

  it('downloadZip 默认文件名与已带 .zip 后缀', async () => {
    vi.useFakeTimers();

    const name1 = await downloadZip([{ name: 'a.png', data: new Uint8Array([1]) }]);
    expect(name1).toBe('export.zip');

    const name2 = await downloadZip([{ name: 'a.png', data: new Uint8Array([1]) }], 'x.zip');
    expect(name2).toBe('x.zip');
  });
});
