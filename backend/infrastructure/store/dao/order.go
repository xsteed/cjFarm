package dao

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
)

// OrderCols 订单表完整列(与 ScanOrder 顺序一一对应)。
const OrderCols = `order_id, order_no, table_id, table_no, table_name, person_count, order_status,
	dish_amount, seat_fee, discount_amount, total_amount, pay_status, pay_type, pay_time,
	transaction_id, pay_channel, refund_amount, refund_time,
	settle_type, settle_time, settle_operator, settle_remark,
	credit_status, credit_amount, credit_settle_time, credit_settle_by, paid_amount, gift_amount,
	finish_time, order_remark, cancel_reason, create_by, create_time, update_by, update_time, remark,
	biz_date, pickup_no, meal_start_time, meal_end_time, viewed_at`

// ScanOrder 扫描一行订单记录(不含明细)。
//
// 可 NULL 的字符串列统一走 sql.NullString:老库加列时未带默认值的列
// (如 refund_time/settle_time/credit_settle_time)历史行就是 NULL,
// 直接 Scan 到 string 会整行报错;行报错在列表查询里表现为「静默丢单」。
func ScanOrder(rows interface{ Scan(...interface{}) error }) (po.Order, error) {
	var o po.Order
	var tableNo, tableName sql.NullString
	var payType, payTime sql.NullString
	var transactionID, payChannel sql.NullString
	var refundTime, settleType sql.NullString
	var settleTime, settleOperator, settleRemark sql.NullString
	var creditSettleTime, creditSettleBy sql.NullString
	var finishTime, orderRemark, cancelReason sql.NullString
	var createBy, createTime, updateBy, updateTime, remark sql.NullString
	// biz_date 允许 NULL(历史行/手工补录行),故走 NullString 再回落为空串。
	var bizDate sql.NullString
	var mealStartTime, mealEndTime sql.NullString
	err := rows.Scan(&o.OrderID, &o.OrderNo, &o.TableID, &tableNo, &tableName, &o.PersonCount, &o.OrderStatus,
		&o.DishAmount, &o.SeatFee, &o.DiscountAmount, &o.TotalAmount, &o.PayStatus, &payType, TS(&payTime),
		&transactionID, &payChannel, &o.RefundAmount, TS(&refundTime),
		&settleType, TS(&settleTime), &settleOperator, &settleRemark,
		&o.CreditStatus, &o.CreditAmount, TS(&creditSettleTime), &creditSettleBy, &o.PaidAmount, &o.GiftAmount,
		TS(&finishTime), &orderRemark, &cancelReason, &createBy, TS(&createTime), &updateBy, TS(&updateTime), &remark,
		&bizDate, &o.PickupNo, TS(&mealStartTime), TS(&mealEndTime), TS(&o.ViewedAt))
	if err != nil {
		return o, err
	}
	o.MealStartTime, o.MealEndTime = nsPtr(mealStartTime), nsPtr(mealEndTime)
	o.BizDate = bizDate.String
	o.TableNo, o.TableName = tableNo.String, tableName.String
	// 模型里可空时间字段是 *string(保留 NULL 语义),这里由 NullString 转指针。
	o.PayType, o.PayTime = nsPtr(payType), nsPtr(payTime)
	o.TransactionID, o.PayChannel = transactionID.String, payChannel.String
	o.RefundTime = nsPtr(refundTime)
	o.SettleTime, o.SettleOperator, o.SettleRemark = nsPtr(settleTime), settleOperator.String, settleRemark.String
	o.CreditSettleTime, o.CreditSettleBy = nsPtr(creditSettleTime), creditSettleBy.String
	o.FinishTime, o.OrderRemark, o.CancelReason = nsPtr(finishTime), orderRemark.String, cancelReason.String
	o.CreateBy, o.CreateTime, o.UpdateBy, o.UpdateTime = createBy.String, createTime.String, updateBy.String, updateTime.String
	o.Remark = nsPtr(remark)
	// 老数据列为 NULL 时兜底为正常收款,避免前端判断出现空值分支。
	o.SettleType = settleType.String
	if o.SettleType == "" {
		o.SettleType = "normal"
	}
	return o, nil
}

// nsPtr 把 sql.NullString 转成 *string:NULL 返回 nil,否则返回指向值的指针。
// 与 po.Order 中可空时间字段的 *string 类型配套,保留 NULL 语义。
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// GetOrderByID 按订单 ID 查询订单。
func GetOrderByID(orderID int) (po.Order, error) {
	return ScanOrder(store.DB.QueryRow(`SELECT `+OrderCols+` FROM tb_order WHERE order_id=?`, orderID))
}

// MarkOrderViewed 记录商家「第一次」实际操作该订单的时间(仅查看不算,见 handler.OrderGet)。
// 条件把空串也视同「未操作」(个别库的列默认值/外部脚本会产生空串,读侧视同 NULL):
// 否则这类行的守卫永远不命中,操作后「新」角标无法消解。
// 只写首次时间,后续操作不重复改写;重复调用是无副作用的幂等操作。
func MarkOrderViewed(orderID int) error {
	_, err := store.DB.Exec(`UPDATE tb_order SET viewed_at=? WHERE order_id=? AND (viewed_at IS NULL OR viewed_at='')`,
		store.Now(), orderID)
	return err
}

// GetOrderByNo 按订单号查询订单。
func GetOrderByNo(orderNo string) (po.Order, error) {
	return ScanOrder(store.DB.QueryRow(`SELECT `+OrderCols+` FROM tb_order WHERE order_no=?`, orderNo))
}

// GetOrderByNoAndCustomer 按单号 + 会员归属查订单(会员「我的订单」详情用)。
//
// 为什么必须有这一条:顾客端已登录后看的是**跨桌**历史订单,不能像点餐时那样
// 用 tableId 证明归属(顾客早就离开那张桌了)。缺了 customer_id 条件,等于让任何
// 登录用户凭单号遍历他人订单 —— 单号会出现在小票、照片与群聊里。
func GetOrderByNoAndCustomer(orderNo string, customerID int) (po.Order, error) {
	return ScanOrder(store.DB.QueryRow(
		`SELECT `+OrderCols+` FROM tb_order WHERE order_no=? AND customer_id=?`, orderNo, customerID))
}

