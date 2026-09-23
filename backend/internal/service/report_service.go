package service

import (
	"fmt"
	"strconv"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// ============ 报表 ============
//
// 营收口径:金额类指标统计「实收资金流」,按钱到账的时间归属(dao.RangeAmount):
//   - 正常收款按 pay_time、挂账核销按 credit_settle_time 归属,退款按退款到账时间扣减;
//   - 一天的数字落定后不再漂移 —— 历史订单的退款不会追溯改写往日营业额,
//     且「当日营业额 - 当日退款」与渠道账单/钱箱能直接对账;
//   - 免单实收 0 不计入营业额,单列为让利额;挂账在核销回款当日计入营业额。
//
// 订单数/客流量按「下单时间(create_time)」统计,反映经营流量,与资金流口径互补。
//
// 时间区间统一采用左闭右开 [start, end),避免「当日」与「次日」在零点边界上重复计数。

const (
	// dayLayout 报表入参 / 出参使用的日期格式。
	dayLayout = "2006-01-02"
	// maxRangeDays 单次查询允许的最大天数,防止一个手写参数把整张订单表逐日扫穿。
	maxRangeDays = 366
)

// dayStart 返回某天的零点(本地时区),用于构造 [start, end) 区间边界。
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// parseDay 解析 YYYY-MM-DD(本地时区)。
func parseDay(s string) (time.Time, bool) {
	t, err := time.ParseInLocation(dayLayout, s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// resolveRange 统一解析「报表区间」参数,返回闭区间 [start, end] 的两个自然日。
//
// 两种传参方式(同时出现时 start/end 优先):
//   - start=YYYY-MM-DD&end=YYYY-MM-DD —— 自定义区间(含首尾);
//   - days=N —— 截至今天的近 N 天。
//
// 非法值一律回退默认,越界则夹到 maxDays,保证任何 query 都构造不出「全表扫描」。
func resolveRange(startStr, endStr, daysStr string, defaultDays, maxDays int) (start, end time.Time) {
	if defaultDays < 1 {
		defaultDays = 1
	}
	if maxDays < 1 {
		maxDays = 1
	}
	today := dayStart(time.Now())
	start, end, custom := today.AddDate(0, 0, -(defaultDays-1)), today, false
	if s, ok := parseDay(startStr); ok {
		start, custom = s, true
	}
	if e, ok := parseDay(endStr); ok {
		end, custom = e, true
	}
	if !custom {
		days := defaultDays
		if daysStr != "" {
			if n, err := strconv.Atoi(daysStr); err == nil && n > 0 {
				days = n
			}
		}
		if days > maxDays {
			days = maxDays
		}
		start, end = today.AddDate(0, 0, -(days-1)), today
	}
	if end.Before(start) {
		start, end = end, start
	}
	if end.Sub(start) > time.Duration(maxDays-1)*24*time.Hour {
		start = end.AddDate(0, 0, -(maxDays - 1))
	}
	return start, end
}

// roundAvg 由「金额(分) / 笔数」算客单价(元),笔数为 0 时返回 0。
func roundAvg(amountCents int64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return po.Round2(po.ToYuan(amountCents) / float64(count))
}

// Summary 报表汇总的用例级出参,金额均为元(已四舍五入到分)。
type Summary struct {
	TodayAmount                   float64
	TodayOrderCount               int
	TodayFinishedCount            int
	TodayGuestCount               int
	TodayAvgAmount                float64
	TodayCancelCount              int
	TodayRefundAmount             float64
	MonthAmount                   float64
	MonthOrderCount               int
	MonthGuestCount               int
	MonthAvgAmount                float64
	FreeTableCount                int
	TableCount                    int
	ActiveOrderCount              int
	YesterdayAmount               float64
	YesterdayOrderCount           int
	YesterdayGuestCount           int
	LastMonthSamePeriodAmount     float64
	LastMonthSamePeriodOrderCount int
	TodayFreeAmount               float64
	CreditPendingAmount           float64
	CreditPendingCount            int
	TodayCreditAmount             float64
	TodayCreditSettledAmount      float64
	TodayCreditSettledCount       int
}

// ReportSummary 汇总今日/昨日/本月/上月同期的营业额、订单量、客流量,
// 以及今日异常、桌台在途、免单与挂账等经营指标。
func ReportSummary() Summary {
	nowT := time.Now()
	today := dayStart(nowT)
	tomorrow := today.AddDate(0, 0, 1)
	yesterday := today.AddDate(0, 0, -1)
	monthStart := time.Date(nowT.Year(), nowT.Month(), 1, 0, 0, 0, 0, nowT.Location())
	nextMonth := monthStart.AddDate(0, 1, 0)
	// 上月同期:上月 1 号 ~ 本月「同一天」。
	// 拿「本月至今」去比「上月整月」永远显示负增长,没有参考价值,所以取同期口径;
	// 上月天数不足时(如 3/31 对上 2 月)截到本月 1 号,避免溢出到本月。
	lastMonthStart := monthStart.AddDate(0, -1, 0)
	lastMonthSameEnd := lastMonthStart.AddDate(0, 0, nowT.Day())
	if lastMonthSameEnd.After(monthStart) {
		lastMonthSameEnd = monthStart
	}

	f := func(t time.Time) string { return t.Format(conf.TimeLayout) }
	todayStart, tomorrowStart := f(today), f(tomorrow)

	// ---- 今日 ----
	todayAmountCents := dao.RangeAmount(todayStart, tomorrowStart)
	todayOrderCount := dao.RangeOrderCount(todayStart, tomorrowStart)
	todayGuestCount := dao.RangeGuestCount(todayStart, tomorrowStart)

	// ---- 昨日(环比基准) ----
	yesterdayAmountCents := dao.RangeAmount(f(yesterday), todayStart)
	yesterdayOrderCount := dao.RangeOrderCount(f(yesterday), todayStart)
	yesterdayGuestCount := dao.RangeGuestCount(f(yesterday), todayStart)

	// ---- 本月 / 上月同期 ----
	monthAmountCents := dao.RangeAmount(f(monthStart), f(nextMonth))
	monthOrderCount := dao.RangeOrderCount(f(monthStart), f(nextMonth))
	monthGuestCount := dao.RangeGuestCount(f(monthStart), f(nextMonth))
	lastMonthAmountCents := dao.RangeAmount(f(lastMonthStart), f(lastMonthSameEnd))
	lastMonthOrderCount := dao.RangeOrderCount(f(lastMonthStart), f(lastMonthSameEnd))

	// ---- 今日异常与风险 ----
	// 已支付订单通常已计入「完成」,这里按 finish_time 统计便于和营业额口径区分。
	todayFinishedCount := dao.CountFinishedIn(todayStart, tomorrowStart)
	todayCancelCount := dao.CountCanceledIn(todayStart, tomorrowStart)
	// 退款按流水逐笔统计(资金流口径,见 dao.SumRefundIn 注释)。
	todayRefundCents := dao.SumRefundIn(todayStart, tomorrowStart)

	// ---- 桌台 / 在途订单 ----
	tableCount := dao.TableCount()
	freeTableCount := dao.FreeTableCount()
	activeOrderCount := dao.ActiveOrderCount()

	// ---- 今日免单(让利额) ----
	todayFreeCents := dao.SumFreeIn(todayStart, tomorrowStart)

	// ---- 挂账 ----
	creditPendingCents, creditPendingCount := dao.CreditPendingStats()
	todayCreditCents := dao.SumCreditIn(todayStart, tomorrowStart)
	todayCreditSettledCents, todayCreditSettledCount := dao.CreditSettledIn(todayStart, tomorrowStart)

	todayAmount := po.ToYuan(todayAmountCents)
	todayAvg := 0.0
	if todayOrderCount > 0 {
		todayAvg = todayAmount / float64(todayOrderCount)
	}

	return Summary{
		TodayAmount:                   po.Round2(todayAmount),
		TodayOrderCount:               todayOrderCount,
		TodayFinishedCount:            todayFinishedCount,
		TodayGuestCount:               todayGuestCount,
		TodayAvgAmount:                po.Round2(todayAvg),
		TodayCancelCount:              todayCancelCount,
		TodayRefundAmount:             po.Round2(po.ToYuan(todayRefundCents)),
		MonthAmount:                   po.Round2(po.ToYuan(monthAmountCents)),
		MonthOrderCount:               monthOrderCount,
		MonthGuestCount:               monthGuestCount,
		MonthAvgAmount:                roundAvg(monthAmountCents, monthOrderCount),
		FreeTableCount:                freeTableCount,
		TableCount:                    tableCount,
		ActiveOrderCount:              activeOrderCount,
		YesterdayAmount:               po.Round2(po.ToYuan(yesterdayAmountCents)),
		YesterdayOrderCount:           yesterdayOrderCount,
		YesterdayGuestCount:           yesterdayGuestCount,
		LastMonthSamePeriodAmount:     po.Round2(po.ToYuan(lastMonthAmountCents)),
		LastMonthSamePeriodOrderCount: lastMonthOrderCount,
		TodayFreeAmount:               po.Round2(po.ToYuan(todayFreeCents)),
		CreditPendingAmount:           po.Round2(po.ToYuan(creditPendingCents)),
		CreditPendingCount:            creditPendingCount,
		TodayCreditAmount:             po.Round2(po.ToYuan(todayCreditCents)),
		TodayCreditSettledAmount:      po.Round2(po.ToYuan(todayCreditSettledCents)),
		TodayCreditSettledCount:       todayCreditSettledCount,
	}
}

// DailyTrendItem 区间内某一天的营业额/订单/客流。
type DailyTrendItem struct {
	Date       string  // 折线图 x 轴标签(跨年时为完整日期,否则 MM-DD)
	Amount     float64 // 营业额(元)
	OrderCount int
	GuestCount int
}

// DailyTrend 区间内逐日营业额/订单/客流,用于折线趋势。
//
// 聚合改为一次性按日分组的 SQL(见 dao.DailyTrendStats):旧实现逐日调用
// RangeAmount / RangeOrderCount / RangeGuestCount,最长 366 天会发起 366×3 次查询;
// 现在固定 2 条 GROUP BY 查询完成聚合,缺失日期由下面的循环补零。
func DailyTrend(startStr, endStr, daysStr string) []DailyTrendItem {
	start, end := resolveRange(startStr, endStr, daysStr, 7, maxRangeDays)
	// 跨年区间用完整日期当标签,否则两个「01-02」在 x 轴上无法区分。
	crossYear := start.Year() != end.Year()

	// DailyTrendStats 的区间是左闭右开 [start, end);resolveRange 返回的是闭区间
	// [start, end],故 end 需 +1 天把「最后一天」也纳入统计。
	stats, err := dao.DailyTrendStats(start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout))
	if err != nil {
		// 统计失败返回空序列,由前端按无数据展示,不再伪造一条全 0 曲线。
		return nil
	}

	out := []DailyTrendItem{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		label := d.Format("01-02")
		if crossYear {
			label = d.Format(dayLayout)
		}
		b := stats[d.Format(dayLayout)]
		out = append(out, DailyTrendItem{
			Date:       label,
			Amount:     po.Round2(po.ToYuan(b.AmtCents)),
			OrderCount: b.OrderCount,
			GuestCount: b.Guests,
		})
	}
	return out
}

// MonthlyTrendItem 最近 12 个月中某个月的营业额/订单/客流。
type MonthlyTrendItem struct {
	Month      string  // 展示标签,如「1月」
	Amount     float64 // 营业额(元)
	OrderCount int
	GuestCount int
}

// MonthlyTrend 返回最近 12 个月(含本月)逐月营业额/订单/客流。
func MonthlyTrend() []MonthlyTrendItem {
	nowT := time.Now()
	thisMonth := time.Date(nowT.Year(), nowT.Month(), 1, 0, 0, 0, 0, nowT.Location())
	out := []MonthlyTrendItem{}
	for i := 11; i >= 0; i-- {
		m := thisMonth.AddDate(0, -i, 0)
		next := m.AddDate(0, 1, 0)
		s, e := m.Format(conf.TimeLayout), next.Format(conf.TimeLayout)
		out = append(out, MonthlyTrendItem{
			Month:      fmt.Sprintf("%d月", int(m.Month())),
			Amount:     po.Round2(po.ToYuan(dao.RangeAmount(s, e))),
			OrderCount: dao.RangeOrderCount(s, e),
			GuestCount: dao.RangeGuestCount(s, e),
		})
	}
	return out
}

// DishRankItem 菜品排行的一行结果。
type DishRankItem struct {
	DishName string
	Quantity int
	Amount   float64 // 销售额(元)
}

// DishRank 菜品排行:sort=qty(默认,按销量) / sort=amount(按销售额)。
//
// 金额排行能看出「卖得贵且卖得动」的菜,与销量排行是两个不同的决策视角
// (销量榜常被低价的引流菜霸占)。排序表达式只允许白名单列,由 dao 再次兜底防注入。
func DishRank(sort string, limit int) ([]DishRankItem, error) {
	orderCol := "qty"
	if sort == "amount" {
		orderCol = "amt"
	}
	rows, err := dao.DishRank(orderCol, limit)
	if err != nil {
		return nil, err
	}
	out := make([]DishRankItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, DishRankItem{
			DishName: row.DishName,
			Quantity: row.Quantity,
			Amount:   po.Round2(po.ToYuan(row.AmountCents)),
		})
	}
	return out, nil
}

// HourlyItem 某一小时的经营分布。
type HourlyItem struct {
	Hour       string  // 小时标签(00-23)
	Amount     float64 // 营业额(元)
	OrderCount int
	GuestCount int
}

// Hourly 区间内「按时段」的经营分布,固定输出 0~23 点、缺失时段补零。
func Hourly(startStr, endStr, daysStr string) ([]HourlyItem, error) {
	start, end := resolveRange(startStr, endStr, daysStr, 7, 90)
	byHour, err := dao.HourlyStats(start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout))
	if err != nil {
		return nil, err
	}

	out := make([]HourlyItem, 0, 24)
	for h := 0; h < 24; h++ {
		key := fmt.Sprintf("%02d", h)
		b := byHour[key]
		out = append(out, HourlyItem{
			Hour:       key,
			Amount:     po.Round2(po.ToYuan(b.AmtCents)),
			OrderCount: b.Count,
			GuestCount: b.Guests,
		})
	}
	return out, nil
}

