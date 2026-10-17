package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// ============ 配置读取 ============

// GetSetting 读取单个配置项(敏感项透明解密)。
func GetSetting(key string) string { return dao.GetSetting(key) }

// SetSetting 写入单个配置项(敏感项自动加密)。
func SetSetting(key, value string) error { return dao.SetSetting(key, value) }

// ============ 店铺配置 ============
//
// 回显策略(白名单):
//   - 普通配置项全部回显;
//   - 敏感项(infrastructure.IsSensitiveKey,如 APIv3 密钥)一律不回显,前端留空即表示「不修改」;
//   - 密钥材料(商户私钥 / 平台证书 / 微信支付公钥)一律只保存并回显「文件路径」,
//     内容本身从不入库,故回显路径是安全的。

// ManagedSettingKeys 配置页管理的全部键(普通项 + 敏感项),是乐观锁指纹的覆盖范围,
// 必须与 ListSettings 的回显字段、SaveSettings 的 vals+secrets 提交面保持一致
// (TestManagedSettingKeysComplete 会守护这层一致)。指纹刻意不覆盖
// agent_last_seen(代理心跳,秒级变化)、admin_pass_hash 等内部键——
// 它们的变化不该把管理员的保存误判为并发冲突。
var ManagedSettingKeys = []string{
	"shop_name", "shop_slogan", "shop_logo", "h5_base_url",
	"seat_fee_enabled", "seat_fee",
	"promotion_enabled", "promotion_threshold", "promotion_discount",
	"pay_qr_wx", "pay_qr_ali",
	"wxpay_enabled", "wxpay_mchid", "wxpay_appid", "wxpay_serial_no",
	"wxpay_private_key_path", "wxpay_platform_cert_path",
	"wxpay_pubkey_id", "wxpay_pubkey_path", "wxpay_notify_url",
	// 小程序桌牌码:AppID 复用 wxpay_appid,此处只需版本与 AppSecret
	"wx_code_env_version",
	"alipay_enabled", "alipay_appid", "alipay_private_key_path",
	"alipay_public_key", "alipay_notify_url",
	"print_enabled", "print_kitchen_show_price", "print_kitchen_split_mode",
	"print_guest_footer", "print_guest_show_discount",
	"print_guest_on_append", "print_guest_on_order",
	"feie_user", "feie_api_url", "agent_latest_version",
	// 桌台遗留订单防护与营业日定义(见 stale_service.go)
	"stale_release_enabled", "stale_alert_enabled", "stale_alert_hours", "biz_date_cutoff_hour",
	// 先付/后付模式(见 PayFirstEnabled)
	"pay_first_enabled",
	// 敏感项(不回显,但同样参与指纹:它们变了,别的管理端就该刷新)
	"wxpay_apiv3_key", "wx_app_secret", "feie_ukey", "agent_token",
}

// settingSaveMu 串行化配置保存:版本校验 → 逐项落库不留并发窗口。
// 本项目为单体部署,进程级互斥已足够;数据库层面的行级原子性由 SetSetting 保证。
var settingSaveMu sync.Mutex

// SettingFlag 读取「开关型」配置项,统一归一化为 "1" / "0"。
//
// 前端 t-switch 使用 :custom-value="['1','0']",一旦收到空串或意外取值会直接抛
// `value is not in ["1","0"]`;该异常发生在组件更新阶段,会中断整棵路由树的 patch,
// 表现为「进入系统配置页后,再点其他菜单全部白屏」。因此这里做下发兜底:
// 只有明确等于 "1" 才算开启,其余(含配置项缺失)一律返回 "0"。
func SettingFlag(key string) string {
	if dao.GetSetting(key) == "1" {
		return "1"
	}
	return "0"
}

// normFlagValue 写入侧的同一套归一化:只有明确的 "1" 才落库为 "1",其余一律 "0"。
// 保证「库里的开关值永远合法」,前端即使再次拿到脏值也不会被 t-switch 判为非法。
func normFlagValue(v string) string {
	if v == "1" {
		return "1"
	}
	return "0"
}

