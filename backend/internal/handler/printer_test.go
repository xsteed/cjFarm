package handler

// ============================================================================
// 打印机 HTTP 层单元测试(printer.go)
//
// 网络相关的直连/飞鹅路径全部避开:统一用 provider=agent 的打印机验证成功路径
// (入队不连网),直连/飞鹅路径只测参数校验与未配置账号时的失败分支。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// prtCall 以指定 method/path/body/路径参数调用 handler,返回 HTTP 状态码与解析后的响应体。
func prtCall(t *testing.T, h gin.HandlerFunc, method, path, body string, params map[string]string) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	for k, v := range params {
		c.Params = append(c.Params, gin.Param{Key: k, Value: v})
	}
	h(c)
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return w.Code, out
}

// prtSeedPrinter 插入一台打印机并返回主键。
func prtSeedPrinter(t *testing.T, provider string, printerType int, name, ip, sn string, status int) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port, feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		name, printerType, provider, ip, 9100, sn, 48, 1, "", status, "0", store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入打印机失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestNormalizePrinter(t *testing.T) {
	p := dto.Printer{PrinterType: 99, Provider: "bad", Port: 0, PaperWidth: 20, Copies: 0, Status: 5}
	normalizePrinter(&p)
	if p.PrinterType != po.PrinterTypeKitchen || p.Provider != po.PrinterProviderTCP || p.Port != 9100 ||
		p.PaperWidth != 48 || p.Copies != 1 || p.Status != 1 {
		t.Fatalf("脏字段归一化失败: %+v", p)
	}

	// 份数超上限收敛到 5。
	p = dto.Printer{Copies: 9}
	normalizePrinter(&p)
	if p.Copies != 5 {
		t.Fatalf("份数应收敛到 5, got %d", p.Copies)
	}

	// 食客小票不保留分类分单。
	p = dto.Printer{PrinterType: po.PrinterTypeGuest, CategoryIDList: []int{1, 2}, CategoryIDs: "1,2"}
	normalizePrinter(&p)
	if p.CategoryIDs != "" || len(p.CategoryIDList) != 0 {
		t.Fatalf("食客小票应清空分类分单, got %+v", p)
	}

	// 厨房单按分类 ID 列表生成 CSV。
	p = dto.Printer{PrinterType: po.PrinterTypeKitchen, CategoryIDList: []int{3, 4}}
	normalizePrinter(&p)
	if p.CategoryIDs != "3,4" {
		t.Fatalf("厨房单分类 CSV 错误, got %q", p.CategoryIDs)
	}
}

func prtValidate(t *testing.T, p dto.Printer) (bool, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	ok := validatePrinter(c, &p)
	return ok, w.Body.String()
}

func TestValidatePrinter(t *testing.T) {
	// 名称缺失。
	ok, body := prtValidate(t, dto.Printer{})
	if ok || !strings.Contains(body, "请填写打印机名称") {
		t.Fatalf("空名称应校验失败, got ok=%v body=%s", ok, body)
	}

	// 飞鹅缺 SN。
	ok, body = prtValidate(t, dto.Printer{PrinterName: "飞鹅机", Provider: po.PrinterProviderFeie})
	if ok || !strings.Contains(body, "请填写飞鹅打印机编号") {
		t.Fatalf("飞鹅缺 SN 应校验失败, got ok=%v body=%s", ok, body)
	}

	// agent 缺 IP。
	ok, body = prtValidate(t, dto.Printer{PrinterName: "代理机", Provider: po.PrinterProviderAgent})
	if ok || !strings.Contains(body, "门店内网") {
		t.Fatalf("代理缺 IP 应校验失败, got ok=%v body=%s", ok, body)
	}

	// tcp 缺 IP。
	ok, body = prtValidate(t, dto.Printer{PrinterName: "直连机", Provider: po.PrinterProviderTCP})
	if ok || !strings.Contains(body, "请填写打印机 IP 地址") {
		t.Fatalf("tcp 缺 IP 应校验失败, got ok=%v body=%s", ok, body)
	}

	// tcp IP 非法。
	ok, body = prtValidate(t, dto.Printer{PrinterName: "直连机", Provider: po.PrinterProviderTCP, IP: "999.1.1.1", Port: 9100})
	if ok || !strings.Contains(body, "打印机 IP 非法") {
		t.Fatalf("非法 IP 应校验失败, got ok=%v body=%s", ok, body)
	}

	// 合法 tcp。
	ok, _ = prtValidate(t, dto.Printer{PrinterName: "直连机", Provider: po.PrinterProviderTCP, IP: "192.168.1.50", Port: 9100})
	if !ok {
		t.Fatal("合法 tcp 打印机应校验通过")
	}
}

