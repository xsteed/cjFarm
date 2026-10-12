// Package logger 提供基于 zap 的全局结构化日志。
//
// 设计要点:
//   - 控制台输出人类可读格式(开发/运维直接看),文件输出 JSON(便于采集到 ELK/Loki);
//   - 文件按大小/天数自动轮转(lumberjack),路径与级别由环境变量控制;
//   - 未调用 Init 时退化为 nop logger,保证测试与工具命令不受影响;
//   - 提供 GinLogger/GinRecovery 中间件,替代 gin.Logger/gin.Recovery,
//     让 HTTP 访问日志与业务日志统一格式、统一落盘;
//   - 全链路追踪:每个日志级别都支持携带 context 里的 requestID 与业务字段 ——
//     单条用 CtxXxx(ctx,...),多条或需连打用 WithContext(ctx, fields...),
//     无请求上下文的定时任务用 With(fields...)。
package logger

import (
	"context"
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

	"dining-system/infrastructure/conf"
)

var (
	mu   sync.RWMutex
	root = zap.NewNop()
	// rootRaw 与 root 输出目标完全相同,唯一区别是不加 AddCallerSkip(1)。
	// root 供本包包装函数(Infof 等)使用——它们多一层调用栈,要 skip 掉才能
	// 把 caller 报到业务代码;WithContext 把 logger 直接交给业务代码调用,
	// 调用栈里没有这层包装,再 skip 就会错报成业务代码的调用方。
	// 两者分开构造(而非 WithOptions 负 skip),避免依赖 zap 对负 skip 的处理。
	rootRaw = zap.NewNop()
	sugar   = root.Sugar()
	// fileOnly 只写主日志文件的独立实例(不刷控制台),Init 重建时同步替换;
	// 用于高频轮询接口(门店打印代理 ping/pull/ack)的访问日志——
	// 控制台保持可读,文件保留完整现场供排查与采集。LOG_PATH 关闭落盘时为 nop。
	fileOnly = zap.NewNop().Sugar()
	// fileRaw 是 fileOnly 的 *zap.Logger 形态(不带 caller skip),
	// 供 With / WithContext 派生子 logger 时保证 caller 仍指向业务代码。
	fileRaw = zap.NewNop()
	// agentOnly 写门店打印代理专属日志文件(默认 ./logs/agent.log)的实例;
	// agent 通道的访问日志与业务日志都走这里,主日志不混入高频轮询噪音,
	// 排查「代理为什么拉不到单」时只看这一个文件。LOG_AGENT_PATH=off 时
	// 回落 fileOnly(并入主日志),保证日志不丢。
	agentOnly = zap.NewNop().Sugar()
	// agentRaw 同理,是 agentOnly 的 *zap.Logger 形态。
	agentRaw = zap.NewNop()
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
		// 时间用本地可读格式(2026-10-13 02:26:43.986),与控制台保持一致。
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

	// cores 可被多个 logger 实例共享:zapcore.Core 是并发安全的。
	newRoot := zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddCallerSkip(1), // 报告业务调用方位置,而非本包包装函数
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	// rootRaw:同输出、无 skip,供 WithContext 交给业务代码直接调用。
	newRootRaw := zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	// fileOnly:仅文件输出(控制台不刷),供高频轮询接口访问日志使用。
	// 未启用文件落盘时退化为 nop——这些日志本来就只服务于文件采集。
	newFileOnly := zap.NewNop().Sugar()
	newFileRaw := zap.NewNop()
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
			zap.AddStacktrace(zapcore.ErrorLevel),
		).Sugar()
		newFileRaw = zap.New(
			fileCore,
			zap.AddCaller(),
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
	}
	// agentOnly:打印代理专属文件(默认 ./logs/agent.log)。未启用时回落
	// fileOnly(并入主日志),绝不静默丢弃。
	// fileRaw 与它同步回落,保证 CtxAgentXxx 在回落时同样不丢日志。
	newAgentOnly := newFileOnly
	newAgentRaw := newFileRaw
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
				zap.AddStacktrace(zapcore.ErrorLevel),
			).Sugar()
			newAgentRaw = zap.New(
				agentCore,
				zap.AddCaller(),
				zap.AddStacktrace(zapcore.ErrorLevel),
			)
		}
	}
	mu.Lock()
	oldSugar := sugar
	oldFileOnly := fileOnly
	oldAgentOnly := agentOnly
	oldSink := fileSink
	oldAgentSink := agentSink
	root = newRoot
	rootRaw = newRootRaw
	sugar = newRoot.Sugar()
	fileOnly = newFileOnly
	fileRaw = newFileRaw
	agentOnly = newAgentOnly
	agentRaw = newAgentRaw
	fileSink = newSink
	agentSink = newAgentSink
	mu.Unlock()

	// 回收被替换的旧实例:先刷缓冲再关文件句柄。放在锁外执行,
	// Close 的慢操作(压缩轮转文件等)不阻塞新日志的写入。
	if oldSink != nil {
		// 三个通道都要刷:fileOnly/agentOnly 与被替换的主 logger 持有
		// 同一个 sink,但各自有独立的 zap 缓冲,漏刷会丢最后几条日志。
		_ = oldSugar.Sync()
		_ = oldFileOnly.Sync()
		_ = oldAgentOnly.Sync()
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
// 三个输出通道(主日志 / 仅文件 / 代理日志)都要刷,否则进程退出时
// 可能丢掉最后几条尚未落盘的日志 —— 恰恰是最关键的那些(Fatal / 关闭异常)。
func Sync() {
	mu.RLock()
	defer mu.RUnlock()
	_ = sugar.Sync()
	_ = fileOnly.Sync()
	_ = agentOnly.Sync()
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

// ============================================================================
// requestID 关联:让业务日志能按请求串联
// ============================================================================
//
// 包级 Infof/Warnf/... 不带 requestID,只能靠时间戳与访问日志人工对齐。
// 排障时真正的痛点是「某笔支付失败那次请求,service/dao 里都打了什么」,
// 因此这里提供 ctx 版本:middleware.RequestID 已把雪花 ID 注入 context,
// 业务代码把 ctx 传进来即可自动带上 requestID 字段。
//
// 键定义在 logger 包而非 middleware 包:middleware 单向依赖 logger 注入值,
// logger 不反向依赖 middleware,避免循环。

// ctxRequestIDKey 是 requestID 在 context.Context 里的键。
// 用私有结构体类型而非字符串:防止业务代码用同名字符串键把值顶掉。
type ctxRequestIDKey struct{}

// ContextWithRequestID 把 requestID 注入 context,返回派生 ctx。
// 由 middleware.RequestID 在每个请求进入时调用。
func ContextWithRequestID(ctx context.Context, id int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxRequestIDKey{}, id)
}

// RequestIDFromContext 从 context 读取 requestID;不存在或类型不符返回 (0, false)。
func RequestIDFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	id, ok := ctx.Value(ctxRequestIDKey{}).(int64)
	return id, ok
}