// SettleMixItem 区间内某一种结算方式的构成。
type SettleMixItem struct {
	SettleType string
	Label      string
	Amount     float64 // 应收金额(元)
	PaidAmount float64 // 实收金额(元)
	OrderCount int
}

// SettleMix 区间内「结算方式构成」。
//
// 金额口径为订单应收金额(total_amount)而不是实收 —— 否则免单组恒为 0,
// 就看不到「让利了多少」。同时附上每组实收(paidAmount)供对照。
func SettleMix(startStr, endStr, daysStr string) ([]SettleMixItem, error) {
	start, end := resolveRange(startStr, endStr, daysStr, 30, maxRangeDays)
	rows, err := dao.SettleMixStats(start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout))
	if err != nil {
		return nil, err
	}

	type mix struct {
		amt  int64
		paid int64
		cnt  int
	}
	known := map[string]string{"normal": "正常收款", "free": "免单", "credit": "挂账"}
	acc := map[string]mix{}
	for _, row := range rows {
		st := row.SettleType
		if _, ok := known[st]; !ok {
			st = "other" // 未知 / 历史脏值统一归入「其他」,不把原始值外泄到前端
		}
		cur := acc[st]
		cur.amt += row.AmountCents
		cur.paid += row.PaidCents
		cur.cnt += row.Count
		acc[st] = cur
	}

	out := []SettleMixItem{}
	for _, key := range []string{"normal", "free", "credit", "other"} {
		m, ok := acc[key]
		if !ok && key == "other" {
			continue // 没有「其他」时不占图例
		}
		label := known[key]
		if label == "" {
			label = "其他"
		}
		out = append(out, SettleMixItem{
			SettleType: key,
			Label:      label,
			Amount:     po.Round2(po.ToYuan(m.amt)),
			PaidAmount: po.Round2(po.ToYuan(m.paid)),
			OrderCount: m.cnt,
		})
	}
	return out, nil
}
