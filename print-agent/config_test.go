package main

import (
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// normalizeLogPath:关闭标志统一为空串,其余原样返回。
// ============================================================================

func TestCfgNormalizeLogPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "off", in: "off", want: ""},
		{name: "none", in: "none", want: ""},
		{name: "false", in: "false", want: ""},
		{name: "数字零", in: "0", want: ""},
		{name: "短横线", in: "-", want: ""},
		{name: "大小写混合", in: " OFF ", want: ""},
		{name: "带空白", in: "  Off\t", want: ""},
		{name: "普通路径", in: "/tmp/print-agent.log", want: "/tmp/print-agent.log"},
		{name: "路径带首尾空白", in: "  /tmp/a.log  ", want: "/tmp/a.log"},
		{name: "空串", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLogPath(tt.in); got != tt.want {
				t.Fatalf("normalizeLogPath(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ============================================================================
// resolveLogPath:相对路径锚定到程序目录,绝对路径/空串原样返回。
// ============================================================================

func TestCfgResolveLogPath(t *testing.T) {
	t.Run("空串表示不落盘", func(t *testing.T) {
		if got := resolveLogPath(""); got != "" {
			t.Fatalf("resolveLogPath(\"\")=%q, want 空串", got)
		}
	})

	t.Run("绝对路径原样返回", func(t *testing.T) {
		const abs = "/var/log/print-agent.log"
		if got := resolveLogPath(abs); got != abs {
			t.Fatalf("resolveLogPath(%q)=%q, want 原样", abs, got)
		}
	})

	t.Run("相对路径锚定到程序目录", func(t *testing.T) {
		exe, err := os.Executable()
		if err != nil {
			t.Skip("无法获取可执行文件路径")
		}
		want := filepath.Join(filepath.Dir(exe), "print-agent.log")
		if got := resolveLogPath("print-agent.log"); got != want {
			t.Fatalf("resolveLogPath(\"print-agent.log\")=%q, want %q", got, want)
		}
	})

	t.Run("相对子目录同样锚定", func(t *testing.T) {
		exe, err := os.Executable()
		if err != nil {
			t.Skip("无法获取可执行文件路径")
		}
		want := filepath.Join(filepath.Dir(exe), "logs", "agent.log")
		if got := resolveLogPath(filepath.Join("logs", "agent.log")); got != want {
			t.Fatalf("resolveLogPath(logs/agent.log)=%q, want %q", got, want)
		}
	})
}

// ============================================================================
// envOr / envInt
// ============================================================================

func TestCfgEnvOr(t *testing.T) {
	const key = "CFG_TEST_ENV_OR"

	t.Run("缺失返回默认值", func(t *testing.T) {
		t.Setenv(key, "")
		if got := envOr(key, "def"); got != "def" {
			t.Fatalf("envOr()=%q, want def", got)
		}
	})
	t.Run("纯空白返回默认值", func(t *testing.T) {
		t.Setenv(key, "   ")
		if got := envOr(key, "def"); got != "def" {
			t.Fatalf("envOr()=%q, want def", got)
		}
	})
	t.Run("合法值返回并去空白", func(t *testing.T) {
		t.Setenv(key, "  abc  ")
		if got := envOr(key, "def"); got != "abc" {
			t.Fatalf("envOr()=%q, want abc", got)
		}
	})
}

func TestCfgEnvInt(t *testing.T) {
	const key = "CFG_TEST_ENV_INT"

	t.Run("缺失返回默认值", func(t *testing.T) {
		t.Setenv(key, "")
		if got := envInt(key, 7); got != 7 {
			t.Fatalf("envInt()=%d, want 7", got)
		}
	})
	t.Run("非法返回默认值", func(t *testing.T) {
		t.Setenv(key, "not-a-number")
		if got := envInt(key, 7); got != 7 {
			t.Fatalf("envInt()=%d, want 7", got)
		}
	})
	t.Run("合法返回整数值", func(t *testing.T) {
		t.Setenv(key, " 42 ")
		if got := envInt(key, 7); got != 42 {
			t.Fatalf("envInt()=%d, want 42", got)
		}
	})
}

// ============================================================================
// loadEnvFile
// ============================================================================

func TestCfgLoadEnvFile(t *testing.T) {
	t.Run("注释空行无等号行跳过且剥离引号", func(t *testing.T) {
		const key = "CFG_TEST_ENV_LOAD"
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		path := filepath.Join(t.TempDir(), "agent.env")
		content := "# 注释行\n" +
			"\n" +
			"没有等号的行\n" +
			key + `='hello world'` + "\n" +
			"另一个空行\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		loadEnvFile(path)

		got := os.Getenv(key)
		if got != "hello world" {
			t.Fatalf("loadEnvFile 后 %s=%q, want %q", key, got, "hello world")
		}
	})

	t.Run("已存在环境变量不覆盖", func(t *testing.T) {
		const key = "CFG_TEST_ENV_LOAD_KEEP"
		t.Setenv(key, "preexist")
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		path := filepath.Join(t.TempDir(), "agent.env")
		if err := os.WriteFile(path, []byte(key+"=from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		loadEnvFile(path)
		if got := os.Getenv(key); got != "preexist" {
			t.Fatalf("已有环境变量不应被覆盖, got %q", got)
		}
	})

	t.Run("文件不存在静默跳过", func(t *testing.T) {
		loadEnvFile(filepath.Join(t.TempDir(), "does-not-exist.env"))
	})
}

// ============================================================================
// envFilePath
// ============================================================================

func TestCfgEnvFilePath(t *testing.T) {
	t.Run("--env 形式", func(t *testing.T) {
		old := os.Args
		os.Args = []string{"print-agent", "--env", "/tmp/custom.env"}
		defer func() { os.Args = old }()
		if got := envFilePath(); got != "/tmp/custom.env" {
			t.Fatalf("envFilePath()=%q, want /tmp/custom.env", got)
		}
	})
	t.Run("--env= 形式", func(t *testing.T) {
		old := os.Args
		os.Args = []string{"print-agent", "--env=/tmp/custom2.env"}
		defer func() { os.Args = old }()
		if got := envFilePath(); got != "/tmp/custom2.env" {
			t.Fatalf("envFilePath()=%q, want /tmp/custom2.env", got)
		}
	})
	t.Run("无参数返回默认非空", func(t *testing.T) {
		old := os.Args
		os.Args = []string{"print-agent"}
		defer func() { os.Args = old }()
		got := envFilePath()
		if got == "" {
			t.Fatal("envFilePath() 不应返回空串")
		}
	})
}

// ============================================================================
// newHTTPClient
// ============================================================================

func TestCfgNewHTTPClient(t *testing.T) {
	t.Run("insecure=true 跳过 TLS 校验", func(t *testing.T) {
		c := newHTTPClient(true)
		tr, ok := c.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("Transport 类型异常: %T", c.Transport)
		}
		if tr.TLSClientConfig == nil {
			t.Fatal("insecure=true 时 TLSClientConfig 不应为 nil")
		}
		if !tr.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("insecure=true 时 InsecureSkipVerify 应为 true")
		}
	})

	t.Run("insecure=false 不跳过校验", func(t *testing.T) {
		c := newHTTPClient(false)
		tr, ok := c.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("Transport 类型异常: %T", c.Transport)
		}
		if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("insecure=false 时不应跳过 TLS 校验")
		}
	})

	t.Run("Timeout 非零", func(t *testing.T) {
		c := newHTTPClient(false)
		if c.Timeout == 0 {
			t.Fatal("newHTTPClient 的 Timeout 不应为零")
		}
	})
}

// ============================================================================
// printVersion
// ============================================================================

func TestCfgPrintVersion(t *testing.T) {
	oldVersion, oldBuildTime, oldGitCommit := version, buildTime, gitCommit
	version, buildTime, gitCommit = "1.2.3", "2026-01-01", "abc123"
	defer func() {
		version, buildTime, gitCommit = oldVersion, oldBuildTime, oldGitCommit
	}()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printVersion()
	_ = w.Close()
	os.Stdout = old

	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "version=1.2.3") {
		t.Fatalf("printVersion 输出应含 version= 行, got %q", out)
	}
	if !strings.Contains(out, "buildTime=2026-01-01") {
		t.Fatalf("printVersion 输出应含 buildTime= 行, got %q", out)
	}
	if !strings.Contains(out, "gitCommit=abc123") {
		t.Fatalf("printVersion 输出应含 gitCommit= 行, got %q", out)
	}
}

