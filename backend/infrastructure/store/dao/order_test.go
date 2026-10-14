package dao

import (
	"database/sql"
	"path/filepath"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// orderDaoInitDB 初始化订单测试用的临时库。
func orderDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "order.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// orderDaoCreate 用 InsertOrderTx 创建一笔进行中、未支付订单,返回订单 ID。
func orderDaoCreate(t *testing.T, orderNo string, tableID int) int {
	t.Helper()
	return orderDaoCreateAt(t, orderNo, tableID, "01")
}

// orderDaoCreateAt 与 orderDaoCreate 相同,但可指定桌号。
func orderDaoCreateAt(t *testing.T, orderNo string, tableID int, tableNo string) int {
	t.Helper()
	var id int64
	if err := store.WithTx(func(tx *sql.Tx) error {
		// 走真实取号路径:既保证 (biz_date, pickup_no) 唯一索引不被同一个号撞上,
		// 也让这批用例顺带覆盖 NextPickupNoTx。
		bizDate := store.Now()[:10]
		pickupNo, err := NextPickupNoTx(tx, bizDate)
		if err != nil {
			return err
		}
		id, err = InsertOrderTx(tx, orderNo, bizDate, pickupNo, tableID, 4, 0, tableNo, "测试桌", "少辣", 3000, 100, 200, 0, 2900)
		return err
	}); err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	return int(id)
}

func TestOrderInsertAndScanRoundTrip(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-TEST-0001", 1)

	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("按 ID 查询订单失败: %v", err)
	}
	if o.OrderNo != "D-TEST-0001" || o.TableID != 1 || o.PersonCount != 4 {
		t.Fatalf("订单回读异常: %+v", o)
	}
	if o.OrderStatus != 1 || o.PayStatus != 0 {
		t.Fatalf("新订单应为进行中未支付, got status=%d pay=%d", o.OrderStatus, o.PayStatus)
	}
	if o.SettleType != "normal" {
		t.Fatalf("settle_type 应兜底为 normal, got %q", o.SettleType)
	}
	// 未支付时支付时间应为 nil(保留 NULL 语义)。
	if o.PayTime != nil || o.RefundTime != nil {
		t.Fatalf("未支付订单的可空时间字段应为 nil, payTime=%v refundTime=%v", o.PayTime, o.RefundTime)
	}

	byNo, err := GetOrderByNo("D-TEST-0001")
	if err != nil {
		t.Fatalf("按单号查询订单失败: %v", err)
	}
	if byNo.OrderID != id {
		t.Fatalf("按单号回读 ID 不一致: %d vs %d", byNo.OrderID, id)
	}

	if _, err := GetOrderByID(999999); err == nil {
		t.Fatal("查询不存在的订单应报错")
	}
}

func TestGetActiveOrderByTable(t *testing.T) {
	orderDaoInitDB(t)

	if _, ok := GetActiveOrderByTable(1, ""); ok {
		t.Fatal("无进行中订单时应返回 ok=false")
	}

	// 插入两笔:较早的进行中 + 较新的已完成,应返回进行中那笔。
	id1 := orderDaoCreate(t, "D-ACTIVE-0001", 1)
	orderDaoCreate(t, "D-ACTIVE-0002", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := FinishOrderTx(tx, id1, "boss", 1)
		return err
	}); err != nil {
		t.Fatalf("归档订单失败: %v", err)
	}

	active, ok := GetActiveOrderByTable(1, "")
	if !ok {
		t.Fatal("应存在进行中订单")
	}
	if active.OrderNo != "D-ACTIVE-0002" {
		t.Fatalf("应返回最近的进行中订单, got %s", active.OrderNo)
	}
}

