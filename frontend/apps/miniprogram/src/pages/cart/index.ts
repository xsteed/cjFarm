// 购物车 / 下单确认页。
//
// 对应 Web 端 apps/customer/src/order/CartDrawer.vue：
// 清单 + 份数增减 + 单项备注 + 人数 + 订单备注 + 金额明细 + 提交（下单或加菜）。
//
// 幂等：同一次提交（含失败重试）复用同一个 clientRequestId，后端据此去重，
// 避免「网络超时后用户再点一次」造成两笔订单（与 H5 的 pendingRequestId 同策略）。
import { appendOrder, createOrder, getOrderByNo } from '../../api/client';
import {
  cartClear,
  cartState,
  cartSettle,
  cartChangeQty,
  cartRemove,
  cartSetRemark,
  cartToItems
} from '../../store/cart';
import { applyOrder, sessionState, setPersonCount } from '../../store/session';
import { toast } from '../../utils/toast';
import { money } from '@dining/shared/money';
import { isActiveStatus } from '@dining/shared/orderStatus';
import type { OrderDetail } from '@dining/types';

interface CartRow {
  key: string;
  dishName: string;
  specName: string;
  itemRemark: string;
  quantity: number;
  priceText: string;
  amountText: string;
}

/** 单项备注上限：与菜品弹层的 maxlength 对齐（超出后端会静默截断，这里先截并提示）。 */
const MAX_ITEM_REMARK = 50;

