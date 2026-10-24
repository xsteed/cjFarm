package dao

import (
	"database/sql"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
)

// ============ 退款流水 ============

// InsertRefund 登记一笔退款单(退款中)。
func InsertRefund(r po.Refund) (int, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time, is_duplicate)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.OrderID, r.OrderNo, r.PaymentID, r.RefundNo, r.Channel, r.ChannelRefundNo,
		r.Amount, r.Status, r.Reason, r.Operator, r.FailReason, store.Now(), store.Now(), r.IsDuplicate)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// InsertRefundTx 是 InsertRefund 的事务版本,用于把「校验 + 落退款单」收进同一事务。
func InsertRefundTx(tx *sql.Tx, r po.Refund) (int, error) {
	res, err := tx.Exec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time, is_duplicate)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.OrderID, r.OrderNo, r.PaymentID, r.RefundNo, r.Channel, r.ChannelRefundNo,
		r.Amount, r.Status, r.Reason, r.Operator, r.FailReason, store.Now(), store.Now(), r.IsDuplicate)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// GetRefund 按主键读取退款单。
func GetRefund(refundID int) (po.Refund, error) {
	var r po.Refund
	err := store.DB.QueryRow(`SELECT refund_id, order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time, COALESCE(is_duplicate,0)
		FROM tb_refund WHERE refund_id=?`, refundID).
		Scan(&r.RefundID, &r.OrderID, &r.OrderNo, &r.PaymentID, &r.RefundNo, &r.Channel,
			&r.ChannelRefundNo, &r.Amount, &r.Status, &r.Reason, &r.Operator, &r.FailReason, &r.CreateTime, &r.UpdateTime, &r.IsDuplicate)
	return r, err
}

