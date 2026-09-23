package dao

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

func svcPayInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "payment-more.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func svcPaySeedOrder(t *testing.T, orderNo string, payStatus, orderStatus int, totalCents, paidCents int64) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_order(order_no, pay_status, order_status, total_amount, paid_amount, create_time, update_time)
		VALUES(?,?,?,?,?,?,?)`, orderNo, payStatus, orderStatus, totalCents, paidCents, store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func svcPaySeedPayment(t *testing.T, orderNo, channel, tradeNo string, status int, amount int64) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_payment(order_no, channel, channel_trade_no, amount, status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?)`, orderNo, channel, tradeNo, amount, status, store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入支付流水失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestSvcPayMarkPaymentStatus(t *testing.T) {
	svcPayInit(t)
	id := svcPaySeedPayment(t, "P1", "wxpay", "T1", PayStatusPending, 1000)

	if err := MarkPaymentStatus(id, PayStatusPaid); err != nil {
		t.Fatalf("MarkPaymentStatus 失败: %v", err)
	}
	var status int
	var updateTime string
	if err := store.DB.QueryRow(`SELECT status, update_time FROM tb_payment WHERE payment_id=?`, id).Scan(&status, &updateTime); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if status != PayStatusPaid {
		t.Fatalf("状态=%d, 期望已支付", status)
	}
	if updateTime == "" {
		t.Fatal("更新时间不应为空")
	}
	// 不存在的流水:UPDATE 影响 0 行但不应报错。
	if err := MarkPaymentStatus(999999, PayStatusPaid); err != nil {
		t.Fatalf("不存在流水不应报错, got %v", err)
	}
}

func TestSvcPayGetOrderAmount(t *testing.T) {
	svcPayInit(t)
	svcPaySeedOrder(t, "P2", 1, 3, 8800, 8800)

	orderID, payStatus, orderStatus, totalCents, err := GetOrderAmount("P2")
	if err != nil {
		t.Fatalf("GetOrderAmount 失败: %v", err)
	}
	if orderID == 0 || payStatus != 1 || orderStatus != 3 || totalCents != 8800 {
		t.Fatalf("结果异常: orderID=%d payStatus=%d orderStatus=%d total=%d", orderID, payStatus, orderStatus, totalCents)
	}
	if _, _, _, _, err := GetOrderAmount("不存在的订单"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在订单应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcPayLastPaymentChannel(t *testing.T) {
	svcPayInit(t)
	svcPaySeedPayment(t, "P3", "wxpay", "T-wx", PayStatusPaid, 1000)
	svcPaySeedPayment(t, "P3", "alipay", "T-ali", PayStatusPending, 2000)

	if ch, err := LastPaymentChannel("P3"); err != nil || ch != "alipay" {
		t.Fatalf("应返回最近一条流水渠道 alipay, got %q err=%v", ch, err)
	}
	if _, err := LastPaymentChannel("无流水订单"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("无流水订单应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcPayGetRefundOrderInfo(t *testing.T) {
	svcPayInit(t)
	id := svcPaySeedOrder(t, "P4", 1, 4, 6600, 6600)

	orderNo, payStatus, totalCents, err := GetRefundOrderInfo(id)
	if err != nil {
		t.Fatalf("GetRefundOrderInfo 失败: %v", err)
	}
	if orderNo != "P4" || payStatus != 1 || totalCents != 6600 {
		t.Fatalf("结果异常: orderNo=%q payStatus=%d total=%d", orderNo, payStatus, totalCents)
	}
	if _, _, _, err := GetRefundOrderInfo(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在订单应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcPayLastPaidPayment(t *testing.T) {
	svcPayInit(t)
	svcPaySeedPayment(t, "P5", "wxpay", "T-pending", PayStatusPending, 1000)
	svcPaySeedPayment(t, "P5", "wxpay", "T-paid", PayStatusPaid, 2000)
	refundedID := svcPaySeedPayment(t, "P5", "alipay", "T-refunded", PayStatusRefunded, 3000)

	id, channel, err := LastPaidPayment("P5")
	if err != nil {
		t.Fatalf("LastPaidPayment 失败: %v", err)
	}
	if id != refundedID || channel != "alipay" {
		t.Fatalf("应返回最近已支付/已退款流水, got id=%d channel=%q(期望 id=%d alipay)", id, channel, refundedID)
	}
}

func TestSvcPayLastPaidPaymentOnlyPending(t *testing.T) {
	svcPayInit(t)
	svcPaySeedPayment(t, "P6", "wxpay", "T-only-pending", PayStatusPending, 1000)
	if _, _, err := LastPaidPayment("P6"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("仅待支付流水应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcPayDeductPaidAmount(t *testing.T) {
	svcPayInit(t)
	svcPaySeedOrder(t, "P7", 1, 4, 10000, 10000)

	if err := DeductPaidAmount("P7", 3000); err != nil {
		t.Fatalf("DeductPaidAmount 失败: %v", err)
	}
	var paid int64
	if err := store.DB.QueryRow(`SELECT paid_amount FROM tb_order WHERE order_no='P7'`).Scan(&paid); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if paid != 7000 {
		t.Fatalf("扣减后实收=%d, 期望 7000", paid)
	}

	// 超额扣减应封底为 0,而不是变成负数。
	if err := DeductPaidAmount("P7", 9000); err != nil {
		t.Fatalf("超额扣减失败: %v", err)
	}
	if err := store.DB.QueryRow(`SELECT paid_amount FROM tb_order WHERE order_no='P7'`).Scan(&paid); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if paid != 0 {
		t.Fatalf("超额扣减应封底为 0, got %d", paid)
	}

	// 不存在的订单:0 行受影响但不报错。
	if err := DeductPaidAmount("不存在", 100); err != nil {
		t.Fatalf("不存在订单不应报错, got %v", err)
	}
}

func TestSvcPayGetOrderNoByID(t *testing.T) {
	svcPayInit(t)
	id := svcPaySeedOrder(t, "P8", 0, 1, 100, 0)

	if got, err := GetOrderNoByID(id); err != nil || got != "P8" {
		t.Fatalf("GetOrderNoByID=%q err=%v, 期望 P8", got, err)
	}
	if _, err := GetOrderNoByID(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在订单应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcPayListPendingPayments(t *testing.T) {
	svcPayInit(t)
	svcPaySeedPayment(t, "P9", "wxpay", "P_wx", PayStatusPending, 1000)
	svcPaySeedPayment(t, "P9", "alipay", "P_ali", PayStatusPending, 2000)
	svcPaySeedPayment(t, "P9", "wxpay", "T_wx_paid", PayStatusPaid, 3000)
	svcPaySeedPayment(t, "P9", "alipay", "T_ali_closed", PayStatusClosed, 4000)

	// 排除 wxpay:只剩 alipay 待支付流水。
	got := ListPendingPayments("P9", "wxpay")
	if len(got) != 1 || got[0].Channel != "alipay" {
		t.Fatalf("排除 wxpay 后应剩 1 条 alipay 待支付流水, got %+v", got)
	}
	// 不排除:两条待支付流水都在。
	all := ListPendingPayments("P9", "")
	if len(all) != 2 {
		t.Fatalf("应返回 2 条待支付流水, got %d", len(all))
	}
	// 无待支付流水:空切片。
	empty := ListPendingPayments("无流水订单", "")
	if len(empty) != 0 {
		t.Fatalf("无待支付流水应返回空切片, got %+v", empty)
	}
}
