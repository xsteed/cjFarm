package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// custInitDB 建临时库并跑完迁移/种子,供顾客端 handler 测试使用。
func custInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "customer.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// custCall 直接调用一个顾客端 handler。
func custCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	if params != nil {
		c.Params = params
	}
	h(c)
	return w
}

// custJSON 解析响应为 map。
func custJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// custData 取响应里的 data 对象。
func custData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// custSeedOrder 直接种一条订单(绕过业务链路,精确控制状态),返回 orderID。
func custSeedOrder(t *testing.T, orderNo string, orderStatus, payStatus int) int {
	t.Helper()
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, 1, "01", "大厅01桌", 2, orderStatus, 2800, 0, 0, 2800, payStatus, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&id); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	return id
}

// TestCustomerTableByID 按数字桌台 ID 扫码进入桌台。
func TestCustomerTableByID(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerTable, http.MethodGet, "/order/1", "",
		gin.Params{{Key: "id", Value: "1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("按 ID 进入桌台应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["tableId"].(float64) != 1 {
		t.Fatalf("tableId 错误: %v", data["tableId"])
	}
}

// TestCustomerTableByCode 按稳定桌台码扫码进入桌台。
func TestCustomerTableByCode(t *testing.T) {
	custInitDB(t)

	// 桌台码由种子数据生成,从库里取真实值,不能硬编码。
	var code string
	if err := store.DB.QueryRow(`SELECT COALESCE(table_code,'') FROM tb_table WHERE table_id=1`).Scan(&code); err != nil || code == "" {
		t.Fatalf("读取桌台稳定码失败: %v (code=%q)", err, code)
	}
	w := custCall(t, CustomerTable, http.MethodGet, "/order/"+code, "",
		gin.Params{{Key: "id", Value: code}})
	if w.Code != http.StatusOK {
		t.Fatalf("按桌台码进入桌台应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["tableId"].(float64) != 1 {
		t.Fatalf("tableId 错误: %v", data["tableId"])
	}
}

// TestCustomerTableNotFound 桌台不存在。
func TestCustomerTableNotFound(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerTable, http.MethodGet, "/order/99999", "",
		gin.Params{{Key: "id", Value: "99999"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("不存在的桌台应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if resp["msg"] != "桌台不存在" {
		t.Fatalf("不存在的桌台应返回桌台不存在, got %v", resp)
	}
}

// TestCustomerTableInvalidRef 非法桌台标识(既非桌台码也非数字 ID)。
func TestCustomerTableInvalidRef(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerTable, http.MethodGet, "/order/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法桌台标识应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestCustomerMenu 顾客端菜单成功路径。
func TestCustomerMenu(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerMenu, http.MethodGet, "/api/customer/menu", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("顾客菜单应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	items, ok := resp["data"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("顾客菜单 data 应非空, got %v", resp["data"])
	}
}

// TestCustomerRemarks 顾客端备注成功路径。
func TestCustomerRemarks(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerRemarks, http.MethodGet, "/api/customer/remarks", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("顾客备注应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	items, ok := resp["data"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("顾客备注 data 应非空, got %v", resp["data"])
	}
}

// TestCustomerPayQr 收款码配置成功路径。
func TestCustomerPayQr(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerPayQr, http.MethodGet, "/api/customer/payqr", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("收款码接口应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["pay_qr_wx"] != "/uploads/pay_wx.png" {
		t.Fatalf("收款码配置错误: %v", data)
	}
}

// TestCustomerSetting 顾客端公开配置成功路径。
func TestCustomerSetting(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerSetting, http.MethodGet, "/api/customer/setting", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("顾客配置应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["shop_name"] == "" {
		t.Fatalf("顾客配置应包含 shop_name, got %v", data)
	}
}

// TestCustomerCreateOrder 顾客首次下单成功路径。
func TestCustomerCreateOrder(t *testing.T) {
	custInitDB(t)

	body := `{"tableId":1,"personCount":2,"orderRemark":"少盐","items":[{"dishId":1,"specId":1,"quantity":1}]}`
	w := custCall(t, CustomerCreateOrder, http.MethodPost, "/api/customer/order", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("顾客下单应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if _, ok := data["orderId"]; !ok {
		t.Fatalf("顾客下单应返回 orderId, got %v", data)
	}
	if _, ok := data["orderNo"]; !ok {
		t.Fatalf("顾客下单应返回 orderNo, got %v", data)
	}
}

// TestCustomerCreateOrderMissingTable 未选择桌台被拒绝。
func TestCustomerCreateOrderMissingTable(t *testing.T) {
	custInitDB(t)

	body := `{"tableId":0,"personCount":2,"items":[{"dishId":1,"specId":1,"quantity":1}]}`
	w := custCall(t, CustomerCreateOrder, http.MethodPost, "/api/customer/order", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未选择桌台应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请选择桌台并点选菜品") {
		t.Fatalf("未选择桌台应返回业务错误提示, got %v", resp)
	}
}

// TestCustomerCreateOrderMissingItems 未点选菜品被拒绝。
func TestCustomerCreateOrderMissingItems(t *testing.T) {
	custInitDB(t)

	body := `{"tableId":1,"personCount":2,"items":[]}`
	w := custCall(t, CustomerCreateOrder, http.MethodPost, "/api/customer/order", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未点选菜品应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请选择桌台并点选菜品") {
		t.Fatalf("未点选菜品应返回业务错误提示, got %v", resp)
	}
}

// TestCustomerOrderByNo 按订单号查询成功路径与不存在分支。
func TestCustomerOrderByNo(t *testing.T) {
	custInitDB(t)
	custSeedOrder(t, "CUSTBYNO1", 1, 0)

	w := custCall(t, CustomerOrderByNo, http.MethodGet, "/api/customer/order/CUSTBYNO1", "",
		gin.Params{{Key: "orderNo", Value: "CUSTBYNO1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("按订单号查询应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["orderNo"] != "CUSTBYNO1" {
		t.Fatalf("订单号错误: %v", data["orderNo"])
	}

	w2 := custCall(t, CustomerOrderByNo, http.MethodGet, "/api/customer/order/NOPE", "",
		gin.Params{{Key: "orderNo", Value: "NOPE"}})
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("不存在的订单应返回 400, got %d body=%s", w2.Code, w2.Body.String())
	}
	resp := custJSON(t, w2)
	if resp["msg"] != "订单不存在或已结束" {
		t.Fatalf("不存在的订单应返回订单不存在或已结束, got %v", resp)
	}
}

// TestCustomerAppendOrder 顾客加菜成功路径。
func TestCustomerAppendOrder(t *testing.T) {
	custInitDB(t)
	custSeedOrder(t, "CUSTAPPEND1", 1, 0)

	body := `{"tableId":1,"orderNo":"CUSTAPPEND1","items":[{"dishId":1,"specId":1,"quantity":1}]}`
	w := custCall(t, CustomerAppendOrder, http.MethodPost, "/api/customer/order/append", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("加菜应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if data["orderNo"] != "CUSTAPPEND1" {
		t.Fatalf("加菜订单号错误: %v", data["orderNo"])
	}
}

// custEnableWxpay 把微信支付配成「已开通」并打桩网关,返回记录到的关单订单号。
//
// 关单要签名,私钥必须是一份真实可读的 RSA PEM —— 路径为空或内容非法会在签名阶段就失败,
// 关单也就无从验证。回调验签材料只判「路径非空」,给个占位路径即可。
func custEnableWxpay(t *testing.T) *[]string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成测试私钥失败: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("编码测试私钥失败: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "wxpay_key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("写入测试私钥失败: %v", err)
	}
	for k, v := range map[string]string{
		"wxpay_enabled":          "1",
		"wxpay_mchid":            "1900000001",
		"wxpay_apiv3_key":        "0123456789abcdef0123456789abcdef",
		"wxpay_serial_no":        "SERIAL-1",
		"wxpay_private_key_path": keyPath,
		"wxpay_pubkey_id":        "PUB_KEY_ID_1",
		"wxpay_pubkey_path":      filepath.Join(t.TempDir(), "pub_key.pem"),
	} {
		if err := service.SetSetting(k, v); err != nil {
			t.Fatalf("写入支付配置 %s 失败: %v", k, err)
		}
	}

	closed := &[]string{}
	old := http.DefaultTransport
	http.DefaultTransport = custRoundTrip(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/close") {
			parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
			*closed = append(*closed, parts[len(parts)-2])
		}
		// 微信关单:204 空体即视为成功(见 Wxpay.Close)。
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	return closed
}

// custRoundTrip 把函数适配为 http.RoundTripper。
type custRoundTrip func(*http.Request) (*http.Response, error)

func (f custRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// custPaymentStatus 读某条支付流水的状态。
func custPaymentStatus(t *testing.T, paymentID int) int {
	t.Helper()
	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_payment WHERE payment_id=?`, paymentID).Scan(&status); err != nil {
		t.Fatalf("查询支付流水状态失败: %v", err)
	}
	return status
}

// TestCustomerAppendOrderClosesStalePayCode 加菜涨价后,顾客手里的旧收款码必须失效。
//
// 场景:下单 → 生成收款码(按当时的应收) → 加菜 → 应收变大。旧码若仍可扫,顾客会按旧
// 金额付款,回调金额与订单当前总额不符,被金额防线拒之门外,留下一笔「顾客已付、订单
// 未入账」要人工捞的挂起单。因此加菜与改单/收款/取消同一口径:关掉待支付的在线渠道单。
func TestCustomerAppendOrderClosesStalePayCode(t *testing.T) {
	custInitDB(t)
	custSeedOrder(t, "CUSTAPPEND2", 1, 0)
	closed := custEnableWxpay(t)
	paymentID := paySeedPayment(t, "CUSTAPPEND2", "wxpay", "P_CUSTAPPEND2", 0)

	body := `{"tableId":1,"orderNo":"CUSTAPPEND2","clientRequestId":"REQ-1","items":[{"dishId":1,"specId":1,"quantity":1}]}`
	w := custCall(t, CustomerAppendOrder, http.MethodPost, "/api/customer/order/append", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("加菜应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(*closed) != 1 || (*closed)[0] != "CUSTAPPEND2" {
		t.Fatalf("加菜后应向渠道关掉该订单的旧收款码, got %v", *closed)
	}
	if got := custPaymentStatus(t, paymentID); got != dao.PayStatusClosed {
		t.Fatalf("旧支付流水应置为已关闭, got %d", got)
	}

	// 幂等重放(同一 clientRequestId):金额没有再变,不重复关单。
	// 重新种一条待支付流水,否则第一次已关单、列表本来就为空,断言没有意义。
	paySeedPayment(t, "CUSTAPPEND2", "wxpay", "P_CUSTAPPEND2_REPLAY", 0)
	w2 := custCall(t, CustomerAppendOrder, http.MethodPost, "/api/customer/order/append", body, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("幂等重放应返回 200, got %d body=%s", w2.Code, w2.Body.String())
	}
	if len(*closed) != 1 {
		t.Fatalf("幂等重放不该再关单, got %v", *closed)
	}
}

// TestCustomerAppendOrderMissingOrderNo 加菜缺少订单号被拒绝。
func TestCustomerAppendOrderMissingOrderNo(t *testing.T) {
	custInitDB(t)

	body := `{"orderNo":"","items":[{"dishId":1,"specId":1,"quantity":1}]}`
	w := custCall(t, CustomerAppendOrder, http.MethodPost, "/api/customer/order/append", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少订单号应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少订单号应返回参数错误, got %v", resp)
	}
}

// TestCustomerUrgeOrder 顾客催菜成功路径。
func TestCustomerUrgeOrder(t *testing.T) {
	custInitDB(t)
	custSeedOrder(t, "CUSTURGE1", 1, 0)

	body := `{"tableId":1,"orderNo":"CUSTURGE1"}`
	w := custCall(t, CustomerUrgeOrder, http.MethodPost, "/api/customer/order/urge", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("催菜应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if _, ok := data["orderId"]; !ok {
		t.Fatalf("催菜应返回 orderId, got %v", data)
	}
}

// TestCustomerUrgeOrderMissingOrderNo 催菜缺少订单号被拒绝。
func TestCustomerUrgeOrderMissingOrderNo(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerUrgeOrder, http.MethodPost, "/api/customer/order/urge",
		`{"orderNo":""}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少订单号应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少订单号应返回参数错误, got %v", resp)
	}
}

// TestCustomerUrgeOrderNotFound 催菜订单不存在。
func TestCustomerUrgeOrderNotFound(t *testing.T) {
	custInitDB(t)

	w := custCall(t, CustomerUrgeOrder, http.MethodPost, "/api/customer/order/urge",
		`{"tableId":1,"orderNo":"NOPE"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("不存在的订单应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := custJSON(t, w)
	if resp["msg"] != "订单不存在或已结束" {
		t.Fatalf("不存在的订单应返回订单不存在或已结束, got %v", resp)
	}
}
