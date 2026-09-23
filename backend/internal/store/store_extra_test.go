package store

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/conf"
)

// ============================================================================
// password.go
// ============================================================================

func TestSvcPwdHashAndVerify(t *testing.T) {
	const pw = "secret-密码"
	stored := HashPassword(pw)
	if !strings.HasPrefix(stored, "$2") {
		t.Fatalf("bcrypt 哈希应以 $2 开头, got %q", stored)
	}
	if !VerifyPassword(pw, stored) {
		t.Fatal("正确密码应校验通过")
	}
	if VerifyPassword("wrong-password", stored) {
		t.Fatal("错误密码不应校验通过")
	}
	// 空存储值:任何密码都不能命中。
	if VerifyPassword(pw, "") {
		t.Fatal("空存储值不应校验通过")
	}
}

func TestSvcPwdLongPasswordNormalization(t *testing.T) {
	// 40 个中文 = 120 字节,超过 bcrypt 72 字节上限,应先做 SHA-256 摘要再哈希。
	long := strings.Repeat("长", 40)
	stored := HashPassword(long)
	if !VerifyPassword(long, stored) {
		t.Fatal("超长密码应能校验通过(先摘要再 bcrypt)")
	}
	if VerifyPassword(strings.Repeat("长", 39), stored) {
		t.Fatal("错误超长密码不应校验通过")
	}
}

func TestSvcPwdIsLegacyHash(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   bool
	}{
		{"bcrypt", "$2a$10$abcdefghijklmnopqrstuv", false},
		{"空串", "", false},
		{"旧格式", legacyHashPassword("pw"), true},
		{"非bcrypt任意串", "plaintext", true},
	}
	for _, c := range cases {
		if got := IsLegacyHash(c.stored); got != c.want {
			t.Errorf("%s: IsLegacyHash(%q)=%v, 期望 %v", c.name, c.stored, got, c.want)
		}
	}
}

func TestSvcPwdLegacyHashFormat(t *testing.T) {
	for i := 0; i < 3; i++ {
		h := legacyHashPassword("pw")
		parts := strings.SplitN(h, "$", 2)
		if len(parts) != 2 {
			t.Fatalf("旧格式应为 hex(盐)$hex(摘要), got %q", h)
		}
		salt, err := hex.DecodeString(parts[0])
		if err != nil || len(salt) != 16 {
			t.Fatalf("盐应为 16 字节 hex, got %q (err=%v)", parts[0], err)
		}
		sum, err := hex.DecodeString(parts[1])
		if err != nil || len(sum) != 32 {
			t.Fatalf("摘要应为 32 字节 hex, got %q (err=%v)", parts[1], err)
		}
		if !legacyVerifyPassword("pw", h) {
			t.Fatalf("旧哈希应能校验自身: %q", h)
		}
		if legacyVerifyPassword("wrong", h) {
			t.Fatalf("错误密码不应命中旧哈希: %q", h)
		}
	}
}

func TestSvcPwdLegacyVerify(t *testing.T) {
	h := legacyHashPassword("pw")
	cases := []struct {
		name   string
		pw     string
		stored string
		want   bool
	}{
		{"正确密码", "pw", h, true},
		{"错误密码", "wrong", h, false},
		{"无分隔符", "pw", "no-dollar", false},
		{"非法盐hex", "pw", "zz$abcdef", false},
		{"空存储", "pw", "", false},
		{"摘要长度不足", "pw", hex.EncodeToString([]byte("salt")) + "$short", false},
	}
	for _, c := range cases {
		if got := legacyVerifyPassword(c.pw, c.stored); got != c.want {
			t.Errorf("%s: legacyVerifyPassword=%v, 期望 %v", c.name, got, c.want)
		}
	}
}

func TestSvcPwdWastePasswordVerifyNoPanic(t *testing.T) {
	// 仅用于抹平「账号不存在」分支的响应时间,任何入参都不应 panic。
	WastePasswordVerify("any-password")
	WastePasswordVerify("")
}

// ============================================================================
// dialect.go
// ============================================================================

func TestSvcDialActiveDialect(t *testing.T) {
	// 包级默认方言是 sqlite;若前面有测试改过,这里只会读到默认值。
	if got := ActiveDialect(); got != DialectSQLite {
		t.Fatalf("默认方言应为 sqlite, got %q", got)
	}
}

