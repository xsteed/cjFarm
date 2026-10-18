package po

import "strings"

// Customer 对应 tb_customer 表:顾客会员**档案**(与「怎么登录」无关)。
//
// 设计要点见 docs/design-docs/customer-member-identity/spec.md:
//   - 账号只由 customer_id 标识,登录方式在 tb_customer_identity 里(见 CustomerIdentity);
//   - 同一份档案可以绑多种登录方式(小程序 openid / 手机号 / 网站密码…),为 App 与网站预留;
//   - 不存 session_key(一期不解密手机号等敏感数据,最小化收集);
//   - 昵称/头像由用户主动填写,不强制,未填时前端显示「微信用户」。
type Customer struct {
	CustomerID int
	// 注:openid 不在本结构里。它是「微信小程序」这一种登录方式的凭据,
	// 存在 tb_customer_identity.external_id;档案本身与登录方式无关。
	Nickname      string
	AvatarURL     string
	Status        int // 1 启用 0 停用(停用后现有令牌在下次校验时立即失效)
	OrderCount    int
	LastOrderTime string
	CreateTime    string
	LastLoginTime string
	Remark        string
}

// CustomerIdentity 对应 tb_customer_identity 表:一条「登录方式 → 账号」的绑定。
//
// (provider, external_id) 全局唯一 —— 同一个登录凭据只能指向一个会员;
// 反过来一个会员可以有多条(小程序 openid + 手机号 + 网站密码…)。
// 不设 status:账号是否可用只由 Customer.Status 一处裁决,解绑 = 物理删行,
// 避免出现「两个状态谁生效」的歧义。
type CustomerIdentity struct {
	IdentityID    int
	CustomerID    int
	Provider      string // 登录方式,取值见下面 CustomerProvider* 常量
	ExternalID    string // 该方式下的用户标识(openid / 手机号 / 登录名 / apple sub)
	Credential    string // 仅 password 用(哈希);wx_* 为空
	UnionID       string // 微信开放平台统一 ID,未绑定开放平台时为空
	LastLoginTime string
	CreateTime    string
	UpdateTime    string
}

// 顾客登录方式(provider)。
//
// 枚举值**现在全部定死**,但只有 WxMP 有实现 —— 新增端时只加一个校验器,
// 接口契约、令牌、订单归属都不用动(见 spec §3.2)。
const (
	CustomerProviderWxMP    = "wx_mp"    // 微信小程序 openid(唯一已实现)
	CustomerProviderWxOA    = "wx_oa"    // 微信公众号 openid(网页授权)
	CustomerProviderWxApp   = "wx_app"   // 微信 App 端 openid
	CustomerProviderPhone   = "phone"    // 手机号 + 短信验证码(App / 网站)
	CustomerProviderPasswd  = "password" // 账号 + 密码(网站)
	CustomerProviderApple   = "apple"    // Apple 登录(App Store 上架要求)
	CustomerProviderDefault = CustomerProviderWxMP
)

// customerProviders 已登记的登录方式(不含「是否已实现」的判断,那是 service 层的事)。
var customerProviders = map[string]bool{
	CustomerProviderWxMP:   true,
	CustomerProviderWxOA:   true,
	CustomerProviderWxApp:  true,
	CustomerProviderPhone:  true,
	CustomerProviderPasswd: true,
	CustomerProviderApple:  true,
}

// IsKnownCustomerProvider 判断 provider 是否是已登记的登录方式。
// 未登记的一律拒绝 —— 免得把拼错的 provider 当成「暂不支持」而悄悄放过。
func IsKnownCustomerProvider(provider string) bool {
	return customerProviders[provider]
}

// NormalizeCustomerProvider 空值按默认登录方式(小程序)处理。
//
// 兼容性关键:老版本小程序登录请求体只有 {code},没有 provider 字段;
// 缺省成 wx_mp 让它们一行不改继续可用(spec D4)。
func NormalizeCustomerProvider(provider string) string {
	if s := strings.TrimSpace(provider); s != "" {
		return s
	}
	return CustomerProviderDefault
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
