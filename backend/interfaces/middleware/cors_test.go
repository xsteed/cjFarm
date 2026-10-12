package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	allowed := map[string]bool{"https://order.example.com": true}

	newReq := func(method, origin string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/api/auth/login", nil)
		if origin != "" {
			c.Request.Header.Set("Origin", origin)
		}
		return c, w
	}

	t.Run("白名单内Origin回显", func(t *testing.T) {
		c, w := newReq("GET", "https://order.example.com")
		NewCORS(allowed)(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://order.example.com" {
			t.Fatalf("应回显白名单 Origin, got %q", got)
		}
	})

	t.Run("白名单外Origin拒绝", func(t *testing.T) {
		c, w := newReq("GET", "https://evil.example.com")
		NewCORS(allowed)(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("白名单外 Origin 不应设置 ACAO, got %q", got)
		}
	})

	t.Run("无Origin不设置", func(t *testing.T) {
		c, w := newReq("GET", "")
		NewCORS(allowed)(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("无 Origin 不应设置 ACAO, got %q", got)
		}
	})

	t.Run("预检OPTIONS返回204", func(t *testing.T) {
		c, w := newReq("OPTIONS", "https://order.example.com")
		NewCORS(allowed)(c)
		if w.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS 应返回 204, got %d", w.Code)
		}
	})
}

// TestLoadAllowedOrigins 回归防护:跨域白名单必须能从进程环境的 CORS_ORIGINS
// 解析(config.yaml/.env 最终都写回该变量)。此前包级 var 在 main() 之前求值,
// 配置文件里的 cors_origins 因此静默失效。
func TestLoadAllowedOrigins(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://order.example.com, https://h5.example.com ,,")
	got := LoadAllowedOrigins()
	want := map[string]bool{
		"https://order.example.com": true,
		"https://h5.example.com":    true,
	}
	if len(got) != len(want) {
		t.Fatalf("解析结果数量不符: got %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("缺少白名单 %s: got %v", k, got)
		}
	}
}
