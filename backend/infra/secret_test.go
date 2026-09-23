package infra

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
	InitSecretKeys()
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
	InitSecretKeys()

	const plain = "legacy-plain-key"
	if got := DecryptSecret(plain); got != plain {
		t.Fatalf("明文应原样返回,实际 %q", got)
	}
	if got := EncryptSecret(""); got != "" {
		t.Fatalf("空值应原样返回,实际 %q", got)
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
