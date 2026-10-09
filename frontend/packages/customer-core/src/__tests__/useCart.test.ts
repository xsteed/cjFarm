// 购物车的单测：重点覆盖规格弹层那条链路（与小程序 spec-sheet + cartAdd 同规则）。
//
// 提示走的 @dining/shared 的 ui 适配器，未注册实现时 warning/success 静默，
// 所以这里可以直接断言「有没有加进购物车」，不用 mock 通知。
import { ref } from 'vue';
import { describe, expect, it } from 'vitest';
import { useCart } from '../useCart';
import { isManySpecs, isOrderable, isSoldOut } from '../spec';
import type { Dish, DishSpec, RemarkOption } from '@dining/types';

const remarksFixture: RemarkOption[] = [
  { remarkId: 1, optionName: '不要辣' },
  { remarkId: 2, optionName: '少油' }
];

function dish(over: Partial<Dish> = {}): Dish {
  return {
    dishId: 1,
    dishName: '牛肉面',
    specs: [{ specId: 11, specName: '小份', price: 18, stock: -1 }],
    ...over
  };
}

function cartOf(remarks: RemarkOption[] = remarksFixture) {
  return useCart(ref(remarks));
}

describe('菜品状态判定', () => {
  it('人工沽清算售罄', () => {
    expect(isSoldOut(dish({ soldOut: 1 }))).toBe(true);
  });

  it('所有规格库存卖完也算售罄（人工未标记的情况）', () => {
    expect(isSoldOut(dish({ specs: [{ specId: 11, specName: '小份', price: 18, stock: 0 }] }))).toBe(true);
  });

  it('还有库存就不算售罄', () => {
    expect(isSoldOut(dish())).toBe(false);
  });

  it('没有规格 = 暂不可点', () => {
    expect(isOrderable(dish({ specs: [] }))).toBe(false);
    expect(isOrderable(dish())).toBe(true);
  });

  it('规格超过 3 个才需要弹层', () => {
    const many = [1, 2, 3, 4].map(i => ({ specId: i, specName: `s${i}`, price: i, stock: -1 }));
    expect(isManySpecs(dish({ specs: many.slice(0, 3) }))).toBe(false);
    expect(isManySpecs(dish({ specs: many }))).toBe(true);
  });
});

describe('加购拦截', () => {
  it('售罄的菜加不进去', () => {
    const c = cartOf();
    c.changeQty(dish({ soldOut: 1 }), 1);
    expect(c.cart.value).toHaveLength(0);
  });

  it('没有规格的菜加不进去（不会加出 ¥0 的行）', () => {
    const c = cartOf();
    c.changeQty(dish({ specs: [] }), 1);
    expect(c.cart.value).toHaveLength(0);
  });

  it('减号不受「今天沽清」影响：已加进去的照样能减掉', () => {
    const c = cartOf();
    c.changeQty(dish(), 1);
    expect(c.cart.value[0].quantity).toBe(1);
    c.changeQty(dish({ soldOut: 1 }), -1);
    expect(c.cart.value).toHaveLength(0);
  });
});

describe('规格弹层', () => {
  it('售罄 / 无规格的菜点不开弹层', () => {
    const c = cartOf();
    c.openSheet(dish({ soldOut: 1 }));
    expect(c.sheetVisible.value).toBe(false);
    c.openSheet(dish({ specs: [] }));
    expect(c.sheetVisible.value).toBe(false);
  });

  it('正常打开：定位到当前规格，并带出该规格已填的备注', () => {
    const c = cartOf();
    const d = dish();
    c.addToCart(d, d.specs![0] as DishSpec, '不要辣、少放盐');
    c.openSheet(d);
    expect(c.sheetVisible.value).toBe(true);
    expect(c.sheetSpecId.value).toBe(11);
    expect(c.sheetRemark.selected).toEqual(['不要辣']);
    expect(c.sheetRemark.custom).toBe('少放盐');
    expect(c.sheetInCartQty.value).toBe(1);
  });

  it('确定 = 加一份，并把选中的规格定为该菜的当前规格', () => {
    const c = cartOf();
    const d = dish({
      specs: [
        { specId: 11, specName: '小份', price: 18, stock: -1 },
        { specId: 12, specName: '大份', price: 28, stock: -1 }
      ]
    });
    c.openSheet(d);
    c.pickSheetSpec(12);
    c.toggleSheetRemark('不要辣');
    c.confirmSheet();

    expect(c.cart.value).toHaveLength(1);
    expect(c.cart.value[0].specId).toBe(12);
    expect(c.cart.value[0].remark).toBe('不要辣');
    expect(c.selectedSpec[1]).toBe(12); // 行内的 chip / 价格 / 步进器跟着走
    expect(c.sheetVisible.value).toBe(false);
  });

  it('再次确定是「加一份」，不是新起一行（备注随之覆盖）', () => {
    const c = cartOf();
    const d = dish();
    c.openSheet(d);
    c.confirmSheet();
    c.openSheet(d);
    c.sheetRemark.custom = '走清汤';
    c.confirmSheet();

    expect(c.cart.value).toHaveLength(1);
    expect(c.cart.value[0].quantity).toBe(2);
    expect(c.cart.value[0].remark).toBe('走清汤');
  });

  it('售罄规格选不中，确认时兜底再拦一次', () => {
    const c = cartOf();
    const d = dish({
      specs: [
        { specId: 11, specName: '小份', price: 18, stock: -1 },
        { specId: 12, specName: '大份', price: 28, stock: 0 }
      ]
    });
    c.openSheet(d);
    c.pickSheetSpec(12);
    expect(c.sheetSpecId.value).toBe(11); // 点不动，仍停在可点的那个
  });

  it('关掉弹层后再确认不会多加（防连点）', () => {
    const c = cartOf();
    const d = dish();
    c.openSheet(d);
    c.confirmSheet();
    c.confirmSheet(); // 弹层已关闭，这一次应当无效
    expect(c.cart.value[0].quantity).toBe(1);
  });

  it('关闭不写入购物车', () => {
    const c = cartOf();
    c.openSheet(dish());
    c.toggleSheetRemark('不要辣');
    c.closeSheet();
    expect(c.cart.value).toHaveLength(0);
    expect(c.sheetVisible.value).toBe(false);
  });

  it('备注组合规则：选中项在前，自定义文本在后，用「、」连接', () => {
    const c = cartOf();
    const d = dish();
    c.openSheet(d);
    c.toggleSheetRemark('不要辣');
    c.toggleSheetRemark('少油');
    c.sheetRemark.custom = '多放香菜';
    c.confirmSheet();
    expect(c.cart.value[0].remark).toBe('不要辣、少油、多放香菜');
  });
});