// bizDateGuardSQL 生成「营业日隔离」条件片段:只认 guardBizDate 当天及以后的订单。
//
// guardBizDate 为空串时返回空条件(不过滤,保持历史行为)—— 是否隔离由调用方按
// 「跨日自动清台」配置开关决定(见 service.StaleGuardBizDate)。
//
// 为什么 biz_date 为 NULL / 空串的行要显式放行:那是手工补录或历史数据,无从判断
// 属于哪个营业日。宁可让它继续占着桌台(店家看得见、能处理),也不能把一张正在用餐
// 的桌误判成空闲。顺带一提,SQL 里 NULL 参与比较的结果是 NULL(不为真),
// 不加这个 OR 会把它们一并过滤掉。
func bizDateGuardSQL(guardBizDate string) (string, []interface{}) {
	if guardBizDate == "" {
		return "", nil
	}
	return ` AND (biz_date IS NULL OR biz_date='' OR biz_date>=?)`, []interface{}{guardBizDate}
}

// GetActiveOrderByTable 查询桌台最近一笔进行中订单。
func GetActiveOrderByTable(tableID int, guardBizDate string) (po.Order, bool) {
	o, ok, _ := GetActiveOrderByTableE(tableID, guardBizDate)
	return o, ok
}

// GetActiveOrderByTableE 是 GetActiveOrderByTable 的带错误版本。
//
// 调用方是「删桌台 / 停用桌台」这类**拦截型守卫**:必须区分「确实没有进行中订单」
// (sql.ErrNoRows) 与「查询失败」。旧实现把两者都返回 (零值, false),库抖动时
// 守卫会被误判为「无订单」而放行 —— 正在用餐的桌台被删掉/停用,进行中订单随即
// 失去入口,加菜/收款/结账全部卡死。
func GetActiveOrderByTableE(tableID int, guardBizDate string) (po.Order, bool, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	o, err := ScanOrder(store.DB.QueryRow(
		`SELECT `+OrderCols+` FROM tb_order WHERE table_id=? AND order_status IN (1,2,3)`+guard+` ORDER BY order_id DESC LIMIT 1`,
		append([]interface{}{tableID}, args...)...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return po.Order{}, false, nil
		}
		return po.Order{}, false, err
	}
	return o, true, nil
}

// GetUnpaidOrderByTable 查询桌台最近一笔「未结账」订单(顾客付款前可加菜/付款的那一单)。
// 与 GetActiveOrderByTable 的区别:已付款但出餐未完成的订单不算「未结账」,
// 顾客此时应能开新单,故此处额外按 pay_status=0 过滤。
func GetUnpaidOrderByTable(tableID int, guardBizDate string) (po.Order, bool) {
	guard, args := bizDateGuardSQL(guardBizDate)
	o, err := ScanOrder(store.DB.QueryRow(
		`SELECT `+OrderCols+` FROM tb_order WHERE table_id=? AND pay_status=0 AND order_status IN (1,2,3)`+guard+` ORDER BY order_id DESC LIMIT 1`,
		append([]interface{}{tableID}, args...)...))
	if err != nil {
		return po.Order{}, false
	}
	return o, true
}

// ListStaleTableIDs 查「上一营业日及更早」仍有进行中订单的桌台 ID(去重)。
//
// 供跨日自动清台使用:这些桌台显示占用,但订单其实是过期遗留 —— 要么店家漏点了
// 收款,要么客人早就走了。biz_date 为空的行走不到这里(NULL 比较不为真),它们
// 仍由店家人工处理。
func ListStaleTableIDs(today string) []int {
	rows, err := store.DB.Query(
		`SELECT DISTINCT table_id FROM tb_order WHERE order_status IN (1,2,3) AND biz_date IS NOT NULL AND biz_date<>'' AND biz_date<?`,
		today)
	if err != nil {
		logger.Warnf("[dao] 查询跨日遗留订单桌台失败: %v", err)
		return nil
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			logger.Warnf("[dao] 扫描跨日遗留订单桌台失败: %v", err)
			return nil
		}
		ids = append(ids, id)
	}
	return ids
}

// ListActiveOrdersByTable 查询桌台进行中订单(1/2/3),按开单顺序升序。
// 与 GetActiveOrderByTable(只取最新一笔)的区别:强制清台要一次性收走该桌全部
// 未结单,漏掉任何一笔都会让桌台卡在占用态。
// guardBizDate 非空时带营业日隔离,语义见 bizDateGuardSQL。
func ListActiveOrdersByTable(tableID int, guardBizDate string) []po.Order {
	list, _ := ListActiveOrdersByTableE(tableID, guardBizDate)
	return list
}

// ListActiveOrdersByTableE 是 ListActiveOrdersByTable 的带错误版本。
//
// 调用方(强制清台)在拿不到完整列表时必须失败而不是当成「没有订单」:
// 后者会走进「仅重置占用状态」分支,操作者却看到「清台完成」—— 未结账的订单
// 一筆都没被处理,桌台反而显示空闲。
func ListActiveOrdersByTableE(tableID int, guardBizDate string) ([]po.Order, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	rows, err := store.DB.Query(
		`SELECT `+OrderCols+` FROM tb_order WHERE table_id=? AND order_status IN (1,2,3)`+guard+` ORDER BY order_id`,
		append([]interface{}{tableID}, args...)...)
	if err != nil {
		logger.Warnf("[dao] 查询桌台进行中订单失败 table=%d: %v", tableID, err)
		return nil, err
	}
	defer rows.Close()
	list := []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			// 单行扫描失败就整批放弃返回:静默少收一笔,桌台会解不开。
			logger.Warnf("[dao] 扫描桌台进行中订单失败 table=%d: %v", tableID, scanErr)
			return nil, scanErr
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		logger.Warnf("[dao] 遍历桌台进行中订单中断 table=%d: %v", tableID, err)
		return nil, err
	}
	return list, nil
}

