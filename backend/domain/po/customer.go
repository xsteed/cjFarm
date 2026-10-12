package po

import "strings"

// Customer 对应 tb_customer 表:顾客会员档案(小程序「注册成为会员」)。
//
// 设计要点见 docs/design-docs/customer-member/spec.md:
//   - openid 是微信小程序侧的用户唯一标识,做唯一键 —— 「首次出现即注册,再次出现即登录」;
//   - 不存 session_key(一期不解密手机号等敏感数据,最小化收集);
//   - 昵称/头像由用户主动填写,不强制,未填时前端显示「微信用户」。
type Customer struct {
	CustomerID    int
	OpenID        string
	Nickname      string
	AvatarURL     string
	Status        int // 1 启用 0 停用(停用后现有令牌在下次校验时立即失效)
	OrderCount    int
	LastOrderTime string
	CreateTime    string
	LastLoginTime string
	Remark        string
}

// 顾客会员状态(与员工账号共用 1/0 语义,但独立常量,避免语义耦合)。
const (
	CustomerStatusEnabled  = 1
	CustomerStatusDisabled = 0
)

// DisplayName 会员卡展示名:未填昵称时显示「微信用户」。
func (c *Customer) DisplayName() string {
	if s := strings.TrimSpace(c.Nickname); s != "" {
		return s
	}
	return "微信用户"
}
