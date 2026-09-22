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
)

var (
	mu    sync.RWMutex
	root  = zap.NewNop()
	sugar = root.Sugar()
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
const defaultLogPath = "./logs/app.log"

// resolveLogPath 求最终日志文件路径:
//   - LOG_PATH 未设置 → 用默认值 ./logs/app.log;
//   - LOG_PATH 显式设为 off/none/false/0/- → 返回空串,表示不落盘(仅控制台)。
func resolveLogPath() string {
	p := strings.TrimSpace(os.Getenv("LOG_PATH"))
	if p == "" {
		p = defaultLogPath
	}
	switch strings.ToLower(p) {
	case "off", "none", "false", "0", "-":
		return ""
	}
	return p
}

func Init() {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
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
	if path := resolveLogPath(); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			// 目录建不出来时不能静默降级为「只写控制台」——否则运维会误以为日志在落盘。
			fmt.Fprintf(os.Stderr, "[logger] 创建日志目录失败,已退化为仅控制台输出: %v\n", err)
		} else {
			fileCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderCfg),
				zapcore.AddSync(&lumberjack.Logger{
					Filename:   path,
					MaxSize:    envInt("LOG_MAX_SIZE_MB", 50),
					MaxAge:     envInt("LOG_MAX_AGE_DAYS", 30),
					MaxBackups: envInt("LOG_MAX_BACKUPS", 7),
					LocalTime:  true,
					Compress:   true,
				}),
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
	mu.Lock()
	defer mu.Unlock()
	root = newRoot
	sugar = newRoot.Sugar()
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

// ---- gin 中间件 ----

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
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		msg := fmt.Sprintf("%3d │ %13v │ %-15s │ %-7s %s", status, latency, c.ClientIP(), c.Request.Method, path)
		if query != "" {
			msg += "?" + query
		}
		if errMsg := c.Errors.ByType(gin.ErrorTypePrivate).String(); errMsg != "" {
			msg += " | " + errMsg
		}
		switch {
		case status >= 500:
			Errorf("%s", msg)
		case status >= 400:
			Warnf("%s", msg)
		default:
			Infof("%s", msg)
		}
	}
}

// GinRecovery 替代 gin.Recovery(): panic 转为 500,并记录 error 级日志含堆栈。
func GinRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, rec any) {
		Errorf("[panic] %v\n%s", rec, debug.Stack())
		c.AbortWithStatus(500)
	})
}