// ListServingOrdersByTable 查询桌台「已付款但出餐未完成」的订单,顾客扫码时据此查看出餐进度。
func ListServingOrdersByTable(tableID int, guardBizDate string) []po.Order {
	guard, args := bizDateGuardSQL(guardBizDate)
	rows, err := store.DB.Query(
		`SELECT `+OrderCols+` FROM tb_order WHERE table_id=? AND pay_status=1 AND order_status IN (1,2,3)`+guard+` ORDER BY order_id`,
		append([]interface{}{tableID}, args...)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []po.Order
	for rows.Next() {
		if o, err := ScanOrder(rows); err == nil {
			list = append(list, o)
		}
	}
	return list
}

// ListStaleUnpaidOrders 查「本营业日、未结账、进行中且开台下单早于 beforeTime」的订单。
//
// 只看本营业日:更早的属于跨日遗留单,那是「跨日自动清台」的职责(它们要么已被隔离、
// 要么店家正拿着处理),混进超时告警里会让看板每天一早先响一片昨天的噪音。
// create_time 为空的行跳过:算不出时长就无从判定是否超时。
// 注意 SQL 里不能写 create_time<>'':MySQL 下该列是 DATETIME,与空串比较会报
// Error 1525,空串过滤只能放在 Go 侧(见 ScanOrder 之后的循环)。
func ListStaleUnpaidOrders(bizDate, beforeTime string) []po.Order {
	rows, err := store.DB.Query(
		// LIMIT 是防御性的:看板那条提醒条要一屏能读完,真有几十桌同时超时,
		// 拉全部回来既撑爆界面也没人逐条看 —— 按开台时长从早到晚取前 50 条即可。
		`SELECT `+OrderCols+` FROM tb_order WHERE order_status IN (1,2,3) AND pay_status=0
			AND biz_date=? AND create_time IS NOT NULL AND create_time<=?
			ORDER BY create_time LIMIT 50`,
		bizDate, beforeTime)
	if err != nil {
		logger.Warnf("[dao] 查询未结账超时订单失败: %v", err)
		return nil
	}
	defer rows.Close()
	list := []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			logger.Warnf("[dao] 扫描未结账超时订单失败: %v", scanErr)
			return nil
		}
		// SQLite 下 create_time 是 VARCHAR,历史数据可能落了空串:算不出时长,跳过。
		if o.CreateTime == "" {
			continue
		}
		list = append(list, o)
	}
	return list
}

// ListLegacyUnpaidOrders 查「上一营业日及更早」仍未结账的订单(跨日遗留单)。
//
// 与 ListStaleUnpaidOrders 的分工:那个管「当天开着台还没结」,这个管「这个营业日已经
// 过去了却还没结」。跨日清台只把桌台恢复空闲,订单仍挂在「进行中」—— 它们会沉在订单
// 列表最深处(列表按单号倒序,越老越靠后),不主动列出来就等于被遗忘:桌台上什么都
// 看不到,对账时却少一笔。
//
// 与跨日自动清台的开关无关:不管开没开,「这个营业日过去了还没结账」都是事实,
// 店家都该知道。开关只决定要不要顺手把桌台恢复空闲。
func ListLegacyUnpaidOrders(bizDate string) []po.Order {
	rows, err := store.DB.Query(
		`SELECT `+OrderCols+` FROM tb_order WHERE order_status IN (1,2,3) AND pay_status=0
			AND biz_date IS NOT NULL AND biz_date<>'' AND biz_date<?
			ORDER BY biz_date, create_time LIMIT 50`,
		bizDate)
	if err != nil {
		logger.Warnf("[dao] 查询跨日遗留订单失败: %v", err)
		return nil
	}
	defer rows.Close()
	list := []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			logger.Warnf("[dao] 扫描跨日遗留订单失败: %v", scanErr)
			return nil
		}
		list = append(list, o)
	}
	return list
}