// TestActiveOrderQueriesRespectBizDateGuard 营业日隔离:带隔离值时,更早营业日的
// 订单不再被当作「本桌进行中」—— 这是跨日自动清台不让旧单串给新客人的核心。
func TestActiveOrderQueriesRespectBizDateGuard(t *testing.T) {
	orderDaoInitDB(t)

	// 一笔昨天的未结账进行中订单
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, biz_date,
		order_status, total_amount, pay_status, create_time, update_time)
		VALUES('D-STALE-0001', 7, '07', '旧桌', '2026-01-01', 3, 5000, 0, '2026-01-01 12:00:00', '2026-01-01 12:00:00')`); err != nil {
		t.Fatalf("插入跨日订单失败: %v", err)
	}
	// 一笔不明营业日(biz_date 为空)的进行中订单,挂在另一桌
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, biz_date,
		order_status, total_amount, pay_status, create_time, update_time)
		VALUES('D-NODATE-0001', 8, '08', '无日期桌', NULL, 3, 6000, 0, '2026-01-01 12:00:00', '2026-01-01 12:00:00')`); err != nil {
		t.Fatalf("插入无日期订单失败: %v", err)
	}

	// 不隔离(传空串):两笔都能查到,行为与历史一致
	if _, ok := GetActiveOrderByTable(7, ""); !ok {
		t.Fatal("不隔离时应能看到跨日订单")
	}
	if _, ok := GetUnpaidOrderByTable(7, ""); !ok {
		t.Fatal("不隔离时应能看到跨日未结账订单")
	}

	// 隔离到今天:昨天的单被挡掉
	if _, ok := GetActiveOrderByTable(7, "2026-10-13"); ok {
		t.Fatal("隔离后不应再返回更早营业日的订单")
	}
	if _, ok := GetUnpaidOrderByTable(7, "2026-10-13"); ok {
		t.Fatal("隔离后不应再返回更早营业日的未结账订单")
	}
	if n := len(ListServingOrdersByTable(7, "2026-10-13")); n != 0 {
		t.Fatalf("隔离后不应再返回更早营业日的出餐中订单, got %d", n)
	}

	// biz_date 为空的行无从判定归属,一律放行(保守:宁可留给店家人工处理,
	// 也不能把一张正在用餐的桌判成空闲)
	if _, ok := GetActiveOrderByTable(8, "2026-10-13"); !ok {
		t.Fatal("biz_date 为空的订单不应被隔离")
	}

	// 清台巡检要能找出「更早营业日仍有进行中订单」的桌台;无日期的桌不算在内
	ids := ListStaleTableIDs("2026-10-13")
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("应只找出桌台 7, got %v", ids)
	}
}

func TestListOrdersFiltersAndPagination(t *testing.T) {
	orderDaoInitDB(t)

	orderDaoCreateAt(t, "D-LIST-0001", 1, "01")
	orderDaoCreateAt(t, "D-LIST-0002", 2, "02")
	// 已支付订单(settle_type 为 normal)。
	paidID := orderDaoCreateAt(t, "D-LIST-0003", 3, "03")
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := SettleOrderTx(tx, paidID, SettleOrderParams{SettleType: "normal", PayType: "现金", Operator: "boss", Now: store.Now(), PaidCents: 2900, PreStatus: 1})
		return err
	}); err != nil {
		t.Fatalf("结算订单失败: %v", err)
	}

	total, list, err := ListOrders(OrderQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询订单列表失败: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("应查到 3 笔订单, got total=%d len=%d", total, len(list))
	}

	// 按订单号模糊匹配。
	_, list, err = ListOrders(OrderQuery{OrderNo: "LIST-0002"}, 1, 100)
	if err != nil || len(list) != 1 || list[0].OrderNo != "D-LIST-0002" {
		t.Fatalf("按单号模糊查询失败: %v (err=%v)", list, err)
	}

	// 按桌号精确匹配。
	_, list, err = ListOrders(OrderQuery{TableNo: "01"}, 1, 100)
	if err != nil || len(list) != 1 || list[0].OrderNo != "D-LIST-0001" {
		t.Fatalf("按桌号查询失败: %v (err=%v)", list, err)
	}

	// 支付状态筛选。
	paid := 1
	_, list, err = ListOrders(OrderQuery{PayStatus: &paid}, 1, 100)
	if err != nil || len(list) != 1 || list[0].OrderNo != "D-LIST-0003" {
		t.Fatalf("按支付状态查询失败: %v (err=%v)", list, err)
	}

	// 分页。
	total, page1, err := ListOrders(OrderQuery{}, 1, 2)
	if err != nil || total != 3 || len(page1) != 2 {
		t.Fatalf("分页查询异常: total=%d len=%d err=%v", total, len(page1), err)
	}
}

