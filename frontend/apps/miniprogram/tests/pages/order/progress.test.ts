// 订单页「出餐进度」与「支付凭证」的回归用例。
//
// 这两处都是给顾客看「到哪一步了 / 我确实付过了」，最容易写错的是状态映射：
// 后厨状态是 1已下单/2制作中/3已上齐/4已完成/5已取消，进度条只映射前三个真实节点，
// 已完成的单要三步全亮且不再标「当前」，取消单整块不渲染。
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { STORAGE_KEY } from '../../../src/config';

interface StepRow {
  label: string;
  on: boolean;
  barOn: boolean;
  now: boolean;
}

/** 取出页面当前渲染的进度步骤（data 是宽松类型，这里收窄一下）。 */
function steps(p: PageInstance | null): StepRow[] {
  return (p?.data.steps ?? []) as StepRow[];
}

function order(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    orderNo: 'D001',
    shortNo: '001',
    orderStatus: 1,
    payStatus: 0,
    personCount: 2,
    dishAmount: 28,
    seatFee: 0,
    discountAmount: 0,
    totalAmount: 28,
    items: [],
    ...overrides
  };
}

describe('订单页 · 出餐进度与支付凭证', () => {
  let page: PageInstance | null = null;

  beforeEach(async () => {
    const app = await bootApp();
    app.session.tableId = 7;
    wxMock.reset();
    // onLoad 会用本地记下的订单号回拉一次，先备一个回包免得它走错误分支
    wxMock.storage.set(STORAGE_KEY, { tableCode: 'A1', orderNo: 'D001' });
    wxMock.reply({ code: 200, msg: 'ok', data: order() });
    page = await mountPage('order', () => import('../../../src/pages/order/index'));
    await flush();
    page.onUnload();
  });

  afterEach(() => {
    page?.onUnload();
    page = null;
  });

  it.each([
    [1, [true, false, false]],
    [2, [true, true, false]],
    [3, [true, true, true]],
    [4, [true, true, true]]
  ])('状态 %i 时进度点亮 %j', (status, lit) => {
    page?.render(order({ orderStatus: status }));
    expect(steps(page).map(s => s.on)).toEqual(lit);
    expect(page?.data.showProgress).toBe(true);
  });

  it('当前节点只标一个；已完成的单不再标「当前」', () => {
    page?.render(order({ orderStatus: 2 }));
    expect(steps(page).filter(s => s.now)).toHaveLength(1);

    page?.render(order({ orderStatus: 4 }));
    expect(steps(page).filter(s => s.now)).toHaveLength(0);
  });

  it('连接线跟随下一步：只有走到的段落才点亮', () => {
    page?.render(order({ orderStatus: 2 }));
    expect(steps(page).map(s => s.barOn)).toEqual([true, false, false]);
  });

  it('取消单不渲染进度；没有原因时给标准说明（未支付不产生费用）', () => {
    page?.render(order({ orderStatus: 5 }));
    expect(page?.data.showProgress).toBe(false);
    expect(page?.data.cancelTip).toBe('订单已取消，本单未支付、不会产生任何费用');

    // 有真实原因就带上（后端已把「商家取消」这类无信息标签抹平，这里只负责拼句）
    page?.render(order({ orderStatus: 5, cancelReason: '菜品售罄' }));
    expect(page?.data.cancelTip).toBe('订单已取消：菜品售罄');
  });

  it('已支付时给出支付凭证（渠道 + 结账时间），未支付时为空', () => {
    page?.render(order({ payStatus: 0 }));
    expect(page?.data.paidInfo).toBe('');

    page?.render(order({ payStatus: 1, payType: '微信', payTime: '2026-10-09 12:03:07' }));
    expect(page?.data.paidInfo).toBe('已支付（微信） · 2026-10-09 12:03');
  });

  it('状态头：大字说清走到哪一步，主题色与结束态说明跟着状态走', () => {
    page?.render(order({ orderStatus: 1 }));
    expect(page?.data.heroTitle).toBe('已下单，等待接单');
    expect(page?.data.heroClass).toBe('card sec hero hero--primary');
    // fixture 没给 createTime 就不编造「已等 X 分钟」
    expect(page?.data.heroSub).toBe('订单已提交');

    page?.render(order({ orderStatus: 2 }));
    expect(page?.data.heroTitle).toBe('后厨正在制作');
    expect(page?.data.heroClass).toBe('card sec hero hero--warning');

    page?.render(order({ orderStatus: 4 }));
    expect(page?.data.heroTitle).toBe('本单已完成');
    expect(page?.data.heroClass).toBe('card sec hero hero--success');
    expect(page?.data.heroTip).toBe('🎉 这顿吃完啦，欢迎下次光临');

    page?.render(order({ orderStatus: 5, cancelReason: '菜品售罄' }));
    expect(page?.data.heroTitle).toBe('订单已取消');
    expect(page?.data.heroClass).toBe('card sec hero hero--danger');
    expect(page?.data.heroSub).toBe('本单不会再出餐');
    expect(page?.data.heroTip).toBe('订单已取消：菜品售罄');
  });

  it('状态头：先付未付款说「等待付款」且不画进度（后厨还没收到单）', async () => {
    const app = await bootApp();
    app.session.config = { shop_name: '长健农场', pay_first_enabled: '1' };

    page?.render(order({ payStatus: 0 }));
    expect(page?.data.heroTitle).toBe('等待付款');
    expect(page?.data.heroSub).toBe('付款后后厨即刻开工');
    expect(page?.data.showProgress).toBe(false);

    // 付完款回到正常出餐口径
    page?.render(order({ orderStatus: 2, payStatus: 1 }));
    expect(page?.data.heroTitle).toBe('后厨正在制作');
    expect(page?.data.showProgress).toBe(true);
  });

  it('状态头：已等时长只来自订单自身 createTime，跨日遗留单不报无意义时长', () => {
    // 远超 24h 的 createTime 归到「订单已提交」,不说「已等 3000 分钟」
    page?.render(order({ createTime: '2020-01-01 12:00:00' }));
    expect(page?.data.heroSub).toBe('订单已提交');

    // 未来时间（时钟漂移）同样兜底
    page?.render(order({ createTime: '2099-01-01 12:00:00' }));
    expect(page?.data.heroSub).toBe('订单已提交');
  });
});
