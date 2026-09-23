package dao

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// initUserTestDB 建一个临时库并跑完 migrate + seed,再补角色与管理员引导。
func initUserTestDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "user.db"))
	BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// ============================================================================
// 启动引导
// ============================================================================

func TestSyncBuiltinRolesCreatesFourRoles(t *testing.T) {
	initUserTestDB(t)
	roles, err := ListRoles()
	if err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("应初始化 4 个内置角色, got %d", len(roles))
	}
	for _, r := range roles {
		if r.IsBuiltin != 1 {
			t.Fatalf("角色 %s 应标记为内置", r.RoleKey)
		}
		if r.UserCount < 0 {
			t.Fatalf("角色 %s 的员工数统计异常", r.RoleKey)
		}
	}
}

// TestSyncBuiltinRolesAdminSelfHeals 验证防锁死的最后保险:
// admin 角色权限被人为清空后,重启(再次 SyncBuiltinRoles)会强制恢复为全量;
// 而其它内置角色被商户改过的权限必须保留,不能被覆盖。
func TestSyncBuiltinRolesAdminSelfHeals(t *testing.T) {
	initUserTestDB(t)

	adminID := RoleIDByKey(store.RoleKeyAdmin)
	if adminID == 0 {
		t.Fatal("未找到超级管理员角色")
	}
	cashierID := RoleIDByKey(store.RoleKeyCashier)

	// 人为破坏:清空 admin 权限、给 cashier 改成只剩 table:view。
	if _, err := store.DB.Exec(`UPDATE tb_role SET perms='' WHERE role_id=?`, adminID); err != nil {
		t.Fatalf("清空 admin 权限失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_role SET perms='table:view' WHERE role_id=?`, cashierID); err != nil {
		t.Fatalf("修改 cashier 权限失败: %v", err)
	}

	SyncBuiltinRoles()

	var adminPerms, cashierPerms string
	store.DB.QueryRow(`SELECT perms FROM tb_role WHERE role_id=?`, adminID).Scan(&adminPerms)
	store.DB.QueryRow(`SELECT perms FROM tb_role WHERE role_id=?`, cashierID).Scan(&cashierPerms)

	// admin 的语义就是「全量」,必须对齐权限目录总数而不是写死数字:
	// 权限目录扩容(26 → 28,新增操作日志模块)后,硬编码会在这里误报。
	if got := len(store.ParsePerms(adminPerms)); got != len(store.AllPermCodes()) {
		t.Fatalf("admin 权限应被恢复为全量 %d 项, got %d: %q", len(store.AllPermCodes()), got, adminPerms)
	}
	if cashierPerms != "table:view" {
		t.Fatalf("商户改过的 cashier 权限不应被覆盖, got %q", cashierPerms)
	}
}

func TestEnsureAdminUser(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	t.Setenv("ADMIN_PASS", "boss123456")
	initUserTestDB(t)

	if n := CountUsers(); n != 1 {
		t.Fatalf("应自动创建 1 个管理员账号, got %d", n)
	}
	auth, err := authByUsername(t, "boss")
	if err != nil {
		t.Fatalf("引导账号应可登录查询: %v", err)
	}
	if auth.RoleKey != store.RoleKeyAdmin || auth.Status != po.UserStatusEnabled {
		t.Fatalf("引导账号应为启用中的超级管理员, got role=%s status=%d", auth.RoleKey, auth.Status)
	}
	// 同上:对齐目录总数,避免权限目录扩容时误报。
	if len(auth.Perms) != len(store.AllPermCodes()) {
		t.Fatalf("管理员应拥有全部权限(%d 项), got %d", len(store.AllPermCodes()), len(auth.Perms))
	}

	// 再次引导不应产生重复账号。
	EnsureAdminUser()
	if n := CountUsers(); n != 1 {
		t.Fatalf("重复引导不应新增账号, got %d", n)
	}
}

// TestEnsureAdminUserRestoresDisabledAdmin 验证管理员被误停用后重启可自愈。
func TestEnsureAdminUserRestoresDisabledAdmin(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	uid := currentOnlyUserID(t)
	if _, err := store.DB.Exec(`UPDATE tb_user SET status=? WHERE user_id=?`, po.UserStatusDisabled, uid); err != nil {
		t.Fatalf("停用账号失败: %v", err)
	}
	if CountActiveAdmins() != 0 {
		t.Fatal("停用后应无启用中的超级管理员")
	}

	EnsureAdminUser()

	auth, err := GetAuthByID(uid)
	if err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}
	if auth.Status != po.UserStatusEnabled || auth.RoleKey != store.RoleKeyAdmin {
		t.Fatalf("引导应恢复该账号的启用状态与角色, got status=%d role=%s", auth.Status, auth.RoleKey)
	}
}

