package service

import (
	"fmt"
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// printSvcInit 初始化独立 SQLite 测试库。
func printSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "print_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestPrintSvcPrinterCRUD(t *testing.T) {
	printSvcInit(t)

	// seed 含 2 台停用打印机。
	printers, err := ListPrinters()
	if err != nil || len(printers) < 2 {
		t.Fatalf("应列出 seed 打印机, got len=%d err=%v", len(printers), err)
	}

	id, err := InsertPrinter(po.Printer{
		PrinterName: "测试厨房机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderTCP,
		IP: "10.0.0.8", Port: 9100, Status: 1, Copies: 2,
	})
	if err != nil || id <= 0 {
		t.Fatalf("新增打印机失败: id=%d err=%v", id, err)
	}

	enabled, err := ListEnabledPrinters(po.PrinterTypeKitchen)
	if err != nil || len(enabled) != 1 || enabled[0].PrinterID != int(id) {
		t.Fatalf("启用厨房机应返回刚新增的打印机, got %+v err=%v", enabled, err)
	}

	p, err := LoadPrinter(int(id))
	if err != nil || p.PrinterName != "测试厨房机" {
		t.Fatalf("读取打印机异常: %+v err=%v", p, err)
	}

	if err := UpdatePrinter(po.Printer{PrinterID: int(id), PrinterName: "改名厨房机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderTCP, IP: "10.0.0.8", Port: 9100, Status: 1, Copies: 2}); err != nil {
		t.Fatalf("更新打印机失败: %v", err)
	}

	first, err := FirstEnabledPrinterByType(po.PrinterTypeKitchen)
	if err != nil || first != int(id) {
		t.Fatalf("第一台启用厨房机应为新增打印机, got %d err=%v", first, err)
	}
	if n := CountPrintersByProvider(po.PrinterProviderTCP); n < 1 {
		t.Fatalf("TCP 打印机统计异常: %d", n)
	}

	if err := DeletePrinter(int(id)); err != nil {
		t.Fatalf("删除打印机失败: %v", err)
	}
}

func TestPrintSvcParseJoinIDList(t *testing.T) {
	cases := []struct {
		csv  string
		want []int
	}{
		{csv: "", want: nil},
		{csv: "1,2,3", want: []int{1, 2, 3}},
		{csv: "1,a,0,2,", want: []int{1, 2}},
		{csv: "  4 , 5 ", want: []int{4, 5}},
	}
	for _, c := range cases {
		got := ParseIDList(c.csv)
		if len(got) != len(c.want) {
			t.Fatalf("ParseIDList(%q) = %v, want %v", c.csv, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("ParseIDList(%q) = %v, want %v", c.csv, got, c.want)
			}
		}
	}
	if got := JoinIDList([]int{2, 1, 2, 3, 0}); got != "2,1,3" {
		t.Fatalf("JoinIDList 应去重并保持顺序, got %q", got)
	}
}

func TestPrintSvcLog(t *testing.T) {
	printSvcInit(t)

	logID, err := InsertPrintLogReturningID(po.PrintLog{
		OrderID: 1, OrderNo: "PL1", TableNo: "T1", TableName: "测试桌",
		PrinterID: 1, PrinterName: "打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderAgent, DocType: po.PrintDocKitchen, Copies: 1,
		Status: po.PrintStatusQueued, TriggerBy: po.PrintTriggerOrder, Operator: "tester",
	})
	if err != nil || logID <= 0 {
		t.Fatalf("写入打印日志失败: id=%d err=%v", logID, err)
	}

	l, err := LoadPrintLog(logID)
	if err != nil || l.OrderNo != "PL1" || l.Status != po.PrintStatusQueued {
		t.Fatalf("读取打印日志异常: %+v err=%v", l, err)
	}

	if err := UpdatePrintLogResult(logID, po.PrintStatusSuccess, "ok", 123); err != nil {
		t.Fatalf("回写打印日志失败: %v", err)
	}
	l, _ = LoadPrintLog(logID)
	if l.Status != po.PrintStatusSuccess || l.Detail != "ok" || l.CostMs != 123 {
		t.Fatalf("打印日志回写异常: %+v", l)
	}

	statusQueued := po.PrintStatusQueued
	total, list, err := ListPrintLogs(PrintLogQuery{OrderNo: "PL1"}, 1, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("打印日志列表异常: total=%d err=%v", total, err)
	}
	_ = statusQueued
}

func TestPrintSvcJobLifecycle(t *testing.T) {
	printSvcInit(t)
	now := store.Now()

	jobID, err := InsertPrintJob(po.PrintJob{
		PrinterID: 1, PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen,
		IP: "10.0.0.8", Port: 9100, DocType: po.PrintDocKitchen,
		OrderID: 1, OrderNo: "PJ1", TableNo: "T1", Copies: 1,
		Payload: "line1\nline2", TriggerBy: po.PrintTriggerOrder, Operator: "tester", CreateTime: now,
	})
	if err != nil || jobID <= 0 {
		t.Fatalf("入队打印任务失败: id=%d err=%v", jobID, err)
	}

	j, err := LoadPrintJob(jobID)
	if err != nil || j.DeliveryID == "" || j.Status != po.PrintJobPending {
		t.Fatalf("读取打印任务异常: %+v err=%v", j, err)
	}

	// 取单。
	claimed, err := ClaimPrintJobs("agent-1", 10, 60, "", "", "", nil)
	if err != nil || len(claimed) != 1 || claimed[0].JobID != jobID {
		t.Fatalf("取单异常: %+v err=%v", claimed, err)
	}
	if claimed[0].Status != po.PrintJobClaimed {
		t.Fatalf("取单后状态应为已取单, got %d", claimed[0].Status)
	}

	// 成功结案。
	if err := MarkPrintJobDone(jobID); err != nil {
		t.Fatalf("打印任务结案失败: %v", err)
	}

	pending, dead := PrintJobStats()
	if pending != 0 || dead != 0 {
		t.Fatalf("结案后队列统计应均为 0, got pending=%d dead=%d", pending, dead)
	}
}

func TestPrintSvcJobRetryAndCancel(t *testing.T) {
	printSvcInit(t)

	jobID, err := InsertPrintJob(po.PrintJob{
		PrinterID: 1, PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen,
		IP: "10.0.0.8", Port: 9100, DocType: po.PrintDocKitchen,
		OrderID: 1, OrderNo: "PJR1", TableNo: "T1", Copies: 1, Payload: "x", CreateTime: store.Now(),
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	// 未用尽重试次数：退回待取单(失败回执必须来自已取单任务)。
	if claimed, err := ClaimPrintJobs("agent-1", 10, 60, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	retry, err := RetryPrintJob(jobID, "paper jam", 0)
	if err != nil || !retry {
		t.Fatalf("未超限重试应返回继续重试, retry=%v err=%v", retry, err)
	}
	j, _ := LoadPrintJob(jobID)
	if j.Status != po.PrintJobPending {
		t.Fatalf("重试后应回到待取单, got %d", j.Status)
	}

	// 用尽重试次数：置为放弃(再次取单后回执)。
	if claimed, err := ClaimPrintJobs("agent-1", 10, 60, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET attempts=? WHERE job_id=?`, po.PrintJobMaxAttempts, jobID); err != nil {
		t.Fatalf("设置重试次数失败: %v", err)
	}
	retry, err = RetryPrintJob(jobID, "still failing", 0)
	if err != nil || retry {
		t.Fatalf("重试次数用尽应返回放弃, retry=%v err=%v", retry, err)
	}
	j, _ = LoadPrintJob(jobID)
	if j.Status != po.PrintJobDead {
		t.Fatalf("重试用尽后应置为放弃, got %d", j.Status)
	}

	// 取消某打印机未送出任务。
	j1, _ := InsertPrintJob(po.PrintJob{PrinterID: 2, PrinterName: "前台机", PrinterType: po.PrinterTypeGuest, IP: "10.0.0.2", Port: 9100, DocType: po.PrintDocGuest, OrderID: 2, OrderNo: "PJR2", TableNo: "T2", Copies: 1, Payload: "y", CreateTime: store.Now()})
	j2, _ := InsertPrintJob(po.PrintJob{PrinterID: 2, PrinterName: "前台机", PrinterType: po.PrinterTypeGuest, IP: "10.0.0.2", Port: 9100, DocType: po.PrintDocGuest, OrderID: 3, OrderNo: "PJR3", TableNo: "T3", Copies: 1, Payload: "z", CreateTime: store.Now()})
	n, _, err := CancelPrintJobsByPrinter(2)
	if err != nil || n != 2 {
		t.Fatalf("取消打印机任务应返回 2 条, got n=%d err=%v", n, err)
	}
	_ = j1
	_ = j2
}

func TestPrintSvcJobExpireCleanAndOldest(t *testing.T) {
	printSvcInit(t)

	// 过期厨房单。
	expiredID, err := InsertPrintJob(po.PrintJob{
		PrinterID: 1, PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen,
		IP: "10.0.0.8", Port: 9100, DocType: po.PrintDocKitchen,
		OrderID: 1, OrderNo: "PJE1", TableNo: "T1", Copies: 1, Payload: "x", CreateTime: "2000-01-01 00:00:00",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	n, _ := ExpireOverduePrintJobs(60, store.Now(), "", "")
	if n != 1 {
		t.Fatalf("过期任务应作废 1 条, got %d", n)
	}
	j, _ := LoadPrintJob(expiredID)
	if j.Status != po.PrintJobDead {
		t.Fatalf("过期任务应置为放弃, got %d", j.Status)
	}

	// 最老待送等待秒数。
	oldPendingID, err := InsertPrintJob(po.PrintJob{
		PrinterID: 1, PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen,
		IP: "10.0.0.8", Port: 9100, DocType: po.PrintDocKitchen,
		OrderID: 2, OrderNo: "PJE2", TableNo: "T2", Copies: 1, Payload: "y", CreateTime: "2000-01-01 00:00:00",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if OldestPendingSec() <= 0 {
		t.Fatalf("最老待送任务等待秒数应大于 0")
	}

	// 清理已结案老任务。
	doneID, err := InsertPrintJob(po.PrintJob{
		PrinterID: 1, PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen,
		IP: "10.0.0.8", Port: 9100, DocType: po.PrintDocKitchen,
		OrderID: 3, OrderNo: "PJE3", TableNo: "T3", Copies: 1, Payload: "z", CreateTime: "2000-01-01 00:00:00",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET status=? WHERE job_id=?`, po.PrintJobDone, doneID); err != nil {
		t.Fatalf("设置任务状态失败: %v", err)
	}
	if n := CleanDonePrintJobs(1); n < 1 {
		t.Fatalf("清理已结案任务应删除至少 1 条, got %d", n)
	}
	_ = oldPendingID
}

func TestPrintSvcAgent(t *testing.T) {
	printSvcInit(t)

	agent, token, err := InsertPrintAgent("门店代理", "1,2")
	if err != nil || agent.AgentID <= 0 || token == "" {
		t.Fatalf("新增代理失败: agent=%+v token=%q err=%v", agent, token, err)
	}

	found, ok := FindPrintAgentByToken(token)
	if !ok || found.AgentID != agent.AgentID {
		t.Fatalf("按令牌查找代理应成功, got %+v ok=%v", found, ok)
	}

	list := ListPrintAgents()
	if len(list) != 1 {
		t.Fatalf("代理列表应返回 1 条, got %d", len(list))
	}

	if err := TouchPrintAgent(agent.AgentID, "heartbeat-ok"); err != nil {
		t.Fatalf("更新代理心跳失败: %v", err)
	}

	if err := SetPrintAgentStatus(agent.AgentID, 0); err != nil {
		t.Fatalf("吊销代理失败: %v", err)
	}
	if _, ok := FindPrintAgentByToken(token); ok {
		t.Fatalf("已吊销代理不应再被找到")
	}
}

// TestPrintSvcAgentUpdateDelete 校验代理的编辑与清理:
// 名称留空=不改名、失效打印机授权被剔除、启用中不可删、吊销后可删。
func TestPrintSvcAgentUpdateDelete(t *testing.T) {
	printSvcInit(t)

	aliveID, err := InsertPrinter(po.Printer{
		PrinterName: "存活厨房机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderAgent,
		IP: "10.0.0.30", Port: 9100, Status: 1, Copies: 1,
	})
	if err != nil {
		t.Fatalf("新增打印机失败: %v", err)
	}
	// 已删打印机:它的 id 残留在 printer_ids 里就是「历史打印机」脏数据。
	goneID, err := InsertPrinter(po.Printer{
		PrinterName: "已删厨房机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderAgent,
		IP: "10.0.0.31", Port: 9100, Status: 1, Copies: 1,
	})
	if err != nil {
		t.Fatalf("新增打印机失败: %v", err)
	}
	if err := DeletePrinter(int(goneID)); err != nil {
		t.Fatalf("删除打印机失败: %v", err)
	}

	agent, _, err := InsertPrintAgent("待清理代理", fmt.Sprintf("%d,%d,999999", aliveID, goneID))
	if err != nil {
		t.Fatalf("新增代理失败: %v", err)
	}

	// 名称留空 → 不改名;授权范围里的已删打印机与不存在 id 应被剔除。
	if err := UpdatePrintAgent(agent.AgentID, "", fmt.Sprintf("%d,%d", aliveID, goneID)); err != nil {
		t.Fatalf("修改代理失败: %v", err)
	}
	got, err := LoadPrintAgent(agent.AgentID)
	if err != nil {
		t.Fatalf("读取代理失败: %v", err)
	}
	if got.AgentName != "待清理代理" {
		t.Fatalf("名称留空应不改名, got %q", got.AgentName)
	}
	if got.PrinterIDs != fmt.Sprintf("%d", aliveID) {
		t.Fatalf("授权范围应只剩存活打印机, got %q", got.PrinterIDs)
	}
	if err := UpdatePrintAgent(agent.AgentID, "改名代理", ""); err != nil {
		t.Fatalf("修改代理失败: %v", err)
	}
	if got, _ = LoadPrintAgent(agent.AgentID); got.AgentName != "改名代理" || got.PrinterIDs != "" {
		t.Fatalf("改名并清空授权范围失败: %+v", got)
	}

	// 启用中不允许删除:必须先吊销。
	if err := DeletePrintAgent(agent.AgentID); err == nil {
		t.Fatal("启用中的代理不应被删除")
	}
	if err := SetPrintAgentStatus(agent.AgentID, 0); err != nil {
		t.Fatalf("吊销代理失败: %v", err)
	}
	if err := DeletePrintAgent(agent.AgentID); err != nil {
		t.Fatalf("删除已吊销代理失败: %v", err)
	}
	if _, err := LoadPrintAgent(agent.AgentID); err == nil {
		t.Fatal("删除后代理不应再存在")
	}
	if err := DeletePrintAgent(agent.AgentID); err == nil {
		t.Fatal("重复删除应返回「代理不存在」")
	}
}

func TestPrintSvcAttachItemCategories(t *testing.T) {
	printSvcInit(t)

	// 取种子菜品的分类 ID。
	var dishID, categoryID int
	if err := store.DB.QueryRow(`SELECT dish_id, category_id FROM tb_dish ORDER BY dish_id LIMIT 1`).Scan(&dishID, &categoryID); err != nil {
		t.Fatalf("读取种子菜品失败: %v", err)
	}

	rows := AttachItemCategories([]po.OrderItem{{DishID: dishID, DishName: "测试菜"}})
	if len(rows) != 1 || rows[0].CategoryID != categoryID {
		t.Fatalf("补齐分类异常: %+v", rows)
	}
}