func TestOrderAppendUrgeAndSettleInfo(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-INFO-0001", 7)

	appendInfo, err := GetOrderAppendInfo("D-INFO-0001")
	if err != nil {
		t.Fatalf("查询加菜信息失败: %v", err)
	}
	if appendInfo.OrderID != id || appendInfo.TableID != 7 || appendInfo.PersonCount != 4 ||
		appendInfo.OrderStatus != 1 || appendInfo.PayStatus != 0 {
		t.Fatalf("加菜信息异常: %+v", appendInfo)
	}

	urge, err := GetUrgeTarget("D-INFO-0001")
	if err != nil {
		t.Fatalf("查询催菜目标失败: %v", err)
	}
	if urge.OrderID != id || urge.TableID != 7 || urge.OrderStatus != 1 || urge.TableNo != "01" || urge.TableName != "测试桌" {
		t.Fatalf("催菜目标异常: %+v", urge)
	}

	info, err := GetOrderSettleInfo(id)
	if err != nil {
		t.Fatalf("查询结算信息失败: %v", err)
	}
	if info.OrderNo != "D-INFO-0001" || info.TableID != 7 || info.DishCents != 3000 || info.SeatCents != 100 || info.DiscountCents != 200 || info.TotalCents != 2900 {
		t.Fatalf("结算信息异常: %+v", info)
	}
	if info.PreSettleStatus != 0 {
		t.Fatalf("未结算订单的 pre_settle_status 应为 0, got %d", info.PreSettleStatus)
	}
}

func TestOrderStateTableAndTotal(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-STATE-0001", 1)

	state, err := GetOrderState(id)
	if err != nil {
		t.Fatalf("查询订单状态失败: %v", err)
	}
	if state.OrderStatus != 1 || state.PayStatus != 0 {
		t.Fatalf("订单状态异常: status=%d pay=%d", state.OrderStatus, state.PayStatus)
	}

	tableID, err := GetOrderTable(id)
	if err != nil {
		t.Fatalf("查询订单桌台失败: %v", err)
	}
	if tableID != 1 {
		t.Fatalf("桌台应为 1, got %d", tableID)
	}
	if _, err := GetOrderTable(999999); err == nil {
		t.Fatal("不存在的订单应报错")
	}

	if total := GetOrderTotal(id); total != 2900 {
		t.Fatalf("订单总额应为 2900, got %d", total)
	}
	if total := GetOrderTotal(999999); total != 0 {
		t.Fatalf("不存在订单总额应为 0, got %d", total)
	}

	// 事务版状态读取。
	if err := store.WithTx(func(tx *sql.Tx) error {
		cur, err := GetOrderStateTx(tx, id)
		if err != nil {
			return err
		}
		if cur.OrderStatus != 1 || cur.PayStatus != 0 || cur.PersonCount != 4 {
			t.Fatalf("事务内订单状态异常: %+v", cur)
		}
		return nil
	}); err != nil {
		t.Fatalf("事务内查询失败: %v", err)
	}
}

