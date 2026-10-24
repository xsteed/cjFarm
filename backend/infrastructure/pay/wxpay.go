// 微信支付(Native)实现,基于 API v3,仅依赖 Go 标准库。
//
// 配置项(存于 tb_config):
//
//	wxpay_mchid              商户号
//	wxpay_appid              AppID(公众号/小程序)
//	wxpay_apiv3_key          APIv3 密钥(32 字节,用于回调解密;加密存储)
//	wxpay_serial_no          商户 API 证书序列号
//	wxpay_private_key_path   商户私钥 PEM 文件绝对路径(用于请求签名)
//	wxpay_pubkey_id          微信支付公钥ID(官方推荐模式,配此项即走公钥验签)
//	wxpay_pubkey_path        微信支付公钥 PEM 文件绝对路径
//	wxpay_platform_cert_path 微信支付平台证书 PEM 文件或目录绝对路径(用于回调验签)
//	wxpay_notify_url         支付结果异步通知地址(公网 HTTPS)
package pay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dining-system/infrastructure/logger"
)

const wxBase = "https://api.mch.weixin.qq.com"

// Wxpay 微信支付 Native 实现。
type Wxpay struct{}

func (p *Wxpay) Name() string { return ChannelWxpay }

// Enabled 判定商户配置是否就绪(商户号、密钥、私钥、序列号齐备,且已配置回调验签材料)。
//
// 要求验签材料齐备的原因:若缺少验签公钥/证书,下单可以成功但回调必然失败,
// 会造成「顾客已付款、订单仍显示未支付」的掉单事故,不如提前判定为未开通。
func (p *Wxpay) Enabled() bool {
	return cfgEnabled("wxpay_enabled") &&
		cfg("wxpay_mchid") != "" &&
		cfg("wxpay_apiv3_key") != "" &&
		cfg("wxpay_serial_no") != "" &&
		cfg("wxpay_private_key_path") != "" &&
		p.verifyMaterialReady()
}

// verifyMaterialReady 判定是否已配置回调验签材料:
// 微信支付公钥模式(公钥ID + 公钥文件)或平台证书模式(证书文件/目录),任一即可。
func (p *Wxpay) verifyMaterialReady() bool {
	if cfg("wxpay_pubkey_id") != "" && cfg("wxpay_pubkey_path") != "" {
		return true
	}
	return cfg("wxpay_platform_cert_path") != ""
}

// Create 发起 Native 下单,返回 code_url(前端渲染为二维码)。
func (p *Wxpay) Create(req PayReq) (PayResult, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"appid":        cfg("wxpay_appid"),
		"mchid":        cfg("wxpay_mchid"),
		"description":  truncateRunes(req.Description, 40),
		"out_trade_no": req.OrderNo,
		"notify_url":   cfg("wxpay_notify_url"),
		"amount":       map[string]interface{}{"total": req.AmountCents, "currency": "CNY"},
	})
	resp, err := p.do("POST", "/v3/pay/transactions/native", body)
	if err != nil {
		return PayResult{}, err
	}
	var out struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.CodeURL == "" {
		return PayResult{}, fmt.Errorf("微信下单失败: %s", string(resp))
	}
	return PayResult{CodeURL: out.CodeURL}, nil
}

