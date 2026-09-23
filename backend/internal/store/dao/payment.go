package dao

import (
	"database/sql"

	"dining-system/internal/store"
)

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
// 因此新写入时用 "P_<order_no>" 占位保证唯一。
//
// 为什么先「插入占位行」再「无条件按最新一条待支付流水更新」,而不是先查再插:
// 先查再插存在 check-then-act 竞态 —— 两个并发请求都查不到待支付流水、都走 INSERT,
// 会同时命中 (channel, channel_trade_no) 唯一索引,后到者直接报错。改成 INSERT IGNORE
// 后,并发下只会有一个真正插入、另一个被忽略,二者随后都会落到同一条待支付流水上更新,
// 竞态自然消除。占位行只负责抢占唯一性,金额与 prepay_id 以随后的 UPDATE 为准。
func InsertPayment(orderNo, channel string, amountCents int64, prepayID string) error {
	_, err := store.DB.Exec(store.InsertIgnoreInto("tb_payment",
		"order_no", "channel", "amount", "status", "prepay_id", "channel_trade_no", "create_time", "update_time"),
		orderNo, channel, amountCents, PayStatusPending, prepayID, "P_"+orderNo, store.Now(), store.Now())
	if err != nil {
		return err
	}
	// 占位行可能已存在:正常复用(待支付)或被关单(closed,改单/结账时 closePendingPayments
	// 会把全部渠道待支付单关闭)。无论哪种,统一把占位行重置为待支付并更新金额与 prepay_id,
	// 覆盖「关单后重新发起支付」的场景:channel_trade_no 仍为 "P_<order_no>" 占位的行
	// 说明从未支付成功过,复活为待支付是安全的;已支付的行 channel_trade_no 已被真实
	// 交易号替换,不会命中本 UPDATE。
	_, err = store.DB.Exec(`UPDATE tb_payment SET status=?, amount=?, prepay_id=?, update_time=? WHERE order_no=? AND channel=? AND channel_trade_no=?`,
		PayStatusPending, amountCents, prepayID, store.Now(), orderNo, channel, "P_"+orderNo)
	return err
}

// GetPayment 查询某订单某渠道最近一条支付流水。
func GetPayment(orderNo, channel string) (paymentID int, status int, amountCents int64, err error) {
	err = store.DB.QueryRow(`SELECT payment_id, status, amount FROM tb_payment WHERE order_no=? AND channel=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel).Scan(&paymentID, &status, &amountCents)
	return
}

// MarkPaymentPaid 支付成功:回写渠道交易号与状态(幂等:仅当仍为待支付时更新)。
// 只更新该订单+渠道最新的一条待支付流水,避免多条流水同时写入同一交易号撞唯一索引。
//
// 为什么拆成「先查主键、再按主键更新」两步,而不是一条 UPDATE ... WHERE payment_id=(SELECT ...):
// MySQL 不允许 UPDATE 的子查询引用正在被更新的目标表(ERROR 1093,SQLite 无此限制),
// 单条同表子查询在 MySQL 部署下支付回调必然失败,因此拆成两步以兼容双库。
func MarkPaymentPaid(orderNo, channel, channelTradeNo string) (bool, error) {
	var paymentID int64
	err := store.DB.QueryRow(`SELECT payment_id FROM tb_payment WHERE order_no=? AND channel=? AND status=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel, PayStatusPending).Scan(&paymentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil // 无待支付流水可回写(流水缺失或已关闭),由调用方告警
		}
		return false, err
	}

	res, err := store.DB.Exec(`UPDATE tb_payment SET channel_trade_no=?, status=?, notify_time=?, update_time=?
		WHERE payment_id=? AND status=?`,
		channelTradeNo, PayStatusPaid, store.Now(), store.Now(), paymentID, PayStatusPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkPaymentStatus 更新支付流水状态。
func MarkPaymentStatus(paymentID, status int) error {
	_, err := store.DB.Exec(`UPDATE tb_payment SET status=?, update_time=? WHERE payment_id=?`, status, store.Now(), paymentID)
	return err
}

// GetOrderAmount 按订单号查询订单的支付金额(分)与状态。
func GetOrderAmount(orderNo string) (orderID, payStatus, orderStatus int, totalCents int64, err error) {
	err = store.DB.QueryRow(`SELECT order_id, pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`,
		orderNo).Scan(&orderID, &payStatus, &orderStatus, &totalCents)
	return
}

// LastPaymentChannel 查询订单最近一条支付流水的渠道。
func LastPaymentChannel(orderNo string) (string, error) {
	var ch string
	err := store.DB.QueryRow(`SELECT channel FROM tb_payment WHERE order_no=? ORDER BY payment_id DESC LIMIT 1`, orderNo).Scan(&ch)
	return ch, err
}

// GetRefundOrderInfo 查询退款所需的订单支付信息。
func GetRefundOrderInfo(orderID int) (orderNo string, payStatus int, totalCents int64, err error) {
	err = store.DB.QueryRow(`SELECT order_no, pay_status, total_amount FROM tb_order WHERE order_id=?`, orderID).
		Scan(&orderNo, &payStatus, &totalCents)
	return
}

// LastPaidPayment 查询订单最近一条已支付或(部分)已退款的支付流水。
func LastPaidPayment(orderNo string) (paymentID int, channel string, err error) {
	err = store.DB.QueryRow(`SELECT payment_id, channel FROM tb_payment
		WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC LIMIT 1`,
		orderNo, PayStatusPaid, PayStatusRefunded).Scan(&paymentID, &channel)
	return
}

// DeductPaidAmount 扣减订单实收金额(退款入账),下限为 0 避免超额扣成负数。
func DeductPaidAmount(orderNo string, cents int64) error {
	_, err := store.DB.Exec(`UPDATE tb_order SET paid_amount = CASE
		WHEN COALESCE(paid_amount,0) - ? < 0 THEN 0 ELSE COALESCE(paid_amount,0) - ? END
		WHERE order_no=?`, cents, cents, orderNo)
	return err
}

// DeductPaidAmountTx 是 DeductPaidAmount 的事务版本。
func DeductPaidAmountTx(tx *sql.Tx, orderNo string, cents int64) error {
	_, err := tx.Exec(`UPDATE tb_order SET paid_amount = CASE
		WHEN COALESCE(paid_amount,0) - ? < 0 THEN 0 ELSE COALESCE(paid_amount,0) - ? END
		WHERE order_no=?`, cents, cents, orderNo)
	return err
}

// GetOrderNoByID 按订单 ID 查询订单号。
func GetOrderNoByID(orderID int) (string, error) {
	var orderNo string
	err := store.DB.QueryRow(`SELECT order_no FROM tb_order WHERE order_id=?`, orderID).Scan(&orderNo)
	return orderNo, err
}

// PendingPayment 待支付流水(用于关闭渠道单)。
type PendingPayment struct {
	ID      int
	Channel string
}

// ListPendingPayments 查询订单除 exceptChannel 外所有渠道的待支付流水
// (exceptChannel 为空串时返回全部渠道)。
func ListPendingPayments(orderNo, exceptChannel string) []PendingPayment {
	rows, err := store.DB.Query(`SELECT payment_id, channel FROM tb_payment
		WHERE order_no=? AND status=? AND channel<>?`,
		orderNo, PayStatusPending, exceptChannel)
	if err != nil {
		return nil
	}
	defer rows.Close()
	list := []PendingPayment{}
	for rows.Next() {
		var p PendingPayment
		if rows.Scan(&p.ID, &p.Channel) == nil {
			list = append(list, p)
		}
	}
	return list
}
