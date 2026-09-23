package handler

// ============================================================================
// 报表 HTTP 层单元测试(report.go)
//
// 报表统计口径与区间解析已下沉到 service,这里只验证 handler 的参数解析与响应组装:
// 空库下各报表应返回 200 与稳定的响应结构,非法区间参数应回退默认而非报错。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// rptGet 以 GET 方式调用报表 handler 并返回 HTTP 状态码与解析后的响应体。
func rptGet(t *testing.T, h gin.HandlerFunc, query string) (int, map[string]interface{}) {
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
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return w.Code, out
}

func TestReportSummary(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportSummary, "")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("汇总报表应返回 200, got %d(%v)", code, res)
	}
	data := res["data"].(map[string]interface{})
	for _, key := range []string{"todayAmount", "todayOrderCount", "monthAmount", "tableCount"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("汇总报表缺少字段 %q: %v", key, data)
		}
	}
}

func TestReportDailyTrend(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportDailyTrend, "start=2026-01-01&end=2026-01-07&days=7")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("日趋势应返回 200, got %d(%v)", code, res)
	}
	if _, ok := res["data"].([]interface{}); !ok {
		t.Fatalf("日趋势 data 应为数组, got %T", res["data"])
	}
}

func TestReportMonthlyTrend(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportMonthlyTrend, "")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("月趋势应返回 200, got %d(%v)", code, res)
	}
	if _, ok := res["data"].([]interface{}); !ok {
		t.Fatalf("月趋势 data 应为数组, got %T", res["data"])
	}
}

func TestReportDishRankLimitBranches(t *testing.T) {
	initAgentTestDB(t)
	// 默认 limit=10。
	for _, q := range []string{"", "limit=abc", "limit=0", "limit=999", "limit=3", "sort=amount&limit=5"} {
		code, res := rptGet(t, ReportDishRank, q)
		if code != http.StatusOK || res["code"] != float64(200) {
			t.Fatalf("菜品排行 query=%q 应返回 200, got %d(%v)", q, code, res)
		}
	}
}

func TestReportHourly(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportHourly, "start=bad&end=bad&days=1")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("时段报表应返回 200, got %d(%v)", code, res)
	}
	if _, ok := res["data"].([]interface{}); !ok {
		t.Fatalf("时段报表 data 应为数组, got %T", res["data"])
	}
}

func TestReportSettleMix(t *testing.T) {
	initAgentTestDB(t)
	code, res := rptGet(t, ReportSettleMix, "start=2026-01-01&end=2026-01-31")
	if code != http.StatusOK || res["code"] != float64(200) {
		t.Fatalf("结算构成应返回 200, got %d(%v)", code, res)
	}
	if _, ok := res["data"].([]interface{}); !ok {
		t.Fatalf("结算构成 data 应为数组, got %T", res["data"])
	}
}