// ============================================================================
// parseFlags
// ============================================================================

// cfgResetEnv 清空 parseFlags 会读取的环境变量,并在测试结束后恢复。
func cfgResetEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"PRINT_AGENT_SERVER", "PRINT_AGENT_TOKEN", "PRINT_AGENT_INTERVAL",
		"PRINT_AGENT_LIMIT", "PRINT_AGENT_WAIT", "PRINT_AGENT_NAME",
		"PRINT_AGENT_STATE", "PRINT_AGENT_LOG", "PRINT_AGENT_INSECURE",
		"PRINT_AGENT_NO_SLEEP", "PRINT_AGENT_VERBOSE",
	}
	saved := map[string]string{}
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
		}
		_ = os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			_ = os.Unsetenv(k)
			if v, ok := saved[k]; ok {
				_ = os.Setenv(k, v)
			}
		}
	})
}

// cfgParseFlags 在隔离的 flag.CommandLine 与 os.Args 下调用 parseFlags。
// 每次调用都重建全局 FlagSet,避免重复注册导致「flag redefined」panic。
func cfgParseFlags(t *testing.T, args ...string) (config, error) {
	t.Helper()
	cfgResetEnv(t)
	return cfgParseFlagsRaw(t, args...)
}

// cfgParseFlagsEnv 在上面叠加环境变量(env 里的键在测试结束时恢复),用于测
// 「同一个开关既能命令行给也能环境变量给」这类配置路径。
func cfgParseFlagsEnv(t *testing.T, env map[string]string, args ...string) (config, error) {
	t.Helper()
	// 先清干净再注入:cfgResetEnv 会 unset 所有 PRINT_AGENT_* ,顺序反了就把注入值冲掉。
	cfgResetEnv(t)
	saved := map[string]string{}
	for k, v := range env {
		if ov, ok := os.LookupEnv(k); ok {
			saved[k] = ov
		}
		_ = os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k := range env {
			_ = os.Unsetenv(k)
			if ov, ok := saved[k]; ok {
				_ = os.Setenv(k, ov)
			}
		}
	})
	return cfgParseFlagsRaw(t, args...)
}