// ============================================================================
// 员工写入与防锁死
// ============================================================================

// TestAntiLockoutRules 逐条验证「任何操作组合都不会让系统失去管理员」。
func TestAntiLockoutRules(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	adminID := currentOnlyUserID(t)
	cashierID := RoleIDByKey(store.RoleKeyCashier)

	t.Run("不能停用当前登录账号", func(t *testing.T) {
		err := SetUserStatus(adminID, po.UserStatusDisabled, adminID, "boss")
		if err == nil || !strings.Contains(err.Error(), "不能停用当前登录账号") {
			t.Fatalf("应拒绝停用自己, got %v", err)
		}
	})

	t.Run("不能删除当前登录账号", func(t *testing.T) {
		err := SoftDeleteUser(adminID, adminID, "boss")
		if err == nil || !strings.Contains(err.Error(), "不能删除当前登录账号") {
			t.Fatalf("应拒绝删除自己, got %v", err)
		}
	})

	t.Run("不能修改自己的角色", func(t *testing.T) {
		err := UpdateUserProfile(po.User{UserID: adminID, RoleID: cashierID, RealName: "老板"},
			adminID, "boss")
		if err == nil || !strings.Contains(err.Error(), "不能修改自己的角色") {
			t.Fatalf("应拒绝改自己的角色, got %v", err)
		}
	})

	t.Run("不能停用最后一个启用的超级管理员", func(t *testing.T) {
		err := SetUserStatus(adminID, po.UserStatusDisabled, 0, "system")
		if err == nil || !strings.Contains(err.Error(), "至少一个启用的超级管理员") {
			t.Fatalf("应拒绝停用最后一个管理员, got %v", err)
		}
	})

	t.Run("不能删除最后一个启用的超级管理员", func(t *testing.T) {
		err := SoftDeleteUser(adminID, 0, "system")
		if err == nil || !strings.Contains(err.Error(), "至少一个启用的超级管理员") {
			t.Fatalf("应拒绝删除最后一个管理员, got %v", err)
		}
	})

	t.Run("不能把最后一个管理员降级", func(t *testing.T) {
		err := UpdateUserProfile(po.User{UserID: adminID, RoleID: cashierID, RealName: "老板"},
			0, "system")
		if err == nil || !strings.Contains(err.Error(), "至少一个启用的超级管理员") {
			t.Fatalf("应拒绝降级最后一个管理员, got %v", err)
		}
	})

	t.Run("有第二个管理员时可以降级", func(t *testing.T) {
		id, err := InsertUser(po.User{Username: "boss2", RealName: "副老板", RoleID: RoleIDByKey(store.RoleKeyAdmin)},
			store.HashPassword("boss2-123456"), "boss")
		if err != nil {
			t.Fatalf("创建第二个管理员失败: %v", err)
		}
		if err := UpdateUserProfile(po.User{UserID: adminID, RoleID: cashierID, RealName: "老板"}, 0, "system"); err != nil {
			t.Fatalf("有备用管理员后应允许降级, got %v", err)
		}
		// 还原,避免影响后续子测试
		if err := UpdateUserProfile(po.User{UserID: adminID, RoleID: RoleIDByKey(store.RoleKeyAdmin), RealName: "老板"}, 0, "system"); err != nil {
			t.Fatalf("还原角色失败: %v", err)
		}
		if err := SoftDeleteUser(int(id), 0, "system"); err != nil {
			t.Fatalf("清理第二个管理员失败: %v", err)
		}
	})
}

