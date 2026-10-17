import { http } from './http';
import type {
  Id,
  MenuCategory,
  OrderCreatePayload,
  OrderCreateResult,
  OrderDetail,
  PayCreatePayload,
  PayCreateResult,
  PayQr,
  PayQueryResult,
  PublicConfig,
  RemarkOption,
  TableInfo,
  UrgeResult
} from '@dining/types';

// 顾客端
export const getTable = (id: Id): Promise<TableInfo> => http.get<unknown, TableInfo>(`/customer/table/${id}`);
export const getMenu = (): Promise<MenuCategory[]> => http.get<unknown, MenuCategory[]>('/customer/menu');
export const getRemarks = (): Promise<RemarkOption[]> => http.get<unknown, RemarkOption[]>('/customer/remarks');
export const createOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  http.post<unknown, OrderCreateResult>('/customer/order', data);
export const appendOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  http.post<unknown, OrderCreateResult>('/customer/order/append', data);
// 按单号查订单详情。带当前桌台 tableId 时,后端校验订单归属 ——
// 单号会出现在小票/照片/群聊里,无鉴权接口必须防「凭单号遍历查他人订单」。
// 老二维码链接未带桌台参数时可省略(后端退化为仅按单号查询 + IP 限流)。
export const getOrderByNo = (no: Id, tableId?: number): Promise<OrderDetail> =>
  http.get<unknown, OrderDetail>(`/customer/order/no/${no}${tableId ? `?tableId=${tableId}` : ''}`);
// 顾客催菜:请求后厨加急(同一订单 3 分钟冷却,超频后端会返回剩余秒数)。
// 后端需校验订单归属桌台,故一并提交当前桌台的 tableId。
// config 可传 { silent: true }:冷却拒绝(HTTP 400 带结构化 cooldown)时不弹全局错误
// toast,由 useUrge 读取 data.cooldown 自行展示倒计时。
export const urgeOrder = (orderNo: Id, tableId: number, config?: { silent?: boolean }): Promise<UrgeResult> =>
  http.post<unknown, UrgeResult>('/customer/order/urge', { orderNo, tableId }, config);
export const getPayQr = (): Promise<PayQr> => http.get<unknown, PayQr>('/customer/pay/qr');
// 顾客端公开配置(无需登录):店铺名/茶位费/促销/收款码
export const getPublicConfig = (): Promise<PublicConfig> => http.get<unknown, PublicConfig>('/customer/config');

// 在线支付(微信/支付宝)
export const createPay = (data: PayCreatePayload): Promise<PayCreateResult> =>
  http.post<unknown, PayCreateResult>('/customer/pay/create', data);
export const queryPay = (orderNo: Id): Promise<PayQueryResult> =>
  http.get<unknown, PayQueryResult>('/customer/pay/query', { params: { orderNo } });

// ---------------------------------------------------------------------------
// 小程序内支付（JSAPI）
//
// 与 createPay（Native 扫码）的区别：Native 返回 code_url，小程序里不能「自己扫自己」，
// 所以这里走 JSAPI 下单 —— 由服务端拿 wx.login 的 code 换 openid，下单后签好调起参数，
// 小程序拿着参数直接 wx.requestPayment。
//
// 后端未启用（没配 AppSecret / 商户号）时该接口会返回业务错误，
// 调用方据此降级到「收款码 + 前台结账」，不要让顾客卡在支付页。
// ---------------------------------------------------------------------------
export interface PayJsapiPayload {
  orderNo?: string;
  /** wx.login 拿到的临时登录凭证（5 分钟有效，一次性） */
  code?: string;
}

/**
 * 调起参数，字段名与 wx.requestPayment 的入参一致，前端可原样透传。
 * signType 收窄成 wx 声明的联合类型，免得调用方再做一次断言。
 */
export interface PayJsapiResult {
  timeStamp: string;
  nonceStr: string;
  /** 形如 "prepay_id=wx..." */
  package: string;
  signType: 'MD5' | 'HMAC-SHA256' | 'RSA';
  paySign: string;
}

export const payJsapi = (data: PayJsapiPayload): Promise<PayJsapiResult> =>
  http.post<unknown, PayJsapiResult>('/customer/pay/jsapi', data);
