// 菜单页「本店招牌」分组的回归用例。
//
// 「本店招牌」是前端合成分类（后端只在菜品上打 signature 标记）：左栏第一项 +
// 右栏置顶分组，分组内容是与首页橱窗同款的横滑卡片条（图带「招牌」角标，
// 点卡片进规格弹层）。钉住八件事：
//   1) 左栏第一项即合成分类，右栏第一行是它的分组标题、第二行是卡片条；
//   2) 卡片数据：只取 signature=1、跳过无规格菜、跨分类重复只收一次、不限量；
//   3) 角标：招牌菜的份数在合成分类与真实分类「各自计一次」（有意重复）；
//   4) 加菜后菜品行（真实分类里的那份）与结算栏同步，招牌卡片条不受影响；
//   5) 点卡片直接开规格弹层（不跳转），售罄只提示不进车；
//   6) 卡片图加载失败只回写那一张，真实分类里的同名行自己走自己的 binderror；
//   7) 搜索态整组消失；
//   8) 没有招牌菜时合成分类整项不出现。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartClear, cartState } from '../../../store/cart';

/** 烧鹅同时归进「荤菜」与「凉菜」：招牌组只应收一次（与 catQtyMap 同口径）。 */
const MENU = [
  {
    categoryId: 1,
    categoryName: '荤菜',
    dishes: [
      {
        dishId: 5,
        dishName: '清蒸鱼',
        dishImage: '/uploads/fish.png',
        signature: 1,
        specs: [{ specId: 9, specName: '份', price: 68 }]
      },
      // 非招牌：不进招牌组
      { dishId: 6, dishName: '炒青菜', specs: [{ specId: 10, specName: '份', price: 18 }] },
      {
        dishId: 7,
        dishName: '烧鹅',
        dishImage: '/uploads/goose.png',
        signature: 1,
        specs: [
          { specId: 11, specName: '例', price: 88 },
          { specId: 12, specName: '半只', price: 48 }
        ]
      }
    ]
  },
  {
    categoryId: 2,
    categoryName: '凉菜',
    dishes: [
      { dishId: 7, dishName: '烧鹅', signature: 1, specs: [{ specId: 11, specName: '例', price: 88 }] },
      { dishId: 8, dishName: '拍黄瓜', specs: [{ specId: 91, specName: '份', price: 12 }] },
      // 人工售罄的招牌菜：照常展示，置灰不可点
      {
        dishId: 13,
        dishName: '限量牛排',
        signature: 1,
        soldOut: 1,
        specs: [{ specId: 131, specName: '份', price: 128 }]
      },
      // 规格库存卖完的招牌菜：同样算售罄
      {
        dishId: 14,
        dishName: '沽清靓汤',
        signature: 1,
        specs: [{ specId: 141, specName: '盅', price: 58, stock: 0 }]
      },
      // 没有规格的招牌菜：根本下不了单，不进招牌组
      { dishId: 15, dishName: '没规格的招牌', signature: 1, specs: [] }
    ]
  }
];

/** 一道招牌菜都没有的菜单：合成分类应整项不出现。 */
const MENU_NO_SIG = [
  {
    categoryId: 1,
    categoryName: '荤菜',
    dishes: [{ dishId: 5, dishName: '清蒸鱼', specs: [{ specId: 9, specName: '份', price: 68 }] }]
  }
];

const TABLE_OK = { tableId: 3, tableName: '大厅03桌', tableCode: 'A1', servingOrders: [] };

interface Card {
  dishId: number;
  dishName: string;
  image: string;
  priceText: string;
  manySpecs: boolean;
  soldOut: boolean;
}

interface Row {
  kind: string;
  key?: string;
  anchor?: string;
  dishId?: number;
  categoryId?: number;
  categoryName?: string;
  image?: string;
  cards?: Card[];
  cartQty?: number;
}

interface Cat {
  categoryId: number;
  categoryName: string;
  qty: number;
}

