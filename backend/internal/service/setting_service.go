package service

import (
	"sort"
	"strings"
	"sync"

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/dto"
	"dining-system/internal/store/dao"
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
//   - 敏感项(infra.IsSensitiveKey,如 APIv3 密钥)一律不回显,前端留空即表示「不修改」;
//   - 密钥材料(商户私钥 / 平台证书 / 微信支付公钥)一律只保存并回显「文件路径」,
//     内容本身从不入库,故回显路径是安全的。

// ManagedSettingKeys 配置页管理的全部键(普通项 + 敏感项),是乐观锁指纹的覆盖范围,
// 必须与 ListSettings 的回显字段、SaveSettings 的 vals+secrets 提交面保持一致
// (TestManagedSettingKeysComplete 会守护这层一致)。指纹刻意不覆盖
// agent_last_seen(代理心跳,秒级变化)、admin_pass_hash 等内部键——
// 它们的变化不该把管理员的保存误判为并发冲突。
var ManagedSettingKeys = []string{
	"shop_name", "shop_logo", "h5_base_url",
	"seat_fee_enabled", "seat_fee",
	"promotion_enabled", "promotion_threshold", "promotion_discount",
	"pay_qr_wx", "pay_qr_ali",
	"wxpay_enabled", "wxpay_mchid", "wxpay_appid", "wxpay_serial_no",
	"wxpay_private_key_path", "wxpay_platform_cert_path",
	"wxpay_pubkey_id", "wxpay_pubkey_path", "wxpay_notify_url",
	"alipay_enabled", "alipay_appid", "alipay_private_key_path",
	"alipay_public_key", "alipay_notify_url",
	"print_enabled", "print_kitchen_show_price",
	"print_guest_footer", "print_guest_show_seat_fee", "print_guest_show_discount",
	"feie_user", "feie_api_url", "agent_latest_version",
	// 敏感项(不回显,但同样参与指纹:它们变了,别的管理端就该刷新)
	"wxpay_apiv3_key", "feie_ukey", "agent_token",
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

// SettingFlagDefaultOn 「默认开启」型开关的读取归一化:只有明确 "0" 才算关闭,
// 其余(含 "1"、空值、配置项缺失)一律返回 "1"。
//
// 与 SettingFlag 的分工:打印总开关等「默认关」用 SettingFlag;食客小票段落开关
// (餐位费/优惠行)出厂即显示,渲染侧(print.guestShowSeatFee)按「!= "0」判定,
// 回显侧必须用同一套语义,否则键缺失时配置页显示「关」而实际仍在打印。
func SettingFlagDefaultOn(key string) string {
	if dao.GetSetting(key) == "0" {
		return "0"
	}
	return "1"
}

// ListSettings 读取并组装配置页回显:普通项直接回显,开关项归一化,
// 敏感项留空,指纹作为乐观锁 version 一并下发。
func ListSettings() dto.Settings {
	return dto.Settings{
		ShopName:           dao.GetSetting("shop_name"),
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
		// 微信支付(APIv3 密钥为敏感项,回显时脱敏为空,前端留空表示不修改)
		WxpayMchid:            dao.GetSetting("wxpay_mchid"),
		WxpayAppid:            dao.GetSetting("wxpay_appid"),
		WxpaySerialNo:         dao.GetSetting("wxpay_serial_no"),
		WxpayPrivateKeyPath:   dao.GetSetting("wxpay_private_key_path"),
		WxpayPlatformCertPath: dao.GetSetting("wxpay_platform_cert_path"),
		WxpayPubkeyID:         dao.GetSetting("wxpay_pubkey_id"),
		WxpayPubkeyPath:       dao.GetSetting("wxpay_pubkey_path"),
		WxpayNotifyURL:        dao.GetSetting("wxpay_notify_url"),
		// 支付宝
		AlipayAppid:          dao.GetSetting("alipay_appid"),
		AlipayPrivateKeyPath: dao.GetSetting("alipay_private_key_path"),
		AlipayPublicKey:      dao.GetSetting("alipay_public_key"),
		AlipayNotifyURL:      dao.GetSetting("alipay_notify_url"),
		// 小票打印
		PrintEnabled:          SettingFlag("print_enabled"),
		PrintKitchenShowPrice: SettingFlag("print_kitchen_show_price"),
		// 食客小票模板配置(页脚可清空,两个段落开关按「默认开」语义归一化,与渲染侧一致)
		PrintGuestFooter:       dao.GetSetting("print_guest_footer"),
		PrintGuestShowSeatFee:  SettingFlagDefaultOn("print_guest_show_seat_fee"),
		PrintGuestShowDiscount: SettingFlagDefaultOn("print_guest_show_discount"),
		// 飞鹅云打印(feie_ukey 为敏感项,回显留空表示不修改)
		FeieUser:   dao.GetSetting("feie_user"),
		FeieApiURL: dao.GetSetting("feie_api_url"),
		// 本地打印代理(普通配置项,直接回显)
		AgentLatestVersion: dao.GetSetting("agent_latest_version"),
		// 乐观锁指纹:保存时原样带回,后端比对发现不一致即拒绝(409)
		Version: dao.SettingsFingerprint(ManagedSettingKeys),
	}
}

// SaveSettings 保存整份配置,返回审计摘要与是否发生乐观锁冲突。
//
// 串行化整个保存过程:指纹校验、快照、落库、摘要之间不留并发窗口。
// 校验必须在锁内——若在锁外,两个请求可同时通过校验,后获得锁的
// 仍会用旧快照整表覆盖先保存者的修改,互斥形同虚设。
//
// 乐观锁:version 来自 ListSettings 下发的指纹,与当前库内容不一致说明
// 有其他人已先保存——继续提交会用旧快照整表覆盖掉对方的修改。
// 旧前端不传 version,跳过校验保持兼容。冲突时不落库、不生成摘要。
func SaveSettings(settings dto.Settings, actor string) (detail string, conflict bool) {
	settingSaveMu.Lock()
	defer settingSaveMu.Unlock()

	if settings.Version != "" && settings.Version != dao.SettingsFingerprint(ManagedSettingKeys) {
		return "", true
	}

	vals := map[string]string{
		"shop_name":                settings.ShopName,
		"shop_logo":                settings.ShopLogo,
		"seat_fee_enabled":         normFlagValue(settings.SeatFeeEnabled),
		"seat_fee":                 settings.SeatFee,
		"pay_qr_wx":                settings.PayQrWx,
		"pay_qr_ali":               settings.PayQrAli,
		"h5_base_url":              settings.H5BaseURL,
		"promotion_enabled":        normFlagValue(settings.PromotionEnabled),
		"promotion_threshold":      settings.PromotionThreshold,
		"promotion_discount":       settings.PromotionDiscount,
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
		"alipay_appid":             settings.AlipayAppid,
		"alipay_private_key_path":  settings.AlipayPrivateKeyPath,
		"alipay_public_key":        settings.AlipayPublicKey,
		"alipay_notify_url":        settings.AlipayNotifyURL,
		"print_enabled":            normFlagValue(settings.PrintEnabled),
		"print_kitchen_show_price": normFlagValue(settings.PrintKitchenShowPrice),
		// 页脚是普通文本项,允许清空(清空=不打印页脚);两个段落开关归一化落库。
		"print_guest_footer":        settings.PrintGuestFooter,
		"print_guest_show_seat_fee": normFlagValue(settings.PrintGuestShowSeatFee),
		"print_guest_show_discount": normFlagValue(settings.PrintGuestShowDiscount),
		"feie_user":                 settings.FeieUser,
		"feie_api_url":              settings.FeieApiURL,
		"agent_latest_version":      settings.AgentLatestVersion,
	}
	// 普通配置项:提交值直接覆盖保存(敏感项由 SetSetting 自动加密)。
	// 旧值快照必须赶在 SetSetting 之前取——落库后 LoadSettings 读到的已是新值,
	// 摘要里的「旧值 → 新值」会因此恒相等,变更记录全部丢失。
	before := dao.LoadSettings()
	for k, v := range vals {
		// 出厂默认非空却被保存为空:记录告警,便于追查「功能静默缺失」的起因。
		if v == "" && dao.HasNonEmptyDefault(k) {
			logger.Warnf("[setting][告警] 管理员 %s 将配置项 %s 保存为空值(出厂默认应为 %q)", actor, k, dao.DefaultValue(k))
		}
		if err := dao.SetSetting(k, v); err != nil {
			logger.Warnf("[setting] 保存配置项 %s 失败: %v", k, err)
		}
	}

	// 敏感配置项:留空表示「不修改」。
	// 前端出于安全不回显敏感项,若按空值覆盖会误清掉已配置的密钥。
	secrets := map[string]string{
		"wxpay_apiv3_key": settings.WxpayApiv3Key,
		"feie_ukey":       settings.FeieUkey,
		"agent_token":     settings.AgentToken,
	}
	for k, v := range secrets {
		if !infra.IsSensitiveKey(k) {
			logger.Warnf("[setting] %s 未登记为敏感项,已跳过", k)
			continue
		}
		if v == "" {
			continue
		}
		if err := dao.SetSetting(k, v); err != nil {
			logger.Warnf("[setting] 保存敏感配置项 %s 失败: %v", k, err)
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
			logger.Warnf("[setting] 清空代理令牌失败: %v", err)
			agentTokenCleared = false
		}
	}
	logger.Infof("[setting] 管理员 %s 保存配置完成(普通项 %d + 敏感项 %d)", actor, len(vals), len(secrets))
	detail = describeSettingChange(before, vals, secrets)
	if agentTokenCleared {
		detail += "；清空代理令牌(敏感项,值不记录)"
	}
	return detail, false
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
