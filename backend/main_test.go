package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorsMiddleware(t *testing.T) {
	// allowedOrigins 在包加载时由环境变量初始化,测试中直接改写白名单以覆盖不同场景。
	old := allowedOrigins
	defer func() { allowedOrigins = old }()
	allowedOrigins = map[string]bool{"https://order.example.com": true}

	gin.SetMode(gin.TestMode)

	newReq := func(method, origin string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/prod-api/auth/login", nil)
		if origin != "" {
			c.Request.Header.Set("Origin", origin)
		}
		return c, w
	}

	t.Run("白名单内Origin回显", func(t *testing.T) {
		c, w := newReq("GET", "https://order.example.com")
		corsMiddleware()(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://order.example.com" {
			t.Fatalf("应回显白名单 Origin, got %q", got)
		}
	})

	t.Run("白名单外Origin拒绝", func(t *testing.T) {
		c, w := newReq("GET", "https://evil.example.com")
		corsMiddleware()(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("白名单外 Origin 不应设置 ACAO, got %q", got)
		}
	})

	t.Run("无Origin不设置", func(t *testing.T) {
		c, w := newReq("GET", "")
		corsMiddleware()(c)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("无 Origin 不应设置 ACAO, got %q", got)
		}
	})

	t.Run("预检OPTIONS返回204", func(t *testing.T) {
		c, w := newReq("OPTIONS", "https://order.example.com")
		corsMiddleware()(c)
		if w.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS 应返回 204, got %d", w.Code)
		}
	})
}