Page({
  data: {
    rows: [] as CartRow[],
    personCount: 1,
    orderRemark: '',
    dishTotalText: '0.00',
    seatFeeText: '0.00',
    discountText: '0.00',
    totalText: '0.00',
    showSeatFee: false,
    showDiscount: false,
    submitting: false,
    /** 本桌已有「进行中且未支付」的订单 → 走加菜而不是新开单 */
    appendMode: false,
    submitText: '提交订单'
  },

  /** 本次提交的幂等键：成功后清空，失败保留以便重试去重。 */
  pendingRequestId: '',
  /** 幂等键对应的「提交内容指纹」：内容变了就换新键（见 submit 内说明）。 */
  pendingFingerprint: '',

  onShow() {
    const s = sessionState();
    const order = s.currentOrder;
    // 状态先归一成 number：后端一旦把 order_status 序列化成字符串，
    // 这里会恒判 false → 走 createOrder 而不是 appendOrder，同一桌开出第二笔订单
    const appendMode = !!order && isActiveStatus(Number(order.orderStatus)) && Number(order.payStatus) === 0;
    // 加菜时人数沿用订单现值（仍可继续调整）；否则沿用会话里记的人数
    const personCount = appendMode ? setPersonCount(Number(order?.personCount) || 1) : setPersonCount(s.personCount);

    this.setData({
      appendMode,
      submitText: appendMode ? '提交加菜' : '提交订单',
      personCount
    });
    this.render();
  },

  render() {
    const cfg = sessionState().config;
    const rows: CartRow[] = cartState().map(l => ({
      key: l.key,
      dishName: l.dishName,
      specName: l.specName,
      itemRemark: l.itemRemark,
      quantity: l.quantity,
      priceText: money(l.price),
      amountText: money(l.price * l.quantity)
    }));
    const settle = cartSettle(cfg, this.data.personCount);

    this.setData({
      rows,
      dishTotalText: money(settle.dishTotal),
      seatFeeText: money(settle.seatFee),
      discountText: money(settle.discount),
      totalText: money(settle.total),
      showSeatFee: settle.seatFee > 0,
      showDiscount: settle.discount > 0
    });
  },

  onInc(e: WechatMiniprogram.TouchEvent): void {
    cartChangeQty(String(e.currentTarget.dataset.key || ''), 1);
    this.render();
  },

  onDec(e: WechatMiniprogram.TouchEvent): void {
    cartChangeQty(String(e.currentTarget.dataset.key || ''), -1);
    this.render();
  },

  onRemove(e: WechatMiniprogram.TouchEvent): void {
    const key = String(e.currentTarget.dataset.key || '');
    const line = cartState().find(l => l.key === key);
    if (!line) return;
    wx.showModal({
      title: '移除菜品',
      content: `确定不要「${line.dishName}」了吗？`,
      confirmText: '移除',
      confirmColor: '#f0481f',
      success: res => {
        if (!res.confirm) return;
        cartRemove(key);
        this.render();
      }
    });
  },

  onClear(): void {
    if (!this.data.rows.length) return;
    wx.showModal({
      title: '清空购物车',
      content: '将移除已选的全部菜品，确定吗？',
      confirmText: '清空',
      confirmColor: '#f0481f',
      success: res => {
        if (!res.confirm) return;
        cartClear();
        this.render();
      }
    });
  },

  onPerson(e: WechatMiniprogram.TouchEvent): void {
    const delta = Number(e.currentTarget.dataset.delta) || 0;
    const next = setPersonCount(this.data.personCount + delta);
    if (next === this.data.personCount) return;
    this.setData({ personCount: next });
    this.render();
  },

  onRemarkInput(e: WechatMiniprogram.Input): void {
    this.setData({ orderRemark: e.detail.value });
  },

  /**
   * 就地改单项备注（H5 的 CartDrawer 有独立入口，这里对齐）。
   * 之前只能靠菜品弹层「确定」再加一份才能改备注，等于为了改字多下一份菜。
   */
  onEditRemark(e: WechatMiniprogram.TouchEvent): void {
    const key = String(e.currentTarget.dataset.key || '');
    const line = cartState().find(l => l.key === key);
    if (!line) return;
    wx.showModal({
      title: '备注这道菜',
      editable: true,
      placeholderText: '如：不要辣、少放盐',
      content: line.itemRemark,
      confirmText: '保存',
      success: res => {
        if (!res.confirm) return;
        // 只有可编辑模式才会返回 content；老基础库拿不到就保持原备注不动
        // （不能按空串处理，那会把顾客写的东西直接清掉）
        if (typeof res.content !== 'string') return;
        let text = res.content.trim();
        if (text.length > MAX_ITEM_REMARK) {
          text = text.slice(0, MAX_ITEM_REMARK);
          toast.warning(`备注最多 ${MAX_ITEM_REMARK} 字，已保留前 ${MAX_ITEM_REMARK} 字`);
        }
        cartSetRemark(key, text);
        this.render();
      }
    });
  },

  /** 幂等键：小程序没有 crypto.randomUUID，退回时间戳 + 随机串（与 H5 的兜底分支一致）。 */
  genRequestId(): string {
    return `req-${Date.now().toString(36)}-${Math.random().toString(16).slice(2)}`;
  },

  async onSubmit() {
    if (!this.data.rows.length) {
      toast.info('先挑几道想吃的菜吧～');
      return;
    }
    if (this.data.submitting) return;

    const s = sessionState();
    if (!s.tableId) {
      toast.error('桌台信息丢失，请重新扫码进入');
      return;
    }

    this.setData({ submitting: true });

    try {
      const items = cartToItems();
      // 幂等键绑定「提交内容指纹」：与 H5 同策略，只有「内容完全一致」的失败重试才复用旧键。
      // 若只按失败与否复用（旧实现），在「订单其实已创建、前端没收到成功响应、顾客又改了菜」
      // 的弱网时序下，重发会命中幂等返回首次结果，本次新增的菜被静默丢弃（漏单）。
      const fingerprint = JSON.stringify({
        append: this.data.appendMode,
        orderNo: s.currentOrder?.orderNo || '',
        personCount: this.data.personCount,
        orderRemark: this.data.orderRemark,
        items
      });
      if (!this.pendingRequestId || this.pendingFingerprint !== fingerprint) {
        this.pendingRequestId = this.genRequestId();
        this.pendingFingerprint = fingerprint;
      }
      const clientRequestId = this.pendingRequestId;
      let orderNo = '';
      if (this.data.appendMode && s.currentOrder?.orderNo) {
        const res = await appendOrder({
          tableId: s.tableId,
          orderNo: String(s.currentOrder.orderNo),
          items,
          personCount: this.data.personCount,
          clientRequestId
        });
        orderNo = String(res.orderNo || '');
        // 文案控制在 7 字以内：带 success 图标的 toast 会被微信截断
        toast.success('加菜成功，冲呀');
      } else {
        const res = await createOrder({
          tableId: s.tableId,
          personCount: this.data.personCount,
          orderRemark: this.data.orderRemark,
          items,
          clientRequestId
        });
        orderNo = String(res.orderNo || '');
        // 与 H5 统一语气（年轻化、安抚），7 字以内避免 success 图标截断
        toast.success('下单成功，冲呀');
      }

      this.pendingRequestId = '';
      this.pendingFingerprint = '';
      cartClear();
      // 用订单号回拉一次完整详情（含后厨状态），比用提交响应更完整。
      // 但回拉失败绝不能挡住跳转：订单已经生成，把顾客留在空的购物车页会诱使他重复下单。
      // 兜底办法是把 orderNo 带进 query，订单页会自己再拉一次（见 pages/order 的 onLoad）。
      let recovered = false;
      try {
        applyOrder((await getOrderByNo(orderNo)) as OrderDetail);
        recovered = true;
      } catch {
        /* 交给订单页按 orderNo 重拉 */
      }
      wx.redirectTo({
        url: recovered ? '/pages/order/index' : `/pages/order/index?orderNo=${encodeURIComponent(orderNo)}`
      });
    } catch {
      // 失败已由拦截器统一提示；停留在本页供重试，pendingRequestId 保留以便去重
    } finally {
      this.setData({ submitting: false });
    }
  },

  onBackToMenu(): void {
    wx.navigateBack({
      fail: () => wx.redirectTo({ url: '/pages/index/index' })
    });
  }
});
