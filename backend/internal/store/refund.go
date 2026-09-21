package store

// ============ 退款流水 ============

// 退款单状态。
const (
	RefundStatusProcessing = 0 // 退款中(已受理,等待渠道到账)
	RefundStatusSuccess    = 1 // 已退款
	RefundStatusFail       = 2 // 退款失败
)

// Refund 退款流水。
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
}

// InsertRefund 登记一笔退款单(退款中)。
func InsertRefund(r Refund) (int, error) {
	res, err := DB.Exec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.OrderID, r.OrderNo, r.PaymentID, r.RefundNo, r.Channel, r.ChannelRefundNo,
		r.Amount, r.Status, r.Reason, r.Operator, r.FailReason, Now(), Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// GetRefund 按主键读取退款单。
func GetRefund(refundID int) (Refund, error) {
	var r Refund
	err := DB.QueryRow(`SELECT refund_id, order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time
		FROM tb_refund WHERE refund_id=?`, refundID).
		Scan(&r.RefundID, &r.OrderID, &r.OrderNo, &r.PaymentID, &r.RefundNo, &r.Channel,
			&r.ChannelRefundNo, &r.Amount, &r.Status, &r.Reason, &r.Operator, &r.FailReason, &r.CreateTime, &r.UpdateTime)
	return r, err
}

// ListRefunds 查询某订单的退款记录(按时间倒序)。
func ListRefunds(orderID int) ([]Refund, error) {
	rows, err := DB.Query(`SELECT refund_id, order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time
		FROM tb_refund WHERE order_id=? ORDER BY refund_id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Refund{}
	for rows.Next() {
		var r Refund
		if err := rows.Scan(&r.RefundID, &r.OrderID, &r.OrderNo, &r.PaymentID, &r.RefundNo, &r.Channel,
			&r.ChannelRefundNo, &r.Amount, &r.Status, &r.Reason, &r.Operator, &r.FailReason, &r.CreateTime, &r.UpdateTime); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// MarkRefund 更新退款单状态(成功/失败)。
func MarkRefund(refundID, status int, channelRefundNo, failReason string) error {
	_, err := DB.Exec(`UPDATE tb_refund SET status=?, channel_refund_no=?, fail_reason=?, update_time=? WHERE refund_id=?`,
		status, channelRefundNo, failReason, Now(), refundID)
	return err
}

// SumRefunded 统计某订单已成功退款的总额(分)。
func SumRefunded(orderNo string) int64 {
	var cents int64
	DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund WHERE order_no=? AND status=?`,
		orderNo, RefundStatusSuccess).Scan(&cents)
	return cents
}

// AddOrderRefunded 累加订单已退款金额并记录退款时间。
func AddOrderRefunded(orderNo string, cents int64) error {
	_, err := DB.Exec(`UPDATE tb_order SET refund_amount=COALESCE(refund_amount,0)+?, refund_time=?, update_time=? WHERE order_no=?`,
		cents, Now(), Now(), orderNo)
	return err
}
