// ============================================================================
// 小程序端全部类型（自包含，不依赖 @dining/types）
//
// 实体类型（Dish / OrderDetail / TableInfo / PublicConfig ……）= 后端响应形态
// （字段权威来源为后端 handler 响应，可选字段 + 索引签名兼容增量字段）；
// 小程序专属状态（SessionState / CartLine / GlobalData）承载跨页共享状态。
// 改动后端字段时，只需同步这里一处。
// ============================================================================

type Extensible = {
  [key: string]: unknown;
};

type NullableString = string | null;

export type Id = number | string;

// ---------------------------------------------------------------------------
// 桌台 / 菜单
// ---------------------------------------------------------------------------

export interface TableInfo extends Extensible {
  tableId?: number;
  tableNo?: string;
  tableName?: string;
  tableCode?: string;
  capacity?: number;
  status?: number;
  // 人工可用性状态:0 正常 / 1 停用(不可用) / 2 预约;与订单自动占用(status)正交。
  lockStatus?: number;
  lockRemark?: string;
  sortOrder?: number;
  createTime?: string;
  remark?: NullableString;
  orderId?: number;
  orderNo?: string;
  shortNo?: string;
  order?: OrderSummary | OrderDetail | null;
  // 一桌多单:看板返回该桌全部进行中订单(付款后顾客可再开新单,旧单仍在出餐)。
  orders?: OrderSummary[] | null;
  currentOrder?: OrderSummary | OrderDetail | null;
  // 顾客扫码时,该桌「已付款但出餐未完成」的订单,用于查看出餐进度。
  servingOrders?: OrderSummary[] | null;
  // 桌级共享的「入座人数」（茶位费门店）：同桌另一部手机已选过时非 0。
  pendingPersonCount?: number;
  // 用餐耗时(桌台列表接口实时计算,不落库):空闲桌台两个字段都不出现。
  openTime?: string;
  diningMinutes?: number;
}

export interface MenuCategory extends Extensible {
  categoryId?: number;
  categoryName?: string;
  sortOrder?: number;
  dishes?: Dish[];
}

export interface Dish extends Extensible {
  dishId?: number;
  categoryId?: number;
  categoryName?: string;
  dishName?: string;
  dishImage?: string;
  description?: string;
  status?: number;
  /** 0 未售罄 / 1 售罄（上架中但今日沽清：顾客端可见但不可下单）。 */
  soldOut?: number;
  /** 0 普通 / 1 招牌菜（顾客端据此聚合「本店招牌」分组）。 */
  signature?: number;
  sortOrder?: number;
  remark?: NullableString;
  specs?: DishSpec[];
}

export interface DishSpec extends Extensible {
  specId?: number;
  dishId?: number;
  specName?: string;
  price?: number;
  /** 剩余库存：-1 不限量，>=0 剩余份数（0 = 售罄）。 */
  stock?: number;
}

export interface RemarkOption extends Extensible {
  remarkId?: number;
  optionName?: string;
  sortOrder?: number;
}

// ---------------------------------------------------------------------------
// 订单
// ---------------------------------------------------------------------------

export interface OrderSummary extends Extensible {
  orderId?: number;
  orderNo?: string;
  shortNo?: string;
  tableId?: number;
  tableNo?: string;
  tableName?: string;
  personCount?: number;
  orderStatus?: number;
  dishAmount?: number;
  seatFee?: number;
  discountAmount?: number;
  totalAmount?: number;
  payStatus?: number;
  payType?: NullableString;
  payTime?: NullableString;
  transactionId?: string;
  payChannel?: string;
  refundAmount?: number;
  refundingAmount?: number;
  refundTime?: NullableString;
  settleType?: string;
  settleTime?: NullableString;
  creditStatus?: number;
  creditAmount?: number;
  paidAmount?: number;
  giftAmount?: number;
  finishTime?: NullableString;
  mealStartTime?: NullableString;
  mealEndTime?: NullableString;
  createTime?: string;
  remark?: NullableString;
  pendingUrge?: boolean;
  /** 是否是商家还没操作过的新单（管理端角标用）。 */
  isNew?: boolean;
  /** 已收款、还差一步完成订单（管理端角标用）。 */
  needFinish?: boolean;
  /** 加菜批次数(明细 distinct batch_no):>1 表示本单加过菜。 */
  batchCount?: number;
}

