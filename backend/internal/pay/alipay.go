// 支付宝(当面付-预下单)实现,基于 openapi + RSA2 签名,仅依赖 Go 标准库。
//
// 配置项(存于 tb_config):
//
//	alipay_enabled           是否启用
//	alipay_appid             应用 AppID
//	alipay_private_key_path  应用私钥 PEM 文件绝对路径(用于请求签名)
//	alipay_public_key        支付宝公钥(用于回调验签,PEM 或纯 base64)
//	alipay_notify_url        异步通知地址(公网 HTTPS)
package pay

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dining-system/infra/logger"
	"dining-system/internal/conf"
)

const aliGateway = "https://openapi.alipay.com/gateway.do"

// Alipay 支付宝当面付实现。
type Alipay struct{}

func (p *Alipay) Name() string { return ChannelAlipay }

func (p *Alipay) Enabled() bool {
	return cfgEnabled("alipay_enabled") &&
		cfg("alipay_appid") != "" &&
		cfg("alipay_private_key_path") != "" &&
		cfg("alipay_public_key") != ""
}

// Create 当面付预下单,返回 qr_code(前端渲染为二维码,顾客用支付宝扫码支付)。
func (p *Alipay) Create(req PayReq) (PayResult, error) {
	biz, _ := json.Marshal(map[string]string{
		"out_trade_no": req.OrderNo,
		"total_amount": centsToYuan(req.AmountCents),
		"subject":      truncateRunes(req.Description, 30),
	})
	params := map[string]string{
		"app_id":      cfg("alipay_appid"),
		"method":      "alipay.trade.precreate",
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format(conf.TimeLayout),
		"version":     "1.0",
		"notify_url":  cfg("alipay_notify_url"),
		"biz_content": string(biz),
	}
	resp, err := p.call(params)
	if err != nil {
		return PayResult{}, err
	}
	var out struct {
		Resp struct {
			Code   string `json:"code"`
			Msg    string `json:"msg"`
			QrCode string `json:"qr_code"`
		} `json:"alipay_trade_precreate_response"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return PayResult{}, err
	}
	if out.Resp.Code != "10000" || out.Resp.QrCode == "" {
		return PayResult{}, fmt.Errorf("支付宝下单失败: %s", out.Resp.Msg)
	}
	return PayResult{CodeURL: out.Resp.QrCode}, nil
}

// Query 查单。
func (p *Alipay) Query(orderNo string) (PayQuery, error) {
	biz, _ := json.Marshal(map[string]string{"out_trade_no": orderNo})
	params := p.base("alipay.trade.query", string(biz))
	resp, err := p.call(params)
	if err != nil {
		return PayQuery{}, err
	}
	var out struct {
		Resp struct {
			Code        string `json:"code"`
			TradeNo     string `json:"trade_no"`
			TradeStatus string `json:"trade_status"`
			TotalAmount string `json:"total_amount"`
		} `json:"alipay_trade_query_response"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return PayQuery{}, err
	}
	paid, err := yuanToCents(out.Resp.TotalAmount)
	if err != nil {
		return PayQuery{}, fmt.Errorf("支付宝查单金额解析失败: %v", err)
	}
	return PayQuery{
		ChannelTradeNo: out.Resp.TradeNo,
		Success:        out.Resp.TradeStatus == "TRADE_SUCCESS" || out.Resp.TradeStatus == "TRADE_FINISHED",
		PaidAmount:     paid,
	}, nil
}

// Close 关闭未支付的订单。
func (p *Alipay) Close(orderNo string) error {
	biz, _ := json.Marshal(map[string]string{"out_trade_no": orderNo})
	params := p.base("alipay.trade.close", string(biz))
	resp, err := p.call(params)
	if err != nil {
		return err
	}
	var out struct {
		Resp struct {
			Code string `json:"code"`
			Msg  string `json:"msg"`
		} `json:"alipay_trade_close_response"`
	}
	_ = json.Unmarshal(resp, &out)
	if out.Resp.Code != "10000" {
		return fmt.Errorf("支付宝关单失败: %s", out.Resp.Msg)
	}
	return nil
}

