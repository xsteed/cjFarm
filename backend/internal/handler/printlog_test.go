package handler

// ============================================================================
// 打印日志与补打 HTTP 层单元测试(printlog.go)
//
// 补打成功路径统一使用 provider=agent 的打印机(入队不连网),覆盖打印日志列表筛选、
// 按日志补打、按订单补打以及相关纯函数。
// ============================================================================

import (
	"net/http"
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// plInsertOrderFixture 种一条带明细的订单,返回订单 ID。
func plInsertOrderFixture(t *testing.T, orderNo string) int {
	t.Helper()
	id := insertFlowOrder(t, orderNo, 1, 0)
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
		VALUES(?,0,'测试菜',0,'',0,1,0,'')`, id); err != nil {
		t.Fatalf("插入订单明细失败: %v", err)
	}
	return id
}

// plInsertAgentPrinter 种一台启用的代理打印机并返回主键。
func plInsertAgentPrinter(t *testing.T, printerType int) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port, feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"代理补打机", printerType, po.PrinterProviderAgent, "192.168.1.9", 9100, "", 48, 1, "", 1, "0", store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入代理打印机失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// plInsertPrintLog 插入一条打印日志并返回主键。
func plInsertPrintLog(t *testing.T, orderID, printerID int, docType string) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_print_log(order_id, order_no, table_no, table_name, printer_id, printer_name, printer_type, provider, doc_type, copies, status, remote_id, detail, trigger_by, operator, cost_ms, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, "ORDPL1", "T1", "测试桌", printerID, "代理补打机", po.PrinterTypeGuest, po.PrinterProviderAgent, docType, 1, po.PrintStatusSuccess, "", "", po.PrintTriggerReprint, "", 0, store.Now())
	if err != nil {
		t.Fatalf("插入打印日志失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestPrintLogList(t *testing.T) {
	initAgentTestDB(t)
	orderID := plInsertOrderFixture(t, "ORDPL1")
	printerID := plInsertAgentPrinter(t, po.PrinterTypeGuest)
	plInsertPrintLog(t, orderID, printerID, po.PrintDocGuest)

	code, res := prtCall(t, PrintLogList, http.MethodGet, "/test?status=1&docType=guest&provider=agent&printerId="+itoa(printerID)+"&orderNo=ORDPL", "", nil)
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("打印日志列表应返回 200, got %d(%v)", code, res)
	}
	data := res["data"].(map[string]interface{})
	if data["total"].(float64) < 1 {
		t.Fatalf("打印日志列表应命中记录, got %v", data["total"])
	}
}

func TestPrintLogReprintBranches(t *testing.T) {
	initAgentTestDB(t)

	if _, res := prtCall(t, PrintLogReprint, http.MethodPost, "/test", `{}`, nil); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if _, res := prtCall(t, PrintLogReprint, http.MethodPost, "/test", `{"printId":99999}`, nil); res["code"] != float64(400) || res["msg"] != "打印记录不存在" {
		t.Fatalf("不存在的日志应返回打印记录不存在, got %v", res)
	}

	// 非订单单据(测试打印)不能补打。
	testLogID := plInsertPrintLog(t, 0, 0, "test")
	if _, res := prtCall(t, PrintLogReprint, http.MethodPost, "/test", `{"printId":`+itoa(testLogID)+`}`, nil); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "无法重打") {
		t.Fatalf("非订单单据应无法重打, got %v", res)
	}

	// 正常补打(代理打印机,入队不连网)。
	orderID := plInsertOrderFixture(t, "ORDPL2")
	printerID := plInsertAgentPrinter(t, po.PrinterTypeGuest)
	logID := plInsertPrintLog(t, orderID, printerID, po.PrintDocGuest)
	code, res := prtCall(t, PrintLogReprint, http.MethodPost, "/test", `{"printId":`+itoa(logID)+`}`, nil)
	if code != http.StatusOK || !strings.Contains(res["msg"].(string), "已重新发送到") {
		t.Fatalf("补打应成功, got %d(%v)", code, res)
	}
}

func TestOrderReprintBranches(t *testing.T) {
	initAgentTestDB(t)

	if _, res := prtCall(t, OrderReprint, http.MethodPost, "/test", `{}`, nil); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}

	// 无启用打印机:按单据类型提示。
	orderID := plInsertOrderFixture(t, "ORDPL3")
	if _, res := prtCall(t, OrderReprint, http.MethodPost, "/test", `{"orderId":`+itoa(orderID)+`,"docType":"guest"}`, nil); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "食客小票") {
		t.Fatalf("无启用小票机应提示, got %v", res)
	}
	if _, res := prtCall(t, OrderReprint, http.MethodPost, "/test", `{"orderId":`+itoa(orderID)+`,"docType":"kitchen"}`, nil); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "厨房单") {
		t.Fatalf("无启用厨房机应提示, got %v", res)
	}

	// 指定代理打印机补打成功。
	printerID := plInsertAgentPrinter(t, po.PrinterTypeGuest)
	code, res := prtCall(t, OrderReprint, http.MethodPost, "/test", `{"orderId":`+itoa(orderID)+`,"printerId":`+itoa(printerID)+`,"docType":"guest"}`, nil)
	if code != http.StatusOK || res["msg"] != "补打指令已发送" {
		t.Fatalf("按订单补打应成功, got %d(%v)", code, res)
	}
}

func TestPrinterTypeLabel(t *testing.T) {
	if got := printerTypeLabel(po.PrinterTypeGuest); got != "食客小票" {
		t.Fatalf("guest 标签错误: %q", got)
	}
	if got := printerTypeLabel(po.PrinterTypeKitchen); got != "厨房单" {
		t.Fatalf("kitchen 标签错误: %q", got)
	}
}

func TestToPreviewLines(t *testing.T) {
	lines := toPreviewLines([]string{"标题", "", "普通菜"})
	if len(lines) != 3 {
		t.Fatalf("预览行数量错误: %d", len(lines))
	}
	if lines[0].Text != "标题" || lines[1].Text != "" || lines[2].Text != "普通菜" {
		t.Fatalf("预览行文本错误: %+v", lines)
	}
}