// ============================================================================
// With 能力:为任意级别附加结构化字段
// ============================================================================
//
// 三种用法,覆盖「有请求上下文」「无请求上下文」「要连打多条」三种场景:
//
//	// 1. 单条、带 requestID(最常用)
//	logger.CtxInfof(ctx, "[pay] 支付成功 订单%s", orderNo)
//
//	// 2. 一次性带多个字段,或同字段要连打多条 —— 复用同一个 logger
//	log := logger.WithContext(ctx, zap.String("orderNo", orderNo))
//	log.Infof("支付成功")
//	log.Warnf("金额不一致")       // 同一 requestID + orderNo
//	log.Errorf("回写失败")
//
//	// 3. 无请求上下文(定时任务/启动流程),只带业务字段
//	logger.With(zap.String("job", "cleanup")).Infof("清理完成")
//
// 返回的 *zap.SugaredLogger 具备 zap 全部级别方法(Debug/Info/Warn/Error/
// Fatal/Panic 及各自的 f/w/ln 变体),因此「各级别 With」无需逐个定义包装函数。

// ctxLogger 返回绑定了 requestID 与附加字段的 *zap.Logger。
//
// base 决定输出通道:rootRaw(主日志)或 agentRaw(打印代理日志)。
// 字段顺序固定为 requestID 在前,便于日志采集端统一抽取。
func ctxLogger(base *zap.Logger, ctx context.Context, fields []zap.Field) *zap.Logger {
	if base == nil {
		base = zap.NewNop()
	}
	// 预分配一次,避免 requestID + 业务字段两次扩容。
	all := make([]zap.Field, 0, len(fields)+1)
	if ctx != nil {
		if id, ok := RequestIDFromContext(ctx); ok {
			all = append(all, zap.Int64("requestID", id))
		}
	}
	all = append(all, fields...)
	// 没有字段时直接返回 base:With() 空字段也会 clone 一个 logger,没意义。
	if len(all) == 0 {
		return base
	}
	return base.With(all...)
}

