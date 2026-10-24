package service

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store/dao"
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
// ⚠️ 双口径并存是**有意的设计**(不是疏漏),读报表时须按指标分别归属:
//   - 资金流(钱到账时间):营业额、储值核销、退款、挂账核销 —— pay_time /
//     credit_settle_time / tb_refund.update_time;
//   - 流量(下单时间)：订单数、客流、免单让利、赠送让利、挂账产生 —— create_time。
// 跨日订单(如 23:50 下单、00:10 收款)会让「今日营业额」与「今日免单让利」
// 不是同一批单。老板对账时以资金流指标为准,流量指标看趋势即可。
//
// 时间区间统一采用左闭右开 [start, end),避免「当日」与「次日」在零点边界上重复计数。

const (
	// dayLayout 报表入参 / 出参使用的日期格式。
	dayLayout = "2006-01-02"
	// maxRangeDays 单次查询允许的最大天数,防止一个手写参数把整张订单表逐日扫穿。
	maxRangeDays = 366
)

// dayStart 返回某天的零点(本地时区)。
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// bizDayStartOf 返回「营业日 d 的起点」:d 当天 00:00 + 营业日切换小时(cutoff)。
//
// cutoff=0(默认)时即自然日零点,与历史口径完全一致;cutoff=4 时,
// 10-13 的营业日是 [10-13 04:00, 10-14 04:00),凌晨 2 点的流水归 10-13。
// 与订单 biz_date、跨日清台共用同一套折算(见 stale_service.BizDateOf)。
func bizDayStartOf(d time.Time, cutoffHour int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), cutoffHour, 0, 0, 0, d.Location())
}

// currentBizDayStart 返回当前营业日的起点(相对 now 与截止时刻)。
func currentBizDayStart(now time.Time, cutoffHour int) time.Time {
	return bizDayStartOf(now.Add(-time.Duration(cutoffHour)*time.Hour), cutoffHour)
}

