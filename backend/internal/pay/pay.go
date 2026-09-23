// Package pay 提供在线支付抽象层:微信支付(Native)与支付宝(当面付预下单)。
//
// 设计目标:
//   - 业务层只依赖 Provider 接口,渠道可插拔;
//   - 商户 key 未配置时 Enabled() 返回 false,上层据此降级到「码牌收款」,
//     待商户申请好 key 并填入配置后自动启用在线支付,无需改代码;
//   - 金额统一以「分」(int64) 在边界传递,由各渠道自行转换为微信(分)/支付宝(元)。
//
// 依赖方向:pay 依赖 service 读取配置,不依赖 handler;service 不依赖 pay,避免循环依赖。
package pay

import (
	"fmt"

	"dining-system/internal/service"
)

// 渠道常量,与 tb_payment.channel 字段取值一致。
const (
	ChannelWxpay  = "wxpay"
	ChannelAlipay = "alipay"
)

// PayReq 发起支付请求(业务层构造)。
type PayReq struct {
	OrderNo     string // 系统订单号(out_trade_no)
	Description string // 商品描述
	AmountCents int64  // 支付金额(分)
	ClientIP    string // 客户端 IP(部分渠道需要)
}

// PayResult 发起支付结果。
type PayResult struct {
	CodeURL string // 可直接渲染为二维码的字符串(微信 code_url / 支付宝 qr_code)
}

// PayQuery 主动查单结果。
type PayQuery struct {
	ChannelTradeNo string // 渠道交易号
	Success        bool   // 是否已支付成功
	PaidAmount     int64  // 实付金额(分)
}

// PayNotify 解析并验签后的回调通知(业务层据此回写订单)。
type PayNotify struct {
	OrderNo        string
	ChannelTradeNo string
	AmountCents    int64 // 实付金额(分)
	Success        bool
}

// RefundResult 退款结果。
type RefundResult struct {
	RefundNo string // 退款单号(渠道返回)
	Success  bool   // 是否受理成功(退款到账通常异步)
	Refunded int64  // 退款金额(分)
}

// 退款单状态(渠道侧)。
const (
	RefundSuccess    = "success"    // 已退到帐
	RefundProcessing = "processing" // 退款中
	RefundFail       = "fail"       // 退款失败/关闭
)

// RefundQuery 主动查询退款结果。
type RefundQuery struct {
	Status          string // success / processing / fail
	ChannelRefundNo string // 渠道退款单号
	Refunded        int64  // 实退金额(分)
}

// Provider 支付渠道统一接口。
type Provider interface {
	Name() string                                                                         // 渠道名(wxpay / alipay)
	Enabled() bool                                                                        // 商户配置是否就绪
	Create(req PayReq) (PayResult, error)                                                 // 发起支付
	Query(orderNo string) (PayQuery, error)                                               // 查单
	Close(orderNo string) error                                                           // 关闭支付单
	Refund(orderNo, refundNo string, refundCents, totalCents int64) (RefundResult, error) // 退款
	QueryRefund(orderNo, refundNo string) (RefundQuery, error)                            // 退款查询
	VerifyNotify(headers map[string]string, body []byte) (PayNotify, error)               // 回调验签+解析
	NotifySuccessBody() string                                                            // 应答渠道「已收到」的响应体
}

// Get 根据渠道名返回对应的 Provider。
func Get(channel string) (Provider, error) {
	switch channel {
	case ChannelWxpay:
		return &Wxpay{}, nil
	case ChannelAlipay:
		return &Alipay{}, nil
	default:
		return nil, fmt.Errorf("不支持的支付渠道: %s", channel)
	}
}

// cfg 便捷读取配置项(空值返回空串)。
func cfg(key string) string {
	return service.GetSetting(key)
}

// cfgEnabled 判断开关类配置("1" 为开启)。
func cfgEnabled(key string) bool {
	return cfg(key) == "1"
}
