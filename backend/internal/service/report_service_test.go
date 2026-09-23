package service

import (
	"path/filepath"
	"testing"
	"time"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// reportSvcInit 初始化独立 SQLite 测试库。
func reportSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "report_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// reportSvcInsertOrder 种一条订单，create_time/update_time 均为当前时间。
func reportSvcInsertOrder(t *testing.T, orderNo string, status, payStatus int, totalCents int64, personCount int) int {
	t.Helper()
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, 1, "T1", "测试桌", personCount, status, totalCents, 0, 0, totalCents, payStatus, now, now)
	if err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// reportSvcUpdateOrder 通过片段更新订单。
func reportSvcUpdateOrder(t *testing.T, id int, sets ...string) {
	t.Helper()
	for _, s := range sets {
		if _, err := store.DB.Exec(`UPDATE tb_order SET `+s+` WHERE order_id=?`, id); err != nil {
			t.Fatalf("更新订单失败(%s): %v", s, err)
		}
	}
}

func TestReportSvcResolveRange(t *testing.T) {
	today := dayStart(time.Now())

	t.Run("默认近N天", func(t *testing.T) {
		start, end := resolveRange("", "", "", 7, 366)
		if !end.Equal(today) || !start.Equal(today.AddDate(0, 0, -6)) {
			t.Fatalf("默认区间应为 [today-6, today], got [%v, %v]", start, end)
		}
	})

	t.Run("days参数", func(t *testing.T) {
		start, end := resolveRange("", "", "3", 7, 366)
		if !end.Equal(today) || !start.Equal(today.AddDate(0, 0, -2)) {
			t.Fatalf("days=3 应为 [today-2, today], got [%v, %v]", start, end)
		}
	})

	t.Run("自定义区间优先", func(t *testing.T) {
		start, end := resolveRange("2024-01-01", "2024-01-05", "3", 7, 366)
		if start.Format(dayLayout) != "2024-01-01" || end.Format(dayLayout) != "2024-01-05" {
			t.Fatalf("自定义区间应优先, got [%v, %v]", start, end)
		}
	})

	t.Run("起止颠倒自动交换", func(t *testing.T) {
		start, end := resolveRange("2024-01-10", "2024-01-01", "", 7, 366)
		if start.Format(dayLayout) != "2024-01-01" || end.Format(dayLayout) != "2024-01-10" {
			t.Fatalf("起止颠倒应交换, got [%v, %v]", start, end)
		}
	})

	t.Run("超出最大天数截断", func(t *testing.T) {
		start, end := resolveRange("2022-01-01", "2024-01-01", "", 7, 366)
		if end.Format(dayLayout) != "2024-01-01" {
			t.Fatalf("结束日应保留, got %v", end)
		}
		if end.Sub(start) > time.Duration(365)*24*time.Hour {
			t.Fatalf("区间应被夹到最大天数内, got %v", end.Sub(start))
		}
	})

	t.Run("非法日期回退默认", func(t *testing.T) {
		start, end := resolveRange("bad", "bad", "abc", 7, 366)
		if !end.Equal(today) || !start.Equal(today.AddDate(0, 0, -6)) {
			t.Fatalf("非法日期应回退默认, got [%v, %v]", start, end)
		}
	})
}

func TestReportSvcRoundAvg(t *testing.T) {
	if got := roundAvg(0, 0); got != 0 {
		t.Fatalf("笔数为 0 时客单价应为 0, got %v", got)
	}
	if got := roundAvg(10000, 2); got != 50.0 {
		t.Fatalf("10000 分 / 2 笔 = 50 元, got %v", got)
	}
}

func TestReportSvcReportSummary(t *testing.T) {
	reportSvcInit(t)
	now := store.Now()

	// 进行中订单：计入在途订单数、今日订单数与客流。
	reportSvcInsertOrder(t, "RP1", OrderStatusPlaced, 0, 8000, 3)

	// 已完成正常收款订单：计入营业额与完成数。
	id2 := reportSvcInsertOrder(t, "RP2", OrderStatusFinished, 1, 10000, 2)
	reportSvcUpdateOrder(t, id2, "settle_type='normal'", "paid_amount=10000", "pay_time='"+now+"'", "finish_time='"+now+"'")

	// 免单订单：计入让利额。
	id3 := reportSvcInsertOrder(t, "RP3", OrderStatusFinished, 1, 5000, 2)
	reportSvcUpdateOrder(t, id3, "settle_type='free'", "paid_amount=0", "finish_time='"+now+"'")

	// 挂账待收款订单。
	id4 := reportSvcInsertOrder(t, "RP4", OrderStatusFinished, 1, 3000, 1)
	reportSvcUpdateOrder(t, id4, "settle_type='credit'", "credit_status=1", "credit_amount=3000", "paid_amount=0", "finish_time='"+now+"'")

	// 已取消订单。
	reportSvcInsertOrder(t, "RP5", OrderStatusCanceled, 0, 2000, 1)

	s := ReportSummary()
	if s.TableCount < 1 || s.FreeTableCount < 1 {
		t.Fatalf("桌台统计异常: %+v", s)
	}
	if s.ActiveOrderCount != 1 {
		t.Fatalf("在途订单数应为 1, got %d", s.ActiveOrderCount)
	}
	if s.TodayOrderCount != 4 {
		t.Fatalf("今日订单数应为 4(不含取消), got %d", s.TodayOrderCount)
	}
	if s.TodayFinishedCount != 3 {
		t.Fatalf("今日完成数应为 3, got %d", s.TodayFinishedCount)
	}
	if s.TodayCancelCount != 1 {
		t.Fatalf("今日取消数应为 1, got %d", s.TodayCancelCount)
	}
	if s.TodayGuestCount != 8 {
		t.Fatalf("今日客流应为 8, got %d", s.TodayGuestCount)
	}
	if s.TodayAmount != 100.0 {
		t.Fatalf("今日营业额应为 100 元, got %v", s.TodayAmount)
	}
	if s.TodayFreeAmount != 50.0 {
		t.Fatalf("今日免单让利应为 50 元, got %v", s.TodayFreeAmount)
	}
	if s.CreditPendingAmount != 30.0 || s.CreditPendingCount != 1 {
		t.Fatalf("挂账待收款统计异常: %+v", s)
	}
}

func TestReportSvcTrendsAndStats(t *testing.T) {
	reportSvcInit(t)
	todayStr := store.Now()[:10]

	// 今日订单，用于折线与分时。
	id := reportSvcInsertOrder(t, "RT1", OrderStatusFinished, 1, 9000, 2)
	reportSvcUpdateOrder(t, id, "settle_type='normal'", "paid_amount=9000", "pay_time='"+todayStr+" 13:20:00'", "create_time='"+todayStr+" 13:20:00'")

	trend := DailyTrend(todayStr, todayStr, "")
	if len(trend) != 1 {
		t.Fatalf("单日趋势应返回 1 项, got %d", len(trend))
	}
	if trend[0].OrderCount != 1 || trend[0].Amount != 90.0 || trend[0].GuestCount != 2 {
		t.Fatalf("单日趋势数据异常: %+v", trend[0])
	}

	monthly := MonthlyTrend()
	if len(monthly) != 12 {
		t.Fatalf("月度趋势应返回 12 项, got %d", len(monthly))
	}

	hourly, err := Hourly(todayStr, todayStr, "")
	if err != nil || len(hourly) != 24 {
		t.Fatalf("分时统计应返回 24 项, got %d err=%v", len(hourly), err)
	}
	if hourly[13].OrderCount != 1 {
		t.Fatalf("13 点应有 1 笔订单, got %+v", hourly[13])
	}

	mix, err := SettleMix(todayStr, todayStr, "")
	if err != nil || len(mix) == 0 {
		t.Fatalf("结算构成查询失败: len=%d err=%v", len(mix), err)
	}
	foundNormal := false
	for _, m := range mix {
		if m.SettleType == SettleTypeNormal && m.OrderCount == 1 {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("结算构成应包含 normal 分组: %+v", mix)
	}
}

func TestReportSvcDishRank(t *testing.T) {
	reportSvcInit(t)

	// 一笔未取消订单，两份不同菜品。
	id := reportSvcInsertOrder(t, "RD1", OrderStatusPlaced, 0, 10000, 2)
	now := store.Now()
	items := []struct {
		name string
		qty  int
		amt  int64
	}{
		{name: "低价菜", qty: 3, amt: 3000},
		{name: "高价菜", qty: 1, amt: 8000},
	}
	for _, it := range items {
		if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount)
			VALUES(?,?,?,?,?,?,?,?)`, id, 0, it.name, 0, "份", it.amt/int64(it.qty), it.qty, it.amt); err != nil {
			t.Fatalf("插入订单明细失败: %v", err)
		}
	}
	_ = now

	qtyRank, err := DishRank("qty", 10)
	if err != nil || len(qtyRank) < 2 {
		t.Fatalf("销量排行应返回数据, got %+v err=%v", qtyRank, err)
	}
	if qtyRank[0].DishName != "低价菜" {
		t.Fatalf("按销量排行第一名应为低价菜, got %+v", qtyRank[0])
	}

	amtRank, err := DishRank("amount", 10)
	if err != nil || len(amtRank) < 2 {
		t.Fatalf("销售额排行应返回数据, got %+v err=%v", amtRank, err)
	}
	if amtRank[0].DishName != "高价菜" {
		t.Fatalf("按销售额排行第一名应为高价菜, got %+v", amtRank[0])
	}
}
