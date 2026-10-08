// 订单页「加菜跳转」与「加载失败重试」的回归用例。
//
// 这两处都真踩过坑：
//   1) 扫码进入时菜单页被 redirectTo 顶掉了，页面栈里只剩订单页，navigateBack 必然失败；
//      兜底又跳回菜单页，而菜单页见到本桌有进行中订单会把顾客踢回订单页 —— 死循环，
//      表现就是「点加菜闪一下又回到订单页」，永远加不了菜。
//   2) 首屏加载失败只给一个「去点菜」，把顾客赶回菜单，可订单其实还在服务端。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage, setCurrentPages } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { STORAGE_KEY } from '../../../config';

async function mount(): Promise<PageInstance> {
  return mountPage('order', () => import('../index'));
}

/** 预置本地记下的订单号，模拟「中途退出后再进来」。 */
function persistOrder(orderNo = 'D001'): void {
  wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo });
}

describe('订单页 · 加菜跳转', () => {
  beforeEach(async () => {
    await bootApp();
    wxMock.reset();
    setCurrentPages([]);
  });

  it('菜单页还在栈里时直接返回（保留它的分类与滚动位置）', async () => {
    setCurrentPages([{ route: 'pages/index/index' }, { route: 'pages/order/index' }]);
    const page = await mount();

    page.onAppend();

    // goToMenu() 用 lastIndexOf 定位：栈里倒数第二个就是菜单页，delta = 1
    expect(wxMock.navigations).toEqual([{ type: 'navigateBack', delta: 1 }]);
  });

  it('扫码进入（栈里只有订单页）时重开菜单页并带 append 标记', async () => {
    setCurrentPages([{ route: 'pages/order/index' }]);
    const page = await mount();

    page.onAppend();

    // 关键在 append=1：否则菜单页会因为本桌有进行中订单再次 redirectTo 回订单页
    expect(wxMock.navigations).toEqual([
      { type: 'redirectTo', url: '/pages/index/index?append=1' }
    ]);
  });
});

describe('订单页 · 加载失败重试', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    await bootApp();
    wxMock.reset();
    setCurrentPages([]);
    page = null;
  });

  afterEach(() => {
    // 成功的 recover 会启动轮询，测试结束前必须停掉，否则定时器一直挂着
    page?.onUnload();
  });

  it('网络失败：留在本页可原地重试，重试成功后清掉错误', async () => {
    persistOrder('D001');
    page = await mount();

    wxMock.replyFail('request:fail');
    await page.recover();
    expect(page.data.error).toBeTruthy();
    expect(page.data.canRetry).toBe(true);

    wxMock.reply({
      code: 200,
      msg: 'ok',
      data: { orderNo: 'D001', orderStatus: 1, payStatus: 0, personCount: 1, items: [] }
    });
    page.onRetry();
    await flush();

    expect(page.data.error).toBe('');
    expect(page.data.canRetry).toBe(false);
    expect(page.data.orderNo).toBe('D001');
  });

  it('本地没有订单号：不提供重试（再点多少次也没用）', async () => {
    page = await mount();

    await page.recover();

    expect(page.data.error).toBe('没有找到进行中的订单');
    expect(page.data.canRetry).toBe(false);
  });
});
