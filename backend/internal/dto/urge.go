package dto

import "dining-system/internal/po"

// OrderUrge 对应 tb_order_urge 表:一条催菜记录(API 出参)。ShortNo 由 OrderNo 派生,不入库。
type OrderUrge struct {
	UrgeID     int     `json:"urgeId"`
	OrderID    int     `json:"orderId"`
	OrderNo    string  `json:"orderNo"`
	ShortNo    string  `json:"shortNo"` // 由 orderNo 派生的可报读短号,不入库
	TableID    int     `json:"tableId"`
	TableNo    string  `json:"tableNo"`
	TableName  string  `json:"tableName"`
	UrgeType   string  `json:"urgeType"`
	Status     int     `json:"status"` // 0 待处理 1 已处理
	Remark     string  `json:"remark"`
	CreateTime string  `json:"createTime"`
	HandleTime *string `json:"handleTime"`
	HandleBy   string  `json:"handleBy"`
}

// FromOrderUrge 将持久化对象转为 API 出参,ShortNo 由 OrderNo 派生。
func FromOrderUrge(p po.OrderUrge) OrderUrge {
	return OrderUrge{
		UrgeID:     p.UrgeID,
		OrderID:    p.OrderID,
		OrderNo:    p.OrderNo,
		ShortNo:    po.ShortOrderNo(p.OrderNo),
		TableID:    p.TableID,
		TableNo:    p.TableNo,
		TableName:  p.TableName,
		UrgeType:   p.UrgeType,
		Status:     p.Status,
		Remark:     p.Remark,
		CreateTime: p.CreateTime,
		HandleTime: p.HandleTime,
		HandleBy:   p.HandleBy,
	}
}

// ToPO 将 API 出参转回持久化对象,ShortNo 不入库。
func (o OrderUrge) ToPO() po.OrderUrge {
	return po.OrderUrge{
		UrgeID:     o.UrgeID,
		OrderID:    o.OrderID,
		OrderNo:    o.OrderNo,
		TableID:    o.TableID,
		TableNo:    o.TableNo,
		TableName:  o.TableName,
		UrgeType:   o.UrgeType,
		Status:     o.Status,
		Remark:     o.Remark,
		CreateTime: o.CreateTime,
		HandleTime: o.HandleTime,
		HandleBy:   o.HandleBy,
	}
}
