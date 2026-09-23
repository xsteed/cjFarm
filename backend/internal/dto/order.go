package dto

import "dining-system/internal/po"

// OrderItem 对应 tb_order_item 表(API 出参),金额以元(float64)表示。
// CategoryID 不入 tb_order_item 表,由打印层临时按 dish_id 关联查询补齐。
type OrderItem struct {
	ItemID     int     `json:"itemId"`
	DishID     int     `json:"dishId"`
	DishName   string  `json:"dishName"`
	SpecID     int     `json:"specId"`
	SpecName   string  `json:"specName"`
	Price      float64 `json:"price"`
	Quantity   int     `json:"quantity"`
	Amount     float64 `json:"amount"`
	Remark     string  `json:"itemRemark"`
	CategoryID int     `json:"categoryId,omitempty"`
}

// FromOrderItem 将持久化对象转为 API 出参,金额由分转元。CategoryID 无来源,不填充。
func FromOrderItem(p po.OrderItem) OrderItem {
	return OrderItem{
		ItemID:   p.ItemID,
		DishID:   p.DishID,
		DishName: p.DishName,
		SpecID:   p.SpecID,
		SpecName: p.SpecName,
		Price:    po.ToYuan(p.Price),
		Quantity: p.Quantity,
		Amount:   po.ToYuan(p.Amount),
		Remark:   p.Remark,
	}
}

// ToPO 将 API 出参转回持久化对象,金额由元转分。CategoryID 不入库。
func (it OrderItem) ToPO() po.OrderItem {
	return po.OrderItem{
		ItemID:   it.ItemID,
		DishID:   it.DishID,
		DishName: it.DishName,
		SpecID:   it.SpecID,
		SpecName: it.SpecName,
		Price:    po.ToCents(it.Price),
		Quantity: it.Quantity,
		Amount:   po.ToCents(it.Amount),
		Remark:   it.Remark,
	}
}

// Order 对应 tb_order 表(API 出参),金额以元(float64)表示。
// ShortNo 由 OrderNo 派生;PendingUrge/Items 为运行时补算,均不入库。
type Order struct {
	OrderID        int     `json:"orderId"`
	OrderNo        string  `json:"orderNo"`
	ShortNo        string  `json:"shortNo"` // 可报读短号(由 orderNo 派生,不入库),供顾客向服务员报号
	TableID        int     `json:"tableId"`
	TableNo        string  `json:"tableNo"`
	TableName      string  `json:"tableName"`
	PersonCount    int     `json:"personCount"`
	OrderStatus    int     `json:"orderStatus"` // 1已下单 2制作中 3已上齐(用餐中) 4已完成 5已取消
	DishAmount     float64 `json:"dishAmount"`
	SeatFee        float64 `json:"seatFee"`
	DiscountAmount float64 `json:"discountAmount"`
	TotalAmount    float64 `json:"totalAmount"`
	PayStatus      int     `json:"payStatus"` // 0未支付 1已支付
	PayType        *string `json:"payType"`
	PayTime        *string `json:"payTime"`
	TransactionID  string  `json:"transactionId"` // 第三方支付渠道交易号
	PayChannel     string  `json:"payChannel"`    // 支付渠道: wxpay / alipay / offline(码牌) / 空
	RefundAmount   float64 `json:"refundAmount"`  // 累计已退款金额(元)
	RefundTime     *string `json:"refundTime"`    // 最近一次退款成功时间
	// ---- 结算方式:免单 / 挂账 ----
	SettleType       string      `json:"settleType"`       // normal 正常收款 / free 免单 / credit 挂账
	SettleTime       *string     `json:"settleTime"`       // 结算(免单/挂账/收款)时间
	SettleOperator   string      `json:"settleOperator"`   // 结算操作人
	SettleRemark     string      `json:"settleRemark"`     // 免单原因 / 挂账人备注
	CreditStatus     int         `json:"creditStatus"`     // 0 非挂账 1 挂账待收款 2 挂账已结清
	CreditAmount     float64     `json:"creditAmount"`     // 挂账金额(元)
	CreditSettleTime *string     `json:"creditSettleTime"` // 挂账核销时间
	CreditSettleBy   string      `json:"creditSettleBy"`   // 挂账核销操作人
	PaidAmount       float64     `json:"paidAmount"`       // 实收金额(元):免单=0,挂账核销前=0
	FinishTime       *string     `json:"finishTime"`
	OrderRemark      string      `json:"orderRemark"`
	CancelReason     string      `json:"cancelReason"`
	BeginTime        *string     `json:"beginTime"`
	EndTime          *string     `json:"endTime"`
	CreateBy         string      `json:"createBy"`
	CreateTime       string      `json:"createTime"`
	UpdateBy         string      `json:"updateBy"`
	UpdateTime       string      `json:"updateTime"`
	Remark           *string     `json:"remark"`
	PendingUrge      bool        `json:"pendingUrge"` // 是否存在未处理的催菜(运行时填充,不入库)
	Items            []OrderItem `json:"items"`
}