// NormalizeAmountValue 归一化金额类配置项(茶位费 / 满减门槛 / 满减额,单位:元)。
//
// 为什么必须校验:这三项直接进入 total = 菜品金额 + 茶位费 - 优惠(见 RecalcAmount),
// 且对新单与每一次加菜重算立即生效。此前原样落库,管理员误填负数(或把单位写成角)
// 会让应收被反向抬高/压低,影响全部订单 —— 属于静默的账目差错。
// 非法(非数字)或负数一律回落 "0"(即不生效),并留告警便于追查。
func NormalizeAmountValue(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "" // 留空表示未配置(与其余普通项一致),消费侧按 0 处理
	}
	f, err := strconv.ParseFloat(s, 64)
	// NaN/Inf:ParseFloat 对 "NaN"/"Inf" 不报错,若放行会让 SeatFee 变成 NaN 传染整个合计。
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		logger.Warnf("[setting] 金额类配置 %q 非法(应为非负数字,单位元),已回落 0.00", v)
		return "0.00"
	}
	// 合法值原样保留:统一格式化会让「6」变「6.00」,在审计摘要里产生并无实事的假变更。
	return s
}

// SettingFlagDefaultOn 「默认开启」型开关的读取归一化:只有明确 "0" 才算关闭,
// 其余(含 "1"、空值、配置项缺失)一律返回 "1"。
//
// 与 SettingFlag 的分工:打印总开关等「默认关」用 SettingFlag;顾客小票段落开关
// (优惠行等)出厂即显示,渲染侧(print.guestShowDiscount)按「!= "0」判定,
// 回显侧必须用同一套语义,否则键缺失时配置页显示「关」而实际仍在打印。
func SettingFlagDefaultOn(key string) string {
	if dao.GetSetting(key) == "0" {
		return "0"
	}
	return "1"
}

