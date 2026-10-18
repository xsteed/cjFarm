// 购物车的单测。
// 不止「函数能跑」：计价口径（茶位费 / 满减 / 合计）必须与 Web 端
// packages/customer-core 的实现一致（最终金额以后端重算为准，但两端口径漂移会让顾客看到两个价），
// 所以断言刻意压在「与 H5 对齐的那几条规则」上。
import { beforeEach, describe, expect, it } from 'vitest';
import {
  cartAdd,
  cartChangeQty,
  cartClear,
  cartCount,
  cartDishQty,
  cartDishTotal,
  cartRemove,
  cartSetRemark,
  cartSettle,
  cartState,
  cartToItems
} from '../../src/store/cart';
import { bootApp } from '../../test/harness';
import type { PublicConfig } from '../../src/types';

function addLine(dishId: number, specId: number, price: number, qty = 1, remark = ''): void {
  cartAdd({
    dishId,
    dishName: `菜${dishId}`,
    specId,
    specName: `规格${specId}`,
    price,
    image: '',
    itemRemark: remark
  });
  for (let i = 1; i < qty; i++) cartChangeQty(`${dishId}:${specId}`, 1);
}

describe('cart store', () => {
  beforeEach(async () => {
    await bootApp();
    cartClear();
  });

  it('跨页共享同一份数组（挂在 App 实例上，而不是模块级单例）', async () => {
    const app = await bootApp();
    addLine(101, 1, 58);
    // 这正是「CJS 分包下模块级单例不共享」的解法：状态在 App 实例上
    expect(cartState()).toBe(app.cart);
    expect(cartCount()).toBe(1);
  });

  it('同一 dish+spec 合并为一行；不同规格分行', () => {
    addLine(101, 1, 58);
    addLine(101, 1, 58);
    addLine(101, 2, 108);
    expect(cartState()).toHaveLength(2);
    expect(cartState()[0].quantity).toBe(2);
    expect(cartCount()).toBe(3);
    expect(cartDishQty(101)).toBe(3);
  });

  it('数量减到 0 自动移除该行', () => {
    addLine(101, 1, 58, 2);
    cartChangeQty('101:1', -1);
    expect(cartState()[0].quantity).toBe(1);
    cartChangeQty('101:1', -1);
    expect(cartState()).toHaveLength(0);
  });

  it('对不存在的 key 操作是安全的（不抛错、不新增行）', () => {
    cartChangeQty('999:9', 1);
    cartRemove('999:9');
    cartSetRemark('999:9', '随便');
    expect(cartState()).toHaveLength(0);
  });

  it('单项备注可写入与覆盖', () => {
    addLine(101, 1, 58, 1, '不要辣');
    expect(cartState()[0].itemRemark).toBe('不要辣');
    cartSetRemark('101:1', '不要辣、多放葱');
    expect(cartState()[0].itemRemark).toBe('不要辣、多放葱');
  });

  it('金额按 price × quantity 累加，并用两位小数收敛浮点误差', () => {
    addLine(1, 1, 0.1, 3); // 0.1 × 3 = 0.30000000000000004
    addLine(2, 1, 33.33, 3); // 99.99
    expect(cartDishTotal()).toBe(100.29);
  });

  it('cartToItems 只带下单需要的字段', () => {
    addLine(101, 2, 108, 2, '不要辣');
    expect(cartToItems()).toEqual([{ dishId: 101, specId: 2, quantity: 2, itemRemark: '不要辣' }]);
  });

  describe('计价规则（与 H5 packages/customer-core 对齐）', () => {
    it('未选菜时合计固定为 0（茶位费照 H5 口径照算，只是视图层不在空车时展示明细）', () => {
      const s = cartSettle({ seat_fee_enabled: '1', seat_fee: '2' }, 4);
      expect(s).toMatchObject({ dishTotal: 0, seatFee: 8, discount: 0, total: 0 });
    });

    it('茶位费 = 人数 × 单价；未开启或单价为 0 时为 0', () => {
      addLine(1, 1, 50, 1);
      const on = cartSettle({ seat_fee_enabled: '1', seat_fee: '2' }, 4);
      expect(on.seatFee).toBe(8);
      expect(on.total).toBe(58);
      expect(cartSettle({ seat_fee_enabled: '0', seat_fee: '2' }, 4).seatFee).toBe(0);
      expect(cartSettle({ seat_fee_enabled: '1', seat_fee: '0' }, 4).seatFee).toBe(0);
    });

    it('人数下限兜到 1（传 0 也不能把茶位费算没）', () => {
      addLine(1, 1, 50, 1);
      expect(cartSettle({ seat_fee_enabled: '1', seat_fee: '2' }, 0).seatFee).toBe(2);
    });

    it('满减按倍数叠加', () => {
      addLine(1, 1, 100, 1);
      const cfg: PublicConfig = {
        promotion_enabled: '1',
        promotion_threshold: '50',
        promotion_discount: '10'
      };
      expect(cartSettle(cfg, 1).discount).toBe(20); // floor(100 / 50) × 10
    });

    it('满减不会超过菜品小计（单价大于小计时的保护）', () => {
      addLine(1, 1, 100, 1);
      const cfg: PublicConfig = {
        promotion_enabled: '1',
        promotion_threshold: '50',
        promotion_discount: '200'
      };
      expect(cartSettle(cfg, 1).discount).toBe(100);
      expect(cartSettle(cfg, 1).total).toBe(0);
    });

    it('满减未开启 / 门槛为 0 / 未达门槛时不减', () => {
      addLine(1, 1, 100, 1);
      const base = { promotion_threshold: '50', promotion_discount: '10' };
      expect(cartSettle({ ...base, promotion_enabled: '0' }, 1).discount).toBe(0);
      expect(cartSettle({ ...base, promotion_enabled: '1', promotion_threshold: '0' }, 1).discount).toBe(0);
      expect(cartSettle({ ...base, promotion_enabled: '1', promotion_threshold: '500' }, 1).discount).toBe(0);
    });

    it('合计 = 小计 + 茶位费 − 满减', () => {
      addLine(1, 1, 100, 1);
      const s = cartSettle(
        {
          seat_fee_enabled: '1',
          seat_fee: '2',
          promotion_enabled: '1',
          promotion_threshold: '50',
          promotion_discount: '10'
        },
        3
      );
      expect(s).toMatchObject({ dishTotal: 100, seatFee: 6, discount: 20, total: 86 });
    });
  });
});