// CreateJsapi 小程序内支付下单:POST /v3/pay/transactions/jsapi,拿 prepay_id 后签好调起参数。
//
// 与 Native 的关键差异:
//   - 请求体多一个 payer.openid(小程序里不能「自己扫自己」,必须知道是哪个微信号在付);
//   - 返回的不是 code_url,而是 wx.requestPayment 的调起参数。
//
// openid 与 appid 必须同主体:这里用的 appid 是 wxpay_appid,所以该项必须是**小程序**
// 的 AppID(与桌牌码出码共用同一项)。填成公众号 AppID 时 Native 仍能跑、JSAPI 必失败。
func (p *Wxpay) CreateJsapi(req PayReq) (JsapiParams, error) {
	if strings.TrimSpace(req.OpenID) == "" {
		return JsapiParams{}, fmt.Errorf("缺少 openid，无法发起小程序内支付")
	}
	appID := cfg("wxpay_appid")
	body, _ := json.Marshal(map[string]interface{}{
		"appid":        appID,
		"mchid":        cfg("wxpay_mchid"),
		"description":  truncateRunes(req.Description, 40),
		"out_trade_no": req.OrderNo,
		"notify_url":   cfg("wxpay_notify_url"),
		"amount":       map[string]interface{}{"total": req.AmountCents, "currency": "CNY"},
		"payer":        map[string]string{"openid": req.OpenID},
	})
	resp, err := p.do("POST", "/v3/pay/transactions/jsapi", body)
	if err != nil {
		return JsapiParams{}, err
	}
	var out struct {
		PrepayID string `json:"prepay_id"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.PrepayID == "" {
		return JsapiParams{}, fmt.Errorf("微信小程序下单失败: %s", truncateRunes(string(resp), 200))
	}
	return p.paySign(appID, out.PrepayID)
}

// paySign 生成 wx.requestPayment 的调起参数。
//
// 签名串固定为 `appId\ntimeStamp\nnonceStr\npackage\n`(package 即 prepay_id=xxx),
// 用商户 API 私钥做 RSA-SHA256 —— 与请求头的 WECHATPAY2-SHA256-RSA2048 签名是同一把
// 私钥、不同签名串,不要混用(混用的现象是收银台报「签名错误」)。
func (p *Wxpay) paySign(appID, prepayID string) (JsapiParams, error) {
	priv, err := loadRSAPrivateKey(cfg("wxpay_private_key_path"))
	if err != nil {
		return JsapiParams{}, err
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce, err := randNonce()
	if err != nil {
		return JsapiParams{}, err
	}
	pkg := "prepay_id=" + prepayID
	sig, err := signRSASHA256(priv, appID+"\n"+ts+"\n"+nonce+"\n"+pkg+"\n")
	if err != nil {
		return JsapiParams{}, err
	}
	return JsapiParams{
		TimeStamp: ts,
		NonceStr:  nonce,
		Package:   pkg,
		SignType:  "RSA",
		PaySign:   sig,
	}, nil
}

// Query 按商户订单号查单。
func (p *Wxpay) Query(orderNo string) (PayQuery, error) {
	resp, err := p.do("GET", "/v3/pay/transactions/out-trade-no/"+orderNo+"?mchid="+cfg("wxpay_mchid"), nil)
	if err != nil {
		return PayQuery{}, err
	}
	var out struct {
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		Amount        struct {
			Total int64 `json:"total"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return PayQuery{}, err
	}
	return PayQuery{
		ChannelTradeNo: out.TransactionID,
		Success:        out.TradeState == "SUCCESS",
		PaidAmount:     out.Amount.Total,
	}, nil
}

// Close 关闭未支付的订单(取消/改单前调用)。
func (p *Wxpay) Close(orderNo string) error {
	body, _ := json.Marshal(map[string]string{"mchid": cfg("wxpay_mchid")})
	resp, err := p.do("POST", "/v3/pay/transactions/out-trade-no/"+orderNo+"/close", body)
	if err != nil {
		return err
	}
	if len(resp) > 0 && !bytes.Equal(resp, []byte("{}")) && !strings.Contains(string(resp), "SUCCESS") {
		return fmt.Errorf("微信关单失败: %s", string(resp))
	}
	return nil
}

