// 字段权威来源为后端 handler 响应，后续任务按需补充。

type Extensible = {
  [key: string]: unknown;
};

type NullableString = string | null;

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
  delFlag?: string;
  createBy?: string;
  createTime?: string;
  updateBy?: string;
  updateTime?: string;
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
  // 用餐耗时(桌台列表接口实时计算,不落库):openTime 该桌最早一笔进行中订单的
  // 开台时间;diningMinutes 距现在的分钟数。空闲桌台两个字段都不出现。
  openTime?: string;
  diningMinutes?: number;
}

// 桌台小程序码:图片由微信生成(后端换 access_token 取回),前端只负责展示与排版。
// dataUrl 而非图片地址 —— 打印排版用的就是 dataUrl(见 utils/tableQr 的 QrCard)。
export interface WeappCodeResult extends Extensible {
  dataUrl?: string;
  cached?: boolean;
  // 码指向的小程序版本;非 release 时说明当前出的码只能在开发/体验版里打开。
  envVersion?: 'release' | 'trial' | 'develop';
  width?: number;
}

export interface MenuCategory extends Extensible {
  categoryId?: number;
  categoryName?: string;
  sortOrder?: number;
  delFlag?: string;
  createTime?: string;
  updateTime?: string;
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
  /** 0 未售罄 / 1 售罄（上架中但今日沽清：顾客端可见但不可下单，与 status 下架正交） */
  soldOut?: number;
  /** 0 普通 / 1 招牌菜（顾客端在菜品图片左上角渲染「招牌」角标） */
  signature?: number;
  sortOrder?: number;
  delFlag?: string;
  createBy?: string;
  createTime?: string;
  updateBy?: string;
  updateTime?: string;
  remark?: NullableString;
  specs?: DishSpec[];
}

export interface DishSpec extends Extensible {
  specId?: number;
  dishId?: number;
  specName?: string;
  price?: number;
  /** 剩余库存：-1 不限量，>=0 剩余份数（0 = 售罄） */
  stock?: number;
}

export interface RemarkOption extends Extensible {
  remarkId?: number;
  optionName?: string;
  sortOrder?: number;
  delFlag?: string;
  createTime?: string;
  updateTime?: string;
}

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
  /** 处理中的退款金额（元，服务端补算）：refundAmount 只在退款成功时累加，该字段用于展示「退款中」并禁用再次退款 */
  refundingAmount?: number;
  refundTime?: NullableString;
  settleType?: string;
  settleTime?: NullableString;
  settleOperator?: string;
  settleRemark?: string;
  creditStatus?: number;
  creditAmount?: number;
  creditSettleTime?: NullableString;
  creditSettleBy?: string;
  paidAmount?: number;
  giftAmount?: number;
  finishTime?: NullableString;
  /** 用餐起止时间:开始=下单;结束=订单到终态(完成/取消)。进行中为空 */
  mealStartTime?: NullableString;
  mealEndTime?: NullableString;
  createBy?: string;
  createTime?: string;
  updateBy?: string;
  updateTime?: string;
  remark?: NullableString;
  pendingUrge?: boolean;
  /**
   * 是否是商家还没操作过的新单:服务端按「已下单 + 从未操作」计算(仅查看不算)。
   * 列表接口恒返回 true/false;前端据此在卡片左上角打「新」角标。
   */
  isNew?: boolean;
  /** 加菜批次数(明细 distinct batch_no):>1 表示本单加过菜,列表据此打「加菜×N」角标。列表接口填充 */
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

export interface Printer extends Extensible {
  printerId?: number;
  printerName?: string;
  printerType?: number;
  provider?: string;
  ip?: string;
  port?: number;
  feieSn?: string;
  paperWidth?: number;
  copies?: number;
  categoryIds?: string;
  categoryIdList?: number[];
  categoryNames?: NullableString;
  onlineStatus?: string;
  status?: number;
  delFlag?: string;
  createTime?: string;
  updateTime?: string;
}

export interface PrintLog extends Extensible {
  printId?: number;
  orderId?: number;
  orderNo?: string;
  shortNo?: string;
  tableNo?: string;
  tableName?: string;
  printerId?: number;
  printerName?: string;
  printerType?: number;
  provider?: string;
  docType?: string;
  copies?: number;
  status?: number;
  remoteId?: string;
  /**
   * 这张票的可读标识:拆单厨房单为「【厨房单】1/4」,其余为票种名,测试页为空。
   * 独立字段而非拼进 detail —— 代理通道的回执/重试/作废会整段改写 detail。
   * 「一菜一单」下一次下单有多条记录,靠它才分得清是哪一张。
   */
  label?: string;
  detail?: string;
  triggerBy?: string;
  operator?: string;
  costMs?: number;
  createTime?: string;
}

