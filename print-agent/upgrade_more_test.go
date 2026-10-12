package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpgradeRestartHint(t *testing.T) {
	// darwin/linux 分支只输出日志,不触发外部命令或 os.Exit,可安全调用。
	restartHint()
}

func TestUpgradeDownloadAgent(t *testing.T) {
	cfg := config{token: "tok", name: "t"}
	path := filepath.Join(t.TempDir(), "print-agent.upgrade")

	t.Run("非法 URL 报错", func(t *testing.T) {
		if _, err := downloadAgent(&http.Client{}, "://bad", cfg, path); err == nil {
			t.Fatal("非法 URL 应报错")
		}
	})

	t.Run("非 200 且带 msg 用 msg 报错", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":500,"msg":"下载通道暂不可用"}`))
		}))
		t.Cleanup(srv.Close)

		_, err := downloadAgent(srv.Client(), srv.URL, cfg, path)
		if err == nil || !strings.Contains(err.Error(), "下载通道暂不可用") {
			t.Fatalf("err=%v, want 含 msg", err)
		}
	})

	t.Run("非 200 且无 msg 带状态码与片段", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("bad gateway body"))
		}))
		t.Cleanup(srv.Close)

		_, err := downloadAgent(srv.Client(), srv.URL, cfg, path)
		if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "bad gateway body") {
			t.Fatalf("err=%v, want 含状态码与响应片段", err)
		}
	})
}

func TestUpgradeReplaceBinaryMissingSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 分支依赖真实 exe 占用语义,单测覆盖在门店真机验证")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "print-agent")
	tmp := filepath.Join(dir, "does-not-exist.upgrade")

	if err := replaceBinary(exe, tmp); err == nil {
		t.Fatal("源临时文件不存在时应报错")
	}
	// 失败后目标 exe 不应被意外创建。
	if _, err := os.Stat(exe); !os.IsNotExist(err) {
		t.Fatalf("源缺失时 exe 不应被创建, stat err=%v", err)
	}
}

// TestUpParseVersionOutput 从产物 `--version` 输出里取版本号。
func TestUpParseVersionOutput(t *testing.T) {
	out := "version=1.2.3\nbuildTime=2026-01-01T00:00:00Z\ngitCommit=abc123\n"
	if got := parseVersionOutput(out); got != "1.2.3" {
		t.Fatalf("parseVersionOutput=%q, want 1.2.3", got)
	}
	if got := parseVersionOutput("no version line here"); got != "" {
		t.Fatalf("没有 version= 行应返回空串, got %q", got)
	}
	if got := parseVersionOutput("  version=dev  \n"); got != "dev" {
		t.Fatalf("应容忍行首尾空白, got %q", got)
	}
}

// TestUpVerifyBinaryVersion 新产物自检:版本不符或跑不起来都必须中止升级。
//
// sha256 只能证明「传输完整」,证明不了「这就是配置里那个版本」:服务端给了旧产物
// 也会照刷不误,还打印「已替换为 v{latest}」——门店看到「升级成功、版本没变」;
// 更糟的是把与本机不兼容的产物刷上去,代理直接起不来。
func TestUpVerifyBinaryVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用 shell 脚本模拟产物,仅 Unix 下有效")
	}
	dir := t.TempDir()

	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\necho version=1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryVersion(good, "1.2.3"); err != nil {
		t.Fatalf("版本一致应放行: %v", err)
	}
	if err := verifyBinaryVersion(good, "2.0.0"); err == nil {
		t.Fatal("版本不一致必须拒绝,否则会谎报「已替换为 v2.0.0」")
	}

	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryVersion(broken, "1.2.3"); err == nil {
		t.Fatal("跑不起来的产物必须被拒绝,否则会刷上一个无法运行的代理")
	}

	noVersion := filepath.Join(dir, "noversion")
	if err := os.WriteFile(noVersion, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryVersion(noVersion, "1.2.3"); err == nil {
		t.Fatal("解析不出版本号必须被拒绝")
	}
}
