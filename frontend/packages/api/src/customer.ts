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
  CustomerReviewStatus,
  ReviewCreatePayload,
  TableInfo,
  UrgeResult
} from '@dining/types';

// 顾客端
export const getTable = (id: Id): Promise<TableInfo> => http.get<unknown, TableInfo>(`/customer/table/${id}`);
// 入座选人数上报（茶位费门店）：同桌共享，其他设备扫码进来直接沿用、不再重复选。
// 调用方 fire-and-forget，失败静默 —— 它只影响「别人免选」的体验，绝不阻断点餐。
export const setTablePerson = (data: { tableId: number; personCount: number }): Promise<void> =>
  http.post<unknown, void>('/customer/table/person', data);
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
// 会员自己的历史订单详情(跨桌,含明细)。与 getOrderByNo 的区别:
//  ・归属只认顾客令牌(customer_id),不能带当前 tableId —— 顾客早已离开那张桌;
//  ・需登录,令牌过期由 @dining/api 的 401 兜底静默重登后重放。
export const getMyOrderDetail = (no: Id): Promise<OrderDetail> =>
  http.get<unknown, OrderDetail>(`/customer/order/mine/${encodeURIComponent(String(no))}`);
// 顾客催菜:请求后厨加急(同一订单 3 分钟冷却,超频后端会返回剩余秒数)。
// 后端需校验订单归属桌台,故一并提交当前桌台的 tableId。
// config 可传 { silent: true }:冷却拒绝(HTTP 400 带结构化 cooldown)时不弹全局错误
// toast,由 useUrge 读取 data.cooldown 自行展示倒计时。
export const urgeOrder = (orderNo: Id, tableId: number, config?: { silent?: boolean }): Promise<UrgeResult> =>
  http.post<unknown, UrgeResult>('/customer/order/urge', { orderNo, tableId }, config);
// 改就餐人数(不改菜品)。人数决定茶位费,后端会重算金额并返回最新订单 ——
// 前端不要自己在前端算茶位费,否则两端口径一旦漂移就会显示两个价。
// 只允许「进行中且未支付」的订单;已支付需走人工退款/改单。
export interface OrderPersonPayload {
  tableId: number;
  orderNo: string;
  personCount: number;
}
export const updateOrderPerson = (data: OrderPersonPayload): Promise<OrderDetail> =>
  http.post<unknown, OrderDetail>('/customer/order/person', data);
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

// ---------------------------------------------------------------------------
// 顾客会员（账号 / 登录方式分离，见 docs/design-docs/customer-member-identity/spec.md）
//
// 登录方式是「可插拔」的：provider 只决定「凭据怎么换成身份」，响应体与令牌
// 完全一致。所以同一组函数在三端通用 —— 小程序传 wx.login 的 code，
// 网站/App 后续传手机号验证码或账号密码即可，页面代码不用重写。
//
// 目前后端只有 wx_mp 有实现，其余 provider 返回「暂不支持该登录方式」。
// ---------------------------------------------------------------------------

/** 会员登录方式，取值与后端 po.CustomerProvider* 一一对应。 */
export type MemberProvider = 'wx_mp' | 'wx_oa' | 'wx_app' | 'phone' | 'password' | 'apple';

/** 会员卡信息（后端只下发展示字段；openid 等站内标识永不外发）。 */
export interface MemberInfo {
  customerId: number;
  /** 未填昵称时后端返回「微信用户」。 */
  nickname: string;
  avatarUrl: string;
  /** 入会日期。 */
  memberSince: string;
  orderCount: number;
  /** 已绑手机号（脱敏 139****0001）；未绑定为空串。 */
  phone?: string;
}

export interface MemberLoginResult {
  token: string;
  /** true = 本次是注册（前端提示「🎉 注册成功，已加入会员」）。 */
  isNew: boolean;
  customer: MemberInfo;
}

