// 购物车本地持久化的单测。
//
// 重点不是「存进去了」，而是**拿来时要按当前菜单复核**：隔一段时间回来，
// 菜可能下架、规格可能售罄、价格可能调过，照单全收只会让顾客看到一份假车。
import { nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it } from 'vitest';
import { useCartPersist } from '../useCartPersist';
import type { CartItem } from '@dining/customer-core';
import type { MenuCategory } from '@dining/types';

const KEY = 'dining:cart:1';

function menu(over: Partial<MenuCategory> = {}): MenuCategory[] {
  return [
    {
      categoryId: 1,
      categoryName: '主食',
      dishes: [
        {
          dishId: 1,
          dishName: '牛肉面',
          specs: [{ specId: 11, specName: '小份', price: 20, stock: -1 }]
        }
      ],
      ...over
    }
  ];
}

function item(over: Partial<CartItem> = {}): CartItem {
  return {
    key: '1-11',
    dishId: 1,
    dishName: '牛肉面',
    specId: 11,
    specName: '小份',
    price: 18, // 故意与菜单现值不同：恢复后应取菜单的 20
    quantity: 2,
    remark: '不要辣',
    ...over
  };
}

function setup(tableId = 1) {
  const cart = ref<CartItem[]>([]);
  const id = ref(tableId);
  return { cart, id, ...useCartPersist(cart, id) };
}

beforeEach(() => {
  window.localStorage.clear();
});

describe('useCartPersist', () => {
  it('购物车变化就落盘', async () => {
    const { cart } = setup();
    cart.value = [item()];
    await nextTick();
    expect(window.localStorage.getItem(KEY)).toBeTruthy();
  });

  it('车空了就把存储删掉，不留空壳', async () => {
    const { cart } = setup();
    cart.value = [item()];
    await nextTick();
    cart.value = [];
    await nextTick();
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it('恢复时价格以菜单现值为准（不沿用存的旧价）', () => {
    const { cart, restore } = setup();
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), items: [item()] }));
    restore(menu());
    expect(cart.value).toHaveLength(1);
    expect(cart.value[0].price).toBe(20);
    expect(cart.value[0].quantity).toBe(2);
    expect(cart.value[0].remark).toBe('不要辣');
  });

  it('菜下架了就整行丢掉', () => {
    const { cart, restore } = setup();
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), items: [item()] }));
    restore([{ categoryId: 1, categoryName: '主食', dishes: [] }]);
    expect(cart.value).toHaveLength(0);
  });

  it('菜售罄了就整行丢掉', () => {
    const { cart, restore } = setup();
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), items: [item()] }));
    restore([
      { categoryId: 1, categoryName: '主食', dishes: [{ dishId: 1, dishName: '牛肉面', soldOut: 1, specs: [] }] }
    ]);
    expect(cart.value).toHaveLength(0);
  });

  it('规格售罄了也丢掉', () => {
    const { cart, restore } = setup();
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), items: [item()] }));
    restore([
      {
        categoryId: 1,
        categoryName: '主食',
        dishes: [{ dishId: 1, dishName: '牛肉面', specs: [{ specId: 11, specName: '小份', price: 20, stock: 0 }] }]
      }
    ]);
    expect(cart.value).toHaveLength(0);
  });

  it('超过 12 小时视为过期，整份丢弃', () => {
    const { cart, restore } = setup();
    const longAgo = Date.now() - 13 * 60 * 60 * 1000;
    window.localStorage.setItem(KEY, JSON.stringify({ at: longAgo, items: [item()] }));
    restore(menu());
    expect(cart.value).toHaveLength(0);
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it('按桌隔离：换个桌台读不到别桌的车', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), items: [item()] }));
    const other = setup(2);
    other.restore(menu());
    expect(other.cart.value).toHaveLength(0);
  });

  it('存储内容坏了就当没有，不影响点餐', () => {
    const { cart, restore } = setup();
    window.localStorage.setItem(KEY, '{不是 json');
    restore(menu());
    expect(cart.value).toHaveLength(0);
  });
});