// 票据预览的单行:文本 + 是否加粗强调(店名/标题/合计,与 ESC/POS 出纸一致)。
export interface TicketLine {
  text: string;
  bold?: boolean;
  /**
   * 非空表示这一行实际出的是二维码;text 只是占位文案。
   * 等宽字符网格画不出二维码图形,前端按 qr 内容渲染真实二维码。
   */
  qr?: string;
  /**
   * 为真表示这一行按「机器居中」出纸(单据名副标题):前端按 CSS 居中渲染,
   * 与放大行同一套观感,不再按字符网格左对齐。
   */
  center?: boolean;
  /**
   * 为真表示这一行按「放大」出纸(打印机 GS ! 倍宽倍高 / 飞鹅 <C><B> 或 <B>)。
   * 放大行的字符网格已失效,前端不再按格渲染,直接用大字显示;
   * 是否居中看 bigLeft。
   */
  big?: boolean;
  /**
   * 为真表示上面那个放大行是「左对齐」的:整单厨房单的菜品清单(逐条编号,
   * 飞鹅用 <B> 而非 <C><B>)。居中了编号就不在行首,后厨划单时视线要左右找。
   */
  bigLeft?: boolean;
}

// 票据预览(后端回放台账存下的票面快照返回的等宽文本行;按等宽字体展示即与出纸 1:1)。
export interface TicketPreview {
  /** 按实际入队规则切好的段:超长票据打印时会拆成多段依次送出,与预览分段一一对应 */
  chunks: TicketLine[][];
  /** 每行可容纳的半角字符数(58mm 纸=32,80mm 纸=48),前端排版用 */
  lineWidth: number;
  docType?: string;
  /** 厨房单打印粒度(whole/item/portion),仅 docType=kitchen 时返回 */
  splitMode?: string;
  /** 触发场景:guest 单据靠它区分两种形态(顾客小票/结账小票),见 dicts.printDocLabel */
  triggerBy?: string;
  orderNo?: string;
  printerName?: string;
  copies?: number;
  /**
   * 为 true 表示内容来自这条记录当时存下的票面快照(回放,一字不差):
   * 订单后来加过菜、店名/页脚/纸宽后来改过,都不影响它。
   */
  fromSnapshot?: boolean;
  /**
   * 为 true 表示这张预览只是「形态一致」而非内容复现 —— 只在没有票面快照时出现
   * (升级前打的老记录):按当前订单重新渲染,加菜小票更是无从还原当时新增了哪些菜,
   * 菜品区退化为订单当前整单,UI 需要显式说明。
   */
  approximate?: boolean;
}

export interface UserInfo extends Extensible {
  userId?: number;
  username?: string;
  realName?: string;
  roleId?: number;
  roleKey?: string;
  roleName?: string;
  phone?: string;
  status?: number;
  lastLoginTime?: string;
  lastLoginIp?: string;
  loginCount?: number;
  pwdUpdateTime?: string;
  delFlag?: string;
  createBy?: string;
  createTime?: string;
  updateBy?: string;
  updateTime?: string;
  remark?: string;
  perms?: string[];
}

export interface Role extends Extensible {
  roleId?: number;
  roleKey?: string;
  roleName?: string;
  perms?: string;
  dataScope?: string;
  isBuiltin?: number;
  sortOrder?: number;
  status?: number;
  delFlag?: string;
  permList?: string[];
  createTime?: string;
  updateTime?: string;
  remark?: string;
  userCount?: number;
}

export interface OperLog extends Extensible {
  logId?: number;
  module?: string;
  businessType?: string;
  action?: string;
  method?: string;
  requestUrl?: string;
  operatorId?: number;
  operator?: string;
  operatorRole?: string;
  operIp?: string;
  targetType?: string;
  targetId?: string;
  operParam?: string;
  detail?: string;
  status?: number;
  errorMsg?: string;
  costMs?: number;
  createTime?: string;
}

