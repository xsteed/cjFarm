package po

// Dish 对应 tb_dish 表。
type Dish struct {
	DishID      int
	CategoryID  int
	DishName    string
	DishImage   string
	Description string
	Status      int // 1 上架 0 下架
	SoldOut     int // 0 未售罄 1 售罄（上架中但今日沽清，顾客端可见但不可点）
	SortOrder   int
	DelFlag     string
	CreateBy    string
	CreateTime  string
	UpdateBy    string
	UpdateTime  string
	Remark      *string
}
