package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// tableInitDB 初始化 SQLite 临时库并清空桌台种子,保证断言精确。
func tableInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "table.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
	if _, err := store.DB.Exec(`DELETE FROM tb_table`); err != nil {
		t.Fatalf("清空桌台表失败: %v", err)
	}
}

// TestTableListAndCodeBackfill 校验分页查询、关键词过滤与缺码桌台的即时补发。
func TestTableListAndCodeBackfill(t *testing.T) {
	tableInitDB(t)

	if _, err := dao.InsertTable(po.Table{TableNo: "01", TableName: "大厅A", Capacity: 4}); err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	if _, err := dao.InsertTable(po.Table{TableNo: "02", TableName: "大厅B", Capacity: 6}); err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	// 手动插入一行缺稳定码的历史数据,验证 TableList 会即时补发。
	if _, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?)`, "99", "无码桌", 4, 0, 9, "0", "", store.Now(), store.Now()); err != nil {
		t.Fatalf("插入无码桌台失败: %v", err)
	}

	total, rows, err := TableList("", 1, 100)
	if err != nil {
		t.Fatalf("TableList 失败: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("total=%d len=%d, want 3/3", total, len(rows))
	}
	for i, r := range rows {
		if r.TableCode == "" {
			t.Fatalf("第 %d 行桌台码为空,应已补发", i)
		}
	}

	// 关键词过滤。
	total, rows, err = TableList("无码", 1, 10)
	if err != nil {
		t.Fatalf("TableList 过滤失败: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].TableName != "无码桌" {
		t.Fatalf("关键词过滤结果异常: total=%d rows=%+v", total, rows)
	}
}

// TestTableSaveTable 校验新增桌台的参数校验与稳定码补发。
func TestTableSaveTable(t *testing.T) {
	tableInitDB(t)

	if _, err := SaveTable(context.Background(), dto.Table{TableNo: "", TableName: "无桌号"}); err == nil || !strings.Contains(err.Error(), "请填写桌号和桌台名称") {
		t.Fatalf("空桌号应报参数错误, got %v", err)
	}
	if _, err := SaveTable(context.Background(), dto.Table{TableNo: "01", TableName: "   "}); err == nil || !strings.Contains(err.Error(), "请填写桌号和桌台名称") {
		t.Fatalf("空桌名应报参数错误, got %v", err)
	}

	id, err := SaveTable(context.Background(), dto.Table{TableNo: "88", TableName: "测试桌", Capacity: 6})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("SaveTable 应返回正数 ID, got %d", id)
	}
	var code string
	if err := store.DB.QueryRow(`SELECT table_code FROM tb_table WHERE table_id=?`, id).Scan(&code); err != nil {
		t.Fatalf("查询桌台码失败: %v", err)
	}
	if code == "" {
		t.Fatal("新桌台应有稳定桌台码")
	}
}

// TestTableUpdateTable 校验更新桌台的参数校验与字段落库。
func TestTableUpdateTable(t *testing.T) {
	tableInitDB(t)
	id, err := SaveTable(context.Background(), dto.Table{TableNo: "01", TableName: "旧名", Capacity: 4})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}

	if err := UpdateTable(dto.Table{TableID: int(id), TableNo: "", TableName: "新名"}); err == nil {
		t.Fatal("空桌号更新应报错")
	}

	if err := UpdateTable(dto.Table{TableID: int(id), TableNo: "02", TableName: "新名", Capacity: 8}); err != nil {
		t.Fatalf("UpdateTable 失败: %v", err)
	}
	var no, name string
	var cap int
	if err := store.DB.QueryRow(`SELECT table_no, table_name, capacity FROM tb_table WHERE table_id=?`, id).Scan(&no, &name, &cap); err != nil {
		t.Fatalf("查询桌台失败: %v", err)
	}
	if no != "02" || name != "新名" || cap != 8 {
		t.Fatalf("更新结果异常: no=%q name=%q cap=%d", no, name, cap)
	}
}

// relSeedOrder 直接种一笔挂在指定桌台的订单(绕过业务链路,精确控制状态),返回订单 ID。
func relSeedOrder(t *testing.T, orderNo string, tableID, orderStatus, payStatus int, totalCents int64) int {
	t.Helper()
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, tableID, "01", "大厅01桌", 2, orderStatus, totalCents, 0, 0, totalCents, payStatus, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&id); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	return id
}

// relOccupiedTable 建一张桌台并置为占用,返回桌台 ID。
func relOccupiedTable(t *testing.T) int {
	t.Helper()
	if _, err := store.DB.Exec(`DELETE FROM tb_order`); err != nil {
		t.Fatalf("清空订单表失败: %v", err)
	}
	id, err := SaveTable(context.Background(), dto.Table{TableNo: "01", TableName: "大厅01桌", Capacity: 4})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_table SET status=1 WHERE table_id=?`, id); err != nil {
		t.Fatalf("置为占用失败: %v", err)
	}
	return int(id)
}