// cfgParseFlagsRaw 只做 flag.CommandLine / os.Args 的隔离,不动环境变量。
func cfgParseFlagsRaw(t *testing.T, args ...string) (config, error) {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("print-agent", flag.ExitOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = append([]string{"print-agent"}, args...)
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	}()
	return parseFlags()
}

func TestCfgSecretsNotEchoedInFlagUsage(t *testing.T) {
	// flag 在用法输出里回显每个开关的默认值。若把 PRINT_AGENT_TOKEN / SERVER 的值直接
	// 当默认值,门店敲错一个参数(或只是 --help)就会把代理令牌明文打印到终端 ——
	// 这种输出经常连同截屏一起进工单,属于实打实的泄露。取值必须改在 Parse 之后补。
	const secret = "SECRET-TOKEN-VALUE-FOR-TEST"
	oldArgs, oldCmdLine := os.Args, flag.CommandLine
	oldToken, hadToken := os.LookupEnv("PRINT_AGENT_TOKEN")
	flag.CommandLine = flag.NewFlagSet("print-agent", flag.ContinueOnError)
	var out strings.Builder
	flag.CommandLine.SetOutput(&out)
	os.Args = []string{"print-agent"}
	_ = os.Setenv("PRINT_AGENT_TOKEN", secret)
	t.Cleanup(func() {
		os.Args, flag.CommandLine = oldArgs, oldCmdLine
		if hadToken {
			_ = os.Setenv("PRINT_AGENT_TOKEN", oldToken)
		} else {
			_ = os.Unsetenv("PRINT_AGENT_TOKEN")
		}
	})

	cfg, err := parseFlags()
	// 本用例只关心「注册与取值」,缺 --server 报错属预期。
	if err == nil && cfg.token != secret {
		t.Fatalf("环境里的令牌应被取到, got %q", cfg.token)
	}
	flag.CommandLine.PrintDefaults()
	if strings.Contains(out.String(), secret) {
		t.Fatalf("用法输出泄露了代理令牌:\n%s", out.String())
	}
	for _, name := range []string{"token", "server"} {
		if f := flag.CommandLine.Lookup(name); f != nil && f.DefValue != "" {
			t.Fatalf("--%s 的默认值不应回显配置内容(当前 DefValue=%q)", name, f.DefValue)
		}
	}
}

