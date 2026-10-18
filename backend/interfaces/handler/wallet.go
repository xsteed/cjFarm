// 储值与手机号的 HTTP 层:顾客自助(需会员令牌)+ 管理端(需权限点)。
//
// 钱相关的接口全部只认顾客令牌解析出的会员 ID,请求体里的任何身份字段都不可信
// (与订单归属同一口径)。
package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
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

// ============================================================================
// 充值对账:回调丢失时的自救通道
// ============================================================================

// WalletRechargeReconcile 向渠道主动查单,补回「顾客已付款但回调没到」的充值。
//
// 为什么必须存在:订单支付有「顾客反复查单」与「管理端查单」两条自救路径,
// 充值单原本只有回调一条 —— 回调一丢(服务重启、网络抖动、渠道重发失败),
// 顾客微信扣款成功、余额永远不到账,且无人知晓。这是纯资损。
//
// 触发时机由前端掌握(进钱包页 / 支付后轮询无果),后端只做「查 + 入账」:
// 入账本身是幂等的(条件更新 0→1),重复调用不会重复加钱。
func WalletRechargeReconcile(c *gin.Context) {
	customerID := customerIDFrom(c)
	provider, err := pay.Get(pay.ChannelWxpay)
	if err != nil || !provider.Enabled() {
		fail(c, "小程序支付尚未开通，请联系商家")
		return
	}
	ctx := c.Request.Context()
	pending, err := service.ListPendingRecharges(customerID, 0)
	if err != nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	credited := 0
	for _, r := range pending {
		q, qErr := provider.Query(r.RechargeNo)
		if qErr != nil {
			logger.CtxWarnf(ctx, "[wallet] 充值补账查单失败 单%s: %v", r.RechargeNo, qErr)
			continue
		}
		if !q.Success {
			continue
		}
		// 金额以渠道实付为准;q.PaidAmount 为 0 的渠道回退到单上金额。
		amount := q.PaidAmount
		if amount <= 0 {
			amount = r.Amount
		}
		if err := service.WalletRechargeCredit(r.RechargeNo, pay.ChannelWxpay, q.ChannelTradeNo, amount); err != nil {
			logger.CtxWarnf(ctx, "[wallet] 充值补账入账失败 单%s: %v", r.RechargeNo, err)
			continue
		}
		credited++
		logger.CtxInfof(ctx, "[wallet] 充值补账成功 单%s 金额%d分 会员%d", r.RechargeNo, amount, r.CustomerID)
	}
	ok(c, gin.H{"credited": credited})
}

// ============================================================================
// 《储值规则》同意
// ============================================================================