func TestSvcDialInsertReplaceInto(t *testing.T) {
	cases := []struct {
		d    Dialect
		want string
	}{
		{DialectSQLite, "INSERT OR REPLACE INTO t(a,b) VALUES(?,?)"},
		{DialectMySQL, "REPLACE INTO t(a,b) VALUES(?,?)"},
	}
	for _, c := range cases {
		t.Run(string(c.d), func(t *testing.T) {
			old := dialect
			dialect = c.d
			defer func() { dialect = old }()
			if got := InsertReplaceInto("t", "a", "b"); got != c.want {
				t.Fatalf("InsertReplaceInto=%q, 期望 %q", got, c.want)
			}
		})
	}
}

func TestSvcDialInsertIgnoreIntoFor(t *testing.T) {
	cases := []struct {
		name  string
		d     Dialect
		table string
		cols  []string
		want  string
	}{
		{"sqlite多列", DialectSQLite, "t", []string{"a", "b"}, "INSERT OR IGNORE INTO t(a,b) VALUES(?,?)"},
		{"mysql多列", DialectMySQL, "t", []string{"a", "b"}, "INSERT IGNORE INTO t(a,b) VALUES(?,?)"},
		{"sqlite零列", DialectSQLite, "t", nil, "INSERT OR IGNORE INTO t() VALUES()"},
		{"mysql零列", DialectMySQL, "t", nil, "INSERT IGNORE INTO t() VALUES()"},
	}
	for _, c := range cases {
		if got := insertIgnoreIntoFor(c.d, c.table, c.cols...); got != c.want {
			t.Errorf("%s: insertIgnoreIntoFor=%q, 期望 %q", c.name, got, c.want)
		}
	}
}

func TestSvcDialMaskDSN(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{"常规含密码", "root:secret@tcp(127.0.0.1:3306)/dining", "root:***@tcp(127.0.0.1:3306)/dining"},
		{"无@", "no-at-sign", "no-at-sign"},
		{"无冒号", "root@tcp(127.0.0.1:3306)/dining", "root@tcp(127.0.0.1:3306)/dining"},
		{"空串", "", ""},
	}
	for _, c := range cases {
		if got := maskDSN(c.dsn); got != c.want {
			t.Errorf("%s: maskDSN=%q, 期望 %q", c.name, got, c.want)
		}
	}
}

func TestSvcDialMysqlHint(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"账号密码错误", "Error 1045: Access denied for user", "账号或密码错误"},
		{"库不存在", "Error 1049: Unknown database 'dining'", "目标库不存在"},
		{"拒绝连接", "dial tcp 127.0.0.1:3306: connect: connection refused", "无法连接"},
		{"主机不存在", "dial tcp: lookup db.example.com: no such host", "无法连接"},
		{"超时", "dial tcp 10.0.0.1:3306: i/o timeout", "无法连接"},
		{"其他错误原样", "something else", "something else"},
	}
	for _, c := range cases {
		got := mysqlHint(errors.New(c.msg))
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: mysqlHint 应包含 %q, got %q", c.name, c.want, got)
		}
	}
}

