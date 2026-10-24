package dao

import (
	"fmt"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// 报表统计查询，金额单位均为分。

// bizDateExpr 返回把时间列折算成「营业日(YYYY-MM-DD)」的 SQL 表达式。
//
// cutoffHour=0 → 自然日,等价于 substr(col,1,10)(两库行为一致);
// >0 → 先把时间整体前移 cutoff 小时再取日期,凌晨 0~cutoff 点的记录归入前一天的营业日。
// 两库的日期函数不同:SQLite 用 datetime(x,'-H hours'),MySQL 用 DATE_SUB(x, INTERVAL H HOUR)。
// 调用方保证列里是 'YYYY-MM-DD HH:MM:SS' 形态(见 conf.TimeLayout)。
func bizDateExpr(col string, cutoffHour int) string {
	if cutoffHour <= 0 {
		return "substr(" + col + ",1,10)"
	}
	if store.IsMySQL() {
		return fmt.Sprintf("DATE_FORMAT(DATE_SUB(%s, INTERVAL %d HOUR),'%%Y-%%m-%%d')", col, cutoffHour)
	}
	return fmt.Sprintf("substr(datetime(%s, '-%d hours'),1,10)", col, cutoffHour)
}

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

// RangeWalletConsume 统计区间 [start, end) 用储值余额核销的营业额(单位分)。
//
// 口径:窗口内已支付订单的「储值流水金额」减去同期退回储值的金额。
// 「储值核销」是营业额的**子集**,两者相减即当天真实到账资金 ——
// 少了这一列,老板看到的「今日营业额」里混着以前收过的钱(储值属负债转收入,
// 当天并没有现金流入)。
//
// 为什么不再用 tb_order.pay_channel='wallet':混合支付(余额 + 微信补差)的订单
// 结算渠道是微信,但它同样有一半钱来自储值,按订单渠道统计会把这部分漏掉。
// 退回到储值的钱不计入核销(它也没产生现金流出),与 RangeAmount 的退款口径配套。
//
// 查询失败返回 error:静默按 0 返回会让报表把「查不到」显示成「今天没有储值核销」,
// 与 DailyTrendStats 等函数的错误处理口径保持一致。
func RangeWalletConsume(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT
		(SELECT COALESCE(SUM(p.amount),0) FROM tb_payment p
		 JOIN tb_order o ON o.order_no=p.order_no
		 WHERE o.pay_time>=? AND o.pay_time<? AND o.order_status!=5 AND o.pay_status=1
		   AND COALESCE(o.settle_type,'normal')<>'credit'
		   AND p.channel=? AND p.status IN (?,?))
		- (SELECT COALESCE(SUM(r.amount),0) FROM tb_refund r
		   WHERE r.update_time>=? AND r.update_time<? AND r.status=? AND COALESCE(r.is_duplicate,0)=0
		     AND r.channel=?)`,
		start, end, "wallet", PayStatusPaid, PayStatusRefunded,
		start, end, po.RefundStatusSuccess, "wallet").Scan(&v)
	return v, err
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
//
// 查询失败返回 error(不静默按 0):否则 DB 抖动时首页会显示「今日营业额 0.00」,
// 店主据此误判「今天没开张」—— 比接口报错更难排查。
func RangeAmount(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT
		(SELECT COALESCE(SUM(COALESCE(paid_amount,0)+COALESCE(refund_amount,0)),0) FROM tb_order
			WHERE pay_time>=? AND pay_time<? AND order_status!=5 AND pay_status=1
			  AND COALESCE(settle_type,'normal')<>'credit')
		+ (SELECT COALESCE(SUM(COALESCE(paid_amount,0)),0) FROM tb_order
			WHERE credit_settle_time>=? AND credit_settle_time<? AND order_status!=5 AND credit_status=2)
		- (SELECT COALESCE(SUM(amount),0) FROM tb_refund
			WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0)`,
		start, end, start, end, start, end, po.RefundStatusSuccess).Scan(&v)
	return v, err
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
//
// cutoffHour 为营业日切换时刻(见 service.BizDayCutoffHour):>0 时日期键按
// 「时间前移 cutoff 小时后取日期」折算,凌晨 0~cutoff 点的流水归入前一天;
// =0 时退化为自然日,与历史口径完全一致。分桶键与 service 侧的区间边界
// 必须用同一个 cutoff,否则折线图与汇总数字会打架。
func DailyTrendStats(start, end string, cutoffHour int) (map[string]DailyTrendBucket, error) {
	out := map[string]DailyTrendBucket{}
	// 三种时间列的营业日表达式同构,只是列名不同。
	dayOf := func(col string) string { return bizDateExpr(col, cutoffHour) }

	// 营业额(分):退款子查询返回负值,与正常收款/挂账核销相加即得到当日资金流净额。
	rows, err := store.DB.Query(`SELECT d, SUM(amt) FROM (
		SELECT `+dayOf("pay_time")+` AS d, COALESCE(SUM(COALESCE(paid_amount,0)+COALESCE(refund_amount,0)),0) AS amt
			FROM tb_order
			WHERE pay_time>=? AND pay_time<? AND order_status!=5 AND pay_status=1
			  AND COALESCE(settle_type,'normal')<>'credit'
			GROUP BY d
		UNION ALL
		SELECT `+dayOf("credit_settle_time")+` AS d, COALESCE(SUM(COALESCE(paid_amount,0)),0) AS amt
			FROM tb_order
			WHERE credit_settle_time>=? AND credit_settle_time<? AND order_status!=5 AND credit_status=2
			GROUP BY d
		UNION ALL
		SELECT `+dayOf("update_time")+` AS d, -COALESCE(SUM(amount),0) AS amt
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
	rows, err = store.DB.Query(`SELECT `+dayOf("create_time")+` AS d, COUNT(*) AS cnt, COALESCE(SUM(person_count),0) AS guests
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
func RangeOrderCount(start, end string) (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v, err
}

// RangeGuestCount 统计区间 [start, end) 内未取消订单的人数合计。
func RangeGuestCount(start, end string) (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(person_count),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v, err
}

// CountFinishedIn 统计区间内按完成时间归属的已完成订单数。
func CountFinishedIn(start, end string) (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE finish_time>=? AND finish_time<? AND order_status=4`, start, end).Scan(&v)
	return v, err
}

// CountCanceledIn 统计区间内按下单时间归属的已取消订单数。
func CountCanceledIn(start, end string) (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE create_time>=? AND create_time<? AND order_status=5`, start, end).Scan(&v)
	return v, err
}

// SumRefundIn 统计区间 [start, end) 内发生的退款总额(分)。
//
// 直接查退款流水表并按「退款到账时间(update_time)」归属,与 RangeAmount 的
// 资金流口径配套(当日营业额已按毛额计入,退款在发生当日单独扣减,两边对得上账)。
// 此前按 tb_order.refund_time 汇总退款金额:一笔订单分两天各退一部分时,
// refund_amount 是累计值、refund_time 是最后一次的时间,第二天会把第一天的
// 金额重复计入;按流水逐笔统计则天然正确。
// 重复支付自动退回(is_duplicate=1)不计入:那部分钱从未计入营收。
func SumRefundIn(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_refund
		WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0`,
		start, end, po.RefundStatusSuccess).Scan(&v)
	return v, err
}

// TableCount 统计未删除桌台总数。
func TableCount() (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0'`).Scan(&v)
	return v, err
}

// FreeTableCount 统计空闲桌台数。
func FreeTableCount() (int, error) {
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE del_flag='0' AND status=0`).Scan(&v)
	return v, err
}

// ActiveOrderCount 统计进行中的订单数。
func ActiveOrderCount(guardBizDate string) (int, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	var v int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE order_status IN (1,2,3)`+guard, args...).Scan(&v)
	return v, err
}

