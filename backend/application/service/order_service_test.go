package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
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
		if err := ChangeOrderStatus(id, 9, "", "tester"); err == nil || !strings.Contains(err.Error(), "非法的订单状态") {
			t.Fatalf("非法状态应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if err := ChangeOrderStatus(999999, OrderStatusCooking, "", "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("非法状态跳转", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS2", OrderStatusPlaced, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusDining, "", "tester"); err == nil || !strings.Contains(err.Error(), "非法的状态变更") {
			t.Fatalf("1→3 跳级应被拒绝, got %v", err)
		}
	})

	t.Run("未支付不可完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS3", OrderStatusDining, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusFinished, "", "tester"); err == nil || !strings.Contains(err.Error(), "未支付") {
			t.Fatalf("未支付订单不可完成, got %v", err)
		}
	})

	t.Run("已支付不可取消", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS4", OrderStatusDining, 1, 5000)
		if err := ChangeOrderStatus(id, OrderStatusCanceled, "", "tester"); err == nil || !strings.Contains(err.Error(), "不能取消") {
			t.Fatalf("已支付订单不可取消, got %v", err)
		}
	})

	t.Run("正常流转1→2", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OS5", OrderStatusPlaced, 0, 5000)
		if err := ChangeOrderStatus(id, OrderStatusCooking, "", "tester"); err != nil {
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

	// 结账这条路径一直允许跳过中间状态(WHERE order_status IN (1,2,3)):门店下完单
	// 客人吃完直接结账是常态,不能逼人先点「制作中→上齐」。这里钉住,防止后续收紧。
	t.Run("已下单可直接结账", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSL8", OrderStatusPlaced, 0, 5000)
		if _, err := SettleOrder(id, SettleTypeNormal, "现金", "", "tester"); err != nil {
			t.Fatalf("未上齐也应可直接结账, got %v", err)
		}
		if s, p := orderSvcState(t, id); s != OrderStatusFinished || p != 1 {
			t.Fatalf("结账后期望(状态4,已支付), got (%d,%d)", s, p)
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

// TestOrderSvcPayThenFinish 后付模式的收尾链路:先「只标记收款」,再「完成」归档。
//
// 收款不归档(见 dao.MarkOrderPaid 注释):order_status 仍是 1/2/3,订单形态与先付
// 模式付款后完全一样。此前「完成」只认 3→4,店家收款后若没逐级点过制作中/上齐,
// 这单就只能去点「强制清台」—— 本次放宽状态机解决的正是这个出口缺失。
func TestOrderSvcPayThenFinish(t *testing.T) {
	orderSvcInit(t)
	id := orderSvcInsertOrder(t, "OPF1", OrderStatusPlaced, 0, 5000)
	// 真实链路里桌台由下单占用;这里直接种订单,手动置占用以便验证完成时确实释放。
	if _, err := store.DB.Exec(`UPDATE tb_table SET status=1 WHERE table_id=1`); err != nil {
		t.Fatalf("置桌台占用失败: %v", err)
	}

	if _, err := PayOrder(id, "现金", "tester"); err != nil {
		t.Fatalf("收款应成功, got %v", err)
	}
	if s, p := orderSvcState(t, id); s != OrderStatusPlaced || p != 1 {
		t.Fatalf("收款后应停在(状态1,已支付), got (%d,%d)", s, p)
	}

	if err := FinishOrder(id, "tester"); err != nil {
		t.Fatalf("收款后应可直接完成, got %v", err)
	}
	if s, _ := orderSvcState(t, id); s != OrderStatusFinished {
		t.Fatalf("完成后期望状态 4, got %d", s)
	}

	// 跳过中间状态也要写全归档字段:报表的用餐时长口径不能因路径不同而缺数据。
	var finishTime, mealEnd string
	if err := store.DB.QueryRow(`SELECT COALESCE(finish_time,''), COALESCE(meal_end_time,'') FROM tb_order WHERE order_id=?`, id).
		Scan(&finishTime, &mealEnd); err != nil {
		t.Fatalf("查询归档时间失败: %v", err)
	}
	if finishTime == "" || mealEnd == "" {
		t.Fatalf("完成应写入 finish_time / meal_end_time, got %q / %q", finishTime, mealEnd)
	}

	// 该桌已无进行中订单 → 释放为空闲。
	var tableStatus int
	if err := store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=1`).Scan(&tableStatus); err != nil {
		t.Fatalf("查询桌台状态失败: %v", err)
	}
	if tableStatus != 0 {
		t.Fatalf("完成后桌台应释放为空闲, got %d", tableStatus)
	}
}

func TestOrderSvcSettleCreditOrder(t *testing.T) {
	orderSvcInit(t)

	t.Run("非挂账拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSC1", OrderStatusFinished, 1, 5000)
		if _, _, err := SettleCreditOrder(context.Background(), id, "", "tester"); err == nil || !strings.Contains(err.Error(), "无需核销") {
			t.Fatalf("非挂账订单应拒绝核销, got %v", err)
		}
	})

	t.Run("成功核销", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OSC2", OrderStatusFinished, 1, 5000)
		orderSvcUpdateOrder(t, id, "settle_type='credit'", "credit_status=1", "credit_amount=5000", "paid_amount=0")
		payType, due, err := SettleCreditOrder(context.Background(), id, "现金", "tester")
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
		if _, _, err := SettleCreditOrder(context.Background(), id, "", "tester"); err == nil || !strings.Contains(err.Error(), "无需核销") {
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

	// 门店实际很少逐级点「制作中→上齐」再收尾;先付模式下付款后单子停在 1/2,
	// 若完成只认 3→4,这些单就没有出口(只能走强制清台)。这里钉住可直接完成。
	t.Run("已支付·已下单可直接完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF5", OrderStatusPlaced, 1, 5000)
		if err := FinishOrder(id, "tester"); err != nil {
			t.Fatalf("已支付的已下单订单应可直接完成, got %v", err)
		}
		if s, _ := orderSvcState(t, id); s != OrderStatusFinished {
			t.Fatalf("完成后期望状态 4, got %d", s)
		}
	})

	t.Run("已支付·制作中可直接完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF6", OrderStatusCooking, 1, 5000)
		if err := FinishOrder(id, "tester"); err != nil {
			t.Fatalf("已支付的制作中订单应可直接完成, got %v", err)
		}
	})

	// 放宽的是「跳过中间状态」,不是「可以先完成再收钱」——漏收防线不能跟着松掉
	t.Run("未支付不可直接完成", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OF7", OrderStatusPlaced, 0, 5000)
		if err := FinishOrder(id, "tester"); err == nil || !strings.Contains(err.Error(), "未支付") {
			t.Fatalf("未支付订单不可完成, got %v", err)
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
		if _, err := EditOrder(context.Background(), 999999, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("非进行中不可改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE1", OrderStatusFinished, 1, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(context.Background(), id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "不可改单") {
			t.Fatalf("非进行中订单不可改单, got %v", err)
		}
	})

	t.Run("已支付不可改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE2", OrderStatusPlaced, 1, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(context.Background(), id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单不可改单, got %v", err)
		}
	})

	t.Run("明细为空拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE3", OrderStatusPlaced, 0, 5000)
		if _, err := EditOrder(context.Background(), id, 2, "", nil, "tester"); err == nil || !strings.Contains(err.Error(), "明细不能为空") {
			t.Fatalf("空明细应被拒绝, got %v", err)
		}
	})

	// 与下单/加菜同一上限:茶位费开启时人数直接乘进金额,误填大数会算出巨额应收。
	t.Run("用餐人数超限拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE7", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
		if _, err := EditOrder(context.Background(), id, 101, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "用餐人数超出限制") {
			t.Fatalf("人数超限应被拒绝, got %v", err)
		}
	})

	t.Run("无效规格拒绝", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE4", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: 999999, Quantity: 1}}
		if _, err := EditOrder(context.Background(), id, 2, "", items, "tester"); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("无效规格应被拒绝, got %v", err)
		}
	})

	t.Run("成功改单", func(t *testing.T) {
		id := orderSvcInsertOrder(t, "OE5", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 3}}
		out, err := EditOrder(context.Background(), id, 0, "少辣", items, "tester")
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

	t.Run("赠菜改单", func(t *testing.T) {
		// 关闭茶位费/满减,保证应收金额可精确断言(重算会按配置计茶位费与优惠)。
		for k, v := range map[string]string{"seat_fee_enabled": "0", "promotion_enabled": "0"} {
			if err := dao.SetSetting(k, v); err != nil {
				t.Fatalf("写入配置 %s 失败: %v", k, err)
			}
		}
		var priceCents int64
		store.DB.QueryRow(`SELECT price FROM tb_spec WHERE spec_id=?`, specID).Scan(&priceCents)

		id := orderSvcInsertOrder(t, "OE6", OrderStatusPlaced, 0, 5000)
		items := []dto.OrderItem{
			{DishID: dishID, SpecID: specID, Quantity: 1},
			{DishID: dishID, SpecID: specID, Quantity: 2, IsGift: 1, GiftReason: "老客户招待"},
		}
		out, err := EditOrder(context.Background(), id, 0, "", items, "tester")
		if err != nil {
			t.Fatalf("赠菜改单应成功, got %v", err)
		}
		if out.GiftCount != 1 || out.GiftAmount <= 0 {
			t.Fatalf("赠送统计异常: %+v", out)
		}
		var giftCents, totalCents int64
		store.DB.QueryRow(`SELECT gift_amount, total_amount FROM tb_order WHERE order_id=?`, id).Scan(&giftCents, &totalCents)
		if giftCents != priceCents*2 {
			t.Fatalf("gift_amount 应为 %d, got %d", priceCents*2, giftCents)
		}
		if totalCents != priceCents {
			t.Fatalf("total_amount 应为 %d(不含赠送), got %d", priceCents, totalCents)
		}
		// 明细落库:赠送行 is_gift=1 且原因保留。
		var isGift int
		var giftReason string
		store.DB.QueryRow(`SELECT is_gift, gift_reason FROM tb_order_item WHERE order_id=? AND is_gift=1`, id).Scan(&isGift, &giftReason)
		if isGift != 1 || giftReason != "老客户招待" {
			t.Fatalf("赠送明细落库异常: is_gift=%d reason=%q", isGift, giftReason)
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