func TestOrderItemsReplaceAndLoad(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-ITEM-0001", 1)

	items := []po.OrderItem{
		{DishID: 1, DishName: "凉拌青瓜", SpecID: 1, SpecName: "份", Price: 2800, Quantity: 1, Amount: 2800, Remark: "少辣"},
		{DishID: 2, DishName: "本场时蔬", SpecID: 2, SpecName: "份", Price: 2800, Quantity: 2, Amount: 5600},
	}
	if err := store.WithTx(func(tx *sql.Tx) error {
		return ReplaceOrderItemsTx(tx, id, items)
	}); err != nil {
		t.Fatalf("替换订单明细失败: %v", err)
	}

	got := LoadOrderItems(id)
	if len(got) != 2 || got[0].DishName != "凉拌青瓜" || got[1].Quantity != 2 {
		t.Fatalf("加载订单明细异常: %+v", got)
	}

	// 事务版加载,并验证替换语义(替换后只剩新的一条)。
	if err := store.WithTx(func(tx *sql.Tx) error {
		if err := ReplaceOrderItemsTx(tx, id, []po.OrderItem{{DishID: 3, DishName: "清炒腐竹", Quantity: 1, Amount: 3800}}); err != nil {
			return err
		}
		got, err := LoadOrderItemsTx(tx, id)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].DishName != "清炒腐竹" {
			t.Fatalf("替换后明细异常: %+v", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("事务内替换明细失败: %v", err)
	}

	if got := LoadOrderItems(999999); len(got) != 0 {
		t.Fatalf("不存在订单应返回空明细, got %v", got)
	}
}

func TestOrderStatusTransitionsTx(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-TX-0001", 1)

	if err := store.WithTx(func(tx *sql.Tx) error {
		// Touch 无副作用成功。
		if err := TouchOrderTx(tx, id); err != nil {
			return err
		}
		// 当前状态守卫匹配时更新成功。
		n, err := UpdateOrderStatusTx(tx, id, 2, "boss", 1)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("状态更新应影响 1 行, got %d", n)
		}
		// 守卫不匹配时影响 0 行。
		n, err = UpdateOrderStatusTx(tx, id, 3, "boss", 1)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("守卫不匹配应影响 0 行, got %d", n)
		}
		// 进行中订单数应为 1(状态已改为 2)。
		active, err := CountActiveOrdersTx(tx, 1)
		if err != nil {
			return err
		}
		if active != 1 {
			t.Fatalf("进行中订单数应为 1, got %d", active)
		}
		// 归档为已完成(状态 2)。
		if _, err := FinishOrderTx(tx, id, "boss", 2); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("订单状态流转失败: %v", err)
	}

	st, err := GetOrderState(id)
	if err != nil {
		t.Fatalf("查询订单状态失败: %v", err)
	}
	if st.OrderStatus != 4 || st.PayStatus != 0 {
		t.Fatalf("归档后状态应为 4, got status=%d pay=%d", st.OrderStatus, st.PayStatus)
	}

	// 取消未支付订单(新建另一笔)。
	id2 := orderDaoCreate(t, "D-TX-0002", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := CancelOrderTx(tx, id2, "客人取消", "boss", 1)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("取消订单应影响 1 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("取消订单失败: %v", err)
	}
	cancelled, err := GetOrderByID(id2)
	if err != nil {
		t.Fatalf("查询取消订单失败: %v", err)
	}
	if cancelled.OrderStatus != 5 || cancelled.CancelReason != "客人取消" {
		t.Fatalf("取消订单状态异常: status=%d reason=%q", cancelled.OrderStatus, cancelled.CancelReason)
	}
}

func TestOrderMarkPaidAndSettleFlow(t *testing.T) {
	orderDaoInitDB(t)

	// MarkOrderPaid:正常收款。
	id := orderDaoCreate(t, "D-PAY-0001", 1)
	n, err := MarkOrderPaid(id, "现金", "boss")
	if err != nil {
		t.Fatalf("标记收款失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("标记收款应影响 1 行, got %d", n)
	}
	paid, err := GetOrderSettleInfo(id)
	if err != nil {
		t.Fatalf("查询收款后信息失败: %v", err)
	}
	if paid.PayStatus != 1 || paid.SettleType != "normal" || paid.PaidCents != 2900 {
		t.Fatalf("收款后信息异常: %+v", paid)
	}

	// SettleOrderTx:免单/挂账结算。
	id2 := orderDaoCreate(t, "D-PAY-0002", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := SettleOrderTx(tx, id2, SettleOrderParams{SettleType: "credit", PayType: "挂账", Operator: "boss", Now: store.Now(), CreditStatus: 1, CreditCents: 2900, PaidCents: 0, PreStatus: 1})
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("挂账结算应影响 1 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("挂账结算失败: %v", err)
	}
	info, err := GetOrderSettleInfo(id2)
	if err != nil {
		t.Fatalf("查询挂账信息失败: %v", err)
	}
	if info.OrderStatus != 4 || info.PayStatus != 1 || info.CreditStatus != 1 || info.PreSettleStatus != 1 {
		t.Fatalf("挂账结算信息异常: %+v", info)
	}

	// SettleCreditOrder:核销挂账。
	n, err = SettleCreditOrder(id2, 2900, "现金", "boss")
	if err != nil {
		t.Fatalf("核销挂账失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("核销挂账应影响 1 行, got %d", n)
	}
	info, err = GetOrderSettleInfo(id2)
	if err != nil {
		t.Fatalf("查询核销后信息失败: %v", err)
	}
	if info.CreditStatus != 2 || info.PaidCents != 2900 {
		t.Fatalf("核销后信息异常: %+v", info)
	}

	// CancelSettleTx:撤销结算恢复状态。
	//
	// 先验证「已核销的挂账不可撤销」这条 SQL 守卫:service 层的 GetOrderSettleInfo 是
	// 事务外读,存在 check-then-act 窗口 —— 若守卫只写在 service,窗口内发生的核销会被
	// 撤销覆盖,把已收回的欠款(paid_amount / credit_settle_time)连带清零。
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := CancelSettleTx(tx, id2, "normal", "撤销", 1, "boss", store.Now())
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("已核销挂账撤销应影响 0 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("撤销结算失败: %v", err)
	}

	// 回到「待核销」后再撤销,应成功并恢复结算前状态。
	if _, err := store.DB.Exec(`UPDATE tb_order SET credit_status=? WHERE order_id=?`, 1, id2); err != nil {
		t.Fatalf("重置挂账状态失败: %v", err)
	}
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := CancelSettleTx(tx, id2, "normal", "撤销", 1, "boss", store.Now())
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("撤销结算应影响 1 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("撤销结算失败: %v", err)
	}
	restored, err := GetOrderSettleInfo(id2)
	if err != nil {
		t.Fatalf("查询撤销后信息失败: %v", err)
	}
	if restored.PayStatus != 0 || restored.OrderStatus != 1 || restored.CreditStatus != 0 {
		t.Fatalf("撤销结算后信息异常: %+v", restored)
	}
}

// TestMarkOrderPaidByChannelWritesPaidAmount 在线支付入账必须回写 paid_amount 等结算字段。
//
// 报表营业额(RangeAmount/DailyTrendStats/HourlyStats/SettleMixStats)与结账小票的
// 「实收」全部只认 paid_amount。漏写会让顾客扫码付的每一笔都被计成 0 元 —— 且只有
// 下次启动的历史回填才会补上,当天所有报表与对账都是错的。
func TestMarkOrderPaidByChannelWritesPaidAmount(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-PAY-CH-1", 1) // 应收 2900 分

	changed, err := MarkOrderPaidByChannel("D-PAY-CH-1", "微信", "trade-1", "wxpay")
	if err != nil || !changed {
		t.Fatalf("在线支付入账失败: changed=%v err=%v", changed, err)
	}
	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.PayStatus != 1 || o.PaidAmount != 2900 {
		t.Fatalf("实收金额应回写为 2900, got paid=%d pay_status=%d", o.PaidAmount, o.PayStatus)
	}
	if o.SettleType != "normal" || o.SettleTime == nil || o.PayTime == nil {
		t.Fatalf("结算字段应一并回写, got settle_type=%q settle_time=%v pay_time=%v",
			o.SettleType, o.SettleTime, o.PayTime)
	}
	// 收款前的订单状态要记入 pre_settle_status,撤销结算才能回到真实状态(该列不在
	// OrderCols 里,直接查库)。
	var preStatus int
	if err := store.DB.QueryRow(`SELECT COALESCE(pre_settle_status,0) FROM tb_order WHERE order_id=?`, id).
		Scan(&preStatus); err != nil {
		t.Fatalf("读取 pre_settle_status 失败: %v", err)
	}
	if preStatus != 1 {
		t.Fatalf("pre_settle_status 应为收款前状态 1, got %d", preStatus)
	}

	// 已支付订单再次回调:条件 UPDATE 不命中,由调用方判定为重复支付。
	changed, err = MarkOrderPaidByChannel("D-PAY-CH-1", "微信", "trade-1", "wxpay")
	if err != nil || changed {
		t.Fatalf("重复入账应 changed=false, got changed=%v err=%v", changed, err)
	}
}

// TestMarkOrderPaidByChannelRejectsCanceledOrder 已取消订单不能被支付回调置为已支付:
// 关渠道单是尽力而为,取消与回调并发时晚到的通知会造成「单已废、钱已收」。
func TestMarkOrderPaidByChannelRejectsCanceledOrder(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-PAY-CH-2", 1)
	if _, err := store.DB.Exec(`UPDATE tb_order SET order_status=? WHERE order_id=?`, 5, id); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	changed, err := MarkOrderPaidByChannel("D-PAY-CH-2", "微信", "trade-2", "wxpay")
	if err != nil || changed {
		t.Fatalf("已取消订单不应被入账, got changed=%v err=%v", changed, err)
	}
}

// TestUpdateOrderStatusTxWritesFinishTime 状态流转置为已完成时必须写 finish_time:
// 「今日完成订单数」(CountFinishedIn)按该列统计,漏写会让这条归档永远不计入完成数。
func TestUpdateOrderStatusTxWritesFinishTime(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-ST-1", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := UpdateOrderStatusTx(tx, id, 4, "boss", 1)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("状态更新应影响 1 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("状态更新失败: %v", err)
	}
	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.FinishTime == nil || *o.FinishTime == "" {
		t.Fatalf("置为已完成应写 finish_time, got %v", o.FinishTime)
	}
}

// TestDiningTimePersisted 用餐起止时间必须落库:
// 下单写 meal_start_time,结账/归档/取消写 meal_end_time,撤销结算时与
// finish_time 一并清空 —— 报表不再靠 create_time/finish_time 现算。
func TestDiningTimePersisted(t *testing.T) {
	orderDaoInitDB(t)
	// 下单:写入用餐开始时间。
	id := orderDaoCreate(t, "D-DINE-1", 1)
	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.MealStartTime == nil || *o.MealStartTime == "" {
		t.Fatalf("下单应写 meal_start_time, got %v", o.MealStartTime)
	}
	if *o.MealStartTime != o.CreateTime {
		t.Fatalf("用餐开始时间应与 create_time 一致, got %q vs %q", *o.MealStartTime, o.CreateTime)
	}
	if o.MealEndTime != nil {
		t.Fatalf("进行中订单的 meal_end_time 应为 NULL, got %v", *o.MealEndTime)
	}

	// 结账(走到终态 4):写入用餐结束时间。
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := SettleOrderTx(tx, id, SettleOrderParams{
			SettleType: "normal", PayType: "现金", Operator: "boss", Now: store.Now(), PreStatus: 1, PaidCents: 2900,
		})
		return err
	}); err != nil {
		t.Fatalf("结账失败: %v", err)
	}
	o, err = GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.MealEndTime == nil || *o.MealEndTime == "" {
		t.Fatal("结账应写 meal_end_time")
	}

	// 撤销结算:订单回到进行中,结束时间必须与 finish_time 同步清空。
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := CancelSettleTx(tx, id, "normal", "误结账", 3, "boss", store.Now())
		return err
	}); err != nil {
		t.Fatalf("撤销结算失败: %v", err)
	}
	o, err = GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.MealEndTime != nil {
		t.Fatalf("撤销结算后 meal_end_time 应清空, got %q", *o.MealEndTime)
	}

	// 归档 / 取消两条终止路径同样要留结束时间。
	id2 := orderDaoCreate(t, "D-DINE-2", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := FinishOrderTx(tx, id2, "boss", 1)
		return err
	}); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	o2, err := GetOrderByID(id2)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o2.MealEndTime == nil || *o2.MealEndTime == "" {
		t.Fatal("归档应写 meal_end_time")
	}

	id3 := orderDaoCreate(t, "D-DINE-3", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, err := CancelOrderTx(tx, id3, "客人取消", "boss", 1)
		return err
	}); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	o3, err := GetOrderByID(id3)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o3.MealEndTime == nil || *o3.MealEndTime == "" {
		t.Fatal("取消应写 meal_end_time(这段占用到此为止)")
	}
}

