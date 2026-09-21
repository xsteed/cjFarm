package handler

import (
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

// TestApplyRefundSuccess 覆盖退款成功后的回写链路(该路径需真实渠道到账,HTTP 层在未配置 key 时无法触发):
// 退款单置成功、订单累计退款金额累加、实收金额(paid_amount)同步扣减、全额退完时支付流水标记已退款。
func TestApplyRefundSuccess(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "handler_refund.db"))
	defer store.DB.Close()

	const orderNo = "H1"
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, total_amount, pay_status, paid_amount, create_time, update_time)
		VALUES(?,?,1,?,?,?)`, orderNo, 10000, 10000, store.Now(), store.Now()); err != nil {
		t.Fatalf("初始化订单失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_payment(order_no, channel, channel_trade_no, amount, status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?)`, orderNo, "wxpay", "TXN1", 10000, store.PayStatusPaid, store.Now(), store.Now()); err != nil {
		t.Fatalf("初始化支付流水失败: %v", err)
	}
	var orderID int
	var paymentID int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&orderID); err != nil {
		t.Fatalf("取订单ID失败: %v", err)
	}
	if err := store.DB.QueryRow(`SELECT payment_id FROM tb_payment WHERE order_no=?`, orderNo).Scan(&paymentID); err != nil {
		t.Fatalf("取流水ID失败: %v", err)
	}

	orderState := func() (refundCents, paidCents int64) {
		if err := store.DB.QueryRow(`SELECT COALESCE(refund_amount,0), COALESCE(paid_amount,0) FROM tb_order WHERE order_no=?`, orderNo).
			Scan(&refundCents, &paidCents); err != nil {
			t.Fatalf("查询订单金额失败: %v", err)
		}
		return
	}

	// 1) 部分退款 30 元
	rid, err := store.InsertRefund(store.Refund{
		OrderNo: orderNo, OrderID: orderID, PaymentID: paymentID,
		RefundNo: "HR1", Channel: "wxpay", Amount: 3000, Status: store.RefundStatusProcessing,
	})
	if err != nil {
		t.Fatalf("登记退款单失败: %v", err)
	}
	applyRefundSuccess(rid, orderID, paymentID, orderNo, 3000, "WXR1")

	if got := store.SumRefunded(orderNo); got != 3000 {
		t.Fatalf("累计已退=%d 期望 3000", got)
	}
	refundCents, paidCents := orderState()
	if refundCents != 3000 || paidCents != 7000 {
		t.Fatalf("部分退款后 refund_amount=%d paid_amount=%d 期望 3000/7000(营收应扣减退款)", refundCents, paidCents)
	}
	r, err := store.GetRefund(rid)
	if err != nil || r.Status != store.RefundStatusSuccess || r.ChannelRefundNo != "WXR1" {
		t.Fatalf("退款单状态异常: %+v err=%v", r, err)
	}
	var payStatus int
	if err := store.DB.QueryRow(`SELECT status FROM tb_payment WHERE payment_id=?`, paymentID).Scan(&payStatus); err != nil || payStatus != store.PayStatusPaid {
		t.Fatalf("部分退款后支付流水状态=%d 期望仍为已支付", payStatus)
	}

	// 2) 退完剩余 70 元
	rid2, err := store.InsertRefund(store.Refund{
		OrderNo: orderNo, OrderID: orderID, PaymentID: paymentID,
		RefundNo: "HR2", Channel: "wxpay", Amount: 7000, Status: store.RefundStatusProcessing,
	})
	if err != nil {
		t.Fatalf("登记第二笔退款失败: %v", err)
	}
	applyRefundSuccess(rid2, orderID, paymentID, orderNo, 7000, "WXR2")

	if got := store.SumRefunded(orderNo); got != 10000 {
		t.Fatalf("全额退款后累计已退=%d 期望 10000", got)
	}
	refundCents, paidCents = orderState()
	if refundCents != 10000 || paidCents != 0 {
		t.Fatalf("全额退款后 refund_amount=%d paid_amount=%d 期望 10000/0(营收归零)", refundCents, paidCents)
	}
	if err := store.DB.QueryRow(`SELECT status FROM tb_payment WHERE payment_id=?`, paymentID).Scan(&payStatus); err != nil || payStatus != store.PayStatusRefunded {
		t.Fatalf("全额退款后支付流水状态=%d 期望已退款(%d)", payStatus, store.PayStatusRefunded)
	}

	// 3) 退无可退:可退余额为 0
	var totalCents int64
	_ = store.DB.QueryRow(`SELECT total_amount FROM tb_order WHERE order_no=?`, orderNo).Scan(&totalCents)
	if remain := totalCents - store.SumRefunded(orderNo); remain != 0 {
		t.Fatalf("剩余可退=%d 期望 0(应拒绝再次退款)", remain)
	}
}
