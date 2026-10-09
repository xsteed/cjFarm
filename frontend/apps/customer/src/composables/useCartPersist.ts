// 顾客端购物车的本地持久化。
//
// 为什么只做在 H5：小程序把购物车挂在 App 实例上，切页面不丢，冷启动本来就该
// 重新点；而 H5 是单页应用，误触刷新/微信里杀掉重进就直接归零，顾客刚点的菜全没了。
//
// 恢复时必须拿当前菜单复核一遍：隔了一段时间回来，菜可能已下架、规格可能已售罄、
// 价格也可能调过。所以——
//   ・菜没了 / 规格没了 / 已售罄 → 整行丢掉（不留半截状态让顾客困惑）
//   ・价格一律以菜单现值为准，不沿用存下来的旧价
//   ・超过 12 小时视为过期，整份丢弃（避免隔天回来看到昨天的车）
import { watch } from 'vue';
import type { Ref } from 'vue';
import { isOrderable, isSoldOut } from '@dining/customer-core';
import { safeStorage } from '@dining/shared';
import type { CartItem } from '@dining/customer-core';
import type { Dish, MenuCategory } from '@dining/types';

const KEY_PREFIX = 'dining:cart:';
const MAX_AGE_MS = 12 * 60 * 60 * 1000;

interface Stored {
  at: number;
  items: CartItem[];
}

/**
 * @param cart    useCart 暴露的购物车（ref）
 * @param tableId 桌台 id：按桌隔离，换桌不会串车
 */
export function useCartPersist(cart: Ref<CartItem[]>, tableId: Ref<number>) {
  function storageKey(): string {
    return `${KEY_PREFIX}${tableId.value || 0}`;
  }

  function save(): void {
    if (!tableId.value) return;
    if (!cart.value.length) {
      safeStorage.remove(storageKey());
      return;
    }
    const payload: Stored = { at: Date.now(), items: cart.value };
    try {
      safeStorage.set(storageKey(), JSON.stringify(payload));
    } catch {
      /* 序列化失败（配额/隐私模式）：safeStorage 已兜底，这里不再打扰顾客 */
    }
  }

  /** 菜单到位后调用：把上次没提交的车按当前菜单复核后放回。 */
  function restore(menu: MenuCategory[]): void {
    if (!tableId.value) return;
    const raw = safeStorage.get(storageKey());
    if (!raw) return;
    let data: Stored;
    try {
      data = JSON.parse(raw) as Stored;
    } catch {
      safeStorage.remove(storageKey());
      return;
    }
    if (!data || !Array.isArray(data.items)) {
      safeStorage.remove(storageKey());
      return;
    }
    if (Date.now() - Number(data.at || 0) > MAX_AGE_MS) {
      safeStorage.remove(storageKey());
      return;
    }

    const byId = new Map<number, Dish>();
    for (const c of menu) {
      for (const d of c.dishes || []) byId.set(Number(d.dishId), d);
    }

    const valid: CartItem[] = [];
    for (const it of data.items) {
      const dish = byId.get(Number(it.dishId));
      if (!dish || isSoldOut(dish) || !isOrderable(dish)) continue;
      const spec = (dish.specs || []).find(s => Number(s.specId) === Number(it.specId));
      if (!spec || Number(spec.stock) === 0) continue;
      valid.push({
        key: `${dish.dishId}-${spec.specId}`,
        dishId: Number(dish.dishId),
        dishName: dish.dishName || '',
        specId: Number(spec.specId),
        specName: spec.specName || '',
        // 价格以菜单现值为准，不沿用存的旧价
        price: Number(spec.price) || 0,
        quantity: Math.max(1, Number(it.quantity) || 1),
        remark: String(it.remark || '')
      });
    }
    // 复核后一行不剩：清掉存储，别让坏数据一直留着
    if (valid.length) cart.value = valid;
    else safeStorage.remove(storageKey());
  }

  // 深度监听：数量、备注的变化也要落盘
  watch(cart, save, { deep: true });

  return { restore, save };
}