export interface OrderDetail extends OrderSummary {
  items?: OrderItem[];
  orderRemark?: string;
  cancelReason?: string;
}

export interface OrderItem extends Extensible {
  itemId?: number;
  dishId?: number;
  dishName?: string;
  specId?: number;
  specName?: string;
  quantity?: number;
  price?: number;
  amount?: number;
  itemRemark?: string;
  categoryId?: number;
  batchNo?: number;
  isGift?: number;
  giftReason?: string;
}

/** 顾客催菜结果：超频时后端回传剩余冷却秒数。 */
export interface UrgeResult extends Extensible {
  orderNo?: string;
  cooldown?: number;
}

/** 改就餐人数（不改菜品）：人数决定茶位费，后端重算金额并返回最新订单。 */
export interface OrderPersonPayload {
  tableId: number;
  orderNo: string;
  personCount: number;
}

export interface OrderCreateItem extends Extensible {
  dishId?: number;
  specId?: number;
  quantity?: number;
  itemRemark?: string;
}

export interface OrderCreatePayload extends Extensible {
  tableId?: number;
  /** 传了 = 加菜（追加到该单）；不传 = 新单。 */
  orderNo?: string;
  personCount?: number;
  orderRemark?: string;
  items?: OrderCreateItem[];
}

export interface OrderCreateResult extends Extensible {
  orderId?: number;
  orderNo: string;
  shortNo?: string;
}

// ---------------------------------------------------------------------------
// 配置 / 支付
// ---------------------------------------------------------------------------

/** 顾客端公开配置（无需登录）。字段为后端 config 键的 snake_case 原样下发。 */
export interface PublicConfig extends Extensible {
  shop_name?: string;
  /** 门店标语：仅顾客端顶部与店铺名称拼接展示。 */
  shop_slogan?: string;
  seat_fee_enabled?: string;
  seat_fee?: string;
  promotion_enabled?: string;
  promotion_threshold?: string;
  promotion_discount?: string;
  pay_qr_wx?: string;
  pay_qr_ali?: string;
  wxpay_enabled?: string;
  alipay_enabled?: string;
  /**
   * 先付模式开关："1" = 下单后先完成支付后厨才制作，下单成功直达支付页；
   * "0"（默认）= 后付模式：下单即出单做菜，餐后结账。
   */
  pay_first_enabled?: string;
  /** 会员功能开关：后端已配小程序 AppID/AppSecret 才为 "1"。 */
  member_enabled?: string;
  /** 储值自助充值开关(默认 0)。 */
  wallet_recharge_enabled?: string;
  /** 小程序内支付（JSAPI）开关：顾客端据此决定结账页是否出现「微信支付」按钮。 */
  wxpay_jsapi_enabled?: string;
}

/** 收款码（顾客用微信/支付宝钱包扫码自助支付）。 */
export interface PayQr extends Extensible {
  wx?: string;
  ali?: string;
  wxQr?: string;
  aliQr?: string;
}

export interface PayCreatePayload extends Extensible {
  orderNo?: string;
  channel?: string;
}

export interface PayCreateResult extends Extensible {
  codeUrl: string;
  orderNo?: string;
  outTradeNo?: string;
  amount?: number;
}

export interface PayQueryResult extends Extensible {
  orderNo?: string;
  payStatus?: number;
  payType?: string;
  transactionId?: string;
}

// ---------------------------------------------------------------------------
// 小程序内支付（JSAPI）
//
// 与 createPay（Native 扫码）的区别：Native 返回 code_url，小程序里不能
// 「自己扫自己」，所以走 JSAPI 下单 —— 由服务端拿 wx.login 的 code 换 openid，
// 下单后签好调起参数，小程序拿着参数直接 wx.requestPayment。
// 后端未启用时返回业务错误，调用方据此降级到「收款码 + 前台结账」。
// ---------------------------------------------------------------------------

