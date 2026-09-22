// Package model 定义数据模型结构体与金额换算工具。
//
// 本包不依赖任何其他内部包,是依赖关系的最底层。
// 约定:金额字段在 API 层以「元」(float64)表示,数据库以「分」(int64)存储,
// 由本包的 ToCents / ToYuan 在边界处转换。
package model

import (
	"math"
	"strings"
)

// ==================== 金额(整数分存储) ====================

// Round2 将金额四舍五入到分,规避浮点误差。
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ToCents 将元转为分(四舍五入到整数分)。
func ToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

// ToYuan 将分转为元。
func ToYuan(cents int64) float64 {
	return float64(cents) / 100.0
}

// ==================== 数据模型 ====================

type Table struct {
	TableID    int     `json:"tableId"`
	TableNo    string  `json:"tableNo"`
	TableName  string  `json:"tableName"`
	Capacity   int     `json:"capacity"`
	Status     int     `json:"status"` // 0 空闲 1 占用
	SortOrder  int     `json:"sortOrder"`
	DelFlag    string  `json:"delFlag"`
	CreateBy   string  `json:"createBy"`
	CreateTime string  `json:"createTime"`
	UpdateBy   string  `json:"updateBy"`
	UpdateTime string  `json:"updateTime"`
	Remark     *string `json:"remark"`
	// TableCode 桌台二维码稳定码:创建时生成且永不变更,二维码内容使用该码,
	// 保证「一次印刷长期有效」,同时避免自增 ID 被枚举遍历。
	TableCode string `json:"tableCode"`
}

type Category struct {
	CategoryID   int    `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	SortOrder    int    `json:"sortOrder"`
	DelFlag      string `json:"delFlag"`
	CreateTime   string `json:"createTime"`
	UpdateTime   string `json:"updateTime"`
}

type Spec struct {
	SpecID   int     `json:"specId"`
	DishID   int     `json:"dishId"`
	SpecName string  `json:"specName"`
	Price    float64 `json:"price"`
}

type Dish struct {
	DishID       int     `json:"dishId"`
	CategoryID   int     `json:"categoryId"`
	CategoryName string  `json:"categoryName"`
	DishName     string  `json:"dishName"`
	DishImage    string  `json:"dishImage"`
	Description  string  `json:"description"`
	Status       int     `json:"status"` // 1 上架 0 下架
	SortOrder    int     `json:"sortOrder"`
	DelFlag      string  `json:"delFlag"`
	CreateBy     string  `json:"createBy"`
	CreateTime   string  `json:"createTime"`
	UpdateBy     string  `json:"updateBy"`
	UpdateTime   string  `json:"updateTime"`
	Remark       *string `json:"remark"`
	Specs        []Spec  `json:"specs"`
}

type Remark struct {
	RemarkID   int    `json:"remarkId"`
	OptionName string `json:"optionName"`
	SortOrder  int    `json:"sortOrder"`
	DelFlag    string `json:"delFlag"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
}

// 打印机接入方式。
const (
	PrinterProviderTCP  = "tcp"  // 网络热敏机: 后端拼 ESC/POS 走 IP:9100
	PrinterProviderFeie = "feie" // 飞鹅云打印机: 后端走开放平台 HTTP 接口推单
	// PrinterProviderAgent 本地打印代理: 后端只把票据入队,由门店内网常驻的
	// 代理程序出站拉单后再向 IP:9100 直发。后端部署在云服务器时,这是
	// 复用门店已有网络打印机的唯一通道(tcp 通道在云端够不到门店内网)。
	// 完整部署说明见 docs/print-agent.md。
	PrinterProviderAgent = "agent"
)

// 打印机类型。
const (
	PrinterTypeKitchen = 1 // 厨房单
	PrinterTypeGuest   = 2 // 食客小票
)

