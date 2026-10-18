// 购物车本地持久化（小程序侧）。
//
// 为什么需要：购物车只挂在 App 实例上，微信杀进程 / 长时间后台回收 / 从「最近使用」
// 冷启动之后内存清零，但桌台与订单号还能从本地缓存恢复（store/session）。结果就是
// 「单还在，刚点的菜没了」—— 顾客只能从头再点一遍。
//
// ⚠️ 与 H5 的关键差异（不要照抄 useCartPersist）：
// H5 用 sessionStorage 记「本次会话起点」，恢复时要求 `at >= visitStart`，用来防
// 「上一批客人的车被下一批看到」。小程序**没有会话级存储**，冷启动本身就是新会话 ——
// 照抄那条判断会让冷启动永远恢复不了，功能形同虚设。
// 这里改用两个维度隔离，时间上限兜底：
//   ・桌台码：换桌就换了 key，不会串车；
//   ・订单号：结账 / 清台后订单号变化，旧车自然读不到（换一批客人必然换单）；
//   ・时间上限：有单给一整餐 12h，还没开单只给 60min（同一个人隔餐再来也不会带出旧车）。
// 同一部手机 = 同一个微信号，换一批客人一定换手机，所以不需要额外的「会话」维度。
//
// 恢复时必须按当前菜单复核：隔了一段时间回来，菜可能已下架、规格可能卖完、价格可能调过。
// 所以「菜没了 / 规格没了 / 已售罄」整行丢掉（不留半截状态让人困惑），价格一律取菜单现值。
import { isOrderable, isSoldOut } from '@dining/customer-core/spec';
import { setPersonCount, markPersonChosen } from './session';
import type { CartLine, GlobalData } from '../types';
import type { Dish, MenuCategory } from '@dining/types';

const KEY_PREFIX = 'dining_mp_cart';
/** 有单覆盖一整餐（与 H5 同值）。 */
const MAX_AGE_MS = 12 * 60 * 60 * 1000;
/** 还没开单：只保留一小时。 */
const ANON_MAX_AGE_MS = 60 * 60 * 1000;

interface PersistedCart {
  at: number;
  tableCode: string;
  /** 落盘时本桌的订单号；还没开单为空串（此时 key 里是 new）。 */
  orderNo: string;
  personCount: number;
  orderRemark: string;
  items: CartLine[];
}

function currentOrderNo(): string {
  return String(getApp<GlobalData>().session.currentOrder?.orderNo || '');
}

/**
 * 存储键：`dining_mp_cart:{桌台码}:{订单号|new}`。
 * 桌台码而不是数字 id —— 桌台码是二维码里那个稳定值，扫码进店时最先拿到。
 */
function storageKey(): string {
  const s = getApp<GlobalData>().session;
  return `${KEY_PREFIX}:${s.tableCode || '0'}:${currentOrderNo() || 'new'}`;
}

function removeKey(key: string): void {
  try {
    wx.removeStorageSync(key);
  } catch {
    /* 存储不可用：本来也没有残留可清 */
  }
}

/** 复核所需的最小字段：落盘的购物车行与历史订单明细都满足这个形状。 */
export interface RawCartLine {
  dishId?: number;
  specId?: number;
  quantity?: number;
  itemRemark?: string;
}

/**
 * 按当前菜单复核一批行：下架 / 菜品售罄 / 规格售罄 / 规格已不存在 → 整行丢弃；
 * 价格与图片一律取菜单现值。
 *
 * 购物车恢复与「再来一单」共用这一份实现 —— 两处一旦分叉，就会出现
 * 「恢复时正确丢掉售罄菜、再来一单却把售罄菜塞进车里」这种只在一条路径复现的问题。
 */
