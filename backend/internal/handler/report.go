package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"dining-system/internal/service"
)

// ============ 报表 ============
//
// 报表统计口径与区间解析已下沉到 service 层,本文件只做参数解析、调 service、
// 组装响应出参。营收/订单/客流等统计口径详见 service.ReportSummary 等方法的注释。

func ReportSummary(c *gin.Context) {
	s := service.ReportSummary()
	ok(c, gin.H{
		"todayAmount":        s.TodayAmount,
		"todayOrderCount":    s.TodayOrderCount,
		"todayFinishedCount": s.TodayFinishedCount,
		"todayGuestCount":    s.TodayGuestCount,
		"todayAvgAmount":     s.TodayAvgAmount,
		"todayCancelCount":   s.TodayCancelCount,
		"todayRefundAmount":  s.TodayRefundAmount,
		"monthAmount":        s.MonthAmount,
		"monthOrderCount":    s.MonthOrderCount,
		"monthGuestCount":    s.MonthGuestCount,
		"monthAvgAmount":     s.MonthAvgAmount,
		"freeTableCount":     s.FreeTableCount,
		"tableCount":         s.TableCount,
		"activeOrderCount":   s.ActiveOrderCount,
		// 环比基准:后端只给绝对值,涨跌幅与方向由前端统一渲染
		"yesterdayAmount":               s.YesterdayAmount,
		"yesterdayOrderCount":           s.YesterdayOrderCount,
		"yesterdayGuestCount":           s.YesterdayGuestCount,
		"lastMonthSamePeriodAmount":     s.LastMonthSamePeriodAmount,
		"lastMonthSamePeriodOrderCount": s.LastMonthSamePeriodOrderCount,
		// 免单 / 挂账
		"todayFreeAmount":          s.TodayFreeAmount,
		"creditPendingAmount":      s.CreditPendingAmount,
		"creditPendingCount":       s.CreditPendingCount,
		"todayCreditAmount":        s.TodayCreditAmount,
		"todayCreditSettledAmount": s.TodayCreditSettledAmount,
		"todayCreditSettledCount":  s.TodayCreditSettledCount,
	})
}

// ReportDailyTrend 区间内逐日营业额/订单/客流,用于折线趋势。
func ReportDailyTrend(c *gin.Context) {
	items := service.DailyTrend(c.Query("start"), c.Query("end"), c.Query("days"))
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"date":       it.Date,
			"amount":     it.Amount,
			"orderCount": it.OrderCount,
			"guestCount": it.GuestCount,
		})
	}
	ok(c, out)
}

func ReportMonthlyTrend(c *gin.Context) {
	items := service.MonthlyTrend()
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"month":      it.Month,
			"amount":     it.Amount,
			"orderCount": it.OrderCount,
			"guestCount": it.GuestCount,
		})
	}
	ok(c, out)
}

// ReportDishRank 菜品排行,sort=qty(默认,按销量) / sort=amount(按销售额)。
func ReportDishRank(c *gin.Context) {
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	rows, err := service.DishRank(c.Query("sort"), limit)
	if err != nil {
		fail(c, err.Error())
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, gin.H{"dishName": row.DishName, "quantity": row.Quantity, "amount": row.Amount})
	}
	ok(c, out)
}

// ReportHourly 区间内「按时段」的经营分布,用于排班与备货。
func ReportHourly(c *gin.Context) {
	items, err := service.Hourly(c.Query("start"), c.Query("end"), c.Query("days"))
	if err != nil {
		fail(c, err.Error())
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"hour":       it.Hour,
			"amount":     it.Amount,
			"orderCount": it.OrderCount,
			"guestCount": it.GuestCount,
		})
	}
	ok(c, out)
}

// ReportSettleMix 区间内「结算方式构成」。
func ReportSettleMix(c *gin.Context) {
	items, err := service.SettleMix(c.Query("start"), c.Query("end"), c.Query("days"))
	if err != nil {
		fail(c, err.Error())
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"settleType": it.SettleType,
			"label":      it.Label,
			"amount":     it.Amount,
			"paidAmount": it.PaidAmount,
			"orderCount": it.OrderCount,
		})
	}
	ok(c, out)
}
