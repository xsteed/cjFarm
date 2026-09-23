package dto

import "dining-system/internal/po"

// Role 对应 tb_role 表:角色(权限集合,API 出参)。PermList/UserCount 为运行时补算,不入库。
type Role struct {
	RoleID    int    `json:"roleId"`
	RoleKey   string `json:"roleKey"`  // 代码内标识,内置角色不可改
	RoleName  string `json:"roleName"` // 展示名
	Perms     string `json:"perms"`    // 权限码 CSV(落库形态)
	DataScope string `json:"dataScope"`
	IsBuiltin int    `json:"isBuiltin"` // 1=内置(不可删除;admin 权限每次启动强制恢复全量)
	SortOrder int    `json:"sortOrder"`
	Status    int    `json:"status"`
	DelFlag   string `json:"delFlag"`

	// PermList 是 Perms 解析后的权限码列表,仅用于接口出入参,不入库。
	PermList   []string `json:"permList"`
	CreateTime string   `json:"createTime"`
	UpdateTime string   `json:"updateTime"`
	Remark     string   `json:"remark"`
	// UserCount 引用该角色的员工数(列表接口补算,删除前的前置检查也要用)。
	UserCount int `json:"userCount"`
}

// FromRole 将持久化对象与运行时补算结果组装为 API 出参。
func FromRole(p po.Role, permList []string, userCount int) Role {
	return Role{
		RoleID:     p.RoleID,
		RoleKey:    p.RoleKey,
		RoleName:   p.RoleName,
		Perms:      p.Perms,
		DataScope:  p.DataScope,
		IsBuiltin:  p.IsBuiltin,
		SortOrder:  p.SortOrder,
		Status:     p.Status,
		DelFlag:    p.DelFlag,
		PermList:   permList,
		CreateTime: p.CreateTime,
		UpdateTime: p.UpdateTime,
		Remark:     p.Remark,
		UserCount:  userCount,
	}
}

// ToPO 将 API 出参转回持久化对象,PermList/UserCount 不入库。
func (r Role) ToPO() po.Role {
	return po.Role{
		RoleID:     r.RoleID,
		RoleKey:    r.RoleKey,
		RoleName:   r.RoleName,
		Perms:      r.Perms,
		DataScope:  r.DataScope,
		IsBuiltin:  r.IsBuiltin,
		SortOrder:  r.SortOrder,
		Status:     r.Status,
		DelFlag:    r.DelFlag,
		CreateTime: r.CreateTime,
		UpdateTime: r.UpdateTime,
		Remark:     r.Remark,
	}
}
