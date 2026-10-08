// 菜单模糊搜索。
//
// 顾客通常只输入一两个字的碎片（「牛」「土」「红肉」），所以匹配要松一点：
//   ・先按「包含」匹配：菜名 / 描述 / 规格名，不区分大小写
//   ・再对菜名补一次「顺序包含」（子序列）匹配，让「红肉」也能找到「红烧肉」
// 多个关键词之间是「与」：每个词都要命中，否则结果会越搜越乱。
//
// 刻意不做拼音匹配：小程序端没有拼音库，自带一份字典的包体与维护成本都不划算，
// 而中文输入法本身会给出候选字，收益有限。
import type { Dish, MenuCategory } from '@dining/types';

/** 拆关键词：空白与中英文逗号都算分隔符；统一小写。 */
export function parseKeywords(raw: string): string[] {
  return String(raw || '')
    .toLowerCase()
    .split(/[\s,，]+/)
    .filter(Boolean);
}

/** 一道菜的可搜文本：菜名 + 描述 + 规格名。 */
export function dishHaystack(dish: Dish): string {
  const specs = (dish.specs || []).map(s => String(s.specName || ''));
  return [String(dish.dishName || ''), String(dish.description || ''), ...specs].join(' ').toLowerCase();
}

/** needle 的字符是否按顺序出现在 hay 里（「红肉」→「红烧肉」）。 */
export function isSubsequence(needle: string, hay: string): boolean {
  let i = 0;
  for (let j = 0; j < hay.length && i < needle.length; j += 1) {
    if (hay[j] === needle[i]) i += 1;
  }
  return i === needle.length;
}

/** 关键词是否命中一道菜：每个词都要在可搜文本里「包含」，或在菜名里「顺序包含」。 */
export function dishMatches(dish: Dish, keywords: string[]): boolean {
  if (!keywords.length) return true;
  const hay = dishHaystack(dish);
  const name = String(dish.dishName || '').toLowerCase();
  return keywords.every(kw => hay.includes(kw) || isSubsequence(kw, name));
}

/**
 * 过滤整份菜单：只保留有命中的菜品与分类，分类顺序不变。
 * 关键词为空时原样返回同一份引用（调用方据此走「不过滤」的零成本路径）。
 */
export function filterMenu(menu: MenuCategory[], raw: string): MenuCategory[] {
  const keywords = parseKeywords(raw);
  if (!keywords.length) return menu;

  const out: MenuCategory[] = [];
  for (const c of menu) {
    const dishes = (c.dishes || []).filter(d => dishMatches(d, keywords));
    if (dishes.length) out.push({ ...c, dishes });
  }
  return out;
}
