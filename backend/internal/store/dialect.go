// 数据库方言适配层。
//
// 设计目标:同一套业务代码(SQL 语句、建表逻辑、种子数据)同时支持
// SQLite(默认,零依赖单机部署)与 MySQL(多实例/高并发部署),
// 切换方式仅为调整环境变量,不需要改动任何业务逻辑。
//
// 为什么要在 Go 层做适配,而不是只依赖两份 SQL 脚本:
//  1. 驱动名与连接串格式完全不同;
//  2. 自增主键关键字不同(AUTOINCREMENT / AUTO_INCREMENT);
//  3. CREATE INDEX IF NOT EXISTS 是 SQLite 专有语法,MySQL 不支持;
//  4. 查询「列是否存在」:SQLite 用 PRAGMA table_info,MySQL 用 information_schema;
//  5. MySQL 建表需要显式声明存储引擎与字符集;
//  6. MySQL 不支持「部分索引」(带 WHERE 的索引);
//  7. INSERT OR IGNORE / INSERT OR REPLACE 在 MySQL 中是 INSERT IGNORE / REPLACE。
//
// 需要注意的语义差异(已在文档中说明):
//   - MySQL 会强制校验 VARCHAR 长度,SQLite 不会(超长会被静默截断/忽略);
//   - MySQL 默认 sql_mode 含 STRICT_TRANS_TABLES,类型不匹配会直接报错;
//   - 时间为「字符串」存储(YYYY-MM-DD HH:MM:SS),两库行为一致,便于跨库迁移。
package store

import (
	"database/sql"
	"dining-system/internal/logger"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	// 数据库驱动:两者都通过 database/sql 注册,按运行期方言选用。
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// Dialect 数据库方言。
type Dialect string

const (
	// DialectSQLite 单机文件数据库,默认选项,零外部依赖。
	DialectSQLite Dialect = "sqlite"
	// DialectMySQL MySQL / MariaDB(InnoDB)。
	DialectMySQL Dialect = "mysql"
)

// dialect 当前进程使用的方言,由 Init 确定;默认 SQLite。
//
// 之所以用包级变量而不是到处传参:store 包的查询函数数量多且无状态,
// 传入 context/dialect 会污染所有签名;方言在一次进程生命周期内不会变化。
var dialect = DialectSQLite

// ActiveDialect 返回当前生效的数据库方言。
func ActiveDialect() Dialect { return dialect }

// IsMySQL 报告当前是否运行在 MySQL 后端。
func IsMySQL() bool { return dialect == DialectMySQL }

// autoIncPKFor 返回「自增主键」列定义。
//
//	SQLite: INTEGER PRIMARY KEY AUTOINCREMENT
//	MySQL:  INT NOT NULL AUTO_INCREMENT PRIMARY KEY
func autoIncPKFor(d Dialect) string {
	if d == DialectMySQL {
		return "INT NOT NULL AUTO_INCREMENT PRIMARY KEY"
	}
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// tableOptionsFor 供建表语句生成器使用。
func tableOptionsFor(d Dialect) string {
	if d == DialectMySQL {
		return " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"
	}
	return ""
}

// InsertIgnoreInto 生成「插入,遇主键/唯一键冲突则忽略」的语句。
//
//	SQLite: INSERT OR IGNORE INTO t(a,b) VALUES(?,?)
//	MySQL:  INSERT IGNORE INTO t(a,b) VALUES(?,?)
func InsertIgnoreInto(table string, cols ...string) string {
	verb := "INSERT OR IGNORE INTO"
	if IsMySQL() {
		verb = "INSERT IGNORE INTO"
	}
	return verb + " " + table + "(" + strings.Join(cols, ",") + ") VALUES(" + placeholders(len(cols)) + ")"
}

// InsertReplaceInto 生成「按主键整体替换写入」的语句。
//
//	SQLite: INSERT OR REPLACE INTO t(a,b) VALUES(?,?)
//	MySQL:  REPLACE INTO t(a,b) VALUES(?,?)
func InsertReplaceInto(table string, cols ...string) string {
	verb := "INSERT OR REPLACE INTO"
	if IsMySQL() {
		verb = "REPLACE INTO"
	}
	return verb + " " + table + "(" + strings.Join(cols, ",") + ") VALUES(" + placeholders(len(cols)) + ")"
}

// insertIgnoreIntoFor / insertReplaceIntoFor 供 SQL 脚本生成器使用。
func insertIgnoreIntoFor(d Dialect, table string, cols ...string) string {
	verb := "INSERT OR IGNORE INTO"
	if d == DialectMySQL {
		verb = "INSERT IGNORE INTO"
	}
	return verb + " " + table + "(" + strings.Join(cols, ",") + ") VALUES(" + placeholders(len(cols)) + ")"
}

// placeholders 返回 n 个「?」占位符(两种数据库都使用 ? 作为参数占位符)。
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ---------------- 元数据查询(建表/升级时使用) ----------------

// columnExists 报告表中是否已存在指定列。
func columnExists(table, column string) bool {
	if IsMySQL() {
		var n int
		err := DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&n)
		return err == nil && n > 0
	}
	rows, err := DB.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == column {
			return true
		}
	}
	return false
}

