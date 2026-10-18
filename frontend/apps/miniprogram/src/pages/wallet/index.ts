// 我的钱包：余额（本金/赠金分桶）、充值（微信 JSAPI）、《储值规则》确认、储值流水。
//
// 相比上一版多了三件事，都来自合规要求（详见 docs/compliance/prepaid-card.md）：
//  1. 本金与赠金分开显示 —— 赠金不可退现，混在一起顾客会以为「余额 110」里
//     110 都是自己的钱，退款时必然产生纠纷；
//  2. 充值前必须阅读并同意《储值规则》，且实名（绑手机号）与否决定余额上限；
//  3. 进页面先触发一次「充值补账」—— 回调丢失时顾客已付款却没到账，
//     轮询只是等，补账才是真正把钱找回来的动作。
//
// 充值入口由管理端开关 `wallet_recharge_enabled` 控制（默认关，动钱的入口 fail-closed）：
// 开关关闭时充值区整块隐藏，但余额与流水照常展示 —— 那是顾客的钱，进得来就出得去。
import {
  agreeWalletRules,
  createWalletRecharge,
  getWallet,
  getWalletRechargeStatus,
  listWalletTxns,
  reconcileWalletRecharge
} from '../../api/client';
import type { WalletInfo, WalletTxnRow } from '../../api/client';
import { money } from '../../shared/money';
import { sessionState } from '../../store/session';
import { goToProfile } from '../../utils/nav';
import { toast } from '../../utils/toast';

/** 流水行：WXML 不支持函数调用，展示串都在这里算好。 */
interface TxnRow {
  txnId: number;
  typeText: string;
  amountText: string;
  amountClass: string;
  balanceText: string;
  /** 本笔含赠金的说明（如「含赠金 10.00」），为 0 时为空串。 */
  giftText: string;
  orderNo: string;
  timeText: string;
}

const PAGE_SIZE = 10;

