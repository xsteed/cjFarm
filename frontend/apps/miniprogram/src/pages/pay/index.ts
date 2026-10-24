// 结账页。
//
// 小程序内支付（JSAPI）已接通：wx.login → 后端用 code 换 openid 并 JSAPI 下单、
// 签好调起参数 → wx.requestPayment（见 onPayOnline）。入口是否出现由公开配置
// wxpay_jsapi_enabled 决定，fail-closed：后端未开通时按钮根本不存在，
// 不会出现「点了却拉不起收银台」。
//
// 未开通时提供与 H5 一致的降级路径：展示门店收款码 + 引导到前台结账，
// 并轮询订单支付状态（商家在后台收款后顾客能立刻看到）。
// 详见 docs/miniprogram.md 4.1 节。
import {
  cancelMixedByWallet,
  getOrderByNo,
  getWallet,
  payJsapi,
  payMixedByWallet,
  payOrderByWallet,
  queryPay
} from '../../api/client';
import type { PayJsapiResult } from '../../api/client';
import { mediaUrl } from '../../config';
import { applyOrder, readPersisted, sessionState } from '../../store/session';
import { createPoller } from '../../store/poller';
import type { Poller } from '../../store/poller';
import { goToOrder } from '../../utils/nav';
import { memberState } from '../../store/member';
import { toast } from '../../utils/toast';
import { money } from '../../shared/money';
import { ORDER_STATUS } from '../../shared/dicts';
import type { OrderDetail } from '../../types';

/** 支付状态轮询间隔：5s（H5 在线支付也是约每 15s 查一次单，这里更密一点）。 */
const PAY_POLL_INTERVAL_MS = 5000;

