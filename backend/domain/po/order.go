package po

import (
	"strconv"
	"strings"
)

// ShortOrderNo 从完整订单号派生一个可口头报读的短号。
//
// 完整单号形如 D261008123456K3M9P2R(20 位),顾客既读不出来
// 也不方便向服务员报号。取其尾部 6 位作为短号(如 3M9P2R):
//   - 由单号派生,无需额外存储与回填,历史订单同样有效;
//   - 订单列表原本就按 order_no LIKE %关键词% 查询,输入短号即可命中;
//   - 随机段改用 Base32 后,尾 6 位含 30bit(约 10.7 亿种),比原先十六进制的
//     24bit(1677 万种)更不易撞号。
//
// 新单请优先用 DisplayNo:它先取 pickup_no,只有缺号时才退回本函数。
func ShortOrderNo(orderNo string) string {
	s := strings.TrimSpace(orderNo)
	if len(s) <= 6 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[len(s)-6:])
}

// DisplayNo 返回可供顾客口头报读的取餐号。
//
// 优先取 pickup_no(纯数字、当天从 1 递增、足够短,pickup_no 见 tb_order.biz_date/pickup_no);
// pickup_no 为 0 的历史订单或手工补录行,退回 ShortOrderNo 从单号派生的尾 6 位,
// 保证展示层永远有值可显示,不会因为新列没回填就露出空白。
func DisplayNo(pickupNo int, orderNo string) string {
	if pickupNo > 0 {
		return strconv.Itoa(pickupNo)
	}
	return ShortOrderNo(orderNo)
}

// OrderItem 对应 tb_order_item 表。
type OrderItem struct {
	ItemID          int
	OrderID         int
	DishID          int
	DishName        string
	SpecID          int
	SpecName        string
	Price           int64 // 单价(分)
	Quantity        int
	Amount          int64 // 小计金额(分)
	Remark          string
	BatchNo         int    // 加菜批次:1=首次下单,每次加菜 +1
	ClientRequestID string // 幂等键:同一次提交的明细共享,用于下单/加菜去重
	IsGift          int    // 是否赠送:0 正常计费 1 赠送(amount 记 0,原价计入订单 gift_amount)
	GiftReason      string // 赠送原因(可选,商家改单时填写)
}

// Order 对应 tb_order 表。
type Order struct {
	OrderID        int
	OrderNo        string
	BizDate        string // 营业日 'YYYY-MM-DD':取餐号按天分组的分区键,随下单写入
	PickupNo       int    // 当日取餐号(从 1 递增),供顾客口头报号;0 表示未分配
	TableID        int
	TableNo        string // tb_order 冗余列
	TableName      string // tb_order 冗余列
	PersonCount    int
	OrderStatus    int   // 1已下单 2制作中 3已上齐(用餐中) 4已完成 5已取消
	DishAmount     int64 // 菜品金额(分)
	SeatFee        int64 // 座位费(分)
	DiscountAmount int64 // 优惠金额(分)
	TotalAmount    int64 // 订单总额(分)
	PayStatus      int   // 0未支付 1已支付
	PayType        *string
	PayTime        *string
	TransactionID  string  // 第三方支付渠道交易号
	PayChannel     string  // 支付渠道: wxpay / alipay / offline(码牌) / 空
	RefundAmount   int64   // 累计已退款金额(分)
	RefundTime     *string // 最近一次退款成功时间
	// ---- 结算方式:免单 / 挂账 ----
	SettleType       string  // normal 正常收款 / free 免单 / credit 挂账
	SettleTime       *string // 结算(免单/挂账/收款)时间
	SettleOperator   string  // 结算操作人
	SettleRemark     string  // 免单原因 / 挂账人备注
	CreditStatus     int     // 0 非挂账 1 挂账待收款 2 挂账已结清
	CreditAmount     int64   // 挂账金额(分)
	CreditSettleTime *string // 挂账核销时间
	CreditSettleBy   string  // 挂账核销操作人
	PaidAmount       int64   // 实收金额(分):免单=0,挂账核销前=0
	GiftAmount       int64   // 赠送金额(分):赠送菜品按原价计的金额合计,不计入应收
	FinishTime       *string
	// 用餐起止时间(落地值,而非实时推算):
	//   meal_start_time —— 下单(开台)时写入,与 create_time 同值,单独成列是为了
	//     让「用餐时长」有独立于下单时间列的语义(将来排队的预订局、换桌续局不改下单时间);
	//   meal_end_time —— 订单走到终态(4 已完成 / 5 已取消)时写入,即「这桌坐到了什么时候」。
	// 历史行与进行中订单为 NULL,读取方回落到 create_time / finish_time。
	MealStartTime *string
	MealEndTime   *string
	// ViewedAt 商家首次在平台查看/操作该订单的时间;NULL 表示新单还没人看过。
	// 列表据此给「已下单」的未查看订单打「新」角标,查看详情或任意操作后写入(不再改写)。
	ViewedAt     *string
	OrderRemark  string
	CancelReason string
	CreateBy     string
	CreateTime   string
	UpdateBy     string
	UpdateTime   string
	Remark       *string
}