type Printer struct {
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"` // 1=厨房单 2=食客小票
	Provider    string `json:"provider"`    // tcp=网络直连 / feie=飞鹅云
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	FeieSN      string `json:"feieSn"`     // 飞鹅打印机编号(机身标签上的 SN)
	PaperWidth  int    `json:"paperWidth"` // 32=58mm 48=80mm
	Copies      int    `json:"copies"`     // 打印份数(1-5)
	// CategoryIDs 按菜品分类分单:CSV 分类 ID,空串表示收全部菜品。
	// 只有厨房单有意义(食客小票必须含全部菜品,否则金额对不上)。
	CategoryIDs string `json:"categoryIds"`
	Status      int    `json:"status"` // 0=停用 1=启用
	DelFlag     string `json:"delFlag"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`

	// 以下为运行时补算,不入库。
	CategoryIDList []int   `json:"categoryIdList,omitempty"`
	OnlineStatus   string  `json:"onlineStatus,omitempty"` // 飞鹅在线状态文本(仅 provider=feie 且主动查询时填充)
	CategoryNames  *string `json:"categoryNames,omitempty"`
}

// IsFeie 报告该打印机是否走飞鹅云接口。
func (p *Printer) IsFeie() bool { return p.Provider == PrinterProviderFeie }

// IsAgent 报告该打印机是否走本地打印代理(云后端入队 + 门店代理取单)。
func (p *Printer) IsAgent() bool { return p.Provider == PrinterProviderAgent }

// IsDirect 报告该打印机是否由后端直连 IP:9100 发送(要求后端与打印机同局域网)。
func (p *Printer) IsDirect() bool { return !p.IsFeie() && !p.IsAgent() }

// EffectiveCopies 返回实际打印份数(兜底 1,上限 5,避免误填 100 份把纸打光)。
func (p *Printer) EffectiveCopies() int {
	if p.Copies < 1 {
		return 1
	}
	if p.Copies > 5 {
		return 5
	}
	return p.Copies
}

// 打印单据类型(落 tb_print_log.doc_type)。
const (
	PrintDocKitchen = "kitchen"
	PrintDocGuest   = "guest"
)

// 打印日志状态。
const (
	PrintStatusFailed  = 0 // 发送失败
	PrintStatusSuccess = 1 // 已送出
	// PrintStatusQueued 已入队、等待本地打印代理取单。
	// 仅 provider=agent 会出现:后端不再同步送出,「已入队」与「真出纸」是两件事,
	// 用独立状态区分,否则代理掉线时日志会显示成「已送出」而实际一张纸都没吐。
	PrintStatusQueued = 2
)

// 打印触发场景(落 tb_print_log.trigger_by),用于区分「自动打印」与「人工补打」。
const (
	PrintTriggerOrder   = "order"   // 顾客下单
	PrintTriggerAppend  = "append"  // 顾客加菜
	PrintTriggerSettle  = "settle"  // 收银结账
	PrintTriggerTest    = "test"    // 测试打印
	PrintTriggerReprint = "reprint" // 人工补打
)

// PrintLog 一次「向某台打印机发送某张单据」的记录。
type PrintLog struct {
	PrintID     int    `json:"printId"`
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	ShortNo     string `json:"shortNo"`
	TableNo     string `json:"tableNo"`
	TableName   string `json:"tableName"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"`
	Provider    string `json:"provider"`
	DocType     string `json:"docType"`
	Copies      int    `json:"copies"`
	Status      int    `json:"status"`
	RemoteID    string `json:"remoteId"` // 飞鹅返回的云端订单号(可用于追问打印状态)
	Detail      string `json:"detail"`   // TCP: 地址 / 失败原因;飞鹅: 云端订单号或错误信息
	TriggerBy   string `json:"triggerBy"`
	Operator    string `json:"operator"`
	CostMs      int    `json:"costMs"`
	CreateTime  string `json:"createTime"`
}

// ============ 本地打印代理任务队列 ============

// 打印任务状态(落 tb_print_job.status)。
const (
	PrintJobPending = 0 // 待代理取单
	PrintJobClaimed = 1 // 已被某个代理取走,打印中(带租约,超时可被重新取走)
	PrintJobDone    = 2 // 代理已成功送出
	PrintJobDead    = 3 // 重试次数用尽,放弃(需人工补打)
)

// 打印任务重试上限:超过后置为 dead,避免坏任务在队列里无限循环。
const PrintJobMaxAttempts = 3

