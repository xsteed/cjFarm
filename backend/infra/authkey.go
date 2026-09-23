// 登录令牌签名密钥持久化。
//
// 设计要点:
//   - 密钥来源优先级:TOKEN_SECRET 环境变量 > AUTH_KEY_PATH 指定文件(默认 ./data/auth.key)
//     > 进程内随机;
//   - 文件不存在时自动生成 32 字节随机 hex 并落盘(0600),保证重启与多实例共享同一签名密钥;
//   - 读文件时区分「不存在」与「权限/损坏」:前者生成新文件,后者 fail-fast,
//     绝不静默覆盖旧密钥 —— 否则旧密钥一旦被覆盖,所有存量登录态会突然失效且无法追溯;
//   - 与 secret.go 的 master key 是两套独立命名空间(不同环境变量、不同文件、不同用途),
//     登录令牌的密钥轮换不影响配置加密,反之亦然。
//
// 为什么需要 LoadAuthKey / InitAuthKey 两个入口:
//   - service 包的 init() 在 main() 之前运行,此时 .env / config.yaml 尚未装配进环境,
//     若在此阶段就落盘 data/auth.key,会按「未配置任何来源」误生成一个随机密钥文件,
//     且测试在源码目录下运行也会把 data/auth.key 散落到各包目录;
//   - 因此 LoadAuthKey 只做只读解析(env → 已存在的文件 → 进程随机),绝不落盘;
//   - 生产启动由 main 在装配完部署配置后调用 InitAuthKey,重新解析并负责落盘与启动告警。
package infra

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"dining-system/infra/logger"
	"dining-system/internal/conf"
)

var (
	authKeyMu       sync.RWMutex
	authKeyValue    string
	authKeyWarnOnce sync.Once
)

// LoadAuthKey 返回当前缓存的登录令牌签名密钥。首次调用时按「TOKEN_SECRET →
// 已存在的 data/auth.key → 进程内随机」做只读解析并缓存,不落盘、不告警。
// 生产启动流程里 main 会在装配部署配置后调用 InitAuthKey 重新解析并持久化,
// 因此运行期拿到的始终是 InitAuthKey 确定下来的最终值。
func LoadAuthKey() string {
	authKeyMu.RLock()
	if authKeyValue != "" {
		v := authKeyValue
		authKeyMu.RUnlock()
		return v
	}
	authKeyMu.RUnlock()

	key, _ := resolveAuthKey(false)
	authKeyMu.Lock()
	if authKeyValue == "" {
		authKeyValue = key
	}
	v := authKeyValue
	authKeyMu.Unlock()
	return v
}

// InitAuthKey 是启动入口:在部署配置(.env / config.yaml)写入环境后调用,
// 重新解析登录令牌签名密钥,必要时落盘 data/auth.key,并按需输出启动告警。
// 与 InitSecretKeys 同构,便于 main 里相邻摆放。
func InitAuthKey() string {
	key, random := resolveAuthKey(true)
	authKeyMu.Lock()
	authKeyValue = key
	authKeyMu.Unlock()
	if random {
		authKeyWarnOnce.Do(func() {
			logger.Warnf("[authkey] 登录令牌密钥为进程随机,重启后所有登录态失效,建议设置 TOKEN_SECRET 或 data/auth.key")
		})
	}
	return key
}

// resolveAuthKey 按三级优先级解析签名密钥,返回密钥与其是否为「进程随机」。
// persist 为 false 时文件缺失不落盘(只读解析);为 true 时允许自动生成并落盘。
func resolveAuthKey(persist bool) (string, bool) {
	if v := strings.TrimSpace(os.Getenv(conf.EnvTokenSecret)); v != "" {
		return normalizeAuthKey(v), false
	}

	path := Getenv(conf.EnvAuthKeyPath, filepath.FromSlash(conf.DefaultAuthKeyPath))
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, perr := parseAuthKeyFile(string(data))
		if perr != nil {
			// 文件存在但内容非法/为空:视为损坏,绝不静默覆盖生成 —— 覆盖会让
			// 之前用旧密钥签发的所有令牌失效。给出明确指引让运维决策。
			logger.Fatalf("[authkey] 登录令牌密钥文件 %s 格式非法: %v(请检查文件内容;若已损坏,请先移走该文件再启动)", path, perr)
		}
		hardenFile(path)
		return key, false

	case os.IsNotExist(err):
		if !persist {
			// 只读解析阶段(如 service 包的 init)文件缺失时回退为进程随机,
			// 避免在源码目录/测试目录散落 data/auth.key。
			return processRandomAuthKey(), true
		}
		key, gerr := newAuthKey()
		if gerr != nil {
			logger.Fatalf("[authkey] 生成登录令牌密钥失败: %v", gerr)
		}
		if merr := os.MkdirAll(filepath.Dir(path), 0o700); merr != nil {
			// 目录建不出来(如只读文件系统):降级为进程随机并告警,不阻断服务启动。
			logger.Warnf("[authkey] 创建登录令牌密钥目录失败,将使用进程随机密钥: %v", merr)
			return key, true
		}
		if werr := os.WriteFile(path, []byte(key+"\n"), 0o600); werr != nil {
			logger.Warnf("[authkey] 写入登录令牌密钥文件失败,将使用进程随机密钥: %v", werr)
			return key, true
		}
		hardenFile(path)
		logger.Infof("[authkey] 已生成登录令牌密钥 %s —— 请勿与数据库一同备份,丢失将导致所有登录态失效", path)
		return key, false

	default:
		// 权限不足 / 文件损坏等「非不存在」错误必须 fail-fast:若静默重新生成覆盖旧密钥,
		// 会重演 master.key 早期实现的坑 —— 旧密钥丢失导致存量令牌全部失效。
		logger.Fatalf("[authkey] 读取登录令牌密钥文件 %s 失败: %v(请检查权限;若文件已损坏,请先移走该文件再启动)", path, err)
	}
	return "", false // 不可达:Fatalf 会 os.Exit(1)
}

// normalizeAuthKey 把 TOKEN_SECRET 环境变量归一化为 hex 形式:
//   - 已是 64 字符合法 hex(与文件生成格式一致)时原样返回(统一小写);
//   - 其它输入(任意长度的原文)视为原始字符串,统一 hex 编码后返回。
//
// 返回的 hex 串作为 HMAC-SHA256 的密钥字节使用,与历史实现保持一致。
func normalizeAuthKey(s string) string {
	if len(s) == 64 {
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s)
		}
	}
	return hex.EncodeToString([]byte(s))
}

// parseAuthKeyFile 解析密钥文件内容:必须为 64 字符 hex(32 字节),否则视为损坏。
//
// 文件是程序自己生成的,格式严格可控;遇到非法内容必须报错,绝不能像环境变量那样
// 当作原文静默接受 —— 那会得到一个与旧密钥不同的值,导致所有存量令牌静默失效。
func parseAuthKeyFile(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return "", fmt.Errorf("需要 64 字符 hex(32 字节),当前 %d 字符", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("非合法 hex: %w", err)
	}
	return strings.ToLower(s), nil
}

// newAuthKey 生成 32 字节随机数的 hex 串(64 字符)作为签名密钥。
func newAuthKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("读取随机源失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// processRandomAuthKey 生成进程内随机密钥;crypto/rand 失败时兜底固定字符串,
// 仅保证进程能启动(该兜底不可跨重启复用,启动告警会提示运维配置持久化密钥)。
func processRandomAuthKey() string {
	key, err := newAuthKey()
	if err != nil {
		return hex.EncodeToString([]byte("dining-fallback-secret"))
	}
	return key
}