export interface ReportSummary extends Extensible {
  todayAmount?: number;
  todayOrderCount?: number;
  todayFinishedCount?: number;
  todayGuestCount?: number;
  todayAvgAmount?: number;
  todayCancelCount?: number;
  todayRefundAmount?: number;
  monthAmount?: number;
  monthOrderCount?: number;
  monthGuestCount?: number;
  monthAvgAmount?: number;
  freeTableCount?: number;
  tableCount?: number;
  activeOrderCount?: number;
  yesterdayAmount?: number;
  yesterdayOrderCount?: number;
  yesterdayGuestCount?: number;
  lastMonthSamePeriodAmount?: number;
  lastMonthSamePeriodOrderCount?: number;
  todayFreeAmount?: number;
  todayGiftAmount?: number;
  creditPendingAmount?: number;
  creditPendingCount?: number;
  todayCreditAmount?: number;
  todayCreditSettledAmount?: number;
  todayCreditSettledCount?: number;
}

export interface DailyTrendRow extends Extensible {
  date?: string;
  amount?: number;
  orderCount?: number;
  guestCount?: number;
}

export interface MonthlyTrendRow extends Extensible {
  month?: string;
  amount?: number;
  orderCount?: number;
  guestCount?: number;
}

export interface DishRankRow extends Extensible {
  dishName?: string;
  quantity?: number;
  amount?: number;
}

export interface HourlyRow extends Extensible {
  hour?: string;
  amount?: number;
  orderCount?: number;
  guestCount?: number;
}

/** 站点访问统计(见 docs/design-docs/visit-analytics/spec.md)。 */

/** 打点渠道:h5 扫码点餐页 / miniprogram 小程序启动 / admin 管理后台登录。 */
export type VisitChannel = 'h5' | 'miniprogram' | 'admin';

/** 打点请求体(公开接口 POST /api/customer/visit)。 */
export interface VisitTrackPayload {
  visitorId?: string;
  channel: VisitChannel;
  /** 入口标识:桌台点餐码(仅 H5 有)。 */
  entry?: string;
}

/** 报表里一天的访问数据(分渠道 PV;UV/新访客为全站口径)。 */
export interface VisitDailyRow extends Extensible {
  date?: string;
  pv?: number;
  uv?: number;
  newUv?: number;
  h5Pv?: number;
  mpPv?: number;
  adminPv?: number;
}

/** 报表里一个渠道的访问数据。 */
export interface VisitChannelRow extends Extensible {
  channel?: VisitChannel;
  label?: string;
  pv?: number;
  uv?: number;
}

/** 扫码入口排行的一行(桌台码被扫次数)。 */
export interface VisitEntryRow extends Extensible {
  entry?: string;
  label?: string;
  pv?: number;
}

/** GET /api/admin/report/visits 的完整出参。 */
export interface VisitStats extends Extensible {
  range?: { start?: string; end?: string };
  summary?: {
    pv?: number;
    uv?: number;
    newUv?: number;
    memberUv?: number;
    totalVisitors?: number;
    memberCount?: number;
    todayPv?: number;
    todayUv?: number;
    todayNewUv?: number;
    yesterdayPv?: number;
    yesterdayUv?: number;
  };
  daily?: VisitDailyRow[];
  channels?: VisitChannelRow[];
  entries?: VisitEntryRow[];
}

export interface SettleMixRow extends Extensible {
  settleType?: string;
  label?: string;
  amount?: number;
  paidAmount?: number;
  orderCount?: number;
}

// 用户用餐时长分析(报表页):已完成订单按完成时间归属,时长 = 完成时间 - 下单时间。
export interface DiningDurationBucket {
  label?: string;
  count?: number;
}

export interface DiningDurationStats extends Extensible {
  orderCount?: number; // 参与统计的已完成订单数
  avgMinutes?: number; // 平均用餐时长(分钟)
  medianMinutes?: number; // 中位时长(分钟)
  maxMinutes?: number; // 最长一单(分钟)
  guestCount?: number; // 就餐人次合计
  buckets?: DiningDurationBucket[]; // 时长分布(固定 5 档)
}

export interface PublicConfig extends Extensible {
  shop_name?: string;
  /** 门店标语:仅顾客端 H5/小程序顶部与店铺名称拼接展示,桌牌二维码不放 */
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
   * 先付模式开关：为 "1" 时顾客下单后需先完成支付，后厨才开始制作，
   * 顾客端下单成功后直达支付页；为 "0"（默认）时是后付模式：下单即出单做菜，餐后结账。
   */
  pay_first_enabled?: string;
  /** 会员功能开关：后端已配置小程序 AppID/AppSecret 才为 "1"，顾客端据此显隐登录入口。 */
  member_enabled?: string;
  /**
   * 小程序内支付（JSAPI）开关：顾客端据此决定结账页是否出现「微信支付」按钮
   * （`wxpay_jsapi_enabled === '1'`）。后端 `CustomerSettings()` 已下发，
   * 管理端「系统配置 → 在线支付 → 小程序内支付」控制，默认关（fail-closed）。
   *
   * 显式声明而不是只靠索引签名：`PublicConfig extends Extensible`，字段名拼错或
   * 后端漏发都不会编译报错 —— 那种错误只会表现为「按钮永远不出现」，极难定位。
   */
  wxpay_jsapi_enabled?: string;
}

