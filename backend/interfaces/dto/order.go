package dto

import (
	"strings"

	"dining-system/domain/po"
)

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
	BatchNo    int     `json:"batchNo"`    // 加菜批次:1=首次下单,每次加菜 +1
	IsGift     int     `json:"isGift"`     // 是否赠送:0 正常计费 1 赠送(商家改单标记)
	GiftReason string  `json:"giftReason"` // 赠送原因(可选)
}

// FromOrderItem 将持久化对象转为 API 出参,金额由分转元。CategoryID 无来源,不填充。
func FromOrderItem(p po.OrderItem) OrderItem {
	return OrderItem{
		ItemID:     p.ItemID,
		DishID:     p.DishID,
		DishName:   p.DishName,
		SpecID:     p.SpecID,
		SpecName:   p.SpecName,
		Price:      po.ToYuan(p.Price),
		Quantity:   p.Quantity,
		Amount:     po.ToYuan(p.Amount),
		Remark:     p.Remark,
		BatchNo:    p.BatchNo,
		IsGift:     p.IsGift,
		GiftReason: p.GiftReason,
	}
}

// ToPO 将 API 出参转回持久化对象,金额由元转分。CategoryID 不入库。
func (it OrderItem) ToPO() po.OrderItem {
	return po.OrderItem{
		ItemID:     it.ItemID,
		DishID:     it.DishID,
		DishName:   it.DishName,
		SpecID:     it.SpecID,
		SpecName:   it.SpecName,
		Price:      po.ToCents(it.Price),
		Quantity:   it.Quantity,
		Amount:     po.ToCents(it.Amount),
		Remark:     it.Remark,
		BatchNo:    it.BatchNo,
		IsGift:     it.IsGift,
		GiftReason: it.GiftReason,
	}
}

// Order 对应 tb_order 表(API 出参),金额以元(float64)表示。
// ShortNo 由 OrderNo 派生;PendingUrge/Items 为运行时补算,均不入库。
type Order struct {
	OrderID        int     `json:"orderId"`
	OrderNo        string  `json:"orderNo"`
	PickupNo       int     `json:"pickupNo"` // 当日取餐号(当天从 1 递增),0 表示未分配
	ShortNo        string  `json:"shortNo"`  // 可报读短号:优先取餐号,缺失时由 orderNo 派生(非持久列,供顾客向服务员报号)
	BizDate        string  `json:"bizDate"`  // 营业日(持久列,随 ToPO 回写),一般不在界面展示
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
	RefundAmount   float64 `json:"refundAmount"`  // 累计已成功退款金额(元)
	RefundTime     *string `json:"refundTime"`    // 最近一次退款成功时间
	// RefundingAmount 处理中的退款金额(元,运行时补算,不入库)。
	// refundAmount 只在退款成功时累加,处理中的退款单不会体现在订单行上,
	// 前端据此展示「退款中」并在退款到账前禁用「退款」按钮(否则点了必被后端拒)。
	RefundingAmount float64 `json:"refundingAmount"`
	// ---- 结算方式:免单 / 挂账 ----
	SettleType       string  `json:"settleType"`       // normal 正常收款 / free 免单 / credit 挂账
	SettleTime       *string `json:"settleTime"`       // 结算(免单/挂账/收款)时间
	SettleOperator   string  `json:"settleOperator"`   // 结算操作人
	SettleRemark     string  `json:"settleRemark"`     // 免单原因 / 挂账人备注
	CreditStatus     int     `json:"creditStatus"`     // 0 非挂账 1 挂账待收款 2 挂账已结清
	CreditAmount     float64 `json:"creditAmount"`     // 挂账金额(元)
	CreditSettleTime *string `json:"creditSettleTime"` // 挂账核销时间
	CreditSettleBy   string  `json:"creditSettleBy"`   // 挂账核销操作人
	PaidAmount       float64 `json:"paidAmount"`       // 实收金额(元):免单=0,挂账核销前=0
	GiftAmount       float64 `json:"giftAmount"`       // 赠送金额(元):赠送菜品原价合计,不计入应收
	FinishTime       *string `json:"finishTime"`
	// 用餐起止时间(落库值):开始=下单,结束=订单到终态(完成/取消);进行中为 NULL。
	// 详情展示「结束时间」取 MealEndTime —— 取消单也有,finish_time 只覆盖已完成单。
	MealStartTime *string     `json:"mealStartTime"`
	MealEndTime   *string     `json:"mealEndTime"`
	OrderRemark   string      `json:"orderRemark"`
	CancelReason  string      `json:"cancelReason"`
	CreateBy      string      `json:"createBy"`
	CreateTime    string      `json:"createTime"`
	UpdateBy      string      `json:"updateBy"`
	UpdateTime    string      `json:"updateTime"`
	Remark        *string     `json:"remark"`
	PendingUrge   bool        `json:"pendingUrge"` // 是否存在未处理的催菜(运行时填充,不入库)
	// IsNew 是否是「商家还没看过/操作过的新单」(运行时填充,不入库):
	// 仅在订单列表接口按「已下单 + viewed_at 为空」置 true,详情/顾客端恒为 false。
	// 用 omitempty:顾客端出参与无关场景不带这个字段,不制造无意义噪音。
	IsNew bool `json:"isNew,omitempty"`
	BatchCount    int         `json:"batchCount"`  // 加菜批次数(运行时填充,不入库):明细 distinct batch_no,1=未加过菜。列表接口填充,详情接口明细自带 batchNo 无需它
	Items         []OrderItem `json:"items"`
}

