package po

// 打印任务状态(落 tb_print_job.status)。
const (
	PrintJobPending = 0 // 待代理取单
	PrintJobClaimed = 1 // 已被某个代理取走,打印中(带租约,超时可被重新取走)
	PrintJobDone    = 2 // 代理已成功送出
	PrintJobDead    = 3 // 重试次数用尽,放弃(需人工补打)
)

// 打印任务重试上限:超过后置为 dead,避免坏任务在队列里无限循环。
const PrintJobMaxAttempts = 3

// PrintJob 对应 tb_print_job 表:一条「待本地打印代理送出」的打印任务。
//
// Payload 存的是渲染后的等宽文本行(用 '\n' 连接),不是厂商协议字节;
// ESC/POS 字节在「代理取单时」由后端现场编码后以 base64 下发。
type PrintJob struct {
	JobID       int
	PrinterID   int
	PrinterName string
	PrinterType int
	IP          string // 打印机在门店内网的地址(云后端不直连,由代理使用)
	Port        int    // 默认 9100
	DocType     string
	OrderID     int
	OrderNo     string
	TableNo     string
	Copies      int
	PrintLogID  int // 关联的 tb_print_log 主键,回执时回写结果
	// DeliveryID 幂等投递号(入队时生成、永不变更):代理打印成功后本地持久化,
	// 若回执丢失导致任务被重新下发,代理凭它判断「这一条我已经打过了」,跳过打印直接回执。
	DeliveryID string
	// Payload 渲染后的文本行,以 '\n' 连接。
	Payload     string
	Status      int
	Attempts    int
	LastError   string
	ClaimedBy   string
	ClaimTime   string
	NextTryTime string
	TriggerBy   string
	Operator    string
	CreateTime  string
	DoneTime    string
}
