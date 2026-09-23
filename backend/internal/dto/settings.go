package dto

// Settings 门店配置快照(tb_setting 为 KV 表,无对应 PO)。全部字段为 string,json tag 为 snake_case。
type Settings struct {
	ShopName           string `json:"shop_name"`
	ShopLogo           string `json:"shop_logo"`
	SeatFeeEnabled     string `json:"seat_fee_enabled"`
	SeatFee            string `json:"seat_fee"`
	PayQrWx            string `json:"pay_qr_wx"`
	PayQrAli           string `json:"pay_qr_ali"`
	H5BaseURL          string `json:"h5_base_url"`
	PromotionEnabled   string `json:"promotion_enabled"`
	PromotionThreshold string `json:"promotion_threshold"`
	PromotionDiscount  string `json:"promotion_discount"`
	// 在线支付开关(未申请 key 前保持 "0",码牌收款不受影响)
	WxpayEnabled  string `json:"wxpay_enabled"`
	AlipayEnabled string `json:"alipay_enabled"`
	// 微信支付商户参数
	WxpayMchid            string `json:"wxpay_mchid"`
	WxpayAppid            string `json:"wxpay_appid"`
	WxpayApiv3Key         string `json:"wxpay_apiv3_key"`
	WxpaySerialNo         string `json:"wxpay_serial_no"`
	WxpayPrivateKeyPath   string `json:"wxpay_private_key_path"`
	WxpayPlatformCertPath string `json:"wxpay_platform_cert_path"`
	// 微信支付公钥模式(官方推荐,无物理过期时间):配置公钥ID后按该模式验签
	WxpayPubkeyID   string `json:"wxpay_pubkey_id"`
	WxpayPubkeyPath string `json:"wxpay_pubkey_path"`
	WxpayNotifyURL  string `json:"wxpay_notify_url"`
	// 支付宝商户参数
	AlipayAppid          string `json:"alipay_appid"`
	AlipayPrivateKeyPath string `json:"alipay_private_key_path"`
	AlipayPublicKey      string `json:"alipay_public_key"`
	AlipayNotifyURL      string `json:"alipay_notify_url"`
	// 小票打印
	PrintEnabled          string `json:"print_enabled"`            // 打印总开关
	PrintKitchenShowPrice string `json:"print_kitchen_show_price"` // 厨房单是否带单价金额
	// 食客小票模板配置(方案A:高频变化点做成设置项,后端渲染实时读取)
	PrintGuestFooter       string `json:"print_guest_footer"`        // 页脚文案,清空则不打印页脚
	PrintGuestShowSeatFee  string `json:"print_guest_show_seat_fee"` // 是否显示餐位费行("1"=显示)
	PrintGuestShowDiscount string `json:"print_guest_show_discount"` // 是否显示优惠行("1"=显示)
	// 飞鹅云打印(feie_ukey 为敏感项,回显时留空表示不修改)
	FeieUser   string `json:"feie_user"`
	FeieUkey   string `json:"feie_ukey"`
	FeieApiURL string `json:"feie_api_url"`
	// 本地打印代理令牌(敏感项),落库加密、接口不回显。
	AgentToken string `json:"agent_token"`
	// AgentTokenClear 为 "1" 时主动清空 agent_token(停用 legacy 全局令牌通道)。
	//
	// 敏感项留空表示「不修改」,光靠 agent_token="" 无法表达「清掉它」;
	// 这个一次性开关只用于提交意图,不落库、不参与指纹。
	AgentTokenClear string `json:"agent_token_clear,omitempty"`
	// 代理最新版本号(普通配置项):管理端配置后随 ping 下发给代理,代理据此提示门店升级。
	AgentLatestVersion string `json:"agent_latest_version"`
	// 配置内容指纹(乐观锁):SettingList 下发,SettingSave 原样带回校验。
	Version string `json:"version,omitempty"`
}