Page({
  data: {
    loading: true,
    error: '',
    /** 储值账户（金额均为元）。 */
    info: {
      balance: 0,
      balanceGift: 0,
      totalRecharge: 0,
      totalConsume: 0,
      totalGift: 0,
      named: false,
      rulesVer: '',
      rulesText: '',
      agreed: false,
      canRecharge: false,
      realnameRequired: true,
      rechargeMin: 1,
      rechargeMax: 1000,
      balanceMax: 1000
    } as WalletInfo,
    balanceText: '0.00',
    giftText: '0.00',
    totalRechargeText: '0.00',
    totalConsumeText: '0.00',
    totalGiftText: '0.00',
    /** 是否有赠金余额（有才展示赠金卡，避免给没有赠金的顾客看一个恒为 0 的数）。 */
    hasGift: false,
    /** 充值区是否可用（后端开关）。 */
    canRecharge: false,
    /** 未同意《储值规则》时充值区只显示「阅读并同意」按钮。 */
    agreed: false,
    /** 强制实名但未绑手机号：提示去「我的」绑定。 */
    needRealname: false,
    /** 规则正文按行拆好（WXML 不支持 split）。 */
    rulesLines: [] as string[],
    /** 规则全文是否展开。 */
    rulesOpen: false,
    /** 充值限额提示（单笔区间 + 余额上限）。 */
    limitTip: '',
    /** 选中的充值金额（元）；-1 表示选了「自定义」。 */
    preset: 100,
    customAmount: '',
    /** 充值流程进行中（防重复拉起收银台）。 */
    recharging: false,
    txns: [] as TxnRow[],
    total: 0,
    page: 1,
    hasMore: false,
    loadingMore: false
  },

  onShow() {
    // 先补账再刷新：补账可能把「顾客已付款但回调没到」的钱找回来，
    // 刷新顺序颠倒的话顾客会先看到旧余额，产生「钱丢了」的错觉。
    void this.reconcileThenReload();
  },

  /** 补账（静默，失败不影响页面）→ 刷新余额与流水。 */
  async reconcileThenReload(): Promise<void> {
    try {
      const r = await reconcileWalletRecharge();
      if (r.credited > 0) {
        // 找回了钱才提示；绝大多数调用是 0 笔，不该打扰顾客。
        toast.success('充值已到账');
      }
    } catch {
      // 补账是尽力而为：渠道查单失败/未开通支付都不该挡住顾客看余额。
    }
    await this.reload();
  },

  async reload(): Promise<void> {
    this.setData({ loading: true, error: '' });
    try {
      const [info, txns] = await Promise.all([getWallet(), listWalletTxns(1, PAGE_SIZE)]);
      // 双重 fail-closed：后端开关 + 微信 JSAPI 已开通，缺任一项都不给入口。
      // 只看后端开关会出现「点了却拉不起收银台」，只看前端配置又会绕过后端的额度校验。
      const canRecharge = info.canRecharge && (sessionState().config ?? {}).wxpay_jsapi_enabled === '1';
      this.setData({
        loading: false,
        info,
        balanceText: money(info.balance),
        giftText: money(info.balanceGift),
        // 累计列此前直接渲染数字（1000 而非 1000.00），与其它金额口径不一致。
        totalRechargeText: money(info.totalRecharge),
        totalConsumeText: money(info.totalConsume),
        totalGiftText: money(info.totalGift),
        hasGift: info.balanceGift > 0,
        canRecharge,
        agreed: info.agreed,
        needRealname: canRecharge && info.realnameRequired && !info.named,
        rulesLines: String(info.rulesText || '')
          .split('\n')
          .filter((l: string) => l.trim() !== ''),
        limitTip: canRecharge
          ? `单笔 ${info.rechargeMin} ~ ${info.rechargeMax} 元；账户余额上限 ${info.balanceMax} 元` +
            (info.named ? '（已实名）' : '（绑定手机号后提高）')
          : '',
        txns: txns.items.map(toRow),
        total: txns.total,
        page: 1,
        hasMore: txns.items.length < txns.total
      });
    } catch (e) {
      this.setData({ loading: false, error: (e as Error)?.message || '加载失败，请重试' });
    }
  },

  onRetry(): void {
    void this.reconcileThenReload();
  },

  onToggleRules(): void {
    this.setData({ rulesOpen: !this.data.rulesOpen });
  },

  /** 去「我的」绑定手机号（实名后才能提高余额上限）。 */
  onGoBindPhone(): void {
    goToProfile();
  },

  /** 选预设金额。 */
  onPickPreset(e: WechatMiniprogram.TouchEvent): void {
    const amount = Number(e.currentTarget.dataset.amount || 0);
    if (!amount) return;
    this.setData({ preset: amount, customAmount: '' });
  },

  /** 输入自定义金额后取消预设选中。 */
  onCustomInput(e: WechatMiniprogram.Input): void {
    this.setData({ customAmount: String(e.detail.value || ''), preset: -1 });
  },

  /**
   * 发起充值：同意规则 → wx.login → JSAPI 下单 → wx.requestPayment → 轮询入账 → 刷新。
   *
   * 规则未同意时先弹确认（合规：发卡须明示章程），确认才落同意留痕并继续。
   */
  async onRecharge(): Promise<void> {
    if (this.data.recharging) return;
    if (!this.data.agreed) {
      void this.agreeRules();
      return;
    }
    const amount = this.data.preset > 0 ? this.data.preset : Math.floor(Number(this.data.customAmount) || 0);
    if (!amount || amount <= 0) {
      toast.info('请选择或输入充值金额');
      return;
    }
    this.setData({ recharging: true });
    try {
      const code = await new Promise<string>((resolve, reject) => {
        wx.login({ success: r => resolve(r.code), fail: () => reject(new Error('微信登录失败，请重试')) });
      });
      const params = await createWalletRecharge({ amount, code });
      await new Promise<void>((resolve, reject) => {
        wx.requestPayment({
          timeStamp: params.timeStamp,
          nonceStr: params.nonceStr,
          package: params.package,
          signType: params.signType as 'MD5' | 'HMAC-SHA256' | 'RSA',
          paySign: params.paySign,
          success: () => resolve(),
          fail: () => reject(new Error('支付未完成'))
        });
      });
      // 轮询入账状态：回调偶尔比前端慢半拍，最多等 5 秒。
      let paid = false;
      for (let i = 0; i < 10; i++) {
        await new Promise(r => setTimeout(r, 500));
        try {
          const st = await getWalletRechargeStatus(params.rechargeNo);
          if (st.status === 1) {
            paid = true;
            break;
          }
        } catch {
          break;
        }
      }
      if (!paid) {
        // 轮询没等到 ≠ 没到账：让后端直接去渠道查一次，把钱找回来。
        // 这一步是「已付款却没入账」的唯一自救通道，不能只留一句「请稍后」。
        try {
          const r = await reconcileWalletRecharge();
          paid = r.credited > 0;
        } catch {
          /* 补账失败按未到账处理，下面给可操作的提示 */
        }
      }
      if (paid) {
        toast.success('充值成功');
      } else {
        toast.info('支付结果确认中，请下拉刷新查看');
      }
      await this.reload();
    } catch (e) {
      const msg = (e as Error)?.message;
      if (msg && msg !== '支付未完成') toast.error(msg);
    } finally {
      this.setData({ recharging: false });
    }
  },

  /** 展示《储值规则》全文并请顾客确认（确认后才落同意留痕）。 */
  agreeRules(): void {
    const info = this.data.info;
    wx.showModal({
      title: '储值规则',
      // 规则全文可能较长，弹窗只放前两行 + 引导展开看全文；
      // 完整正文在页面里已展开，顾客随时可查。
      content: `${info.rulesText || ''}\n\n同意后方可充值，本店将保留你的同意记录。`,
      confirmText: '同意',
      cancelText: '再看看',
      success: r => {
        if (!r.confirm) return;
        void this.doAgreeRules();
      }
    });
  },

  async doAgreeRules(): Promise<void> {
    try {
      await agreeWalletRules(this.data.info.rulesVer);
      this.setData({ agreed: true });
      toast.success('已同意储值规则');
      await this.reload();
    } catch (e) {
      toast.error(String((e as Error)?.message || '保存失败，请重试'));
    }
  },

  async onLoadMore(): Promise<void> {
    if (this.data.loadingMore || !this.data.hasMore) return;
    this.setData({ loadingMore: true });
    try {
      const next = this.data.page + 1;
      const res = await listWalletTxns(next, PAGE_SIZE);
      const merged = this.data.txns.concat(res.items.map(toRow));
      this.setData({
        loadingMore: false,
        txns: merged,
        page: next,
        hasMore: merged.length < res.total
      });
    } catch (e) {
      this.setData({ loadingMore: false, error: (e as Error)?.message || '加载失败，请重试' });
    }
  }
});

function toRow(t: WalletTxnRow): TxnRow {
  const positive = Number(t.amount) >= 0;
  const gift = Math.abs(Number(t.giftAmount) || 0);
  return {
    txnId: t.txnId,
    typeText: TXN_LABELS[t.bizType] || t.bizType,
    amountText: (positive ? '+' : '') + money(t.amount),
    amountClass: positive ? 'up' : 'down',
    balanceText: money(t.balanceAfter),
    // 消费流水里含赠金时说清楚，否则顾客会以为本金被多扣了。
    giftText: gift > 0 ? `含赠金 ${money(gift)}` : '',
    orderNo: String(t.orderNo || ''),
    timeText: String(t.createTime || '')
      .replace('T', ' ')
      .slice(0, 16)
  };
}

const TXN_LABELS: Record<string, string> = {
  recharge: '充值',
  consume: '消费',
  refund: '退回',
  gift: '赠送',
  gift_void: '赠金作废',
  gift_back: '赠金退回',
  adjust: '调整',
  withdraw: '退余额'
};
