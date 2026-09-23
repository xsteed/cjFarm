package po

// Dish 对应 tb_dish 表。
type Dish struct {
	DishID      int
	CategoryID  int
	DishName    string
	DishImage   string
	Description string
	Status      int // 1 上架 0 下架
	SortOrder   int
	DelFlag     string
	CreateBy    string
	CreateTime  string
	UpdateBy    string
	UpdateTime  string
	Remark      *string
}
