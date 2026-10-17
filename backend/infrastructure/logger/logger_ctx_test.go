package logger

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"dining-system/infrastructure/conf"
)

// ============================================================================
// 测试脚手架:把全局 logger 换成写内存缓冲的实例,以便断言日志内容
// ============================================================================

// syncBuffer 是并发安全的内存 WriteSyncer,充当 zapcore 的输出目标。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) Sync() error { return nil }

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLogs 在 fn 执行期间把主日志通道指向内存缓冲,返回期间产生的全部日志
// (JSON 每行一条)。defer 里恢复全局状态,避免污染后续用例与其它包的测试。
func captureLogs(fn func()) string {
	main, _ := captureLogsBoth(fn)
	return main()
}

// captureLogsBoth 同时捕获主日志与代理日志两个通道,分别返回。
// 用于验证「agent 通道的 ctx 日志确实走 agent 日志、不混进主日志」。
func captureLogsBoth(fn func()) (mainOut func() string, agentOut func() string) {
	var mainBuf, agentBuf syncBuffer
	cfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		MessageKey:     "msg",
		CallerKey:      "caller",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.EpochTimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	newPair := func(buf *syncBuffer) (*zap.Logger, *zap.Logger) {
		core := zapcore.NewCore(zapcore.NewJSONEncoder(cfg), buf, zapcore.DebugLevel)
		return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel)),
			zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	}
	mainL, mainRaw := newPair(&mainBuf)
	agentL, agentRawLogger := newPair(&agentBuf)

	mu.Lock()
	oldRoot, oldRaw, oldSugar := root, rootRaw, sugar
	oldAgentOnly, oldAgentRaw := agentOnly, agentRaw
	oldFileOnly, oldFileRaw := fileOnly, fileRaw
	root, rootRaw, sugar = mainL, mainRaw, mainL.Sugar()
	agentOnly, agentRaw = agentL.Sugar(), agentRawLogger
	fileOnly, fileRaw = agentOnly, agentRawLogger
	mu.Unlock()
	defer func() {
		mu.Lock()
		root, rootRaw, sugar = oldRoot, oldRaw, oldSugar
		agentOnly, agentRaw = oldAgentOnly, oldAgentRaw
		fileOnly, fileRaw = oldFileOnly, oldFileRaw
		mu.Unlock()
	}()

	fn()
	return func() string { return mainBuf.String() }, func() string { return agentBuf.String() }
}

// ============================================================================
// requestID / context 关联
// ============================================================================

// TestContextRequestIDRoundTrip 契约:注入 context 的 requestID 能被原样读回,
// 且没有注入时返回 false(而不是 0 被当成有效 ID)。
func TestContextRequestIDRoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxRequestIDKey{}, int64(12345))
	if id, ok := RequestIDFromContext(ctx); !ok || id != 12345 {
		t.Fatalf("RequestIDFromContext = (%d,%v), want (12345,true)", id, ok)
	}
	if id, ok := RequestIDFromContext(context.Background()); ok || id != 0 {
		t.Errorf("空 context 应返回 (0,false), got (%d,%v)", id, ok)
	}
	// nil context 不得 panic —— 后台任务/定时任务常直接传 nil。
	if _, ok := RequestIDFromContext(nil); ok {
		t.Error("nil context 应返回 false")
	}
	// ContextWithRequestID 兼容 nil:返回可用的 Background 派生 ctx。
	if ctx := ContextWithRequestID(nil, 7); ctx == nil {
		t.Fatal("ContextWithRequestID(nil,...) 不应返回 nil")
	} else if id, ok := RequestIDFromContext(ctx); !ok || id != 7 {
		t.Errorf("nil 基 context 注入后读得 (%d,%v), want (7,true)", id, ok)
	}
}

