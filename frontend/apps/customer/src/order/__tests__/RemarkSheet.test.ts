// 单项备注弹窗的单测。
//
// 重点钉住「关闭 ≠ 保存」:弹层盖在购物车抽屉之上,顾客点空白处/右上角的本意
// 都是「不改了」,只有「确定」才该写回购物车。
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import RemarkSheet from '../RemarkSheet.vue';
import type { RemarkOption } from '@dining/types';

const remarks: RemarkOption[] = [
  { remarkId: 1, optionName: '不要辣' },
  { remarkId: 2, optionName: '少油' }
];

function mountSheet(over: Partial<{ selected: string[]; custom: string; hasRemark: boolean }> = {}) {
  return mount(RemarkSheet, {
    props: {
      dishName: '牛肉面',
      selected: [],
      custom: '',
      remarks,
      hasRemark: false,
      ...over
    }
  });
}

describe('RemarkSheet', () => {
  it('点遮罩是「关闭」而不是「保存」', async () => {
    const w = mountSheet();
    await w.find('.remark-mask').trigger('click');
    expect(w.emitted('close')).toHaveLength(1);
    expect(w.emitted('save')).toBeUndefined();
  });

  it('右上角 × 也是关闭', async () => {
    const w = mountSheet();
    await w.find('.remark-close').trigger('click');
    expect(w.emitted('close')).toHaveLength(1);
    expect(w.emitted('save')).toBeUndefined();
  });

  it('只有「确定」才保存', async () => {
    const w = mountSheet();
    await w.find('.remark-confirm').trigger('click');
    expect(w.emitted('save')).toHaveLength(1);
    expect(w.emitted('close')).toBeUndefined();
  });

  it('选中过的选项带 active 态，再点一次触发 toggle 取消', async () => {
    const w = mountSheet({ selected: ['不要辣'] });
    const chips = w.findAll('.remark-chip');
    expect(chips[0].classes()).toContain('active');
    expect(chips[1].classes()).not.toContain('active');
    await chips[0].trigger('click');
    expect(w.emitted('toggle')?.[0]).toEqual(['不要辣']);
  });

  it('自定义输入往上抛 update:custom', async () => {
    const w = mountSheet();
    const input = w.find('.remark-custom');
    await input.setValue('多加点香菜');
    expect(w.emitted('update:custom')?.[0]).toEqual(['多加点香菜']);
  });

  it('已有备注才出现「清除」入口', () => {
    expect(mountSheet({ hasRemark: false }).find('.remark-clear').exists()).toBe(false);
    expect(mountSheet({ hasRemark: true }).find('.remark-clear').exists()).toBe(true);
  });

  it('点「清除」只抛 clear，不直接保存', async () => {
    const w = mountSheet({ hasRemark: true, selected: ['不要辣'] });
    await w.find('.remark-clear').trigger('click');
    expect(w.emitted('clear')).toHaveLength(1);
    expect(w.emitted('save')).toBeUndefined();
  });

  it('展示菜名，让顾客确认改的是哪一道', () => {
    expect(mountSheet().find('.remark-target').text()).toBe('牛肉面');
  });
});
