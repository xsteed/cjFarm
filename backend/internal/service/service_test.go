package service

import (
	"regexp"
	"testing"
)

func TestGenOrderNo(t *testing.T) {
	re := regexp.MustCompile(`^D\d{14}[0-9a-f]{16}$`)
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		no := GenOrderNo()
		if !re.MatchString(no) {
			t.Fatalf("订单号格式非法: %q", no)
		}
		if seen[no] {
			t.Fatalf("订单号发生碰撞: %q", no)
		}
		seen[no] = true
	}
}

func TestRandHex(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{16}$`)
	if !re.MatchString(randHex(16)) {
		t.Fatalf("randHex(16) 应为 16 位十六进制, got %q", randHex(16))
	}
	// 不同长度
	if len(randHex(32)) != 32 {
		t.Fatalf("randHex(32) 长度应为 32")
	}
}
