package handler

import (
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

	"dining-system/infra/logger"
	"dining-system/internal/pay"
	"dining-system/internal/po"
	"dining-system/internal/service"
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

	_, totalCents, err := service.ValidateOrderPayable(p.OrderNo)
	if err != nil {
		fail(c, err.Error())
		return
	}

	// 切换渠道前先关闭其它渠道的待支付单:顾客从微信码切到支付宝码是常见路径,
	// 不关旧单的话旧码仍可扫码付款,两个渠道各收一笔就是重复支付(钱无处安放)。
	closePendingPayments(p.OrderNo, p.Channel)

	result, err := provider.Create(pay.PayReq{
		OrderNo:     p.OrderNo,
		Description: "餐饮消费" + p.OrderNo,
		AmountCents: totalCents,
		ClientIP:    c.ClientIP(),
	})
	if err != nil {
		logger.Warnf("[pay] 发起支付失败 订单%s 渠道%s: %v", p.OrderNo, p.Channel, err)
		fail(c, "发起支付失败: "+err.Error())
		return
	}

	// 落支付流水(待支付)。同一订单同一渠道重复发起时,复用旧流水记录状态。
	// 落库失败绝不能再把二维码给顾客:否则顾客付款后回调找不到待支付流水可回写,
	// 订单永远停在未支付。这里 fail-closed,并尝试关掉刚生成的渠道收款码。
	if err := service.CreatePayment(p.OrderNo, p.Channel, totalCents, result.CodeURL); err != nil {
		logger.Errorf("[pay][告警] 支付流水落库失败,拒绝返回二维码 订单%s 渠道%s: %v", p.OrderNo, p.Channel, err)
		// 渠道收款码已生成,若不关单顾客仍可扫码付款,只能尽力关单降低掉单风险;
		// 关单失败仅告警(渠道侧单可能仍可付,需人工关注)。
		if closeErr := provider.Close(p.OrderNo); closeErr != nil {
			logger.Warnf("[pay][告警] 支付流水落库失败后关单也失败(存在掉单风险,请人工关注) 订单%s 渠道%s: %v", p.OrderNo, p.Channel, closeErr)
		}
		fail(c, "支付发起异常，请稍后重试")
		return
	}
	logger.Infof("[pay] 已发起支付 订单%s 渠道%s 金额%d分", p.OrderNo, p.Channel, totalCents)

	ok(c, gin.H{
		"channel": p.Channel,
		"codeUrl": result.CodeURL,
		"orderNo": p.OrderNo,
		"amount":  totalCents,
	})
}