// Refund 申请退款。
func (p *Wxpay) Refund(orderNo, refundNo string, refundCents, totalCents int64) (RefundResult, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"out_trade_no":  orderNo,
		"out_refund_no": refundNo,
		"amount": map[string]interface{}{
			"refund":   refundCents,
			"total":    totalCents,
			"currency": "CNY",
		},
	})
	resp, err := p.do("POST", "/v3/refund/domestic/refunds", body)
	if err != nil {
		return RefundResult{}, err
	}
	var out struct {
		RefundID    string `json:"refund_id"`
		OutRefundNo string `json:"out_refund_no"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return RefundResult{}, err
	}
	// status 取值见微信文档:SUCCESS 成功 / PROCESSING 处理中 / CLOSED 关闭 / ABNORMAL 异常。
	// CLOSED/ABNORMAL 是明确失败(钱没退),必须与「处理中」区分开:调用方据此把退款单
	// 置为失败,否则这笔退款单会一直停在处理中、永久占用可退额度,后续再也退不出去。
	// 无法识别的状态(字段缺失/渠道新增状态码)fail-safe 到「处理中」:继续占用额度、
	// 等退款查单兜底。默认成「明确失败」会释放可退额度,若渠道实际已受理/已退出这笔钱,
	// 就打开了超退的口子 —— 宁可多一次人工核对,不可放过可能的资金风险。
	status := RefundProcessing
	switch out.Status {
	case "SUCCESS":
		status = RefundSuccess
	case "PROCESSING":
		status = RefundProcessing
	case "CLOSED", "ABNORMAL":
		status = RefundFail
	}
	return RefundResult{
		RefundNo: out.RefundID,
		Success:  status != RefundFail,
		Status:   status,
		Refunded: refundCents,
	}, nil
}

// QueryRefund 按商户退款单号查询退款状态。
// 微信退款状态: SUCCESS 成功 / CLOSED 关闭(失败) / PROCESSING 处理中 / CHANGE 异常(需人工)。
func (p *Wxpay) QueryRefund(orderNo, refundNo string) (RefundQuery, error) {
	resp, err := p.do("GET", "/v3/refund/domestic/refunds/out-refund-no/"+refundNo+"?mchid="+cfg("wxpay_mchid"), nil)
	if err != nil {
		return RefundQuery{}, err
	}
	var out struct {
		RefundID string `json:"refund_id"`
		Status   string `json:"status"`
		Amount   struct {
			Refund int64 `json:"refund"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return RefundQuery{}, err
	}
	q := RefundQuery{ChannelRefundNo: out.RefundID, Refunded: out.Amount.Refund}
	// 与 Refund 同一口径:CLOSED 是明确失败,无法识别的状态 fail-safe 到「处理中」,
	// 等下次查单兜底,不把可能的已退款置成失败而释放可退额度。
	switch out.Status {
	case "SUCCESS":
		q.Status = RefundSuccess
	case "PROCESSING", "CHANGE":
		q.Status = RefundProcessing
	case "CLOSED", "ABNORMAL":
		q.Status = RefundFail
	default:
		q.Status = RefundProcessing
	}
	return q, nil
}

