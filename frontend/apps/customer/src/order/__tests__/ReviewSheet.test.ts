// 订单评价弹层的单测。
//
// 钉住三个关键交互:星级必填(不选不能提交)、标签按星级联动(切组清空防矛盾组合)、
// 提交防重锁(弱网下连点只发一次)。
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ReviewSheet from '../ReviewSheet.vue';

function mountSheet(over: Partial<{ submitting: boolean }> = {}) {
  return mount(ReviewSheet, {
    props: { submitting: false, ...over }
  });
}

describe('ReviewSheet', () => {
  it('未选星级时提交按钮禁用且不触发 submit', async () => {
    const w = mountSheet();
    expect(w.find('.review-submit').classes()).toContain('disabled');
    await w.find('.review-submit').trigger('click');
    expect(w.emitted('submit')).toBeUndefined();
  });

  it('点星星亮起并显示文案,带星提交抛出完整 payload', async () => {
    const w = mountSheet();
    await w.findAll('.review-star')[4].trigger('click');
    expect(w.find('.review-rating-text').text()).toBe('非常满意');
    await w.find('.review-submit').trigger('click');
    const evt = w.emitted('submit');
    expect(evt).toHaveLength(1);
    expect(evt![0][0]).toEqual({ rating: 5, tags: [], content: '', images: [] });
  });

  it('标签按星级联动:5 星出好评组、2 星出差评组、切换清空已选', async () => {
    const w = mountSheet();
    await w.findAll('.review-star')[4].trigger('click');
    const good = w.findAll('.review-chip').map(c => c.text());
    expect(good).toContain('味道好');
    expect(good).not.toContain('上菜慢');
    // 选一个好评标签,再切到 2 星:标签组应换且已选清空。
    await w.findAll('.review-chip')[0].trigger('click');
    await w.findAll('.review-star')[1].trigger('click');
    const bad = w.findAll('.review-chip').map(c => c.text());
    expect(bad).toContain('上菜慢');
    expect(bad).not.toContain('味道好');
    await w.find('.review-submit').trigger('click');
    expect(w.emitted('submit')![0][0]).toMatchObject({ rating: 2, tags: [] });
  });

  it('3 星给两组标签,最多选 6 个', async () => {
    const w = mountSheet();
    await w.findAll('.review-star')[2].trigger('click');
    const chips = w.findAll('.review-chip');
    expect(chips.length).toBe(12);
    for (const c of chips) await c.trigger('click');
    expect(w.findAll('.review-chip.active').length).toBe(6);
  });

  it('文字输入计入字数并随 submit 抛出', async () => {
    const w = mountSheet();
    await w.find('.review-content').setValue('上菜有点慢，味道不错');
    expect(w.find('.review-count').text()).toBe('10/200');
    await w.findAll('.review-star')[3].trigger('click');
    await w.find('.review-submit').trigger('click');
    expect(w.emitted('submit')![0][0]).toMatchObject({ content: '上菜有点慢，味道不错' });
  });

  it('提交中锁定按钮,不重复触发 submit', async () => {
    const w = mountSheet({ submitting: true });
    await w.findAll('.review-star')[4].trigger('click');
    await w.find('.review-submit').trigger('click');
    expect(w.emitted('submit')).toBeUndefined();
    expect(w.find('.review-submit').text()).toBe('提交中…');
  });

  it('点遮罩与 × 都是关闭,不触发 submit', async () => {
    const w = mountSheet();
    await w.find('.review-mask').trigger('click');
    await w.find('.review-close').trigger('click');
    expect(w.emitted('close')).toHaveLength(2);
    expect(w.emitted('submit')).toBeUndefined();
  });
});
