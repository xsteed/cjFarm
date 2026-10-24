package dao

import (
	"path/filepath"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

func covDaoInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "coverage.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// covPickupSeq 测试订单的取餐号序列:(biz_date, pickup_no) 有唯一索引,
// 同一营业日插多行必须各配一个号,否则第二行起违反唯一约束。
var covPickupSeq int

// covInsertOrder 插入一笔订单,可控订单状态/支付状态/营业日/创建时间。
func covInsertOrder(t *testing.T, orderNo string, tableID, status, payStatus int, bizDate, createTime string) int {
	t.Helper()
	covPickupSeq++
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, biz_date, pickup_no, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, tableID, "T1", "桌", 2, status, 0, 0, 0, 0, payStatus, bizDate, covPickupSeq, createTime, now)
	if err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func covInsertItem(t *testing.T, orderID, dishID, specID, batchNo int, reqID string, amount int64) {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark, batch_no, client_request_id, is_gift, gift_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,0,'')`,
		orderID, dishID, "菜", specID, "规格", amount, 1, amount, "", batchNo, reqID); err != nil {
		t.Fatalf("插入订单明细失败: %v", err)
	}
}

func covInsertRefund(t *testing.T, r po.Refund) int {
	t.Helper()
	id, err := InsertRefund(r)
	if err != nil {
		t.Fatalf("插入退款单失败: %v", err)
	}
	return id
}

func covInsertSpec(t *testing.T, dishID, stock int) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price, stock) VALUES(?,?,?,?)`,
		dishID, "规格", 1000, stock)
	if err != nil {
		t.Fatalf("插入规格失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func covInsertDish(t *testing.T) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_dish(category_id, dish_name, del_flag, create_time, update_time) VALUES(1,'菜','0',?,?)`,
		store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入菜品失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// ============================ 订单列表 / 计数 ============================

func TestListActiveOrdersByTable(t *testing.T) {
	covDaoInit(t)
	covInsertOrder(t, "A1", 1, 2, 0, "2026-10-13", store.Now()) // 进行中
	covInsertOrder(t, "A2", 1, 4, 1, "2026-10-13", store.Now()) // 已完成,不应出现
	covInsertOrder(t, "A3", 2, 2, 0, "2026-10-13", store.Now()) // 另一桌

	list := ListActiveOrdersByTable(1, "")
	if len(list) != 1 || list[0].OrderNo != "A1" {
		t.Fatalf("应只返回桌1进行中订单, got %+v", list)
	}
}

func TestListStaleUnpaidOrders(t *testing.T) {
	covDaoInit(t)
	covInsertOrder(t, "S1", 1, 2, 0, "2026-10-13", "2026-10-13 10:00:00") // 超时
	covInsertOrder(t, "S2", 1, 2, 0, "2026-10-13", "2026-10-13 23:00:00") // 未超时
	covInsertOrder(t, "S3", 1, 2, 1, "2026-10-13", "2026-10-13 10:00:00") // 已支付

	list := ListStaleUnpaidOrders("2026-10-13", "2026-10-13 12:00:00")
	if len(list) != 1 || list[0].OrderNo != "S1" {
		t.Fatalf("应只返回 S1, got %+v", list)
	}
}

func TestListLegacyUnpaidOrders(t *testing.T) {
	covDaoInit(t)
	covInsertOrder(t, "L1", 1, 2, 0, "2026-10-12", store.Now()) // 上一营业日
	covInsertOrder(t, "L2", 1, 2, 0, "2026-10-13", store.Now()) // 当天,不应出现

	list := ListLegacyUnpaidOrders("2026-10-13")
	if len(list) != 1 || list[0].OrderNo != "L1" {
		t.Fatalf("应只返回 L1, got %+v", list)
	}
}

func TestListActiveOrders(t *testing.T) {
	covDaoInit(t)
	covInsertOrder(t, "AC1", 1, 2, 0, "2026-10-13", store.Now())
	covInsertOrder(t, "AC2", 1, 1, 0, "2026-10-13", store.Now())
	covInsertOrder(t, "AC3", 1, 5, 0, "2026-10-13", store.Now()) // 已取消

	list, err := ListActiveOrders("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应返回 2 笔进行中订单, got %d", len(list))
	}
}

func TestLoadOrderItemsByOrderIDs(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "OI1", 1, 2, 0, "2026-10-13", store.Now())
	covInsertItem(t, oid, 1, 1, 1, "req-1", 100)
	covInsertItem(t, oid, 1, 2, 1, "req-1", 200)

	m, err := LoadOrderItemsByOrderIDs([]int{oid})
	if err != nil {
		t.Fatal(err)
	}
	if len(m[oid]) != 2 {
		t.Fatalf("应加载 2 条明细, got %d", len(m[oid]))
	}
	// 空列表直接返回空 map。
	if m2, _ := LoadOrderItemsByOrderIDs(nil); len(m2) != 0 {
		t.Fatal("空输入应返回空 map")
	}
}