// TestCtxLogCarriesRequestID 业务日志带 requestID —— 这是「按请求串联日志」的核心契约。
// 排障时拿到响应头 X-Request-ID,要能 grep 出这次请求在 service/dao 里打的每一条日志。
func TestCtxLogCarriesRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(9527))

	out := captureLogs(func() {
		CtxInfof(ctx, "[pay] 支付成功 订单%s", "N001")
	})

	if !strings.Contains(out, `"requestID":9527`) {
		t.Errorf("ctx 日志应带 requestID 字段,实际输出: %s", out)
	}
	if !strings.Contains(out, "支付成功") {
		t.Errorf("日志内容丢失,实际输出: %s", out)
	}
	// caller 应指向本测试文件,而不是 logger 包的实现文件(logger.go)——
	// 报错位置会让「这条日志是哪个业务文件打的」失效,排查时等于少了半个线索。
	if strings.Contains(out, `"caller":"logger/logger.go`) {
		t.Errorf("caller 错报成 logger 包内部,skip 未算对: %s", out)
	}
	if !strings.Contains(out, `logger_ctx_test.go:`) {
		t.Errorf("caller 应指向业务调用方(本测试文件),实际: %s", out)
	}
}

// TestCtxLogWithoutRequestID ctx 无 requestID 时退化为普通日志:
// 不能因为取不到 ID 就把整条日志丢掉(后台任务没有请求上下文是常态)。
func TestCtxLogWithoutRequestID(t *testing.T) {
	out := captureLogs(func() {
		CtxWarnf(context.Background(), "[cleanup] 清理任务完成")
	})
	if !strings.Contains(out, "清理任务完成") {
		t.Fatalf("无 requestID 时日志不应丢失,实际输出: %s", out)
	}
	if strings.Contains(out, "requestID") {
		t.Errorf("无 requestID 的 ctx 不应输出空 requestID 字段: %s", out)
	}
}

// TestWithContextAppendsFields WithContext 返回的 logger 应带 requestID,
// 且业务代码可以继续 .With(...) 追加自己的字段。
func TestWithContextAppendsFields(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(555))

	out := captureLogs(func() {
		WithContext(ctx).With(zap.String("orderNo", "N002")).Infof("[pay] 退款入账")
		// 无 requestID 的通道不应报错、不应带字段。
		WithContext(context.Background()).Infof("[cron] 定时任务")
	})

	if !strings.Contains(out, `"requestID":555`) {
		t.Errorf("WithContext 应带 requestID,实际: %s", out)
	}
	if !strings.Contains(out, `"orderNo":"N002"`) {
		t.Errorf("业务追加字段丢失,实际: %s", out)
	}
	if !strings.Contains(out, "[cron] 定时任务") {
		t.Errorf("无 requestID 时不应丢日志,实际: %s", out)
	}
}

// ============================================================================
// With 能力:各级别携带 requestID + 业务字段
// ============================================================================

// TestWithContextFieldsAllLevels 核心契约:WithContext 返回的 logger 在
// 每一个级别上都带着 requestID 与业务字段 —— 这是「全链路追踪」得以成立的
// 前提。任一级别漏带字段,按 requestID grep 出来的链路就会断一截。
func TestWithContextFieldsAllLevels(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(2468))

	out := captureLogs(func() {
		log := WithContext(ctx, zap.String("orderNo", "N003"))
		log.Debugf("[pay] 开始处理")
		log.Infof("[pay] 支付成功")
		log.Warnf("[pay] 金额不一致")
		log.Errorf("[pay] 回写失败")
	})

	// 4 个级别各自一行,且每行都同时带 requestID 与 orderNo。
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("应有 4 行日志(Debug 级别已放行),实际 %d 行: %s", len(lines), out)
	}
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, line := range lines {
		if !strings.Contains(line, `"level":"`+levels[i]+`"`) {
			t.Errorf("第 %d 行级别应为 %s,实际: %s", i+1, levels[i], line)
		}
		if !strings.Contains(line, `"requestID":2468`) {
			t.Errorf("第 %d 行缺 requestID: %s", i+1, line)
		}
		if !strings.Contains(line, `"orderNo":"N003"`) {
			t.Errorf("第 %d 行缺业务字段 orderNo: %s", i+1, line)
		}
		// 每个级别的 caller 都要指向业务调用方,不能是 logger 包内部。
		if strings.Contains(line, `"caller":"logger/logger.go`) {
			t.Errorf("第 %d 行 caller 错报: %s", i+1, line)
		}
	}
}

