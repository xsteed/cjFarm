import { shortTime } from '../reportShared';

describe('shortTime', () => {
  // 固定"当前年份"做参照,用例不随真实时间漂移
  const now = new Date(2026, 9, 24, 12, 0, 0);

  it('当年时间省略年份与秒', () => {
    expect(shortTime('2026-01-01 09:30:00', now)).toBe('01-01 09:30');
    expect(shortTime('2026-12-31 23:05:59', now)).toBe('12-31 23:05');
  });

  it('跨年时间保留年份', () => {
    expect(shortTime('2025-12-31 23:59:00', now)).toBe('2025-12-31 23:59');
  });

  it('空值返回占位符,不显示空白', () => {
    expect(shortTime('', now)).toBe('—');
    expect(shortTime(null, now)).toBe('—');
    expect(shortTime(undefined, now)).toBe('—');
  });

  it('畸形串原样返回,不吞信息', () => {
    expect(shortTime('not-a-date', now)).toBe('not-a-date');
  });
});
