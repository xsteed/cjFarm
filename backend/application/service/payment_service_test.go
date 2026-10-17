package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
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
		if _, _, err := ValidateOrderPayable(context.Background(), "NO_SUCH"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("已支付拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY1", OrderStatusPlaced, 1, 5000)
		if _, _, err := ValidateOrderPayable(context.Background(), "PAY1"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单应拒绝, got %v", err)
		}
	})

	t.Run("非进行中拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY2", OrderStatusCanceled, 0, 5000)
		if _, _, err := ValidateOrderPayable(context.Background(), "PAY2"); err == nil || !strings.Contains(err.Error(), "不可支付") {
			t.Fatalf("终态订单应拒绝支付, got %v", err)
		}
	})

	t.Run("金额异常拒绝", func(t *testing.T) {
		paymentSvcInsertOrder(t, "PAY3", OrderStatusPlaced, 0, 0)
		if _, _, err := ValidateOrderPayable(context.Background(), "PAY3"); err == nil || !strings.Contains(err.Error(), "金额异常") {
			t.Fatalf("金额异常应拒绝支付, got %v", err)
		}
	})

	t.Run("成功", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "PAY4", OrderStatusPlaced, 0, 5000)
		orderID, totalCents, err := ValidateOrderPayable(context.Background(), "PAY4")
		if err != nil || orderID != id || totalCents != 5000 {
			t.Fatalf("可支付订单应通过, got id=%d total=%d err=%v", orderID, totalCents, err)
		}
	})
}

