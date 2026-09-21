package store

import (
	"path/filepath"
	"testing"
)

// TestRefundFlow 验证退款流水的登记、累计与状态流转:
// 部分退款后订单可退余额应正确递减,全额退款后累计退款等于订单金额。
func TestRefundFlow(t *testing.T) {
	Init(filepath.Join(t.TempDir(), "refund.db"))
	defer DB.Close()

	const orderNo = "DTEST0001"
	if _, err := DB.Exec(`INSERT INTO tb_order(order_no, total_amount, pay_status, create_time, update_time)
		VALUES(?,?,1,?,?)`, orderNo, 10000, Now(), Now()); err != nil {
		t.Fatalf("初始化订单失败: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO tb_payment(order_no, channel, amount, status, create_time, update_time)
		VALUES(?,?,?,?,?,?)`, orderNo, "wxpay", 10000, PayStatusPaid, Now(), Now()); err != nil {
		t.Fatalf("初始化支付流水失败: %v", err)
	}

	// 首次部分退款 30 元(3000 分)
	id, err := InsertRefund(Refund{OrderNo: orderNo, OrderID: 1, PaymentID: 1, RefundNo: "R1", Channel: "wxpay", Amount: 3000, Status: RefundStatusProcessing})
	if err != nil {
		t.Fatalf("登记退款单失败: %v", err)
	}
	if SumRefunded(orderNo) != 0 {
		t.Fatalf("处理中的退款不应计入已退金额")
	}
	if err := MarkRefund(id, RefundStatusSuccess, "WXREFUND1", ""); err != nil {
		t.Fatalf("更新退款状态失败: %v", err)
	}
	if err := AddOrderRefunded(orderNo, 3000); err != nil {
		t.Fatalf("累加订单退款失败: %v", err)
	}
	if got := SumRefunded(orderNo); got != 3000 {
		t.Fatalf("已退金额=%d 期望 3000", got)
	}

	// 第二次退剩余 70 元
	if _, err := InsertRefund(Refund{OrderNo: orderNo, OrderID: 1, PaymentID: 1, RefundNo: "R2", Channel: "wxpay", Amount: 7000, Status: RefundStatusSuccess}); err != nil {
		t.Fatalf("登记第二笔退款失败: %v", err)
	}
	_ = AddOrderRefunded(orderNo, 7000)
	if got := SumRefunded(orderNo); got != 10000 {
		t.Fatalf("累计已退=%d 期望 10000(全额)", got)
	}

	// 校验订单退款字段与列表
	var refundAmount int64
	var refundTime string
	if err := DB.QueryRow(`SELECT COALESCE(refund_amount,0), COALESCE(refund_time,'') FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&refundAmount, &refundTime); err != nil {
		t.Fatalf("查询订单退款字段失败: %v", err)
	}
	if refundAmount != 10000 || refundTime == "" {
		t.Fatalf("订单退款字段异常: amount=%d time=%q", refundAmount, refundTime)
	}
	list, err := ListRefunds(1)
	if err != nil || len(list) != 2 {
		t.Fatalf("退款记录数=%d 期望 2 (err=%v)", len(list), err)
	}
	if list[0].RefundNo != "R2" {
		t.Fatalf("退款记录应按倒序返回, 首条=%s", list[0].RefundNo)
	}

	// 退款失败的单据不应计入累计
	if _, err := InsertRefund(Refund{OrderNo: orderNo, OrderID: 1, PaymentID: 1, RefundNo: "R3", Channel: "wxpay", Amount: 5000, Status: RefundStatusFail}); err != nil {
		t.Fatalf("登记失败退款单出错: %v", err)
	}
	if got := SumRefunded(orderNo); got != 10000 {
		t.Fatalf("失败的退款被错误计入: %d", got)
	}
}