func TestCountOrderBatches(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "CB1", 1, 2, 0, "2026-10-13", store.Now())
	covInsertItem(t, oid, 1, 1, 1, "r1", 100)
	covInsertItem(t, oid, 1, 2, 2, "r2", 100)
	covInsertItem(t, oid, 1, 3, 1, "r3", 100) // 与第一条同批次

	m := CountOrderBatches([]int{oid})
	if m[oid] != 2 {
		t.Fatalf("应统计到 2 个批次, got %d", m[oid])
	}
	if m2 := CountOrderBatches(nil); len(m2) != 0 {
		t.Fatal("空输入应返回空 map")
	}
}

// ============================ 事务函数 ============================

func TestCountActiveUnpaidUnfinishedTx(t *testing.T) {
	covDaoInit(t)
	covInsertOrder(t, "T1", 7, 2, 0, "2026-10-13", store.Now())
	covInsertOrder(t, "T2", 7, 4, 1, "2026-10-13", store.Now()) // 已完成已支付

	tx, err := store.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	active, _ := CountActiveOrdersTx(tx, 7)
	if active != 1 {
		t.Fatalf("进行中订单数应为 1, got %d", active)
	}
	unpaid, _ := CountUnpaidOrdersTx(tx, 7, "")
	if unpaid != 1 {
		t.Fatalf("未结账订单数应为 1, got %d", unpaid)
	}
	unfin, _ := CountUnfinishedOrdersTx(tx, 7, "")
	if unfin != 1 {
		t.Fatalf("未完成订单数应为 1, got %d", unfin)
	}
}

func TestOrderItemRequestTx(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "R1", 1, 2, 0, "2026-10-13", store.Now())
	covInsertItem(t, oid, 1, 1, 1, "req-dedup", 100)

	tx, _ := store.DB.Begin()
	defer tx.Rollback()
	id, ok := GetOrderIDByRequestIDTx(tx, "req-dedup")
	if !ok || id != oid {
		t.Fatalf("应按幂等键查到订单: ok=%v id=%d", ok, id)
	}
	if _, ok := GetOrderIDByRequestIDTx(tx, "no-such"); ok {
		t.Fatal("不存在的幂等键应返回 false")
	}
	has, _ := HasOrderItemRequestTx(tx, oid, "req-dedup")
	if !has {
		t.Fatal("应检测到该订单已有此幂等键明细")
	}
	if has, _ := HasOrderItemRequestTx(tx, oid, "other"); has {
		t.Fatal("其它幂等键不应命中")
	}
}

