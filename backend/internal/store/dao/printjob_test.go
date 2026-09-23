package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// jobDaoInitDB 初始化打印任务测试用的临时库。
func jobDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "printjob.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// jobDaoJob 构造一条入队任务。
func jobDaoJob(docType string, printerID int, createTime string) po.PrintJob {
	return po.PrintJob{
		PrinterID:   printerID,
		PrinterName: "测试打印机",
		PrinterType: po.PrinterTypeKitchen,
		IP:          "127.0.0.1",
		Port:        9100,
		DocType:     docType,
		OrderID:     1,
		OrderNo:     "JOB-ORDER",
		TableNo:     "01",
		Copies:      1,
		PrintLogID:  100,
		Payload:     "line1\nline2",
		TriggerBy:   po.PrintTriggerOrder,
		Operator:    "boss",
		CreateTime:  createTime,
	}
}

func TestPrinterCRUD(t *testing.T) {
	jobDaoInitDB(t)

	// 种子包含 2 台停用打印机。
	list, err := ListPrinters()
	if err != nil {
		t.Fatalf("查询打印机失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("种子应包含 2 台打印机, got %d", len(list))
	}
	if enabled, _ := ListEnabledPrinters(0); len(enabled) != 0 {
		t.Fatalf("种子打印机应全部停用, got %d", len(enabled))
	}

	id, err := InsertPrinter(po.Printer{
		PrinterName: "厨房机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderAgent,
		IP: "192.168.1.10", Port: 9100, FeieSN: "sn-1", PaperWidth: 48, Copies: 2, CategoryIDs: "1,2", Status: 1,
	})
	if err != nil {
		t.Fatalf("新增打印机失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("新增打印机应返回自增 ID, got %d", id)
	}

	if got := CountPrintersByProvider(po.PrinterProviderAgent); got != 1 {
		t.Fatalf("agent 打印机应为 1 台, got %d", got)
	}
	if got, err := FirstEnabledPrinterByType(po.PrinterTypeKitchen); err != nil || got != int(id) {
		t.Fatalf("首台启用厨房机应为 %d, got %d err=%v", id, got, err)
	}

	if err := UpdatePrinter(po.Printer{
		PrinterID: int(id), PrinterName: "厨房机-改", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderAgent,
		IP: "192.168.1.11", Port: 9100, FeieSN: "sn-1", PaperWidth: 48, Copies: 3, CategoryIDs: "1", Status: 0,
	}); err != nil {
		t.Fatalf("更新打印机失败: %v", err)
	}
	p, err := LoadPrinter(int(id))
	if err != nil {
		t.Fatalf("回读打印机失败: %v", err)
	}
	if p.PrinterName != "厨房机-改" || p.Copies != 3 || p.Status != 0 {
		t.Fatalf("打印机更新未生效: %+v", p)
	}

	// 停用后不再出现在启用列表。
	if enabled, _ := ListEnabledPrinters(0); len(enabled) != 0 {
		t.Fatalf("停用后启用列表应为空, got %d", len(enabled))
	}

	if err := DeletePrinter(int(id)); err != nil {
		t.Fatalf("删除打印机失败: %v", err)
	}
	after, _ := ListPrinters()
	if len(after) != 2 {
		t.Fatalf("软删除后应回到 2 台, got %d", len(after))
	}
}

func TestPrintJobInsertAndLoad(t *testing.T) {
	jobDaoInitDB(t)

	j := jobDaoJob(po.PrintDocKitchen, 1, store.Now())
	j.DeliveryID = "custom-delivery-id"
	id, err := InsertPrintJob(j)
	if err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("应返回自增任务 ID, got %d", id)
	}

	got, err := LoadPrintJob(id)
	if err != nil {
		t.Fatalf("回读任务失败: %v", err)
	}
	if got.DeliveryID != "custom-delivery-id" {
		t.Fatalf("指定 delivery_id 应保留, got %q", got.DeliveryID)
	}
	if got.Status != po.PrintJobPending || got.Attempts != 0 {
		t.Fatalf("新任务应为待取单且未尝试, got status=%d attempts=%d", got.Status, got.Attempts)
	}
	if got.Payload != "line1\nline2" {
		t.Fatalf("payload 回读异常: %q", got.Payload)
	}

	// 未指定 delivery_id 时自动生成 32 位 hex。
	j2 := jobDaoJob(po.PrintDocGuest, 1, store.Now())
	id2, err := InsertPrintJob(j2)
	if err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	got2, _ := LoadPrintJob(id2)
	if len(got2.DeliveryID) != 32 {
		t.Fatalf("自动生成的 delivery_id 应为 32 位, got %q", got2.DeliveryID)
	}
	if got := newDeliveryID(); len(got) != 32 {
		t.Fatalf("newDeliveryID 应返回 32 位, got %q", got)
	}

	if _, err := LoadPrintJob(999999); err == nil {
		t.Fatal("查询不存在的任务应报错")
	}
}

func TestClaimPrintJobsPending(t *testing.T) {
	jobDaoInitDB(t)
	id, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}

	claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil)
	if err != nil {
		t.Fatalf("取单失败: %v", err)
	}
	if len(claimed) != 1 || claimed[0].JobID != id {
		t.Fatalf("应取到任务 %d, got %+v", id, claimed)
	}
	if claimed[0].Status != po.PrintJobClaimed || claimed[0].Attempts != 1 || claimed[0].ClaimedBy != "agent-1" {
		t.Fatalf("取单后任务状态异常: %+v", claimed[0])
	}

	// 已被取走且租约未超时,不应再次取到。
	again, err := ClaimPrintJobs("agent-2", 10, 30, "", "", "", nil)
	if err != nil {
		t.Fatalf("再次取单失败: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("租约未超时不应重复取单, got %+v", again)
	}
}

