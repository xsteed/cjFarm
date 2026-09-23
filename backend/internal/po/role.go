package po

// Role 对应 tb_role 表:角色(权限集合)。
type Role struct {
	RoleID     int
	RoleKey    string // 代码内标识,内置角色不可改
	RoleName   string // 展示名
	Perms      string // 权限码 CSV(落库形态)
	DataScope  string
	IsBuiltin  int // 1=内置(不可删除;admin 权限每次启动强制恢复全量)
	SortOrder  int
	Status     int
	DelFlag    string
	CreateTime string
	UpdateTime string
	Remark     string
}