// With 返回携带附加字段的 logger(写主日志),所有级别通用。
//
// 不绑定 requestID:适用定时任务、启动流程等没有请求上下文的场景。
// 有 ctx 时请优先用 WithContext —— 多一个字段就多一条串联线索。
func With(fields ...zap.Field) *zap.SugaredLogger {
	mu.RLock()
	base := rootRaw
	mu.RUnlock()
	return ctxLogger(base, nil, fields).Sugar()
}

// WithContext 返回绑定了 ctx 中 requestID(以及可选附加字段)的 logger,
// 它的 Debugf/Infof/Warnf/Errorf/Fatalf 都会自动带上这些字段。
//
// ctx 中没有 requestID 时返回只带 fields 的 logger —— 不报错、不丢日志,
// 因为后台任务/定时任务本就没有请求上下文。
func WithContext(ctx context.Context, fields ...zap.Field) *zap.SugaredLogger {
	mu.RLock()
	base := rootRaw
	mu.RUnlock()
	return ctxLogger(base, ctx, fields).Sugar()
}

// AgentWithContext 同 WithContext,但写打印代理专属日志(默认 ./logs/agent.log)。
// 排查「代理为什么拉不到单」时用,避免高频轮询噪音混进主日志。
func AgentWithContext(ctx context.Context, fields ...zap.Field) *zap.SugaredLogger {
	mu.RLock()
	base := agentRaw
	mu.RUnlock()
	return ctxLogger(base, ctx, fields).Sugar()
}

// logfWith 是所有 CtxXxx 包装函数的公共实现。
//
// callerSkip 由调用方给出:基准 0 会报到 logfWith 自身(它才是直接调 zap 的
// 那一层),往上依次是各层包装函数,最后才是业务代码。目前 CtxXxx 的固定
// 调用链是「业务 → CtxXxx → ctxLogf/agentCtxLogf → logfWith」,故传 3。
// 显式传参而非写死常数:将来若增删包装层,只需改调用处,不必重新推算。
func logfWith(base *zap.Logger, ctx context.Context, lvl zapcore.Level, callerSkip int, template string, args ...any) {
	s := ctxLogger(base, ctx, nil).WithOptions(zap.AddCallerSkip(callerSkip)).Sugar()
	switch lvl {
	case zapcore.DebugLevel:
		s.Debugf(template, args...)
	case zapcore.WarnLevel:
		s.Warnf(template, args...)
	case zapcore.ErrorLevel:
		s.Errorf(template, args...)
	case zapcore.FatalLevel:
		s.Fatalf(template, args...)
	default:
		s.Infof(template, args...)
	}
}

// CtxDebugf / CtxInfof / CtxWarnf / CtxErrorf / CtxFatalf:带 requestID 的业务日志,
// 与包级同名函数的唯一区别是自动附加 requestID 字段。
// 已有代码的 logger.Infof 可原地替换为 logger.CtxInfof(ctx, ...)。
// ctxCallerSkip CtxXxx 的 caller 层数:业务 → CtxXxx → ctxLogf → logfWith。
// 与 agentCtxLogf 共用同一条链长,故共用一个常数。
const ctxCallerSkip = 3

func ctxLogf(ctx context.Context, lvl zapcore.Level, template string, args ...any) {
	mu.RLock()
	base := rootRaw
	mu.RUnlock()
	logfWith(base, ctx, lvl, ctxCallerSkip, template, args...)
}

func CtxDebugf(ctx context.Context, template string, args ...any) {
	ctxLogf(ctx, zapcore.DebugLevel, template, args...)
}
func CtxInfof(ctx context.Context, template string, args ...any) {
	ctxLogf(ctx, zapcore.InfoLevel, template, args...)
}
func CtxWarnf(ctx context.Context, template string, args ...any) {
	ctxLogf(ctx, zapcore.WarnLevel, template, args...)
}
func CtxErrorf(ctx context.Context, template string, args ...any) {
	ctxLogf(ctx, zapcore.ErrorLevel, template, args...)
}