func TestClaimPrintJobsLeaseExpiredAndScope(t *testing.T) {
	jobDaoInitDB(t)

	// 直插一条「打印中」且租约已超时的任务。
	old := shiftSeconds(store.Now(), -120)
	if _, err := store.DB.Exec(`INSERT INTO tb_print_job(printer_id, printer_name, printer_type, ip, port,
		doc_type, order_id, order_no, table_no, copies, print_log_id, delivery_id, payload,
		status, attempts, last_error, claimed_by, claim_time, next_try_time,
		trigger_by, operator, create_time, done_time)
		VALUES(1,'p',1,'127.0.0.1',9100,'kitchen',1,'NO', '01',1,100,'d1','payload',
		1,1,'','agent-old',?,'','order','boss',?, '')`, old, store.Now()); err != nil {
		t.Fatalf("直插租约超时任务失败: %v", err)
	}

	claimed, err := ClaimPrintJobs("agent-new", 10, 30, "", "", "", nil)
	if err != nil {
		t.Fatalf("取单失败: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ClaimedBy != "agent-new" || claimed[0].Attempts != 2 {
		t.Fatalf("租约超时任务应被重新取走, got %+v", claimed)
	}

	// scope 过滤:插入两台打印机的任务,只取指定打印机。
	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now())); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 2, store.Now())); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	scoped, err := ClaimPrintJobs("agent-3", 10, 30, "", "", "", []int{1})
	if err != nil {
		t.Fatalf("带 scope 取单失败: %v", err)
	}
	for _, j := range scoped {
		if j.PrinterID != 1 {
			t.Fatalf("scope 过滤失效,取到了打印机 %d 的任务", j.PrinterID)
		}
	}
}

func TestClaimPrintJobsExpiredAndLimit(t *testing.T) {
	jobDaoInitDB(t)

	// 过期厨房单:create_time 早于 cutoff。
	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, "2000-01-01 00:00:00")); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	got, err := ClaimPrintJobs("agent", 10, 30, "2020-01-01 00:00:00", "", "", nil)
	if err != nil {
		t.Fatalf("取单失败: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("过期任务不应被取走, got %+v", got)
	}

	// limit<=0 或 >50 都按 10 兜底:插入 3 条待取单,limit=0 应取到 3 条。
	// 继续带上 kitchenCutoff,避免把上面的过期厨房单也取走。
	for i := 0; i < 3; i++ {
		if _, err := InsertPrintJob(jobDaoJob(po.PrintDocGuest, 1, store.Now())); err != nil {
			t.Fatalf("入队任务失败: %v", err)
		}
	}
	got, err = ClaimPrintJobs("agent", 0, 30, "2020-01-01 00:00:00", "", "", nil)
	if err != nil {
		t.Fatalf("取单失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limit=0 应按 10 兜底并取到 3 条, got %d", len(got))
	}
}

