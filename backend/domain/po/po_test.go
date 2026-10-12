package po

import "testing"

// TestShortOrderNo 校验短号派生:取订单号尾部 6 位并转大写,
// 短于 6 位时原样返回,保证顾客端/商家端展示一致。
func TestShortOrderNo(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"D202609200122298a45bf800674d485", "74D485"},
		{"D202609200122298a45bf800674dABC", "74DABC"},
		{"ABCDEF", "ABCDEF"},
		{"abc", "ABC"},
		{"", ""},
		{"  D20260920012229abcdef  ", "ABCDEF"},
	}
	for _, c := range cases {
		if got := ShortOrderNo(c.in); got != c.want {
			t.Errorf("ShortOrderNo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDisplayNo 校验可报读短号的取值优先级:
// 有取餐号时用它(纯数字、当天唯一、够短);没有时退回单号尾 6 位,
// 保证历史订单与手工补录的行永远有值可显示。
func TestDisplayNo(t *testing.T) {
	const orderNo = "D202609200122298a45bf800674d485"
	cases := []struct {
		name     string
		pickupNo int
		want     string
	}{
		{"有取餐号优先用取餐号", 12, "12"},
		{"取餐号从 1 起而非 0", 1, "1"},
		{"历史订单退回尾6位", 0, "74D485"},
		{"负数按未分配处理", -1, "74D485"},
	}
	for _, c := range cases {
		if got := DisplayNo(c.pickupNo, orderNo); got != c.want {
			t.Errorf("%s: DisplayNo(%d, %q) = %q, want %q", c.name, c.pickupNo, orderNo, got, c.want)
		}
	}
}

// TestMoneyRoundTrip 校验金额工具往返与 Round2。
func TestMoneyRoundTrip(t *testing.T) {
	if got := ToCents(66.66); got != 6666 {
		t.Errorf("ToCents(66.66) = %d, want 6666", got)
	}
	if got := ToYuan(6666); got != 66.66 {
		t.Errorf("ToYuan(6666) = %v, want 66.66", got)
	}
	if got := Round2(66.666); got != 66.67 {
		t.Errorf("Round2(66.666) = %v, want 66.67", got)
	}
	if got := ToYuan(ToCents(66.66)); got != 66.66 {
		t.Errorf("ToYuan(ToCents(66.66)) = %v, want 66.66", got)
	}
}
