package service

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// customerSvcInit 初始化独立 SQLite 测试库。
func customerSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "customer_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// customerSvcSpecID 取一对真实存在的菜品/规格。
func customerSvcSpecID(t *testing.T) (dishID, specID int) {
	t.Helper()
	if err := store.DB.QueryRow(`SELECT dish_id, spec_id FROM tb_spec ORDER BY spec_id LIMIT 1`).
		Scan(&dishID, &specID); err != nil {
		t.Fatalf("读取种子规格失败: %v", err)
	}
	return
}

func TestCustomerSvcOrderItemsFromPO(t *testing.T) {
	items := orderItemsFromPO([]po.OrderItem{
		{ItemID: 1, DishID: 10, DishName: "测试菜", SpecID: 20, SpecName: "份", Price: 2800, Quantity: 2, Amount: 5600},
	})
	if len(items) != 1 {
		t.Fatalf("转换后应保留 1 条, got %d", len(items))
	}
	if items[0].Price != 28.0 || items[0].Amount != 56.0 {
		t.Fatalf("金额应转换为元: %+v", items[0])
	}
}

func TestCustomerSvcTable(t *testing.T) {
	customerSvcInit(t)

	// 按数字桌台 ID 解析 seed 桌台。
	tbl, cur, _, err := CustomerTable("1")
	if err != nil || tbl.TableID != 1 {
		t.Fatalf("按 ID 解析桌台应成功, got tbl=%+v err=%v", tbl, err)
	}
	if cur != nil {
		t.Fatalf("空闲桌台不应有进行中订单, got %+v", cur)
	}

	// 非法引用。
	if _, _, _, err := CustomerTable("bad-ref"); err == nil {
		t.Fatalf("非法引用应报错")
	}

	// 桌台稳定码缺失时兜底补发。
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?)`, "99", "补码桌", 4, 0, 99, "0", "", now, now)
	if err != nil {
		t.Fatalf("插入无码桌台失败: %v", err)
	}
	id, _ := res.LastInsertId()
	tbl, _, _, err = CustomerTable(strconv.Itoa(int(id)))
	if err != nil {
		t.Fatalf("解析桌台失败: %v", err)
	}
	if tbl.TableCode == "" {
		t.Fatalf("桌台稳定码应被兜底补发")
	}
}

func TestCustomerSvcMenuAndSettings(t *testing.T) {
	customerSvcInit(t)

	menu, err := CustomerMenu()
	if err != nil || len(menu) == 0 {
		t.Fatalf("顾客菜单应返回分类, got len=%d err=%v", len(menu), err)
	}
	totalDishes := 0
	for _, c := range menu {
		totalDishes += len(c.Dishes)
	}
	if totalDishes == 0 {
		t.Fatalf("顾客菜单应包含菜品")
	}

	qr := CustomerPayQr()
	if _, ok := qr["pay_qr_wx"]; !ok {
		t.Fatalf("收款码配置缺少 pay_qr_wx")
	}
	settings := CustomerSettings()
	if settings["wxpay_enabled"] != "0" || settings["alipay_enabled"] != "0" {
		t.Fatalf("在线支付开关默认应为 0, got %+v", settings)
	}
}

func TestCustomerSvcCreateOrder(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)

	t.Run("桌台为空拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(0, 0, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "桌台") {
			t.Fatalf("空桌台应被拒绝, got %v", err)
		}
	})

	t.Run("人数超限拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(0, 1, 101, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "人数") {
			t.Fatalf("超限人数应被拒绝, got %v", err)
		}
	})

	t.Run("桌台不存在拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(0, 999999, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "桌台不存在") {
			t.Fatalf("不存在桌台应被拒绝, got %v", err)
		}
	})

	t.Run("无效规格拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: 999999, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("无效规格应被拒绝, got %v", err)
		}
	})

	t.Run("成功下单并拒绝重复下单", func(t *testing.T) {
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 2}}
		out, err := CustomerCreateOrder(0, 1, 2, "少辣", items)
		if err != nil {
			t.Fatalf("下单应成功, got %v", err)
		}
		if out.OrderID <= 0 || out.OrderNo == "" || len(out.Items) != 1 || out.TotalAmount <= 0 {
			t.Fatalf("下单结果异常: %+v", out)
		}
		// 同一桌台已有未结账订单，再次开新单应被拒绝。
		if _, err := CustomerCreateOrder(0, 1, 2, "", items); err == nil || !strings.Contains(err.Error(), "未结账的订单") {
			t.Fatalf("重复下单应被拒绝, got %v", err)
		}
	})
}

func TestCustomerSvcOrderByNo(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	out, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}

	order, err := CustomerOrderByNo(out.OrderNo)
	if err != nil || order.OrderID != int(out.OrderID) {
		t.Fatalf("按单号查订单应成功, got order=%+v err=%v", order, err)
	}

	if _, err := CustomerOrderByNo("NO_SUCH_ORDER"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
		t.Fatalf("不存在订单应报错, got %v", err)
	}
}

func TestCustomerSvcAppendOrder(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)

	t.Run("参数错误", func(t *testing.T) {
		if _, err := CustomerAppendOrder(0, "", nil, 0); err == nil || !strings.Contains(err.Error(), "参数错误") {
			t.Fatalf("空参数应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if _, err := CustomerAppendOrder(1, "NO_SUCH", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}, 0); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("成功加菜", func(t *testing.T) {
		out, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		app, err := CustomerAppendOrder(1, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 2}}, 0)
		if err != nil {
			t.Fatalf("加菜应成功, got %v", err)
		}
		if app.OrderID != int(out.OrderID) || len(app.Items) != 1 {
			t.Fatalf("加菜结果异常: %+v", app)
		}
	})

	t.Run("桌台不匹配拒绝", func(t *testing.T) {
		out, err := CustomerCreateOrder(0, 3, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := CustomerAppendOrder(4, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}, 0); err == nil || !strings.Contains(err.Error(), "订单不属于当前桌台") {
			t.Fatalf("跨桌台加菜应被拒绝, got %v", err)
		}
	})

	t.Run("已支付不可加菜", func(t *testing.T) {
		out, err := CustomerCreateOrder(0, 2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := PayOrder(int(out.OrderID), "", "tester"); err != nil {
			t.Fatalf("收款失败: %v", err)
		}
		if _, err := CustomerAppendOrder(2, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}, 0); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单不可加菜, got %v", err)
		}
	})
}

// customerSvcTableCode 补发并返回桌台稳定码(顾客扫码路径需稳定码才返回订单能力)。
func customerSvcTableCode(t *testing.T, tableID int) string {
	t.Helper()
	code, err := store.EnsureTableCode(tableID)
	if err != nil {
		t.Fatalf("补发桌台码失败: %v", err)
	}
	return code
}

func TestCustomerSvcIdempotency(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}

	first, err := CustomerCreateOrder(0, 1, 2, "", items, "req-1")
	if err != nil {
		t.Fatalf("首单应成功: %v", err)
	}
	if first.Duplicated {
		t.Fatalf("首单不应标记幂等")
	}
	second, err := CustomerCreateOrder(0, 1, 2, "", items, "req-1")
	if err != nil {
		t.Fatalf("重复请求应返回成功: %v", err)
	}
	if !second.Duplicated || second.OrderNo != first.OrderNo {
		t.Fatalf("重复请求应返回同一订单且标记幂等: %+v vs %+v", second, first)
	}
	var cnt int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE table_id=1 AND pay_status=0 AND order_status IN (1,2,3)`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("重复请求不应产生第二笔未结账单, got %d", cnt)
	}
}

