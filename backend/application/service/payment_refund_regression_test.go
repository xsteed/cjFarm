package service

import (
	"context"
	"strings"
	"testing"
)

// TestRefundAmountRejectsNegative 退款金额为负数必须被拒绝。
//
// 负数不是「全额」的同义词:旧实现按 `amount <= 0` 判全额,请求体传 amount=-1
// 会退掉整单剩余金额(该值来自请求体,没有其它校验)。
func TestRefundAmountRejectsNegative(t *testing.T) {
	paymentSvcInit(t)

	cents, err := RefundAmount(context.Background(), "NO-SUCH-ORDER", 10000, -1)
	if err == nil || !strings.Contains(err.Error(), "不能为负数") {
		t.Fatalf("负数退款应被拒绝, got cents=%d err=%v", cents, err)
	}

	// 0/留空仍表示全额退款。
	cents, err = RefundAmount(context.Background(), "NO-SUCH-ORDER", 10000, 0)
	if err != nil || cents != 10000 {
		t.Fatalf("amount=0 应表示全额退款 10000 分, got cents=%d err=%v", cents, err)
	}
}