// TestCtxLoggerReuseFields 同一个 WithContext logger 连打多条:
// 字段只需声明一次,避免每条日志重复拼 orderNo/requestID。
func TestCtxLoggerReuseFields(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(1357))

	out := captureLogs(func() {
		log := WithContext(ctx, zap.String("orderNo", "N004"), zap.String("channel", "wxpay"))
		log.Infof("创建支付单")
		log.Infof("等待回调")
	})

	if n := strings.Count(out, `"requestID":1357`); n != 2 {
		t.Errorf("两条日志都应带 requestID,实际 %d 处: %s", n, out)
	}
	if n := strings.Count(out, `"orderNo":"N004"`); n != 2 {
		t.Errorf("两条日志都应带 orderNo,实际 %d 处: %s", n, out)
	}
	if n := strings.Count(out, `"channel":"wxpay"`); n != 2 {
		t.Errorf("两条日志都应带 channel,实际 %d 处: %s", n, out)
	}
}

// TestWithFieldsNoContext 无请求上下文(定时任务/启动流程)时,
// With 应只带业务字段、不带 requestID,且不丢日志。
func TestWithFieldsNoContext(t *testing.T) {
	out := captureLogs(func() {
		With(zap.String("job", "cleanup"), zap.Int("deleted", 12)).Infof("[cron] 清理过期日志完成")
	})
	if !strings.Contains(out, `"job":"cleanup"`) || !strings.Contains(out, `"deleted":12`) {
		t.Errorf("With 未带上业务字段: %s", out)
	}
	if strings.Contains(out, "requestID") {
		t.Errorf("无 ctx 时不应出现 requestID 字段: %s", out)
	}
}

// TestWithNoFields 不传字段时 With/WithContext 应退化为普通 logger,
// 而不是返回一个「字段为空但多套一层」的实例(浪费一次 clone)。
func TestWithNoFields(t *testing.T) {
	out := captureLogs(func() {
		With().Infof("[cron] 无字段日志")
		WithContext(context.Background()).Infof("[cron] 无字段无 ctx")
	})
	if n := strings.Count(strings.TrimSpace(out), "\n"); n != 1 {
		t.Errorf("应有两行日志,实际: %s", out)
	}
	if strings.Contains(out, "requestID") {
		t.Errorf("无字段时不应出现 requestID: %s", out)
	}
}

// TestCtxLogfAllLevelsHaveRequestID CtxXxx 系列每个级别都要带 requestID。
// 与上一个用例的区别:这条走的是「单条日志」路径,上一条走「复用 logger」路径。
func TestCtxLogfAllLevelsHaveRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(8642))

	out := captureLogs(func() {
		CtxDebugf(ctx, "[pay] debug")
		CtxInfof(ctx, "[pay] info")
		CtxWarnf(ctx, "[pay] warn")
		CtxErrorf(ctx, "[pay] error")
	})

	if n := strings.Count(out, `"requestID":8642`); n != 4 {
		t.Errorf("4 个级别都应带 requestID,实际 %d 处: %s", n, out)
	}
	for _, lvl := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(out, `"level":"`+lvl+`"`) {
			t.Errorf("缺少 %s 级日志: %s", lvl, out)
		}
	}
}

