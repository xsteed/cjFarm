package po

// PrintAgent 对应 tb_print_agent 表:一台门店打印代理的身份记录。
//
// 令牌只存 SHA-256 hash,明文仅在创建时返回一次。
// printer_ids 为空表示授权全部打印机;非空时该代理只能取到列表内打印机的任务。
type PrintAgent struct {
	AgentID    int
	AgentName  string
	TokenHash  string // 永不序列化,只存 SHA-256 hash
	TokenHint  string
	PrinterIDs string
	Status     int // 1=启用 0=吊销
	LastSeen   string
	LastReport string
	CreateTime string
	UpdateTime string
}
