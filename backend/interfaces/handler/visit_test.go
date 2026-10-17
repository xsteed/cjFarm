package handler

// ============================================================================
// 访问统计 HTTP 层测试(visit.go)
//
// 打点接口的三个「不合常理」约定在此固化:恒 200(参数非法/限流都不打扰用户)、
// 开关关闭时不落库、报表接口字段齐全。落库与口径的正确性在 service/dao 测试里
// 验证,这里只守 HTTP 契约。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/infrastructure/store"
)

// visitPost 以 POST 方式调用打点 handler,返回状态码与响应体。
func visitPost(t *testing.T, body string) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "test-ua")
	VisitTrack(c)
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return w.Code, out
}

// TestVisitTrackOK 正常打点:恒 200,不因统计失败打扰用户。
func TestVisitTrackOK(t *testing.T) {
	initAgentTestDB(t)
	code, res := visitPost(t, `{"visitorId":"abc-123","channel":"h5","entry":"K7M3PQ9X"}`)
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("打点应恒返回 200, got %d(%v)", code, res)
	}
}

// TestVisitTrackBadBodyStillOK 请求体非法也返回 200(埋点失败不是用户的错),
// 但不写库。
func TestVisitTrackBadBodyStillOK(t *testing.T) {
	initAgentTestDB(t)
	code, res := visitPost(t, `{not json`)
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("非法请求体也应 200, got %d(%v)", code, res)
	}
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log`).Scan(&n); err != nil {
		t.Fatalf("查询明细失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("非法请求体不应落库, 实际 %d 行", n)
	}
}

// TestVisitTrackDisabled 开关关闭:200 且不落库。
func TestVisitTrackDisabled(t *testing.T) {
	initAgentTestDB(t)
	t.Setenv("VISIT_STAT_ENABLED", "0")
	code, _ := visitPost(t, `{"visitorId":"abc","channel":"h5"}`)
	if code != http.StatusOK {
		t.Fatalf("关闭时应 200, got %d", code)
	}
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log`).Scan(&n); err != nil {
		t.Fatalf("查询明细失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("关闭时不应落库, 实际 %d 行", n)
	}
}

// TestVisitTrackRateLimited 限流命中:仍然 200,只是不记录(接口存在本身
// 不该被用来探测服务状态)。
func TestVisitTrackRateLimited(t *testing.T) {
	initAgentTestDB(t)
	t.Setenv("VISIT_RATE_PER_MIN", "1")
	code, _ := visitPost(t, `{"visitorId":"a","channel":"h5"}`)
	if code != http.StatusOK {
		t.Fatalf("第一次应 200, got %d", code)
	}
	code, _ = visitPost(t, `{"visitorId":"b","channel":"h5"}`)
	if code != http.StatusOK {
		t.Fatalf("被限流也应 200, got %d", code)
	}
}

// TestReportVisits 报表接口:200 且字段齐全(summary/daily/channels/range)。
func TestReportVisits(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportVisits, "days=7")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("访问报表应返回 200, got %d(%v)", code, res)
	}
	data := res["data"].(map[string]interface{})
	for _, key := range []string{"range", "summary", "daily", "channels", "entries"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("访问报表缺少字段 %q: %v", key, data)
		}
	}
	summary := data["summary"].(map[string]interface{})
	for _, key := range []string{"pv", "uv", "newUv", "memberUv", "totalVisitors", "memberCount", "todayPv", "todayUv", "todayNewUv", "yesterdayPv", "yesterdayUv"} {
		if _, ok := summary[key]; !ok {
			t.Fatalf("summary 缺少字段 %q: %v", key, summary)
		}
	}
	daily, ok := data["daily"].([]interface{})
	if !ok || len(daily) != 7 {
		t.Fatalf("daily 应为 7 行(缺日补零), got %v", data["daily"])
	}
}
