package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestGeneratedScriptMatchesRuntimeSchema 校验「手工执行的 SQL 脚本」与
// 「后端启动时的建表逻辑」产出的库结构完全一致。
//
// 这是防止三处漂移的关键测试:一旦有人只改了 schema.go 而忘了重新生成脚本
// (或反之),本测试会立刻失败。
func TestGeneratedScriptMatchesRuntimeSchema(t *testing.T) {
	// ---- 1. 运行期建库 ----
	runtimeDB := filepath.Join(t.TempDir(), "runtime.db")
	Init(runtimeDB)
	// Init 会把全局 DB 指向临时库;测试结束前关闭句柄,否则 t.TempDir 无法清理文件。
	defer func() { _ = DB.Close() }()
	runtimeTables := snapshotSchema(t, DB)

	// ---- 2. 用生成的脚本建库 ----
	scriptDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "script.db"))
	if err != nil {
		t.Fatalf("打开脚本库失败: %v", err)
	}
	defer scriptDB.Close()
	for _, stmt := range SchemaSQL(DialectSQLite) {
		if _, err := scriptDB.Exec(stmt); err != nil {
			t.Fatalf("执行建表脚本失败: %v\n%s", err, stmt)
		}
	}
	scriptTables := snapshotSchema(t, scriptDB)

	// ---- 3. 结构对比 ----
	if !reflect.DeepEqual(runtimeTables, scriptTables) {
		for name, rt := range runtimeTables {
			st, ok := scriptTables[name]
			if !ok {
				t.Errorf("脚本缺少表 %s", name)
				continue
			}
			if !reflect.DeepEqual(rt, st) {
				t.Errorf("表 %s 结构不一致:\n  运行期: %v\n  脚本  : %v", name, rt, st)
			}
		}
		for name := range scriptTables {
			if _, ok := runtimeTables[name]; !ok {
				t.Errorf("脚本多出表 %s", name)
			}
		}
	}

	// ---- 4. 种子数据脚本:可重复执行且数量正确 ----
	for i := 0; i < 2; i++ {
		for _, stmt := range SeedSQL(DialectSQLite) {
			if _, err := scriptDB.Exec(stmt); err != nil {
				t.Fatalf("执行种子脚本失败(第 %d 次): %v\n%s", i+1, err, stmt)
			}
		}
	}
	want := SeedCounts()
	got := map[string]int{}
	// 表名与 SeedCounts 的键一一对应;新增种子表时两处一起加。
	for k, table := range map[string]string{
		"config":   "tb_config",
		"role":     "tb_role",
		"table":    "tb_table",
		"category": "tb_category",
		"dish":     "tb_dish",
		"spec":     "tb_spec",
		"remark":   "tb_remark",
		"printer":  "tb_printer",
	} {
		var n int
		if err := scriptDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("统计 %s 失败: %v", table, err)
		}
		got[k] = n
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("种子数据数量不一致:\n  期望: %v\n  实际: %v", want, got)
	}
}