// WalletAgreeRules 记录会员同意当前版《储值规则》(合规:发卡须明示章程并留痕)。
func WalletAgreeRules(c *gin.Context) {
	var p struct {
		RulesVer string `json:"rulesVer"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if err := service.AgreeWalletRules(customerIDFrom(c), strings.TrimSpace(p.RulesVer)); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "已同意")
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

// MemberWalletWithdraw 退余额:把会员储值余额清零,门店据此线下退款(现金/微信)。
//
// 合规落点(见 docs/compliance/prepaid-card.md):顾客的预付款必须拿得回去。
// 没有这个通道,会员一旦不想来了钱就永久锁在店里 —— 法律上站不住。
// 响应回传实际退出的本金金额,供门店据此付款并对账。
func MemberWalletWithdraw(c *gin.Context) {
	var p struct {
		CustomerID int    `json:"customerId"`
		Reason     string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.CustomerID <= 0 {
		fail(c, "参数错误")
		return
	}
	res, err := service.AdminWalletWithdraw(p.CustomerID, p.Reason, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	reason := strings.TrimSpace(p.Reason)
	SetAuditDetail(c, "member", strconv.Itoa(p.CustomerID),
		"退余额 本金"+po.FormatYuan(res.Principal)+"元（赠金作废"+po.FormatYuan(res.Gift)+"元），原因："+reason)
	ok(c, gin.H{
		"principal": po.ToYuan(res.Principal),
		"gift":      po.ToYuan(res.Gift),
		"msg":       "余额已清零，请向顾客支付 " + po.FormatYuan(res.Principal) + " 元",
	})
}

// ============================================================================
// 混合支付(余额抵扣 + 微信补差)
// ============================================================================

// WalletMixedPay 先用储值余额抵扣 walletAmount,不足以付清时返回微信支付调起参数,
// 由顾客补齐差额。
//
// 顺序刻意是「先扣余额,再拉起收银台」:反过来(先付渠道再扣余额)一旦余额不足,
// 顾客的钱已经进了商户号,只能再发起一笔退款 —— 体验与对账都更差。
// 代价是「顾客放弃渠道支付时余额被占用」,由 AbortMixedPay(即时)与
// StartMixedPaySweeper(超时)两条路径兜底,不会永久占用。
func WalletMixedPay(c *gin.Context) {
	var p struct {
		OrderNo      string  `json:"orderNo"`
		Code         string  `json:"code"`         // wx.login 的临时凭证(仅渠道补差时需要)
		WalletAmount float64 `json:"walletAmount"` // 元,本次要用掉的储值金额
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.OrderNo) == "" {
		fail(c, "参数错误")
		return
	}
	orderNo := strings.TrimSpace(p.OrderNo)
	walletCents := po.ToCents(p.WalletAmount)
	if walletCents <= 0 {
		fail(c, "请选择要使用的储值金额")
		return
	}
	ctx := c.Request.Context()

	prep, err := service.PrepareMixedPay(ctx, customerIDFrom(c), orderNo, walletCents)
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 余额已把订单付清:不需要拉起收银台。先付模式补出厨单(与纯余额支付同一拍)。
	if prep.PayCents == 0 {
		if prep.FirstTime {
			if o, e := service.GetOrderAmount(orderNo); e == nil {
				printKitchenOnPaid(ctx, o.OrderID)
			}
		}
		ok(c, gin.H{"paid": true, "walletCents": po.ToYuan(prep.WalletCents)})
		return
	}

	// 余额已经扣掉了。从这里到「渠道下单成功」之间任何一步失败都必须把余额退回,
	// 否则就是「钱扣了、单没付」—— 顾客没有任何自助恢复手段。
	abort := func(reason string) {
		if e := service.AbortMixedPay(ctx, orderNo, reason); e != nil {
			logger.CtxErrorf(ctx, "[wallet][告警] 混合支付失败后回退余额也失败(请人工核对) 订单%s: %v", orderNo, e)
		}
	}

	provider, err := pay.Get(pay.ChannelWxpay)
	if err != nil || !provider.Enabled() {
		abort("小程序支付未开通，余额已退回")
		fail(c, "小程序支付尚未开通，请用收款码或到前台结账")
		return
	}
	openID, err := weapp.Code2Session(ctx, p.Code)
	if err != nil {
		abort("换取支付身份失败，余额已退回")
		if err == weapp.ErrMemberDisabled {
			fail(c, "小程序支付尚未开通，请用收款码或到前台结账")
			return
		}
		fail(c, err.Error())
		return
	}
	// 同一订单只保留一条能付款的渠道单(与纯渠道支付同一口径)。
	closePendingPayments(ctx, orderNo, pay.ChannelWxpay)
	params, err := provider.CreateJsapi(pay.PayReq{
		OrderNo:     orderNo,
		Description: "餐饮消费" + orderNo,
		AmountCents: prep.PayCents,
		ClientIP:    c.ClientIP(),
		OpenID:      openID,
	})
	if err != nil {
		abort("渠道下单失败，余额已退回")
		fail(c, "发起支付失败: "+err.Error())
		return
	}
	// 落支付流水时再做一次金额复核(与纯渠道支付同一道防线):
	// CreatePayment 的判据是「本笔 + 其它渠道已入账 == 订单总额」,混合支付天然满足。
	if err := service.CreatePayment(prep.OrderID, orderNo, pay.ChannelWxpay, prep.PayCents, params.Package); err != nil {
		if closeErr := provider.Close(orderNo); closeErr != nil {
			logger.CtxWarnf(ctx, "[pay][告警] 混合支付落库失败后关单也失败(旧参数可能仍可支付) 订单%s: %v", orderNo, closeErr)
		}
		abort("支付流水落库失败，余额已退回")
		fail(c, "支付发起异常，请稍后重试")
		return
	}
	logger.CtxInfof(ctx, "[wallet] 混合支付 订单%s 余额%d分 待微信%d分", orderNo, prep.WalletCents, prep.PayCents)
	ok(c, gin.H{
		"paid":        false,
		"walletCents": po.ToYuan(prep.WalletCents),
		"payCents":    po.ToYuan(prep.PayCents),
		"timeStamp":   params.TimeStamp,
		"nonceStr":    params.NonceStr,
		"package":     params.Package,
		"signType":    params.SignType,
		"paySign":     params.PaySign,
	})
}

// ============================================================================
// 混合支付超时释放
// ============================================================================

const (
	// mixedPayHoldTimeout 余额被占用多久后视为「顾客已放弃」。
	// 取 15 分钟:足够顾客完成一次微信支付(含输密码/切后台),又不至于把钱锁太久。
	mixedPayHoldTimeout = 15 * time.Minute
	// mixedPaySweepEvery 巡检间隔。
	mixedPaySweepEvery = 5 * time.Minute
)

// StartMixedPaySweeper 后台释放「余额已扣、渠道迟迟未付」的混合支付占用。
//
// 放在 handler 层而不是 service:释放前必须先用 pay 包关掉渠道单
// (关单成功才意味着这笔微信交易不可能再支付成功),而 service 不得 import pay。
func StartMixedPaySweeper() {
	go func() {
		for {
			time.Sleep(mixedPaySweepEvery)
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[wallet] 混合支付超时释放单轮 panic(已跳过本轮): %v", r)
					}
				}()
				releaseMixedHoldsOnce(context.Background())
			}()
		}
	}()
}

// releaseMixedHoldsOnce 执行一轮释放。
func releaseMixedHoldsOnce(ctx context.Context) {
	before := time.Now().Add(-mixedPayHoldTimeout).Format(conf.TimeLayout)
	orders, err := service.ListMixedHoldCandidates(before, 20)
	if err != nil {
		logger.CtxWarnf(ctx, "[wallet] 查询混合支付占用失败: %v", err)
		return
	}
	if len(orders) == 0 {
		return
	}
	provider, err := pay.Get(pay.ChannelWxpay)
	if err != nil || !provider.Enabled() {
		return
	}
	for _, orderNo := range orders {
		// 先关渠道单:关单成功 ⇒ 该笔微信交易不可能再支付成功,之后退余额才是安全的。
		// 关单失败(例如这笔其实刚支付成功)就跳过,让回调链路正常入账。
		if e := provider.Close(orderNo); e != nil {
			logger.CtxWarnf(ctx, "[wallet] 混合支付超时释放前关单失败,本轮跳过(下轮重试) 订单%s: %v", orderNo, e)
			continue
		}
		if e := service.AbortMixedPay(ctx, orderNo, "混合支付超时，余额已退回"); e != nil {
			logger.CtxErrorf(ctx, "[wallet][告警] 混合支付超时释放失败 订单%s: %v", orderNo, e)
			continue
		}
		logger.CtxInfof(ctx, "[wallet] 混合支付超时,已退回余额并关单 订单%s", orderNo)
	}
}

// WalletCancelMixedPay 顾客取消混合支付:立刻把刚扣的余额退回。
//
// 为什么不能只靠 15 分钟的超时释放:顾客点了「取消」却被告知「稍后自动退回」,
// 体感就是「钱被扣了」——下次他就不敢再用储值了。
//
// 安全性来自「先关渠道单」:微信对已支付的交易会拒绝关单,关单成功才说明
// 这笔钱不可能再付出去,此时退余额不会与回调打架。关单失败一律不释放,
// 交给回调入账或超时巡检(宁可晚退,也不能在支付成功的边缘把钱退回去)。
func WalletCancelMixedPay(c *gin.Context) {
	var p struct {
		OrderNo string `json:"orderNo"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.OrderNo) == "" {
		fail(c, "参数错误")
		return
	}
	orderNo := strings.TrimSpace(p.OrderNo)
	ctx := c.Request.Context()

	// 只能撤销自己名下的订单:单号是可枚举的,不校验归属就成了「帮你把别人的支付取消掉」。
	// 混合支付在扣余额时会认领匿名单,所以这里订单必然已有归属。
	if owner := service.OrderOwner(orderNo); owner != 0 && owner != customerIDFrom(c) {
		fail(c, "只能撤销自己的订单")
		return
	}

	o, err := service.GetOrderAmount(orderNo)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if o.PayStatus == 1 {
		// 已经付清了(可能回调刚到):不能退余额。
		okMsg(c, "订单已支付")
		return
	}

	provider, err := pay.Get(pay.ChannelWxpay)
	if err == nil && provider.Enabled() {
		if e := provider.Close(orderNo); e != nil {
			logger.CtxWarnf(ctx, "[wallet] 取消混合支付前关单失败(可能已支付),不释放余额 订单%s: %v", orderNo, e)
			fail(c, "支付结果确认中，请稍后刷新订单")
			return
		}
	}
	if err := service.AbortMixedPay(ctx, orderNo, "顾客取消支付，余额已退回"); err != nil {
		fail(c, "取消失败，余额将稍后自动退回")
		return
	}
	okMsg(c, "已取消，储值余额已退回")
}
