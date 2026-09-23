// Package logger 提供基于 zap 的全局结构化日志。
//
// 设计要点:
//   - 控制台输出人类可读格式(开发/运维直接看),文件输出 JSON(便于采集到 ELK/Loki);
//   - 文件按大小/天数自动轮转(lumberjack),路径与级别由环境变量控制;
//   - 未调用 Init 时退化为 nop logger,保证测试与工具命令不受影响;
//   - 提供 GinLogger/GinRecovery 中间件,替代 gin.Logger/gin.Recovery,
//     让 HTTP 访问日志与业务日志统一格式、统一落盘。
package logger

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-isatty"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"dining-system/internal/conf"
)

var (
	mu    sync.RWMutex
	root  = zap.NewNop()
	sugar = root.Sugar()
	// fileOnly 只写主日志文件的独立实例(不刷控制台),Init 重建时同步替换;
	// 用于高频轮询接口(门店打印代理 ping/pull/ack)的访问日志——
	// 控制台保持可读,文件保留完整现场供排查与采集。LOG_PATH 关闭落盘时为 nop。
	fileOnly = zap.NewNop().Sugar()
	// agentOnly 写门店打印代理专属日志文件(默认 ./logs/agent.log)的实例;
	// agent 通道的访问日志与业务日志都走这里,主日志不混入高频轮询噪音,
	// 排查「代理为什么拉不到单」时只看这一个文件。LOG_AGENT_PATH=off 时
	// 回落 fileOnly(并入主日志),保证日志不丢。
	agentOnly = zap.NewNop().Sugar()
	// fileSink / agentSink 当前文件输出实例。Init 重建时负责回收上一个——
	// lumberjack 持有的文件句柄不会随 GC 释放,启动期两次 Init
	// (装配部署配置前后各一次)若不回收,旧句柄会一直挂到进程退出。
	fileSink  *lumberjack.Logger
	agentSink *lumberjack.Logger
	// stdoutIsTTY 标记控制台是否为真实终端;重定向到文件/管道时关闭 ANSI 颜色,
	// 避免 journald、nohup 等采集场景里留下乱码转义符。
	stdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
)

// Init 初始化全局日志。必须在任何业务日志输出前调用(通常位于 main 最前)。
//
// 环境变量:
//   - LOG_LEVEL: debug/info/warn/error,默认 info;
//   - LOG_PATH: 日志文件路径,默认 ./logs/app.log(固定落盘);
//     设为 off/none/false/0/- 可关闭文件落盘(仅写控制台);
//   - LOG_MAX_SIZE_MB / LOG_MAX_AGE_DAYS / LOG_MAX_BACKUPS: 轮转策略,默认 50MB/30天/7 个。
//
// 默认落盘的考虑:线上排障往往事后才发生,只写控制台(尤其被 systemd/nohup 接管时)
// 容易丢失现场;固定一个文件路径,运维 tail 即可。生产用 systemd 部署时会在服务
// 单元里显式指定绝对路径(见 deploy/dining-backend.service),避免受工作目录变化影响。
// defaultLogPath 是未配置 LOG_PATH 时的默认落盘路径(相对进程工作目录)。
const defaultLogPath = conf.DefaultLogPath

// resolveLogPath 求最终日志文件路径:
//   - LOG_PATH 未设置 → 用默认值 ./logs/app.log;
//   - LOG_PATH 显式设为 off/none/false/0/- → 返回空串,表示不落盘(仅控制台)。
func resolveLogPath() string {
	p := strings.TrimSpace(os.Getenv(conf.EnvLogPath))
	if p == "" {
		p = defaultLogPath
	}
	switch strings.ToLower(p) {
	case "off", "none", "false", "0", "-":
		return ""
	}
	return p
}

