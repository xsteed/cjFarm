package dao

import (
	"path/filepath"
	"testing"
	"time"

	"dining-system/infrastructure/store"
)

// TestGetSettingCache 配置缓存的四条约定:
//  1. 命中:同键重复读取不重复查库(查询计数不增长);
//  2. 写穿:SetSetting 之后立即读到新值(管理后台保存即刻生效);
//  3. TTL 兜底:绕过 dao 的裸 SQL 改写(多实例另一实例的写入、迁移脚本)最多延迟
//     TTL 可见,过期后必须能读到;
//  4. 换库:store.Init 重建库句柄后旧缓存整体作废 —— 测试每个用例各建一个库,
//     隔离正依赖这一点。
func TestGetSettingCache(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "cache.db"))

	if err := SetSetting("cache_probe", "v1"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	ResetSettingQueries()
	if got := GetSetting("cache_probe"); got != "v1" {
		t.Fatalf("首次读取应为 v1, got %q", got)
	}
	if got := GetSetting("cache_probe"); got != "v1" {
		t.Fatalf("二次读取应为 v1, got %q", got)
	}
	if n := SettingQueries(); n != 1 {
		t.Fatalf("第二次读取应命中缓存, 实际发起了 %d 次查询", n)
	}

	// 写穿:保存后立即读到新值。
	if err := SetSetting("cache_probe", "v2"); err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if got := GetSetting("cache_probe"); got != "v2" {
		t.Fatalf("保存后应立即读到新值, got %q", got)
	}

	// TTL 兜底:绕过 dao 的裸 SQL 改写,过期后必须可见。
	oldTTL := settingCacheTTL
	settingCacheTTL = time.Millisecond
	t.Cleanup(func() { settingCacheTTL = oldTTL })
	time.Sleep(2 * time.Millisecond)
	if _, err := store.DB.Exec(`UPDATE tb_config SET cfg_value='v3' WHERE cfg_key='cache_probe'`); err != nil {
		t.Fatalf("裸 SQL 改写失败: %v", err)
	}
	if got := GetSetting("cache_probe"); got != "v3" {
		t.Fatalf("TTL 过期后应看到绕行写入的值, got %q", got)
	}

	// 换库:重建句柄后旧库的缓存不得泄漏到新库(cache_probe 不是种子键)。
	if store.DB != nil {
		_ = store.DB.Close()
	}
	store.Init(filepath.Join(t.TempDir(), "cache2.db"))
	t.Cleanup(func() {
		if store.DB != nil {
			_ = store.DB.Close()
		}
	})
	if got := GetSetting("cache_probe"); got != "" {
		t.Fatalf("换库后不应读到旧库的缓存值, got %q", got)
	}
}

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
