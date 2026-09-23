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
	"strings"
	"time"

	"dining-system/infra/logger"
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
	return RefundResult{
		RefundNo: out.RefundID,
		Success:  out.Status == "SUCCESS" || out.Status == "PROCESSING",
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
	switch out.Status {
	case "SUCCESS":
		q.Status = RefundSuccess
	case "PROCESSING", "CHANGE":
		q.Status = RefundProcessing
	default:
		q.Status = RefundFail
	}
	return q, nil
}

// VerifyNotify 验签 + 解密回调,返回标准化的通知结构。
func (p *Wxpay) VerifyNotify(headers map[string]string, body []byte) (PayNotify, error) {
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

	// 2) 解密 resource(AES-256-GCM)
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

	// 3) 解析支付结果。微信 APIv3 回调解密后的明文带有 mchid/appid,
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
		logger.Warnf("[pay] 微信回调商户归属不符 out_trade_no=%s mchid=%s", ev.OutTradeNo, ev.Mchid)
		return n, fmt.Errorf("微信回调商户归属不符")
	}
	// appid 为可选强校验:历史部署可能未配置 wxpay_appid,仅在配置了的情况下校验,
	// 避免旧配置直接升级后回调全部失败;只要配置了就必须与回调一致。
	if wantAppid := cfg("wxpay_appid"); wantAppid != "" && ev.Appid != wantAppid {
		logger.Warnf("[pay] 微信回调商户归属不符 out_trade_no=%s appid=%s", ev.OutTradeNo, ev.Appid)
		return n, fmt.Errorf("微信回调商户归属不符")
	}
	n.OrderNo = ev.OutTradeNo
	n.ChannelTradeNo = ev.TransactionID
	n.AmountCents = ev.Amount.Total
	n.Success = ev.TradeState == "SUCCESS"
	return n, nil
}

func (p *Wxpay) NotifySuccessBody() string { return `{"code":"SUCCESS","message":"成功"}` }

// ---- 内部工具 ----

// do 发送带 WECHATPAY2-SHA256-RSA2048 签名的请求并返回响应体。
func (p *Wxpay) do(method, path string, body []byte) ([]byte, error) {
	priv, err := loadRSAPrivateKey(cfg("wxpay_private_key_path"))
	if err != nil {
		return nil, err
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randNonce()
	msg := method + "\n" + path + "\n" + ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig := signRSASHA256(priv, msg)
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

func signRSASHA256(priv *rsa.PrivateKey, msg string) string {
	hash := sha256.Sum256([]byte(msg))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hash[:])
	return base64.StdEncoding.EncodeToString(sig)
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

func randNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