// TestPrintJobAckStateGuard 回执状态守卫(高危回归):
// 非 claimed 状态的回执被拒;已 done 的任务不可被失败回执复活;重复成功回执幂等。
func TestPrintJobAckStateGuard(t *testing.T) {
	jobDaoInitDB(t)

	// pending 状态直接结案:应被拒(必须先被代理取单)。
	id, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if err := MarkPrintJobDone(id); err == nil {
		t.Fatal("pending 状态结案应被拒")
	}

	// 取单后正常结案。
	if claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	if err := MarkPrintJobDone(id); err != nil {
		t.Fatalf("claimed 状态结案失败: %v", err)
	}

	// done 状态的迟到失败回执:应被拒,禁止复活已完结任务(否则票会重复打印)。
	if _, err := RetryPrintJob(id, "迟到失败回执", 5); err == nil {
		t.Fatal("done 状态重试应被拒")
	}
	done, _ := LoadPrintJob(id)
	if done.Status != po.PrintJobDone {
		t.Fatalf("done 任务不应被回执改状态, got %d", done.Status)
	}

	// 重复成功回执:幂等成功(网络重放场景)。
	if err := MarkPrintJobDone(id); err != nil {
		t.Fatalf("重复成功回执应幂等成功: %v", err)
	}
}

func TestPrintJobLifecycle(t *testing.T) {
	jobDaoInitDB(t)

	// 成功结案:必须先取单(claimed),成功回执才允许从打印中迁到 done。
	id, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	if err := MarkPrintJobDone(id); err != nil {
		t.Fatalf("结案失败: %v", err)
	}
	done, _ := LoadPrintJob(id)
	if done.Status != po.PrintJobDone || done.DoneTime == "" {
		t.Fatalf("结案后任务状态异常: %+v", done)
	}

	// 失败重试:attempts 未用尽返回 true(同样需先处于 claimed)。
	id2, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	willRetry, err := RetryPrintJob(id2, "打印失败", 5)
	if err != nil {
		t.Fatalf("重试失败: %v", err)
	}
	if !willRetry {
		t.Fatal("attempts 未用尽应继续重试")
	}
	retryJob, _ := LoadPrintJob(id2)
	if retryJob.Status != po.PrintJobPending || retryJob.NextTryTime == "" {
		t.Fatalf("重试后任务应退回待取单, got %+v", retryJob)
	}

	// 重试次数用尽:attempts>=3 时置为放弃。
	id3, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET attempts=? WHERE job_id=?`, po.PrintJobMaxAttempts, id3); err != nil {
		t.Fatalf("设置 attempts 失败: %v", err)
	}
	willRetry, err = RetryPrintJob(id3, "又失败", 5)
	if err != nil {
		t.Fatalf("重试失败: %v", err)
	}
	if willRetry {
		t.Fatal("attempts 用尽后不应再重试")
	}
	dead, _ := LoadPrintJob(id3)
	if dead.Status != po.PrintJobDead {
		t.Fatalf("用尽重试后任务应为放弃, got %d", dead.Status)
	}

	// 统计与积压。
	pending, deadCount := PrintJobStats()
	if pending < 1 || deadCount < 1 {
		t.Fatalf("任务统计异常: pending=%d dead=%d", pending, deadCount)
	}
	if n := PendingJobCountByPrinter(1); n < 1 {
		t.Fatalf("打印机积压应至少 1 条, got %d", n)
	}
}

func TestCancelAndExpirePrintJobs(t *testing.T) {
	jobDaoInitDB(t)

	// 两台打印机各入队一条 pending。
	id1, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 2, store.Now())); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}

	n, logIDs, err := CancelPrintJobsByPrinter(1)
	if err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}
	if n != 1 || len(logIDs) != 1 || logIDs[0] != 100 {
		t.Fatalf("取消任务结果异常: n=%d logIDs=%v", n, logIDs)
	}
	if _, err := LoadPrintJob(id1); err == nil {
		t.Fatal("被取消的任务应已物理删除")
	}

	// 过期作废:cutoffs 全空直接返回 0。
	if n, logs := ExpireOverduePrintJobs(60, "", "", ""); n != 0 || logs != nil {
		t.Fatalf("空 cutoff 应直接返回 0, got n=%d logs=%v", n, logs)
	}
	// 插入一条过期厨房单。
	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 2, "2000-01-01 00:00:00")); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	n, logIDs = ExpireOverduePrintJobs(60, "2020-01-01 00:00:00", "", "")
	if n != 1 || len(logIDs) != 1 {
		t.Fatalf("过期作废结果异常: n=%d logIDs=%v", n, logIDs)
	}
}

func TestCleanDonePrintJobs(t *testing.T) {
	jobDaoInitDB(t)

	// keepDays<=0 不清理。
	if n := CleanDonePrintJobs(0); n != 0 {
		t.Fatalf("keepDays<=0 不应清理, got %d", n)
	}

	// 造一条很久以前的已结案任务(先取单再结案)。
	id, _ := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now()))
	if claimed, err := ClaimPrintJobs("agent-1", 10, 30, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}
	if err := MarkPrintJobDone(id); err != nil {
		t.Fatalf("结案失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET create_time='2000-01-01 00:00:00' WHERE job_id=?`, id); err != nil {
		t.Fatalf("设置创建时间失败: %v", err)
	}

	if n := CleanDonePrintJobs(7); n != 1 {
		t.Fatalf("应清理 1 条老任务, got %d", n)
	}
}

