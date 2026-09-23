package pay

// ============================================================================
// 支付渠道 HTTP 通路测试(无真实网络)
//
// 微信/支付宝网关地址是硬编码常量,且各渠道内部自建 http.Client(未注入
// Transport,默认走 http.DefaultTransport)。这里通过临时替换
// http.DefaultTransport 拦截发往网关的请求,验证:
//   - 报文组装(路径、方法、关键字段);
//   - 各渠道 Create/Query/Close/Refund/QueryRefund 的响应解析与错误分支;
//   - 非 200 状态码、私钥缺失等失败路径。
// 替换在 t.Cleanup 中恢复,测试包内无 t.Parallel,串行执行无污染。
// ============================================================================

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// roundTripFunc 将函数适配为 http.RoundTripper。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// pmMockGateway 替换 http.DefaultTransport:handler 依据请求返回 (状态码, 响应体)。
func pmMockGateway(t *testing.T, handler func(method, target, body string) (int, string)) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		code, body := handler(req.Method, req.URL.String(), string(b))
		return &http.Response{
			StatusCode: code,
			Status:     fmt.Sprintf("%d", code),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
}

// pmPrivKeyFile 生成商户 RSA 私钥并写入临时 PEM 文件,返回文件路径。
func pmPrivKeyFile(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 私钥失败: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "merchant_priv.pem")
	pmWriteFile(t, path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return path
}

// pmWxpayConfig 写入微信下单所需最小配置(私钥为真实可读的临时 PEM)。
func pmWxpayConfig(t *testing.T) {
	t.Helper()
	pmSetSettings(t, map[string]string{
		"wxpay_mchid":            "1900000001",
		"wxpay_serial_no":        "5157F09EDC4A5F1234",
		"wxpay_appid":            "wx1234567890",
		"wxpay_private_key_path": pmPrivKeyFile(t),
		"wxpay_notify_url":       "https://example.com/notify/wx",
	})
}

// pmAlipayConfig 写入支付宝下单所需最小配置。
func pmAlipayConfig(t *testing.T) {
	t.Helper()
	pmSetSettings(t, map[string]string{
		"alipay_appid":            "2021000000000001",
		"alipay_private_key_path": pmPrivKeyFile(t),
		"alipay_notify_url":       "https://example.com/notify/ali",
	})
}

// TestPayWxpayCreateSuccess 微信 Native 下单:报文组装 + 成功解析。
func TestPayWxpayCreateSuccess(t *testing.T) {
	pmInitPayDB(t)
	pmWxpayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		if method != http.MethodPost || !strings.Contains(target, "/v3/pay/transactions/native") {
			t.Fatalf("下单请求异常: %s %s", method, target)
		}
		if !strings.Contains(body, `"out_trade_no":"D20260101000000"`) ||
			!strings.Contains(body, `"total":1234`) || !strings.Contains(body, `"mchid":"1900000001"`) {
			t.Fatalf("下单报文缺关键字段: %s", body)
		}
		return 200, `{"code_url":"weixin://wxpay/bizpayurl?pr=abcdef"}`
	})
	res, err := (&Wxpay{}).Create(PayReq{OrderNo: "D20260101000000", AmountCents: 1234, Description: "测试单"})
	if err != nil {
		t.Fatalf("微信下单失败: %v", err)
	}
	if res.CodeURL != "weixin://wxpay/bizpayurl?pr=abcdef" {
		t.Fatalf("CodeURL = %q", res.CodeURL)
	}
}