// PayNotifyWxpay 微信支付结果回调(验签 + 解密 + 回写)。
func PayNotifyWxpay(c *gin.Context) {
	provider, _ := pay.Get(pay.ChannelWxpay)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, `{"code":"FAIL"}`)
		return
	}
	headers := map[string]string{
		"Wechatpay-Timestamp": c.GetHeader("Wechatpay-Timestamp"),
		"Wechatpay-Nonce":     c.GetHeader("Wechatpay-Nonce"),
		"Wechatpay-Signature": c.GetHeader("Wechatpay-Signature"),
		"Wechatpay-Serial":    c.GetHeader("Wechatpay-Serial"),
	}
	notify, err := provider.VerifyNotify(headers, body)
	if err != nil {
		logger.Warnf("[pay] 微信回调验签失败: %v", err)
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
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	notify, err := provider.VerifyNotify(nil, body)
	if err != nil {
		logger.Warnf("[pay] 支付宝回调验签失败: %v", err)
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
	if !notify.Success {
		// 非成功通知(如关闭/退款通知)无需处理订单支付状态,直接应答成功。
		return true
	}
	orderID, _, _, totalCents, err := service.GetOrderAmount(notify.OrderNo)
	if err != nil {
		// 必须区分「订单不存在」与「DB 瞬时故障」:
		// 前者是脏数据/历史回调,应答成功避免平台无谓重试;
		// 后者若也应答成功,渠道不再重试,顾客已付款却永不入账(掉单不可恢复)。
		if errors.Is(err, sql.ErrNoRows) {
			logger.Warnf("[pay] 回调订单不存在,应答成功: %s", notify.OrderNo)
			return true
		}
		logger.Errorf("[pay][告警] 回调查询订单失败,应答失败待渠道重试 订单%s: %v", notify.OrderNo, err)
		return false
	}
	// 金额一致性:回调金额必须与订单金额一致,否则挂起人工处理,绝不自动入账。
	// 同时落一条退款流水标记(状态=失败、原因写明差额),让「钱收到了但入不了账」
	// 在退款记录里可见、可追 —— 只留日志的话,事后根本发现不了顾客多付/少付。
	if notify.AmountCents != totalCents {
		logger.Warnf("[pay][告警] 金额不一致 订单%s 期望%d分 实际%d分", notify.OrderNo, totalCents, notify.AmountCents)
		service.RecordMismatchedPayment(orderID, notify.OrderNo, channel, notify.AmountCents, totalCents, notify.ChannelTradeNo)
		return true
	}
	// 回写失败(DB 故障)必须应答 FAIL 让渠道重试:首读成功不代表回写一定成功,
	// 若折叠成成功应答,顾客已付款却永不入账(掉单不可恢复)。
	if !applyPaymentSuccess(orderID, notify.OrderNo, channel, notify.AmountCents, notify.ChannelTradeNo) {
		logger.Errorf("[pay][告警] 支付回写失败,应答失败待渠道重试 订单%s", notify.OrderNo)
		return false
	}
	return true
}

// applyPaymentSuccess 支付成功回写(回调与主动查单补单共用):
// 幂等回写订单与流水;若订单已由其它渠道/线下收款,则为重复支付,自动原路退回。
// 返回 false 表示回写遭遇 DB 故障,调用方(回调)应应答 FAIL 让渠道重试;
// 主动查单路径可忽略返回值(本次不入账,下次轮询再试)。
func applyPaymentSuccess(orderID int, orderNo, channel string, amountCents int64, channelTradeNo string) bool {
	res := service.ApplyPaymentSuccess(orderNo, channel, channelTradeNo, channelName(channel), amountCents)
	if res.Failed {
		return false
	}
	if res.Duplicate {
		autoRefundDuplicatePayment(orderID, orderNo, channel, amountCents, channelTradeNo)
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
func autoRefundDuplicatePayment(orderID int, orderNo, channel string, amountCents int64, channelTradeNo string) {
	// 查重:该渠道已有未终态的重复支付退款单(通知重发场景),复用而非重复发起,
	// 否则同一笔支付会被渠道二次退款(商户资金损失)。
	if refundID, refundNo := service.HasPendingDuplicateRefund(orderNo, channel); refundID > 0 {
		logger.Infof("[pay] 重复支付退款已受理过,跳过重复发起 订单%s 渠道%s 退款单%s", orderNo, channel, refundNo)
		return
	}
	logger.Warnf("[pay][告警] 检测到重复支付,自动原路退回 订单%s 渠道%s 金额%d分 流水号%s", orderNo, channel, amountCents, channelTradeNo)
	refundNo, refundID, paymentID, totalCents, err := service.RecordDuplicateRefund(orderID, orderNo, channel, amountCents, channelTradeNo)
	if err != nil {
		return
	}
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		logger.Warnf("[pay][告警] 渠道未配置,重复支付无法自动退回,请人工处理 退款单%s", refundNo)
		return
	}
	result, err := provider.Refund(orderNo, refundNo, amountCents, totalCents)
	if err != nil || !result.Success {
		_ = service.MarkRefund(refundID, po.RefundStatusFail, "", "重复支付自动退回失败,请人工处理")
		logger.Warnf("[pay][告警] 重复支付自动退回失败,请人工处理 订单%s 退款单%s: %v", orderNo, refundNo, err)
		return
	}
	if refundSettled(channel) {
		_ = service.MarkRefund(refundID, po.RefundStatusSuccess, result.RefundNo, "")
		_ = service.MarkPaymentRefunded(paymentID)
		logger.Infof("[pay] 重复支付已原路退回 订单%s 渠道%s 金额%d分", orderNo, channel, amountCents)
		return
	}
	logger.Infof("[pay] 重复支付退款已受理,等待渠道到账 订单%s 退款单%s", orderNo, refundNo)
}

// settleQueryResult 处理主动查单的支付结果:金额一致才补单入账,不一致/非正金额
// 只留痕人工核对(与通知路径 handlePayNotify 的金额校验同一口径)。主动查单路径
// 需 mock 渠道不易走 handler 级测试,抽成独立函数便于单测钉住「少付入账」防线。
func settleQueryResult(orderID int, orderNo, channel string, q pay.PayQuery) {
	// 补单前重读订单最新应收金额,覆盖查单请求发出到返回之间的改价窗口。
	_, _, _, totalCents, err := service.GetOrderAmount(orderNo)
	if err != nil {
		logger.Warnf("[pay][告警] 主动查单确认已支付但读取订单金额失败,暂不补单 订单%s: %v", orderNo, err)
		return
	}
	// 金额不一致(含 <=0,如支付宝金额解析失败返回 0)绝不入账,只留痕人工核对,
	// 否则「生成旧码→加菜涨价→用旧码付旧金额→前端轮询补单」会造成少付入账。
	if q.PaidAmount <= 0 || q.PaidAmount != totalCents {
		logger.Warnf("[pay][告警] 主动查单金额不一致 订单%s 期望%d分 实际%d分", orderNo, totalCents, q.PaidAmount)
		service.RecordMismatchedPayment(orderID, orderNo, channel, q.PaidAmount, totalCents, q.ChannelTradeNo)
		return
	}
	applyPaymentSuccess(orderID, orderNo, channel, q.PaidAmount, q.ChannelTradeNo)
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
	// 优先返回订单当前支付状态。
	orderID, payStatus, _, _, err := service.GetOrderAmount(orderNo)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	// 未支付时尝试主动查单补单。
	if payStatus == 0 {
		// 找出该订单最近的支付渠道。
		ch, err := service.LastPaymentChannel(orderNo)
		if err == nil && ch != "" {
			if provider, e := pay.Get(ch); e == nil && provider.Enabled() {
				// 频控只作用于「渠道主动查单」这一步,不阻塞订单状态查询返回:
				// 冷却期内直接返回 DB 状态,避免公开接口被任意 orderNo 刷渠道 API 配额。
				if !payQueryAllowed(orderNo) {
					ok(c, gin.H{"orderNo": orderNo, "payStatus": payStatus})
					return
				}
				if q, e := provider.Query(orderNo); e == nil && q.Success {
					settleQueryResult(orderID, orderNo, ch, q)
					_, payStatus, _, _, _ = service.GetOrderAmount(orderNo)
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
	orderNo, payStatus, totalCents, err := service.GetRefundOrderInfo(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if payStatus != 1 {
		fail(c, "订单未支付，无法在线退款")
		return
	}
	// 找支付流水:已支付或(部分)已退款的流水都可用于继续退款,避免部分退款后剩余金额退不掉。
	paymentID, channel, err := service.LastPaidPayment(orderNo)
	if err != nil {
		fail(c, "未找到在线支付流水，该订单为线下收款，请人工退款")
		return
	}
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		fail(c, "支付渠道未配置，无法在线退款")
		return
	}

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
	refundNo, refundID, refundCents, err := service.CreateRefund(p.OrderID, orderNo, paymentID, channel, totalCents, p.Amount, p.Reason, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}

	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("发起退款 ¥%.2f（%s），原因：%s", po.ToYuan(refundCents), channelName(channel), reason))

	result, err := provider.Refund(orderNo, refundNo, refundCents, totalCents)
	if err != nil {
		logger.Warnf("[pay] 渠道退款失败 订单%s 退款单%s 金额%d分: %v", orderNo, refundNo, refundCents, err)
		_ = service.MarkRefund(refundID, po.RefundStatusFail, "", truncateMsg(err.Error()))
		fail(c, "退款失败: "+err.Error())
		return
	}
	// 微信可能返回受理中(PROCESSING),此时不立即入账,由管理端「同步状态」或退款查单补结果。
	if result.Success && refundSettled(channel) {
		if err := service.ApplyRefundSuccess(refundID, paymentID, orderNo, refundCents, result.RefundNo); err != nil {
			// 渠道钱已退但账务回写失败:不能回复「退款成功」误导商家。
			fail(c, "退款已发起但入账失败，请稍后重试或在退款记录中核对")
			return
		}
		logger.Infof("[pay] 退款成功 订单%s 退款单%s 金额%d分 操作人%s", orderNo, refundNo, refundCents, adminName(c))
		okMsg(c, "退款成功")
		return
	}
	ok(c, gin.H{"refundId": refundID, "status": po.RefundStatusProcessing, "msg": "退款已受理，等待渠道到账"})
}

// refundSettled 判断渠道退款结果是否为「已到账」(支付宝同步到账,微信多为异步)。
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
			service.ApplyDuplicateRefundSuccess(r.RefundID, r.PaymentID, q.ChannelRefundNo, r.RefundNo)
			ok(c, gin.H{"status": po.RefundStatusSuccess, "msg": "重复支付退回已到账"})
			return
		}
		if err := service.ApplyRefundSuccess(r.RefundID, r.PaymentID, r.OrderNo, r.Amount, q.ChannelRefundNo); err != nil {
			ok(c, gin.H{"status": po.RefundStatusProcessing, "msg": "渠道已退款但账务入账失败，请稍后重试"})
			return
		}
		ok(c, gin.H{"status": po.RefundStatusSuccess, "msg": "退款已到账"})
	case pay.RefundProcessing:
		ok(c, gin.H{"status": po.RefundStatusProcessing, "msg": "退款处理中，请稍后重试"})
	default:
		_ = service.MarkRefund(r.RefundID, po.RefundStatusFail, q.ChannelRefundNo, "渠道返回退款失败")
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
func closeOnlinePayment(orderID int) {
	orderNo, err := service.GetOrderNoByID(orderID)
	if err != nil {
		return
	}
	closePendingPayments(orderNo, "")
}

// closePendingPayments 关闭订单在 exceptChannel 之外所有渠道的待支付单
// (exceptChannel 为空串时关闭全部渠道)。
//
// 注意不按订单 pay_status 过滤:收款/结账后订单已置已支付,但仍需关掉
// 顾客手里还开着的二维码,否则就是重复支付的入口。
func closePendingPayments(orderNo, exceptChannel string) {
	for _, p := range service.ListPendingPayments(orderNo, exceptChannel) {
		provider, err := pay.Get(p.Channel)
		if err != nil || !provider.Enabled() {
			continue
		}
		if err := provider.Close(orderNo); err != nil {
			logger.Warnf("[pay][告警] 关闭渠道支付单失败(存在重复支付风险,请人工关注) 订单%s 渠道%s: %v", orderNo, p.Channel, err)
			continue
		}
		_ = service.MarkPaymentClosed(p.ID)
	}
}