function rebuild(rawItems: RawCartLine[], menu: MenuCategory[]): CartLine[] {
  const byId = new Map<number, Dish>();
  for (const c of menu) {
    for (const d of c.dishes || []) byId.set(Number(d.dishId), d);
  }

  const valid: CartLine[] = [];
  for (const it of rawItems) {
    const dish = byId.get(Number(it.dishId));
    if (!dish || isSoldOut(dish) || !isOrderable(dish)) continue;
    const spec = (dish.specs || []).find(sp => Number(sp.specId) === Number(it.specId));
    if (!spec || Number(spec.stock) === 0) continue;
    valid.push({
      // key 规则与 store/cart.ts 保持一致：`菜品:规格`
      key: `${Number(dish.dishId)}:${Number(spec.specId)}`,
      dishId: Number(dish.dishId),
      dishName: String(dish.dishName || ''),
      specId: Number(spec.specId),
      specName: String(spec.specName || ''),
      // 价格以菜单现值为准，不沿用历史价
      price: Number(spec.price) || 0,
      image: String(dish.dishImage || ''),
      quantity: Math.max(1, Number(it.quantity) || 1),
      itemRemark: String(it.itemRemark || '')
    });
  }
  return valid;
}

/**
 * 把一批「菜 + 规格」换算成当前可下单的购物车行（「再来一单」用）。
 *
 * 只做换算，不动购物车：由调用方决定是替换还是并入（见 store/cart.ts 的
 * cartMergeLines）。返回空数组表示这些菜现在一样都点不了。
 */
export function linesFromItems(rawItems: RawCartLine[], menu: MenuCategory[]): CartLine[] {
  return rebuild(rawItems, menu);
}

/**
 * 落盘当前购物车。
 *
 * 调用点**只在 store/cart.ts 的每个写操作里**。小程序没有响应式 watch，
 * 若散落在各个页面（加菜、减菜、改备注、清空各一处），必然会漏掉某条路径，
 * 表现为「改了之后恢复出来还是旧车」—— 比不持久化更难排查。
 */
export function saveCart(): void {
  const app = getApp<GlobalData>();
  const s = app.session;
  // 还没识别到桌台：不落盘。否则所有「未识别桌台」的会话会共用 `0` 这个 key 互相污染。
  if (!s.tableCode) return;
  const key = storageKey();
  if (!app.cart.length) {
    removeKey(key);
    return;
  }
  const payload: PersistedCart = {
    at: Date.now(),
    tableCode: s.tableCode,
    orderNo: currentOrderNo(),
    personCount: s.personCount,
    orderRemark: s.orderRemark,
    items: app.cart.map(l => ({ ...l }))
  };
  try {
    wx.setStorageSync(key, payload);
  } catch {
    /* 磁盘满 / 隐私限制：只影响「下次能否恢复」，不打扰顾客 */
  }
}

/**
 * 菜单到位后调用：把上次没提交的车按当前菜单复核后放回。
 *
 * @param menu 刚拉到的菜单（复核下架/售罄/改价都靠它）
 * @returns 是否真的恢复了内容（调用方无需处理，测试用）
 */
export function restoreCart(menu: MenuCategory[]): boolean {
  const app = getApp<GlobalData>();
  const s = app.session;
  if (!s.tableCode) return false;
  // 内存里已经有车（顾客正在点）：不覆盖。renderMenu 可能被多次调用
  // （首次进入、跳订单页失败回退），这条判断让恢复只发生一次。
  if (app.cart.length) return false;

  const key = storageKey();
  let data: PersistedCart | '' | undefined;
  try {
    data = wx.getStorageSync(key) as PersistedCart | '' | undefined;
  } catch {
    return false;
  }
  if (!data || typeof data !== 'object' || !Array.isArray(data.items)) {
    removeKey(key);
    return false;
  }

  // 不是同一批客人（订单号变了）或太久没动：整份丢弃
  const maxAge = data.orderNo ? MAX_AGE_MS : ANON_MAX_AGE_MS;
  if (
    String(data.orderNo || '') !== currentOrderNo() ||
    !Number.isFinite(Number(data.at)) ||
    Date.now() - Number(data.at) > maxAge
  ) {
    removeKey(key);
    return false;
  }

  const valid = rebuild(data.items, menu);
  // 复核后一行不剩：清掉存储，别让坏数据一直留着
  if (!valid.length) {
    removeKey(key);
    return false;
  }

  // 原地替换而不是换引用：各页面都是通过 cartState() 现取 getApp().cart
  app.cart.length = 0;
  app.cart.push(...valid);
  if (Number(data.personCount) > 0) {
    setPersonCount(Number(data.personCount));
    // 恢复出的人数本身就是一次「已选」证据：有车 = 之前进店时已经选过，
    // 标记后菜单页不会再弹「选人数」面板
    markPersonChosen();
  }
  s.orderRemark = String(data.orderRemark || '');
  return true;
}
