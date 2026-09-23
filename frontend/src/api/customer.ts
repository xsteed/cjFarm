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
} from '../types/entities';

// 顾客端
export const getTable = (id: Id): Promise<TableInfo> => http.get<unknown, TableInfo>(`/customer/table/${id}`);
export const getMenu = (): Promise<MenuCategory[]> => http.get<unknown, MenuCategory[]>('/customer/menu');
export const getRemarks = (): Promise<RemarkOption[]> => http.get<unknown, RemarkOption[]>('/customer/remarks');
export const createOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  http.post<unknown, OrderCreateResult>('/customer/order', data);
export const appendOrder = (data: OrderCreatePayload): Promise<OrderCreateResult> =>
  http.post<unknown, OrderCreateResult>('/customer/order/append', data);
export const getOrderByNo = (no: Id): Promise<OrderDetail> =>
  http.get<unknown, OrderDetail>(`/customer/order/no/${no}`);
// 顾客催菜:请求后厨加急(同一订单 3 分钟冷却,超频后端会返回剩余秒数)。
// 后端需校验订单归属桌台,故一并提交当前桌台的 tableId。
// config 可传 { silent: true }:冷却拒绝(HTTP 400 带结构化 cooldown)时不弹全局错误
// toast,由 useUrge 读取 data.cooldown 自行展示倒计时。
export const urgeOrder = (orderNo: Id, tableId: number, config?: { silent?: boolean }): Promise<UrgeResult> =>
  http.post<unknown, UrgeResult>('/customer/order/urge', { orderNo, tableId }, config);
export const getPayQr = (): Promise<PayQr> => http.get<unknown, PayQr>('/customer/pay/qr');
// 顾客端公开配置(无需登录):店铺名/餐位费/促销/收款码
export const getPublicConfig = (): Promise<PublicConfig> => http.get<unknown, PublicConfig>('/customer/config');

// 在线支付(微信/支付宝)
export const createPay = (data: PayCreatePayload): Promise<PayCreateResult> =>
  http.post<unknown, PayCreateResult>('/customer/pay/create', data);
export const queryPay = (orderNo: Id): Promise<PayQueryResult> =>
  http.get<unknown, PayQueryResult>('/customer/pay/query', { params: { orderNo } });
