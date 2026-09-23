package service

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// paymentSvcInit 初始化独立 SQLite 测试库。
func paymentSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "payment_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// paymentSvcInsertOrder 种一条订单。
func paymentSvcInsertOrder(t *testing.T, orderNo string, status, payStatus, totalCents int) int {
	t.Helper()
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, 1, "T1", "测试桌", 2, status, totalCents, 0, 0, totalCents, payStatus, now, now)
	if err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestPaymentSvcValidateOrderPayable(t *testing.T) {
	paymentSvcInit(t)

	t.Run("订单不存在", func(t *testing.T) {
		if _, _, err := ValidateOrderPayable("NO_SUCH"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("已支付拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY1", OrderStatusPlaced, 1, 5000)
		if _, _, err := ValidateOrderPayable("PAY1"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单应拒绝, got %v", err)
		}
	})

	t.Run("非进行中拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY2", OrderStatusCanceled, 0, 5000)
		if _, _, err := ValidateOrderPayable("PAY2"); err == nil || !strings.Contains(err.Error(), "不可支付") {
			t.Fatalf("终态订单应拒绝支付, got %v", err)
		}
	})

	t.Run("金额异常拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY3", OrderStatusPlaced, 0, 0)
		if _, _, err := ValidateOrderPayable("PAY3"); err == nil || !strings.Contains(err.Error(), "金额异常") {
			t.Fatalf("金额异常应拒绝支付, got %v", err)
		}
	})

	t.Run("成功", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "PAY4", OrderStatusPlaced, 0, 5000)
		orderID, totalCents, err := ValidateOrderPayable("PAY4")
		if err != nil || orderID != id || totalCents != 5000 {
			t.Fatalf("可支付订单应通过, got id=%d total=%d err=%v", orderID, totalCents, err)
		}
	})
}

func TestPaymentSvcPaymentFlow(t *testing.T) {
	paymentSvcInit(t)
	paymentSvcInsertOrder(t, "PF1", OrderStatusPlaced, 0, 5000)

	if err := CreatePayment("PF1", "wxpay", 5000, "prepay-1"); err != nil {
		t.Fatalf("创建支付流水失败: %v", err)
	}
	paymentID, status, amount, err := GetPayment("PF1", "wxpay")
	if err != nil || paymentID <= 0 || status != dao.PayStatusPending || amount != 5000 {
		t.Fatalf("支付流水查询异常: id=%d status=%d amount=%d err=%v", paymentID, status, amount, err)
	}

	// 重复发起同一渠道复用旧流水。
	if err := CreatePayment("PF1", "wxpay", 5001, "prepay-2"); err != nil {
		t.Fatalf("重复创建支付流水失败: %v", err)
	}
	_, _, amount, _ = GetPayment("PF1", "wxpay")
	if amount != 5001 {
		t.Fatalf("复用流水应更新金额, got %d", amount)
	}

	// 另一渠道待支付流水。
	if err := CreatePayment("PF1", "alipay", 5000, "prepay-ali"); err != nil {
		t.Fatalf("创建支付宝流水失败: %v", err)
	}
	pending := ListPendingPayments("PF1", "wxpay")
	if len(pending) != 1 || pending[0].Channel != "alipay" {
		t.Fatalf("待关闭流水过滤异常: %+v", pending)
	}

	if ch, err := LastPaymentChannel("PF1"); err != nil || ch != "alipay" {
		t.Fatalf("最近渠道应为 alipay, got %s err=%v", ch, err)
	}

	if no, err := GetOrderNoByID(1); err != nil || no == "" {
		t.Fatalf("按 ID 查订单号失败: no=%q err=%v", no, err)
	}

	if err := MarkPaymentClosed(paymentID); err != nil {
		t.Fatalf("关闭流水失败: %v", err)
	}
	if err := MarkPaymentRefunded(paymentID); err != nil {
		t.Fatalf("标记退款失败: %v", err)
	}
}

