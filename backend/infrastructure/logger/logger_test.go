package logger

import "testing"

func TestIsAgentPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/agent/print/pull", true},
		{"/api/agent/print/ack", true},
		{"/api/agent/print/ping", true},
		{"/api/admin/printer/list", false},
		{"/api/dining/pay/query", false},
		{"/agent/print/pull", false}, // 缺 /api 前缀不算(路由组恒带 /api)
	}
	for _, c := range cases {
		if got := isAgentPath(c.path); got != c.want {
			t.Errorf("isAgentPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestSkipPath(t *testing.T) {
	for _, p := range []string{"/uploads/a.png", "/static/js/app.js", "/assets/x.css", "/favicon.ico"} {
		if !skipPath(p) {
			t.Errorf("skipPath(%q) 应为 true", p)
		}
	}
	if skipPath("/api/agent/print/pull") {
		t.Errorf("skipPath(agent 路径) 应为 false(它走 fileOnly,不是 skip)")
	}
}