/** 菜单页 onLoad 会并发拉「公开配置 / 桌台 / 菜单 / 备注」，按这个顺序预置四个回包。 */
async function loadMenu(menu: unknown = MENU): Promise<PageInstance> {
  const page = await mountPage('index', () => import('../index'));
  wxMock.reply({ code: 200, msg: 'ok', data: {} }); // 公开配置
  wxMock.reply({ code: 200, msg: 'ok', data: TABLE_OK }); // 桌台
  wxMock.reply({ code: 200, msg: 'ok', data: menu }); // 菜单
  wxMock.reply({ code: 200, msg: 'ok', data: [] }); // 备注
  page.onLoad({ table: 'A1' });
  await flush();
  return page;
}

function rows(page: PageInstance): Row[] {
  return page.data.rows as Row[];
}

/** 卡片条那一行（整个分组收进一行，见 MenuRow 的注释）。 */
function strip(page: PageInstance): Row {
  const hit = rows(page).find(r => r.kind === 'sig');
  if (!hit) throw new Error('没有渲染出招牌卡片条');
  return hit;
}

function cats(page: PageInstance): Cat[] {
  return page.data.cats as Cat[];
}

function tap(dishId: number): never {
  return { currentTarget: { dataset: { id: String(dishId) } } } as never;
}

