// 主页面跳转（点餐 ↔ 订单）的单测。
//
// 底部导航是小程序端常驻的，顾客会在两个 tab 之间反复切，所以这段逻辑有两条硬要求：
//   ・别把页面栈越切越深（小程序上限 10 层，超了跳转直接失败）
//   ・别跳错页（点餐页在「本桌已有进行中订单」时会把顾客弹回订单页，必须带 append=1）
// 三条分支（已在目标页 / 目标页在栈里 / 不在栈里）都钉一下。
import { beforeEach, describe, expect, it } from 'vitest';
import { goToHome, goToMenu, goToOrder } from '../nav';
import { wxMock } from '../../../test/mock-wx';

/** 造一个页面栈：数组头部是首页，尾部是当前页。 */
function setStack(...routes: string[]): void {
  (globalThis as Record<string, unknown>).getCurrentPages = () =>
    routes.map(route => ({ route }));
}

beforeEach(() => {
  setStack('pages/index/index');
});

describe('goToHome', () => {
  it('已在首页时什么都不做', () => {
    setStack('pages/home/index');
    goToHome();
    expect(wxMock.navigations).toHaveLength(0);
  });

  it('首页在栈里时返回它（扫码直达菜单后再切首页）', () => {
    setStack('pages/home/index', 'pages/index/index');
    goToHome();
    expect(wxMock.navigations).toEqual([{ type: 'navigateBack', delta: 1 }]);
  });

  it('首页不在栈里时用 redirectTo 打开', () => {
    setStack('pages/index/index');
    goToHome();
    expect(wxMock.navigations).toEqual([{ type: 'redirectTo', url: '/pages/home/index' }]);
  });
});

describe('goToMenu', () => {
  it('已在菜单页时什么都不做（不会把页面栈越切越深）', () => {
    goToMenu();
    expect(wxMock.navigations).toHaveLength(0);
  });

  it('菜单页在栈里时返回它，delta 指向菜单页', () => {
    setStack('pages/index/index', 'pages/order/index');
    goToMenu();
    expect(wxMock.navigations).toEqual([{ type: 'navigateBack', delta: 1 }]);
  });

  it('菜单页不在栈里时用 redirectTo 打开，并带 append=1', () => {
    setStack('pages/order/index');
    goToMenu();
    // append=1 很关键：不带的话菜单页会因「本桌已有进行中订单」把顾客又弹回订单页
    expect(wxMock.navigations).toEqual([{ type: 'redirectTo', url: '/pages/index/index?append=1' }]);
  });

  it('中间还夹着别的页面时，一次退到菜单页（购物车下单后的栈形态）', () => {
    setStack('pages/index/index', 'pages/pay/index', 'pages/order/index');
    goToMenu();
    expect(wxMock.navigations).toEqual([{ type: 'navigateBack', delta: 2 }]);
  });
});

describe('goToOrder', () => {
  it('已在订单页时什么都不做', () => {
    setStack('pages/order/index');
    goToOrder();
    expect(wxMock.navigations).toHaveLength(0);
  });

  it('订单页在栈里时返回它', () => {
    setStack('pages/order/index', 'pages/pay/index');
    goToOrder();
    expect(wxMock.navigations).toEqual([{ type: 'navigateBack', delta: 1 }]);
  });

  it('订单页不在栈里时用 redirectTo 打开，不带参数（订单页自己会取当前订单）', () => {
    setStack('pages/index/index');
    goToOrder();
    expect(wxMock.navigations).toEqual([{ type: 'redirectTo', url: '/pages/order/index' }]);
  });
});
