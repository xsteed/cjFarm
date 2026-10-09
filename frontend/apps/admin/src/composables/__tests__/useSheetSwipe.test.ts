import { describe, it, expect } from 'vitest';
import { shouldDismissSheet, SHEET_DISMISS_THRESHOLD } from '../useSheetSwipe';

describe('shouldDismissSheet', () => {
  it('超过阈值即关闭,等于阈值不关闭', () => {
    expect(shouldDismissSheet(SHEET_DISMISS_THRESHOLD + 1)).toBe(true);
    expect(shouldDismissSheet(SHEET_DISMISS_THRESHOLD)).toBe(false);
  });

  it('小幅下拉不关闭', () => {
    expect(shouldDismissSheet(10)).toBe(false);
    expect(shouldDismissSheet(0)).toBe(false);
  });

  it('阈值可配置', () => {
    expect(shouldDismissSheet(50, 40)).toBe(true);
    expect(shouldDismissSheet(40, 40)).toBe(false);
  });
});
