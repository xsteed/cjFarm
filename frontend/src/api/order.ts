import { http } from './http';
import type { ApiResponse, PageQuery, PageResult } from '../types/api';
import type {
  Id,
  OrderActionPayload,
  OrderDetail,
  OrderEditPayload,
  OrderSummary,
  PrintCommandPayload,
  Refund,
  RefundPayload,
  RefundQueryResult,
  TableInfo,
  Urge
} from '../types/entities';

// 订单
export const listOrders = (params?: PageQuery): Promise<PageResult<OrderSummary>> =>
  http.get<unknown, PageResult<OrderSummary>>('/admin/order/list', { params });
export const getOrder = (id: Id): Promise<OrderDetail> => http.get<unknown, OrderDetail>(`/admin/order/${id}`);
// 看板:所有桌台 + 每桌当前进行中订单(后端返回桌台数组,含 order 字段)
export const getOrderBoard = (): Promise<TableInfo[]> => http.get<unknown, TableInfo[]>('/admin/order/board');
// 催菜(商家端):催菜列表与处理,用于接单/上菜前查看哪些桌在催
export const listUrges = (params?: PageQuery): Promise<PageResult<Urge>> =>
  http.get<unknown, PageResult<Urge>>('/admin/order/urge/list', { params });
export const handleUrge = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/urge/handle', data);
export const changeOrderStatus = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/status', data);
export const payOrder = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/pay', data);
// 结账:支持正常收款 / 免单 / 挂账(settleType: normal | free | credit)
export const settleOrder = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/settle', data);
// 挂账核销:补收挂账欠款
export const settleCreditOrder = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/credit/settle', data);
// 撤销结算:把已免单/已挂账的订单退回未支付
export const cancelSettle = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/settle/cancel', data);
export const finishOrder = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/finish', data);
export const cancelOrder = (data: OrderActionPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/cancel', data);
export const editOrder = (data: OrderEditPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/edit', data);
export const refundPay = (data: RefundPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/pay/refund', data);
export const queryRefund = (data: RefundPayload): Promise<RefundQueryResult> =>
  http.post<unknown, RefundQueryResult>('/admin/pay/refund/query', data);
export const listRefunds = (orderId: Id): Promise<Refund[]> =>
  http.get<unknown, Refund[]>('/admin/pay/refund/list', { params: { orderId } });
// 按订单补打;不传 printerId 时后端自动挑一台该类型的启用打印机
export const reprintOrder = (data: PrintCommandPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/order/reprint', data, { timeout: 60000 });
