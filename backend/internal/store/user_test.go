package store

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/model"
)

// initUserTestDB 建一个临时库并跑完 migrate + seed(含角色与管理员引导)。
func initUserTestDB(t *testing.T) {
	t.Helper()
	Init(filepath.Join(t.TempDir(), "user.db"))
	t.Cleanup(func() { _ = DB.Close() })
}

// ============================================================================
// 权限目录与归一化
// ============================================================================

func TestPermCatalogIntegrity(t *testing.T) {
	codes := AllPermCodes()
	// 28 = 13 个模块 / 28 个权限点。这个数字是「权限目录完整性」的锚:
	// 扩容时必须同步更新内置角色、README 与前端文档,所以刻意保持硬编码 ——
	// 改了目录却忘了别处,这里会立刻提醒你。
	if len(codes) != 28 {
		t.Fatalf("权限点应为 28 个, got %d", len(codes))
	}
	// 目录里不应有重复码,且每个码都必须能被识别。
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("权限码重复: %s", c)
		}
		seen[c] = true
		if !IsPermCode(c) {
			t.Fatalf("权限码 %s 未被索引识别", c)
		}
		if PermName(c) == "" {
			t.Fatalf("权限码 %s 缺少中文名", c)
		}
	}
	// 分组数与分组内权限数之和必须等于总数。
	sum := 0
	for _, g := range PermGroups() {
		if g.Key == "" || g.Name == "" || len(g.Perms) == 0 {
			t.Fatalf("权限分组定义不完整: %+v", g)
		}
		sum += len(g.Perms)
	}
	if sum != len(codes) {
		t.Fatalf("分组内权限数合计 %d 与总数 %d 不一致", sum, len(codes))
	}
}

// TestNormalizePermsEditImpliesView 验证核心规则:非 view 权限自动补同模块 view。
func TestNormalizePermsEditImpliesView(t *testing.T) {
	cases := []struct {
		in       []string
		mustHave []string
	}{
		{[]string{"table:edit"}, []string{"table:view", "table:edit"}},
		{[]string{"order:settle"}, []string{"order:view", "order:settle"}},
		{[]string{"order:operate"}, []string{"order:view", "order:operate"}},
		{[]string{"order:cancel"}, []string{"order:view", "order:cancel"}},
		{[]string{"credit:settle"}, []string{"credit:view", "credit:settle"}},
		{[]string{"refund:operate"}, []string{"refund:view", "refund:operate"}},
		{[]string{"user:edit"}, []string{"user:view", "user:edit"}},
		{[]string{"role:edit"}, []string{"role:view", "role:edit"}},
		{[]string{"config:edit"}, []string{"config:view", "config:edit"}},
	}
	for _, tc := range cases {
		got := NormalizePerms(tc.in)
		for _, need := range tc.mustHave {
			if !HasPermCode(got, need) {
				t.Fatalf("NormalizePerms(%v) 应包含 %s, got %v", tc.in, need, got)
			}
		}
	}
}

// TestNormalizePermsCrossModuleImplies 守住跨模块隐含依赖(2026-09-22 审计修复)。
//
// 起因:挂账管理页的数据实际来自 GET /dining/order/list(要 order:view),
// 员工管理页要读角色下拉(要 role:view)。只勾 credit:view / user:view 会
// 出现「菜单能进、一开页就 403」。内置 4 个角色恰好都同时拥有这两对权限,
// 所以这个坑只在**自定义角色**上暴露 —— 必须靠测试守住,不能靠肉眼看。
func TestNormalizePermsCrossModuleImplies(t *testing.T) {
	cases := []struct {
		in       []string
		mustHave []string
	}{
		{[]string{"credit:view"}, []string{"credit:view", "order:view"}},
		{[]string{"user:view"}, []string{"user:view", "role:view"}},
		// 两环串联:credit:settle -> credit:view(同模块规则) -> order:view(跨模块规则)。
		// 这条最重要 —— 单趟补齐会漏掉第二环,必须迭代到不动点。
		{[]string{"credit:settle"}, []string{"credit:settle", "credit:view", "order:view"}},
		{[]string{"user:edit"}, []string{"user:edit", "user:view", "role:view"}},
	}
	for _, tc := range cases {
		got := NormalizePerms(tc.in)
		for _, need := range tc.mustHave {
			if !HasPermCode(got, need) {
				t.Fatalf("NormalizePerms(%v) 应包含 %s, got %v", tc.in, need, got)
			}
		}
	}
	// 反向不成立:隐含是单向的。否则所有能看订单的人都能进挂账页,
	// 「挂账查看」这个权限点就失去意义了。
	if HasPermCode(NormalizePerms([]string{"order:view"}), "credit:view") {
		t.Fatal("order:view 不应隐含 credit:view")
	}
	if HasPermCode(NormalizePerms([]string{"role:view"}), "user:view") {
		t.Fatal("role:view 不应隐含 user:view")
	}
}

