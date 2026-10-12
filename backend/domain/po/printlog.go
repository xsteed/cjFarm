package po

// 打印单据类型(落 tb_print_log.doc_type)。
//
// guest_append(加菜小票)只在加菜场景产生。它的独立形态被完整保留下来
// (标题「【加菜小票】」、金额只有一行「当前合计」),补打与预览照样按它渲染,
// 重出来的票才与原始纪录对得上;唯一还原不了的是「当时新增了哪些菜」
// (订单只增不减、日志不存渲染快照),菜品区退化为当前整单,由管理端标注说明。
const (
	PrintDocKitchen     = "kitchen"
	PrintDocGuest       = "guest"
	PrintDocGuestAppend = "guest_append"
)

// 厨房单打印粒度(配置项 print_kitchen_split_mode):
//   - KitchenSplitWhole   整单一张(默认,与历史行为一致);
//   - KitchenSplitItem    一菜一单:同一道菜合并为一张(标 xN);
//   - KitchenSplitPortion 一菜一单且按份数拆:点 3 份出 3 张、每张 x1,供分盘出菜。
//
// 无论哪一档,食客小票永远整单一张 —— 金额合计必须完整,拆了就对不上账。
const (
	KitchenSplitWhole   = "whole"
	KitchenSplitItem    = "item"
	KitchenSplitPortion = "portion"
)

// KitchenSplitModes 打印粒度的合法取值白名单:读配置与写配置都据此收敛,
// 库里的值因此永远合法(前端 t-radio-group 拿到非法值会显示「一个都没选中」)。
var KitchenSplitModes = []string{KitchenSplitWhole, KitchenSplitItem, KitchenSplitPortion}

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