// Refund 退款。
func (p *Alipay) Refund(orderNo, refundNo string, refundCents, totalCents int64) (RefundResult, error) {
	biz, _ := json.Marshal(map[string]string{
		"out_trade_no":   orderNo,
		"out_request_no": refundNo,
		"refund_amount":  centsToYuan(refundCents),
	})
	params := p.base("alipay.trade.refund", string(biz))
	resp, err := p.call(params)
	if err != nil {
		return RefundResult{}, err
	}
	var out struct {
		Resp struct {
			Code      string `json:"code"`
			Msg       string `json:"msg"`
			TradeNo   string `json:"trade_no"`
			RefundFee string `json:"refund_fee"`
		} `json:"alipay_trade_refund_response"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return RefundResult{}, err
	}
	if out.Resp.Code != "10000" {
		return RefundResult{}, fmt.Errorf("支付宝退款失败: %s", out.Resp.Msg)
	}
	refunded, err := yuanToCents(out.Resp.RefundFee)
	if err != nil {
		return RefundResult{}, fmt.Errorf("支付宝退款金额解析失败: %v", err)
	}
	return RefundResult{RefundNo: out.Resp.TradeNo, Success: true, Refunded: refunded}, nil
}

// QueryRefund 查询退款状态(支付宝退款一般同步返回,此接口用于对账/兜底)。
func (p *Alipay) QueryRefund(orderNo, refundNo string) (RefundQuery, error) {
	biz, _ := json.Marshal(map[string]string{
		"out_trade_no":   orderNo,
		"out_request_no": refundNo,
	})
	params := p.base("alipay.trade.fastpay.refund.query", string(biz))
	resp, err := p.call(params)
	if err != nil {
		return RefundQuery{}, err
	}
	var out struct {
		Resp struct {
			Code         string `json:"code"`
			SubCode      string `json:"sub_code"`
			Msg          string `json:"msg"`
			TradeNo      string `json:"trade_no"`
			RefundAmount string `json:"refund_amount"`
		} `json:"alipay_trade_fastpay_refund_query_response"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return RefundQuery{}, err
	}
	q := RefundQuery{ChannelRefundNo: out.Resp.TradeNo}
	if out.Resp.Code == "10000" {
		q.Status = RefundSuccess
		// 只有退款成功时 refund_amount 才有意义;处理中/失败时支付宝不携带该字段,
		// 金额默认为 0,解析失败说明响应异常,须报错而非静默当 0 元。
		refunded, err := yuanToCents(out.Resp.RefundAmount)
		if err != nil {
			return RefundQuery{}, fmt.Errorf("支付宝退款查询金额解析失败: %v", err)
		}
		q.Refunded = refunded
	} else if out.Resp.Code == "40004" || out.Resp.SubCode == "ACQ.TRADE_NOT_EXIST" {
		// 退款记录尚未生成:视为处理中,稍后重试。
		q.Status = RefundProcessing
	} else {
		q.Status = RefundFail
	}
	return q, nil
}

