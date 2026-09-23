package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// snippet
// ============================================================================

func TestApiSnippet(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "空", in: nil, want: ""},
		{name: "空白", in: []byte("  \n\t"), want: ""},
		{name: "短串", in: []byte("hello"), want: "hello"},
		{name: "刚好 200", in: []byte(strings.Repeat("a", 200)), want: strings.Repeat("a", 200)},
		{name: "超 200 截断", in: []byte(strings.Repeat("a", 201)), want: strings.Repeat("a", 200) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := snippet(tt.in); got != tt.want {
				t.Fatalf("snippet(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ============================================================================
// post
// ============================================================================

func apiNewServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestApiPost(t *testing.T) {
	cfg := config{token: "tok", name: "agent-1"}

	t.Run("200 且 data 解析到 out", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-Agent-Token"); got != "tok" {
				t.Errorf("X-Agent-Token=%q, want tok", got)
			}
			if got := r.Header.Get("X-Agent-Version"); got != strconv.Itoa(agentProtocolVersion) {
				t.Errorf("X-Agent-Version=%q, want %d", got, agentProtocolVersion)
			}
			if got := r.Header.Get("X-Agent-Name"); got != "agent-1" {
				t.Errorf("X-Agent-Name=%q, want agent-1", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"foo":"bar"}}`))
		})
		var out struct {
			Foo string `json:"foo"`
		}
		if err := post(srv.Client(), srv.URL, "/x", cfg, map[string]string{"a": "b"}, &out); err != nil {
			t.Fatalf("post 失败: %v", err)
		}
		if out.Foo != "bar" {
			t.Fatalf("out.Foo=%q, want bar", out.Foo)
		}
	})

	t.Run("code 非 200 带 msg 用 msg 报错", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":500,"msg":"后端错误"}`))
		})
		err := post(srv.Client(), srv.URL, "/x", cfg, nil, nil)
		if err == nil || err.Error() != "后端错误" {
			t.Fatalf("err=%v, want 后端错误", err)
		}
	})

	t.Run("code 非 200 无 msg 报 HTTP 状态码", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":500,"msg":""}`))
		})
		err := post(srv.Client(), srv.URL, "/x", cfg, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "云端返回 HTTP 500") {
			t.Fatalf("err=%v, want 含「云端返回 HTTP 500」", err)
		}
	})

	t.Run("响应非 JSON 报格式异常且含 snippet", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not-json-body"))
		})
		err := post(srv.Client(), srv.URL, "/x", cfg, nil, nil)
		if err == nil {
			t.Fatal("非 JSON 响应应报错")
		}
		if !strings.Contains(err.Error(), "云端返回格式异常") {
			t.Fatalf("err=%v, want 含「云端返回格式异常」", err)
		}
		if !strings.Contains(err.Error(), "not-json-body") {
			t.Fatalf("err=%v, want 含响应片段", err)
		}
	})

	t.Run("data 反序列化失败报解析失败", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"n":"not-an-int"}}`))
		})
		var out struct {
			N int `json:"n"`
		}
		err := post(srv.Client(), srv.URL, "/x", cfg, nil, &out)
		if err == nil || !strings.Contains(err.Error(), "解析云端数据失败") {
			t.Fatalf("err=%v, want 含「解析云端数据失败」", err)
		}
	})

	t.Run("HTTP 请求失败透传 err", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // 关闭后连接必然失败
		err := post(srv.Client(), srv.URL, "/x", cfg, nil, nil)
		if err == nil {
			t.Fatal("连接失败应返回错误")
		}
	})
}

// ============================================================================
// pull:长轮询时替换为更长超时,否则复用原 client。
// ============================================================================

func TestApiPullTimeoutBehavior(t *testing.T) {
	cfg := config{token: "tok", name: "agent-1", limit: 10}

	t.Run("wait>0 使用 wait+10s 超时", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"jobs":[]}}`))
		})
		client := srv.Client()
		client.Timeout = 10 * time.Millisecond // 若复用原 client 会超时失败
		cfg.wait = 1
		jobs, err := pull(client, srv.URL, cfg)
		if err != nil {
			t.Fatalf("长轮询应使用更长超时而非原 client 超时, got %v", err)
		}
		if len(jobs) != 0 {
			t.Fatalf("jobs=%v, want empty", jobs)
		}
	})

	t.Run("wait=0 复用原 client 超时", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"jobs":[]}}`))
		})
		client := srv.Client()
		client.Timeout = 10 * time.Millisecond // 复用原 client 会超时
		cfg.wait = 0
		if _, err := pull(client, srv.URL, cfg); err == nil {
			t.Fatal("wait=0 时应复用原 client 的短超时, expect timeout error")
		}
	})
}

// ============================================================================
// ping
// ============================================================================

func TestApiPing(t *testing.T) {
	cfg := config{token: "tok", name: "agent-1"}

	t.Run("成功返回 nil", func(t *testing.T) {
		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"shopName":"店","serverTime":"t","pending":1,"latestAgentVersion":"1.0.0"}}`))
		})
		if err := ping(srv.Client(), srv.URL, cfg); err != nil {
			t.Fatalf("ping 应成功, got %v", err)
		}
	})

	t.Run("云端版本与本地不同仍返回 nil", func(t *testing.T) {
		oldVersion := version
		version = "1.0.0"
		defer func() { version = oldVersion }()

		srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"shopName":"店","serverTime":"t","pending":1,"latestAgentVersion":"2.0.0"}}`))
		})
		if err := ping(srv.Client(), srv.URL, cfg); err != nil {
			t.Fatalf("版本落后时 ping 仍应返回 nil(仅日志提示), got %v", err)
		}
	})
}

// ============================================================================
// ack:首轮成功不重试、不 sleep。
// ============================================================================

func TestApiAckFirstAttemptSucceeds(t *testing.T) {
	var calls int32
	srv := apiNewServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
	})
	cfg := config{token: "tok", name: "agent-1"}
	ack(srv.Client(), srv.URL, cfg, []map[string]interface{}{{"jobId": 1, "ok": true}})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("首轮成功不应重试, calls=%d, want 1", got)
	}
}

// ============================================================================
// pullBody / pingBody
// ============================================================================

func TestApiPullBodyFields(t *testing.T) {
	oldVersion := version
	version = "9.9.9"
	defer func() { version = oldVersion }()

	b := pullBody("agent-x", 10, 25)
	if b["agentId"] != "agent-x" {
		t.Fatalf("agentId=%v, want agent-x", b["agentId"])
	}
	if b["limit"] != 10 || b["wait"] != 25 {
		t.Fatalf("limit/wait 错误: %v", b)
	}
	if b["version"] != agentProtocolVersion {
		t.Fatalf("version=%v, want %d", b["version"], agentProtocolVersion)
	}
	if b["buildVersion"] != "9.9.9" {
		t.Fatalf("buildVersion=%v, want 9.9.9", b["buildVersion"])
	}
	caps, ok := b["capabilities"].([]string)
	if !ok {
		t.Fatalf("capabilities 类型错误: %T", b["capabilities"])
	}
	want := []string{"printer-status", "long-pull", capabilityAckSkipped}
	if len(caps) != len(want) {
		t.Fatalf("capabilities=%v, want %v", caps, want)
	}
	for i, c := range want {
		if caps[i] != c {
			t.Fatalf("capabilities=%v, want %v", caps, want)
		}
	}
}

func TestApiPingBodyFields(t *testing.T) {
	oldVersion := version
	version = "8.8.8"
	defer func() { version = oldVersion }()

	b := pingBody("agent-y")
	if b["agentId"] != "agent-y" {
		t.Fatalf("agentId=%v, want agent-y", b["agentId"])
	}
	if b["version"] != agentProtocolVersion {
		t.Fatalf("version=%v, want %d", b["version"], agentProtocolVersion)
	}
	if b["buildVersion"] != "8.8.8" {
		t.Fatalf("buildVersion=%v, want 8.8.8", b["buildVersion"])
	}
	if _, ok := b["capabilities"]; !ok {
		t.Fatal("pingBody 应包含 capabilities 字段")
	}

	// 序列化后字段必须能被 JSON 正常表达(无 chan/函数等)。
	if _, err := json.Marshal(b); err != nil {
		t.Fatalf("pingBody 无法序列化: %v", err)
	}
}
