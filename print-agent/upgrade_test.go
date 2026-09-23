package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ============================================================================
// 版本比较
// ============================================================================

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.1.0", -1},
		{"1.2.0", "1.1.9", 1},
		{"2.0.0", "10.0.0", -1},
		{"1.0", "1.0.0", 0},        // 缺省末段补 0
		{"v1.0.0", "1.0.0", 0},     // 容忍 v 前缀
		{"dev", "1.0.0", -1},       // 开发构建比任何正式版本旧
		{"1.0.0", "dev", 1},        // 云端配置异常时本地更"新",按新版本下载
		{"not-a-ver", "0.0.1", -1}, // 非法版本按 0.0.0
		{"1.0.0-rc1", "1.0.0", -1}, // 预发布后缀视为不可解析 → 最旧,会触发升级(符合预期)
		{" 1.0.0 ", "1.0.0", 0},    // 容忍空白
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// ============================================================================
// 下载 + sha256 校验(用 httptest 模拟云端,不触碰进程自身文件)
// ============================================================================

// newDownloadServer 起一个模拟云端的服务器:download 返回固定产物与 sha256 头。
func newDownloadServer(t *testing.T, payload []byte, shaHeader string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(payload)
	if shaHeader == "" {
		shaHeader = hex.EncodeToString(sum[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/print/ping":
			writeTestJSON(w, `{"code":200,"msg":"","data":{"latestAgentVersion":"1.2.3"}}`)
		case r.URL.Path == "/api/agent/print/download":
			if r.Header.Get("X-Agent-Token") == "" {
				http.Error(w, `{"code":401,"msg":"代理令牌不正确"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("X-Agent-Sha256", shaHeader)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.Error(w, `{"code":404,"msg":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeTestJSON(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(s))
}

func TestFetchLatestVersion(t *testing.T) {
	srv := newDownloadServer(t, nil, "")
	cfg := config{token: "tok", name: "t"}
	latest, err := fetchLatestVersion(srv.Client(), srv.URL, cfg)
	if err != nil {
		t.Fatalf("fetchLatestVersion 失败: %v", err)
	}
	if latest != "1.2.3" {
		t.Fatalf("latest = %q, want 1.2.3", latest)
	}
}

func TestDownloadAndVerify(t *testing.T) {
	payload := []byte("fake print-agent binary")
	srv := newDownloadServer(t, payload, "")
	cfg := config{token: "tok", name: "t"}

	dir := t.TempDir()
	path := filepath.Join(dir, "print-agent.upgrade")
	sha, err := downloadAgent(srv.Client(), srv.URL, cfg, path)
	if err != nil {
		t.Fatalf("downloadAgent 失败: %v", err)
	}
	if sha == "" {
		t.Fatal("应返回 sha256 响应头")
	}
	if err := verifyFileSHA256(path, sha); err != nil {
		t.Fatalf("校验应通过: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(payload) {
		t.Fatalf("下载内容不一致: %q", data)
	}

	// 篡改后校验必须失败
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA256(path, sha); err == nil {
		t.Fatal("内容被篡改后校验应失败")
	}

	// 服务端无 sha256 头时应返回空串,由调用方中止升级
	srv2 := newDownloadServer(t, payload, "x")
	srv2.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
	if sha, _ = downloadAgent(srv2.Client(), srv2.URL, cfg, path); sha != "" {
		t.Fatalf("无 sha256 头时应返回空串, got %q", sha)
	}
}

// ============================================================================
// 替换自身(unix 分支;不动测试进程自身的可执行文件)
// ============================================================================

func TestReplaceBinaryUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 分支依赖真实 exe 占用语义,单测覆盖在门店真机验证")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "print-agent")
	old := []byte("old binary")
	neu := []byte("new binary")
	if err := os.WriteFile(exe, old, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "print-agent.upgrade")
	if err := os.WriteFile(tmp, neu, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(exe, tmp); err != nil {
		t.Fatalf("replaceBinary 失败: %v", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("替换后读不到 exe: %v", err)
	}
	if string(data) != string(neu) {
		t.Fatalf("替换后内容 = %q, want %q", data, neu)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("替换成功后临时文件应已消失")
	}
}

func TestUpgradeTmpPath(t *testing.T) {
	exe := "/dir/print-agent"
	got := upgradeTmpPath(exe)
	if runtime.GOOS == "windows" {
		if got != exe+".upgrade.exe" {
			t.Fatalf("windows 临时名 = %q", got)
		}
	} else if got != exe+".upgrade" {
		t.Fatalf("unix 临时名 = %q", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("临时名不应含路径穿越: %q", got)
	}
}
