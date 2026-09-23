package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/middleware"
)

// newRspRouter 构造挂 RequestID 中间件的引擎,把各响应出口挂成路由,
// 便于直接验证统一响应结构的序列化结果。
func newRspRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/ok", func(c *gin.Context) { ok(c, map[string]int{"a": 1}) })
	r.GET("/okmsg", func(c *gin.Context) { okMsg(c, "已保存") })
	r.GET("/table", func(c *gin.Context) { tableResult(c, 0, []int{}) })
	r.GET("/fail", func(c *gin.Context) { fail(c, "参数错误") })
	r.GET("/forbidden", func(c *gin.Context) { forbidden(c, "没有操作权限") })
	return r
}

func doReq(t *testing.T, r *gin.Engine, path string) (int, map[string]interface{}, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s 响应体非法 JSON: %v\n%s", path, err, w.Body.String())
	}
	return w.Code, body, w.Header()
}

// TestUnifiedResponseInjectsRequestID 核心契约:各响应出口都带 requestId,
// 且与响应头 X-Request-ID 一致。
func TestUnifiedResponseInjectsRequestID(t *testing.T) {
	r := newRspRouter()
	for _, p := range []string{"/ok", "/okmsg", "/table", "/fail", "/forbidden"} {
		code, body, hdr := doReq(t, r, p)
		if code == 0 {
			t.Fatalf("%s 无状态码", p)
		}
		rid, ok := body["requestId"].(string)
		if !ok || rid == "" {
			t.Errorf("%s 响应体缺少 requestId 字段,实际 = %v", p, body)
			continue
		}
		if got := hdr.Get(middleware.HeaderRequestID); got != rid {
			t.Errorf("%s 响应头 X-Request-ID = %q 与响应体 requestId = %q 不一致", p, got, rid)
		}
	}
}

// TestUnifiedResponseFields 验证各出口的字段结构与 code/msg 语义。
func TestUnifiedResponseFields(t *testing.T) {
	r := newRspRouter()

	_, body, _ := doReq(t, r, "/ok")
	if body["code"] != float64(200) || body["msg"] != "操作成功" {
		t.Errorf("/ok 基础字段异常: %v", body)
	}
	if data, _ := body["data"].(map[string]interface{}); data["a"] != float64(1) {
		t.Errorf("/ok 的 data 字段异常: %v", body["data"])
	}

	_, body, _ = doReq(t, r, "/table")
	// 分页数据整体收在 data 下:{ data: { total, items } };
	// total 即使为 0 也必须输出,前端分页组件依赖该字段。
	page, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("/table 的 data 应为分页对象,实际 = %v", body["data"])
	}
	if page["total"] != float64(0) {
		t.Errorf("/table 的 data.total=0 被吞掉: %v", page)
	}
	if items, ok := page["items"].([]interface{}); !ok || len(items) != 0 {
		t.Errorf("/table 的 data.items 应为空数组: %v", page["items"])
	}
	if _, exists := body["rows"]; exists {
		t.Errorf("分页响应不应再有顶层 rows 字段: %v", body)
	}

	if code, body, _ := doReq(t, r, "/fail"); code != http.StatusBadRequest || body["code"] != float64(400) {
		t.Errorf("/fail 应为 400/400: status=%d body=%v", code, body)
	}
	if code, _, _ := doReq(t, r, "/forbidden"); code != http.StatusForbidden {
		t.Errorf("/forbidden 状态码应为 403,实际 %d", code)
	}
}

// TestUnifiedResponseWithoutMiddleware 未挂 RequestID 中间件时,
// requestId 为空串被 omitempty 省略,响应不出现空字段。
func TestUnifiedResponseWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ok", func(c *gin.Context) { ok(c, nil) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体非法 JSON: %v", err)
	}
	if _, exists := body["requestId"]; exists {
		t.Errorf("未挂中间件时不应输出 requestId 字段: %v", body)
	}
	if _, exists := body["data"]; exists {
		t.Errorf("data 为 nil 时应被 omitempty 省略: %v", body)
	}
}
