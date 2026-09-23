package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
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
