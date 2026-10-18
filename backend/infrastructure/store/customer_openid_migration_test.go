package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"dining-system/domain/po"
)

// TestMigrateCustomerOpenidFromLegacyDB P2 的核心迁移:老库升级。
//
// 老库的 tb_customer 还带着 openid(NOT NULL + 唯一索引),升级必须一次性做到:
//  1. 把每条 openid 搬进 tb_customer_identity(provider='wx_mp');
//  2. 删掉唯一索引(SQLite 不允许删除仍被索引引用的列;且它只允许一行空串);
//  3. 删掉 openid 列。
//
// 失败的表现不是报错,而是「升级后新会员注册不了」或「非微信登录永远建不了档」,
// 所以必须由测试在提交前拦住。
func TestMigrateCustomerOpenidFromLegacyDB(t *testing.T) {
	t.Setenv("DB_DRIVER", string(DialectSQLite))
	t.Setenv("DB_DSN", "")

	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyCustomerTable(t, path)

	// 走与线上完全相同的启动路径(migrate + seed),不直接调迁移函数。
	Init(path)
	t.Cleanup(func() { _ = DB.Close() })

	if columnExists("tb_customer", "openid") {
		t.Fatal("启动迁移后 tb_customer.openid 应已删除")
	}
	if indexExists("idx_customer_openid") {
		t.Fatal("旧的 idx_customer_openid 应已删除")
	}

	// 每条 openid 都搬成了 wx_mp 身份,并指向原来的 customer_id。
	for _, tc := range []struct {
		openid string
		id     int
	}{
		{"legacy-openid-1", 1},
		{"legacy-openid-2", 2},
	} {
		var cid int
		if err := DB.QueryRow(`SELECT customer_id FROM tb_customer_identity WHERE provider=? AND external_id=?`,
			po.CustomerProviderWxMP, tc.openid).Scan(&cid); err != nil {
			t.Fatalf("openid %s 未搬迁到 identity 表: %v", tc.openid, err)
		}
		if cid != tc.id {
			t.Fatalf("openid %s 指向的会员不符: want %d got %d", tc.openid, tc.id, cid)
		}
	}
	// 搬迁保留档案本身的字段(昵称 / 创建时间),不能只搬出一个空壳。
	var nickname, createTime string
	if err := DB.QueryRow(`SELECT nickname, COALESCE(create_time,'') FROM tb_customer WHERE customer_id=1`).
		Scan(&nickname, &createTime); err != nil {
		t.Fatalf("读取搬迁后的档案失败: %v", err)
	}
	if nickname != "老客" || createTime == "" {
		t.Fatalf("档案字段未保留: nickname=%q createTime=%q", nickname, createTime)
	}

	// 迁移后必须能新增「没有 openid」的档案 —— 旧唯一索引下第二行空串会被拒绝,
	// 这正是 P2 要解决的核心问题。
	if _, err := DB.Exec(`INSERT INTO tb_customer(nickname) VALUES('新客')`); err != nil {
		t.Fatalf("迁移后应能新增无 openid 的档案: %v", err)
	}

	// 幂等:列已不在,再跑一次迁移直接返回,不应报错。
	migrateCustomerOpenid()
}

// seedLegacyCustomerTable 造一个「P2 之前」的库:只有 tb_customer,带 openid 与唯一索引。
// 其余表由 Init 的建表迁移补齐 —— 这也正是老库升级的真实形态。
func seedLegacyCustomerTable(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("打开老库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, stmt := range []string{
		`CREATE TABLE tb_customer (
			customer_id      INTEGER PRIMARY KEY AUTOINCREMENT,
			openid           VARCHAR(64)  NOT NULL,
			nickname         VARCHAR(64)  DEFAULT '',
			avatar_url       VARCHAR(255) DEFAULT '',
			status           INTEGER      DEFAULT 1,
			order_count      INTEGER      DEFAULT 0,
			last_order_time  VARCHAR(32)  DEFAULT NULL,
			last_login_time  VARCHAR(32)  DEFAULT NULL,
			remark           VARCHAR(255) DEFAULT '',
			create_time      VARCHAR(32)  DEFAULT (datetime('now','localtime')),
			update_time      VARCHAR(32)  DEFAULT NULL
		)`,
		`CREATE UNIQUE INDEX idx_customer_openid ON tb_customer(openid)`,
		`INSERT INTO tb_customer(openid, nickname, create_time, last_login_time)
			VALUES('legacy-openid-1', '老客', '2026-10-01 10:00:00', '2026-10-17 09:00:00')`,
		`INSERT INTO tb_customer(openid, nickname, create_time)
			VALUES('legacy-openid-2', '', '2026-10-02 10:00:00')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("造老库失败: %v\n%s", err, stmt)
		}
	}
}
