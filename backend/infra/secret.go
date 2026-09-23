// 敏感配置加密原语。
//
// 设计要点:
//   - 本机主密钥(master key)为 32 字节,来源优先级:
//     CONFIG_MASTER_KEY 环境变量 > MASTER_KEY_PATH 指定文件 > 默认 ./data/master.key(不存在则自动生成);
//   - 敏感配置项(tb_config 表)落库前用 AES-256-GCM 加密,密文带 enc:v1: 前缀;
//   - GetSetting/SetSetting 透明加解密,业务层无感知;历史明文值在启动时自动迁移为密文;
//   - 主密钥缺失时降级为明文存储并告警,不阻断服务启动(避免升级即不可用)。
package infra

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"dining-system/infra/logger"
	"dining-system/internal/conf"
)

// encPrefix 密文前缀,用于与历史明文区分(解密时按前缀判断,非密文原样返回)。
const encPrefix = "enc:v1:"

// encAAD 附加认证数据,防止密文被移植到其它字段后被正常解开。
var encAAD = []byte("dining-config-secret-v1")

// sensitiveKeys 需要加密落库的配置项白名单。
// 商户私钥/公钥以「文件路径」形式保存,内容本身不落库,故无需加入。
var sensitiveKeys = map[string]bool{
	"wxpay_apiv3_key": true,
	// 飞鹅开发者 UKEY:等同于账号密码(拿到它就能用商户账号给任意已绑定打印机推单)。
	"feie_ukey": true,
	// 本地打印代理令牌:拿到它就能冒充门店代理把队列里的票据全部拉走(含订单金额)。
	"agent_token": true,
}

var (
	masterKey       []byte
	masterKeyOK     bool
	masterKeyMu     sync.RWMutex
	warnKeyOnce     sync.Once
	warnDecOnce     sync.Once
	warnDecFailOnce sync.Once
)

// InitSecretKeys 加载或生成本机主密钥。
func InitSecretKeys() {
	key, err := loadMasterKey()
	if err != nil {
		logger.Warnf("[secret] 主密钥不可用,敏感配置将以明文存储: %v", err)
	}
	masterKeyMu.Lock()
	masterKey = key
	masterKeyOK = len(key) > 0
	masterKeyMu.Unlock()
}

// SecretsEncrypted 报告敏感配置是否已启用加密存储(供启动日志提示)。
func SecretsEncrypted() bool {
	masterKeyMu.RLock()
	defer masterKeyMu.RUnlock()
	return masterKeyOK
}

// loadMasterKey 按优先级获取主密钥;均不存在时自动生成一份。
func loadMasterKey() ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv(conf.EnvConfigMasterKey)); v != "" {
		key, err := parseKeyMaterial(v)
		if err != nil {
			return nil, fmt.Errorf("环境变量 CONFIG_MASTER_KEY 格式非法: %w", err)
		}
		return key, nil
	}

	path := Getenv(conf.EnvMasterKeyPath, filepath.FromSlash(conf.DefaultMasterKeyPath))
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, perr := parseKeyMaterial(strings.TrimSpace(string(data)))
		if perr != nil {
			return nil, fmt.Errorf("主密钥文件 %s 格式非法: %w", path, perr)
		}
		hardenFile(path)
		return key, nil

	case os.IsNotExist(err):
		// 只有「文件不存在」才允许生成新密钥;权限不足 / 读取失败等其它错误必须
		// 直接返回,绝不能落到「生成新密钥」分支覆盖现有 master.key —— 否则旧密钥
		// 一旦被覆盖,存量密文将永久不可解。参考 authkey.go 的同一区分模式。

	default:
		return nil, fmt.Errorf("读取主密钥文件 %s 失败: %w(请检查权限;若文件已损坏,请先移走该文件再启动)", path, err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成主密钥失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建主密钥目录失败: %w", err)
	}
	content := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("写入主密钥文件失败: %w", err)
	}
	hardenFile(path)
	logger.Infof("[secret] 已生成主密钥 %s —— 请勿与数据库一同备份或拷贝,丢失将无法解密支付密钥", path)
	return key, nil
}

// parseKeyMaterial 解析密钥材料,支持原始 32 字节 / hex(64 字符) / base64。
func parseKeyMaterial(s string) ([]byte, error) {
	if len(s) == 32 {
		return []byte(s), nil
	}
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, fmt.Errorf("需要 32 字节密钥(原始 32 字符 / base64 / hex 均可)")
}

// EncryptSecret 加密敏感配置值。空值或主密钥不可用时原样返回。
func EncryptSecret(plain string) string {
	if plain == "" {
		return plain
	}
	// 带 enc:v1: 前缀的值可能是真密文,也可能是管理员输入的明文恰好以该前缀开头。
	// 先尝试按密文解析:能解开说明确实已加密,幂等返回(避免二次加密);
	// 解不开则视为明文继续走加密流程,否则带前缀明文会被当成「已加密」落库,
	// 之后 DecryptSecret 永远解不出、功能静默失效。TryDecrypt 失败不落日志,
	// 正好用作这里的安静探测。
	if strings.HasPrefix(plain, encPrefix) {
		if _, ok := TryDecrypt(nil, plain); ok {
			return plain
		}
	}
	key := currentKey()
	if len(key) == 0 {
		warnKeyOnce.Do(func() {
			logger.Warnf("[secret] 主密钥未就绪,敏感配置将以明文写入")
		})
		return plain
	}
	ct, err := sealGCM(key, []byte(plain))
	if err != nil {
		logger.Warnf("[secret] 加密失败,将以明文写入: %v", err)
		return plain
	}
	return encPrefix + ct
}

