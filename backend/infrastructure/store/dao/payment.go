package dao

import (
	"database/sql"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
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

// InsertPaymentTx 是 InsertPayment 的事务版本:在订单行锁保护下落支付流水。
//
// 配合 GetOrderAmountTx 做「金额复核」:PayCreate 读金额到渠道下单之间若发生
// 加菜/改单抬价,事务内重读会发现金额已变,调用方据此拒绝出码(并关掉渠道单),
// 避免生成旧金额二维码 —— 顾客按旧金额付款后,回调会被金额防线拒之门外,
// 钱到了订单却入不了账。
func InsertPaymentTx(tx *sql.Tx, orderNo, channel string, amountCents int64, prepayID string) error {
	if _, err := tx.Exec(store.InsertIgnoreInto("tb_payment",
		"order_no", "channel", "amount", "status", "prepay_id", "channel_trade_no", "create_time", "update_time"),
		orderNo, channel, amountCents, PayStatusPending, prepayID, "P_"+orderNo, store.Now(), store.Now()); err != nil {
		return err
	}
	// 占位行可能已存在:正常复用(待支付)或被关单(closed,改单/结账时 closePendingPayments
	// 会把全部渠道待支付单关闭)。无论哪种,统一把占位行重置为待支付并更新金额与 prepay_id,
	// 覆盖「关单后重新发起支付」的场景:channel_trade_no 仍为 "P_<order_no>" 占位的行
	// 说明从未支付成功过,复活为待支付是安全的;已支付的行 channel_trade_no 已被真实
	// 交易号替换,不会命中本 UPDATE。
	_, err := tx.Exec(`UPDATE tb_payment SET status=?, amount=?, prepay_id=?, update_time=? WHERE order_no=? AND channel=? AND channel_trade_no=?`,
		PayStatusPending, amountCents, prepayID, store.Now(), orderNo, channel, "P_"+orderNo)
	return err
}

// GetOrderAmountTx 是 GetOrderAmount 的事务版本,在行锁保护下重读订单最新应收金额。
// 只填充 po.Order 的 OrderID/PayStatus/OrderStatus/TotalAmount 四列。
func GetOrderAmountTx(tx *sql.Tx, orderNo string) (po.Order, error) {
	var o po.Order
	err := tx.QueryRow(`SELECT order_id, pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`,
		orderNo).Scan(&o.OrderID, &o.PayStatus, &o.OrderStatus, &o.TotalAmount)
	return o, err
}

// MarkPaymentPaidTx 是 MarkPaymentPaid 的事务版本。
//
// 余额支付必须把「扣储值余额、落支付流水、订单入账」放进同一个事务 ——
// 渠道支付有回调重试兜底,余额扣掉之后没有任何重试机制,事务是唯一的原子性保障。
func MarkPaymentPaidTx(tx *sql.Tx, orderNo, channel, channelTradeNo string) (bool, error) {
	var paymentID int64
	err := tx.QueryRow(`SELECT payment_id FROM tb_payment WHERE order_no=? AND channel=? AND status=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel, PayStatusPending).Scan(&paymentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	res, err := tx.Exec(`UPDATE tb_payment SET channel_trade_no=?, status=?, notify_time=?, update_time=?
		WHERE payment_id=? AND status=?`,
		channelTradeNo, PayStatusPaid, store.Now(), store.Now(), paymentID, PayStatusPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetPayment 查询某订单某渠道最近一条支付流水。
//
// 只填充 po.Payment 的 PaymentID/Status/Amount 三列(调用方只需要这三项),
// 其余字段为零值,请勿按整行使用。
func GetPayment(orderNo, channel string) (po.Payment, error) {
	var p po.Payment
	err := store.DB.QueryRow(`SELECT payment_id, status, amount FROM tb_payment WHERE order_no=? AND channel=? ORDER BY payment_id DESC LIMIT 1`,
		orderNo, channel).Scan(&p.PaymentID, &p.Status, &p.Amount)
	return p, err
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
//
// 只填充 po.Order 的 OrderID/PayStatus/OrderStatus/TotalAmount 四列,其余为零值。
func GetOrderAmount(orderNo string) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_id, pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`,
		orderNo).Scan(&o.OrderID, &o.PayStatus, &o.OrderStatus, &o.TotalAmount)
	return o, err
}

// LastPaymentChannel 查询订单最近一条支付流水的渠道。
func LastPaymentChannel(orderNo string) (string, error) {
	var ch string
	err := store.DB.QueryRow(`SELECT channel FROM tb_payment WHERE order_no=? ORDER BY payment_id DESC LIMIT 1`, orderNo).Scan(&ch)
	return ch, err
}

// GetRefundOrderInfo 查询退款所需的订单支付信息。
//
// 只填充 po.Order 的 OrderNo/PayStatus/TotalAmount 三列,其余为零值。
func GetRefundOrderInfo(orderID int) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_no, pay_status, total_amount FROM tb_order WHERE order_id=?`, orderID).
		Scan(&o.OrderNo, &o.PayStatus, &o.TotalAmount)
	return o, err
}

// SumPaidPaymentsExcept 订单在「除某渠道之外」已入账的支付金额合计(分)。
//
// 混合支付(余额抵扣 + 渠道支付剩余)的金额防线全靠它:
// 渠道回调的金额只覆盖「剩余部分」,拿它跟订单总额比必然不相等;
// 正确的判据是「本笔 + 其它渠道已入账 = 订单总额」。
func SumPaidPaymentsExcept(orderNo, exceptChannel string) int64 {
	var v int64
	_ = store.DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_payment
		WHERE order_no=? AND channel<>? AND status IN (?,?)`,
		orderNo, exceptChannel, PayStatusPaid, PayStatusRefunded).Scan(&v)
	return v
}

