package dto

import "dining-system/domain/po"

// PrintAgent 对应 tb_print_agent 表:一台门店打印代理的身份记录(API 出参)。
// PrinterIDList 为运行时补算,不入库。
type PrintAgent struct {
	AgentID    int    `json:"agentId"`
	AgentName  string `json:"agentName"`
	TokenHash  string `json:"-"` // 永不序列化,只存 SHA-256 hash
	TokenHint  string `json:"tokenHint"`
	PrinterIDs string `json:"printerIds"`
	Status     int    `json:"status"` // 1=启用 0=吊销
	LastSeen   string `json:"lastSeen"`
	LastReport string `json:"lastReport"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`

	// PrinterIDList 为运行时补算,不入库。
	PrinterIDList []int `json:"printerIdList,omitempty"`

	// Online 为运行时补算:服务端按 90s 窗口判定,前端不要自己解析 lastSeen ——
	// lastSeen 是服务端本地时区的墙钟字符串,浏览器按本地时区解析会在
	// 「容器 UTC + 门店浏览器东八区」下恒判离线(见 print.AgentSeenOnline)。
	Online bool `json:"online"`
}

// FromPrintAgent 将持久化对象与运行时补算结果组装为 API 出参。
func FromPrintAgent(p po.PrintAgent, printerIDList []int) PrintAgent {
	return PrintAgent{
		AgentID:       p.AgentID,
		AgentName:     p.AgentName,
		TokenHash:     p.TokenHash,
		TokenHint:     p.TokenHint,
		PrinterIDs:    p.PrinterIDs,
		Status:        p.Status,
		LastSeen:      p.LastSeen,
		LastReport:    p.LastReport,
		CreateTime:    p.CreateTime,
		UpdateTime:    p.UpdateTime,
		PrinterIDList: printerIDList,
	}
}

// ToPO 将 API 出参转回持久化对象,PrinterIDList 不入库。
func (p PrintAgent) ToPO() po.PrintAgent {
	return po.PrintAgent{
		AgentID:    p.AgentID,
		AgentName:  p.AgentName,
		TokenHash:  p.TokenHash,
		TokenHint:  p.TokenHint,
		PrinterIDs: p.PrinterIDs,
		Status:     p.Status,
		LastSeen:   p.LastSeen,
		LastReport: p.LastReport,
		CreateTime: p.CreateTime,
		UpdateTime: p.UpdateTime,
	}
}
