package handler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 报表 ============
//
// 营收口径:金额类指标统计「实收金额」(paid_amount),且只统计已支付(pay_status=1)、
// 未取消(order_status!=5)的订单。采用实收而非应收,是为了让免单/挂账不虚增营收:
//   - 免单:实收 0,不计营业额,单列为让利额;
//   - 挂账:核销前实收 0,不计营业额,单列在「挂账待收」;核销后按实收计入下单当日营业额;
//   - 退款:退款成功时同步扣减实收金额。
// 订单数/客流量仍按「未取消」统计,反映真实经营笔数。
//
// 时间区间统一采用左闭右开 [start, end),避免「当日」与「次日」在零点边界上重复计数。

const (
	// timeLayout 与 store.Now() 写入的格式严格一致(tb_order 的时间列是字符串)。
	timeLayout = "2006-01-02 15:04:05"
	// dayLayout 报表入参 / 出参使用的日期格式。
	dayLayout = "2006-01-02"
	// maxRangeDays 单次查询允许的最大天数,防止一个手写参数把整张订单表逐日扫穿。
	maxRangeDays = 366
)

// rangeAmount 区间实收金额(分):未取消 + 已支付。
func rangeAmount(start, end string) int64 {
	var v int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND pay_status=1`, start, end).Scan(&v)
	return v
}

// rangeOrderCount 区间订单数:未取消(含未支付)。
func rangeOrderCount(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v
}

// rangeGuestCount 区间客流:未取消订单的人数合计。
func rangeGuestCount(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COALESCE(SUM(person_count),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v
}

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
func resolveRange(c *gin.Context, defaultDays, maxDays int) (start, end time.Time) {
	if defaultDays < 1 {
		defaultDays = 1
	}
	if maxDays < 1 {
		maxDays = 1
	}
	today := dayStart(time.Now())
	start, end, custom := today.AddDate(0, 0, -(defaultDays-1)), today, false
	if s, ok := parseDay(c.Query("start")); ok {
		start, custom = s, true
	}
	if e, ok := parseDay(c.Query("end")); ok {
		end, custom = e, true
	}
	if !custom {
		days := defaultDays
		if v := c.Query("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
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
	return model.Round2(model.ToYuan(amountCents) / float64(count))
}

func ReportSummary(c *gin.Context) {
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

	f := func(t time.Time) string { return t.Format(timeLayout) }
	todayStart, tomorrowStart := f(today), f(tomorrow)

	// ---- 今日 ----
	todayAmountCents := rangeAmount(todayStart, tomorrowStart)
	todayOrderCount := rangeOrderCount(todayStart, tomorrowStart)
	todayGuestCount := rangeGuestCount(todayStart, tomorrowStart)

	// ---- 昨日(环比基准) ----
	yesterdayAmountCents := rangeAmount(f(yesterday), todayStart)
	yesterdayOrderCount := rangeOrderCount(f(yesterday), todayStart)
	yesterdayGuestCount := rangeGuestCount(f(yesterday), todayStart)

	// ---- 本月 / 上月同期 ----
	monthAmountCents := rangeAmount(f(monthStart), f(nextMonth))
	monthOrderCount := rangeOrderCount(f(monthStart), f(nextMonth))
	monthGuestCount := rangeGuestCount(f(monthStart), f(nextMonth))
	lastMonthAmountCents := rangeAmount(f(lastMonthStart), f(lastMonthSameEnd))
	lastMonthOrderCount := rangeOrderCount(f(lastMonthStart), f(lastMonthSameEnd))

	// ---- 今日异常与风险 ----
	// 已支付订单通常已计入「完成」,这里按 finish_time 统计便于和营业额口径区分。
	var todayFinishedCount int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE finish_time>=? AND finish_time<? AND order_status=4`,
		todayStart, tomorrowStart).Scan(&todayFinishedCount)
	var todayCancelCount int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE create_time>=? AND create_time<? AND order_status=5`,
		todayStart, tomorrowStart).Scan(&todayCancelCount)
	// 退款按「退款时间」落库,与下单日无关,因此单独用 refund_time 统计。
	var todayRefundCents int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(refund_amount),0) FROM tb_order WHERE refund_time>=? AND refund_time<?`,
		todayStart, tomorrowStart).Scan(&todayRefundCents)

	// ---- 桌台 / 在途订单 ----
	var tableCount, freeTableCount, activeOrderCount int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0'`).Scan(&tableCount)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0' AND status=0`).Scan(&freeTableCount)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE order_status IN (1,2,3)`).Scan(&activeOrderCount)

	// ---- 今日免单(让利额) ----
	var todayFreeCents int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='free'`,
		todayStart, tomorrowStart).Scan(&todayFreeCents)

	// ---- 挂账 ----
	var creditPendingCents int64
	var creditPendingCount int
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE order_status!=5 AND settle_type='credit' AND credit_status=1`).
		Scan(&creditPendingCents, &creditPendingCount)
	var todayCreditCents int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='credit'`,
		todayStart, tomorrowStart).Scan(&todayCreditCents)
	var todayCreditSettledCents int64
	var todayCreditSettledCount int
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE credit_settle_time>=? AND credit_settle_time<? AND credit_status=2`,
		todayStart, tomorrowStart).Scan(&todayCreditSettledCents, &todayCreditSettledCount)

	todayAmount := model.ToYuan(todayAmountCents)
	todayAvg := 0.0
	if todayOrderCount > 0 {
		todayAvg = todayAmount / float64(todayOrderCount)
	}

	ok(c, gin.H{
		"todayAmount":        model.Round2(todayAmount),
		"todayOrderCount":    todayOrderCount,
		"todayFinishedCount": todayFinishedCount,
		"todayGuestCount":    todayGuestCount,
		"todayAvgAmount":     model.Round2(todayAvg),
		"todayCancelCount":   todayCancelCount,
		"todayRefundAmount":  model.Round2(model.ToYuan(todayRefundCents)),
		"monthAmount":        model.Round2(model.ToYuan(monthAmountCents)),
		"monthOrderCount":    monthOrderCount,
		"monthGuestCount":    monthGuestCount,
		"monthAvgAmount":     roundAvg(monthAmountCents, monthOrderCount),
		"freeTableCount":     freeTableCount,
		"tableCount":         tableCount,
		"activeOrderCount":   activeOrderCount,
		// 环比基准:后端只给绝对值,涨跌幅与方向由前端统一渲染
		"yesterdayAmount":               model.Round2(model.ToYuan(yesterdayAmountCents)),
		"yesterdayOrderCount":           yesterdayOrderCount,
		"yesterdayGuestCount":           yesterdayGuestCount,
		"lastMonthSamePeriodAmount":     model.Round2(model.ToYuan(lastMonthAmountCents)),
		"lastMonthSamePeriodOrderCount": lastMonthOrderCount,
		// 免单 / 挂账
		"todayFreeAmount":          model.Round2(model.ToYuan(todayFreeCents)),
		"creditPendingAmount":      model.Round2(model.ToYuan(creditPendingCents)),
		"creditPendingCount":       creditPendingCount,
		"todayCreditAmount":        model.Round2(model.ToYuan(todayCreditCents)),
		"todayCreditSettledAmount": model.Round2(model.ToYuan(todayCreditSettledCents)),
		"todayCreditSettledCount":  todayCreditSettledCount,
	})
}

