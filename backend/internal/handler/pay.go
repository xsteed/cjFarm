package handler

import (
	"dining-system/internal/logger"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/pay"
	"dining-system/internal/service"
	"dining-system/internal/store"
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

	orderID, payStatus, orderStatus, totalCents, err := store.GetOrderAmount(p.OrderNo)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if payStatus == 1 {
		fail(c, "订单已支付")
		return
	}
	if !service.ActiveStatus(orderStatus) {
		fail(c, "当前订单状态不可支付")
		return
	}
	if totalCents <= 0 {
		logger.Warnf("[pay][告警] 订单金额异常 拒绝发起支付 订单%s 金额%d分", p.OrderNo, totalCents)
		fail(c, "订单金额异常，请联系收银员")
		return
	}

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
	_ = store.InsertPayment(p.OrderNo, p.Channel, totalCents, result.CodeURL)
	_ = orderID
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
		return
	}
	c.String(http.StatusOK, "success")
}

// handlePayNotify 回调公共处理:金额一致性校验 + 幂等回写。
// 返回 false 表示已自行写响应(金额不一致等场景),调用方不应再写。
func handlePayNotify(c *gin.Context, channel string, notify pay.PayNotify) bool {
	if !notify.Success {
		// 非成功通知(如关闭/退款通知)无需处理订单支付状态,直接应答成功。
		return true
	}
	_, _, _, totalCents, err := store.GetOrderAmount(notify.OrderNo)
	if err != nil {
		logger.Warnf("[pay] 回调订单不存在: %s", notify.OrderNo)
		return true // 应答成功避免平台无谓重试
	}
	// 金额一致性:回调金额必须与订单金额一致,否则挂起人工处理,绝不自动入账。
	if notify.AmountCents != totalCents {
		logger.Warnf("[pay][告警] 金额不一致 订单%s 期望%d分 实际%d分", notify.OrderNo, totalCents, notify.AmountCents)
		return true
	}
	// 幂等:仅首次(待支付→已支付)更新;重复回调直接返回成功。
	changed, err := store.MarkPaymentPaid(notify.OrderNo, channel, notify.ChannelTradeNo)
	if err != nil {
		logger.Warnf("[pay] 流水更新失败: %v", err)
		return true
	}
	if changed {
		_, _ = store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, transaction_id=?, pay_channel=?, update_time=?
			WHERE order_no=? AND pay_status=0`,
			channelName(channel), store.Now(), notify.ChannelTradeNo, channel, store.Now(), notify.OrderNo)
		logger.Infof("[pay] 支付成功 订单%s 渠道%s 金额%d分 渠道流水号%s", notify.OrderNo, channel, totalCents, notify.ChannelTradeNo)
	}
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
	_, payStatus, _, _, err := store.GetOrderAmount(orderNo)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	// 未支付时尝试主动查单补单。
	if payStatus == 0 {
		// 找出该订单最近的支付渠道。
		row := store.DB.QueryRow(`SELECT channel FROM tb_payment WHERE order_no=? ORDER BY payment_id DESC LIMIT 1`, orderNo)
		var ch string
		if err := row.Scan(&ch); err == nil && ch != "" {
			if provider, e := pay.Get(ch); e == nil && provider.Enabled() {
				if q, e := provider.Query(orderNo); e == nil && q.Success {
					// 主动查单确认已支付 → 补单。
					if changed, _ := store.MarkPaymentPaid(orderNo, ch, q.ChannelTradeNo); changed {
						_, _ = store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, transaction_id=?, pay_channel=?, update_time=?
							WHERE order_no=? AND pay_status=0`,
							channelName(ch), store.Now(), q.ChannelTradeNo, ch, store.Now(), orderNo)
						logger.Infof("[pay] 主动查单补单成功 订单%s 渠道%s 渠道流水号%s", orderNo, ch, q.ChannelTradeNo)
					}
					payStatus = 1
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
	var orderNo string
	var payStatus int
	var totalCents int64
	if err := store.DB.QueryRow(`SELECT order_no, pay_status, total_amount FROM tb_order WHERE order_id=?`, p.OrderID).
		Scan(&orderNo, &payStatus, &totalCents); err != nil {
		fail(c, "订单不存在")
		return
	}
	if payStatus != 1 {
		fail(c, "订单未支付，无法在线退款")
		return
	}
	// 找支付流水:已支付或(部分)已退款的流水都可用于继续退款,避免部分退款后剩余金额退不掉。
	var paymentID int
	var channel string
	if err := store.DB.QueryRow(`SELECT payment_id, channel FROM tb_payment
		WHERE order_no=? AND status IN (?,?) ORDER BY payment_id DESC LIMIT 1`,
		orderNo, store.PayStatusPaid, store.PayStatusRefunded).Scan(&paymentID, &channel); err != nil {
		fail(c, "未找到在线支付流水，该订单为线下收款，请人工退款")
		return
	}
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		fail(c, "支付渠道未配置，无法在线退款")
		return
	}

	// 可退金额 = 订单金额 - 已成功退款金额。
	refunded := store.SumRefunded(orderNo)
	remain := totalCents - refunded
	if remain <= 0 {
		fail(c, "该订单已全额退款")
		return
	}
	var refundCents int64
	if p.Amount <= 0 {
		refundCents = remain // 全额(剩余)退款
	} else {
		refundCents = model.ToCents(p.Amount)
	}
	if refundCents <= 0 {
		fail(c, "退款金额必须大于 0")
		return
	}
	if refundCents > remain {
		fail(c, fmt.Sprintf("退款金额超出可退金额 ¥%.2f", model.ToYuan(remain)))
		return
	}
	if len(p.Reason) > 60 {
		p.Reason = string([]rune(p.Reason)[:60])
	}

	// 审计摘要写在落库前:万一渠道退款失败,日志里仍能看到「想退多少、为什么退」,
	// 失败原因由中间件从响应里补上(状态记为失败)。
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "未填写"
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("发起退款 ¥%.2f（%s），原因：%s", model.ToYuan(refundCents), channelName(channel), reason))

	refundNo := "R" + service.GenOrderNo()[1:]
	refundID, err := store.InsertRefund(store.Refund{
		OrderID:   p.OrderID,
		OrderNo:   orderNo,
		PaymentID: paymentID,
		RefundNo:  refundNo,
		Channel:   channel,
		Amount:    refundCents,
		Status:    store.RefundStatusProcessing,
		Reason:    p.Reason,
		Operator:  adminName(c),
	})
	if err != nil {
		logger.Warnf("[pay] 退款单创建失败 订单%s: %v", orderNo, err)
		fail(c, "退款单创建失败: "+err.Error())
		return
	}

	result, err := provider.Refund(orderNo, refundNo, refundCents, totalCents)
	if err != nil {
		logger.Warnf("[pay] 渠道退款失败 订单%s 退款单%s 金额%d分: %v", orderNo, refundNo, refundCents, err)
		_ = store.MarkRefund(refundID, store.RefundStatusFail, "", truncateMsg(err.Error()))
		fail(c, "退款失败: "+err.Error())
		return
	}
	// 微信可能返回受理中(PROCESSING),此时不立即入账,由管理端「同步状态」或退款查单补结果。
	if result.Success && refundSettled(channel) {
		applyRefundSuccess(refundID, p.OrderID, paymentID, orderNo, refundCents, result.RefundNo)
		logger.Infof("[pay] 退款成功 订单%s 退款单%s 金额%d分 操作人%s", orderNo, refundNo, refundCents, adminName(c))
		okMsg(c, "退款成功")
		return
	}
	ok(c, gin.H{"refundId": refundID, "status": store.RefundStatusProcessing, "msg": "退款已受理，等待渠道到账"})
}

