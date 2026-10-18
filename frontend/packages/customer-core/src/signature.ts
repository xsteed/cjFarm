// 「本店招牌」合成分类（H5 与小程序共用同一份提取算法，避免两端橱窗不一致）。
//
// 它不是后端分类：菜单接口只把招牌标记下在菜品上（dish.signature），
// 前端把 signature=1 的菜聚成「左栏第一项 + 右栏置顶分组」（与星巴克
// 「人气推荐」、麦当劳「人气套餐」同结构），不需要后端任何改动。
// 真实分类 id 来自后端（正整数），取 -1 不会撞车。
import type { Dish, DishSpec, MenuCategory } from '@dining/types';

/** 招牌合成分类的 id（真实分类 id 为正整数，取 -1 不撞车）。 */
export const SIG_CAT_ID = -1;
/** 合成分类在左栏与分组标题上的文案：与小程序端一致。 */
export const SIG_CAT_NAME = '本店招牌';

/** 招牌菜卡片（展示所需字段，与小程序端 SigCard 同结构）。 */
export interface SigCard {
  dishId: number;
  dishName: string;
  /** 图片地址：H5 与小程序不同，H5 直接用接口给的 dishImage，不另走 CDN 前缀。 */
  image: string;
  /** 最低规格价（number，渲染时再用 money 格式化），多规格卡片展示「起」。 */
  priceText: number;
  /** 多个规格：价格是「起售价」，模板补一个「起」字。 */
  manySpecs: boolean;
  /** 已售罄（人工沽清或所有规格库存卖完）。 */
  soldOut: boolean;
}

/**
 * 从整份菜单里提取招牌菜卡片。
 *
 * 只取 signature=1 且有规格的菜（没有规格的根本下不了单），按菜单原序；
 * 同一道菜被归进多个真实分类时只收一次（与 catQtyMap「首次出现为准」同口径）。
 *
 * **不设数量上限**：分组是菜单的一部分，横向滑多久由店家打的招牌标记决定，
 * 悄悄截断会让左栏写着「本店招牌」、组里却少几道，顾客不知道还有没有。
 * （小程序首页橱窗另受 8 道上限，那是进店前的 teaser，口径不同。）
 */
export function signatureDishes(menu: MenuCategory[]): SigCard[] {
  const out: SigCard[] = [];
  const seen = new Set<number>();
  for (const c of menu) {
    for (const dish of (c.dishes || []) as Dish[]) {
      if (Number(dish.signature) !== 1) continue;
      const dishId = Number(dish.dishId) || 0;
      if (!dishId || seen.has(dishId)) continue;
      const specs = (dish.specs || []) as DishSpec[];
      if (!specs.length) continue;
      seen.add(dishId);
      const allSoldOut = specs.every(s => Number(s.stock) === 0);
      out.push({
        dishId,
        dishName: String(dish.dishName || ''),
        image: String(dish.dishImage || ''),
        priceText: Math.min(...specs.map(s => Number(s.price) || 0)),
        manySpecs: specs.length > 1,
        soldOut: Number(dish.soldOut) === 1 || allSoldOut
      });
    }
  }
  return out;
}
