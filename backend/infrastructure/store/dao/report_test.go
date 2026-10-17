package dao

import (
	"path/filepath"
	"testing"
	"time"

	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
)

// TestRangeAmountMoneyFlow 资金流口径:营业额按钱到账时间归属,
// 历史订单的退款不追溯改写往日营业额,当日退款在当日扣减,重复退回不计入。
func TestRangeAmountMoneyFlow(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "report.db"))
	defer func() { _ = store.DB.Close() }()

	now := time.Now()
	yStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	tStart := yStart.AddDate(0, 0, 1)
	tEnd := tStart.AddDate(0, 0, 1)
	f := func(v time.Time) string { return v.Format(conf.TimeLayout) }
	noon := func(day time.Time) string {
		return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, day.Location()).Format(conf.TimeLayout)
	}

	mustExec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := store.DB.Exec(query, args...); err != nil {
			t.Fatalf("执行失败: %v\n%s", err, query)
		}
	}

	// 订单 A:昨日收款 100 元(已完成),今日全额退款。
	mustExec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, order_status, total_amount,
		pay_status, pay_time, settle_type, paid_amount, create_time, update_time)
		VALUES('RA1', 1, 'T1', '桌1', 4, 10000, 1, ?, 'normal', 10000, ?, ?)`,
		noon(yStart), noon(yStart), noon(yStart))
	// 订单 B:挂账今日核销回款 50 元。
	mustExec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, order_status, total_amount,
		pay_status, pay_time, settle_type, credit_status, credit_amount, credit_settle_time, paid_amount, create_time, update_time)
		VALUES('RA2', 2, 'T2', '桌2', 4, 5000, 1, ?, 'credit', 2, 5000, ?, 5000, ?, ?)`,
		noon(yStart), noon(tStart), noon(yStart), noon(tStart))
	var orderAID int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no='RA1'`).Scan(&orderAID); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}

	// 昨日营业额(退款发生前):100。
	if got := RangeAmount(f(yStart), f(tStart)); got != 10000 {
		t.Fatalf("昨日营业额期望 10000, got %d", got)
	}

	// 今日对订单 A 全额退款 + 一笔重复支付退回(不应计入任何统计)。
	mustExec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel, amount, status, reason, operator, create_time, update_time)
		VALUES(?, 'RA1', 0, 'RR1', 'wxpay', 10000, 1, '测试退款', 'tester', ?, ?)`, orderAID, noon(tStart), noon(tStart))
	mustExec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel, amount, status, reason, operator, is_duplicate, create_time, update_time)
		VALUES(?, 'RA1', 0, 'RR2', 'alipay', 10000, 1, '重复支付自动原路退回', 'system', 1, ?, ?)`, orderAID, noon(tStart), noon(tStart))
	// 退款入账:实收扣到 0、退款额累计(与 applyRefundSuccess 同口径)。
	mustExec(`UPDATE tb_order SET paid_amount=0, refund_amount=10000, refund_time=? WHERE order_no='RA1'`, noon(tStart))

	// 昨日营业额不受今日退款影响(数字落定后不漂移)。
	if got := RangeAmount(f(yStart), f(tStart)); got != 10000 {
		t.Fatalf("今日退款后昨日营业额应保持 10000(不漂移), got %d", got)
	}
	// 今日:挂账回款 +5000,退款 -10000,重复退回不计 → -5000。
	if got := RangeAmount(f(tStart), f(tEnd)); got != -5000 {
		t.Fatalf("今日净资金流期望 -5000, got %d", got)
	}
	// 今日退款额 = 100(重复退回不计入)。
	if got := SumRefundIn(f(tStart), f(tEnd)); got != 10000 {
		t.Fatalf("今日退款额期望 10000(排除重复退回), got %d", got)
	}
}

// TestHourlyStatsMoneyFlow 时段分布与营业额同口径:金额按钱到账时间归属
// (pay_time / credit_settle_time / 退款 update_time),各小时合计 = RangeAmount,
// 分时图与「今日营业额」不再对不上;订单数/客流仍按下单时间归属。
func TestHourlyStatsMoneyFlow(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "hourly.db"))
	defer func() { _ = store.DB.Close() }()

	now := time.Now()
	tStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tEnd := tStart.AddDate(0, 0, 1)
	f := func(v time.Time) string { return v.Format(conf.TimeLayout) }
	at := func(hour int) string {
		return time.Date(tStart.Year(), tStart.Month(), tStart.Day(), hour, 0, 0, 0, tStart.Location()).Format(conf.TimeLayout)
	}
	mustExec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := store.DB.Exec(query, args...); err != nil {
			t.Fatalf("执行失败: %v\n%s", err, query)
		}
	}

	// 10 点正常收款 100 元(2 人)。
	mustExec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status, total_amount,
		pay_status, pay_time, settle_type, paid_amount, create_time, update_time)
		VALUES('HB1', 1, 'T1', '桌1', 2, 4, 10000, 1, ?, 'normal', 10000, ?, ?)`,
		at(10), at(10), at(10))
	// 11 点挂账核销回款 50 元(下单时间回填 10 点,订单数应归到 10 点)。
	mustExec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status, total_amount,
		pay_status, settle_type, credit_status, credit_amount, credit_settle_time, paid_amount, create_time, update_time)
		VALUES('HB2', 2, 'T2', '桌2', 3, 4, 5000, 1, 'credit', 2, 5000, ?, 5000, ?, ?)`,
		at(11), at(10), at(11))

	var orderAID int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no='HB1'`).Scan(&orderAID); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	// 12 点对 HB1 退款 30 元。
	mustExec(`INSERT INTO tb_refund(order_id, order_no, payment_id, refund_no, channel, amount, status, reason, operator, create_time, update_time)
		VALUES(?, 'HB1', 0, 'HR1', 'wxpay', 3000, 1, '测试退款', 'tester', ?, ?)`, orderAID, at(12), at(12))

	byHour, err := HourlyStats(f(tStart), f(tEnd))
	if err != nil {
		t.Fatalf("HourlyStats 失败: %v", err)
	}
	if got := byHour["10"].AmtCents; got != 10000 {
		t.Fatalf("10 点营业额期望 10000(正常收款), got %d", got)
	}
	if got := byHour["11"].AmtCents; got != 5000 {
		t.Fatalf("11 点营业额期望 5000(挂账核销), got %d", got)
	}
	if got := byHour["12"].AmtCents; got != -3000 {
		t.Fatalf("12 点营业额期望 -3000(退款扣减), got %d", got)
	}
	// 各小时合计必须与 RangeAmount 一致 —— 这正是本次修复的目标(此前两者口径不同)。
	var sum int64
	for _, b := range byHour {
		sum += b.AmtCents
	}
	if want := RangeAmount(f(tStart), f(tEnd)); sum != want {
		t.Fatalf("分时合计 %d 应等于营业额 %d", sum, want)
	}
	// 订单数/客流按下单时间:两单都在 10 点下单。
	if byHour["10"].Count != 2 || byHour["10"].Guests != 5 {
		t.Fatalf("10 点订单数/客流期望 2/5, got %d/%d", byHour["10"].Count, byHour["10"].Guests)
	}
}
