import { describe, it, expect } from 'vitest';
import { batchLabels } from '../orderDisplay';

describe('orderDisplay batchLabels 加菜批次标签', () => {
  it('单批次订单不打标(全部为空串)', () => {
    const items = [
      { batchNo: 1 },
      { batchNo: 1 },
      { batchNo: undefined }
    ];
    expect(batchLabels(items)).toEqual(['', '', '']);
  });

  it('多批次按出现顺序编号:首单 / 加菜N', () => {
    const items = [{ batchNo: 1 }, { batchNo: 1 }, { batchNo: 2 }, { batchNo: 3 }];
    expect(batchLabels(items)).toEqual(['首单', '首单', '加菜1', '加菜2']);
  });

  it('后端批次号跳号(1、3)时文案不错位', () => {
    const items = [{ batchNo: 1 }, { batchNo: 3 }];
    expect(batchLabels(items)).toEqual(['首单', '加菜1']);
  });

  it('缺失/异常 batchNo 回落为 1(首单)', () => {
    const items = [{ batchNo: 1 }, { batchNo: 0 }, { batchNo: 2 }, { batchNo: null }];
    expect(batchLabels(items)).toEqual(['首单', '首单', '加菜1', '首单']);
  });

  it('空列表与 undefined 均返回空数组', () => {
    expect(batchLabels([])).toEqual([]);
    expect(batchLabels(undefined)).toEqual([]);
    expect(batchLabels(null)).toEqual([]);
  });
});