func TestPaymentSvcApplyPaymentSuccess(t *testing.T) {
	paymentSvcInit(t)

	t.Run("正常支付成功", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "APS1", OrderStatusPlaced, 0, 5000)
		if err := CreatePayment("APS1", "wxpay", 5000, "prepay"); err != nil {
			t.Fatalf("创建流水失败: %v", err)
		}
		res := ApplyPaymentSuccess("APS1", "wxpay", "wxTradeNo", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("正常支付不应标记为重复")
		}
		var payStatus int
		store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_id=?`, id).Scan(&payStatus)
		if payStatus != 1 {
			t.Fatalf("支付成功后订单应已支付, got %d", payStatus)
		}
		_, flowStatus, _, _ := dao.GetPayment("APS1", "wxpay")
		if flowStatus != dao.PayStatusPaid {
			t.Fatalf("支付成功后流水应已支付, got %d", flowStatus)
		}
	})

	t.Run("同渠道重复回调幂等", func(t *testing.T) {
		paymentSvcInsertOrder(t, "APS2", OrderStatusPlaced, 0, 5000)
		CreatePayment("APS2", "wxpay", 5000, "prepay")
		ApplyPaymentSuccess("APS2", "wxpay", "t1", "wxpay", 5000)
		res := ApplyPaymentSuccess("APS2", "wxpay", "t1", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("同渠道重复回调不应标记为重复")
		}
	})

	t.Run("订单已支付返回重复", func(t *testing.T) {
		paymentSvcInsertOrder(t, "APS3", OrderStatusPlaced, 1, 5000)
		CreatePayment("APS3", "wxpay", 5000, "prepay")
		res := ApplyPaymentSuccess("APS3", "wxpay", "t2", "wxpay", 5000)
		if !res.Duplicate {
			t.Fatalf("已支付订单再次支付应返回重复标记")
		}
	})

	t.Run("重复支付通知重发不再重复标记", func(t *testing.T) {
		// 钉住重复退款防线:输家流水先被置为已支付,同交易号通知重发时命中
		// 幂等短路,不会再次返回 Duplicate —— 否则会向渠道发起第二笔退款。
		paymentSvcInsertOrder(t, "APS5", OrderStatusPlaced, 1, 5000)
		CreatePayment("APS5", "wxpay", 5000, "prepay")
		res := ApplyPaymentSuccess("APS5", "wxpay", "t5", "wxpay", 5000)
		if !res.Duplicate {
			t.Fatalf("第一次应返回重复标记")
		}
		_, flowStatus, _, _ := dao.GetPayment("APS5", "wxpay")
		if flowStatus != dao.PayStatusPaid {
			t.Fatalf("重复支付后输家流水应置为已支付, got %d", flowStatus)
		}
		res = ApplyPaymentSuccess("APS5", "wxpay", "t5", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("通知重发不应再次标记为重复(否则会二次退款)")
		}
	})

	t.Run("流水缺失不崩溃", func(t *testing.T) {
		paymentSvcInsertOrder(t, "APS4", OrderStatusPlaced, 0, 5000)
		ApplyPaymentSuccess("APS4", "wxpay", "t3", "wxpay", 5000)
		var payStatus int
		store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_no='APS4'`).Scan(&payStatus)
		if payStatus != 0 {
			t.Fatalf("无流水时订单不应被标记支付, got %d", payStatus)
		}
	})
}

func TestPaymentSvcRefundAmount(t *testing.T) {
	paymentSvcInit(t)
	paymentSvcInsertOrder(t, "RF1", OrderStatusFinished, 1, 10000)

	t.Run("全额退款", func(t *testing.T) {
		got, err := RefundAmount("RF1", 10000, 0)
		if err != nil || got != 10000 {
			t.Fatalf("全额退款应返回 10000, got %d err=%v", got, err)
		}
	})

	t.Run("指定金额", func(t *testing.T) {
		got, err := RefundAmount("RF1", 10000, 35.5)
		if err != nil || got != 3550 {
			t.Fatalf("指定退款 35.5 元应为 3550 分, got %d err=%v", got, err)
		}
	})

	t.Run("金额过小拒绝", func(t *testing.T) {
		if _, err := RefundAmount("RF1", 10000, 0.004); err == nil || !strings.Contains(err.Error(), "大于 0") {
			t.Fatalf("0 分退款应被拒绝, got %v", err)
		}
	})

	t.Run("超出可退额度拒绝", func(t *testing.T) {
		if _, err := dao.InsertRefund(po.Refund{
			OrderID: 1, OrderNo: "RF1", RefundNo: "RFN1", Channel: "wxpay",
			Amount: 4000, Status: po.RefundStatusProcessing,
		}); err != nil {
			t.Fatalf("插入退款失败: %v", err)
		}
		if _, err := RefundAmount("RF1", 10000, 100); err == nil || !strings.Contains(err.Error(), "超出可退金额") {
			t.Fatalf("超出额度应被拒绝, got %v", err)
		}
	})
}

