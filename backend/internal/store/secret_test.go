package store

import (
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

// setupSecretEnv 隔离主密钥环境:只使用临时目录,不污染真实部署。
func setupSecretEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIG_MASTER_KEY", "")
	t.Setenv("MASTER_KEY_PATH", filepath.Join(t.TempDir(), "master.key"))
	masterKeyMu.Lock()
	masterKey = nil
	masterKeyOK = false
	masterKeyMu.Unlock()
}

// TestSecretEncryptRoundTrip 验证敏感配置加密后能正确解密,且密文中不出现明文。
func TestSecretEncryptRoundTrip(t *testing.T) {
	setupSecretEnv(t)
	InitSecret()
	if !SecretsEncrypted() {
		t.Fatal("主密钥未就绪")
	}

	const plain = "0123456789abcdef0123456789abcdef"
	enc := EncryptSecret(plain)
	if !strings.HasPrefix(enc, encPrefix) {
		t.Fatalf("密文缺少 %s 前缀: %s", encPrefix, enc)
	}
	if strings.Contains(enc, plain) {
		t.Fatal("密文中出现明文")
	}
	if got := DecryptSecret(enc); got != plain {
		t.Fatalf("解密结果=%q 期望 %q", got, plain)
	}
	// 每次加密使用随机 nonce,两次结果不应相同
	if EncryptSecret(plain) == enc {
		t.Fatal("两次加密结果相同,nonce 未随机化")
	}
}

// TestSecretPlaintextCompat 验证历史明文配置可原样读取(向后兼容)。
func TestSecretPlaintextCompat(t *testing.T) {
	setupSecretEnv(t)
	InitSecret()

	const plain = "legacy-plain-key"
	if got := DecryptSecret(plain); got != plain {
		t.Fatalf("明文应原样返回,实际 %q", got)
	}
	if got := EncryptSecret(""); got != "" {
		t.Fatalf("空值应原样返回,实际 %q", got)
	}
}

// TestSecretStoredEncrypted 验证敏感项经 SetCfg 落库后为密文,GetCfg 能透明读回明文,
// 普通配置项不受影响,且重复迁移是幂等的。
func TestSecretStoredEncrypted(t *testing.T) {
	setupSecretEnv(t)
	Init(filepath.Join(t.TempDir(), "secret.db"))
	defer DB.Close()
	InitSecret()

	const key = "wxpay_apiv3_key"
	const plain = "abcdefghijklmnopqrstuvwxyz012345"
	if err := SetCfg(key, plain); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}

	var raw string
	if err := DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, key).Scan(&raw); err != nil {
		t.Fatalf("读取原始值失败: %v", err)
	}
	if raw == plain {
		t.Fatal("敏感项在库中仍为明文")
	}
	if !strings.HasPrefix(raw, encPrefix) {
		t.Fatalf("库中值缺少密文前缀: %s", raw)
	}
	if got := GetCfg(key); got != plain {
		t.Fatalf("GetCfg=%q 期望 %q", got, plain)
	}

	// 普通配置项不应被加密
	if err := SetCfg("shop_name", "测试餐厅"); err != nil {
		t.Fatal(err)
	}
	var rawShop string
	if err := DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key='shop_name'`).Scan(&rawShop); err != nil {
		t.Fatal(err)
	}
	if rawShop != "测试餐厅" {
		t.Fatalf("普通配置被意外加密: %s", rawShop)
	}

	// 迁移必须幂等:已加密的值不能被二次加密
	MigrateSecrets()
	if got := GetCfg(key); got != plain {
		t.Fatalf("重复迁移后取值异常: %q", got)
	}
}

// TestSecretMigratePlaintext 验证历史明文敏感配置在启动迁移后变为密文。
func TestSecretMigratePlaintext(t *testing.T) {
	setupSecretEnv(t)
	Init(filepath.Join(t.TempDir(), "migrate.db"))
	defer DB.Close()

	const plain = "legacy-plaintext-apiv3-key-value!"
	if _, err := DB.Exec(`INSERT OR REPLACE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_apiv3_key', ?)`, plain); err != nil {
		t.Fatal(err)
	}

	InitSecret() // 内部会执行历史明文迁移

	var raw string
	if err := DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key='wxpay_apiv3_key'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encPrefix) {
		t.Fatalf("历史明文未被迁移为密文: %s", raw)
	}
	if strings.Contains(raw, plain) {
		t.Fatal("迁移后仍能在库中看到明文")
	}
	if got := GetCfg("wxpay_apiv3_key"); got != plain {
		t.Fatalf("迁移后读取=%q 期望 %q", got, plain)
	}
}

// TestSecretWrongMasterKey 验证主密钥不符时解密返回空(而不是崩溃或吐出密文)。
func TestSecretWrongMasterKey(t *testing.T) {
	setupSecretEnv(t)
	InitSecret()
	enc := EncryptSecret("some-secret-value")

	key := currentKey()
	if len(key) == 0 {
		t.Fatal("主密钥未就绪")
	}
	other := make([]byte, len(key))
	copy(other, key)
	other[0] ^= 0xFF
	masterKeyMu.Lock()
	masterKey = other
	masterKeyMu.Unlock()

	if got := DecryptSecret(enc); got != "" {
		t.Fatalf("主密钥不符时应返回空,实际 %q", got)
	}
}

// TestParseKeyMaterial 验证主密钥材料支持原始 / hex / base64 三种写法。
func TestParseKeyMaterial(t *testing.T) {
	raw := "0123456789abcdef0123456789abcdef"
	cases := []struct{ name, in string }{
		{"原始 32 字节", raw},
		{"hex", hex.EncodeToString([]byte(raw))},
		{"base64", base64.StdEncoding.EncodeToString([]byte(raw))},
	}
	for _, c := range cases {
		key, err := parseKeyMaterial(c.in)
		if err != nil {
			t.Fatalf("%s 解析失败: %v", c.name, err)
		}
		if string(key) != raw {
			t.Fatalf("%s 解析结果不符", c.name)
		}
	}
	if _, err := parseKeyMaterial("too-short"); err == nil {
		t.Fatal("非法密钥应报错")
	}
}