// indexExists 报告当前库中是否已存在指定名字的索引。
func indexExists(name string) bool {
	if IsMySQL() {
		var n int
		err := DB.QueryRow(`SELECT COUNT(*) FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA=DATABASE() AND INDEX_NAME=?`, name).Scan(&n)
		return err == nil && n > 0
	}
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n)
	return err == nil && n > 0
}

// ---------------- 连接配置 ----------------

// dbConfig 一次初始化所需的连接参数。
type dbConfig struct {
	Dialect Dialect
	Driver  string // sql.DB 驱动名
	DSN     string // 真实连接串(MySQL 含密码)
	SafeDSN string // 日志展示用(密码已脱敏)
}

// sqliteDSN 把 SQLite 文件路径转换为带「逐连接 PRAGMA」参数的 file: URI。
//
// 为什么必须写进 DSN 而不能在启动时 Exec 一遍 PRAGMA:
// database/sql 是连接池(MaxOpenConns=16),启动时对其中「某一个连接」
// 执行 PRAGMA 只会影响那一个连接;池中其他连接、以及后续被重新建立的连接
// 都不会带上 busy_timeout / foreign_keys,结果是并发写时出现
// "database is locked"、外键约束形同虚设。
// modernc.org/sqlite 会对 DSN 里的每个 _pragma=xxx(yyy) 参数在
// 「每个新连接建立时」逐条执行 PRAGMA(见驱动 applyQueryParams),
// 因此放进 DSN 才能保证所有连接行为一致。
//
// 路径转换:Windows 盘符路径统一转成正斜杠(C:\x\y → C:/x/y),SQLite 的
// URI 解析器可直接接受;相对路径(如 ./dining.db)同样合法。
// 注意:路径中若包含 '?' 或 '#' 会破坏 URI,属异常场景,不做处理。
func sqliteDSN(path string) string {
	p := filepath.ToSlash(strings.TrimSpace(path))
	return "file:" + p + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
}

// resolveDBConfig 解析环境变量得到连接配置。
//
//	DB_DRIVER   sqlite | mysql   (默认 sqlite;未显式指定但设置了 DB_DSN 时按 mysql 处理)
//	DB_PATH     SQLite 数据文件   (默认 ./dining.db)
//	DB_DSN      MySQL 连接串      (设置后优先于下面的分项配置)
//	DB_HOST     MySQL 主机        (默认 127.0.0.1)
//	DB_PORT     MySQL 端口        (默认 3306)
//	DB_USER     MySQL 用户        (默认 root)
//	DB_PASSWORD MySQL 密码        (默认空)
//	DB_NAME     MySQL 库名        (默认 dining)
//	DB_PARAMS   MySQL 附加参数    (默认 charset=utf8mb4&parseTime=true&loc=Local)
func resolveDBConfig(sqlitePath string) (dbConfig, error) {
	drv := strings.ToLower(strings.TrimSpace(Getenv("DB_DRIVER", "")))
	if drv == "" {
		if strings.TrimSpace(Getenv("DB_DSN", "")) != "" {
			drv = string(DialectMySQL)
		} else {
			drv = string(DialectSQLite)
		}
	}

	switch Dialect(drv) {
	case DialectMySQL:
		dsn := strings.TrimSpace(Getenv("DB_DSN", ""))
		if dsn == "" {
			params := Getenv("DB_PARAMS", "charset=utf8mb4&parseTime=true&loc=Local")
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s",
				Getenv("DB_USER", "root"),
				Getenv("DB_PASSWORD", ""),
				Getenv("DB_HOST", "127.0.0.1"),
				Getenv("DB_PORT", "3306"),
				Getenv("DB_NAME", "dining"),
				params,
			)
		}
		return dbConfig{Dialect: DialectMySQL, Driver: "mysql", DSN: dsn, SafeDSN: maskDSN(dsn)}, nil

	case DialectSQLite:
		p := strings.TrimSpace(sqlitePath)
		if p == "" {
			p = Getenv("DB_PATH", "./dining.db")
		}
		return dbConfig{Dialect: DialectSQLite, Driver: "sqlite", DSN: sqliteDSN(p), SafeDSN: p}, nil

	default:
		return dbConfig{}, fmt.Errorf("不支持的 DB_DRIVER=%q(可选:sqlite / mysql)", drv)
	}
}

