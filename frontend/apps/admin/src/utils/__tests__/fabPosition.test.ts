import { describe, it, expect } from 'vitest';
import { clampFabPosition, nearestFabSide, snappedFabX } from '../fabPosition';

const layout = {
  width: 80,
  height: 40,
  viewportWidth: 390,
  viewportHeight: 844,
  margin: 12,
  topReserve: 60,
  bottomReserve: 62
};

describe('clampFabPosition', () => {
  it('超出右上角时夹到边界内', () => {
    const p = clampFabPosition({ x: 9999, y: -9999 }, layout);
    expect(p.x).toBe(390 - 80 - 12); // 298
    expect(p.y).toBe(60); // 顶部预留
  });

  it('超出左下角时夹到边界内', () => {
    const p = clampFabPosition({ x: -100, y: 9999 }, layout);
    expect(p.x).toBe(12);
    expect(p.y).toBe(844 - 40 - 62); // 742
  });

  it('落在合法范围内时原样返回', () => {
    const p = clampFabPosition({ x: 200, y: 500 }, layout);
    expect(p).toEqual({ x: 200, y: 500 });
  });

  it('视口极窄时 min 不被 max 反超(仍至少有 margin)', () => {
    const p = clampFabPosition({ x: 500, y: 900 }, { ...layout, viewportWidth: 60, viewportHeight: 80 });
    expect(p.x).toBe(12);
    expect(p.y).toBe(60);
  });
});

describe('snappedFabX', () => {
  it('吸左 = margin', () => {
    expect(snappedFabX(layout, 'left')).toBe(12);
  });

  it('吸右 = viewport - width - margin', () => {
    expect(snappedFabX(layout, 'right')).toBe(298);
  });
});

describe('nearestFabSide', () => {
  it('中心在左半区 → left', () => {
    expect(nearestFabSide(50, 80, 390)).toBe('left');
  });

  it('中心在右半区 → right', () => {
    expect(nearestFabSide(250, 80, 390)).toBe('right');
  });

  it('恰好越过中线 → right', () => {
    // 中心 = 155 + 40 = 195,恰好等于 390/2
    expect(nearestFabSide(155, 80, 390)).toBe('right');
  });
});
