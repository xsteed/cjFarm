package po

// Category 对应 tb_category 表。
type Category struct {
	CategoryID   int
	CategoryName string
	SortOrder    int
	DelFlag      string
	CreateTime   string
	UpdateTime   string
}
