// 按单号查订单 · 归属校验回归。
//
// GET /customer/order/no/:orderNo 是**无鉴权**的公开接口，后端靠调用方带上的
// tableId 判断「这单是不是这张桌的」（orders/sn 单号会出现在小票、照片、群聊里）。
// 前端不传这个参数，那道防线就等于没建 ——任何人猜到一个单号就能看到别人的
// 桌台、菜品、金额与备注，后端的 IP 限流只是第二道兜底。
//
// 这里锁住两件事：
//   1) 会话已识别出桌台 → 必须带上 tableId；
//   2) 桌台信息还没到（tableId === 0） → 不带，让后端退化成不限桌查询，
//      否则顾客会在「刚进来还在加载」的窗口里看到「订单不存在」。
//
// 脚手架只造实例、不跑生命周期，onLoad 由用例手动触发（与 serving-list.test 一致）。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { STORAGE_KEY } from '../../../src/config';

function replyOrder(): unknown {
  return {
    code: 200,
    msg: 'ok',
    data: {
      orderNo: 'D001',
      shortNo: '001',
      tableId: 7,
      orderStatus: 2,
      payStatus: 0,
      personCount: 2,
      dishAmount: 30,
      seatFee: 0,
      discountAmount: 0,
      totalAmount: 30,
      items: []
    }
  };
}

/** 请求里那条「按单号查订单」的记录。 */
function orderRequest(): { url: string } | undefined {
  return wxMock.requests.find(r => r.url.includes('/customer/order/no/D001'));
}

async function bootOrderPage(tableId: number): Promise<PageInstance> {
  const app = await bootApp();
  app.session.tableId = tableId;
  app.session.table = null;
  app.session.currentOrder = null;
  wxMock.reset();
  wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo: 'D001' });
  wxMock.reply(replyOrder());
  const page = await mountPage('order', () => import('../../../src/pages/order/index'));
  page.onLoad({});
  await flush();
  return page;
}

describe('订单查询 · tableId 归属校验', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    page = await bootOrderPage(7);
  });

  afterEach(() => {
    page?.onUnload();
    page = null;
  });

  it('已识别桌台：请求必须带 tableId', () => {
    const req = orderRequest();
    expect(req).toBeDefined();
    expect(req?.url).toContain('tableId=7');
  });

  it('桌台还没加载出来（tableId=0）：不带 tableId，回退为不限桌查询', async () => {
    page?.onUnload();
    page = await bootOrderPage(0);
    const req = orderRequest();
    expect(req).toBeDefined();
    expect(req?.url).not.toContain('tableId');
  });
});