// VerifyNotify 验签 + 解密回调,返回标准化的通知结构。
func (p *Wxpay) VerifyNotify(ctx context.Context, headers map[string]string, body []byte) (PayNotify, error) {
	var n PayNotify
	ts := headers["Wechatpay-Timestamp"]
	nonce := headers["Wechatpay-Nonce"]
	sig := headers["Wechatpay-Signature"]
	serial := headers["Wechatpay-Serial"]

	// 1) 验签:按 Serial 自动路由「微信支付公钥模式」或「平台证书模式」
	pub, err := p.verifyPubKey(serial)
	if err != nil {
		return n, err
	}
	message := ts + "\n" + nonce + "\n" + string(body) + "\n"
	if !verifyRSASignature(pub, message, sig) {
		return n, fmt.Errorf("微信回调验签失败")
	}
	// 2) 时间戳新鲜度:验签只能证明「这条通知确实由微信签发」,不能证明「它是刚发的」。
	//    一条被录下来重放的旧通知签名依然有效,必须在验签后按微信建议的 5 分钟窗口拒绝。
	//    放在验签之后:先确认来源可信,再对可信输入做时效判断。
	if err := verifyNotifyTimestamp(ts, time.Now()); err != nil {
		return n, err
	}

	// 3) 解密 resource(AES-256-GCM)
	var envelope struct {
		Resource struct {
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return n, err
	}
	plain, err := decryptAESGCM(cfg("wxpay_apiv3_key"), envelope.Resource.Nonce, envelope.Resource.AssociatedData, envelope.Resource.Ciphertext)
	if err != nil {
		return n, err
	}

	// 4) 解析支付结果。微信 APIv3 回调解密后的明文带有 mchid/appid,
	// 必须校验它们与本商户配置一致 —— 验签只能证明消息来自微信,不能证明属于本商户,
	// 平台级部署(共享平台证书/微信支付公钥)下串单会直接错账。
	var ev struct {
		Mchid         string `json:"mchid"`
		Appid         string `json:"appid"`
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		Amount        struct {
			Total int64 `json:"total"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(plain, &ev); err != nil {
		return n, err
	}
	if ev.Mchid != cfg("wxpay_mchid") {
		logger.CtxWarnf(ctx, "[pay] 微信回调商户归属不符 out_trade_no=%s mchid=%s", ev.OutTradeNo, ev.Mchid)
		return n, fmt.Errorf("微信回调商户归属不符")
	}
	// appid 为可选强校验:历史部署可能未配置 wxpay_appid,仅在配置了的情况下校验,
	// 避免旧配置直接升级后回调全部失败;只要配置了就必须与回调一致。
	if wantAppid := cfg("wxpay_appid"); wantAppid != "" && ev.Appid != wantAppid {
		logger.CtxWarnf(ctx, "[pay] 微信回调商户归属不符 out_trade_no=%s appid=%s", ev.OutTradeNo, ev.Appid)
		return n, fmt.Errorf("微信回调商户归属不符")
	}
	n.OrderNo = ev.OutTradeNo
	n.ChannelTradeNo = ev.TransactionID
	n.AmountCents = ev.Amount.Total
	n.Success = ev.TradeState == "SUCCESS"
	return n, nil
}

func (p *Wxpay) NotifySuccessBody() string { return `{"code":"SUCCESS","message":"成功"}` }

// notifyMaxSkew 是微信回调时间戳允许的最大偏差。
//
// 取微信官方建议的 5 分钟:窗口内足以容纳正常网络重传与轻度时钟漂移,
// 又短到让被截获的旧通知在重放时有意义地失效。生产机需保持 NTP 同步,
// 否则偏差超过该窗口会导致回调全部被拒(渠道会持续重试,时钟纠正后自动恢复)。
const notifyMaxSkew = 5 * time.Minute

// verifyNotifyTimestamp 校验 Wechatpay-Timestamp 是否为合法 Unix 秒,且与 now 的偏差不超过 notifyMaxSkew。
//
// 单独抽成纯函数(时间由调用方注入)是为了能用固定时刻写测试,不依赖真实时钟。
func verifyNotifyTimestamp(raw string, now time.Time) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fmt.Errorf("微信回调缺少 Wechatpay-Timestamp 头")
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("微信回调时间戳非法: %q", s)
	}
	skew := now.Sub(time.Unix(sec, 0))
	if skew > notifyMaxSkew || skew < -notifyMaxSkew {
		return fmt.Errorf("微信回调时间戳超出允许窗口(偏差 %s,上限 %s)", skew.Round(time.Second), notifyMaxSkew)
	}
	return nil
}

// ---- 内部工具 ----

// do 发送带 WECHATPAY2-SHA256-RSA2048 签名的请求并返回响应体。
func (p *Wxpay) do(method, path string, body []byte) ([]byte, error) {
	priv, err := loadRSAPrivateKey(cfg("wxpay_private_key_path"))
	if err != nil {
		return nil, err
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce, err := randNonce()
	if err != nil {
		return nil, err
	}
	msg := method + "\n" + path + "\n" + ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig, err := signRSASHA256(priv, msg)
	if err != nil {
		return nil, err
	}
	auth := fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		cfg("wxpay_mchid"), nonce, sig, ts, cfg("wxpay_serial_no"))

	req, err := http.NewRequest(method, wxBase+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return data, fmt.Errorf("微信接口 %s 返回 %d: %s", path, resp.StatusCode, truncateRunes(string(data), 200))
	}
	return data, nil
}

// verifyPubKey 按回调头 Wechatpay-Serial 选择验签公钥。
//
// 两种模式自动路由,且可在微信换证灰度期内并存(此时回调会同时出现两个序列号):
//  1. 微信支付公钥模式(官方推荐,无物理过期):Serial 等于配置的公钥ID时,用 wxpay_pubkey_path 指向的公钥;
//  2. 平台证书模式:Serial 为平台证书序列号时,从 wxpay_platform_cert_path(文件或目录)按序列号匹配。
func (p *Wxpay) verifyPubKey(serial string) (*rsa.PublicKey, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return nil, fmt.Errorf("微信回调缺少 Wechatpay-Serial 头")
	}
	if id := strings.TrimSpace(cfg("wxpay_pubkey_id")); id != "" && strings.EqualFold(id, serial) {
		return loadPublicKeyFromPEM(cfg("wxpay_pubkey_path"), "微信支付公钥")
	}
	return p.platformCertPubKey(serial)
}

// platformCertPubKey 从平台证书(单个 .pem 文件或目录)中按序列号取验签公钥。
//
// 支持目录是为了应对「微信平台证书 5 年轮换」:换证前 24 小时新证书即会下发,
// 灰度期内微信会同时用新旧两套序列号签名,因此必须支持多张证书并存。
func (p *Wxpay) platformCertPubKey(serial string) (*rsa.PublicKey, error) {
	path := strings.TrimSpace(cfg("wxpay_platform_cert_path"))
	if path == "" {
		return nil, fmt.Errorf("微信平台证书未配置(请填写平台证书 .pem 文件或目录路径,或改用微信支付公钥模式)")
	}
	files, err := certFiles(path)
	if err != nil {
		return nil, err
	}
	loaded := 0
	for _, f := range files {
		cert, err := parseCertFile(f)
		if err != nil {
			continue
		}
		loaded++
		if !certSerialEqual(cert.SerialNumber, serial) {
			continue
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("平台证书 %s 公钥类型异常", filepath.Base(f))
		}
		return pub, nil
	}
	if loaded == 0 {
		return nil, fmt.Errorf("未在 %s 找到可解析的微信平台证书(.pem)", path)
	}
	return nil, fmt.Errorf("未找到序列号 %s 对应的平台证书(已加载 %d 张,证书可能已轮换,请更新证书文件)",
		serial, loaded)
}

// certFiles 返回待加载的证书文件列表:路径为目录时取目录下全部 .pem 文件。
func certFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("平台证书路径不可读(%s): %v", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("读取平台证书目录失败(%s): %v", path, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".pem") {
			continue
		}
		files = append(files, filepath.Join(path, e.Name()))
	}
	return files, nil
}

// parseCertFile 读取并解析 PEM 证书文件。
func parseCertFile(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("PEM 解析失败")
	}
	return x509.ParseCertificate(block.Bytes)
}

// certSerialEqual 比较证书序列号与微信回调头中的序列号。
//
// 注意:微信用 Wechatpay-Serial 传的是「大写十六进制的序列号」,而 x509 解析出的
// 是 big.Int。必须统一进制后比较 —— 直接拿 big.Int 的十进制字符串去比会恒不相等,
// 导致回调验签永远失败。
func certSerialEqual(s *big.Int, want string) bool {
	want = strings.TrimSpace(want)
	if s == nil || want == "" {
		return false
	}
	return strings.EqualFold(s.Text(16), want)
}

// loadPublicKeyFromPEM 读取 PEM 公钥,兼容「X.509 证书」与「SubjectPublicKeyInfo 公钥」两种格式。
// 微信支付公钥文件(pub_key.pem)属于后者,平台证书属于前者。
func loadPublicKeyFromPEM(path, label string) (*rsa.PublicKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s 未配置文件路径", label)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s 不可读(%s): %v", label, path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s PEM 解析失败(%s)", label, path)
	}
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pub, nil
		}
		return nil, fmt.Errorf("%s 公钥类型异常", label)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s 解析失败: %v", label, err)
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s 公钥类型异常", label)
	}
	return pub, nil
}

// ---- 通用密码学工具 ----

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("商户私钥文件不可读(%s): %v", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("商户私钥 PEM 解析失败")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if priv, ok := key.(*rsa.PrivateKey); ok {
			return priv, nil
		}
	}
	// 兼容 PKCS1 格式
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// signRSASHA256 用商户私钥对消息做 RSA-SHA256 签名,返回 base64 签名串。
//
// 错误必须上抛:此前这里丢弃了 rsa.SignPKCS1v15 的 error,签名失败会返回空串,
// 表现为渠道侧「签名错误」而本地毫无线索 —— 而失败原因(私钥不完整、随机源不可用)
// 恰恰只在本地可见。空签名继续往下走只会把问题推到更难排查的地方。
func signRSASHA256(priv *rsa.PrivateKey, msg string) (string, error) {
	hash := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("RSA-SHA256 签名失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func verifyRSASignature(pub *rsa.PublicKey, msg, sigB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(msg))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sig) == nil
}

func decryptAESGCM(keyHex, nonce, aad, ciphertextB64 string) ([]byte, error) {
	key := []byte(keyHex)
	if len(key) != 32 {
		return nil, fmt.Errorf("APIv3 密钥长度应为 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(nonce), ct, []byte(aad))
}

// randNonce 生成 32 位十六进制随机 nonce。
//
// crypto/rand 失败时返回 error 而不是拿全零 nonce 继续签名:nonce 可预测会
// 削弱签名防重放的意义,而且「偶发签名失败」比显式失败更难排查
// (与 service.randBase32 对单号的处理口径一致)。
func randNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("读取随机源失败: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