// refundSettled 判断渠道退款结果是否为「已到账」(支付宝同步到账,微信多为异步)。
func refundSettled(channel string) bool {
	return channel == pay.ChannelAlipay
}

// applyRefundSuccess 退款成功回写:退款单置成功 + 订单累计退款金额 + 同步支付流水状态。
func applyRefundSuccess(refundID, orderID, paymentID int, orderNo string, refundCents int64, channelRefundNo string) {
	_ = store.MarkRefund(refundID, store.RefundStatusSuccess, channelRefundNo, "")
	_ = store.AddOrderRefunded(orderNo, refundCents)
	// 同步扣减实收金额(营业额口径),退款后不再算作营收;下限为 0,避免超额扣成负数。
	_, _ = store.DB.Exec(`UPDATE tb_order SET paid_amount = CASE
		WHEN COALESCE(paid_amount,0) - ? < 0 THEN 0 ELSE COALESCE(paid_amount,0) - ? END
		WHERE order_no=?`, refundCents, refundCents, orderNo)
	// 订单全额退完时,支付流水标记为已退款。
	var totalCents int64
	if err := store.DB.QueryRow(`SELECT total_amount FROM tb_order WHERE order_no=?`, orderNo).Scan(&totalCents); err == nil {
		if store.SumRefunded(orderNo) >= totalCents {
			_ = store.MarkPaymentStatus(paymentID, store.PayStatusRefunded)
		}
	}
	_ = orderID
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
	r, err := store.GetRefund(p.RefundID)
	if err != nil {
		fail(c, "退款单不存在")
		return
	}
	if r.Status == store.RefundStatusSuccess {
		ok(c, gin.H{"status": store.RefundStatusSuccess, "msg": "已退款"})
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
		applyRefundSuccess(r.RefundID, r.OrderID, r.PaymentID, r.OrderNo, r.Amount, q.ChannelRefundNo)
		ok(c, gin.H{"status": store.RefundStatusSuccess, "msg": "退款已到账"})
	case pay.RefundProcessing:
		ok(c, gin.H{"status": store.RefundStatusProcessing, "msg": "退款处理中，请稍后重试"})
	default:
		_ = store.MarkRefund(r.RefundID, store.RefundStatusFail, q.ChannelRefundNo, "渠道返回退款失败")
		ok(c, gin.H{"status": store.RefundStatusFail, "msg": "退款失败，请在渠道后台核对"})
	}
}

