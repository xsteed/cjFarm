package dto

import "dining-system/internal/po"

// PrintJob 对应 tb_print_job 表:一条「待本地打印代理送出」的打印任务(API 出参)。
// Payload 体积大,不出现在列表接口里。
type PrintJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"`
	IP          string `json:"ip"`   // 打印机在门店内网的地址(云后端不直连,由代理使用)
	Port        int    `json:"port"` // 默认 9100
	DocType     string `json:"docType"`
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	Copies      int    `json:"copies"`
	PrintLogID  int    `json:"printLogId"` // 关联的 tb_print_log 主键,回执时回写结果
	DeliveryID  string `json:"deliveryId"`
	Payload     string `json:"-"` // 不出现在列表接口里(体积大,只有取单接口用)
	Status      int    `json:"status"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"lastError"`
	ClaimedBy   string `json:"claimedBy"`
	ClaimTime   string `json:"claimTime"`
	NextTryTime string `json:"nextTryTime"`
	TriggerBy   string `json:"triggerBy"`
	Operator    string `json:"operator"`
	CreateTime  string `json:"createTime"`
	DoneTime    string `json:"doneTime"`
}

// FromPrintJob 将持久化对象转为 API 出参。
func FromPrintJob(p po.PrintJob) PrintJob {
	return PrintJob{
		JobID:       p.JobID,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		IP:          p.IP,
		Port:        p.Port,
		DocType:     p.DocType,
		OrderID:     p.OrderID,
		OrderNo:     p.OrderNo,
		TableNo:     p.TableNo,
		Copies:      p.Copies,
		PrintLogID:  p.PrintLogID,
		DeliveryID:  p.DeliveryID,
		Payload:     p.Payload,
		Status:      p.Status,
		Attempts:    p.Attempts,
		LastError:   p.LastError,
		ClaimedBy:   p.ClaimedBy,
		ClaimTime:   p.ClaimTime,
		NextTryTime: p.NextTryTime,
		TriggerBy:   p.TriggerBy,
		Operator:    p.Operator,
		CreateTime:  p.CreateTime,
		DoneTime:    p.DoneTime,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (p PrintJob) ToPO() po.PrintJob {
	return po.PrintJob{
		JobID:       p.JobID,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		IP:          p.IP,
		Port:        p.Port,
		DocType:     p.DocType,
		OrderID:     p.OrderID,
		OrderNo:     p.OrderNo,
		TableNo:     p.TableNo,
		Copies:      p.Copies,
		PrintLogID:  p.PrintLogID,
		DeliveryID:  p.DeliveryID,
		Payload:     p.Payload,
		Status:      p.Status,
		Attempts:    p.Attempts,
		LastError:   p.LastError,
		ClaimedBy:   p.ClaimedBy,
		ClaimTime:   p.ClaimTime,
		NextTryTime: p.NextTryTime,
		TriggerBy:   p.TriggerBy,
		Operator:    p.Operator,
		CreateTime:  p.CreateTime,
		DoneTime:    p.DoneTime,
	}
}

// AgentJob 下发给本地打印代理的单条任务(接口出参,payload 为调用方已编码好的字符串)。
type AgentJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Copies      int    `json:"copies"`
	DocType     string `json:"docType"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	DeliveryID  string `json:"deliveryId"`
	Payload     string `json:"payload"`
}

// FromPrintJobWithPayload 将持久化对象与已编码好的 payload 组装为代理取单出参。
func FromPrintJobWithPayload(p po.PrintJob, payload string) AgentJob {
	return AgentJob{
		JobID:       p.JobID,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		IP:          p.IP,
		Port:        p.Port,
		Copies:      p.Copies,
		DocType:     p.DocType,
		OrderNo:     p.OrderNo,
		TableNo:     p.TableNo,
		DeliveryID:  p.DeliveryID,
		Payload:     payload,
	}
}