func TestCustomerSvcAppendIdempotency(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	out, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	add := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 2}}
	if _, err := CustomerAppendOrder(1, out.OrderNo, add, 0, "req-append-1"); err != nil {
		t.Fatalf("加菜应成功: %v", err)
	}
	second, err := CustomerAppendOrder(1, out.OrderNo, add, 0, "req-append-1")
	if err != nil {
		t.Fatalf("重复加菜应返回成功: %v", err)
	}
	if !second.Duplicated {
		t.Fatalf("重复加菜应标记幂等")
	}
	var cnt int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_item WHERE order_id=?`, out.OrderID).Scan(&cnt)
	if cnt != 2 {
		t.Fatalf("重复加菜不应重复插明细(应维持 1 原单 + 1 加菜), got %d", cnt)
	}
}

func TestCustomerSvcAppendBatch(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}
	out, err := CustomerCreateOrder(0, 1, 2, "", items)
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}

	batchOf := func() []int {
		rows, err := store.DB.Query(`SELECT batch_no FROM tb_order_item WHERE order_id=? ORDER BY item_id`, out.OrderID)
		if err != nil {
			t.Fatalf("查询批次失败: %v", err)
		}
		defer rows.Close()
		var got []int
		for rows.Next() {
			var b int
			if err := rows.Scan(&b); err != nil {
				t.Fatalf("扫描批次失败: %v", err)
			}
			got = append(got, b)
		}
		return got
	}

	if got := batchOf(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("首次下单明细批次应为 1, got %v", got)
	}
	if _, err := CustomerAppendOrder(1, out.OrderNo, items, 0); err != nil {
		t.Fatalf("第一次加菜失败: %v", err)
	}
	if got := batchOf(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("第一次加菜明细批次应为 2, got %v", got)
	}
	if _, err := CustomerAppendOrder(1, out.OrderNo, items, 0); err != nil {
		t.Fatalf("第二次加菜失败: %v", err)
	}
	if got := batchOf(); len(got) != 3 || got[2] != 3 {
		t.Fatalf("第二次加菜明细批次应为 3, got %v", got)
	}
}

func TestCustomerSvcPayThenNewOrderAndServing(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	code := customerSvcTableCode(t, 1)
	items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}

	out, err := CustomerCreateOrder(0, 1, 2, "", items)
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := PayOrder(int(out.OrderID), "", "tester"); err != nil {
		t.Fatalf("收款失败: %v", err)
	}

	// 扫码进入:无未结账单,但 servingOrders 含已付款未出餐订单。
	_, cur, serving, err := CustomerTable(code)
	if err != nil {
		t.Fatalf("扫码失败: %v", err)
	}
	if cur != nil {
		t.Fatalf("付款后应无未结账单, got %+v", cur)
	}
	if len(serving) != 1 || serving[0].OrderNo != out.OrderNo {
		t.Fatalf("servingOrders 应包含已付款订单: %+v", serving)
	}

	// 付款后同一桌可再开新单(一桌多单)。
	second, err := CustomerCreateOrder(0, 1, 2, "", items)
	if err != nil {
		t.Fatalf("付款后应能开新单: %v", err)
	}
	if second.OrderNo == out.OrderNo {
		t.Fatalf("新单应使用不同订单号")
	}
	// 同一营业日内取餐号继续递增:号重复的后果是服务员叫号出现歧义。
	if second.PickupNo != out.PickupNo+1 {
		t.Fatalf("新单取餐号应比上一单 +1: %d → %d", out.PickupNo, second.PickupNo)
	}
}

// TestCustomerSvcPickupNo 取餐号的赋值与回读:pickupNo 必须当天从 1 起、
// 写进 tb_order、并通过 DTO 的 pickupNo / shortNo 两个字段对外暴露。
func TestCustomerSvcPickupNo(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}

	out, err := CustomerCreateOrder(0, 1, 2, "", items)
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if out.PickupNo != 1 {
		t.Fatalf("当天首单取餐号应为 1, got %d", out.PickupNo)
	}

	// 与 PO 层一致:短号取的就是取餐号,而不是 20 位随机单号的尾 6 位。
	o, err := dao.GetOrderByID(int(out.OrderID))
	if err != nil {
		t.Fatalf("读取订单失败: %v", err)
	}
	if o.PickupNo != 1 || o.BizDate != store.Now()[:10] {
		t.Fatalf("取餐号未落库: %+v", o)
	}

	detail, err := CustomerOrderByNo(out.OrderNo)
	if err != nil {
		t.Fatalf("按单号查单失败: %v", err)
	}
	if detail.PickupNo != 1 || detail.ShortNo != "1" {
		t.Fatalf("顾客端应能看到取餐号: pickupNo=%d shortNo=%q", detail.PickupNo, detail.ShortNo)
	}
}

func TestCustomerSvcUrgeOrder(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)

	t.Run("参数错误", func(t *testing.T) {
		if _, err := CustomerUrgeOrder(0, "  "); err == nil || !strings.Contains(err.Error(), "参数错误") {
			t.Fatalf("空单号应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if _, err := CustomerUrgeOrder(1, "NO_SUCH"); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("桌台不匹配拒绝", func(t *testing.T) {
		out, err := CustomerCreateOrder(0, 3, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := CustomerUrgeOrder(4, out.OrderNo); err == nil || !strings.Contains(err.Error(), "订单不属于当前桌台") {
			t.Fatalf("跨桌台催菜应被拒绝, got %v", err)
		}
	})

	t.Run("成功催菜与冷却", func(t *testing.T) {
		out, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		res, err := CustomerUrgeOrder(1, out.OrderNo)
		if err != nil {
			t.Fatalf("首次催菜应成功, got %v", err)
		}
		if res.OrderID != int(out.OrderID) || res.Cooldown != po.UrgeCooldownSecond {
			t.Fatalf("催菜结果异常: %+v", res)
		}
		if _, err := CustomerUrgeOrder(1, out.OrderNo); err == nil || !strings.Contains(err.Error(), "秒后再催") {
			t.Fatalf("冷却期内应提示等待, got %v", err)
		}
	})
}
