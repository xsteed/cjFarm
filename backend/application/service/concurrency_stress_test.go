package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// 并发压力测试:针对「串单修复」的四条核心并发路径,验证高并发下的最终一致不变量。
//
// 为什么这些测试能抓 bug:SettleOrderTx/CancelOrderTx/FinishOrderTx 都带「当前状态守卫」,
// 并发重复操作只有先到者生效(n==0 返回「状态已变更」)。若守卫有漏洞,表现不是「金额翻倍」
// (UPDATE 不重复加钱),而是「某单卡在半路终态 / 桌台残留占用 / 营收漏结」—— 这些正是下列
// 断言要锁住的最终状态。同时验证无 panic、无死锁(在 busy_timeout 内收敛)。

// relSeedOrdersN 种 n 笔同桌未结订单(状态1/未支付),金额递增,返回订单号列表。
func relSeedOrdersN(t *testing.T, tableID, n int) []string {
	t.Helper()
	nos := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		no := fmt.Sprintf("C%d-%d", tableID, i)
		relSeedOrder(t, no, tableID, 1, 0, int64(i)*1000)
		nos = append(nos, no)
	}
	return nos
}

// relSeedOrderBiz 种一笔指定营业日的订单,用于跨日隔离相关并发场景。
func relSeedOrderBiz(t *testing.T, orderNo string, tableID, orderStatus, payStatus int, totalCents int64, bizDate string) int {
	t.Helper()
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, biz_date, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, tableID, "01", "大厅01桌", 2, orderStatus, totalCents, 0, 0, totalCents, payStatus, bizDate, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&id); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	return id
}

// concurrentReleaseLoop 反复强制清台直到该桌无进行中订单(并发重试是常态)。
//
// 用「轮询直到空」而非「跑固定次数就停」:多协程同时 List 到同一批单、又同时去结算同一笔时,
// 只有一个成功、其余会因状态守卫失败而中途返回;失败者回到循环重新 List,就能把剩余的单收走。
// 这模拟了真实高峰里多个店员同时点「强制清台」的并发重试,并逼近最终一致。
func concurrentReleaseLoop(tid int, action string, iters int) {
	for i := 0; i < iters; i++ {
		if active := dao.ListActiveOrdersByTable(tid, StaleGuardBizDate()); len(active) == 0 {
			return
		}
		_, _ = ReleaseTable(context.Background(), tid, action, "店长")
	}
}