func TestUpdateOrderAmountAndPersonTx(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "UA1", 1, 2, 0, "2026-10-13", store.Now())
	tx, _ := store.DB.Begin()
	if err := UpdateOrderAmountAndPersonTx(tx, oid, 4, 500, 0, 0, 0, 500); err != nil {
		t.Fatal(err)
	}
	if err := UpdateOrderAmountTx(tx, oid, 500, 0, 0, 0, 500); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var person int
	var total int64
	if err := store.DB.QueryRow(`SELECT person_count, total_amount FROM tb_order WHERE order_id=?`, oid).Scan(&person, &total); err != nil {
		t.Fatal(err)
	}
	if person != 4 || total != 500 {
		t.Fatalf("金额/人数未更新: person=%d total=%d", person, total)
	}
}

// ============================ 退款 / 支付 事务 ============================

func TestRefundTxFlow(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "RF1", 1, 4, 1, "2026-10-13", store.Now())

	// 插入一条处理中的重复支付退款单。
	dupID := covInsertRefund(t, po.Refund{
		OrderID: oid, OrderNo: "RF1", Channel: "wxpay", RefundNo: "RN1",
		Amount: 100, Status: po.RefundStatusProcessing, IsDuplicate: 1,
	})
	// 插入一条成功退款单。
	covInsertRefund(t, po.Refund{
		OrderID: oid, OrderNo: "RF1", Channel: "wxpay", RefundNo: "RN2",
		Amount: 50, Status: po.RefundStatusSuccess, IsDuplicate: 0,
	})

	// GetPendingDuplicateRefund 应查到处理中的重复退款单。
	got, err := GetPendingDuplicateRefund("RF1", "wxpay")
	if err != nil || got.RefundID != dupID || got.RefundNo != "RN1" {
		t.Fatalf("查重退款单失败: id=%d no=%q err=%v", got.RefundID, got.RefundNo, err)
	}

	// MarkRefundSuccessOnceTx 首次成功、二次幂等返回 false。
	tx, _ := store.DB.Begin()
	ok1, _ := MarkRefundSuccessOnceTx(tx, dupID, "channel-rn")
	ok2, _ := MarkRefundSuccessOnceTx(tx, dupID, "channel-rn")
	tx.Commit()
	if !ok1 {
		t.Fatal("首次置成功应返回 true")
	}
	if ok2 {
		t.Fatal("二次置成功应幂等返回 false")
	}

	// SumRefundedInclPendingTx 含处理中与成功,但不含重复支付退回(is_duplicate=1,
	// 与退款额度口径一致):dup(100) 已成功但不计入,只统计正常成功的 50。
	sum, err := func() (int64, error) {
		tx2, _ := store.DB.Begin()
		defer tx2.Rollback()
		return SumRefundedInclPendingTx(tx2, "RF1")
	}()
	if err != nil || sum != 50 {
		t.Fatalf("已退含处理中(不含重复支付退回)应为 50, got %d err=%v", sum, err)
	}
}

func TestRefundingAmountsAndSum(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "RA1", 1, 4, 1, "2026-10-13", store.Now())
	covInsertRefund(t, po.Refund{OrderID: oid, OrderNo: "RA1", Channel: "wxpay", RefundNo: "RA-RN1", ChannelRefundNo: "ch-ra-1", Amount: 30, Status: po.RefundStatusProcessing})
	covInsertRefund(t, po.Refund{OrderID: oid, OrderNo: "RA1", Channel: "wxpay", RefundNo: "RA-RN2", ChannelRefundNo: "ch-ra-2", Amount: 20, Status: po.RefundStatusSuccess})

	m, err := RefundingAmounts([]int{oid})
	if err != nil {
		t.Fatalf("统计处理中退款失败: %v", err)
	}
	if m[oid] != 30 {
		t.Fatalf("处理中退款应为 30, got %d", m[oid])
	}
	m2, err := RefundingAmounts(nil)
	if err != nil || len(m2) != 0 {
		t.Fatalf("空输入应返回空 map, got %v err=%v", m2, err)
	}
	// SumRefundedWithErr 只算成功(不含重复支付退回),此处无 is_duplicate=0 的成功单=0。
	s, err := SumRefundedWithErr("RA1")
	if err != nil || s != 20 {
		t.Fatalf("已成功退款应为 20, got %d err=%v", s, err)
	}
	sp, err := SumRefundedInclPending("RA1")
	if err != nil || sp != 50 {
		t.Fatalf("含处理中应为 50, got %d err=%v", sp, err)
	}
}