// TestPermImpliesTable 校验跨模块隐含表本身没写坏 —— 这张表是手写的常量,
// 拼错权限码会让规则**静默失效**(不报错、不生效,是最难发现的一类 bug)。
func TestPermImpliesTable(t *testing.T) {
	if len(permImplies) == 0 {
		t.Skip("未定义跨模块隐含关系")
	}
	for k, v := range permImplies {
		if !IsPermCode(k) || !IsPermCode(v) {
			t.Fatalf("permImplies 含未登记的权限码: %q -> %q", k, v)
		}
		if k == v {
			t.Fatalf("permImplies 不能自指: %q", k)
		}
		// 同模块的交给「非 view 隐含 view」规则,重复声明会让两条规则互相漂移。
		if moduleOf(k) == moduleOf(v) {
			t.Fatalf("permImplies[%q]=%q 是同模块,应由隐含 view 规则处理", k, v)
		}
	}
	// 成环检测:成环会把整条链拉满 —— 勾上任意一环等于勾上全部。
	for start := range permImplies {
		seen := map[string]bool{}
		cur := start
		for i := 0; i <= len(permImplies)+1; i++ {
			if seen[cur] {
				t.Fatalf("permImplies 成环: 从 %q 出发再次回到 %q", start, cur)
			}
			seen[cur] = true
			next, ok := permImplies[cur]
			if !ok {
				break
			}
			cur = next
		}
	}
}

