// 默认主页的回归用例。
//
// 首页承担的是一个以前不存在的入口，三条硬要求：
//   1) 扫码是主路径：扫到桌台码必须带着它进菜单（不能改成 redirectTo 把首页顶掉）；
//   2) 扫到无关内容 / 顾客取消要能区分：前者提示重扫，后者一声不响；
//   3) 续用入口只认真实存在的桌台与订单 —— 没扫过码的顾客不该看到一个空的「继续点餐」。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { persistSession, sessionState } from '../../../src/store/session';

async function mount(): Promise<PageInstance> {
  return mountPage('home', () => import('../../../src/pages/home/index'));
}

const MENU = [
  {
    categoryId: 1,
    categoryName: '招牌',
    dishes: [
      {
        dishId: 5,
        dishName: '清蒸鱼',
        dishImage: '/uploads/fish.png',
        signature: 1,
        specs: [{ specId: 9, specName: '份', price: 68 }]
      },
      // 非招牌：不进橱窗
      { dishId: 6, dishName: '炒青菜', specs: [{ specId: 10, specName: '份', price: 18 }] },
      {
        dishId: 7,
        dishName: '烧鹅',
        signature: 1,
        specs: [
          { specId: 11, specName: '例', price: 88 },
          { specId: 12, specName: '半只', price: 48 }
        ]
      }
    ]
  }
];

/** 首页 onLoad 会并发拉「公开配置 / 菜单」，有上次桌台时 onShow 再补一个桌台详情。 */
function replyHomeLoad(config: Record<string, unknown> = {}, table?: Record<string, unknown>): void {
  wxMock.reply({ code: 200, msg: 'ok', data: config });
  wxMock.reply({ code: 200, msg: 'ok', data: MENU });
  if (table) wxMock.reply({ code: 200, msg: 'ok', data: table });
}

describe('默认主页', () => {
  beforeEach(async () => {
    const app = await bootApp();
    app.session.currentOrder = null;
    app.session.tableCode = '';
    app.session.tableId = 0;
    app.session.config = null;
    wxMock.reset();
    wxMock.storage.clear();
  });

  it('店名/标语渲染到头部，招牌菜只取 signature=1 的菜', async () => {
    const page = await mount();
    replyHomeLoad({ shop_name: '小馆', shop_slogan: '现炒现做' });
    page.onLoad({});
    page.onShow();
    await flush();

    expect(page.data.shopName).toBe('小馆');
    expect(page.data.slogan).toBe('现炒现做');
    // 导航栏标题也换成店名，避免顶栏一直写着「扫码点餐」
    expect(wxMock.navTitle).toBe('小馆');

    const dishes = page.data.sigDishes as { dishId: number; image: string; priceText: string; manySpecs: boolean }[];
    expect(dishes.map(d => d.dishId)).toEqual([5, 7]);
    expect(dishes[0].image).toContain('/uploads/fish.png');
    // 多规格给最低价 +「起」
    expect(dishes[1].priceText).toBe('48.00');
    expect(dishes[1].manySpecs).toBe(true);
  });

  it('没扫过码：不显示续用卡片（空入口只会误导）', async () => {
    const page = await mount();
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    expect(page.data.tableCode).toBe('');
    expect(page.data.hasTable).toBe(false);
    expect(page.data.tableLabel).toBe('');
    expect(page.data.hasOrder).toBe(false);
  });

  it('有上次的桌台：确认存在后回填桌台名，「去点餐」带上桌台码进菜单', async () => {
    persistSession('A1', '');
    const page = await mount();
    replyHomeLoad({}, { tableId: 3, tableName: '大厅03桌', tableCode: 'A1' });
    page.onLoad({});
    page.onShow();
    await flush();

    // 桌台详情确认存在，「继续点餐」才有资格显示
    expect(page.data.hasTable).toBe(true);
    expect(page.data.tableCode).toBe('A1');
    expect(page.data.tableLabel).toBe('大厅03桌');

    page.onResumeMenu();
    // navigateTo：点完菜还能退回首页，首页不能从页面栈里消失
    expect(wxMock.navigations).toEqual([{ type: 'navigateTo', url: '/pages/index/index?tableCode=A1' }]);
  });

  it('桌台详情拉不到（已删 / 网络失败）：不显示「继续点餐」，也不给编造的通用文案', async () => {
    persistSession('A1', '');
    const page = await mount();
    // 只有配置与菜单两个回包：桌台详情那次请求会拿到默认失败回包（code 200 + data null → 走 catch）
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    expect(page.data.hasTable).toBe(false);
    expect(page.data.tableLabel).toBe('');
    page.onResumeMenu();
    expect(wxMock.navigations).toHaveLength(0);
  });

  it('会话里有进行中的订单：首屏给出查看入口', async () => {
    const page = await mount();
    sessionState().currentOrder = {
      orderNo: 'D001',
      shortNo: '12',
      orderStatus: 2,
      payStatus: 0,
      totalAmount: 88,
      items: []
    } as never;
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    expect(page.data.hasOrder).toBe(true);
    expect(page.data.orderShort).toBe('12');
    expect(page.data.orderStatusText).toBe('制作中');
    expect(page.data.orderAmountText).toBe('88.00');

    page.onResumeOrder();
    expect(wxMock.navigations).toEqual([{ type: 'navigateTo', url: '/pages/order/index' }]);
  });

  it('已结束的订单不进「进行中」卡片（订单页会把完成的单写进会话）', async () => {
    const page = await mount();
    sessionState().currentOrder = {
      orderNo: 'D002',
      shortNo: '13',
      orderStatus: 4,
      payStatus: 1,
      totalAmount: 66,
      items: []
    } as never;
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    expect(page.data.hasOrder).toBe(false);
  });

  it('扫码成功：解析出桌台码并带参数进菜单', async () => {
    const page = await mount();
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    wxMock.scanCodeResult = 'https://dining.test/order/8YG3Q335';
    await page.onScan();

    expect(wxMock.navigations).toEqual([{ type: 'navigateTo', url: '/pages/index/index?tableCode=8YG3Q335' }]);
  });

  it('扫到无关内容：提示重扫，不跳转', async () => {
    const page = await mount();
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    wxMock.scanCodeResult = 'https://www.example.com/about';
    await page.onScan();

    expect(wxMock.navigations).toHaveLength(0);
    expect(wxMock.lastToast).toContain('桌台码');
  });

  it('顾客取消扫码：不跳转也不弹提示', async () => {
    const page = await mount();
    replyHomeLoad({});
    page.onLoad({});
    page.onShow();
    await flush();

    wxMock.scanCodeFail = 'cancel';
    await page.onScan();

    expect(wxMock.navigations).toHaveLength(0);
    expect(wxMock.toasts).toHaveLength(0);
  });
});
