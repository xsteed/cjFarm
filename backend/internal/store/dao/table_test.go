package dao

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// tableDaoInitDB 初始化桌台测试用的临时库。
func tableDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "table.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestListAllTablesSeeded(t *testing.T) {
	tableDaoInitDB(t)
	list, err := ListAllTables()
	if err != nil {
		t.Fatalf("查询桌台失败: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("种子数据应包含桌台")
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].SortOrder > list[i].SortOrder {
			t.Fatalf("桌台排序错误: %d > %d", list[i-1].SortOrder, list[i].SortOrder)
		}
	}
}

func TestListTablesKeywordAndPagination(t *testing.T) {
	tableDaoInitDB(t)

	total, list, err := ListTables(TableQuery{Keyword: "大厅"}, 1, 100)
	if err != nil {
		t.Fatalf("关键字查询失败: %v", err)
	}
	if total != 4 || len(list) != 4 {
		t.Fatalf("关键字「大厅」应命中 4 桌, got total=%d len=%d", total, len(list))
	}

	total, page2, err := ListTables(TableQuery{}, 2, 3)
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if total == 0 || len(page2) > 3 {
		t.Fatalf("分页结果异常: total=%d len=%d", total, len(page2))
	}
}

func TestTableInsertUpdateDeleteRoundTrip(t *testing.T) {
	tableDaoInitDB(t)

	baseline, err := ListAllTables()
	if err != nil {
		t.Fatalf("查询基线失败: %v", err)
	}

	id, err := InsertTable(po.Table{TableNo: "99", TableName: "测试桌", Capacity: 4, Status: 0, SortOrder: 999})
	if err != nil {
		t.Fatalf("新增桌台失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("新增桌台应返回自增 ID, got %d", id)
	}

	// 新桌台应生成稳定码。
	tbl, _, err := GetTableByRef(strconv.Itoa(int(id)))
	if err != nil {
		t.Fatalf("按 ID 回读桌台失败: %v", err)
	}
	if tbl.TableCode == "" {
		t.Fatal("新增桌台应生成桌台码")
	}

	if err := UpdateTable(po.Table{TableID: int(id), TableNo: "98", TableName: "测试桌-改", Capacity: 6, Status: 1, SortOrder: 888}); err != nil {
		t.Fatalf("更新桌台失败: %v", err)
	}
	noName, name, err := GetTableNoName(int(id))
	if err != nil {
		t.Fatalf("查询桌号失败: %v", err)
	}
	if noName != "98" || name != "测试桌-改" {
		t.Fatalf("桌台更新未生效: no=%s name=%s", noName, name)
	}

	if err := DeleteTable(int(id)); err != nil {
		t.Fatalf("删除桌台失败: %v", err)
	}
	after, err := ListAllTables()
	if err != nil {
		t.Fatalf("删除后查询失败: %v", err)
	}
	if len(after) != len(baseline) {
		t.Fatalf("软删除后应回到 %d 张, got %d", len(baseline), len(after))
	}
	if _, _, err := GetTableNoName(int(id)); err == nil {
		t.Fatal("删除后的桌台不应再被查询到")
	}
}

func TestSetTableStatusTx(t *testing.T) {
	tableDaoInitDB(t)
	id, err := InsertTable(po.Table{TableNo: "50", TableName: "状态桌", Capacity: 2, SortOrder: 1})
	if err != nil {
		t.Fatalf("新增桌台失败: %v", err)
	}

	if err := store.WithTx(func(tx *sql.Tx) error {
		return SetTableStatusTx(tx, int(id), 1)
	}); err != nil {
		t.Fatalf("设置桌台状态失败: %v", err)
	}
	tbl, _, err := GetTableByRef(strconv.Itoa(int(id)))
	if err != nil {
		t.Fatalf("回读桌台失败: %v", err)
	}
	if tbl.Status != 1 {
		t.Fatalf("桌台状态应为 1, got %d", tbl.Status)
	}
}

func TestOccupyTableTx(t *testing.T) {
	tableDaoInitDB(t)
	id, err := InsertTable(po.Table{TableNo: "51", TableName: "占用桌", Capacity: 2, SortOrder: 1})
	if err != nil {
		t.Fatalf("新增桌台失败: %v", err)
	}

	// 首次占用应成功。
	var n int64
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err = OccupyTableTx(tx, int(id))
		return err
	}); err != nil {
		t.Fatalf("占用桌台失败: %v", err)
	}
	if n == 0 {
		t.Fatal("首次占用应返回非零影响行数")
	}

	// 已占用桌台再次占用视为成功(存在即返回非零)。
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err = OccupyTableTx(tx, int(id))
		return err
	}); err != nil {
		t.Fatalf("重复占用失败: %v", err)
	}
	if n == 0 {
		t.Fatal("已占用桌台再次占用应返回非零(存在即成功)")
	}

	// 不存在的桌台返回 0。
	if err := store.WithTx(func(tx *sql.Tx) error {
		n, err = OccupyTableTx(tx, 999999)
		return err
	}); err != nil {
		t.Fatalf("占用不存在桌台不应报错: %v", err)
	}
	if n != 0 {
		t.Fatalf("不存在桌台应返回 0, got %d", n)
	}
}

func TestGetTableByRef(t *testing.T) {
	tableDaoInitDB(t)

	id, err := InsertTable(po.Table{TableNo: "60", TableName: "扫码桌", Capacity: 2, SortOrder: 1})
	if err != nil {
		t.Fatalf("新增桌台失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_table SET table_code='AB12CD34' WHERE table_id=?`, id); err != nil {
		t.Fatalf("设置桌台码失败: %v", err)
	}

	// 桌台码大小写不敏感。
	tbl, _, err := GetTableByRef("ab12cd34")
	if err != nil {
		t.Fatalf("按桌台码查询失败: %v", err)
	}
	if tbl.TableID != int(id) {
		t.Fatalf("桌台码应命中桌台 %d, got %d", id, tbl.TableID)
	}

	// 纯数字按 ID 解析(兼容旧二维码)。
	tbl, _, err = GetTableByRef(strconv.Itoa(int(id)))
	if err != nil {
		t.Fatalf("按数字 ID 查询失败: %v", err)
	}
	if tbl.TableID != int(id) {
		t.Fatalf("数字 ID 应命中桌台 %d, got %d", id, tbl.TableID)
	}

	// 空串 / 非法引用返回 ErrNoRows。
	for _, ref := range []string{"", "abc", "-1", "1abc", "1234567890"} {
		if _, _, err := GetTableByRef(ref); err != sql.ErrNoRows {
			t.Fatalf("引用 %q 应返回 ErrNoRows, got %v", ref, err)
		}
	}
}
