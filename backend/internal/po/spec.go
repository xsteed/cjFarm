package po

// Spec 对应 tb_spec 表。
type Spec struct {
	SpecID   int
	DishID   int
	SpecName string
	Price    int64 // 单价(分)
}
