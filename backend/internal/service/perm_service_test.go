package service

import "testing"

// TestPermName 校验权限码中文名查询:已登记返回中文名,未登记返回空串。
func TestPermName(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"order:view", "订单查看"},
		{"table:edit", "桌台管理"},
		{"log:manage", "操作日志清理"},
		{"config:edit", "系统配置修改"},
		{"unknown:code", ""},
		{"", ""},
		{"order", ""},
	}
	for _, c := range cases {
		if got := PermName(c.code); got != c.want {
			t.Fatalf("PermName(%q)=%q, want %q", c.code, got, c.want)
		}
	}
}

// TestHasPermCode 校验权限码集合的包含判定。
func TestHasPermCode(t *testing.T) {
	cases := []struct {
		name  string
		perms []string
		code  string
		want  bool
	}{
		{"包含", []string{"order:view", "order:settle"}, "order:view", true},
		{"不包含", []string{"order:view"}, "order:settle", false},
		{"空集合", nil, "order:view", false},
		{"空权限码", []string{"order:view"}, "", false},
		{"含重复码仍命中", []string{"order:view", "order:view"}, "order:view", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasPermCode(c.perms, c.code); got != c.want {
				t.Fatalf("HasPermCode(%v,%q)=%v, want %v", c.perms, c.code, got, c.want)
			}
		})
	}
}
