package print

// ============================================================================
// 票据预览测试
//
// 核心断言是「预览即所得」:PreviewTicket 的输出必须与实际出纸使用的渲染函数
// (RenderGuestTicket / RenderKitchenTicket,经 job.render 分发)在相同输入下
// 完全一致 —— 否则前端预览会与 9100 出现版式漂移,预览就失去意义。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/service"
	"dining-system/internal/store"
)

// insertPreviewOrder 插入一条订单 + 两道菜(分属分类 1 / 6)并返回 orderID。
func insertPreviewOrder(t *testing.T) int {
	t.Helper()
	// dish_id 用高段值,避开 seed.go 初始化的示例菜品。
	if _, err := store.DB.Exec(`INSERT INTO tb_dish(dish_id, category_id, dish_name, status, del_flag, create_time, update_time)
		VALUES(9001, 1, '凉拌青瓜', 1, '0', '2026-09-21 10:00:00', '2026-09-21 10:00:00'),
		      (9002, 6, '海鲜炒饭', 1, '0', '2026-09-21 10:00:00', '2026-09-21 10:00:00')`); err != nil {
		t.Fatalf("插入菜品失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, order_remark, create_time, update_time)
		VALUES('D20260921120000abcdef', 1, '03', '大厅03桌', 4, 1, 6600, 2400, 600, 8400, 0, '不要香菜', '2026-09-21 12:00:00', '2026-09-21 12:00:00')`); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var orderID int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no='D20260921120000abcdef'`).Scan(&orderID); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
		VALUES(?, 9001, '凉拌青瓜', 1, '份', 2800, 2, 5600, '加辣'),
		      (?, 9002, '海鲜炒饭', 0, '', 1000, 1, 1000, '')`, orderID, orderID); err != nil {
		t.Fatalf("插入订单明细失败: %v", err)
	}
	return orderID
}

// insertPreviewPrinter 插入一台打印机并返回 printerID。
func insertPreviewPrinter(t *testing.T, printerType int, categoryIDs string, paperWidth int) int {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?, ?, 'tcp', '192.168.1.100', 9100, '', ?, 1, ?, 1, '0', '2026-09-21 10:00:00', '2026-09-21 10:00:00')`,
		"预览测试机", printerType, paperWidth, categoryIDs); err != nil {
		t.Fatalf("插入打印机失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT printer_id FROM tb_printer WHERE printer_name='预览测试机'`).Scan(&id); err != nil {
		t.Fatalf("查询打印机失败: %v", err)
	}
	return id
}

// previewOrderItems 按预览订单读回带分类的明细(与 PreviewTicket 内部同一条读取路径)。
func previewOrderItems(t *testing.T, orderID int) (po.Order, []po.OrderItem) {
	t.Helper()
	o, rows, err := service.LoadOrderAndItemsForPrint(orderID)
	if err != nil {
		t.Fatalf("读取订单失败: %v", err)
	}
	items := make([]po.OrderItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, r.OrderItem)
	}
	return o, items
}

// flattenPreview 把分段展平成单一文本(断言「分段拼接 == 原渲染」用)。
func flattenPreview(chunks [][]string) string {
	var sb strings.Builder
	for i, c := range chunks {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Join(c, "\n"))
	}
	return sb.String()
}

// TestPreviewTicketMatchesGuestRender 预览输出必须与出纸渲染函数完全一致(预览即所得)。
func TestPreviewTicketMatchesGuestRender(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48) // 80mm

	chunks, lineWidth, err := PreviewTicket(printerID, orderID, po.PrintDocGuest)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	if lineWidth != 48 {
		t.Fatalf("80mm 纸行宽应为 48, got %d", lineWidth)
	}
	// 常规单据只有一段。
	if len(chunks) != 1 {
		t.Fatalf("常规单据应只有 1 段, got %d", len(chunks))
	}

	// 同源断言:分段拼接后与直接调用 RenderGuestTicket(同一订单数据、同一纸宽)逐行一致。
	o, items := previewOrderItems(t, orderID)
	want := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	if flattenPreview(chunks) != want {
		t.Fatalf("预览与出纸渲染不一致:\n预览:\n%s\n出纸:\n%s", flattenPreview(chunks), want)
	}
	for _, c := range chunks {
		for _, l := range c {
			if displayWidth(l) > 48 {
				t.Fatalf("行超宽(%d): %q", displayWidth(l), l)
			}
		}
	}
}