func TestPaymentSvcPaymentFlow(t *testing.T) {
	paymentSvcInit(t)
	pf1 := paymentSvcInsertOrder(t, "PF1", OrderStatusPlaced, 0, 5000)

	if err := CreatePayment(pf1, "PF1", "wxpay", 5000, "prepay-1"); err != nil {
		t.Fatalf("创建支付流水失败: %v", err)
	}
	flow, err := GetPayment("PF1", "wxpay")
	if err != nil || flow.PaymentID <= 0 || flow.Status != dao.PayStatusPending || flow.Amount != 5000 {
		t.Fatalf("支付流水查询异常: id=%d status=%d amount=%d err=%v", flow.PaymentID, flow.Status, flow.Amount, err)
	}

	// 重复发起同一渠道复用旧流水(金额不变)。
	if err := CreatePayment(pf1, "PF1", "wxpay", 5000, "prepay-2"); err != nil {
		t.Fatalf("重复创建支付流水失败: %v", err)
	}
	if reused, _ := GetPayment("PF1", "wxpay"); reused.Amount != 5000 {
		t.Fatalf("复用流水金额不应变化, got %d", reused.Amount)
	}

	// 出码金额与订单当前应收不一致(加菜/改单抬价)必须拒绝:否则旧金额二维码
	// 会流出,顾客按旧金额付款后回调被金额防线拒绝,钱到了订单却入不了账。
	if err := CreatePayment(pf1, "PF1", "wxpay", 5001, "prepay-3"); !errors.Is(err, ErrOrderAmountChanged) {
		t.Fatalf("金额不一致应拒绝出码, got %v", err)
	}

	// 另一渠道待支付流水。
	if err := CreatePayment(pf1, "PF1", "alipay", 5000, "prepay-ali"); err != nil {
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

	if err := MarkPaymentClosed(flow.PaymentID); err != nil {
		t.Fatalf("关闭流水失败: %v", err)
	}
	if err := MarkPaymentRefunded(flow.PaymentID); err != nil {
		t.Fatalf("标记退款失败: %v", err)
	}
}

func TestPaymentSvcApplyPaymentSuccess(t *testing.T) {
	paymentSvcInit(t)

	t.Run("正常支付成功", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "APS1", OrderStatusPlaced, 0, 5000)
		if err := CreatePayment(id, "APS1", "wxpay", 5000, "prepay"); err != nil {
			t.Fatalf("创建流水失败: %v", err)
		}
		res := ApplyPaymentSuccess(context.Background(), "APS1", "wxpay", "wxTradeNo", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("正常支付不应标记为重复")
		}
		if !res.FirstTime {
			t.Fatalf("首次入账应回报 FirstTime(先付模式据此出厨单)")
		}
		var payStatus int
		store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_id=?`, id).Scan(&payStatus)
		if payStatus != 1 {
			t.Fatalf("支付成功后订单应已支付, got %d", payStatus)
		}
		if f, _ := dao.GetPayment("APS1", "wxpay"); f.Status != dao.PayStatusPaid {
			t.Fatalf("支付成功后流水应已支付, got %d", f.Status)
		}
	})

	t.Run("同渠道重复回调幂等", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "APS2", OrderStatusPlaced, 0, 5000)
		CreatePayment(id, "APS2", "wxpay", 5000, "prepay")
		ApplyPaymentSuccess(context.Background(), "APS2", "wxpay", "t1", "wxpay", 5000)
		res := ApplyPaymentSuccess(context.Background(), "APS2", "wxpay", "t1", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("同渠道重复回调不应标记为重复")
		}
		if res.FirstTime {
			t.Fatalf("重复回调不应再报首次入账(否则会重复出厨单)")
		}
	})

	t.Run("订单已支付返回重复", func(t *testing.T) {
		id := paymentSvcInsertOrder(t, "APS3", OrderStatusPlaced, 1, 5000)
		CreatePayment(id, "APS3", "wxpay", 5000, "prepay")
		res := ApplyPaymentSuccess(context.Background(), "APS3", "wxpay", "t2", "wxpay", 5000)
		if !res.Duplicate {
			t.Fatalf("已支付订单再次支付应返回重复标记")
		}
	})

	t.Run("重复支付通知重发不再重复标记", func(t *testing.T) {
		// 钉住重复退款防线:输家流水先被置为已支付,同交易号通知重发时命中
		// 幂等短路,不会再次返回 Duplicate —— 否则会向渠道发起第二笔退款。
		id := paymentSvcInsertOrder(t, "APS5", OrderStatusPlaced, 1, 5000)
		CreatePayment(id, "APS5", "wxpay", 5000, "prepay")
		res := ApplyPaymentSuccess(context.Background(), "APS5", "wxpay", "t5", "wxpay", 5000)
		if !res.Duplicate {
			t.Fatalf("第一次应返回重复标记")
		}
		if f, _ := dao.GetPayment("APS5", "wxpay"); f.Status != dao.PayStatusPaid {
			t.Fatalf("重复支付后输家流水应置为已支付, got %d", f.Status)
		}
		res = ApplyPaymentSuccess(context.Background(), "APS5", "wxpay", "t5", "wxpay", 5000)
		if res.Duplicate {
			t.Fatalf("通知重发不应再次标记为重复(否则会二次退款)")
		}
	})

	t.Run("流水缺失不崩溃", func(t *testing.T) {
		paymentSvcInsertOrder(t, "APS4", OrderStatusPlaced, 0, 5000)
		ApplyPaymentSuccess(context.Background(), "APS4", "wxpay", "t3", "wxpay", 5000)
		var payStatus int
		store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_no='APS4'`).Scan(&payStatus)
		if payStatus != 0 {
			t.Fatalf("无流水时订单不应被标记支付, got %d", payStatus)
		}
	})

	t.Run("掉单补偿:流水已支付但订单未入账", func(t *testing.T) {
		// 回归用例:模拟上一轮 MarkPaymentPaid 成功、MarkOrderPaidByChannel 失败后留下的
		// 中间态(流水已支付、订单仍 pay_status=0)。渠道重试时必须补回订单入账,
		// 不能被「流水已支付」短路成成功应答 —— 否则渠道停止重试,订单永远不入账。
		id := paymentSvcInsertOrder(t, "APS6", OrderStatusPlaced, 0, 5000)
		CreatePayment(id, "APS6", "wxpay", 5000, "prepay")
		if _, err := dao.MarkPaymentPaid("APS6", "wxpay", "t6"); err != nil {
			t.Fatalf("预置流水已支付失败: %v", err)
		}
		res := ApplyPaymentSuccess(context.Background(), "APS6", "wxpay", "t6", "wxpay", 5000)
		if res.Failed {
			t.Fatalf("掉单补偿不应应答失败")
		}
		if !res.FirstTime {
			t.Fatalf("掉单补偿也是首次入账,应回报 FirstTime(先付模式据此补出厨单)")
		}
		var payStatus int
		store.DB.QueryRow(`SELECT pay_status FROM tb_order WHERE order_id=?`, id).Scan(&payStatus)
		if payStatus != 1 {
			t.Fatalf("掉单补偿后订单应入账, got pay_status=%d", payStatus)
		}
	})
}