// PayRefundList 查询某订单的退款记录。
func PayRefundList(c *gin.Context) {
	orderID, _ := strconv.Atoi(c.Query("orderId"))
	if orderID == 0 {
		fail(c, "参数错误")
		return
	}
	list, err := store.ListRefunds(orderID)
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
			"amount":          model.ToYuan(r.Amount),
			"status":          r.Status,
			"reason":          r.Reason,
			"operator":        r.Operator,
			"createTime":      r.CreateTime,
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

// closeOnlinePayment 关闭订单未支付的在线支付单。
// 场景:顾客已生成微信/支付宝收款码但未付款时商家取消订单,若不关单,顾客之后仍可扫码付款,
// 造成「钱已收、订单已取消」的对不上账问题。失败只记日志,不阻断取消流程。
func closeOnlinePayment(orderID int) {
	var orderNo, channel string
	var paymentID int
	err := store.DB.QueryRow(`SELECT o.order_no, p.channel, p.payment_id
		FROM tb_order o JOIN tb_payment p ON p.order_no=o.order_no
		WHERE o.order_id=? AND o.pay_status=0 AND p.status=? ORDER BY p.payment_id DESC LIMIT 1`,
		orderID, store.PayStatusPending).Scan(&orderNo, &channel, &paymentID)
	if err != nil {
		return
	}
	provider, err := pay.Get(channel)
	if err != nil || !provider.Enabled() {
		return
	}
	if err := provider.Close(orderNo); err != nil {
		logger.Warnf("[pay][告警] 关闭渠道支付单失败 订单%s 渠道%s: %v", orderNo, channel, err)
		return
	}
	_ = store.MarkPaymentStatus(paymentID, store.PayStatusClosed)
}
