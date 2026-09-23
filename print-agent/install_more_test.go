package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPlatformName(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want string
	}{
		{name: "darwin", goos: "darwin", want: "macOS"},
		{name: "windows", goos: "windows", want: "Windows"},
		{name: "linux", goos: "linux", want: "Linux"},
		{name: "未知平台原样", goos: "freebsd", want: "freebsd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := platformName(tt.goos); got != tt.want {
				t.Fatalf("platformName(%q)=%q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestInstallExecutableAbsPath(t *testing.T) {
	got, err := executableAbsPath()
	if err != nil {
		t.Fatalf("executableAbsPath 失败: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("executableAbsPath 应返回绝对路径, got %q", got)
	}
}

func TestInstallWriteAgentEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.env")
	if _, err := writeAgentEnv(path, "https://s.example.com", "tok-123", false, false); err != nil {
		t.Fatalf("writeAgentEnv 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"PRINT_AGENT_SERVER=https://s.example.com",
		"PRINT_AGENT_TOKEN=tok-123",
		"PRINT_AGENT_LOG=print-agent.log",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("agent.env 应包含 %q, got:\n%s", want, content)
		}
	}
}

func TestInstallRunCommand(t *testing.T) {
	t.Run("成功命令返回 nil", func(t *testing.T) {
		if err := runCommand([]string{"/bin/echo", "hello-command"}); err != nil {
			t.Fatalf("echo 命令应成功, got %v", err)
		}
	})
	t.Run("失败命令返回错误", func(t *testing.T) {
		if err := runCommand([]string{"/bin/sh", "-c", "exit 3"}); err == nil {
			t.Fatal("非零退出码命令应返回错误")
		}
	})
}

// installSetStdin 用管道替换 os.Stdin 与包级 stdinReader,供交互函数读取。
func installSetStdin(t *testing.T, input string) {
	t.Helper()
	oldStdin := os.Stdin
	oldReader := stdinReader
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if input != "" {
		_, _ = w.WriteString(input)
	}
	_ = w.Close()
	os.Stdin = r
	stdinReader = bufio.NewReader(r)
	t.Cleanup(func() {
		_ = r.Close()
		os.Stdin = oldStdin
		stdinReader = oldReader
	})
}

func TestInstallPrompt(t *testing.T) {
	t.Run("读取一行并去空白", func(t *testing.T) {
		installSetStdin(t, "  hello \n")
		got, err := prompt("> ")
		if err != nil {
			t.Fatalf("prompt 失败: %v", err)
		}
		if got != "hello" {
			t.Fatalf("prompt=%q, want hello", got)
		}
	})
	t.Run("EOF 返回错误", func(t *testing.T) {
		installSetStdin(t, "")
		if _, err := prompt("> "); err == nil {
			t.Fatal("EOF 且无内容时应返回错误")
		}
	})
}

func TestInstallReadTokenHidden(t *testing.T) {
	installSetStdin(t, "secret-token\n")
	got, err := readTokenHidden()
	if err != nil {
		t.Fatalf("readTokenHidden 失败: %v", err)
	}
	if got != "secret-token" {
		t.Fatalf("readTokenHidden=%q, want secret-token", got)
	}
}

func TestInstallAskServer(t *testing.T) {
	installSetStdin(t, "https://example.com\n")
	got := askServer()
	if got != "https://example.com" {
		t.Fatalf("askServer=%q, want https://example.com", got)
	}
}

func TestInstallAskToken(t *testing.T) {
	installSetStdin(t, "tok-999\n")
	got := askToken()
	if got != "tok-999" {
		t.Fatalf("askToken=%q, want tok-999", got)
	}
}
