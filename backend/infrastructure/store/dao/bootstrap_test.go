package dao

import (
	"path/filepath"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
)

// bootDaoInitDB 只初始化数据库,不执行任何角色/管理员引导,便于精确控制引导时机。
func bootDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "bootstrap.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// bootDaoRoleCount 统计未删除角色数量。
func bootDaoRoleCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_role WHERE del_flag='0'`).Scan(&n); err != nil {
		t.Fatalf("统计角色失败: %v", err)
	}
	return n
}

// bootDaoAdminPerms 返回 admin 角色的权限 CSV。
func bootDaoAdminPerms(t *testing.T) string {
	t.Helper()
	id := RoleIDByKey(store.RoleKeyAdmin)
	if id == 0 {
		t.Fatal("未找到 admin 角色")
	}
	var perms string
	if err := store.DB.QueryRow(`SELECT COALESCE(perms,'') FROM tb_role WHERE role_id=?`, id).Scan(&perms); err != nil {
		t.Fatalf("查询 admin 权限失败: %v", err)
	}
	return perms
}

func TestBootstrapRolesIdempotent(t *testing.T) {
	bootDaoInitDB(t)

	BootstrapRoles()
	roleCount1 := bootDaoRoleCount(t)
	adminPerms1 := bootDaoAdminPerms(t)
	admins1 := CountActiveAdmins()

	if roleCount1 != 4 {
		t.Fatalf("应初始化 4 个内置角色, got %d", roleCount1)
	}
	if got := len(store.ParsePerms(adminPerms1)); got != len(store.AllPermCodes()) {
		t.Fatalf("admin 应为全量权限 %d 项, got %d", len(store.AllPermCodes()), got)
	}
	if admins1 != 1 {
		t.Fatalf("应自动创建 1 个管理员, got %d", admins1)
	}

	// 再次引导必须幂等:角色数、admin 权限、管理员数都不变。
	BootstrapRoles()
	if got := bootDaoRoleCount(t); got != roleCount1 {
		t.Fatalf("重复引导后角色数变化: %d -> %d", roleCount1, got)
	}
	if got := bootDaoAdminPerms(t); got != adminPerms1 {
		t.Fatalf("重复引导后 admin 权限变化: %q -> %q", adminPerms1, got)
	}
	if got := CountActiveAdmins(); got != admins1 {
		t.Fatalf("重复引导后管理员数变化: %d -> %d", admins1, got)
	}
}

// TestSyncBuiltinRolesWithoutUniqueIndex 模拟 MySQL 下 idx_role_key 的降级:
// MySQL 不支持带 WHERE 的部分索引,唯一索引会退化为普通索引,INSERT IGNORE
// 永远不会因 role_key 重复而忽略。幂等必须由应用层先查后插保证,否则每次
// 启动都会重复插入 4 个内置角色(tb_role 数据随部署不断膨胀)。
func TestSyncBuiltinRolesWithoutUniqueIndex(t *testing.T) {
	bootDaoInitDB(t)
	SyncBuiltinRoles()
	before := bootDaoRoleCount(t)
	if before != 4 {
		t.Fatalf("应初始化 4 个内置角色, got %d", before)
	}

	// 模拟 MySQL 降级:删掉 role_key 的唯一索引,让 INSERT IGNORE 失去约束保护。
	if _, err := store.DB.Exec(`DROP INDEX IF EXISTS idx_role_key`); err != nil {
		t.Fatalf("删除唯一索引失败: %v", err)
	}

	SyncBuiltinRoles()
	if got := bootDaoRoleCount(t); got != before {
		t.Fatalf("唯一索引缺失时重复引导后角色数变化: %d -> %d", before, got)
	}
}

// TestSyncBuiltinRolesRevivesDeletedBuiltin 内置角色被外部手段软删后(正常业务
// 删不掉,出现即说明数据被外部改过),再次引导应复活原行而不是任其缺失。
// 全局唯一索引下软删除行仍占用 role_key,只靠 INSERT IGNORE 会让角色永远缺位。
func TestSyncBuiltinRolesRevivesDeletedBuiltin(t *testing.T) {
	bootDaoInitDB(t)
	SyncBuiltinRoles()
	if _, err := store.DB.Exec(`UPDATE tb_role SET del_flag='1' WHERE role_key=?`, store.RoleKeyStaff); err != nil {
		t.Fatalf("软删除 staff 角色失败: %v", err)
	}

	SyncBuiltinRoles()

	if got := bootDaoRoleCount(t); got != 4 {
		t.Fatalf("内置角色应被复活为 4 个, got %d", got)
	}
	if RoleIDByKey(store.RoleKeyStaff) == 0 {
		t.Fatal("staff 角色应恢复为未删除状态")
	}
}

// TestRoleKeyUniqueIndexGlobal role_key 全局唯一索引:任何路径(含多实例并发
// 启动)插入重复标识都必须被数据库拒绝,应用层查重只是快速路径。
func TestRoleKeyUniqueIndexGlobal(t *testing.T) {
	bootDaoInitDB(t)
	SyncBuiltinRoles()
	_, err := store.DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('manager','假店长','','all',0,99,1,'0',?,?,'')`, store.Now(), store.Now())
	if err == nil {
		t.Fatal("插入与内置角色重复的 role_key 应被唯一索引拒绝")
	}
}

