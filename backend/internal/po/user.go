package po

import "strings"

// 员工账号状态。
const (
	UserStatusEnabled  = 1 // 启用
	UserStatusDisabled = 0 // 停用(停用后立即失去访问权,旧令牌同时失效)
)

// User 对应 tb_user 表:员工账号。
type User struct {
	UserID        int
	Username      string
	RealName      string // 订单操作留痕用这个值,为空时回退用户名
	RoleID        int
	Phone         string
	Status        int // 1 启用 0 停用
	LastLoginTime string
	LastLoginIP   string
	LoginCount    int
	PwdUpdateTime string
	DelFlag       string
	CreateBy      string
	CreateTime    string
	UpdateBy      string
	UpdateTime    string
	Remark        string
}

// DisplayName 留痕显示名:优先中文姓名,未填则退回登录名。
func (u *User) DisplayName() string {
	if strings.TrimSpace(u.RealName) != "" {
		return u.RealName
	}
	return u.Username
}
