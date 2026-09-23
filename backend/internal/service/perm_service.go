package service

import "dining-system/internal/store"

// ============ 权限 ============
//
// 权限点目录展示与角色归一化校验已收口在 auth_service.go(PermGroups / SaveRole / UpdateRole 等);
// 这里只补鉴权中间件(handler/perm.go)所需的两个权限原语,避免 handler 直接依赖 store。

// PermName 返回权限码的中文名,未登记返回空串。
func PermName(code string) string {
	return store.PermName(code)
}

// HasPermCode 判断权限码集合是否包含指定权限。
func HasPermCode(perms []string, code string) bool {
	return store.HasPermCode(perms, code)
}
