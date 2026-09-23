package dao

import (
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// 报表统计查询，金额单位均为分。

// DishRankRow 菜品排行的一行结果。
type DishRankRow struct {
	DishName    string
	Quantity    int
	AmountCents int64
}

// HourlyBucket 按时段聚合的订单统计桶。
type HourlyBucket struct {
	AmtCents int64
	Count    int
	Guests   int
}

// SettleMixRow 结算方式构成的一行结果。
type SettleMixRow struct {
	SettleType  string
	AmountCents int64
	PaidCents   int64
	Count       int
}

// RangeAmount 统计区间 [start, end) 的营业额(实收资金流,单位分)。
//
// 口径(资金流,按「钱到账的时间」归属):
//   - 正常收款(现金/在线,含挂账前的正常单):按 pay_time 归属,取 paid_amount+refund_amount
//     (加回退款是还原收款当时的毛额;退款本身由第三项按退款时间单独扣减);
//   - 挂账核销回款:按 credit_settle_time 归属,取 paid_amount;
//   - 退款:按退款到账时间(update_time)归属,从当日资金流中扣减。
//
// 为什么不用「下单时间(create_time)+实收净额」:那种口径下,历史订单的退款会
// 追溯拉低往日营业额(昨天的数字今天再看会变小),而「今日退款额」又按今天统计,
// 两个数字永远对不上账;资金流口径下每天的数字落定后不再漂移,且能与渠道账单对账。
// 重复支付自动退回(is_duplicate=1)的钱从未计入营收,因此也不在这里扣减。
func RangeAmount(start, end string) int64 {
	var v int64
	store.DB.QueryRow(`SELECT
		(SELECT COALESCE(SUM(COALESCE(paid_amount,0)+COALESCE(refund_amount,0)),0) FROM tb_order
			WHERE pay_time>=? AND pay_time<? AND order_status!=5 AND pay_status=1
			  AND COALESCE(settle_type,'normal')<>'credit')
		+ (SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order
			WHERE credit_settle_time>=? AND credit_settle_time<? AND order_status!=5 AND credit_status=2)
		- (SELECT COALESCE(SUM(amount),0) FROM tb_refund
			WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0)`,
		start, end, start, end, start, end, po.RefundStatusSuccess).Scan(&v)
	return v
}

// DailyTrendBucket 区间内某一天的营业额/订单/客流(用于折线趋势)。
type DailyTrendBucket struct {
	AmtCents   int64
	OrderCount int
	Guests     int
}

// DailyTrendStats 按自然日聚合区间 [start, end) 内的营业额/订单/客流,key 为 YYYY-MM-DD。
//
// 取代 DailyTrend 里「逐日调用 RangeAmount / RangeOrderCount / RangeGuestCount」的写法:
// 最长 366 天时旧写法会发起 366×(3 个子查询 + 2 个统计)≈ 1800 条 SQL,这里固定 2 条
// GROUP BY 查询完成聚合,缺失日期由调用方补零。
//
// 口径与既有单日查询完全一致(见 RangeAmount / RangeOrderCount / RangeGuestCount):
//   - 营业额按「钱到账时间」归属:正常收款 pay_time、挂账核销 credit_settle_time、
//     退款 update_time;三类来源先各自按日期前缀分组,再用 UNION ALL 合并求和;
//   - 订单数 / 客流按「下单时间 create_time」归属;
//   - substr(x,1,10) 在 SQLite 与 MySQL 下都返回 'YYYY-MM-DD',两库行为一致。
func DailyTrendStats(start, end string) (map[string]DailyTrendBucket, error) {
	out := map[string]DailyTrendBucket{}

	// 营业额(分):退款子查询返回负值,与正常收款/挂账核销相加即得到当日资金流净额。
	rows, err := store.DB.Query(`SELECT d, SUM(amt) FROM (
		SELECT substr(pay_time,1,10) AS d, COALESCE(SUM(COALESCE(paid_amount,0)+COALESCE(refund_amount,0)),0) AS amt
			FROM tb_order
			WHERE pay_time>=? AND pay_time<? AND order_status!=5 AND pay_status=1
			  AND COALESCE(settle_type,'normal')<>'credit'
			GROUP BY d
		UNION ALL
		SELECT substr(credit_settle_time,1,10) AS d, COALESCE(SUM(COALESCE(paid_amount,0)),0) AS amt
			FROM tb_order
			WHERE credit_settle_time>=? AND credit_settle_time<? AND order_status!=5 AND credit_status=2
			GROUP BY d
		UNION ALL
		SELECT substr(update_time,1,10) AS d, -COALESCE(SUM(amount),0) AS amt
			FROM tb_refund
			WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0
			GROUP BY d
	) t GROUP BY d`, start, end, start, end, start, end, po.RefundStatusSuccess)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var amt int64
		if err := rows.Scan(&d, &amt); err != nil {
			rows.Close()
			return nil, err
		}
		b := out[d]
		b.AmtCents += amt
		out[d] = b
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 订单数 / 客流一次按日分组拿到,避免再逐日 COUNT。
	rows, err = store.DB.Query(`SELECT substr(create_time,1,10) AS d, COUNT(*) AS cnt, COALESCE(SUM(person_count),0) AS guests
		FROM tb_order WHERE create_time>=? AND create_time<? AND order_status!=5 GROUP BY d`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var cnt, guests int
		if err := rows.Scan(&d, &cnt, &guests); err != nil {
			return nil, err
		}
		b := out[d]
		b.OrderCount = cnt
		b.Guests = guests
		out[d] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RangeOrderCount 统计区间 [start, end) 内未取消的订单数。
func RangeOrderCount(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v
}

// RangeGuestCount 统计区间 [start, end) 内未取消订单的人数合计。
func RangeGuestCount(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COALESCE(SUM(person_count),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v
}

// CountFinishedIn 统计区间内按完成时间归属的已完成订单数。
func CountFinishedIn(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE finish_time>=? AND finish_time<? AND order_status=4`, start, end).Scan(&v)
	return v
}

// CountCanceledIn 统计区间内按下单时间归属的已取消订单数。
func CountCanceledIn(start, end string) int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE create_time>=? AND create_time<? AND order_status=5`, start, end).Scan(&v)
	return v
}

// SumRefundIn 统计区间 [start, end) 内发生的退款总额(分)。
//
// 直接查退款流水表并按「退款到账时间(update_time)」归属,与 RangeAmount 的
// 资金流口径配套(当日营业额已按毛额计入,退款在发生当日单独扣减,两边对得上账)。
// 此前按 tb_order.refund_time 汇总退款金额:一笔订单分两天各退一部分时,
// refund_amount 是累计值、refund_time 是最后一次的时间,第二天会把第一天的
// 金额重复计入;按流水逐笔统计则天然正确。
// 重复支付自动退回(is_duplicate=1)不计入:那部分钱从未计入营收。
func SumRefundIn(start, end string) int64 {
	var v int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund
		WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0`,
		start, end, po.RefundStatusSuccess).Scan(&v)
	return v
}

// TableCount 统计未删除桌台总数。
func TableCount() int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0'`).Scan(&v)
	return v
}

// FreeTableCount 统计空闲桌台数。
func FreeTableCount() int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0' AND status=0`).Scan(&v)
	return v
}

// ActiveOrderCount 统计进行中的订单数。
func ActiveOrderCount() int {
	var v int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE order_status IN (1,2,3)`).Scan(&v)
	return v
}

// SumFreeIn 统计区间内免单让利总额(按订单应收金额)。
func SumFreeIn(start, end string) int64 {
	var v int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='free'`, start, end).Scan(&v)
	return v
}

