package infra

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/conf"
)

// resetAuthKeyCache 清空登录令牌密钥的包级缓存,模拟进程重启重新解析。
func resetAuthKeyCache() {
	authKeyMu.Lock()
	authKeyValue = ""
	authKeyMu.Unlock()
}

// TestParseAuthKeyFile 密钥文件解析:严格 64 字符 hex,非法内容必须报错
// (绝不静默接受,否则会得到一个与旧密钥不同的值,存量令牌全部失效)。
func TestParseAuthKeyFile(t *testing.T) {
	const valid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got, err := parseAuthKeyFile(valid + "\n"); err != nil || got != valid {
		t.Fatalf("合法文件内容解析失败: got=%q err=%v", got, err)
	}
	if _, err := parseAuthKeyFile("short"); err == nil {
		t.Fatal("非法长度应报错")
	}
	if _, err := parseAuthKeyFile(strings.Repeat("z", 64)); err == nil {
		t.Fatal("非 hex 内容应报错")
	}
	if _, err := parseAuthKeyFile(""); err == nil {
		t.Fatal("空内容应报错")
	}
}

// TestNormalizeAuthKey 环境变量归一化:64 位 hex 原样(小写),原文按 hex 编码。
func TestNormalizeAuthKey(t *testing.T) {
	hexKey := strings.Repeat("ab", 32) // 64 字符合法 hex
	if got := normalizeAuthKey(hexKey); got != hexKey {
		t.Fatalf("64 位 hex 应原样返回, got %q", got)
	}
	if got := normalizeAuthKey("hello"); got != hex.EncodeToString([]byte("hello")) {
		t.Fatalf("原文应按 hex 编码, got %q", got)
	}
}

// TestAuthKeyFromEnv TOKEN_SECRET 优先级最高。
func TestAuthKeyFromEnv(t *testing.T) {
	resetAuthKeyCache()
	secret := strings.Repeat("cd", 32)
	t.Setenv(conf.EnvTokenSecret, secret)
	if got := InitAuthKey(); got != secret {
		t.Fatalf("TOKEN_SECRET 应优先生效, got %q", got)
	}
}

// TestAuthKeyFilePersist 无环境变量时密钥文件自动生成(0600),重启后复用同一密钥。
func TestAuthKeyFilePersist(t *testing.T) {
	resetAuthKeyCache()
	t.Setenv(conf.EnvTokenSecret, "")
	path := filepath.Join(t.TempDir(), "auth.key")
	t.Setenv(conf.EnvAuthKeyPath, path)

	k1 := InitAuthKey()
	if len(k1) != 64 {
		t.Fatalf("生成密钥应为 64 字符 hex, got %d", len(k1))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("密钥文件未生成: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限应为 0600, got %o", fi.Mode().Perm())
	}

	// 模拟重启:清缓存后重新加载,必须从文件读到同一个密钥(登录态不失效)。
	resetAuthKeyCache()
	if k2 := InitAuthKey(); k2 != k1 {
		t.Fatalf("重启后密钥应一致: %q != %q", k2, k1)
	}
}