// TestSchemaSQLBothDialects 用轻量规则校验两种方言生成的语句都「看起来可执行」:
// MySQL 不能出现 AUTOINCREMENT / IF NOT EXISTS 索引 / TEXT DEFAULT,SQLite 不能出现
// AUTO_INCREMENT / ENGINE=。这类问题无法在本机连库发现,只能靠规则兜底。
func TestSchemaSQLBothDialects(t *testing.T) {
	mysql := strings.Join(SchemaSQL(DialectMySQL), "\n")
	sqlite := strings.Join(SchemaSQL(DialectSQLite), "\n")

	for _, bad := range []string{"AUTOINCREMENT", "INDEX IF NOT EXISTS", "PRAGMA", "VARCHAR(4000) NOT NULL"} {
		if strings.Contains(mysql, bad) {
			t.Errorf("MySQL 脚本不应包含 %q", bad)
		}
	}
	for _, bad := range []string{"AUTO_INCREMENT", "ENGINE=InnoDB"} {
		if strings.Contains(sqlite, bad) {
			t.Errorf("SQLite 脚本不应包含 %q", bad)
		}
	}

	// 每条建表语句都必须以分号结尾,且 MySQL 必须显式声明引擎与字符集。
	for _, stmt := range SchemaSQL(DialectMySQL) {
		if !strings.HasSuffix(strings.TrimSpace(stmt), ";") {
			t.Errorf("MySQL 语句缺少分号: %s", stmt)
		}
	}
	if n := strings.Count(mysql, "ENGINE=InnoDB"); n != len(schemaTemplate) {
		t.Errorf("MySQL 建表语句应有 %d 条带 ENGINE,实际 %d", len(schemaTemplate), n)
	}

	// 两个方言的表数量与表名必须一致。
	names := func(sql string) []string {
		var out []string
		for _, line := range strings.Split(sql, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "CREATE TABLE IF NOT EXISTS ") {
				f := strings.Fields(line)
				out = append(out, strings.TrimSuffix(f[5], "("))
			}
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(names(mysql), names(sqlite)) {
		t.Errorf("两方言表清单不一致:\n  mysql : %v\n  sqlite: %v", names(mysql), names(sqlite))
	}
	// 表数量不写死常量:以 schemaTemplate 为准,避免每加一张表就要改测试
	// (曾因写死 12 而反复失败)。真正的回归风险是「两方言条数不一致」,
	// 已由上面的 names 相等断言覆盖;这里再确认生成器没有漏掉/多出建表语句。
	if got, wantN := len(names(mysql)), len(schemaTemplate); got != wantN {
		t.Errorf("生成脚本的表数应与 schemaTemplate 一致: 期望 %d,实际 %d", wantN, got)
	}
}

// mysqlReservedWords MySQL 8.0 保留字(来源:官方 Keywords and Reserved Words 列表)。
// 保留字不可以用作未加反引号的表名/列名,否则建表语句会直接报 1064 语法错误。
var mysqlReservedWords = strings.Fields(`
ACCESSIBLE ADD ALL ALTER ANALYZE AND AS ASC ASENSITIVE BEFORE BETWEEN BIGINT BINARY BLOB BOTH BY
CALL CASCADE CASE CHANGE CHAR CHARACTER CHECK COLLATE COLUMN CONDITION CONSTRAINT CONTINUE CONVERT
CREATE CROSS CUBE CUME_DIST CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER CURSOR
DATABASE DATABASES DAY_HOUR DAY_MICROSECOND DAY_MINUTE DAY_SECOND DEC DECIMAL DECLARE DEFAULT DELAYED
DELETE DENSE_RANK DESC DESCRIBE DETERMINISTIC DISTINCT DISTINCTROW DIV DOUBLE DROP DUAL EACH ELSE
ELSEIF EMPTY ENCLOSED ESCAPED EXCEPT EXISTS EXIT EXPLAIN FALSE FETCH FIRST_VALUE FLOAT FLOAT4 FLOAT8
FOR FORCE FOREIGN FROM FULLTEXT FUNCTION GENERATED GET GRANT GROUP GROUPING GROUPS HAVING HIGH_PRIORITY
HOUR_MICROSECOND HOUR_MINUTE HOUR_SECOND IF IGNORE IN INDEX INFILE INNER INOUT INSENSITIVE INSERT INT
INT1 INT2 INT3 INT4 INT8 INTEGER INTERSECT INTERVAL INTO IO_AFTER_GTIDS IO_BEFORE_GTIDS IS ITERATE JOIN
JSON_TABLE KEY KEYS KILL LAG LAST_VALUE LATERAL LEAD LEADING LEAVE LEFT LIKE LIMIT LINEAR LINES LOAD
LOCALTIME LOCALTIMESTAMP LOCK LONG LONGBLOB LONGTEXT LOOP LOW_PRIORITY MASTER_BIND MATCH MAXVALUE
MEDIUMBLOB MEDIUMINT MEDIUMTEXT MIDDLEINT MINUTE_MICROSECOND MINUTE_SECOND MOD MODIFIES NATURAL NOT
NO_WRITE_TO_BINLOG NTH_VALUE NTILE NULL NUMERIC OF ON OPTIMIZE OPTIMIZER_COSTS OPTION OPTIONALLY OR
ORDER OUT OUTER OUTFILE OVER PARTITION PERCENT_RANK PRECISION PRIMARY PROCEDURE PURGE RANGE RANK READ
READS READ_WRITE REAL RECURSIVE REFERENCES REGEXP RELEASE RENAME REPEAT REPLACE REQUIRE RESIGNAL
RESTRICT RETURN REVOKE RIGHT RLIKE ROW ROWS ROW_NUMBER SCHEMA SCHEMAS SECOND_MICROSECOND SELECT
SENSITIVE SEPARATOR SET SHOW SIGNAL SMALLINT SPATIAL SPECIFIC SQL SQLEXCEPTION SQLSTATE SQLWARNING
SQL_BIG_RESULT SQL_CALC_FOUND_ROWS SQL_SMALL_RESULT SSL STARTING STORED STRAIGHT_JOIN SYSTEM TABLE
TERMINATED THEN TINYBLOB TINYINT TINYTEXT TO TRAILING TRIGGER TRUE UNDO UNION UNIQUE UNLOCK UNSIGNED
UPDATE USAGE USE USING UTC_DATE UTC_TIME UTC_TIMESTAMP VALUES VARBINARY VARCHAR VARCHARACTER VARYING
VIRTUAL WHEN WHERE WHILE WINDOW WITH WRITE XOR YEAR_MONTH ZEROFILL`)

// TestNoMySQLReservedIdentifiers 确保表名与所有列名都不是 MySQL 保留字。
// 这类问题在 SQLite 上完全正常、只有切到 MySQL 建表时才会暴露,必须提前拦住。
func TestNoMySQLReservedIdentifiers(t *testing.T) {
	reserved := map[string]bool{}
	for _, w := range mysqlReservedWords {
		reserved[w] = true
	}

	tables, cols := identifiersIn(SchemaSQL(DialectMySQL))
	if len(tables) == 0 || len(cols) == 0 {
		t.Fatalf("未能从建表语句中解析出标识符(表 %d / 列 %d)", len(tables), len(cols))
	}
	for _, name := range append(append([]string{}, tables...), cols...) {
		if reserved[strings.ToUpper(name)] {
			t.Errorf("标识符 %q 是 MySQL 保留字,切库会建表失败,请改名", name)
		}
	}
}

// identifiersIn 从建表语句里解析出表名与列名(仅用于静态检查,不做完整 SQL 解析)。
func identifiersIn(stmts []string) (tables []string, cols []string) {
	for _, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt)
		if !strings.HasPrefix(trimmed, "CREATE TABLE") {
			continue
		}
		for i, line := range strings.Split(stmt, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimSuffix(line, ",")
			switch {
			case i == 0:
				// CREATE TABLE IF NOT EXISTS tb_table (
				f := strings.Fields(line)
				tables = append(tables, strings.TrimSuffix(f[len(f)-1], "("))
			case line == "" || strings.HasPrefix(line, ")") || strings.HasPrefix(line, "--"):
				// 语句结尾的 ) ENGINE=... 或注释
			default:
				f := strings.Fields(line)
				if len(f) > 0 {
					cols = append(cols, f[0])
				}
			}
		}
	}
	return tables, cols
}