// ListActiveOrders 查询全部进行中订单(1已下单/2制作中/3已上齐),按 order_id 升序。
// 桌台看板一次性取出后按 table_id 分组取最新一笔,避免逐桌 GetActiveOrderByTable 的 N+1。
func ListActiveOrders(guardBizDate string) ([]po.Order, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	rows, err := store.DB.Query(`SELECT `+OrderCols+` FROM tb_order WHERE order_status IN (1,2,3)`+guard+` ORDER BY order_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// OrderQuery 订单列表筛选条件;零值字段表示不限。
type OrderQuery struct {
	OrderStatus  string // 空表示不限;字符串绑定,与历史行为一致
	PayStatus    *int   // nil 表示不限
	OrderNo      string // 模糊匹配
	TableNo      string // 精确匹配
	TableName    string // 模糊匹配
	SettleType   string // normal/free/credit
	CreditStatus *int   // nil 表示不限
	// SettleRemark 结算备注模糊匹配(免单原因/挂账人)。
	// 挂账人是自由文本,必须在服务端过滤:前端只过滤当前页会让第 2 页起的命中记录
	// 永远搜不到,同时把分页总数改成「本页命中数」,页数与内容完全对不上。
	SettleRemark string
	// NewOnly 只看新单:已下单(order_status=1)且商家从未操作过(viewed_at IS NULL)。
	NewOnly bool
}

// ListOrders 按条件分页查询订单列表。
func ListOrders(q OrderQuery, pageNum, pageSize int) (total int, list []po.Order, err error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if q.OrderStatus != "" {
		where = append(where, "order_status=?")
		args = append(args, q.OrderStatus)
	}
	if q.PayStatus != nil {
		// 数值型字段必须以整数入参:SQLite 下 COALESCE(...) 表达式会丢失列亲和性,
		// 用字符串 "1" 去比整数列会恒不相等。
		where = append(where, "pay_status=?")
		args = append(args, *q.PayStatus)
	}
	if q.OrderNo != "" {
		where = append(where, "order_no LIKE ? ESCAPE '\\'")
		args = append(args, store.LikeContains(q.OrderNo))
	}
	if q.TableNo != "" {
		where = append(where, "table_no=?")
		args = append(args, q.TableNo)
	}
	// 桌台名称模糊匹配:桌号只是「01/02」这类编号,收银认的是「大厅01桌」这类名称,
	// 且桌台可改名,历史单上留的是改名前的快照 —— 必须服务端过滤,
	// 前端只过滤当前页会让第 2 页起的命中记录永远搜不到。
	if q.TableName != "" {
		where = append(where, "COALESCE(table_name,'') LIKE ? ESCAPE '\\'")
		args = append(args, store.LikeContains(q.TableName))
	}
	// 结算方式筛选:normal 正常收款 / free 免单 / credit 挂账
	if q.SettleType != "" {
		where = append(where, "COALESCE(settle_type,'normal')=?")
		args = append(args, q.SettleType)
	}
	// 挂账状态筛选:0 非挂账 1 待收款 2 已结清
	if q.CreditStatus != nil {
		where = append(where, "COALESCE(credit_status,0)=?")
		args = append(args, *q.CreditStatus)
	}
	// 结算备注(免单原因/挂账人)模糊匹配:必须与 COUNT 用同一 cond,
	// 否则总数与实际列表口径不一致,分页器会出现「翻不到的空页」。
	if q.SettleRemark != "" {
		where = append(where, "COALESCE(settle_remark,'') LIKE ? ESCAPE '\\'")
		args = append(args, store.LikeContains(q.SettleRemark))
	}
	// 只看新单:「已下单且商家从未操作过」。判定必须与读侧完全对齐 —— dao.TS 会把
	// NULL/空串/MySQL 历史零值都折叠成「未操作」,这里若只判 IS NULL,就会出现
	// 「角标显示新单、筛选取不到」的分裂(实际发生过)。
	if q.NewOnly {
		where = append(where, "order_status=1",
			"(viewed_at IS NULL OR viewed_at='' OR viewed_at LIKE '0000-00-00%')")
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+OrderCols+` FROM tb_order`+cond+` ORDER BY order_id DESC LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, nil, err
	}
	defer rows.Close()
	list = []po.Order{}
	for rows.Next() {
		o, scanErr := ScanOrder(rows)
		if scanErr != nil {
			return 0, nil, scanErr // 单行失败必须报错,静默丢弃等于丢单
		}
		list = append(list, o)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// GetOrderTotal 查询订单应收总额;查询失败时返回 0。
//
// 只用于「金额不参与账务决策」的展示场景。涉及审计/对账等需要区分
// 「金额真的是 0」与「查询失败」的场景,请用 GetOrderTotalE。
func GetOrderTotal(orderID int) int64 {
	total, _ := GetOrderTotalE(orderID)
	return total
}

// GetOrderTotalE 与 GetOrderTotal 相同,但把查询错误如实返回。
// 改单审计摘要要写「改单前应收 ¥X → 改单后 ¥Y」,静默折叠成 0 会让
// 让利/调价的审计记录凭空出现「¥0.00」这一假象,事后无法据此对账。
func GetOrderTotalE(orderID int) (int64, error) {
	var total int64
	if err := store.DB.QueryRow(`SELECT COALESCE(total_amount,0) FROM tb_order WHERE order_id=?`, orderID).
		Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// GetOrderAppendInfo 查询顾客加菜所需的订单状态信息。
//
// 只填充 po.Order 的 OrderID/TableID/PersonCount/OrderStatus/PayStatus 五列,其余为零值。
func GetOrderAppendInfo(orderNo string) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_id, table_id, person_count, order_status, pay_status
		FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&o.OrderID, &o.TableID, &o.PersonCount, &o.OrderStatus, &o.PayStatus)
	return o, err
}

// GetUrgeTarget 查询顾客催菜所需的订单与桌台信息。
//
// 只填充 po.Order 的 OrderID/TableID/OrderStatus/TableNo/TableName 五列,其余为零值。
func GetUrgeTarget(orderNo string) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_id, table_id, order_status, table_no, table_name
		FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&o.OrderID, &o.TableID, &o.OrderStatus, &o.TableNo, &o.TableName)
	return o, err
}

// OrderSettleInfo 订单结算信息(用于免单/挂账/核销前校验)。
type OrderSettleInfo struct {
	OrderStatus   int
	PayStatus     int
	SettleType    string
	CreditStatus  int
	TotalCents    int64
	CreditCents   int64
	PaidCents     int64
	RefundCents   int64
	OrderNo       string
	PersonCount   int
	TableID       int
	TableNo       string
	TableName     string
	OrderRemark   string
	DishCents     int64
	SeatCents     int64
	DiscountCents int64
	CreateTime    string
	// PayChannel 非空表示订单经在线渠道(微信/支付宝)支付;
	// 此类订单撤销结算必须走退款流程(渠道侧资金需原路退回),不能简单回退支付状态。
	PayChannel string
	// PreSettleStatus 结账时的订单状态快照(1/2/3);0 表示历史数据未记录。
	PreSettleStatus int
}

// GetOrderSettleInfo 读取订单结算关键信息。
func GetOrderSettleInfo(orderID int) (OrderSettleInfo, error) {
	var s OrderSettleInfo
	err := store.DB.QueryRow(`SELECT order_status, pay_status, COALESCE(settle_type,'normal'),
		COALESCE(credit_status,0), total_amount, COALESCE(credit_amount,0), COALESCE(paid_amount,0),
		COALESCE(refund_amount,0), order_no, person_count, table_id, table_no, table_name,
		order_remark, dish_amount, seat_fee, discount_amount, create_time,
		COALESCE(pay_channel,''), COALESCE(pre_settle_status,0)
		FROM tb_order WHERE order_id=?`, orderID).
		Scan(&s.OrderStatus, &s.PayStatus, &s.SettleType, &s.CreditStatus, &s.TotalCents, &s.CreditCents,
			&s.PaidCents, &s.RefundCents, &s.OrderNo, &s.PersonCount, &s.TableID, &s.TableNo, &s.TableName,
			&s.OrderRemark, &s.DishCents, &s.SeatCents, &s.DiscountCents, &s.CreateTime,
			&s.PayChannel, &s.PreSettleStatus)
	return s, err
}

// LoadOrderItems 加载订单明细。
//
// 展示/打印路径的便捷封装:这类调用方无法向上传递 error,读失败时记 warn 后
// 返回空明细(至少留下日志,不再「明细静默变空」)。需要 error 语义(如事务内
// 金额重算)的调用方请直接用 LoadOrderItemsTx。
func LoadOrderItems(orderID int) []po.OrderItem {
	items, err := LoadOrderItemsTx(store.DB, orderID)
	if err != nil {
		logger.Warnf("[order] 加载订单 %d 明细失败: %v", orderID, err)
		return nil
	}
	return items
}

