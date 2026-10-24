// store/session 的持久化策略单测。
//
// 这段逻辑曾经每个轮询拍(3s)都同步写一次 storage,改成「稳态不写盘」后又必须保证
// 两个语义不丢:① 单号变化要落盘;② 清空必须真的落盘(否则冷启动会恢复出上一轮的旧单)。
// 这两条都是「看不见的回归」,用测试钉住。
import { beforeEach, describe, expect, it } from 'vitest';
import { applyOrder, clearCurrentOrder, readPersisted, resetOrder, sessionState } from '../../src/store/session';
import { bootApp } from '../../test/harness';
import type { OrderDetail } from '../../src/types';

// 记录 setStorageSync 的调用次数(mock 是内存 Map,本身不记账)。
// installGlobals 在每个测试文件加载时只跑一次,这里的包装对本文件全程有效。
const realSetStorageSync = wx.setStorageSync;
let setStorageWrites = 0;
wx.setStorageSync = (key: string, value: unknown): void => {
  setStorageWrites++;
  realSetStorageSync.call(wx, key, value);
};

function orderOf(orderNo: string, personCount = 4): OrderDetail {
  return { orderNo, personCount } as OrderDetail;
}

describe('session 持久化策略', () => {
  beforeEach(async () => {
    await bootApp();
    sessionState().tableCode = 'T1';
    sessionState().currentOrder = null;
    setStorageWrites = 0;
  });

  it('轮询稳态:同一单号反复 applyOrder 不写盘', () => {
    applyOrder(orderOf('A1'));
    const after = setStorageWrites;
    for (let i = 0; i < 5; i++) applyOrder(orderOf('A1'));
    expect(setStorageWrites).toBe(after);
    expect(readPersisted().orderNo).toBe('A1');
  });

  it('加菜后仍是同一单号:不重复写盘,恢复线索不变', () => {
    applyOrder(orderOf('A1'));
    const after = setStorageWrites;
    applyOrder(orderOf('A1'));
    expect(setStorageWrites).toBe(after);
  });

  it('换单(清台后开新单)要落盘', () => {
    applyOrder(orderOf('A1'));
    const after = setStorageWrites;
    applyOrder(orderOf('A2'));
    expect(setStorageWrites).toBeGreaterThan(after);
    expect(readPersisted().orderNo).toBe('A2');
  });

  it('清空必须真的落盘:内存已空但磁盘残留旧单号时也要写', () => {
    applyOrder(orderOf('A1'));
    // 模拟冷启动:内存态归零,磁盘上还留着 A1
    sessionState().currentOrder = null;
    applyOrder(null);
    expect(readPersisted().orderNo).toBe('');
  });

  it('磁盘已是目标值时跳过写入(清空后再收到一次无单快照的场景)', () => {
    applyOrder(orderOf('A1'));
    applyOrder(null); // 先把磁盘清成 ''
    sessionState().currentOrder = null;
    const after = setStorageWrites;
    applyOrder(null); // 磁盘已是 '',不必再写
    expect(setStorageWrites).toBe(after);
  });

  it('clearCurrentOrder 只清内存,保留持久化的 orderNo', () => {
    applyOrder(orderOf('A1'));
    clearCurrentOrder();
    expect(sessionState().currentOrder).toBeNull();
    expect(readPersisted().orderNo).toBe('A1');
  });

  it('resetOrder 连持久化一起清掉', () => {
    applyOrder(orderOf('A1'));
    resetOrder();
    expect(sessionState().currentOrder).toBeNull();
    expect(readPersisted().orderNo).toBe('');
  });
});
