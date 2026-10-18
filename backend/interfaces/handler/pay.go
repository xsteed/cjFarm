package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/pay"
	"dining-system/infrastructure/weapp"
)

// refundMu 串行化「退款额度校验 + 落退款单 + 调渠道」全过程。
//
// 退款是收银员低频操作,全局锁足够;若不串行,两个并发退款请求会基于同一
// 可退余额各自调用渠道退款,造成退款总额超过订单金额。单实例部署(本项目的
// systemd 部署模型)下全局锁完全有效,保留为第一道防线;
// service.CreateRefund 内部已用数据库订单行锁做第二道防线,多实例演进时仍可兜底。
var refundMu sync.Mutex

// payQueryCooldown 主动查单的进程内频控阈值:同一订单 3 秒内只允许触发一次
// 渠道主动查单,期间的 PayQuery 请求直接返回 DB 状态。
const payQueryCooldown = 3 * time.Second

// payQueryLastAt 记录各订单最近一次渠道主动查单的时间(orderNo -> time.Time)。
//
// PayQuery 是公开接口,任何 orderNo 都能触发渠道主动查单消耗渠道 API 配额,
// 因此用进程内 map+mutex 做简单频控。单实例部署(项目 systemd 模型)下有效;
// 多实例部署时各实例互不相通,需换成 Redis 等共享存储。订单号数量有限,
// 单实例下 map 增长可忽略,故不做定期清理。
var (
	payQueryMu     sync.Mutex
	payQueryLastAt = map[string]time.Time{}
)

// ============ 在线支付 ============

// channelName 渠道展示名。
func channelName(channel string) string {
	if channel == pay.ChannelWxpay {
		return "微信支付"
	}
	if channel == pay.ChannelAlipay {
		return "支付宝"
	}
	return channel
}

