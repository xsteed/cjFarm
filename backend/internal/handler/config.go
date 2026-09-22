package handler

import (
	"dining-system/internal/logger"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 店铺配置 ============
//
// 回显策略(白名单):
//   - 普通配置项全部回显;
//   - 敏感项(store.IsSensitiveKey,如 APIv3 密钥)一律不回显,前端留空即表示「不修改」;
//   - 密钥材料(商户私钥 / 平台证书 / 微信支付公钥)一律只保存并回显「文件路径」,
//     内容本身从不入库,故回显路径是安全的。

// cfgFlag 读取「开关型」配置项,统一归一化为 "1" / "0"。
//
// 前端 t-switch 使用 :custom-value="['1','0']",一旦收到空串或意外取值会直接抛
// `value is not in ["1","0"]`;该异常发生在组件更新阶段,会中断整棵路由树的 patch,
// 表现为「进入系统配置页后,再点其他菜单全部白屏」。因此这里做下发兜底:
// 只有明确等于 "1" 才算开启,其余(含配置项缺失)一律返回 "0"。
func cfgFlag(key string) string {
	if store.GetCfg(key) == "1" {
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

func ConfigList(c *gin.Context) {
	cfg := model.Config{
		ShopName:           store.GetCfg("shop_name"),
		ShopLogo:           store.GetCfg("shop_logo"),
		SeatFeeEnabled:     cfgFlag("seat_fee_enabled"),
		SeatFee:            store.GetCfg("seat_fee"),
		PayQrWx:            store.GetCfg("pay_qr_wx"),
		PayQrAli:           store.GetCfg("pay_qr_ali"),
		H5BaseURL:          store.GetCfg("h5_base_url"),
		PromotionEnabled:   cfgFlag("promotion_enabled"),
		PromotionThreshold: store.GetCfg("promotion_threshold"),
		PromotionDiscount:  store.GetCfg("promotion_discount"),
		WxpayEnabled:       cfgFlag("wxpay_enabled"),
		AlipayEnabled:      cfgFlag("alipay_enabled"),
		// 微信支付(APIv3 密钥为敏感项,回显时脱敏为空,前端留空表示不修改)
		WxpayMchid:            store.GetCfg("wxpay_mchid"),
		WxpayAppid:            store.GetCfg("wxpay_appid"),
		WxpaySerialNo:         store.GetCfg("wxpay_serial_no"),
		WxpayPrivateKeyPath:   store.GetCfg("wxpay_private_key_path"),
		WxpayPlatformCertPath: store.GetCfg("wxpay_platform_cert_path"),
		WxpayPubkeyID:         store.GetCfg("wxpay_pubkey_id"),
		WxpayPubkeyPath:       store.GetCfg("wxpay_pubkey_path"),
		WxpayNotifyURL:        store.GetCfg("wxpay_notify_url"),
		// 支付宝
		AlipayAppid:          store.GetCfg("alipay_appid"),
		AlipayPrivateKeyPath: store.GetCfg("alipay_private_key_path"),
		AlipayPublicKey:      store.GetCfg("alipay_public_key"),
		AlipayNotifyURL:      store.GetCfg("alipay_notify_url"),
		// 小票打印
		PrintEnabled:          cfgFlag("print_enabled"),
		PrintKitchenShowPrice: cfgFlag("print_kitchen_show_price"),
		// 飞鹅云打印(feie_ukey 为敏感项,回显留空表示不修改)
		FeieUser:   store.GetCfg("feie_user"),
		FeieApiURL: store.GetCfg("feie_api_url"),
	}
	ok(c, cfg)
}

func ConfigSave(c *gin.Context) {
	var cfg model.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		fail(c, "参数错误")
		return
	}
	vals := map[string]string{
		"shop_name":                cfg.ShopName,
		"shop_logo":                cfg.ShopLogo,
		"seat_fee_enabled":         normFlagValue(cfg.SeatFeeEnabled),
		"seat_fee":                 cfg.SeatFee,
		"pay_qr_wx":                cfg.PayQrWx,
		"pay_qr_ali":               cfg.PayQrAli,
		"h5_base_url":              cfg.H5BaseURL,
		"promotion_enabled":        normFlagValue(cfg.PromotionEnabled),
		"promotion_threshold":      cfg.PromotionThreshold,
		"promotion_discount":       cfg.PromotionDiscount,
		"wxpay_enabled":            normFlagValue(cfg.WxpayEnabled),
		"alipay_enabled":           normFlagValue(cfg.AlipayEnabled),
		"wxpay_mchid":              cfg.WxpayMchid,
		"wxpay_appid":              cfg.WxpayAppid,
		"wxpay_serial_no":          cfg.WxpaySerialNo,
		"wxpay_private_key_path":   cfg.WxpayPrivateKeyPath,
		"wxpay_platform_cert_path": cfg.WxpayPlatformCertPath,
		"wxpay_pubkey_id":          cfg.WxpayPubkeyID,
		"wxpay_pubkey_path":        cfg.WxpayPubkeyPath,
		"wxpay_notify_url":         cfg.WxpayNotifyURL,
		"alipay_appid":             cfg.AlipayAppid,
		"alipay_private_key_path":  cfg.AlipayPrivateKeyPath,
		"alipay_public_key":        cfg.AlipayPublicKey,
		"alipay_notify_url":        cfg.AlipayNotifyURL,
		"print_enabled":            normFlagValue(cfg.PrintEnabled),
		"print_kitchen_show_price": normFlagValue(cfg.PrintKitchenShowPrice),
		"feie_user":                cfg.FeieUser,
		"feie_api_url":             cfg.FeieApiURL,
	}
	// 普通配置项:提交值直接覆盖保存(敏感项由 SetCfg 自动加密)。
	actor := adminName(c)
	for k, v := range vals {
		// 出厂默认非空却被保存为空:记录告警,便于追查「功能静默缺失」的起因。
		if v == "" && store.HasNonEmptyDefault(k) {
			logger.Warnf("[config][告警] 管理员 %s 将配置项 %s 保存为空值(出厂默认应为 %q)", actor, k, store.DefaultValue(k))
		}
		if err := store.SetCfg(k, v); err != nil {
			logger.Warnf("[config] 保存配置项 %s 失败: %v", k, err)
		}
	}

	// 敏感配置项:留空表示「不修改」。
	// 前端出于安全不回显敏感项,若按空值覆盖会误清掉已配置的密钥。
	secrets := map[string]string{
		"wxpay_apiv3_key": cfg.WxpayApiv3Key,
		"feie_ukey":       cfg.FeieUkey,
		"agent_token":     cfg.AgentToken,
	}
	for k, v := range secrets {
		if !store.IsSensitiveKey(k) {
			logger.Warnf("[config] %s 未登记为敏感项,已跳过", k)
			continue
		}
		if v == "" {
			continue
		}
		if err := store.SetCfg(k, v); err != nil {
			logger.Warnf("[config] 保存敏感配置项 %s 失败: %v", k, err)
		}
	}
	logger.Infof("[config] 管理员 %s 保存配置完成(普通项 %d + 敏感项 %d)", actor, len(vals), len(secrets))
	SetAuditDetail(c, "config", "", describeConfigChange(vals, secrets))
	okMsg(c, "保存成功")
}

// describeConfigChange 生成配置变更摘要:普通项记键名与新旧值,敏感项只记键名。
//
// 配置里混着支付密钥,值一律不进日志;但「谁在什么时候改了支付商户号」
// 必须能查到 —— 这类改动往往直接关联资金流向。
func describeConfigChange(vals, secrets map[string]string) string {
	cur := store.LoadConfig()
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changed := []string{}
	for _, k := range keys {
		if cur[k] != vals[k] {
			changed = append(changed, k+": "+blankIfEmpty(cur[k])+" → "+blankIfEmpty(vals[k]))
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