// DecryptSecret 解密配置值。非密文(历史明文)原样返回,保证向后兼容。
func DecryptSecret(stored string) string {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored
	}
	key := currentKey()
	if len(key) == 0 {
		// 存在密文却拿不到主密钥属于「配置悄悄挂了」级别的故障,记 error 级日志让运维可观测。
		// 返回值仍保持空串(fail-open),不改变调用方行为,避免升级即不可用。
		warnDecOnce.Do(func() {
			logger.Errorf("[secret] 检测到加密配置但主密钥不可用,相关配置按空处理(请检查 CONFIG_MASTER_KEY / MASTER_KEY_PATH)")
		})
		return ""
	}
	plain, err := openGCM(key, strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		// 同上:密文存在但解不开(主密钥可能已更换/损坏),error 级日志暴露问题,
		// 返回值仍为空串,保持调用方 fail-open 语义不变。高频调用方(打印代理
		// 每 3s 轮询一次 agent_token)会反复触发,限频一次避免刷爆日志。
		warnDecFailOnce.Do(func() {
			logger.Errorf("[secret] 解密失败(主密钥可能已更换),相关配置按空处理: %v", err)
		})
		return ""
	}
	return string(plain)
}

// IsEncrypted 报告配置值是否为加密密文。
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encPrefix)
}

// IsSensitiveKey 报告某配置项是否属于需要加密的敏感项。
func IsSensitiveKey(key string) bool {
	return sensitiveKeys[key]
}

// SensitiveKeys 返回需要加密落库的配置项白名单。
func SensitiveKeys() []string {
	keys := make([]string, 0, len(sensitiveKeys))
	for k := range sensitiveKeys {
		keys = append(keys, k)
	}
	return keys
}

// OldMasterKey 读取轮换用的旧主密钥(MASTER_KEY_OLD,格式与主密钥相同:
// 原始 32 字符 / hex / base64)。未配置返回 (nil, false);与当前密钥相同
// 时也视为未配置——多半是轮换完成后忘了移除环境变量,幂等跳过。
func OldMasterKey() ([]byte, bool) {
	v := strings.TrimSpace(Getenv(conf.EnvMasterKeyOld, ""))
	if v == "" {
		return nil, false
	}
	key, err := parseKeyMaterial(v)
	if err != nil {
		logger.Warnf("[secret] MASTER_KEY_OLD 格式非法,跳过密钥轮换: %v", err)
		return nil, false
	}
	if string(key) == string(currentKey()) {
		logger.Infof("[secret] MASTER_KEY_OLD 与当前主密钥相同,无需轮换(可移除该变量)")
		return nil, false
	}
	return key, true
}

// TryDecrypt 用指定密钥解密一条密文;失败只返回 ok=false,不落日志,
// 由调用方决定如何处置(轮换流程需要安静的探测,告警集中在失败项上)。
// key 为 nil 时使用当前主密钥,用于判断存量密文是否已由当前密钥加密。
func TryDecrypt(key []byte, stored string) (string, bool) {
	if key == nil {
		key = currentKey()
	}
	if len(key) == 0 || !strings.HasPrefix(stored, encPrefix) {
		return "", false
	}
	plain, err := openGCM(key, strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", false
	}
	return string(plain), true
}

// ---- 内部工具 ----

func currentKey() []byte {
	masterKeyMu.RLock()
	defer masterKeyMu.RUnlock()
	return masterKey
}

func sealGCM(key, plain []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, plain, encAAD)
	return base64.StdEncoding.EncodeToString(out), nil
}

func openGCM(key []byte, payload string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("密文 base64 解析失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("密文长度异常")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], encAAD)
}

// hardenFile 尽力收紧敏感文件权限。
//
// 类 Unix 下 chmod 0600;Windows 下仅当显式设置 HARDEN_FILE_ACL=1 时才调用 icacls
// 移除继承并只保留 SYSTEM / Administrators —— 默认不动 ACL,避免服务以非管理员账户
// 运行时被误锁导致数据库无法访问。
func hardenFile(path string) {
	if err := os.Chmod(path, 0o600); err != nil {
		logger.Warnf("[secret] 收紧 %s 权限失败: %v", path, err)
	}
	if runtime.GOOS != "windows" || Getenv(conf.EnvHardenFileACL, "") != "1" {
		return
	}
	cmd := exec.Command("icacls", path, "/inheritance:r",
		"/grant:r", "*S-1-5-18:(F)", "*S-1-5-32-544:(F)")
	if out, err := cmd.CombinedOutput(); err != nil {
		logger.Warnf("[secret] icacls 收紧 %s 失败(可手工处理): %v %s", path, err, strings.TrimSpace(string(out)))
	}
}
