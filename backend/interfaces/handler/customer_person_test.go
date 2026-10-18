package handler

import (
	"net/http"
	"testing"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// TestCustomerUpdatePerson 顾客在订单页改就餐人数：成功路径。
//
// 覆盖三条硬约束：
//   - 人数真的落库（顾客端轮询回来要看到新人数）；
//   - 明细一条都不能变 —— 改人数不是加菜，动了明细就会重复扣库存、重复出厨单；
//   - 响应返回最新订单（前端按它刷新账单，不自己在前端重算金额）。
func TestCustomerUpdatePerson(t *testing.T) {
	custInitDB(t)
	orderNo := "D20261018001"
	orderID := custSeedOrder(t, orderNo, 1, 0)

	// 种一条明细：改人数只应重算金额，绝不能重写明细
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name,
		price, quantity, amount, remark, batch_no, is_gift, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, 1, "红烧肉", 1, "大份", 2800, 1, 2800, "", 1, 0, store.Now()); err != nil {
		t.Fatalf("插入明细失败: %v", err)
	}

	w := custCall(t, CustomerUpdatePerson, http.MethodPost, "/api/customer/order/person",
		`{"tableId":1,"orderNo":"`+orderNo+`","personCount":5}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("改人数应成功, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	if int(data["personCount"].(float64)) != 5 {
		t.Fatalf("响应人数应为 5, got %v", data["personCount"])
	}

	var dbPerson int
	if err := store.DB.QueryRow(`SELECT person_count FROM tb_order WHERE order_id=?`, orderID).Scan(&dbPerson); err != nil {
		t.Fatalf("读取订单人数失败: %v", err)
	}
	if dbPerson != 5 {
		t.Fatalf("人数应落库为 5, got %d", dbPerson)
	}

	var itemCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_item WHERE order_id=?`, orderID).Scan(&itemCount); err != nil {
		t.Fatalf("统计明细失败: %v", err)
	}
	if itemCount != 1 {
		t.Fatalf("改人数不应改动明细, got %d 条", itemCount)
	}
}

// TestCustomerUpdatePersonIdempotent 人数没变时直接返回，不报错（连点 / 重复提交）。
func TestCustomerUpdatePersonIdempotent(t *testing.T) {
	custInitDB(t)
	orderNo := "D20261018011"
	custSeedOrder(t, orderNo, 1, 0) // 种子里 person_count = 2

	for i := 0; i < 2; i++ {
		w := custCall(t, CustomerUpdatePerson, http.MethodPost, "/api/customer/order/person",
			`{"tableId":1,"orderNo":"`+orderNo+`","personCount":2}`, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次重复提交应成功, got %d body=%s", i+1, w.Code, w.Body.String())
		}
		if got := int(custData(t, custJSON(t, w))["personCount"].(float64)); got != 2 {
			t.Fatalf("人数应保持 2, got %d", got)
		}
	}
}

// TestCustomerUpdatePersonRejects 各种不该放行的场景。
func TestCustomerUpdatePersonRejects(t *testing.T) {
	custInitDB(t)
	activeOrder := "D20261018021"
	paidOrder := "D20261018022"
	doneOrder := "D20261018023"
	custSeedOrder(t, activeOrder, 1, 0)
	custSeedOrder(t, paidOrder, 1, 1) // 已支付：不能改金额
	custSeedOrder(t, doneOrder, 4, 0) // 已完成

	cases := []struct {
		name string
		body string
		msg  string
	}{
		{"桌台不符", `{"tableId":9,"orderNo":"` + activeOrder + `","personCount":3}`, "订单不属于当前桌台"},
		{"已支付", `{"tableId":1,"orderNo":"` + paidOrder + `","personCount":3}`, "订单不存在或已结束"},
		{"已完成", `{"tableId":1,"orderNo":"` + doneOrder + `","personCount":3}`, "订单不存在或已结束"},
		{"订单不存在", `{"tableId":1,"orderNo":"NOPE","personCount":3}`, "订单不存在或已结束"},
		{"人数为 0", `{"tableId":1,"orderNo":"` + activeOrder + `","personCount":0}`, "用餐人数需在 1~100 之间"},
		{"人数超上限", `{"tableId":1,"orderNo":"` + activeOrder + `","personCount":101}`, "用餐人数需在 1~100 之间"},
		{"缺桌台", `{"tableId":0,"orderNo":"` + activeOrder + `","personCount":3}`, "参数错误"},
		{"非法 JSON", `{"tableId":1,`, "参数错误"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := custCall(t, CustomerUpdatePerson, http.MethodPost, "/api/customer/order/person", c.body, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("应返回 400, got %d body=%s", w.Code, w.Body.String())
			}
			if got := custJSON(t, w)["msg"]; got != c.msg {
				t.Fatalf("文案应为 %q, got %v", c.msg, got)
			}
		})
	}

	// 拒绝路径不能留下任何副作用
	var dbPerson int
	if err := store.DB.QueryRow(`SELECT person_count FROM tb_order WHERE order_no=?`, activeOrder).Scan(&dbPerson); err != nil {
		t.Fatalf("读取订单人数失败: %v", err)
	}
	if dbPerson != 2 {
		t.Fatalf("被拒绝的请求不应改动人数, got %d", dbPerson)
	}
}

// TestCustomerUpdatePersonSeatFee 茶位费随人数重算（开启茶位费时，改人数必须改金额）。
func TestCustomerUpdatePersonSeatFee(t *testing.T) {
	custInitDB(t)
	// 打开茶位费：2 元/人（种子库默认 6 元/人，这里改成 2 便于断言）
	for k, v := range map[string]string{"seat_fee_enabled": "1", "seat_fee": "2", "promotion_enabled": "0"} {
		if err := dao.SetSetting(k, v); err != nil {
			t.Fatalf("更新配置 %s 失败: %v", k, err)
		}
	}

	orderNo := "D20261018031"
	orderID := custSeedOrder(t, orderNo, 1, 0) // person_count=2, dish_amount=2800(分), seat_fee=0
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name,
		price, quantity, amount, remark, batch_no, is_gift, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, 1, "红烧肉", 1, "大份", 2800, 1, 2800, "", 1, 0, store.Now()); err != nil {
		t.Fatalf("插入明细失败: %v", err)
	}

	w := custCall(t, CustomerUpdatePerson, http.MethodPost, "/api/customer/order/person",
		`{"tableId":1,"orderNo":"`+orderNo+`","personCount":4}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("改人数应成功, got %d body=%s", w.Code, w.Body.String())
	}
	data := custData(t, custJSON(t, w))
	// 4 人 × 2 元 = 8 元茶位费；菜品 28 元 → 合计 36 元
	if seat, _ := data["seatFee"].(float64); seat != 8 {
		t.Fatalf("茶位费应为 8, got %v", data["seatFee"])
	}
	if total, _ := data["totalAmount"].(float64); total != 36 {
		t.Fatalf("合计应为 36, got %v", data["totalAmount"])
	}
}
