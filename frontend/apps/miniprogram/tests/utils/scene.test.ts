// 桌台码解析的单测。
// 这段逻辑直接决定「扫码能不能进对桌」，而入口有四种形态（小程序码 scene、裸码值、
// urlencode、普通二维码 query），全靠手写解析（小程序 JSCore 不保证有 URL API）。
import { describe, expect, it } from 'vitest';
import { decodeScene, resolveTableCode, resolveLaunchTableCode, parseScannedTableCode } from '../../src/utils/scene';

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

describe('resolveLaunchTableCode(启动参数)', () => {
  it('扫小程序码:码值在 query.scene', () => {
    expect(
      resolveLaunchTableCode({ scene: 1047, query: { scene: 'tableCode=K7M3PQ9X' } })
    ).toBe('K7M3PQ9X');
  });

  it('扫普通二维码/分享进入:码值在 query.tableCode', () => {
    expect(resolveLaunchTableCode({ scene: 1011, query: { tableCode: '8YG3Q335' } })).toBe('8YG3Q335');
  });

  it('裸码值的 scene 也能解析', () => {
    expect(resolveLaunchTableCode({ query: { scene: 'K7M3PQ9X' } })).toBe('K7M3PQ9X');
  });

  // 这条最关键:onLaunch 的 scene 是数字场景值(如 1047),若当成桌台码解析,
  // 后台「扫码入口 Top」会出现 "1047" 这种假入口。
  it('数字场景值不得被当成桌台码', () => {
    expect(resolveLaunchTableCode({ scene: 1047, query: {} })).toBe('');
    expect(resolveLaunchTableCode({ scene: 1047 })).toBe('');
  });

  it('直接打开(无参数)返回空串', () => {
    expect(resolveLaunchTableCode()).toBe('');
    expect(resolveLaunchTableCode({ query: {} })).toBe('');
  });
});

describe('parseScannedTableCode(小程序内扫码)', () => {
  it('H5 点餐码:从 /order/<桌台码> 取出码', () => {
    expect(parseScannedTableCode('https://dining.example.com/order/8YG3Q335')).toBe('8YG3Q335');
    expect(parseScannedTableCode('http://127.0.0.1:8080/order/K7M3PQ9X?from=qr')).toBe('K7M3PQ9X');
  });

  it('存量桌牌里的数字桌号也认(路径格式相同)', () => {
    expect(parseScannedTableCode('https://dining.example.com/order/12')).toBe('12');
  });

  it('链接里的 tableCode / scene 参数', () => {
    expect(parseScannedTableCode('https://dining.example.com/?tableCode=K7M3PQ9X')).toBe('K7M3PQ9X');
    expect(parseScannedTableCode('https://dining.example.com/?scene=K7M3PQ9X&x=1')).toBe('K7M3PQ9X');
  });

  it('小程序码携带的 path(scene / tableCode 两种形态)', () => {
    expect(parseScannedTableCode('', 'pages/index/index?scene=K7M3PQ9X')).toBe('K7M3PQ9X');
    expect(parseScannedTableCode('', 'pages/index/index?tableCode=K7M3PQ9X')).toBe('K7M3PQ9X');
  });

  it('裸码值原样返回(相册识别 / 调试手输的形态)', () => {
    expect(parseScannedTableCode('K7M3PQ9X')).toBe('K7M3PQ9X');
  });

  // 关键:扫到官网、付款码时不能把整串文字当桌台码去请求接口
  it('无关内容返回空串,交给调用方提示重扫', () => {
    expect(parseScannedTableCode('https://www.example.com/about')).toBe('');
    expect(parseScannedTableCode('收款码：张三')).toBe('');
    expect(parseScannedTableCode('')).toBe('');
    expect(parseScannedTableCode(undefined)).toBe('');
  });
});
