// 菜单模糊搜索的单测。
//
// 这段逻辑决定「顾客能不能搜到自己想吃的」，而匹配得太松（满屏无关结果）与
// 太紧（明明有却搜不到）都是差体验，所以把几种输入形态都钉住。
import { describe, expect, it } from 'vitest';
import { dishMatches, filterMenu, isSubsequence, parseKeywords } from '../../src/core/search';
import type { Dish, MenuCategory } from '../../src/types';

function dish(dishName: string, description = '', specs: string[] = []): Dish {
  return {
    dishId: 1,
    dishName,
    description,
    specs: specs.map((specName, i) => ({ specId: i + 1, specName, price: 1000 }))
  };
}

describe('parseKeywords', () => {
  it('按空白与中英文逗号拆分并小写', () => {
    expect(parseKeywords('牛肉 面')).toEqual(['牛肉', '面']);
    expect(parseKeywords('牛肉,面，汤')).toEqual(['牛肉', '面', '汤']);
    expect(parseKeywords('  Beef ')).toEqual(['beef']);
  });

  it('空值得到空数组（调用方据此走不过滤的路径）', () => {
    expect(parseKeywords('')).toEqual([]);
    expect(parseKeywords('   ')).toEqual([]);
    expect(parseKeywords(undefined as unknown as string)).toEqual([]);
  });
});

describe('isSubsequence', () => {
  it('按顺序出现即命中', () => {
    expect(isSubsequence('红肉', '红烧肉')).toBe(true);
    expect(isSubsequence('肉红', '红烧肉')).toBe(false);
  });
});

describe('dishMatches', () => {
  it('菜名包含（不区分大小写）', () => {
    expect(dishMatches(dish('牛肉面'), parseKeywords('牛肉'))).toBe(true);
    expect(dishMatches(dish('Cola'), parseKeywords('cola'))).toBe(true);
  });

  it('描述命中也算（口味词常写在描述里）', () => {
    expect(dishMatches(dish('招牌拌面', '微辣，含花生'), parseKeywords('微辣'))).toBe(true);
  });

  it('规格名命中也算', () => {
    expect(dishMatches(dish('可乐', '', ['大杯', '小杯']), parseKeywords('大杯'))).toBe(true);
  });

  it('菜名支持顺序包含：红肉 → 红烧肉', () => {
    expect(dishMatches(dish('红烧肉'), parseKeywords('红肉'))).toBe(true);
  });

  it('多个关键词是「与」：有一个不命中就淘汰', () => {
    expect(dishMatches(dish('牛肉面'), parseKeywords('牛肉 面'))).toBe(true);
    expect(dishMatches(dish('牛肉面'), parseKeywords('牛肉 米饭'))).toBe(false);
  });

  it('关键词为空时全部命中', () => {
    expect(dishMatches(dish('任意菜'), [])).toBe(true);
  });
});

describe('filterMenu', () => {
  const menu: MenuCategory[] = [
    { categoryId: 1, categoryName: '主食', dishes: [dish('牛肉面'), dish('蛋炒饭')] },
    { categoryId: 2, categoryName: '饮品', dishes: [dish('可乐', '', ['大杯', '小杯'])] },
    { categoryId: 3, categoryName: '凉菜', dishes: [] }
  ];

  it('关键词为空时原样返回同一份引用（零成本路径）', () => {
    expect(filterMenu(menu, '')).toBe(menu);
  });

  it('只保留命中的菜品，并丢掉没有命中的分类', () => {
    const out = filterMenu(menu, '牛肉');
    expect(out).toHaveLength(1);
    expect(out[0].categoryName).toBe('主食');
    expect(out[0].dishes).toHaveLength(1);
    expect(out[0].dishes?.[0].dishName).toBe('牛肉面');
  });

  it('规格名也能搜到所属菜品', () => {
    const out = filterMenu(menu, '大杯');
    expect(out).toHaveLength(1);
    expect(out[0].dishes?.[0].dishName).toBe('可乐');
  });

  it('搜不到时返回空数组（页面据此显示「没有找到」）', () => {
    expect(filterMenu(menu, '龙虾')).toEqual([]);
  });

  it('不改写原菜单（分类对象是浅拷贝，原 dishes 不受影响）', () => {
    filterMenu(menu, '牛肉');
    expect(menu[0].dishes).toHaveLength(2);
  });
});