// ListRefunds 查询某订单的退款记录(按时间倒序)。
func ListRefunds(orderID int) ([]po.Refund, error) {
	rows, err := store.DB.Query(`SELECT refund_id, order_id, order_no, payment_id, refund_no, channel,
		channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time, COALESCE(is_duplicate,0)
		FROM tb_refund WHERE order_id=? ORDER BY refund_id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []po.Refund{}
	for rows.Next() {
		var r po.Refund
		if err := rows.Scan(&r.RefundID, &r.OrderID, &r.OrderNo, &r.PaymentID, &r.RefundNo, &r.Channel,
			&r.ChannelRefundNo, &r.Amount, &r.Status, &r.Reason, &r.Operator, &r.FailReason, TS(&r.CreateTime), TS(&r.UpdateTime), &r.IsDuplicate); err != nil {
			return nil, err // 退款记录扫描失败必须报错,静默丢弃会让商家少看一笔退款
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPendingDuplicateRefund 查询某订单某渠道「重复支付自动退回」的未终态退款单
// (is_duplicate=1 且状态为处理中/成功)。支付通知重发时会再次进入重复支付分支,
// 若不加查重就会对同一笔支付二次调用渠道退款(商户同一笔被退两次);查到已有
// 未终态退款单时直接复用、不再发起。
//
// 只填充 po.Refund 的 RefundID/RefundNo 两列,其余为零值;
// 查不到时返回零值结构体与 nil ——「没有重复退款单」是正常分支,不是错误。
func GetPendingDuplicateRefund(orderNo, channel string) (po.Refund, error) {
	var r po.Refund
	err := store.DB.QueryRow(`SELECT refund_id, refund_no FROM tb_refund
		WHERE order_no=? AND channel=? AND COALESCE(is_duplicate,0)=1 AND status IN (?,?)
		ORDER BY refund_id DESC LIMIT 1`,
		orderNo, channel, po.RefundStatusProcessing, po.RefundStatusSuccess).Scan(&r.RefundID, &r.RefundNo)
	if err == sql.ErrNoRows {
		return po.Refund{}, nil
	}
	return r, err
}

// GetPendingDuplicateRefundTx 是 GetPendingDuplicateRefund 的事务版本。
// 配合 TouchOrderTx 使用:先在事务内锁住订单行,再查重、落退款单,
// 使「查重 → 落单」在同一订单行上串行化,消除两条并发支付通知
// (渠道重发 + 前端轮询补单)各自查重通过、对同一笔支付发起两笔退款的竞态。
func GetPendingDuplicateRefundTx(tx *sql.Tx, orderNo, channel string) (po.Refund, error) {
	var r po.Refund
	err := tx.QueryRow(`SELECT refund_id, refund_no FROM tb_refund
		WHERE order_no=? AND channel=? AND COALESCE(is_duplicate,0)=1 AND status IN (?,?)
		ORDER BY refund_id DESC LIMIT 1`,
		orderNo, channel, po.RefundStatusProcessing, po.RefundStatusSuccess).Scan(&r.RefundID, &r.RefundNo)
	if err == sql.ErrNoRows {
		return po.Refund{}, nil
	}
	return r, err
}

// MarkRefund 更新退款单状态(成功/失败)。
func MarkRefund(refundID, status int, channelRefundNo, failReason string) error {
	_, err := store.DB.Exec(`UPDATE tb_refund SET status=?, channel_refund_no=?, fail_reason=?, update_time=? WHERE refund_id=?`,
		status, channelRefundNo, failReason, store.Now(), refundID)
	return err
}

// MarkRefundSuccessOnce 幂等地把退款单置为成功:仅「非成功」状态可被置成功,
// 返回本次是否真的发生了状态变更。并发重复置成功(如退款回调与查单同时到达)时
// 只有一次返回 true,调用方据此决定是否入账,防止退款金额被重复累加。
func MarkRefundSuccessOnce(refundID int, channelRefundNo string) (bool, error) {
	res, err := store.DB.Exec(`UPDATE tb_refund SET status=?, channel_refund_no=?, fail_reason='', update_time=? WHERE refund_id=? AND status<>?`,
		po.RefundStatusSuccess, channelRefundNo, store.Now(), refundID, po.RefundStatusSuccess)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkRefundSuccessOnceTx 是 MarkRefundSuccessOnce 的事务版本,语义一致:
// 仅「非成功」状态可被置成功,返回本次是否真的发生状态变更。
// 与 AddOrderRefundedTx/DeductPaidAmountTx 配合,保证退款入账三步同生共死。
func MarkRefundSuccessOnceTx(tx *sql.Tx, refundID int, channelRefundNo string) (bool, error) {
	res, err := tx.Exec(`UPDATE tb_refund SET status=?, channel_refund_no=?, fail_reason='', update_time=? WHERE refund_id=? AND status<>?`,
		po.RefundStatusSuccess, channelRefundNo, store.Now(), refundID, po.RefundStatusSuccess)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SumRefundedWithErr 统计某订单已成功退款的总额(分),查询失败时返回 error。
// 不含重复支付退回(is_duplicate=1):那部分金额从未计入订单营收,不占退款额度。
//
// 与 SumRefunded 的区别:本函数把「查询失败」显式抛给调用方,用于「判断是否退完」
// 这类需要区分「真 0」与「查询失败」的场景 —— 失败时静默按 0 处理会让全退标记
// 永不置位,或把可退额度放大到全额。
func SumRefundedWithErr(orderNo string) (int64, error) {
	var cents int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_no=? AND status=? AND COALESCE(is_duplicate,0)=0`,
		orderNo, po.RefundStatusSuccess).Scan(&cents)
	return cents, err
}

// SumRefunded 统计某订单已成功退款的总额(分)。
// 查询失败时打告警并返回 0:本函数用于「展示」等非阻断场景,失败按 0 处理不影响主流程;
// 需要区分「失败」与「0」的调用请用 SumRefundedWithErr,额度校验请用 SumRefundedInclPending。
func SumRefunded(orderNo string) int64 {
	cents, err := SumRefundedWithErr(orderNo)
	if err != nil {
		logger.Warnf("[pay] 统计已退款金额失败(订单%s): %v", orderNo, err)
	}
	return cents
}

