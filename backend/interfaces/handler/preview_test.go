package handler

// ============================================================================
// 票据预览接口测试(PrintPreview)
//
// print 包的 preview_test.go 已覆盖渲染同源;这里只验证 handler 层的参数解析、
// 日志查找与响应结构(printId 入口 → 查日志 → 调 PreviewTicket → 组装响应)。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/print"
	"dining-system/infrastructure/store"
)

// previewGet 构造一个 GET 请求并执行 PrintPreview,返回响应记录器。
func previewGet(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	PrintPreview(c)
	return w
}

func previewJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// insertPreviewFixture 插入一套可预览的数据(菜品/订单/明细/打印机),返回 (orderID, printerID)。
func insertPreviewFixture(t *testing.T) (int, int) {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_dish(dish_id, category_id, dish_name, status, del_flag, create_time, update_time)
		VALUES(9101, 1, '凉拌青瓜', 1, '0', '2026-09-21 10:00:00', '2026-09-21 10:00:00')`); err != nil {
		t.Fatalf("插入菜品失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, create_time, update_time)
		VALUES('D20260922PREVIEW01', 1, '03', '大厅03桌', 2, 1, 2800, 0, 0, 2800, 0, '2026-09-22 12:00:00', '2026-09-22 12:00:00')`); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var orderID int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no='D20260922PREVIEW01'`).Scan(&orderID); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
		VALUES(?, 9101, '凉拌青瓜', 0, '', 2800, 1, 2800, '')`, orderID); err != nil {
		t.Fatalf("插入明细失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?, ?, 'tcp', '192.168.1.100', 9100, '', 48, 1, '', 1, '0', '2026-09-22 10:00:00', '2026-09-22 10:00:00')`,
		"预览机", po.PrinterTypeGuest); err != nil {
		t.Fatalf("插入打印机失败: %v", err)
	}
	var printerID int
	if err := store.DB.QueryRow(`SELECT printer_id FROM tb_printer WHERE printer_name='预览机'`).Scan(&printerID); err != nil {
		t.Fatalf("查询打印机失败: %v", err)
	}
	return orderID, printerID
}

// insertPreviewLog 插一条打印日志并返回 printID。
func insertPreviewLog(t *testing.T, orderID, printerID int) int {
	t.Helper()
	logID, err := service.InsertPrintLogReturningID(po.PrintLog{
		OrderID:     orderID,
		OrderNo:     "D20260922PREVIEW01",
		TableNo:     "03",
		TableName:   "大厅03桌",
		PrinterID:   printerID,
		PrinterName: "预览机",
		PrinterType: po.PrinterTypeGuest,
		Provider:    "tcp",
		DocType:     po.PrintDocGuest,
		Copies:      1,
		Status:      po.PrintStatusSuccess,
		TriggerBy:   po.PrintTriggerOrder,
		CreateTime:  store.Now(),
	})
	if err != nil {
		t.Fatalf("插入打印日志失败: %v", err)
	}
	return logID
}

func TestPrintPreviewHandlerContract(t *testing.T) {
	initAgentTestDB(t)
	orderID, printerID := insertPreviewFixture(t)
	logID := insertPreviewLog(t, orderID, printerID)

	// 1) 缺少 printId:参数错误。
	w := previewGet(t, "/api/admin/print/preview")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "参数错误") {
		t.Fatalf("缺少 printId 应返回 400, got %d(%s)", w.Code, w.Body.String())
	}

	// 2) printId 不存在。
	w2 := previewGet(t, "/api/admin/print/preview?printId=99999")
	if w2.Code != http.StatusBadRequest || !strings.Contains(w2.Body.String(), "打印记录不存在") {
		t.Fatalf("不存在的日志应返回 400+不存在提示, got %d(%s)", w2.Code, w2.Body.String())
	}

	// 3) 正常路径:返回文本行 + 行宽 + 元信息。
	w3 := previewGet(t, "/api/admin/print/preview?printId="+strconv.Itoa(logID))
	if w3.Code != http.StatusOK {
		t.Fatalf("正常预览应返回 200, got %d(%s)", w3.Code, w3.Body.String())
	}
	data := dataOf(t, previewJSON(t, w3))
	if data["lineWidth"] != float64(48) {
		t.Fatalf("80mm 纸 lineWidth 应为 48, got %v", data["lineWidth"])
	}
	// chunks:常规单据 1 段;每行是 {text, bold} 对象,与实际入队的任务段一一对应。
	chunks, ok := data["chunks"].([]interface{})
	if !ok || len(chunks) == 0 {
		t.Fatalf("chunks 应为非空数组, got %v", data["chunks"])
	}
	joined, boldLines := "", map[string]bool{}
	for _, c := range chunks {
		lines, ok := c.([]interface{})
		if !ok || len(lines) == 0 {
			t.Fatalf("每段应为非空行数组, got %v", c)
		}
		for _, l := range lines {
			m := l.(map[string]interface{})
			joined += m["text"].(string) + "\n"
			if b, ok := m["bold"].(bool); ok && b {
				// 行文本带 pad 尾随空格,按 trim 后的 key 收集便于断言。
				boldLines[strings.TrimSpace(m["text"].(string))] = true
			}
		}
	}
	if !strings.Contains(joined, "【食客小票】") || !strings.Contains(joined, "凉拌青瓜") {
		t.Fatalf("预览内容缺少关键行:\n%s", joined)
	}
	// 加粗标记:标题行与合计行应标记 bold,普通菜名行不应。
	if !boldLines["【食客小票】"] {
		t.Fatalf("标题行应加粗:\n%s", joined)
	}
	if !anyStartsWith(boldLines, "合计") {
		t.Fatalf("合计行应加粗:\n%s", joined)
	}
	if anyStartsWith(boldLines, "凉拌青瓜") {
		t.Fatalf("普通菜名行不应加粗:\n%s", joined)
	}
	if data["printerName"] != "预览机" || data["docType"] != po.PrintDocGuest {
		t.Fatalf("预览元信息错误: %v", data)
	}
}

