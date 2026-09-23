package handler

// ============================================================================
// 在线支付 HTTP 层单元测试(pay.go)
//
// 渠道 key 未配置时 provider.Enabled() 返回 false,因此这里的测试全部避开真实网络:
// 覆盖参数校验、订单状态检查、未启用在线支付时的降级路径,以及回调公共处理的
// 金额一致性与幂等回写分支。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/pay"
	"dining-system/internal/service"
	"dining-system/internal/store"
)

// payGet 以 GET 方式调用 handler 并返回解析后的响应体。
func payGet(t *testing.T, h gin.HandlerFunc, query string) map[string]interface{} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	target := "/test"
	if query != "" {
		target += "?" + query
	}
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	h(c)
	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// paySeedPayment 插入一条支付流水。
func paySeedPayment(t *testing.T, orderNo, channel, tradeNo string, status int) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_payment(order_no, channel, channel_trade_no, amount, status, prepay_id, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?)`, orderNo, channel, tradeNo, 10000, status, "", store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入支付流水失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// paySeedRefund 插入一条退款流水并返回主键。
func paySeedRefund(t *testing.T, orderID int, orderNo, channel, refundNo string, status int) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel, channel_refund_no, amount, status, reason, operator, fail_reason, create_time, update_time, is_duplicate)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, orderNo, 0, refundNo, channel, "", 1000, status, "测试", "tester", "", store.Now(), store.Now(), 0)
	if err != nil {
		t.Fatalf("插入退款流水失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func payCountRefunds(t *testing.T, orderNo string) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_refund WHERE order_no=?`, orderNo).Scan(&n); err != nil {
		t.Fatalf("统计退款流水失败: %v", err)
	}
	return n
}

func TestChannelNameAndRefundSettled(t *testing.T) {
	if got := channelName(pay.ChannelWxpay); got != "微信支付" {
		t.Fatalf("channelName(wxpay) 期望 微信支付, got %q", got)
	}
	if got := channelName(pay.ChannelAlipay); got != "支付宝" {
		t.Fatalf("channelName(alipay) 期望 支付宝, got %q", got)
	}
	if got := channelName("bogus"); got != "bogus" {
		t.Fatalf("channelName(bogus) 应原样返回, got %q", got)
	}
	if !refundSettled(pay.ChannelAlipay) {
		t.Fatal("支付宝退款应判定为已到账")
	}
	if refundSettled(pay.ChannelWxpay) {
		t.Fatal("微信退款不应判定为已到账")
	}
}

func TestTruncateMsg(t *testing.T) {
	if got := truncateMsg("短消息"); got != "短消息" {
		t.Fatalf("短消息应原样返回, got %q", got)
	}
	long := strings.Repeat("长", 250)
	if got := truncateMsg(long); len([]rune(got)) != 200 {
		t.Fatalf("超长消息应裁剪到 200 字符, got %d", len([]rune(got)))
	}
}

func TestPayCreateBranches(t *testing.T) {
	initFlowTestDB(t)

	if res := callHandler(t, PayCreate, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, PayCreate, `{"orderNo":"X","channel":"bogus"}`); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "不支持的支付渠道") {
		t.Fatalf("非法渠道应被拒绝, got %v", res)
	}
	// 微信未配置时降级提示「尚未开通」,不发起网络请求。
	if res := callHandler(t, PayCreate, `{"orderNo":"X","channel":"wxpay"}`); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "尚未开通") {
		t.Fatalf("未启用渠道应提示尚未开通, got %v", res)
	}
}

func TestPayQueryBranches(t *testing.T) {
	initFlowTestDB(t)
	insertFlowOrder(t, "PAYQUERY1", 1, 0)
	insertFlowOrder(t, "PAYQUERY2", 4, 1)

	if res := payGet(t, PayQuery, ""); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := payGet(t, PayQuery, "orderNo=NOPE"); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := payGet(t, PayQuery, "orderNo=PAYQUERY1"); res["code"] != float64(200) || res["data"].(map[string]interface{})["payStatus"] != float64(0) {
		t.Fatalf("未支付订单查询应返回 payStatus=0, got %v", res)
	}
	if res := payGet(t, PayQuery, "orderNo=PAYQUERY2"); res["code"] != float64(200) || res["data"].(map[string]interface{})["payStatus"] != float64(1) {
		t.Fatalf("已支付订单查询应返回 payStatus=1, got %v", res)
	}
}

