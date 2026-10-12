package dto

// StaleOrderAlert 一条「开台超时仍未结账」的告警项。
//
// 不嵌 Order:告警只需要「哪一桌、开了多久、多少钱」三件事就能让人过去处理,
// 带上整单明细只会让看板接口变重(而且明细在订单详情里本来就能看)。
type StaleOrderAlert struct {
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	ShortNo     string `json:"shortNo"`
	TableID     int    `json:"tableId"`
	TableNo     string `json:"tableNo"`
	TableName   string `json:"tableName"`
	OrderStatus int    `json:"orderStatus"`
	PersonCount int    `json:"personCount"`
	CreateTime  string `json:"createTime"`
	// Minutes 已开台分钟数:前端直接用它显示「已 3 小时 20 分」,避免各端各算一套时长。
	Minutes int `json:"minutes"`
	// TotalAmount 应收金额(元,与其余订单接口同口径)。
	TotalAmount float64 `json:"totalAmount"`
	// Legacy 是否是「跨日遗留单」:上一营业日及更早、至今仍未结账的订单。
	// 当天开台超时与跨日遗留都进同一个列表 —— 对店家来说要做的动作是同一件事
	// (去把那桌的单结掉),分成两个入口只会让其中一类被漏看。
	Legacy bool `json:"legacy"`
}

// StaleOrderAlerts 未结账超时告警的出参。
//
// Enabled / Hours 一并下发而不是让前端去读配置:告警开关属 config:edit 侧的管理项,
// 而看板只要 order:view 就能进 —— 让前端各自判断会出现「开关开了但界面不显示」的
// 错配。后端是唯一权威。
type StaleOrderAlerts struct {
	// Enabled 两个开关任一开启即为 true。前端实际看 items 是否为空来决定显隐,
	// 这个字段用于「开关开着但确实没有待处理单」与「开关没开」的区分。
	Enabled bool              `json:"enabled"`
	Hours   int               `json:"hours"`
	Items   []StaleOrderAlert `json:"items"`
}