// CreditPendingStats 统计待收款挂账的金额与笔数。
func CreditPendingStats() (cents int64, count int) {
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE order_status!=5 AND settle_type='credit' AND credit_status=1`).
		Scan(&cents, &count)
	return cents, count
}

// SumCreditIn 统计区间内产生的挂账金额。
func SumCreditIn(start, end string) int64 {
	var v int64
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='credit'`, start, end).Scan(&v)
	return v
}

// CreditSettledIn 统计区间内核销的挂账金额与笔数。
func CreditSettledIn(start, end string) (cents int64, count int) {
	store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE credit_settle_time>=? AND credit_settle_time<? AND credit_status=2`, start, end).
		Scan(&cents, &count)
	return cents, count
}

// dishRankMaxLimit 菜品排行单次返回上限。排行本质是「看头部」,全量拉取既无
// 业务价值又可能一次扫穿订单明细表,这里在 dao 层兜底夹紧,客户端传多大都只取 100。
const dishRankMaxLimit = 100

// DishRank 菜品排行:按销量(qty)或销售额(amt)排序,仅拼接白名单列防注入。
func DishRank(orderCol string, limit int) ([]DishRankRow, error) {
	if orderCol != "qty" && orderCol != "amt" {
		orderCol = "qty"
	}
	// limit 夹紧到 [1, dishRankMaxLimit]:0/负数按默认 100,超大值也按 100。
	if limit <= 0 || limit > dishRankMaxLimit {
		limit = dishRankMaxLimit
	}
	rows, err := store.DB.Query(`SELECT i.dish_name, SUM(i.quantity) AS qty, SUM(i.amount) AS amt
		FROM tb_order_item i JOIN tb_order o ON o.order_id = i.order_id
		WHERE o.order_status != 5
		GROUP BY i.dish_name ORDER BY `+orderCol+` DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DishRankRow{}
	for rows.Next() {
		var row DishRankRow
		rows.Scan(&row.DishName, &row.Quantity, &row.AmountCents)
		out = append(out, row)
	}
	return out, nil
}

// HourlyStats 按时段统计区间内的营业额/订单数/客流,key 为小时(00-23)。
func HourlyStats(start, end string) (map[string]HourlyBucket, error) {
	rows, err := store.DB.Query(`SELECT substr(create_time,12,2) AS h,
			COALESCE(SUM(CASE WHEN pay_status=1 THEN COALESCE(paid_amount,0) ELSE 0 END),0) AS amt,
			COUNT(*) AS cnt,
			COALESCE(SUM(person_count),0) AS guests
		FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5
		GROUP BY h`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHour := make(map[string]HourlyBucket, 24)
	for rows.Next() {
		var h string
		var b HourlyBucket
		rows.Scan(&h, &b.AmtCents, &b.Count, &b.Guests)
		byHour[h] = b
	}
	return byHour, nil
}

// SettleMixStats 按结算方式统计区间内的应收/实收/笔数。
func SettleMixStats(start, end string) ([]SettleMixRow, error) {
	rows, err := store.DB.Query(`SELECT COALESCE(NULLIF(settle_type,''),'normal') AS st,
			COALESCE(SUM(total_amount),0) AS amt,
			COALESCE(SUM(CASE WHEN pay_status=1 THEN COALESCE(paid_amount,0) ELSE 0 END),0) AS paid,
			COUNT(*) AS cnt
		FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5
		GROUP BY st`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SettleMixRow{}
	for rows.Next() {
		var row SettleMixRow
		rows.Scan(&row.SettleType, &row.AmountCents, &row.PaidCents, &row.Count)
		out = append(out, row)
	}
	return out, nil
}
