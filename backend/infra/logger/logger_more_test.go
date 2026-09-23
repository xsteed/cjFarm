package logger

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"

	"dining-system/internal/conf"
)

// ---- 纯函数 ----

// TestLoggerMoreParseLevel 校验级别字符串解析:大小写/空白/别名与默认值。
func TestLoggerMoreParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want zapcore.Level
	}{
		{"", zapcore.InfoLevel},
		{"debug", zapcore.DebugLevel},
		{"DEBUG", zapcore.DebugLevel},
		{"  debug  ", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{"warn", zapcore.WarnLevel},
		{"warning", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"fatal", zapcore.InfoLevel}, // 未识别级别回落 info
	}
	for _, c := range cases {
		if got := parseLevel(c.in); got != c.want {
			t.Errorf("parseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestLoggerMorePrettyLevel 校验非 TTY 下级别标签为定宽纯文本(无 ANSI 转义)。
func TestLoggerMorePrettyLevel(t *testing.T) {
	old := stdoutIsTTY
	stdoutIsTTY = false
	defer func() { stdoutIsTTY = old }()

	cases := []struct {
		l    zapcore.Level
		want string
	}{
		{zapcore.DebugLevel, "DEBUG"},
		{zapcore.InfoLevel, "INFO "},
		{zapcore.WarnLevel, "WARN "},
		{zapcore.ErrorLevel, "ERROR"},
		{zapcore.DPanicLevel, "FATAL"},
		{zapcore.PanicLevel, "FATAL"},
		{zapcore.FatalLevel, "FATAL"},
	}
	for _, c := range cases {
		if got := prettyLevel(c.l); got != c.want {
			t.Errorf("prettyLevel(%v) = %q, want %q", c.l, got, c.want)
		}
	}
}

// TestLoggerMoreResolveLogPath 校验主日志路径解析:默认、关闭开关与空白裁剪。
func TestLoggerMoreResolveLogPath(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"", conf.DefaultLogPath},
		{"off", ""},
		{"OFF", ""},
		{"none", ""},
		{"false", ""},
		{"0", ""},
		{"-", ""},
		{"/tmp/x/app.log", "/tmp/x/app.log"},
		{"  /tmp/y/app.log  ", "/tmp/y/app.log"},
	}
	for _, c := range cases {
		t.Setenv(conf.EnvLogPath, c.env)
		if got := resolveLogPath(); got != c.want {
			t.Errorf("resolveLogPath(LOG_PATH=%q) = %q, want %q", c.env, got, c.want)
		}
	}
}

// TestLoggerMoreResolveAgentLogPath 校验代理日志路径解析:默认并入主日志目录与关闭开关。
func TestLoggerMoreResolveAgentLogPath(t *testing.T) {
	t.Setenv(conf.EnvLogPath, "/tmp/logs/app.log")
	t.Setenv("LOG_AGENT_PATH", "")
	if got := resolveAgentLogPath(); got != filepath.Join("/tmp/logs", "agent.log") {
		t.Errorf("未配置代理路径时 = %q, want /tmp/logs/agent.log", got)
	}
	for _, off := range []string{"off", "none", "false", "0", "-"} {
		t.Setenv("LOG_AGENT_PATH", off)
		if got := resolveAgentLogPath(); got != "" {
			t.Errorf("LOG_AGENT_PATH=%q 时 = %q, want 空串", off, got)
		}
	}
	t.Setenv("LOG_AGENT_PATH", "/tmp/agent.log")
	if got := resolveAgentLogPath(); got != "/tmp/agent.log" {
		t.Errorf("显式代理路径 = %q, want /tmp/agent.log", got)
	}
}

// TestLoggerMoreEnvInt 校验轮转配置解析:非法/非正值回落默认。
func TestLoggerMoreEnvInt(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", 50},
		{"0", 50},
		{"-1", 50},
		{"abc", 50},
		{"50", 50},
		{" 100 ", 100},
	}
	for _, c := range cases {
		t.Setenv(conf.EnvLogMaxSizeMB, c.env)
		if got := envInt(conf.EnvLogMaxSizeMB, conf.DefaultLogMaxSizeMB); got != c.want {
			t.Errorf("envInt(%q) = %d, want %d", c.env, got, c.want)
		}
	}
}

// ---- Init 与输出 ----

// TestLoggerMoreInitSmoke 校验 Init 到临时文件后各等级日志正常落盘。
func TestLoggerMoreInitSmoke(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")
	t.Setenv(conf.EnvLogLevel, "debug")
	t.Setenv(conf.EnvLogPath, logFile)
	t.Setenv("LOG_AGENT_PATH", "off")

	Init()
	Debugf("smoke-debug")
	Infof("smoke-info")
	Warnf("smoke-warn")
	Errorf("smoke-error")
	Sync()

	data := readLogFile(t, logFile)
	for _, want := range []string{"smoke-debug", "smoke-info", "smoke-warn", "smoke-error"} {
		if !strings.Contains(data, want) {
			t.Errorf("日志文件缺少 %q", want)
		}
	}
}

// TestLoggerMoreLevelFiltering 校验 SetLevel 过滤低于级别的日志。
func TestLoggerMoreLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")
	t.Setenv(conf.EnvLogLevel, "warn")
	t.Setenv(conf.EnvLogPath, logFile)
	t.Setenv("LOG_AGENT_PATH", "off")

	Init()
	Debugf("drop-debug")
	Infof("drop-info")
	Warnf("keep-warn")
	Errorf("keep-error")
	Sync()

	data := readLogFile(t, logFile)
	if strings.Contains(data, "drop-debug") || strings.Contains(data, "drop-info") {
		t.Errorf("低于 warn 级别的日志不应落盘")
	}
	if !strings.Contains(data, "keep-warn") || !strings.Contains(data, "keep-error") {
		t.Errorf("warn/error 级别日志应落盘")
	}
}

