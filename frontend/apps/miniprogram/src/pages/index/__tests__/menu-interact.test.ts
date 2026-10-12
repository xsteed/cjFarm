// 菜单页「搜索」与「行内规格」的回归用例。
//
// 这两块都容易在改动中悄悄坏掉，而且坏了顾客很难描述清楚：
//   ・搜索：搜不到（明明有这道菜）或搜太松（满屏无关结果）
//   ・行内规格 chip：切了规格但价格/份数没跟着走，或「+」加的不是当前选中的规格
//     —— 后者会直接下错单，所以按 H5 useCart 的口径（只算所选规格）钉住。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartAdd, cartClear, cartState } from '../../../store/cart';

/** 4 个规格（超过行内阈值）、2 个规格、1 个规格、以及一道没有规格的历史数据。 */
const MENU = [
  {
    categoryId: 1,
    categoryName: '荤菜',
    dishes: [
      { dishId: 5, dishName: '清蒸鱼', specs: [{ specId: 51, specName: '份', price: 68 }] },
      {
        dishId: 6,
        dishName: '红烧肉',
        description: '微辣，招牌',
        specs: [
          { specId: 61, specName: '小份', price: 38 },
          { specId: 62, specName: '大份', price: 58 }
        ]
      },
      {
        dishId: 7,
        dishName: '四规格菜',
        specs: [1, 2, 3, 4].map(i => ({ specId: 70 + i, specName: `规格${i}`, price: i * 10 }))
      },
      { dishId: 8, dishName: '没有规格的菜', specs: [] },
      { dishId: 10, dishName: '限量牛排', soldOut: 1, specs: [{ specId: 101, specName: '份', price: 128 }] }
    ]
  },
  {
    categoryId: 2,
    categoryName: '凉菜',
    dishes: [{ dishId: 9, dishName: '拍黄瓜', specs: [{ specId: 91, specName: '份', price: 12 }] }]
  }
];

const TABLE_OK = { tableId: 3, tableName: '大厅03桌', tableCode: 'A1', servingOrders: [] };

interface Row {
  kind: string;
  dishId?: number;
  key?: string;
  priceText?: string;
  manySpecs?: boolean;
  orderable?: boolean;
  soldOut?: boolean;
  specChips?: { specName: string; on: boolean }[];
  cartQty?: number;
  categoryName?: string;
}

async function loadMenu(): Promise<PageInstance> {
  const page = await mountPage('index', () => import('../index'));
  wxMock.reply({ code: 200, msg: 'ok', data: {} }); // 公开配置
  wxMock.reply({ code: 200, msg: 'ok', data: TABLE_OK }); // 桌台
  wxMock.reply({ code: 200, msg: 'ok', data: MENU }); // 菜单
  wxMock.reply({ code: 200, msg: 'ok', data: [] }); // 备注
  page.onLoad({ table: 'A1' });
  await flush();
  return page;
}

function rows(page: PageInstance): Row[] {
  return page.data.rows as Row[];
}

/** 右栏当前渲染出来的菜品 id（按顺序）。 */
function dishIds(page: PageInstance): number[] {
  return rows(page)
    .filter(r => r.kind === 'dish')
    .map(r => Number(r.dishId));
}

function dishRow(page: PageInstance, dishId: number): Row {
  const hit = rows(page).find(r => r.kind === 'dish' && r.dishId === dishId);
  if (!hit) throw new Error(`没有渲染出菜品 ${dishId}`);
  return hit;
}

function tap(id: number, extra: Record<string, string> = {}): never {
  return { currentTarget: { dataset: { id: String(id), ...extra } } } as never;
}

describe('菜单页 · 搜索', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  it('按菜名过滤：只剩命中的菜品，没有命中的分类连标题一起收掉', async () => {
    const page = await loadMenu();
    page.applyKeyword('鱼');

    expect(page.data.searching).toBe(true);
    expect(dishIds(page)).toEqual([5]);
    expect(rows(page).filter(r => r.kind === 'sec').map(r => r.categoryName)).toEqual(['荤菜']);
  });

  it('支持顺序包含（红肉 → 红烧肉）与描述命中', async () => {
    const page = await loadMenu();

    page.applyKeyword('红肉');
    expect(dishIds(page)).toEqual([6]);

    page.applyKeyword('微辣');
    expect(dishIds(page)).toEqual([6]);
  });

  it('搜不到时给「没有找到」，而不是一片空白', async () => {
    const page = await loadMenu();
    page.applyKeyword('龙虾');

    expect(page.data.searchEmpty).toBe(true);
    expect(dishIds(page)).toEqual([]);
  });

  it('清空搜索词后恢复完整菜单', async () => {
    const page = await loadMenu();
    page.applyKeyword('鱼');
    page.applyKeyword('');

    expect(page.data.searching).toBe(false);
    expect(page.data.searchEmpty).toBe(false);
    expect(dishIds(page)).toEqual([5, 6, 7, 8, 10, 9]);
  });
});

