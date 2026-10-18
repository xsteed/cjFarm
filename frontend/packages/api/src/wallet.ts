import { http } from './http';

// ============================================================================
// 储值(顾客自助 + 管理端)
//
// 顾客端走会员令牌(/customer/wallet/*),管理端走 member 权限体系
// (/admin/member/wallet*)。金额一律以「元」下发,与订单金额同一口径。
// ============================================================================

/** 储值账户(余额与累计口径,单位:元)。 */
export interface WalletInfo {
  balance: number;
  totalRecharge: number;
  totalConsume: number;
  totalGift: number;
}

/** 储值流水行(后端 dao.WalletTxnRow,amount 带符号:正入账/负出账)。 */
export interface WalletTxnRow {
  txnId: number;
  bizType: string;
  amount: number;
  balanceAfter: number;
  orderNo: string;
  reason: string;
  operator: string;
  createTime: string;
}

// ---- 顾客自助(需会员令牌) ----

export const getWallet = (): Promise<WalletInfo> => http.get<unknown, WalletInfo>('/customer/wallet');

export const listWalletTxns = (pageNum: number, pageSize = 10): Promise<{ total: number; items: WalletTxnRow[] }> =>
  http.get<unknown, { total: number; items: WalletTxnRow[] }>('/customer/wallet/txns', {
    params: { pageNum, pageSize }
  });

/** 发起充值:返回 wx.requestPayment 所需的调起参数 + 充值单号(轮询入账用)。 */
export interface WalletRechargeParams {
  rechargeNo: string;
  timeStamp: string;
  nonceStr: string;
  package: string;
  signType: 'MD5' | 'HMAC-SHA256' | 'RSA';
  paySign: string;
}

export const createWalletRecharge = (data: { amount: number; code: string }): Promise<WalletRechargeParams> =>
  http.post<unknown, WalletRechargeParams>('/customer/wallet/recharge', data);

/** 轮询充值是否已入账(status: 0 待支付 / 1 已入账)。 */
export const getWalletRechargeStatus = (rechargeNo: string): Promise<{ status: number; amount?: number }> =>
  http.get<unknown, { status: number; amount?: number }>('/customer/wallet/recharge/status', {
    params: { rechargeNo }
  });

/** 用储值余额支付订单。 */
export const payOrderByWallet = (orderNo: string): Promise<void> =>
  http.post<unknown, void>('/customer/wallet/pay', { orderNo });

// ---- 管理端(wallet:view / wallet:edit) ----

export const getMemberWallet = (
  customerId: number,
  pageNum = 1,
  pageSize = 20
): Promise<{ wallet: WalletInfo; total: number; items: WalletTxnRow[] }> =>
  http.get<unknown, { wallet: WalletInfo; total: number; items: WalletTxnRow[] }>('/admin/member/wallet', {
    params: { customerId, pageNum, pageSize }
  });

/** 后台调账:正数赠送(入账)、负数扣减(纠错),必须填原因。 */
export const adjustMemberWallet = (data: { customerId: number; amount: number; reason: string }): Promise<void> =>
  http.post<unknown, void>('/admin/member/wallet/adjust', data);

/** 流水业务类型的中文名(与后端 po.WalletTxn* 对应)。 */
export const WALLET_TXN_LABELS: Record<string, string> = {
  recharge: '充值',
  consume: '消费',
  refund: '退回',
  gift: '赠送',
  adjust: '调整'
};
