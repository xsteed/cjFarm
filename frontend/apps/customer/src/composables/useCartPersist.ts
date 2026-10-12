// 顾客端购物车的本地持久化。
//
// 为什么只做在 H5：小程序把购物车挂在 App 实例上，切页面不丢，冷启动本来就该
// 重新点；而 H5 是单页应用，误触刷新/微信里杀掉重进就直接归零，顾客刚点的菜全没了。
//
// 恢复时必须拿当前菜单复核一遍：隔了一段时间回来，菜可能已下架、规格可能已售罄、
// 价格也可能调过。所以——
//   ・菜没了 / 规格没了 / 已售罄 → 整行丢掉（不留半截状态让顾客困惑）
//   ・价格一律以菜单现值为准，不沿用存下来的旧价
//
// 隔离维度（防止上一桌客人的车被下一桌看到）：
//   ・订单号：一桌的「这一批客人」在系统里就是那笔未结账订单。换单 / 清台后订单号
//     变了，旧的车自然读不到。
//   ・会话起点：购物车只在本次会话内有效。换一批客人一定是换一部手机重新扫码（新
//     会话），所以即便同一张桌、桌上也没遗留订单，上一批没提交的车同样不会被带出来。
//   ・时间上限：有订单覆盖一整餐（12 小时）；还没开单只给 60 分钟。
import { watch } from 'vue';
import type { Ref } from 'vue';
import { isOrderable, isSoldOut } from '@dining/customer-core';
import { safeSession, safeStorage } from '@dining/shared';
import type { CartItem } from '@dining/customer-core';
import type { Dish, MenuCategory } from '@dining/types';

const KEY_PREFIX = 'dining:cart:';
const VISIT_PREFIX = 'dining:visit:';
const MAX_AGE_MS = 12 * 60 * 60 * 1000;
const ANON_MAX_AGE_MS = 60 * 60 * 1000;

interface Stored {
  at: number;
  items: CartItem[];
  /** 落盘时本桌的订单号；还没开单为空串。恢复时要求与当前一致。 */
  orderNo: string;
}

// 旧版本只有两段（dining:cart:{tableId}）。新键带订单号维度后旧键永远读不到、
// 也不会被删，会一直占着本地存储 —— 首次进入时清一次即可。
let legacyCleaned = false;

function dropLegacyKeys(): void {
  legacyCleaned = true;
  try {
    const stale: string[] = [];
    for (let i = 0; i < window.localStorage.length; i++) {
      const k = window.localStorage.key(i);
      // 新键形如 dining:cart:5:A1B2，旧键形如 dining:cart:5（冒号后不再有冒号）
      if (k && k.startsWith(KEY_PREFIX) && !k.slice(KEY_PREFIX.length).includes(':')) stale.push(k);
    }
    for (const k of stale) safeStorage.remove(k);
  } catch {
    /* 存储不可用时本就没有残留可清 */
  }
}

/**
 * @param cart    useCart 暴露的购物车（ref）
 * @param tableId 桌台 id：按桌隔离，换桌不会串车
 * @param orderNo 本桌当前订单号：按「批客人」隔离，换单/清台后旧车不再恢复
 */
export function useCartPersist(cart: Ref<CartItem[]>, tableId: Ref<number>, orderNo: Ref<string>) {
  function storageKey(): string {
    return `${KEY_PREFIX}${tableId.value || 0}:${orderNo.value || 'new'}`;
  }

  /** 本次会话第一次进这张桌的时刻（刷新保持不变，关掉重开才重置）。 */
  function visitStart(): number {
    const key = `${VISIT_PREFIX}${tableId.value || 0}`;
    const now = Date.now();
    const raw = Number(safeSession.get(key));
    if (Number.isFinite(raw) && raw > 0) return raw;
    safeSession.set(key, String(now));
    return now;
  }

  function save(): void {
    if (!tableId.value) return;
    if (!cart.value.length) {
      safeStorage.remove(storageKey());
      return;
    }
    const payload: Stored = { at: Date.now(), items: cart.value, orderNo: orderNo.value || '' };
    try {
      safeStorage.set(storageKey(), JSON.stringify(payload));
    } catch {
      /* 序列化失败（配额/隐私模式）：safeStorage 已兜底，这里不再打扰顾客 */
    }
  }

  /** 菜单到位后调用：把上次没提交的车按当前菜单复核后放回。 */
  function restore(menu: MenuCategory[]): void {
    if (!legacyCleaned) dropLegacyKeys();
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
    // 不是同一批客人（订单号变了 / 不是本次会话点的 / 太久）：整份丢弃
    const maxAge = data.orderNo ? MAX_AGE_MS : ANON_MAX_AGE_MS;
    if (
      (data.orderNo || '') !== (orderNo.value || '') ||
      !Number.isFinite(Number(data.at)) ||
      Date.now() - Number(data.at) > maxAge ||
      Number(data.at) < visitStart()
    ) {
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

  // 本次会话的起点必须在拿到桌台 id 的当下就记下来：若拖到「有车要恢复」时才记,
  // 顾客这一顿点的菜就会被判成上一批的（存的时刻永远早于那一刻）。
  watch(
    tableId,
    id => {
      if (id) visitStart();
    },
    { immediate: true }
  );

  // 深度监听：数量、备注的变化也要落盘
  watch(cart, save, { deep: true });

  return { restore, save };
}