func TestSvcDialInitFromEnvSQLite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(conf.EnvDBDriver, "sqlite")
	t.Setenv(conf.EnvDBDSN, "")
	t.Setenv(conf.EnvDBPath, filepath.Join(dir, "env.db"))

	InitFromEnv()
	if DB == nil {
		t.Fatal("InitFromEnv 后 DB 不应为 nil")
	}
	defer func() { _ = DB.Close() }()

	if got := ActiveDialect(); got != DialectSQLite {
		t.Fatalf("InitFromEnv(sqlite) 后方言应为 sqlite, got %q", got)
	}
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_config`).Scan(&n); err != nil {
		t.Fatalf("查询配置表失败: %v", err)
	}
	if n == 0 {
		t.Fatal("InitFromEnv 应完成建表与种子配置写入")
	}
}

// ============================================================================
// permission.go
// ============================================================================

func TestSvcPermImpliesReturnsCopy(t *testing.T) {
	got := PermImplies()
	if got["credit:view"] != "order:view" {
		t.Errorf("credit:view 应隐含 order:view, got %q", got["credit:view"])
	}
	if got["user:view"] != "role:view" {
		t.Errorf("user:view 应隐含 role:view, got %q", got["user:view"])
	}
	if len(got) != len(permImplies) {
		t.Fatalf("隐含表大小=%d, 期望 %d", len(got), len(permImplies))
	}
	// 返回的必须是副本:外部修改不能污染包级隐含表。
	got["credit:view"] = "polluted"
	if again := PermImplies(); again["credit:view"] != "order:view" {
		t.Fatal("PermImplies 应返回独立副本")
	}
}

func TestSvcPermIsBuiltinRoleKey(t *testing.T) {
	for _, k := range []string{RoleKeyAdmin, RoleKeyManager, RoleKeyCashier, RoleKeyStaff} {
		if !IsBuiltinRoleKey(k) {
			t.Errorf("内置角色 %q 应被识别", k)
		}
	}
	for _, k := range []string{"", "custom", "ADMIN", "manager "} {
		if IsBuiltinRoleKey(k) {
			t.Errorf("非内置角色 %q 不应被识别", k)
		}
	}
}

// ============================================================================
// store.go
// ============================================================================

func svcTxInit(t *testing.T, name string) {
	t.Helper()
	Init(filepath.Join(t.TempDir(), name))
	t.Cleanup(func() { _ = DB.Close() })
}

func svcTxCreateTable(t *testing.T, table string) {
	t.Helper()
	if _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS ` + table + `(id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
}

func TestSvcTxCommit(t *testing.T) {
	svcTxInit(t, "tx-commit.db")
	svcTxCreateTable(t, "svc_tx_commit")

	if err := WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO svc_tx_commit(v) VALUES('a')`)
		return err
	}); err != nil {
		t.Fatalf("WithTx 提交失败: %v", err)
	}

	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM svc_tx_commit`).Scan(&n); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("提交后应写入 1 行, got %d", n)
	}
}

func TestSvcTxRollback(t *testing.T) {
	svcTxInit(t, "tx-rollback.db")
	svcTxCreateTable(t, "svc_tx_rollback")

	sentinel := errors.New("业务失败")
	if err := WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO svc_tx_rollback(v) VALUES('partial')`); err != nil {
			return err
		}
		return sentinel
	}); err != sentinel {
		t.Fatalf("WithTx 应原样返回 fn 错误, got %v", err)
	}

	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM svc_tx_rollback`).Scan(&n); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("回滚后不应有残留数据, got %d", n)
	}
}

func TestSvcTxPanicPropagatesAndNotCommitted(t *testing.T) {
	// 注意:WithTx 不捕获 panic,也不会在 panic 路径显式 Rollback(fn 内事务连接会泄漏)。
	// 因此本测试结束时主动丢弃全局句柄,避免 DB.Close 等待被泄漏连接归还而阻塞;
	// 泄漏的连接随测试进程退出由 OS 回收,后续测试会各自重新 Init。
	Init(filepath.Join(t.TempDir(), "tx-panic.db"))
	svcTxCreateTable(t, "svc_tx_panic")

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("WithTx 应把 fn 的 panic 传播给调用方")
			}
		}()
		_ = WithTx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`INSERT INTO svc_tx_panic(v) VALUES('partial')`); err != nil {
				t.Fatalf("事务内写入失败: %v", err)
			}
			panic("boom")
		})
	}()

	// 未提交的数据对其他连接不可见(等价于回滚语义)。
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM svc_tx_panic`).Scan(&n); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("panic 后事务数据不应被提交, got %d", n)
	}
	DB = nil
}

// ============================================================================
// tablecode.go
// ============================================================================

