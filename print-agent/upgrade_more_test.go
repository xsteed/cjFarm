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