func TestPayRefundBranches(t *testing.T) {
	initFlowTestDB(t)
	unpaid := insertFlowOrder(t, "PAYREFUND1", 3, 0)
	paid := insertFlowOrder(t, "PAYREFUND2", 4, 1)
	paySeedPayment(t, "PAYREFUND2", "wxpay", "TXN2", 1)

	if res := callHandler(t, PayRefund, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, PayRefund, `{"orderId":99999}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, PayRefund, fmt.Sprintf(`{"orderId":%d}`, unpaid)); res["code"] != float64(400) || res["msg"] != "订单未支付，无法在线退款" {
		t.Fatalf("未支付订单应被拒绝, got %v", res)
	}
	// 已支付但无在线流水。
	paidNoFlow := insertFlowOrder(t, "PAYREFUND3", 4, 1)
	if res := callHandler(t, PayRefund, fmt.Sprintf(`{"orderId":%d}`, paidNoFlow)); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "线下收款") {
		t.Fatalf("无在线流水应提示线下收款, got %v", res)
	}
	// 有在线流水但渠道未配置。
	if res := callHandler(t, PayRefund, fmt.Sprintf(`{"orderId":%d,"amount":1}`, paid)); res["code"] != float64(400) || res["msg"] != "支付渠道未配置，无法在线退款" {
		t.Fatalf("渠道未配置应被拒绝, got %v", res)
	}
}

func TestPayRefundListBranches(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "PAYREFUNDLIST1", 4, 1)
	paySeedRefund(t, id, "PAYREFUNDLIST1", "wxpay", "RF1", 1)

	if res := payGet(t, PayRefundList, ""); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	res := payGet(t, PayRefundList, fmt.Sprintf("orderId=%d", id))
	if res["code"] != float64(200) {
		t.Fatalf("退款列表应返回 200, got %v", res)
	}
	list := res["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("退款列表应返回 1 条, got %d", len(list))
	}
}

func TestPayRefundQueryBranches(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "PAYREFUNDQUERY1", 4, 1)
	successID := paySeedRefund(t, id, "PAYREFUNDQUERY1", "wxpay", "RFQ1", 1)
	bogusID := paySeedRefund(t, id, "PAYREFUNDQUERY1", "bogus", "RFQ2", 0)

	if res := callHandler(t, PayRefundQuery, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, PayRefundQuery, `{"refundId":99999}`); res["code"] != float64(400) || res["msg"] != "退款单不存在" {
		t.Fatalf("不存在的退款单应返回退款单不存在, got %v", res)
	}
	if res := callHandler(t, PayRefundQuery, fmt.Sprintf(`{"refundId":%d}`, successID)); res["code"] != float64(200) || res["data"].(map[string]interface{})["status"] != float64(1) {
		t.Fatalf("已退款应直接返回成功, got %v", res)
	}
	if res := callHandler(t, PayRefundQuery, fmt.Sprintf(`{"refundId":%d}`, bogusID)); res["code"] != float64(400) || res["msg"] != "支付渠道未配置，无法查询退款" {
		t.Fatalf("非法渠道应提示渠道未配置, got %v", res)
	}
}

// payTestContext 构造一个空的 gin 上下文,供纯函数型回调公共逻辑调用。
func payTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))
	return c
}

func TestHandlePayNotifyBranches(t *testing.T) {
	initFlowTestDB(t)

	// 非成功通知直接应答成功。
	if !handlePayNotify(payTestContext(), pay.ChannelWxpay, pay.PayNotify{Success: false}) {
		t.Fatal("非成功通知应返回 true")
	}

	// 订单不存在:应答成功避免平台重试。
	if !handlePayNotify(payTestContext(), pay.ChannelWxpay, pay.PayNotify{Success: true, OrderNo: "NOPE", AmountCents: 100}) {
		t.Fatal("订单不存在时应返回 true")
	}

	// 金额不一致:留痕一条失败退款,不自动入账。
	insertFlowOrder(t, "PAYNOTIFY1", 3, 0)
	if !handlePayNotify(payTestContext(), pay.ChannelWxpay, pay.PayNotify{Success: true, OrderNo: "PAYNOTIFY1", AmountCents: 9999, ChannelTradeNo: "M1"}) {
		t.Fatal("金额不一致应返回 true")
	}
	if n := payCountRefunds(t, "PAYNOTIFY1"); n != 1 {
		t.Fatalf("金额不一致应留痕 1 条退款流水, got %d", n)
	}

	// 金额一致 + 有待支付流水:正常入账。
	insertFlowOrder(t, "PAYNOTIFY2", 3, 0)
	paySeedPayment(t, "PAYNOTIFY2", "wxpay", "P_PAYNOTIFY2", 0)
	if !handlePayNotify(payTestContext(), pay.ChannelWxpay, pay.PayNotify{Success: true, OrderNo: "PAYNOTIFY2", AmountCents: 10000, ChannelTradeNo: "T2"}) {
		t.Fatal("正常回调应返回 true")
	}
	var payStatus int
	if err := store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_no='PAYNOTIFY2'`).Scan(&payStatus); err != nil || payStatus != 1 {
		t.Fatalf("正常回调后订单应已支付, got payStatus=%d err=%v", payStatus, err)
	}
}