func TestSvcEnsureTableCode(t *testing.T) {
	svcTxInit(t, "tablecode.db")

	// 首次生成:写入 8 位稳定码。
	res, err := DB.Exec(`INSERT INTO tb_table(table_no, table_name) VALUES('A1','测试桌')`)
	if err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	tableID, _ := res.LastInsertId()

	code, err := EnsureTableCode(int(tableID))
	if err != nil {
		t.Fatalf("EnsureTableCode 失败: %v", err)
	}
	if len(code) != 8 {
		t.Fatalf("桌台码应为 8 位, got %q", code)
	}

	// 幂等:再次调用应返回同一个码。
	again, err := EnsureTableCode(int(tableID))
	if err != nil {
		t.Fatalf("第二次 EnsureTableCode 失败: %v", err)
	}
	if again != code {
		t.Fatalf("桌台码应保持稳定: 首次 %q, 二次 %q", code, again)
	}

	// 已存在码时应直接复用,而不是重新生成。
	if _, err := DB.Exec(`UPDATE tb_table SET table_code='ABCD2345' WHERE table_id=?`, tableID); err != nil {
		t.Fatalf("预置桌台码失败: %v", err)
	}
	if got, err := EnsureTableCode(int(tableID)); err != nil || got != "ABCD2345" {
		t.Fatalf("应复用已存在的桌台码, got %q err=%v", got, err)
	}
}

func TestSvcEnsureTableCodeNotFound(t *testing.T) {
	svcTxInit(t, "tablecode-notfound.db")
	if _, err := EnsureTableCode(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在的桌台应返回 sql.ErrNoRows, got %v", err)
	}
}

// ============================================================================
// seed.go
// ============================================================================

func TestSvcSettingDefault(t *testing.T) {
	if got := SettingDefault("shop_name"); got == "" {
		t.Fatal("shop_name 应有出厂默认值")
	}
	if got := SettingDefault("pay_qr_wx"); !strings.HasPrefix(got, UploadURLPrefix) {
		t.Errorf("收款码默认值应使用统一前缀 %s, got %q", UploadURLPrefix, got)
	}
	if got := SettingDefault("no_such_key"); got != "" {
		t.Errorf("未登记键应返回空串, got %q", got)
	}
}

// ============================================================================
// schema.go
// ============================================================================

func svcSchemaFind(t *testing.T, stmts []string, name string) string {
	t.Helper()
	for _, s := range stmts {
		if strings.Contains(s, name) {
			return s
		}
	}
	t.Fatalf("未找到含 %q 的语句", name)
	return ""
}

func TestSvcSchemaIndexStatements(t *testing.T) {
	sqlite := indexStatements(DialectSQLite)
	if len(sqlite) != len(indexDefs) {
		t.Fatalf("SQLite 索引语句数=%d, 期望 %d", len(sqlite), len(indexDefs))
	}
	// 部分唯一索引在 SQLite 下保留 UNIQUE 与 WHERE。
	if got := svcSchemaFind(t, sqlite, "idx_table_code"); got != "CREATE UNIQUE INDEX idx_table_code ON tb_table(table_code) WHERE table_code!=''" {
		t.Errorf("SQLite 部分唯一索引生成错误: %q", got)
	}

	mysql := indexStatements(DialectMySQL)
	if len(mysql) != len(indexDefs) {
		t.Fatalf("MySQL 索引语句数=%d, 期望 %d", len(mysql), len(indexDefs))
	}
	// MySQL 不支持部分索引:退化为普通索引(无 UNIQUE、无 WHERE)。
	for _, name := range []string{"idx_table_code", "idx_user_username", "idx_role_key"} {
		if got := svcSchemaFind(t, mysql, name); strings.Contains(got, "UNIQUE") || strings.Contains(got, "WHERE") {
			t.Errorf("MySQL 部分索引 %s 应退化为普通索引, got %q", name, got)
		}
	}
	if got := svcSchemaFind(t, mysql, "idx_order_no"); !strings.Contains(got, "UNIQUE") {
		t.Errorf("MySQL 普通唯一索引 idx_order_no 应保留 UNIQUE, got %q", got)
	}
}

func TestSvcSchemaAlterStatements(t *testing.T) {
	stmts := alterStatements(DialectSQLite)
	if len(stmts) != len(alterCols) {
		t.Fatalf("ALTER 语句数=%d, 期望 %d", len(stmts), len(alterCols))
	}
	if got := svcSchemaFind(t, stmts, "transaction_id"); got != "ALTER TABLE tb_order ADD COLUMN transaction_id VARCHAR(64) DEFAULT ''" {
		t.Errorf("ALTER 语句生成错误: %q", got)
	}
	for _, s := range stmts {
		if !strings.HasPrefix(s, "ALTER TABLE ") || !strings.Contains(s, " ADD COLUMN ") {
			t.Errorf("非法 ALTER 语句: %q", s)
		}
	}
}
