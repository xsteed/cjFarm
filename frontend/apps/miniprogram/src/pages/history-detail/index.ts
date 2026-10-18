// 历史订单详情（从「我的订单」列表点进来）。
//
// 与订单页的区别：这里看的是**已结束的、可能属于别的桌**的单，所以
//   ・只读：不轮询、不加菜、不催菜；
//   ・归属只认会员令牌（/customer/order/mine/:orderNo），不看 tableId —— 顾客早已
//     离开那张桌，拿当前桌台去校验必然失败；
//   ・唯一的主动作是「再来一单」：把上次点的菜装进**当前**购物车（当前桌下单）。
//     菜可能已下架/售罄/改价，所以走与购物车恢复同一套复核（linesFromItems），
//     不复制赠菜、不沿用历史价格。
import { getMenu } from '../../api/client';
import { cartMergeLines } from '../../store/cart';
import { linesFromItems } from '../../store/cartPersist';
import { memberOrderDetail } from '../../store/member';
import { sessionState } from '../../store/session';
import { toast } from '../../utils/toast';
import { money } from '@dining/shared/money';
import { ORDER_STATUS } from '@dining/shared/dicts';
import { isActiveStatus } from '@dining/shared/orderStatus';
import { payStatusText, payStatusTheme } from '@dining/shared/orderDisplay';
import type { MenuCategory, OrderDetail } from '@dining/types';

/** 明细行（WXML 不做函数调用，展示串全部预计算）。 */
interface Row {
  key: string;
  name: string;
  specName: string;
  itemRemark: string;
  qtyText: string;
  /** 已是可展示串：赠送菜为「赠」，其余为「¥12.00」 */
  amountText: string;
  isGift: boolean;
}

