package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDedupeRoleKeys 老库升级去重:MySQL 时代 idx_role_key 退化为普通索引 +
// 启动引导依赖唯一约束,导致每次启动重复插入内置角色。升级到全局唯一索引前,
// 必须把重复行清理干净(否则建索引失败),且员工的角色引用要改挂到保留行。
func TestDedupeRoleKeys(t *testing.T) {
	Init(filepath.Join(t.TempDir(), "dedup.db"))
	defer func() { _ = DB.Close() }()

	// 模拟老库:迁移建好的已是全局唯一索引,先删掉才能插入历史重复数据。
	if _, err := DB.Exec(`DROP INDEX idx_role_key`); err != nil {
		t.Fatalf("删除索引失败: %v", err)
	}

	// 构造重复数据(role_id 按插入顺序自增):
	//   1=admin(最早,保留) 2=admin(重复,删) 3=admin(已删除,删)
	//   4=staff(保留) 5=staff(重复,删)
	insert := func(key, name, delFlag string) int {
		t.Helper()
		res, err := DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
			VALUES(?,?,'','all',0,1,1,?,?,?,'')`, key, name, delFlag, Now(), Now())
		if err != nil {
			t.Fatalf("构造重复角色失败: %v", err)
		}
		id, _ := res.LastInsertId()
		return int(id)
	}
	id1 := insert("admin", "超管1", "0")
	id2 := insert("admin", "超管2", "0")
	id3 := insert("admin", "超管3", "1")
	id4 := insert("staff", "员工A", "0")
	id5 := insert("staff", "员工B", "0")

	// 两名员工分别挂在保留行与将被删除的重复行上。
	insUser := func(name string, roleID int) {
		t.Helper()
		if _, err := DB.Exec(`INSERT INTO tb_user(username, password_hash, real_name, role_id, status, del_flag, create_time, update_time)
			VALUES(?,?,?,?,1,'0',?,?)`, name, "x", name, roleID, Now(), Now()); err != nil {
			t.Fatalf("构造员工失败: %v", err)
		}
	}
	insUser("keep_ref", id1)
	insUser("moved_ref", id2)

	dedupeRoleKeys()

	// 重复行被物理删除;每个 role_key 只剩一行,且保留的是未删除行中最早的。
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_role`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("去重后应只剩 2 行, got %d err=%v", n, err)
	}
	var del int
	DB.QueryRow(`SELECT COUNT(*) FROM tb_role WHERE role_id IN (?,?,?)`, id2, id3, id5).Scan(&del)
	if del != 0 {
		t.Fatalf("重复行应被删除, 仍剩 %d 行", del)
	}
	for _, keep := range []int{id1, id4} {
		var one int
		DB.QueryRow(`SELECT COUNT(*) FROM tb_role WHERE role_id=?`, keep).Scan(&one)
		if one != 1 {
			t.Fatalf("保留行 role_id=%d 不应被删除", keep)
		}
	}

	// 员工引用改挂:挂在被删行(id=2)上的应指向保留行(id=1),原挂保留行的不动。
	var moved, kept int
	DB.QueryRow(`SELECT role_id FROM tb_user WHERE username='moved_ref'`).Scan(&moved)
	DB.QueryRow(`SELECT role_id FROM tb_user WHERE username='keep_ref'`).Scan(&kept)
	if moved != id1 {
		t.Fatalf("挂在重复行上的员工应改挂到保留行 %d, got %d", id1, moved)
	}
	if kept != id1 {
		t.Fatalf("原挂保留行的员工引用不应变化, got %d", kept)
	}

	// 幂等:再次执行不再有任何变更。
	dedupeRoleKeys()
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_role`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("重复执行去重应零变更, got %d err=%v", n, err)
	}

	// 去重完成后,建全局唯一索引应能成功,且此后重复插入被拒绝。
	// (实际迁移流程:migrate 的索引循环先建好索引,recreateRoleKeyIndex 只负责
	// 把「已存在但定义旧」的同名索引原地升级;此处索引已被删,直接按新定义补建。)
	if _, err := DB.Exec(`CREATE UNIQUE INDEX idx_role_key ON tb_role(role_key)`); err != nil {
		t.Fatalf("去重后建唯一索引失败: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('admin','重复插入','','all',0,1,1,'0',?,?,'')`, Now(), Now()); err == nil {
		t.Fatal("建唯一索引后,重复 role_key 插入应被拒绝")
	}
}

// TestRecreateRoleKeyIndex SQLite 旧库的 idx_role_key 是带 WHERE 的部分唯一
// 索引,迁移应把它原地重建为全局唯一索引。
func TestRecreateRoleKeyIndex(t *testing.T) {
	Init(filepath.Join(t.TempDir(), "reindex.db"))
	defer func() { _ = DB.Close() }()

	// 模拟旧库定义:删除迁移建好的全局唯一索引,重建为带 WHERE 的部分唯一索引。
	if _, err := DB.Exec(`DROP INDEX idx_role_key`); err != nil {
		t.Fatalf("删除索引失败: %v", err)
	}
	if _, err := DB.Exec(`CREATE UNIQUE INDEX idx_role_key ON tb_role(role_key) WHERE del_flag='0'`); err != nil {
		t.Fatalf("重建旧定义索引失败: %v", err)
	}
	// 造一条已删除行,验证旧定义下 role_key 允许与已删除行重复、迁移后不允许。
	if _, err := DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('old_key','旧角色','','all',0,1,1,'1',?,?,'')`, Now(), Now()); err != nil {
		t.Fatalf("插入已删除角色失败: %v", err)
	}

	recreateRoleKeyIndex()

	var def string
	if err := DB.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='index' AND name='idx_role_key'`).Scan(&def); err != nil {
		t.Fatalf("查询索引定义失败: %v", err)
	}
	if strings.Contains(strings.ToUpper(def), "WHERE") {
		t.Fatalf("idx_role_key 应已重建为全局唯一索引(无 WHERE), got %q", def)
	}
	if _, err := DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('old_key','撞已删除行','','all',0,1,1,'0',?,?,'')`, Now(), Now()); err == nil {
		t.Fatal("全局唯一索引下,与已删除行同 role_key 的新插入应被拒绝")
	}
}
