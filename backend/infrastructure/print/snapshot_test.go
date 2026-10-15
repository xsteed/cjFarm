package print

// ============================================================================
// 票面快照测试
//
// 断言的核心是「存下来的就是打出来的」:
//   - 每次发送都把这一张纸的文本行随台账存下;
//   - 订单/配置后来变了,同一条记录的预览与补打仍然回放当时那张;
//   - 升级前没有快照的老记录回落到重算,并由 fromSnapshot 显式区分。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// latestPrintLog 读回最新一条打印台账(含票面快照)。
func latestPrintLog(t *testing.T) po.PrintLog {
	t.Helper()
	var id int
	if err := store.DB.QueryRow(`SELECT print_id FROM tb_print_log ORDER BY print_id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("读取最新打印台账失败: %v", err)
	}
	entry, err := service.LoadPrintLog(id)
	if err != nil {
		t.Fatalf("读取打印台账失败: %v", err)
	}
	return entry
}

// insertLegacyPrintLog 插一条「升级前」的台账行:没有票面快照,只有订单与打印机信息。
func insertLegacyPrintLog(t *testing.T, orderID, printerID int, paperWidth int) po.PrintLog {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_print_log(order_id, order_no, table_no, table_name,
		printer_id, printer_name, printer_type, provider, doc_type, copies, status, detail,
		content, paper_width, trigger_by, operator, cost_ms, create_time)
		VALUES(?, 'D20260921120000abcdef', '03', '大厅03桌', ?, '老台账机', 2, 'tcp', 'guest', 1, 1, '',
		        NULL, ?, 'order', 'legacy', 12, '2026-09-21 12:00:00')`,
		orderID, printerID, paperWidth); err != nil {
		t.Fatalf("插入历史台账失败: %v", err)
	}
	return latestPrintLog(t)
}

// TestRecordLogStoresTicketSnapshot 每次发送都要把这一张纸的完整文本行与当时纸宽存进台账。
func TestRecordLogStoresTicketSnapshot(t *testing.T) {
	setupTestDB(t)
	_, contents := feiePrintStub(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 32, po.PrinterProviderFeie) // 58mm

	if err := Reprint(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder, "tester"); err != nil {
		t.Fatalf("补打失败: %v", err)
	}

	entry := latestPrintLog(t)
	if entry.Content == "" {
		t.Fatal("台账必须存下票面快照")
	}
	if entry.PaperWidth != 32 {
		t.Fatalf("纸宽应随快照一起存(58mm=32), got %d", entry.PaperWidth)
	}
	snap, ok := DecodeTicketContent(entry.Content)
	if !ok || len(snap.Lines) == 0 {
		t.Fatalf("快照无法解析: %q", entry.Content)
	}
	// 快照内容必须与真正发出去的一致(飞鹅通道把行拼成 <BR> 分隔的 content)。
	sent := strings.Join(snap.Lines, "\n")
	if !strings.Contains(sent, "凉拌青瓜") || !strings.Contains(sent, "海鲜炒饭") {
		t.Fatalf("快照缺少票面内容:\n%s", sent)
	}
	if len(*contents) != 1 || !strings.Contains((*contents)[0], "凉拌青瓜") {
		t.Fatalf("实际推送内容异常: %#v", *contents)
	}
}

// TestPreviewAndReprintLogReplaySnapshot 订单后来变了,同一条记录的预览与补打仍回放当时那张票。
func TestPreviewAndReprintLogReplaySnapshot(t *testing.T) {
	setupTestDB(t)
	_, contents := feiePrintStub(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderFeie)

	if err := Reprint(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder, "tester"); err != nil {
		t.Fatalf("首次打印失败: %v", err)
	}
	entry := latestPrintLog(t)

	// 订单后来加了一道菜:按当前数据重算出来的票会多出这一行。
	const laterDish = "超长测试菜品加一个很长的菜名用于撑长票据"
	appendDishes(t, orderID, 1)

	res, err := PreviewLog(entry)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !res.FromSnapshot {
		t.Fatal("这条记录有快照,预览必须回放而不是重算")
	}
	if got := flattenPreview(res.Chunks); strings.Contains(got, laterDish) {
		t.Fatalf("预览不该出现后来加的菜:\n%s", got)
	}
	if res.LineWidth != 48 {
		t.Fatalf("行宽应取快照里的纸宽(80mm=48), got %d", res.LineWidth)
	}

	// 补打:发出的内容必须与预览同一份,同样不该多出后加的菜。
	*contents = nil
	if err := ReprintLog(entry, "tester"); err != nil {
		t.Fatalf("按台账补打失败: %v", err)
	}
	if len(*contents) != 1 {
		t.Fatalf("应推送 1 次, got %d", len(*contents))
	}
	if strings.Contains((*contents)[0], laterDish) {
		t.Fatalf("补打发出的是后来重算的票,而不是当时那张:\n%s", (*contents)[0])
	}
	if !strings.Contains((*contents)[0], "凉拌青瓜") {
		t.Fatalf("补打内容缺少原票据内容:\n%s", (*contents)[0])
	}
}