// TestCtxAgentLogGoesToAgentChannel 代理通道的 ctx 日志必须落在 agent 日志里,
// 且不混入主日志 —— 否则高频轮询会把主日志淹掉。
func TestCtxAgentLogGoesToAgentChannel(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), int64(1122))

	main, agent := captureLogsBoth(func() {
		CtxAgentInfof(ctx, "[agent] 代理拉取任务 %d", 3)
		AgentWithContext(ctx, zap.String("agentID", "A1")).Warnf("[agent] 心跳超时")
	})

	agentOut, mainOut := agent(), main()
	if !strings.Contains(agentOut, `"requestID":1122`) {
		t.Errorf("agent 日志应带 requestID,实际: %s", agentOut)
	}
	if !strings.Contains(agentOut, `"agentID":"A1"`) {
		t.Errorf("agent 日志应带业务字段,实际: %s", agentOut)
	}
	if strings.TrimSpace(mainOut) != "" {
		t.Errorf("agent 通道日志不应混入主日志,主日志实际: %s", mainOut)
	}
}

// TestCtxFatalfExitsWithRequestID CtxFatalf 既要真的终止进程,
// 又要在终止前把带 requestID 的 FATAL 日志写下去 —— 少任何一半都失去了意义:
// 不终止就不是 Fatal 语义,不写日志则启动失败时将无从查起。
//
// Fatal 必然 os.Exit(1),无法在测试进程内直接触发(会连带杀掉整个测试),
// 故用「重跑自身 + 环境变量分流」的子进程方式验证(Go 官方测 os.Exit 的常规做法)。
func TestCtxFatalfExitsWithRequestID(t *testing.T) {
	const subEnv = "LOGGER_FATAL_SUBPROCESS"
	logPath := filepath.Join(t.TempDir(), "app.log")

	if os.Getenv(subEnv) == "1" {
		// ---- 子进程分支 ----
		os.Setenv(conf.EnvLogPath, os.Getenv("LOGGER_FATAL_LOGPATH"))
		Init()
		ctx := ContextWithRequestID(context.Background(), int64(9999))
		CtxFatalf(ctx, "[boot] 启动失败 %s", "配置缺失")
		return // 上一行不会返回,进程在此终止
	}

	// ---- 父进程分支 ----
	cmd := exec.Command(os.Args[0], "-test.run=TestCtxFatalfExitsWithRequestID")
	cmd.Env = append(os.Environ(), subEnv+"=1", "LOGGER_FATAL_LOGPATH="+logPath)
	err := cmd.Run()

	// 1. 进程必须非零退出:Fatal 的语义就是终止。
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("CtxFatalf 应让进程以非零码退出,实际 err=%v", err)
	}

	// 2. 退出前必须已落盘 FATAL 日志,且带 requestID 与完整内容。
	raw, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("读取子进程日志失败(日志未落盘?): %v", readErr)
	}
	s := string(raw)
	if !strings.Contains(s, `"level":"FATAL"`) {
		t.Errorf("应有 FATAL 级别日志,实际: %s", s)
	}
	if !strings.Contains(s, `"requestID":9999`) {
		t.Errorf("Fatal 日志也应带 requestID,实际: %s", s)
	}
	if !strings.Contains(s, "配置缺失") {
		t.Errorf("Fatal 日志内容丢失: %s", s)
	}
}

// ============================================================================
// 访问日志脱敏:防泄露的唯一防线,此前零覆盖
// ============================================================================

// TestMaskQuerySensitiveValues 敏感 query 参数的值不得出现在访问日志里。
// 断言用「原值不出现 + 出现掩码」双向校验:只查掩码可能漏掉漏脱的分支。
func TestMaskQuerySensitiveValues(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"令牌", "token=abc123secret"},
		{"密钥", "secret=sk-live-xxxx"},
		{"口令", "password=admin123"},
		{"用户标识", "openid=oAbc123XYZ"},
		{"手机号", "phone=13800138000"},
		{"签名", "signature=9A0B8C"},
		{"大小写混合", "Access_Token=zzz"},
		{"嵌套键名", "wxpay[key]=v"},
	}
	for _, c := range cases {
		got := maskQuery(c.raw)
		// 取出原值部分(= 号右边)逐一比对。
		val := c.raw[strings.Index(c.raw, "=")+1:]
		if strings.Contains(got, val) {
			t.Errorf("%s 未脱敏:maskQuery(%q) = %q,原值 %q 仍在输出中", c.name, c.raw, got, val)
		}
		// 掩码是 ***,经 url.Values.Encode() 后为 %2A%2A%2A。
		if !strings.Contains(got, "%2A") && !strings.Contains(got, "***") {
			t.Errorf("%s 未打掩码:maskQuery(%q) = %q", c.name, c.raw, got)
		}
	}
}

