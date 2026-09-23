package dao

import (
	"database/sql"
	"strings"

	"dining-system/infra/logger"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// OrderCols 订单表完整列(与 ScanOrder 顺序一一对应)。
const OrderCols = `order_id, order_no, table_id, table_no, table_name, person_count, order_status,
	dish_amount, seat_fee, discount_amount, total_amount, pay_status, pay_type, pay_time,
	transaction_id, pay_channel, refund_amount, refund_time,
	settle_type, settle_time, settle_operator, settle_remark,
	credit_status, credit_amount, credit_settle_time, credit_settle_by, paid_amount,
	finish_time, order_remark, cancel_reason, begin_time, end_time, create_by, create_time, update_by, update_time, remark`

// ScanOrder 扫描一行订单记录(不含明细)。
//
// 可 NULL 的字符串列统一走 sql.NullString:老库加列时未带默认值的列
// (如 refund_time/settle_time/credit_settle_time)历史行就是 NULL,
// 直接 Scan 到 string 会整行报错;行报错在列表查询里表现为「静默丢单」。
func ScanOrder(rows interface{ Scan(...interface{}) error }) (po.Order, error) {
	var o po.Order
	var tableNo, tableName sql.NullString
	var payType, payTime sql.NullString
	var transactionID, payChannel sql.NullString
	var refundTime, settleType sql.NullString
	var settleTime, settleOperator, settleRemark sql.NullString
	var creditSettleTime, creditSettleBy sql.NullString
	var finishTime, orderRemark, cancelReason sql.NullString
	var beginTime, endTime sql.NullString
	var createBy, createTime, updateBy, updateTime, remark sql.NullString
	err := rows.Scan(&o.OrderID, &o.OrderNo, &o.TableID, &tableNo, &tableName, &o.PersonCount, &o.OrderStatus,
		&o.DishAmount, &o.SeatFee, &o.DiscountAmount, &o.TotalAmount, &o.PayStatus, &payType, &payTime,
		&transactionID, &payChannel, &o.RefundAmount, &refundTime,
		&settleType, &settleTime, &settleOperator, &settleRemark,
		&o.CreditStatus, &o.CreditAmount, &creditSettleTime, &creditSettleBy, &o.PaidAmount,
		&finishTime, &orderRemark, &cancelReason, &beginTime, &endTime, &createBy, &createTime, &updateBy, &updateTime, &remark)
	if err != nil {
		return o, err
	}
	o.TableNo, o.TableName = tableNo.String, tableName.String
	// 模型里可空时间字段是 *string(保留 NULL 语义),这里由 NullString 转指针。
	o.PayType, o.PayTime = nsPtr(payType), nsPtr(payTime)
	o.TransactionID, o.PayChannel = transactionID.String, payChannel.String
	o.RefundTime = nsPtr(refundTime)
	o.SettleTime, o.SettleOperator, o.SettleRemark = nsPtr(settleTime), settleOperator.String, settleRemark.String
	o.CreditSettleTime, o.CreditSettleBy = nsPtr(creditSettleTime), creditSettleBy.String
	o.FinishTime, o.OrderRemark, o.CancelReason = nsPtr(finishTime), orderRemark.String, cancelReason.String
	o.BeginTime, o.EndTime = nsPtr(beginTime), nsPtr(endTime)
	o.CreateBy, o.CreateTime, o.UpdateBy, o.UpdateTime = createBy.String, createTime.String, updateBy.String, updateTime.String
	o.Remark = nsPtr(remark)
	// 老数据列为 NULL 时兜底为正常收款,避免前端判断出现空值分支。
	o.SettleType = settleType.String
	if o.SettleType == "" {
		o.SettleType = "normal"
	}
	return o, nil
}

// nsPtr 把 sql.NullString 转成 *string:NULL 返回 nil,否则返回指向值的指针。
// 与 po.Order 中可空时间字段的 *string 类型配套,保留 NULL 语义。
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// GetOrderByID 按订单 ID 查询订单。
func GetOrderByID(orderID int) (po.Order, error) {
	return ScanOrder(store.DB.QueryRow(`SELECT `+OrderCols+` FROM tb_order WHERE order_id=?`, orderID))
}

// GetOrderByNo 按订单号查询订单。
func GetOrderByNo(orderNo string) (po.Order, error) {
	return ScanOrder(store.DB.QueryRow(`SELECT `+OrderCols+` FROM tb_order WHERE order_no=?`, orderNo))
}

// GetActiveOrderByTable 查询桌台最近一笔进行中订单。
func GetActiveOrderByTable(tableID int) (po.Order, bool) {
	o, err := ScanOrder(store.DB.QueryRow(`SELECT `+OrderCols+` FROM tb_order WHERE table_id=? AND order_status IN (1,2,3) ORDER BY order_id DESC LIMIT 1`, tableID))
	if err != nil {
		return po.Order{}, false
	}
	return o, true
}

// ListActiveOrders 查询全部进行中订单(1已下单/2制作中/3已上齐),按 order_id 升序。
// 桌台看板一次性取出后按 table_id 分组取最新一笔,避免逐桌 GetActiveOrderByTable 的 N+1。
func ListActiveOrders() ([]po.Order, error) {
	rows, err := store.DB.Query(`SELECT ` + OrderCols + ` FROM tb_order WHERE order_status IN (1,2,3) ORDER BY order_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// OrderQuery 订单列表筛选条件;零值字段表示不限。
type OrderQuery struct {
	OrderStatus  string // 空表示不限;字符串绑定,与历史行为一致
	PayStatus    *int   // nil 表示不限
	OrderNo      string // 模糊匹配
	TableNo      string // 精确匹配
	SettleType   string // normal/free/credit
	CreditStatus *int   // nil 表示不限
}

// ListOrders 按条件分页查询订单列表。
func ListOrders(q OrderQuery, pageNum, pageSize int) (total int, list []po.Order, err error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if q.OrderStatus != "" {
		where = append(where, "order_status=?")
		args = append(args, q.OrderStatus)
	}
	if q.PayStatus != nil {
		// 数值型字段必须以整数入参:SQLite 下 COALESCE(...) 表达式会丢失列亲和性,
		// 用字符串 "1" 去比整数列会恒不相等。
		where = append(where, "pay_status=?")
		args = append(args, *q.PayStatus)
	}
	if q.OrderNo != "" {
		where = append(where, "order_no LIKE ?")
		args = append(args, "%"+q.OrderNo+"%")
	}
	if q.TableNo != "" {
		where = append(where, "table_no=?")
		args = append(args, q.TableNo)
	}
	// 结算方式筛选:normal 正常收款 / free 免单 / credit 挂账
	if q.SettleType != "" {
		where = append(where, "COALESCE(settle_type,'normal')=?")
		args = append(args, q.SettleType)
	}
	// 挂账状态筛选:0 非挂账 1 待收款 2 已结清
	if q.CreditStatus != nil {
		where = append(where, "COALESCE(credit_status,0)=?")
		args = append(args, *q.CreditStatus)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+OrderCols+` FROM tb_order`+cond+` ORDER BY order_id DESC LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, nil, err
	}
	defer rows.Close()
	list = []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			return 0, nil, scanErr // 单行失败必须报错,静默丢弃等于丢单
		}
		list = append(list, o)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// GetOrderTotal 查询订单应收总额;查询失败时返回 0。
func GetOrderTotal(orderID int) int64 {
	var total int64
	_ = store.DB.QueryRow(`SELECT COALESCE(total_amount,0) FROM tb_order WHERE order_id=?`, orderID).Scan(&total)
	return total
}

// GetOrderAppendInfo 查询顾客加菜所需的订单状态信息。
func GetOrderAppendInfo(orderNo string) (orderID, tableID, personCount, orderStatus, payStatus int, err error) {
	err = store.DB.QueryRow(`SELECT order_id, table_id, person_count, order_status, pay_status
		FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&orderID, &tableID, &personCount, &orderStatus, &payStatus)
	return
}

// GetUrgeTarget 查询顾客催菜所需的订单与桌台信息。
func GetUrgeTarget(orderNo string) (orderID, tableID, orderStatus int, tableNo, tableName string, err error) {
	err = store.DB.QueryRow(`SELECT order_id, table_id, order_status, table_no, table_name
		FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&orderID, &tableID, &orderStatus, &tableNo, &tableName)
	return
}

// OrderSettleInfo 订单结算信息(用于免单/挂账/核销前校验)。
type OrderSettleInfo struct {
	OrderStatus   int
	PayStatus     int
	SettleType    string
	CreditStatus  int
	TotalCents    int64
	CreditCents   int64
	PaidCents     int64
	RefundCents   int64
	OrderNo       string
	PersonCount   int
	TableID       int
	TableNo       string
	TableName     string
	OrderRemark   string
	DishCents     int64
	SeatCents     int64
	DiscountCents int64
	CreateTime    string
	// PayChannel 非空表示订单经在线渠道(微信/支付宝)支付;
	// 此类订单撤销结算必须走退款流程(渠道侧资金需原路退回),不能简单回退支付状态。
	PayChannel string
	// PreSettleStatus 结账时的订单状态快照(1/2/3);0 表示历史数据未记录。
	PreSettleStatus int
}

// GetOrderSettleInfo 读取订单结算关键信息。
func GetOrderSettleInfo(orderID int) (OrderSettleInfo, error) {
	var s OrderSettleInfo
	err := store.DB.QueryRow(`SELECT order_status, pay_status, COALESCE(settle_type,'normal'),
		COALESCE(credit_status,0), total_amount, COALESCE(credit_amount,0), COALESCE(paid_amount,0),
		COALESCE(refund_amount,0), order_no, person_count, table_id, table_no, table_name,
		order_remark, dish_amount, seat_fee, discount_amount, create_time,
		COALESCE(pay_channel,''), COALESCE(pre_settle_status,0)
		FROM tb_order WHERE order_id=?`, orderID).
		Scan(&s.OrderStatus, &s.PayStatus, &s.SettleType, &s.CreditStatus, &s.TotalCents, &s.CreditCents,
			&s.PaidCents, &s.RefundCents, &s.OrderNo, &s.PersonCount, &s.TableID, &s.TableNo, &s.TableName,
			&s.OrderRemark, &s.DishCents, &s.SeatCents, &s.DiscountCents, &s.CreateTime,
			&s.PayChannel, &s.PreSettleStatus)
	return s, err
}

// LoadOrderItems 加载订单明细。
//
// 展示/打印路径的便捷封装:这类调用方无法向上传递 error,读失败时记 warn 后
// 返回空明细(至少留下日志,不再「明细静默变空」)。需要 error 语义(如事务内
// 金额重算)的调用方请直接用 LoadOrderItemsTx。
func LoadOrderItems(orderID int) []po.OrderItem {
	items, err := LoadOrderItemsTx(store.DB, orderID)
	if err != nil {
		logger.Warnf("[order] 加载订单 %d 明细失败: %v", orderID, err)
		return nil
	}
	return items
}

// LoadOrderItemsTx 在指定执行器(DB 或事务)上加载订单明细。
// 扫描失败返回错误:调用方(如加菜金额重算)基于旧明细重算金额,读失败会算出
// 错误金额,必须显式失败而不是静默拿空明细继续。
func LoadOrderItemsTx(q interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}, orderID int) ([]po.OrderItem, error) {
	rows, err := q.Query(`SELECT item_id, order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark
		FROM tb_order_item WHERE order_id=? ORDER BY item_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []po.OrderItem{}
	for rows.Next() {
		var it po.OrderItem
		var oid int
		if err := rows.Scan(&it.ItemID, &oid, &it.DishID, &it.DishName, &it.SpecID, &it.SpecName, &it.Price, &it.Quantity, &it.Amount, &it.Remark); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// LoadOrderItemsByOrderIDs 批量加载多个订单的明细,key 为 order_id。
// 看板一次性取全部进行中订单的明细时使用,避免逐单 LoadOrderItems 的 N+1 查询。
func LoadOrderItemsByOrderIDs(orderIDs []int) (map[int][]po.OrderItem, error) {
	out := map[int][]po.OrderItem{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]interface{}, 0, len(orderIDs))
	for i, id := range orderIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT item_id, order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark
		FROM tb_order_item WHERE order_id IN (`+placeholders+`) ORDER BY order_id, item_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it po.OrderItem
		var oid int
		if err := rows.Scan(&it.ItemID, &oid, &it.DishID, &it.DishName, &it.SpecID, &it.SpecName, &it.Price, &it.Quantity, &it.Amount, &it.Remark); err != nil {
			return nil, err
		}
		out[oid] = append(out[oid], it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrderTable 返回订单所属桌台,若订单不存在返回错误。
func GetOrderTable(orderID int) (int, error) {
	var tableID int
	err := store.DB.QueryRow(`SELECT table_id FROM tb_order WHERE order_id=?`, orderID).Scan(&tableID)
	return tableID, err
}

// GetOrderState 返回订单的状态与支付状态,用于状态机流转校验。
func GetOrderState(orderID int) (orderStatus, payStatus int, err error) {
	err = store.DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_id=?`, orderID).
		Scan(&orderStatus, &payStatus)
	return
}