// maskDSN 隐藏连接串中的密码,便于安全打印日志。
//
//	root:secret@tcp(127.0.0.1:3306)/dining -> root:***@tcp(127.0.0.1:3306)/dining
func maskDSN(dsn string) string {
	at := strings.LastIndex(dsn, "@")
	if at < 0 {
		return dsn
	}
	cred := dsn[:at]
	colon := strings.Index(cred, ":")
	if colon < 0 {
		return dsn
	}
	return cred[:colon] + ":***" + dsn[at:]
}

// applyPool 按方言设置连接池。
// MySQL 通常由服务端维护连接,需要限制生命周期避免被服务端 wait_timeout 断开;
// SQLite 是本地文件,连接数过多反而加剧写锁竞争,保持小池。
func applyPool(d Dialect) {
	if d == DialectMySQL {
		DB.SetMaxOpenConns(64)
		DB.SetMaxIdleConns(16)
		DB.SetConnMaxLifetime(30 * time.Minute)
		DB.SetConnMaxIdleTime(5 * time.Minute)
		return
	}
	DB.SetMaxOpenConns(16)
	DB.SetMaxIdleConns(16)
}

// InitFromEnv 按环境变量选择数据库后端并初始化(生产入口)。
func InitFromEnv() { Init("") }

// Init 打开数据库、执行建表迁移与种子数据。
//
// sqlitePath 仅在 SQLite 方言下生效;传空串时回退到 DB_PATH 环境变量。
// MySQL 下连接信息完全来自环境变量,该参数被忽略。
func Init(sqlitePath string) {
	cfg, err := resolveDBConfig(sqlitePath)
	if err != nil {
		logger.Fatalf("数据库配置错误: %v", err)
	}
	dialect = cfg.Dialect

	DB, err = sql.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		logger.Fatalf("打开 %s 数据库失败: %v", cfg.Dialect, err)
	}
	applyPool(cfg.Dialect)

	if err := DB.Ping(); err != nil {
		logger.Fatalf("连接 %s 数据库失败(%s): %v", cfg.Dialect, cfg.SafeDSN, mysqlHint(err))
	}

	if cfg.Dialect == DialectSQLite {
		// WAL / busy_timeout / foreign_keys 已通过 DSN 的 _pragma 参数逐连接生效,
		// 见 sqliteDSN();这里不能再 Exec 一遍 —— 连接池中那只对单个连接有效。
		logger.Infof("[db] SQLite PRAGMA(逐连接): busy_timeout=5000, foreign_keys=ON, journal_mode=WAL")
	}

	logger.Infof("[db] 已连接 %s 后端: %s", cfg.Dialect, cfg.SafeDSN)
	migrate()
	seed()
}

// mysqlHint 针对最常见的 MySQL 连接失败给出中文排查提示。
func mysqlHint(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Access denied"):
		return err.Error() + " → 账号或密码错误,请检查 DB_USER / DB_PASSWORD"
	case strings.Contains(msg, "Unknown database"):
		return err.Error() + " → 目标库不存在,请先执行 CREATE DATABASE(见 migrations/full/mysql/schema.sql 头部)"
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "i/o timeout"):
		return err.Error() + " → 无法连接 MySQL 服务,请检查 DB_HOST / DB_PORT 与防火墙"
	default:
		return msg
	}
}