// resolveAgentLogPath 求打印代理专属日志文件路径:
//   - LOG_AGENT_PATH 未设置 → 默认 ./logs/agent.log(与主日志同目录);
//   - 设为 off/none/false/0/- → 返回空串,agent 日志回落并入主日志文件。
//
// 轮转策略复用 LOG_MAX_SIZE_MB / LOG_MAX_AGE_DAYS / LOG_MAX_BACKUPS。
func resolveAgentLogPath() string {
	p := strings.TrimSpace(os.Getenv(conf.EnvLogAgentPath))
	if p == "" {
		p = filepath.Join(filepath.Dir(resolveLogPath()), "agent.log")
	}
	switch strings.ToLower(p) {
	case "off", "none", "false", "0", "-":
		return ""
	}
	return p
}

func Init() {
	level := parseLevel(os.Getenv(conf.EnvLogLevel))
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 控制台: 列式布局「时间 │ 级别 │ 位置 │ 内容」,级别定宽 5 字符并对齐,
	// 颜色区分级别(绿=INFO 黄=WARN 红=ERROR),便于人工快速扫读。
	consoleCfg := encoderCfg
	consoleCfg.ConsoleSeparator = " │ "
	consoleCfg.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(prettyLevel(l))
	}
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(consoleCfg),
		zapcore.AddSync(os.Stdout),
		level,
	)

	cores := []zapcore.Core{consoleCore}

	// 文件: JSON 格式,按大小/时间轮转;默认落盘到 ./logs/app.log,
	// 可用 LOG_PATH=off/none/false/0/- 关闭(此时仅写控制台)。
	var newSink *lumberjack.Logger
	if path := resolveLogPath(); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			// 目录建不出来时不能静默降级为「只写控制台」——否则运维会误以为日志在落盘。
			fmt.Fprintf(os.Stderr, "[logger] 创建日志目录失败,已退化为仅控制台输出: %v\n", err)
		} else {
			newSink = &lumberjack.Logger{
				Filename:   path,
				MaxSize:    envInt(conf.EnvLogMaxSizeMB, conf.DefaultLogMaxSizeMB),
				MaxAge:     envInt(conf.EnvLogMaxAgeDays, conf.DefaultLogMaxAgeDays),
				MaxBackups: envInt(conf.EnvLogMaxBackups, conf.DefaultLogMaxBackups),
				LocalTime:  true,
				Compress:   true,
			}
			fileCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderCfg),
				zapcore.AddSync(newSink),
				level,
			)
			cores = append(cores, fileCore)
		}
	}

	newRoot := zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddCallerSkip(1), // 报告业务调用方位置,而非本包包装函数
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	// fileOnly:仅文件输出(控制台不刷),供高频轮询接口访问日志使用。
	// 未启用文件落盘时退化为 nop——这些日志本来就只服务于文件采集。
	newFileOnly := zap.NewNop().Sugar()
	if newSink != nil {
		fileCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(newSink),
			level,
		)
		newFileOnly = zap.New(
			fileCore,
			zap.AddCaller(),
			zap.AddCallerSkip(1),
		).Sugar()
	}
	// agentOnly:打印代理专属文件(默认 ./logs/agent.log)。未启用时回落
	// fileOnly(并入主日志),绝不静默丢弃。
	newAgentOnly := newFileOnly
	var newAgentSink *lumberjack.Logger
	if path := resolveAgentLogPath(); path != "" && newSink != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] 创建代理日志目录失败,已并入主日志文件: %v\n", err)
		} else {
			newAgentSink = &lumberjack.Logger{
				Filename:   path,
				MaxSize:    envInt(conf.EnvLogMaxSizeMB, conf.DefaultLogMaxSizeMB),
				MaxAge:     envInt(conf.EnvLogMaxAgeDays, conf.DefaultLogMaxAgeDays),
				MaxBackups: envInt(conf.EnvLogMaxBackups, conf.DefaultLogMaxBackups),
				LocalTime:  true,
				Compress:   true,
			}
			agentCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderCfg),
				zapcore.AddSync(newAgentSink),
				level,
			)
			newAgentOnly = zap.New(
				agentCore,
				zap.AddCaller(),
				zap.AddCallerSkip(1),
			).Sugar()
		}
	}
	mu.Lock()
	oldSugar := sugar
	oldSink := fileSink
	oldAgentSink := agentSink
	root = newRoot
	sugar = newRoot.Sugar()
	fileOnly = newFileOnly
	agentOnly = newAgentOnly
	fileSink = newSink
	agentSink = newAgentSink
	mu.Unlock()

	// 回收被替换的旧实例:先刷缓冲再关文件句柄。放在锁外执行,
	// Close 的慢操作(压缩轮转文件等)不阻塞新日志的写入。
	if oldSink != nil {
		_ = oldSugar.Sync()
		if err := oldSink.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] 关闭旧日志文件失败: %v\n", err)
		}
	}
	if oldAgentSink != nil {
		if err := oldAgentSink.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] 关闭旧代理日志文件失败: %v\n", err)
		}
	}
}