func TestDeductAndAddRefundedTx(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "PA1", 1, 4, 1, "2026-10-13", store.Now())
	// 设实收 100。
	store.DB.Exec(`UPDATE tb_order SET paid_amount=100 WHERE order_id=?`, oid)

	tx, _ := store.DB.Begin()
	if err := DeductPaidAmountTx(tx, "PA1", 30); err != nil {
		t.Fatal(err)
	}
	if err := AddOrderRefundedTx(tx, "PA1", 30); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var paid, refunded int64
	store.DB.QueryRow(`SELECT paid_amount, refund_amount FROM tb_order WHERE order_no='PA1'`).Scan(&paid, &refunded)
	if paid != 70 {
		t.Fatalf("实收应扣到 70, got %d", paid)
	}
	if refunded != 30 {
		t.Fatalf("已退应累加 30, got %d", refunded)
	}

	// 扣减超额时下限为 0,不出现负数。
	tx2, _ := store.DB.Begin()
	_ = DeductPaidAmountTx(tx2, "PA1", 99999)
	tx2.Commit()
	store.DB.QueryRow(`SELECT paid_amount FROM tb_order WHERE order_no='PA1'`).Scan(&paid)
	if paid != 0 {
		t.Fatalf("超额扣减应触底 0, got %d", paid)
	}
}

func TestMarkRefundSuccessOnceNonTx(t *testing.T) {
	covDaoInit(t)
	oid := covInsertOrder(t, "MR1", 1, 4, 1, "2026-10-13", store.Now())
	id := covInsertRefund(t, po.Refund{OrderID: oid, OrderNo: "MR1", Channel: "wxpay", Amount: 10, Status: po.RefundStatusProcessing})
	if ok, _ := MarkRefundSuccessOnce(id, "cr"); !ok {
		t.Fatal("首次应成功")
	}
	if ok, _ := MarkRefundSuccessOnce(id, "cr"); ok {
		t.Fatal("二次应幂等失败")
	}
}

// ============================ 规格库存 ============================

func TestSpecStockTx(t *testing.T) {
	covDaoInit(t)
	dishID := covInsertDish(t)
	specID := covInsertSpec(t, dishID, 10)

	tx, _ := store.DB.Begin()
	if err := DecrementSpecStockTx(tx, specID, 3); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	var stock int64
	store.DB.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&stock)
	if stock != 7 {
		t.Fatalf("扣减后库存应为 7, got %d", stock)
	}

	// 不限量(stock<0)不扣减。
	unlim := covInsertSpec(t, dishID, -1)
	tx2, _ := store.DB.Begin()
	if err := DecrementSpecStockTx(tx2, unlim, 100); err != nil {
		t.Fatal(err)
	}
	tx2.Commit()

	// 库存不足时返回 ErrSoldOut,事务回滚。
	tx3, _ := store.DB.Begin()
	err := DecrementSpecStockTx(tx3, specID, 100)
	if err != ErrSoldOut {
		t.Fatalf("库存不足应返回 ErrSoldOut, got %v", err)
	}
	tx3.Rollback()
	// 回补可恢复。
	tx4, _ := store.DB.Begin()
	if err := RestoreSpecStockTx(tx4, specID, 3); err != nil {
		t.Fatal(err)
	}
	tx4.Commit()
	store.DB.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&stock)
	if stock != 10 {
		t.Fatalf("回补后应为 10, got %d", stock)
	}
}