// TestMaskQueryKeepsNormalParams 脱敏不能顺手把正常参数也吞掉 ——
// 访问日志是排障主入口,把订单号/金额都脱掉就废了。
func TestMaskQueryKeepsNormalParams(t *testing.T) {
	got := maskQuery("orderNo=N001&amount=1200&page=2")
	for _, want := range []string{"orderNo=N001", "amount=1200", "page=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("正常参数被误脱敏:maskQuery = %q,缺少 %q", got, want)
		}
	}
}

// TestMaskQueryMalformedRaw 畸形 query 原样返回:日志宁可不脱敏,
// 也不能悄悄丢掉「这个请求为什么是畸形的」这条线索。
func TestMaskQueryMalformedRaw(t *testing.T) {
	const raw = "a=%zz&b=1"
	if got := maskQuery(raw); got != raw {
		t.Errorf("畸形 query 应原样返回, got %q want %q", got, raw)
	}
	if got := maskQuery(""); got != "" {
		t.Errorf("空串应返回空串, got %q", got)
	}
}

// ============================================================================
// Gin 中间件:状态码分级、requestID 关联、panic 恢复
// ============================================================================

// newLogTestRouter 构造挂了 RequestID + 待测中间件的最小引擎。
// withPanic 为 true 时 handler 故意 panic,用于验证 Recovery。
func newLogTestRouter(mw gin.HandlerFunc, status int, withPanic bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(GinRequestIDKey, int64(7788)) }, mw)
	r.GET("/t", func(c *gin.Context) {
		if withPanic {
			panic("boom")
		}
		c.Status(status)
	})
	return r
}

// serveLogTest 跑一次请求,返回期间产生的日志。
func serveLogTest(mw gin.HandlerFunc, status int, withPanic bool) (string, int) {
	r := newLogTestRouter(mw, status, withPanic)
	var code int
	out := captureLogs(func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
		code = w.Code
	})
	return out, code
}

// TestGinLoggerStatusToLevel 访问日志分级:5xx→ERROR、4xx→WARN、其余→INFO。
// 这条映射是运维按级别配告警的依据,错了就漏报或噪音刷屏。
func TestGinLoggerStatusToLevel(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{200, "INFO"},
		{302, "INFO"},
		{400, "WARN"},
		{404, "WARN"},
		{500, "ERROR"},
		{503, "ERROR"},
	}
	for _, c := range cases {
		out, _ := serveLogTest(GinLogger(), c.status, false)
		if !strings.Contains(out, `"level":"`+c.want+`"`) {
			t.Errorf("状态码 %d 应记 %s,实际输出: %s", c.status, c.want, out)
		}
		if !strings.Contains(out, "/t") {
			t.Errorf("访问日志应含路径,实际: %s", out)
		}
		// msg 必须是自带结果的摘要:「GET /t <status> <耗时>」,
		// 否则日志文件里 msg 只剩半截请求行,人读不到成败与快慢。
		if !strings.Contains(out, `"msg":"GET /t `+strconv.Itoa(c.status)+` `) {
			t.Errorf("msg 应为「GET /t %d 耗时」摘要,实际: %s", c.status, out)
		}
	}
}

// TestGinLoggerCarriesRequestID 访问日志必须带 requestID,
// 否则「用响应头 X-Request-ID 反查这次请求」这条排障路径不成立。
func TestGinLoggerCarriesRequestID(t *testing.T) {
	out, _ := serveLogTest(GinLogger(), 200, false)
	if !strings.Contains(out, "7788") {
		t.Errorf("访问日志应带 requestID 7788,实际: %s", out)
	}
}