// Sync 刷新缓冲,main 退出前调用。
func Sync() {
	mu.RLock()
	defer mu.RUnlock()
	_ = sugar.Sync()
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// prettyLevel 返回定宽 5 字符的级别标签;真实终端下附加 ANSI 颜色,
// 重定向输出时返回纯文本,保证各列对齐且无乱码转义符。
func prettyLevel(l zapcore.Level) string {
	var color, name string
	switch l {
	case zapcore.DebugLevel:
		color, name = "\x1b[36m", "DEBUG"
	case zapcore.WarnLevel:
		color, name = "\x1b[33m", "WARN "
	case zapcore.ErrorLevel:
		color, name = "\x1b[31m", "ERROR"
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		color, name = "\x1b[31m", "FATAL"
	default:
		color, name = "\x1b[32m", "INFO "
	}
	if !stdoutIsTTY {
		return name
	}
	return color + name + "\x1b[0m"
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// ---- 包装函数: 统一加 caller skip,业务代码直接 logger.Infof(...) 即可 ----

func Debugf(template string, args ...any) {
	mu.RLock()
	s := sugar
	mu.RUnlock()
	s.Debugf(template, args...)
}
func Infof(template string, args ...any) {
	mu.RLock()
	s := sugar
	mu.RUnlock()
	s.Infof(template, args...)
}
func Warnf(template string, args ...any) {
	mu.RLock()
	s := sugar
	mu.RUnlock()
	s.Warnf(template, args...)
}
func Errorf(template string, args ...any) {
	mu.RLock()
	s := sugar
	mu.RUnlock()
	s.Errorf(template, args...)
}

// Fatalf 记录 error 级日志(含堆栈)后 os.Exit(1),语义同 log.Fatalf。
func Fatalf(template string, args ...any) {
	mu.RLock()
	s := sugar
	mu.RUnlock()
	s.Fatalf(template, args...)
}

// Agentf 系列:门店打印代理专属日志(默认写入 ./logs/agent.log,不刷控制台)。
// agent 通道的访问日志与业务日志统一走这里,主日志文件不混入高频轮询噪音;
// LOG_AGENT_PATH=off 时回落并入主日志文件,绝不静默丢弃。
func agentLogf(level zapcore.Level, template string, args ...any) {
	mu.RLock()
	s := agentOnly
	mu.RUnlock()
	switch level {
	case zapcore.DebugLevel:
		s.Debugf(template, args...)
	case zapcore.WarnLevel:
		s.Warnf(template, args...)
	case zapcore.ErrorLevel:
		s.Errorf(template, args...)
	default:
		s.Infof(template, args...)
	}
}

func AgentDebugf(template string, args ...any) { agentLogf(zapcore.DebugLevel, template, args...) }
func AgentInfof(template string, args ...any)  { agentLogf(zapcore.InfoLevel, template, args...) }
func AgentWarnf(template string, args ...any)  { agentLogf(zapcore.WarnLevel, template, args...) }
func AgentErrorf(template string, args ...any) { agentLogf(zapcore.ErrorLevel, template, args...) }

// ---- gin 中间件 ----

// requestID 从 gin.Context 读取雪花 requestID(值类型 int64),返回十进制字符串;
// 不存在或类型不符时返回空串。这里内联读取而非依赖 middleware 包,
// 避免基础设施层反向依赖 HTTP 中间件层。
func requestID(c *gin.Context) string {
	if v, ok := c.Get("requestID"); ok {
		if id, ok := v.(int64); ok {
			return strconv.FormatInt(id, 10)
		}
	}
	return ""
}

// 静态资源路径不打访问日志,避免刷屏;静态文件缺失已有 [static] 告警覆盖。
var skipPrefixes = []string{"/uploads", "/static", "/assets", "/favicon"}

func skipPath(p string) bool {
	for _, pre := range skipPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// agentPathPrefixes 高频轮询接口前缀:门店打印代理每 3~25 秒一轮
// ping/pull/ack,访问日志只落文件、不刷控制台(完整现场文件里仍有)。
var agentPathPrefixes = []string{"/api/agent/"}

func isAgentPath(p string) bool {
	for _, pre := range agentPathPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// sensitiveQueryKeyParts 与审计脱敏共用同一思路:键名包含任意片段即视为敏感。
//
// "key" 是极短词,子串匹配会误伤 monkey/whiskey 等无关键,但访问日志脱敏同样
// 宁可多脱不可漏脱 —— 少几个可见参数不影响排障,漏掉令牌就是泄露通道。
var sensitiveQueryKeyParts = []string{
	"token", "key", "secret", "password", "passwd", "pwd", "ukey", "apiv3",
	"credential", "cert",
}

// maskQuery 对 URL query 串做键名脱敏:敏感参数的值统一替换为 *** 后再拼接。
//
// GinLogger 之前原样拼接 RawQuery,依赖的是「当前接口不用 query 传令牌」这一
// 约定而非保证;一旦未来有人把 token/key 放进 query,访问日志就成了泄露入口。
// ParseQuery 失败时原样返回 —— 日志宁可不脱敏,也不能悄悄丢掉出问题的原始串,
// 否则「为什么这个请求是畸形的」会无从排查。
func maskQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	for k, vs := range values {
		if isSensitiveQueryKey(k) {
			for i := range vs {
				vs[i] = "***"
			}
		}
	}
	return values.Encode()
}

func isSensitiveQueryKey(key string) bool {
	k := strings.ToLower(key)
	for _, p := range sensitiveQueryKeyParts {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// GinLogger 替代 gin.Logger(): 统一输出到 zap。
// 5xx 记 Error、4xx 记 Warn、其余记 Info,并带上耗时/客户端 IP/错误信息。
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		if skipPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		fileOnly := isAgentPath(path)
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		msg := fmt.Sprintf("%3d │ %13v │ %-15s │ %-7s %s", status, latency, c.ClientIP(), c.Request.Method, path)
		if query != "" {
			msg += "?" + maskQuery(query)
		}
		if errMsg := c.Errors.ByType(gin.ErrorTypePrivate).String(); errMsg != "" {
			msg += " | " + errMsg
		}
		// requestID 挂在列尾:排障时用响应头 X-Request-ID 反查这条访问日志。
		if rid := requestID(c); rid != "" {
			msg += " | " + rid
		}
		switch {
		case status >= 500:
			if fileOnly {
				AgentErrorf("%s", msg)
			} else {
				Errorf("%s", msg)
			}
		case status >= 400:
			if fileOnly {
				AgentWarnf("%s", msg)
			} else {
				Warnf("%s", msg)
			}
		default:
			if fileOnly {
				AgentInfof("%s", msg)
			} else {
				Infof("%s", msg)
			}
		}
	}
}

// GinRecovery 替代 gin.Recovery(): panic 转为 500,并记录 error 级日志含堆栈。
// 日志带上 requestID,panic 现场可直接关联到具体请求。
func GinRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, rec any) {
		Errorf("[panic][requestID=%s] %v\n%s", requestID(c), rec, debug.Stack())
		c.AbortWithStatus(500)
	})
}
