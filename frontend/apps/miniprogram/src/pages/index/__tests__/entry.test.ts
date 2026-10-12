// 菜单页「进入方式」与「购物车交互」的回归用例。
//
// 覆盖三处真踩过的坑：
//   1) 首屏加载失败后点「重新加载」无效 —— 桌台码只在成功路径写进会话，
//      重试时取不到，又变成「请扫码进入」，只能退出重进。
//   2) 从订单页点「加菜」回来（append=1）时，菜单页必须留在菜单，
//      否则会因本桌有进行中订单把顾客再踢回订单页，加菜永远点不成。
//   3) 快速「+」把顾客已经填好的「不要辣」清掉；菜品图加载失败后反复闪回破图。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { cartAdd, cartClear, cartState } from '../../../store/cart';
import { sessionState } from '../../../store/session';

async function mount(): Promise<PageInstance> {
  return mountPage('index', () => import('../index'));
}

const MENU = [
  {
    categoryId: 1,
    categoryName: '荤菜',
    dishes: [
      {
        dishId: 5,
        dishName: '清蒸鱼',
        dishImage: '/uploads/dish_5.png',
        specs: [{ specId: 9, specName: '份', price: 68 }]
      }
    ]
  }
];

type DishRow = { key: string; image: string; itemRemark?: string };

function rowOf(page: PageInstance, key: string): DishRow | undefined {
  return (page.data.rows as DishRow[]).find(r => r.key === key);
}

/** 菜单页 onLoad 会并发拉「公开配置 / 桌台 / 菜单 / 备注」，按这个顺序预置四个回包。 */
function replyMenuLoad(table: Record<string, unknown>, menu: unknown = MENU): void {
  wxMock.reply({ code: 200, msg: 'ok', data: {} }); // 公开配置
  wxMock.reply({ code: 200, msg: 'ok', data: table }); // 桌台
  wxMock.reply({ code: 200, msg: 'ok', data: menu }); // 菜单
  wxMock.reply({ code: 200, msg: 'ok', data: [] }); // 备注
}

const TABLE_OK = { tableId: 3, tableName: '大厅03桌', tableCode: 'A1', servingOrders: [] };

describe('菜单页 · 进入与重试', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    app.session.tableCode = '';
    app.session.tableId = 0;
    app.session.config = null;
    wxMock.reset();
    // 归属认领结果存在 storage 里：不清的话上一用例认领过的单会漏到下一用例
    wxMock.storage.clear();
  });

  it('首屏失败后「重新加载」用的是本次解析出的桌台码，而不是空会话', async () => {
    const page = await mount();
    wxMock.replyFail('request:fail');
    page.onLoad({ scene: 'A1' });
    await flush();

    expect(page.data.error).toBeTruthy();
    expect(page.data.error).not.toBe('请扫描餐桌上的二维码进入点餐');
    expect(page.loadedCode).toBe('A1');
    // applyTable 只走成功路径，所以此时会话里还没有桌台 —— 这正是以前重试失效的原因
    expect(sessionState().tableCode).toBe('');

    replyMenuLoad(TABLE_OK);
    page.onRetry();
    await flush();

    expect(page.data.error).toBe('');
    expect(page.data.tableLabel).toBe('大厅03桌');
  });

  it('带 append=1 进入时，本桌有进行中订单也留在菜单页继续点', async () => {
    const page = await mount();
    replyMenuLoad({
      ...TABLE_OK,
      tableId: 1,
      tableName: '大厅01桌',
      currentOrder: { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 2, items: [] }
    });
    page.onLoad({ table: 'A1', append: '1' });
    await flush();

    expect(wxMock.navigations).toHaveLength(0);
    expect(page.data.loading).toBe(false);
    expect(page.data.appendNotice).toContain('加菜');
    // 订单要写进会话，购物车页才能据此走「提交加菜」而不是新开单
    expect(sessionState().currentOrder?.orderNo).toBe('D001');
  });

  it('平时进入：本桌未结账订单先认领，确认是自己的才进订单页', async () => {
    const page = await mount();
    replyMenuLoad({
      ...TABLE_OK,
      tableId: 1,
      tableName: '大厅01桌',
      currentOrder: { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 2, items: [] }
    });
    page.onLoad({ table: 'A1' });
    await flush();

    // 店家忘了点「收款」时，这笔单可能就是上一桌留下的 —— 必须先问一句
    expect(wxMock.modals).toHaveLength(1);
    expect(wxMock.modals[0].title).toContain('未结账');
    expect(wxMock.navigations).toContainEqual({ type: 'redirectTo', url: '/pages/order/index' });
    expect(page.data.appendNotice).toBe('');
  });

  // 选「不是我的」：不能把上一桌的账单摊给新客人，也不该把他推进订单页
  it('未结账订单被认作「不是我的」时，停在提示页等服务员清台', async () => {
    wxMock.modalConfirm = false;
    const page = await mount();
    replyMenuLoad({
      ...TABLE_OK,
      tableId: 1,
      tableName: '大厅01桌',
      currentOrder: { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 2, items: [] }
    });
    page.onLoad({ table: 'A1' });
    await flush();

    expect(wxMock.navigations).toHaveLength(0);
    expect(page.data.error).toContain('服务员清台');
    // 会话里的订单线索也要清掉，否则购物车页会误判成加菜
    expect(sessionState().currentOrder).toBeNull();
  });
});

describe('菜单页 · 菜品行交互', () => {
  beforeEach(async () => {
    const app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    wxMock.reset();
  });

  it('快速「+」不会抹掉这道菜已填的单项备注', async () => {
    const page = await mount();
    replyMenuLoad(TABLE_OK);
    page.onLoad({ table: 'A1' });
    await flush();

    cartAdd({
      dishId: 5,
      dishName: '清蒸鱼',
      specId: 9,
      specName: '份',
      price: 68,
      image: '',
      itemRemark: '不要辣'
    });
    page.onAdd({ currentTarget: { dataset: { id: '5' } } } as never);

    expect(cartState()).toHaveLength(1);
    expect(cartState()[0].itemRemark).toBe('不要辣');
    expect(cartState()[0].quantity).toBe(2);
  });

  it('菜品图加载失败后换占位，重建列表时不会闪回破图', async () => {
    const page = await mount();
    replyMenuLoad(TABLE_OK);
    page.onLoad({ table: 'A1' });
    await flush();

    const key = 'dish-5';
    expect(rowOf(page, key)?.image).toContain('/uploads/');

    page.onImgError({ currentTarget: { dataset: { key } } } as never);
    expect(rowOf(page, key)?.image).toBe('');

    // onShow 会重建列表：失败过的图必须仍是空串，否则顾客看到占位/破图反复闪
    page.onShow();
    expect(rowOf(page, key)?.image).toBe('');
  });
});