// TestSetOrderCancelReasonTx 取消原因可落库(走 /order/status{5} 取消时补写)。
func TestSetOrderCancelReasonTx(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-ST-2", 1)
	if err := store.WithTx(func(tx *sql.Tx) error {
		return SetOrderCancelReasonTx(tx, id, "顾客走了")
	}); err != nil {
		t.Fatalf("写取消原因失败: %v", err)
	}
	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if o.CancelReason != "顾客走了" {
		t.Fatalf("取消原因回读异常: %q", o.CancelReason)
	}
}

func TestOrderUpdateAmountAndEdit(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-EDIT-0001", 1)

	if err := store.WithTx(func(tx *sql.Tx) error {
		if err := UpdateOrderAmountTx(tx, id, 4000, 200, 300, 0, 3900); err != nil {
			return err
		}
		n, err := UpdateOrderForEditTx(tx, id, 6, "改备注", 4000, 200, 300, 0, 3900, "boss")
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("改单应影响 1 行, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("更新订单金额失败: %v", err)
	}

	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if o.PersonCount != 6 || o.OrderRemark != "改备注" || o.TotalAmount != 3900 {
		t.Fatalf("改单结果异常: %+v", o)
	}
}

func TestMarkOrderPaidByChannel(t *testing.T) {
	orderDaoInitDB(t)
	id := orderDaoCreate(t, "D-CHANNEL-0001", 1)

	changed, err := MarkOrderPaidByChannel("D-CHANNEL-0001", "wxpay", "TXN-123", "wxpay")
	if err != nil {
		t.Fatalf("渠道支付回写失败: %v", err)
	}
	if !changed {
		t.Fatal("未支付订单的渠道支付回写应命中并返回 changed=true")
	}
	o, err := GetOrderByID(id)
	if err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if o.PayStatus != 1 || o.TransactionID != "TXN-123" || o.PayChannel != "wxpay" {
		t.Fatalf("渠道支付信息异常: %+v", o)
	}
	if o.PayType == nil || *o.PayType != "wxpay" {
		t.Fatalf("支付方式应非空且为 wxpay, got %v", o.PayType)
	}
	if o.PayTime == nil {
		t.Fatal("支付时间应非空")
	}
}
