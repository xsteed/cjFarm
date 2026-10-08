package service

import (
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// stockSeedSpec 取一对种子规格并把库存设为 stock（-1 不限量）。
func stockSeedSpec(t *testing.T, stock int64) (dishID, specID int) {
	t.Helper()
	if err := store.DB.QueryRow(`SELECT dish_id, spec_id FROM tb_spec ORDER BY spec_id LIMIT 1`).
		Scan(&dishID, &specID); err != nil {
		t.Fatalf("读取种子规格失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_spec SET stock=? WHERE spec_id=?`, stock, specID); err != nil {
		t.Fatalf("设置库存失败: %v", err)
	}
	return
}

func stockTableID(t *testing.T) int {
	t.Helper()
	var id int
	if err := store.DB.QueryRow(`SELECT table_id FROM tb_table ORDER BY table_id LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("读取桌台失败: %v", err)
	}
	return id
}

func stockOf(t *testing.T, specID int) int64 {
	t.Helper()
	var s int64
	if err := store.DB.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&s); err != nil {
		t.Fatalf("读取库存失败: %v", err)
	}
	return s
}

// 零值陷阱：未设置库存（Go 零值 0）必须归一化为「不限量 -1」，而不是「售罄 0」。
func TestStockNormalizeOnCreate(t *testing.T) {
	customerSvcInit(t)

	id, err := dao.CreateDish(po.Dish{CategoryID: 1, DishName: "库存归一化菜", Status: 1, SortOrder: 9000},
		[]po.Spec{{SpecName: "份", Price: 1000}})
	if err != nil {
		t.Fatalf("创建菜品失败: %v", err)
	}
	specs := dao.LoadDishSpecs(int(id))
	if len(specs) != 1 || specs[0].Stock != -1 {
		t.Fatalf("未设置库存应归一化为 -1（不限量）, got %+v", specs)
	}

	id2, err := dao.CreateDish(po.Dish{CategoryID: 1, DishName: "库存显式菜", Status: 1, SortOrder: 9001},
		[]po.Spec{{SpecName: "份", Price: 1000, Stock: 5}})
	if err != nil {
		t.Fatalf("创建菜品失败: %v", err)
	}
	specs2 := dao.LoadDishSpecs(int(id2))
	if len(specs2) != 1 || specs2[0].Stock != 5 {
		t.Fatalf("显式库存应原样保存, got %+v", specs2)
	}
}

func TestStockResolveRejectsSoldOut(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := stockSeedSpec(t, 0)

	_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}})
	if err == nil || !strings.Contains(err.Error(), "已售罄") {
		t.Fatalf("库存 0 应报「已售罄」, got %v", err)
	}
}

func TestStockCreateDecrementAndCancelRestore(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := stockSeedSpec(t, 5)
	tableID := stockTableID(t)

	res, err := CustomerCreateOrder(0, tableID, 2, "", []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 2}}, "req-stock-1")
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if got := stockOf(t, specID); got != 3 {
		t.Fatalf("下单 2 份后库存应为 3, got %d", got)
	}

	if _, err := CancelOrder(int(res.OrderID), "测试取消", "tester"); err != nil {
		t.Fatalf("取消订单失败: %v", err)
	}
	if got := stockOf(t, specID); got != 5 {
		t.Fatalf("取消未支付订单后库存应回补到 5, got %d", got)
	}
}

func TestStockAppendDecrements(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := stockSeedSpec(t, 5)
	tableID := stockTableID(t)

	res, err := CustomerCreateOrder(0, tableID, 2, "", []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}}, "req-stock-a1")
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := CustomerAppendOrder(tableID, res.OrderNo, []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}}, 2, "req-stock-a2"); err != nil {
		t.Fatalf("加菜失败: %v", err)
	}
	if got := stockOf(t, specID); got != 3 {
		t.Fatalf("下单 1 + 加菜 1 后库存应为 3, got %d", got)
	}
}

func TestStockEditOrderAdjusts(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := stockSeedSpec(t, 5)
	tableID := stockTableID(t)

	res, err := CustomerCreateOrder(0, tableID, 2, "", []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 2}}, "req-stock-e1")
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	// 改单 2 → 1：回补 2、扣 1，净 +1。
	if _, err := EditOrder(int(res.OrderID), 2, "", []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}}, "tester"); err != nil {
		t.Fatalf("改单失败: %v", err)
	}
	if got := stockOf(t, specID); got != 4 {
		t.Fatalf("改单 2→1 后库存应为 4, got %d", got)
	}
}

// 并发兜底：库存 1 份、一次要 2 份 —— ResolveOrderItems 放行（库存 > 0），
// 事务内扣减应失败并回滚，不让订单落库。
func TestStockOverdraftRollsBack(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := stockSeedSpec(t, 1)
	tableID := stockTableID(t)

	_, err := CustomerCreateOrder(0, tableID, 2, "", []dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 2}}, "req-stock-over")
	if err == nil || !strings.Contains(err.Error(), "售罄") {
		t.Fatalf("库存不足应报售罄, got %v", err)
	}
	if got := stockOf(t, specID); got != 1 {
		t.Fatalf("下单失败后库存应保持 1, got %d", got)
	}
}
