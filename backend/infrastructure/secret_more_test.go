package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/infrastructure/conf"
)

func TestIsEncryptedAndSensitiveHelpers(t *testing.T) {
	if !IsEncrypted(encPrefix + "abc") {
		t.Fatal("带前缀应判定为已加密")
	}
	if IsEncrypted("plain-text") {
		t.Fatal("无前缀应判定为明文")
	}
	if !IsSensitiveKey("wx_app_secret") {
		t.Fatal("wx_app_secret 应在敏感白名单")
	}
	if IsSensitiveKey("not_sensitive") {
		t.Fatal("非敏感键不应在白名单")
	}
	keys := SensitiveKeys()
	if len(keys) == 0 {
		t.Fatal("SensitiveKeys 应返回非空白名单")
	}
	found := false
	for _, k := range keys {
		if k == "feie_ukey" {
			found = true
		}
	}
	if !found {
		t.Fatal("白名单应包含 feie_ukey")
	}
}

func TestTryDecrypt(t *testing.T) {
	setupSecretEnv(t)
	InitSecretKeys()

	const plain = "secret-value-xyz"
	enc := EncryptSecret(plain)

	// 当前密钥应能解开。
	if got, ok := TryDecrypt(nil, enc); !ok || got != plain {
		t.Fatalf("TryDecrypt(nil) 应成功: ok=%v got=%q", ok, got)
	}
	// 错误密钥解不开。
	if _, ok := TryDecrypt([]byte("0123456789abcdef0123456789abcdef"), enc); ok {
		t.Fatal("错误密钥不应解开密文")
	}
	// 非密文直接失败。
	if _, ok := TryDecrypt(nil, "plaintext"); ok {
		t.Fatal("非密文应返回 ok=false")
	}
	// 坏密文(带前缀但 base64 非法)也应失败,且不落日志。
	if _, ok := TryDecrypt(nil, encPrefix+"!!!not-base64"); ok {
		t.Fatal("非法密文应返回 ok=false")
	}
}

func TestOldMasterKey(t *testing.T) {
	setupSecretEnv(t)
	InitSecretKeys()
	cur := currentKey()

	// 未配置 -> (nil, false)。
	t.Setenv(conf.EnvMasterKeyOld, "")
	if _, ok := OldMasterKey(); ok {
		t.Fatal("未配置 MASTER_KEY_OLD 应返回 false")
	}

	// 配置一个与当前不同的合法密钥 -> 返回该密钥。
	other := "abcdefghijklmnopqrstuvwxyz012345"
	t.Setenv(conf.EnvMasterKeyOld, other)
	if got, ok := OldMasterKey(); !ok || string(got) != other {
		t.Fatalf("应返回旧密钥: ok=%v got=%q", ok, got)
	}

	// 与当前密钥相同 -> 视为未配置(幂等跳过)。
	t.Setenv(conf.EnvMasterKeyOld, string(cur))
	if _, ok := OldMasterKey(); ok {
		t.Fatal("与当前密钥相同应返回 false")
	}

	// 非法格式 -> 返回 false。
	t.Setenv(conf.EnvMasterKeyOld, "bad")
	if _, ok := OldMasterKey(); ok {
		t.Fatal("非法旧密钥应返回 false")
	}
}

func TestHardenFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hardenFile(f)
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("hardenFile 应把权限收紧到 0600, got %o", fi.Mode().Perm())
	}
}

func TestLoadMasterKeyFromEnv(t *testing.T) {
	t.Setenv(conf.EnvConfigMasterKey, "0123456789abcdef0123456789abcdef")
	t.Setenv(conf.EnvMasterKeyPath, filepath.Join(t.TempDir(), "master.key"))
	key, err := loadMasterKey()
	if err != nil || len(key) != 32 {
		t.Fatalf("从环境变量加载主密钥失败: len=%d err=%v", len(key), err)
	}
}

func TestLoadMasterKeyGenerateAndReuse(t *testing.T) {
	// 环境变量为空、文件不存在 -> 自动生成并落盘。
	t.Setenv(conf.EnvConfigMasterKey, "")
	path := filepath.Join(t.TempDir(), "master.key")
	t.Setenv(conf.EnvMasterKeyPath, path)

	key1, err := loadMasterKey()
	if err != nil || len(key1) != 32 {
		t.Fatalf("生成主密钥失败: len=%d err=%v", len(key1), err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("主密钥文件未生成: %v", statErr)
	}
	// 再次加载应从文件读取同一把密钥(不重新生成)。
	key2, err := loadMasterKey()
	if err != nil || string(key2) != string(key1) {
		t.Fatalf("重复加载主密钥应一致: %q vs %q err=%v", key2, key1, err)
	}
}

func TestLoadMasterKeyInvalidEnv(t *testing.T) {
	t.Setenv(conf.EnvConfigMasterKey, "too-short")
	t.Setenv(conf.EnvMasterKeyPath, filepath.Join(t.TempDir(), "master.key"))
	if _, err := loadMasterKey(); err == nil {
		t.Fatal("非法环境变量主密钥应报错")
	}
}

func TestEncryptSecretPrefixProbe(t *testing.T) {
	setupSecretEnv(t)
	InitSecretKeys()

	// 明文恰好以 enc:v1: 开头且能被当前密钥解开 -> 幂等返回(不二次加密)。
	const plain = "enc:v1:should-not-be-reencrypted"
	enc := EncryptSecret(plain)
	if !strings.HasPrefix(enc, encPrefix) {
		t.Fatal("已带前缀的明文加密后应仍是密文")
	}
	if DecryptSecret(enc) != plain {
		t.Fatal("幂等加密后仍能还原")
	}
}
