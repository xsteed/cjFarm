package dto

import "dining-system/internal/po"

// Spec 对应 tb_spec 表(API 出参),单价以元(float64)表示。
type Spec struct {
	SpecID   int     `json:"specId"`
	DishID   int     `json:"dishId"`
	SpecName string  `json:"specName"`
	Price    float64 `json:"price"`
}

// FromSpec 将持久化对象转为 API 出参,单价由分转元。
func FromSpec(p po.Spec) Spec {
	return Spec{
		SpecID:   p.SpecID,
		DishID:   p.DishID,
		SpecName: p.SpecName,
		Price:    po.ToYuan(p.Price),
	}
}

// ToPO 将 API 出参转回持久化对象,单价由元转分。
func (s Spec) ToPO() po.Spec {
	return po.Spec{
		SpecID:   s.SpecID,
		DishID:   s.DishID,
		SpecName: s.SpecName,
		Price:    po.ToCents(s.Price),
	}
}