// ReportDailyTrend 区间内逐日营业额/订单/客流,用于折线趋势。
func ReportDailyTrend(c *gin.Context) {
	start, end := resolveRange(c, 7, maxRangeDays)
	// 跨年区间用完整日期当标签,否则两个「01-02」在 x 轴上无法区分。
	crossYear := start.Year() != end.Year()

	out := []gin.H{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		next := d.AddDate(0, 0, 1)
		s, e := d.Format(timeLayout), next.Format(timeLayout)
		label := d.Format("01-02")
		if crossYear {
			label = d.Format(dayLayout)
		}
		out = append(out, gin.H{
			"date":       label,
			"amount":     model.Round2(model.ToYuan(rangeAmount(s, e))),
			"orderCount": rangeOrderCount(s, e),
			"guestCount": rangeGuestCount(s, e),
		})
	}
	ok(c, out)
}

func ReportMonthlyTrend(c *gin.Context) {
	nowT := time.Now()
	thisMonth := time.Date(nowT.Year(), nowT.Month(), 1, 0, 0, 0, 0, nowT.Location())
	out := []gin.H{}
	for i := 11; i >= 0; i-- {
		m := thisMonth.AddDate(0, -i, 0)
		next := m.AddDate(0, 1, 0)
		s, e := m.Format(timeLayout), next.Format(timeLayout)
		out = append(out, gin.H{
			"month":      fmt.Sprintf("%d月", int(m.Month())),
			"amount":     model.Round2(model.ToYuan(rangeAmount(s, e))),
			"orderCount": rangeOrderCount(s, e),
			"guestCount": rangeGuestCount(s, e),
		})
	}
	ok(c, out)
}