export interface PayJsapiPayload {
  orderNo?: string;
  /** wx.login 拿到的临时登录凭证（5 分钟有效，一次性）。 */
  code?: string;
}

/** 调起参数，字段名与 wx.requestPayment 的入参一致，前端可原样透传。 */
export interface PayJsapiResult {
  timeStamp: string;
  nonceStr: string;
  /** 形如 "prepay_id=wx..."。 */
  package: string;
  signType: 'MD5' | 'HMAC-SHA256' | 'RSA';
  paySign: string;
}

// ---------------------------------------------------------------------------
// 会员（账号 / 登录方式分离：provider 只决定「凭据怎么换成身份」）
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

/** 登录 / 注册的请求体。provider 缺省为 wx_mp（兼容只发 code 的老客户端）。 */
export interface MemberLoginPayload {
  provider?: MemberProvider;
  /** provider=wx_mp 时必填：wx.login 现取现用的 code。 */
  code?: string;
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

// ---------------------------------------------------------------------------
// 储值（金额一律以「元」下发，与订单金额同一口径）
// ---------------------------------------------------------------------------

/**
 * 储值账户（余额与累计口径）。
 *
 * 本金与赠金分桶：balance 是本金刚（可退），balanceGift 是赠金（只能消费、不可退现）。
 * 分桶的必要性在退款时才看得出来 —— 混在一个数里，退余额时无法回答
 * 「这笔钱该不该退给顾客」，只能一刀切全退（可被套现）。
 */
export interface WalletInfo {
  balance: number;
  balanceGift: number;
  totalRecharge: number;
  totalConsume: number;
  totalGift: number;
  // ---- 合规与交互 ----
  /** 是否已实名（绑手机号）：决定余额上限走 1000 还是 5000。 */
  named: boolean;
  /** 当前《储值规则》版本。 */
  rulesVer: string;
  /** 《储值规则》正文（可能含换行，前端按 \n 分段渲染）。 */
  rulesText: string;
  /** 是否已同意当前版本规则；未同意时不能充值。 */
  agreed: boolean;
  /** 门店是否开放自助充值。 */
  canRecharge: boolean;
  /** 充值前是否强制实名。 */
  realnameRequired: boolean;
  /** 单笔充值下限（元）。 */
  rechargeMin: number;
  /** 单笔充值上限（元）。 */
  rechargeMax: number;
  /** 该会员的余额上限（元）。 */
  balanceMax: number;
}

/** 储值流水行（amount 带符号：正入账 / 负出账）。 */
export interface WalletTxnRow {
  txnId: number;
  bizType: string;
  amount: number;
  balanceAfter: number;
  /** 本笔变动中属于赠金桶的部分（带符号）；顾客据此看懂「扣的是赠金还是本金」。 */
  giftAmount: number;
  orderNo: string;
  reason: string;
  operator: string;
  createTime: string;
}

/**
 * 混合支付（余额抵扣 + 微信补差）结果。
 *
 * paid=true 表示余额已把订单付清（无需拉起收银台）；否则带着
 * wx.requestPayment 所需的调起参数，由顾客补齐差额。
 */
export interface WalletMixedPayResult {
  paid: boolean;
  /** 本次实际扣掉的余额（元）。 */
  walletCents: number;
  /** 还需微信支付的金额（元）。 */
  payCents?: number;
  timeStamp?: string;
  nonceStr?: string;
  package?: string;
  signType?: 'MD5' | 'HMAC-SHA256' | 'RSA';
  paySign?: string;
}

/** 发起充值：返回 wx.requestPayment 所需的调起参数 + 充值单号（轮询入账用）。 */
export interface WalletRechargeParams {
  rechargeNo: string;
  timeStamp: string;
  nonceStr: string;
  package: string;
  signType: 'MD5' | 'HMAC-SHA256' | 'RSA';
  paySign: string;
}

// ---------------------------------------------------------------------------
// 访问统计打点（公开接口 POST /api/customer/visit）
// ---------------------------------------------------------------------------

/** 打点渠道：h5 扫码点餐页 / miniprogram 小程序启动 / admin 管理后台登录。 */
export type VisitChannel = 'h5' | 'miniprogram' | 'admin';

export interface VisitTrackPayload {
  visitorId?: string;
  channel: VisitChannel;
  /** 入口标识：桌台点餐码（H5 路由参数 / 小程序码 scene）。 */
  entry?: string;
  /** 端上直传的设备信息（仅小程序能精确提供；H5 不传，服务端解析 UA 兜底）。 */
  client?: {
    /** 形如 "iOS 16.6"（wx.getDeviceInfo().system）。 */
    system?: string;
    /** ios / android / windows / mac。 */
    platform?: string;
    /** 机型，如 "iPhone 14 Pro"。 */
    model?: string;
  };
}

// ============================================================================
// 小程序专属：跨页共享状态
// ============================================================================

/** 本次进入的桌台与订单。 */
export interface SessionState {
  /** 桌台稳定码（二维码里的 8 位点餐码）。 */
  tableCode: string;
  /** 数字桌台 ID，下单接口需要。 */
  tableId: number;
  table: TableInfo | null;
  shopName: string;
  /** 顾客端公开配置：茶位费 / 促销 / 收款码等。 */
  config: PublicConfig | null;
  /** 当前桌台进行中的订单；null 表示尚未下单。 */
  currentOrder: OrderDetail | null;
  /**
   * 就餐人数。放在会话里而不是各页面自己存：茶位费按人数计算，
   * 菜单页底部结算栏与购物车页的合计必须一致，否则同一批菜在两页显示两个价。
   */
  personCount: number;
  /**
   * 是否已「显式选择」过人数（选人数面板确认 / 购物车步进 / 订单页改人数）。
   * 与 personCount 分开：personCount 可能来自购物车恢复或订单现值，
   * 不能据此判断顾客有没有做过选择 —— 茶位费门店进入菜单时必须选一次，
   * 选过（本会话内）就不再弹。
   */
  personChosen: boolean;
  /**
   * 整单备注。同样放会话里：购物车要能跨冷启动恢复（见 store/cartPersist），
   * 备注若只存在购物车页的 data 里，页面一销毁就丢。
   */
  orderRemark: string;
}

/** 购物车行：同一道菜的不同规格算两行。 */
export interface CartLine {
  /** `dishId:specId`。 */
  key: string;
  dishId: number;
  dishName: string;
  specId: number;
  specName: string;
  price: number;
  image: string;
  quantity: number;
  itemRemark: string;
  /** 加购时该规格的剩余份数快照：-1 / 缺省 = 不限量，>=0 为可购份数。
   *  购物车页的 + 拿不到菜单对象，靠它夹紧（后端下单事务按 stock >= qty 校验）。 */
  stock?: number;
}

/** 规格选择快照（规格弹层确定后回写菜品行的「当前规格」）。 */
export interface SpecPick {
  specId: number;
  specName: string;
  price: number;
}

/** 备注 chip（规格弹层内）。 */
export interface RemarkChip {
  name: string;
  on: boolean;
}

/** 会员登录态：挂在 App 实例上跨页共享；令牌另存本地存储供冷启动恢复。 */
export interface MemberState {
  /** 会员 ID；0 表示未登录，-1 表示「有本地令牌但档案尚未拉取」。 */
  customerId: number;
  info: MemberInfo | null;
  /** 登录流程进行中（防重复点击与并发静默重登）。 */
  pending: boolean;
}

/**
 * 挂在 App 实例上的跨页状态。
 *
 * 注意它**不是** `globalData` 字段的子结构：miniprogram-api-typings 的
 * `App.Options<T> = Partial<Option> & T`，即 T 的字段会直接摊到 App 实例上，
 * 因此写法是 `App<GlobalData>({ session, cart, ... })` 与
 * `getApp<GlobalData>().cart`（等价于传统 globalData，但带类型）。
 */
export interface GlobalData {
  session: SessionState;
  cart: CartLine[];
  member: MemberState;
}