func TestRoleWriteRules(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	adminRoleID := RoleIDByKey(store.RoleKeyAdmin)
	managerRoleID := RoleIDByKey(store.RoleKeyManager)

	t.Run("超级管理员角色权限不可修改", func(t *testing.T) {
		err := UpdateRole(po.Role{RoleID: adminRoleID, RoleName: "超级管理员"}, []string{"table:view"}, "boss")
		if err == nil || !strings.Contains(err.Error(), "不可修改") {
			t.Fatalf("应拒绝修改 admin 角色权限, got %v", err)
		}
	})

	t.Run("内置角色不可删除", func(t *testing.T) {
		for _, id := range []int{adminRoleID, managerRoleID, RoleIDByKey(store.RoleKeyCashier), RoleIDByKey(store.RoleKeyStaff)} {
			if err := SoftDeleteRole(id, "boss"); err == nil || !strings.Contains(err.Error(), "不可删除") {
				t.Fatalf("内置角色 %d 不应被删除, got %v", id, err)
			}
		}
	})

	t.Run("角色标识不可与内置角色重名", func(t *testing.T) {
		_, err := InsertRole(po.Role{RoleKey: store.RoleKeyAdmin, RoleName: "冒牌管理员"}, []string{"table:view"}, "boss")
		if err == nil || !strings.Contains(err.Error(), "内置角色保留") {
			t.Fatalf("应拒绝占用内置角色标识, got %v", err)
		}
	})

	t.Run("被员工引用的角色不可删除", func(t *testing.T) {
		rid, err := InsertRole(po.Role{RoleKey: "shift_lead", RoleName: "值班经理"},
			[]string{"order:view", "order:operate"}, "boss")
		if err != nil {
			t.Fatalf("创建自定义角色失败: %v", err)
		}
		if _, err := InsertUser(po.User{Username: "lead1", RealName: "值班", RoleID: int(rid)},
			store.HashPassword("lead1-123456"), "boss"); err != nil {
			t.Fatalf("创建员工失败: %v", err)
		}
		err = SoftDeleteRole(int(rid), "boss")
		if err == nil || !strings.Contains(err.Error(), "还有 1 名员工") {
			t.Fatalf("应拒绝删除仍被引用的角色, got %v", err)
		}
	})

	t.Run("自定义角色权限写入时自动补 view", func(t *testing.T) {
		rid, err := InsertRole(po.Role{RoleKey: "kitchen_only", RoleName: "只管后厨"},
			[]string{"order:operate"}, "boss")
		if err != nil {
			t.Fatalf("创建角色失败: %v", err)
		}
		r, err := GetRoleByID(int(rid))
		if err != nil {
			t.Fatalf("查询角色失败: %v", err)
		}
		if !store.HasPermCode(r.PermList, "order:view") {
			t.Fatalf("order:operate 应自动带上 order:view, got %v", r.PermList)
		}
	})
}

func TestUsernameUniqueness(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	_, err := InsertUser(po.User{Username: "boss", RoleID: RoleIDByKey(store.RoleKeyStaff)},
		store.HashPassword("dup-123456"), "boss")
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("重复用户名应被拒绝, got %v", err)
	}

	// 删除后同名可以重新使用(部分唯一索引只约束未删除行)。
	uid, err := InsertUser(po.User{Username: "waiter1", RoleID: RoleIDByKey(store.RoleKeyStaff)},
		store.HashPassword("waiter-123456"), "boss")
	if err != nil {
		t.Fatalf("创建员工失败: %v", err)
	}
	if err := SoftDeleteUser(int(uid), 0, "boss"); err != nil {
		t.Fatalf("删除员工失败: %v", err)
	}
	if _, err := InsertUser(po.User{Username: "waiter1", RoleID: RoleIDByKey(store.RoleKeyStaff)},
		store.HashPassword("waiter-123456"), "boss"); err != nil {
		t.Fatalf("删除后应可重新使用同名账号, got %v", err)
	}
}

// TestPasswordResetInvalidatesToken 验证改密后 token_version 递增
// (鉴权中间件据此让旧令牌立即失效)。
func TestPasswordResetInvalidatesToken(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	uid := currentOnlyUserID(t)
	before, err := GetAuthByID(uid)
	if err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}
	if err := SetUserPassword(uid, store.HashPassword("new-pass-123"), "boss"); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}
	after, err := GetAuthByID(uid)
	if err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}
	if after.TokenVersion != before.TokenVersion+1 {
		t.Fatalf("改密后 token_version 应 +1, got %d -> %d", before.TokenVersion, after.TokenVersion)
	}
	if !store.VerifyPassword("new-pass-123", GetUserPasswordHash(uid)) {
		t.Fatal("新密码应校验通过")
	}
	if store.VerifyPassword("旧密码不对", GetUserPasswordHash(uid)) {
		t.Fatal("错误密码不应通过")
	}
	if SetUserPassword(99999, "x", "boss") == nil {
		t.Fatal("对不存在的账号重置密码应报错")
	}
}

