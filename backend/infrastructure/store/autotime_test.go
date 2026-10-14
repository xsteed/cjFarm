package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dining-system/infrastructure/conf"
)

// TestTimeColumnDialectDefinitions 时间列的自动填写必须落进两库的建表语句:
// MySQL 用原生 DEFAULT/ON UPDATE,SQLite 用 DEFAULT + 触发器。
// 这条测试锁的是「SQL 层自己就能生成时间」这个能力,而不是应用层有没有写。
func TestTimeColumnDialectDefinitions(t *testing.T) {
	mysql := strings.Join(SchemaSQL(DialectMySQL), "\n")
	if !strings.Contains(mysql, "create_time DATETIME DEFAULT CURRENT_TIMESTAMP") {
		t.Error("MySQL 的 create_time 应为 DATETIME DEFAULT CURRENT_TIMESTAMP")
	}
	if !strings.Contains(mysql, "update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP") {
		t.Error("MySQL 的 update_time 应为 DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP")
	}
	if strings.Contains(mysql, "{{") {
		t.Error("MySQL 脚本不应残留占位符")
	}

	sqlite := strings.Join(SchemaSQL(DialectSQLite), "\n")
	if !strings.Contains(sqlite, "create_time VARCHAR(32) DEFAULT (datetime('now','localtime'))") {
		t.Error("SQLite 的 create_time 应带本地时间默认值")
	}
	if strings.Contains(sqlite, "{{") {
		t.Error("SQLite 脚本不应残留占位符")
	}
	// SQLite 没有 ON UPDATE,靠触发器;每张含 update_time 的表都应有自己的触发器
	for _, tbl := range updateTimeTables {
		if !strings.Contains(sqlite, "CREATE TRIGGER IF NOT EXISTS trg_"+tbl+"_touch") {
			t.Errorf("SQLite 缺少 %s 的 update_time 触发器", tbl)
		}
	}
	if strings.Contains(mysql, "CREATE TRIGGER") {
		t.Error("MySQL 用列上的 ON UPDATE,不需要触发器")
	}
}

// TestSQLiteUpdateTimeAutoFilled 触发器必须在「行被更新但没带 update_time」时补当前时间,
// 而应用显式写入 update_time 时不覆盖 —— 与 MySQL 的 ON UPDATE 语义保持一致。
func TestSQLiteUpdateTimeAutoFilled(t *testing.T) {
	t.Setenv("DB_DRIVER", string(DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "touch.db"))
	Init("")
	t.Cleanup(func() { _ = DB.Close() })

	if _, err := DB.Exec(`INSERT INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time)
		VALUES(901, '测试备注', 1, '0', '2020-01-01 00:00:00')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// 1) 插入时没写 update_time:数据库默认值不参与(NOT NULL 未声明),应保持 NULL,
	//    这也验证了「create_time 自动、update_time 不自动填在插入时」的约定。
	var ut *string
	if err := DB.QueryRow(`SELECT update_time FROM tb_remark WHERE remark_id=901`).Scan(&ut); err != nil {
		t.Fatal(err)
	}
	if ut != nil {
		t.Fatalf("插入不应自动写 update_time, got %q", *ut)
	}
	var ct *string
	if err := DB.QueryRow(`SELECT create_time FROM tb_remark WHERE remark_id=901`).Scan(&ct); err != nil {
		t.Fatal(err)
	}
	if ct == nil || *ct != "2020-01-01 00:00:00" {
		t.Fatalf("显式写入的 create_time 不应被默认值覆盖, got %v", ct)
	}

	// 2) 更新业务列但不带 update_time → 触发器补时间
	if _, err := DB.Exec(`UPDATE tb_remark SET option_name='改过的备注' WHERE remark_id=901`); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if err := DB.QueryRow(`SELECT update_time FROM tb_remark WHERE remark_id=901`).Scan(&ut); err != nil {
		t.Fatal(err)
	}
	if ut == nil || *ut == "" {
		t.Fatal("更新后 update_time 应由触发器自动补齐")
	}
	if _, err := time.Parse(conf.TimeLayout, *ut); err != nil {
		t.Fatalf("自动补齐的 update_time 格式应为 %s, got %q", conf.TimeLayout, *ut)
	}

	// 3) 应用显式写 update_time 时以应用值为准(触发器不覆盖)
	if _, err := DB.Exec(`UPDATE tb_remark SET option_name='再改', update_time='2021-02-03 04:05:06' WHERE remark_id=901`); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow(`SELECT update_time FROM tb_remark WHERE remark_id=901`).Scan(&ut); err != nil {
		t.Fatal(err)
	}
	if ut == nil || *ut != "2021-02-03 04:05:06" {
		t.Fatalf("显式写入的 update_time 不应被触发器覆盖, got %v", ut)
	}
}
