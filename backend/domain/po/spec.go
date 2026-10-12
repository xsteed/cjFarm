package po

// Spec 对应 tb_spec 表。
type Spec struct {
	SpecID   int
	DishID   int
	SpecName string
	Price    int64 // 单价(分)
	Stock    int64 // 剩余库存：-1 不限量，>=0 为剩余份数（0 = 售罄）
}
