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
  currentOrder?: OrderSummary | OrderDetail | null;
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
  finishTime?: NullableString;
  beginTime?: NullableString;
  endTime?: NullableString;
  createBy?: string;
  createTime?: string;
  updateBy?: string;
  updateTime?: string;
  remark?: NullableString;
  pendingUrge?: boolean;
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
}

// 票据预览(后端重放渲染返回的等宽文本行;按等宽字体展示即与出纸 1:1)。
export interface TicketPreview {
  /** 按实际入队规则切好的段:超长票据打印时会拆成多段依次送出,与预览分段一一对应 */
  chunks: TicketLine[][];
  /** 每行可容纳的半角字符数(58mm 纸=32,80mm 纸=48),前端排版用 */
  lineWidth: number;
  docType?: string;
  orderNo?: string;
  printerName?: string;
  copies?: number;
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

export interface SettleMixRow extends Extensible {
  settleType?: string;
  label?: string;
  amount?: number;
  paidAmount?: number;
  orderCount?: number;
}

export interface PublicConfig extends Extensible {
  shop_name?: string;
  seat_fee_enabled?: string;
  seat_fee?: string;
  promotion_enabled?: string;
  promotion_threshold?: string;
  promotion_discount?: string;
  pay_qr_wx?: string;
  pay_qr_ali?: string;
  wxpay_enabled?: string;
  alipay_enabled?: string;
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
  sortOrder?: number;
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
