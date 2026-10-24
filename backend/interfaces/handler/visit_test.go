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
	for _, key := range []string{"range", "summary", "daily", "channels", "entries", "devices", "oses", "browsers"} {
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

// TestReportVisitLogs 访问明细下钻:区间/渠道/终端筛选、「未知」= 空值行、
// 去重访客数与新访客判定、畸形日期 400。
func TestReportVisitLogs(t *testing.T) {
	initAgentTestDB(t)
	seedLog := func(date, channel, visitor, device, os, browser, createTime string) {
		t.Helper()
		if _, err := store.DB.Exec(`INSERT INTO tb_visit_log(visit_date, channel, visitor_key, is_member, customer_id,
			entry, ip, city, os, browser, device, model, create_time)
			VALUES(?,?,?,?,0,?, '1.2.3.4', '', ?, ?, ?, '', ?)`,
			date, channel, visitor, 0, "K7M3PQ9X", os, browser, device, createTime); err != nil {
			t.Fatalf("插入访问明细失败: %v", err)
		}
	}
	// v0 是老访客(首访远在窗口之前),用于验证 isNew=false 分支。
	seedLog("2026-01-01", "h5", "c:v0", "手机", "iOS", "微信", "2026-01-01 09:00:00")
	seedLog("2026-01-01", "h5", "c:v1", "手机", "iOS", "微信", "2026-01-01 10:00:00")
	seedLog("2026-01-01", "h5", "c:v1", "手机", "iOS", "微信", "2026-01-01 11:00:00") // 同访客同日再来
	seedLog("2026-01-02", "admin", "c:v2", "", "", "", "2026-01-02 09:00:00")     // 终端信息缺失 = 「未知」
	for _, v := range [][2]string{{"c:v0", "2025-12-01"}, {"c:v1", "2026-01-01"}, {"c:v2", "2026-01-02"}} {
		if _, err := store.DB.Exec(`INSERT INTO tb_visitor(visitor_key, first_visit_date, last_visit_date, visit_count)
			VALUES(?,?,?,1)`, v[0], v[1], v[1]); err != nil {
			t.Fatalf("插入访客档案失败: %v", err)
		}
	}

	bodyOf := func(query string) map[string]interface{} {
		t.Helper()
		code, res := rptGet(t, ReportVisitLogs, query)
		if code != http.StatusOK || res["code"] != float64(200) {
			t.Fatalf("访问明细(%s)应返回 200, got %d(%v)", query, code, res)
		}
		return res["data"].(map[string]interface{})
	}

	// 全区间:4 次访问、3 位去重访客。
	data := bodyOf("start=2026-01-01&end=2026-01-02")
	if data["total"].(float64) != 4 || data["visitors"].(float64) != 3 {
		t.Fatalf("全区间应 4 次访问 3 位访客, got %v", data)
	}
	items := data["items"].([]interface{})
	if len(items) != 4 {
		t.Fatalf("items 应 4 行, got %v", items)
	}
	// visit_id 倒序:最新一条(01-02 的 v2)在最前。isNew 是访客级口径 ——
	// 「首访日期=访问日」的访客当天所有访问都打新客标(v1 同日两访都为 true),
	// 与报表 NewUV 的 COUNT(DISTINCT) 口径一致。
	first := items[0].(map[string]interface{})
	if !first["isNew"].(bool) || first["channelLabel"] != "管理后台" {
		t.Fatalf("v2 应为新客且带渠道标签, got %v", first)
	}
	if !items[1].(map[string]interface{})["isNew"].(bool) || !items[2].(map[string]interface{})["isNew"].(bool) {
		t.Fatalf("v1 当日的访问都应打新客标, got %v", items)
	}
	// 最后一条是老访客 v0:首访在窗口之前,不算新客。
	last := items[3].(map[string]interface{})
	if last["isNew"].(bool) {
		t.Fatalf("老访客不应判为新客, got %v", last)
	}
	if last["entryLabel"] != "K7M3PQ9X" {
		t.Fatalf("关联不到桌台的入口应回退码值, got %v", last["entryLabel"])
	}

	// 渠道筛选。
	if data := bodyOf("start=2026-01-01&end=2026-01-02&channel=admin"); data["total"].(float64) != 1 {
		t.Fatalf("channel=admin 应命中 1 条, got %v", data)
	}
	// 终端筛选:展示值直接匹配;「未知」匹配空值行。
	if data := bodyOf("start=2026-01-01&end=2026-01-02&device=%E6%89%8B%E6%9C%BA"); data["total"].(float64) != 3 {
		t.Fatalf("device=手机 应命中 3 条, got %v", data)
	}
	if data := bodyOf("start=2026-01-01&end=2026-01-02&device=未知"); data["total"].(float64) != 1 {
		t.Fatalf("device=未知 应命中 1 条(空值行), got %v", data)
	}
	// 区间单边。
	if data := bodyOf("start=2026-01-02"); data["total"].(float64) != 1 {
		t.Fatalf("start=2026-01-02 应命中 1 条, got %v", data)
	}
	// 缺省区间走 days 回退。
	if data := bodyOf("days=7"); data["range"].(map[string]interface{})["start"] == "" {
		t.Fatalf("缺省区间应回退 days 解析, got %v", data["range"])
	}

	// 畸形日期 400。
	for _, bad := range []string{"start=2026-1-1", "end=2026/01/01"} {
		if code, res := rptGet(t, ReportVisitLogs, bad); code != http.StatusBadRequest || res["code"] != float64(400) {
			t.Fatalf("非法参数(%s)应返回 400, got %d(%v)", bad, code, res)
		}
	}
}