// TestPayWxpayCreateFailures 微信下单失败分支:私钥缺失、缺 code_url、网关非 200。
func TestPayWxpayCreateFailures(t *testing.T) {
	pmInitPayDB(t)

	// 私钥路径不可读:在 do() 的签名阶段即失败。
	pmSetSettings(t, map[string]string{
		"wxpay_mchid":            "1900000001",
		"wxpay_serial_no":        "S1",
		"wxpay_private_key_path": filepath.Join(t.TempDir(), "no_such_key.pem"),
	})
	if _, err := (&Wxpay{}).Create(PayReq{OrderNo: "D1", AmountCents: 1}); err == nil || !strings.Contains(err.Error(), "私钥") {
		t.Fatalf("私钥缺失应报错, got %v", err)
	}

	// 响应无 code_url:判定下单失败。
	pmWxpayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		return 200, `{"code_url":""}`
	})
	if _, err := (&Wxpay{}).Create(PayReq{OrderNo: "D2", AmountCents: 1}); err == nil || !strings.Contains(err.Error(), "微信下单失败") {
		t.Fatalf("缺 code_url 应报下单失败, got %v", err)
	}

	// 网关 400:透出状态码与响应体摘要。
	pmMockGateway(t, func(method, target, body string) (int, string) {
		return 400, `{"code":"PARAM_ERROR","message":"订单号格式非法"}`
	})
	if _, err := (&Wxpay{}).Create(PayReq{OrderNo: "D3", AmountCents: 1}); err == nil || !strings.Contains(err.Error(), "返回 400") {
		t.Fatalf("网关 400 应报错, got %v", err)
	}
}

// TestPayWxpayQuery 微信查单:成功/未支付两种状态。
func TestPayWxpayQuery(t *testing.T) {
	pmInitPayDB(t)
	pmWxpayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		if method != http.MethodGet || !strings.Contains(target, "/v3/pay/transactions/out-trade-no/D9") {
			t.Fatalf("查单请求异常: %s %s", method, target)
		}
		if strings.Contains(target, "D9-su") {
			return 200, `{"transaction_id":"4200001","trade_state":"SUCCESS","amount":{"total":999}}`
		}
		return 200, `{"transaction_id":"4200002","trade_state":"NOTPAY","amount":{"total":0}}`
	})
	q, err := (&Wxpay{}).Query("D9-su")
	if err != nil || !q.Success || q.ChannelTradeNo != "4200001" || q.PaidAmount != 999 {
		t.Fatalf("已支付查单异常: %+v err=%v", q, err)
	}
	q2, err := (&Wxpay{}).Query("D9-notpay")
	if err != nil || q2.Success {
		t.Fatalf("未支付查单应为 Success=false, got %+v err=%v", q2, err)
	}
}

// TestPayWxpayClose 微信关单:204/空体/`{}` 视为成功,其余报错。
func TestPayWxpayClose(t *testing.T) {
	pmInitPayDB(t)
	pmWxpayConfig(t)
	seq := 0
	pmMockGateway(t, func(method, target, body string) (int, string) {
		seq++
		switch seq {
		case 1:
			return 204, ""
		case 2:
			return 200, `{}`
		case 3:
			return 200, `{"code":"ORDER_PAID","message":"订单已支付,不能关闭"}`
		}
		return 500, "unexpected"
	})
	if err := (&Wxpay{}).Close("D1"); err != nil {
		t.Fatalf("204 关单应成功, got %v", err)
	}
	if err := (&Wxpay{}).Close("D2"); err != nil {
		t.Fatalf("空体 {} 关单应成功, got %v", err)
	}
	if err := (&Wxpay{}).Close("D3"); err == nil || !strings.Contains(err.Error(), "微信关单失败") {
		t.Fatalf("已支付订单关单应报错, got %v", err)
	}
}