export type Id = number | string;

export type ApiPayload = Record<string, unknown>;

export interface LoginPayload extends ApiPayload {
  username?: string;
  password?: string;
  code?: string;
  uuid?: string;
  rememberMe?: boolean;
}

export interface RememberLoginPayload extends ApiPayload {
  rememberToken?: string;
}

// 「记住我」会话(登录设备页):完整令牌不出服务端,只回传前 8 位供本机识别。
export interface RememberSession extends Extensible {
  tokenId: number;
  tokenPrefix?: string;
  ua?: string;
  createTime?: string;
  lastUsedTime?: string;
  expireTime?: string;
}

export interface RememberSessionsResult extends Extensible {
  sessions?: RememberSession[];
}

export interface LoginResult extends Extensible {
  token?: string;
  rememberToken?: string;
  username?: string;
  realName?: string;
  roleKey?: string;
  roleName?: string;
  perms?: string[];
}

export interface ChangePasswordPayload extends ApiPayload {
  oldPassword?: string;
  newPassword?: string;
}

export type Profile = UserInfo;

export interface UserPayload extends ApiPayload {
  userId?: number;
  username?: string;
  realName?: string;
  password?: string;
  roleId?: number;
  phone?: string;
  status?: number;
}

export interface RolePayload extends ApiPayload {
  roleId?: number;
  roleKey?: string;
  roleName?: string;
  perms?: string;
  permList?: string[];
  dataScope?: string;
  sortOrder?: number;
  status?: number;
}

// 与后端 store/permission.go 的 PermGroup/PermDef 契约对齐:key/name/perms。
export interface PermDef extends Extensible {
  code?: string;
  name?: string;
}

export interface PermCatalogItem extends Extensible {
  key?: string;
  name?: string;
  perms?: PermDef[];
}

export interface PermCatalog extends Extensible {
  groups?: PermCatalogItem[];
  // 后端 PermImplies() 返回 map[string]string(一对一依赖:勾 A 连带授 B),可传递。
  implies?: Record<string, string>;
  total?: number;
}

export interface TablePayload extends ApiPayload {
  tableId?: number;
  tableNo?: string;
  tableName?: string;
  tableCode?: string;
  capacity?: number;
  status?: number;
  lockStatus?: number;
  lockRemark?: string;
  sortOrder?: number;
  /** 强制清台时对未结账订单的处理方式：settle 补结账 / cancel 作废 */
  action?: string;
}

export interface CategoryPayload extends ApiPayload {
  categoryId?: number;
  categoryName?: string;
  sortOrder?: number;
}

export interface DishPayload extends ApiPayload {
  dishId?: number;
  categoryId?: number;
  dishName?: string;
  dishImage?: string;
  description?: string;
  status?: number;
  soldOut?: number;
  signature?: number;
  sortOrder?: number;
  specs?: DishSpec[];
}

export interface RemarkPayload extends ApiPayload {
  remarkId?: number;
  optionName?: string;
  sortOrder?: number;
}

export interface ConfigData extends PublicConfig {
  shop_logo?: string;
  h5_base_url?: string;
  version?: string;
}

/** 一条「开台超时仍未结账」的告警（管理端看板用）。 */
export interface StaleOrderAlert {
  orderId?: number;
  orderNo?: string;
  shortNo?: string;
  tableId?: number;
  tableNo?: string;
  tableName?: string;
  orderStatus?: number;
  personCount?: number;
  createTime?: string;
  /** 已开台分钟数：由后端算好，避免各端各算一套时长 */
  minutes?: number;
  totalAmount?: number;
  /**
   * 是否跨日遗留单：上一营业日及更早、至今仍未结账。
   * 这类单的桌台已被自动清空，看板上看不到，只在提醒列表里能找到。
   */
  legacy?: boolean;
}

/**
 * 未结账超时告警的出参。
 * enabled/hours 由后端随响应下发，而不是让前端去读系统配置 ——
 * 告警开关属 config:edit 侧的管理项，看板只要 order:view 就能进。
 */
export interface StaleOrderAlerts {
  enabled?: boolean;
  hours?: number;
  items?: StaleOrderAlert[];
}