func TestPaymentSvcRefundAmount(t *testing.T) {
	paymentSvcInit(t)
	paymentSvcInsertOrder(t, "RF1", OrderStatusFinished, 1, 10000)

	t.Run("全额退款", func(t *testing.T) {
		got, err := RefundAmount(context.Background(), "RF1", 10000, 0)
		if err != nil || got != 10000 {
			t.Fatalf("全额退款应返回 10000, got %d err=%v", got, err)
		}
	})

	t.Run("指定金额", func(t *testing.T) {
		got, err := RefundAmount(context.Background(), "RF1", 10000, 35.5)
		if err != nil || got != 3550 {
			t.Fatalf("指定退款 35.5 元应为 3550 分, got %d err=%v", got, err)
		}
	})

	t.Run("金额过小拒绝", func(t *testing.T) {
		if _, err := RefundAmount(context.Background(), "RF1", 10000, 0.004); err == nil || !strings.Contains(err.Error(), "大于 0") {
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
		if _, err := RefundAmount(context.Background(), "RF1", 10000, 100); err == nil || !strings.Contains(err.Error(), "超出可退金额") {
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
	if err := ApplyRefundSuccess(context.Background(), refundID, 0, "RF2", 1000, "chRefundNo"); err != nil {
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
	if err := ApplyRefundSuccess(context.Background(), refundID, 0, "RF2", 1000, "chRefundNo"); err != nil {
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
	RecordMismatchedPayment(context.Background(), id, "RMD1", "wxpay", 12000, 10000, "trade-mismatch")
	list, err := ListRefunds(id)
	if err != nil || len(list) != 1 || list[0].Status != po.RefundStatusFail {
		t.Fatalf("金额不一致留痕异常: %+v err=%v", list, err)
	}

	// 重复支付自动退回登记。
	refundNo, refundID, _, totalCents, err := RecordDuplicateRefund(context.Background(), id, "RMD1", "alipay", 10000, "trade-dup")
	if err != nil || refundID <= 0 || totalCents != 10000 || !strings.HasPrefix(refundNo, "R") {
		t.Fatalf("重复退款登记异常: refundNo=%s id=%d total=%d err=%v", refundNo, refundID, totalCents, err)
	}

	// 并发通知(渠道重发 + 前端轮询补单)会各自进入重复支付分支:第二次登记必须被
	// 事务内查重(锁订单行后重查)拦截,否则同一笔支付会被渠道退两次。
	_, dupID, _, _, err2 := RecordDuplicateRefund(context.Background(), id, "RMD1", "alipay", 10000, "trade-dup")
	if !errors.Is(err2, ErrDuplicateRefundPending) {
		t.Fatalf("重复登记应被查重拦截, got id=%d err=%v", dupID, err2)
	}
	// 换一个渠道不算重复:微信/支付宝各付一笔时两笔都要退。
	if _, _, _, _, err3 := RecordDuplicateRefund(context.Background(), id, "RMD1", "wxpay", 10000, "trade-dup2"); err3 != nil {
		t.Fatalf("其它渠道的重复支付应允许登记, got %v", err3)
	}

	// 到账回写。
	ApplyDuplicateRefundSuccess(context.Background(), refundID, 0, "chRefundDup", refundNo)
	r, _ := GetRefund(refundID)
	if r.Status != po.RefundStatusSuccess {
		t.Fatalf("重复退回到账后应置成功, got %d", r.Status)
	}
}
