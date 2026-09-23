package dto

import "dining-system/internal/po"

// Refund 对应 tb_refund 表:退款记录(API 出参)。金额单位为分,不做单位转换。
//
// 注意:当前未被任何 handler 使用。退款列表接口 PayRefundList 的出参合同
// 与本结构不同(amount 为元 po.ToYuan、channel 为中文展示名、duplicate 为
// 派生 bool),不得直接拿本结构替换该端点的手写投影,否则会造成分↔元漂移。
type Refund struct {
	RefundID        int    `json:"refundId"`
	OrderID         int    `json:"orderId"`
	OrderNo         string `json:"orderNo"`
	PaymentID       int    `json:"paymentId"`
	RefundNo        string `json:"refundNo"`
	Channel         string `json:"channel"`
	ChannelRefundNo string `json:"channelRefundNo"`
	Amount          int64  `json:"amount"` // 退款金额(分)
	Status          int    `json:"status"`
	Reason          string `json:"reason"`
	Operator        string `json:"operator"`
	FailReason      string `json:"failReason"`
	CreateTime      string `json:"createTime"`
	UpdateTime      string `json:"updateTime"`
	// IsDuplicate 1=重复支付自动原路退回:该笔钱从未计入订单营收,不占退款额度。
	IsDuplicate int `json:"isDuplicate"`
}

// FromRefund 将持久化对象转为 API 出参。
func FromRefund(p po.Refund) Refund {
	return Refund{
		RefundID:        p.RefundID,
		OrderID:         p.OrderID,
		OrderNo:         p.OrderNo,
		PaymentID:       p.PaymentID,
		RefundNo:        p.RefundNo,
		Channel:         p.Channel,
		ChannelRefundNo: p.ChannelRefundNo,
		Amount:          p.Amount,
		Status:          p.Status,
		Reason:          p.Reason,
		Operator:        p.Operator,
		FailReason:      p.FailReason,
		CreateTime:      p.CreateTime,
		UpdateTime:      p.UpdateTime,
		IsDuplicate:     p.IsDuplicate,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (r Refund) ToPO() po.Refund {
	return po.Refund{
		RefundID:        r.RefundID,
		OrderID:         r.OrderID,
		OrderNo:         r.OrderNo,
		PaymentID:       r.PaymentID,
		RefundNo:        r.RefundNo,
		Channel:         r.Channel,
		ChannelRefundNo: r.ChannelRefundNo,
		Amount:          r.Amount,
		Status:          r.Status,
		Reason:          r.Reason,
		Operator:        r.Operator,
		FailReason:      r.FailReason,
		CreateTime:      r.CreateTime,
		UpdateTime:      r.UpdateTime,
		IsDuplicate:     r.IsDuplicate,
	}
}
