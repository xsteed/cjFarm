// 打印域用例:把 handler 与 print 包的数据访问统一收口到 service,
// 使二者不再直接依赖 dao/store。数据访问仍落 dao,业务判断(任务状态机、
// 重试策略、agent 心跳、日志统计)的取数/回写在此下沉。
//
// 依赖方向:service 依赖 po + store + dao,不依赖 handler/print。
package service

import (
	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// PrintOrderItemRow 打印域订单明细投影(带菜品分类)。
// 与 dao.OrderItemRow 同一类型,仅作为 service 对 print/handler 暴露的别名,
// 避免调用方为拿一个切片元素类型而反向 import dao。
type PrintOrderItemRow = dao.OrderItemRow

// ============ 打印机 ============

// ListPrinters 查询未删除的打印机列表。
func ListPrinters() ([]po.Printer, error) { return dao.ListPrinters() }

// ListEnabledPrinters 查询启用的打印机;printerType 传 0 表示全部,
// 否则按类型过滤(1=厨房单 2=食客小票)。
func ListEnabledPrinters(printerType int) ([]po.Printer, error) {
	return dao.ListEnabledPrinters(printerType)
}

// LoadPrinter 按 ID 读取一台打印机(含已停用/已删除 —— 补打历史单据时仍需要它)。
func LoadPrinter(printerID int) (po.Printer, error) { return dao.LoadPrinter(printerID) }

// InsertPrinter 新增打印机并返回自增 ID。
func InsertPrinter(p po.Printer) (int64, error) { return dao.InsertPrinter(p) }

// UpdatePrinter 更新打印机信息。
func UpdatePrinter(p po.Printer) error { return dao.UpdatePrinter(p) }

// DeletePrinter 软删除打印机。
func DeletePrinter(id int) error { return dao.DeletePrinter(id) }

// FirstEnabledPrinterByType 查询指定类型的第一台启用打印机。
func FirstEnabledPrinterByType(printerType int) (int, error) {
	return dao.FirstEnabledPrinterByType(printerType)
}

// CountPrintersByProvider 按接入方式统计未删除打印机数量。
func CountPrintersByProvider(provider string) int { return dao.CountPrintersByProvider(provider) }

// ListCategories 查询未删除的分类列表。
func ListCategories() ([]po.Category, error) { return dao.ListCategories() }

// ParseIDList 把 CSV ID 串解析为整数切片。
func ParseIDList(csv string) []int { return dao.ParseIDList(csv) }

// JoinIDList 把整数 ID 切片拼成 CSV(去重、保持首次出现顺序)。
func JoinIDList(ids []int) string { return dao.JoinIDList(ids) }

// ============ 打印日志 ============

// InsertPrintLog 写入一条打印日志(旁路数据,调用方决定是否记后端日志)。
func InsertPrintLog(l po.PrintLog) error { return dao.InsertPrintLog(l) }

// InsertPrintLogReturningID 写入一条打印日志并返回主键(代理通道入队用)。
func InsertPrintLogReturningID(l po.PrintLog) (int, error) {
	return dao.InsertPrintLogReturningID(l)
}

// UpdatePrintLogResult 回写打印日志结果(仅当仍为「排队中」时改写)。
func UpdatePrintLogResult(printID, status int, detail string, costMs int) error {
	return dao.UpdatePrintLogResult(printID, status, detail, costMs)
}

// LoadPrintLog 按 ID 读取一条打印日志。
func LoadPrintLog(printID int) (po.PrintLog, error) { return dao.LoadPrintLog(printID) }

// PrintLogQuery 打印日志列表筛选条件;零值字段表示不限。
type PrintLogQuery struct {
	Status    *int
	DocType   string
	Provider  string
	PrinterID *int
	OrderNo   string
}

// ListPrintLogs 按条件分页查询打印日志。
func ListPrintLogs(q PrintLogQuery, pageNum, pageSize int) (total int, list []po.PrintLog, err error) {
	return dao.ListPrintLogs(dao.PrintLogQuery{
		Status:    q.Status,
		DocType:   q.DocType,
		Provider:  q.Provider,
		PrinterID: q.PrinterID,
		OrderNo:   q.OrderNo,
	}, pageNum, pageSize)
}

// ============ 打印任务队列 ============

// InsertPrintJob 入队一条打印任务,返回自增主键。
func InsertPrintJob(j po.PrintJob) (int, error) { return dao.InsertPrintJob(j) }

// LoadPrintJob 按 ID 读取一条任务。
func LoadPrintJob(jobID int) (po.PrintJob, error) { return dao.LoadPrintJob(jobID) }

// ClaimPrintJobs 代理取单:抢占可执行任务并原子标记为已取走。
func ClaimPrintJobs(agentID string, limit, leaseSeconds int, kitchenCutoff, guestCutoff, otherCutoff string, printerIDs []int) ([]po.PrintJob, error) {
	return dao.ClaimPrintJobs(agentID, limit, leaseSeconds, kitchenCutoff, guestCutoff, otherCutoff, printerIDs)
}

// MarkPrintJobDone 任务打印成功结案。
func MarkPrintJobDone(jobID int) error { return dao.MarkPrintJobDone(jobID) }

// RetryPrintJob 打印失败:重试次数未用尽则退回待取单,否则置为放弃。
func RetryPrintJob(jobID int, errMsg string, backoffSeconds int) (bool, error) {
	return dao.RetryPrintJob(jobID, errMsg, backoffSeconds)
}

// ReleasePrintJob 代理未真正尝试(熔断冷却挂起):任务退回队列并归还尝试次数。
func ReleasePrintJob(jobID int, errMsg string, retryAfterSeconds int) error {
	return dao.ReleasePrintJob(jobID, errMsg, retryAfterSeconds)
}

// CancelPrintJobsByPrinter 取消某台打印机尚未送出的任务。
func CancelPrintJobsByPrinter(printerID int) (int, []int, error) {
	return dao.CancelPrintJobsByPrinter(printerID)
}

// ExpireOverduePrintJobs 作废尚未送出的过期代理任务。
func ExpireOverduePrintJobs(leaseSeconds int, kitchenCutoff, guestCutoff, otherCutoff string) (int, []int) {
	return dao.ExpireOverduePrintJobs(leaseSeconds, kitchenCutoff, guestCutoff, otherCutoff)
}

// CleanDonePrintJobs 清理已结案的老任务。
func CleanDonePrintJobs(keepDays int) int { return dao.CleanDonePrintJobs(keepDays) }

// PrintJobStats 队列概况(待送 / 卡住)。
func PrintJobStats() (pending, dead int) { return dao.PrintJobStats() }

// PendingJobCountByPrinter 某台打印机的积压任务数。
func PendingJobCountByPrinter(printerID int) int { return dao.PendingJobCountByPrinter(printerID) }

// OldestPendingSec 返回最老未结案代理任务已等待的秒数。
func OldestPendingSec() int { return dao.OldestPendingSec() }

// ============ 打印代理身份 ============

// FindPrintAgentByToken 按明文令牌查找启用中的代理身份。
func FindPrintAgentByToken(tok string) (po.PrintAgent, bool) { return dao.FindPrintAgentByToken(tok) }

// LoadPrintAgent 按 ID 读取一台代理身份。
func LoadPrintAgent(agentID int) (po.PrintAgent, error) { return dao.LoadPrintAgent(agentID) }

// ListPrintAgents 按创建顺序返回全部代理身份。
func ListPrintAgents() []po.PrintAgent { return dao.ListPrintAgents() }

// InsertPrintAgent 新增一台代理身份,返回身份记录与仅展示一次的明文令牌。
func InsertPrintAgent(name, printerIDs string) (po.PrintAgent, string, error) {
	return dao.InsertPrintAgent(name, printerIDs)
}

// SetPrintAgentStatus 启用或吊销一台代理身份。
func SetPrintAgentStatus(agentID, status int) error { return dao.SetPrintAgentStatus(agentID, status) }

// UpdatePrintAgent 修改代理名称与授权范围(不重新签发令牌)。
//
// 名称留空表示不改名。授权范围先过一遍 FilterAlivePrinterIDs:打印机删掉后它的
// id 仍留在 printer_ids 里,前端会显示成「#12」这种无人认领的授权项,借每次编辑
// 顺手清掉。空范围表示「授权全部打印机」。
func UpdatePrintAgent(agentID int, name, printerIDs string) error {
	return dao.UpdatePrintAgent(agentID, name, JoinIDList(dao.FilterAlivePrinterIDs(ParseIDList(printerIDs))))
}

// DeletePrintAgent 删除一台已吊销的代理身份(启用中的必须先吊销)。
func DeletePrintAgent(agentID int) error { return dao.DeletePrintAgent(agentID) }

// TouchPrintAgent 记录代理最近一次心跳/取单/回执上报信息。
func TouchPrintAgent(agentID int, report string) error { return dao.TouchPrintAgent(agentID, report) }

// ============ 打印订单数据 ============

// AttachItemCategories 按 dish_id 补齐订单明细的分类 ID。
func AttachItemCategories(items []po.OrderItem) []PrintOrderItemRow {
	return dao.AttachItemCategories(items)
}

// LoadOrderItemsForPrint 加载订单明细并补齐分类(小票/厨房单打印用)。
func LoadOrderItemsForPrint(orderID int) []PrintOrderItemRow {
	return dao.AttachItemCategories(dao.LoadOrderItems(orderID))
}

// LoadOrderAndItemsForPrint 读取订单及其带分类明细(补打历史订单用,不校验订单状态)。
func LoadOrderAndItemsForPrint(orderID int) (po.Order, []PrintOrderItemRow, error) {
	o, err := dao.GetOrderByID(orderID)
	if err != nil {
		return o, nil, err
	}
	return o, dao.AttachItemCategories(dao.LoadOrderItems(orderID)), nil
}
