// 菜单页「左栏分类 ↔ 右栏滚动」联动的回归用例。
//
// 这块逻辑依赖节点测量（measureSections 用 createSelectorQuery 量分组标题偏移），
// 单测里量不到真实偏移，所以直接把 offsets 摆好、只验证「按偏移切换高亮」的判定本身：
//   ・标题顶到列表顶部 20px 内 → 高亮切过去
//   ・已滚到底 → 高亮最后一项（末尾分类内容不足一屏时标题永远顶不到顶部）
//   ・点分类期间锁定高亮，动画滚动产生的 scroll 事件不能把高亮抢回去
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartClear } from '../../../store/cart';

async function mount(): Promise<PageInstance> {
  return mountPage('index', () => import('../index'));
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
