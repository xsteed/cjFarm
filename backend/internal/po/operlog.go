package po

// 操作类型(落 business_type),用于在列表里快速筛出某一类动作。
const (
	OperTypeInsert = "insert" // 新增
	OperTypeUpdate = "update" // 修改
	OperTypeDelete = "delete" // 删除
	OperTypeLogin  = "login"  // 登录(含失败尝试)
	OperTypeGrant  = "grant"  // 授权类:改角色 / 重置密码 / 启停用
	OperTypePrint  = "print"  // 会触发出纸的动作(测试页 / 补打)
	OperTypeOther  = "other"  // 其它(查询类写接口、越权尝试等)
)

// 操作结果(落 status)。
const (
	OperStatusFail    = 0 // 失败(业务报错 / 被拒绝)
	OperStatusSuccess = 1 // 成功
)

// OperLog 对应 tb_oper_log 表:一条操作日志。
type OperLog struct {
	LogID        int
	Module       string // 模块中文名:订单管理 / 员工管理 / 登录
	BusinessType string // 见 OperType* 常量
	Action       string // 动作中文名:修改菜品 / 重置密码
	Method       string // 路由模板 "POST /api/admin/dish/update"
	RequestURL   string // 含 query 的真实 URL
	OperatorID   int    // 0 表示未登录 / 无法识别(如密码错误)
	Operator     string // 显示名快照:改名后历史日志不变
	OperatorRole string // 当时角色名快照:改角色后历史日志不变
	OperIP       string
	TargetType   string // 业务对象类型:order / user / dish / table ...
	TargetID     string // 业务对象标识(订单号 / 主键);VARCHAR 以兼容单号
	OperParam    string // 脱敏后的请求参数 JSON
	Detail       string // 人话摘要:高风险动作由业务代码显式填写
	Status       int    // 见 OperStatus* 常量
	ErrorMsg     string // 失败原因(取自响应 msg)
	CostMs       int
	CreateTime   string
}
