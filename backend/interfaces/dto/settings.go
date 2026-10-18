package dto

// Settings 门店配置快照(tb_setting 为 KV 表,无对应 PO)。全部字段为 string,json tag 为 snake_case。
type Settings struct {
	ShopName           string `json:"shop_name"`
	ShopSlogan         string `json:"shop_slogan"` // 门店标语:仅顾客端 H5/小程序顶部与品牌名拼接展示,桌牌二维码不放
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
	// 小程序内支付(JSAPI)开关。开启后小程序结账页出现「微信支付」按钮;
	// 依赖项:小程序 AppID(填在 wxpay_appid)+ AppSecret(wx_app_secret)+
	// 商户 API 私钥与证书 —— 缺任一都调不起收银台,所以默认关(fail-closed)。
	WxpayJsapiEnabled string `json:"wxpay_jsapi_enabled"`
	// 储值自助充值开关(默认 0):只管「顾客能否自助充值」,不影响已有余额的消费与退款。
	WalletRechargeEnabled string `json:"wallet_recharge_enabled"`
	// ---- 储值合规参数(出处见 docs/compliance/prepaid-card.md) ----
	// 餐饮储值属于「单用途商业预付卡」,以下几项是监管口径而非产品偏好:
	// 单张不记名卡余额上限 1000 元、记名卡 5000 元。改大之前请先读那份文档。
	WalletRechargeMin      string `json:"wallet_recharge_min"`      // 单笔充值下限(元)
	WalletRechargeMax      string `json:"wallet_recharge_max"`      // 单笔充值上限(元)
	WalletBalanceMax       string `json:"wallet_balance_max"`       // 未记名余额上限(元)
	WalletBalanceMaxNamed  string `json:"wallet_balance_max_named"` // 已记名余额上限(元)
	WalletRealnameRequired string `json:"wallet_realname_required"` // 充值是否强制实名("1")
	WalletAdjustMax        string `json:"wallet_adjust_max"`        // 后台单次调账上限(元)
	// 《储值规则》版本与正文。改了正文就必须改版本号:版本号变化会强制顾客重新确认,
	// 这是「他充值时看到的是哪一版」的唯一留痕依据。
	WalletRulesVersion string `json:"wallet_rules_version"`
	WalletRulesText    string `json:"wallet_rules_text"`
	// 微信支付商户参数(wxpay_apiv3_key 为敏感项:已配置回显 ****** 占位,原样带回或留空均表示不修改)
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
	// 小程序桌牌码(AppID 复用 wxpay_appid)
	WxAppSecret      string `json:"wx_app_secret"`       // 敏感项:已配置回显 ****** 占位,原样带回或留空均表示不修改
	WxCodeEnvVersion string `json:"wx_code_env_version"` // release / trial / develop
	// 支付宝商户参数
	AlipayAppid          string `json:"alipay_appid"`
	AlipayPrivateKeyPath string `json:"alipay_private_key_path"`
	AlipayPublicKey      string `json:"alipay_public_key"`
	AlipayNotifyURL      string `json:"alipay_notify_url"`
	// 小票打印
	PrintEnabled          string `json:"print_enabled"`            // 打印总开关
	PrintKitchenShowPrice string `json:"print_kitchen_show_price"` // 厨房单是否带单价金额
	// 厨房单打印粒度:whole=整单一张(默认)/ item=一菜一单 / portion=一菜一单且按份数拆
	PrintKitchenSplitMode string `json:"print_kitchen_split_mode"`
	// 顾客小票模板配置(方案A:高频变化点做成设置项,后端渲染实时读取)
	// 金额区按「有才打」渲染(茶位费/优惠/赠送),没有对应的展示开关。
	PrintGuestFooter       string `json:"print_guest_footer"`        // 页脚文案,清空则不打印页脚
	PrintGuestShowDiscount string `json:"print_guest_show_discount"` // 是否显示优惠行("1"=显示)
	PrintGuestOnAppend     string `json:"print_guest_on_append"`     // 加菜后是否出加菜小票("1"=打印)
	PrintGuestOnOrder      string `json:"print_guest_on_order"`      // 下单后是否出点菜单("1"=打印)
	// 飞鹅云打印(feie_ukey 为敏感项:已配置回显 ****** 占位,原样带回或留空均表示不修改)
	FeieUser   string `json:"feie_user"`
	FeieUkey   string `json:"feie_ukey"`
	FeieApiURL string `json:"feie_api_url"`
	// 本地打印代理令牌(敏感项),落库加密;已配置回显 ****** 占位,不回显明文。
	AgentToken string `json:"agent_token"`
	// AgentTokenClear 为 "1" 时主动清空 agent_token(停用 legacy 全局令牌通道)。
	//
	// 敏感项留空表示「不修改」,光靠 agent_token="" 无法表达「清掉它」;
	// 这个一次性开关只用于提交意图,不落库、不参与指纹。
	AgentTokenClear string `json:"agent_token_clear,omitempty"`
	// 代理最新版本号(普通配置项):管理端配置后随 ping 下发给代理,代理据此提示门店升级。
	AgentLatestVersion string `json:"agent_latest_version"`
	// 桌台遗留订单防护(两项默认关,详见 store.settingDefaults 的说明)
	StaleReleaseEnabled string `json:"stale_release_enabled"` // 跨日自动清台开关
	StaleAlertEnabled   string `json:"stale_alert_enabled"`   // 未结账超时告警开关
	StaleAlertHours     string `json:"stale_alert_hours"`     // 告警阈值(小时,1~72)
	// 营业日切换时刻(0~12 点,0=自然日 00:00)。同时决定跨日清台的判定边界与当日取餐号的重置时刻。
	BizDateCutoffHour string `json:"biz_date_cutoff_hour"`
	// 先付模式开关("1"=先付,其余=后付)。开启后顾客下单需先完成支付,后厨在收款成功后才出单制作;
	// 关闭(出厂默认)为后付模式:下单即出单做菜,餐后/离店前再结账。
	PayFirstEnabled string `json:"pay_first_enabled"`
	// 会员功能总闸("1"=开放,"0"=关闭)。控制小程序「我的」入口显隐与会员登录/注册放行;
	// 与 AppSecret 等登录能力配置解耦(没配密钥时入口照常出现,注册会得到明确错误文案)。
	MemberEnabled string `json:"member_enabled"`
	// 配置内容指纹(乐观锁):SettingList 下发,SettingSave 原样带回校验。
	Version string `json:"version,omitempty"`
}