export type ConfigPayload = ConfigData;

export interface UploadResult extends Extensible {
  url: string;
  fileName?: string;
  originalName?: string;
}

export interface OrderCreateItem extends Extensible {
  dishId?: number;
  specId?: number;
  quantity?: number;
  itemRemark?: string;
}

export interface OrderCreatePayload extends ApiPayload {
  tableId?: number;
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

export interface UrgeResult extends Extensible {
  orderNo?: string;
  cooldown?: number;
}

export interface PayQr extends Extensible {
  wx?: string;
  ali?: string;
  wxQr?: string;
  aliQr?: string;
}

export interface PayCreatePayload extends ApiPayload {
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

export interface Urge extends Extensible {
  urgeId?: number;
  orderId?: number;
  orderNo?: string;
  tableId?: number;
  tableNo?: string;
  tableName?: string;
  status?: number;
  createTime?: string;
  handleTime?: NullableString;
}

export interface OrderActionPayload extends ApiPayload {
  orderId?: number;
  orderNo?: string;
  orderStatus?: number;
  payType?: string;
  settleType?: string;
  settleRemark?: string;
  /** 结账时是否出小票(省略时后端按“打印”处理,兼容旧行为) */
  printTicket?: boolean;
  cancelReason?: string;
  reason?: string;
}

export interface OrderEditPayload extends OrderActionPayload {
  personCount?: number;
  orderRemark?: string;
  items?: OrderItem[];
}

export interface RefundPayload extends ApiPayload {
  refundId?: number;
  orderId?: number;
  amount?: number;
  reason?: string;
}

export interface Refund extends Extensible {
  refundId?: number;
  orderId?: number;
  orderNo?: string;
  refundNo?: string;
  amount?: number;
  reason?: string;
  status?: number;
  msg?: string;
  transactionId?: string;
  createTime?: string;
  updateTime?: string;
}

export interface RefundQueryResult extends ApiResponseLike {
  refund?: Refund;
}

export interface ApiResponseLike extends Extensible {
  code?: number;
  msg?: string;
}

export interface PrinterPayload extends ApiPayload {
  printerId?: number;
  printerName?: string;
  printerType?: number;
  provider?: string;
  ip?: string;
  port?: number;
  feieSn?: string;
  paperWidth?: number;
  copies?: number;
  categoryIdList?: number[];
  status?: number;
}

export interface PrinterBindPayload extends ApiPayload {
  sn?: string;
  key?: string;
  name?: string;
  phone?: string;
}

export interface PrinterStatus extends Extensible {
  online?: boolean;
  status?: string;
  todayPrinted?: number;
  todayWaiting?: number;
}

export interface FeieInfo extends Extensible {
  configured?: boolean;
  user?: string;
}

export interface PrintAgent extends Extensible {
  agentId?: number;
  agentName?: string;
  tokenHint?: string;
  printerIds?: string;
  printerIdList?: number[];
  status?: number;
  lastSeen?: string;
  /** 由服务端按同一时区判定的在线状态（见 print.AgentSeenOnline）。前端不要自己解析 lastSeen：它按服务端墙钟格式化，浏览器时区不同会恒判离线 */
  online?: boolean;
  lastReport?: string;
  createTime?: string;
  updateTime?: string;
  // 从 lastReport「代理名|软件版本」解析出的软件版本;老后端无此字段,按缺省容错。
  version?: string;
}

export interface AgentInfo extends Extensible {
  configured?: boolean;
  online?: boolean;
  lastSeen?: string;
  pending?: number;
  dead?: number;
  // v2 多代理身份与最老待打印单等待秒数;老后端无这两个字段,前端按缺省容错。
  oldestPendingSec?: number;
  agents?: PrintAgent[];
  // 云端配置的代理最新版本号;留空表示不提示升级。
  latestAgentVersion?: string;
}

export interface AgentPayload extends ApiPayload {
  agentName?: string;
  printerIds?: string;
}

export interface AgentSaveResult extends Extensible {
  token?: string;
  tokenHint?: string;
  agentId?: number;
}

// 签发 legacy 全局代理令牌的返回:明文令牌只出现一次;
// version 是落库后的新配置指纹,前端据此刷新乐观锁,避免下次保存撞 409。
export interface AgentTokenResult extends Extensible {
  token?: string;
  tokenHint?: string;
  version?: string;
}

export interface PrintCommandPayload extends ApiPayload {
  orderId?: number;
  printerId?: number;
  docType?: string;
}
