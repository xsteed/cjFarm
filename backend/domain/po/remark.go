package po

// Remark 对应 tb_remark 表(菜品备注选项)。
type Remark struct {
	RemarkID   int
	OptionName string
	SortOrder  int
	DelFlag    string
	CreateTime string
	UpdateTime string
}