// TestInsertRoleRevivesSoftDeletedKey 删除自定义角色后重建同名角色:全局唯一
// 索引下软删除行仍占用 role_key,应复活原行(role_id 不变、总数不变),而不是
// 撞唯一约束报错或另插新行。
func TestInsertRoleRevivesSoftDeletedKey(t *testing.T) {
	bootDaoInitDB(t)
	SyncBuiltinRoles()
	id1, err := InsertRole(po.Role{RoleKey: "shift_lead", RoleName: "领班", SortOrder: 10}, []string{"order:view"}, "tester")
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	if err := SoftDeleteRole(int(id1), "tester"); err != nil {
		t.Fatalf("删除角色失败: %v", err)
	}
	deletedCount := bootDaoRoleCount(t)

	id2, err := InsertRole(po.Role{RoleKey: "shift_lead", RoleName: "值班领班", SortOrder: 11, Remark: "重建"}, []string{"order:view", "order:operate"}, "tester")
	if err != nil {
		t.Fatalf("重建同名角色应复活原行而不是报错: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("应复活原行(role_id=%d), got %d", id1, id2)
	}
	if got := bootDaoRoleCount(t); got != deletedCount+1 {
		t.Fatalf("复活后角色总数应 +1: %d -> %d", deletedCount, got)
	}
	// 复活行的名称与权限应为重建时的新值。
	r, err := GetRoleByID(int(id2))
	if err != nil || r == nil {
		t.Fatalf("查询复活角色失败: %v", err)
	}
	if r.RoleName != "值班领班" || r.Perms != "order:view,order:operate" {
		t.Fatalf("复活行应为重建时的新值, got %+v", r)
	}
}

func TestNormalizeRolePermsCleansDirtyPerms(t *testing.T) {
	bootDaoInitDB(t)

	// 先补齐内置角色,再插入一个带脏权限的自定义角色。
	SyncBuiltinRoles()
	if _, err := store.DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('dirty_role','脏权限角色','table:edit,gone:perm,order:settle','all',0,100,1,'0',?,?,'')`, store.Now(), store.Now()); err != nil {
		t.Fatalf("插入脏权限角色失败: %v", err)
	}

	NormalizeRolePerms()

	var got string
	if err := store.DB.QueryRow(`SELECT COALESCE(perms,'') FROM tb_role WHERE role_key='dirty_role'`).Scan(&got); err != nil {
		t.Fatalf("查询归一化结果失败: %v", err)
	}
	want := store.JoinPerms([]string{"table:edit", "order:settle"})
	if got != want {
		t.Fatalf("归一化结果错误: got %q want %q", got, want)
	}
	if store.HasPermCode(store.ParsePerms(got), "gone:perm") {
		t.Fatal("已下线权限码不应残留")
	}
	if !store.HasPermCode(store.ParsePerms(got), "order:view") {
		t.Fatal("order:settle 应隐含补齐 order:view")
	}

	// 幂等:再次归一化结果不变(已归一化则不写库)。
	NormalizeRolePerms()
	var again string
	if err := store.DB.QueryRow(`SELECT COALESCE(perms,'') FROM tb_role WHERE role_key='dirty_role'`).Scan(&again); err != nil {
		t.Fatalf("查询归一化结果失败: %v", err)
	}
	if again != want {
		t.Fatalf("重复归一化结果变化: %q -> %q", got, again)
	}
}

func TestEnsureAdminUserCreatesWhenAbsent(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	t.Setenv("ADMIN_PASS", "boss123456")
	bootDaoInitDB(t)

	SyncBuiltinRoles()
	if CountActiveAdmins() != 0 {
		t.Fatal("引导前不应有启用管理员")
	}

	EnsureAdminUser()

	if CountUsers() != 1 {
		t.Fatalf("应创建 1 个管理员, got %d", CountUsers())
	}
	u, err := GetUserByUsername("boss")
	if err != nil || u == nil {
		t.Fatalf("引导管理员应存在: %v", err)
	}
	if u.RoleID != RoleIDByKey(store.RoleKeyAdmin) || u.Status != po.UserStatusEnabled {
		t.Fatalf("引导管理员应为启用中的超级管理员, got role=%d status=%d", u.RoleID, u.Status)
	}
}

// TestEnsureAdminUserRejectsInvalidUsername 引导建号必须拒绝非法用户名字符集。
//
// 登录令牌的载荷用 '|' 做字段分隔符:ADMIN_USER 里含 '|' 时建号与登录都能过,
// 但该账号签发的令牌载荷会被拆成 5 段而永远校验失败 —— 表现为「登录成功、
// 之后每个接口 401」,极难排查。这里应回退默认账号并告警,而不是建出这种账号。
func TestEnsureAdminUserRejectsInvalidUsername(t *testing.T) {
	t.Setenv("ADMIN_USER", "bad|name")
	bootDaoInitDB(t)

	SyncBuiltinRoles()
	EnsureAdminUser()

	if u, err := GetUserByUsername("bad|name"); err == nil && u != nil {
		t.Fatal("含 '|' 的 ADMIN_USER 不应被建号")
	}
	u, err := GetUserByUsername(conf.DefaultAdminUser)
	if err != nil || u == nil {
		t.Fatalf("应回退创建默认管理员 %s: %v", conf.DefaultAdminUser, err)
	}
	if u.RoleID != RoleIDByKey(store.RoleKeyAdmin) {
		t.Fatalf("回退创建的管理员应为超级管理员, got role=%d", u.RoleID)
	}
}

func TestEnsureAdminUserNoopWhenPresent(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	bootDaoInitDB(t)

	BootstrapRoles()
	u, err := GetUserByUsername("boss")
	if err != nil || u == nil {
		t.Fatalf("引导管理员应存在: %v", err)
	}
	beforeHash := GetUserPasswordHash(u.UserID)

	EnsureAdminUser()

	if got := CountUsers(); got != 1 {
		t.Fatalf("已有管理员时不应新增账号, got %d", got)
	}
	after, err := GetUserByUsername("boss")
	if err != nil || after == nil {
		t.Fatalf("管理员应仍存在: %v", err)
	}
	if after.UserID != u.UserID {
		t.Fatalf("管理员账号不应变化: %d -> %d", u.UserID, after.UserID)
	}
	if GetUserPasswordHash(after.UserID) != beforeHash {
		t.Fatal("已有管理员时不应覆盖密码")
	}
}
