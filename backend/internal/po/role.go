package po

// Role 对应 tb_role 表:角色(权限集合)。
type Role struct {
	RoleID     int
	RoleKey    string // 代码内标识,内置角色不可改
	RoleName   string // 展示名
	Perms      string // 权限码 CSV(落库形态)
	// DataScope 是表结构里预留的数据行级权限位(all=全部 / self=仅本人),
	// **当前系统为单租户,该字段不参与任何查询过滤或权限判定**,仅在角色读写时原样往返。
	// 详见 docs/user-permission-design.md 1.3「非目标(预留)」。
	DataScope string
	IsBuiltin  int // 1=内置(不可删除;admin 权限每次启动强制恢复全量)
	SortOrder  int
	Status     int
	DelFlag    string
	CreateTime string
	UpdateTime string
	Remark     string
}