// PrintJob 一条「待本地打印代理送出」的打印任务。
//
// Payload 存的是渲染后的等宽文本行(用 '\n' 连接),不是厂商协议字节:
//   - 文本行与厂商无关,和小票直连/飞鹅云共用同一份渲染结果(ticket.go);
//   - ESC/POS 字节(GBK 编码 + 切纸指令)在「代理取单时」由后端现场编码后
//     以 base64 下发,代理程序因此完全不需要懂打印协议,只做字节搬运。
type PrintJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"`
	IP          string `json:"ip"`   // 打印机在门店内网的地址(云后端不直连,由代理使用)
	Port        int    `json:"port"` // 默认 9100
	DocType     string `json:"docType"`
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	Copies      int    `json:"copies"`
	PrintLogID  int    `json:"printLogId"` // 关联的 tb_print_log 主键,回执时回写结果
	// DeliveryID 幂等投递号(入队时生成、永不变更):代理打印成功后本地持久化这个号,
	// 若回执丢失导致任务被重新下发,代理凭它判断「这一条我已经打过了」,跳过打印直接回执,
	// 从根上消除「回执丢失 → 重复出票」。见 docs/print-agent.md 的「幂等投递」。
	DeliveryID string `json:"deliveryId"`
	// Payload 渲染后的文本行,以 '\n' 连接。
	Payload     string `json:"-"` // 不出现在列表接口里(体积大,只有取单接口用)
	Status      int    `json:"status"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"lastError"`
	ClaimedBy   string `json:"claimedBy"`
	ClaimTime   string `json:"claimTime"`
	NextTryTime string `json:"nextTryTime"`
	TriggerBy   string `json:"triggerBy"`
	Operator    string `json:"operator"`
	CreateTime  string `json:"createTime"`
	DoneTime    string `json:"doneTime"`
}

