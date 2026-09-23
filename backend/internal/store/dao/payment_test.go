package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

// TestInsertPaymentUniqueIndex 回归验证:待支付流水不能因 (channel, channel_trade_no) 唯一索引
// 而丢失。历史 bug:待支付阶段写入空交易号,同渠道第二笔订单的流水会撞唯一约束插入失败,
// 导致支付回调无法回写、订单无法在线退款。
func TestInsertPaymentUniqueIndex(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "pay.db"))
	defer store.DB.Close()

	if err := InsertPayment("A", "wxpay", 1000, "pre1"); err != nil {
		t.Fatalf("第一笔流水写入失败: %v", err)
	}
	if err := InsertPayment("B", "wxpay", 2000, "pre2"); err != nil {
		t.Fatalf("同渠道第二笔流水写入失败(唯一索引冲突回归): %v", err)
	}
	if err := InsertPayment("B", "alipay", 2000, "pre3"); err != nil {
		t.Fatalf("跨渠道流水写入失败: %v", err)
	}

	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_payment`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("流水条数=%d 期望 3 (err=%v)", n, err)
	}

	// 同一订单+渠道重复发起支付应复用待支付流水,而不是堆积记录。
	if err := InsertPayment("A", "wxpay", 1500, "pre1-new"); err != nil {
		t.Fatalf("重复发起支付失败: %v", err)
	}
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_payment WHERE order_no='A'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("订单A流水条数=%d 期望 1 (err=%v)", n, err)
	}
	var amount int64
	if err := store.DB.QueryRow(`SELECT amount FROM tb_payment WHERE order_no='A'`).Scan(&amount); err != nil || amount != 1500 {
		t.Fatalf("复用流水未更新金额: %d (err=%v)", amount, err)
	}

	// 两笔订单分别回调支付成功,都应正确回写。
	for _, c := range []struct {
		orderNo, tradeNo string
	}{{"A", "TXN-A"}, {"B", "TXN-B"}} {
		changed, err := MarkPaymentPaid(c.orderNo, "wxpay", c.tradeNo)
		if err != nil || !changed {
			t.Fatalf("订单%s 回写支付状态失败 changed=%v err=%v", c.orderNo, changed, err)
		}
	}
	if got := SumRefunded("A"); got != 0 {
		t.Fatalf("未退款订单已退金额应为 0, 实际 %d", got)
	}
}

// TestRefundAfterPartialRefund 覆盖:部分退款后支付流水被标记为「已退款」时,
// 仍能查到该流水继续退剩余金额(退款查询条件需包含已退款状态)。
func TestRefundAfterPartialRefund(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "pay2.db"))
	defer store.DB.Close()

	const orderNo = "C"
	if err := InsertPayment(orderNo, "wxpay", 10000, "pre"); err != nil {
		t.Fatalf("写入流水失败: %v", err)
	}
	paymentID, status, _, err := GetPayment(orderNo, "wxpay")
	if err != nil {
		t.Fatalf("查询流水失败: %v", err)
	}
	if status != PayStatusPending {
		t.Fatalf("新流水状态=%d 期望待支付", status)
	}
	if _, err := store.DB.Exec(`UPDATE tb_payment SET status=? WHERE payment_id=?`, PayStatusRefunded, paymentID); err != nil {
		t.Fatalf("模拟部分退款后状态失败: %v", err)
	}

	// 退款处理器使用的查询条件:已支付或已退款
	var id int
	if err := store.DB.QueryRow(`SELECT payment_id FROM tb_payment WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC LIMIT 1`,
		orderNo, PayStatusPaid, PayStatusRefunded).Scan(&id); err != nil {
		t.Fatalf("部分退款后找不到可继续退款的流水: %v", err)
	}
	if id != paymentID {
		t.Fatalf("匹配到错误流水: %d", id)
	}
}