// VerifyNotify 验签回调表单,返回标准化通知。
// 支付宝回调为 application/x-www-form-urlencoded,本方法从 body 解析参数后验签。
func (p *Alipay) VerifyNotify(_ map[string]string, body []byte) (PayNotify, error) {
	var n PayNotify
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return n, fmt.Errorf("支付宝回调参数解析失败: %v", err)
	}
	sign := form.Get("sign")
	if sign == "" {
		return n, fmt.Errorf("支付宝回调缺少签名")
	}
	// 除 sign / sign_type 外的参数参与验签
	keys := make([]string, 0, len(form))
	for k := range form {
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
		sb.WriteString(form.Get(k))
	}
	pub, err := p.publicKey()
	if err != nil {
		return n, err
	}
	if !verifyRSASignature(pub, sb.String(), sign) {
		return n, fmt.Errorf("支付宝回调验签失败")
	}

	// 验签只能证明「消息来自支付宝」,不能证明「属于本商户应用」:在平台级公钥/共享证书
	// 部署下,验签公钥可能被多个应用共享,必须再比对回调里的 app_id 与本应用配置一致,
	// 否则其它应用的订单会串到本系统入账。支付宝当面付回调不携带 seller_id,且本系统
	// 也未配置商户号相关键(仅 alipay_appid 标识应用),因此归属校验只比对 app_id。
	if appID := form.Get("app_id"); appID != cfg("alipay_appid") {
		logger.Warnf("[pay] 支付宝回调商户归属不符 out_trade_no=%s app_id=%s", form.Get("out_trade_no"), appID)
		return n, fmt.Errorf("支付宝回调商户归属不符")
	}

	// 金额解析失败必须拒绝:yuanToCents 解析失败若静默返回 0,0 元可能被误判为
	// 「金额一致」或「免费单」而错误入账,这里显式拦截。
	amountCents, err := yuanToCents(form.Get("total_amount"))
	if err != nil {
		logger.Warnf("[pay] 支付宝回调金额解析失败 out_trade_no=%s total_amount=%s: %v", form.Get("out_trade_no"), form.Get("total_amount"), err)
		return n, fmt.Errorf("支付宝回调金额解析失败: %v", err)
	}

	status := form.Get("trade_status")
	n.OrderNo = form.Get("out_trade_no")
	n.ChannelTradeNo = form.Get("trade_no")
	n.AmountCents = amountCents
	n.Success = status == "TRADE_SUCCESS" || status == "TRADE_FINISHED"
	return n, nil
}

func (p *Alipay) NotifySuccessBody() string { return "success" }

// ---- 内部工具 ----

// base 构造通用请求参数(不含 biz_content 已合并)。
func (p *Alipay) base(method, bizContent string) map[string]string {
	return map[string]string{
		"app_id":      cfg("alipay_appid"),
		"method":      method,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format(conf.TimeLayout),
		"version":     "1.0",
		"notify_url":  cfg("alipay_notify_url"),
		"biz_content": bizContent,
	}
}

// call 对参数排序、签名后 POST 到支付宝网关,返回响应体(JSON)。
func (p *Alipay) call(params map[string]string) ([]byte, error) {
	priv, err := loadRSAPrivateKey(cfg("alipay_private_key_path"))
	if err != nil {
		return nil, err
	}
	// 排序拼接
	keys := make([]string, 0, len(params))
	for k := range params {
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
	sig := signRSASHA256(priv, sb.String())
	params["sign"] = sig

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(aliGateway, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("支付宝接口返回 %d: %s", resp.StatusCode, truncateRunes(string(data), 200))
	}
	return data, nil
}

// publicKey 读取支付宝公钥(PEM 或纯 base64)。
func (p *Alipay) publicKey() (*rsa.PublicKey, error) {
	raw := cfg("alipay_public_key")
	if raw == "" {
		return nil, fmt.Errorf("支付宝公钥未配置")
	}
	pemStr := raw
	if !strings.Contains(raw, "BEGIN") {
		pemStr = "-----BEGIN PUBLIC KEY-----\n" + raw + "\n-----END PUBLIC KEY-----"
	}
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("支付宝公钥 PEM 解析失败")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("支付宝公钥类型异常")
	}
	return rsaPub, nil
}

// ---- 金额转换 ----

// centsToYuan 分 -> 元字符串(两位小数),支付宝金额单位为元。
func centsToYuan(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100.0, 'f', 2, 64)
}

// yuanToCents 元字符串 -> 分。解析失败返回 error,而不是静默返回 0:
// 0 元在金额比较中可能被误判为「金额一致」或「免费单」,导致错误的支付成功入账。
func yuanToCents(yuan string) (int64, error) {
	f, err := strconv.ParseFloat(yuan, 64)
	if err != nil {
		return 0, fmt.Errorf("金额解析失败(%q): %v", yuan, err)
	}
	return int64(f*100 + 0.5), nil
}
