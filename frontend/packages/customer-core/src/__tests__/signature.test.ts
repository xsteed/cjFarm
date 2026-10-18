// 「本店招牌」提取算法的单测：与小程序端 signatureOf 同口径。
import { describe, expect, it } from 'vitest';
import { SIG_CAT_ID, SIG_CAT_NAME, signatureDishes } from '../signature';
import type { Dish, MenuCategory } from '@dining/types';

function spec(specId: number, price: number, stock = -1) {
  return { specId, specName: `规格${specId}`, price, stock };
}

function dish(dishId: number, opts: Partial<Dish> = {}): Dish {
  return { dishId, dishName: `菜${dishId}`, specs: [spec(1, 10)], ...opts };
}

describe('signatureDishes', () => {
  it('只收 signature=1 且有规格的菜，去重、按菜单原序', () => {
    const menu: MenuCategory[] = [
      {
        categoryId: 1,
        dishes: [
          dish(1, { signature: 1 }), // 招牌
          dish(2) // 普通
        ]
      },
      {
        categoryId: 2,
        dishes: [dish(3, { signature: 1 }), dish(1, { signature: 1 })] // 菜1 跨分类重复
      }
    ];
    const cards = signatureDishes(menu);
    expect(cards.map(c => c.dishId)).toEqual([1, 3]); // 去重，且原序（1 先于 3）
  });

  it('没有规格的招牌菜不收（下不了单）', () => {
    const menu: MenuCategory[] = [{ categoryId: 1, dishes: [dish(1, { signature: 1, specs: [] })] }];
    expect(signatureDishes(menu)).toHaveLength(0);
  });

  it('价格取最低规格价；多规格标 manySpecs', () => {
    const menu: MenuCategory[] = [
      {
        categoryId: 1,
        dishes: [
          dish(1, {
            signature: 1,
            specs: [spec(1, 48), spec(2, 38), spec(3, 28)] // 最低 28
          })
        ]
      }
    ];
    const [card] = signatureDishes(menu);
    expect(card.priceText).toBe(28);
    expect(card.manySpecs).toBe(true);
  });

  it('单规格不标 manySpecs', () => {
    const menu: MenuCategory[] = [{ categoryId: 1, dishes: [dish(1, { signature: 1, specs: [spec(1, 18)] })] }];
    const [card] = signatureDishes(menu);
    expect(card.manySpecs).toBe(false);
    expect(card.priceText).toBe(18);
  });

  it('人工售罄或所有规格库存卖完 → soldOut', () => {
    const menu: MenuCategory[] = [
      {
        categoryId: 1,
        dishes: [
          dish(1, { signature: 1, soldOut: 1, specs: [spec(1, 18)] }),
          dish(2, { signature: 1, specs: [spec(1, 18, 0), spec(2, 28, 0)] }) // 全沽清
        ]
      }
    ];
    const cards = signatureDishes(menu);
    expect(cards).toHaveLength(2);
    expect(cards.every(c => c.soldOut)).toBe(true);
  });

  it('无招牌菜时返回空数组', () => {
    expect(signatureDishes([{ categoryId: 1, dishes: [dish(1)] }])).toHaveLength(0);
  });

  it('合成分类常量不与真实正整数分类撞车', () => {
    expect(SIG_CAT_ID).toBe(-1);
    expect(SIG_CAT_NAME).toBe('本店招牌');
  });
});
