// 订单进度页。
//
// 对应 Web 端 apps/customer/src/order/OrderTrackView.vue + useOrderPolling + useUrge：
// 状态步骤条、菜品清单、催菜（带冷却）、加菜、去支付、结束后「再点一单」。
//
// 轮询：3s 一拍（与 H5 一致），页面 onShow 启动 / onHide 停止 —— 小程序的页面生命周期
// 已经覆盖「切后台」和「跳去支付页」，所以不需要再监听 onAppHide（见 store/poller.ts）。
import { getOrderByNo, updateOrderPerson, urgeOrder } from '../../api/client';
import { ORDER_POLL_INTERVAL_MS, URGE_COOLDOWN_FALLBACK_S } from '../../config';
import { cartClear } from '../../store/cart';
import {
  applyOrder,
  markPersonChosen,
  persistPersonChoice,
  readPersisted,
  resetOrder,
  sessionState,
  setPersonCount
} from '../../store/session';
import { createPoller } from '../../store/poller';
import type { Poller } from '../../store/poller';
import { memberEnabled } from '../../store/member';
import { goToMenu } from '../../utils/nav';
import { toast } from '../../utils/toast';
import { money } from '../../shared/money';
import { ORDER_STATUS } from '../../shared/dicts';
import { ORDER_STATUS_CODE, isActiveStatus } from '../../shared/orderStatus';
import { payStatusText, payStatusTheme } from '../../shared/orderDisplay';
import type { OrderDetail, TableInfo } from '../../types';

/** 「出餐中订单」列表行：WXML 不支持函数调用，标签文案在这里算好。 */
interface ServingRow {
  key: string;
  orderNo: string;
  label: string;
}

/** 桌台详情里的「已付款但出餐未完成」订单 → 列表行（#短号 · 状态）。 */
function servingRowsOf(table: TableInfo | null): ServingRow[] {
  return (table?.servingOrders || []).map((o, i) => ({
    key: String(o.orderNo || `s${i}`),
    orderNo: String(o.orderNo || ''),
    label: `#${String(o.shortNo || o.orderNo || '')} · ${ORDER_STATUS[Number(o.orderStatus)]?.label || '处理中'}`
  }));
}

interface OrderRow {
  key: string;
  name: string;
  specName: string;
  itemRemark: string;
  qtyText: string;
  amountText: string;
  /** 赠送菜:菜名前标「赠」,金额位显示「赠」而非 ¥0.00 */
  isGift: boolean;
  /** 批次标签：只有「本单加过菜」时才非空（如「首单」「加菜2」），见 render 里的 multiBatch */
  batchLabel: string;
}

/** 出餐进度的一步（对应 H5 OrderProgress.vue，WXML 不做函数调用，状态全部预计算） */
interface StepRow {
  label: string;
  /** 这一步是否已到达（圆点与文案高亮） */
  on: boolean;
  /** 本步到下一步的连接线是否点亮 */
  barOn: boolean;
  /** 当前所在步（加一圈光晕）；已完成的单不再标「当前」 */
  now: boolean;
}