// TestPayWxpayRefund 微信退款:SUCCESS/PROCESSING 视为受理成功。
func TestPayWxpayRefund(t *testing.T) {
	pmInitPayDB(t)
	pmWxpayConfig(t)
	seq := 0
	pmMockGateway(t, func(method, target, body string) (int, string) {
		seq++
		if !strings.Contains(target, "/v3/refund/domestic/refunds") || !strings.Contains(body, `"refund":100`) {
			t.Fatalf("退款请求异常: %s %s body=%s", method, target, body)
		}
		if seq == 1 {
			return 200, `{"refund_id":"5030001","out_refund_no":"RF1","status":"SUCCESS"}`
		}
		return 200, `{"refund_id":"5030002","out_refund_no":"RF2","status":"PROCESSING"}`
	})
	r1, err := (&Wxpay{}).Refund("D1", "RF1", 100, 1000)
	if err != nil || !r1.Success || r1.RefundNo != "5030001" || r1.Refunded != 100 {
		t.Fatalf("退款受理异常: %+v err=%v", r1, err)
	}
	r2, err := (&Wxpay{}).Refund("D1", "RF2", 100, 1000)
	if err != nil || !r2.Success || r2.RefundNo != "5030002" {
		t.Fatalf("退款中应视为受理成功: %+v err=%v", r2, err)
	}
}

// TestPayWxpayRefundFailed 微信退款失败状态:Success=false 但不报错(以渠道状态为准)。
func TestPayWxpayRefundFailed(t *testing.T) {
	pmInitPayDB(t)
	pmWxpayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		return 200, `{"refund_id":"5030003","out_refund_no":"RF3","status":"ABNORMAL"}`
	})
	r, err := (&Wxpay{}).Refund("D1", "RF3", 100, 1000)
	if err != nil || r.Success {
		t.Fatalf("退款失败状态应为 Success=false, got %+v err=%v", r, err)
	}
}

// TestPayAlipayCreate 支付宝预下单:成功/失败两个分支。
func TestPayAlipayCreate(t *testing.T) {
	pmInitPayDB(t)
	pmAlipayConfig(t)
	seq := 0
	pmMockGateway(t, func(method, target, body string) (int, string) {
		seq++
		if method != http.MethodPost || !strings.Contains(target, "gateway.do") {
			t.Fatalf("支付宝请求异常: %s %s", method, target)
		}
		form, err := url.ParseQuery(body)
		if err != nil {
			t.Fatalf("预下单表单解析失败: %v", err)
		}
		if form.Get("method") != "alipay.trade.precreate" ||
			!strings.Contains(form.Get("biz_content"), `"total_amount":"12.34"`) {
			t.Fatalf("预下单表单缺关键字段: %s", body)
		}
		if seq == 1 {
			return 200, `{"alipay_trade_precreate_response":{"code":"10000","msg":"Success","qr_code":"https://qr.alipay.com/bax1"}}`
		}
		return 200, `{"alipay_trade_precreate_response":{"code":"40004","msg":"Business Failed"}}`
	})
	res, err := (&Alipay{}).Create(PayReq{OrderNo: "D1", AmountCents: 1234, Description: "测试"})
	if err != nil || res.CodeURL != "https://qr.alipay.com/bax1" {
		t.Fatalf("支付宝预下单异常: %+v err=%v", res, err)
	}
	if _, err := (&Alipay{}).Create(PayReq{OrderNo: "D2", AmountCents: 1234}); err == nil || !strings.Contains(err.Error(), "支付宝下单失败") {
		t.Fatalf("业务失败应报错, got %v", err)
	}
}

