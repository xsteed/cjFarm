package dao

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/infra"
	"dining-system/internal/store"
)

// setupSecretEnv 隔离主密钥环境:只使用临时目录,不污染真实部署。
func setupSecretEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIG_MASTER_KEY", "")
	t.Setenv("MASTER_KEY_PATH", filepath.Join(t.TempDir(), "master.key"))
}

// TestSecretStoredEncrypted 验证敏感项经 SetSetting 落库后为密文,GetSetting 能透明读回明文,
// 普通配置项不受影响,且重复迁移是幂等的。
func TestSecretStoredEncrypted(t *testing.T) {
	setupSecretEnv(t)
	store.Init(filepath.Join(t.TempDir(), "secret.db"))
	defer store.DB.Close()
	infra.InitSecretKeys()

	const key = "wxpay_apiv3_key"
	const plain = "abcdefghijklmnopqrstuvwxyz012345"
	if err := SetSetting(key, plain); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}

	var raw string
	if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, key).Scan(&raw); err != nil {
		t.Fatalf("读取原始值失败: %v", err)
	}
	if raw == plain {
		t.Fatal("敏感项在库中仍为明文")
	}
	if !infra.IsEncrypted(raw) {
		t.Fatalf("库中值缺少密文前缀: %s", raw)
	}
	if got := GetSetting(key); got != plain {
		t.Fatalf("GetSetting=%q 期望 %q", got, plain)
	}

	// 普通配置项不应被加密
	if err := SetSetting("shop_name", "测试餐厅"); err != nil {
		t.Fatal(err)
	}
	var rawShop string
	if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key='shop_name'`).Scan(&rawShop); err != nil {
		t.Fatal(err)
	}
	if rawShop != "测试餐厅" {
		t.Fatalf("普通配置被意外加密: %s", rawShop)
	}

	// 迁移必须幂等:已加密的值不能被二次加密
	MigrateSecrets()
	if got := GetSetting(key); got != plain {
		t.Fatalf("重复迁移后取值异常: %q", got)
	}
}

// TestSecretMigratePlaintext 验证历史明文敏感配置在启动迁移后变为密文。
func TestSecretMigratePlaintext(t *testing.T) {
	setupSecretEnv(t)
	store.Init(filepath.Join(t.TempDir(), "migrate.db"))
	defer store.DB.Close()

	const plain = "legacy-plaintext-apiv3-key-value!"
	if _, err := store.DB.Exec(`INSERT OR REPLACE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_apiv3_key', ?)`, plain); err != nil {
		t.Fatal(err)
	}

	infra.InitSecretKeys()
	MigrateSecrets()

	var raw string
	if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key='wxpay_apiv3_key'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !infra.IsEncrypted(raw) {
		t.Fatalf("历史明文未被迁移为密文: %s", raw)
	}
	if strings.Contains(raw, plain) {
		t.Fatal("迁移后仍能在库中看到明文")
	}
	if got := GetSetting("wxpay_apiv3_key"); got != plain {
		t.Fatalf("迁移后读取=%q 期望 %q", got, plain)
	}
}

// TestSecretWrongMasterKey 验证主密钥不符时解密返回空(而不是崩溃或吐出密文)。
func TestSecretWrongMasterKey(t *testing.T) {
	setupSecretEnv(t)
	store.Init(filepath.Join(t.TempDir(), "wrong-key.db"))
	defer store.DB.Close()
	infra.InitSecretKeys()

	const key = "wxpay_apiv3_key"
	if err := SetSetting(key, "some-secret-value"); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}

	t.Setenv("CONFIG_MASTER_KEY", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	infra.InitSecretKeys()

	if got := GetSetting(key); got != "" {
		t.Fatalf("主密钥不符时应返回空,实际 %q", got)
	}
}

// TestSecretRotateMasterKey 验证主密钥轮换:换用新密钥 + 声明 MASTER_KEY_OLD 后,
// 存量密文被改写为新密钥加密,GetSetting 仍能读回原明文(不改写则一律解密失败)。
func TestSecretRotateMasterKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rotate.db")
	oldKeyPath := filepath.Join(dir, "old-master.key")
	newKeyPath := filepath.Join(dir, "new-master.key")

	t.Setenv("CONFIG_MASTER_KEY", "")
	t.Setenv("MASTER_KEY_PATH", oldKeyPath)

	store.Init(dbPath)
	defer store.DB.Close()
	infra.InitSecretKeys() // 生成旧密钥并落 oldKeyPath

	const key = "wxpay_apiv3_key"
	const plain = "rotate-secret-value-0123456789ab"
	if err := SetSetting(key, plain); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}
	oldKeyMaterial, err := os.ReadFile(oldKeyPath)
	if err != nil {
		t.Fatalf("读取旧密钥失败: %v", err)
	}

	// 模拟密钥更替:新密钥文件不存在(自动生成新密钥),旧密钥经 MASTER_KEY_OLD 声明。
	t.Setenv("MASTER_KEY_PATH", newKeyPath)
	t.Setenv("MASTER_KEY_OLD", strings.TrimSpace(string(oldKeyMaterial)))

	infra.InitSecretKeys()
	MigrateSecrets()
	if old, ok := infra.OldMasterKey(); ok {
		RotateSecrets(old)
	}

	if got := GetSetting(key); got != plain {
		t.Fatalf("轮换后读取=%q 期望 %q", got, plain)
	}
	// 轮换后不留痕迹:再次 InitSecretKeys(相当于忘了移除 MASTER_KEY_OLD)应幂等通过。
	infra.InitSecretKeys()
	MigrateSecrets()
	if old, ok := infra.OldMasterKey(); ok {
		RotateSecrets(old)
	}
	if got := GetSetting(key); got != plain {
		t.Fatalf("重复轮换后读取=%q 期望 %q", got, plain)
	}
}
