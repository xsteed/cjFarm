package dto

import "dining-system/internal/po"

// PrintLog 对应 tb_print_log 表:一次「向某台打印机发送某张单据」的记录(API 出参)。
type PrintLog struct {
	PrintID     int    `json:"printId"`
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	ShortNo     string `json:"shortNo"` // 由 orderNo 派生的可报读短号,不入库
	TableNo     string `json:"tableNo"`
	TableName   string `json:"tableName"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"`
	Provider    string `json:"provider"`
	DocType     string `json:"docType"`
	Copies      int    `json:"copies"`
	Status      int    `json:"status"`
	RemoteID    string `json:"remoteId"` // 飞鹅返回的云端订单号(可用于追问打印状态)
	Detail      string `json:"detail"`   // TCP: 地址 / 失败原因;飞鹅: 云端订单号或错误信息
	TriggerBy   string `json:"triggerBy"`
	Operator    string `json:"operator"`
	CostMs      int    `json:"costMs"`
	CreateTime  string `json:"createTime"`
}

// FromPrintLog 将持久化对象转为 API 出参,ShortNo 由 OrderNo 派生。
func FromPrintLog(p po.PrintLog) PrintLog {
	return PrintLog{
		PrintID:     p.PrintID,
		OrderID:     p.OrderID,
		OrderNo:     p.OrderNo,
		ShortNo:     po.ShortOrderNo(p.OrderNo),
		TableNo:     p.TableNo,
		TableName:   p.TableName,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		Provider:    p.Provider,
		DocType:     p.DocType,
		Copies:      p.Copies,
		Status:      p.Status,
		RemoteID:    p.RemoteID,
		Detail:      p.Detail,
		TriggerBy:   p.TriggerBy,
		Operator:    p.Operator,
		CostMs:      p.CostMs,
		CreateTime:  p.CreateTime,
	}
}

// ToPO 将 API 出参转回持久化对象,ShortNo 不入库。
func (l PrintLog) ToPO() po.PrintLog {
	return po.PrintLog{
		PrintID:     l.PrintID,
		OrderID:     l.OrderID,
		OrderNo:     l.OrderNo,
		TableNo:     l.TableNo,
		TableName:   l.TableName,
		PrinterID:   l.PrinterID,
		PrinterName: l.PrinterName,
		PrinterType: l.PrinterType,
		Provider:    l.Provider,
		DocType:     l.DocType,
		Copies:      l.Copies,
		Status:      l.Status,
		RemoteID:    l.RemoteID,
		Detail:      l.Detail,
		TriggerBy:   l.TriggerBy,
		Operator:    l.Operator,
		CostMs:      l.CostMs,
		CreateTime:  l.CreateTime,
	}
}