// TestGenSQLCheck 校验磁盘上的脚本与当前 Go 定义一致(等价于 go run ./cmd/gensql -check)。
// 脚本目录不存在时跳过(例如把 backend 单独打包分发)。
func TestGenSQLCheck(t *testing.T) {
	root := filepath.Join("..", "..", "migrations", "full")
	if _, err := os.Stat(root); err != nil {
		t.Skip("未找到 migrations 目录,跳过")
	}
	cases := []struct {
		path   string
		expect []string
	}{
		{filepath.Join(root, "sqlite", "schema.sql"), SchemaSQL(DialectSQLite)},
		{filepath.Join(root, "sqlite", "seed.sql"), SeedSQL(DialectSQLite)},
		{filepath.Join(root, "mysql", "schema.sql"), SchemaSQL(DialectMySQL)},
		{filepath.Join(root, "mysql", "seed.sql"), SeedSQL(DialectMySQL)},
	}
	for _, c := range cases {
		b, err := os.ReadFile(c.path)
		if err != nil {
			t.Errorf("读取 %s 失败: %v", c.path, err)
			continue
		}
		body := string(b)
		// 只校验生成的语句部分(文件头部说明允许自由调整)。
		for _, stmt := range c.expect {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			stmt = strings.ReplaceAll(stmt, "\t", "    ")
			if !strings.Contains(body, stmt) {
				t.Errorf("%s 缺少语句(请重新执行 go run ./cmd/gensql):\n%s", c.path, stmt)
			}
		}
	}
}

