package pay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"dining-system/internal/service"
	"dining-system/internal/store"
)

// ---- 测试辅助(统一 pm_ 前缀,避免与 wxpay_test.go 冲突) ----

// pmInitPayDB 为支付测试准备独立配置库,并把上传目录指到临时目录避免污染工作区。
func pmInitPayDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "pay_more.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// pmSetSettings 批量写入配置项。
func pmSetSettings(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := service.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%q) 失败: %v", k, err)
		}
	}
}

// pmGenRSA 生成 RSA 私钥与公钥 PKIX PEM,供签名/验签测试使用。
func pmGenRSA(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("序列化公钥失败: %v", err)
	}
	return priv, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// pmWriteFile 写文件,失败即中止测试。
func pmWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写文件 %s 失败: %v", path, err)
	}
}

// pmAlipaySign 按支付宝回调验签规则(排除 sign/sign_type、按键排序)生成签名。
func pmAlipaySign(t *testing.T, priv *rsa.PrivateKey, params map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params[k])
	}
	return signRSASHA256(priv, sb.String())
}

// pmAESGCMEncrypt 用 AES-256-GCM 加密明文,返回密文(与微信回调 resource 一致)。
func pmAESGCMEncrypt(t *testing.T, key, nonce, aad, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher 失败: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM 失败: %v", err)
	}
	return gcm.Seal(nil, nonce, plain, aad)
}

// ---- 渠道分发与命名 ----

// TestPayGetChannelDispatch 校验合法/非法渠道的分发与错误返回。
func TestPayGetChannelDispatch(t *testing.T) {
	if p, err := Get(ChannelWxpay); err != nil {
		t.Fatalf("Get(wxpay) 意外失败: %v", err)
	} else if _, ok := p.(*Wxpay); !ok {
		t.Fatalf("Get(wxpay) 类型 = %T, want *Wxpay", p)
	}
	if p, err := Get(ChannelAlipay); err != nil {
		t.Fatalf("Get(alipay) 意外失败: %v", err)
	} else if _, ok := p.(*Alipay); !ok {
		t.Fatalf("Get(alipay) 类型 = %T, want *Alipay", p)
	}
	for _, ch := range []string{"", "unionpay", "WXPAY", "wxpay ", "ali"} {
		p, err := Get(ch)
		if err == nil {
			t.Errorf("Get(%q) 应返回错误, got %T", ch, p)
		}
		if p != nil {
			t.Errorf("Get(%q) 失败时 Provider 应为 nil, got %T", ch, p)
		}
	}
}

// TestPayProviderNames 校验两个渠道的名称常量。
func TestPayProviderNames(t *testing.T) {
	if got := (&Wxpay{}).Name(); got != ChannelWxpay {
		t.Errorf("Wxpay.Name() = %q, want %q", got, ChannelWxpay)
	}
	if got := (&Alipay{}).Name(); got != ChannelAlipay {
		t.Errorf("Alipay.Name() = %q, want %q", got, ChannelAlipay)
	}
}

// ---- 配置读取与开关 ----

// TestPayCfgAndCfgEnabled 校验配置读取:空值、明文读取与开关归一化。
func TestPayCfgAndCfgEnabled(t *testing.T) {
	pmInitPayDB(t)
	if got := cfg("pay_more_missing"); got != "" {
		t.Errorf("缺失配置应返回空串, got %q", got)
	}
	if cfgEnabled("pay_more_missing") {
		t.Errorf("缺失开关配置不应视为开启")
	}
	pmSetSettings(t, map[string]string{"pay_more_on": "1", "pay_more_other": "yes"})
	if !cfgEnabled("pay_more_on") {
		t.Errorf("值为 \"1\" 应视为开启")
	}
	if cfgEnabled("pay_more_other") {
		t.Errorf("值为 \"yes\" 不应视为开启")
	}
	if got := cfg("pay_more_other"); got != "yes" {
		t.Errorf("cfg 未透传明文, got %q", got)
	}
}