func TestLoadSpecsForOrder(t *testing.T) {
	covDaoInit(t)
	dishID := covInsertDish(t)
	specID := covInsertSpec(t, dishID, 5)
	m, err := LoadSpecsForOrder([]int{specID})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[specID].SpecID != specID {
		t.Fatalf("应加载到规格, got %+v", m)
	}
	if m2, _ := LoadSpecsForOrder(nil); len(m2) != 0 {
		t.Fatal("空输入应返回空 map")
	}
}

// ============================ 打印代理 / 任务 ============================

func TestPrintAgentLifecycle(t *testing.T) {
	covDaoInit(t)
	agent, token, err := InsertPrintAgent("门店A", "1,2")
	if err != nil || token == "" {
		t.Fatalf("新增代理失败: %v token=%q", err, token)
	}
	if NewAgentToken() == "" {
		t.Fatal("NewAgentToken 应返回非空令牌")
	}

	// 改名 + 改授权范围(空名沿用旧名)。
	if err := UpdatePrintAgent(agent.AgentID, "门店A改名", "3"); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadPrintAgent(agent.AgentID)
	if got.AgentName != "门店A改名" || got.PrinterIDs != "3" {
		t.Fatalf("更新后不符: %+v", got)
	}
	if err := UpdatePrintAgent(agent.AgentID, "", "4"); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadPrintAgent(agent.AgentID)
	if got.AgentName != "门店A改名" {
		t.Fatalf("空名应沿用旧名, got %q", got.AgentName)
	}

	// 启用中不可删;先吊销再删。
	if err := DeletePrintAgent(agent.AgentID); err == nil {
		t.Fatal("启用中删除应被拒绝")
	}
	if err := SetPrintAgentStatus(agent.AgentID, 0); err != nil {
		t.Fatal(err)
	}
	if err := DeletePrintAgent(agent.AgentID); err != nil {
		t.Fatalf("吊销后应可删除: %v", err)
	}
}

func TestFilterAlivePrinterIDs(t *testing.T) {
	covDaoInit(t)
	alive, err := InsertPrinter(po.Printer{PrinterName: "P1", PrinterType: 1, Provider: "tcp", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 不存在的 id 应被过滤。
	got := FilterAlivePrinterIDs([]int{int(alive), 99999, 0})
	if len(got) != 1 || got[0] != int(alive) {
		t.Fatalf("应只保留存活打印机, got %v", got)
	}
}

func TestReleasePrintJob(t *testing.T) {
	covDaoInit(t)
	res, err := store.DB.Exec(`INSERT INTO tb_print_job(printer_id, printer_name, printer_type, ip, port,
		doc_type, order_id, order_no, table_no, copies, print_log_id, delivery_id, payload,
		status, attempts, last_error, claimed_by, claim_time, next_try_time, trigger_by, operator, create_time, done_time)
		VALUES(1,'P',1,'1.1.1.1',9100,'kitchen',0,'ONO','T1',1,0,'d','p',?,1,'','agent',?,?, 't','o',?, '')`,
		po.PrintJobClaimed, store.Now(), store.Now(), store.Now())
	if err != nil {
		t.Fatalf("插入打印任务失败: %v", err)
	}
	jobID, _ := res.LastInsertId()

	if err := ReleasePrintJob(int(jobID), "网络不通", 10); err != nil {
		t.Fatalf("退回队列失败: %v", err)
	}
	var status int
	var attempts int
	var lastErr string
	store.DB.QueryRow(`SELECT status, attempts, last_error FROM tb_print_job WHERE job_id=?`, jobID).
		Scan(&status, &attempts, &lastErr)
	if status != po.PrintJobPending {
		t.Fatalf("退回后应为 pending, got %d", status)
	}
	if attempts != 0 {
		t.Fatalf("attempts 应被减 1 到 0, got %d", attempts)
	}
	if lastErr != "网络不通" {
		t.Fatalf("last_error 应记录, got %q", lastErr)
	}
	// 非 claimed 状态不允许退回。
	if err := ReleasePrintJob(int(jobID), "x", 1); err == nil {
		t.Fatal("非 claimed 任务退回应失败")
	}
}
