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

// TestCallRawLogsEveryResponse 飞鹅所有接口(打印/状态查询/清队列/绑定…)的返回原文
// 都必须经 logRawResponse 落日志:只要收到过响应(ret=0、ret!=0、甚至非 JSON 的维护页)
// 就一定有痕。「请求飞鹅后返回了什么」不能只留在调用方手里。
func TestCallRawLogsEveryResponse(t *testing.T) {
	orig := logRawResponse
	t.Cleanup(func() { logRawResponse = orig })

	type rec struct{ apiname, sn, raw string }
	var got []rec
	logRawResponse = func(apiname, sn, raw string) { got = append(got, rec{apiname, sn, raw}) }

	srv := feieStub(t, func(apiname string, _ map[string]string) []byte {
		if apiname == "Open_printMsg" {
			return okResp(`"FEIE-1"`)
		}
		return []byte(`{"ret":-3,"msg":"bad"}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	if _, err := c.PrintMsg("sn1", "c", 1); err != nil {
		t.Fatalf("PrintMsg 失败: %v", err)
	}
	if _, err := c.PrinterStatus("sn2"); err == nil {
		t.Fatal("ret!=0 应返回错误")
	}
	if err := c.ClearQueue("sn3"); err == nil {
		t.Fatal("ret!=0 应返回错误")
	}
	if len(got) != 3 {
		t.Fatalf("三次调用都应记录返回原文, got %d 条: %+v", len(got), got)
	}
	if got[0].apiname != "Open_printMsg" || got[0].sn != "sn1" || !strings.Contains(got[0].raw, "FEIE-1") {
		t.Fatalf("打印接口返回未记录: %+v", got[0])
	}
	if got[1].apiname != "Open_queryPrinterStatus" || got[1].sn != "sn2" || !strings.Contains(got[1].raw, "bad") {
		t.Fatalf("状态查询返回未记录: %+v", got[1])
	}
	if got[2].apiname != "Open_delPrinterSqs" || !strings.Contains(got[2].raw, `"ret":-3`) {
		t.Fatalf("清队列返回未记录: %+v", got[2])
	}

	// 非 JSON 异常响应(飞鹅维护页 / 网关错误页)同样留痕。
	got = nil
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>maintenance</html>"))
	}))
	t.Cleanup(badSrv.Close)
	c2 := NewClient(Config{User: "u", Ukey: "k", APIURL: badSrv.URL})
	if err := c2.ClearQueue("sn9"); err == nil {
		t.Fatal("非 JSON 响应应报错")
	}
	if len(got) != 1 || !strings.Contains(got[0].raw, "maintenance") {
		t.Fatalf("异常响应也应留痕, got %+v", got)
	}
}

// TestCallRawLogsEveryRequest 飞鹅所有接口的请求入参都必须经 logRequest 落日志:
// 「云端说错了」必须和「自己发了什么」配对才能核对 —— ret=-2/-3、1003、
// 「记成功却没出纸」都得先确认入参(SN、份数、content)对不对。
// 连接失败(请求根本没发出去)时同样要留痕:那是「试图发了什么」的唯一证据。
func TestCallRawLogsEveryRequest(t *testing.T) {
	orig := logRequest
	t.Cleanup(func() { logRequest = orig })

	type rec struct{ apiname, sn, params string }
	var got []rec
	logRequest = func(apiname, sn string, params map[string]string) {
		got = append(got, rec{apiname, sn, formatFeieParams(params)})
	}

	srv := feieStub(t, func(apiname string, _ map[string]string) []byte {
		if apiname == "Open_printMsg" {
			return okResp(`"FEIE-1"`)
		}
		return []byte(`{"ret":-3,"msg":"bad"}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	if _, err := c.PrintMsg("sn1", "content", 1); err != nil {
		t.Fatalf("PrintMsg 失败: %v", err)
	}
	if _, err := c.PrinterStatus("sn2"); err == nil {
		t.Fatal("ret!=0 应返回错误")
	}
	if err := c.ClearQueue("sn3"); err == nil {
		t.Fatal("ret!=0 应返回错误")
	}
	if len(got) != 3 {
		t.Fatalf("三次调用都应记录请求入参, got %d 条: %+v", len(got), got)
	}
	if got[0].apiname != "Open_printMsg" || got[0].sn != "sn1" {
		t.Fatalf("打印接口请求未记录: %+v", got[0])
	}
	if !strings.Contains(got[0].params, "content=content") || !strings.Contains(got[0].params, "times=1") {
		t.Fatalf("打印接口入参缺失: %q", got[0].params)
	}
	if got[1].apiname != "Open_queryPrinterStatus" || got[1].sn != "sn2" {
		t.Fatalf("状态查询请求未记录: %+v", got[1])
	}
	if got[2].apiname != "Open_delPrinterSqs" || got[2].sn != "sn3" {
		t.Fatalf("清队列请求未记录: %+v", got[2])
	}

	// 连接失败时(请求没发出去)也要留下「试图发了什么」。
	got = nil
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badSrv.Close()
	c2 := NewClient(Config{User: "u", Ukey: "k", APIURL: badSrv.URL})
	if err := c2.ClearQueue("sn9"); err == nil {
		t.Fatal("连接失败应报错")
	}
	if len(got) != 1 || got[0].apiname != "Open_delPrinterSqs" || got[0].sn != "sn9" {
		t.Fatalf("连接失败也应记录请求入参, got %+v", got)
	}
}

// TestFormatFeieParams 入参串必须稳定可读:按 key 排序(多次调用可逐行 diff)、
// content 原样完整打印(截断会让排版问题看不出来)、识别码掩码 —— 日志会落盘,
// 设备识别码不该明文出现在里面。
func TestFormatFeieParams(t *testing.T) {
	if s := formatFeieParams(nil); s != "" {
		t.Fatalf("空参数应返回空串, got %q", s)
	}
	if s := formatFeieParams(map[string]string{"times": "2", "sn": "s1"}); s != "sn=s1 times=2" {
		t.Fatalf("参数应按 key 排序, got %q", s)
	}
	// content 不截断:一张长单数千字节也要原样进日志 —— <BR>/<CUT> 切段与
	// 标签落地是核对重点,截断后恰好看不出问题。
	long := strings.Repeat("<BR>菜", 800) // 远超历史上 500 字符的截断线
	want := "content=" + long + " times=1"
	if s := formatFeieParams(map[string]string{"content": long, "times": "1"}); s != want {
		t.Fatalf("content 应原样完整打印, got %d 字符, want %d 字符", len([]rune(s)), len([]rune(want)))
	}
	// 识别码掩码:key 整值、printerContent 的第二个 # 段。
	if s := formatFeieParams(map[string]string{"key": "secretkey"}); s != "key=***" {
		t.Fatalf("key 应掩码, got %q", s)
	}
	if s := formatFeieParams(map[string]string{"printerContent": "sn1#secretkey#前台#1234"}); s != "printerContent=sn1#***#前台#1234" {
		t.Fatalf("printerContent 识别码段应掩码, got %q", s)
	}
	// 未超限的值原样打印(SN/份数正是核对时最需要的字段)。
	if s := formatFeieParams(map[string]string{"sn": "961828266", "times": "2"}); s != "sn=961828266 times=2" {
		t.Fatalf("普通参数应原样打印, got %q", s)
	}
}

// TestPrintMsgRawReturnsRaw 成功时必须把云端返回原文交回调用方 ——
// 排查「台账记成功、纸却没出来」时,这是唯一能确认飞鹅到底回了什么的证据。
func TestPrintMsgRawReturnsRaw(t *testing.T) {
	srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(`"FEIE-1"`) })
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

	id, raw, err := c.PrintMsgRaw("sn1", "content", 1)
	if err != nil || id != "FEIE-1" {
		t.Fatalf("PrintMsgRaw 失败: id=%q err=%v", id, err)
	}
	if !strings.Contains(raw, "FEIE-1") || !strings.Contains(raw, `"ret":0`) {
		t.Fatalf("应原样返回云端响应, got %q", raw)
	}
}

// TestPrintMsgRawRejectsBadOrderID ret=0 但 data 不是订单号时必须判失败并带回原文。
// 飞鹅偶尔用「ret=0 + 空/异常 data」表达「本次没真正受理」,盲信 ret 会记成成功,
// 最终表现为「台账成功、纸没出来」这种最难排查的丢单。
func TestPrintMsgRawRejectsBadOrderID(t *testing.T) {
	for _, dataJSON := range []string{`""`, `null`, `{"x":1}`, `[]`} {
		srv := feieStub(t, func(_ string, _ map[string]string) []byte {
			return []byte(`{"ret":0,"msg":"ok","data":` + dataJSON + `}`)
		})
		c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})

		id, raw, err := c.PrintMsgRaw("sn1", "content", 1)
		if err == nil {
			t.Fatalf("data=%s 应判失败, got id=%q", dataJSON, id)
		}
		if id != "" {
			t.Fatalf("失败时订单号应为空, got %q", id)
		}
		if raw == "" {
			t.Fatalf("data=%s 失败时也应带回云端原文", dataJSON)
		}
	}
	// 数字订单号(个别接口版本)仍应放行。
	srvNum := feieStub(t, func(_ string, _ map[string]string) []byte {
		return []byte(`{"ret":0,"msg":"ok","data":12345}`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srvNum.URL})
	if id, _, err := c.PrintMsgRaw("sn1", "content", 1); err != nil || id != "12345" {
		t.Fatalf("数字订单号应可用: id=%q err=%v", id, err)
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

// TestQueryOrderState data 的形态随接口版本不同:布尔 true/false 或文本
// 「已打印」/「未打印」,两种都要认 —— 只按单一形态解析,会把另一种当成
// 「未打印」,从而误报丢单、误导商户补打。原文必须一并带回供台账留痕。
func TestQueryOrderState(t *testing.T) {
	for _, dataJSON := range []string{`true`, `"已打印"`} {
		srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(dataJSON) })
		c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
		ok, raw, err := c.QueryOrderState("sn1", "FEIE-ORDER-1")
		if err != nil || !ok {
			t.Fatalf("data=%s 应判定已打印: ok=%v err=%v", dataJSON, ok, err)
		}
		if !strings.Contains(raw, `"ret":0`) {
			t.Fatalf("应带回云端原文, got %q", raw)
		}
	}
	for _, dataJSON := range []string{`false`, `"未打印"`, `null`} {
		srv := feieStub(t, func(_ string, _ map[string]string) []byte { return okResp(dataJSON) })
		c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
		if ok, _, err := c.QueryOrderState("sn1", "FEIE-ORDER-1"); err != nil || ok {
			t.Fatalf("data=%s 应判定未打印: ok=%v err=%v", dataJSON, ok, err)
		}
	}
	// sn 必须随请求携带(官方参数表同时列出 sn 与 orderid)。
	var gotSN, gotOrder string
	srv := feieStub(t, func(_ string, form map[string]string) []byte {
		gotSN, gotOrder = form["sn"], form["orderid"]
		return okResp(`true`)
	})
	c := NewClient(Config{User: "u", Ukey: "k", APIURL: srv.URL})
	if _, _, err := c.QueryOrderState("snX", "FEIE-ORDER-9"); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if gotSN != "snX" || gotOrder != "FEIE-ORDER-9" {
		t.Fatalf("请求参数错误: sn=%q orderid=%q", gotSN, gotOrder)
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
		func() error { _, _, e := c.QueryOrderState("s", "o"); return e },
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