// AgentJob 下发给本地打印代理的单条任务(接口出参,含已编码好的字节)。
type AgentJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Copies      int    `json:"copies"`
	DocType     string `json:"docType"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	// DeliveryID 幂等投递号:代理打印成功后本地持久化,重发时据此去重(见 PrintJob.DeliveryID)。
	DeliveryID string `json:"deliveryId"`
	// Payload 为 ESC/POS 指令流的 base64(含初始化、GBK 文本、切纸)。
	Payload string `json:"payload"`
}

type Config struct {
	ShopName           string `json:"shop_name"`
	ShopLogo           string `json:"shop_logo"`
	SeatFeeEnabled     string `json:"seat_fee_enabled"`
	SeatFee            string `json:"seat_fee"`
	PayQrWx            string `json:"pay_qr_wx"`
	PayQrAli           string `json:"pay_qr_ali"`
	H5BaseURL          string `json:"h5_base_url"`
	PromotionEnabled   string `json:"promotion_enabled"`
	PromotionThreshold string `json:"promotion_threshold"`
	PromotionDiscount  string `json:"promotion_discount"`
	// 在线支付开关(未申请 key 前保持 "0",码牌收款不受影响)
	WxpayEnabled  string `json:"wxpay_enabled"`
	AlipayEnabled string `json:"alipay_enabled"`
	// 微信支付商户参数
	WxpayMchid            string `json:"wxpay_mchid"`
	WxpayAppid            string `json:"wxpay_appid"`
	WxpayApiv3Key         string `json:"wxpay_apiv3_key"`
	WxpaySerialNo         string `json:"wxpay_serial_no"`
	WxpayPrivateKeyPath   string `json:"wxpay_private_key_path"`
	WxpayPlatformCertPath string `json:"wxpay_platform_cert_path"`
	// 微信支付公钥模式(官方推荐,无物理过期时间):配置公钥ID后按该模式验签
	WxpayPubkeyID   string `json:"wxpay_pubkey_id"`
	WxpayPubkeyPath string `json:"wxpay_pubkey_path"`
	WxpayNotifyURL  string `json:"wxpay_notify_url"`
	// 支付宝商户参数
	AlipayAppid          string `json:"alipay_appid"`
	AlipayPrivateKeyPath string `json:"alipay_private_key_path"`
	AlipayPublicKey      string `json:"alipay_public_key"`
	AlipayNotifyURL      string `json:"alipay_notify_url"`
	// 小票打印
	PrintEnabled          string `json:"print_enabled"`            // 打印总开关
	PrintKitchenShowPrice string `json:"print_kitchen_show_price"` // 厨房单是否带单价金额
	// 飞鹅云打印(feie_ukey 为敏感项,回显时留空表示不修改)
	FeieUser   string `json:"feie_user"`
	FeieUkey   string `json:"feie_ukey"`
	FeieApiURL string `json:"feie_api_url"`
	// 本地打印代理令牌(敏感项):门店代理程序出站拉单时用它鉴权,
	// 与飞鹅 UKEY 同理——拿到它就能冒充代理拉走并打印任意票据,故落库加密、接口不回显。
	AgentToken string `json:"agent_token"`
}

// Payment 支付流水(与 tb_payment 表对应)。
type Payment struct {
	PaymentID      int    `json:"paymentId"`
	OrderNo        string `json:"orderNo"`
	Channel        string `json:"channel"` // wxpay / alipay
	ChannelTradeNo string `json:"channelTradeNo"`
	Amount         int64  `json:"amount"` // 支付金额(分)
	Status         int    `json:"status"` // 0待支付 1已支付 2已关闭 3退款中 4已退款 5退款失败
	PrepayID       string `json:"prepayId"`
	NotifyTime     string `json:"notifyTime"`
	CreateTime     string `json:"createTime"`
	UpdateTime     string `json:"updateTime"`
}

type OrderItem struct {
	ItemID   int     `json:"itemId"`
	DishID   int     `json:"dishId"`
	DishName string  `json:"dishName"`
	SpecID   int     `json:"specId"`
	SpecName string  `json:"specName"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
	Amount   float64 `json:"amount"`
	Remark   string  `json:"itemRemark"`
	// CategoryID 菜品所属分类,不入 tb_order_item 表 ——
	// 打印时按分类分单(凉菜机/热菜机)需要它,由打印层临时按 dish_id 关联查询补齐。
	CategoryID int `json:"categoryId,omitempty"`
}

type Order struct {
	OrderID        int     `json:"orderId"`
	OrderNo        string  `json:"orderNo"`
	ShortNo        string  `json:"shortNo"` // 可报读短号(由 orderNo 派生,不入库),供顾客向服务员报号
	TableID        int     `json:"tableId"`
	TableNo        string  `json:"tableNo"`
	TableName      string  `json:"tableName"`
	PersonCount    int     `json:"personCount"`
	OrderStatus    int     `json:"orderStatus"` // 1已下单 2制作中 3已上齐(用餐中) 4已完成 5已取消
	DishAmount     float64 `json:"dishAmount"`
	SeatFee        float64 `json:"seatFee"`
	DiscountAmount float64 `json:"discountAmount"`
	TotalAmount    float64 `json:"totalAmount"`
	PayStatus      int     `json:"payStatus"` // 0未支付 1已支付
	PayType        *string `json:"payType"`
	PayTime        *string `json:"payTime"`
	TransactionID  string  `json:"transactionId"` // 第三方支付渠道交易号
	PayChannel     string  `json:"payChannel"`    // 支付渠道: wxpay / alipay / offline(码牌) / 空
	RefundAmount   float64 `json:"refundAmount"`  // 累计已退款金额(元)
	RefundTime     *string `json:"refundTime"`    // 最近一次退款成功时间
	// ---- 结算方式:免单 / 挂账 ----
	SettleType       string      `json:"settleType"`       // normal 正常收款 / free 免单 / credit 挂账
	SettleTime       *string     `json:"settleTime"`       // 结算(免单/挂账/收款)时间
	SettleOperator   string      `json:"settleOperator"`   // 结算操作人
	SettleRemark     string      `json:"settleRemark"`     // 免单原因 / 挂账人备注
	CreditStatus     int         `json:"creditStatus"`     // 0 非挂账 1 挂账待收款 2 挂账已结清
	CreditAmount     float64     `json:"creditAmount"`     // 挂账金额(元)
	CreditSettleTime *string     `json:"creditSettleTime"` // 挂账核销时间
	CreditSettleBy   string      `json:"creditSettleBy"`   // 挂账核销操作人
	PaidAmount       float64     `json:"paidAmount"`       // 实收金额(元):免单=0,挂账核销前=0
	FinishTime       *string     `json:"finishTime"`
	OrderRemark      string      `json:"orderRemark"`
	CancelReason     string      `json:"cancelReason"`
	BeginTime        *string     `json:"beginTime"`
	EndTime          *string     `json:"endTime"`
	CreateBy         string      `json:"createBy"`
	CreateTime       string      `json:"createTime"`
	UpdateBy         string      `json:"updateBy"`
	UpdateTime       string      `json:"updateTime"`
	Remark           *string     `json:"remark"`
	PendingUrge      bool        `json:"pendingUrge"` // 是否存在未处理的催菜(运行时填充,不入库)
	Items            []OrderItem `json:"items"`
}

