// 订单评价的纯逻辑口径(标签分组与上限)。
//
// 与 H5 的 ReviewSheet.vue 各自维护一份、语义必须逐字一致(平行实现,见
// docs/miniprogram.md 第五节):改动任何一组文案/上限,两端同步。
// 单测钉住:tests/core/review.test.ts。

/** 快捷标签固定两组(v1 不做商户自定义):好评组 / 差评组。 */
export const REVIEW_GOOD_TAGS = ['味道好', '上菜快', '服务好', '分量足', '性价比高', '环境舒适'];
export const REVIEW_BAD_TAGS = ['味道一般', '上菜慢', '服务态度', '分量少', '环境吵', '不如预期'];

/** 标签最多个数;单个标签最长字符数。 */
export const REVIEW_TAGS_MAX = 6;
export const REVIEW_TAG_MAX_LEN = 12;
/** 文字评价最长字符数(rune)。 */
export const REVIEW_CONTENT_MAX_LEN = 200;

/**
 * 星级 → 可选标签组:4/5 好评组、1/2 差评组、3 星两组都给;0 与越界给空(非法输入不给标签组)。
 * 切换星级时调用方应清空已选,避免「2 星 + 味道好」的矛盾组合。
 */
export function reviewTagsOf(rating: number): string[] {
  if (rating === 4 || rating === 5) return REVIEW_GOOD_TAGS;
  if (rating === 1 || rating === 2) return REVIEW_BAD_TAGS;
  if (rating === 3) return [...REVIEW_GOOD_TAGS, ...REVIEW_BAD_TAGS];
  return [];
}

/** 星级文案(0 星未选返回提示文案)。 */
export function reviewRatingText(rating: number): string {
  return ['', '很不满意', '不太满意', '一般', '满意', '非常满意'][rating] || '点星星评分';
}

/** 清洗标签入参:去空格、逗号替换为空格(逗号是存储分隔符)、截断、去重、限量。 */
export function sanitizeReviewTags(tags: string[]): string[] {
  const out: string[] = [];
  for (const raw of tags) {
    let t = String(raw || '').trim().replace(/,/g, ' ');
    if (t.length > REVIEW_TAG_MAX_LEN) t = Array.from(t).slice(0, REVIEW_TAG_MAX_LEN).join('');
    if (!t || out.includes(t)) continue;
    out.push(t);
    if (out.length >= REVIEW_TAGS_MAX) break;
  }
  return out;
}
