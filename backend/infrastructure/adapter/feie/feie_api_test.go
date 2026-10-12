package feie

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// feieStub 启动一个假的飞鹅开放平台,按 apiname 回不同响应,并记录收到的请求。
func feieStub(t *testing.T, respond func(apiname string, form map[string]string) []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form := map[string]string{}
		for _, kv := range strings.Split(string(body), "&") {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				form[kv[:i]] = kv[i+1:]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respond(form["apiname"], form))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func okResp(data string) []byte {
	return []byte(`{"ret":0,"msg":"ok","data":` + data + `}`)
}

func TestNewClientDefaultAndCustomAPI(t *testing.T) {
	if c := NewClient(Config{User: "u", Ukey: "k"}); c.apiURL != DefaultAPI {
		t.Fatalf("空 APIURL 应回退 DefaultAPI, got %q", c.apiURL)
	}
	if c := NewClient(Config{User: "u", Ukey: "k", APIURL: "  http://x/  "}); c.apiURL != "http://x/" {
		t.Fatalf("APIURL 应被裁剪, got %q", c.apiURL)
	}
}

func TestAPIErrorError(t *testing.T) {
	e := &APIError{Ret: -3, Msg: "签名失败"}
	if e.Error() != "签名失败" {
		t.Fatalf("APIError.Error 应返回 Msg, got %q", e.Error())
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("短", 10); got != "短" {
		t.Fatalf("短串应原样返回, got %q", got)
	}
	long := strings.Repeat("中", 250)
	got := truncateRunes(long, 200)
	if len([]rune(got)) != 200 {
		t.Fatalf("长串应截断到 200 字符, got %d", len([]rune(got)))
	}
}

func TestRespDataDecoders(t *testing.T) {
	r := &resp{Data: json.RawMessage(`"hello"`)}
	if got := r.dataString(); got != "hello" {
		t.Fatalf("dataString = %q, want hello", got)
	}
	// 非字符串 data 解码失败应返回空串。
	r2 := &resp{Data: json.RawMessage(`123`)}
	if got := r2.dataString(); got != "" {
		t.Fatalf("非字符串 data 应返回空串, got %q", got)
	}

	r = &resp{Data: json.RawMessage(`true`)}
	if !r.dataBool() {
		t.Fatal("dataBool 应为 true")
	}
	r = &resp{Data: json.RawMessage(`"not-bool"`)}
	if r.dataBool() {
		t.Fatal("非布尔 data 应返回 false")
	}

	var obj struct {
		Print   int `json:"print"`
		Waiting int `json:"waiting"`
	}
	if err := (&resp{Data: json.RawMessage(`{"print":3,"waiting":1}`)}).dataObject(&obj); err != nil {
		t.Fatalf("dataObject 失败: %v", err)
	}
	if obj.Print != 3 || obj.Waiting != 1 {
		t.Fatalf("dataObject 解析错误: %+v", obj)
	}
	if err := (&resp{Data: json.RawMessage(`bad`)}).dataObject(&obj); err == nil {
		t.Fatal("非法 data 应返回错误")
	}
}

// TestCallSuccess 成功响应走通 call,且签名/公共参数被带上。
func TestCallSuccess(t *testing.T) {
	var gotForm map[string]string
	srv := feieStub(t, func(apiname string, form map[string]string) []byte {
		gotForm = form
		return okResp(`"OK"`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	r, err := c.call("Open_printMsg", map[string]string{"sn": "sn1", "content": "x"})
	if err != nil {
		t.Fatalf("call 应成功: %v", err)
	}
	if r.dataString() != "OK" {
		t.Fatalf("data 应为 OK, got %q", r.dataString())
	}
	if gotForm["user"] != "u" || gotForm["apiname"] != "Open_printMsg" || gotForm["sn"] != "sn1" {
		t.Fatalf("公共参数/业务参数缺失: %v", gotForm)
	}
	// sig 必须等于 sha1(user+ukey+stime),与导出的 Sign 一致(防止内部拼接漂移)。
	if gotForm["sig"] != Sign("u", "k", gotForm["stime"]) {
		t.Fatal("sig 与 Sign 不一致")
	}
}

func TestCallAPIError(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte {
		return []byte(`{"ret":-3,"msg":"bad"}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	_, err := c.call("Open_printMsg", nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("ret!=0 应返回 *APIError, got %T: %v", err, err)
	}
	if apiErr.Ret != -3 || !strings.Contains(apiErr.Msg, "签名") {
		t.Fatalf("APIError 文案不符: %+v", apiErr)
	}
}

func TestCallNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>maintenance</html>"))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	_, err := c.call("Open_printMsg", nil)
	if err == nil || !strings.Contains(err.Error(), "响应格式异常") {
		t.Fatalf("非 JSON 响应应报错, got %v", err)
	}
}

func TestCallHTTPStatusNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	_, err := c.call("Open_printMsg", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("非 200 应报错, got %v", err)
	}
}

func TestPrintMsg(t *testing.T) {
	var gotTimes string
	srv := feieStub(t, func(_ string, form map[string]string) []byte {
		gotTimes = form["times"]
		return okResp(`"FEIE-ORDER-1"`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	// times<1 应默认成 1。
	id, err := c.PrintMsg("sn1", "content", 0)
	if err != nil || id != "FEIE-ORDER-1" {
		t.Fatalf("PrintMsg 失败: id=%q err=%v", id, err)
	}
	if gotTimes != "1" {
		t.Fatalf("times<1 应发送 1, got %q", gotTimes)
	}
	if id2, _ := c.PrintMsg("sn1", "content", 3); id2 != "FEIE-ORDER-1" {
		t.Fatalf("多次打印应返回订单号, got %q", id2)
	}
}

func TestPrinterStatus(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte {
		return okResp(`"在线，工作状态正常"`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	st, err := c.PrinterStatus("sn1")
	if err != nil || st != "在线，工作状态正常" {
		t.Fatalf("PrinterStatus 失败: st=%q err=%v", st, err)
	}
}

func TestClearQueue(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(`""`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	if err := c.ClearQueue("sn1"); err != nil {
		t.Fatalf("ClearQueue 失败: %v", err)
	}
}

func TestQueryOrderState(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(`true`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	ok, err := c.QueryOrderState("FEIE-ORDER-1")
	if err != nil || !ok {
		t.Fatalf("QueryOrderState 失败: ok=%v err=%v", ok, err)
	}
}

func TestQueryOrderInfoByDate(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte {
		return okResp(`{"print":3,"waiting":1}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	printed, waiting, err := c.QueryOrderInfoByDate("sn1", "2026-10-13")
	if err != nil || printed != 3 || waiting != 1 {
		t.Fatalf("QueryOrderInfoByDate 失败: p=%d w=%d err=%v", printed, waiting, err)
	}
}

func TestGetModel(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(`{"model":1}`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	model, err := c.GetModel("sn1", "key1")
	if err != nil || model != 1 {
		t.Fatalf("GetModel 失败: model=%d err=%v", model, err)
	}
}

func TestAddPrinter(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte {
		return okResp(`{"ok":["sn1"],"no":["sn2"]}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	okList, noList, err := c.AddPrinter("sn1#key1#name")
	if err != nil || len(okList) != 1 || len(noList) != 1 {
		t.Fatalf("AddPrinter 失败: ok=%v no=%v err=%v", okList, noList, err)
	}
}

func TestDelPrinter(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(`""`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	if err := c.DelPrinter("sn1"); err != nil {
		t.Fatalf("DelPrinter 失败: %v", err)
	}
}

func TestCallConnectionError(t *testing.T) {
	// 指向已关闭的服务,强制 hc.Do 返回网络错误,验证「连接飞鹅云失败」分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	_, err := c.call("Open_printMsg", nil)
	if err == nil || !strings.Contains(err.Error(), "连接飞鹅云失败") {
		t.Fatalf("连接失败应报「连接飞鹅云失败」, got %v", err)
	}
}

// 每个 API 方法在飞鹅返回错误码时都应把 *APIError 透传出来。
func TestAllApisPropagateAPIError(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return []byte(`{"ret":-2,"msg":"param"}`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	cases := []func() error{
		func() error { _, e := c.PrintMsg("s", "c", 1); return e },
		func() error { _, e := c.PrinterStatus("s"); return e },
		func() error { return c.ClearQueue("s") },
		func() error { _, e := c.QueryOrderState("o"); return e },
		func() error { _, _, e := c.QueryOrderInfoByDate("s", "2026-10-13"); return e },
		func() error { _, e := c.GetModel("s", "k"); return e },
		func() error { _, _, e := c.AddPrinter("s#k"); return e },
		func() error { return c.DelPrinter("s") },
	}
	for i, fn := range cases {
		if err := fn(); err == nil {
			t.Fatalf("第 %d 个 API 应返回错误", i)
		}
	}
}