// LoadOrderItemsTx 在指定执行器(DB 或事务)上加载订单明细。
// 扫描失败返回错误:调用方(如加菜金额重算)基于旧明细重算金额,读失败会算出
// 错误金额,必须显式失败而不是静默拿空明细继续。
func LoadOrderItemsTx(q interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}, orderID int) ([]po.OrderItem, error) {
	rows, err := q.Query(`SELECT item_id, order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark, batch_no, client_request_id, is_gift, gift_reason
		FROM tb_order_item WHERE order_id=? ORDER BY item_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []po.OrderItem{}
	for rows.Next() {
		var it po.OrderItem
		if err := rows.Scan(&it.ItemID, &it.OrderID, &it.DishID, &it.DishName, &it.SpecID, &it.SpecName, &it.Price, &it.Quantity, &it.Amount, &it.Remark, &it.BatchNo, &it.ClientRequestID, &it.IsGift, &it.GiftReason); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// LoadOrderItemsByOrderIDs 批量加载多个订单的明细,key 为 order_id。
// 看板一次性取全部进行中订单的明细时使用,避免逐单 LoadOrderItems 的 N+1 查询。
func LoadOrderItemsByOrderIDs(orderIDs []int) (map[int][]po.OrderItem, error) {
	out := map[int][]po.OrderItem{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]interface{}, 0, len(orderIDs))
	for i, id := range orderIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT item_id, order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark, batch_no, client_request_id, is_gift, gift_reason
		FROM tb_order_item WHERE order_id IN (`+placeholders+`) ORDER BY order_id, item_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it po.OrderItem
		if err := rows.Scan(&it.ItemID, &it.OrderID, &it.DishID, &it.DishName, &it.SpecID, &it.SpecName, &it.Price, &it.Quantity, &it.Amount, &it.Remark, &it.BatchNo, &it.ClientRequestID, &it.IsGift, &it.GiftReason); err != nil {
			return nil, err
		}
		out[it.OrderID] = append(out[it.OrderID], it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrderTable 返回订单所属桌台,若订单不存在返回错误。
func GetOrderTable(orderID int) (int, error) {
	var tableID int
	err := store.DB.QueryRow(`SELECT table_id FROM tb_order WHERE order_id=?`, orderID).Scan(&tableID)
	return tableID, err
}

// GetOrderState 返回订单的状态与支付状态,用于状态机流转校验。
// 只填充 po.Order 的 OrderStatus/PayStatus 两列,其余为零值。
func GetOrderState(orderID int) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_id=?`, orderID).
		Scan(&o.OrderStatus, &o.PayStatus)
	return o, err
}

// CountActiveOrdersTx 统计桌台进行中的订单数(事务内防重复下单校验用)。
func CountActiveOrdersTx(tx *sql.Tx, tableID int) (int, error) {
	var active int
	err := tx.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE table_id=? AND order_status IN (1,2,3)`, tableID).Scan(&active)
	return active, err
}

// CountUnpaidOrdersTx 统计桌台「未结账」订单数(下单防重用):
// 付款前的订单才算未结账,已付款但出餐未完成的订单不阻断新单。
func CountUnpaidOrdersTx(tx *sql.Tx, tableID int, guardBizDate string) (int, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE table_id=? AND pay_status=0 AND order_status IN (1,2,3)`+guard,
		append([]interface{}{tableID}, args...)...).Scan(&n)
	return n, err
}