describe('菜单页 · 行内规格', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  it('规格按数量分档：2~3 个出行内 chip，更多只能进弹层，没有规格的标为不可点', async () => {
    const page = await loadMenu();

    const two = dishRow(page, 6);
    expect(two.specChips?.map(c => c.specName)).toEqual(['小份', '大份']);
    expect(two.specChips?.[0].on).toBe(true);
    expect(two.priceText).toBe('38.00');
    expect(two.manySpecs).toBe(false);

    const many = dishRow(page, 7);
    expect(many.specChips).toEqual([]);
    expect(many.manySpecs).toBe(true);
    // 行内点不了规格，价格只能给最低价 +「起」
    expect(many.priceText).toBe('10.00');

    expect(dishRow(page, 8).orderable).toBe(false);
    expect(dishRow(page, 9).orderable).toBe(true);
  });

  it('切换 chip 后价格、选中态与份数都跟着所选规格走', async () => {
    const page = await loadMenu();
    const dish = MENU[0].dishes[1];

    // 小份 ×1、大份 ×2：份数要按「当前选中规格」显示，与 H5 dishQty 同口径
    cartAdd({ dishId: 6, dishName: '红烧肉', specId: 61, specName: '小份', price: 38, image: '' });
    cartAdd({ dishId: 6, dishName: '红烧肉', specId: 62, specName: '大份', price: 58, image: '' });
    cartAdd({ dishId: 6, dishName: '红烧肉', specId: 62, specName: '大份', price: 58, image: '' });

    page.specPicks()[6] = 61;
    const small = page.dishRowOf(dish, {});
    expect(small.priceText).toBe('38.00');
    expect(small.cartQty).toBe(1);
    expect(small.specChips?.[0].on).toBe(true);

    page.specPicks()[6] = 62;
    const large = page.dishRowOf(dish, {});
    expect(large.priceText).toBe('58.00');
    expect(large.cartQty).toBe(2);
    expect(large.specChips?.[1].on).toBe(true);
  });

  it('「+」加的是当前选中的规格（不是第一个规格）', async () => {
    const page = await loadMenu();
    page.specPicks()[6] = 62;
    page.onAdd(tap(6));

    expect(cartState()).toHaveLength(1);
    expect(cartState()[0].specId).toBe(62);
    expect(cartState()[0].price).toBe(58);
  });

  it('规格太多时「+」打开弹层，绝不替顾客猜规格', async () => {
    const page = await loadMenu();
    page.onAdd(tap(7));

    expect(page.data.sheetVisible).toBe(true);
    expect(cartState()).toHaveLength(0);
  });

  it('没有规格的菜「+」只提示，不入购物车', async () => {
    const page = await loadMenu();
    page.onAdd(tap(8));

    expect(cartState()).toHaveLength(0);
    expect(wxMock.lastToast).toContain('暂不可点');
  });
});

describe('菜单页 · 售罄', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  it('售罄菜仍展示（可下单标记为 false 但 orderable 为 true），只是不可下单', async () => {
    const page = await loadMenu();
    const row = dishRow(page, 10);
    expect(row.soldOut).toBe(true);
    // 有规格、只是今天没了：与「没有规格」是两回事，价格要照常显示
    expect(row.orderable).toBe(true);
    expect(row.priceText).toBe('128.00');
  });

  it('售罄菜的「+」与「点菜名」都提示已售罄，绝不入车', async () => {
    const page = await loadMenu();

    page.onAdd(tap(10));
    expect(cartState()).toHaveLength(0);
    expect(wxMock.lastToast).toContain('售罄');

    page.onOpenSheet(tap(10));
    expect(page.data.sheetVisible).toBe(false);
    expect(wxMock.lastToast).toContain('售罄');
  });
});
