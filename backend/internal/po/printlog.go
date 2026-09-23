package po

// 打印单据类型(落 tb_print_log.doc_type)。
const (
	PrintDocKitchen = "kitchen"
	PrintDocGuest   = "guest"
)

// 打印日志状态。
const (
	PrintStatusFailed  = 0 // 发送失败
	PrintStatusSuccess = 1 // 已送出
	// PrintStatusQueued 已入队、等待本地打印代理取单。
	// 仅 provider=agent 会出现:后端不再同步送出,「已入队」与「真出纸」是两件事,
	// 用独立状态区分,否则代理掉线时日志会显示成「已送出」而实际一张纸都没吐。
	PrintStatusQueued = 2
)

// 打印触发场景(落 tb_print_log.trigger_by),用于区分「自动打印」与「人工补打」。
const (
	PrintTriggerOrder   = "order"   // 顾客下单
	PrintTriggerAppend  = "append"  // 顾客加菜
	PrintTriggerSettle  = "settle"  // 收银结账
	PrintTriggerTest    = "test"    // 测试打印
	PrintTriggerReprint = "reprint" // 人工补打
)

// PrintLog 对应 tb_print_log 表:一次「向某台打印机发送某张单据」的记录。
type PrintLog struct {
	PrintID     int
	OrderID     int
	OrderNo     string
	TableNo     string
	TableName   string
	PrinterID   int
	PrinterName string
	PrinterType int
	Provider    string
	DocType     string
	Copies      int
	Status      int
	RemoteID    string // 飞鹅返回的云端订单号(可用于追问打印状态)
	Detail      string // TCP: 地址 / 失败原因;飞鹅: 云端订单号或错误信息
	TriggerBy   string
	Operator    string
	CostMs      int
	CreateTime  string
}
