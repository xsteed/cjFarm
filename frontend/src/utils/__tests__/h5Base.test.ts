import { describe, it, expect } from 'vitest';
import { PAGE_ON_LOOPBACK, normalizeBaseUrl, LOOPBACK_RE } from '../h5Base';

describe('h5Base H5 地址校验', () => {
  it('PAGE_ON_LOOPBACK 对回环地址返回 true', () => {
    expect(PAGE_ON_LOOPBACK('localhost')).toBe(true);
    expect(PAGE_ON_LOOPBACK('127.0.0.1')).toBe(true);
    expect(PAGE_ON_LOOPBACK('0.0.0.0')).toBe(true);
    expect(PAGE_ON_LOOPBACK('[::1]')).toBe(true);
    expect(PAGE_ON_LOOPBACK('LOCALHOST')).toBe(true);
  });

  it('PAGE_ON_LOOPBACK 对真实域名返回 false', () => {
    expect(PAGE_ON_LOOPBACK('example.com')).toBe(false);
    expect(PAGE_ON_LOOPBACK('www.example.com')).toBe(false);
    expect(PAGE_ON_LOOPBACK('111.230.154.50')).toBe(false);
    expect(PAGE_ON_LOOPBACK('localhost.example.com')).toBe(false);
  });

  it('LOOPBACK_RE 匹配完整回环 URL 而非真实域名', () => {
    expect(LOOPBACK_RE.test('http://localhost:5173/table/1')).toBe(true);
    expect(LOOPBACK_RE.test('https://127.0.0.1/')).toBe(true);
    expect(LOOPBACK_RE.test('http://0.0.0.0:8080')).toBe(true);
    expect(LOOPBACK_RE.test('http://example.com/')).toBe(false);
  });

  it('normalizeBaseUrl 补协议', () => {
    expect(normalizeBaseUrl('example.com')).toBe('http://example.com');
    expect(normalizeBaseUrl('111.230.154.50')).toBe('http://111.230.154.50');
  });

  it('normalizeBaseUrl 去尾斜杠', () => {
    expect(normalizeBaseUrl('http://example.com/')).toBe('http://example.com');
    expect(normalizeBaseUrl('https://example.com///')).toBe('https://example.com');
  });

  it('normalizeBaseUrl 保留正确 URL 与空值', () => {
    expect(normalizeBaseUrl('https://example.com')).toBe('https://example.com');
    expect(normalizeBaseUrl('  http://a.com/  ')).toBe('http://a.com');
    expect(normalizeBaseUrl('')).toBe('');
    expect(normalizeBaseUrl(null)).toBe('');
  });
});
