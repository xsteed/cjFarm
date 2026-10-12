package po

// 催菜处理状态与冷却间隔。
const (
	UrgeStatusPending  = 0 // 待处理
	UrgeStatusHandled  = 1 // 已处理
	UrgeTypeUrge       = "urge"
	UrgeCooldownSecond = 180 // 同一订单两次催菜的最小间隔(秒),防止顾客连点刷屏
)

// OrderUrge 对应 tb_order_urge 表:一条催菜记录。顾客可多次催菜,每次独立成记录。
type OrderUrge struct {
	UrgeID     int
	OrderID    int
	OrderNo    string
	TableID    int
	TableNo    string
	TableName  string
	UrgeType   string
	Status     int // 0 待处理 1 已处理
	Remark     string
	CreateTime string
	HandleTime *string
	HandleBy   string
}
