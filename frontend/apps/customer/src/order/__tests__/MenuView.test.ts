// 菜单视图的单测。
//
// 匹配算法本身在 packages/customer-core 的 search.test.ts 里钉住了，
// 这里只验证「页面接上搜索之后的表现」：左栏收起、命中过滤、无结果提示、清空恢复，
// 以及菜单行的点击分工（整行开弹层 vs 行内 chip / 加减号各自拦掉冒泡）。
import { mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import MenuView from '../MenuView.vue';
import type { Dish, DishSpec, MenuCategory } from '@dining/types';

function spec(specId: number, specName: string, price: number): DishSpec {
  return { specId, specName, price, stock: -1 };
}

const menu: MenuCategory[] = [
  {
    categoryId: 1,
    categoryName: '主食',
    dishes: [
      { dishId: 1, dishName: '牛肉面', specs: [spec(11, '小份', 18)] },
      { dishId: 2, dishName: '红烧肉', specs: [spec(12, '例份', 38)] }
    ]
  },
  {
    categoryId: 2,
    categoryName: '饮品',
    // 2 个规格：行内铺 chip，不进弹层
    dishes: [{ dishId: 3, dishName: '可乐', specs: [spec(21, '大杯', 5), spec(22, '小杯', 3)] }]
  },
  {
    categoryId: 3,
    categoryName: '锅底',
    // 4 个规格 > INLINE_SPEC_MAX：行内放不下，必须走弹层
    dishes: [
      {
        dishId: 4,
        dishName: '鸳鸯锅',
        specs: [spec(31, '小锅', 28), spec(32, '中锅', 38), spec(33, '大锅', 48), spec(34, '特大锅', 58)]
      }
    ]
  }
];

// 卖家把唯一规格点成沽清（stock=0）的菜单：整道菜按「售罄」处理。
const soldOutMenu: MenuCategory[] = [
  {
    categoryId: 1,
    categoryName: '主食',
    dishes: [{ dishId: 1, dishName: '牛肉面', specs: [{ specId: 11, specName: '小份', price: 18, stock: 0 }] }]
  }
];

const openSheet = vi.fn();
const changeQty = vi.fn();
const selectSpec = vi.fn();

function commonProps() {
  return {
    menu,
    keyword: '',
    selectedSpec: { 1: 11, 2: 12, 3: 21, 4: 31 },
    imgErrs: new Set<number>(),
    showTabbar: false,
    activeCat: 1,
    orderedQty: () => 0,
    currentSpec: (d: Dish) => (d.specs || [])[0] as DishSpec,
    dishQty: () => 0,
    selectSpec,
    changeQty,
    onImgErr: () => undefined,
    setListRef: () => undefined,
    setGroupRef: () => undefined,
    jumpTo: () => undefined,
    handleScroll: () => undefined,
    openSheet
  };
}

function mountView() {
  return mount(MenuView, { props: commonProps() });
}

function dishNames(w: ReturnType<typeof mountView>): string[] {
  return w.findAll('.op-dish .op-name').map(n => n.text());
}

beforeEach(() => {
  openSheet.mockClear();
  changeQty.mockClear();
  selectSpec.mockClear();
});

describe('MenuView 搜索', () => {
  it('默认按分类浏览：左栏在、全部菜品都在', () => {
    const w = mountView();
    expect(w.find('.op-side').exists()).toBe(true);
    expect(w.findAll('.op-dish')).toHaveLength(4);
  });

  it('输入关键词后收起左栏，只留命中的菜品', async () => {
    const w = mountView();
    await w.setProps({ keyword: '牛肉' });
    expect(w.find('.op-side').exists()).toBe(false);
    expect(dishNames(w)).toEqual(['牛肉面']);
  });

  it('菜名支持顺序包含：红肉 → 红烧肉', async () => {
    const w = mountView();
    await w.setProps({ keyword: '红肉' });
    expect(dishNames(w)).toEqual(['红烧肉']);
  });

  it('规格名也能搜到所属菜品', async () => {
    const w = mountView();
    await w.setProps({ keyword: '大杯' });
    expect(dishNames(w)).toEqual(['可乐']);
  });

  it('没命中时给出「没有找到」且不渲染任何菜品', async () => {
    const w = mountView();
    await w.setProps({ keyword: '龙虾' });
    expect(w.findAll('.op-dish')).toHaveLength(0);
    expect(w.find('.op-empty').text()).toContain('没有找到「龙虾」');
  });

  it('清空关键词后恢复分类浏览', async () => {
    const w = mountView();
    await w.setProps({ keyword: '可乐' });
    expect(w.findAll('.op-dish')).toHaveLength(1);
    await w.setProps({ keyword: '' });
    expect(w.find('.op-side').exists()).toBe(true);
    expect(w.findAll('.op-dish')).toHaveLength(4);
  });

  it('菜单整体为空时提示联系店员', () => {
    const w = mount(MenuView, { props: { ...commonProps(), menu: [] } });
    expect(w.find('.op-empty').text()).toContain('菜单暂时为空');
  });
});

describe('MenuView 行的点击分工（对齐小程序：整行开弹层）', () => {
  it('整行任意位置都能打开弹层', async () => {
    const w = mountView();
    await w.findAll('.op-dish')[0].trigger('click');
    expect(openSheet).toHaveBeenCalledTimes(1);
  });

  it('点行内的规格 chip 只切规格，不会顺带弹层', async () => {
    const w = mountView();
    const cola = w.findAll('.op-dish')[2];
    expect(cola.findAll('.op-spec')).toHaveLength(2);
    await cola.findAll('.op-spec')[1].trigger('click');
    expect(selectSpec).toHaveBeenCalledTimes(1);
    expect(openSheet).not.toHaveBeenCalled();
  });

  it('规格在 3 个以内：点「+」直接加购，不弹层', async () => {
    const w = mountView();
    await w.findAll('.op-dish')[0].find('.op-add').trigger('click');
    expect(changeQty).toHaveBeenCalledTimes(1);
    expect(openSheet).not.toHaveBeenCalled();
  });

  it('规格超过 3 个：行内不铺 chip，价格是最低价并带「起」', () => {
    const w = mountView();
    const many = w.findAll('.op-dish')[3];
    expect(many.find('.op-specs').exists()).toBe(false);
    expect(many.find('.op-price').text()).toContain('28.00');
    expect(many.find('.op-price').text()).toContain('起');
  });

  it('规格超过 3 个：点「+」进弹层而不是直接加购', async () => {
    const w = mountView();
    await w.findAll('.op-dish')[3].find('.op-add').trigger('click');
    expect(openSheet).toHaveBeenCalledTimes(1);
    expect(changeQty).not.toHaveBeenCalled();
  });
});

describe('MenuView 沽清菜品的步进器', () => {
  it('沽清且不在购物车里：显示「售罄」占位，没有加减号', () => {
    const w = mount(MenuView, {
      props: { ...commonProps(), menu: soldOutMenu }
    });
    const row = w.findAll('.op-dish')[0];
    expect(row.find('.op-soldout').text()).toBe('售罄');
    expect(row.find('.op-stepper').exists()).toBe(false);
    expect(row.find('.op-add').exists()).toBe(false);
  });

  it('沽清但已在购物车里：保留步进器，「−」可减、「+」置灰不可加', async () => {
    const w = mount(MenuView, {
      props: { ...commonProps(), menu: soldOutMenu, dishQty: () => 2 }
    });
    const row = w.findAll('.op-dish')[0];
    // 不能把加减号整体换成「售罄」——那样顾客在卡片上减不掉，只能进购物车抽屉删。
    expect(row.find('.op-stepper').exists()).toBe(true);
    expect(row.find('.op-soldout').exists()).toBe(false);
    expect(row.find('.op-stepper .n').text()).toBe('2');

    await row.find('.op-stepper .m').trigger('click');
    expect(changeQty).toHaveBeenCalledWith(expect.objectContaining({ dishId: 1 }), -1);

    // 「+」置灰：点了不该加购、也不该弹层
    changeQty.mockClear();
    const plus = row.find('.op-stepper .p');
    expect(plus.classes()).toContain('off');
    await plus.trigger('click');
    expect(changeQty).not.toHaveBeenCalled();
    expect(openSheet).not.toHaveBeenCalled();
  });
});

describe('MenuView 本店招牌（对齐小程序端 sigstrip）', () => {
  const sigCards = [
    { dishId: 1, dishName: '牛肉面', image: '', priceText: 18, manySpecs: false, soldOut: false },
    { dishId: 3, dishName: '可乐', image: '', priceText: 3, manySpecs: true, soldOut: false }
  ];

  function mountWithSig(extra: Record<string, unknown> = {}) {
    return mount(MenuView, { props: { ...commonProps(), sigCards, ...extra } });
  }

  it('非搜索态且有招牌菜：左栏置顶「本店招牌」、右栏出现横滑卡片条', () => {
    const w = mountWithSig();
    const sideNames = w.findAll('.op-cat').map(c => c.text());
    expect(sideNames[0]).toContain('本店招牌');
    expect(w.find('.op-sigstrip').exists()).toBe(true);
    expect(w.findAll('.op-sigcard')).toHaveLength(2);
  });

  it('招牌卡片点击打开规格弹层（按 dishId 找回完整菜品）', async () => {
    const w = mountWithSig();
    await w.findAll('.op-sigcard')[0].trigger('click');
    expect(openSheet).toHaveBeenCalledTimes(1);
    const arg = openSheet.mock.calls[0][0] as { dishId?: number };
    expect(arg.dishId).toBe(1);
  });

  it('搜索态下隐藏招牌条与左栏', async () => {
    const w = mountWithSig({ keyword: '牛肉' });
    expect(w.find('.op-sigstrip').exists()).toBe(false);
    expect(w.find('.op-side').exists()).toBe(false);
  });

  it('没有招牌菜（sigCards 为空）时不渲染招牌条，左栏不含「本店招牌」', () => {
    const w = mountView();
    expect(w.find('.op-sigstrip').exists()).toBe(false);
    expect(w.findAll('.op-cat').map(c => c.text())).not.toContain('本店招牌');
  });

  it('招牌分类角标显示已点/购物车合计份数', () => {
    const w = mountWithSig({
      orderedQty: (id?: number) => (id === 1 ? 2 : 0),
      cart: [{ dishId: 3, quantity: 1 }] as never
    });
    const sigCat = w.findAll('.op-cat').find(c => c.text().includes('本店招牌'))!;
    expect(sigCat.find('.cat-badge').text()).toBe('3'); // 已点 2 + 购物车 1
  });
});
