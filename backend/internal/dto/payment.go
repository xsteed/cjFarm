package dto

import "dining-system/internal/po"

// Payment 对应 tb_payment 表:支付流水(API 出参)。金额单位为分,不做单位转换。
//
// 注意:当前未被任何 handler 使用;接入新端点前需先确认该端点的历史
// amount 单位约定(现有支付/退款相关端点出参为元),避免分↔元漂移。
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

// FromPayment 将持久化对象转为 API 出参。
func FromPayment(p po.Payment) Payment {
	return Payment{
		PaymentID:      p.PaymentID,
		OrderNo:        p.OrderNo,
		Channel:        p.Channel,
		ChannelTradeNo: p.ChannelTradeNo,
		Amount:         p.Amount,
		Status:         p.Status,
		PrepayID:       p.PrepayID,
		NotifyTime:     p.NotifyTime,
		CreateTime:     p.CreateTime,
		UpdateTime:     p.UpdateTime,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (p Payment) ToPO() po.Payment {
	return po.Payment{
		PaymentID:      p.PaymentID,
		OrderNo:        p.OrderNo,
		Channel:        p.Channel,
		ChannelTradeNo: p.ChannelTradeNo,
		Amount:         p.Amount,
		Status:         p.Status,
		PrepayID:       p.PrepayID,
		NotifyTime:     p.NotifyTime,
		CreateTime:     p.CreateTime,
		UpdateTime:     p.UpdateTime,
	}
}
