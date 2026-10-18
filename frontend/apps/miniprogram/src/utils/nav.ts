// ============================================================================
// 主页面之间的跳转（首页 ↔ 点餐 ↔ 订单 ↔ 我的）
//
// 小程序端没有用原生 tabBar：点餐与订单是两个独立页面，且「订单」在未下单时是空态，
// 支付页还需要专注，原生 tabBar 的「全局常驻 + 自动切页」语义在这里反而添乱。
// 因此底部导航做成自定义组件（components/tab-bar），切页规则收在这里，供它与页面内按钮共用。
//
// 策略：
//   ・目标页已在页面栈里 → navigateBack 返回它，保留它的滚动位置与已加载的数据
//   ・不在栈里 → redirectTo 打开，替换当前页，避免来回切页把页面栈撑到 10 层上限
// ============================================================================

export const HOME_ROUTE = 'pages/home/index';
export const MENU_ROUTE = 'pages/index/index';
export const ORDER_ROUTE = 'pages/order/index';
export const PROFILE_ROUTE = 'pages/profile/index';

/** 去首页（默认主页：扫码入口 / 进行中订单 / 招牌菜）。 */
export function goToHome(): void {
  switchTo(HOME_ROUTE, false);
}

/**
 * 去点餐页。
 * 带上 append=1：本桌已有进行中订单时，菜单页才会留在菜单（否则它会把顾客又弹回订单页）。
 * 走 navigateBack 回到栈里已有的菜单页时不带参数——那个实例本来就是菜单态。
 */
export function goToMenu(): void {
  switchTo(MENU_ROUTE, true);
}

/** 去订单页（订单页自己会从会话/本地缓存里取当前订单，不需要额外参数）。 */
export function goToOrder(): void {
  switchTo(ORDER_ROUTE, false);
}

/** 去「我的」（会员卡 + 历史订单）。 */
export function goToProfile(): void {
  switchTo(PROFILE_ROUTE, false);
}

function switchTo(route: string, append: boolean): void {
  const stack = currentRoutes();
  if (stack[stack.length - 1] === route) return;
  const idx = stack.lastIndexOf(route);
  if (idx >= 0) {
    wx.navigateBack({ delta: stack.length - 1 - idx });
    return;
  }
  wx.redirectTo({ url: append ? `/${route}?append=1` : `/${route}` });
}

/** 当前页面栈的 route 列表（首页在数组头部，当前页在尾部）。 */
function currentRoutes(): string[] {
  return (getCurrentPages() as unknown as { route?: string }[]).map(p => String(p?.route || ''));
}
