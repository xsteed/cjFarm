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

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store"
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

// insertPreviewPrinter 插入一台直连打印机并返回 printerID。
func insertPreviewPrinter(t *testing.T, printerType int, categoryIDs string, paperWidth int) int {
	return insertPreviewPrinterAs(t, printerType, categoryIDs, paperWidth, po.PrinterProviderTCP)
}

// insertPreviewPrinterAs 插入指定通道的打印机并返回 printerID。
// provider 决定票据实际怎么送出去,进而决定预览该怎么分段(见 lineChunksFor)。
func insertPreviewPrinterAs(t *testing.T, printerType int, categoryIDs string, paperWidth int, provider string) int {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?, ?, ?, '192.168.1.100', 9100, 'SAMPLE001', ?, 1, ?, 1, '0', '2026-09-21 10:00:00', '2026-09-21 10:00:00')`,
		"预览测试机", printerType, provider, paperWidth, categoryIDs); err != nil {
		t.Fatalf("插入打印机失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT printer_id FROM tb_printer WHERE printer_name='预览测试机' ORDER BY printer_id DESC`).Scan(&id); err != nil {
		t.Fatalf("查询打印机失败: %v", err)
	}
	return id
}

// appendDishes 往订单里追加 n 道菜(把票据撑过单段上限,用于验证分段)。
func appendDishes(t *testing.T, orderID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
			VALUES(?, 9002, '超长测试菜品加一个很长的菜名用于撑长票据', 0, '', 1000, 1, 1000, '')`, orderID); err != nil {
			t.Fatalf("追加明细失败: %v", err)
		}
	}
}

// previewChunksOf 取一台打印机上某订单的顾客小票预览分段。
func previewChunksOf(t *testing.T, printerID, orderID int) [][]string {
	t.Helper()
	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	return chunks
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
// trigger=order 出点菜单形态,与 RenderGuestOrderTicket 同源比对。
func TestPreviewTicketMatchesGuestRender(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48) // 80mm

	chunks, lineWidth, err := PreviewTicket(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder)
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

	// 同源断言:分段拼接后与直接调用 RenderGuestOrderTicket(同一订单数据、同一纸宽)逐行一致。
	o, items := previewOrderItems(t, orderID)
	want := strings.Join(RenderGuestOrderTicket(o, items, 48), "\n")
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

// TestPreviewTicketSplitByChannel 分段必须随通道而变 —— 「预览即所得」的核心。
//
// 直连一次连接把整张票写完 → 1 段;代理受单条任务字符上限约束 → 多段;
// 飞鹅受单次推送字节上限约束 → 多段,且切点与代理不同(<BR> 与标签会把行撑大)。
// 此前预览统一按代理规则切,三种机器的预览都失真。
func TestPreviewTicketSplitByChannel(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	// 追加 120 道菜,把票据推过各通道的单段上限。
	appendDishes(t, orderID, 120)

	tcp := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderTCP)
	agent := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderAgent)
	feie := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderFeie)

	if got := len(previewChunksOf(t, tcp, orderID)); got != 1 {
		t.Fatalf("直连通道一次写完,预览应只有 1 段, got %d", got)
	}
	agentChunks := previewChunksOf(t, agent, orderID)
	if len(agentChunks) < 2 {
		t.Fatalf("代理通道应拆成多段, got %d", len(agentChunks))
	}
	feieChunks := previewChunksOf(t, feie, orderID)
	if len(feieChunks) < 2 {
		t.Fatalf("飞鹅通道应拆成多段, got %d", len(feieChunks))
	}

	// 段数与逐段内容都必须与实际发送所用函数一致:预览的每一段就是一张纸。
	o, items := previewOrderItems(t, orderID)
	lines := RenderGuestOrderTicket(o, items, 48)
	assertChunksEqualSent(t, "代理", agentChunks, splitJobPayload(lines), "\n")
	assertChunksEqualSent(t, "飞鹅", feieChunks, splitFeieContent(lines), "<BR>")
}

// assertChunksEqualSent 断言预览分段与该通道「实际送出的段」完全相同。
// 实际送出的每行可能带通道标签(飞鹅),故只比对段数与每段行数 ——
// 标签形态本就不该出现在预览里。
func assertChunksEqualSent(t *testing.T, channel string, preview [][]string, actual []string, sep string) {
	t.Helper()
	if len(preview) != len(actual) {
		t.Fatalf("%s 通道预览段数 %d 与实际发送段数 %d 不一致", channel, len(preview), len(actual))
	}
	for i := range preview {
		want := strings.Split(actual[i], sep)
		if len(preview[i]) != len(want) {
			t.Fatalf("%s 通道第 %d 段行数 %d 与实际 %d 不一致", channel, i+1, len(preview[i]), len(want))
		}
	}
}

// TestPreviewTicketAgentSplitKeepsEveryLine 分段不得丢行或重行:
// 展平后的预览必须与直接渲染的结果逐行一致。
func TestPreviewTicketAgentSplitKeepsEveryLine(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	appendDishes(t, orderID, 120)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderAgent)

	chunks := previewChunksOf(t, printerID, orderID)
	o, items := previewOrderItems(t, orderID)
	want := strings.Join(RenderGuestOrderTicket(o, items, 48), "\n")
	if got := flattenPreview(chunks); got != want {
		t.Fatalf("分段后预览与整张渲染不一致:\n预览:\n%s\n渲染:\n%s", got, want)
	}
}

// TestPreviewTicketKitchenLineWidth58 58mm 纸的行宽映射与厨房单内容。
func TestPreviewTicketKitchenLineWidth58(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 32) // 58mm

	chunks, lineWidth, err := PreviewTicket(printerID, orderID, po.PrintDocKitchen, po.PrintTriggerOrder)
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

// TestPreviewTicketKitchenFollowsSplitMode 厨房单预览必须按 print_kitchen_split_mode
// 拆成与出纸同样的几张。
//
// 「一菜一单 / 按份拆」下后厨手里是一叠带序号的夹单,而预览此前恒渲染整单一张:
// 菜品带编号、末尾还有「共 N 道 M 份」—— 与后厨手里的任何一张都对不上,
// 「预览即所得」当场失效。预览的段数必须等于后厨实际收到的纸张数。
func TestPreviewTicketKitchenFollowsSplitMode(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t) // 凉拌青瓜 x2(分类 1) + 海鲜炒饭 x1(分类 6)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 48)

	previewKitchen := func(t *testing.T) [][]string {
		t.Helper()
		chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocKitchen, po.PrintTriggerOrder)
		if err != nil {
			t.Fatalf("PreviewTicket 失败: %v", err)
		}
		return chunks
	}

	t.Run("整单:仍是一张,带份数汇总", func(t *testing.T) {
		setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitWhole)
		chunks := previewKitchen(t)
		if len(chunks) != 1 {
			t.Fatalf("整单档预览应只有 1 段, got %d", len(chunks))
		}
		if got := flattenPreview(chunks); !strings.Contains(got, "共 2 道 3 份") {
			t.Fatalf("整单应带份数汇总:\n%s", got)
		}
	})

	t.Run("一菜一单:2 段,各含一道菜且带序号与放大菜名", func(t *testing.T) {
		setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
		chunks := previewKitchen(t)
		if len(chunks) != 2 {
			t.Fatalf("两道菜应预览出 2 段, got %d", len(chunks))
		}
		first := strings.Join(chunks[0], "\n")
		if !strings.Contains(first, "【厨房单】1/2") {
			t.Fatalf("第 1 段应带序号标题:\n%s", first)
		}
		if !strings.Contains(first, "凉拌青瓜") || strings.Contains(first, "海鲜炒饭") {
			t.Fatalf("第 1 段只应含凉拌青瓜:\n%s", first)
		}
		if !hasBigLine(chunks[0], "凉拌青瓜 (份) x2") {
			t.Fatalf("拆单应放大菜名, got %v:\n%s", bigLinesOf(chunks[0]), first)
		}
		if strings.Contains(first, "共 2 道") {
			t.Fatalf("拆单不应出份数汇总:\n%s", first)
		}
		if second := strings.Join(chunks[1], "\n"); !strings.Contains(second, "【厨房单】2/2") {
			t.Fatalf("第 2 段应带序号标题:\n%s", second)
		}
	})

	t.Run("按份拆:2 份 + 1 份 → 3 段,每张 x1", func(t *testing.T) {
		setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)
		chunks := previewKitchen(t)
		if len(chunks) != 3 {
			t.Fatalf("2 份 + 1 份应预览出 3 段, got %d", len(chunks))
		}
		for i, c := range chunks {
			if got := strings.Join(c, "\n"); !strings.Contains(got, "x1") {
				t.Fatalf("第 %d 段应是单份:\n%s", i+1, got)
			}
		}
	})
}

// TestPreviewTicketCategoryScopeError 分类分单过滤后无菜可打时报可理解的错误。
func TestPreviewTicketCategoryScopeError(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	// 只授权分类 99(订单里没有):itemsForPrinter 过滤后为空。
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "99", 48)

	if _, _, err := PreviewTicket(printerID, orderID, po.PrintDocKitchen, po.PrintTriggerOrder); err == nil {
		t.Fatal("授权分类外无菜可打时应报错")
	} else if !strings.Contains(err.Error(), "分类") {
		t.Fatalf("错误信息应指明分类原因, got %q", err.Error())
	}
}

// TestPreviewTestTicket 测试页预览:不出纸就能看到票面上会写什么,
// 此前只有「测试打印」一条路,每核一次版式就得白扔一张纸。
func TestPreviewTestTicket(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinterAs(t, po.PrinterTypeKitchen, "", 48, po.PrinterProviderAgent)

	chunks, lineWidth, err := PreviewTestTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewTestTicket 失败: %v", err)
	}
	if lineWidth != 48 {
		t.Fatalf("80mm 纸行宽应为 48, got %d", lineWidth)
	}
	joined := flattenPreview(chunks)
	for _, want := range []string{"测试打印", "预览测试机", "192.168.1.100:9100", "厨房单", "80mm"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("测试页预览缺少 %q:\n%s", want, joined)
		}
	}
	if _, _, err := PreviewTestTicket(99999); err == nil {
		t.Fatal("打印机不存在时应报错")
	}
}

// TestPreviewTicketOrderNotFound 订单/打印机不存在时报可理解的错误。
func TestPreviewTicketOrderNotFound(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	if _, _, err := PreviewTicket(printerID, 99999, po.PrintDocGuest, po.PrintTriggerOrder); err == nil || !strings.Contains(err.Error(), "订单不存在") {
		t.Fatalf("订单不存在应报错, got %v", err)
	}
	if _, _, err := PreviewTicket(99999, orderID, po.PrintDocGuest, po.PrintTriggerOrder); err == nil || !strings.Contains(err.Error(), "打印机不存在") {
		t.Fatalf("打印机不存在应报错, got %v", err)
	}
}