func TestListUsersFilters(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	staffRole := RoleIDByKey(store.RoleKeyStaff)
	if _, err := InsertUser(po.User{Username: "waiter1", RealName: "张服务", Phone: "13800000001", RoleID: staffRole},
		store.HashPassword("waiter-123456"), "boss"); err != nil {
		t.Fatalf("创建员工失败: %v", err)
	}
	if _, err := InsertUser(po.User{Username: "cook1", RealName: "李后厨", RoleID: staffRole},
		store.HashPassword("cook-123456"), "boss"); err != nil {
		t.Fatalf("创建员工失败: %v", err)
	}

	total, list, err := ListUsers(UserQuery{}, 1, 10)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("应有 3 个账号(零值 UserQuery 必须表示「不限」,曾因 Status 零值与停用同码而误过滤), got total=%d len=%d",
			total, len(list))
	}
	// 中文姓名可被关键字命中(COALESCE 处理过 NULL 的历史行)。
	_, list, err = ListUsers(UserQuery{Keyword: "后厨"}, 1, 10)
	if err != nil || len(list) != 1 || list[0].Username != "cook1" {
		t.Fatalf("按姓名筛选应命中 cook1, got %v (err=%v)", list, err)
	}
	// 按手机号前缀也可命中。
	_, list, err = ListUsers(UserQuery{Keyword: "1380000"}, 1, 10)
	if err != nil || len(list) != 1 || list[0].Username != "waiter1" {
		t.Fatalf("按手机号筛选应命中 waiter1, got %v (err=%v)", list, err)
	}
	// 状态筛选:全部启用时按停用筛选应为空。
	disabled := po.UserStatusDisabled
	_, list, err = ListUsers(UserQuery{Status: &disabled}, 1, 10)
	if err != nil || len(list) != 0 {
		t.Fatalf("按停用筛选应为空, got %v (err=%v)", list, err)
	}
	// 按启用筛选应命中全部 3 个。
	enabled := po.UserStatusEnabled
	if _, list, err = ListUsers(UserQuery{Status: &enabled}, 1, 10); err != nil || len(list) != 3 {
		t.Fatalf("按启用筛选应命中 3 个, got %v (err=%v)", list, err)
	}
	// 按角色筛选。
	if _, list, err = ListUsers(UserQuery{RoleID: staffRole}, 1, 10); err != nil || len(list) != 2 {
		t.Fatalf("按员工角色筛选应命中 2 个, got %v (err=%v)", list, err)
	}
	if err := SetUserStatus(currentOnlyUserID(t), po.UserStatusDisabled, 0, "s"); err == nil {
		// 唯一管理员不可停用,这里只确认该规则仍在生效
		t.Fatal("唯一管理员不应可停用")
	}
	// 分页:每页 2 条。
	total, page1, err := ListUsers(UserQuery{}, 1, 2)
	if err != nil || total != 3 || len(page1) != 2 {
		t.Fatalf("分页应返回 total=3 / 首页 2 条, got total=%d len=%d (err=%v)", total, len(page1), err)
	}
}

// TestDisplayNameFallback 留痕显示名:优先姓名,未填回退登录名。
func TestDisplayNameFallback(t *testing.T) {
	initUserTestDB(t)
	a := &AuthInfo{Username: "zhangsan", RealName: "张三"}
	if a.DisplayName() != "张三" {
		t.Fatalf("有姓名时应返回姓名, got %s", a.DisplayName())
	}
	b := &AuthInfo{Username: "lisi", RealName: "  "}
	if b.DisplayName() != "lisi" {
		t.Fatalf("姓名空白时应回退登录名, got %s", b.DisplayName())
	}
	u := &po.User{Username: "wangwu"}
	if u.DisplayName() != "wangwu" {
		t.Fatalf("po.User 姓名缺失时应回退登录名, got %s", u.DisplayName())
	}
}

// ---- 小工具 ----

// currentOnlyUserID 取当前库里唯一的账号 ID(测试里即启动引导创建的管理员)。
func currentOnlyUserID(t *testing.T) int {
	t.Helper()
	var id int
	if err := store.DB.QueryRow(`SELECT user_id FROM tb_user WHERE del_flag='0' ORDER BY user_id LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("查询引导账号失败: %v", err)
	}
	return id
}

func authByUsername(t *testing.T, username string) (*AuthInfo, error) {
	t.Helper()
	return GetAuthByUsername(username)
}