func relTableStatus(t *testing.T, tableID int) int {
	t.Helper()
	var s int
	if err := store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=?`, tableID).Scan(&s); err != nil {
		t.Fatalf("查询桌台状态失败: %v", err)
	}
	return s
}

// TestReleaseTableSettlesUnpaidOrders 补结账:未支付订单按已收款入账并归档,桌台回到空闲。
func TestReleaseTableSettlesUnpaidOrders(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	relSeedOrder(t, "R1", tid, 3, 0, 2800)
	relSeedOrder(t, "R2", tid, 2, 0, 1200)

	out, err := ReleaseTable(context.Background(), tid, ReleaseActionSettle, "店长")
	if err != nil {
		t.Fatalf("强制清台失败: %v", err)
	}
	if out.Settled != 2 || out.Canceled != 0 || out.Finished != 0 {
		t.Fatalf("清台结果异常: %+v", out)
	}
	if out.TotalCents != 4000 {
		t.Fatalf("补结账金额应为 4000 分, got %d", out.TotalCents)
	}
	if len(out.OrderNos) != 2 {
		t.Fatalf("应返回 2 个订单号供关闭支付单, got %v", out.OrderNos)
	}
	for _, no := range []string{"R1", "R2"} {
		var payStatus, orderStatus int
		var remark string
		if err := store.DB.QueryRow(`SELECT pay_status, order_status, COALESCE(settle_remark,'') FROM tb_order WHERE order_no=?`, no).
			Scan(&payStatus, &orderStatus, &remark); err != nil {
			t.Fatalf("查询订单 %s 失败: %v", no, err)
		}
		if payStatus != 1 || orderStatus != 4 {
			t.Fatalf("订单 %s 应已结账并归档, got pay=%d status=%d", no, payStatus, orderStatus)
		}
		if !strings.Contains(remark, "强制清台") {
			t.Fatalf("订单 %s 应留下强制清台备注, got %q", no, remark)
		}
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("桌台应恢复空闲, got %d", s)
	}
}

// TestReleaseTableCancelsUnpaidOrders 作废:订单直接取消且不计收入(仍是未支付)。
func TestReleaseTableCancelsUnpaidOrders(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	relSeedOrder(t, "R1", tid, 1, 0, 2800)

	out, err := ReleaseTable(context.Background(), tid, ReleaseActionCancel, "店长")
	if err != nil {
		t.Fatalf("强制清台失败: %v", err)
	}
	if out.Canceled != 1 || out.Settled != 0 || out.TotalCents != 0 {
		t.Fatalf("清台结果异常: %+v", out)
	}
	var payStatus, orderStatus int
	if err := store.DB.QueryRow(`SELECT pay_status, order_status FROM tb_order WHERE order_no='R1'`).Scan(&payStatus, &orderStatus); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if payStatus != 0 || orderStatus != 5 {
		t.Fatalf("作废后应为未支付且已取消, got pay=%d status=%d", payStatus, orderStatus)
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("桌台应恢复空闲, got %d", s)
	}
}

// TestReleaseTableFinishesPaidOrder 已收款但还在出餐的订单只归档,不动账务。
func TestReleaseTableFinishesPaidOrder(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t)
	relSeedOrder(t, "R1", tid, 2, 1, 5000)

	out, err := ReleaseTable(context.Background(), tid, ReleaseActionSettle, "店长")
	if err != nil {
		t.Fatalf("强制清台失败: %v", err)
	}
	if out.Finished != 1 || out.Settled != 0 || out.Canceled != 0 {
		t.Fatalf("清台结果异常: %+v", out)
	}
	var payStatus, orderStatus int
	if err := store.DB.QueryRow(`SELECT pay_status, order_status FROM tb_order WHERE order_no='R1'`).Scan(&payStatus, &orderStatus); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if payStatus != 1 || orderStatus != 4 {
		t.Fatalf("已收款订单应归档为已完成, got pay=%d status=%d", payStatus, orderStatus)
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("桌台应恢复空闲, got %d", s)
	}
}

// TestReleaseTableFreesStaleOccupiedTable 脏数据(显示占用却无进行中订单)也要能解开。
func TestReleaseTableFreesStaleOccupiedTable(t *testing.T) {
	tableInitDB(t)
	tid := relOccupiedTable(t) // 只占用,不建单

	out, err := ReleaseTable(context.Background(), tid, ReleaseActionSettle, "店长")
	if err != nil {
		t.Fatalf("强制清台失败: %v", err)
	}
	if out.Settled != 0 || out.Canceled != 0 || out.Finished != 0 {
		t.Fatalf("无订单时不应产生账务动作: %+v", out)
	}
	if s := relTableStatus(t, tid); s != 0 {
		t.Fatalf("桌台应恢复空闲, got %d", s)
	}
}

// TestReleaseTableRejectsBadInput 处理方式必须显式选择,桌台必须存在。
func TestReleaseTableRejectsBadInput(t *testing.T) {
	tableInitDB(t)

	if _, err := ReleaseTable(context.Background(), 1, "", "店长"); err == nil || !strings.Contains(err.Error(), "清台方式") {
		t.Fatalf("未指定处理方式应报错, got %v", err)
	}
	if _, err := ReleaseTable(context.Background(), 0, ReleaseActionSettle, "店长"); err == nil {
		t.Fatal("桌台 ID 为 0 应报错")
	}
	if _, err := ReleaseTable(context.Background(), 99999, ReleaseActionSettle, "店长"); err == nil || !strings.Contains(err.Error(), "桌台不存在") {
		t.Fatalf("桌台不存在应报错, got %v", err)
	}
}

// TestTableGuardRejectsActiveOrder 钉住「桌上有进行中订单时不允许删除/停用」。
//
// 守卫的判定来自 GetActiveOrderByTableE:它必须把 sql.ErrNoRows 与真实查询错误
// 分开 —— 旧实现把任何错误都当成「没有订单」,库抖动时会放行删除/停用正在用餐
// 的桌台,进行中订单随即失去桌台入口(加菜/收款/结账全部卡死)。
func TestTableGuardRejectsActiveOrder(t *testing.T) {
	tableInitDB(t)
	id, err := SaveTable(context.Background(), dto.Table{TableNo: "T9", TableName: "占用桌"})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}
	// 直接插一笔「已下单、未支付」的进行中订单(table_id 指向该桌)。
	if err := store.WithTx(func(tx *sql.Tx) error {
		_, e := dao.InsertOrderTx(tx, "TBL-GUARD-1", CurrentBizDate(), 0, int(id), 2, 0,
			"T9", "占用桌", "", 1000, 0, 0, 0, 1000)
		return e
	}); err != nil {
		t.Fatalf("插入进行中订单失败: %v", err)
	}

	if err := DeleteTable(int(id)); err == nil || !strings.Contains(err.Error(), "进行中订单") {
		t.Fatalf("有进行中订单时删除应被拒绝, got %v", err)
	}
	if err := SetTableLock(int(id), po.TableLockDisabled, "维修"); err == nil || !strings.Contains(err.Error(), "进行中订单") {
		t.Fatalf("有进行中订单时停用应被拒绝, got %v", err)
	}
	// 停用以外的状态(预约)允许与进行中订单并存。
	if err := SetTableLock(int(id), po.TableLockReserved, "王先生 18:30"); err != nil {
		t.Fatalf("预约不应被进行中订单挡住: %v", err)
	}
}

// TestTableDeleteTable 校验逻辑删除后不再出现在列表。
func TestTableDeleteTable(t *testing.T) {
	tableInitDB(t)
	id, err := SaveTable(context.Background(), dto.Table{TableNo: "01", TableName: "待删桌"})
	if err != nil {
		t.Fatalf("SaveTable 失败: %v", err)
	}

	if err := DeleteTable(int(id)); err != nil {
		t.Fatalf("DeleteTable 失败: %v", err)
	}
	total, _, err := TableList("待删", 1, 10)
	if err != nil {
		t.Fatalf("TableList 失败: %v", err)
	}
	if total != 0 {
		t.Fatalf("软删除后仍可查到, total=%d", total)
	}
}
