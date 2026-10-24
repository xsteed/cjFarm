// ============================================================================
// 接口装配 + 全部顾客端端点（自包含，不依赖 @dining/api）
//
// 本文件在「模块加载期」完成两件事（被任意页面 import 即触发，仅一次）：
//   1) configureHttp：把 baseURL 设成「后端 origin + /api」，超时 15s；
//   2) setUiNotifier：把逻辑层的 error/warning/success 接到小程序的 wx.showToast。
// app.ts 里 `import './api/client'` 正是为了这个副作用在请求发出前完成装配。
// ============================================================================
import { httpGet, httpUploadFile, httpPost, configureHttp } from './http';
import { setUiNotifier } from '../shared/ui';
import { API_BASE_URL, REQUEST_TIMEOUT_MS } from '../config';
import type {
  CustomerReviewStatus,
  Id,
  MenuCategory,
  OrderCreatePayload,
  OrderCreateResult,
  ReviewCreatePayload,
  OrderDetail,
  PayQueryResult,
  PublicConfig,
  RemarkOption,
  TableInfo,
  UrgeResult,
  MemberProvider,
  MemberInfo,
  MemberLoginResult,
  MemberLoginPayload,
  MemberOrderRow,
  WalletInfo,
  WalletMixedPayResult,
  WalletRechargeParams,
  WalletTxnRow,
  VisitTrackPayload,
  OrderPersonPayload,
  PayJsapiPayload,
  PayJsapiResult
} from '../types';

// 装配：baseURL 指向后端 origin（单源在 config.ts，避免此处再拼一遍字符串）。
configureHttp({ baseURL: API_BASE_URL, timeoutMs: REQUEST_TIMEOUT_MS });

// 通知落地：逻辑层只调 ui.error/success，由这里决定小程序怎么呈现。
setUiNotifier({
  success: (message: string) => wx.showToast({ title: message, icon: 'success' }),
  warning: (message: string) => wx.showToast({ title: message, icon: 'none' }),
  error: (message: string) => wx.showToast({ title: message, icon: 'none' })
});

// 顾客端
export type { PayJsapiPayload, PayJsapiResult, WalletInfo, WalletMixedPayResult, WalletRechargeParams, WalletTxnRow };
export const getTable = (id: Id): Promise<TableInfo> => httpGet<TableInfo>(`/customer/table/${id}`);
export const setTablePerson = (data: { tableId: number; personCount: number }): Promise<void> =>
  httpPost<void>('/customer/table/person', data);
export const getMenu = (): Promise<MenuCategory[]> => httpGet<MenuCategory[]>('/customer/menu');
export const getRemarks = (): Promise<RemarkOption[]> => httpGet<RemarkOption[]>('/customer/remarks');

export const createOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  httpPost<OrderCreateResult>('/customer/order', data);
export const appendOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  httpPost<OrderCreateResult>('/customer/order/append', data);

// 按单号查订单详情。带当前桌台 tableId 时后端校验归属，防「凭单号遍历查他人订单」。
// config.silent：轮询路径传 true，网络失败不弹全局 toast（由 poller 静默重试）。
export const getOrderByNo = (no: Id, tableId?: number, config?: { silent?: boolean }): Promise<OrderDetail> =>
  httpGet<OrderDetail>(`/customer/order/no/${no}${tableId ? `?tableId=${tableId}` : ''}`, undefined, config);

// 会员自己的历史订单详情（跨桌、需登录，令牌过期由 401 兜底静默重登后重放）。
export const getMyOrderDetail = (no: Id): Promise<OrderDetail> =>
  httpGet<OrderDetail>(`/customer/order/mine/${encodeURIComponent(String(no))}`);

// 顾客催菜：请求后厨加急（同单 3 分钟冷却，超频后端回传剩余秒数）。
// config 可传 { silent: true }：冷却拒绝时不弹全局错误，由调用方读 data.cooldown 自行展示。
export const urgeOrder = (orderNo: Id, tableId: number, config?: { silent?: boolean }): Promise<UrgeResult> =>
  httpPost<UrgeResult>('/customer/order/urge', { orderNo, tableId }, config);

// 改就餐人数（不改菜品）：人数决定茶位费，后端重算金额。只允许「进行中且未支付」的订单。
export const updateOrderPerson = (data: OrderPersonPayload): Promise<OrderDetail> =>
  httpPost<OrderDetail>('/customer/order/person', data);

// 顾客端公开配置（无需登录）：店铺名 / 茶位费 / 促销 / 收款码。
export const getPublicConfig = (): Promise<PublicConfig> => httpGet<PublicConfig>('/customer/config');

// 在线支付只走小程序内 JSAPI（见下方 payJsapi）。Native 扫码那套（/customer/pay/qr、
// /customer/pay/create）是给 H5 用的，小程序里「自己扫自己」没有意义 ——
// 此前拷贝过来的 getPayQr / createPay 无任何调用方，已删。
export const queryPay = (orderNo: Id, config?: { silent?: boolean }): Promise<PayQueryResult> =>
  httpGet<PayQueryResult>('/customer/pay/query', { orderNo }, config);

// ---------------------------------------------------------------------------
// 小程序内支付（JSAPI）：服务端拿 wx.login 的 code 换 openid，签好调起参数，
// 小程序拿着参数直接 wx.requestPayment。后端未配商户号时返回业务错误，调用方据此
// 降级到「收款码 + 前台结账」。
// ---------------------------------------------------------------------------
export const payJsapi = (data: PayJsapiPayload): Promise<PayJsapiResult> =>
  httpPost<PayJsapiResult>('/customer/pay/jsapi', data);