// CountUnfinishedOrdersTx 统计桌台「未完成出餐」的订单数(桌台释放判定用):
// 只要还有订单在出餐流程中(1/2/3),桌台就仍视为占用。
//
// 必须与顾客端读口径一致地带营业日隔离:否则跨日遗留单会让桌台永远释放不掉 ——
// 新客人正常吃完结账,桌台却因为前一天那笔旧单一直显示占用。
func CountUnfinishedOrdersTx(tx *sql.Tx, tableID int, guardBizDate string) (int, error) {
	guard, args := bizDateGuardSQL(guardBizDate)
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE table_id=? AND order_status IN (1,2,3)`+guard,
		append([]interface{}{tableID}, args...)...).Scan(&n)
	return n, err
}

// GetOrderIDByRequestIDTx 按幂等键(client_request_id)查询订单 ID(下单去重用)。
// 幂等键冗余存储在 tb_order_item 上:同一次提交的所有明细共享同一请求号。
func GetOrderIDByRequestIDTx(tx *sql.Tx, requestID string) (int, bool) {
	var orderID int
	err := tx.QueryRow(`SELECT order_id FROM tb_order_item WHERE client_request_id=? LIMIT 1`, requestID).Scan(&orderID)
	if err != nil {
		return 0, false
	}
	return orderID, true
}

// HasOrderItemRequestTx 判断订单是否已存在携带指定幂等键的明细(加菜去重用)。
func HasOrderItemRequestTx(tx *sql.Tx, orderID int, requestID string) (bool, error) {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM tb_order_item WHERE order_id=? AND client_request_id=? LIMIT 1`, orderID, requestID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// TouchOrderTx 对订单行做自赋值 no-op 更新,在 MySQL/SQLite 上先取该行写锁,
// 把同一订单的后续校验/写入在行上串行化(消除 check-then-act 竞态窗口)。
func TouchOrderTx(tx *sql.Tx, orderID int) error {
	_, err := tx.Exec(`UPDATE tb_order SET update_time=update_time WHERE order_id=?`, orderID)
	return err
}

// UpdateOrderStatusTx 在事务中更新订单状态,并用当前状态守卫并发变更。
//
// 置为已完成(4)时同步写 finish_time:报表的「今日完成订单数」(CountFinishedIn)按
// finish_time 统计,不写该字段会让走本函数的归档订单永远不计入完成数(与 FinishOrderTx
// 口径不一致,同一个「已完成」在不同入口下含义不同)。
// 同时写 meal_end_time:这是「用餐结束」的落地点之一(另两处见 SettleOrderTx / FinishOrderTx)。
//
// 支付维度守卫(service 侧的校验是事务外快照,存在 check-then-act 窗口,SQL 守卫
// 才是原子兜底):
//   - 目标为已完成(4):要求 pay_status=1 —— 未收款订单不能被「完成」归档,否则漏收;
//   - 目标为已取消(5):要求 pay_status=0 —— 已收款订单不能被「取消」作废,否则
//     钱已收、单已废,且取消路径还会回补库存造成超卖(与 CancelOrderTx 的守卫一致);
//   - 其余目标(1/2/3 进行中):不设支付守卫 —— 已收款订单仍可正常流转上齐/出餐。
func UpdateOrderStatusTx(tx *sql.Tx, orderID, status int, operator string, curStatus int) (int64, error) {
	now := store.Now()
	payGuard := ""
	switch status {
	case 4: // 4 = 已完成(po.OrderStatusFinished 的字面值,dao 层不反向依赖 service)
		payGuard = " AND pay_status=1"
	case 5: // 5 = 已取消(po.OrderStatusCanceled 的字面值)
		payGuard = " AND pay_status=0"
	}
	if status == 4 {
		res, err := tx.Exec(`UPDATE tb_order SET order_status=?, finish_time=?, meal_end_time=?, update_by=?, update_time=?
			WHERE order_id=? AND order_status=?`+payGuard,
			status, now, now, operator, now, orderID, curStatus)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	res, err := tx.Exec(`UPDATE tb_order SET order_status=?, update_by=?, update_time=? WHERE order_id=? AND order_status=?`+payGuard,
		status, operator, now, orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetOrderCancelReasonTx 在事务中补写取消原因。
// 供「/order/status 直转取消(5)」这条入口使用:它与 /order/cancel 是同一业务动作的
// 两个入口,必须都记录原因,否则那条路径取消的订单在列表/详情里看不到取消原因。
func SetOrderCancelReasonTx(tx *sql.Tx, orderID int, reason string) error {
	_, err := tx.Exec(`UPDATE tb_order SET cancel_reason=?, update_time=? WHERE order_id=?`,
		reason, store.Now(), orderID)
	return err
}

// MarkOrderPaid 将进行中且未支付订单标记为正常收款(现金/线下)。
//
// 与 SettleOrderTx 不同,现金收款不归档(order_status 仍为 1/2/3),因此需要同步把
// 收款前的订单状态记入 pre_settle_status,撤销结算时才能按收款前状态恢复;
// 否则该字段为空,撤销方只能硬编码回 3,丢失「已下单/制作中」的真实状态。
func MarkOrderPaid(orderID int, payType, operator string) (int64, error) {
	res, err := store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, settle_type=?, settle_time=?,
		settle_operator=?, paid_amount=total_amount-COALESCE(refund_amount,0), credit_status=0, credit_amount=0,
		pre_settle_status=order_status, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		payType, store.Now(), "normal", store.Now(), operator, operator, store.Now(), orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SettleOrderParams 订单结算回写参数(由 handler 计算好业务字段后传入)。
type SettleOrderParams struct {
	SettleType   string // 结算方式(normal/free/credit)
	PayType      string // 展示用支付方式(免单/挂账时为对应文案)
	SettleRemark string // 结算备注(免单原因/挂账事由)
	Operator     string // 操作人
	Now          string // 操作时间(事务外取一次,保证各字段时间一致)
	CreditStatus int    // 挂账状态(0 非挂账/1 待收款/2 已结清)
	CreditCents  int64  // 挂账金额(分)
	PaidCents    int64  // 实收金额(分)
	PreStatus    int    // 结账前订单状态快照(1/2/3)
}

// SettleOrderTx 在事务中结算订单并记录结算前状态快照。
//
// 结账即「这桌吃到这一刻为止」,因此与 finish_time 一并写 meal_end_time。
func SettleOrderTx(tx *sql.Tx, orderID int, p SettleOrderParams) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?,
		settle_type=?, settle_time=?, settle_operator=?, settle_remark=?,
		credit_status=?, credit_amount=?, paid_amount=?,
		order_status=4, pre_settle_status=?, finish_time=?, meal_end_time=?, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		p.PayType, p.Now, p.SettleType, p.Now, p.Operator, p.SettleRemark,
		p.CreditStatus, p.CreditCents, p.PaidCents,
		p.PreStatus, p.Now, p.Now, p.Operator, p.Now, orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SettleCreditOrder 核销挂账订单;credit_status=1 表示待收款,核销后置为 2(已结清)。
func SettleCreditOrder(orderID int, due int64, payType, operator string) (int64, error) {
	now := store.Now()
	res, err := store.DB.Exec(`UPDATE tb_order SET credit_status=?, credit_settle_time=?, credit_settle_by=?,
		pay_type=?, paid_amount=?, update_by=?, update_time=? WHERE order_id=? AND credit_status=?`,
		2, now, operator, payType, due, operator, now, orderID, 1)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CancelSettleTx 在事务中撤销订单结算并恢复结算前订单状态。
//
// 守卫放宽为 pay_status=1 AND order_status IN (1,2,3,4):
//   - 4 对应 SettleOrderTx 归档后的已结算订单(免单/挂账/normal 结账);
//   - 1/2/3 对应 PayOrder(现金收款)只置 pay_status=1、不归档的订单。这类订单此前被
//     order_status=4 的守卫挡住,UPDATE 命中 0 行,撤销结算对其永远不可用。
//
// 另外两条守卫与业务校验同构,但落在 SQL 里以获得原子性(service 侧的
// GetOrderSettleInfo 是事务外读,存在 check-then-act 窗口):
//   - COALESCE(credit_status,0)<>2:已核销的挂账不可撤销 —— 否则会把已收回的欠款
//     (credit_settle_time/credit_settle_by/paid_amount)连带清零,钱收了账上却没痕迹;
//   - COALESCE(refund_amount,0)=0:已发生退款的订单不可撤销 —— 否则实收金额归零,
//     与渠道侧已退出去的钱对不上。
//
// 恢复状态由调用方按 pre_settle_status 决定(现金收款已记录收款前状态;历史未记录时回退 3)。
func CancelSettleTx(tx *sql.Tx, orderID int, settleType, reason string, restoreStatus int, operator, now string) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET pay_status=0, pay_type=NULL, pay_time=NULL,
		settle_type=?, settle_time=NULL, settle_operator='', settle_remark=?,
		credit_status=0, credit_amount=0, credit_settle_time=NULL, credit_settle_by='',
		paid_amount=0, finish_time=NULL, meal_end_time=NULL, order_status=?, update_by=?, update_time=?
		WHERE order_id=? AND pay_status=1 AND order_status IN (1,2,3,4)
		  AND COALESCE(credit_status,0)<>2 AND COALESCE(refund_amount,0)=0`,
		settleType, reason, restoreStatus, operator, now, orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FinishOrderTx 在事务中将订单归档为已完成,并用当前状态守卫并发变更。
//
// 归档同样是一次「用餐结束」,写 meal_end_time(与 SettleOrderTx 同口径)。
//
// pay_status=1 守卫:完成是对「已支付」订单的收尾归档。service 侧的未支付校验是
// 事务外快照,若并发收款/撤销结算(撤销会把 pay_status 打回 0)落在校验与写入之间,
// 没有这道 SQL 守卫就会把一笔未收款订单归档成已完成 —— 报表不计营收,钱却永远
// 收不回来(漏收)。
func FinishOrderTx(tx *sql.Tx, orderID int, operator string, curStatus int) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET order_status=4, finish_time=?, meal_end_time=?, update_by=?, update_time=? WHERE order_id=? AND order_status=? AND pay_status=1`,
		store.Now(), store.Now(), operator, store.Now(), orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CancelOrderTx 在事务中取消未支付订单,并用当前状态守卫并发变更。
func CancelOrderTx(tx *sql.Tx, orderID int, reason, operator string, curStatus int) (int64, error) {
	now := store.Now()
	// 取消也是一次「占用结束」:桌台随之为下一桌腾出来,故写 meal_end_time。
	// 报表口径仍只统计 order_status=4 的已完成订单,取消单不会污染用餐时长均值。
	res, err := tx.Exec(`UPDATE tb_order SET order_status=5, cancel_reason=?, meal_end_time=?, update_by=?, update_time=? WHERE order_id=? AND order_status=? AND pay_status=0`,
		reason, now, operator, now, orderID, curStatus)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateOrderForEditTx 在事务中更新订单基础金额信息,仅允许进行中且未支付订单。
func UpdateOrderForEditTx(tx *sql.Tx, orderID, personCount int, remark string, dishCents, seatCents, discountCents, giftCents, totalCents int64, operator string) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_order SET person_count=?, order_remark=?, dish_amount=?, seat_fee=?, discount_amount=?, gift_amount=?, total_amount=?, update_by=?, update_time=? WHERE order_id=? AND pay_status=0 AND order_status IN (1,2,3)`,
		personCount, remark, dishCents, seatCents, discountCents, giftCents, totalCents, operator, store.Now(), orderID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReplaceOrderItemsTx 在事务中替换订单明细。
func ReplaceOrderItemsTx(tx *sql.Tx, orderID int, items []po.OrderItem) error {
	if _, err := tx.Exec(`DELETE FROM tb_order_item WHERE order_id=?`, orderID); err != nil {
		return err
	}
	for _, it := range items {
		if err := InsertOrderItemTx(tx, int64(orderID), it); err != nil {
			return err
		}
	}
	return nil
}

// pickupNoRetryMax NextPickupNoTx 的取号重试次数上限。
// 只有「当天首单 + 并发插入同一计数行」才会触发重试,2~3 次足以覆盖;
// 超出只可能是主键持续冲突等异常情况,此时应让下单失败暴露问题,而不是给出重复号。
const pickupNoRetryMax = 3

// NextPickupNoTx 在事务内取下一个当日取餐号(当天首单为 1,之后依次 +1)。
//
// 必须在事务里调用,原因有二:
//  1. UPDATE 会对该营业日的计数行上行锁,把并发下单在本行上串行化
//     —— 若放在事务外,MIN间隙里两个连接会拿到同一个号;
//  2. 事务回滚时自增一并回滚,不会出现「号被占用却查不到订单」的空洞。
//
// 流程是「先自增已有行、行不存在再插入初值」,两种数据库语义一致,
// 无需依赖 MySQL 的 ON DUPLICATE KEY / LAST_INSERT_ID 或 SQLite 的 ON CONFLICT。
func NextPickupNoTx(tx *sql.Tx, bizDate string) (int, error) {
	if bizDate == "" {
		return 0, errors.New("营业日为空,无法取餐号")
	}
	for i := 0; i < pickupNoRetryMax; i++ {
		// 1) 已存在计数行:自增后读回。自增在同一事务内立即可见。
		res, err := tx.Exec(`UPDATE tb_day_seq SET next_val = next_val + 1, update_time = ? WHERE biz_date = ?`, store.Now(), bizDate)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			var next int
			if err := tx.QueryRow(`SELECT next_val FROM tb_day_seq WHERE biz_date = ?`, bizDate).Scan(&next); err != nil {
				return 0, err
			}
			if next <= 0 {
				return 0, fmt.Errorf("取餐号计数器异常: biz_date=%s next_val=%d", bizDate, next)
			}
			return next, nil
		}
		// 2) 当天首单:计数行还不存在,插入初值 1。
		//    并发的另一个事务可能刚插进去 → 主键冲突,回到步骤 1 重试自增。
		if _, err := tx.Exec(`INSERT INTO tb_day_seq(biz_date, next_val, create_time) VALUES(?, 1, ?)`, bizDate, store.Now()); err != nil {
			continue
		}
		return 1, nil
	}
	return 0, fmt.Errorf("取餐号取号失败(重试 %d 次未成功): biz_date=%s", pickupNoRetryMax, bizDate)
}

// InsertOrderTx 在事务中新增食客订单并返回主键 ID。
// bizDate/pickupNo 为当日取餐号(见 NextPickupNoTx),由下单流程在事务内取号后传入。
// customerID 为下单会员的归属(0 = 匿名),只认服务端从顾客令牌解析出的值,请求体不可带。
//
// 同时写入 meal_start_time(= 此刻,即用餐开始):用餐时长的起点在下单那一刻定下来,
// 后续桌台页/报表都以它为锚,不必再用 create_time 兜底。
func InsertOrderTx(tx *sql.Tx, orderNo, bizDate string, pickupNo, tableID, personCount, customerID int, tableNo, tableName, orderRemark string, dishCents, seatCents, discountCents, giftCents, totalCents int64) (int64, error) {
	now := store.Now()
	res, err := tx.Exec(`INSERT INTO tb_order(order_no, biz_date, pickup_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, gift_amount, total_amount, pay_status, customer_id, order_remark, create_by, create_time, update_by, update_time,
		meal_start_time)
		VALUES(?,?,?,?,?,?,?,1,?,?,?,?,?,0,?,?,?,?,?,?,?)`,
		orderNo, bizDate, pickupNo, tableID, tableNo, tableName, personCount,
		dishCents, seatCents, discountCents, giftCents, totalCents, customerID, orderRemark,
		"食客", now, "食客", now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertOrderItemTx 在事务中新增订单明细。批次(BatchNo)未显式指定时默认 1(首次下单);
// 幂等键(ClientRequestID)冗余写入明细,用于下单/加菜去重。
func InsertOrderItemTx(tx *sql.Tx, orderID int64, it po.OrderItem) error {
	batch := it.BatchNo
	if batch <= 0 {
		batch = 1
	}
	_, err := tx.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark, batch_no, client_request_id, is_gift, gift_reason, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, orderID, it.DishID, it.DishName, it.SpecID, it.SpecName, it.Price, it.Quantity, it.Amount, it.Remark, batch, it.ClientRequestID, it.IsGift, it.GiftReason, store.Now())
	return err
}

// GetOrderStateTx 在事务中重读订单状态、支付状态与用餐人数。
// 只填充 po.Order 的 OrderStatus/PayStatus/PersonCount 三列,其余为零值。
func GetOrderStateTx(tx *sql.Tx, orderID int) (po.Order, error) {
	var o po.Order
	err := tx.QueryRow(`SELECT order_status, pay_status, person_count FROM tb_order WHERE order_id=?`, orderID).
		Scan(&o.OrderStatus, &o.PayStatus, &o.PersonCount)
	return o, err
}

// CountOrderBatches 批量统计各订单的加菜批次数(明细 distinct batch_no)。
// 供订单列表展示「加菜」角标:一次 IN 查询覆盖整页,与列表「不逐单查明细(N+1)」的
// 口径一致(见 handler.OrderList);查询失败静默返回空 map,列表只是不打角标,不影响出单。
func CountOrderBatches(orderIDs []int) map[int]int {
	out := map[int]int{}
	if len(orderIDs) == 0 {
		return out
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(orderIDs)), ",")
	args := make([]interface{}, 0, len(orderIDs))
	for _, id := range orderIDs {
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT order_id, COUNT(DISTINCT batch_no) FROM tb_order_item
		WHERE order_id IN (`+placeholders+`) GROUP BY order_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// UpdateOrderAmountTx 在事务中更新订单金额。
func UpdateOrderAmountTx(tx *sql.Tx, orderID int, dishCents, seatCents, discountCents, giftCents, totalCents int64) error {
	_, err := tx.Exec(`UPDATE tb_order SET dish_amount=?, seat_fee=?, discount_amount=?, gift_amount=?, total_amount=?, update_time=? WHERE order_id=?`,
		dishCents, seatCents, discountCents, giftCents, totalCents, store.Now(), orderID)
	return err
}

// UpdateOrderAmountAndPersonTx 在事务中更新订单金额与用餐人数(加菜时人数可变)。
func UpdateOrderAmountAndPersonTx(tx *sql.Tx, orderID, personCount int, dishCents, seatCents, discountCents, giftCents, totalCents int64) error {
	_, err := tx.Exec(`UPDATE tb_order SET person_count=?, dish_amount=?, seat_fee=?, discount_amount=?, gift_amount=?, total_amount=?, update_time=? WHERE order_id=?`,
		personCount, dishCents, seatCents, discountCents, giftCents, totalCents, store.Now(), orderID)
	return err
}

// MarkOrderPaidByChannel 根据在线支付渠道回写订单支付状态。
//
// 返回 changed 表示「本方法真正把订单从待支付置为已支付」:条件 UPDATE 带
// WHERE pay_status=0,若订单已在并发窗口内被其它渠道/线下收款先行置为已支付,
// 该行不命中,rowsAffected 为 0,changed=false。调用方(ApplyPaymentSuccess)据此
// 判定本笔为重复支付并原路退回,消除「流水已置已支付、订单却非本笔入账」的竞态。
//
// 字段口径与现金收款 MarkOrderPaid 严格一致 —— 尤其是 paid_amount:
// 报表的营业额(RangeAmount/DailyTrendStats/HourlyStats/SettleMixStats)与结账小票
// 的「实收」全部只认 paid_amount,若在线支付不回写它,顾客扫码付的每一笔都会被
// 计成 0 元(只有下次启动的历史回填才会补上,当天数据全程是错的)。
//
// 守卫加 order_status IN (1,2,3):关渠道单是尽力而为(关单失败只记日志),取消/改单
// 与支付回调并发时,晚到的成功回调不能把已作废(order_status=5)的订单置为已支付。
// 该行不命中时返回 changed=false,由调用方按重复支付/掉单告警处理。
func MarkOrderPaidByChannel(orderNo, payType, transactionID, channel string) (bool, error) {
	now := store.Now()
	res, err := store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?,
		transaction_id=?, pay_channel=?, settle_type='normal', settle_time=?,
		paid_amount=total_amount-COALESCE(refund_amount,0),
		credit_status=0, credit_amount=0, pre_settle_status=order_status, update_time=?
		WHERE order_no=? AND pay_status=0 AND order_status IN (1,2,3)`,
		payType, now, transactionID, channel, now, now, orderNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkOrderPaidByChannelTx 是 MarkOrderPaidByChannel 的事务版本,字段口径完全一致。
//
// 余额支付在同一个事务里完成「扣储值余额 → 落支付流水 → 订单入账」,
// 三者要么同时生效要么同时回滚 —— 渠道支付靠回调重试兜底,余额扣掉后没有重试,
// 事务是唯一保障。
func MarkOrderPaidByChannelTx(tx *sql.Tx, orderNo, payType, transactionID, channel string) (bool, error) {
	now := store.Now()
	res, err := tx.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?,
		transaction_id=?, pay_channel=?, settle_type='normal', settle_time=?,
		paid_amount=total_amount-COALESCE(refund_amount,0),
		credit_status=0, credit_amount=0, pre_settle_status=order_status, update_time=?
		WHERE order_no=? AND pay_status=0 AND order_status IN (1,2,3)`,
		payType, now, transactionID, channel, now, now, orderNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
