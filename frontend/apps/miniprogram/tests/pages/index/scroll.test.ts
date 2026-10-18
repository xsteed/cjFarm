// 菜单页「左栏分类 ↔ 右栏滚动」联动的回归用例。
//
// 这块逻辑依赖节点测量（measureSections 用 createSelectorQuery 量分组标题偏移），
// 单测里量不到真实偏移，所以直接把 offsets 摆好、只验证「按偏移切换高亮」的判定本身：
//   ・标题顶到列表顶部 20px 内 → 高亮切过去
//   ・已滚到底 → 高亮最后一项（末尾分类内容不足一屏时标题永远顶不到顶部）
//   ・点分类期间锁定高亮，动画滚动产生的 scroll 事件不能把高亮抢回去
//   ・回归：末尾只有一道菜的分类，滚过时高亮必须逐一停留，
//     不能从上一类直接蹦到最后（旧逻辑只有「滚到底」特判，短分类整个被跳过）
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { cartClear } from '../../../src/store/cart';

async function mount(): Promise<PageInstance> {
  return mountPage('index', () => import('../../../src/pages/index/index'));
}

const MENU = [
  {
    categoryId: 1,
    categoryName: '凉菜',
    dishes: [{ dishId: 1, dishName: '凉拌青瓜', specs: [{ specId: 1, specName: '份', price: 18 }] }]
  },
  {
    categoryId: 2,
    categoryName: '荤菜',
    dishes: [{ dishId: 5, dishName: '清蒸鱼', specs: [{ specId: 9, specName: '份', price: 68 }] }]
  }
];

function replyLoad(): void {
  wxMock.reply({ code: 200, msg: 'ok', data: {} }); // 公开配置
  wxMock.reply({
    code: 200,
    msg: 'ok',
    data: { tableId: 3, tableName: '大厅03桌', tableCode: 'A1', servingOrders: [] }
  }); // 桌台
  wxMock.reply({ code: 200, msg: 'ok', data: MENU }); // 菜单
  wxMock.reply({ code: 200, msg: 'ok', data: [] }); // 备注
}

/** 摆好「两个分组标题的偏移」，等价于 measureSections 量完的结果。 */
function seedOffsets(page: PageInstance): void {
  page.offsets = [
    { categoryId: 1, top: 0 },
    { categoryId: 2, top: 300 }
  ];
  page.listHeight = 500;
}

/** 摆好一整份布局的偏移：一道菜行高约 106px、分组标题约 29px、列表底部内边距 96px，
 *  单菜分类 ≈ 135px（与 H5 useScrollSpy 单测同口径）。 */
function seedLayout(page: PageInstance, offsets: { categoryId: number; top: number }[]): void {
  page.offsets = offsets;
  page.listHeight = 700;
}

describe('菜单页 · 分类与滚动联动', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  it('滚动让下一个分组标题顶到顶部时，左栏高亮跟过去', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedOffsets(page);

    expect(page.data.activeCatId).toBe(1);
    page.onScroll({ detail: { scrollTop: 320, scrollHeight: 2000 } } as never);
    expect(page.data.activeCatId).toBe(2);

    page.onScroll({ detail: { scrollTop: 100, scrollHeight: 2000 } } as never);
    expect(page.data.activeCatId).toBe(1);
  });

  it('已滚到底时高亮最后一个分类（末尾内容不足一屏也不能停在上一类）', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedOffsets(page);

    // 只滚到 320（第 2 类标题刚过顶），但已到底：1500 + 500 >= 2000 - 2
    page.onScroll({ detail: { scrollTop: 1500, scrollHeight: 2000 } } as never);
    expect(page.data.activeCatId).toBe(2);
  });

  it('点分类后锁定高亮：动画滚动产生的 scroll 不会把高亮抢回去', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedOffsets(page);

    page.onSwitchCat({ currentTarget: { dataset: { id: '2' } } } as never);
    expect(page.data.activeCatId).toBe(2);

    // 锁定期间滚回顶部：若没有锁，高亮会被改回第 1 类，动画过程中会来回跳
    page.onScroll({ detail: { scrollTop: 0, scrollHeight: 2000 } } as never);
    expect(page.data.activeCatId).toBe(2);
  });

  it('已经在当前分类上再点一次：不重复触发滚动', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedOffsets(page);

    page.onSwitchCat({ currentTarget: { dataset: { id: '1' } } } as never);
    expect(page.data.activeCatId).toBe(1);
    // 没有锁，说明没走 jumpToCat
    expect(page.spyLock).toBe(false);
  });
});