func TestPaymentSvcRefundCRUDAndApply(t *testing.T) {
	paymentSvcInit(t)
	id := paymentSvcInsertOrder(t, "RF2", OrderStatusFinished, 1, 10000)
	if _, err := store.DB.Exec(`UPDATE tb_order SET paid_amount=10000 WHERE order_id=?`, id); err != nil {
		t.Fatalf("设置实收金额失败: %v", err)
	}

	refundID, err := InsertRefund(po.Refund{
		OrderID: id, OrderNo: "RF2", PaymentID: 0, RefundNo: "RFN2", Channel: "wxpay",
		Amount: 1000, Status: po.RefundStatusProcessing, Reason: "顾客退款", Operator: "tester",
	})
	if err != nil || refundID <= 0 {
		t.Fatalf("登记退款失败: %v (id=%d)", err, refundID)
	}

	r, err := GetRefund(refundID)
	if err != nil || r.RefundNo != "RFN2" || r.Status != po.RefundStatusProcessing {
		t.Fatalf("读取退款单异常: %+v err=%v", r, err)
	}

	list, err := ListRefunds(id)
	if err != nil || len(list) != 1 {
		t.Fatalf("退款列表应返回 1 条, got %d err=%v", len(list), err)
	}

	// 退款成功回写入账。
	if err := ApplyRefundSuccess(refundID, 0, "RF2", 1000, "chRefundNo"); err != nil {
		t.Fatalf("退款入账失败: %v", err)
	}
	r, _ = GetRefund(refundID)
	if r.Status != po.RefundStatusSuccess {
		t.Fatalf("退款单应置成功, got %d", r.Status)
	}
	var refundAmount, paidAmount int64
	store.DB.QueryRow(`SELECT refund_amount, paid_amount FROM tb_order WHERE order_id=?`, id).Scan(&refundAmount, &paidAmount)
	if refundAmount != 1000 || paidAmount != 9000 {
		t.Fatalf("退款入账异常: refund=%d paid=%d", refundAmount, paidAmount)
	}

	// 幂等：重复回写不重复累加。
	if err := ApplyRefundSuccess(refundID, 0, "RF2", 1000, "chRefundNo"); err != nil {
		t.Fatalf("幂等回写入账失败: %v", err)
	}
	store.DB.QueryRow(`SELECT refund_amount FROM tb_order WHERE order_id=?`, id).Scan(&refundAmount)
	if refundAmount != 1000 {
		t.Fatalf("幂等回写不应重复累加, got %d", refundAmount)
	}

	// MarkRefund 状态回写。
	if err := MarkRefund(refundID, po.RefundStatusSuccess, "ch2", ""); err != nil {
		t.Fatalf("回写退款状态失败: %v", err)
	}
}

func TestPaymentSvcRecordMismatchedAndDuplicate(t *testing.T) {
	paymentSvcInit(t)
	id := paymentSvcInsertOrder(t, "RMD1", OrderStatusFinished, 1, 10000)

	// 金额不一致留痕。
	RecordMismatchedPayment(id, "RMD1", "wxpay", 12000, 10000, "trade-mismatch")
	list, err := ListRefunds(id)
	if err != nil || len(list) != 1 || list[0].Status != po.RefundStatusFail {
		t.Fatalf("金额不一致留痕异常: %+v err=%v", list, err)
	}

	// 重复支付自动退回登记。
	refundNo, refundID, _, totalCents, err := RecordDuplicateRefund(id, "RMD1", "alipay", 10000, "trade-dup")
	if err != nil || refundID <= 0 || totalCents != 10000 || !strings.HasPrefix(refundNo, "R") {
		t.Fatalf("重复退款登记异常: refundNo=%s id=%d total=%d err=%v", refundNo, refundID, totalCents, err)
	}

	// 到账回写。
	ApplyDuplicateRefundSuccess(refundID, 0, "chRefundDup", refundNo)
	r, _ := GetRefund(refundID)
	if r.Status != po.RefundStatusSuccess {
		t.Fatalf("重复退回到账后应置成功, got %d", r.Status)
	}
}
