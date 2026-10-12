package store

import (
	"context"
	"database/sql"
	"testing"
)

// TestSQLiteDSNPerConnectionPragmas 回归测试:PRAGMA 必须通过 DSN 的 _pragma
// 参数逐连接生效,而不是启动时 Exec 一遍(那只会作用于连接池中的某一个连接)。
// 这里用 db.Conn 取两条不同连接,分别校验 foreign_keys / busy_timeout。
func TestSQLiteDSNPerConnectionPragmas(t *testing.T) {
	db, err := sql.Open("sqlite", sqliteDSN(t.TempDir()+"/pragma.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("取连接 %d 失败: %v", i, err)
		}
		var fk, bt int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("连接 %d 查询 foreign_keys 失败: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("连接 %d: foreign_keys=%d, 期望 1(外键约束未逐连接生效)", i, fk)
		}
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
			t.Fatalf("连接 %d 查询 busy_timeout 失败: %v", i, err)
		}
		if bt != 5000 {
			t.Errorf("连接 %d: busy_timeout=%d, 期望 5000(忙等待未逐连接生效)", i, bt)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("连接 %d 关闭失败: %v", i, err)
		}
	}
}

// TestSQLiteDSNPathForms 校验 sqliteDSN 对常见路径形态的转换。
func TestSQLiteDSNPathForms(t *testing.T) {
	cases := []struct{ in, wantPrefix string }{
		{"./dining.db", "file:./dining.db?"},
		{"C:\\data\\dining.db", "file:C:/data/dining.db?"},
		{"/var/lib/dining/dining.db", "file:/var/lib/dining/dining.db?"},
	}
	for _, c := range cases {
		got := sqliteDSN(c.in)
		if len(got) < len(c.wantPrefix) || got[:len(c.wantPrefix)] != c.wantPrefix {
			t.Errorf("sqliteDSN(%q) = %q, 期望前缀 %q", c.in, got, c.wantPrefix)
		}
		if got != "" && got[len(got)-len("_pragma=journal_mode(WAL)"):] != "_pragma=journal_mode(WAL)" {
			t.Errorf("sqliteDSN(%q) 末尾应包含 journal_mode pragma, 实际 %q", c.in, got)
		}
	}
}