// FromOrder 将持久化对象与运行时补算结果组装为 API 出参,金额由分转元。
func FromOrder(p po.Order, items []OrderItem, pendingUrge bool) Order {
	return Order{
		OrderID:          p.OrderID,
		OrderNo:          p.OrderNo,
		ShortNo:          po.ShortOrderNo(p.OrderNo),
		TableID:          p.TableID,
		TableNo:          p.TableNo,
		TableName:        p.TableName,
		PersonCount:      p.PersonCount,
		OrderStatus:      p.OrderStatus,
		DishAmount:       po.ToYuan(p.DishAmount),
		SeatFee:          po.ToYuan(p.SeatFee),
		DiscountAmount:   po.ToYuan(p.DiscountAmount),
		TotalAmount:      po.ToYuan(p.TotalAmount),
		PayStatus:        p.PayStatus,
		PayType:          p.PayType,
		PayTime:          p.PayTime,
		TransactionID:    p.TransactionID,
		PayChannel:       p.PayChannel,
		RefundAmount:     po.ToYuan(p.RefundAmount),
		RefundTime:       p.RefundTime,
		SettleType:       p.SettleType,
		SettleTime:       p.SettleTime,
		SettleOperator:   p.SettleOperator,
		SettleRemark:     p.SettleRemark,
		CreditStatus:     p.CreditStatus,
		CreditAmount:     po.ToYuan(p.CreditAmount),
		CreditSettleTime: p.CreditSettleTime,
		CreditSettleBy:   p.CreditSettleBy,
		PaidAmount:       po.ToYuan(p.PaidAmount),
		FinishTime:       p.FinishTime,
		OrderRemark:      p.OrderRemark,
		CancelReason:     p.CancelReason,
		BeginTime:        p.BeginTime,
		EndTime:          p.EndTime,
		CreateBy:         p.CreateBy,
		CreateTime:       p.CreateTime,
		UpdateBy:         p.UpdateBy,
		UpdateTime:       p.UpdateTime,
		Remark:           p.Remark,
		PendingUrge:      pendingUrge,
		Items:            items,
	}
}

// FromOrderCustomer 顾客端订单出参:复用 FromOrder 后裁剪顾客端无消费场景的敏感字段。
// 剥离:TransactionID(第三方渠道交易号)、SettleOperator(收银员姓名)、CreateBy/UpdateBy(内部操作人)、
// CancelReason(取消原因,可能含内部备注)。保留菜品、金额、状态、桌号、备注、下单时间等顾客需要的信息。
// 管理端接口(OrderGet 等)仍用 FromOrder 全量版本,不受影响。
func FromOrderCustomer(p po.Order, items []OrderItem, pendingUrge bool) Order {
	o := FromOrder(p, items, pendingUrge)
	o.TransactionID = ""
	o.SettleOperator = ""
	o.CreateBy = ""
	o.UpdateBy = ""
	o.CancelReason = ""
	return o
}

// ToPO 将 API 出参转回持久化对象,金额由元转分。ShortNo/Items/PendingUrge 不入库。
func (o Order) ToPO() po.Order {
	return po.Order{
		OrderID:          o.OrderID,
		OrderNo:          o.OrderNo,
		TableID:          o.TableID,
		TableNo:          o.TableNo,
		TableName:        o.TableName,
		PersonCount:      o.PersonCount,
		OrderStatus:      o.OrderStatus,
		DishAmount:       po.ToCents(o.DishAmount),
		SeatFee:          po.ToCents(o.SeatFee),
		DiscountAmount:   po.ToCents(o.DiscountAmount),
		TotalAmount:      po.ToCents(o.TotalAmount),
		PayStatus:        o.PayStatus,
		PayType:          o.PayType,
		PayTime:          o.PayTime,
		TransactionID:    o.TransactionID,
		PayChannel:       o.PayChannel,
		RefundAmount:     po.ToCents(o.RefundAmount),
		RefundTime:       o.RefundTime,
		SettleType:       o.SettleType,
		SettleTime:       o.SettleTime,
		SettleOperator:   o.SettleOperator,
		SettleRemark:     o.SettleRemark,
		CreditStatus:     o.CreditStatus,
		CreditAmount:     po.ToCents(o.CreditAmount),
		CreditSettleTime: o.CreditSettleTime,
		CreditSettleBy:   o.CreditSettleBy,
		PaidAmount:       po.ToCents(o.PaidAmount),
		FinishTime:       o.FinishTime,
		OrderRemark:      o.OrderRemark,
		CancelReason:     o.CancelReason,
		BeginTime:        o.BeginTime,
		EndTime:          o.EndTime,
		CreateBy:         o.CreateBy,
		CreateTime:       o.CreateTime,
		UpdateBy:         o.UpdateBy,
		UpdateTime:       o.UpdateTime,
		Remark:           o.Remark,
	}
}