// ShortOrderNo 从完整订单号派生一个可口头报读的短号。
//
// 完整单号形如 D202609200122298a45bf800674d485(31 位),顾客既读不出来
// 也不方便向服务员报号。取其尾部 6 位作为短号(如 74D485):
//   - 由单号派生,无需额外存储与回填,历史订单同样有效;
//   - 订单列表原本就按 order_no LIKE %关键词% 查询,输入短号即可命中;
//   - 随机段取自 crypto/rand,同批次重复概率可忽略。
func ShortOrderNo(orderNo string) string {
	s := strings.TrimSpace(orderNo)
	if len(s) <= 6 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[len(s)-6:])
}

// ============ 催菜(顾客呼叫后厨加急) ============

// UrgeStatus 催菜处理状态。
const (
	UrgeStatusPending  = 0 // 待处理
	UrgeStatusHandled  = 1 // 已处理
	UrgeTypeUrge       = "urge"
	UrgeCooldownSecond = 180 // 同一订单两次催菜的最小间隔(秒),防止顾客连点刷屏
)

// OrderUrge 一条催菜记录。顾客可多次催菜,每次独立成记录,
// 商家端按「未处理」筛选即可知道哪些桌正在催。
type OrderUrge struct {
	UrgeID     int     `json:"urgeId"`
	OrderID    int     `json:"orderId"`
	OrderNo    string  `json:"orderNo"`
	ShortNo    string  `json:"shortNo"`
	TableID    int     `json:"tableId"`
	TableNo    string  `json:"tableNo"`
	TableName  string  `json:"tableName"`
	UrgeType   string  `json:"urgeType"`
	Status     int     `json:"status"` // 0 待处理 1 已处理
	Remark     string  `json:"remark"`
	CreateTime string  `json:"createTime"`
	HandleTime *string `json:"handleTime"`
	HandleBy   string  `json:"handleBy"`
}

// ============ 员工与权限 ============

// 员工账号状态。
const (
	UserStatusEnabled  = 1 // 启用
	UserStatusDisabled = 0 // 停用(停用后立即失去访问权,旧令牌同时失效)
)

// Role 角色(权限集合)。
type Role struct {
	RoleID    int    `json:"roleId"`
	RoleKey   string `json:"roleKey"`  // 代码内标识,内置角色不可改
	RoleName  string `json:"roleName"` // 展示名
	Perms     string `json:"perms"`    // 权限码 CSV(落库形态)
	DataScope string `json:"dataScope"`
	IsBuiltin int    `json:"isBuiltin"` // 1=内置(不可删除;admin 权限每次启动强制恢复全量)
	SortOrder int    `json:"sortOrder"`
	Status    int    `json:"status"`
	DelFlag   string `json:"delFlag"`
	// PermList 是 Perms 解析后的权限码列表,仅用于接口出入参,不入库。
	PermList   []string `json:"permList"`
	CreateTime string   `json:"createTime"`
	UpdateTime string   `json:"updateTime"`
	Remark     string   `json:"remark"`
	// UserCount 引用该角色的员工数(列表接口补算,删除前的前置检查也要用)。
	UserCount int `json:"userCount"`
}