Page({
  data: {
    loading: true,
    error: '',
    // 「我的」tab 是否显示：跟随后端开关（进入本页时配置通常已由菜单页拉到）
    showProfile: true,
    /** 这个错误值不值得原地重试：网络类 true，「没有找到订单」这类再点也没用，false */
    canRetry: false,
    orderNo: '',
    shortNo: '',
    tableLabel: '',
    statusText: '',
    statusClass: 'tag-primary',
    payText: '',
    payClass: 'tag-default',
    totalText: '0.00',
    /** 账单明细：菜品小计 / 茶位费 / 满减（为 0 的项不渲染） */
    dishTotalText: '0.00',
    seatFeeText: '0.00',
    discountText: '0.00',
    showSeatFee: false,
    showDiscount: false,
    personCount: 1,
    createTimeText: '',
    orderRemark: '',
    rows: [] as OrderRow[],
    // 动作可用性
    canAppend: false,
    canUrge: false,
    canPay: false,
    /** 能否改就餐人数：进行中且未支付（已支付的单改金额会对不上实收） */
    canEditPerson: false,
    /** 改人数请求进行中（防连点重复请求） */
    updatingPerson: false,
    /** 底部主按钮文案：先付模式未支付时带火苗，提示「付款后厨才开工」 */
    payBtnText: '去支付',
    /** 先付模式下「未支付」时的那句说明：付款后才出厨单，别让顾客干等 */
    showPayFirstTip: false,
    urging: false,
    urgeCooldown: 0,
    // 结束态
    settled: false,
    done: false,
    canceled: false,
    /** 取消提示整句：有真实原因带上，没有就给标准说明（后端已对原因做可展示性收口） */
    cancelTip: '',
    /** 出餐进度：1已下单 → 2制作中 → 3已上齐（取消单不渲染） */
    steps: [] as StepRow[],
    showProgress: false,
    /** 支付凭证行：已支付时给出「已支付（微信）· 结账时间」，可核对可回看 */
    paidInfo: '',
    /**
     * 「出餐中订单」列表：本桌订单已付款、还在出餐时（session.currentOrder 不含它们），
     * 订单 tab 列出这些单让顾客点一条看进度，而不是甩一句「没有找到进行中的订单」。
     */
    showServingList: false,
    servingRows: [] as ServingRow[],
    /**
     * 本桌没有进行中的订单：展示与 H5 一致的空态（插画 + 文案 + 去点餐）。
     * error 仍保留那句「没有找到进行中的订单」作为原因（onShow 靠它决定不轮询），
     * 但**不再直接渲染它** —— 干巴巴一行灰字换成插画空态，见 index.wxml。
     */
    noOrder: false,
    /**
     * 左上角返回按钮。小程序切 tab 走 redirectTo（页面栈深 1），原生导航栏没有返回箭头；
     * 而从菜单页「出餐中」标签 navigateTo 进来时原生箭头已在，不再重复画一个。
     */
    showBack: false,
    /** 当前订单是从「出餐中订单」列表点进来的：返回时先回列表 */
    fromServingList: false
  },

  poller: null as Poller | null,
  /** 同 store/poller.ts：小程序返回 number、Node 下返回 Timeout，用 ReturnType 兼容两边 */
  urgeTimer: null as ReturnType<typeof setInterval> | null,
  /** 冷启动带 query 时 recover 已经在拉：onShow 的第一拍跳过，免得同一单同时请求两次 */
  skipFirstTick: false,

  onLoad(options: Record<string, string | undefined>) {
    // 没有原生返回箭头时（tab 切页 redirectTo，栈深 1）才画自己的返回按钮
    this.setData({ showBack: getCurrentPages().length <= 1 });
    // 1) 菜单页点「出餐中」标签进来时带 orderNo（那是已付款的单，session 里没有）
    const fromQuery = String(options?.orderNo || '').trim();
    if (fromQuery) {
      this.setData({ orderNo: fromQuery });
      // recover 拿到结果后会自己启动轮询，onShow 的首拍就跳过（省一次重复请求）
      this.skipFirstTick = true;
      void this.recover(fromQuery);
      return;
    }
    // 2) 本进程内有当前订单（刚下单 / 从购物车跳来）：直接用，省一次请求
    const order = sessionState().currentOrder;
    if (order?.orderNo) {
      this.render(order);
      this.setData({ loading: false });
      return;
    }
    // 3) 冷启动/从 tab 进来且本进程没有当前订单：本桌可能还有「已付款出餐中」的
    //    订单（session.currentOrder 不含它们），先列出来让顾客点，再按订单号回拉
    if (this.refreshServingList()) return;
    void this.recover();
  },

  onShow() {
    // 「我的」tab 跟随会员开关（要放在错误态 return 之前：出错时 tab 也不能丢）
    this.setData({ showProfile: memberEnabled(), showBack: getCurrentPages().length <= 1 });
    // 菜单页可能刚拉到桌台详情（里面有出餐中的订单）：没有正在看的订单时就重算列表，
    // 有就切到列表（并清掉「没有找到进行中的订单」的错误态），没有才维持原状
    if (!this.data.orderNo) this.refreshServingList();
    // 错误态不轮询：顾客没点「重试」之前，别一直打注定失败的请求
    if (!this.data.orderNo || this.data.error) return;
    // 刚用 query 冷启动时 recover 已经在拉数据，这一拍跳过（见 onLoad）
    const immediate = !this.skipFirstTick;
    this.skipFirstTick = false;
    this.startPolling(immediate);
  },

  onHide() {
    this.stopPolling();
  },

  onUnload() {
    this.stopPolling();
    this.clearUrgeTimer();
  },

  /**
   * 重算「出餐中订单」列表：桌台详情里带 servingOrders 且当前没有正在看的订单时，
   * 展示列表并清掉错误态。返回是否展示了列表（调用方据此决定还走不走 recover）。
   */
  refreshServingList(): boolean {
    const rows = servingRowsOf(sessionState().table);
    if (!rows.length) {
      if (this.data.showServingList) this.setData({ showServingList: false, servingRows: [] });
      return false;
    }
    this.setData({
      loading: false,
      error: '',
      canRetry: false,
      noOrder: false,
      showServingList: true,
      servingRows: rows
    });
    return true;
  },

  /** 点列表里的一条：按单号回拉详情并渲染（与菜单页点「出餐中」标签同一条路）。 */
  onOpenServing(e: WechatMiniprogram.TouchEvent): void {
    const orderNo = String(e.currentTarget.dataset.no || '');
    if (!orderNo) return;
    this.setData({ showServingList: false, orderNo, fromServingList: true, noOrder: false });
    void this.recover(orderNo);
  },

  /**
   * 左上角返回：从「出餐中订单」列表点进来的先回列表（订单还在出餐就回得去），
   * 其他情况回点餐页。栈里没有可返回的页面时 goToMenu 会 redirectTo 打开菜单页。
   */
  onBack(): void {
    if (this.data.fromServingList) {
      this.setData({ fromServingList: false, orderNo: '' });
      this.stopPolling();
      this.clearUrgeTimer();
      // 列表没了（订单刚好出餐完成、被清台）就回点餐页，别把顾客留在空页
      if (!this.refreshServingList()) goToMenu();
      return;
    }
    goToMenu();
  },

  /**
   * 按订单号回拉一次详情。
   * @param explicit 显式传入的订单号（来自 query）；缺省时用本地缓存里记的。
   */
  async recover(explicit?: string) {
    const orderNo = String(explicit || '').trim() || readPersisted().orderNo;
    if (!orderNo) {
      this.setData({
        loading: false,
        error: '没有找到进行中的订单',
        canRetry: false,
        noOrder: true
      });
      return;
    }
    try {
      // 带 tableId：后端据此校验订单归属。该接口无鉴权，订单号会出现在小票与照片里，
      // 不带桌台参数等于任人凭单号遍历别人的订单（桌台、金额、菜品、备注）。
      // tableId 为 0（会话还没拿到桌台详情）时后端自动退化为不限桌，与原先行为一致。
      const order = (await getOrderByNo(orderNo, sessionState().tableId)) as OrderDetail;
      applyOrder(order);
      this.render(order);
      this.setData({ loading: false, canRetry: false, noOrder: false });
      // 刚刚拉过一次，别再立刻补一拍
      this.startPolling(false);
    } catch (e) {
      const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
      // 网络类失败留在本页可以重试，比只给「去点菜」把顾客赶回菜单友好
      this.setData({
        loading: false,
        error: err?.friendlyMessage || err?.message || '加载订单失败',
        canRetry: true,
        noOrder: false
      });
      // 先停轮询：错误态下每 3s 打一次注定失败的请求（失败还被 poller 静默吞掉）
      this.stopPolling();
    }
  },

  /**
   * 启动轮询。
   * @param immediate 是否立刻补一拍。recover() 刚拉过一次，就别马上再拉（省一次重复请求）。
   */
  startPolling(immediate = true) {
    if (!this.poller) {
      this.poller = createPoller(() => this.tick(), ORDER_POLL_INTERVAL_MS);
    }
    this.poller.start();
    if (immediate) this.poller.trigger();
  },

  stopPolling() {
    this.poller?.stop();
  },

  async tick() {
    const orderNo = this.data.orderNo;
    if (!orderNo) return;
    // 与 recover 同口径带上 tableId：轮询是同一个接口的另一条调用路径，
    // 只在 recover 里传等于给高频请求留了一扇敞开的门。
    const order = (await getOrderByNo(orderNo, sessionState().tableId)) as OrderDetail;
    applyOrder(order);
    this.render(order);
    // 已结束的订单不必继续轮询（状态先归一：字符串型状态会让这个判断永远不成立）
    const status = Number(order.orderStatus) || 0;
    if (status === ORDER_STATUS_CODE.DONE || status === ORDER_STATUS_CODE.CANCELED) {
      this.stopPolling();
    }
  },

  /** 把订单详情转成渲染所需的一切（WXML 不做函数调用，全部预计算）。 */
  render(order: OrderDetail) {
    const status = Number(order.orderStatus) || ORDER_STATUS_CODE.PLACED;
    const payStatus = Number(order.payStatus) || 0;
    const active = isActiveStatus(status);
    const done = status === ORDER_STATUS_CODE.DONE;
    const canceled = status === ORDER_STATUS_CODE.CANCELED;
    // 已结账：付过款但仍在出餐（顾客侧不再可加菜/催菜之外的操作）
    const settled = payStatus === 1 && active;

    // 加过菜的单才标批次：顾客能分清哪几道是后来加的；只有一批时不标，省得满屏标签。
    // 按「出现顺序」编号而不是 batchNo-1：后端批次号万一跳号（1、3），文案也不会错位。
    const batchOrder = new Map<number, number>();
    for (const it of order.items || []) {
      const b = Number(it.batchNo) || 1;
      if (!batchOrder.has(b)) batchOrder.set(b, batchOrder.size + 1);
    }
    const multiBatch = batchOrder.size > 1;

    const rows: OrderRow[] = (order.items || []).map((it, i) => {
      const seq = batchOrder.get(Number(it.batchNo) || 1) || 1;
      // 赠送菜(amount 记 0):金额位显示「赠」而不是 ¥0.00,避免顾客误以为漏算。
      const isGift = Number(it.isGift) === 1;
      return {
        key: String(it.itemId ?? `${it.dishId}-${it.specId}-${i}`),
        name: String(it.dishName || ''),
        specName: String(it.specName || ''),
        itemRemark: String(it.itemRemark || ''),
        qtyText: `x${Number(it.quantity) || 0}`,
        amountText: isGift ? '赠' : money(it.amount),
        isGift,
        batchLabel: multiBatch ? (seq <= 1 ? '首单' : `加菜${seq - 1}`) : ''
      };
    });

    // 出餐进度：只映射后厨的三个真实节点（1已下单 → 2制作中 → 3已上齐），
    // 已完成的单三步全亮；不承诺时间、不做倒计时 —— 后厨改状态才是唯一事实来源。
    const stepIdx = done ? 3 : Math.min(Math.max(status - 1, 0), 2);
    const steps: StepRow[] = [
      { label: '已下单', on: true, barOn: stepIdx >= 1, now: stepIdx === 0 },
      { label: '制作中', on: stepIdx >= 1, barOn: stepIdx >= 2, now: stepIdx === 1 },
      { label: '已上齐', on: stepIdx >= 2, barOn: false, now: stepIdx === 2 }
    ];

    // 取消提示：有真实原因（后端已把「商家取消」这类无信息标签抹平）就带上，
    // 没有就给标准说明 —— 取消只可能发生在未支付订单上（后端强制），能明确告诉顾客不产生费用。
    const cancelReason = String(order.cancelReason || '').trim();
    const cancelTip = canceled
      ? cancelReason
        ? `订单已取消：${cancelReason}`
        : '订单已取消，本单未支付、不会产生任何费用'
      : '';

    // 支付凭证：payTime 是「2026-10-09 12:03:07」这类字符串，截到分钟即可
    const paidAt = String(order.payTime || '')
      .replace('T', ' ')
      .slice(0, 16);
    const paidInfo = payStatus === 1 ? [payStatusText(order), paidAt].filter(Boolean).join(' · ') : '';

    // 先付模式下的两处收紧（与 H5 OrderTrackView 同口径）：
    //   1) 催菜只对「后厨已收到厨单」的订单有意义 —— 未支付时后厨还没收到，催了只会得到
    //      一个「已催」的假反馈，顾客反而以为菜在做；
    //   2) 未支付时给一句「待付款」引导，与底部「去支付」按钮形成因果。
    const payFirst = sessionState().config?.pay_first_enabled === '1';
    // 先付模式下后厨还没收到这颗单，付款才是关键动作，给按钮加个火苗催一下；
    // 后付模式只是结账，不加这个紧迫感（与 H5 OrderTrackView 的 st-paybtn 文案同口径）
    const payBtnText = payFirst && active && payStatus === 0 ? '🔥 去支付' : '去支付';

    const table = sessionState().table;
    this.setData({
      steps,
      showProgress: !canceled,
      paidInfo,
      orderNo: String(order.orderNo || ''),
      shortNo: String(order.shortNo || order.orderNo || ''),
      tableLabel: String(table?.tableName || table?.tableNo || '').trim() || '本桌',
      statusText: ORDER_STATUS[status]?.label || '处理中',
      statusClass: `tag-${ORDER_STATUS[status]?.theme || 'primary'}`,
      payText: payStatusText(order),
      payClass: `tag-${payStatusTheme(order)}`,
      totalText: money(order.totalAmount),
      dishTotalText: money(order.dishAmount),
      seatFeeText: money(order.seatFee),
      discountText: money(order.discountAmount),
      showSeatFee: Number(order.seatFee) > 0,
      showDiscount: Number(order.discountAmount) > 0,
      personCount: Number(order.personCount) || 1,
      createTimeText: String(order.createTime || ''),
      orderRemark: String(order.orderRemark || ''),
      rows,
      canAppend: active && payStatus === 0,
      // 改人数与加菜同一前提：进行中且未支付。已支付的单改人数会改应收金额，
      // 与实收对不上，只能到前台走退款/改单
      canEditPerson: active && payStatus === 0,
      canUrge: active && (!payFirst || payStatus === 1),
      canPay: active && payStatus === 0,
      payBtnText,
      showPayFirstTip: payFirst && active && payStatus === 0,
      settled,
      done,
      canceled,
      cancelTip
    });
  },

  // ---------- 催菜（与 H5 useUrge 同策略：silent 请求，冷却倒计时本地维护） ----------
  async onUrge() {
    if (this.data.urging || this.data.urgeCooldown > 0) return;
    const s = sessionState();
    if (!this.data.orderNo || !s.tableId) return;

    this.setData({ urging: true });
    try {
      const res = await urgeOrder(this.data.orderNo, s.tableId, { silent: true });
      // 与 H5 useUrge 统一语气；7 字以内避免 success 图标截断
      toast.success('催单成功，冲呀');
      this.startUrgeCooldown(Number(res?.cooldown) || URGE_COOLDOWN_FALLBACK_S);
    } catch (e) {
      // 冷却被拒时后端在响应体里给出剩余秒数，直接拿来展示倒计时
      const err = e as { response?: { data?: { data?: { cooldown?: number } } } } | null | undefined;
      const cd = Number(err?.response?.data?.data?.cooldown);
      if (Number.isFinite(cd) && cd > 0) {
        this.startUrgeCooldown(cd);
        toast.info(`已催过啦，请等待 ${cd} 秒`);
      } else {
        toast.error('催菜失败，请稍后重试');
      }
    } finally {
      this.setData({ urging: false });
    }
  },

  startUrgeCooldown(sec: number): void {
    this.clearUrgeTimer();
    let left = Math.max(0, Math.floor(sec) || 0);
    this.setData({ urgeCooldown: left });
    if (left <= 0) return;
    this.urgeTimer = setInterval(() => {
      left -= 1;
      this.setData({ urgeCooldown: left });
      if (left <= 0) this.clearUrgeTimer();
    }, 1000);
  },

  clearUrgeTimer(): void {
    if (this.urgeTimer !== null) {
      clearInterval(this.urgeTimer);
      this.urgeTimer = null;
    }
  },

  // ---------- 改就餐人数 ----------
  /**
   * 「修改」人数：弹一个可编辑输入框。
   *
   * 为什么用 showModal 而不是加减按钮：人数直接决定茶位费，改完金额就变，
   * 让顾客在弹窗里一次确认「改成几」比连点加减更不容易误操作；也与购物车页
   * 改单项备注的交互一致（同一个基础库能力，无额外依赖）。
   */
  onEditPerson(): void {
    if (!this.data.canEditPerson || this.data.updatingPerson) return;
    const current = Number(this.data.personCount) || 1;
    wx.showModal({
      title: '修改用餐人数',
      // content 预填当前值：顾客只需改动数字，不用重新输一遍
      content: String(current),
      editable: true,
      placeholderText: '1~20 人',
      confirmText: '保存',
      success: r => {
        if (!r.confirm) return;
        // 老基础库不给 content：保持原值不动（不能当 0 处理，那会把人数改成 1）
        if (typeof r.content !== 'string') return;
        const text = r.content.trim();
        if (!text) return;
        const next = Number(text);
        if (!Number.isInteger(next) || next < 1 || next > 20) {
          toast.info('请输入 1~20 之间的整数');
          return;
        }
        if (next === current) return;
        void this.submitPerson(next);
      }
    });
  },

  /** 提交人数变更：金额由后端重算并返回最新订单，前端只负责同步与重绘。 */
  async submitPerson(personCount: number): Promise<void> {
    if (this.data.updatingPerson) return;
    const s = sessionState();
    if (!s.tableId || !this.data.orderNo) {
      toast.error('桌台信息丢失，请重新扫码进入');
      return;
    }
    this.setData({ updatingPerson: true });
    try {
      const order = (await updateOrderPerson({
        tableId: s.tableId,
        orderNo: this.data.orderNo,
        personCount
      })) as OrderDetail;
      // 三处同步：会话人数（购物车页/菜单页的茶位费都读它）、订单快照、本页渲染
      setPersonCount(Number(order.personCount) || personCount);
      // 显式改过人数即「已选」：落盘后冷启动不再弹「选人数」面板
      markPersonChosen();
      persistPersonChoice();
      applyOrder(order);
      this.render(order);
      toast.success(`已更新为 ${Number(order.personCount) || personCount} 人`);
    } catch {
      // 失败提示由请求拦截器统一给出（含「订单已支付，不可修改人数」这类业务文案）
    } finally {
      this.setData({ updatingPerson: false });
    }
  },

  // ---------- 页面动作 ----------
  /**
   * 「本桌没有进行中订单」空态里的「去点菜」。
   * 与 onNewOrder（再点一单）区别开：顾客可能只是切过来看一眼订单，
   * 顺手点这个按钮不该把购物车里已经选好的菜清掉。
   */
  onGoMenu() {
    goToMenu();
  },

  onAppend() {
    // 加菜 = 回菜单页继续点（购物车在下单成功后已清空）。
    // 切页策略复用 utils/nav：菜单页在栈里就退回（用 lastIndexOf，中间夹页也能一次退到底，
    // 并保留它的分类/滚动位置），不在栈里就带 append=1 重开 ——
    // 没这个标记，菜单页会因本桌有进行中订单又把顾客弹回订单页，加菜永远点不成。
    goToMenu();
  },

  onPay() {
    wx.navigateTo({ url: '/pages/pay/index' });
  },

  /** 结束后「再点一单」：清掉本单，回菜单重新开始。 */
  onNewOrder() {
    cartClear();
    resetOrder();
    // 用 reLaunch 而非 redirectTo：提交时是 cart→order 的 redirectTo，当前栈为 [index, order]；
    // 再 redirectTo 回 index 会在栈里塞进第二个 index（出现重复首页，返回两次才退出）。
    // reLaunch 关闭所有页面后重开，栈回到干净的 [index]。
    wx.reLaunch({ url: '/pages/index/index' });
  },

  onRefresh() {
    if (this.data.loading) return;
    // 走 poller.trigger()：它带 busy 守卫，手动刷新与定时那一拍不会并发
    // （两拍并发会乱序，旧快照可能覆盖新数据）
    if (!this.poller) {
      this.poller = createPoller(() => this.tick(), ORDER_POLL_INTERVAL_MS);
    }
    this.poller.trigger();
  },

  /** 错误态里的「重试」：走同一条 recover 路径，订单号来源不变。 */
  onRetry(): void {
    this.setData({ loading: true, error: '', canRetry: false });
    void this.recover(this.data.orderNo || undefined);
  }
});
