package po

// Payment 对应 tb_payment 表:支付流水。
type Payment struct {
	PaymentID      int
	OrderNo        string
	Channel        string // wxpay / alipay
	ChannelTradeNo string
	Amount         int64 // 支付金额(分)
	Status         int   // 0待支付 1已支付 2已关闭 3退款中 4已退款 5退款失败
	PrepayID       string
	NotifyTime     string
	CreateTime     string
	UpdateTime     string
}
