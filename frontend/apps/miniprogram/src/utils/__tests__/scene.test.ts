// 桌台码解析的单测。
// 这段逻辑直接决定「扫码能不能进对桌」，而入口有四种形态（小程序码 scene、裸码值、
// urlencode、普通二维码 query），全靠手写解析（小程序 JSCore 不保证有 URL API）。
import { describe, expect, it } from 'vitest';
import { decodeScene, resolveTableCode } from '../scene';

describe('decodeScene', () => {
  it('裸码值原样返回', () => {
    expect(decodeScene('K7M3PQ9X')).toBe('K7M3PQ9X');
  });

  it('支持 tableCode=xxx / table_code=xxx（大小写不敏感）', () => {
    expect(decodeScene('tableCode=K7M3PQ9X')).toBe('K7M3PQ9X');
    expect(decodeScene('table_code=K7M3PQ9X')).toBe('K7M3PQ9X');
    expect(decodeScene('TABLECODE=K7M3PQ9X')).toBe('K7M3PQ9X');
  });

  it('支持 key=value 形式（取 = 之后）', () => {
    expect(decodeScene('t=K7M3PQ9X')).toBe('K7M3PQ9X');
  });

  it('先做一次 URL 解码（微信会把 scene 做 urlencode）', () => {
    expect(decodeScene('tableCode%3DK7M3PQ9X')).toBe('K7M3PQ9X');
  });

  it('多参数时取 tableCode，忽略其余', () => {
    expect(decodeScene('a=1&tableCode=K7M3PQ9X&b=2')).toBe('K7M3PQ9X');
  });

  it('非法百分号编码不抛错，按原值处理', () => {
    expect(decodeScene('%E4%B8%AD%')).toBe('%E4%B8%AD%');
  });

  it('空值返回空串', () => {
    expect(decodeScene('')).toBe('');
    expect(decodeScene(undefined)).toBe('');
    expect(decodeScene(null)).toBe('');
    expect(decodeScene('   ')).toBe('');
  });
});

describe('resolveTableCode', () => {
  it('scene 优先于 tableCode', () => {
    expect(resolveTableCode({ scene: 'tableCode=A1', tableCode: 'B2' })).toBe('A1');
  });

  it('没有 scene 时退回普通 query 的 tableCode / table', () => {
    expect(resolveTableCode({ tableCode: 'B2' })).toBe('B2');
    expect(resolveTableCode({ table: 'C3' })).toBe('C3');
  });

  it('什么都没有时返回空串（页面据此提示「请扫码进入」）', () => {
    expect(resolveTableCode({})).toBe('');
    expect(resolveTableCode(undefined)).toBe('');
  });
});
