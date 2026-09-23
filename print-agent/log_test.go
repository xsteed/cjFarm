package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logReset 关闭并清空包级 logFile,避免测试间串扰。
func logReset(t *testing.T) {
	t.Helper()
	logMu.Lock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logMu.Unlock()
	t.Cleanup(func() {
		logMu.Lock()
		if logFile != nil {
			_ = logFile.Close()
			logFile = nil
		}
		logMu.Unlock()
	})
}

func TestLogInitLogFile(t *testing.T) {
	t.Run("空路径返回 nil", func(t *testing.T) {
		logReset(t)
		if err := initLogFile(""); err != nil {
			t.Fatalf("空路径应返回 nil, got %v", err)
		}
	})

	t.Run("临时路径成功打开", func(t *testing.T) {
		logReset(t)
		path := filepath.Join(t.TempDir(), "app.log")
		if err := initLogFile(path); err != nil {
			t.Fatalf("可写路径应成功, got %v", err)
		}
		logMu.Lock()
		opened := logFile != nil
		logMu.Unlock()
		if !opened {
			t.Fatal("initLogFile 成功后 logFile 不应为 nil")
		}
	})

	t.Run("不可写目录报错", func(t *testing.T) {
		logReset(t)
		path := filepath.Join(t.TempDir(), "no-such-dir", "app.log")
		if err := initLogFile(path); err == nil {
			t.Fatal("不存在目录下的日志路径应报错")
		}
	})
}

func TestLogLogfWritesFile(t *testing.T) {
	logReset(t)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := initLogFile(path); err != nil {
		t.Fatal(err)
	}

	logf("hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Fatalf("日志文件应包含格式串展开后的内容, got %q", data)
	}
}

// captureLogToFile 把日志重定向到临时文件并返回路径(测试内断言日志内容用)。
// 复用 logReset 保证测试结束时关闭并清空包级 logFile。
func captureLogToFile(t *testing.T) string {
	t.Helper()
	logReset(t)
	path := filepath.Join(t.TempDir(), "print-agent.log")
	if err := initLogFile(path); err != nil {
		t.Fatalf("打开测试日志文件失败: %v", err)
	}
	return path
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取测试日志失败: %v", err)
	}
	return string(data)
}

func TestLogVerboseGate(t *testing.T) {
	logPath := captureLogToFile(t)

	old := verbose
	t.Cleanup(func() { setVerbose(old) })

	setVerbose(false)
	logvf("这条不该出现")
	setVerbose(true)
	logvf("这条应出现")

	log := readLogFile(t, logPath)
	if strings.Contains(log, "这条不该出现") {
		t.Fatalf("非 verbose 时 logvf 应静默,实际:\n%s", log)
	}
	if !strings.Contains(log, "这条应出现") {
		t.Fatalf("verbose 时 logvf 应输出,实际:\n%s", log)
	}
	if !strings.Contains(log, "[调试]") {
		t.Fatalf("调试级日志应带 [调试] 前缀,实际:\n%s", log)
	}
}