// PayCreate 顾客发起在线支付:校验订单后调用渠道下单,返回可渲染二维码的字符串。
func PayCreate(c *gin.Context) {
	var p struct {
		OrderNo string `json:"orderNo"`
		Channel string `json:"channel"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderNo == "" || p.Channel == "" {
		fail(c, "参数错误")
		return
	}
	provider, err := pay.Get(p.Channel)
	if err != nil {
		fail(c, err.Error())
		return
	}
	if !provider.Enabled() {
		fail(c, channelName(p.Channel)+"尚未开通，请联系商家配置")
		return
	}

	ctx := c.Request.Context()
	// 本函数后续日志都围绕「同一笔订单 + 同一渠道」,一次绑定、多条复用。
	log := logger.WithContext(ctx,
		zap.String("orderNo", p.OrderNo), zap.String("channel", p.Channel))

	orderID, totalCents, err := service.ValidateOrderPayable(ctx, p.OrderNo)
	if err != nil {
		fail(c, err.Error())
		return
	}

	// 切换渠道前先关闭其它渠道的待支付单:顾客从微信码切到支付宝码是常见路径,
	// 不关旧单的话旧码仍可扫码付款,两个渠道各收一笔就是重复支付(钱无处安放)。
	closePendingPayments(ctx, p.OrderNo, p.Channel)

	result, err := provider.Create(pay.PayReq{
		OrderNo:     p.OrderNo,
		Description: "餐饮消费" + p.OrderNo,
		AmountCents: totalCents,
		ClientIP:    c.ClientIP(),
	})
	if err != nil {
		log.Warnf("[pay] 发起支付失败: %v", err)
		fail(c, "发起支付失败: "+err.Error())
		return
	}

	// 落支付流水(待支付)。同一订单同一渠道重复发起时,复用旧流水记录状态。
	// 落库在事务内复核订单金额(锁订单行后重读):校验读金额到渠道下单之间若发生
	// 加菜/改单抬价,这里会发现金额已变,必须关掉刚生成的旧金额码并请顾客重新发起 ——
	// 否则顾客按旧金额付款后回调被金额防线拒绝,钱到了订单却入不了账。
	// 落库失败绝不能再把二维码给顾客:顾客付款后回调找不到待支付流水可回写,
	// 订单永远停在未支付。这里 fail-closed,并尝试关掉刚生成的渠道收款码。
	if err := service.CreatePayment(orderID, p.OrderNo, p.Channel, totalCents, result.CodeURL); err != nil {
		if errors.Is(err, service.ErrOrderAmountChanged) {
			log.Warnf("[pay] 订单金额在出码期间被修改,关闭本次收款码 订单%s", p.OrderNo)
			if closeErr := provider.Close(p.OrderNo); closeErr != nil {
				log.Warnf("[pay][告警] 金额变更后关单也失败(旧码可能仍可支付,请人工关注): %v", closeErr)
			}
			fail(c, "订单金额已变更，请刷新后重新发起支付")
			return
		}
		log.Errorf("[pay][告警] 支付流水落库失败,拒绝返回二维码: %v", err)
		// 渠道收款码已生成,若不关单顾客仍可扫码付款,只能尽力关单降低掉单风险;
		// 关单失败仅告警(渠道侧单可能仍可付,需人工关注)。
		if closeErr := provider.Close(p.OrderNo); closeErr != nil {
			log.Warnf("[pay][告警] 支付流水落库失败后关单也失败(存在掉单风险,请人工关注): %v", closeErr)
		}
		fail(c, "支付发起异常，请稍后重试")
		return
	}
	log.Infof("[pay] 已发起支付 金额%d分", totalCents)

	ok(c, gin.H{
		"channel": p.Channel,
		"codeUrl": result.CodeURL,
		"orderNo": p.OrderNo,
		"amount":  totalCents,
	})
}

// PayJsapi 顾客在小程序内发起微信支付(JSAPI)。
//
// 为什么需要它:小程序里不能「自己扫自己」,Native 的 code_url 在微信内无处可扫。
// 小程序内完成支付必须走 JSAPI —— 服务端用 wx.login 的 code 换 openid、用
// transactions/jsapi 下单拿 prepay_id、再用商户私钥签好调起参数,前端直接
// wx.requestPayment,顾客全程不离开小程序。
//
// 商户配置与 Native 完全共用(wxpay_*),回调也是同一个 notify_url;唯一的额外要求是
// wxpay_appid 必须是**小程序**的 AppID —— openid 在不同 AppID 下不互通,填成公众号的
// 会出现「下单成功但微信拒付」。openid 走 code2session 现换不落库:未登录顾客也能付。
func PayJsapi(c *gin.Context) {
	var p struct {
		OrderNo string `json:"orderNo"`
		Code    string `json:"code"` // wx.login 的临时凭证(一次性,5 分钟有效)
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderNo == "" || p.Code == "" {
		fail(c, "参数错误")
		return
	}

	provider, _ := pay.Get(pay.ChannelWxpay)
	if !provider.Enabled() {
		fail(c, "小程序内支付尚未开通，请用收款码或到前台结账")
		return
	}

	ctx := c.Request.Context()
	log := logger.WithContext(ctx,
		zap.String("orderNo", p.OrderNo), zap.String("channel", pay.ChannelWxpay))

	orderID, totalCents, err := service.ValidateOrderPayable(ctx, p.OrderNo)
	if err != nil {
		fail(c, err.Error())
		return
	}

	// openid 用小程序 code 现换:与登录链路无关,匿名顾客同样能付。
	openID, err := weapp.Code2Session(ctx, p.Code)
	if err != nil {
		log.Warnf("[pay] JSAPI 换取 openid 失败: %v", err)
		// 小程序 AppSecret 未配置时 code2session 会返回「会员功能未开启」——
		// 这个文案对正在付款的顾客没有意义,换成能指路的说法。
		if errors.Is(err, weapp.ErrMemberDisabled) {
			fail(c, "小程序内支付尚未开通，请用收款码或到前台结账")
			return
		}
		fail(c, err.Error())
		return
	}

	// 同一订单只保留一条能付款的渠道单:重复发起时先关旧的,避免顾客手里的
	// 旧参数与新参数都能拉起收银台(两笔都可能付成功)。
	closePendingPayments(ctx, p.OrderNo, pay.ChannelWxpay)

	params, err := provider.CreateJsapi(pay.PayReq{
		OrderNo:     p.OrderNo,
		Description: "餐饮消费" + p.OrderNo,
		AmountCents: totalCents,
		ClientIP:    c.ClientIP(),
		OpenID:      openID,
	})
	if err != nil {
		log.Warnf("[pay] 发起小程序支付失败: %v", err)
		fail(c, "发起支付失败: "+err.Error())
		return
	}

	// 与 Native 同一条金额防线:落流水前在订单行锁内复核金额,变了就关掉刚生成的
	// 支付单并请顾客重新发起 —— 否则顾客按旧金额付款后回调会被金额防线拒绝。
	if err := service.CreatePayment(orderID, p.OrderNo, pay.ChannelWxpay, totalCents, params.Package); err != nil {
		if errors.Is(err, service.ErrOrderAmountChanged) {
			log.Warnf("[pay] 订单金额在 JSAPI 下单期间被修改,关闭本次支付单 订单%s", p.OrderNo)
			if closeErr := provider.Close(p.OrderNo); closeErr != nil {
				log.Warnf("[pay][告警] 金额变更后关单也失败(旧参数可能仍可支付,请人工关注): %v", closeErr)
			}
			fail(c, "订单金额已变更，请刷新后重新发起支付")
			return
		}
		log.Errorf("[pay][告警] JSAPI 支付流水落库失败,拒绝返回调起参数: %v", err)
		if closeErr := provider.Close(p.OrderNo); closeErr != nil {
			log.Warnf("[pay][告警] 流水落库失败后关单也失败(存在掉单风险,请人工关注): %v", closeErr)
		}
		fail(c, "支付发起异常，请稍后重试")
		return
	}
	log.Infof("[pay] 已发起小程序支付 金额%d分", totalCents)

	ok(c, params)
}

// PayNotifyWxpay 微信支付结果回调(验签 + 解密 + 回写)。
func PayNotifyWxpay(c *gin.Context) {
	provider, _ := pay.Get(pay.ChannelWxpay)
	log := logger.WithContext(c.Request.Context(), zap.String("channel", pay.ChannelWxpay))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		// 读不到回调体 = 渠道侧确实付款了但我们没收到,必须留痕:
		// 静默应答 FAIL 会让「顾客已付款、订单未入账」变成无头案。
		log.Errorf("[pay] 微信回调读取请求体失败,应答 FAIL 待重试: %v", err)
		c.String(http.StatusBadRequest, `{"code":"FAIL"}`)
		return
	}
	headers := map[string]string{
		"Wechatpay-Timestamp": c.GetHeader("Wechatpay-Timestamp"),
		"Wechatpay-Nonce":     c.GetHeader("Wechatpay-Nonce"),
		"Wechatpay-Signature": c.GetHeader("Wechatpay-Signature"),
		"Wechatpay-Serial":    c.GetHeader("Wechatpay-Serial"),
	}
	notify, err := provider.VerifyNotify(c.Request.Context(), headers, body)
	if err != nil {
		log.Warnf("[pay] 微信回调验签失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": err.Error()})
		return
	}
	if !handlePayNotify(c, pay.ChannelWxpay, notify) {
		// 处理失败(如回写时 DB 故障):应答 FAIL 让微信稍后重试,避免顾客已付款却掉单。
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "处理失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
}

// PayNotifyAlipay 支付宝异步通知(验签 + 回写)。
func PayNotifyAlipay(c *gin.Context) {
	provider, _ := pay.Get(pay.ChannelAlipay)
	log := logger.WithContext(c.Request.Context(), zap.String("channel", pay.ChannelAlipay))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Errorf("[pay] 支付宝回调读取请求体失败,应答 fail 待重试: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}
	notify, err := provider.VerifyNotify(c.Request.Context(), nil, body)
	if err != nil {
		log.Warnf("[pay] 支付宝回调验签失败: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if !handlePayNotify(c, pay.ChannelAlipay, notify) {
		// 处理失败(如回写时 DB 故障):应答 fail 让支付宝稍后重试,避免顾客已付款却掉单。
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// handlePayNotify 回调公共处理:金额一致性校验 + 幂等回写。
//
// 返回 false 表示本次处理失败,调用方应应答 FAIL(渠道会重试);返回 true 表示
// 已正常处理(或订单确实不存在,无需再重试),调用方应答成功。
func handlePayNotify(c *gin.Context, channel string, notify pay.PayNotify) bool {
	// 从这里往下的每一条支付日志都带上本次回调的 requestID:
	// 微信/支付宝会重发通知,同一笔支付在日志里会出现多次且间隔不定,
	// 靠 requestID 才能把「这一次回调到底走到哪一步」完整串起来。
	ctx := c.Request.Context()
	if !notify.Success {
		// 非成功通知(如关闭/退款通知)无需处理订单支付状态,直接应答成功。
		return true
	}
	o, err := service.GetOrderAmount(notify.OrderNo)
	if err != nil {
		// 必须区分「订单不存在」与「DB 瞬时故障」:
		// 前者是脏数据/历史回调,应答成功避免平台无谓重试;
		// 后者若也应答成功,渠道不再重试,顾客已付款却永不入账(掉单不可恢复)。
		if errors.Is(err, sql.ErrNoRows) {
			logger.CtxWarnf(ctx, "[pay] 回调订单不存在,应答成功: %s", notify.OrderNo)
			return true
		}
		logger.CtxErrorf(ctx, "[pay][告警] 回调查询订单失败,应答失败待渠道重试 订单%s: %v", notify.OrderNo, err)
		return false
	}
	orderID, totalCents := o.OrderID, o.TotalAmount
	// 金额一致性:回调金额必须与订单金额一致,否则挂起人工处理,绝不自动入账。
	// 同时落一条退款流水标记(状态=失败、原因写明差额),让「钱收到了但入不了账」
	// 在退款记录里可见、可追 —— 只留日志的话,事后根本发现不了顾客多付/少付。
	if notify.AmountCents != totalCents {
		logger.CtxWarnf(ctx, "[pay][告警] 金额不一致 订单%s 期望%d分 实际%d分", notify.OrderNo, totalCents, notify.AmountCents)
		service.RecordMismatchedPayment(ctx, orderID, notify.OrderNo, channel, notify.AmountCents, totalCents, notify.ChannelTradeNo)
		return true
	}
	// 回写失败(DB 故障)必须应答 FAIL 让渠道重试:首读成功不代表回写一定成功,
	// 若折叠成成功应答,顾客已付款却永不入账(掉单不可恢复)。
	if !applyPaymentSuccess(ctx, orderID, notify.OrderNo, channel, notify.AmountCents, notify.ChannelTradeNo) {
		logger.CtxErrorf(ctx, "[pay][告警] 支付回写失败,应答失败待渠道重试 订单%s", notify.OrderNo)
		return false
	}
	return true
}

// applyPaymentSuccess 支付成功回写(回调与主动查单补单共用):
// 幂等回写订单与流水;若订单已由其它渠道/线下收款,则为重复支付,自动原路退回。
// 返回 false 表示回写遭遇 DB 故障,调用方(回调)应应答 FAIL 让渠道重试;
// 主动查单路径可忽略返回值(本次不入账,下次轮询再试)。
func applyPaymentSuccess(ctx context.Context, orderID int, orderNo, channel string, amountCents int64, channelTradeNo string) bool {
	// 先付模式出厨单的依据由 ApplyPaymentSuccess 回报(FirstTime = 本次才把订单
	// 从未支付翻到已支付,含掉单补偿入账)。不在事务外读 beforePaid 快照:两个并发
	// 通知(渠道重发 + 前端轮询补单)可同时读到「未支付」,各自出单,后厨会收到
	// 两张同一订单的厨单,照单做两遍。
	res := service.ApplyPaymentSuccess(ctx, orderNo, channel, channelTradeNo, channelName(channel), amountCents)
	if res.Failed {
		return false
	}
	if res.Duplicate {
		autoRefundDuplicatePayment(ctx, orderID, orderNo, channel, amountCents, channelTradeNo)
		return true
	}
	// 先付模式:本次首次入账 → 后厨出单(后付模式下无操作)。
	if res.FirstTime {
		printKitchenOnPaid(ctx, orderID)
	}
	return true
}

// autoRefundDuplicatePayment 重复支付自动原路退回。
//
// 触发:订单已通过其它渠道/线下收款成功后,本渠道又收到了一笔等额支付
// (典型:顾客同时打开了微信与支付宝的收款码,两个都付了)。
// 处理:登记一条 is_duplicate=1 的退款流水并调渠道退款;支付宝同步到账直接置成功,
// 微信异步则留「处理中」,由管理端「同步状态」收尾(见 PayRefundQuery 的分流)。
// 重复退回的钱从未计入订单营收,因此不走 ApplyRefundSuccess 的订单账务回写。
func autoRefundDuplicatePayment(ctx context.Context, orderID int, orderNo, channel string, amountCents int64, channelTradeNo string) {
	// 本函数会连打多条日志,且都围绕同一笔订单、同一渠道 —— 用 WithContext 把
	// requestID + 订单字段一次性绑定,后续每条日志自动带上,不必逐条重复拼字符串。
	// 排障时按订单号 grep 也更快:字段是结构化的,不受文案措辞变化影响。
	log := logger.WithContext(ctx, zap.String("orderNo", orderNo), zap.String("channel", channel))

	// 查重(快速路径):该渠道已有未终态的重复支付退款单(通知重发场景),复用而非重复发起,
	// 否则同一笔支付会被渠道二次退款(商户资金损失)。 authoritative 判定在
	// RecordDuplicateRefund 的事务内(锁订单行后重查),这里只是省掉一次事务。
	if refundID, refundNo := service.HasPendingDuplicateRefund(ctx, orderNo, channel); refundID > 0 {
		log.Infof("[pay] 重复支付退款已受理过,跳过重复发起 退款单%s", refundNo)
		return
	}
	log.Warnf("[pay][告警] 检测到重复支付,自动原路退回 金额%d分 流水号%s", amountCents, channelTradeNo)
	refundNo, refundID, paymentID, totalCents, err := service.RecordDuplicateRefund(ctx, orderID, orderNo, channel, amountCents, channelTradeNo)
	if errors.Is(err, service.ErrDuplicateRefundPending) {
		// 并发通知抢先落了单(快速路径查重未命中、事务内查重命中):本协程不再发起,
		// 由抢先者完成退款即可。
		log.Infof("[pay] 重复支付退款已被并发请求登记,跳过重复发起 订单%s", orderNo)
		return
	}
	if err != nil {
		// 登记失败 = 重复支付的钱退不出去(渠道侧无退款单可依据),只能人工介入;
		// 静默 return 会让这笔钱彻底消失在账上。
		log.Errorf("[pay][告警] 重复支付退款单登记失败,请人工处理 金额%d分: %v", amountCents, err)
		return
	}
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		log.Warnf("[pay][告警] 渠道未配置,重复支付无法自动退回,请人工处理 退款单%s", refundNo)
		return
	}
	result, err := provider.Refund(orderNo, refundNo, amountCents, totalCents)
	if err != nil || !result.Success {
		// 状态回写失败也要留痕:退款单会一直停在「处理中」,
		// 下次查重被当成「已受理」而跳过(见 HasPendingDuplicateRefund),钱就再也退不出去。
		if e := service.MarkRefund(refundID, po.RefundStatusFail, "", "重复支付自动退回失败,请人工处理"); e != nil {
			log.Errorf("[pay][告警] 重复支付退回失败且退款单置失败也失败,请人工处理 退款单%d: %v", refundID, e)
		}
		log.Warnf("[pay][告警] 重复支付自动退回失败,请人工处理 退款单%s: %v", refundNo, err)
		return
	}
	// 判据用「渠道返回的实际状态」而非渠道名:微信退款同样可能同步返回 SUCCESS,
	// 按渠道名放行会把已退出的钱停在「处理中」,报表虚高且文案误导(见 PayRefund 同款修复)。
	if result.Status == pay.RefundSuccess || refundSettled(channel) {
		if e := service.MarkRefund(refundID, po.RefundStatusSuccess, result.RefundNo, ""); e != nil {
			log.Errorf("[pay][告警] 重复支付已退回但退款单置成功失败,请人工核对 退款单%d: %v", refundID, e)
		}
		if e := service.MarkPaymentRefunded(paymentID); e != nil {
			log.Errorf("[pay][告警] 重复支付已退回但支付流水置已退失败,请人工核对 流水%d: %v", paymentID, e)
		}
		log.Infof("[pay] 重复支付已原路退回 金额%d分", amountCents)
		return
	}
	log.Infof("[pay] 重复支付退款已受理,等待渠道到账 退款单%s", refundNo)
}

// settleQueryResult 处理主动查单的支付结果:金额一致才补单入账,不一致/非正金额
// 只留痕人工核对(与通知路径 handlePayNotify 的金额校验同一口径)。主动查单路径
// 需 mock 渠道不易走 handler 级测试,抽成独立函数便于单测钉住「少付入账」防线。
func settleQueryResult(ctx context.Context, orderID int, orderNo, channel string, q pay.PayQuery) {
	// 补单前重读订单最新应收金额,覆盖查单请求发出到返回之间的改价窗口。
	o, err := service.GetOrderAmount(orderNo)
	if err != nil {
		logger.CtxWarnf(ctx, "[pay][告警] 主动查单确认已支付但读取订单金额失败,暂不补单 订单%s: %v", orderNo, err)
		return
	}
	totalCents := o.TotalAmount
	// 金额不一致(含 <=0,如支付宝金额解析失败返回 0)绝不入账,只留痕人工核对,
	// 否则「生成旧码→加菜涨价→用旧码付旧金额→前端轮询补单」会造成少付入账。
	if q.PaidAmount <= 0 || q.PaidAmount != totalCents {
		logger.CtxWarnf(ctx, "[pay][告警] 主动查单金额不一致 订单%s 期望%d分 实际%d分", orderNo, totalCents, q.PaidAmount)
		service.RecordMismatchedPayment(ctx, orderID, orderNo, channel, q.PaidAmount, totalCents, q.ChannelTradeNo)
		return
	}
	applyPaymentSuccess(ctx, orderID, orderNo, channel, q.PaidAmount, q.ChannelTradeNo)
}

// payQueryAllowed 判断某订单本次是否允许触发渠道主动查单(进程内频控)。
// 距上次主动查单不足 payQueryCooldown 时返回 false,期间 PayQuery 直接返回 DB 状态。
// 顺带清理已过冷却期的 key:订单号随时间持续递增,不清理的话 map 随历史订单数
// 单调增长(每 entry 约 80B,单店年增数 MB,低量级但无必要)。
func payQueryAllowed(orderNo string) bool {
	now := time.Now()
	payQueryMu.Lock()
	defer payQueryMu.Unlock()
	// 每次顺带扫一遍冷却窗口外的陈旧 key;map 容量有限,摊销成本可忽略。
	for k, last := range payQueryLastAt {
		if now.Sub(last) >= payQueryCooldown {
			delete(payQueryLastAt, k)
		}
	}
	if last, ok := payQueryLastAt[orderNo]; ok && now.Sub(last) < payQueryCooldown {
		return false
	}
	payQueryLastAt[orderNo] = now
	return true
}

// PayQuery 顾客/管理端主动查单兜底(回调丢失时由前端触发补单)。
func PayQuery(c *gin.Context) {
	orderNo := c.Query("orderNo")
	if orderNo == "" {
		fail(c, "参数错误")
		return
	}
	// 顾客端会高频轮询本接口,且同一订单号可能被多个客户端并发查;
	// 带上 requestID 才能区分「哪一次轮询走到了补单分支」。
	ctx := c.Request.Context()
	// 优先返回订单当前支付状态。
	o, err := service.GetOrderAmount(orderNo)
	if err != nil {
		// 不能只回「订单不存在」:数据库故障与真的没这个订单表现一样,
		// 但前者是全站故障、后者是用户输错单号,排查方向完全不同。
		logger.CtxWarnf(ctx, "[pay] 查询支付状态时读取订单失败(订单不存在或数据库故障) 订单%s: %v", orderNo, err)
		fail(c, "订单不存在")
		return
	}
	orderID, payStatus := o.OrderID, o.PayStatus
	// 未支付时尝试主动查单补单。
	if payStatus == 0 {
		// 找出该订单最近的支付渠道。
		ch, chErr := service.LastPaymentChannel(orderNo)
		if chErr != nil {
			logger.CtxWarnf(ctx, "[pay] 查询订单最近支付渠道失败,本次不主动查单 订单%s: %v", orderNo, chErr)
		}
		if chErr == nil && ch != "" {
			if provider, e := pay.Get(ch); e == nil && provider.Enabled() {
				// 频控只作用于「渠道主动查单」这一步,不阻塞订单状态查询返回:
				// 冷却期内直接返回 DB 状态,避免公开接口被任意 orderNo 刷渠道 API 配额。
				if !payQueryAllowed(orderNo) {
					ok(c, gin.H{"orderNo": orderNo, "payStatus": payStatus})
					return
				}
				q, qErr := provider.Query(orderNo)
				if qErr != nil {
					// 查单失败不等于未支付:渠道抖动/证书过期都会走到这里。
					// 不留痕的话「顾客说付了、系统说没付」就查不出原因。
					logger.CtxWarnf(ctx, "[pay] 主动查单失败,本次按 DB 状态返回 订单%s 渠道%s: %v", orderNo, ch, qErr)
				}
				if qErr == nil && q.Success {
					settleQueryResult(ctx, orderID, orderNo, ch, q)
					if cur, e2 := service.GetOrderAmount(orderNo); e2 != nil {
						logger.CtxWarnf(ctx, "[pay] 补单后重读订单支付状态失败,返回补单前状态 订单%s: %v", orderNo, e2)
					} else {
						payStatus = cur.PayStatus
					}
				}
			}
		}
	}
	ok(c, gin.H{"orderNo": orderNo, "payStatus": payStatus})
}

// PayRefund 管理端退款:支持全额与部分退款。
// 入参 amount 单位为元(0 或留空表示全额退款),退款成功后累加订单已退金额。
func PayRefund(c *gin.Context) {
	var p struct {
		OrderID int     `json:"orderId"`
		Amount  float64 `json:"amount"` // 元,0=全额
		Reason  string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	// 查询订单支付信息。
	o, err := service.GetRefundOrderInfo(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	orderNo, payStatus, totalCents := o.OrderNo, o.PayStatus, o.TotalAmount
	if payStatus != 1 {
		fail(c, "订单未支付，无法在线退款")
		return
	}
	// 发起退款即视为商家已实际处理这单:列表「新」角标随之消解(与制作/收款/结账等操作同口径)。
	// 放在参数与状态校验之后:无效请求不标记;后续渠道退款无论成功/失败/转处理中,都算操作过。
	service.MarkOrderViewed(p.OrderID)
	// 找支付流水:已支付或(部分)已退款的流水都可用于继续退款,避免部分退款后剩余金额退不掉。
	flow, flowErr := service.LastPaidPayment(orderNo)
	if flowErr != nil {
		fail(c, "未找到在线支付流水，该订单为线下收款，请人工退款")
		return
	}
	paymentID, channel := flow.PaymentID, flow.Channel
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		fail(c, "支付渠道未配置，无法在线退款")
		return
	}

	ctx := c.Request.Context()
	// 后续日志都围绕同一笔退款单,一次绑定 requestID + 订单/渠道字段。
	log := logger.WithContext(ctx,
		zap.String("orderNo", orderNo), zap.String("channel", channel))

	// 可退金额 = 订单金额 - (已成功 + 处理中)退款金额。
	// 处理中的退款单同样占用额度,防止并发退款超额;全程持锁保证校验与落单原子。
	// 基线查询失败时 fail-closed 拒绝退款:静默按 0 处理等于把可退额度放大到全额。
	refundMu.Lock()
	defer refundMu.Unlock()
	if len(p.Reason) > 60 {
		p.Reason = string([]rune(p.Reason)[:60])
	}

	// 审计摘要文案先准备好,待 CreateRefund 落单成功后、调渠道退款前写入:
	// 万一渠道退款失败,日志里仍能看到「想退多少、为什么退」,
	// 失败原因由中间件从响应里补上(状态记为失败)。
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "未填写"
	}

	// 校验 + 落单在同一 DB 事务内完成(订单行锁串行化),refundMu 仍保留为单实例第一道防线。
	// 落库用规范化后的 reason(与下方审计摘要同源),避免退款记录里存着未 Trim 的空白原因。
	refundNo, refundID, refundCents, err := service.CreateRefund(ctx, p.OrderID, orderNo, paymentID, channel, totalCents, p.Amount, reason, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}

	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("发起退款 ¥%.2f（%s），原因：%s", po.ToYuan(refundCents), channelName(channel), reason))

	result, err := provider.Refund(orderNo, refundNo, refundCents, totalCents)
	if err != nil {
		log.Warnf("[pay] 渠道退款失败 退款单%s 金额%d分: %v", refundNo, refundCents, err)
		if e := service.MarkRefund(refundID, po.RefundStatusFail, "", truncateMsg(err.Error())); e != nil {
			// 退款单停在「处理中」会导致可退额度被永久占用(见 CreateRefund 的额度校验),
			// 这笔钱就再也退不出去,故必须留痕让人工介入。
			log.Errorf("[pay][告警] 渠道退款失败且退款单置失败也失败,请人工处理 退款单%d: %v", refundID, e)
		}
		fail(c, "退款失败: "+err.Error())
		return
	}
	// 入账判据用「渠道返回的实际状态」而非渠道名:微信退款同样可能同步返回
	// SUCCESS(钱已退出),此前按渠道名放行会把已退出的钱停在「处理中」——
	// refund_amount 不累加、paid_amount 不扣减(当天营业额虚高),退款单继续占用
	// 可退额度,且回给商家「等待到账」的文案与事实相反。
	if result.Success && (result.Status == pay.RefundSuccess || refundSettled(channel)) {
		if err := service.ApplyRefundSuccess(ctx, refundID, paymentID, orderNo, refundCents, result.RefundNo); err != nil {
			// 渠道钱已退但账务回写失败:不能回复「退款成功」误导商家。
			// 这是最需要告警的一类 —— 钱已经出去了,账上却没记。
			log.Errorf("[pay][告警] 渠道已退款但账务入账失败(钱已退、账未记,请人工核对) 退款单%d 金额%d分: %v",
				refundID, refundCents, err)
			fail(c, "退款已发起但入账失败，请稍后重试或在退款记录中核对")
			return
		}
		log.Infof("[pay] 退款成功 退款单%s 金额%d分 操作人%s", refundNo, refundCents, adminName(c))
		okMsg(c, "退款成功")
		return
	}
	// 渠道明确返回失败(如微信 CLOSED/ABNORMAL:err 为 nil 但 Status=fail)。
	// 此时钱没有退出去,必须把退款单置为失败 —— 否则它会停在「处理中」,
	// 按 CreateRefund 的额度口径(已成功 + 处理中)永久占用可退额度,这笔钱再也退不出去。
	if result.Status == pay.RefundFail {
		log.Errorf("[pay] 渠道明确拒绝退款(钱未退出) 退款单%s 金额%d分 渠道状态=%s",
			refundNo, refundCents, result.Status)
		if e := service.MarkRefund(refundID, po.RefundStatusFail, result.RefundNo, "渠道退款失败"); e != nil {
			log.Errorf("[pay][告警] 退款单置失败也失败,请人工处理 退款单%d: %v", refundID, e)
		}
		fail(c, "退款失败：渠道未能受理该笔退款，请稍后在「退款记录」中核对")
		return
	}
	ok(c, gin.H{"refundId": refundID, "status": po.RefundStatusProcessing, "msg": "退款已受理，等待渠道到账"})
}

// refundSettled 判断渠道退款结果是否为「已到账」(支付宝同步到账)。
// 仅作为 result.Status 的补充判据:微信等异步渠道以其返回的实际状态为准
// (SUCCESS=已退 / PROCESSING=受理中),不按渠道名一刀切。
func refundSettled(channel string) bool {
	return channel == pay.ChannelAlipay
}

// PayRefundQuery 主动向渠道查询退款结果并回写(微信退款异步时兜底)。
func PayRefundQuery(c *gin.Context) {
	var p struct {
		RefundID int `json:"refundId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.RefundID == 0 {
		fail(c, "参数错误")
		return
	}
	r, err := service.GetRefund(p.RefundID)
	if err != nil {
		fail(c, "退款单不存在")
		return
	}
	if r.Status == po.RefundStatusSuccess {
		ok(c, gin.H{"status": po.RefundStatusSuccess, "msg": "已退款"})
		return
	}
	provider, err := pay.Get(r.Channel)
	if err != nil || !provider.Enabled() {
		fail(c, "支付渠道未配置，无法查询退款")
		return
	}
	ctx := c.Request.Context()
	// 后续日志都围绕同一张退款单,一次绑定 requestID + 订单/渠道/退款单字段。
	log := logger.WithContext(ctx,
		zap.String("orderNo", r.OrderNo), zap.String("channel", r.Channel),
		zap.Int("refundID", r.RefundID))
	q, err := provider.QueryRefund(r.OrderNo, r.RefundNo)
	if err != nil {
		fail(c, "退款查询失败: "+err.Error())
		return
	}
	switch q.Status {
	case pay.RefundSuccess:
		// 重复支付退回的钱从未计入订单营收,收尾只动退款单与支付流水,
		// 不能走 ApplyRefundSuccess(那会累加订单退款额度并扣减实收,把账记坏)。
		if r.IsDuplicate == 1 {
			if err := service.ApplyDuplicateRefundSuccess(ctx, r.RefundID, r.PaymentID, q.ChannelRefundNo, r.RefundNo); err != nil {
				log.Errorf("[pay][告警] 重复支付退回已到账但入账失败,请人工核对: %v", err)
			}
			ok(c, gin.H{"status": po.RefundStatusSuccess, "msg": "重复支付退回已到账"})
			return
		}
		if err := service.ApplyRefundSuccess(ctx, r.RefundID, r.PaymentID, r.OrderNo, r.Amount, q.ChannelRefundNo); err != nil {
			log.Errorf("[pay][告警] 渠道已退款但账务入账失败(钱已退、账未记,请人工核对) 金额%d分: %v", r.Amount, err)
			ok(c, gin.H{"status": po.RefundStatusProcessing, "msg": "渠道已退款但账务入账失败，请稍后重试"})
			return
		}
		ok(c, gin.H{"status": po.RefundStatusSuccess, "msg": "退款已到账"})
	case pay.RefundProcessing:
		ok(c, gin.H{"status": po.RefundStatusProcessing, "msg": "退款处理中，请稍后重试"})
	default:
		if err := service.MarkRefund(r.RefundID, po.RefundStatusFail, q.ChannelRefundNo, "渠道返回退款失败"); err != nil {
			log.Errorf("[pay][告警] 渠道返回退款失败且退款单置失败也失败,请人工处理: %v", err)
		}
		ok(c, gin.H{"status": po.RefundStatusFail, "msg": "退款失败，请在渠道后台核对"})
	}
}