/** 「我的订单」列表行（后端 dao.CustomerOrderRow 的响应形态，金额单位是元）。 */
export interface MemberOrderRow {
  orderId: number;
  orderNo: string;
  pickupNo: number;
  tableId: number;
  tableName: string;
  orderStatus: number;
  payStatus: number;
  totalAmount: number;
  createTime: string;
}

/** 登录 / 注册的请求体。provider 缺省为 wx_mp（兼容只发 code 的老客户端）。 */
export interface MemberLoginPayload {
  provider?: MemberProvider;
  /** provider=wx_mp 时必填：wx.login 现取现用的 code。 */
  code?: string;
}

export const memberLogin = (data: MemberLoginPayload): Promise<MemberLoginResult> =>
  http.post<unknown, MemberLoginResult>('/customer/auth/login', data);

export const memberProfile = (): Promise<MemberInfo> => http.get<unknown, MemberInfo>('/customer/auth/profile');

export const memberOrders = (pageNum: number, pageSize = 10): Promise<{ total: number; items: MemberOrderRow[] }> =>
  http.get<unknown, { total: number; items: MemberOrderRow[] }>('/customer/order/mine', {
    params: { pageNum, pageSize }
  });

/**
 * 完善会员资料(目前只有昵称)。
 *
 * 头像不在这个接口里:顾客可上传需要一条独立的上传通道(现有上传接口要管理端权限),
 * 属于另一个功能。只传昵称时服务端会原样保留已设置的头像。
 */
export const updateMemberProfile = (data: { nickname: string }): Promise<MemberInfo> =>
  http.post<unknown, MemberInfo>('/customer/auth/profile', data);

/**
 * 注销会员(不可撤销)。
 *
 * 档案与登录方式被物理删除,历史订单归入门店的匿名单(不再出现在「我的订单」)。
 * 令牌无状态:注销后下次请求查不到档案即 401,前端清本地令牌即可。
 */
export const deleteMemberAccount = (): Promise<void> => http.delete<unknown, void>('/customer/auth/account');

/**
 * 绑定手机号(getPhoneNumber 按钮的 code,与 wx.login 的 code 不是同一个东西)。
 * 返回脱敏后的手机号(139****0001)。同一手机号全店只能属于一个会员。
 */
export const bindMemberPhone = (code: string): Promise<{ phone: string }> =>
  http.post<unknown, { phone: string }>('/customer/auth/phone', { code });

// 订单评价(仅商家可见的内部反馈):匿名与会员同一入口,会员令牌自动带上身份。
// 读写都挂后端 IP 限流;「已评价」重复提交是幂等错误(HTTP 400 + reviewed:true),
// 提交入口在点击后即锁定,正常交互不会触发。
// reviewerKey=匿名设备键(visitorId,持久化):「我是否已评」按设备判定,同桌他人
// 评过不影响;会员不带键,后端按令牌身份派生。
export const getReviewStatus = (orderNo: Id, tableId: number, reviewerKey?: string): Promise<CustomerReviewStatus> =>
  http.get<unknown, CustomerReviewStatus>(`/customer/review?orderNo=${encodeURIComponent(String(orderNo))}&tableId=${tableId}${reviewerKey ? `&reviewerKey=${encodeURIComponent(reviewerKey)}` : ''}`);
export const createReview = (data: ReviewCreatePayload): Promise<CustomerReviewStatus> =>
  http.post<unknown, CustomerReviewStatus>('/customer/review', data);

// 评价图片上传(「先传后评」):图片先落在独立表 tb_review_image,提交评价时事务
// 绑定;未绑定的孤儿由后端 24h 清理。返回 url 形如 /uploads/review_xxx.jpg,
// 提交时只需尾段文件名。
export const uploadReviewImage = (file: File): Promise<{ url: string }> => {
  const fd = new FormData();
  fd.append('file', file);
  return http.post<unknown, { url: string }>('/customer/review/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 120000
  });
};
