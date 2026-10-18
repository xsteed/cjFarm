import { http } from './http';

// ============================================================================
// 储值(顾客自助 + 管理端)
//
// 顾客端走会员令牌(/customer/wallet/*),管理端走 member 权限体系
// (/admin/member/wallet*)。金额一律以「元」下发,与订单金额同一口径。
// ============================================================================

/**
 * 储值账户(余额与累计口径,单位:元)。
 *
 * 本金与赠金分桶:balance 可退、balanceGift 只能消费。
 * 退款时只退本金,所以两个桶在管理端也必须分开显示。
 */
export interface WalletInfo {
  /** 本金余额(可退)。 */
  balance: number;
  /** 赠金余额(不可退现)。 */
  balanceGift: number;
  totalRecharge: number;
  totalConsume: number;
  totalGift: number;
  /** 是否已实名(绑手机号):决定余额上限走不记名还是记名额度。 */
  named: boolean;
  rulesVer: string;
  rulesText: string;
  agreed: boolean;
  canRecharge: boolean;
  realnameRequired: boolean;
  rechargeMin: number;
  rechargeMax: number;
  balanceMax: number;
}

/** 储值流水行(后端 dao.WalletTxnRow,amount 带符号:正入账/负出账)。 */
export interface WalletTxnRow {
  txnId: number;
  bizType: string;
  amount: number;
  balanceAfter: number;
  /** 本笔变动中属于赠金桶的部分(带符号)。 */
  giftAmount: number;
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

/**
 * 退余额:把会员储值余额清零,门店据此线下付款给顾客。
 *
 * 合规落点 —— 顾客的预付款必须拿得回去。principal 是真要退给顾客的钱,
 * gift 是被作废的赠金(门店白送的,不产生现金流出)。
 */
export interface WalletWithdrawResult {
  principal: number;
  gift: number;
  msg: string;
}

export const withdrawMemberWallet = (
  data: { customerId: number; reason: string }
): Promise<WalletWithdrawResult> =>
  http.post<unknown, WalletWithdrawResult>('/admin/member/wallet/withdraw', data);

/** 流水业务类型的中文名(与后端 po.WalletTxn* 对应)。 */
export const WALLET_TXN_LABELS: Record<string, string> = {
  recharge: '充值',
  consume: '消费',
  refund: '退回',
  gift: '赠送',
  gift_void: '赠金作废',
  gift_back: '赠金退回',
  adjust: '调整',
  withdraw: '退余额'
};