// TestGinLoggerCarriesUser 已登录请求的访问日志必须带操作人 user 字段 ——
// 否则「谁改坏了数据」在访问日志里查不到人,只能事后翻审计表。
// 未登录(键未写入)时则不应出现该字段。
func TestGinLoggerCarriesUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(setUser bool) string {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(GinRequestIDKey, int64(7788)) },
			func(c *gin.Context) {
				if setUser {
					c.Set(GinUserKey, "张三")
				}
			},
			GinLogger())
		r.GET("/t", func(c *gin.Context) { c.Status(200) })
		return captureLogs(func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
		})
	}
	if out := run(true); !strings.Contains(out, `"user":"张三"`) {
		t.Errorf("已登录请求的访问日志应带 user=张三,实际: %s", out)
	} else if i, j := strings.Index(out, `"user"`), strings.Index(out, `"method"`); i < 0 || j < 0 || i > j {
		t.Errorf("user 应排在 method 之前(紧跟 msg),实际: %s", out)
	}
	if out := run(false); strings.Contains(out, `"user"`) {
		t.Errorf("未登录请求的访问日志不应带 user 字段,实际: %s", out)
	}
}

// TestGinLoggerSkipsStatic 静态资源不打访问日志(否则每次页面加载几十条,淹掉业务日志)。
func TestGinLoggerSkipsStatic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinLogger())
	r.GET("/static/*p", func(c *gin.Context) { c.Status(200) })
	out := captureLogs(func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("静态资源不应产生访问日志,实际: %s", out)
	}
}

// TestGinRecoveryLogsPanic panic 必须:1) 转 500 而不是让连接断掉;
// 2) 记录含 requestID 与堆栈的 ERROR 日志 —— 否则线上 panic 无从查起。
func TestGinRecoveryLogsPanic(t *testing.T) {
	out, code := serveLogTest(GinRecovery(), 0, true)
	if code != http.StatusInternalServerError {
		t.Errorf("panic 应转 500,实际 %d", code)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("panic 日志应含 panic 值,实际: %s", out)
	}
	if !strings.Contains(out, "7788") {
		t.Errorf("panic 日志应含 requestID,实际: %s", out)
	}
	if !strings.Contains(out, `"level":"ERROR"`) {
		t.Errorf("panic 应记 ERROR 级,实际: %s", out)
	}
	// 堆栈是定位 panic 位置的关键,AddStacktrace(ErrorLevel) 必须生效。
	if !strings.Contains(out, "goroutine") && !strings.Contains(out, `"stack"`) {
		t.Errorf("panic 日志应含堆栈,实际: %s", out)
	}
}

// ============================================================================
// 生命周期
// ============================================================================

// TestSyncFlushesAllChannels Sync 要刷三个通道(主日志/仅文件/代理日志),
// 任一漏刷都会丢掉进程退出前的最后几条 —— 恰恰是最关键的那些。
// 这里不断言输出内容(未 Init 时是 nop),只保证不 panic、不阻塞。
func TestSyncFlushesAllChannels(t *testing.T) {
	captureLogs(func() {
		Sync()
		CtxInfof(context.Background(), "Sync 之后仍可正常打日志")
	})
}

// TestGinContextKeyMatches 契约守护:logger 内联读取的 gin.Context 键
// 必须与 middleware 写入的键同源(middleware.GinContextKey 直接引用本常量),
// 防止将来有人在 logger 里改写字面量导致日志静默丢 requestID。
func TestGinContextKeyMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(GinRequestIDKey, int64(42))
	if got := requestID(c); got != "42" {
		t.Errorf("requestID(c) = %q, want %q", got, "42")
	}
	// 类型不符(有人塞了 string)时应返回空串而不是猜测。
	c.Set(GinRequestIDKey, "not-an-int64")
	if got := requestID(c); got != "" {
		t.Errorf("类型不符应返回空串, got %q", got)
	}
}
