// 敏感配置加密存储。
//
// 设计要点:
//   - 本机主密钥(master key)为 32 字节,来源优先级:
//     CONFIG_MASTER_KEY 环境变量 > MASTER_KEY_PATH 指定文件 > 默认 ./data/master.key(不存在则自动生成);
//   - 敏感配置项(tb_config 表)落库前用 AES-256-GCM 加密,密文带 enc:v1: 前缀;
//   - GetCfg/SetCfg 透明加解密,业务层无感知;历史明文值在启动时自动迁移为密文;
//   - 主密钥缺失时降级为明文存储并告警,不阻断服务启动(避免升级即不可用)。
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"dining-system/internal/logger"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	masterKey    []byte
	masterKeyOK  bool
	masterKeyMu  sync.RWMutex
	warnKeyOnce  sync.Once
	warnDecOnce  sync.Once
	secretInited bool
)

// InitSecret 加载或生成本机主密钥,并把历史明文敏感配置迁移为密文。
// 需在 store.Init 之后、开始处理请求之前调用。
func InitSecret() {
	key, err := loadMasterKey()
	if err != nil {
		logger.Warnf("[secret] 主密钥不可用,敏感配置将以明文存储: %v", err)
	}
	masterKeyMu.Lock()
	masterKey = key
	masterKeyOK = len(key) > 0
	secretInited = true
	masterKeyMu.Unlock()

	if !masterKeyOK {
		return
	}
	MigrateSecrets()
}

// SecretsEncrypted 报告敏感配置是否已启用加密存储(供启动日志提示)。
func SecretsEncrypted() bool {
	masterKeyMu.RLock()
	defer masterKeyMu.RUnlock()
	return masterKeyOK
}

// loadMasterKey 按优先级获取主密钥;均不存在时自动生成一份。
func loadMasterKey() ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("CONFIG_MASTER_KEY")); v != "" {
		key, err := parseKeyMaterial(v)
		if err != nil {
			return nil, fmt.Errorf("环境变量 CONFIG_MASTER_KEY 格式非法: %w", err)
		}
		return key, nil
	}

	path := Getenv("MASTER_KEY_PATH", filepath.Join("data", "master.key"))
	if data, err := os.ReadFile(path); err == nil {
		key, perr := parseKeyMaterial(strings.TrimSpace(string(data)))
		if perr != nil {
			return nil, fmt.Errorf("主密钥文件 %s 格式非法: %w", path, perr)
		}
		hardenFile(path)
		return key, nil
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

// EncryptSecret 加密敏感配置值。已是密文、空值或主密钥不可用时原样返回。
func EncryptSecret(plain string) string {
	if plain == "" || strings.HasPrefix(plain, encPrefix) {
		return plain
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
		warnDecOnce.Do(func() {
			logger.Warnf("[secret] 检测到加密配置但主密钥不可用,相关配置按空处理(请检查 CONFIG_MASTER_KEY / MASTER_KEY_PATH)")
		})
		return ""
	}
	plain, err := openGCM(key, strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		logger.Warnf("[secret] 解密失败(主密钥可能已更换): %v", err)
		return ""
	}
	return string(plain)
}

// SetCfg 写入单个配置项,敏感项自动加密。
// 语句由方言层生成:SQLite 为 INSERT OR REPLACE,MySQL 为 REPLACE(按主键整体替换)。
func SetCfg(key, value string) error {
	if sensitiveKeys[key] {
		value = EncryptSecret(value)
	}
	_, err := DB.Exec(InsertReplaceInto("tb_config", "cfg_key", "cfg_value"), key, value)
	return err
}

// MigrateSecrets 把历史明文敏感配置加密回写(幂等,可重复执行)。
func MigrateSecrets() {
	if len(currentKey()) == 0 {
		return
	}
	for k := range sensitiveKeys {
		var v string
		if err := DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, k).Scan(&v); err != nil {
			continue
		}
		if v == "" || strings.HasPrefix(v, encPrefix) {
			continue
		}
		if _, err := DB.Exec(`UPDATE tb_config SET cfg_value=? WHERE cfg_key=?`, EncryptSecret(v), k); err != nil {
			logger.Warnf("[secret] 配置 %s 加密迁移失败: %v", k, err)
			continue
		}
		logger.Infof("[secret] 历史明文配置 %s 已加密存储", k)
	}
}

// IsSensitiveKey 报告某配置项是否属于需要加密的敏感项。
func IsSensitiveKey(key string) bool {
	return sensitiveKeys[key]
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
	if runtime.GOOS != "windows" || Getenv("HARDEN_FILE_ACL", "") != "1" {
		return
	}
	cmd := exec.Command("icacls", path, "/inheritance:r",
		"/grant:r", "*S-1-5-18:(F)", "*S-1-5-32-544:(F)")
	if out, err := cmd.CombinedOutput(); err != nil {
		logger.Warnf("[secret] icacls 收紧 %s 失败(可手工处理): %v %s", path, err, strings.TrimSpace(string(out)))
	}
}