// FromOrder 将持久化对象与运行时补算结果组装为 API 出参,金额由分转元。
func FromOrder(p po.Order, items []OrderItem, pendingUrge bool) Order {
	return Order{
		OrderID:          p.OrderID,
		OrderNo:          p.OrderNo,
		PickupNo:         p.PickupNo,
		BizDate:          p.BizDate,
		ShortNo:          po.DisplayNo(p.PickupNo, p.OrderNo),
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
		GiftAmount:       po.ToYuan(p.GiftAmount),
		FinishTime:       p.FinishTime,
		MealStartTime:    p.MealStartTime,
		MealEndTime:      p.MealEndTime,
		OrderRemark:      p.OrderRemark,
		CancelReason:     p.CancelReason,
		CreateBy:         p.CreateBy,
		CreateTime:       p.CreateTime,
		UpdateBy:         p.UpdateBy,
		UpdateTime:       p.UpdateTime,
		Remark:           p.Remark,
		PendingUrge:      pendingUrge,
		Items:            items,
	}
}

// genericCancelReason 管理端「取消」按钮的固定标签:它只说明「谁取消的」,不含任何原因,
// 对顾客等于没说(「订单已取消：商家取消」),由 cancelReasonForCustomer 统一抹平。
const genericCancelReason = "商家取消"

// maxCustomerCancelReasonRune 顾客可见取消原因的长度上限(按字符数,不按字节,避免截断中文)。
const maxCustomerCancelReasonRune = 40

// cancelReasonForCustomer 把内部取消原因转成「能直接给顾客看」的文案。
//
// 规则:
//   - 空 / 通用标签(商家取消) → 空串:没有信息量,由前端给标准提示(未支付、不产生费用);
//   - 其余原样下发,但去首尾空白并截断到 40 字符 —— 取消原因是管理端可写的自由文本,
//     整段内部备注(如「这桌客人很难沟通」)不该出现在顾客手机上。
//
// 前端不做这层判断:哪些内容能给顾客看属于「出参口径」,只在这一处收口,
// H5 / 小程序两端拿到的就是最终可展示的文案。
func cancelReasonForCustomer(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || reason == genericCancelReason {
		return ""
	}
	r := []rune(reason)
	if len(r) > maxCustomerCancelReasonRune {
		return string(r[:maxCustomerCancelReasonRune])
	}
	return reason
}

// FromOrderCustomer 顾客端订单出参:复用 FromOrder 后裁剪顾客端无消费场景的敏感字段。
// 剥离:TransactionID(第三方渠道交易号)、SettleOperator(收银员姓名)、CreateBy/UpdateBy(内部操作人)。
// CancelReason 不直接透传,走 cancelReasonForCustomer 做「可展示性」收口:
// 取消只发生在未支付订单上(service.CancelOrder / ChangeOrderStatus 均强制 pay_status=0),
// 顾客看到的永远是一笔没花钱的单,所以文案只需要说明「为什么没了」,不必承担账务解释。
// 管理端接口(OrderGet 等)仍用 FromOrder 全量版本,不受影响。
func FromOrderCustomer(p po.Order, items []OrderItem, pendingUrge bool) Order {
	o := FromOrder(p, items, pendingUrge)
	o.TransactionID = ""
	o.SettleOperator = ""
	o.CreateBy = ""
	o.UpdateBy = ""
	o.CancelReason = cancelReasonForCustomer(p.CancelReason)
	return o
}

// ToPO 将 API 出参转回持久化对象,金额由元转分。ShortNo/Items/PendingUrge 不入库。
// BizDate/PickupNo 是持久列,必须原样回带:否则经「DTO→PO→落库」的改单路径
// 会把取餐号抹成 0,顾客报号失效。
func (o Order) ToPO() po.Order {
	return po.Order{
		OrderID:          o.OrderID,
		OrderNo:          o.OrderNo,
		BizDate:          o.BizDate,
		PickupNo:         o.PickupNo,
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
		GiftAmount:       po.ToCents(o.GiftAmount),
		FinishTime:       o.FinishTime,
		MealStartTime:    o.MealStartTime,
		MealEndTime:      o.MealEndTime,
		OrderRemark:      o.OrderRemark,
		CancelReason:     o.CancelReason,
		CreateBy:         o.CreateBy,
		CreateTime:       o.CreateTime,
		UpdateBy:         o.UpdateBy,
		UpdateTime:       o.UpdateTime,
		Remark:           o.Remark,
	}
}
