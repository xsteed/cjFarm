import { describe, it, expect } from 'vitest';
import { buildA4Html } from '../printA4';
import type { OrderDetail } from '../../types/entities';

function makeOrder(overrides: Partial<OrderDetail> = {}): OrderDetail {
  return {
    orderNo: 'NO001',
    tableNo: '3',
    tableName: '靠窗',
    personCount: 2,
    orderStatus: 1,
    dishAmount: 12.5,
    seatFee: 1,
    totalAmount: 13.5,
    paidAmount: 13.5,
    payStatus: 1,
    createTime: '2024-01-01 12:00:00',
    items: [{ dishName: '红烧肉', price: 12.5, quantity: 1, amount: 12.5 }],
    ...overrides
  };
}

describe('printA4 A4 票据排版', () => {
  it('kitchen 单输出对应标题', () => {
    const html = buildA4Html(makeOrder(), 'kitchen', { shopName: '店' });
    expect(html).toContain('厨房单');
    expect(html).toContain('KITCHEN ORDER');
    expect(html).toContain('店');
  });

  it('guest 单输出对应标题', () => {
    const html = buildA4Html(makeOrder(), 'guest', { shopName: '店' });
    expect(html).toContain('结账单');
    expect(html).toContain('GUEST BILL');
  });

  it('kitchen 单不含价格列，guest 含价格列', () => {
    const kitchen = buildA4Html(makeOrder(), 'kitchen');
    expect(kitchen).not.toContain('单价');
    expect(kitchen).not.toContain('小计');

    const guest = buildA4Html(makeOrder(), 'guest');
    expect(guest).toContain('单价');
    expect(guest).toContain('小计');
  });

  it('金额格式化 money 保留两位小数', () => {
    const html = buildA4Html(makeOrder({ dishAmount: 12.5, seatFee: 1, totalAmount: 13.5 }), 'guest');
    expect(html).toContain('¥12.50');
    expect(html).toContain('¥1.00');
    expect(html).toContain('¥13.50');
  });

  it('itemRows 空明细输出占位文案', () => {
    const html = buildA4Html(makeOrder({ items: [] }), 'guest');
    expect(html).toContain('无菜品明细');
  });

  it('ORDER_STATUS 文案映射与未知状态兜底', () => {
    expect(buildA4Html(makeOrder({ orderStatus: 1 }), 'guest')).toContain('已下单');
    expect(buildA4Html(makeOrder({ orderStatus: 5 }), 'guest')).toContain('已取消');
    expect(buildA4Html(makeOrder({ orderStatus: 99 }), 'guest')).toContain('99');
  });

  it('转义 HTML 特殊字符', () => {
    const html = buildA4Html(makeOrder({ orderNo: '<NO&">' }), 'guest', { shopName: '店<script>' });
    expect(html).toContain('&lt;NO&amp;&quot;&gt;');
    expect(html).toContain('店&lt;script&gt;');
    expect(html).not.toContain('店<script>');
  });
});