// TestPayAlipayQueryClose 支付宝查单与关单。
func TestPayAlipayQueryClose(t *testing.T) {
	pmInitPayDB(t)
	pmAlipayConfig(t)
	seq := 0
	pmMockGateway(t, func(method, target, body string) (int, string) {
		seq++
		switch {
		case strings.Contains(body, "alipay.trade.query") && strings.Contains(body, "D-succ"):
			return 200, `{"alipay_trade_query_response":{"code":"10000","trade_no":"2021T1","trade_status":"TRADE_SUCCESS","total_amount":"12.34"}}`
		case strings.Contains(body, "alipay.trade.query"):
			return 200, `{"alipay_trade_query_response":{"code":"10000","trade_no":"2021T2","trade_status":"WAIT_BUYER_PAY","total_amount":"0.00"}}`
		case strings.Contains(body, "alipay.trade.close") && strings.Contains(body, "D-ok"):
			return 200, `{"alipay_trade_close_response":{"code":"10000","msg":"Success"}}`
		default:
			return 200, `{"alipay_trade_close_response":{"code":"40004","msg":"Order Not Exist"}}`
		}
	})
	q, err := (&Alipay{}).Query("D-succ")
	if err != nil || !q.Success || q.ChannelTradeNo != "2021T1" || q.PaidAmount != 1234 {
		t.Fatalf("已支付查单异常: %+v err=%v", q, err)
	}
	q2, err := (&Alipay{}).Query("D-wait")
	if err != nil || q2.Success {
		t.Fatalf("未支付查单应为 Success=false, got %+v err=%v", q2, err)
	}
	if err := (&Alipay{}).Close("D-ok"); err != nil {
		t.Fatalf("关单应成功, got %v", err)
	}
	if err := (&Alipay{}).Close("D-bad"); err == nil || !strings.Contains(err.Error(), "支付宝关单失败") {
		t.Fatalf("关单失败应报错, got %v", err)
	}
}

// TestPayAlipayRefundAndQuery 支付宝退款与退款查询。
func TestPayAlipayRefundAndQuery(t *testing.T) {
	pmInitPayDB(t)
	pmAlipayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		switch {
		case strings.Contains(body, "alipay.trade.refund"):
			return 200, `{"alipay_trade_refund_response":{"code":"10000","msg":"Success","trade_no":"2021R1","refund_fee":"5.00"}}`
		case strings.Contains(body, "fastpay.refund.query") && strings.Contains(body, "RF-ok"):
			return 200, `{"alipay_trade_fastpay_refund_query_response":{"code":"10000","trade_no":"2021R1","refund_amount":"5.00"}}`
		case strings.Contains(body, "fastpay.refund.query") && strings.Contains(body, "RF-pending"):
			return 200, `{"alipay_trade_fastpay_refund_query_response":{"code":"40004","msg":"Processing"}}`
		default:
			return 200, `{"alipay_trade_fastpay_refund_query_response":{"code":"40000","msg":"Refund Failed"}}`
		}
	})
	r, err := (&Alipay{}).Refund("D1", "RF1", 500, 1234)
	if err != nil || !r.Success || r.RefundNo != "2021R1" || r.Refunded != 500 {
		t.Fatalf("支付宝退款异常: %+v err=%v", r, err)
	}
	if q, err := (&Alipay{}).QueryRefund("D1", "RF-ok"); err != nil || q.Status != RefundSuccess || q.Refunded != 500 {
		t.Fatalf("退款查询应为成功, got %+v err=%v", q, err)
	}
	if q, err := (&Alipay{}).QueryRefund("D1", "RF-pending"); err != nil || q.Status != RefundProcessing {
		t.Fatalf("退款查询应为处理中, got %+v err=%v", q, err)
	}
	if q, err := (&Alipay{}).QueryRefund("D1", "RF-fail"); err != nil || q.Status != RefundFail {
		t.Fatalf("退款查询应为失败, got %+v err=%v", q, err)
	}
}

// TestPayAlipayCallNon200 支付宝网关非 200:报错并透出状态码。
func TestPayAlipayCallNon200(t *testing.T) {
	pmInitPayDB(t)
	pmAlipayConfig(t)
	pmMockGateway(t, func(method, target, body string) (int, string) {
		return 500, "internal error"
	})
	if _, err := (&Alipay{}).Query("D1"); err == nil || !strings.Contains(err.Error(), "返回 500") {
		t.Fatalf("网关 500 应报错, got %v", err)
	}
}

// TestPayRandNonce 微信签名 nonce:32 位十六进制、单次调用唯一性高。
func TestPayRandNonce(t *testing.T) {
	n := randNonce()
	if len(n) != 32 || strings.Trim(n, "0123456789abcdef") != "" {
		t.Fatalf("randNonce 格式非法: %q", n)
	}
	if randNonce() == n {
		t.Fatal("randNonce 连续两次不应相同")
	}
}