describe('菜单页 · 本店招牌分组', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    app.session.tableCode = '';
    app.session.tableId = 0;
    app.session.config = null;
    wxMock.reset();
    wxMock.storage.clear();
  });

  it('左栏第一项是合成分类，右栏第一行是分组标题、第二行是卡片条', async () => {
    const page = await loadMenu();

    expect(cats(page).map(c => c.categoryName)).toEqual(['本店招牌', '荤菜', '凉菜']);
    expect(cats(page)[0].categoryId).toBe(-1);
    // 初始高亮跟着第一项走：右栏顶部正是招牌分组
    expect(page.data.activeCatId).toBe(-1);
    expect(rows(page)[0]).toMatchObject({ kind: 'sec', categoryName: '本店招牌', categoryId: -1 });
    // 分组标题用固定锚点（点左栏第一项滚到这里，见 jumpToCat）
    expect(rows(page)[0].anchor).toBe('sig-sec');
    expect(rows(page)[1].kind).toBe('sig');
    expect(strip(page).key).toBe('sig-strip');
  });

  it('卡片只取 signature=1 且有规格的菜，跨分类重复只收一次，多规格给最低价 +「起」', async () => {
    const page = await loadMenu();

    const cards = strip(page).cards as Card[];
    // 烧鹅在两个分类里都标了招牌：组里只出现一次
    expect(cards.map(c => c.dishId)).toEqual([5, 7, 13, 14]);
    expect(cards[0].image).toContain('/uploads/fish.png');
    // 多规格给最低价 +「起」，单规格不标
    expect(cards[1].priceText).toBe('48.00');
    expect(cards[1].manySpecs).toBe(true);
    expect(cards[0].priceText).toBe('68.00');
    expect(cards[0].manySpecs).toBe(false);
    // 售罄照常展示（人工售罄 / 库存卖完都算），盖遮罩不可点
    expect(cards[2].soldOut).toBe(true);
    expect(cards[3].soldOut).toBe(true);
  });

  it('角标各自计一次：招牌组与真实分类都亮（有意重复）', async () => {
    const page = await loadMenu();

    page.onAdd(tap(5)); // 清蒸鱼：招牌菜，属于「荤菜」

    const byId = new Map(cats(page).map(c => [c.categoryId, c.qty]));
    expect(byId.get(-1)).toBe(1); // 本店招牌
    expect(byId.get(1)).toBe(1); // 它的真实分类
    expect(byId.get(2)).toBe(0); // 无关分类不亮
  });

  it('加菜后真实分类里的菜品行与结算栏同步，招牌卡片条不受影响', async () => {
    const page = await loadMenu();

    page.onAdd(tap(5));

    const dishRow = rows(page).find(r => r.kind === 'dish' && r.dishId === 5);
    expect(dishRow?.cartQty).toBe(1);
    expect(page.data.cartCount).toBe(1);
    // 卡片条不重排：加菜只动菜品行与角标
    expect((strip(page).cards as Card[])[0].dishId).toBe(5);
  });

  it('点卡片：直接开规格弹层（顾客已在桌上，不再引导扫码）', async () => {
    const page = await loadMenu();

    page.onOpenSig(tap(7));

    expect(page.data.sheetVisible).toBe(true);
    const dish = page.data.sheetDish as { dishName: string; specs: unknown[] };
    expect(dish.dishName).toBe('烧鹅');
    expect(dish.specs).toHaveLength(2);
    // 不开新页面：规格与备注都在弹层里选完
    expect(wxMock.navigations).toHaveLength(0);
  });

  it('弹层确定带上所选份数：一次加 3 份进购物车', async () => {
    const page = await loadMenu();

    page.onOpenSig(tap(5));
    page.onSheetConfirm({
      detail: { specId: 9, specName: '份', price: 68, quantity: 3 }
    } as never);

    expect(page.data.cartCount).toBe(3);
    const line = cartState().find(l => l.dishId === 5);
    expect(line?.quantity).toBe(3);
    // 坏值兜底 1 份：不让脏数据静默丢加购或爆量
    page.onOpenSig(tap(7));
    page.onSheetConfirm({ detail: { specId: 11, specName: '例', price: 88, quantity: 'x' } } as never);
    expect(page.data.cartCount).toBe(4);
  });

  it('点侧边栏「本店招牌」：滚回列表顶部（list-top 锚点），高亮已在自己上也照样滚', async () => {
    const page = await loadMenu();

    // 进页面高亮默认就是招牌（-1）：不绕过早退的话这次点击会完全失效
    expect(page.data.activeCatId).toBe(-1);
    page.onSwitchCat({ currentTarget: { dataset: { id: '-1' } } } as never);

    expect(page.data.scrollIntoId).toBe('list-top');
    expect(page.data.activeCatId).toBe(-1);
  });

  it('售罄招牌菜点不动：只提示，不进购物车', async () => {
    const page = await loadMenu();

    page.onOpenSig(tap(13));

    expect(page.data.sheetVisible).toBe(false);
    expect(wxMock.lastToast).toContain('售罄');
    expect(page.data.cartCount).toBe(0);
  });

  it('卡片图加载失败：只回写那一张，真实分类里的同名行自己走自己的 binderror', async () => {
    const page = await loadMenu();

    page.onSigImgError({ currentTarget: { dataset: { id: '5' } } } as never);

    const cards = strip(page).cards as Card[];
    expect(cards[0].image).toBe('');
    expect(cards[1].image).not.toBe('');
    // 菜品行不跟着置空：它的 binderror 自己触发、自己走 onImgError
    const dishRow = rows(page).find(r => r.kind === 'dish' && r.dishId === 5);
    expect(dishRow?.image).not.toBe('');
  });

  it('搜索态整组消失：只留真实分类里的命中', async () => {
    const page = await loadMenu();

    page.applyKeyword('鱼');

    expect(rows(page).some(r => r.kind === 'sig')).toBe(false);
    expect(rows(page).some(r => r.kind === 'sec' && r.categoryName === '本店招牌')).toBe(false);
    expect(rows(page).filter(r => r.kind === 'dish').map(r => r.dishId)).toEqual([5]);
  });

  it('没有招牌菜：合成分类整项不出现', async () => {
    const page = await loadMenu(MENU_NO_SIG);

    expect(cats(page).map(c => c.categoryName)).toEqual(['荤菜']);
    expect(page.data.activeCatId).toBe(1);
    expect(rows(page).some(r => r.kind === 'sig')).toBe(false);
  });
});