// anyStartsWith 报告集合中是否有以 prefix 开头的行。
func anyStartsWith(set map[string]bool, prefix string) bool {
	for k := range set {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// TestToPreviewLinesControlRows 放大行(夹单桌号)是控制行,预览层必须消费并剥掉前缀:
// 带 big 标记、文本干净;拆单序号标题仍保持 bold 标记。
func TestToPreviewLinesControlRows(t *testing.T) {
	initAgentTestDB(t)

	o := po.Order{OrderNo: "D20260922PREVIEW01", TableNo: "03", TableName: "大厅03桌",
		PersonCount: 2, CreateTime: "2026-09-22 12:00:00"}
	kitchen := print.RenderKitchenTicket(o,
		[]po.OrderItem{{DishName: "凉拌青瓜", Quantity: 1, Price: 2800, Amount: 2800}}, 48,
		print.KitchenTicketOptions{Title: "【厨房单】"})
	bigRaw := ""
	for _, l := range kitchen {
		if _, ok := print.SplitBigLine(l); ok {
			bigRaw = l
		}
	}
	if bigRaw == "" {
		t.Fatalf("厨房单应含放大桌号控制行:\n%s", strings.Join(kitchen, "\n"))
	}

	lines := toPreviewLines([]string{bigRaw, "【厨房单】1/2"})
	if len(lines) != 2 {
		t.Fatalf("应产出 2 行, got %d", len(lines))
	}
	if !lines[0].Big || lines[0].Text != "桌号 03" {
		t.Fatalf("放大行应带 big 标记且文本干净, got %+v", lines[0])
	}
	if strings.ContainsRune(lines[0].Text, '\x02') {
		t.Fatalf("前端不应看到控制前缀, got %q", lines[0].Text)
	}
	if !lines[1].Bold {
		t.Fatalf("带序号的标题行应仍标记 bold, got %+v", lines[1])
	}
}

// TestPrintPreviewSampleHandler 示例模板预览:请求体的覆盖参数必须透传到渲染
// (表单里未保存的值也能预览到)。
func TestPrintPreviewSampleHandler(t *testing.T) {
	initAgentTestDB(t)

	// 用与 callAgent 相同的 POST 方式调用(该接口本身就是 POST + JSON body)。
	code, resp := callAgent(t, PrintPreviewSample, "", `{"docType":"guest","paperWidth":48,"footer":"未保存的口号","showDiscount":false}`)
	if code != http.StatusOK {
		t.Fatalf("示例预览应返回 200, got %d(%v)", code, resp)
	}
	data := dataOf(t, resp)
	if data["lineWidth"] != float64(48) {
		t.Fatalf("lineWidth 应为 48, got %v", data["lineWidth"])
	}
	chunks, ok := data["chunks"].([]interface{})
	if !ok || len(chunks) != 1 {
		t.Fatalf("chunks 应为 1 段, got %v", data["chunks"])
	}
	lines := chunks[0].([]interface{})
	joined := ""
	for _, l := range lines {
		joined += l.(map[string]interface{})["text"].(string) + "\n"
	}
	if !strings.Contains(joined, "未保存的口号") {
		t.Fatalf("覆盖页脚应出现在示例预览中:\n%s", joined)
	}
	if strings.Contains(joined, "优惠") {
		t.Fatalf("showDiscount=false 应隐藏优惠行:\n%s", joined)
	}
	// 金额区:菜品金额不单列,茶位费显示(示例订单有人头费)。
	if strings.Contains(joined, "菜品金额") {
		t.Fatalf("金额区不应单列菜品金额:\n%s", joined)
	}
	if !strings.Contains(joined, "茶位费") {
		t.Fatalf("有茶位费时应显示该行:\n%s", joined)
	}
	if !strings.Contains(joined, "【食客小票】") {
		t.Fatalf("示例小票缺少标题:\n%s", joined)
	}
}
