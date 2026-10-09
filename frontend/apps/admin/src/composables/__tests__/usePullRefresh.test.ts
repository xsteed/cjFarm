import { describe, it, expect } from 'vitest';
import { pullDistanceFor, shouldTriggerRefresh } from '../usePullRefresh';

describe('pullDistanceFor', () => {
  it('无位移或向上拖动返回 0', () => {
    expect(pullDistanceFor(0, 92)).toBe(0);
    expect(pullDistanceFor(-30, 92)).toBe(0);
  });

  it('按阻尼系数映射位移', () => {
    expect(pullDistanceFor(40, 92)).toBeCloseTo(18);
    expect(pullDistanceFor(100, 92)).toBe(45);
  });

  it('不超过最大下拉距离', () => {
    expect(pullDistanceFor(1000, 92)).toBe(92);
  });

  it('阻尼系数可配置', () => {
    expect(pullDistanceFor(100, 92, 1)).toBe(92);
  });
});

describe('shouldTriggerRefresh', () => {
  it('达到阈值比例即触发', () => {
    expect(shouldTriggerRefresh(50, 66)).toBe(true); // 50 >= 46.2
    expect(shouldTriggerRefresh(46, 66)).toBe(false); // 46 < 46.2
  });

  it('比例可配置', () => {
    expect(shouldTriggerRefresh(40, 40, 1)).toBe(true);
    expect(shouldTriggerRefresh(39, 40, 1)).toBe(false);
  });
});