// User 员工账号。
type User struct {
	UserID        int    `json:"userId"`
	Username      string `json:"username"`
	RealName      string `json:"realName"` // 订单操作留痕用这个值,为空时回退用户名
	RoleID        int    `json:"roleId"`
	Phone         string `json:"phone"`
	Status        int    `json:"status"` // 1 启用 0 停用
	LastLoginTime string `json:"lastLoginTime"`
	LastLoginIP   string `json:"lastLoginIp"`
	LoginCount    int    `json:"loginCount"`
	PwdUpdateTime string `json:"pwdUpdateTime"`
	DelFlag       string `json:"delFlag"`
	CreateBy      string `json:"createBy"`
	CreateTime    string `json:"createTime"`
	UpdateBy      string `json:"updateBy"`
	UpdateTime    string `json:"updateTime"`
	Remark        string `json:"remark"`

	// 以下为关联字段(来自 tb_role),不入 tb_user 表。
	RoleKey  string   `json:"roleKey"`
	RoleName string   `json:"roleName"`
	Perms    []string `json:"perms,omitempty"`
}

// DisplayName 留痕显示名:优先中文姓名,未填则退回登录名。
func (u *User) DisplayName() string {
	if strings.TrimSpace(u.RealName) != "" {
		return u.RealName
	}
	return u.Username
}

// ============ 操作日志(审计) ============
//
// 业务表上的 create_by / update_by 只回答了「谁改的」,回答不了
// 「什么时候、用什么参数、改成了什么、成功了没」。tb_oper_log 补齐这一层:
// 每次管理端写操作落一行,只追加、不修改,供事后追责与对账。

// 操作类型(落 business_type),用于在列表里快速筛出某一类动作。
const (
	OperTypeInsert = "insert" // 新增
	OperTypeUpdate = "update" // 修改
	OperTypeDelete = "delete" // 删除
	OperTypeLogin  = "login"  // 登录(含失败尝试)
	OperTypeGrant  = "grant"  // 授权类:改角色 / 重置密码 / 启停用
	OperTypePrint  = "print"  // 会触发出纸的动作(测试页 / 补打)
	OperTypeOther  = "other"  // 其它(查询类写接口、越权尝试等)
)

// 操作结果(落 status)。
const (
	OperStatusFail    = 0 // 失败(业务报错 / 被拒绝)
	OperStatusSuccess = 1 // 成功
)

// OperLog 一条操作日志。
type OperLog struct {
	LogID        int    `json:"logId"`
	Module       string `json:"module"`       // 模块中文名:订单管理 / 员工管理 / 登录
	BusinessType string `json:"businessType"` // 见 OperType* 常量
	Action       string `json:"action"`       // 动作中文名:修改菜品 / 重置密码
	Method       string `json:"method"`       // 路由模板 "POST /prod-api/dining/dish/update"
	RequestURL   string `json:"requestUrl"`   // 含 query 的真实 URL
	OperatorID   int    `json:"operatorId"`   // 0 表示未登录 / 无法识别(如密码错误)
	Operator     string `json:"operator"`     // 显示名快照:改名后历史日志不变
	OperatorRole string `json:"operatorRole"` // 当时角色名快照:改角色后历史日志不变
	OperIP       string `json:"operIp"`
	TargetType   string `json:"targetType"` // 业务对象类型:order / user / dish / table ...
	TargetID     string `json:"targetId"`   // 业务对象标识(订单号 / 主键);VARCHAR 以兼容单号
	OperParam    string `json:"operParam"`  // 脱敏后的请求参数 JSON
	Detail       string `json:"detail"`     // 人话摘要:高风险动作由业务代码显式填写
	Status       int    `json:"status"`     // 见 OperStatus* 常量
	ErrorMsg     string `json:"errorMsg"`   // 失败原因(取自响应 msg)
	CostMs       int    `json:"costMs"`
	CreateTime   string `json:"createTime"`
}