// TestPayAlipayEnabledMore 校验支付宝启用判定:配置齐全/逐项缺失。
func TestPayAlipayEnabledMore(t *testing.T) {
	pmInitPayDB(t)
	p := &Alipay{}
	if p.Enabled() {
		t.Fatal("未配置任何项时不应启用")
	}
	full := map[string]string{
		"alipay_enabled":          "1",
		"alipay_appid":            "appid",
		"alipay_private_key_path": "/tmp/ali_key.pem",
		"alipay_public_key":       "pub",
	}
	pmSetSettings(t, full)
	if !p.Enabled() {
		t.Fatal("配置齐全时应启用")
	}
	for _, key := range []string{"alipay_enabled", "alipay_appid", "alipay_private_key_path", "alipay_public_key"} {
		pmSetSettings(t, map[string]string{key: ""})
		if p.Enabled() {
			t.Errorf("缺少 %s 时不应启用", key)
		}
		pmSetSettings(t, map[string]string{key: full[key]})
	}
	// 开关关闭时其余齐全也不启用。
	pmSetSettings(t, map[string]string{"alipay_enabled": "0"})
	if p.Enabled() {
		t.Error("alipay_enabled=0 时不应启用")
	}
}

// TestPayWxpayEnabledMore 校验微信启用判定:基础项 + 验签材料任一模式。
func TestPayWxpayEnabledMore(t *testing.T) {
	pmInitPayDB(t)
	p := &Wxpay{}
	if p.Enabled() {
		t.Fatal("未配置任何项时不应启用")
	}
	full := map[string]string{
		"wxpay_enabled":            "1",
		"wxpay_mchid":              "1900000109",
		"wxpay_apiv3_key":          "0123456789abcdef0123456789abcdef",
		"wxpay_serial_no":          "ABCDEF123456",
		"wxpay_private_key_path":   "/tmp/apiclient_key.pem",
		"wxpay_platform_cert_path": "/tmp/certs",
	}
	pmSetSettings(t, full)
	if !p.Enabled() {
		t.Fatal("基础项 + 平台证书齐备时应启用")
	}
	// 清掉平台证书但配置公钥模式,同样可启用。
	pmSetSettings(t, map[string]string{
		"wxpay_platform_cert_path": "",
		"wxpay_pubkey_id":          "PUB_ID",
		"wxpay_pubkey_path":        "/tmp/pub_key.pem",
	})
	if !p.Enabled() {
		t.Fatal("公钥模式齐备时应启用")
	}
	// 两种验签材料都缺 → 不启用。
	pmSetSettings(t, map[string]string{"wxpay_pubkey_id": "", "wxpay_pubkey_path": ""})
	if p.Enabled() {
		t.Fatal("缺少验签材料时不应启用")
	}
	// 关闭开关。
	pmSetSettings(t, full)
	pmSetSettings(t, map[string]string{"wxpay_enabled": "0"})
	if p.Enabled() {
		t.Fatal("wxpay_enabled=0 时不应启用")
	}
}

// TestPayWxpayVerifyMaterialReady 单独校验验签材料判定逻辑。
func TestPayWxpayVerifyMaterialReady(t *testing.T) {
	pmInitPayDB(t)
	p := &Wxpay{}
	if p.verifyMaterialReady() {
		t.Fatal("未配置验签材料时应为 false")
	}
	pmSetSettings(t, map[string]string{"wxpay_platform_cert_path": "/tmp/c"})
	if !p.verifyMaterialReady() {
		t.Fatal("配置平台证书后应为 true")
	}
	pmSetSettings(t, map[string]string{
		"wxpay_platform_cert_path": "",
		"wxpay_pubkey_id":          "ID",
		"wxpay_pubkey_path":        "/tmp/p",
	})
	if !p.verifyMaterialReady() {
		t.Fatal("公钥ID+公钥文件齐备后应为 true")
	}
	pmSetSettings(t, map[string]string{"wxpay_pubkey_path": ""})
	if p.verifyMaterialReady() {
		t.Fatal("只有公钥ID、缺公钥文件时应为 false")
	}
}

// ---- 金额转换 ----

