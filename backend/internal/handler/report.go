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

// 营收口径:金额类指标统计「实收金额」(paid_amount),且只统计已支付(pay_status=1)、未取消(order_status!=5)的订单。
// 采用实收而非应收,是为了让免单/挂账不虚增营收:
//   - 免单:实收 0,不计营业额,单列为让利额;
//   - 挂账:核销前实收 0,不计营业额,单列在「挂账待收」;核销后按实收计入下单当日营业额;
//   - 退款:退款成功时同步扣减实收金额。
// 订单数/客流量仍按「未取消」统计,反映真实经营笔数。

func ReportSummary(c *gin.Context) {
	nowT := time.Now()
	todayStart := nowT.Format("2006-01-02") + " 00:00:00"
	monthStart := nowT.Format("2006-01") + "-01 00:00:00"

	var todayAmountCents, monthAmountCents int64
	var todayOrderCount, todayFinishedCount, todayGuestCount, monthOrderCount, freeTableCount, activeOrderCount int

	// 今日营收:实收金额(免单/未核销挂账为 0,退款已扣减)
	store.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order WHERE create_time>=? AND order_status!=5 AND pay_status=1`, todayStart).
		Scan(&todayAmountCents)
	// 今日订单数:未取消(含未支付)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE create_time>=? AND order_status!=5`, todayStart).Scan(&todayOrderCount)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE finish_time>=? AND order_status=4`, todayStart).Scan(&todayFinishedCount)
	store.DB.QueryRow(`SELECT COALESCE(SUM(person_count),0) FROM tb_order WHERE create_time>=? AND order_status!=5`, todayStart).Scan(&todayGuestCount)
	// 本月营收:实收金额
	store.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order WHERE create_time>=? AND order_status!=5 AND pay_status=1`, monthStart).
		Scan(&monthAmountCents)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE create_time>=? AND order_status!=5`, monthStart).Scan(&monthOrderCount)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0' AND status=0`).Scan(&freeTableCount)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE order_status IN (1,2,3)`).Scan(&activeOrderCount)

	// 今日免单金额(让利额)
	var todayFreeCents int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order
		WHERE create_time>=? AND order_status!=5 AND settle_type='free'`, todayStart).Scan(&todayFreeCents)
	// 挂账待收:全部未核销的挂账订单
	var creditPendingCents int64
	var creditPendingCount int
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE order_status!=5 AND settle_type='credit' AND credit_status=1`).
		Scan(&creditPendingCents, &creditPendingCount)
	// 今日新增挂账
	var todayCreditCents int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0) FROM tb_order
		WHERE create_time>=? AND order_status!=5 AND settle_type='credit'`, todayStart).Scan(&todayCreditCents)
	// 今日挂账回款(核销)
	var todayCreditSettledCents int64
	var todayCreditSettledCount int
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE credit_settle_time>=? AND credit_status=2`, todayStart).
		Scan(&todayCreditSettledCents, &todayCreditSettledCount)

	todayAmount := model.ToYuan(todayAmountCents)
	monthAmount := model.ToYuan(monthAmountCents)
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
		"monthAmount":        model.Round2(monthAmount),
		"monthOrderCount":    monthOrderCount,
		"freeTableCount":     freeTableCount,
		"activeOrderCount":   activeOrderCount,
		// 免单 / 挂账
		"todayFreeAmount":          model.Round2(model.ToYuan(todayFreeCents)),
		"creditPendingAmount":      model.Round2(model.ToYuan(creditPendingCents)),
		"creditPendingCount":       creditPendingCount,
		"todayCreditAmount":        model.Round2(model.ToYuan(todayCreditCents)),
		"todayCreditSettledAmount": model.Round2(model.ToYuan(todayCreditSettledCents)),
		"todayCreditSettledCount":  todayCreditSettledCount,
	})
}

func ReportDailyTrend(c *gin.Context) {
	days := 7
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	nowT := time.Now()
	out := []gin.H{}
	for i := days - 1; i >= 0; i-- {
		d := nowT.AddDate(0, 0, -i)
		start := d.Format("2006-01-02") + " 00:00:00"
		end := d.AddDate(0, 0, 1).Format("2006-01-02") + " 00:00:00"
		var amountCents int64
		var cnt int
		// 趋势同样按实收金额统计:免单 0、未核销挂账 0,核销后计入下单日。
		store.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order
			WHERE create_time>=? AND create_time<? AND order_status!=5 AND pay_status=1`,
			start, end).Scan(&amountCents)
		store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order
			WHERE create_time>=? AND create_time<? AND order_status!=5`,
			start, end).Scan(&cnt)
		out = append(out, gin.H{"date": d.Format("01-02"), "amount": model.Round2(model.ToYuan(amountCents)), "orderCount": cnt})
	}
	ok(c, out)
}

func ReportMonthlyTrend(c *gin.Context) {
	nowT := time.Now()
	out := []gin.H{}
	for i := 11; i >= 0; i-- {
		m := nowT.AddDate(0, -i, 0)
		start := m.Format("2006-01") + "-01 00:00:00"
		end := m.AddDate(0, 1, 0).Format("2006-01") + "-01 00:00:00"
		var amountCents int64
		var cnt int
		// 趋势同样按实收金额统计:免单 0、未核销挂账 0,核销后计入下单日。
		store.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order
			WHERE create_time>=? AND create_time<? AND order_status!=5 AND pay_status=1`,
			start, end).Scan(&amountCents)
		store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order
			WHERE create_time>=? AND create_time<? AND order_status!=5`,
			start, end).Scan(&cnt)
		out = append(out, gin.H{"month": fmt.Sprintf("%d月", int(m.Month())), "amount": model.Round2(model.ToYuan(amountCents)), "orderCount": cnt})
	}
	ok(c, out)
}

func ReportDishRank(c *gin.Context) {
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	// 热销榜关联订单表,排除已取消订单,避免取消单计入销量。
	rows, err := store.DB.Query(`SELECT i.dish_name, SUM(i.quantity) AS qty, SUM(i.amount) AS amt
		FROM tb_order_item i JOIN tb_order o ON o.order_id = i.order_id
		WHERE o.order_status != 5
		GROUP BY i.dish_name ORDER BY qty DESC LIMIT ?`, limit)
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