// CountActiveOrdersTx 统计桌台进行中的订单数(事务内防重复下单校验用)。
func CountActiveOrdersTx(tx *sql.Tx, tableID int) (int, error) {
	var active int
	err := tx.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE table_id=? AND order_status IN (1,2,3)`, tableID).Scan(&active)
	return active, err
}

// TouchOrderTx 对订单行做自赋值 no-op 更新,在 MySQL/SQLite 上先取该行写锁,
// 把同一订单的后续校验/写入在行上串行化(消除 check-then-act 竞态窗口)。
func TouchOrderTx(tx *sql.Tx, orderID int) error {
	_, err := tx.Exec(`UPDATE tb_order SET update_time=update_time WHERE order_id=?`, orderID)
	return err
}

// UpdateOrderStatusTx 在事务中更新订单状态,并用当前状态守卫并发变更。
func UpdateOrderStatusTx(tx *sql.Tx, orderID, status int, operator string, curStatus int) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET order_status=?, update_by=?, update_time=? WHERE order_id=? AND order_status=?`,
		status, operator, store.Now(), orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkOrderPaid 将进行中且未支付订单标记为正常收款(现金/线下)。
//
// 与 SettleOrderTx 不同,现金收款不归档(order_status 仍为 1/2/3),因此需要同步把
// 收款前的订单状态记入 pre_settle_status,撤销结算时才能按收款前状态恢复;
// 否则该字段为空,撤销方只能硬编码回 3,丢失「已下单/制作中」的真实状态。
func MarkOrderPaid(orderID int, payType, operator string) (int64, error) {
	res, err := store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, settle_type=?, settle_time=?,
		settle_operator=?, paid_amount=total_amount-COALESCE(refund_amount,0), credit_status=0, credit_amount=0,
		pre_settle_status=order_status, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		payType, store.Now(), "normal", store.Now(), operator, operator, store.Now(), orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SettleOrderParams 订单结算回写参数(由 handler 计算好业务字段后传入)。
type SettleOrderParams struct {
	SettleType   string // 结算方式(normal/free/credit)
	PayType      string // 展示用支付方式(免单/挂账时为对应文案)
	SettleRemark string // 结算备注(免单原因/挂账事由)
	Operator     string // 操作人
	Now          string // 操作时间(事务外取一次,保证各字段时间一致)
	CreditStatus int    // 挂账状态(0 非挂账/1 待收款/2 已结清)
	CreditCents  int64  // 挂账金额(分)
	PaidCents    int64  // 实收金额(分)
	PreStatus    int    // 结账前订单状态快照(1/2/3)
}

// SettleOrderTx 在事务中结算订单并记录结算前状态快照。
func SettleOrderTx(tx *sql.Tx, orderID int, p SettleOrderParams) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?,
		settle_type=?, settle_time=?, settle_operator=?, settle_remark=?,
		credit_status=?, credit_amount=?, paid_amount=?,
		order_status=4, pre_settle_status=?, finish_time=?, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		p.PayType, p.Now, p.SettleType, p.Now, p.Operator, p.SettleRemark,
		p.CreditStatus, p.CreditCents, p.PaidCents,
		p.PreStatus, p.Now, p.Operator, p.Now, orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SettleCreditOrder 核销挂账订单;credit_status=1 表示待收款,核销后置为 2(已结清)。
func SettleCreditOrder(orderID int, due int64, payType, operator string) (int64, error) {
	now := store.Now()
	res, err := store.DB.Exec(`UPDATE tb_order SET credit_status=?, credit_settle_time=?, credit_settle_by=?,
		pay_type=?, paid_amount=?, update_by=?, update_time=? WHERE order_id=? AND credit_status=?`,
		2, now, operator, payType, due, operator, now, orderID, 1)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CancelSettleTx 在事务中撤销订单结算并恢复结算前订单状态。
//
// 守卫放宽为 pay_status=1 AND order_status IN (1,2,3,4):
//   - 4 对应 SettleOrderTx 归档后的已结算订单(免单/挂账/normal 结账);
//   - 1/2/3 对应 PayOrder(现金收款)只置 pay_status=1、不归档的订单。这类订单此前被
//     order_status=4 的守卫挡住,UPDATE 命中 0 行,撤销结算对其永远不可用。
//
// 恢复状态由调用方按 pre_settle_status 决定(现金收款已记录收款前状态;历史未记录时回退 3)。
func CancelSettleTx(tx *sql.Tx, orderID int, settleType, reason string, restoreStatus int, operator, now string) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET pay_status=0, pay_type=NULL, pay_time=NULL,
		settle_type=?, settle_time=NULL, settle_operator='', settle_remark=?,
		credit_status=0, credit_amount=0, credit_settle_time=NULL, credit_settle_by='',
		paid_amount=0, finish_time=NULL, order_status=?, update_by=?, update_time=? WHERE order_id=? AND pay_status=1 AND order_status IN (1,2,3,4)`,
		settleType, reason, restoreStatus, operator, now, orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FinishOrderTx 在事务中将订单归档为已完成,并用当前状态守卫并发变更。
func FinishOrderTx(tx *sql.Tx, orderID int, operator string, curStatus int) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET order_status=4, finish_time=?, update_by=?, update_time=? WHERE order_id=? AND order_status=?`,
		store.Now(), operator, store.Now(), orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CancelOrderTx 在事务中取消未支付订单,并用当前状态守卫并发变更。
func CancelOrderTx(tx *sql.Tx, orderID int, reason, operator string, curStatus int) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET order_status=5, cancel_reason=?, update_by=?, update_time=? WHERE order_id=? AND order_status=? AND pay_status=0`,
		reason, operator, store.Now(), orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateOrderForEditTx 在事务中更新订单基础金额信息,仅允许进行中且未支付订单。
func UpdateOrderForEditTx(tx *sql.Tx, orderID, personCount int, remark string, dishCents, seatCents, discountCents, totalCents int64, operator string) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET person_count=?, order_remark=?, dish_amount=?, seat_fee=?, discount_amount=?, total_amount=?, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		personCount, remark, dishCents, seatCents, discountCents, totalCents, operator, store.Now(), orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReplaceOrderItemsTx 在事务中替换订单明细。
func ReplaceOrderItemsTx(tx *sql.Tx, orderID int, items []po.OrderItem) error {
	if _, err := tx.Exec(`DELETE FROM tb_order_item WHERE order_id=?`, orderID); err != nil {
		return err
	}
	for _, it := range items {
		if err := InsertOrderItemTx(tx, int64(orderID), it); err != nil {
			return err
		}
	}
	return nil
}

// InsertOrderTx 在事务中新增食客订单并返回主键 ID。
func InsertOrderTx(tx *sql.Tx, orderNo string, tableID, personCount int, tableNo, tableName, orderRemark string, dishCents, seatCents, discountCents, totalCents int64) (int64, error) {
	res, err := tx.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, order_remark, create_by, create_time, update_by, update_time)
		VALUES(?,?,?,?,?,1,?,?,?,?,0,?,?,?,?,?)`,
		orderNo, tableID, tableNo, tableName, personCount,
		dishCents, seatCents, discountCents, totalCents, orderRemark,
		"食客", store.Now(), "食客", store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertOrderItemTx 在事务中新增订单明细。
func InsertOrderItemTx(tx *sql.Tx, orderID int64, it po.OrderItem) error {
	_, err := tx.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
		VALUES(?,?,?,?,?,?,?,?,?)`, orderID, it.DishID, it.DishName, it.SpecID, it.SpecName, it.Price, it.Quantity, it.Amount, it.Remark)
	return err
}

// GetOrderStateTx 在事务中重读订单状态、支付状态与用餐人数。
func GetOrderStateTx(tx *sql.Tx, orderID int) (orderStatus, payStatus, personCount int, err error) {
	err = tx.QueryRow(`SELECT order_status, pay_status, person_count FROM tb_order WHERE order_id=?`, orderID).
		Scan(&orderStatus, &payStatus, &personCount)
	return
}

// UpdateOrderAmountTx 在事务中更新订单金额。
func UpdateOrderAmountTx(tx *sql.Tx, orderID int, dishCents, seatCents, discountCents, totalCents int64) error {
	_, err := tx.Exec(`UPDATE tb_order SET dish_amount=?, seat_fee=?, discount_amount=?, total_amount=?, update_time=? WHERE order_id=?`,
		dishCents, seatCents, discountCents, totalCents, store.Now(), orderID)
	return err
}

// MarkOrderPaidByChannel 根据在线支付渠道回写订单支付状态。
//
// 返回 changed 表示「本方法真正把订单从待支付置为已支付」:条件 UPDATE 带
// WHERE pay_status=0,若订单已在并发窗口内被其它渠道/线下收款先行置为已支付,
// 该行不命中,rowsAffected 为 0,changed=false。调用方(ApplyPaymentSuccess)据此
// 判定本笔为重复支付并原路退回,消除「流水已置已支付、订单却非本笔入账」的竞态。
func MarkOrderPaidByChannel(orderNo, payType, transactionID, channel string) (bool, error) {
	res, err := store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, transaction_id=?, pay_channel=?, update_time=?
		WHERE order_no=? AND pay_status=0`,
		payType, store.Now(), transactionID, channel, store.Now(), orderNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