// SumPaidPaymentsTx 事务内统计订单已入账(已支付/已退款)的支付合计(分)。
// 混合支付据此算「还差多少」:订单总额 - 已入账合计。
func SumPaidPaymentsTx(tx *sql.Tx, orderNo string) (int64, error) {
	var v int64
	err := tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_payment
		WHERE order_no=? AND status IN (?,?)`,
		orderNo, PayStatusPaid, PayStatusRefunded).Scan(&v)
	return v, err
}

// SumPaidPaymentsExceptTx 是 SumPaidPaymentsExcept 的事务版本。
func SumPaidPaymentsExceptTx(tx *sql.Tx, orderNo, exceptChannel string) (int64, error) {
	var v int64
	err := tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_payment
		WHERE order_no=? AND channel<>? AND status IN (?,?)`,
		orderNo, exceptChannel, PayStatusPaid, PayStatusRefunded).Scan(&v)
	return v, err
}

// ListPaidPayments 订单已入账(已支付 / 已退款)的支付流水,按 payment_id 倒序。
// 退款分摊按「最近的支付先退」遍历它。
func ListPaidPayments(orderNo string) ([]po.Payment, error) {
	rows, err := store.DB.Query(`SELECT payment_id, order_no, channel, COALESCE(channel_trade_no,''), amount, status
		FROM tb_payment WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC`,
		orderNo, PayStatusPaid, PayStatusRefunded)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []po.Payment{}
	for rows.Next() {
		var p po.Payment
		if err := rows.Scan(&p.PaymentID, &p.OrderNo, &p.Channel, &p.ChannelTradeNo, &p.Amount, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SumRefundedByPayment 按支付流水汇总「已成功 + 处理中」的退款金额(分)。
// 处理中的退款同样占用额度 —— 与订单级可退额度的口径保持一致。
func SumRefundedByPayment(orderNo string) (map[int]int64, error) {
	rows, err := store.DB.Query(`SELECT payment_id, COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_no=? AND status IN (?,?) AND COALESCE(is_duplicate,0)=0 GROUP BY payment_id`,
		orderNo, po.RefundStatusProcessing, po.RefundStatusSuccess)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]int64{}
	for rows.Next() {
		var pid int
		var amount int64
		if err := rows.Scan(&pid, &amount); err != nil {
			return nil, err
		}
		out[pid] = amount
	}
	return out, rows.Err()
}

// ListPaidPaymentsTx 是 ListPaidPayments 的事务版本。
func ListPaidPaymentsTx(tx *sql.Tx, orderNo string) ([]po.Payment, error) {
	rows, err := tx.Query(`SELECT payment_id, order_no, channel, COALESCE(channel_trade_no,''), amount, status
		FROM tb_payment WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC`,
		orderNo, PayStatusPaid, PayStatusRefunded)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []po.Payment{}
	for rows.Next() {
		var p po.Payment
		if err := rows.Scan(&p.PaymentID, &p.OrderNo, &p.Channel, &p.ChannelTradeNo, &p.Amount, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SumRefundedByPaymentTx 是 SumRefundedByPayment 的事务版本。
func SumRefundedByPaymentTx(tx *sql.Tx, orderNo string) (map[int]int64, error) {
	rows, err := tx.Query(`SELECT payment_id, COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_no=? AND status IN (?,?) AND COALESCE(is_duplicate,0)=0 GROUP BY payment_id`,
		orderNo, po.RefundStatusProcessing, po.RefundStatusSuccess)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]int64{}
	for rows.Next() {
		var pid int
		var amount int64
		if err := rows.Scan(&pid, &amount); err != nil {
			return nil, err
		}
		out[pid] = amount
	}
	return out, rows.Err()
}

// MarkPaymentStatusTx 是 MarkPaymentStatus 的事务版本。
func MarkPaymentStatusTx(tx *sql.Tx, paymentID, status int) error {
	_, err := tx.Exec(`UPDATE tb_payment SET status=?, update_time=? WHERE payment_id=?`, status, store.Now(), paymentID)
	return err
}

// LastPaidPayment 查询订单最近一条已支付或(部分)已退款的支付流水。
//
// 只填充 po.Payment 的 PaymentID/Channel 两列,其余为零值。
func LastPaidPayment(orderNo string) (po.Payment, error) {
	var p po.Payment
	err := store.DB.QueryRow(`SELECT payment_id, channel FROM tb_payment
		WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC LIMIT 1`,
		orderNo, PayStatusPaid, PayStatusRefunded).Scan(&p.PaymentID, &p.Channel)
	return p, err
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

// ListPendingPayments 查询订单除 exceptChannel 外所有渠道的待支付流水
// (exceptChannel 为空串时返回全部渠道)。
//
// 只填充 po.Payment 的 PaymentID/Channel 两列(关闭渠道单只需要这两项),其余为零值。
func ListPendingPayments(orderNo, exceptChannel string) []po.Payment {
	rows, err := store.DB.Query(`SELECT payment_id, channel FROM tb_payment
		WHERE order_no=? AND status=? AND channel<>?`,
		orderNo, PayStatusPending, exceptChannel)
	if err != nil {
		return nil
	}
	defer rows.Close()
	list := []po.Payment{}
	for rows.Next() {
		var p po.Payment
		if rows.Scan(&p.PaymentID, &p.Channel) == nil {
			list = append(list, p)
		}
	}
	return list
}