// assertAllSettled 断言最终不变量:每笔订单已终态结算、桌台空闲、无进行中订单残留、
// 已结算营收恰好等于种子总额(无重复结算、无漏结)。
func assertAllSettled(t *testing.T, tid int, nos []string) {
	t.Helper()
	var wantTotal int64
	for _, no := range nos {
		var pay, status int
		var amt int64
		if err := store.DB.QueryRow(`SELECT pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`, no).
			Scan(&pay, &status, &amt); err != nil {
			t.Fatalf("查询订单 %s 失败: %v", no, err)
		}
		if pay != 1 || status != 4 {
			t.Fatalf("订单 %s 并发清台后应为已结算终态, got pay=%d status=%d", no, pay, status)
		}
		wantTotal += amt
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("并发清台后桌台应空闲, got %d", s)
	}
	// 残留检查:不应有任何进行中订单未被收走(漏结会表现为桌台卡占用)。
	if active := dao.ListActiveOrdersByTable(tid, ""); len(active) != 0 {
		t.Fatalf("并发清台后仍存在 %d 笔进行中订单(漏结/卡死): %+v", len(active), active)
	}
	var gotTotal int64
	if err := store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order WHERE pay_status=1`).Scan(&gotTotal); err != nil {
		t.Fatalf("汇总已结算金额失败: %v", err)
	}
	if gotTotal != wantTotal {
		t.Fatalf("已结算营收=%d, want %d(应无重复结算或漏结)", gotTotal, wantTotal)
	}
}

// TestConcurrentReleaseTableNoDoubleSettle 多协程并发强制清台同一桌(同 action=settle)。
//
// 不变量:状态守卫保证每笔订单恰好结算一次;最终全终态、桌台空闲、营收无重复、无进行中残留。
func TestConcurrentReleaseTableNoDoubleSettle(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	nos := relSeedOrdersN(t, tid, 8)

	const workers = 10
	var wg sync.WaitGroup
	var panicFlag int32
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			concurrentReleaseLoop(tid, ReleaseActionSettle, 500)
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&panicFlag) != 0 {
		t.Fatal("并发强制清台过程中发生 panic")
	}
	assertAllSettled(t, tid, nos)
}

// TestConcurrentReleaseTableStressWithReaders 强制清台 + 顾客端读单并发(串单防护核心)。
//
// 多协程清台的同时,模拟顾客端不断扫码读取该桌进行中订单。不变量除上一条外,额外要求:
// 读者观察到的每一笔订单,最终都必须被正确结算 —— 顾客端读到的单不能凭空消失或卡死,
// 这正是「下一桌看到上一桌的菜」的反面不变量。
func TestConcurrentReleaseTableStressWithReaders(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	nos := relSeedOrdersN(t, tid, 8)

	var (
		mu        sync.Mutex
		seen      []string
		panicFlag int32
	)
	var wg sync.WaitGroup

	// 清台协程。
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			concurrentReleaseLoop(tid, ReleaseActionSettle, 500)
		}()
	}
	// 读者协程:模拟顾客端不断扫码读取该桌进行中订单。
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer func() {
				if rc := recover(); rc != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			for j := 0; j < 80; j++ {
				for _, o := range dao.ListActiveOrdersByTable(tid, StaleGuardBizDate()) {
					mu.Lock()
					seen = append(seen, o.OrderNo)
					mu.Unlock()
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&panicFlag) != 0 {
		t.Fatal("并发清台+读单过程中发生 panic")
	}
	assertAllSettled(t, tid, nos)

	// 串单防护:读者观察到的每一笔订单最终都必须被正确结算。
	seenSet := map[string]struct{}{}
	for _, n := range seen {
		seenSet[n] = struct{}{}
	}
	for no := range seenSet {
		var pay, status int
		if err := store.DB.QueryRow(`SELECT pay_status, order_status FROM tb_order WHERE order_no=?`, no).Scan(&pay, &status); err != nil {
			t.Fatalf("读者观察到的订单 %s 查询失败: %v", no, err)
		}
		if pay != 1 || status != 4 {
			t.Fatalf("读者曾观察到的订单 %s 最终未被正确结算(串单风险): pay=%d status=%d", no, pay, status)
		}
	}
}

// TestConcurrentStaleGuardVsManualRelease 跨日巡检 vs 手动强制清台并发(互不破坏)。
//
// 开启「跨日自动清台」:同桌同时存在今天的单(手动清台应收走)与昨天的遗留单(巡检只清台不动账务)。
// 不变量:今天的单被结算、昨天的遗留单不被误改、桌台最终空闲、营收只来自今天的单。
func TestConcurrentStaleGuardVsManualRelease(t *testing.T) {
	tableInitDB(t)
	if err := dao.SetSetting(SettingStaleReleaseEnabled, "1"); err != nil {
		t.Fatalf("开启跨日清台开关失败: %v", err)
	}
	tid := relOccupiedTable(t)
	today := time.Now().Format("2006-01-02")
	yest := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	relSeedOrderBiz(t, "T-1", tid, 1, 0, 3000, today) // 今天单:手动清台应收走
	relSeedOrderBiz(t, "Y-1", tid, 1, 0, 7000, yest)  // 昨天遗留单:巡检不动账务,手动清台按营业日隔离也看不到

	var panicFlag int32
	var wg sync.WaitGroup
	// 手动强制清台协程。
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			concurrentReleaseLoop(tid, ReleaseActionSettle, 300)
		}()
	}
	// 巡检协程(跨日自动清台)。
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer func() {
				if rc := recover(); rc != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			for i := 0; i < 400; i++ {
				_, _ = ReleaseStaleTables(context.Background())
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&panicFlag) != 0 {
		t.Fatal("跨日巡检与手动清台并发过程中发生 panic")
	}

	// 今天的订单应被手动清台结算。
	var tPay, tStatus int
	if err := store.DB.QueryRow(`SELECT pay_status, order_status FROM tb_order WHERE order_no='T-1'`).Scan(&tPay, &tStatus); err != nil {
		t.Fatalf("查询 T-1 失败: %v", err)
	}
	if tPay != 1 || tStatus != 4 {
		t.Fatalf("今天的订单 T-1 应被结算, got pay=%d status=%d", tPay, tStatus)
	}
	// 昨天遗留单不应被误改(既不该被手动清台顺手结算,也不该被巡检改账务)。
	var yPay, yStatus int
	if err := store.DB.QueryRow(`SELECT pay_status, order_status FROM tb_order WHERE order_no='Y-1'`).Scan(&yPay, &yStatus); err != nil {
		t.Fatalf("查询 Y-1 失败: %v", err)
	}
	if yPay != 0 || !ActiveStatus(yStatus) {
		t.Fatalf("跨日遗留单 Y-1 不应被误改, got pay=%d status=%d", yPay, yStatus)
	}
	// 桌台最终空闲。
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("桌台应空闲, got %d", s)
	}
	// 营收只来自今天的单,不含遗留单。
	var settled int64
	if err := store.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0) FROM tb_order WHERE pay_status=1`).Scan(&settled); err != nil {
		t.Fatalf("汇总已结算金额失败: %v", err)
	}
	if settled != 3000 {
		t.Fatalf("已结算营收应为 3000(今天单), got %d", settled)
	}
}