// TestPayCentsToYuanMore 补充「分→元」字符串转换边界。
func TestPayCentsToYuanMore(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{1234, "12.34"},
		{2800, "28.00"},
		{-1234, "-12.34"},
		{123456789012, "1234567890.12"},
	}
	for _, c := range cases {
		if got := centsToYuan(c.in); got != c.want {
			t.Errorf("centsToYuan(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPayYuanToCentsMore 补充「元→分」字符串转换边界(仅正常正值,支付宝金额恒为正)。
// 非法金额必须报错,而不是静默返回 0 —— 0 元可能被误判为「金额一致」或「免费单」。
func TestPayYuanToCentsMore(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"1", 100, false},
		{"0.01", 1, false},
		{"12.34", 1234, false},
		{"28.00", 2800, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := yuanToCents(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("yuanToCents(%q) 应报错, got %d", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("yuanToCents(%q) = %d, err=%v, want %d", c.in, got, err, c.want)
		}
	}
}

// ---- 文本截断与签名边界 ----

// TestPayTruncateRunes 校验按 rune 截断,中文多字节不截半个字符。
func TestPayTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc"},
		{"你好世界", 2, "你好"},
		{"你好世界", 4, "你好世界"},
		{"abc", 0, ""},
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.n); got != c.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

// TestPayVerifyRSASignatureInvalidBase64 非法 base64 签名串应返回 false 而非 panic。
func TestPayVerifyRSASignatureInvalidBase64(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if verifyRSASignature(&priv.PublicKey, "msg", "!!not-base64!!") {
		t.Fatal("非法 base64 签名不应验签通过")
	}
	if verifyRSASignature(&priv.PublicKey, "msg", "") {
		t.Fatal("空签名不应验签通过")
	}
}

// TestPayNotifySuccessBody 校验应答渠道「已收到」的固定响应体。
func TestPayNotifySuccessBody(t *testing.T) {
	if got := (&Alipay{}).NotifySuccessBody(); got != "success" {
		t.Errorf("Alipay.NotifySuccessBody() = %q, want success", got)
	}
	if got := (&Wxpay{}).NotifySuccessBody(); got != `{"code":"SUCCESS","message":"成功"}` {
		t.Errorf("Wxpay.NotifySuccessBody() = %q", got)
	}
}

// ---- 支付宝回调验签 ----

// TestPayAlipayPublicKey 校验支付宝公钥解析:纯 base64、PEM 与非法输入。
func TestPayAlipayPublicKey(t *testing.T) {
	pmInitPayDB(t)
	p := &Alipay{}
	if _, err := p.publicKey(); err == nil {
		t.Fatal("公钥未配置时应报错")
	}
	_, pubPEM := pmGenRSA(t)
	block, _ := pem.Decode(pubPEM)
	b64 := base64.StdEncoding.EncodeToString(block.Bytes)

	pmSetSettings(t, map[string]string{"alipay_public_key": b64})
	if _, err := p.publicKey(); err != nil {
		t.Fatalf("纯 base64 公钥应解析成功: %v", err)
	}
	pmSetSettings(t, map[string]string{"alipay_public_key": string(pubPEM)})
	if _, err := p.publicKey(); err != nil {
		t.Fatalf("PEM 公钥应解析成功: %v", err)
	}
	pmSetSettings(t, map[string]string{"alipay_public_key": "not-a-valid-key"})
	if _, err := p.publicKey(); err == nil {
		t.Fatal("非法公钥应报错")
	}
}

// TestPayAlipayVerifyNotify 覆盖支付宝回调验签的成功与失败路径。
func TestPayAlipayVerifyNotify(t *testing.T) {
	pmInitPayDB(t)
	priv, pubPEM := pmGenRSA(t)
	pmSetSettings(t, map[string]string{
		"alipay_public_key": string(pubPEM),
		"alipay_appid":      "2021000000000001",
	})
	p := &Alipay{}

	baseParams := map[string]string{
		"app_id":       "2021000000000001",
		"out_trade_no": "D123",
		"trade_no":     "2024010122001234567890",
		"total_amount": "12.34",
		"trade_status": "TRADE_SUCCESS",
		"sign_type":    "RSA2",
	}
	formBody := func(m map[string]string) []byte {
		v := url.Values{}
		for k, val := range m {
			v.Set(k, val)
		}
		return []byte(v.Encode())
	}

	t.Run("成功", func(t *testing.T) {
		params := map[string]string{}
		for k, v := range baseParams {
			params[k] = v
		}
		params["sign"] = pmAlipaySign(t, priv, params)
		n, err := p.VerifyNotify(nil, formBody(params))
		if err != nil {
			t.Fatalf("VerifyNotify 失败: %v", err)
		}
		if n.OrderNo != "D123" || n.ChannelTradeNo != "2024010122001234567890" || n.AmountCents != 1234 || !n.Success {
			t.Errorf("通知解析结果不符: %+v", n)
		}
	})

	t.Run("商户归属不符", func(t *testing.T) {
		params := map[string]string{}
		for k, v := range baseParams {
			params[k] = v
		}
		params["app_id"] = "2099000000000000" // 其它应用的 app_id,签名合法但归属不符
		params["sign"] = pmAlipaySign(t, priv, params)
		if _, err := p.VerifyNotify(nil, formBody(params)); err == nil || !strings.Contains(err.Error(), "商户归属不符") {
			t.Errorf("app_id 不符应报「商户归属不符」, got %v", err)
		}
	})

	t.Run("金额解析失败", func(t *testing.T) {
		params := map[string]string{}
		for k, v := range baseParams {
			params[k] = v
		}
		params["total_amount"] = "abc"
		params["sign"] = pmAlipaySign(t, priv, params)
		if _, err := p.VerifyNotify(nil, formBody(params)); err == nil || !strings.Contains(err.Error(), "金额解析失败") {
			t.Errorf("非法金额应报「金额解析失败」, got %v", err)
		}
	})

	t.Run("缺签名", func(t *testing.T) {
		if _, err := p.VerifyNotify(nil, formBody(baseParams)); err == nil || !strings.Contains(err.Error(), "缺少签名") {
			t.Errorf("缺少签名时应报「缺少签名」, got %v", err)
		}
	})

	t.Run("验签失败", func(t *testing.T) {
		params := map[string]string{}
		for k, v := range baseParams {
			params[k] = v
		}
		sig := pmAlipaySign(t, priv, params)
		params["total_amount"] = "99.99" // 篡改金额但保留旧签名
		params["sign"] = sig
		if _, err := p.VerifyNotify(nil, formBody(params)); err == nil || !strings.Contains(err.Error(), "验签失败") {
			t.Errorf("篡改金额后应报「验签失败」, got %v", err)
		}
	})

	t.Run("参数解析失败", func(t *testing.T) {
		if _, err := p.VerifyNotify(nil, []byte("%zz")); err == nil {
			t.Fatal("非法表单应报参数解析失败")
		}
	})

	t.Run("未成交状态", func(t *testing.T) {
		params := map[string]string{}
		for k, v := range baseParams {
			params[k] = v
		}
		params["trade_status"] = "WAIT_BUYER_PAY"
		params["sign"] = pmAlipaySign(t, priv, params)
		n, err := p.VerifyNotify(nil, formBody(params))
		if err != nil {
			t.Fatalf("VerifyNotify 失败: %v", err)
		}
		if n.Success {
			t.Errorf("WAIT_BUYER_PAY 不应判定为成功: %+v", n)
		}
	})
}

// ---- 微信回调验签+解密 ----

// TestPayWxpayVerifyNotify 覆盖微信回调验签+解密的成功与失败路径(公钥模式)。
func TestPayWxpayVerifyNotify(t *testing.T) {
	pmInitPayDB(t)
	priv, pubPEM := pmGenRSA(t)
	pubPath := filepath.Join(t.TempDir(), "pub_key.pem")
	pmWriteFile(t, pubPath, pubPEM)

	const pubKeyID = "PUB_KEY_ID_0112345678901234"
	const apiKey = "0123456789abcdef0123456789abcdef" // 32 字节
	pmSetSettings(t, map[string]string{
		"wxpay_pubkey_id":   pubKeyID,
		"wxpay_pubkey_path": pubPath,
		"wxpay_apiv3_key":   apiKey,
		"wxpay_mchid":       "1900000001",
		"wxpay_appid":       "wx1234567890",
	})
	p := &Wxpay{}

	buildNotify := func(t *testing.T, plain []byte, signPriv *rsa.PrivateKey, bodyMutator func([]byte) []byte) (map[string]string, []byte) {
		t.Helper()
		// 使用 12 字节 ASCII nonce,避免 JSON 序列化把非 UTF-8 字节替换成 U+FFFD 破坏 GCM nonce。
		nonce := []byte("123456789012")
		aad := "transaction"
		ct := pmAESGCMEncrypt(t, []byte(apiKey), nonce, []byte(aad), plain)
		body, err := json.Marshal(map[string]any{
			"resource": map[string]any{
				"ciphertext":      base64.StdEncoding.EncodeToString(ct),
				"nonce":           string(nonce),
				"associated_data": aad,
			},
		})
		if err != nil {
			t.Fatalf("构造回调体失败: %v", err)
		}
		ts := "1700000000"
		nonceStr := "nonce123"
		// 签名基于原始 body;bodyMutator 在签名后应用,用于模拟传输中篡改。
		msg := ts + "\n" + nonceStr + "\n" + string(body) + "\n"
		sig := signRSASHA256(signPriv, msg)
		if bodyMutator != nil {
			body = bodyMutator(body)
		}
		headers := map[string]string{
			"Wechatpay-Timestamp": ts,
			"Wechatpay-Nonce":     nonceStr,
			"Wechatpay-Signature": sig,
			"Wechatpay-Serial":    pubKeyID,
		}
		return headers, body
	}

	t.Run("成功", func(t *testing.T) {
		plain := []byte(`{"mchid":"1900000001","appid":"wx1234567890","out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		headers, body := buildNotify(t, plain, priv, nil)
		n, err := p.VerifyNotify(headers, body)
		if err != nil {
			t.Fatalf("VerifyNotify 失败: %v", err)
		}
		if n.OrderNo != "D123" || n.ChannelTradeNo != "T456" || n.AmountCents != 1234 || !n.Success {
			t.Errorf("通知解析结果不符: %+v", n)
		}
	})

	t.Run("商户归属不符", func(t *testing.T) {
		plain := []byte(`{"mchid":"1900000002","appid":"wx1234567890","out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		headers, body := buildNotify(t, plain, priv, nil)
		if _, err := p.VerifyNotify(headers, body); err == nil || !strings.Contains(err.Error(), "商户归属不符") {
			t.Errorf("mchid 不符应报「商户归属不符」, got %v", err)
		}
	})

	t.Run("appid不符", func(t *testing.T) {
		plain := []byte(`{"mchid":"1900000001","appid":"wx0000000000","out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		headers, body := buildNotify(t, plain, priv, nil)
		if _, err := p.VerifyNotify(headers, body); err == nil || !strings.Contains(err.Error(), "商户归属不符") {
			t.Errorf("appid 不符应报「商户归属不符」, got %v", err)
		}
	})

	t.Run("缺少Serial头", func(t *testing.T) {
		plain := []byte(`{"out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		headers, body := buildNotify(t, plain, priv, nil)
		delete(headers, "Wechatpay-Serial")
		if _, err := p.VerifyNotify(headers, body); err == nil || !strings.Contains(err.Error(), "Wechatpay-Serial") {
			t.Errorf("缺 Serial 头应报错, got %v", err)
		}
	})

	t.Run("验签失败", func(t *testing.T) {
		plain := []byte(`{"out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		// 构造正确签名,但提交前篡改 body。
		headers, body := buildNotify(t, plain, priv, func(b []byte) []byte {
			return []byte("tampered")
		})
		if _, err := p.VerifyNotify(headers, body); err == nil || !strings.Contains(err.Error(), "验签失败") {
			t.Errorf("篡改 body 后应报「验签失败」, got %v", err)
		}
	})

	t.Run("APIv3密钥长度错误", func(t *testing.T) {
		pmSetSettings(t, map[string]string{"wxpay_apiv3_key": "short"})
		plain := []byte(`{"out_trade_no":"D123","transaction_id":"T456","trade_state":"SUCCESS","amount":{"total":1234}}`)
		headers, body := buildNotify(t, plain, priv, nil)
		if _, err := p.VerifyNotify(headers, body); err == nil || !strings.Contains(err.Error(), "32 字节") {
			t.Errorf("密钥长度非 32 字节应报错, got %v", err)
		}
		pmSetSettings(t, map[string]string{"wxpay_apiv3_key": apiKey})
	})
}
