// 规格 / 备注弹层的单测（对齐小程序 components/spec-sheet）。
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import SpecSheet from '../SpecSheet.vue';
import type { DishSpec, RemarkOption } from '@dining/types';

const specs: DishSpec[] = [
  { specId: 1, specName: '小份', price: 18, stock: -1 },
  { specId: 2, specName: '中份', price: 28, stock: -1 },
  { specId: 3, specName: '大份', price: 38, stock: 0 } // 售罄
];

const remarks: RemarkOption[] = [
  { remarkId: 1, optionName: '不要辣' },
  { remarkId: 2, optionName: '少油' }
];

function mountSheet(
  over: Partial<{ selectedSpecId: number; selected: string[]; custom: string; inCartQty: number }> = {}
) {
  return mount(SpecSheet, {
    props: {
      dishName: '牛肉面',
      specs,
      remarks,
      selectedSpecId: 1,
      selected: [],
      custom: '',
      inCartQty: 0,
      ...over
    }
  });
}

describe('SpecSheet', () => {
  it('展示菜名与当前选中规格的价格', () => {
    const w = mountSheet({ selectedSpecId: 2 });
    expect(w.find('.spec-title').text()).toBe('牛肉面');
    expect(w.find('.ft-price').text()).toContain('28.00');
  });

  it('售罄规格标「售罄」且不显示价格，选中态只给可点的那个', () => {
    const w = mountSheet();
    const chips = w.findAll('.spec-sec .chip');
    expect(chips[0].classes()).toContain('on');
    expect(chips[2].classes()).toContain('off');
    expect(chips[2].text()).toContain('售罄');
    expect(chips[2].text()).not.toContain('¥');
  });

  it('点规格抛出 pick-spec（是否可售由 useCart 拦）', async () => {
    const w = mountSheet();
    await w.findAll('.spec-sec .chip')[1].trigger('click');
    expect(w.emitted('pick-spec')?.[0]).toEqual([2]);
  });

  it('备注选项选中态与 toggle', async () => {
    const w = mountSheet({ selected: ['不要辣'] });
    const remarkChips = w.findAll('.spec-sec')[1].findAll('.chip');
    expect(remarkChips[0].classes()).toContain('on');
    await remarkChips[1].trigger('click');
    expect(w.emitted('toggle-remark')?.[0]).toEqual(['少油']);
  });

  it('自定义输入往上抛 update:custom', async () => {
    const w = mountSheet();
    await w.find('.spec-input').setValue('少放盐');
    expect(w.emitted('update:custom')?.[0]).toEqual(['少放盐']);
  });

  it('购物车已有份数时提示「确定 = 再加一份」', () => {
    expect(mountSheet({ inCartQty: 0 }).find('.spec-hint').exists()).toBe(false);
    expect(mountSheet({ inCartQty: 2 }).find('.spec-hint').text()).toContain('购物车已有 2 份');
  });

  it('点遮罩与 × 都是关闭，不触发 confirm', async () => {
    const w = mountSheet();
    await w.find('.spec-close').trigger('click');
    await w.find('.spec-mask').trigger('click');
    expect(w.emitted('close')).toHaveLength(2);
    expect(w.emitted('confirm')).toBeUndefined();
  });

  it('只有「确定」才 confirm', async () => {
    const w = mountSheet();
    await w.find('.sp-btn').trigger('click');
    expect(w.emitted('confirm')).toHaveLength(1);
    expect(w.emitted('close')).toBeUndefined();
  });
});
