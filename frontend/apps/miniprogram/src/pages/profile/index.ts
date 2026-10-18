// 「我的」页：会员卡 + 储值余额 + 历史订单。
//
// 登录是自愿的（spec D1）：页面结构登录与否完全一致（会员卡 / 余额行 / 订单区），
// 未登录时会员卡显示头像占位与登录/注册引导（点卡即登录，走 store/member 的
// 静默登录），余额与订单区渲染引导文案而不是整个消失 —— 用户先看到「登录后有
// 什么」，登录意愿才立得住。已登录渲染会员卡、余额与本会员的跨桌历史订单。
// 会员功能未开启（管理端总闸关闭）时隐藏登录引导，只留说明。
import { ORDER_STATUS } from '@dining/shared/dicts';
import { money } from '@dining/shared/money';
import { bindMemberPhone, deleteMemberAccount, getWallet, updateMemberProfile } from '../../api/client';
import { memberEnabled, memberLogin, memberLogout, memberOrders, memberProfile, memberState } from '../../store/member';
import { sessionState } from '../../store/session';
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
    /** 会员功能开关（管理端总闸关闭时为 false，隐藏登录入口）。 */
    enabled: true,
    /** 已登录（有会员 ID 或本地令牌）。 */
    loggedIn: false,
    /** 登录流程进行中（防重复点击）。 */
    pending: false,
    member: null as MemberInfo | null,
    /** 会员卡头像位显示的字：昵称首字，未填显示「微」。 */
    avatarChar: '微',
    /** 储值余额（元，展示串）；-1 表示「未拉到」时不显示金额。 */
    balance: -1,
    balanceText: '—',
    /** 已绑定手机号（脱敏串）；空表示未绑定/未拉到。 */
    phone: '',
    /** 储值自助充值是否开放（管理端开关 + 微信支付已开通）。 */
    canRecharge: false,
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
    // 储值充值开关:管理端配置 + 微信支付已开通,任一未就绪就只显示余额不给充值入口。
    const cfg = sessionState().config ?? {};
    this.setData({
      canRecharge: cfg.wallet_recharge_enabled === '1' && cfg.wxpay_jsapi_enabled === '1'
    });
    if (loggedIn) {
      void this.reload();
      void this.reloadWallet();
    }
  },

  /** 拉储值余额（失败静默：余额是增强信息，不该打断会员卡渲染）。 */
  async reloadWallet(): Promise<void> {
    try {
      const w = await getWallet();
      this.setData({ balance: w.balance, balanceText: money(w.balance) });
    } catch {
      /* 静默 */
    }
  },

  /**
   * 充值与储值明细都已移到独立的「我的钱包」页：
   * 余额卡 + 金额网格 + 微信支付 + 流水，本页只留余额速览，点整行跳过去。
   * 未登录时这一行是引导位：点它等同点登录，不进钱包页（钱包接口需要会员令牌）。
   */
  onOpenWallet(): void {
    if (!this.data.loggedIn) {
      void this.onLogin();
      return;
    }
    wx.navigateTo({ url: '/pages/wallet/index' });
  },

  /** 点会员卡：未登录且功能开启时等同点登录按钮；已登录点卡不做任何事。 */
  onCardTap(): void {
    if (this.data.loggedIn || !this.data.enabled) return;
    void this.onLogin();
  },

  /** 手机号一键绑定：getPhoneNumber 回调的 code 与 wx.login 的 code 不是一个东西。 */
  async onGetPhoneNumber(e: WechatMiniprogram.CustomEvent<{ code?: string; errMsg?: string }>): Promise<void> {
    const code = e.detail?.code || '';
    if (!code) return; // 用户取消授权，静默即可
    try {
      const res = await bindMemberPhone(code);
      this.setData({ phone: res.phone });
      toastDone('手机号已绑定');
    } catch {
      /* 失败文案已由请求拦截器统一 toast */
    }
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
        // 已绑手机号（脱敏）：据此决定「绑定手机号」按钮是否还出现。
        phone: info.phone || '',
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

  /**
   * 修改昵称：用 wx.showModal 的 editable 模式（基础库 2.17.1+），不必自建弹层。
   *
   * 后端在顾客未填昵称时返回占位「微信用户」，预填时要把它当成空 ——
   * 否则用户点一下确认就把占位文案存成了真昵称。
   */
  onEditNickname(): void {
    const current = this.data.member?.nickname || '';
    const preset = current === '微信用户' ? '' : current;
    wx.showModal({
      title: '修改昵称',
      editable: true,
      placeholderText: '请输入昵称（最多 32 个字）',
      content: preset,
      success: r => {
        if (!r.confirm) return;
        void this.saveNickname(String(r.content || '').trim());
      }
    });
  },

  async saveNickname(nickname: string): Promise<void> {
    try {
      const info = await updateMemberProfile({ nickname });
      this.setData({
        member: info,
        avatarChar: (info.nickname || '微').trim().charAt(0) || '微'
      });
      toastDone('昵称已更新');
    } catch {
      /* 失败文案已由请求拦截器统一 toast，这里不再重复提示 */
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
  },

  /**
   * 注销会员（不可撤销）：档案与登录方式由后端物理删除，
   * 历史订单留在门店但归属置 0（不再出现在「我的订单」）。
   */
  onDeleteAccount(): void {
    wx.showModal({
      title: '注销会员',
      content:
        '注销后会员资料与登录记录将被删除，历史订单仍留在门店（但不再显示在「我的订单」）。该操作不可恢复，确定注销吗？',
      confirmText: '注销',
      confirmColor: '#d54941',
      success: r => {
        if (!r.confirm) return;
        void this.doDeleteAccount();
      }
    });
  },

  async doDeleteAccount(): Promise<void> {
    try {
      await deleteMemberAccount();
    } catch {
      return; // 失败文案已由拦截器统一提示，保持登录态不变
    }
    // 令牌无状态：清本地即可，后端已查不到档案。
    memberLogout();
    this.setData({ loggedIn: false, member: null, orders: [], total: 0, hasMore: false, page: 1 });
    toastDone('会员已注销');
  }
});

/** 登录反馈统一走成功 toast（区别于页面内的错误文案）。 */
function toastDone(msg: string): void {
  wx.showToast({ title: msg, icon: 'none' });
}
