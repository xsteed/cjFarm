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
	tbl, cur, err := CustomerTable("1")
	if err != nil || tbl.TableID != 1 {
		t.Fatalf("按 ID 解析桌台应成功, got tbl=%+v err=%v", tbl, err)
	}
	if cur != nil {
		t.Fatalf("空闲桌台不应有进行中订单, got %+v", cur)
	}

	// 非法引用。
	if _, _, err := CustomerTable("bad-ref"); err == nil {
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
	tbl, _, err = CustomerTable(strconv.Itoa(int(id)))
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
		_, err := CustomerCreateOrder(0, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "桌台") {
			t.Fatalf("空桌台应被拒绝, got %v", err)
		}
	})

	t.Run("人数超限拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(1, 101, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "人数") {
			t.Fatalf("超限人数应被拒绝, got %v", err)
		}
	})

	t.Run("桌台不存在拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(999999, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "桌台不存在") {
			t.Fatalf("不存在桌台应被拒绝, got %v", err)
		}
	})

	t.Run("无效规格拒绝", func(t *testing.T) {
		_, err := CustomerCreateOrder(1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: 999999, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("无效规格应被拒绝, got %v", err)
		}
	})

	t.Run("成功下单并拒绝重复下单", func(t *testing.T) {
		items := []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 2}}
		out, err := CustomerCreateOrder(1, 2, "少辣", items)
		if err != nil {
			t.Fatalf("下单应成功, got %v", err)
		}
		if out.OrderID <= 0 || out.OrderNo == "" || len(out.Items) != 1 || out.TotalAmount <= 0 {
			t.Fatalf("下单结果异常: %+v", out)
		}
		// 同一桌台已有进行中订单，再次开新单应被拒绝。
		if _, err := CustomerCreateOrder(1, 2, "", items); err == nil || !strings.Contains(err.Error(), "进行中的订单") {
			t.Fatalf("重复下单应被拒绝, got %v", err)
		}
	})
}

func TestCustomerSvcOrderByNo(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	out, err := CustomerCreateOrder(1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
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
		if _, err := CustomerAppendOrder(0, "", nil); err == nil || !strings.Contains(err.Error(), "参数错误") {
			t.Fatalf("空参数应被拒绝, got %v", err)
		}
	})

	t.Run("订单不存在", func(t *testing.T) {
		if _, err := CustomerAppendOrder(1, "NO_SUCH", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}); err == nil || !strings.Contains(err.Error(), "订单不存在") {
			t.Fatalf("不存在订单应报错, got %v", err)
		}
	})

	t.Run("成功加菜", func(t *testing.T) {
		out, err := CustomerCreateOrder(1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		app, err := CustomerAppendOrder(1, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 2}})
		if err != nil {
			t.Fatalf("加菜应成功, got %v", err)
		}
		if app.OrderID != int(out.OrderID) || len(app.Items) != 1 {
			t.Fatalf("加菜结果异常: %+v", app)
		}
	})

	t.Run("桌台不匹配拒绝", func(t *testing.T) {
		out, err := CustomerCreateOrder(3, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := CustomerAppendOrder(4, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}); err == nil || !strings.Contains(err.Error(), "订单不属于当前桌台") {
			t.Fatalf("跨桌台加菜应被拒绝, got %v", err)
		}
	})

	t.Run("已支付不可加菜", func(t *testing.T) {
		out, err := CustomerCreateOrder(2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := PayOrder(int(out.OrderID), "", "tester"); err != nil {
			t.Fatalf("收款失败: %v", err)
		}
		if _, err := CustomerAppendOrder(2, out.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}); err == nil || !strings.Contains(err.Error(), "已支付") {
			t.Fatalf("已支付订单不可加菜, got %v", err)
		}
	})
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
		out, err := CustomerCreateOrder(3, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
		if err != nil {
			t.Fatalf("下单失败: %v", err)
		}
		if _, err := CustomerUrgeOrder(4, out.OrderNo); err == nil || !strings.Contains(err.Error(), "订单不属于当前桌台") {
			t.Fatalf("跨桌台催菜应被拒绝, got %v", err)
		}
	})

	t.Run("成功催菜与冷却", func(t *testing.T) {
		out, err := CustomerCreateOrder(1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
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
