// 购物车持久化的单测。
//
// 重点不在「能存能读」，而在几条**隔离与复核**规则上 —— 它们是「顾客看到别人剩下的车」
// 或「按旧价下单」的唯一防线：
//   ・桌台码 / 订单号变了就读不到（换桌、换一批客人）；
//   ・超时丢弃（有单 12h，未开单 60min）；
//   ・恢复时按当前菜单复核：下架 / 售罄 / 规格没了整行丢，价格取菜单现值。
//
// 另外覆盖一个与 H5 的刻意差异：小程序**不**按「会话起点」判断，冷启动必须能恢复
// （H5 那条判断依赖 sessionStorage，小程序没有，照抄会导致永远恢复不了）。
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cartAdd, cartClear, cartState } from '../cart';
import { restoreCart, saveCart } from '../cartPersist';
import { setPersonCount } from '../session';
import { bootApp, resetApp } from '../../../test/harness';
import { wxMock } from '../../../test/mock-wx';
import type { GlobalData } from '../../types';
import type { MenuCategory } from '@dining/types';

interface MenuDish {
  dishId: number;
  specId: number;
  price: number;
  stock?: number;
  soldOut?: number;
}

/** 造一份菜单：每道菜一个规格，够用于复核。 */
function menuOf(dishes: MenuDish[]): MenuCategory[] {
  return [
    {
      categoryId: 1,
      categoryName: '热菜',
      dishes: dishes.map(d => ({
        dishId: d.dishId,
        dishName: `菜${d.dishId}`,
        dishImage: `/uploads/${d.dishId}.png`,
        soldOut: d.soldOut ?? 0,
        specs: [
          {
            specId: d.specId,
            dishId: d.dishId,
            specName: `规格${d.specId}`,
            price: d.price,
            stock: d.stock ?? -1
          }
        ]
      }))
    }
  ];
}

/** 摆好桌台会话（不经过网络），并清空内存车与本地存储，等价一次干净的冷启动。 */
async function bootAt(tableCode: string, orderNo = ''): Promise<GlobalData> {
  const app = await bootApp();
  app.session.tableCode = tableCode;
  app.session.tableId = 7;
  app.session.currentOrder = orderNo ? ({ orderNo } as GlobalData['session']['currentOrder']) : null;
  app.session.personCount = 1;
  app.session.orderRemark = '';
  app.cart.length = 0;
  wxMock.storage.clear();
  return app;
}

function addDish(dishId: number, specId: number, price: number, qty = 1): void {
  cartAdd({ dishId, dishName: `菜${dishId}`, specId, specName: `规格${specId}`, price, image: '' });
  for (let i = 1; i < qty; i++) cartAdd({ dishId, dishName: `菜${dishId}`, specId, specName: `规格${specId}`, price, image: '' });
}