describe('菜单页 · 末尾短分类不再被跳过', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  /** 第 1 类 20 道菜（一屏以上），末尾三类各只有一道菜：
   *  它们连同后面的内容不足一屏，标题再滚也顶不到顶部触发线。 */
  function seedTail(page: PageInstance): void {
    seedLayout(page, [
      { categoryId: 1, top: 0 },
      { categoryId: 2, top: 2149 },
      { categoryId: 3, top: 2284 },
      { categoryId: 4, top: 2419 }
    ]);
  }

  it('向下滚：每个短分类都被选中停留，不会从上一类直接蹦到最后', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedTail(page);

    const seen: number[] = [];
    for (const s of [0, 500, 1000, 1469, 1604, 1739, 1947]) {
      page.onScroll({ detail: { scrollTop: s, scrollHeight: 2650 } } as never);
      seen.push(page.data.activeCatId as number);
    }
    // 旧逻辑在这里只会是 1,1,1,1,1,1,1 —— 第 2/3/4 类整个被跳过
    expect(seen).toEqual([1, 1, 1, 2, 3, 4, 4]);
  });

  it('向上滚：同样逐类回落，不漏不跳', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedTail(page);

    page.onScroll({ detail: { scrollTop: 1947, scrollHeight: 2650 } } as never);
    expect(page.data.activeCatId).toBe(4);
    page.onScroll({ detail: { scrollTop: 1700, scrollHeight: 2650 } } as never);
    expect(page.data.activeCatId).toBe(3);
    page.onScroll({ detail: { scrollTop: 1500, scrollHeight: 2650 } } as never);
    expect(page.data.activeCatId).toBe(2);
    page.onScroll({ detail: { scrollTop: 1400, scrollHeight: 2650 } } as never);
    expect(page.data.activeCatId).toBe(1);
  });

  it('滚到底仍由特判收尾：高亮最后一个分类', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedTail(page);

    page.onScroll({ detail: { scrollTop: 1948, scrollHeight: 2650 } } as never);
    expect(page.data.activeCatId).toBe(4);
  });
});

describe('菜单页 · 整份菜单偏短', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  /** 共约 1.8 屏：第 1 类 6 道菜，末尾两类短（1 道 / 3 道）。
   *  第 2 类标题（665）落在首屏里，底部触发线会抢跑，改用视口中线。 */
  function seedShort(page: PageInstance): void {
    seedLayout(page, [
      { categoryId: 1, top: 0 },
      { categoryId: 2, top: 665 },
      { categoryId: 3, top: 800 }
    ]);
  }

  it('刚进页面不抢跑：停在第一个分类', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedShort(page);

    page.onScroll({ detail: { scrollTop: 0, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(1);
    page.onScroll({ detail: { scrollTop: 300, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(1);
  });

  it('标题滚到视口中线：短分类也能被选中并停留', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedShort(page);

    page.onScroll({ detail: { scrollTop: 315, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(2);
    page.onScroll({ detail: { scrollTop: 449, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(2);
    page.onScroll({ detail: { scrollTop: 450, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(3);
    page.onScroll({ detail: { scrollTop: 400, scrollHeight: 1243 } } as never);
    expect(page.data.activeCatId).toBe(2);
  });
});

describe('菜单页 · 标题起点在首屏上半区', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  /** 整份菜单几乎一屏（742px）：第 2 类标题（241）在首屏上半区，
   *  任何触发线都会在 scrollTop=0 时抢跑，只能继续靠「滚到底」特判。 */
  function seedTiny(page: PageInstance): void {
    seedLayout(page, [
      { categoryId: 1, top: 0 },
      { categoryId: 2, top: 241 },
      { categoryId: 3, top: 376 },
      { categoryId: 4, top: 511 }
    ]);
  }

  it('不进页面就误切：停在第一个分类，滚到底落在最后一类', async () => {
    const page = await mount();
    replyLoad();
    page.onLoad({ table: 'A1' });
    await flush();
    seedTiny(page);

    page.onScroll({ detail: { scrollTop: 0, scrollHeight: 742 } } as never);
    expect(page.data.activeCatId).toBe(1);
    page.onScroll({ detail: { scrollTop: 40, scrollHeight: 742 } } as never);
    expect(page.data.activeCatId).toBe(4);
  });
});
