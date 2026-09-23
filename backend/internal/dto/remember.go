package dto

// RememberSession 「记住我」会话信息(登录设备管理页展示用)。由调用方用字面量构造,无 FromPO/ToPO。
type RememberSession struct {
	TokenID      int    `json:"tokenId"`
	TokenPrefix  string `json:"tokenPrefix"` // 令牌前 8 位,仅供前端识别「当前设备」;完整令牌不回传
	UA           string `json:"ua"`          // 签发环境指纹,如 "Chrome·macOS"
	CreateTime   string `json:"createTime"`
	LastUsedTime string `json:"lastUsedTime"`
	ExpireTime   string `json:"expireTime"`
}