// ReportDishRank 菜品排行。
//
// sort=qty(默认,按销量) / sort=amount(按销售额)。
// 金额排行能看出「卖得贵且卖得动」的菜,与销量排行是两个不同的决策视角
// (销量榜常被低价的引流菜霸占)。
func ReportDishRank(c *gin.Context) {
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	// 排序表达式无法参数化,只允许在白名单列上排序,以此挡住注入。
	orderCol := "qty"
	if c.Query("sort") == "amount" {
		orderCol = "amt"
	}
	// 热销榜关联订单表,排除已取消订单,避免取消单计入销量。
	rows, err := store.DB.Query(`SELECT i.dish_name, SUM(i.quantity) AS qty, SUM(i.amount) AS amt
		FROM tb_order_item i JOIN tb_order o ON o.order_id = i.order_id
		WHERE o.order_status != 5
		GROUP BY i.dish_name ORDER BY `+orderCol+` DESC LIMIT ?`, limit)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var name string
		var qty int
		var amtCents int64
		rows.Scan(&name, &qty, &amtCents)
		out = append(out, gin.H{"dishName": name, "quantity": qty, "amount": model.Round2(model.ToYuan(amtCents))})
	}
	ok(c, out)
}

// ReportHourly 区间内「按时段」的经营分布,用于排班与备货。
//
// 时间列是 'YYYY-MM-DD HH:MM:SS' 字符串,substr(create_time,12,2) 正好截出小时位;
// 该函数在 SQLite 与 MySQL 下均为 1-based,语义一致,无需按方言分支。
// 空/异常时间串会落进空字符串分组,直接被下表丢弃,不会污染 0 点数据。
func ReportHourly(c *gin.Context) {
	start, end := resolveRange(c, 7, 90)
	rows, err := store.DB.Query(`SELECT substr(create_time,12,2) AS h,
			COALESCE(SUM(CASE WHEN pay_status=1 THEN COALESCE(paid_amount,0) ELSE 0 END),0) AS amt,
			COUNT(*) AS cnt,
			COALESCE(SUM(person_count),0) AS guests
		FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5
		GROUP BY h`, start.Format(timeLayout), end.AddDate(0, 0, 1).Format(timeLayout))
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()

	type bucket struct {
		amt    int64
		cnt    int
		guests int
	}
	byHour := make(map[string]bucket, 24)
	for rows.Next() {
		var h string
		var b bucket
		rows.Scan(&h, &b.amt, &b.cnt, &b.guests)
		byHour[h] = b
	}
	// 固定输出 0~23 点、缺失时段补零,前端无需再对齐时间轴。
	out := make([]gin.H, 0, 24)
	for h := 0; h < 24; h++ {
		key := fmt.Sprintf("%02d", h)
		b := byHour[key]
		out = append(out, gin.H{
			"hour":       key,
			"amount":     model.Round2(model.ToYuan(b.amt)),
			"orderCount": b.cnt,
			"guestCount": b.guests,
		})
	}
	ok(c, out)
}

// ReportSettleMix 区间内「结算方式构成」。
//
// 金额口径为订单应收金额(total_amount)而不是实收 —— 否则免单组恒为 0,
// 就看不到「让利了多少」。同时附上每组实收(paidAmount)供对照。
func ReportSettleMix(c *gin.Context) {
	start, end := resolveRange(c, 30, maxRangeDays)
	rows, err := store.DB.Query(`SELECT COALESCE(NULLIF(settle_type,''),'normal') AS st,
			COALESCE(SUM(total_amount),0) AS amt,
			COALESCE(SUM(CASE WHEN pay_status=1 THEN COALESCE(paid_amount,0) ELSE 0 END),0) AS paid,
			COUNT(*) AS cnt
		FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5
		GROUP BY st`, start.Format(timeLayout), end.AddDate(0, 0, 1).Format(timeLayout))
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()

	type mix struct {
		amt  int64
		paid int64
		cnt  int
	}
	known := map[string]string{"normal": "正常收款", "free": "免单", "credit": "挂账"}
	acc := map[string]mix{}
	for rows.Next() {
		var st string
		var m mix
		rows.Scan(&st, &m.amt, &m.paid, &m.cnt)
		if _, ok := known[st]; !ok {
			st = "other" // 未知 / 历史脏值统一归入「其他」,不把原始值外泄到前端
		}
		cur := acc[st]
		cur.amt += m.amt
		cur.paid += m.paid
		cur.cnt += m.cnt
		acc[st] = cur
	}

	out := []gin.H{}
	for _, key := range []string{"normal", "free", "credit", "other"} {
		m, ok := acc[key]
		if !ok && key == "other" {
			continue // 没有「其他」时不占图例
		}
		label := known[key]
		if label == "" {
			label = "其他"
		}
		out = append(out, gin.H{
			"settleType": key,
			"label":      label,
			"amount":     model.Round2(model.ToYuan(m.amt)),
			"paidAmount": model.Round2(model.ToYuan(m.paid)),
			"orderCount": m.cnt,
		})
	}
	ok(c, out)
}
