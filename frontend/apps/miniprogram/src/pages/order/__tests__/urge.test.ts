// 订单页催菜的回归用例。
//
// 催菜有三个状态要互相挡住：请求中（urging）、冷却中（urgeCooldown）、以及后端拒绝时
// 回传的剩余秒数。任何一处挡不住，顾客就能连点刷后厨，或者把「已催过」的提示吞掉。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';
import { flush, wxMock } from '../../../../test/mock-wx';
import { URGE_COOLDOWN_FALLBACK_S } from '../../../config';

async function mount(): Promise<PageInstance> {
  return mountPage('order', () => import('../index'));
}

describe('订单页 · 催菜', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    wxMock.reset();
    page = null;
  });

  afterEach(() => {
    // 冷却倒计时是 setInterval：不清掉测试进程不会退出
    page?.clearUrgeTimer();
    page?.onUnload();
  });

  async function ready(): Promise<PageInstance> {
    const p = await mount();
    p.setData({ orderNo: 'D001' });
    return p;
  }

  it('催菜成功：提示并进入后端给的冷却', async () => {
    page = await ready();

    wxMock.reply({ code: 200, msg: 'ok', data: { cooldown: 90 } });
    await page.onUrge();
    await flush();

    expect(wxMock.toasts.map(t => t.title)).toContain('已通知后厨加急');
    expect(page.data.urgeCooldown).toBe(90);
    expect(page.data.urging).toBe(false);
  });

  it('后端没给冷却秒数时用兜底值', async () => {
    page = await ready();

    wxMock.reply({ code: 200, msg: 'ok', data: {} });
    await page.onUrge();
    await flush();

    expect(page.data.urgeCooldown).toBe(URGE_COOLDOWN_FALLBACK_S);
  });

  it('后端拒绝并回传剩余秒数：按后端值展示倒计时', async () => {
    page = await ready();

    wxMock.reply({ code: 400, msg: '操作过于频繁', data: { cooldown: 96 } });
    await page.onUrge();
    await flush();

    expect(page.data.urgeCooldown).toBe(96);
    expect(wxMock.lastToast).toBe('已催过啦，请等待 96 秒');
  });

  it('冷却中再点不发请求（防连点刷后厨）', async () => {
    page = await ready();
    page.setData({ urgeCooldown: 30 });

    await page.onUrge();
    await flush();

    expect(wxMock.requests).toHaveLength(0);
  });

  it('没有订单号时直接返回，不发请求', async () => {
    page = await mount();

    await page.onUrge();
    await flush();

    expect(wxMock.requests).toHaveLength(0);
  });
});
