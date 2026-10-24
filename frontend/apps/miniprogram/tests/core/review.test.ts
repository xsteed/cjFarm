// 订单评价纯逻辑的单测:标签分组联动 / 文案 / 清洗。
// 与 H5 ReviewSheet.vue 是平行实现,这里的断言就是「两端语义一致」的锚点之一
// (H5 侧由 ReviewSheet.test.ts 钉住同一批语义)。
import { describe, expect, it } from 'vitest';
import {
  REVIEW_BAD_TAGS,
  REVIEW_CONTENT_MAX_LEN,
  REVIEW_GOOD_TAGS,
  REVIEW_TAGS_MAX,
  reviewRatingText,
  reviewTagsOf,
  sanitizeReviewTags
} from '../../src/core/review';

describe('reviewTagsOf 星级联动', () => {
  it('4-5 星出好评组', () => {
    expect(reviewTagsOf(4)).toEqual(REVIEW_GOOD_TAGS);
    expect(reviewTagsOf(5)).toEqual(REVIEW_GOOD_TAGS);
    expect(reviewTagsOf(5)).not.toContain('上菜慢');
  });

  it('1-2 星出差评组', () => {
    expect(reviewTagsOf(1)).toEqual(REVIEW_BAD_TAGS);
    expect(reviewTagsOf(2)).toEqual(REVIEW_BAD_TAGS);
    expect(reviewTagsOf(2)).not.toContain('味道好');
  });

  it('3 星两组都给;0 星与越界给空', () => {
    expect(reviewTagsOf(3)).toHaveLength(14);
    expect(reviewTagsOf(0)).toEqual([]);
    expect(reviewTagsOf(6)).toEqual([]);
    expect(reviewTagsOf(-1)).toEqual([]);
  });
});

describe('reviewRatingText', () => {
  it('各星级文案与未选提示', () => {
    expect(reviewRatingText(0)).toBe('点星星评分');
    expect(reviewRatingText(1)).toBe('很不满意');
    expect(reviewRatingText(5)).toBe('非常满意');
  });
});

describe('sanitizeReviewTags', () => {
  it('去重/去空/限量', () => {
    expect(sanitizeReviewTags(['味道好', '味道好', '', '上菜快'])).toEqual(['味道好', '上菜快']);
    expect(sanitizeReviewTags(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'])).toHaveLength(REVIEW_TAGS_MAX);
  });

  it('逗号替换为空格(逗号是存储分隔符)并按字符截断', () => {
    expect(sanitizeReviewTags(['味道,好'])).toEqual(['味道 好']);
    const long = Array.from({ length: 20 }, () => '长').join('');
    expect(sanitizeReviewTags([long])[0]).toHaveLength(12);
  });

  it('非字符串入参安全', () => {
    expect(sanitizeReviewTags([undefined as unknown as string, null as unknown as string, 'ok'])).toEqual(['ok']);
  });
});

describe('常量与 H5 对齐', () => {
  it('文字上限 200', () => {
    expect(REVIEW_CONTENT_MAX_LEN).toBe(200);
  });
});
