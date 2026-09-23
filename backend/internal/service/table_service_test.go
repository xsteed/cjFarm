package service

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
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

	if _, err := SaveTable(dto.Table{TableNo: "", TableName: "无桌号"}); err == nil || !strings.Contains(err.Error(), "请填写桌号和桌台名称") {
		t.Fatalf("空桌号应报参数错误, got %v", err)
	}
	if _, err := SaveTable(dto.Table{TableNo: "01", TableName: "   "}); err == nil || !strings.Contains(err.Error(), "请填写桌号和桌台名称") {
		t.Fatalf("空桌名应报参数错误, got %v", err)
	}

	id, err := SaveTable(dto.Table{TableNo: "88", TableName: "测试桌", Capacity: 6})
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
	id, err := SaveTable(dto.Table{TableNo: "01", TableName: "旧名", Capacity: 4})
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

// TestTableDeleteTable 校验逻辑删除后不再出现在列表。
func TestTableDeleteTable(t *testing.T) {
	tableInitDB(t)
	id, err := SaveTable(dto.Table{TableNo: "01", TableName: "待删桌"})
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
