package po

// 退款状态。
const (
	RefundStatusProcessing = 0 // 处理中
	RefundStatusSuccess    = 1 // 成功
	RefundStatusFail       = 2 // 失败
)

// Refund 对应 tb_refund 表:退款记录。
type Refund struct {
	RefundID        int
	OrderID         int
	OrderNo         string
	PaymentID       int
	RefundNo        string
	Channel         string
	ChannelRefundNo string
	Amount          int64 // 退款金额(分)
	Status          int
	Reason          string
	Operator        string
	FailReason      string
	CreateTime      string
	UpdateTime      string
	IsDuplicate     int // 1=重复支付自动原路退回
}
