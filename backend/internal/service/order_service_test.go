package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// orderSvcInit 初始化独立 SQLite 测试库。
func orderSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "order_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// orderSvcInsertOrder 种一条订单，dish_amount/total_amount 均设为 totalCents。
func orderSvcInsertOrder(t *testing.T, orderNo string, status, payStatus, totalCents int) int {
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

// orderSvcUpdateOrder 通过片段更新订单（仅测试用）。
func orderSvcUpdateOrder(t *testing.T, id int, sets ...string) {
	t.Helper()
	for _, s := range sets {
		if _, err := store.DB.Exec(fmt.Sprintf(`UPDATE tb_order SET %s WHERE order_id=%d`, s, id)); err != nil {
			t.Fatalf("更新订单失败(%s): %v", s, err)
		}
	}
}

// orderSvcState 读取订单状态与支付状态。
func orderSvcState(t *testing.T, id int) (status, payStatus int) {
	t.Helper()
	if err := store.DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_id=?`, id).
		Scan(&status, &payStatus); err != nil {
		t.Fatalf("查询订单状态失败: %v", err)
	}
	return
}

// orderSvcSpecID 取一对真实存在的菜品/规格。
func orderSvcSpecID(t *testing.T) (dishID, specID int) {
	t.Helper()
	if err := store.DB.QueryRow(`SELECT dish_id, spec_id FROM tb_spec ORDER BY spec_id LIMIT 1`).
		Scan(&dishID, &specID); err != nil {
		t.Fatalf("读取种子规格失败: %v", err)
	}
	return
}

func TestOrderSvcChangeOrderStatus(t *testing.T) {
	orderSvcInit(t)

	t.Run("非法状态值", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS1", OrderStatusPlaced, 0, 5000)
		if err := ChangeOrderStatus(id, 9, "tester"); err == nil || !strings.Contains(err.Error(), "非法的订单状态") {
			t.Fatalf("非法状态应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if err := ChangeOrderStatus(999999, OrderStatusCooking, "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("非法状态跳转", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS2", OrderStatusPlaced, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusDining, "tester"); err == nil || !strings.Contains(err.Error(), "非法的状态变更") {
			t.Fatalf("1→3 跳级应被拒绝, got %v", err)
		}
	})

	t.Run("未支付不可完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS3", OrderStatusDining, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusFinished, "tester"); err == nil || !strings.Contains(err.Error(), "未支付") {
			t.Fatalf("未支付订单不可完成, got %v", err)
		}
	})

	t.Run("已支付不可取消", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS4", OrderStatusDining, 1, 5000)
		if err := ChangeOrderStatus(id, OrderStatusCanceled, "tester"); err == nil || !strings.Contains(err.Error(), "不能取消") {
			t.Fatalf("已支付订单不可取消, got %v", err)
		}
	})

	t.Run("正常流转1→2", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS5", OrderStatusPlaced, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusCooking, "tester"); err != nil {
			t.Fatalf("1→2 应成功, got %v", err)
		}
		if s, _ := orderSvcState(t, id); s != OrderStatusCooking {
			t.Fatalf("流转后期望状态 2, got %d", s)
		}
	})
}

func TestOrderSvcPayOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("订单不存在", func(t *testing.T) {
		if _, err := PayOrder(999999, "", "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("终态不可收款", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OP1", OrderStatusFinished, 0, 5000)
		if _, err := PayOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "不可收款") {
			t.Fatalf("终态订单不可收款, got %v", err)
		}
	})

	t.Run("成功收款", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OP2", OrderStatusPlaced, 0, 5000)
		payType, err := PayOrder(id, "  ", "tester")
		if err != nil {
			t.Fatalf("收款应成功, got %v", err)
		}
		if payType != "现金" {
			t.Fatalf("空白支付方式应归一化为现金, got %q", payType)
		}
		if _, p := orderSvcState(t, id); p != 1 {
			t.Fatalf("收款后应已支付, got payStatus=%d", p)
		}
		var paid int64
		store.DB.QueryRow(`SELECT paid_amount FROM tb_order WHERE order_id=?`, id).Scan(&paid)
		if paid != 5000 {
			t.Fatalf("实收金额应为 5000, got %d", paid)
		}
	})

	t.Run("重复收款被拒", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OP3", OrderStatusPlaced, 0, 5000)
		if _, err := PayOrder(id, "", "tester"); err != nil {
			t.Fatalf("首次收款应成功: %v", err)
		}
		if _, err := PayOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("重复收款应被拒绝, got %v", err)
		}
	})
}

func TestOrderSvcSettleOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("非法结算方式", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL1", OrderStatusPlaced, 0, 5000)
		if _, err := SettleOrder(id, "xxx", "", "", "tester"); err == nil || !strings.Contains(err.Error(), "非法的结算方式") {
			t.Fatalf("非法结算方式应被拒绝, got %v", err)
		}
	})

	t.Run("免单缺少原因", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL2", OrderStatusPlaced, 0, 5000)
		if _, err := SettleOrder(id, SettleTypeFree, "", "", "tester"); err == nil || !strings.Contains(err.Error(), "免单必须填写原因") {
			t.Fatalf("免单缺原因应被拒绝, got %v", err)
		}
	})

	t.Run("挂账缺少事由", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL3", OrderStatusPlaced, 0, 5000)
		if _, err := SettleOrder(id, SettleTypeCredit, "", "", "tester"); err == nil || !strings.Contains(err.Error(), "挂账必须填写") {
			t.Fatalf("挂账缺事由应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if _, err := SettleOrder(999999, SettleTypeNormal, "", "", "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("已支付订单需归档", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL4", OrderStatusDining, 1, 5000)
		if _, err := SettleOrder(id, SettleTypeNormal, "", "", "tester"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单应拒绝结账, got %v", err)
		}
	})

	t.Run("正常收款", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL5", OrderStatusDining, 0, 5000)
		out, err := SettleOrder(id, SettleTypeNormal, "微信", "备注", "tester")
		if err != nil {
			t.Fatalf("正常结账应成功, got %v", err)
		}
		if out.SettleType != SettleTypeNormal || out.PaidCents != 5000 {
			t.Fatalf("正常结账结果异常: %+v", out)
		}
		s, p := orderSvcState(t, id)
		if s != OrderStatusFinished || p != 1 {
			t.Fatalf("正常结账后期望(4,1), got (%d,%d)", s, p)
		}
	})

	t.Run("免单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL6", OrderStatusDining, 0, 5000)
		out, err := SettleOrder(id, SettleTypeFree, "", "老板请客", "tester")
		if err != nil {
			t.Fatalf("免单应成功, got %v", err)
		}
		if out.SettleType != SettleTypeFree || out.PaidCents != 0 {
			t.Fatalf("免单结果异常: %+v", out)
		}
		var settle string
		var paid, credit int64
		store.DB.QueryRow(`SELECT settle_type, paid_amount, credit_status FROM tb_order WHERE order_id=?`, id).Scan(&settle, &paid, &credit)
		if settle != SettleTypeFree || paid != 0 {
			t.Fatalf("免单落库异常: settle=%s paid=%d", settle, paid)
		}
	})

	t.Run("挂账", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL7", OrderStatusDining, 0, 5000)
		out, err := SettleOrder(id, SettleTypeCredit, "", "某公司", "tester")
		if err != nil {
			t.Fatalf("挂账应成功, got %v", err)
		}
		if out.SettleType != SettleTypeCredit || out.CreditCents != 5000 || out.PaidCents != 0 {
			t.Fatalf("挂账结果异常: %+v", out)
		}
		var creditStatus int
		var creditAmount int64
		store.DB.QueryRow(`SELECT credit_status, credit_amount FROM tb_order WHERE order_id=?`, id).Scan(&creditStatus, &creditAmount)
		if creditStatus != CreditStatusPending || creditAmount != 5000 {
			t.Fatalf("挂账落库异常: status=%d amount=%d", creditStatus, creditAmount)
		}
	})
}

func TestOrderSvcSettleCreditOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("非挂账拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSC1", OrderStatusFinished, 1, 5000)
		if _, _, err := SettleCreditOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "无需核销") {
			t.Fatalf("非挂账订单应拒绝核销, got %v", err)
		}
	})

	t.Run("成功核销", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSC2", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='credit'", "credit_status=1", "credit_amount=5000", "paid_amount=0")
		payType, due, err := SettleCreditOrder(id, "现金", "tester")
		if err != nil {
			t.Fatalf("核销应成功, got %v", err)
		}
		if payType != "现金" || due != 5000 {
			t.Fatalf("核销返回异常: payType=%s due=%d", payType, due)
		}
		var status int
		var paid int64
		store.DB.QueryRow(`SELECT credit_status, paid_amount FROM tb_order WHERE order_id=?`, id).Scan(&status, &paid)
		if status != CreditStatusSettled || paid != 5000 {
			t.Fatalf("核销落库异常: status=%d paid=%d", status, paid)
		}
	})

	t.Run("已核销拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSC3", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='credit'", "credit_status=2", "credit_amount=5000", "paid_amount=5000")
		if _, _, err := SettleCreditOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "无需核销") {
			t.Fatalf("已核销订单应拒绝重复核销, got %v", err)
		}
	})
}

func TestOrderSvcCancelSettleOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("未结算拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OCL1", OrderStatusDining, 0, 5000)
		if _, err := CancelSettleOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "未结算") {
			t.Fatalf("未结算订单应拒绝撤销, got %v", err)
		}
	})

	t.Run("在线支付拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OCL2", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='normal'", "paid_amount=5000", "pay_channel='wxpay'")
		if _, err := CancelSettleOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "退款") {
			t.Fatalf("在线支付订单应要求走退款, got %v", err)
		}
	})

	t.Run("已退款拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OCL3", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='normal'", "paid_amount=5000", "refund_amount=1000")
		if _, err := CancelSettleOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "已发生退款") {
			t.Fatalf("已退款订单应拒绝撤销, got %v", err)
		}
	})

	t.Run("挂账已核销拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OCL4", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='credit'", "credit_status=2", "credit_amount=5000", "paid_amount=5000")
		if _, err := CancelSettleOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "挂账已核销") {
			t.Fatalf("已核销挂账应拒绝撤销, got %v", err)
		}
	})

	t.Run("免单撤销恢复结账前状态", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OCL5", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='free'", "paid_amount=0", "pre_settle_status=3")
		out, err := CancelSettleOrder(id, "录错了", "tester")
		if err != nil {
			t.Fatalf("免单撤销应成功, got %v", err)
		}
		if out.SettleType != SettleTypeFree {
			t.Fatalf("撤销结果异常: %+v", out)
		}
		s, p := orderSvcState(t, id)
		if s != OrderStatusDining || p != 0 {
			t.Fatalf("免单撤销后期望(3,0), got (%d,%d)", s, p)
		}
	})
}

func TestOrderSvcFinishAndCancelOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("未支付不可完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF1", OrderStatusDining, 0, 5000)
		if err := FinishOrder(id, "tester"); err == nil || !strings.Contains(err.Error(), "未支付") {
			t.Fatalf("未支付订单不可完成, got %v", err)
		}
	})

	t.Run("完成订单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF2", OrderStatusDining, 1, 5000)
		if err := FinishOrder(id, "tester"); err != nil {
			t.Fatalf("完成订单应成功, got %v", err)
		}
		if s, _ := orderSvcState(t, id); s != OrderStatusFinished {
			t.Fatalf("完成后期望状态 4, got %d", s)
		}
	})

	t.Run("终态不可取消", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF3", OrderStatusFinished, 1, 5000)
		if _, err := CancelOrder(id, "", "tester"); err == nil || !strings.Contains(err.Error(), "不可取消") {
			t.Fatalf("终态订单不可取消, got %v", err)
		}
	})

	t.Run("取消未支付订单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF4", OrderStatusPlaced, 0, 5000)
		reason, err := CancelOrder(id, "顾客不要了", "tester")
		if err != nil || reason != "顾客不要了" {
			t.Fatalf("取消订单应成功, reason=%s err=%v", reason, err)
		}
		if s, _ := orderSvcState(t, id); s != OrderStatusCanceled {
			t.Fatalf("取消后期望状态 5, got %d", s)
		}
	})
}

func TestOrderSvcEditOrder(t *testing.T) {
	orderSvcInit(t)
	dishID, specID := orderSvcSpecID(t)

	t.Run("订单不存在", func(t *testing.T) {
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(999999, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("非进行中不可改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE1", OrderStatusFinished, 1, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "不可改单") {
			t.Fatalf("非进行中订单不可改单, got %v", err)
		}
	})

	t.Run("已支付不可改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE2", OrderStatusPlaced, 1, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单不可改单, got %v", err)
		}
	})

	t.Run("明细为空拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE3", OrderStatusPlaced, 0, 5000)
		if _, err := EditOrder(id, 2, "", nil, "tester"); err == nil || !strings.Contains(err.Error(), "明细不能为空") {
			t.Fatalf("空明细应被拒绝, got %v", err)
		}
	})

	t.Run("无效规格拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE4", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: 999999, Quantity: 1}}
		if _, err := EditOrder(id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("无效规格应被拒绝, got %v", err)
		}
	})

	t.Run("成功改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE5", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 3}}
		out, err := EditOrder(id, 0, "少辣", items, "tester")
		if err != nil {
			t.Fatalf("改单应成功, got %v", err)
		}
		if out.ItemCount != 1 || out.PersonCount != 1 {
			t.Fatalf("改单结果异常: %+v", out)
		}
		var total int64
		store.DB.QueryRow(`SELECT total_amount FROM tb_order WHERE order_id=?`, id).Scan(&total)
		if total <= 0 {
			t.Fatalf("改单后金额应大于 0, got %d", total)
		}
	})
}

func TestOrderSvcListGetBoardUrge(t *testing.T) {
	orderSvcInit(t)

	// 列表与筛选。
	id := orderSvcInsertOrder(t, "OL1", OrderStatusPlaced, 0, 5000)
	total, list, pending, err := ListOrders(OrderQuery{OrderStatus: "1"}, 1, 10)
	if err != nil || total < 1 || len(list) < 1 || pending == nil {
		t.Fatalf("订单列表查询异常: total=%d err=%v", total, err)
	}

	// 详情。
	_, items, hasUrge, err := GetOrder(id)
	if err != nil || items == nil || hasUrge {
		t.Fatalf("订单详情查询异常: err=%v hasUrge=%v", err, hasUrge)
	}

	// 看板。
	board, err := OrderBoard()
	if err != nil || len(board) < 1 {
		t.Fatalf("看板查询异常: len=%d err=%v", len(board), err)
	}

	// 催菜记录与处理。
	if err := dao.InsertUrge(id, "OL1", 1, "T1", "测试桌", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("插入催菜失败: %v", err)
	}
	if !dao.HasPendingUrge(id) {
		t.Fatalf("应存在待处理催菜")
	}
	totalU, listU, err := ListUrges(UrgeQuery{}, 1, 10)
	if err != nil || totalU != 1 || len(listU) != 1 {
		t.Fatalf("催菜列表查询异常: total=%d err=%v", totalU, err)
	}
	if err := HandleUrges(0, id, "tester"); err != nil {
		t.Fatalf("批量处理催菜失败: %v", err)
	}
	if dao.HasPendingUrge(id) {
		t.Fatalf("处理后不应再有待处理催菜")
	}
}

func TestOrderSvcLoadOrderForPrint(t *testing.T) {
	orderSvcInit(t)
	id := orderSvcInsertOrder(t, "OPR1", OrderStatusPlaced, 0, 5000)
	if o, ok := LoadOrderForPrint(id); !ok || o.OrderID != id {
		t.Fatalf("存在订单应加载成功, ok=%v o=%+v", ok, o)
	}
	if _, ok := LoadOrderForPrint(999999); ok {
		t.Fatalf("不存在订单应返回 false")
	}
}