func TestPrinterList(t *testing.T) {
	initAgentTestDB(t)
	code, res := prtCall(t, PrinterList, http.MethodGet, "/test", "", nil)
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("打印机列表应返回 200, got %d(%v)", code, res)
	}
	data := res["data"].(map[string]interface{})
	if data["total"].(float64) < 1 {
		t.Fatalf("打印机列表应包含种子打印机, got %v", data["total"])
	}
}

func TestPrinterSaveBranches(t *testing.T) {
	initAgentTestDB(t)

	// 非法 JSON。
	if code, res := prtCall(t, PrinterSave, http.MethodPost, "/test", `{"printerName":`, nil); code != http.StatusBadRequest || res["code"] != float64(400) {
		t.Fatalf("非法 JSON 应返回 400, got %d(%v)", code, res)
	}
	// 名称缺失。
	if _, res := prtCall(t, PrinterSave, http.MethodPost, "/test", `{"provider":"tcp","ip":"192.168.1.50"}`, nil); !strings.Contains(res["msg"].(string), "请填写打印机名称") {
		t.Fatalf("空名称应校验失败, got %v", res)
	}
	// 飞鹅缺 SN。
	if _, res := prtCall(t, PrinterSave, http.MethodPost, "/test", `{"printerName":"飞鹅机","provider":"feie"}`, nil); !strings.Contains(res["msg"].(string), "请填写飞鹅打印机编号") {
		t.Fatalf("飞鹅缺 SN 应校验失败, got %v", res)
	}
	// 合法 tcp 保存成功。
	code, res := prtCall(t, PrinterSave, http.MethodPost, "/test", `{"printerName":"新直连机","provider":"tcp","ip":"192.168.1.50","port":9100,"paperWidth":48,"copies":1,"status":1}`, nil)
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("合法 tcp 保存应成功, got %d(%v)", code, res)
	}
	if res["data"].(map[string]interface{})["printerId"].(float64) <= 0 {
		t.Fatalf("保存后应返回打印机 ID, got %v", res["data"])
	}
}

func TestPrinterUpdateBranches(t *testing.T) {
	initAgentTestDB(t)
	id := prtSeedPrinter(t, po.PrinterProviderTCP, po.PrinterTypeKitchen, "待改机", "192.168.1.60", "", 1)

	// 缺少 printerId。
	if _, res := prtCall(t, PrinterUpdate, http.MethodPost, "/test", `{"printerName":"改名"}`, nil); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺 printerId 应返回参数错误, got %v", res)
	}
	body := `{"printerId":` + itoa(id) + `,"printerName":"改名后","provider":"tcp","ip":"192.168.1.61","port":9100,"paperWidth":48,"copies":1,"status":1}`
	if code, res := prtCall(t, PrinterUpdate, http.MethodPost, "/test", body, nil); code != http.StatusOK || res["msg"] != "修改成功" {
		t.Fatalf("更新应成功, got %d(%v)", code, res)
	}
}

func TestPrinterDeleteBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterDelete, http.MethodPost, "/test", "", map[string]string{"id": "abc"}); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "无效的ID") {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", res)
	}
	if code, res := prtCall(t, PrinterDelete, http.MethodPost, "/test", "", map[string]string{"id": "1"}); code != http.StatusOK || res["msg"] != "删除成功" {
		t.Fatalf("删除应成功, got %d(%v)", code, res)
	}
}

func TestPrinterTestBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterTest, http.MethodPost, "/test", "", map[string]string{"id": "abc"}); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "无效的ID") {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", res)
	}
	if _, res := prtCall(t, PrinterTest, http.MethodPost, "/test", "", map[string]string{"id": "99999"}); res["code"] != float64(400) || res["msg"] != "打印机不存在" {
		t.Fatalf("不存在的打印机应返回打印机不存在, got %v", res)
	}
	id := prtSeedPrinter(t, po.PrinterProviderAgent, po.PrinterTypeGuest, "代理测试机", "192.168.1.9", "", 1)
	code, res := prtCall(t, PrinterTest, http.MethodPost, "/test", "", map[string]string{"id": itoa(id)})
	if code != http.StatusOK || !strings.Contains(res["msg"].(string), "发送测试打印指令") {
		t.Fatalf("代理测试打印应成功, got %d(%v)", code, res)
	}
}

func TestPrinterProbeBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterProbe, http.MethodPost, "/test", "", map[string]string{"id": "abc"}); res["code"] != float64(400) {
		t.Fatalf("非法 ID 应返回 400, got %v", res)
	}
	if _, res := prtCall(t, PrinterProbe, http.MethodPost, "/test", "", map[string]string{"id": "99999"}); res["code"] != float64(400) || res["msg"] != "打印机不存在" {
		t.Fatalf("不存在的打印机应返回打印机不存在, got %v", res)
	}
	id := prtSeedPrinter(t, po.PrinterProviderAgent, po.PrinterTypeGuest, "代理机", "192.168.1.9", "", 1)
	code, res := prtCall(t, PrinterProbe, http.MethodPost, "/test", "", map[string]string{"id": itoa(id)})
	// 未启用本地代理时探测直接失败(400),不降级:云后端够不到门店内网。
	if code != http.StatusBadRequest || !strings.Contains(res["msg"].(string), "连接失败") {
		t.Fatalf("未启用代理时探测应返回 400 连接失败, got %d(%v)", code, res)
	}
}

func TestPrinterStatusBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterStatus, http.MethodGet, "/test", "", map[string]string{"id": "abc"}); res["code"] != float64(400) {
		t.Fatalf("非法 ID 应返回 400, got %v", res)
	}
	if _, res := prtCall(t, PrinterStatus, http.MethodGet, "/test", "", map[string]string{"id": "99999"}); res["code"] != float64(400) || res["msg"] != "打印机不存在" {
		t.Fatalf("不存在的打印机应返回打印机不存在, got %v", res)
	}

	agentID := prtSeedPrinter(t, po.PrinterProviderAgent, po.PrinterTypeGuest, "代理机", "192.168.1.9", "", 1)
	if code, res := prtCall(t, PrinterStatus, http.MethodGet, "/test", "", map[string]string{"id": itoa(agentID)}); code != http.StatusOK {
		t.Fatalf("代理状态应返回 200, got %d(%v)", code, res)
	}

	feieID := prtSeedPrinter(t, po.PrinterProviderFeie, po.PrinterTypeGuest, "飞鹅机", "", "SN123", 1)
	if code, res := prtCall(t, PrinterStatus, http.MethodGet, "/test", "", map[string]string{"id": itoa(feieID)}); code != http.StatusBadRequest || !strings.Contains(res["msg"].(string), "未配置飞鹅账号") {
		t.Fatalf("飞鹅未配置应返回 400, got %d(%v)", code, res)
	}
}

func TestPrinterBindBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterBind, http.MethodPost, "/test", `{"sn":`, nil); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("非法 JSON 应返回参数错误, got %v", res)
	}
	if _, res := prtCall(t, PrinterBind, http.MethodPost, "/test", `{"sn":"SN1"}`, nil); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "识别码") {
		t.Fatalf("缺 KEY 应校验失败, got %v", res)
	}
	if code, res := prtCall(t, PrinterBind, http.MethodPost, "/test", `{"sn":"SN1","key":"KEY1"}`, nil); code != http.StatusBadRequest || !strings.Contains(res["msg"].(string), "未配置飞鹅账号") {
		t.Fatalf("飞鹅未配置应返回 400, got %d(%v)", code, res)
	}
}

func TestPrinterClearBranches(t *testing.T) {
	initAgentTestDB(t)
	if _, res := prtCall(t, PrinterClear, http.MethodPost, "/test", "", map[string]string{"id": "abc"}); res["code"] != float64(400) {
		t.Fatalf("非法 ID 应返回 400, got %v", res)
	}
	if _, res := prtCall(t, PrinterClear, http.MethodPost, "/test", "", map[string]string{"id": "99999"}); res["code"] != float64(400) || res["msg"] != "打印机不存在" {
		t.Fatalf("不存在的打印机应返回打印机不存在, got %v", res)
	}
	tcpID := prtSeedPrinter(t, po.PrinterProviderTCP, po.PrinterTypeKitchen, "直连机", "192.168.1.8", "", 1)
	if _, res := prtCall(t, PrinterClear, http.MethodPost, "/test", "", map[string]string{"id": itoa(tcpID)}); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "仅支持飞鹅云") {
		t.Fatalf("tcp 清队列应被拒绝, got %v", res)
	}
	agentID := prtSeedPrinter(t, po.PrinterProviderAgent, po.PrinterTypeGuest, "代理机", "192.168.1.9", "", 1)
	if code, res := prtCall(t, PrinterClear, http.MethodPost, "/test", "", map[string]string{"id": itoa(agentID)}); code != http.StatusOK || !strings.Contains(res["msg"].(string), "已清空") {
		t.Fatalf("代理清队列应成功, got %d(%v)", code, res)
	}
}

func TestFeieInfo(t *testing.T) {
	initAgentTestDB(t)
	code, res := prtCall(t, FeieInfo, http.MethodGet, "/test", "", nil)
	if code != http.StatusOK {
		t.Fatalf("飞鹅配置概况应返回 200, got %d(%v)", code, res)
	}
	if res["data"].(map[string]interface{})["configured"] != false {
		t.Fatalf("未配置飞鹅账号时 configured 应为 false, got %v", res["data"])
	}
}

func TestPrinterTarget(t *testing.T) {
	feie := printerTarget(dto.Printer{Provider: po.PrinterProviderFeie, FeieSN: "SN9"})
	if feie != "飞鹅 SN SN9" {
		t.Fatalf("飞鹅定位串错误: %q", feie)
	}
	tcp := printerTarget(dto.Printer{Provider: po.PrinterProviderTCP, IP: "192.168.1.1", Port: 9100})
	if tcp != "192.168.1.1:9100" {
		t.Fatalf("tcp 定位串错误: %q", tcp)
	}
	port0 := printerTarget(dto.Printer{Provider: po.PrinterProviderTCP, IP: "192.168.1.1", Port: 0})
	if port0 != "192.168.1.1:9100" {
		t.Fatalf("tcp 端口兜底错误: %q", port0)
	}
	agent := printerTarget(dto.Printer{Provider: po.PrinterProviderAgent, IP: "192.168.1.1", Port: 9100})
	if !strings.Contains(agent, "门店内网") {
		t.Fatalf("代理定位串错误: %q", agent)
	}
}

// itoa 避免在测试里反复写 strconv.Itoa。
func itoa(n int) string {
	return strconv.Itoa(n)
}
