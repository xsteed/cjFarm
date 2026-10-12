package po

import (
	"math"
	"testing"
)

// TestPoToCentsMore 补充金额「元→分」的边界:零、负数、整元、两位小数与 NaN。
func TestPoToCentsMore(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want int64
	}{
		{"零", 0, 0},
		{"一分", 0.01, 1},
		{"九十九分", 0.99, 99},
		{"整元", 12, 1200},
		{"两位小数", 12.34, 1234},
		{"负数两位小数", -12.34, -1234},
		{"四舍五入进位", 0.005, 1},
		{"四舍五入舍去", 0.004, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToCents(c.in); got != c.want {
				t.Errorf("ToCents(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
	// NaN 不 panic:结果按平台语义返回,这里只保证可调用。
	_ = ToCents(math.NaN())
}

// TestPoToYuanMore 补充金额「分→元」的边界:零、负数与大额。
func TestPoToYuanMore(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want float64
	}{
		{"零", 0, 0},
		{"一分", 1, 0.01},
		{"整元", 1200, 12},
		{"两位小数", 1234, 12.34},
		{"负数", -1234, -12.34},
		{"大额整元", 12345678900, 123456789},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToYuan(c.in); got != c.want {
				t.Errorf("ToYuan(%d) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestPoRound2More 补充四舍五入边界:零、正负与 NaN。
func TestPoRound2More(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"零", 0, 0},
		{"舍去", 0.004, 0},
		{"进位", 0.005, 0.01},
		{"负数进位", -0.005, -0.01},
		{"两位保留", 1.234, 1.23},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Round2(c.in); got != c.want {
				t.Errorf("Round2(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
	if got := Round2(math.NaN()); !math.IsNaN(got) {
		t.Errorf("Round2(NaN) = %v, want NaN", got)
	}
}

// TestPoShortOrderNoMore 补充短号派生边界:恰好 6 位、7 位、超长与纯空格。
func TestPoShortOrderNoMore(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"恰好六位", "ABCDEF", "ABCDEF"},
		{"七位取后六位", "aBCDEFG", "BCDEFG"},
		{"超长取后六位", "D20260921120000abcdef0123456789", "456789"},
		{"带空白截取", "  1234567  ", "234567"},
		{"纯空格", "   ", ""},
		{"小写转大写", "d20260", "D20260"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShortOrderNo(c.in); got != c.want {
				t.Errorf("ShortOrderNo(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestPoUserDisplayName 校验留痕显示名:优先真实姓名,空/纯空白回退登录名。
func TestPoUserDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		realName string
		username string
		want     string
	}{
		{"优先中文姓名", "张三", "zhangsan", "张三"},
		{"空姓名回退", "", "zhangsan", "zhangsan"},
		{"纯空白回退", "   ", "zhangsan", "zhangsan"},
		{"都为空", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := &User{RealName: c.realName, Username: c.username}
			if got := u.DisplayName(); got != c.want {
				t.Errorf("DisplayName() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestPoPrinterProviderPredicates 校验打印机接入方式的三个谓词。
func TestPoPrinterProviderPredicates(t *testing.T) {
	cases := []struct {
		provider string
		isFeie   bool
		isAgent  bool
		isDirect bool
	}{
		{PrinterProviderFeie, true, false, false},
		{PrinterProviderAgent, false, true, false},
		{PrinterProviderTCP, false, false, true},
		{"", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			p := &Printer{Provider: c.provider}
			if got := p.IsFeie(); got != c.isFeie {
				t.Errorf("IsFeie() = %v, want %v", got, c.isFeie)
			}
			if got := p.IsAgent(); got != c.isAgent {
				t.Errorf("IsAgent() = %v, want %v", got, c.isAgent)
			}
			if got := p.IsDirect(); got != c.isDirect {
				t.Errorf("IsDirect() = %v, want %v", got, c.isDirect)
			}
		})
	}
}

// TestPoPrinterEffectiveCopies 校验打印份数兜底:下限 1、上限 5。
func TestPoPrinterEffectiveCopies(t *testing.T) {
	cases := []struct {
		copies int
		want   int
	}{
		{0, 1},
		{-1, 1},
		{1, 1},
		{3, 3},
		{5, 5},
		{6, 5},
		{100, 5},
	}
	for _, c := range cases {
		p := &Printer{Copies: c.copies}
		if got := p.EffectiveCopies(); got != c.want {
			t.Errorf("EffectiveCopies(%d) = %d, want %d", c.copies, got, c.want)
		}
	}
}

// TestPoConstants 守护领域常量取值,防止无意识改动破坏落库/展示约定。
func TestPoConstants(t *testing.T) {
	if DelFlagOK != "0" || DelFlagDeleted != "1" {
		t.Errorf("软删除标记异常: %q / %q", DelFlagOK, DelFlagDeleted)
	}
	if UserStatusEnabled != 1 || UserStatusDisabled != 0 {
		t.Errorf("员工状态常量异常: %d / %d", UserStatusEnabled, UserStatusDisabled)
	}
	if PrinterProviderTCP != "tcp" || PrinterProviderFeie != "feie" || PrinterProviderAgent != "agent" {
		t.Errorf("打印机接入方式常量异常")
	}
	if PrintJobMaxAttempts != 3 {
		t.Errorf("PrintJobMaxAttempts = %d, want 3", PrintJobMaxAttempts)
	}
	if RefundStatusSuccess != 1 || RefundStatusFail != 2 || RefundStatusProcessing != 0 {
		t.Errorf("退款状态常量异常")
	}
	if OperStatusSuccess != 1 || OperStatusFail != 0 {
		t.Errorf("操作结果常量异常")
	}
	if UrgeCooldownSecond != 180 {
		t.Errorf("UrgeCooldownSecond = %d, want 180", UrgeCooldownSecond)
	}
}