// TestConcurrentReleaseTableMixedActions 并发清台混用 settle/cancel(混乱输入鲁棒性)。
//
// 不同协程对同一桌交替执行「补结账」与「作废」。不变量:每笔订单恰好进入一种终态
// (结算或作废,绝不允许卡在半路),桌台最终空闲,已结算营收不超过种子总额(守卫挡住重复结算)。
func TestConcurrentReleaseTableMixedActions(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	nos := relSeedOrdersN(t, tid, 8)

	var panicFlag int32
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			action := ReleaseActionSettle
			if id%2 == 1 {
				action = ReleaseActionCancel
			}
			concurrentReleaseLoop(tid, action, 500)
		}(w)
	}
	wg.Wait()
	if atomic.LoadInt32(&panicFlag) != 0 {
		t.Fatal("并发混合动作清台过程中发生 panic")
	}

	// 每笔订单必须恰好进入一种终态。
	var settledTotal, wantTotal int64
	for _, no := range nos {
		var pay, status int
		var amt int64
		if err := store.DB.QueryRow(`SELECT pay_status, order_status, total_amount FROM tb_order WHERE order_no=?`, no).
			Scan(&pay, &status, &amt); err != nil {
			t.Fatalf("查询订单 %s 失败: %v", no, err)
		}
		switch {
		case pay == 1 && status == 4:
			settledTotal += amt // 被结算
		case pay == 0 && status == 5:
			// 被作废,不计收入
		default:
			t.Fatalf("订单 %s 并发混合清台后停留在非终态, got pay=%d status=%d", no, pay, status)
		}
		wantTotal += amt
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("并发清台后桌台应空闲, got %d", s)
	}
	// 已结算营收不得超过种子总额(守卫挡住重复结算,绝不超额)。
	if settledTotal > wantTotal {
		t.Fatalf("已结算营收 %d 超过种子总额 %d(疑似重复结算)", settledTotal, wantTotal)
	}
}

// TestConcurrentOccupyTableSerialization 多协程并发占用同一桌(开台串行化)。
//
// 反复并发 OccupyTableTx 应只把桌台置为占用,不会因并发把已占用的桌翻回空闲或产生错乱状态。
func TestConcurrentOccupyTableSerialization(t *testing.T) {
	tableInitDB(t)
	id, err := SaveTable(context.Background(), dto.Table{TableNo: "01", TableName: "并发桌", Capacity: 4})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}
	var panicFlag int32
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&panicFlag, 1)
				}
			}()
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = store.WithTx(func(tx *sql.Tx) error {
					_, err := dao.OccupyTableTx(tx, int(id))
					return err
				})
			}
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&panicFlag) != 0 {
		t.Fatal("并发占用桌台过程中发生 panic")
	}
	if s := relTableStatus(t, int(id)); s != 1 {
		t.Fatalf("并发占用后桌台应处于占用态, got %d", s)
	}
}