func TestPayNotifyHandlersRejectWithoutConfig(t *testing.T) {
	initFlowTestDB(t)

	// 微信回调:缺少验签材料,验签失败,应返回 400 FAIL。
	req := httptest.NewRequest(http.MethodPost, "/pay/notify/wxpay", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	PayNotifyWxpay(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "FAIL") {
		t.Fatalf("微信回调验签失败应返回 400 FAIL, got %d(%s)", w.Code, w.Body.String())
	}

	// 支付宝回调:空 body 缺少签名,应返回 400 fail。
	req2 := httptest.NewRequest(http.MethodPost, "/pay/notify/alipay", strings.NewReader(""))
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = req2
	PayNotifyAlipay(c2)
	if w2.Code != http.StatusBadRequest || w2.Body.String() != "fail" {
		t.Fatalf("支付宝回调验签失败应返回 400 fail, got %d(%s)", w2.Code, w2.Body.String())
	}
}

func TestClosePendingPaymentsNoNetwork(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "PAYCLOSE1", 3, 0)
	paySeedPayment(t, "PAYCLOSE1", "wxpay", "P_PAYCLOSE1", 0)

	// 渠道未配置:关闭流程应跳过网络,不改变流水状态,也不 panic。
	closeOnlinePayment(id)
	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_payment WHERE order_no='PAYCLOSE1'`).Scan(&status); err != nil || status != 0 {
		t.Fatalf("渠道未配置时待支付流水应保持原状态, got %d err=%v", status, err)
	}

	// 不存在的订单:直接返回,不 panic。
	closeOnlinePayment(99999)
}

// TestSettleQueryResultAmountGuard 主动查单补单的金额防线(高危回归):
// 少付/0 元绝不入账、只留痕人工核对;金额一致才补单。钉住「生成旧码→加菜涨价→
// 用旧码付旧金额→前端轮询补单」的少付入账漏洞。
func TestSettleQueryResultAmountGuard(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "PAYQSET1", 1, 0) // insertFlowOrder 固定金额 10000 分
	paySeedPayment(t, "PAYQSET1", "wxpay", "P_PAYQSET1", 0)

	// 少付(9000 分):拒绝入账,留痕一条失败退款,订单保持未支付。
	settleQueryResult(id, "PAYQSET1", "wxpay", pay.PayQuery{Success: true, PaidAmount: 9000, ChannelTradeNo: "TXN9000"})
	if _, ps, _, _, _ := service.GetOrderAmount("PAYQSET1"); ps != 0 {
		t.Fatalf("少付补单不应入账, payStatus=%d", ps)
	}
	if n := payCountRefunds(t, "PAYQSET1"); n != 1 {
		t.Fatalf("少付应留痕 1 条失败退款, got %d", n)
	}

	// 0 元(如支付宝金额解析失败):拒绝入账并留痕。
	settleQueryResult(id, "PAYQSET1", "wxpay", pay.PayQuery{Success: true, PaidAmount: 0, ChannelTradeNo: "TXN0"})
	if _, ps, _, _, _ := service.GetOrderAmount("PAYQSET1"); ps != 0 {
		t.Fatalf("0 元补单不应入账, payStatus=%d", ps)
	}
	if n := payCountRefunds(t, "PAYQSET1"); n != 2 {
		t.Fatalf("0 元应再留痕 1 条失败退款, got %d", n)
	}

	// 金额一致(10000 分):正常补单入账,不再新增留痕。
	settleQueryResult(id, "PAYQSET1", "wxpay", pay.PayQuery{Success: true, PaidAmount: 10000, ChannelTradeNo: "TXN10000"})
	if _, ps, _, _, _ := service.GetOrderAmount("PAYQSET1"); ps != 1 {
		t.Fatalf("金额一致应补单入账, payStatus=%d", ps)
	}
	if n := payCountRefunds(t, "PAYQSET1"); n != 2 {
		t.Fatalf("金额一致补单不应新增留痕, got %d", n)
	}
}