// SumRefundedInclPending 统计某订单「已成功 + 处理中」的退款总额(分)。
// 用于退款额度校验:处理中的退款单(如微信异步退款)同样占用可退额度,
// 防止两笔并发退款基于同一余额各自发起,导致退款总额超过订单金额。
// 同样不含重复支付退回。
//
// 与 SumRefunded 不同,查询失败时**返回错误**而不是静默 0:调用方据此
// fail-closed(拒绝退款)而非把可退额度放大到全额 —— 基线查询挂掉时
// 静默放行等于拆掉超退防线。
func SumRefundedInclPending(orderNo string) (int64, error) {
	var cents int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_no=? AND status IN (?,?) AND COALESCE(is_duplicate,0)=0`,
		orderNo, po.RefundStatusProcessing, po.RefundStatusSuccess).Scan(&cents)
	if err != nil {
		return 0, err
	}
	return cents, nil
}

// RefundingAmounts 批量统计各订单「处理中」的退款金额(分),key 为 order_id。
// 供订单列表展示「退款中」:tb_order.refund_amount 只在退款**成功**时累加,
// 处理中的退款单已占用可退额度却不会体现在订单行上 —— 收银据此会误以为可以再退一次,
// 点下去必然被后端拒绝,且看不出该单正在退款。
// 与额度口径一致:不含重复支付退回(is_duplicate=1)。
//
// 查询失败返回 error 而不是静默空 map:漏标一笔「退款中」,收银就会重复发起退款。
func RefundingAmounts(orderIDs []int) (map[int]int64, error) {
	out := map[int]int64{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(orderIDs)), ",")
	args := make([]interface{}, 0, len(orderIDs)+1)
	for _, id := range orderIDs {
		args = append(args, id)
	}
	args = append(args, po.RefundStatusProcessing)
	rows, err := store.DB.Query(`SELECT order_id, COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_id IN (`+placeholders+`) AND status=? AND COALESCE(is_duplicate,0)=0
		GROUP BY order_id`, args...)
	if err != nil {
		logger.Warnf("[pay] 统计订单处理中退款金额失败: %v", err)
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var cents int64
		// 扫描失败必须上抛:漏标一笔「退款中」会让收银以为还能退,重复发起退款。
		if err := rows.Scan(&id, &cents); err != nil {
			logger.Warnf("[pay] 扫描处理中退款金额失败: %v", err)
			return out, err
		}
		if cents > 0 {
			out[id] = cents
		}
	}
	if err := rows.Err(); err != nil {
		logger.Warnf("[pay] 遍历处理中退款金额中断: %v", err)
		return out, err
	}
	return out, nil
}

// SumRefundedInclPendingTx 是 SumRefundedInclPending 的事务版本,语义一致。
// 在事务内配合 TouchOrderTx 使用:先锁订单行,再重查已退(含处理中),
// 使额度校验与退款单落库基于同一把行锁串行化。
func SumRefundedInclPendingTx(tx *sql.Tx, orderNo string) (int64, error) {
	var cents int64
	err := tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund
		WHERE order_no=? AND status IN (?,?) AND COALESCE(is_duplicate,0)=0`,
		orderNo, po.RefundStatusProcessing, po.RefundStatusSuccess).Scan(&cents)
	if err != nil {
		return 0, err
	}
	return cents, nil
}

// AddOrderRefunded 累加订单已退款金额并记录退款时间。
func AddOrderRefunded(orderNo string, cents int64) error {
	_, err := store.DB.Exec(`UPDATE tb_order SET refund_amount=COALESCE(refund_amount,0)+?, refund_time=?, update_time=? WHERE order_no=?`,
		cents, store.Now(), store.Now(), orderNo)
	return err
}

// AddOrderRefundedTx 是 AddOrderRefunded 的事务版本。
func AddOrderRefundedTx(tx *sql.Tx, orderNo string, cents int64) error {
	_, err := tx.Exec(`UPDATE tb_order SET refund_amount=COALESCE(refund_amount,0)+?, refund_time=?, update_time=? WHERE order_no=?`,
		cents, store.Now(), store.Now(), orderNo)
	return err
}