func TestUpdatePrintLogResult(t *testing.T) {
	jobDaoInitDB(t)

	// printID<=0 直接返回 nil。
	if err := UpdatePrintLogResult(0, po.PrintStatusSuccess, "x", 0); err != nil {
		t.Fatalf("printID<=0 应返回 nil, got %v", err)
	}

	logID := logDaoInsert(t, po.PrintLog{OrderNo: "R-1", Status: po.PrintStatusQueued, CreateTime: store.Now()})

	if err := UpdatePrintLogResult(logID, po.PrintStatusSuccess, "已送出", 120); err != nil {
		t.Fatalf("回写日志结果失败: %v", err)
	}
	l, _ := LoadPrintLog(logID)
	if l.Status != po.PrintStatusSuccess || l.CostMs != 120 || l.Detail != "已送出" {
		t.Fatalf("日志回写异常: %+v", l)
	}

	// costMs<0 分支:不更新 cost_ms。
	logID2 := logDaoInsert(t, po.PrintLog{OrderNo: "R-2", Status: po.PrintStatusQueued, CreateTime: store.Now()})
	if err := UpdatePrintLogResult(logID2, po.PrintStatusFailed, "失败", -1); err != nil {
		t.Fatalf("回写日志结果失败: %v", err)
	}
	l2, _ := LoadPrintLog(logID2)
	if l2.Status != po.PrintStatusFailed || l2.CostMs != 0 {
		t.Fatalf("costMs<0 回写异常: %+v", l2)
	}
}

func TestOldestPendingSecAndShiftSeconds(t *testing.T) {
	jobDaoInitDB(t)

	if got := OldestPendingSec(); got != 0 {
		t.Fatalf("无积压应返回 0, got %d", got)
	}

	if _, err := InsertPrintJob(jobDaoJob(po.PrintDocKitchen, 1, store.Now())); err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	if got := OldestPendingSec(); got < 0 {
		t.Fatalf("有积压应返回非负秒数, got %d", got)
	}

	// 非法创建时间返回 0。
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET create_time='bad-time' WHERE status IN (?,?)`, po.PrintJobPending, po.PrintJobClaimed); err != nil {
		t.Fatalf("设置非法时间失败: %v", err)
	}
	if got := OldestPendingSec(); got != 0 {
		t.Fatalf("非法时间应返回 0, got %d", got)
	}

	if got := shiftSeconds("2026-01-01 00:00:00", 60); got != "2026-01-01 00:01:00" {
		t.Fatalf("shiftSeconds 偏移错误: %q", got)
	}
	if got := shiftSeconds("bad-time", 60); got != "bad-time" {
		t.Fatalf("非法时间应原样返回, got %q", got)
	}
}
