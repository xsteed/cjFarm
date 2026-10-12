package feie

import "testing"

// TestSign 校验签名算法: sha1(user + UKEY + stime) 的 40 位小写十六进制。
// 期望值是外部独立算出来的(sha1sum),不是用同一段代码生成的 —— 否则测不出错。
func TestSign(t *testing.T) {
	got := Sign("demo_user", "UKEY", "1234567890")
	want := "92bd75ff96974fbba84590de3bc1f101f8ff56c1"
	if got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
	if len(got) != 40 || got != lower(got) {
		t.Fatalf("签名必须是 40 位小写十六进制, got %q", got)
	}
}

// TestErrorMessage 错误码必须翻成中文可读文案,未知码兜底带 ret。
func TestErrorMessage(t *testing.T) {
	if got := errorMessage(1003, ""); got == "" {
		t.Error("已知错误码应返回非空中文文案")
	}
	if got := errorMessage(9999, "boom"); got != "飞鹅接口返回错误(ret=9999): boom" {
		t.Errorf("未知错误码应兜底为 ret 提示, got %q", got)
	}
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}