// ---------------------------------------------------------------------------
// 顾客会员（账号 / 登录方式分离）：provider 只决定「凭据怎么换成身份」，响应体与令牌
// 完全一致。小程序传 wx.login 的 code，网站/App 后续传手机号或账号密码即可。
// ---------------------------------------------------------------------------
export type { MemberProvider, MemberInfo, MemberLoginResult, MemberLoginPayload, MemberOrderRow };

export const memberLogin = (data: MemberLoginPayload): Promise<MemberLoginResult> =>
  httpPost<MemberLoginResult>('/customer/auth/login', data);
export const memberProfile = (): Promise<MemberInfo> => httpGet<MemberInfo>('/customer/auth/profile');
export const memberOrders = (pageNum: number, pageSize = 10): Promise<{ total: number; items: MemberOrderRow[] }> =>
  httpGet<{ total: number; items: MemberOrderRow[] }>('/customer/order/mine', { pageNum, pageSize });
export const updateMemberProfile = (data: { nickname: string }): Promise<MemberInfo> =>
  httpPost<MemberInfo>('/customer/auth/profile', data);
export const deleteMemberAccount = (): Promise<void> => httpPost<void>('/customer/auth/account');
export const bindMemberPhone = (code: string): Promise<{ phone: string }> =>
  httpPost<{ phone: string }>('/customer/auth/phone', { code });

// ---------------------------------------------------------------------------
// 储值（顾客自助，需会员令牌）
// ---------------------------------------------------------------------------
export const getWallet = (): Promise<WalletInfo> => httpGet<WalletInfo>('/customer/wallet');
export const listWalletTxns = (pageNum: number, pageSize = 10): Promise<{ total: number; items: WalletTxnRow[] }> =>
  httpGet<{ total: number; items: WalletTxnRow[] }>('/customer/wallet/txns', { pageNum, pageSize });
export const createWalletRecharge = (data: { amount: number; code: string }): Promise<WalletRechargeParams> =>
  httpPost<WalletRechargeParams>('/customer/wallet/recharge', data);
export const getWalletRechargeStatus = (
  rechargeNo: string,
  config?: { silent?: boolean }
): Promise<{ status: number; amount?: number }> =>
  httpGet<{ status: number; amount?: number }>('/customer/wallet/recharge/status', { rechargeNo }, config);
/**
 * 充值补账：回调丢失时让后端主动向渠道查单。
 * 顾客微信已扣款、余额却没到账是纯资损，这条通道是唯一的自救手段。
 */
export const reconcileWalletRecharge = (): Promise<{ credited: number }> =>
  httpPost<{ credited: number }>('/customer/wallet/recharge/reconcile', {});
/** 同意《储值规则》（合规：发卡须明示章程并留痕）。 */
export const agreeWalletRules = (rulesVer: string): Promise<void> =>
  httpPost<void>('/customer/wallet/rules/agree', { rulesVer });
export const payOrderByWallet = (orderNo: string): Promise<void> => httpPost<void>('/customer/wallet/pay', { orderNo });
/**
 * 混合支付：先用余额抵扣 walletAmount（元），不足部分返回微信支付调起参数。
 * 后端会先扣余额再出参，调用方必须处理「拿到参数后拉起失败」的回退
 * （页面上的提示语已说明余额会自动退回）。
 */
export const payMixedByWallet = (data: {
  orderNo: string;
  code: string;
  walletAmount: number;
}): Promise<WalletMixedPayResult> => httpPost<WalletMixedPayResult>('/customer/wallet/pay-mixed', data);
/** 取消混合支付：立刻把已扣的余额退回（后端先关渠道单，关单成功才释放）。 */
export const cancelMixedByWallet = (orderNo: string): Promise<void> =>
  httpPost<void>('/customer/wallet/pay-cancel', { orderNo });

// ---------------------------------------------------------------------------
// 访问统计打点：挂在顾客公开组下，故意不带登录态拦截（匿名打点天然干净）；
// 失败时绝不能打扰点餐的顾客，故显式 silent，由调用方 .catch 静默消化。
// ---------------------------------------------------------------------------
// 订单评价(仅商家可见的内部反馈):匿名与会员同入口,会员令牌自动带上身份。
// 读写都挂后端 IP 限流;「已评价」重复提交是幂等错误(HTTP 400 + reviewed:true)。
// reviewerKey=匿名设备键(visitorId):「我是否已评」按设备判定;会员不带键。
export const getReviewStatus = (orderNo: Id, tableId: number, reviewerKey?: string, config?: { silent?: boolean }): Promise<CustomerReviewStatus> =>
  httpGet<CustomerReviewStatus>(
    `/customer/review?orderNo=${encodeURIComponent(String(orderNo))}&tableId=${tableId}${reviewerKey ? `&reviewerKey=${encodeURIComponent(reviewerKey)}` : ''}`,
    config
  );
export const createReview = (data: ReviewCreatePayload, config?: { silent?: boolean }): Promise<CustomerReviewStatus> =>
  httpPost<CustomerReviewStatus>('/customer/review', data, config);
// 评价图片上传(wx.uploadFile):返回 /uploads/{name},提交时取尾段文件名绑定。
export const uploadReviewImage = (filePath: string, config?: { silent?: boolean }): Promise<{ url: string }> =>
  httpUploadFile<{ url: string }>('/customer/review/upload', filePath, 'file', config);
export const reportVisit = (payload: VisitTrackPayload): Promise<unknown> =>
  httpPost<unknown>('/customer/visit', payload, { silent: true });