// parseDay 解析 YYYY-MM-DD(本地时区)。
func parseDay(s string) (time.Time, bool) {
	t, err := time.ParseInLocation(dayLayout, s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// resolveRange 统一解析「报表区间」参数,返回闭区间 [start, end] 的两个**营业日**起点。
//
// 两种传参方式(同时出现时 start/end 优先):
//   - start=YYYY-MM-DD&end=YYYY-MM-DD —— 自定义区间(含首尾,按营业日理解);
//   - days=N —— 截至今天营业日的近 N 个营业日。
//
// start/end 都是「该营业日 00:00 + cutoff」的时刻,与报表汇总、日分桶同一口径
// (cutoff=0 时即自然日零点,与旧行为一致);调用方用 Format(conf.TimeLayout)
// 作为左闭右开区间边界,end 再加一天即为区间右端。
//
// 非法值一律回退默认,越界则夹到 maxDays,保证任何 query 都构造不出「全表扫描」。
//
// cutoffHour 由调用方显式传入(而不是函数内读配置):本函数是纯计算,
// 不该为了一个边界折算去碰数据库 —— 否则纯函数单测必须先建库。
func resolveRange(startStr, endStr, daysStr string, defaultDays, maxDays, cutoffHour int) (start, end time.Time) {
	if defaultDays < 1 {
		defaultDays = 1
	}
	if maxDays < 1 {
		maxDays = 1
	}
	cutoff := cutoffHour
	today := currentBizDayStart(time.Now(), cutoff)
	start, end, custom := today.AddDate(0, 0, -(defaultDays-1)), today, false
	startOK, endOK := false, false
	if s, ok := parseDay(startStr); ok {
		start, startOK, custom = bizDayStartOf(s, cutoff), true, true
	}
	if e, ok := parseDay(endStr); ok {
		end, endOK, custom = bizDayStartOf(e, cutoff), true, true
	}
	// 只传了一侧时,另一侧按 defaultDays 贴着它推导,而不是留在「相对今天」的
	// 默认值上 —— 否则 start=今天-29 配上一年前的 end,会交换成一个横跨整年的
	// 无意义区间(虽然仍受 maxDays 夹紧,但数字与调用方意图完全不符)。
	if startOK && !endOK {
		end = start.AddDate(0, 0, defaultDays-1)
	}
	if endOK && !startOK {
		start = end.AddDate(0, 0, -(defaultDays - 1))
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
	// MonthSameSpanAmount 本月与「上月同期」**等长**区间的营业额:环比必须用它。
	//
	// 月末会出现天数不对称:3/31 时上月只有 28 天,拿「本月 31 天(MonthAmount)」
	// 去比「上月 28 天」永远显示负增长,没有参考价值。本字段把本月侧也截到同样的
	// 天数(3/1~3/28),与 LastMonthSamePeriodAmount 构成可比的一对。
	MonthSameSpanAmount     float64
	MonthSameSpanOrderCount int
	TodayFreeAmount         float64
	TodayGiftAmount         float64
	// TodayWalletAmount 今日营业额中用储值余额核销的部分。
	//
	// 储值是「先收钱、后消费」:充值当天不计收入(那是负债),消费当天才确认收入,
	// 但那天并没有真实现金流入。把它单列出来,老板才看得到
	// 「今日营业额 8000 / 其中储值核销 3000 / 实际到账 5000」这个真实结构。
	TodayWalletAmount float64
	// TodayCashAmount 今日实际到账资金 = 今日营业额 - 储值核销。
	TodayCashAmount          float64
	CreditPendingAmount      float64
	CreditPendingCount       int
	TodayCreditAmount        float64
	TodayCreditSettledAmount float64
	TodayCreditSettledCount  int
}

// ReportSummary 汇总今日/昨日/本月/上月同期的营业额、订单量、客流量,
// 以及今日异常、桌台在途、免单与挂账等经营指标。
//
// 任一统计查询失败即返回 error,由 handler 应答失败 —— 不静默按 0 展示:
// DB 抖动时「今日营业额 0.00」会被店主当成真没开张,比报错更难排查。
func ReportSummary() (Summary, error) {
	nowT := time.Now()
	// 区间边界统一按「营业日」折算:cutoff=4 时,凌晨 0~4 点的流水属于前一营业日,
	// 与订单 biz_date、跨日清台、日趋势分桶保持同一口径。cutoff=0 时退化为自然日。
	cutoff := BizDayCutoffHour()
	today := currentBizDayStart(nowT, cutoff)
	tomorrow := today.AddDate(0, 0, 1)
	yesterday := today.AddDate(0, 0, -1)
	// 本营业月起点:以「营业日」所在月份为准 —— 月初凌晨仍属上一天(乃至上个月)。
	monthStart := bizDayStartOf(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()), cutoff)
	nextMonth := monthStart.AddDate(0, 1, 0)
	// 同期跨度:本月已过的天数与上月实际天数取小,避免 3/31 溢出到本月。
	lastMonthStart := monthStart.AddDate(0, -1, 0)
	spanDays := today.Day()
	if lastMonthDays := monthStart.AddDate(0, 0, -1).Day(); spanDays > lastMonthDays {
		spanDays = lastMonthDays
	}
	// 上月同期:上月 1 号起、与本月等长的 spanDays 天。
	lastMonthSameEnd := lastMonthStart.AddDate(0, 0, spanDays)
	// 本月同期:本月 1 号起同样 spanDays 天(用于与上月同期做公平环比)。
	monthSameSpanEnd := monthStart.AddDate(0, 0, spanDays)

	f := func(t time.Time) string { return t.Format(conf.TimeLayout) }
	todayStart, tomorrowStart := f(today), f(tomorrow)

	// ---- 今日 ----
	todayAmountCents, err := dao.RangeAmount(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	todayOrderCount, err := dao.RangeOrderCount(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	todayGuestCount, err := dao.RangeGuestCount(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}

	// ---- 昨日(环比基准) ----
	yesterdayAmountCents, err := dao.RangeAmount(f(yesterday), todayStart)
	if err != nil {
		return Summary{}, err
	}
	yesterdayOrderCount, err := dao.RangeOrderCount(f(yesterday), todayStart)
	if err != nil {
		return Summary{}, err
	}
	yesterdayGuestCount, err := dao.RangeGuestCount(f(yesterday), todayStart)
	if err != nil {
		return Summary{}, err
	}

	// ---- 本月 / 上月同期 ----
	monthAmountCents, err := dao.RangeAmount(f(monthStart), f(nextMonth))
	if err != nil {
		return Summary{}, err
	}
	monthOrderCount, err := dao.RangeOrderCount(f(monthStart), f(nextMonth))
	if err != nil {
		return Summary{}, err
	}
	monthGuestCount, err := dao.RangeGuestCount(f(monthStart), f(nextMonth))
	if err != nil {
		return Summary{}, err
	}
	lastMonthAmountCents, err := dao.RangeAmount(f(lastMonthStart), f(lastMonthSameEnd))
	if err != nil {
		return Summary{}, err
	}
	lastMonthOrderCount, err := dao.RangeOrderCount(f(lastMonthStart), f(lastMonthSameEnd))
	if err != nil {
		return Summary{}, err
	}
	// 本月同期:与上月同期**等长**的一段(月末会短于本月至今),专供环比使用。
	monthSameSpanAmountCents, err := dao.RangeAmount(f(monthStart), f(monthSameSpanEnd))
	if err != nil {
		return Summary{}, err
	}
	monthSameSpanOrderCount, err := dao.RangeOrderCount(f(monthStart), f(monthSameSpanEnd))
	if err != nil {
		return Summary{}, err
	}

	// ---- 今日异常与风险 ----
	// 已支付订单通常已计入「完成」,这里按 finish_time 统计便于和营业额口径区分。
	todayFinishedCount, err := dao.CountFinishedIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	todayCancelCount, err := dao.CountCanceledIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	// 退款按流水逐笔统计(资金流口径,见 dao.SumRefundIn 注释)。
	todayRefundCents, err := dao.SumRefundIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}

	// ---- 桌台 / 在途订单 ----
	tableCount, err := dao.TableCount()
	if err != nil {
		return Summary{}, err
	}
	freeTableCount, err := dao.FreeTableCount()
	if err != nil {
		return Summary{}, err
	}
	activeOrderCount, err := dao.ActiveOrderCount(StaleGuardBizDate())
	if err != nil {
		return Summary{}, err
	}

	// ---- 今日免单(让利额) ----
	todayFreeCents, err := dao.SumFreeIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}

	// ---- 今日赠送(让利额) ----
	todayGiftCents, err := dao.SumGiftIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}

	// ---- 今日储值核销(营业额中「以前收过的钱」) ----
	todayWalletCents, err := dao.RangeWalletConsume(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	if todayWalletCents > todayAmountCents {
		// 理论上是子集;真超了说明有历史脏数据,按营业额封顶,避免算出负的到账资金。
		todayWalletCents = todayAmountCents
	}

	// ---- 挂账 ----
	creditPendingCents, creditPendingCount, err := dao.CreditPendingStats()
	if err != nil {
		return Summary{}, err
	}
	todayCreditCents, err := dao.SumCreditIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}
	todayCreditSettledCents, todayCreditSettledCount, err := dao.CreditSettledIn(todayStart, tomorrowStart)
	if err != nil {
		return Summary{}, err
	}

	todayAmount := po.ToYuan(todayAmountCents)

	return Summary{
		TodayAmount:                   po.Round2(todayAmount),
		TodayOrderCount:               todayOrderCount,
		TodayFinishedCount:            todayFinishedCount,
		TodayGuestCount:               todayGuestCount,
		TodayAvgAmount:                roundAvg(todayAmountCents, todayOrderCount),
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
		MonthSameSpanAmount:           po.Round2(po.ToYuan(monthSameSpanAmountCents)),
		MonthSameSpanOrderCount:       monthSameSpanOrderCount,
		TodayFreeAmount:               po.Round2(po.ToYuan(todayFreeCents)),
		TodayGiftAmount:               po.Round2(po.ToYuan(todayGiftCents)),
		TodayWalletAmount:             po.Round2(po.ToYuan(todayWalletCents)),
		TodayCashAmount:               po.Round2(po.ToYuan(todayAmountCents - todayWalletCents)),
		CreditPendingAmount:           po.Round2(po.ToYuan(creditPendingCents)),
		CreditPendingCount:            creditPendingCount,
		TodayCreditAmount:             po.Round2(po.ToYuan(todayCreditCents)),
		TodayCreditSettledAmount:      po.Round2(po.ToYuan(todayCreditSettledCents)),
		TodayCreditSettledCount:       todayCreditSettledCount,
	}, nil
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
	start, end := resolveRange(startStr, endStr, daysStr, 7, maxRangeDays, BizDayCutoffHour())
	// 跨年区间用完整日期当标签,否则两个「01-02」在 x 轴上无法区分。
	crossYear := start.Year() != end.Year()

	// DailyTrendStats 的区间是左闭右开 [start, end);resolveRange 返回的是闭区间
	// [start, end],故 end 需 +1 天把「最后一天」也纳入统计。
	// 分桶键必须与区间边界使用同一个 cutoff:通宵门店凌晨的流水要落回前一个营业日。
	stats, err := dao.DailyTrendStats(start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout),
		BizDayCutoffHour())
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
func MonthlyTrend() ([]MonthlyTrendItem, error) {
	// 月边界同样按营业日起点:cutoff=4 时,11-01 凌晨 2 点仍属 10 月,不能记进 11 月。
	cutoff := BizDayCutoffHour()
	today := currentBizDayStart(time.Now(), cutoff)
	thisMonth := bizDayStartOf(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()), cutoff)
	out := []MonthlyTrendItem{}
	for i := 11; i >= 0; i-- {
		m := thisMonth.AddDate(0, -i, 0)
		next := m.AddDate(0, 1, 0)
		s, e := m.Format(conf.TimeLayout), next.Format(conf.TimeLayout)
		amtCents, err := dao.RangeAmount(s, e)
		if err != nil {
			return nil, err
		}
		orderCount, err := dao.RangeOrderCount(s, e)
		if err != nil {
			return nil, err
		}
		guestCount, err := dao.RangeGuestCount(s, e)
		if err != nil {
			return nil, err
		}
		out = append(out, MonthlyTrendItem{
			Month:      fmt.Sprintf("%d月", int(m.Month())),
			Amount:     po.Round2(po.ToYuan(amtCents)),
			OrderCount: orderCount,
			GuestCount: guestCount,
		})
	}
	return out, nil
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
//
// 区间口径(start/end/days)与日报一致,唯一区别是默认窗口直接取上限 maxRangeDays:
// 排行这一块刻意不跟随报表页顶部的日期区间(见 Report.vue loadRange 的注释),此前
// 因此干脆不带任何过滤 —— 明细 JOIN 订单主表后 GROUP BY 就是全历史扫描,订单量一
// 上来它就是整页最慢的一块。这里给个一年窗口兜底,把扫描范围从「全历史」收敛到
// 「近一年」,既消掉无界扫描,又不改变它不受日期控件影响的既有行为。
func DishRank(sort string, limit int, startStr, endStr, daysStr string) ([]DishRankItem, error) {
	orderCol := "qty"
	if sort == "amount" {
		orderCol = "amt"
	}
	start, end := resolveRange(startStr, endStr, daysStr, maxRangeDays, maxRangeDays, BizDayCutoffHour())
	// dao 侧是左闭右开 [start, end),故 end 需 +1 天把最后一天也算进来(同 DailyTrend)。
	rows, err := dao.DishRank(orderCol, limit, start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout))
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
	start, end := resolveRange(startStr, endStr, daysStr, 7, 90, BizDayCutoffHour())
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
	start, end := resolveRange(startStr, endStr, daysStr, 30, maxRangeDays, BizDayCutoffHour())
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

// ---- 用餐时长分析 ----
//
// 「这桌客人一般坐多久」是排台与翻台预估的依据。口径:已完成订单(order_status=4)
// 按 finish_time 归属区间,时长 = finish_time - create_time(下单到完成,含等餐,
// 与桌台页「已用餐」的实时口径同源;强制清台归档的单也计入,它确实占用了那么久)。

// diningBucketEdges 用餐时长分布的区间边界(分钟,左闭右开),最后一档为「及以上」。
var diningBucketEdges = []struct{ min, max int }{
	{0, 30}, {30, 60}, {60, 90}, {90, 120}, {120, -1},
}

// diningBucketLabels 与 diningBucketEdges 一一对应的展示文案。
var diningBucketLabels = []string{"30分钟内", "30-60分钟", "1-1.5小时", "1.5-2小时", "2小时以上"}

// DiningDurationBucket 用餐时长分布的一个区间。
type DiningDurationBucket struct {
	Label string
	Count int
}

// DiningDurationStats 区间内已完成订单的用餐时长分析。
type DiningDurationStats struct {
	OrderCount    int                    // 参与统计的已完成订单数
	AvgMinutes    int                    // 平均用餐时长(分钟)
	MedianMinutes int                    // 中位时长(分钟):比均值更抗「跑到天亮的挂账单」拉偏
	MaxMinutes    int                    // 最长一单(分钟)
	GuestCount    int                    // 就餐人次合计(人均时长参考)
	Buckets       []DiningDurationBucket // 时长分布(固定 5 档,缺失档为 0)
}

// DiningDuration 区间内「用户用餐时长」分析。区间口径与其他报表一致(start/end/days)。
func DiningDuration(startStr, endStr, daysStr string) (DiningDurationStats, error) {
	start, end := resolveRange(startStr, endStr, daysStr, 30, maxRangeDays, BizDayCutoffHour())
	rows, err := dao.DiningDurationRows(start.Format(conf.TimeLayout), end.AddDate(0, 0, 1).Format(conf.TimeLayout))
	if err != nil {
		return DiningDurationStats{}, err
	}

	buckets := make([]DiningDurationBucket, len(diningBucketLabels))
	for i, label := range diningBucketLabels {
		buckets[i].Label = label
	}
	minutes := make([]int, 0, len(rows))
	sum, maxM, guests := 0, 0, 0
	for _, r := range rows {
		startAt, e1 := time.ParseInLocation(conf.TimeLayout, r.MealStartTime, time.Local)
		endAt, e2 := time.ParseInLocation(conf.TimeLayout, r.MealEndTime, time.Local)
		if e1 != nil || e2 != nil {
			continue // 时间字段异常的脏数据不参与统计,但也不让整个接口失败
		}
		m := int(endAt.Sub(startAt).Minutes())
		if m < 0 {
			continue // 用餐结束早于开始的历史脏数据,统计进去只会污染均值
		}
		if m > maxM {
			maxM = m
		}
		sum += m
		guests += r.PersonCount
		minutes = append(minutes, m)
		for i, b := range diningBucketEdges {
			// 最后一档 max=-1 表示「不设上限」;其余左闭右开,60 分钟归「30-60」而非「1-1.5小时」。
			if m >= b.min && (b.max < 0 || m < b.max) {
				buckets[i].Count++
				break
			}
		}
	}

	stats := DiningDurationStats{
		OrderCount: len(minutes),
		MaxMinutes: maxM,
		GuestCount: guests,
		Buckets:    buckets,
	}
	if len(minutes) > 0 {
		stats.AvgMinutes = sum / len(minutes)
		sort.Ints(minutes)
		mid := len(minutes) / 2
		if len(minutes)%2 == 1 {
			stats.MedianMinutes = minutes[mid]
		} else {
			stats.MedianMinutes = (minutes[mid-1] + minutes[mid]) / 2
		}
	}
	return stats, nil
}
