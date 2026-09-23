package dao

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// urgeDaoInitDB 初始化催菜测试用的临时库。
func urgeDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "urge.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestUrgeInsertAndList(t *testing.T) {
	urgeDaoInitDB(t)

	if err := InsertUrge(1, "NO1", 1, "01", "大厅01桌", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}
	if err := InsertUrge(2, "NO2", 2, "02", "大厅02桌", po.UrgeTypeUrge, po.UrgeStatusHandled); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}

	total, list, err := ListUrges(UrgeQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询催菜失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("应查到 2 条催菜, got total=%d len=%d", total, len(list))
	}
	// 待处理(status=0)排在前面。
	if list[0].Status != po.UrgeStatusPending {
		t.Fatalf("待处理催菜应排前, got status=%d", list[0].Status)
	}

	// 状态筛选与桌号筛选。
	pending := po.UrgeStatusPending
	_, plist, err := ListUrges(UrgeQuery{Status: &pending}, 1, 100)
	if err != nil {
		t.Fatalf("按状态查询失败: %v", err)
	}
	if len(plist) != 1 || plist[0].OrderNo != "NO1" {
		t.Fatalf("按待处理筛选应命中 1 条 NO1, got %v", plist)
	}
	_, tlist, err := ListUrges(UrgeQuery{TableNo: "02"}, 1, 100)
	if err != nil {
		t.Fatalf("按桌号查询失败: %v", err)
	}
	if len(tlist) != 1 || tlist[0].OrderNo != "NO2" {
		t.Fatalf("按桌号筛选应命中 NO2, got %v", tlist)
	}
}

func TestUrgePendingAndHandle(t *testing.T) {
	urgeDaoInitDB(t)

	if err := InsertUrge(10, "NO10", 1, "01", "桌1", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}
	if err := InsertUrge(10, "NO10", 1, "01", "桌1", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}
	if err := InsertUrge(11, "NO11", 2, "02", "桌2", po.UrgeTypeUrge, po.UrgeStatusHandled); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}

	if !HasPendingUrge(10) {
		t.Fatal("订单 10 应存在待处理催菜")
	}
	if HasPendingUrge(11) {
		t.Fatal("订单 11 不应存在待处理催菜")
	}

	ids := PendingUrgeOrderIDs()
	if !ids[10] || ids[11] {
		t.Fatalf("待处理订单集合异常: %v", ids)
	}

	// 按订单批量处理。
	HandleUrgesByOrder(10, "boss")
	if HasPendingUrge(10) {
		t.Fatal("批量处理后订单 10 不应再有待处理催菜")
	}
}

func TestHandleUrgeSingle(t *testing.T) {
	urgeDaoInitDB(t)
	if err := InsertUrge(20, "NO20", 1, "01", "桌1", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}

	// 找到该催菜 ID。
	_, list, err := ListUrges(UrgeQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询催菜失败: %v", err)
	}
	urgeID := list[0].UrgeID

	n, err := HandleUrge(urgeID, "boss")
	if err != nil {
		t.Fatalf("处理催菜失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("处理催菜应影响 1 行, got %d", n)
	}
	// 再次处理(已是已处理)影响 0 行。
	if n, err := HandleUrge(urgeID, "boss"); err != nil || n != 0 {
		t.Fatalf("重复处理应影响 0 行, got n=%d err=%v", n, err)
	}
}

func TestLastUrgeTime(t *testing.T) {
	urgeDaoInitDB(t)

	if _, ok := LastUrgeTime(999); ok {
		t.Fatal("从未催过的订单应返回 ok=false")
	}

	if err := InsertUrge(30, "NO30", 1, "01", "桌1", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
		t.Fatalf("新增催菜失败: %v", err)
	}
	got, ok := LastUrgeTime(30)
	if !ok {
		t.Fatal("已催过的订单应返回 ok=true")
	}
	// 时间应为当前附近(误差不超过 1 分钟)。
	if d := time.Since(got); d < -time.Minute || d > time.Minute {
		t.Fatalf("催菜时间偏离当前时间过多: %v", got)
	}

	// 直插一条非法时间,验证解析失败分支返回 false。
	if _, err := store.DB.Exec(`INSERT INTO tb_order_urge(order_id, order_no, table_id, table_no, table_name, urge_type, status, create_time) VALUES(31,'NO31',1,'01','桌1','urge',0,'bad-time')`); err != nil {
		t.Fatalf("插入非法时间失败: %v", err)
	}
	if _, ok := LastUrgeTime(31); ok {
		t.Fatal("非法时间应返回 ok=false")
	}
}

func TestUrgeTxFunctions(t *testing.T) {
	urgeDaoInitDB(t)

	cutoff := time.Now().Add(-time.Minute).Format(conf.TimeLayout)
	if err := store.WithTx(func(tx *sql.Tx) error {
		if err := InsertUrgeTx(tx, 40, "NO40", 1, "01", "桌1", po.UrgeTypeUrge, po.UrgeStatusPending); err != nil {
			return err
		}
		n, err := CountRecentUrgesTx(tx, 40, cutoff)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("cutoff 之后应有 1 条催菜, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("事务内催菜操作失败: %v", err)
	}

	// cutoff 取未来时间,则没有 recent 记录。
	future := time.Now().Add(time.Minute).Format(conf.TimeLayout)
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err := CountRecentUrgesTx(tx, 40, future)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("未来 cutoff 不应命中催菜, got %d", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("查询 recent 失败: %v", err)
	}
}
