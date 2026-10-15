package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// settingInitDB 初始化 SQLite 临时库,供配置读写/保存测试使用。
func settingInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "setting.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// TestNormalizeAmountValue 金额类配置的归一化:非法/负数回落 0,合法值保留两位小数。
func TestNormalizeAmountValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"5", "5"},
		{"3.5", "3.5"},
		{"0", "0"},
		{"", ""},
		{" 8 ", "8"},
		{"-3", "0.00"},
		{"abc", "0.00"},
		{"NaN", "0.00"},
		{"Inf", "0.00"},
	}
	for _, c := range cases {
		if got := NormalizeAmountValue(c.in); got != c.want {
			t.Fatalf("NormalizeAmountValue(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestPayFirstEnabledDefaultOff 先付模式默认必须是「关」(后付):
// 误开会让没收到钱的订单永不出单,风险远大于多出几张晚付的单。
func TestPayFirstEnabledDefaultOff(t *testing.T) {
	settingInitDB(t)
	if PayFirstEnabled() {
		t.Fatal("出厂默认应为后付模式(PayFirstEnabled=false)")
	}
	// 脏值/空值同样按后付处理。
	for _, v := range []string{"", "yes", "true", "01"} {
		if err := dao.SetSetting("pay_first_enabled", v); err != nil {
			t.Fatalf("写入配置失败: %v", err)
		}
		if PayFirstEnabled() {
			t.Fatalf("非 \"1\" 取值 %q 应判定为后付", v)
		}
	}
	if err := dao.SetSetting("pay_first_enabled", "1"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if !PayFirstEnabled() {
		t.Fatal("明确 \"1\" 应判定为先付模式")
	}
}

// TestCustomerSettingsExposesPayFirst 顾客公开配置必须下发先付开关:
// 顾客端 H5/小程序据此决定下单后是否直达支付页。
func TestCustomerSettingsExposesPayFirst(t *testing.T) {
	settingInitDB(t)
	if got := CustomerSettings()["pay_first_enabled"]; got != "0" {
		t.Fatalf("默认应下发 pay_first_enabled=0, got %q", got)
	}
	if err := dao.SetSetting("pay_first_enabled", "1"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if got := CustomerSettings()["pay_first_enabled"]; got != "1" {
		t.Fatalf("开启后应下发 pay_first_enabled=1, got %q", got)
	}
}

// TestSaveSettingsClampsNegativeAmount 负数的茶位费/满减配置在保存时回落为 0,
// 防止 total = 菜品金额 + 茶位费 - 优惠 被反算,影响全部新单与加菜重算。
func TestSaveSettingsClampsNegativeAmount(t *testing.T) {
	settingInitDB(t)

	SaveSettings(context.Background(), dto.Settings{
		SeatFeeEnabled:     "1",
		SeatFee:            "-5",
		PromotionEnabled:   "1",
		PromotionThreshold: "-100",
		PromotionDiscount:  "-10",
	}, "tester")

	for _, k := range []string{"seat_fee", "promotion_threshold", "promotion_discount"} {
		if got := GetSetting(k); got != "0.00" {
			t.Fatalf("负数配置 %s 应回落 0.00, got %q", k, got)
		}
	}
}

// TestSettingGetSet 校验配置项的写入/读取(含敏感项透明加解密)。
func TestSettingGetSet(t *testing.T) {
	settingInitDB(t)

	if err := SetSetting("shop_name", "测试店"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	if got := GetSetting("shop_name"); got != "测试店" {
		t.Fatalf("GetSetting(shop_name)=%q, want 测试店", got)
	}

	// 覆盖写
	if err := SetSetting("shop_name", "新店"); err != nil {
		t.Fatalf("SetSetting 覆盖写失败: %v", err)
	}
	if got := GetSetting("shop_name"); got != "新店" {
		t.Fatalf("覆盖写后 GetSetting=%q, want 新店", got)
	}

	// 缺失键返回空串
	if got := GetSetting("not_exist_key"); got != "" {
		t.Fatalf("缺失键应返回空串, got %q", got)
	}

	// 敏感项在无主密钥的测试环境下明文存取,round-trip 应一致。
	if err := SetSetting("wxpay_apiv3_key", "secret-value"); err != nil {
		t.Fatalf("SetSetting 敏感项失败: %v", err)
	}
	if got := GetSetting("wxpay_apiv3_key"); got != "secret-value" {
		t.Fatalf("敏感项 round-trip 不一致, got %q", got)
	}
}

// TestSettingFlag 校验「默认关」开关的下发兜底:只有明确 "1" 才算开启。
func TestSettingFlag(t *testing.T) {
	settingInitDB(t)
	const key = "print_enabled"

	cases := []struct {
		name string
		val  string // "" 表示删除键模拟缺失
		want string
	}{
		{"明确开启", "1", "1"},
		{"明确关闭", "0", "0"},
		{"空值", "", "0"},
		{"脏值", "yes", "0"},
		{"键缺失", "<missing>", "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.val == "<missing>" {
				if _, err := store.DB.Exec(`DELETE FROM tb_config WHERE cfg_key=?`, key); err != nil {
					t.Fatalf("删除配置键失败: %v", err)
				}
			} else if err := dao.SetSetting(key, c.val); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}
			if got := SettingFlag(key); got != c.want {
				t.Fatalf("SettingFlag(%s)=%q, want %q", key, got, c.want)
			}
		})
	}
}

// TestSettingNormFlagValue 校验写入侧开关归一化。
func TestSettingNormFlagValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1", "1"},
		{"0", "0"},
		{"", "0"},
		{"x", "0"},
		{"true", "0"},
		{"01", "0"},
	}
	for _, c := range cases {
		if got := normFlagValue(c.in); got != c.want {
			t.Fatalf("normFlagValue(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestSettingFlagDefaultOn 校验「默认开」开关:只有明确 "0" 才关闭。
func TestSettingFlagDefaultOn(t *testing.T) {
	settingInitDB(t)
	const key = "print_guest_show_discount"

	cases := []struct {
		name string
		val  string
		want string
	}{
		{"明确关闭", "0", "0"},
		{"明确开启", "1", "1"},
		{"脏值按默认开", "x", "1"},
		{"空值按默认开", "", "1"},
		{"键缺失按默认开", "<missing>", "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.val == "<missing>" {
				if _, err := store.DB.Exec(`DELETE FROM tb_config WHERE cfg_key=?`, key); err != nil {
					t.Fatalf("删除配置键失败: %v", err)
				}
			} else if err := dao.SetSetting(key, c.val); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}
			if got := SettingFlagDefaultOn(key); got != c.want {
				t.Fatalf("SettingFlagDefaultOn(%s)=%q, want %q", key, got, c.want)
			}
		})
	}
}

// TestListSettings 校验配置页回显:普通项回显、开关归一化、敏感项占位脱敏、指纹一致。
func TestListSettings(t *testing.T) {
	settingInitDB(t)

	if err := dao.SetSetting("shop_name", "回显店名"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if err := dao.SetSetting("seat_fee_enabled", "dirty"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if err := dao.SetSetting("wxpay_apiv3_key", "super-secret"); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}
	if err := dao.SetSetting("wx_app_secret", "wx-app-secret"); err != nil {
		t.Fatalf("写入敏感配置失败: %v", err)
	}

	got := ListSettings()
	if got.ShopName != "回显店名" {
		t.Fatalf("ShopName 回显 = %q, want 回显店名", got.ShopName)
	}
	if got.SeatFeeEnabled != "0" {
		t.Fatalf("脏开关值应归一化为 0, got %q", got.SeatFeeEnabled)
	}
	// 敏感项不回显明文:已配置回显 ****** 占位(让管理员看出「已设置」),
	// 响应中绝不能出现真实密钥。
	if got.WxpayApiv3Key != SecretMask {
		t.Fatalf("敏感项应回显占位符 %q, got %q", SecretMask, got.WxpayApiv3Key)
	}
	if strings.Contains(got.WxpayApiv3Key, "super-secret") {
		t.Fatalf("敏感项不得回显明文")
	}
	// 小程序 AppSecret 同样是敏感项:拿到它就能以小程序身份换 access_token。
	if got.WxAppSecret != SecretMask {
		t.Fatalf("小程序 AppSecret 应回显占位符, got %q", got.WxAppSecret)
	}
	// 未配置的敏感项回显空串,与「已配置」区分开。
	if got.FeieUkey != "" || got.AgentToken != "" {
		t.Fatalf("未配置的敏感项应回显空串, got feie_ukey=%q agent_token=%q", got.FeieUkey, got.AgentToken)
	}
	// 默认开开关:种子默认 "1"。
	if got.PrintGuestShowDiscount != "1" {
		t.Fatalf("默认开开关应回显 1, got discount=%q", got.PrintGuestShowDiscount)
	}
	if got.Version != dao.SettingsFingerprint(ManagedSettingKeys) {
		t.Fatalf("Version 与指纹不一致: got %q want %q", got.Version, dao.SettingsFingerprint(ManagedSettingKeys))
	}
}

// TestSaveSettingsConflict 校验乐观锁冲突时不落库。
func TestSaveSettingsConflict(t *testing.T) {
	settingInitDB(t)
	if err := dao.SetSetting("shop_name", "旧值"); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	settings := dto.Settings{ShopName: "新值", Version: "deadbeefdeadbeef"}
	detail, conflict := SaveSettings(context.Background(), settings, "admin")
	if !conflict {
		t.Fatal("版本指纹不一致应判定为冲突")
	}
	if detail != "" {
		t.Fatalf("冲突时不应生成摘要, got %q", detail)
	}
	if got := dao.GetSetting("shop_name"); got != "旧值" {
		t.Fatalf("冲突时不应落库, shop_name=%q", got)
	}
}

// TestSaveSettingsNormal 校验旧前端(不传 version)正常保存并生成变更摘要。
func TestSaveSettingsNormal(t *testing.T) {
	settingInitDB(t)
	// 先保存一次把库中普通项收敛为快照(其余字段清空),
	// 后续只改 shop_name,变更摘要不会被种子默认值变化干扰。
	if _, conflict := SaveSettings(context.Background(), dto.Settings{ShopName: "旧值"}, "admin"); conflict {
		t.Fatal("首次保存不应冲突")
	}

	detail, conflict := SaveSettings(context.Background(), dto.Settings{ShopName: "新店"}, "admin")
	if conflict {
		t.Fatal("空 version 应跳过乐观锁校验")
	}
	if got := dao.GetSetting("shop_name"); got != "新店" {
		t.Fatalf("保存后 shop_name=%q, want 新店", got)
	}
	if detail != "修改配置项: shop_name: 旧值 → 新店" {
		t.Fatalf("变更摘要 = %q", detail)
	}
}

// TestSaveSettingsNoChange 校验内容未变时摘要为「无字段变更」。
func TestSaveSettingsNoChange(t *testing.T) {
	settingInitDB(t)
	if _, conflict := SaveSettings(context.Background(), dto.Settings{ShopName: "店A"}, "admin"); conflict {
		t.Fatal("首次保存不应冲突")
	}
	// 用回显快照再保存一次:普通项与库中一致,敏感项留空,应判定为无变更。
	detail, conflict := SaveSettings(context.Background(), ListSettings(), "admin")
	if conflict {
		t.Fatal("指纹一致不应冲突")
	}
	if detail != "保存系统配置，无字段变更" {
		t.Fatalf("无变更摘要 = %q", detail)
	}
}

// TestSaveSettingsClearAgentToken 校验「清空代理令牌」:敏感项留空表示不修改,
// 只有 agent_token_clear="1" 能真正置空并留下审计痕迹。
func TestSaveSettingsClearAgentToken(t *testing.T) {
	settingInitDB(t)
	if _, conflict := SaveSettings(context.Background(), dto.Settings{AgentToken: "agent-secret"}, "admin"); conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("agent_token"); got != "agent-secret" {
		t.Fatalf("敏感项应落库, got %q", got)
	}
	// 敏感项留空:不修改(与 wxpay_apiv3_key 同一语义)。
	if _, conflict := SaveSettings(context.Background(), dto.Settings{ShopName: "店A"}, "admin"); conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("agent_token"); got != "agent-secret" {
		t.Fatalf("留空不应清空代理令牌, got %q", got)
	}

	detail, conflict := SaveSettings(context.Background(), dto.Settings{AgentTokenClear: "1"}, "admin")
	if conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("agent_token"); got != "" {
		t.Fatalf("清空指令应把代理令牌置空, got %q", got)
	}
	if !strings.Contains(detail, "清空代理令牌") {
		t.Fatalf("摘要应记录清空调作, got %q", detail)
	}
}

// TestSaveSettingsFlagNormalize 校验开关脏值在保存时被归一化。
func TestSaveSettingsFlagNormalize(t *testing.T) {
	settingInitDB(t)
	if _, conflict := SaveSettings(context.Background(), dto.Settings{SeatFeeEnabled: "abc"}, "admin"); conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("seat_fee_enabled"); got != "0" {
		t.Fatalf("脏开关值应归一化为 0 落库, got %q", got)
	}
}

// TestSaveSettingsSecret 校验敏感项保存与留空不覆盖。
func TestSaveSettingsSecret(t *testing.T) {
	settingInitDB(t)
	if _, conflict := SaveSettings(context.Background(), dto.Settings{WxpayApiv3Key: "new-secret"}, "admin"); conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("wxpay_apiv3_key"); got != "new-secret" {
		t.Fatalf("敏感项应落库, got %q", got)
	}

	// 再次保存但敏感项留空:不应覆盖已有密钥。
	if _, conflict := SaveSettings(context.Background(), dto.Settings{ShopName: "随便改个名"}, "admin"); conflict {
		t.Fatal("保存不应冲突")
	}
	if got := dao.GetSetting("wxpay_apiv3_key"); got != "new-secret" {
		t.Fatalf("敏感项留空不应覆盖, got %q", got)
	}
}

// TestDescribeSettingChange 校验变更摘要的组装:新旧值、空值占位、敏感项脱敏、截断。
func TestDescribeSettingChange(t *testing.T) {
	t.Run("无变更", func(t *testing.T) {
		got := describeSettingChange(map[string]string{}, map[string]string{}, map[string]string{})
		if got != "保存系统配置，无字段变更" {
			t.Fatalf("无变更摘要 = %q", got)
		}
	})
	t.Run("普通项变更含新旧值", func(t *testing.T) {
		got := describeSettingChange(
			map[string]string{"shop_name": "旧"},
			map[string]string{"shop_name": "新"},
			map[string]string{},
		)
		if got != "修改配置项: shop_name: 旧 → 新" {
			t.Fatalf("变更摘要 = %q", got)
		}
	})
	t.Run("空值显示占位", func(t *testing.T) {
		got := describeSettingChange(
			map[string]string{"a": "", "b": "x"},
			map[string]string{"a": "x", "b": ""},
			map[string]string{},
		)
		if !strings.Contains(got, "a: (空) → x") || !strings.Contains(got, "b: x → (空)") {
			t.Fatalf("空值应显示 (空), got %q", got)
		}
	})
	t.Run("敏感项只记键名", func(t *testing.T) {
		got := describeSettingChange(
			map[string]string{},
			map[string]string{},
			map[string]string{"wxpay_apiv3_key": "secret"},
		)
		if !strings.Contains(got, "wxpay_apiv3_key(敏感项,值不记录)") {
			t.Fatalf("敏感项应只记键名, got %q", got)
		}
	})
	t.Run("超长截断", func(t *testing.T) {
		got := describeSettingChange(
			map[string]string{"k": ""},
			map[string]string{"k": strings.Repeat("很", 500)},
			map[string]string{},
		)
		if !strings.HasSuffix(got, "…") || len([]rune(got)) != 401 {
			t.Fatalf("超长摘要应截断为 400 字符+省略号, 长度=%d", len([]rune(got)))
		}
	})
}

// TestBlankIfEmpty 校验空值占位显示。
func TestBlankIfEmpty(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "(空)"},
		{"   ", "(空)"},
		{"\t", "(空)"},
		{"x", "x"},
		{" x ", " x "},
	}
	for _, c := range cases {
		if got := blankIfEmpty(c.in); got != c.want {
			t.Fatalf("blankIfEmpty(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}