// TestLoggerMoreAgentLogging 校验代理专属日志写入独立文件,不混入主日志。
func TestLoggerMoreAgentLogging(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "app.log")
	agentFile := filepath.Join(dir, "agent.log")
	t.Setenv(conf.EnvLogLevel, "debug")
	t.Setenv(conf.EnvLogPath, mainFile)
	t.Setenv("LOG_AGENT_PATH", agentFile)

	Init()
	Infof("main-info")
	AgentDebugf("agent-debug")
	AgentInfof("agent-info")
	AgentWarnf("agent-warn")
	AgentErrorf("agent-error")
	Sync()

	agentData := readLogFile(t, agentFile)
	for _, want := range []string{"agent-debug", "agent-info", "agent-warn", "agent-error"} {
		if !strings.Contains(agentData, want) {
			t.Errorf("代理日志文件缺少 %q", want)
		}
	}
	mainData := readLogFile(t, mainFile)
	if !strings.Contains(mainData, "main-info") {
		t.Errorf("主日志文件缺少 main-info")
	}
	if strings.Contains(mainData, "agent-info") {
		t.Errorf("代理日志不应混入主日志文件")
	}
}

// ---- gin 辅助 ----

// TestLoggerMoreRequestID 校验 requestID 从 gin.Context 读取雪花 ID 的兜底行为。
func TestLoggerMoreRequestID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := requestID(c); got != "" {
		t.Errorf("未设置 requestID 时应返回空串, got %q", got)
	}
	c.Set("requestID", int64(123))
	if got := requestID(c); got != "123" {
		t.Errorf("int64 类型 requestID = %q, want 123", got)
	}
	c.Set("requestID", "abc")
	if got := requestID(c); got != "" {
		t.Errorf("类型不符的 requestID 应返回空串, got %q", got)
	}
}

// readLogFile 读取日志文件内容,失败即中止测试。
func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志文件 %s 失败: %v", path, err)
	}
	return string(data)
}