// SumFreeIn 统计区间内免单让利总额(按订单应收金额)。
func SumFreeIn(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='free'`, start, end).Scan(&v)
	return v, err
}

// SumGiftIn 统计区间内赠送让利总额(按订单赠送金额 gift_amount)。
// 与 SumFreeIn 同口径:按下单时间 create_time 归属,排除已取消订单。
func SumGiftIn(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(gift_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5`, start, end).Scan(&v)
	return v, err
}

// CreditPendingStats 统计待收款挂账的金额与笔数。
func CreditPendingStats() (cents int64, count int, err error) {
	err = store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE order_status!=5 AND settle_type='credit' AND credit_status=1`).
		Scan(&cents, &count)
	return cents, count, err
}

// SumCreditIn 统计区间内产生的挂账金额。
func SumCreditIn(start, end string) (int64, error) {
	var v int64
	err := store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0) FROM tb_order
		WHERE create_time>=? AND create_time<? AND order_status!=5 AND settle_type='credit'`, start, end).Scan(&v)
	return v, err
}

// CreditSettledIn 统计区间内核销的挂账金额与笔数。
func CreditSettledIn(start, end string) (cents int64, count int, err error) {
	err = store.DB.QueryRow(`SELECT COALESCE(SUM(credit_amount),0), COUNT(*) FROM tb_order
		WHERE credit_settle_time>=? AND credit_settle_time<? AND credit_status=2`, start, end).
		Scan(&cents, &count)
	return cents, count, err
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
	// 按 (dish_id, dish_name) 分组:不同分类下的同名菜品是两道不同的菜
	// (价格/成本各自独立),只按名称分组会把它们合并成一行,排行与金额都失真。
	// 历史行 dish_id 可能为 0,同名的会合并在一起 —— 可接受,好过误并不同菜。
	rows, err := store.DB.Query(`SELECT i.dish_name, SUM(i.quantity) AS qty, SUM(i.amount) AS amt
		FROM tb_order_item i JOIN tb_order o ON o.order_id = i.order_id
		WHERE o.order_status != 5
		GROUP BY i.dish_id, i.dish_name ORDER BY `+orderCol+` DESC, i.dish_id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DishRankRow{}
	for rows.Next() {
		var row DishRankRow
		if err := rows.Scan(&row.DishName, &row.Quantity, &row.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// HourlyStats 按时段统计区间内的营业额/订单数/客流,key 为小时(00-23)。
//
// 口径与 RangeAmount / DailyTrendStats 保持一致(均为资金流,按「钱到账的时间」归属):
//   - 营业额:正常收款按 pay_time、挂账核销按 credit_settle_time、退款按 update_time 扣减
//     (子查询返回负值),三路先各自按小时分组再 UNION ALL 合并求和;
//   - 订单数 / 客流仍按下单时间 create_time 归属 —— 与日报趋势同一口径。
//
// 为什么不按「下单时间 + 实收净额」:那样各小时金额加起来 ≠ 「今日营业额」,
// 店长对账时两个数字对不上;统一资金流口径后,分时图合计与营业额一致。
// 重复支付自动退回(is_duplicate=1)的钱从未计入营收,同样不参与扣减。
func HourlyStats(start, end string) (map[string]HourlyBucket, error) {
	rows, err := store.DB.Query(`SELECT h, SUM(amt) FROM (
		SELECT substr(pay_time,12,2) AS h,
				COALESCE(SUM(COALESCE(paid_amount,0)+COALESCE(refund_amount,0)),0) AS amt
			FROM tb_order
			WHERE pay_time>=? AND pay_time<? AND order_status!=5 AND pay_status=1
			  AND COALESCE(settle_type,'normal')<>'credit'
			GROUP BY h
		UNION ALL
		SELECT substr(credit_settle_time,12,2) AS h, COALESCE(SUM(COALESCE(paid_amount,0)),0) AS amt
			FROM tb_order
			WHERE credit_settle_time>=? AND credit_settle_time<? AND order_status!=5 AND credit_status=2
			GROUP BY h
		UNION ALL
		SELECT substr(update_time,12,2) AS h, -COALESCE(SUM(amount),0) AS amt
			FROM tb_refund
			WHERE update_time>=? AND update_time<? AND status=? AND COALESCE(is_duplicate,0)=0
			GROUP BY h
	) t GROUP BY h`, start, end, start, end, start, end, po.RefundStatusSuccess)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHour := make(map[string]HourlyBucket, 24)
	for rows.Next() {
		var h string
		var amt int64
		if err := rows.Scan(&h, &amt); err != nil {
			return nil, err
		}
		b := byHour[h]
		b.AmtCents = amt
		byHour[h] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 订单数 / 客流:按下单时间分组(与 DailyTrendStats 的订单/客流口径一致)。
	rows, err = store.DB.Query(`SELECT substr(create_time,12,2) AS h, COUNT(*) AS cnt,
			COALESCE(SUM(person_count),0) AS guests
		FROM tb_order WHERE create_time>=? AND create_time<? AND order_status!=5 GROUP BY h`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		var b HourlyBucket
		if err := rows.Scan(&h, &b.Count, &b.Guests); err != nil {
			return nil, err
		}
		cur := byHour[h]
		cur.Count = b.Count
		cur.Guests = b.Guests
		byHour[h] = cur
	}
	return byHour, rows.Err()
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
		if err := rows.Scan(&row.SettleType, &row.AmountCents, &row.PaidCents, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DiningDurationRow 一笔已完成订单的用餐起止时间(含就餐人数,供人均口径参考)。
// MealStartTime/MealEndTime 均为 conf.TimeLayout 字符串,时长换算放在 service 内存里做,
// 不依赖数据库时间函数,MySQL / SQLite 双方言通用。
//
// 字段名按「用餐」语义命名:取的是 meal_start_time / meal_end_time,
// 历史行才 COALESCE 回落到 create_time / finish_time,故不再叫 CreateTime/FinishTime。
type DiningDurationRow struct {
	MealStartTime string
	MealEndTime   string
	PersonCount   int
}

// DiningDurationRows 取区间内按完成时间归属的已完成订单的用餐起止时间。
//
// 口径与 CountFinishedIn 一致(order_status=4,finish_time∈[start,end))。
// 起止时间优先取落库的 meal_start_time / meal_end_time(下单与结账时写入),
// 历史行为 NULL,故 COALESCE 回落到 create_time / finish_time —— 升级前后口径不变。
func DiningDurationRows(start, end string) ([]DiningDurationRow, error) {
	rows, err := store.DB.Query(`SELECT COALESCE(meal_start_time, create_time), COALESCE(meal_end_time, finish_time),
			COALESCE(person_count,0)
		FROM tb_order
		WHERE order_status=4 AND finish_time>=? AND finish_time<?`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DiningDurationRow{}
	for rows.Next() {
		var row DiningDurationRow
		if err := rows.Scan(TS(&row.MealStartTime), TS(&row.MealEndTime), &row.PersonCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
