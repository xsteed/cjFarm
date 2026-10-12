package infrastructure

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"dining-system/infrastructure/conf"
)

// TestLoadAuthKeyReadonlyRandom 无可信来源(无 TOKEN_SECRET、无密钥文件)时,
// 只读解析阶段(LoadAuthKey)回退为进程随机密钥,且不落盘。
func TestLoadAuthKeyReadonlyRandom(t *testing.T) {
	resetAuthKeyCache()
	t.Setenv(conf.EnvTokenSecret, "")
	path := filepath.Join(t.TempDir(), "auth.key")
	t.Setenv(conf.EnvAuthKeyPath, path)

	k1 := LoadAuthKey()
	if len(k1) != 64 {
		t.Fatalf("随机密钥应为 64 字符 hex, got %d", len(k1))
	}
	// 第二次调用应命中缓存,返回同一把,且不应在磁盘落下文件。
	k2 := LoadAuthKey()
	if k2 != k1 {
		t.Fatalf("LoadAuthKey 应缓存结果: %q != %q", k2, k1)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("只读解析阶段不应落盘 auth.key")
	}
}

// TestLoadAuthKeyEnvCaches 环境变量来源的密钥同样被缓存,且归一化生效。
func TestLoadAuthKeyEnvCaches(t *testing.T) {
	resetAuthKeyCache()
	secret := "raw-secret-value"
	t.Setenv(conf.EnvTokenSecret, secret)
	t.Setenv(conf.EnvAuthKeyPath, filepath.Join(t.TempDir(), "auth.key"))

	want := hex.EncodeToString([]byte(secret))
	if got := LoadAuthKey(); got != want {
		t.Fatalf("LoadAuthKey 应返回归一化密钥 %q, got %q", want, got)
	}
}