// ============================================================================
// 图片/收款码路径前缀统一
// ============================================================================

// TestSeedUploadPathsUseUnifiedPrefix 种子数据里的图片与收款码必须统一使用
// UploadURLPrefix(/uploads/)。历史上菜品图写入 /picture/、上传接口返回 /uploads/,
// 两套前缀并存导致线上图片 404(请求落进前端 SPA 兜底),这里把「新种子数据写错前缀」
// 拦在提交前。若将来再改前缀,只需改 UploadURLPrefix 一处,本测试会同步守住。
func TestSeedUploadPathsUseUnifiedPrefix(t *testing.T) {
	for _, d := range seedDishRows {
		if !strings.HasPrefix(d.img, UploadURLPrefix) {
			t.Errorf("菜品 %q 的图片路径 %q 未使用 %s 前缀", d.name, d.img, UploadURLPrefix)
		}
	}
	for _, k := range []string{"pay_qr_wx", "pay_qr_ali"} {
		if v := cfgDefaults[k]; !strings.HasPrefix(v, UploadURLPrefix) {
			t.Errorf("配置 %s 默认值 %q 未使用 %s 前缀", k, v, UploadURLPrefix)
		}
	}
}

// TestMigrateUploadPrefix 老库里的 /picture/ 路径必须在启动迁移时被改写为 /uploads/,
// 且重复执行幂等 —— 这是删掉 /picture 静态路由后存量图片与收款码不 404 的前提。
func TestMigrateUploadPrefix(t *testing.T) {
	Init(filepath.Join(t.TempDir(), "upload-prefix.db"))
	defer func() { _ = DB.Close() }()

	const legacyDish = "/picture/dining_20260918_001.jpeg"
	if _, err := DB.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, del_flag)
		VALUES(1, '存量菜品', ?, '0')`, legacyDish); err != nil {
		t.Fatalf("写入存量菜品失败: %v", err)
	}
	if _, err := DB.Exec(`UPDATE tb_config SET cfg_value=? WHERE cfg_key='pay_qr_wx'`, legacyUploadPrefix+"pay_wx.png"); err != nil {
		t.Fatalf("写入存量收款码失败: %v", err)
	}

	// 跑两遍:验证改写正确且幂等。
	migrateUploadPrefix()
	migrateUploadPrefix()

	var dishImg, qrImg string
	DB.QueryRow(`SELECT dish_image FROM tb_dish WHERE dish_name='存量菜品'`).Scan(&dishImg)
	DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key='pay_qr_wx'`).Scan(&qrImg)
	if want := UploadURLPrefix + "dining_20260918_001.jpeg"; dishImg != want {
		t.Errorf("菜品图片路径未改写: got %q, want %q", dishImg, want)
	}
	if want := UploadURLPrefix + "pay_wx.png"; qrImg != want {
		t.Errorf("收款码路径未改写: got %q, want %q", qrImg, want)
	}

	var left int
	DB.QueryRow(`SELECT COUNT(*) FROM tb_dish WHERE dish_image LIKE ?`, legacyUploadPrefix+"%").Scan(&left)
	if left != 0 {
		t.Errorf("仍有 %d 条菜品图片使用废弃前缀 %s", left, legacyUploadPrefix)
	}
}

// snapshotSchema 抓取库中所有表的列清单与索引清单。
func snapshotSchema(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	out := map[string][]string{}

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("读取表清单失败: %v", err)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables = append(tables, name)
		}
	}
	rows.Close()

	for _, table := range tables {
		cols := []string{}
		cr, err := db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatalf("读取 %s 列失败: %v", table, err)
		}
		for cr.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt interface{}
			if err := cr.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				cols = append(cols, name+":"+ctype)
			}
		}
		cr.Close()

		ir, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=? AND name NOT LIKE 'sqlite_autoindex_%' ORDER BY name`, table)
		if err != nil {
			t.Fatalf("读取 %s 索引失败: %v", table, err)
		}
		idx := []string{}
		for ir.Next() {
			var name string
			if err := ir.Scan(&name); err == nil {
				idx = append(idx, name)
			}
		}
		ir.Close()

		out[table] = append(append([]string{}, cols...), append([]string{"--indexes--"}, idx...)...)
	}
	return out
}
