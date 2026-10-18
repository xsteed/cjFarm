// 「我的」页：会员卡 + 历史订单。
//
// 登录是自愿的（spec D1）：未登录渲染注册引导卡，点「微信一键注册成为会员」走
// store/member 的静默登录；已登录渲染会员卡与本会员的跨桌历史订单。
// 会员功能未开启（后端未配 AppSecret）时隐藏登录入口，只留说明。
import { ORDER_STATUS } from '@dining/shared/dicts';
import { money } from '@dining/shared/money';
import { memberEnabled, memberLogin, memberLogout, memberOrders, memberProfile, memberState } from '../../store/member';
import type { MemberInfo, MemberOrderRow } from '../../types';

/** 列表行：WXML 不支持函数调用，展示串都在这里算好（见 docs/miniprogram.md 5.3）。 */
interface OrderRow {
  orderNo: string;
  pickupText: string;
  tableName: string;
  statusText: string;
  statusClass: string;
  amountText: string;
  timeText: string;
}

function toRow(o: MemberOrderRow): OrderRow {
  const status = Number(o.orderStatus) || 1;
  return {
    orderNo: String(o.orderNo || ''),
    pickupText: Number(o.pickupNo) > 0 ? `取餐号 ${o.pickupNo}` : '',
    tableName: String(o.tableName || ''),
    statusText: ORDER_STATUS[status]?.label || '处理中',
    statusClass: `tag-${ORDER_STATUS[status]?.theme || 'primary'}`,
    amountText: money(o.totalAmount),
    timeText: String(o.createTime || '')
      .replace('T', ' ')
      .slice(0, 16)
  };
}

const PAGE_SIZE = 10;

Page({
  data: {
    loading: false,
    error: '',
    /** 会员功能开关（后端未配 AppSecret 时为 false，隐藏登录入口）。 */
    enabled: true,
    /** 已登录（有会员 ID 或本地令牌）。 */
    loggedIn: false,
    /** 登录流程进行中（防重复点击）。 */
    pending: false,
    member: null as MemberInfo | null,
    /** 会员卡头像位显示的字：昵称首字，未填显示「微」。 */
    avatarChar: '微',
    orders: [] as OrderRow[],
    total: 0,
    page: 1,
    hasMore: false,
    loadingMore: false
  },

  onShow() {
    // 每次进入都刷新一次开关与登录态（配置可能在菜单页才拉到）。
    const enabled = memberEnabled();
    const loggedIn = memberState().customerId !== 0;
    this.setData({ enabled, loggedIn });
    if (loggedIn) void this.reload();
  },

  /** 登录后拉会员卡 + 第一页订单。 */
  async reload(): Promise<void> {
    if (this.data.loading) return;
    this.setData({ loading: true, error: '' });
    try {
      const info = await memberProfile();
      const res = await memberOrders(1, PAGE_SIZE);
      this.setData({
        loading: false,
        member: info,
        avatarChar: (info.nickname || '微').trim().charAt(0) || '微',
        loggedIn: true,
        orders: res.items.map(toRow),
        total: res.total,
        page: 1,
        hasMore: res.items.length < res.total
      });
    } catch (e) {
      this.setData({ loading: false, error: (e as Error)?.message || '加载失败，请重试' });
    }
  },

  onRetry(): void {
    void this.reload();
  },

  /** 微信一键注册/登录。 */
  async onLogin(): Promise<void> {
    if (this.data.pending) return;
    this.setData({ pending: true, error: '' });
    try {
      const { isNew } = await memberLogin();
      this.setData({ pending: false, loggedIn: true });
      // 注册与回访的反馈不同；文案在 toast 里，页面交给 reload 渲染会员卡。
      toastDone(isNew ? '🎉 注册成功，已加入会员' : '欢迎回来');
      await this.reload();
    } catch (e) {
      this.setData({ pending: false, error: (e as Error)?.message || '登录失败，请重试' });
    }
  },

  /** 打开历史订单详情（只读：明细 + 「再来一单」）。 */
  onOpenOrder(e: WechatMiniprogram.TouchEvent): void {
    const orderNo = String(e.currentTarget.dataset.no || '');
    if (!orderNo) return;
    wx.navigateTo({ url: `/pages/history-detail/index?orderNo=${encodeURIComponent(orderNo)}` });
  },

  /** 加载下一页历史订单。 */
  async onLoadMore(): Promise<void> {
    if (this.data.loadingMore || !this.data.hasMore) return;
    this.setData({ loadingMore: true });
    try {
      const next = this.data.page + 1;
      const res = await memberOrders(next, PAGE_SIZE);
      const merged = this.data.orders.concat(res.items.map(toRow));
      this.setData({
        loadingMore: false,
        orders: merged,
        page: next,
        hasMore: merged.length < res.total
      });
    } catch (e) {
      this.setData({ loadingMore: false, error: (e as Error)?.message || '加载失败，请重试' });
    }
  },

  /** 退出登录（令牌无状态，清本地即可）。 */
  onLogout(): void {
    wx.showModal({
      title: '退出登录',
      content: '退出后将无法查看历史订单，确定退出吗？',
      success: r => {
        if (!r.confirm) return;
        memberLogout();
        this.setData({ loggedIn: false, member: null, orders: [], total: 0, hasMore: false, page: 1 });
      }
    });
  }
});

/** 登录反馈统一走成功 toast（区别于页面内的错误文案）。 */
function toastDone(msg: string): void {
  wx.showToast({ title: msg, icon: 'none' });
}
