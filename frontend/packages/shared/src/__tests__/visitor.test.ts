import { describe, it, expect, beforeEach } from 'vitest';
import { LS_VISITOR_ID, generateVisitorId, visitorId } from '../visitor';

// 访客 ID 的三条不变量:
//   1. 生成结果非空且只含 URL 安全字符(要进 JSON,不能带奇怪字符);
//   2. 首次调用落盘,之后永远返回同一个值(否则 UV 退化成 PV);
//   3. 同一浏览器两个标签页共享同一个 ID(localStorage 的作用)。
describe('visitorId 访客标识', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('generateVisitorId 非空且格式安全', () => {
    const id = generateVisitorId();
    expect(id.length).toBeGreaterThanOrEqual(16);
    // 只允许字母数字与连字符(uuid v4 的形态);降级路径是 32 位十六进制。
    expect(/^[A-Za-z0-9-]+$/.test(id)).toBe(true);
  });

  it('两次生成不相同(每次都是新访客)', () => {
    expect(generateVisitorId()).not.toBe(generateVisitorId());
  });

  it('首次调用落盘,再次调用返回同一个值', () => {
    const first = visitorId();
    expect(localStorage.getItem(LS_VISITOR_ID)).toBe(first);
    expect(visitorId()).toBe(first);
  });

  it('已存在存储值时直接复用,不覆盖', () => {
    localStorage.setItem(LS_VISITOR_ID, 'existing-id-123');
    expect(visitorId()).toBe('existing-id-123');
  });
});
