package store

// ============ 支付流水 ============

// 支付流水状态。
const (
	PayStatusPending    = 0 // 待支付
	PayStatusPaid       = 1 // 已支付
	PayStatusClosed     = 2 // 已关闭
	PayStatusRefunding  = 3 // 退款中
	PayStatusRefunded   = 4 // 已退款
	PayStatusRefundFail = 5 // 退款失败
)

// InsertPayment 写入一条支付流水(待支付)。
//
// 注意:tb_payment 上有 (channel, channel_trade_no) 唯一索引,待支付阶段尚无渠道交易号,
// 若直接写入空串,同渠道的第二笔待支付流水会撞唯一约束而丢失(导致回调无法回写、无法退款)。
// 因此:同一订单+渠道已存在待支付流水时复用并更新;新写入时用 "P_<order_no>" 占位保证唯一。
func InsertPayment(orderNo, channel string, amountCents int64, prepayID string) error {
	var paymentID int64
	err := DB.QueryRow(`SELECT payment_id FROM tb_payment WHERE order_no=? AND channel=? AND status=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel, PayStatusPending).Scan(&paymentID)
	if err == nil {
		_, err = DB.Exec(`UPDATE tb_payment SET amount=?, prepay_id=?, update_time=? WHERE payment_id=?`,
			amountCents, prepayID, Now(), paymentID)
		return err
	}
	_, err = DB.Exec(`INSERT INTO tb_payment(order_no, channel, amount, status, prepay_id, channel_trade_no, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?)`, orderNo, channel, amountCents, PayStatusPending, prepayID, "P_"+orderNo, Now(), Now())
	return err
}

// GetPayment 查询某订单某渠道最近一条支付流水。
func GetPayment(orderNo, channel string) (paymentID int, status int, amountCents int64, err error) {
	err = DB.QueryRow(`SELECT payment_id, status, amount FROM tb_payment WHERE order_no=? AND channel=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel).Scan(&paymentID, &status, &amountCents)
	return
}

// MarkPaymentPaid 支付成功:回写渠道交易号与状态(幂等:仅当仍为待支付时更新)。
// 只更新该订单+渠道最新的一条待支付流水,避免多条流水同时写入同一交易号撞唯一索引。
func MarkPaymentPaid(orderNo, channel, channelTradeNo string) (bool, error) {
	res, err := DB.Exec(`UPDATE tb_payment SET channel_trade_no=?, status=?, notify_time=?, update_time=?
		WHERE payment_id=(SELECT payment_id FROM tb_payment WHERE order_no=? AND channel=? AND status=? ORDER BY payment_id DESC LIMIT 1)`,
		channelTradeNo, PayStatusPaid, Now(), Now(), orderNo, channel, PayStatusPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkPaymentStatus 更新支付流水状态。
func MarkPaymentStatus(paymentID, status int) error {
	_, err := DB.Exec(`UPDATE tb_payment SET status=?, update_time=? WHERE payment_id=?`, status, Now(), paymentID)
	return err
}

// GetOrderAmount 按订单号查询订单的支付金额(分)与状态。
func GetOrderAmount(orderNo string) (orderID, payStatus, orderStatus int, totalCents int64, err error) {
	err = DB.QueryRow(`SELECT order_id, pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`,
		orderNo).Scan(&orderID, &payStatus, &orderStatus, &totalCents)
	return
}
