package print

// ============================================================================
// 台账票据标识(label 列)
//
// 「一菜一单」下一次下单会写多条台账,记录页上必须一眼看出哪条是哪一张纸。
// 标识原本拼在 detail 前面,而代理通道的台账要等回执才定稿 —— 回执、重试、
// 过期作废都会整段改写 detail(见 dao.UpdatePrintLogResult),拼进去的标识
// 一到那一步就没了。本文件锁住「标识独立成列、任何改写都不丢」。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// agentLogLabels 按入队顺序读回打印台账的主键与其票据标识。
func agentLogLabels(t *testing.T) (ids []int, labels []string) {
	t.Helper()
	rows, err := store.DB.Query(`SELECT print_id, COALESCE(label,'') FROM tb_print_log ORDER BY print_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids, labels
}

// TestAgentLogKeepsTicketLabelAcrossAck 代理通道的标识必须在「回执整段改写 detail」之后仍然在。
func TestAgentLogKeepsTicketLabelAcrossAck(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 5, PrinterName: "代理机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderAgent, IP: "192.168.1.60", Port: 9100, PaperWidth: 48,
	}}
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("一菜一单应生成 2 张, got %d", len(jobs))
	}
	if err := dispatchJobs(jobs); err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	ids, labels := agentLogLabels(t)
	if len(ids) != 2 {
		t.Fatalf("两张票应各记一条台账, got %d", len(ids))
	}
	if labels[0] != "【厨房单】1/2" || labels[1] != "【厨房单】2/2" {
		t.Fatalf("台账标识应区分是第几张, got %q / %q", labels[0], labels[1])
	}

	// 代理回执:整段改写 detail 与状态。
	if err := service.UpdatePrintLogResult(ids[0], po.PrintStatusSuccess, "代理已送出", 123); err != nil {
		t.Fatalf("回写回执失败: %v", err)
	}
	if _, after := agentLogLabels(t); after[0] != "【厨房单】1/2" {
		t.Fatalf("回执改写 detail 后票据标识不能丢, got %q", after[0])
	}
	var detail string
	if err := store.DB.QueryRow(`SELECT detail FROM tb_print_log WHERE print_id=?`, ids[0]).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if detail != "代理已送出" {
		t.Fatalf("detail 应被回执整段改写, got %q", detail)
	}
}

// TestPrintLogLabelNotInDetail 标识不再拼进 detail:两类信息各占各的字段,
// 否则 detail 一被改写标识就丢,而标识一被解析又要靠字符串猜格式。
func TestPrintLogLabelNotInDetail(t *testing.T) {
	setupTestDB(t)
	_, contents := feiePrintStub(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderFeie)

	if err := Reprint(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder, "tester"); err != nil {
		t.Fatalf("补打失败: %v", err)
	}
	if len(*contents) != 1 {
		t.Fatalf("应推送 1 次, got %d", len(*contents))
	}

	entry := latestPrintLog(t)
	if entry.Label != "顾客小票" {
		t.Fatalf("非拆单票据的标识应为票种名, got %q", entry.Label)
	}
	if strings.Contains(entry.Detail, entry.Label) {
		t.Fatalf("detail 不应再重复票据标识, got %q", entry.Detail)
	}
}