// TestImpliedByMatchesNormalize ImpliedBy(前端勾选提示)必须和 NormalizePerms
// (实际落库授权)用同一套规则,否则前端提示的和实际拿到手的会不一致 ——
// 那比不提示更糟,管理员会以为自己没拿到某个权限。
func TestImpliedByMatchesNormalize(t *testing.T) {
	for _, code := range AllPermCodes() {
		granted := NormalizePerms([]string{code})
		implied := ImpliedBy(code)
		for _, c := range implied {
			if !HasPermCode(granted, c) {
				t.Fatalf("ImpliedBy(%q) 提示 %s,但 NormalizePerms 未授予: %v", code, c, granted)
			}
		}
		for _, c := range granted {
			if c == code {
				continue
			}
			found := false
			for _, ic := range implied {
				if ic == c {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("NormalizePerms(%q) 授予了 %s,但 ImpliedBy 未提示: %v", code, c, implied)
			}
		}
	}
}

func TestNormalizePermsDropsUnknownAndDedupes(t *testing.T) {
	got := NormalizePerms([]string{"table:view", "table:view", "  ", "order:boom", "notacode"})
	if len(got) != 1 || got[0] != "table:view" {
		t.Fatalf("未知权限码应被剔除、重复项应去重, got %v", got)
	}
	if bad := UnknownPerms([]string{"order:boom", "table:view", "order:boom"}); len(bad) != 1 || bad[0] != "order:boom" {
		t.Fatalf("UnknownPerms 应返回去重后的未知码, got %v", bad)
	}
	// 输出顺序必须稳定(同一集合多次调用结果一致),否则落库字符串会无意义地抖动。
	if strings.Join(NormalizePerms([]string{"order:settle", "table:edit"}), ",") !=
		strings.Join(NormalizePerms([]string{"table:edit", "order:settle"}), ",") {
		t.Fatal("NormalizePerms 输出顺序应与入参顺序无关")
	}
}

// TestBuiltinRoleMatrix 校验 4 个内置角色的权限数量与关键取舍。
func TestBuiltinRoleMatrix(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{RoleKeyAdmin, 28},
		{RoleKeyManager, 22},
		{RoleKeyCashier, 13},
		{RoleKeyStaff, 6},
	}
	for _, tc := range cases {
		got := len(ParsePerms(BuiltinRolePerms(tc.key)))
		if got != tc.want {
			t.Fatalf("内置角色 %s 应有 %d 项权限, got %d", tc.key, tc.want, got)
		}
	}
	// 退款发起只给超级管理员(本轮确认的决策)。
	for _, key := range []string{RoleKeyManager, RoleKeyCashier, RoleKeyStaff} {
		if HasPermCode(ParsePerms(BuiltinRolePerms(key)), "refund:operate") {
			t.Fatalf("角色 %s 不应拥有 refund:operate", key)
		}
	}
	// 员工/店长都不能管员工与权限。
	for _, key := range []string{RoleKeyManager, RoleKeyCashier, RoleKeyStaff} {
		perms := ParsePerms(BuiltinRolePerms(key))
		if HasPermCode(perms, "user:edit") || HasPermCode(perms, "role:edit") {
			t.Fatalf("角色 %s 不应拥有员工/角色管理权限", key)
		}
	}
	// 员工角色只读资料 + 订单流转。
	staff := ParsePerms(BuiltinRolePerms(RoleKeyStaff))
	if HasPermCode(staff, "order:settle") || HasPermCode(staff, "order:edit") || HasPermCode(staff, "order:cancel") {
		t.Fatal("员工角色不应拥有收款/改单/取消权限")
	}
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

	adminID := RoleIDByKey(RoleKeyAdmin)
	if adminID == 0 {
		t.Fatal("未找到超级管理员角色")
	}
	cashierID := RoleIDByKey(RoleKeyCashier)

	// 人为破坏:清空 admin 权限、给 cashier 改成只剩 table:view。
	if _, err := DB.Exec(`UPDATE tb_role SET perms='' WHERE role_id=?`, adminID); err != nil {
		t.Fatalf("清空 admin 权限失败: %v", err)
	}
	if _, err := DB.Exec(`UPDATE tb_role SET perms='table:view' WHERE role_id=?`, cashierID); err != nil {
		t.Fatalf("修改 cashier 权限失败: %v", err)
	}

	SyncBuiltinRoles()

	var adminPerms, cashierPerms string
	DB.QueryRow(`SELECT perms FROM tb_role WHERE role_id=?`, adminID).Scan(&adminPerms)
	DB.QueryRow(`SELECT perms FROM tb_role WHERE role_id=?`, cashierID).Scan(&cashierPerms)

	// admin 的语义就是「全量」,必须对齐权限目录总数而不是写死数字:
	// 权限目录扩容(26 → 28,新增操作日志模块)后,硬编码会在这里误报。
	if got := len(ParsePerms(adminPerms)); got != len(AllPermCodes()) {
		t.Fatalf("admin 权限应被恢复为全量 %d 项, got %d: %q", len(AllPermCodes()), got, adminPerms)
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
	if auth.RoleKey != RoleKeyAdmin || auth.Status != model.UserStatusEnabled {
		t.Fatalf("引导账号应为启用中的超级管理员, got role=%s status=%d", auth.RoleKey, auth.Status)
	}
	// 同上:对齐目录总数,避免权限目录扩容时误报。
	if len(auth.Perms) != len(AllPermCodes()) {
		t.Fatalf("管理员应拥有全部权限(%d 项), got %d", len(AllPermCodes()), len(auth.Perms))
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
	if _, err := DB.Exec(`UPDATE tb_user SET status=? WHERE user_id=?`, model.UserStatusDisabled, uid); err != nil {
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
	if auth.Status != model.UserStatusEnabled || auth.RoleKey != RoleKeyAdmin {
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
	cashierID := RoleIDByKey(RoleKeyCashier)

	t.Run("不能停用当前登录账号", func(t *testing.T) {
		err := SetUserStatus(adminID, model.UserStatusDisabled, adminID, "boss")
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
		err := UpdateUserProfile(model.User{UserID: adminID, RoleID: cashierID, RealName: "老板"},
			adminID, "boss")
		if err == nil || !strings.Contains(err.Error(), "不能修改自己的角色") {
			t.Fatalf("应拒绝改自己的角色, got %v", err)
		}
	})

	t.Run("不能停用最后一个启用的超级管理员", func(t *testing.T) {
		err := SetUserStatus(adminID, model.UserStatusDisabled, 0, "system")
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
		err := UpdateUserProfile(model.User{UserID: adminID, RoleID: cashierID, RealName: "老板"},
			0, "system")
		if err == nil || !strings.Contains(err.Error(), "至少一个启用的超级管理员") {
			t.Fatalf("应拒绝降级最后一个管理员, got %v", err)
		}
	})

	t.Run("有第二个管理员时可以降级", func(t *testing.T) {
		id, err := InsertUser(model.User{Username: "boss2", RealName: "副老板", RoleID: RoleIDByKey(RoleKeyAdmin)},
			HashPassword("boss2-123456"), "boss")
		if err != nil {
			t.Fatalf("创建第二个管理员失败: %v", err)
		}
		if err := UpdateUserProfile(model.User{UserID: adminID, RoleID: cashierID, RealName: "老板"}, 0, "system"); err != nil {
			t.Fatalf("有备用管理员后应允许降级, got %v", err)
		}
		// 还原,避免影响后续子测试
		if err := UpdateUserProfile(model.User{UserID: adminID, RoleID: RoleIDByKey(RoleKeyAdmin), RealName: "老板"}, 0, "system"); err != nil {
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

	adminRoleID := RoleIDByKey(RoleKeyAdmin)
	managerRoleID := RoleIDByKey(RoleKeyManager)

	t.Run("超级管理员角色权限不可修改", func(t *testing.T) {
		err := UpdateRole(model.Role{RoleID: adminRoleID, RoleName: "超级管理员", PermList: []string{"table:view"}}, "boss")
		if err == nil || !strings.Contains(err.Error(), "不可修改") {
			t.Fatalf("应拒绝修改 admin 角色权限, got %v", err)
		}
	})

	t.Run("内置角色不可删除", func(t *testing.T) {
		for _, id := range []int{adminRoleID, managerRoleID, RoleIDByKey(RoleKeyCashier), RoleIDByKey(RoleKeyStaff)} {
			if err := SoftDeleteRole(id, "boss"); err == nil || !strings.Contains(err.Error(), "不可删除") {
				t.Fatalf("内置角色 %d 不应被删除, got %v", id, err)
			}
		}
	})

	t.Run("角色标识不可与内置角色重名", func(t *testing.T) {
		_, err := InsertRole(model.Role{RoleKey: RoleKeyAdmin, RoleName: "冒牌管理员", PermList: []string{"table:view"}}, "boss")
		if err == nil || !strings.Contains(err.Error(), "内置角色保留") {
			t.Fatalf("应拒绝占用内置角色标识, got %v", err)
		}
	})

	t.Run("被员工引用的角色不可删除", func(t *testing.T) {
		rid, err := InsertRole(model.Role{RoleKey: "shift_lead", RoleName: "值班经理",
			PermList: []string{"order:view", "order:operate"}}, "boss")
		if err != nil {
			t.Fatalf("创建自定义角色失败: %v", err)
		}
		if _, err := InsertUser(model.User{Username: "lead1", RealName: "值班", RoleID: int(rid)},
			HashPassword("lead1-123456"), "boss"); err != nil {
			t.Fatalf("创建员工失败: %v", err)
		}
		err = SoftDeleteRole(int(rid), "boss")
		if err == nil || !strings.Contains(err.Error(), "还有 1 名员工") {
			t.Fatalf("应拒绝删除仍被引用的角色, got %v", err)
		}
	})

	t.Run("自定义角色权限写入时自动补 view", func(t *testing.T) {
		rid, err := InsertRole(model.Role{RoleKey: "kitchen_only", RoleName: "只管后厨",
			PermList: []string{"order:operate"}}, "boss")
		if err != nil {
			t.Fatalf("创建角色失败: %v", err)
		}
		r, err := GetRoleByID(int(rid))
		if err != nil {
			t.Fatalf("查询角色失败: %v", err)
		}
		if !HasPermCode(r.PermList, "order:view") {
			t.Fatalf("order:operate 应自动带上 order:view, got %v", r.PermList)
		}
	})
}

func TestUsernameUniqueness(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	_, err := InsertUser(model.User{Username: "boss", RoleID: RoleIDByKey(RoleKeyStaff)},
		HashPassword("dup-123456"), "boss")
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("重复用户名应被拒绝, got %v", err)
	}

	// 删除后同名可以重新使用(部分唯一索引只约束未删除行)。
	uid, err := InsertUser(model.User{Username: "waiter1", RoleID: RoleIDByKey(RoleKeyStaff)},
		HashPassword("waiter-123456"), "boss")
	if err != nil {
		t.Fatalf("创建员工失败: %v", err)
	}
	if err := SoftDeleteUser(int(uid), 0, "boss"); err != nil {
		t.Fatalf("删除员工失败: %v", err)
	}
	if _, err := InsertUser(model.User{Username: "waiter1", RoleID: RoleIDByKey(RoleKeyStaff)},
		HashPassword("waiter-123456"), "boss"); err != nil {
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
	if err := SetUserPassword(uid, HashPassword("new-pass-123"), "boss"); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}
	after, err := GetAuthByID(uid)
	if err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}
	if after.TokenVersion != before.TokenVersion+1 {
		t.Fatalf("改密后 token_version 应 +1, got %d -> %d", before.TokenVersion, after.TokenVersion)
	}
	if !VerifyPassword("new-pass-123", GetUserPasswordHash(uid)) {
		t.Fatal("新密码应校验通过")
	}
	if VerifyPassword("旧密码不对", GetUserPasswordHash(uid)) {
		t.Fatal("错误密码不应通过")
	}
	if SetUserPassword(99999, "x", "boss") == nil {
		t.Fatal("对不存在的账号重置密码应报错")
	}
}

func TestListUsersFilters(t *testing.T) {
	t.Setenv("ADMIN_USER", "boss")
	initUserTestDB(t)

	staffRole := RoleIDByKey(RoleKeyStaff)
	if _, err := InsertUser(model.User{Username: "waiter1", RealName: "张服务", Phone: "13800000001", RoleID: staffRole},
		HashPassword("waiter-123456"), "boss"); err != nil {
		t.Fatalf("创建员工失败: %v", err)
	}
	if _, err := InsertUser(model.User{Username: "cook1", RealName: "李后厨", RoleID: staffRole},
		HashPassword("cook-123456"), "boss"); err != nil {
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
	disabled := model.UserStatusDisabled
	_, list, err = ListUsers(UserQuery{Status: &disabled}, 1, 10)
	if err != nil || len(list) != 0 {
		t.Fatalf("按停用筛选应为空, got %v (err=%v)", list, err)
	}
	// 按启用筛选应命中全部 3 个。
	enabled := model.UserStatusEnabled
	if _, list, err = ListUsers(UserQuery{Status: &enabled}, 1, 10); err != nil || len(list) != 3 {
		t.Fatalf("按启用筛选应命中 3 个, got %v (err=%v)", list, err)
	}
	// 按角色筛选。
	if _, list, err = ListUsers(UserQuery{RoleID: staffRole}, 1, 10); err != nil || len(list) != 2 {
		t.Fatalf("按员工角色筛选应命中 2 个, got %v (err=%v)", list, err)
	}
	if err := SetUserStatus(currentOnlyUserID(t), model.UserStatusDisabled, 0, "s"); err == nil {
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
	u := &model.User{Username: "wangwu"}
	if u.DisplayName() != "wangwu" {
		t.Fatalf("model.User 姓名缺失时应回退登录名, got %s", u.DisplayName())
	}
}

// ---- 小工具 ----

// currentOnlyUserID 取当前库里唯一的账号 ID(测试里即启动引导创建的管理员)。
func currentOnlyUserID(t *testing.T) int {
	t.Helper()
	var id int
	if err := DB.QueryRow(`SELECT user_id FROM tb_user WHERE del_flag='0' ORDER BY user_id LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("查询引导账号失败: %v", err)
	}
	return id
}

func authByUsername(t *testing.T, username string) (*AuthInfo, error) {
	t.Helper()
	return GetAuthByUsername(username)
}
