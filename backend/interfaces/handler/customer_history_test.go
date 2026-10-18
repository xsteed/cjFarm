package handler

import (
	"testing"

	"dining-system/application/service"
	"dining-system/infrastructure/store"
)

// TestCustomerMyOrderDetail 会员查看自己的历史订单详情。
//
// 这一条的核心是**归属校验**:历史订单是跨桌的,不能用 tableId 证明,只能靠
// customer_id。少了这个条件,任何登录用户都能凭单号(会出现在小票/照片/群聊里)
// 遍历他人订单 —— 所以「别人的单」与「不存在的单」必须返回同一文案。
func TestCustomerMyOrderDetail(t *testing.T) {
	custInitDB(t)

	const me = 88
	orderNo := "D20261018041"
	orderID := custSeedOrder(t, orderNo, 4, 1) // 已完成、已支付的历史单
	if _, err := store.DB.Exec(`UPDATE tb_order SET customer_id=? WHERE order_id=?`, me, orderID); err != nil {
		t.Fatalf("绑定会员失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name,
		price, quantity, amount, remark, batch_no, is_gift, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, 1, "红烧肉", 1, "大份", 2800, 2, 5600, "不要辣", 1, 0, store.Now()); err != nil {
		t.Fatalf("插入明细失败: %v", err)
	}

	// 别人的单：customer_id 保持 0
	otherNo := "D20261018042"
	custSeedOrder(t, otherNo, 4, 1)

	t.Run("自己的单能查到，且带明细", func(t *testing.T) {
		order, err := service.CustomerMyOrderDetail(me, orderNo)
		if err != nil {
			t.Fatalf("应查到自己的订单: %v", err)
		}
		if order.OrderNo != orderNo {
			t.Fatalf("单号错误: %s", order.OrderNo)
		}
		if len(order.Items) != 1 {
			t.Fatalf("应带 1 条明细, got %d", len(order.Items))
		}
		if order.Items[0].DishName != "红烧肉" || order.Items[0].Quantity != 2 {
			t.Fatalf("明细内容错误: %+v", order.Items[0])
		}
		if order.Items[0].Remark != "不要辣" {
			t.Fatalf("明细备注应透出: %q", order.Items[0].Remark)
		}
	})

	t.Run("别人的单：与不存在的单同一文案", func(t *testing.T) {
		if _, err := service.CustomerMyOrderDetail(me, otherNo); err == nil || err.Error() != "订单不存在" {
			t.Fatalf("他人的订单应返回「订单不存在」, got %v", err)
		}
		if _, err := service.CustomerMyOrderDetail(me, "NOPE"); err == nil || err.Error() != "订单不存在" {
			t.Fatalf("不存在的订单应返回「订单不存在」, got %v", err)
		}
	})

	t.Run("未登录（customerID=0）一律拒绝", func(t *testing.T) {
		if _, err := service.CustomerMyOrderDetail(0, orderNo); err == nil {
			t.Fatal("未登录不应查到任何订单")
		}
	})
}
