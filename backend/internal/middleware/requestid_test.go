package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/conf"
)

// newTestRouter 构造只挂 RequestID 的最小引擎,handler 回显从
// 标准 context 与 gin context 读到的 ID,便于断言两个通道一致。
func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/echo", func(c *gin.Context) {
		id, ok := FromContext(c.Request.Context())
		if !ok {
			c.String(http.StatusInternalServerError, "no ctx id")
			return
		}
		gid, ok2 := FromGin(c)
		if !ok2 || gid != id {
			c.String(http.StatusInternalServerError, "id mismatch")
			return
		}
		c.String(http.StatusOK, strconv.FormatInt(id, 10))
	})
	return r
}

// TestRequestIDInjectsAndPropagates 核心契约:雪花 ID 同时进入
// 标准 context、gin context,并回写到 X-Request-ID 响应头。
func TestRequestIDInjectsAndPropagates(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, 响应体 = %s", w.Code, w.Body.String())
	}
	id, err := strconv.ParseInt(w.Body.String(), 10, 64)
	if err != nil || id <= 0 {
		t.Fatalf("响应体应为正 int64 ID,实际 %q", w.Body.String())
	}
	if got := w.Header().Get(HeaderRequestID); got != w.Body.String() {
		t.Errorf("X-Request-ID = %q, 应与 ID %s 一致", got, w.Body.String())
	}
}

// TestRequestIDUniquePerRequest 每个请求独立生成,不得复用。
func TestRequestIDUniquePerRequest(t *testing.T) {
	r := newTestRouter()

	seen := make(map[string]struct{})
	for i := 0; i < 200; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/echo", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求状态码 = %d", i, w.Code)
		}
		id := w.Body.String()
		if _, dup := seen[id]; dup {
			t.Fatalf("requestID 重复: %s", id)
		}
		seen[id] = struct{}{}
	}
}

// TestNodeIDFromEnv 节点号读取:未配置/非法回退默认值,合法值透传。
func TestNodeIDFromEnv(t *testing.T) {
	cases := map[string]int64{
		"":     defaultNodeID,
		"abc":  defaultNodeID,
		"-1":   defaultNodeID,
		"1024": defaultNodeID,
		"0":    0,
		"7":    7,
		"1023": 1023,
		" 42 ": 42,
	}
	for v, want := range cases {
		if v == "" {
			if err := os.Unsetenv(conf.EnvSnowflakeNodeID); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Setenv(conf.EnvSnowflakeNodeID, v); err != nil {
			t.Fatal(err)
		}
		if got := nodeIDFromEnv(); got != want {
			t.Errorf("SNOWFLAKE_NODE_ID=%q 解析得 %d,期望 %d", v, got, want)
		}
	}
	os.Unsetenv(conf.EnvSnowflakeNodeID)
}
