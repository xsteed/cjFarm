// 菜单页 ·「入座选人数」面板。
//
// 茶位费 = 人数 × 每位茶位费，人数错了茶位费就不准。这里的边界与设计文档
// （docs/design-docs/customer-person-count/spec.md）一一对应：
//   ・只在「茶位费开启 + 本桌无订单 + 本会话没选过」时弹；
//   ・菜单必须先渲染完成（面板只是蒙层 + 贴底面板，不能出现白板）；
//   ・不预选：未选时确认被拦，人数必须被显式选出来；
//   ・冷启动 60 分钟内的选择自动恢复，过期 / 换桌重新问。
import { beforeEach, describe, expect, it } from 'vitest';
import { bootApp, mountPage } from '../../../test/harness';
import type { PageInstance } from '../../../test/harness';
import { flush, wxMock } from '../../../test/mock-wx';
import { cartClear } from '../../../src/store/cart';
import { persistPersonChoice, sessionState } from '../../../src/store/session';

async function mount(): Promise<PageInstance> {
  return mountPage('index', () => import('../../../src/pages/index/index'));
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

const TABLE_OK = { tableId: 3, tableName: '大厅03桌', tableCode: 'A1', servingOrders: [] };

/** 按菜单页 onLoad 的并发顺序预置四个回包：公开配置 / 桌台 / 菜单 / 备注。 */
function replyMenuLoad(config: Record<string, unknown>, table: Record<string, unknown> = TABLE_OK): void {
  wxMock.reply({ code: 200, msg: 'ok', data: config });
  wxMock.reply({ code: 200, msg: 'ok', data: table });
  wxMock.reply({ code: 200, msg: 'ok', data: MENU });
  wxMock.reply({ code: 200, msg: 'ok', data: [] });
}

const SEAT_FEE_ON = { shop_name: '长健农场', seat_fee_enabled: '1', seat_fee: '2' };
const SEAT_FEE_OFF = { shop_name: '长健农场', seat_fee_enabled: '0', seat_fee: '2' };

/** 把「点 chip」包成一次触摸事件（dataset.n 携带所选人数）。 */
function pickOf(n: number): WechatMiniprogram.TouchEvent {
  return { currentTarget: { dataset: { n } } } as unknown as WechatMiniprogram.TouchEvent;
}

/** 把「点步进器」包成一次触摸事件（dataset.delta 为 ±1）。 */
function stepOf(delta: number): WechatMiniprogram.TouchEvent {
  return { currentTarget: { dataset: { delta } } } as unknown as WechatMiniprogram.TouchEvent;
}

describe('菜单页 · 入座选人数', () => {
  let page: PageInstance | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let app: any = null;

  beforeEach(async () => {
    app = await bootApp();
    cartClear();
    app.session.currentOrder = null;
    app.session.tableCode = '';
    app.session.tableId = 0;
    app.session.config = null;
    app.session.personCount = 1;
    app.session.personChosen = false;
    wxMock.reset();
    // 选择记录与归属认领都落 storage：不清的话上一用例的选择会漏到下一用例
    wxMock.storage.clear();
    page = await mount();
  });

  it('茶位费开启 + 全新桌：菜单渲染完成后盖面板，未选时确认被拦', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    // 面板出现，且菜单已经渲染（顾客扫码后看到的是「菜单 + 蒙层」，不是白板）
    expect(page?.data.needPerson).toBe(true);
    expect(page?.data.personDraft).toBe(0);
    expect((page?.data.rows as unknown[]).length).toBeGreaterThan(0);

    // 不预选：没选就点确认 → 只提示，不写人数、不收面板
    page?.onConfirmPerson();
    expect(page?.data.needPerson).toBe(true);
    expect(app.session.personCount).toBe(1);
    expect(app.session.personChosen).toBe(false);
    expect(wxMock.lastToast).toContain('1~20');
  });

  it('选 4 人确认：面板收起，人数写进会话并落盘（冷启动可恢复）', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    page?.onPickPerson(pickOf(4));
    expect(page?.data.personDraft).toBe(4);

    page?.onConfirmPerson();
    expect(page?.data.needPerson).toBe(false);
    expect(app.session.personCount).toBe(4);
    expect(app.session.personChosen).toBe(true);
    expect(wxMock.storage.get('dining_mp_person:A1')).toMatchObject({ count: 4 });
  });

  it('输入不是 1~20 的整数：只提示，不改草稿', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    for (const bad of ['0', '21', 'abc']) {
      wxMock.modalInput = bad;
      page?.onInputPerson();
      await flush();
      expect(page?.data.personDraft).toBe(0);
    }
    expect(wxMock.lastToast).toContain('1~20');
  });

  it('直接输入合法值：覆盖 11~20（快捷 chip 之外的桌况）', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    wxMock.modalInput = '12';
    page?.onInputPerson();
    await flush();
    expect(page?.data.personDraft).toBe(12);
  });

  it('步进器：未选时按「−」不产生选择；按「+」从 1 开始', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    page?.onPersonDraft(stepOf(-1));
    expect(page?.data.personDraft).toBe(0);
    page?.onPersonDraft(stepOf(1));
    expect(page?.data.personDraft).toBe(1);
    page?.onPersonDraft(stepOf(1));
    expect(page?.data.personDraft).toBe(2);
  });

  it('茶位费关闭的门店：不弹面板，行为与改造前一致', async () => {
    replyMenuLoad(SEAT_FEE_OFF);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(false);
    expect((page?.data.rows as unknown[]).length).toBeGreaterThan(0);
  });

  it('本桌已有未结账订单：不弹面板（人数以订单为准）', async () => {
    replyMenuLoad(SEAT_FEE_ON, { ...TABLE_OK, currentOrder: { orderNo: 'D9', orderStatus: 1, payStatus: 0 } });
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(false);
    // 有订单时走既有的归属确认 → 订单页流程，菜单页不该被面板打断
    expect(wxMock.navigations.some(n => n.url === '/pages/order/index')).toBe(true);
  });

  it('冷启动：60 分钟内的选择自动恢复，不再弹面板', async () => {
    // 先落一份选择记录（上一次进店选了 4 人）
    app.session.tableCode = 'A1';
    app.session.personCount = 4;
    persistPersonChoice();
    app.session.personCount = 1;
    app.session.personChosen = false;

    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(false);
    expect(app.session.personCount).toBe(4);
    expect(app.session.personChosen).toBe(true);
    expect(sessionState().personCount).toBe(4);
  });

  it('冷启动：超过 60 分钟的选择记录作废，重新弹面板', async () => {
    wxMock.storage.set('dining_mp_person:A1', { count: 4, at: Date.now() - 61 * 60 * 1000 });

    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(true);
    expect(app.session.personCount).toBe(1);
  });

  it('同桌已有人选过（桌台接口带 pendingPersonCount）：直接沿用，不再弹面板', async () => {
    // 另一部手机先选了 6 人并上报：本机扫码进来沿用桌级共享值
    replyMenuLoad(SEAT_FEE_ON, { ...TABLE_OK, pendingPersonCount: 6 });
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(false);
    expect(app.session.personCount).toBe(6);
    expect(app.session.personChosen).toBe(true);
  });

  it('确认人数后上报后端（全桌共享，供同桌其他设备沿用）', async () => {
    replyMenuLoad(SEAT_FEE_ON);
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    page?.onPickPerson(pickOf(4));
    page?.onConfirmPerson();
    await flush();

    const report = wxMock.requests.find(r => r.url.includes('/customer/table/person'));
    expect(report).toBeTruthy();
    expect(report?.data).toMatchObject({ tableId: 3, personCount: 4 });
  });

  it('本桌有已付款出餐中的订单（茶位费已收过）：不弹面板，人数沿最近一笔', async () => {
    replyMenuLoad(SEAT_FEE_ON, {
      ...TABLE_OK,
      servingOrders: [{ orderNo: 'D9', shortNo: '009', personCount: 5, orderStatus: 2 }]
    });
    page?.onLoad({ scene: 'A1' });
    await flush();
    await flush();

    expect(page?.data.needPerson).toBe(false);
    expect(app.session.personCount).toBe(5);
    expect(app.session.personChosen).toBe(true);
  });
});