// TestPreviewTicketSplitsLongTicket 超长票据按实际入队规则拆段:
// 预览的段数与每段内容必须与「打印时拆出的任务段」一一对应,否则长单预览失真。
func TestPreviewTicketSplitsLongTicket(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	// 追加 120 道菜,把票据推过单段上限(约 75 行 / 3600 字符),触发拆段。
	for i := 0; i < 120; i++ {
		if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
			VALUES(?, 9002, '超长测试菜品加一个很长的菜名用于撑长票据', 0, '', 1000, 1, 1000, '')`, orderID); err != nil {
			t.Fatalf("追加明细失败: %v", err)
		}
	}
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuest)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("超长票据应拆成多段, got %d 段", len(chunks))
	}
	// 每段(除末段)都应贴近上限:拆段规则与实际入队(splitJobPayload)同一条代码路径。
	for i, c := range chunks {
		joined := strings.Join(c, "\n")
		if i < len(chunks)-1 && len(joined)+len(chunks[i+1][0]) <= jobPayloadMaxChars {
			t.Fatalf("第 %d 段过早结束(%d 字符),拆段与入队规则不一致", i+1, len(joined))
		}
		if len(joined) > jobPayloadMaxChars+len(c[len(c)-1]) {
			t.Fatalf("第 %d 段超长(%d)", i+1, len(joined))
		}
	}
}

// TestPreviewTicketKitchenLineWidth58 58mm 纸的行宽映射与厨房单内容。
func TestPreviewTicketKitchenLineWidth58(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 32) // 58mm

	chunks, lineWidth, err := PreviewTicket(printerID, orderID, po.PrintDocKitchen)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	if lineWidth != 32 {
		t.Fatalf("58mm 纸行宽应为 32, got %d", lineWidth)
	}
	joined := flattenPreview(chunks)
	if !strings.Contains(joined, "【厨房单】") || !strings.Contains(joined, "凉拌青瓜") {
		t.Fatalf("厨房单缺少关键内容:\n%s", joined)
	}
	for _, c := range chunks {
		for _, l := range c {
			if displayWidth(l) > 32 {
				t.Fatalf("行超宽(%d): %q", displayWidth(l), l)
			}
		}
	}
}

// TestPreviewTicketCategoryScopeError 分类分单过滤后无菜可打时报可理解的错误。
func TestPreviewTicketCategoryScopeError(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	// 只授权分类 99(订单里没有):itemsForPrinter 过滤后为空。
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "99", 48)

	if _, _, err := PreviewTicket(printerID, orderID, po.PrintDocKitchen); err == nil {
		t.Fatal("授权分类外无菜可打时应报错")
	} else if !strings.Contains(err.Error(), "分类") {
		t.Fatalf("错误信息应指明分类原因, got %q", err.Error())
	}
}

// TestPreviewTicketOrderNotFound 订单/打印机不存在时报可理解的错误。
func TestPreviewTicketOrderNotFound(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	if _, _, err := PreviewTicket(printerID, 99999, po.PrintDocGuest); err == nil || !strings.Contains(err.Error(), "订单不存在") {
		t.Fatalf("订单不存在应报错, got %v", err)
	}
	if _, _, err := PreviewTicket(99999, orderID, po.PrintDocGuest); err == nil || !strings.Contains(err.Error(), "打印机不存在") {
		t.Fatalf("打印机不存在应报错, got %v", err)
	}
}