Page({
  data: {
    loading: true,
    error: '',
    orderNo: '',
    shortNo: '',
    tableLabel: '',
    statusText: '',
    statusClass: 'tag-primary',
    payText: '',
    payClass: 'tag-default',
    personCount: 1,
    createTimeText: '',
    orderRemark: '',
    dishTotalText: '0.00',
    seatFeeText: '0.00',
    discountText: '0.00',
    totalText: '0.00',
    showSeatFee: false,
    showDiscount: false,
    rows: [] as Row[],
    /** 是否还有可复制的菜：全是赠菜 / 都没明细时置灰，别让顾客点了没反应 */
    canReorder: false,
    /** 只有「当前桌台 + 未支付 + 进行中」才给「去支付」：跨桌旧单没有支付意义 */
    canPay: false,
    reordering: false
  },

  /** 详情快照：再来一单要用它的明细（data 里只放渲染串，不放原始结构）。 */
  order: null as OrderDetail | null,
  /** 菜单缓存：同一页面可能点多次「再来一单」，只拉一次 */
  menu: null as MenuCategory[] | null,

  onLoad(options: Record<string, string | undefined>) {
    const orderNo = decodeURIComponent(String(options?.orderNo || '')).trim();
    if (!orderNo) {
      this.setData({ loading: false, error: '缺少订单号' });
      return;
    }
    this.setData({ orderNo });
    void this.load(orderNo);
  },

  async load(orderNo: string): Promise<void> {
    this.setData({ loading: true, error: '' });
    try {
      // 走会员封装：令牌过期会静默重登并重放，顾客不会莫名看到「请先登录」
      const order = await memberOrderDetail(orderNo);
      this.order = order;
      this.render(order);
    } catch (e) {
      const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
      this.setData({
        loading: false,
        error: err?.friendlyMessage || err?.message || '加载订单失败'
      });
    }
  },

  onRetry(): void {
    void this.load(this.data.orderNo);
  },

  render(order: OrderDetail): void {
    const status = Number(order.orderStatus) || 1;
    const payStatus = Number(order.payStatus) || 0;

    const rows: Row[] = (order.items || []).map((it, i) => {
      const isGift = Number(it.isGift) === 1;
      return {
        key: String(it.itemId ?? `${it.dishId}-${it.specId}-${i}`),
        name: String(it.dishName || ''),
        specName: String(it.specName || ''),
        itemRemark: String(it.itemRemark || ''),
        qtyText: `x${Number(it.quantity) || 0}`,
        amountText: isGift ? '赠' : `¥${money(it.amount)}`,
        isGift
      };
    });

    const s = sessionState();
    // 历史订单是跨桌的：只有「就是当前这桌、还没付款、还在进行中」时，
    // 「去支付」才有意义（否则跳到支付页会把这笔旧单的费用算到当前桌头上）。
    const sameTable = Number(order.tableId) > 0 && Number(order.tableId) === Number(s.tableId);

    this.setData({
      loading: false,
      error: '',
      shortNo: String(order.shortNo || order.orderNo || ''),
      tableLabel: String(order.tableName || order.tableNo || '').trim() || '—',
      statusText: ORDER_STATUS[status]?.label || '处理中',
      statusClass: `tag-${ORDER_STATUS[status]?.theme || 'primary'}`,
      payText: payStatusText(order),
      payClass: `tag-${payStatusTheme(order)}`,
      personCount: Number(order.personCount) || 1,
      createTimeText: String(order.createTime || '').replace('T', ' ').slice(0, 16),
      orderRemark: String(order.orderRemark || ''),
      dishTotalText: money(order.dishAmount),
      seatFeeText: money(order.seatFee),
      discountText: money(order.discountAmount),
      totalText: money(order.totalAmount),
      showSeatFee: Number(order.seatFee) > 0,
      showDiscount: Number(order.discountAmount) > 0,
      rows,
      // 全是赠菜就不给「再来一单」：赠菜由商家决定，不该被顾客自助复制
      canReorder: rows.some(r => !r.isGift),
      canPay: sameTable && payStatus === 0 && isActiveStatus(status)
    });
  },

  /**
   * 再来一单：把历史明细并入**当前**购物车。
   *
   * 为什么不直接调下单接口复制一张新单：
   *   ・桌台变了（历史单是别的桌），必须按当前桌下单；
   *   ・菜可能已下架/售罄/改价，必须按当前菜单复核；
   *   ・本桌若已有进行中订单，正确行为是「加菜」而不是新开一张单 ——
   *     走购物车页能自动判别（见 pages/cart 的 appendMode）。
   */
  async onReorder(): Promise<void> {
    if (this.data.reordering || !this.order) return;
    const s = sessionState();
    // 下单接口强制要求 tableId：没有桌台（没扫码/从「最近使用」冷启动）就无处可下
    if (!s.tableId || !s.tableCode) {
      toast.info('请先扫描餐桌上的二维码进入点餐');
      return;
    }

    this.setData({ reordering: true });
    try {
      if (!this.menu) this.menu = (await getMenu()) as MenuCategory[];
      // 赠菜不复制：那是商家送的，不该由顾客自助再点一份
      const items = (this.order.items || []).filter(it => Number(it.isGift) !== 1);
      const lines = linesFromItems(items, this.menu);
      if (!lines.length) {
        toast.info('这些菜暂时点不了，换几样吧');
        return;
      }
      // 并入而不是替换：顾客可能已经在菜单页点了几道，不许被历史订单冲掉
      cartMergeLines(lines);
      toast.success('已加入购物车');
      // redirectTo：顾客的意图是去下单，不必再回到这张历史单（返回键回到「我的」）
      wx.redirectTo({ url: '/pages/cart/index' });
    } catch {
      // 菜单拉取失败已由请求拦截器统一提示
    } finally {
      this.setData({ reordering: false });
    }
  },

  /** 去支付：仅当前桌台未支付单可见（见 canPay 的计算）。 */
  onPay(): void {
    if (!this.data.canPay) return;
    wx.navigateTo({ url: `/pages/pay/index?orderNo=${encodeURIComponent(this.data.orderNo)}` });
  }
});
