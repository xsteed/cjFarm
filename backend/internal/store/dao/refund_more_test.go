package dao

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

func svcRefundInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "refund-more.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func svcRefundInsert(t *testing.T, r po.Refund) int {
	t.Helper()
	id, err := InsertRefund(r)
	if err != nil {
		t.Fatalf("InsertRefund 失败: %v", err)
	}
	return id
}

func TestSvcRefundGetRefund(t *testing.T) {
	svcRefundInit(t)

	want := po.Refund{
		OrderID:         1,
		OrderNo:         "R1",
		PaymentID:       2,
		RefundNo:        "REF001",
		Channel:         "wxpay",
		ChannelRefundNo: "WXREF001",
		Amount:          3000,
		Status:          po.RefundStatusSuccess,
		Reason:          "顾客退菜",
		Operator:        "张三",
		FailReason:      "",
		IsDuplicate:     1,
	}
	id := svcRefundInsert(t, want)

	got, err := GetRefund(id)
	if err != nil {
		t.Fatalf("GetRefund 失败: %v", err)
	}
	if got.RefundID != id || got.OrderNo != want.OrderNo || got.RefundNo != want.RefundNo ||
		got.Amount != want.Amount || got.Status != want.Status || got.IsDuplicate != want.IsDuplicate {
		t.Fatalf("GetRefund 结果不匹配: got %+v, want %+v", got, want)
	}
}

func TestSvcRefundGetRefundNotFound(t *testing.T) {
	svcRefundInit(t)
	if _, err := GetRefund(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在的退款单应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcRefundMarkRefundSuccessOnce(t *testing.T) {
	svcRefundInit(t)

	id := svcRefundInsert(t, po.Refund{
		OrderID: 1, OrderNo: "R2", PaymentID: 1, RefundNo: "REF002",
		Channel: "wxpay", Amount: 1000, Status: po.RefundStatusProcessing,
		FailReason: "旧失败原因",
	})

	// 第一次:处理中 -> 成功,返回 true,并清空 fail_reason。
	changed, err := MarkRefundSuccessOnce(id, "WXREF1")
	if err != nil || !changed {
		t.Fatalf("首次置成功应返回 true, changed=%v err=%v", changed, err)
	}
	var status int
	var channelRefundNo, failReason string
	if err := store.DB.QueryRow(`SELECT status, channel_refund_no, fail_reason FROM tb_refund WHERE refund_id=?`, id).
		Scan(&status, &channelRefundNo, &failReason); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if status != po.RefundStatusSuccess || channelRefundNo != "WXREF1" || failReason != "" {
		t.Fatalf("置成功后状态异常: status=%d channel=%q failReason=%q", status, channelRefundNo, failReason)
	}

	// 第二次:已是成功态,幂等返回 false,且不覆盖已有渠道退款号。
	changed, err = MarkRefundSuccessOnce(id, "WXREF2")
	if err != nil || changed {
		t.Fatalf("重复置成功应返回 false, changed=%v err=%v", changed, err)
	}
	if err := store.DB.QueryRow(`SELECT channel_refund_no FROM tb_refund WHERE refund_id=?`, id).Scan(&channelRefundNo); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if channelRefundNo != "WXREF1" {
		t.Fatalf("重复调用不应覆盖渠道退款号, got %q", channelRefundNo)
	}
}

func TestSvcRefundMarkRefundSuccessOnceAlreadySuccess(t *testing.T) {
	svcRefundInit(t)
	id := svcRefundInsert(t, po.Refund{
		OrderID: 1, OrderNo: "R3", PaymentID: 1, RefundNo: "REF003",
		Channel: "wxpay", Amount: 1000, Status: po.RefundStatusSuccess,
	})
	if changed, err := MarkRefundSuccessOnce(id, "X"); err != nil || changed {
		t.Fatalf("已是成功态应返回 false, changed=%v err=%v", changed, err)
	}
}

func TestSvcRefundMarkRefundSuccessOnceFromFail(t *testing.T) {
	svcRefundInit(t)
	id := svcRefundInsert(t, po.Refund{
		OrderID: 1, OrderNo: "R4", PaymentID: 1, RefundNo: "REF004",
		Channel: "wxpay", Amount: 1000, Status: po.RefundStatusFail,
	})
	changed, err := MarkRefundSuccessOnce(id, "WXREF4")
	if err != nil || !changed {
		t.Fatalf("失败态也应可置成功, changed=%v err=%v", changed, err)
	}
}

func TestSvcRefundMarkRefundSuccessOnceNotFound(t *testing.T) {
	svcRefundInit(t)
	if changed, err := MarkRefundSuccessOnce(999999, "X"); err != nil || changed {
		t.Fatalf("不存在的退款单应返回 (false, nil), got changed=%v err=%v", changed, err)
	}
}