Page({
  data: {
    loading: true,
    error: '',
    orderNo: '',
    shortNo: '',
    totalText: '0.00',
    statusText: '',
    payTab: 'wx' as 'wx' | 'ali',
    qrImage: '',
    hasQr: false,
    /** 当前渠道的付款指引（按「是否开通在线支付」+「是否配置收款码」分档） */
    chanTip: '',
    paid: false,
    /** 门店先付模式：显示「付完才开做」提示 */
    payFirst: false,
    checking: false,
    /**
     * 后端是否已开通小程序内支付（JSAPI）：由公开配置的 `wxpay_jsapi_enabled` 决定。
     * 未开通时主按钮仍是「我已完成支付」（收款码 + 前台结账的降级路径）。
     */
    canJsapi: false,
    /** 储值余额支付：登录会员且余额足够时出现（与 JSAPI 按钮并存，顾客自己选）。 */
    canWallet: false,
    /** 混合支付：有余额但不够付本单，且小程序支付已开通时出现。 */
    canMixed: false,
    /** 混合支付按钮文案（如「余额 10.00 + 微信 18.00」）。 */
    mixedText: '',
    /** 本单实际可用的余额（元，不超过订单金额）。 */
    walletUsable: 0,
    /** 订单金额（元，数值型：混合支付要算差额，不能从展示串反解）。 */
    totalValue: 0,
    /** 登录会员有余额但不够付本单：显示一条「余额不足」提示（不可点，纯告知）。 */
    walletShort: false,
    walletBalanceText: '0.00'
  },

  poller: null as Poller | null,

  /** 是否已发起离页导航：支付成功的 800ms 延时跳转与顾客手动返回可能撞在一起 */
  leaving: false,

  /** 页面是否在前台：recover 返回时若页面已隐藏就别启动轮询（回前台由 onShow 起） */
  pageVisible: true,

  /** 查支付状态的并发锁：手动点击与静默轮询共用（data.checking 只负责按钮样式） */
  querying: false,

  /** 储值余额请求在途标记:render 末尾与切 tab 都会触发,避免并发重复请求 */
  walletLoading: false,
  /** 最近一次请求对应的订单金额:在途期间金额变过就按最新值补跑一次 */
  walletTotal: 0,

  /** 支付成功后延时跳转的定时器：页面提前卸载时必须清掉 */
  leaveTimer: null as ReturnType<typeof setTimeout> | null,

  onLoad(options: Record<string, string | undefined>) {
    // 会话里没有订单时，允许从链接参数带 orderNo 进来（与订单页同款兜底）
    const fromQuery = String(options?.orderNo || '').trim();
    if (fromQuery) {
      this.setData({ orderNo: fromQuery });
      void this.recover(fromQuery);
      return;
    }
    const order = sessionState().currentOrder;
    if (order?.orderNo) {
      this.render(order);
      this.setData({ loading: false });
      return;
    }
    void this.recover();
  },

  onShow() {
    this.pageVisible = true;
    if (!this.data.orderNo) return;
    this.startPolling();
  },

  onHide() {
    this.pageVisible = false;
    this.poller?.stop();
  },

  onUnload() {
    this.poller?.stop();
    // 顾客可能在这 800ms 内已经离开：不清的话定时器仍会把他拽回订单页
    if (this.leaveTimer) {
      clearTimeout(this.leaveTimer);
      this.leaveTimer = null;
    }
  },

  async recover(explicit?: string) {
    const orderNo = String(explicit || '').trim() || readPersisted().orderNo;
    if (!orderNo) {
      this.setData({ loading: false, error: '没有找到待结账的订单' });
      return;
    }
    try {
      // 带 tableId 让后端校验归属（详见 pages/order 的同名注释）。
      const order = (await getOrderByNo(orderNo, sessionState().tableId)) as OrderDetail;
      applyOrder(order);
      this.render(order);
      this.setData({ loading: false });
      // 请求期间顾客可能已退到后台：隐藏时别启动轮询，回前台由 onShow 起
      if (this.pageVisible) this.startPolling();
    } catch (e) {
      const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
      this.setData({ loading: false, error: err?.friendlyMessage || err?.message || '加载订单失败' });
    }
  },

  startPolling() {
    if (!this.poller) {
      // 包一层再丢弃返回值：poller 的回调契约是 void/Promise<void>，
      // 而 checkPaid 现在会回传「是否已确认到账」
      this.poller = createPoller(() => {
        void this.checkPaid(true);
      }, PAY_POLL_INTERVAL_MS);
    }
    this.poller.start();
  },

  render(order: OrderDetail) {
    const cfg = sessionState().config ?? {};
    const tab = this.data.payTab;
    const qr = tab === 'wx' ? cfg.pay_qr_wx : cfg.pay_qr_ali;
    const onlineEnabled = tab === 'wx' ? cfg.wxpay_enabled === '1' : cfg.alipay_enabled === '1';
    const hasQr = !!qr;

    this.setData({
      orderNo: String(order.orderNo || ''),
      shortNo: String(order.shortNo || order.orderNo || ''),
      totalText: money(order.totalAmount),
      totalValue: Number(order.totalAmount) || 0,
      statusText: ORDER_STATUS[Number(order.orderStatus)]?.label || '处理中',
      qrImage: mediaUrl(qr),
      hasQr,
      // 分三档：有码且开通在线支付 / 有码但未开通 / 没码
      chanTip: hasQr
        ? onlineEnabled
          ? '本店已开通在线支付。小程序内暂不能直接调起支付，请用微信「扫一扫 → 相册」识别上方收款码，或到前台结账。'
          : '请用微信「扫一扫 → 相册」识别上方收款码付款，或直接到前台结账。'
        : '本店未配置该渠道的收款码，请到前台结账。',
      paid: Number(order.payStatus) === 1,
      // 先付模式：给顾客一句「付完才开做」，避免以为已下厨
      payFirst: cfg.pay_first_enabled === '1',
      // 小程序内支付只有微信渠道有 JSAPI 通道；支付宝渠道仍走收款码
      canJsapi: tab === 'wx' && cfg.wxpay_jsapi_enabled === '1'
    });
    // 储值余额支付:登录会员且余额足够时出现(未登录/余额不足则不显示,不打扰匿名顾客)。
    void this.refreshWallet(Number(order.totalAmount) || 0);
  },

  /** 拉储值余额并判断是否够付当前订单。失败静默:按钮不出现,收款码路径照常可用。 */
  async refreshWallet(total: number): Promise<void> {
    // 未登录不发请求:储值余额接口需要会员令牌,匿名顾客拉了也只是 401 + 白白触发一次静默重登。
    if (memberState().customerId === 0) {
      this.setData({ canWallet: false, walletShort: false });
      return;
    }
    // 记下本次请求用的金额:在途请求返回时若已过期(订单金额变过),按最新值再补跑一次,
    // 否则 canWallet / mixedText 会按旧金额算,混合支付拆分显示错。
    this.walletTotal = total;
    // 在途守卫:render 末尾与切支付 tab 都会触发,同一时刻只放一份请求出去。
    if (this.walletLoading) return;
    this.walletLoading = true;
    try {
      const w = await getWallet();
      // 可用余额 = 本金 + 赠金:赠金不能退现,但买单和本金一样好使。
      const available = (Number(w.balance) || 0) + (Number(w.balanceGift) || 0);
      // 混合支付只在「有钱但不够」且小程序支付已开通时出现:
      // 没有 JSAPI 就没有补差通道,这时给按钮只会把人引到死路。
      const latest = this.walletTotal;
      const usable = Math.min(available, latest);
      const canMixed = latest > 0 && available > 0 && available < latest && this.data.canJsapi;
      this.setData({
        walletBalanceText: money(available),
        canWallet: available >= latest && latest > 0,
        canMixed,
        // 按分取整:展示与下单金额必须一致,不能出现「显示 10.005 元」。
        walletUsable: Math.floor(usable * 100) / 100,
        mixedText: canMixed ? `余额 ${money(usable)} + 微信 ${money(latest - usable)}` : '',
        // 有余额但不够付:提示「去充值」,比默默隐藏按钮更能让会员知道储值的存在。
        walletShort: available > 0 && available < latest && latest > 0
      });
    } catch {
      this.setData({ canWallet: false, canMixed: false, walletShort: false });
    } finally {
      this.walletLoading = false;
      // 在途期间订单金额又变过:按最新值补跑一次(最多一层,不会递归)
      if (this.walletTotal !== total) void this.refreshWallet(this.walletTotal);
    }
  },

  /**
   * 储值余额支付:后端在**同一个事务**里完成扣余额、落支付流水与订单入账
   * (储值没有回调重试兜底,事务是唯一的原子性保障)。
   * 成功后与 JSAPI 同一拍:以查单结果为准,再由订单页/出厨单接力。
   */
  async onPayWallet(): Promise<void> {
    if (this.data.paid || this.querying) return;
    const orderNo = this.data.orderNo;
    if (!orderNo) return;
    // 二次确认:微信支付有收银台这一步,余额支付却没有 —— 点一下就把钱扣了。
    // 少了它,同桌误触或手机被别人拿着时,扣款既无感知也无撤销入口。
    const confirmed = await new Promise<boolean>(resolve => {
      wx.showModal({
        title: '储值余额支付',
        content: `将使用储值余额支付 ${this.data.totalText} 元（当前余额 ${this.data.walletBalanceText} 元）`,
        confirmText: '确认支付',
        success: r => resolve(!!r.confirm),
        fail: () => resolve(false)
      });
    });
    if (!confirmed) return;
    this.querying = true;
    this.setData({ checking: true });
    try {
      await payOrderByWallet(orderNo);
      this.querying = false;
      const confirmed = await this.checkPaid(true);
      if (!confirmed) toast.info('支付结果确认中，请稍等');
    } catch (e) {
      this.querying = false;
      this.setData({ checking: false });
      toast.error(String((e as Error)?.message || '余额支付失败'));
    }
  },

  /**
   * 混合支付：余额抵扣 + 微信补差。
   *
   * 后端**先扣余额再返回调起参数**，所以顾客取消或拉起失败时必须立刻通知后端
   * 释放（pay-cancel），否则这笔钱要等 15 分钟的超时巡检才回来 ——
   * 对顾客来说那就是「点了取消钱却没了」。
   */
  async onPayMixed(): Promise<void> {
    if (this.data.paid || this.querying) return;
    const orderNo = this.data.orderNo;
    const useWallet = Number(this.data.walletUsable) || 0;
    const total = Number(this.data.totalValue) || 0;
    if (!orderNo || useWallet <= 0 || total <= 0) return;

    const confirmed = await new Promise<boolean>(resolve => {
      wx.showModal({
        title: '余额 + 微信支付',
        content: `将先扣储值余额 ${money(useWallet)} 元，剩余 ${money(total - useWallet)} 元用微信支付。`,
        confirmText: '去支付',
        success: r => resolve(!!r.confirm),
        fail: () => resolve(false)
      });
    });
    if (!confirmed) return;

    await this.doPayMixed(orderNo, useWallet, total, false);
  },

  /**
   * 执行一次混合支付。retried 用于防止「取消并重试」无限递归：
   * 重试后若仍被在途守卫拦下，只报错、不再弹取消确认。
   */
  async doPayMixed(orderNo: string, useWallet: number, total: number, retried: boolean): Promise<void> {
    this.querying = true;
    this.setData({ checking: true });
    try {
      const code = await this.loginCode();
      const res = await payMixedByWallet({ orderNo, code, walletAmount: useWallet });
      // 余额恰好够付清：后端已把订单置为已支付，不需要拉起收银台。
      if (res.paid) {
        this.querying = false;
        const confirmedPaid = await this.checkPaid(true);
        if (!confirmedPaid) toast.info('支付结果确认中，请稍等');
        return;
      }
      await this.requestPayment({
        timeStamp: res.timeStamp || '',
        nonceStr: res.nonceStr || '',
        package: res.package || '',
        signType: (res.signType || 'RSA') as 'MD5' | 'HMAC-SHA256' | 'RSA',
        paySign: res.paySign || ''
      });
      this.querying = false;
      const confirmedPaid = await this.checkPaid(true);
      if (!confirmedPaid) toast.info('支付结果确认中，请稍等');
    } catch (e) {
      this.querying = false;
      this.setData({ checking: false });
      const msg = String(
        (e as { errMsg?: string } | null)?.errMsg || (e as Error)?.message || ''
      );
      // 后端「在途支付」守卫：上一笔（如中途退出收银台）还扣着余额没释放。
      // 此时直接重发会被拒，顾客只能干等 15 分钟超时巡检 —— 引导他取消上一笔后立即重试。
      if (!retried && /已发起过混合支付/.test(msg)) {
        const again = await new Promise<boolean>(resolve => {
          wx.showModal({
            title: '上一笔支付未完成',
            content: '检测到上一笔支付还没有结果。是否取消上一笔（余额立即退回）并重新发起？',
            confirmText: '取消并重试',
            success: r => resolve(!!r.confirm),
            fail: () => resolve(false)
          });
        });
        if (again) {
          try {
            await cancelMixedByWallet(orderNo);
            await this.doPayMixed(orderNo, useWallet, total, true);
            return;
          } catch {
            toast.error('取消上一笔支付失败，请稍后再试');
          }
        }
        await this.refreshWallet(total);
        return;
      }
      if (/cancel/i.test(msg)) {
        try {
          await cancelMixedByWallet(orderNo);
          toast.info('已取消，储值余额已退回');
        } catch {
          toast.info('已取消，储值余额将稍后自动退回');
        }
      } else {
        // 非取消类失败（签名错误 / 参数非法 / 后端未开通）同样意味着「确定没付成」，
        // 但余额已被后端扣下：必须主动关单退回，不能只提示「稍后自动退回」——
        // 那要把余额锁到 15 分钟后的超时巡检，顾客看到的就是「钱少了」。
        try {
          await cancelMixedByWallet(orderNo);
          toast.info('支付未完成，储值余额已退回');
        } catch {
          toast.error('支付未完成，储值余额将稍后自动退回');
        }
      }
      await this.refreshWallet(total);
    }
  },

  onSwitchTab(e: WechatMiniprogram.TouchEvent): void {
    const tab = String(e.currentTarget.dataset.tab || 'wx') === 'ali' ? 'ali' : 'wx';
    if (tab === this.data.payTab) return;
    this.setData({ payTab: tab });
    const order = sessionState().currentOrder;
    if (order) this.render(order);
  },

  /**
   * 查一次支付状态；silent=true 时不弹错误（轮询场景）。
   * @returns 服务端是否已确认收款（JSAPI 付款成功后靠它判定「真到账」）
   */
  async checkPaid(silent: boolean): Promise<boolean> {
    const orderNo = this.data.orderNo;
    // querying 是手动点击与静默轮询**共用**的锁：两者并发返回会乱序，
    // 可能出现「刚提示已付款、紧接着又弹『尚未收到付款』」
    if (!orderNo || this.data.paid || this.querying) return !!this.data.paid;
    this.querying = true;
    if (!silent) this.setData({ checking: true });
    try {
      // silent 与入参一致：手动查询时 checkPaid 的 catch 有自己的提示，
      // 轮询路径（silent=true）则全程安静，不再由 http 层每 5s 弹一次 toast。
      const res = await queryPay(orderNo, { silent });
      if (Number(res?.payStatus) === 1) {
        // 先落「已付款」再回拉详情：详情失败也不能让顾客看到「还没付款」的假象
        this.poller?.stop();
        this.setData({ paid: true });
        // 与成功卡文案一致；7 字以内：带 success 图标的 toast 会被微信截断
        toast.success('支付成功');
        try {
          applyOrder((await getOrderByNo(orderNo, sessionState().tableId)) as OrderDetail);
        } catch {
          /* 详情回拉失败：订单页会按 orderNo 自己再拉一次 */
        }
        // 回订单页看出餐进度（存下 handle：页面提前卸载时要清掉，否则顾客已被强行拽回）
        this.leaveTimer = setTimeout(() => this.leaveToOrder(), 800);
        return true;
      }
      if (!silent) toast.info('尚未收到付款，请稍后再试');
      return false;
    } catch {
      if (!silent) toast.error('查询支付状态失败，请稍后重试');
      return false;
    } finally {
      this.querying = false;
      if (!silent) this.setData({ checking: false });
    }
  },

  onCheck(): void {
    // 连点会并发多个查询请求（querying 与轮询共用同一把锁）
    if (this.querying) return;
    void this.checkPaid(false);
  },

  /**
   * 小程序内支付（JSAPI）：`wx.login` 拿 code → 后端换 openid + JSAPI 下单并签好调起参数
   * → `wx.requestPayment` 拉起收银台。
   *
   * 与「查一次支付状态」共用 querying 锁：拉起支付期间轮询不该再来插一脚。
   * 后端未开通（缺 AppSecret / 商户号）时这一步会失败，此时降级提示 ——
   * 收款码与前台结账仍然可用，不能让顾客卡在支付页。
   */
  async onPayOnline(): Promise<void> {
    if (this.data.paid || this.querying) return;
    const orderNo = this.data.orderNo;
    if (!orderNo) return;

    this.querying = true;
    this.setData({ checking: true });
    try {
      const code = await this.loginCode();
      const params = await payJsapi({ orderNo, code });
      await this.requestPayment(params);
      // requestPayment 的 success 只代表「顾客完成了付款动作」，不代表服务端已入账
      // （微信回调有延迟）。一律以查单结果为准，否则会出现「顾客看到支付成功、
      // 订单仍是未支付」的错觉。先释放锁：checkPaid 与这里共用 querying。
      this.querying = false;
      const confirmed = await this.checkPaid(true);
      if (!confirmed) {
        // 没确认到就交给轮询继续跟（poller 未被停），顾客留在支付页等结果
        toast.info('支付结果确认中，请稍等');
      }
    } catch (e) {
      const msg = String((e as { errMsg?: string } | null)?.errMsg || '');
      if (/cancel/i.test(msg)) {
        toast.info('支付已取消');
      } else {
        // 后端没开通或下单失败：说清「还能怎么付」，别只报一句失败
        toast.error('小程序内支付暂不可用，请用上方收款码或到前台结账');
      }
    } finally {
      this.querying = false;
      this.setData({ checking: false });
    }
  },

  /** `wx.login` 换临时凭证（后端拿它去换 openid，5 分钟有效）。 */
  loginCode(): Promise<string> {
    return new Promise<string>((resolve, reject) => {
      wx.login({
        success: res => (res.code ? resolve(res.code) : reject(new Error('wx.login 未返回 code'))),
        fail: (e: unknown) => reject(e)
      });
    });
  },

  /** 拉起微信支付收银台。 */
  requestPayment(params: PayJsapiResult): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      wx.requestPayment({
        ...params,
        success: () => resolve(),
        fail: (e: unknown) => reject(e)
      });
    });
  },

  onCopyOrderNo(): void {
    if (!this.data.shortNo) return;
    // 成功时微信自带「内容已复制」提示,不再重复 toast;失败必须给出口,
    // 否则顾客点了复制毫无反应,还以为单号已经复制走了。
    wx.setClipboardData({
      data: this.data.shortNo,
      fail: () => toast.info('复制失败，请长按单号手动复制')
    });
  },

  onBack(): void {
    this.leaveToOrder();
  },

  /**
   * 回订单页看出餐进度。
   * 支付成功后有 800ms 的延时导航，顾客可能抢先手动返回 —— 标记位保证只跳一次。
   *
   * 切页策略交给 utils/nav（与订单页/菜单页同源）：栈里已有订单页就退回它，
   * 保留它的滚动位置与已加载数据；栈里没有才 redirectTo 打开。
   * 栈里没有订单页的情况（例如直接分享进本页），若无脑 navigateBack，
   * 顾客会被丢回上一屏，既看不到刚下的单，也看不出「付完才开做」的进度。
   */
  leaveToOrder(): void {
    if (this.leaving) return;
    this.leaving = true;
    goToOrder();
  }
});
