package po

import "strings"

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

// OrderItem 对应 tb_order_item 表。
type OrderItem struct {
	ItemID   int
	DishID   int
	DishName string
	SpecID   int
	SpecName string
	Price    int64 // 单价(分)
	Quantity int
	Amount   int64 // 小计金额(分)
	Remark   string
}

// Order 对应 tb_order 表。
type Order struct {
	OrderID        int
	OrderNo        string
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
	FinishTime       *string
	OrderRemark      string
	CancelReason     string
	BeginTime        *string
	EndTime          *string
	CreateBy         string
	CreateTime       string
	UpdateBy         string
	UpdateTime       string
	Remark           *string
}