describe('cartPersist', () => {
  beforeEach(async () => {
    vi.restoreAllMocks();
    await bootApp();
    resetApp();
    wxMock.storage.clear();
  });

  it('加菜即落盘；冷启动（内存清零）后按菜单恢复，人数与整单备注一起回来', async () => {
    const app = await bootAt('TBL00001');
    setPersonCount(4);
    app.session.orderRemark = '少放辣';
    addDish(101, 1, 58, 2);
    addDish(102, 5, 12);

    // 模拟被杀进程：内存车清零，会话里的桌台仍在（session 由本地缓存恢复）
    app.cart.length = 0;

    expect(restoreCart(menuOf([
      { dishId: 101, specId: 1, price: 58 },
      { dishId: 102, specId: 5, price: 12 }
    ]))).toBe(true);

    expect(cartState()).toHaveLength(2);
    expect(cartState()[0]).toMatchObject({ dishId: 101, quantity: 2, image: '/uploads/101.png' });
    expect(app.session.personCount).toBe(4);
    // 恢复出的人数本身就是一次「已选」证据：菜单页不会再弹「选人数」面板
    expect(app.session.personChosen).toBe(true);
    expect(app.session.orderRemark).toBe('少放辣');
  });

  it('价格以菜单现值为准，不沿用落盘时的旧价', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    app.cart.length = 0;

    restoreCart(menuOf([{ dishId: 101, specId: 1, price: 66 }]));
    expect(cartState()[0].price).toBe(66);
  });

  it('下架 / 菜品售罄 / 规格售罄 / 规格不存在 → 整行丢弃，其余照常恢复', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58); // 正常
    addDish(102, 1, 20); // 菜品人工售罄
    addDish(103, 1, 30); // 规格库存 0
    addDish(104, 9, 40); // 规格已不存在
    addDish(105, 1, 50); // 已下架（菜单里没有）
    app.cart.length = 0;

    restoreCart(menuOf([
      { dishId: 101, specId: 1, price: 58 },
      { dishId: 102, specId: 1, price: 20, soldOut: 1 },
      { dishId: 103, specId: 1, price: 30, stock: 0 },
      { dishId: 104, specId: 1, price: 40 }
    ]));

    expect(cartState().map(l => l.dishId)).toEqual([101]);
  });

  it('复核后一行不剩：清掉存储，不让坏数据一直留着', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    const key = [...wxMock.storage.keys()][0];
    app.cart.length = 0;

    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58, soldOut: 1 }]))).toBe(false);
    expect(cartState()).toHaveLength(0);
    expect(wxMock.storage.has(key)).toBe(false);
  });

  it('订单号变了（结账 / 清台后换了单）→ 旧车读不到', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    app.cart.length = 0;
    // 同一张桌，但订单号变了：key 不同，直接读不到
    app.session.currentOrder = { orderNo: 'D20261018002' } as GlobalData['session']['currentOrder'];

    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58 }]))).toBe(false);
    expect(cartState()).toHaveLength(0);
  });

  it('换桌台 → 读不到（桌台码是 key 的一部分）', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    app.cart.length = 0;
    app.session.tableCode = 'TBL00002';

    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58 }]))).toBe(false);
  });

  it('未开单的车超过 60 分钟 → 丢弃', async () => {
    const t0 = 1_700_000_000_000;
    const now = vi.spyOn(Date, 'now').mockReturnValue(t0);
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    app.cart.length = 0;

    now.mockReturnValue(t0 + 61 * 60 * 1000);
    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58 }]))).toBe(false);
  });

  it('有单的车可以跨餐保存（12 小时内仍恢复）', async () => {
    const t0 = 1_700_000_000_000;
    const now = vi.spyOn(Date, 'now').mockReturnValue(t0);
    const app = await bootAt('TBL00001', 'D20261018001');
    addDish(101, 1, 58);
    app.cart.length = 0;

    now.mockReturnValue(t0 + 11 * 60 * 60 * 1000);
    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58 }]))).toBe(true);
    expect(cartState()).toHaveLength(1);
  });

  it('清空购物车会一并删掉存储（下单成功后走的是同一条路径）', async () => {
    await bootAt('TBL00001');
    addDish(101, 1, 58);
    expect(wxMock.storage.size).toBe(1);
    cartClear();
    expect(wxMock.storage.size).toBe(0);
  });

  it('还没识别到桌台时不落盘（避免所有会话共用同一个 key 互相污染）', async () => {
    const app = await bootApp();
    resetApp(); // tableCode ''
    app.cart.length = 0;
    wxMock.storage.clear();
    addDish(101, 1, 58);
    expect(wxMock.storage.size).toBe(0);
  });

  it('内存里已有车时不覆盖（顾客正在点，renderMenu 可能被重复调用）', async () => {
    const app = await bootAt('TBL00001');
    addDish(101, 1, 58);
    addDish(102, 1, 20); // 落盘的是两道菜
    app.cart.length = 0;

    // 先恢复一次，再手工加一道别的
    restoreCart(menuOf([
      { dishId: 101, specId: 1, price: 58 },
      { dishId: 102, specId: 1, price: 20 }
    ]));
    addDish(103, 1, 30);

    // 再次进菜单页：不能把刚加的 103 抹掉
    restoreCart(menuOf([
      { dishId: 101, specId: 1, price: 58 },
      { dishId: 102, specId: 1, price: 20 }
    ]));
    expect(cartState().map(l => l.dishId)).toEqual([101, 102, 103]);
  });

  it('坏数据（不是对象 / items 不是数组）直接清掉，不抛错', async () => {
    const app = await bootAt('TBL00001');
    wxMock.storage.set('dining_mp_cart:TBL00001:new', 'not-an-object');
    app.cart.length = 0;

    expect(restoreCart(menuOf([{ dishId: 101, specId: 1, price: 58 }]))).toBe(false);
    expect(wxMock.storage.size).toBe(0);
  });

  it('saveCart 可在无车时安全调用（删 key 而不是写空数组）', async () => {
    await bootAt('TBL00001');
    saveCart();
    expect(wxMock.storage.size).toBe(0);
  });
});
