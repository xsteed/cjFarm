package store

import (
	"database/sql"

	"dining-system/internal/model"
)

// OrderCols 订单表完整列(与 ScanOrder 顺序一一对应)。
const OrderCols = `order_id, order_no, table_id, table_no, table_name, person_count, order_status,
	dish_amount, seat_fee, discount_amount, total_amount, pay_status, pay_type, pay_time,
	transaction_id, pay_channel, refund_amount, refund_time,
	settle_type, settle_time, settle_operator, settle_remark,
	credit_status, credit_amount, credit_settle_time, credit_settle_by, paid_amount,
	finish_time, order_remark, cancel_reason, begin_time, end_time, create_by, create_time, update_by, update_time, remark`

// ScanOrder 扫描一行订单记录(不含明细)。
func ScanOrder(rows interface{ Scan(...interface{}) error }) (model.Order, error) {
	var o model.Order
	var dishCents, seatCents, discountCents, totalCents, refundCents int64
	var creditCents, paidCents int64
	var settleType sql.NullString
	err := rows.Scan(&o.OrderID, &o.OrderNo, &o.TableID, &o.TableNo, &o.TableName, &o.PersonCount, &o.OrderStatus,
		&dishCents, &seatCents, &discountCents, &totalCents, &o.PayStatus, &o.PayType, &o.PayTime,
		&o.TransactionID, &o.PayChannel, &refundCents, &o.RefundTime,
		&settleType, &o.SettleTime, &o.SettleOperator, &o.SettleRemark,
		&o.CreditStatus, &creditCents, &o.CreditSettleTime, &o.CreditSettleBy, &paidCents,
		&o.FinishTime, &o.OrderRemark, &o.CancelReason, &o.BeginTime, &o.EndTime, &o.CreateBy, &o.CreateTime, &o.UpdateBy, &o.UpdateTime, &o.Remark)
	if err != nil {
		return o, err
	}
	o.DishAmount = model.ToYuan(dishCents)
	o.SeatFee = model.ToYuan(seatCents)
	o.DiscountAmount = model.ToYuan(discountCents)
	o.TotalAmount = model.ToYuan(totalCents)
	o.RefundAmount = model.ToYuan(refundCents)
	o.CreditAmount = model.ToYuan(creditCents)
	o.PaidAmount = model.ToYuan(paidCents)
	// 老数据列为 NULL 时兜底为正常收款,避免前端判断出现空值分支。
	o.SettleType = settleType.String
	if o.SettleType == "" {
		o.SettleType = "normal"
	}
	// 短号由订单号派生,历史订单无需回填即可生效。
	o.ShortNo = model.ShortOrderNo(o.OrderNo)
	return o, nil
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
}

// GetOrderSettleInfo 读取订单结算关键信息。
func GetOrderSettleInfo(orderID int) (OrderSettleInfo, error) {
	var s OrderSettleInfo
	err := DB.QueryRow(`SELECT order_status, pay_status, COALESCE(settle_type,'normal'),
		COALESCE(credit_status,0), total_amount, COALESCE(credit_amount,0), COALESCE(paid_amount,0),
		COALESCE(refund_amount,0), order_no, person_count, table_id, table_no, table_name,
		order_remark, dish_amount, seat_fee, discount_amount, create_time
		FROM tb_order WHERE order_id=?`, orderID).
		Scan(&s.OrderStatus, &s.PayStatus, &s.SettleType, &s.CreditStatus, &s.TotalCents, &s.CreditCents,
			&s.PaidCents, &s.RefundCents, &s.OrderNo, &s.PersonCount, &s.TableID, &s.TableNo, &s.TableName,
			&s.OrderRemark, &s.DishCents, &s.SeatCents, &s.DiscountCents, &s.CreateTime)
	return s, err
}

// LoadOrderItems 加载订单明细。
func LoadOrderItems(orderID int) []model.OrderItem {
	rows, err := DB.Query(`SELECT item_id, order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark
		FROM tb_order_item WHERE order_id=? ORDER BY item_id`, orderID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	items := []model.OrderItem{}
	for rows.Next() {
		var it model.OrderItem
		var oid int
		var priceCents, amountCents int64
		rows.Scan(&it.ItemID, &oid, &it.DishID, &it.DishName, &it.SpecID, &it.SpecName, &priceCents, &it.Quantity, &amountCents, &it.Remark)
		it.Price = model.ToYuan(priceCents)
		it.Amount = model.ToYuan(amountCents)
		items = append(items, it)
	}
	return items
}

// GetOrderTable 返回订单所属桌台,若订单不存在返回错误。
func GetOrderTable(orderID int) (int, error) {
	var tableID int
	err := DB.QueryRow(`SELECT table_id FROM tb_order WHERE order_id=?`, orderID).Scan(&tableID)
	return tableID, err
}

// GetOrderState 返回订单的状态与支付状态,用于状态机流转校验。
func GetOrderState(orderID int) (orderStatus, payStatus int, err error) {
	err = DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_id=?`, orderID).
		Scan(&orderStatus, &payStatus)
	return
}
