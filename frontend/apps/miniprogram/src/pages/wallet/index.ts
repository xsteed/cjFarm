// 我的钱包：余额、充值（微信 JSAPI）、储值流水。
//
// 充值入口由管理端开关 `wallet_recharge_enabled` 控制（默认关,动钱的入口 fail-closed）：
// 开关关闭时充值区整块隐藏,但余额与流水照常展示 —— 那是顾客的钱,进得来就出得去。
// 充值链路与订单支付共用同一个回调地址,这里轮询入账状态只为即时刷新余额。
import { createWalletRecharge, getWallet, getWalletRechargeStatus, listWalletTxns } from '../../api/client';
import type { WalletInfo, WalletTxnRow } from '../../api/client';
import { money } from '@dining/shared/money';
import { sessionState } from '../../store/session';
import { toast } from '../../utils/toast';

/** 流水行：WXML 不支持函数调用，展示串都在这里算好。 */
interface TxnRow {
  txnId: number;
  typeText: string;
  amountText: string;
  amountClass: string;
  balanceText: string;
  orderNo: string;
  timeText: string;
}

const PAGE_SIZE = 10;

Page({
  data: {
    loading: true,
    error: '',
    /** 储值账户（金额均为元）。 */
    info: { balance: 0, totalRecharge: 0, totalConsume: 0, totalGift: 0 } as WalletInfo,
    balanceText: '0.00',
    /** 充值区是否可用（管理端开关 + 微信支付已开通的公开配置）。 */
    canRecharge: false,
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
    void this.reload();
  },

  async reload(): Promise<void> {
    this.setData({ loading: true, error: '' });
    try {
      const [info, txns] = await Promise.all([getWallet(), listWalletTxns(1, PAGE_SIZE)]);
      this.setData({
        loading: false,
        info,
        balanceText: money(info.balance),
        canRecharge: this.rechargeEnabled(),
        txns: txns.items.map(toRow),
        total: txns.total,
        page: 1,
        hasMore: txns.items.length < txns.total
      });
    } catch (e) {
      this.setData({ loading: false, error: (e as Error)?.message || '加载失败，请重试' });
    }
  },

  /** 充值开关：管理端配置 + 微信支付已开通（缺任一项都不给入口，fail-closed）。 */
  rechargeEnabled(): boolean {
    const cfg = sessionState().config ?? {};
    return cfg.wallet_recharge_enabled === '1' && cfg.wxpay_jsapi_enabled === '1';
  },

  onRetry(): void {
    void this.reload();
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

  /** 发起充值：wx.login → JSAPI 下单 → wx.requestPayment → 轮询入账 → 刷新。 */
  async onRecharge(): Promise<void> {
    if (this.data.recharging) return;
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
      // 轮询入账状态：回调偶尔比前端慢半拍，最多等 5 秒；
      // 钱的入账以回调为准，轮询只是让「刚充的钱」立刻体现在余额里。
      for (let i = 0; i < 10; i++) {
        await new Promise(r => setTimeout(r, 500));
        try {
          const st = await getWalletRechargeStatus(params.rechargeNo);
          if (st.status === 1) break;
        } catch {
          break;
        }
      }
      toast.success('充值成功');
      await this.reload();
    } catch (e) {
      const msg = (e as Error)?.message;
      if (msg && msg !== '支付未完成') toast.error(msg);
    } finally {
      this.setData({ recharging: false });
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
  return {
    txnId: t.txnId,
    typeText: TXN_LABELS[t.bizType] || t.bizType,
    amountText: (positive ? '+' : '') + money(t.amount),
    amountClass: positive ? 'up' : 'down',
    balanceText: money(t.balanceAfter),
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
  adjust: '调整'
};
