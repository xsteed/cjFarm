// 储值与手机号的 HTTP 层:顾客自助(需会员令牌)+ 管理端(需权限点)。
//
// 钱相关的接口全部只认顾客令牌解析出的会员 ID,请求体里的任何身份字段都不可信
// (与订单归属同一口径)。
package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/pay"
	"dining-system/infrastructure/weapp"
)

// ============================================================================
// 顾客自助
// ============================================================================

// WalletInfo 查询自己的储值余额。
func WalletInfo(c *gin.Context) {
	info, err := service.WalletDetail(customerIDFrom(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, info)
}

// WalletTxns 自己的储值流水(分页,金额以元下发)。
func WalletTxns(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	total, rows, err := service.ListWalletTxns(customerIDFrom(c), pageNum, pageSize)
	if err != nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	tableResult(c, total, rows)
}

// WalletRecharge 发起储值充值(微信 JSAPI):建充值单并返回调起参数。
// 回调走与订单同一个 notify_url,handler 靠单号前缀(R/D)分流。
func WalletRecharge(c *gin.Context) {
	var p struct {
		Amount float64 `json:"amount"` // 元,整数
		Code   string  `json:"code"`   // wx.login 的临时凭证(JSAPI 下单要 openid)
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.Amount <= 0 || strings.TrimSpace(p.Code) == "" {
		fail(c, "参数错误")
		return
	}
	provider, _ := pay.Get(pay.ChannelWxpay)
	if !provider.Enabled() {
		fail(c, "小程序支付尚未开通，请联系商家")
		return
	}
	ctx := c.Request.Context()
	openID, err := weapp.Code2Session(ctx, p.Code)
	if err != nil {
		if err == weapp.ErrMemberDisabled {
			fail(c, "小程序支付尚未开通，请联系商家")
			return
		}
		fail(c, err.Error())
		return
	}
	rechargeNo, cents, err := service.CreateWalletRecharge(customerIDFrom(c), p.Amount)
	if err != nil {
		fail(c, err.Error())
		return
	}
	params, err := provider.CreateJsapi(pay.PayReq{
		OrderNo:     rechargeNo,
		Description: "储值充值" + rechargeNo,
		AmountCents: cents,
		ClientIP:    c.ClientIP(),
		OpenID:      openID,
	})
	if err != nil {
		fail(c, "发起支付失败: "+err.Error())
		return
	}
	// rechargeNo 一并下发:前端支付完成后靠它轮询入账状态(回调偶尔比前端慢半拍)。
	ok(c, gin.H{
		"rechargeNo": rechargeNo,
		"timeStamp":  params.TimeStamp,
		"nonceStr":   params.NonceStr,
		"package":    params.Package,
		"signType":   params.SignType,
		"paySign":    params.PaySign,
	})
}

// WalletRechargeStatus 轮询充值是否已入账(回调丢失时前端兜底)。
func WalletRechargeStatus(c *gin.Context) {
	rechargeNo := strings.TrimSpace(c.Query("rechargeNo"))
	if rechargeNo == "" {
		fail(c, "参数错误")
		return
	}
	r, err := service.GetWalletRecharge(rechargeNo)
	if err != nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	// 不存在/不是自己的单:一律按「未入账」回,不泄露单号存在性。
	if r == nil || r.CustomerID != customerIDFrom(c) {
		ok(c, gin.H{"status": 0})
		return
	}
	ok(c, gin.H{"status": r.Status, "amount": po.ToYuan(r.Amount)})
}

// WalletPay 用储值余额支付订单。
func WalletPay(c *gin.Context) {
	var p struct {
		OrderNo string `json:"orderNo"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.OrderNo) == "" {
		fail(c, "参数错误")
		return
	}
	orderNo := strings.TrimSpace(p.OrderNo)
	firstTime, err := service.PayOrderByWallet(c.Request.Context(), customerIDFrom(c), orderNo)
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 先付模式:首次入账补出厨单(与渠道支付回调的 applyPaymentSuccess 同一拍)。
	if firstTime {
		if o, e := service.GetOrderAmount(orderNo); e == nil {
			printKitchenOnPaid(c.Request.Context(), o.OrderID)
		}
	}
	okMsg(c, "支付成功")
}

// BindPhone 手机号一键绑定/登录(getPhoneNumber 按钮)。
// 登录接口也接受 phoneCode(见 customer_auth.go):绑定失败不影响登录本身。
func BindPhone(c *gin.Context) {
	var p struct {
		Code string `json:"code"` // getPhoneNumber 回调的 code(与 wx.login 的 code 不是同一个东西)
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.Code) == "" {
		fail(c, "参数错误")
		return
	}
	masked, err := service.BindCustomerPhone(c.Request.Context(), customerIDFrom(c), p.Code)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"phone": masked})
}

// ============================================================================
// 管理端
// ============================================================================

// MemberWalletDetail 某会员的储值账户与流水。
func MemberWalletDetail(c *gin.Context) {
	customerID, _ := strconv.Atoi(c.Query("customerId"))
	if customerID <= 0 {
		fail(c, "参数错误")
		return
	}
	info, err := service.WalletDetail(customerID)
	if err != nil {
		fail(c, err.Error())
		return
	}
	pageNum, pageSize := pageParams(c)
	total, rows, err := service.ListWalletTxns(customerID, pageNum, pageSize)
	if err != nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, gin.H{"wallet": info, "total": total, "items": rows})
}

// MemberWalletAdjust 后台调账:正数赠送、负数扣减,必须填原因。
func MemberWalletAdjust(c *gin.Context) {
	var p struct {
		CustomerID int     `json:"customerId"`
		Amount     float64 `json:"amount"` // 元,带符号
		Reason     string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.CustomerID <= 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.AdminWalletAdjust(p.CustomerID, p.Amount, p.Reason, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "member", strconv.Itoa(p.CustomerID),
		"储值调账 "+strconv.FormatFloat(p.Amount, 'f', 2, 64)+"元，原因："+strings.TrimSpace(p.Reason))
	okMsg(c, "调账成功")
}