// SettingEnum 读取「枚举型」配置项:只有白名单内的取值放行,其余(含配置项缺失、
// 历史脏值)一律回落默认值。
//
// 与开关型的区别:开关只有 1/0 两态,枚举有三个以上取值,前端用 t-radio-group
// 绑定;一旦下发非法值,界面上会「一个都没选中」,商家以为配置丢了。
func SettingEnum(key string, allowed []string, def string) string {
	v := strings.TrimSpace(dao.GetSetting(key))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

// PayFirstEnabled 先付模式开关:开启后顾客下单需先完成支付,后厨在收款成功后才出单制作。
//
// 采用「默认关」语义(SettingFlag):只有明确 "1" 才算先付,其余(含配置缺失、脏值)
// 一律按后付处理。误判成先付的后果更严重 —— 没付钱的单会永远不出单、菜永远不做;
// 多出几张晚付的单只是账期问题,两者风险不对称,故默认必须偏向后付。
func PayFirstEnabled() bool { return dao.GetSetting("pay_first_enabled") == "1" }

// normEnumValue 写入侧的同一套归一化:非法值落库为默认值,保证库里永远是白名单取值。
func normEnumValue(v string, allowed []string, def string) string {
	v = strings.TrimSpace(v)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

// SecretMask 已配置敏感项的回显占位符。
//
// 敏感项(UKEY/AppSecret/代理令牌等)不回显明文,但「恒为空串」会让管理员分不清
// 「没配置」与「配置了但不回显」,误以为保存无效。已配置时回显该占位符;
// 保存时收到占位符按「未修改」处理(用户没有改动它,绝不能把占位符当真值落库)。
const SecretMask = "******"

// maskIfSet 敏感项回显:已配置返回占位符,未配置返回空串。
func maskIfSet(key string) string {
	if dao.GetSetting(key) != "" {
		return SecretMask
	}
	return ""
}

// ListSettings 读取并组装配置页回显:普通项直接回显,开关项归一化,
// 敏感项已配置回显占位符(留空=未配置),指纹作为乐观锁 version 一并下发。
func ListSettings() dto.Settings {
	return dto.Settings{
		ShopName:           dao.GetSetting("shop_name"),
		ShopSlogan:         dao.GetSetting("shop_slogan"),
		ShopLogo:           dao.GetSetting("shop_logo"),
		SeatFeeEnabled:     SettingFlag("seat_fee_enabled"),
		SeatFee:            dao.GetSetting("seat_fee"),
		PayQrWx:            dao.GetSetting("pay_qr_wx"),
		PayQrAli:           dao.GetSetting("pay_qr_ali"),
		H5BaseURL:          dao.GetSetting("h5_base_url"),
		PromotionEnabled:   SettingFlag("promotion_enabled"),
		PromotionThreshold: dao.GetSetting("promotion_threshold"),
		PromotionDiscount:  dao.GetSetting("promotion_discount"),
		WxpayEnabled:       SettingFlag("wxpay_enabled"),
		AlipayEnabled:      SettingFlag("alipay_enabled"),
		// 微信支付(敏感项:已配置回显占位符,前端原样带回表示不修改)
		WxpayApiv3Key:         maskIfSet("wxpay_apiv3_key"),
		WxpayMchid:            dao.GetSetting("wxpay_mchid"),
		WxpayAppid:            dao.GetSetting("wxpay_appid"),
		WxpaySerialNo:         dao.GetSetting("wxpay_serial_no"),
		WxpayPrivateKeyPath:   dao.GetSetting("wxpay_private_key_path"),
		WxpayPlatformCertPath: dao.GetSetting("wxpay_platform_cert_path"),
		WxpayPubkeyID:         dao.GetSetting("wxpay_pubkey_id"),
		WxpayPubkeyPath:       dao.GetSetting("wxpay_pubkey_path"),
		WxpayNotifyURL:        dao.GetSetting("wxpay_notify_url"),
		// 小程序桌牌码(wx_app_secret 为敏感项,与 APIv3 密钥同款占位回显)
		WxAppSecret:      maskIfSet("wx_app_secret"),
		WxCodeEnvVersion: dao.GetSetting("wx_code_env_version"),
		// 支付宝
		AlipayAppid:          dao.GetSetting("alipay_appid"),
		AlipayPrivateKeyPath: dao.GetSetting("alipay_private_key_path"),
		AlipayPublicKey:      dao.GetSetting("alipay_public_key"),
		AlipayNotifyURL:      dao.GetSetting("alipay_notify_url"),
		// 小票打印
		PrintEnabled:          SettingFlag("print_enabled"),
		PrintKitchenShowPrice: SettingFlag("print_kitchen_show_price"),
		// 厨房单打印粒度(枚举项,回落值为整单;与渲染侧 print.kitchenSplitMode 同一套白名单)
		PrintKitchenSplitMode: SettingEnum("print_kitchen_split_mode", po.KitchenSplitModes, po.KitchenSplitWhole),
		// 顾客小票模板配置(页脚可清空,段落开关按「默认开」语义归一化,与渲染侧一致)
		PrintGuestFooter:       dao.GetSetting("print_guest_footer"),
		PrintGuestShowDiscount: SettingFlagDefaultOn("print_guest_show_discount"),
		// 加菜重出小票默认开,与渲染侧 guestPrintOnAppend 的「!= "0"」语义一致
		PrintGuestOnAppend: SettingFlagDefaultOn("print_guest_on_append"),
		// 下单点菜单默认开,与渲染侧 guestPrintOnOrder 的「!= "0"」语义一致
		PrintGuestOnOrder: SettingFlagDefaultOn("print_guest_on_order"),
		// 飞鹅云打印(feie_ukey 为敏感项,已配置回显占位符)
		FeieUser:   dao.GetSetting("feie_user"),
		FeieUkey:   maskIfSet("feie_ukey"),
		FeieApiURL: dao.GetSetting("feie_api_url"),
		// 本地打印代理(agent_token 为敏感项,同款占位回显;普通项直接回显)
		AgentToken:         maskIfSet("agent_token"),
		AgentLatestVersion: dao.GetSetting("agent_latest_version"),
		// 桌台遗留订单防护:开关按 SettingFlag 归一化,阈值回落合法区间
		StaleReleaseEnabled: SettingFlag(SettingStaleReleaseEnabled),
		StaleAlertEnabled:   SettingFlag(SettingStaleAlertEnabled),
		StaleAlertHours:     strconv.Itoa(StaleAlertHours()),
		BizDateCutoffHour:   strconv.Itoa(BizDayCutoffHour()),
		// 先付模式开关(默认关,只有明确 "1" 才先付)
		PayFirstEnabled: SettingFlag("pay_first_enabled"),
		// 乐观锁指纹:保存时原样带回,后端比对发现不一致即拒绝(409)
		Version: dao.SettingsFingerprint(ManagedSettingKeys),
	}
}

// SaveSettings 保存整份配置,返回审计摘要、是否发生乐观锁冲突与落库错误。
//
// 串行化整个保存过程:指纹校验、快照、落库、摘要之间不留并发窗口。
// 校验必须在锁内——若在锁外,两个请求可同时通过校验,后获得锁的
// 仍会用旧快照整表覆盖先保存者的修改,互斥形同虚设。
//
// 乐观锁:version 来自 ListSettings 下发的指纹,与当前库内容不一致说明
// 有其他人已先保存——继续提交会用旧快照整表覆盖掉对方的修改。
// 旧前端不传 version,跳过校验保持兼容。冲突时不落库、不生成摘要。
//
// 任一项落库失败必须返回 error:静默 warning 会让管理员看到「保存成功」,
// 而支付密钥/打印开关实际并未写入(DB 抖动、磁盘满、列超长),审计摘要
// 还是基于提交值生成的,日志与库不一致 —— 这类「假成功」比报错更难排查。
// ctx 用于把 requestID 带进日志:保存配置涉及多条告警(清空默认值、敏感项跳过、
// 令牌清空),排查「配置怎么被改坏的」必须能定位到具体那次提交。
func SaveSettings(ctx context.Context, settings dto.Settings, actor string) (detail string, conflict bool, err error) {
	settingSaveMu.Lock()
	defer settingSaveMu.Unlock()

	if settings.Version != "" && settings.Version != dao.SettingsFingerprint(ManagedSettingKeys) {
		return "", true, nil
	}

	vals := map[string]string{
		"shop_name":                settings.ShopName,
		"shop_slogan":              settings.ShopSlogan,
		"shop_logo":                settings.ShopLogo,
		"seat_fee_enabled":         normFlagValue(settings.SeatFeeEnabled),
		"seat_fee":                 NormalizeAmountValue(settings.SeatFee),
		"pay_qr_wx":                settings.PayQrWx,
		"pay_qr_ali":               settings.PayQrAli,
		"h5_base_url":              settings.H5BaseURL,
		"promotion_enabled":        normFlagValue(settings.PromotionEnabled),
		"promotion_threshold":      NormalizeAmountValue(settings.PromotionThreshold),
		"promotion_discount":       NormalizeAmountValue(settings.PromotionDiscount),
		"wxpay_enabled":            normFlagValue(settings.WxpayEnabled),
		"alipay_enabled":           normFlagValue(settings.AlipayEnabled),
		"wxpay_mchid":              settings.WxpayMchid,
		"wxpay_appid":              settings.WxpayAppid,
		"wxpay_serial_no":          settings.WxpaySerialNo,
		"wxpay_private_key_path":   settings.WxpayPrivateKeyPath,
		"wxpay_platform_cert_path": settings.WxpayPlatformCertPath,
		"wxpay_pubkey_id":          settings.WxpayPubkeyID,
		"wxpay_pubkey_path":        settings.WxpayPubkeyPath,
		"wxpay_notify_url":         settings.WxpayNotifyURL,
		"wx_code_env_version":      settings.WxCodeEnvVersion,
		"alipay_appid":             settings.AlipayAppid,
		"alipay_private_key_path":  settings.AlipayPrivateKeyPath,
		"alipay_public_key":        settings.AlipayPublicKey,
		"alipay_notify_url":        settings.AlipayNotifyURL,
		"print_enabled":            normFlagValue(settings.PrintEnabled),
		"print_kitchen_show_price": normFlagValue(settings.PrintKitchenShowPrice),
		"print_kitchen_split_mode": normEnumValue(settings.PrintKitchenSplitMode, po.KitchenSplitModes, po.KitchenSplitWhole),
		// 页脚是普通文本项,允许清空(清空=不打印页脚);段落开关归一化落库。
		"print_guest_footer":        settings.PrintGuestFooter,
		"print_guest_show_discount": normFlagValue(settings.PrintGuestShowDiscount),
		"print_guest_on_append":     normFlagValue(settings.PrintGuestOnAppend),
		"print_guest_on_order":      normFlagValue(settings.PrintGuestOnOrder),
		"feie_user":                 settings.FeieUser,
		"feie_api_url":              settings.FeieApiURL,
		"agent_latest_version":      settings.AgentLatestVersion,
		// 桌台遗留订单防护:开关落库前归一化,阈值越界回落默认值(不整份拒绝保存)
		"stale_release_enabled": normFlagValue(settings.StaleReleaseEnabled),
		"stale_alert_enabled":   normFlagValue(settings.StaleAlertEnabled),
		"stale_alert_hours":     NormalizeStaleAlertHours(settings.StaleAlertHours),
		"biz_date_cutoff_hour":  NormalizeBizDateCutoffHour(settings.BizDateCutoffHour),
		// 先付/后付模式:开关归一化,落库永远是 "1"/"0"
		"pay_first_enabled": normFlagValue(settings.PayFirstEnabled),
	}
	// 普通配置项:提交值直接覆盖保存(敏感项由 SetSetting 自动加密)。
	// 旧值快照必须赶在 SetSetting 之前取——落库后 LoadSettings 读到的已是新值,
	// 摘要里的「旧值 → 新值」会因此恒相等,变更记录全部丢失。
	before := dao.LoadSettings()
	// 落库失败的配置键:任一项失败都不允许回「保存成功」,调用方据此应答错误。
	var failedKeys []string
	for k, v := range vals {
		// 出厂默认非空却被保存为空:记录告警,便于追查「功能静默缺失」的起因。
		if v == "" && dao.HasNonEmptyDefault(k) {
			logger.CtxWarnf(ctx, "[setting][告警] 管理员 %s 将配置项 %s 保存为空值(出厂默认应为 %q)", actor, k, dao.DefaultValue(k))
		}
		if err := dao.SetSetting(k, v); err != nil {
			failedKeys = append(failedKeys, k)
			logger.CtxErrorf(ctx, "[setting][告警] 保存配置项 %s 失败: %v", k, err)
		}
	}

	// 敏感配置项:留空或原样带回占位符都表示「不修改」。
	// 前端把回显的占位符(******)随表单原样提交,必须先归一化为空串:
	// 否则占位符会被当真值落库,审计摘要也会每次都记一笔假变更。
	secrets := map[string]string{
		"wxpay_apiv3_key": settings.WxpayApiv3Key,
		"wx_app_secret":   settings.WxAppSecret,
		"feie_ukey":       settings.FeieUkey,
		"agent_token":     settings.AgentToken,
	}
	for k, v := range secrets {
		if v == SecretMask {
			secrets[k] = ""
		}
	}
	for k, v := range secrets {
		if !infrastructure.IsSensitiveKey(k) {
			logger.CtxWarnf(ctx, "[setting] %s 未登记为敏感项,已跳过", k)
			continue
		}
		if v == "" {
			continue
		}
		if err := dao.SetSetting(k, v); err != nil {
			failedKeys = append(failedKeys, k)
			logger.CtxErrorf(ctx, "[setting][告警] 保存敏感配置项 %s 失败: %v", k, err)
		}
	}
	// 「清空代理令牌」是一次性指令,必须在敏感项写入之后执行,否则会被本次提交的
	// 新令牌覆盖。清空后所有用该全局令牌的旧代理立即失联(须逐台改配或改签逐台身份),
	// 属于影响面较大的动作,故在审计摘要里单独留痕。
	agentTokenCleared := false
	if settings.AgentTokenClear == "1" {
		if dao.GetSetting("agent_token") != "" {
			agentTokenCleared = true
		}
		if err := dao.SetSetting("agent_token", ""); err != nil {
			logger.CtxErrorf(ctx, "[setting][告警] 清空代理令牌失败: %v", err)
			agentTokenCleared = false
			failedKeys = append(failedKeys, "agent_token")
		}
	}
	// 有配置项落库失败:不生成「保存成功」的审计摘要,把失败键清单交给调用方应答。
	if len(failedKeys) > 0 {
		return "", false, fmt.Errorf("以下配置项保存失败: %s", strings.Join(failedKeys, ", "))
	}
	logger.CtxInfof(ctx, "[setting] 管理员 %s 保存配置完成(普通项 %d + 敏感项 %d)", actor, len(vals), len(secrets))
	detail = describeSettingChange(before, vals, secrets)
	if agentTokenCleared {
		detail += "；清空代理令牌(敏感项,值不记录)"
	}
	return detail, false, nil
}

// IssueAgentToken 签发 legacy 全局打印代理令牌,返回仅展示一次的明文与新指纹。
//
// 令牌改为后端生成并直接落库(加密),不再走「前端生成 → 随整份配置保存提交」:
// 后者要求管理员记得再点一次「保存配置」,漏点就会出现「令牌已填进门店代理、
// 云端却从未存过它」的哑火状态,而界面上敏感项不回显、输入框恒为空,看不出差别。
//
// 必须回传新指纹:agent_token 参与 ManagedSettingKeys 指纹,签发后库内容已变,
// 管理端手上那份 version 立即过期;不回传的话管理员下一次保存整份配置会撞 409,
// 而 409 分支会用后端数据整表覆盖表单,把他还没保存的其它改动一起冲掉。
//
// 明文与 hint 只在本响应里出现,库中只有密文;旧令牌被就地替换,用它的代理立即失联。
func IssueAgentToken() (token, hint, version string, err error) {
	settingSaveMu.Lock()
	defer settingSaveMu.Unlock()

	token = dao.NewAgentToken()
	if err = dao.SetSetting("agent_token", token); err != nil {
		return "", "", "", err
	}
	return token, token[:4], dao.SettingsFingerprint(ManagedSettingKeys), nil
}

// describeSettingChange 生成配置变更摘要:普通项记键名与新旧值,敏感项只记键名。
//
// 配置里混着支付密钥,值一律不进日志;但「谁在什么时候改了支付商户号」
// 必须能查到 —— 这类改动往往直接关联资金流向。
// before 是保存前的旧值快照,由调用方在 SetSetting 之前取好传入。
func describeSettingChange(before map[string]string, vals, secrets map[string]string) string {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changed := []string{}
	for _, k := range keys {
		if before[k] != vals[k] {
			changed = append(changed, k+": "+blankIfEmpty(before[k])+" → "+blankIfEmpty(vals[k]))
		}
	}
	// 敏感项:前端不回显、留空表示不修改,故只判断「本次是否提交了新的值」。
	for _, k := range []string{"wxpay_apiv3_key", "feie_ukey", "agent_token"} {
		if secrets[k] != "" {
			changed = append(changed, k+"(敏感项,值不记录)")
		}
	}
	if len(changed) == 0 {
		return "保存系统配置，无字段变更"
	}
	s := "修改配置项: " + strings.Join(changed, "；")
	if len([]rune(s)) > 400 {
		return string([]rune(s)[:400]) + "…"
	}
	return s
}

// blankIfEmpty 空值显示为「(空)」,避免摘要里出现「 → 」这种看不懂的片段。
func blankIfEmpty(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(空)"
	}
	return v
}
