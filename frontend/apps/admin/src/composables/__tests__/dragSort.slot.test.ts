import { describe, it, expect } from 'vitest';
import { slotFromPoint } from '../useDragSort';

/** 造一个矩形:默认按「整行」尺寸(表格行/移动端竖排卡片)。 */
function rect(top: number, left = 0, width = 600, height = 60) {
  return { top, left, width, height };
}

describe('slotFromPoint 插入槽位', () => {
  it('没有可参照元素时插到第 0 位', () => {
    expect(slotFromPoint([], 100, 100)).toEqual({ slot: 0, anchor: 0 });
  });

  it('单列:以中线为界判断插在某项之前还是之后', () => {
    const rows = [rect(0), rect(60), rect(120)];
    // 第一行上半区 → 插到最前
    expect(slotFromPoint(rows, 300, 10)).toEqual({ slot: 0, anchor: 0 });
    // 第一行下半区 → 插到第二行之前(锚点是第一行:手指离它的中心更近)
    expect(slotFromPoint(rows, 300, 50)).toEqual({ slot: 1, anchor: 0 });
    // 最后一行下半区之后 → 插到末尾
    expect(slotFromPoint(rows, 300, 400)).toEqual({ slot: 3, anchor: 2 });
  });

  it('单列:横向位置不影响判断(整行都是拖放区)', () => {
    const rows = [rect(0), rect(60)];
    expect(slotFromPoint(rows, 5, 80)).toEqual({ slot: 1, anchor: 1 });
    expect(slotFromPoint(rows, 595, 80)).toEqual({ slot: 1, anchor: 1 });
  });

  it('单列:中心最近的一项决定锚点 —— 跨分组拖到交界时靠它定组', () => {
    // 两组相邻:第一组(0,60),第二组(120,180)。手指停在 105(两组交界)
    const rows = [rect(0), rect(60), rect(120), rect(180)];
    // 离第一组末行中心(90)更近 → 锚点是它,插到它后面(留在上一组)
    expect(slotFromPoint(rows, 300, 100)).toEqual({ slot: 2, anchor: 1 });
    // 再往下一点,离第二组首行中心(150)更近 → 锚点换组,插到它前面
    expect(slotFromPoint(rows, 300, 125)).toEqual({ slot: 2, anchor: 2 });
  });

  it('多列网格:同一行内按左右半区决定插在前还是后', () => {
    const cards = [rect(0, 0, 100, 100), rect(0, 110, 100, 100), rect(110, 0, 100, 100), rect(110, 110, 100, 100)];
    // 落在第一张卡的左半 → 插到它之前
    expect(slotFromPoint(cards, 20, 50)).toEqual({ slot: 0, anchor: 0 });
    // 落在第一张卡的右半 → 插到它之后
    expect(slotFromPoint(cards, 80, 50)).toEqual({ slot: 1, anchor: 0 });
    // 落在第二张卡的右半 → 插到第三位之前(即第二张之后)
    expect(slotFromPoint(cards, 170, 50)).toEqual({ slot: 2, anchor: 1 });
  });

  it('多列网格:明显落在某一行上方时按行插到该行最前', () => {
    const cards = [rect(0, 0, 100, 100), rect(0, 110, 100, 100), rect(110, 0, 100, 100), rect(110, 110, 100, 100)];
    // 指针在第二行卡片的上半部 → 插到第二行首项之前
    expect(slotFromPoint(cards, 30, 115)).toEqual({ slot: 2, anchor: 2 });
  });
});
