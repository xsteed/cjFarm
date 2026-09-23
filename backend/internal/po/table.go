package po

// Table 对应 tb_table 表。
type Table struct {
	TableID    int
	TableNo    string
	TableName  string
	Capacity   int
	Status     int // 0 空闲 1 占用
	SortOrder  int
	DelFlag    string
	CreateBy   string
	CreateTime string
	UpdateBy   string
	UpdateTime string
	Remark     *string
	// TableCode 桌台二维码稳定码:创建时生成且永不变更,二维码内容使用该码,
	// 保证「一次印刷长期有效」,同时避免自增 ID 被枚举遍历。
	TableCode string
}