// TestReprintLogKeepsSnapshotPaperWidth 打印机后来改了纸宽,补打的台账仍记当时的纸宽。
func TestReprintLogKeepsSnapshotPaperWidth(t *testing.T) {
	setupTestDB(t)
	feiePrintStub(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 32, po.PrinterProviderFeie)

	if err := Reprint(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder, "tester"); err != nil {
		t.Fatalf("首次打印失败: %v", err)
	}
	entry := latestPrintLog(t)

	// 同一台机器改成 80mm 纸。
	if _, err := store.DB.Exec(`UPDATE tb_printer SET paper_width=48 WHERE printer_id=?`, printerID); err != nil {
		t.Fatalf("改纸宽失败: %v", err)
	}
	if err := ReprintLog(entry, "tester"); err != nil {
		t.Fatalf("按台账补打失败: %v", err)
	}

	after := latestPrintLog(t)
	if after.PaperWidth != 32 {
		t.Fatalf("补打台账应继续记当时的纸宽 32, got %d", after.PaperWidth)
	}
	// 行本身也是当时按 58mm pad 出来的,不能因为现在换成 80mm 就让行宽对不上。
	if w := LineWidth(after.PaperWidth); w != 32 {
		t.Fatalf("补打台账的行宽应为 32, got %d", w)
	}
}

// TestPreviewLogFallsBackForLegacyRecord 升级前的老记录没有快照:回落重算,并显式标注。
func TestPreviewLogFallsBackForLegacyRecord(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)
	entry := insertLegacyPrintLog(t, orderID, printerID, 48)

	res, err := PreviewLog(entry)
	if err != nil {
		t.Fatalf("老台账预览失败: %v", err)
	}
	if res.FromSnapshot {
		t.Fatal("没有快照的老记录不能声称来自快照")
	}
	if len(res.Chunks) == 0 {
		t.Fatal("老记录应回落到按当前数据重渲染")
	}
	if got := flattenPreview(res.Chunks); !strings.Contains(got, "凉拌青瓜") {
		t.Fatalf("重算结果缺少票面内容:\n%s", got)
	}
}

// TestReprintLogFallsBackForLegacyRecord 没有快照时,补打回落到按当前数据重渲染(历史行为)。
func TestReprintLogFallsBackForLegacyRecord(t *testing.T) {
	setupTestDB(t)
	_, contents := feiePrintStub(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderFeie)
	if _, err := store.DB.Exec(`UPDATE tb_printer SET status=1, del_flag='0' WHERE printer_id=?`, printerID); err != nil {
		t.Fatal(err)
	}
	entry := insertLegacyPrintLog(t, orderID, printerID, 48)

	if err := ReprintLog(entry, "tester"); err != nil {
		t.Fatalf("老台账补打失败: %v", err)
	}
	if len(*contents) != 1 || !strings.Contains((*contents)[0], "凉拌青瓜") {
		t.Fatalf("回落到重算时应发出当前渲染结果, got %#v", *contents)
	}
}

// TestDecodeTicketContent 只有「非空且含文本行」才认作快照,其余一律回落重算。
func TestDecodeTicketContent(t *testing.T) {
	if _, ok := DecodeTicketContent(""); ok {
		t.Fatal("空内容不应被当作快照")
	}
	for _, bad := range []string{"不是 JSON", "{}", `{"lines":[]}`, "[]"} {
		if _, ok := DecodeTicketContent(bad); ok {
			t.Fatalf("非法/空快照不应被接受: %q", bad)
		}
	}
	// 控制标记(放大行/二维码行)必须原样往返:发送层要靠它决定怎么出纸。
	in := TicketContent{Label: "【厨房单】2/5", Lines: []string{bigMarkLine("桌号 03"), "1. 凉拌青瓜 (份) x2", ""}}
	out, ok := DecodeTicketContent(EncodeTicketContent(in))
	if !ok {
		t.Fatal("合法快照应能解析")
	}
	if out.Label != in.Label || len(out.Lines) != len(in.Lines) {
		t.Fatalf("快照往返不一致: %+v", out)
	}
	for i := range in.Lines {
		if out.Lines[i] != in.Lines[i] {
			t.Fatalf("第 %d 行往返不一致: %q != %q", i, out.Lines[i], in.Lines[i])
		}
	}
}
