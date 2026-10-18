package handler

import (
	"net/http"
	"strings"
	"testing"
)

// TestPayJsapiRequiresParams 缺订单号或 code 时直接拒绝，不发任何渠道请求。
func TestPayJsapiRequiresParams(t *testing.T) {
	custInitDB(t)

	cases := []string{
		`{"orderNo":"D001"}`,            // 缺 code（没登录凭证换不到 openid）
		`{"code":"c1"}`,                 // 缺订单号
		`{}`,                            // 都缺
		`{"orderNo":"",` + `"code":""}`, // 显式空串
	}
	for _, body := range cases {
		w := custCall(t, PayJsapi, http.MethodPost, "/api/customer/pay/jsapi", body, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 应返回 400, got %d %s", body, w.Code, w.Body.String())
		}
		if msg := custJSON(t, w)["msg"]; msg != "参数错误" {
			t.Fatalf("body=%s 文案应为「参数错误」, got %v", body, msg)
		}
	}
}

// TestPayJsapiDisabledFallback 后端未开通在线支付时，必须给出「还能怎么付」的指引，
// 而不是让顾客卡在支付页或看到一句无用的技术错误。
func TestPayJsapiDisabledFallback(t *testing.T) {
	custInitDB(t) // 种子库 wxpay_enabled = 0

	w := custCall(t, PayJsapi, http.MethodPost, "/api/customer/pay/jsapi",
		`{"orderNo":"D001","code":"c1"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未开通时应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	msg, _ := custJSON(t, w)["msg"].(string)
	if !strings.Contains(msg, "收款码") {
		t.Fatalf("未开通时文案应指路「收款码/前台结账」, got %q", msg)
	}
}
