package dto

import "dining-system/internal/po"

// User 对应 tb_user 表:员工账号(API 出参)。RoleKey/RoleName/Perms 为联表字段,不入库。
type User struct {
	UserID        int    `json:"userId"`
	Username      string `json:"username"`
	RealName      string `json:"realName"` // 订单操作留痕用这个值,为空时回退用户名
	RoleID        int    `json:"roleId"`
	Phone         string `json:"phone"`
	Status        int    `json:"status"` // 1 启用 0 停用
	LastLoginTime string `json:"lastLoginTime"`
	LastLoginIP   string `json:"lastLoginIp"`
	LoginCount    int    `json:"loginCount"`
	PwdUpdateTime string `json:"pwdUpdateTime"`
	DelFlag       string `json:"delFlag"`
	CreateBy      string `json:"createBy"`
	CreateTime    string `json:"createTime"`
	UpdateBy      string `json:"updateBy"`
	UpdateTime    string `json:"updateTime"`
	Remark        string `json:"remark"`

	// 以下为关联字段(来自 tb_role),不入 tb_user 表。
	RoleKey  string   `json:"roleKey"`
	RoleName string   `json:"roleName"`
	Perms    []string `json:"perms,omitempty"`
}

// FromUser 将持久化对象与联表补算结果组装为 API 出参。
func FromUser(p po.User, roleKey, roleName string, perms []string) User {
	return User{
		UserID:        p.UserID,
		Username:      p.Username,
		RealName:      p.RealName,
		RoleID:        p.RoleID,
		Phone:         p.Phone,
		Status:        p.Status,
		LastLoginTime: p.LastLoginTime,
		LastLoginIP:   p.LastLoginIP,
		LoginCount:    p.LoginCount,
		PwdUpdateTime: p.PwdUpdateTime,
		DelFlag:       p.DelFlag,
		CreateBy:      p.CreateBy,
		CreateTime:    p.CreateTime,
		UpdateBy:      p.UpdateBy,
		UpdateTime:    p.UpdateTime,
		Remark:        p.Remark,
		RoleKey:       roleKey,
		RoleName:      roleName,
		Perms:         perms,
	}
}

// ToPO 将 API 出参转回持久化对象,RoleKey/RoleName/Perms 不入库。
func (u User) ToPO() po.User {
	return po.User{
		UserID:        u.UserID,
		Username:      u.Username,
		RealName:      u.RealName,
		RoleID:        u.RoleID,
		Phone:         u.Phone,
		Status:        u.Status,
		LastLoginTime: u.LastLoginTime,
		LastLoginIP:   u.LastLoginIP,
		LoginCount:    u.LoginCount,
		PwdUpdateTime: u.PwdUpdateTime,
		DelFlag:       u.DelFlag,
		CreateBy:      u.CreateBy,
		CreateTime:    u.CreateTime,
		UpdateBy:      u.UpdateBy,
		UpdateTime:    u.UpdateTime,
		Remark:        u.Remark,
	}
}
