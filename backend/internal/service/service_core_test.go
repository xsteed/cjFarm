package service

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// coreInitDB 初始化 SQLite 临时库,供依赖 dao/setting 的测试使用。
func coreInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	// 上传目录指向不存在的临时目录,避免 store.Init 误导入工作目录的 uploads。
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "core.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// coreSeedDish 直接落库一条菜品+规格,返回 (dishID, specID),
// 供 ResolveOrderItems 以数据库为准校验的测试播种。
func coreSeedDish(t *testing.T, dishName string, status int, delFlag string, priceCents int64) (int, int) {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time, remark)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, 1, dishName, "", "", status, 1, delFlag, store.Now(), store.Now(), "")
	if err != nil {
		t.Fatalf("插入菜品失败: %v", err)
	}
	dishID, _ := res.LastInsertId()
	res, err = store.DB.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price) VALUES(?,?,?)`, dishID, "标准", priceCents)
	if err != nil {
		t.Fatalf("插入规格失败: %v", err)
	}
	specID, _ := res.LastInsertId()
	return int(dishID), int(specID)
}

// TestCoreCanTransition 校验订单状态机的合法/非法跳转。
func TestCoreCanTransition(t *testing.T) {
	cases := []struct {
		name     string
		from, to int
		want     bool
	}{
		{"下单→制作中", OrderStatusPlaced, OrderStatusCooking, true},
		{"下单→取消", OrderStatusPlaced, OrderStatusCanceled, true},
		{"下单→上齐(跳级)", OrderStatusPlaced, OrderStatusDining, false},
		{"下单→完成(跳级)", OrderStatusPlaced, OrderStatusFinished, false},
		{"下单→自己", OrderStatusPlaced, OrderStatusPlaced, false},
		{"制作中→上齐", OrderStatusCooking, OrderStatusDining, true},
		{"制作中→取消", OrderStatusCooking, OrderStatusCanceled, true},
		{"制作中→回退下单", OrderStatusCooking, OrderStatusPlaced, false},
		{"制作中→完成(跳级)", OrderStatusCooking, OrderStatusFinished, false},
		{"上齐→完成", OrderStatusDining, OrderStatusFinished, true},
		{"上齐→取消", OrderStatusDining, OrderStatusCanceled, true},
		{"上齐→回退下单", OrderStatusDining, OrderStatusPlaced, false},
		{"上齐→回退制作中", OrderStatusDining, OrderStatusCooking, false},
		{"完成(终态)→任意", OrderStatusFinished, OrderStatusCanceled, false},
		{"完成(终态)→下单", OrderStatusFinished, OrderStatusPlaced, false},
		{"取消(终态)→任意", OrderStatusCanceled, OrderStatusPlaced, false},
		{"非法状态0→下单", 0, OrderStatusPlaced, false},
		{"下单→非法状态0", OrderStatusPlaced, 0, false},
		{"非法状态6→下单", 6, OrderStatusPlaced, false},
		{"下单→非法状态6", OrderStatusPlaced, 6, false},
		{"负数→下单", -1, OrderStatusPlaced, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanTransition(c.from, c.to); got != c.want {
				t.Fatalf("CanTransition(%d,%d)=%v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

// TestCoreActiveStatus 校验进行中状态判定。
func TestCoreActiveStatus(t *testing.T) {
	cases := []struct {
		s    int
		want bool
	}{
		{OrderStatusPlaced, true},
		{OrderStatusCooking, true},
		{OrderStatusDining, true},
		{OrderStatusFinished, false},
		{OrderStatusCanceled, false},
		{0, false},
		{6, false},
		{-1, false},
	}
	for _, c := range cases {
		if got := ActiveStatus(c.s); got != c.want {
			t.Fatalf("ActiveStatus(%d)=%v, want %v", c.s, got, c.want)
		}
	}
}

// TestCoreValidOrderStatus 校验订单状态合法性(1~5)。
func TestCoreValidOrderStatus(t *testing.T) {
	cases := []struct {
		s    int
		want bool
	}{
		{1, true},
		{5, true},
		{0, false},
		{6, false},
		{-1, false},
		{100, false},
	}
	for _, c := range cases {
		if got := ValidOrderStatus(c.s); got != c.want {
			t.Fatalf("ValidOrderStatus(%d)=%v, want %v", c.s, got, c.want)
		}
	}
}

// TestCoreValidSettleType 校验结算方式合法性(空值兼容旧客户端)。
func TestCoreValidSettleType(t *testing.T) {
	cases := []struct {
		t    string
		want bool
	}{
		{"", true},
		{SettleTypeNormal, true},
		{SettleTypeFree, true},
		{SettleTypeCredit, true},
		{"NORMAL", false},
		{"cash", false},
		{"x", false},
		{"free ", false},
	}
	for _, c := range cases {
		if got := ValidSettleType(c.t); got != c.want {
			t.Fatalf("ValidSettleType(%q)=%v, want %v", c.t, got, c.want)
		}
	}
}

// TestCoreNormalizeSettleType 校验结算方式归一化(空值为正常收款)。
func TestCoreNormalizeSettleType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", SettleTypeNormal},
		{SettleTypeNormal, SettleTypeNormal},
		{SettleTypeFree, SettleTypeFree},
		{SettleTypeCredit, SettleTypeCredit},
	}
	for _, c := range cases {
		if got := NormalizeSettleType(c.in); got != c.want {
			t.Fatalf("NormalizeSettleType(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestCoreNormalizePayType 校验支付方式归一化:空白默认现金、超长截断、去空格。
func TestCoreNormalizePayType(t *testing.T) {
	long16 := strings.Repeat("很", 16)
	long20 := strings.Repeat("很", 20)
	cases := []struct {
		in   string
		want string
	}{
		{"", "现金"},
		{"   ", "现金"},
		{"\t\n", "现金"},
		{"微信", "微信"},
		{" 微信 ", "微信"},
		{long16, long16},
		{long20, long16},
		{"abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnop"},
	}
	for _, c := range cases {
		if got := NormalizePayType(c.in); got != c.want {
			t.Fatalf("NormalizePayType(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestCoreCalcSettle 校验按结算方式计算实收/挂账金额。
func TestCoreCalcSettle(t *testing.T) {
	const total = int64(12345)
	cases := []struct {
		name       string
		settleType string
		wantPaid   int64
		wantCredit int
		wantCents  int64
	}{
		{"正常收款", SettleTypeNormal, total, CreditStatusNone, 0},
		{"免单", SettleTypeFree, 0, CreditStatusNone, 0},
		{"挂账", SettleTypeCredit, 0, CreditStatusPending, total},
		{"未知类型按正常处理", "cash", total, CreditStatusNone, 0},
		{"空值按正常处理", "", total, CreditStatusNone, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CalcSettle(c.settleType, total)
			if got.PaidCents != c.wantPaid || got.CreditStatus != c.wantCredit || got.CreditCents != c.wantCents {
				t.Fatalf("CalcSettle(%q,%d)=%+v, want paid=%d creditStatus=%d creditCents=%d",
					c.settleType, total, got, c.wantPaid, c.wantCredit, c.wantCents)
			}
		})
	}
}

// TestCoreRandHexEdge 补充 randHex 的边界:0/奇数长度退化为空串,正常长度为偶数十六进制。
func TestCoreRandHexEdge(t *testing.T) {
	if got := randHex(0); got != "" {
		t.Fatalf("randHex(0) 应为空串, got %q", got)
	}
	if got := randHex(1); got != "" {
		t.Fatalf("randHex(1) 应为空串, got %q", got)
	}
	if got := randHex(4); len(got) != 4 {
		t.Fatalf("randHex(4) 长度应为 4, got %q", got)
	}
}

// coreAmounts 构造仅含金额的订单明细,便于金额重算测试。
func coreAmounts(amounts ...float64) []dto.OrderItem {
	items := make([]dto.OrderItem, 0, len(amounts))
	for _, a := range amounts {
		items = append(items, dto.OrderItem{Amount: a})
	}
	return items
}

// coreNear 断言浮点金额在分级别容差内相等。
func coreNear(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// TestCoreRecalcAmount 校验金额重算:菜品金额 + 餐位费 - 优惠(依赖配置)。
func TestCoreRecalcAmount(t *testing.T) {
	cases := []struct {
		name      string
		seatOn    string // seat_fee_enabled
		seatFee   string
		promoOn   string // promotion_enabled
		threshold string
		discount  string
		personCnt int
		items     []dto.OrderItem
		wantDish  float64
		wantSeat  float64
		wantDisc  float64
		wantTotal float64
	}{
		{"无餐位费无优惠", "0", "6", "0", "100", "10", 3, coreAmounts(10, 20.5), 30.5, 0, 0, 30.5},
		{"餐位费按人数计", "1", "6", "0", "100", "10", 3, coreAmounts(10, 20.5), 30.5, 18, 0, 48.5},
		{"满减优惠", "0", "6", "1", "100", "10", 3, coreAmounts(250), 250, 0, 20, 230},
		{"优惠可叠加", "0", "6", "1", "100", "80", 3, coreAmounts(150), 150, 0, 80, 70},
		{"优惠封顶于菜品金额", "0", "6", "1", "50", "100", 3, coreAmounts(150), 150, 0, 150, 0},
		{"未达门槛不优惠", "0", "6", "1", "100", "10", 3, coreAmounts(99), 99, 0, 0, 99},
		{"空明细", "0", "6", "0", "100", "10", 1, coreAmounts(), 0, 0, 0, 0},
		{"餐位费+优惠叠加", "1", "5", "1", "100", "10", 4, coreAmounts(250), 250, 20, 20, 250},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			coreInitDB(t)
			for k, v := range map[string]string{
				"seat_fee_enabled":    c.seatOn,
				"seat_fee":            c.seatFee,
				"promotion_enabled":   c.promoOn,
				"promotion_threshold": c.threshold,
				"promotion_discount":  c.discount,
			} {
				if err := dao.SetSetting(k, v); err != nil {
					t.Fatalf("写入配置 %s 失败: %v", k, err)
				}
			}
			dish, seat, disc, total := RecalcAmount(c.personCnt, c.items)
			coreNear(t, "菜品金额", dish, c.wantDish)
			coreNear(t, "餐位费", seat, c.wantSeat)
			coreNear(t, "优惠", disc, c.wantDisc)
			coreNear(t, "合计", total, c.wantTotal)
		})
	}
}

// TestCoreResolveOrderItemsValidation 校验订单明细的各类非法入参。
func TestCoreResolveOrderItemsValidation(t *testing.T) {
	t.Run("空明细", func(t *testing.T) {
		coreInitDB(t)
		_, err := ResolveOrderItems(nil)
		if err == nil || !strings.Contains(err.Error(), "订单明细不能为空") {
			t.Fatalf("空明细应报「订单明细不能为空」, got %v", err)
		}
	})
	t.Run("明细过多", func(t *testing.T) {
		coreInitDB(t)
		items := make([]dto.OrderItem, 51)
		for i := range items {
			items[i] = dto.OrderItem{SpecID: 1, DishID: 1, Quantity: 1}
		}
		_, err := ResolveOrderItems(items)
		if err == nil || !strings.Contains(err.Error(), "单次点菜过多") {
			t.Fatalf("明细过多应报「单次点菜过多」, got %v", err)
		}
	})
	t.Run("规格ID为空", func(t *testing.T) {
		coreInitDB(t)
		_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: 0, DishID: 1, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "菜品规格无效") {
			t.Fatalf("规格ID为空应报「菜品规格无效」, got %v", err)
		}
	})
	t.Run("菜品不存在", func(t *testing.T) {
		coreInitDB(t)
		_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: 99999, DishID: 99999, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "部分菜品不存在") {
			t.Fatalf("菜品不存在应报「部分菜品不存在」, got %v", err)
		}
	})
	t.Run("菜品已删除", func(t *testing.T) {
		coreInitDB(t)
		dishID, specID := coreSeedDish(t, "已删菜", 1, po.DelFlagDeleted, 1000)
		_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "已下架") {
			t.Fatalf("已删除菜品应报「已下架」, got %v", err)
		}
	})
	t.Run("菜品已下架", func(t *testing.T) {
		coreInitDB(t)
		dishID, specID := coreSeedDish(t, "下架菜", 0, po.DelFlagOK, 1000)
		_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 1}})
		if err == nil || !strings.Contains(err.Error(), "已下架") {
			t.Fatalf("下架菜品应报「已下架」, got %v", err)
		}
	})
}

// TestCoreResolveOrderItemsRebuild 校验以数据库为准重建明细,覆盖客户端篡改。
func TestCoreResolveOrderItemsRebuild(t *testing.T) {
	coreInitDB(t)
	dishID, specID := coreSeedDish(t, "真实菜名", 1, po.DelFlagOK, 1234) // 单价 12.34 元

	out, err := ResolveOrderItems([]dto.OrderItem{{
		DishID:   dishID,
		SpecID:   specID,
		DishName: "篡改菜名",
		SpecName: "篡改规格",
		Price:    9999.99,
		Quantity: 2,
	}})
	if err != nil {
		t.Fatalf("ResolveOrderItems 失败: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("明细条数 = %d, want 1", len(out))
	}
	got := out[0]
	if got.DishName != "真实菜名" {
		t.Fatalf("DishName 应以数据库为准, got %q", got.DishName)
	}
	if got.SpecName != "标准" {
		t.Fatalf("SpecName 应以数据库为准, got %q", got.SpecName)
	}
	coreNear(t, "Price", got.Price, 12.34)
	coreNear(t, "Amount", got.Amount, 24.68)
}

// TestCoreResolveOrderItemsQuantity 校验数量归一化与上限。
func TestCoreResolveOrderItemsQuantity(t *testing.T) {
	coreInitDB(t)
	dishID, specID := coreSeedDish(t, "数量菜", 1, po.DelFlagOK, 500) // 5 元

	t.Run("数量小于1归一化为1", func(t *testing.T) {
		out, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 0}})
		if err != nil {
			t.Fatalf("ResolveOrderItems 失败: %v", err)
		}
		if out[0].Quantity != 1 {
			t.Fatalf("Quantity 应归一化为 1, got %d", out[0].Quantity)
		}
		coreNear(t, "Amount", out[0].Amount, 5)
	})
	t.Run("负数量归一化为1", func(t *testing.T) {
		out, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: -5}})
		if err != nil {
			t.Fatalf("ResolveOrderItems 失败: %v", err)
		}
		if out[0].Quantity != 1 {
			t.Fatalf("Quantity 应归一化为 1, got %d", out[0].Quantity)
		}
	})
	t.Run("数量达到上限99合法", func(t *testing.T) {
		out, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 99}})
		if err != nil {
			t.Fatalf("数量99应合法, got %v", err)
		}
		if out[0].Quantity != 99 {
			t.Fatalf("Quantity = %d, want 99", out[0].Quantity)
		}
	})
	t.Run("数量超过99", func(t *testing.T) {
		_, err := ResolveOrderItems([]dto.OrderItem{{SpecID: specID, DishID: dishID, Quantity: 100}})
		if err == nil || !strings.Contains(err.Error(), "数量超出上限") {
			t.Fatalf("数量100应报「数量超出上限」, got %v", err)
		}
	})
}