// PayRefundList 查询某订单的退款记录。
func PayRefundList(c *gin.Context) {
	orderID, _ := strconv.Atoi(c.Query("orderId"))
	if orderID == 0 {
		fail(c, "参数错误")
		return
	}
	list, err := service.ListRefunds(orderID)
	if err != nil {
		fail(c, "查询失败")
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		out = append(out, gin.H{
			"refundId":        r.RefundID,
			"orderId":         r.OrderID,
			"refundNo":        r.RefundNo,
			"channel":         channelName(r.Channel),
			"channelRefundNo": r.ChannelRefundNo,
			"amount":          po.ToYuan(r.Amount),
			"status":          r.Status,
			"reason":          r.Reason,
			"operator":        r.Operator,
			"createTime":      r.CreateTime,
			// 1=重复支付自动原路退回(金额未计入营收),前端可据此标注展示。
			"duplicate": r.IsDuplicate == 1,
		})
	}
	ok(c, out)
}

// truncateMsg 裁剪错误信息长度,避免超长入库。
func truncateMsg(s string) string {
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// closeOnlinePayment 关闭该订单全部待支付的在线支付单。
// 场景:顾客已生成收款码但未付款时,订单被取消/改单/收款/结账 —— 不关单的话,
// 顾客之后仍可扫码付款,造成「钱已收、订单已终结」或重复支付的对不上账问题。
// 失败只记日志,不阻断主流程。
//
// ctx 由调用方(取消/改单/收款/结账等接口)传入:关单失败意味着顾客手里的旧收款码
// 仍可付款,是重复支付的直接入口,事后排查必须能回溯到「哪一次业务操作触发的关单」。
func closeOnlinePayment(ctx context.Context, orderID int) {
	orderNo, err := service.GetOrderNoByID(orderID)
	if err != nil {
		// 取不到单号就无法关单 —— 顾客手里的收款码会一直可付,
		// 埋下重复支付隐患。静默跳过等于把风险藏起来。
		logger.CtxWarnf(ctx, "[pay][告警] 关闭在线支付单前查询订单号失败,未关单(存在重复支付风险) 订单ID%d: %v", orderID, err)
		return
	}
	closePendingPayments(ctx, orderNo, "")
}

// closePendingPayments 关闭订单在 exceptChannel 之外所有渠道的待支付单
// (exceptChannel 为空串时关闭全部渠道)。
//
// 注意不按订单 pay_status 过滤:收款/结账后订单已置已支付,但仍需关掉
// 顾客手里还开着的二维码,否则就是重复支付的入口。
func closePendingPayments(ctx context.Context, orderNo, exceptChannel string) {
	// 循环内可能连打多条日志,且都围绕同一订单:一次绑定 orderNo,循环内复用。
	log := logger.WithContext(ctx, zap.String("orderNo", orderNo))
	for _, p := range service.ListPendingPayments(orderNo, exceptChannel) {
		provider, err := pay.Get(p.Channel)
		if err != nil || !provider.Enabled() {
			continue
		}
		if err := provider.Close(orderNo); err != nil {
			log.Warnf("[pay][告警] 关闭渠道支付单失败(存在重复支付风险,请人工关注) 渠道%s: %v", p.Channel, err)
			continue
		}
		// 渠道已关单但本地流水仍标「待支付」:关单动作会在每次查单时被重复执行,
		// 且前端可能据此继续展示收款码。
		if err := service.MarkPaymentClosed(p.PaymentID); err != nil {
			log.Errorf("[pay][告警] 渠道已关单但支付流水置已关闭失败,请人工核对 流水%d: %v", p.PaymentID, err)
		}
	}
}