func TestCfgParseFlags(t *testing.T) {
	t.Run("缺少 server 报错", func(t *testing.T) {
		_, err := cfgParseFlags(t)
		if err == nil || !strings.Contains(err.Error(), "缺少 --server") {
			t.Fatalf("err=%v, want 缺少 --server", err)
		}
	})

	t.Run("非法 server URL 报错", func(t *testing.T) {
		_, err := cfgParseFlags(t, "--server", "not-a-url", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "不是合法地址") {
			t.Fatalf("err=%v, want 不是合法地址", err)
		}
	})

	t.Run("缺少 token 报错", func(t *testing.T) {
		_, err := cfgParseFlags(t, "--server", "https://example.com")
		if err == nil || !strings.Contains(err.Error(), "缺少 --token") {
			t.Fatalf("err=%v, want 缺少 --token", err)
		}
	})

	t.Run("interval limit wait 越界归一化", func(t *testing.T) {
		cfg, err := cfgParseFlags(t,
			"--server", "https://example.com", "--token", "tok",
			"--interval", "0", "--limit", "0", "--wait", "100")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.interval != time.Second {
			t.Fatalf("interval=%s, want 1s", cfg.interval)
		}
		if cfg.limit != 1 {
			t.Fatalf("limit=%d, want 1", cfg.limit)
		}
		if cfg.wait != 30 {
			t.Fatalf("wait=%d, want 30", cfg.wait)
		}
	})

	t.Run("wait 负值归一化为 0 且 limit 上限 50", func(t *testing.T) {
		cfg, err := cfgParseFlags(t,
			"--server", "https://example.com", "--token", "tok",
			"--interval", "60", "--limit", "100", "--wait", "-5")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.interval != 60*time.Second {
			t.Fatalf("interval=%s, want 60s", cfg.interval)
		}
		if cfg.limit != 50 {
			t.Fatalf("limit=%d, want 50", cfg.limit)
		}
		if cfg.wait != 0 {
			t.Fatalf("wait=%d, want 0", cfg.wait)
		}
	})

	t.Run("--once 强制 wait=0", func(t *testing.T) {
		cfg, err := cfgParseFlags(t,
			"--server", "https://example.com", "--token", "tok",
			"--wait", "25", "--once")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if !cfg.once {
			t.Fatal("once 应为 true")
		}
		if cfg.wait != 0 {
			t.Fatalf("--once 应强制 wait=0, got %d", cfg.wait)
		}
		if !cfg.verbose {
			t.Fatal("--once 是排障自检,应自动开启 verbose")
		}
	})

	t.Run("--verbose 开关与环境变量", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--server", "https://example.com", "--token", "tok")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.verbose {
			t.Fatal("默认不应开启 verbose(空轮次每几秒一条,会刷屏)")
		}

		cfg2, err := cfgParseFlags(t, "--server", "https://example.com", "--token", "tok", "--verbose")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if !cfg2.verbose {
			t.Fatal("--verbose 应开启调试日志")
		}

		cfg3, err := cfgParseFlagsEnv(t, map[string]string{"PRINT_AGENT_VERBOSE": "1"},
			"--server", "https://example.com", "--token", "tok")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if !cfg3.verbose {
			t.Fatal("PRINT_AGENT_VERBOSE=1 应开启调试日志")
		}

		cfg4, err := cfgParseFlagsEnv(t, map[string]string{"PRINT_AGENT_VERBOSE": "0"},
			"--server", "https://example.com", "--token", "tok")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg4.verbose {
			t.Fatal("PRINT_AGENT_VERBOSE=0 不应开启调试日志")
		}
	})

	t.Run("--version 短路无需 server/token", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--version")
		if err != nil {
			t.Fatalf("--version 不应报错, got %v", err)
		}
		if !cfg.showVersion {
			t.Fatal("showVersion 应为 true")
		}
	})

	t.Run("--install 跳过校验", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--install")
		if err != nil {
			t.Fatalf("--install 不应报错, got %v", err)
		}
		if !cfg.install {
			t.Fatal("install 应为 true")
		}
	})

	t.Run("--uninstall 跳过校验", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--uninstall")
		if err != nil {
			t.Fatalf("--uninstall 不应报错, got %v", err)
		}
		if !cfg.uninstall {
			t.Fatal("uninstall 应为 true")
		}
	})

	t.Run("--log 相对路径锚定到程序目录", func(t *testing.T) {
		cfg, err := cfgParseFlags(t,
			"--server", "https://example.com", "--token", "tok",
			"--log", "print-agent.log")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if !filepath.IsAbs(cfg.logPath) {
			t.Fatalf("相对 --log 应被锚定为绝对路径, got %q", cfg.logPath)
		}
		if filepath.Base(cfg.logPath) != "print-agent.log" {
			t.Fatalf("logPath 文件名应保持不变, got %q", cfg.logPath)
		}
	})

	t.Run("--log off 关闭落盘", func(t *testing.T) {
		cfg, err := cfgParseFlags(t,
			"--server", "https://example.com", "--token", "tok",
			"--log", "off")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.logPath != "" {
			t.Fatalf("--log off 应解析为空串, got %q", cfg.logPath)
		}
	})

	t.Run("state 默认路径", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--server", "https://example.com", "--token", "tok")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.state == "" {
			t.Fatal("state 不应为空")
		}
		if filepath.Base(cfg.state) != "print-agent.state" {
			t.Fatalf("state 默认文件名应为 print-agent.state, got %q", cfg.state)
		}
	})
}
