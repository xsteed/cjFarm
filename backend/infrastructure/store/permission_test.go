package store

import (
	"strings"
	"testing"
)

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
// 起因:挂账管理页的数据实际来自 GET /api/admin/order/list(要 order:view),
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
