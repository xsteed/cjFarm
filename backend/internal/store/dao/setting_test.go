package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

// TestSettingsFingerprint 验证配置指纹的行为约定:
//   - 受管键变化 → 指纹变化;同内容重复计算 → 稳定;键序无关;
//   - 键集之外的内部键(如代理心跳 agent_last_seen)变化 → 指纹不变,
//     否则代理每次心跳都会把管理员的保存误判为并发冲突。
func TestSettingsFingerprint(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "fingerprint.db"))
	defer store.DB.Close()

	keys := []string{"shop_name", "seat_fee", "wxpay_apiv3_key"}
	fp1 := SettingsFingerprint(keys)
	if len(fp1) != 16 {
		t.Fatalf("指纹应为 16 位 hex, got %q", fp1)
	}

	// 同内容稳定 + 键序无关
	if fp2 := SettingsFingerprint(keys); fp2 != fp1 {
		t.Fatalf("同内容指纹不稳定: %q vs %q", fp1, fp2)
	}
	if fp := SettingsFingerprint([]string{"seat_fee", "shop_name", "wxpay_apiv3_key"}); fp != fp1 {
		t.Fatalf("指纹应与键序无关: %q vs %q", fp, fp1)
	}

	// 受管键变化 → 指纹变化
	if err := SetSetting("shop_name", "指纹测试店铺"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	fp3 := SettingsFingerprint(keys)
	if fp3 == fp1 {
		t.Fatal("受管键变化后指纹未变化")
	}

	// 键集外的键变化 → 指纹不受影响(agent_last_seen 是代理心跳,秒级变化)
	if err := SetSetting("agent_last_seen", "2026-09-22 10:00:00"); err != nil {
		t.Fatalf("写入心跳键失败: %v", err)
	}
	if fp := SettingsFingerprint(keys); fp != fp3 {
		t.Fatal("键集外的变化不应影响指纹")
	}

	// 敏感项经加密落库,同一明文重复保存(新 nonce、新密文)指纹应稳定:
	// 指纹基于解密后的明文,加密层的变化不应影响乐观锁判断。
	if err := SetSetting("wxpay_apiv3_key", "same-plain-value"); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}
	fp4 := SettingsFingerprint(keys)
	if err := SetSetting("wxpay_apiv3_key", "same-plain-value"); err != nil {
		t.Fatalf("重复写入敏感配置失败: %v", err)
	}
	if fp := SettingsFingerprint(keys); fp != fp4 {
		t.Fatal("同一明文重复加密不应改变指纹")
	}
}