// CtxFatalf 带 requestID 的致命错误:记录后 os.Exit(1)。
// 启动期失败(如配置装配不了)用它,能顺带留下「是哪次启动流程失败」的线索。
func CtxFatalf(ctx context.Context, template string, args ...any) {
	ctxLogf(ctx, zapcore.FatalLevel, template, args...)
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

// CtxAgentXxx:打印代理日志的 requestID 版本,与 CtxXxx 完全同构。
// 代理的 ping/pull/ack 是长轮询高频接口,按 requestID 串联才能看清
// 「某一次拉单请求在服务端到底经历了什么」。
func agentCtxLogf(ctx context.Context, lvl zapcore.Level, template string, args ...any) {
	mu.RLock()
	base := agentRaw
	mu.RUnlock()
	logfWith(base, ctx, lvl, ctxCallerSkip, template, args...)
}

func CtxAgentDebugf(ctx context.Context, template string, args ...any) {
	agentCtxLogf(ctx, zapcore.DebugLevel, template, args...)
}
func CtxAgentInfof(ctx context.Context, template string, args ...any) {
	agentCtxLogf(ctx, zapcore.InfoLevel, template, args...)
}
func CtxAgentWarnf(ctx context.Context, template string, args ...any) {
	agentCtxLogf(ctx, zapcore.WarnLevel, template, args...)
}
func CtxAgentErrorf(ctx context.Context, template string, args ...any) {
	agentCtxLogf(ctx, zapcore.ErrorLevel, template, args...)
}

// ---- gin 中间件 ----

// GinRequestIDKey 是 requestID 在 gin.Context 里的键,由 middleware.RequestID 写入。
//
// 定义在 logger 包、由 middleware 引用(middleware 单向依赖 logger,
// 反过来会成环):两边共用同一常量,杜绝「各写一份字面量、改一处就静默断链」。
const GinRequestIDKey = "requestID"

// requestID 从 gin.Context 读取雪花 requestID(值类型 int64),返回十进制字符串;
// 不存在或类型不符时返回空串。这里内联读取而非依赖 middleware 包,
// 避免基础设施层反向依赖 HTTP 中间件层。
func requestID(c *gin.Context) string {
	if v, ok := c.Get(GinRequestIDKey); ok {
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
	// 与审计脱敏(store/dao/audit.go)保持一致:个人信息与支付签名同样
	// 不能出现在访问日志里 —— 访问日志会被采集走,留存面比数据库更广。
	"openid", "unionid", "phone", "mobile", "idcard", "signature", "sign",
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

// GinLogger 替代 gin.Logger():统一输出到 zap。
//
// 访问日志以「结构化字段」落盘(method/path/status/latency_ms/client_ip/query/
// error/request_id),而不是把一列空格对齐文本拼进 msg —— app.log 的 JSON 因此能被
// jq/ELK/Loki 直接按字段检索;终端则由 console 编码器把这些字段以 {...} 附在行尾。
// 级别映射:5xx→Error、4xx→Warn、其余→Info。
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		if skipPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		agent := isAgentPath(path)
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := make([]zap.Field, 0, 7)
		fields = append(fields,
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Float64("latency_ms", float64(latency.Nanoseconds())/1e6),
			zap.String("client_ip", c.ClientIP()),
		)
		if query != "" {
			fields = append(fields, zap.String("query", maskQuery(query)))
		}
		if errMsg := c.Errors.ByType(gin.ErrorTypePrivate).String(); errMsg != "" {
			fields = append(fields, zap.String("error", errMsg))
		}
		// request_id 独立成字段:排障时用响应头 X-Request-ID 反查这条访问日志,
		// 也能与业务日志里的 requestID 对齐。
		if rid := requestID(c); rid != "" {
			fields = append(fields, zap.String("request_id", rid))
		}

		lvl := zapcore.InfoLevel
		switch {
		case status >= 500:
			lvl = zapcore.ErrorLevel
		case status >= 400:
			lvl = zapcore.WarnLevel
		}

		// msg 只保留人类可读的请求行;数据都在字段里,避免 JSON 里出现空格对齐噪音。
		accessLog(agent, lvl, c.Request.Method+" "+path, fields)
	}
}

// accessLog 写一条访问日志,与业务日志有几点刻意差异:
//   - 关闭 caller:访问日志统一由中间件产生,caller 恒指向 logger 包内部,没有信息量;
//   - 关闭堆栈(阈值抬到 Fatal):5xx 访问日志的调用栈只会指向日志内部,真正需要
//     堆栈的 panic 已由 GinRecovery 单独记录,这里再打纯属噪音;
//   - agent 通道(门店打印代理 ping/pull/ack)只落文件、不刷控制台,避免高频轮询刷屏。
func accessLog(agent bool, lvl zapcore.Level, msg string, fields []zap.Field) {
	mu.RLock()
	base := rootRaw
	if agent {
		base = agentRaw
	}
	mu.RUnlock()

	lg := ctxLogger(base, nil, fields).WithOptions(
		zap.WithCaller(false),
		zap.AddStacktrace(zapcore.FatalLevel),
	)
	switch lvl {
	case zapcore.WarnLevel:
		lg.Warn(msg)
	case zapcore.ErrorLevel:
		lg.Error(msg)
	default:
		lg.Info(msg)
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
