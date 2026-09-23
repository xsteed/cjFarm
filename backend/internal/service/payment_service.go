package service

import (
	"database/sql"
	"fmt"

	"dining-system/infra/logger"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============ 支付域用例 ============
//
// 本文件承载支付/退款的业务编排:支付状态机、退款流程与金额校验下沉到 service。
// 数据访问统一走 dao;不依赖 handler/print/pay。
// 渠道抽象仍留在 pay 包,由 handler 编排调用(service 不得 import pay)。
// 配置读写复用 print_service.go 的 GetSetting/SetSetting。

// PendingPayment 待关闭的在线支付流水(渠道侧关单由 handler 编排)。
type PendingPayment struct {
	ID      int
	Channel string
}

// GetOrderAmount 按订单号查询订单支付金额与状态。
func GetOrderAmount(orderNo string) (orderID, payStatus, orderStatus int, totalCents int64, err error) {
	return dao.GetOrderAmount(orderNo)
}

// ValidateOrderPayable 校验订单是否可发起在线支付,返回订单ID与应收金额(分)。
// 状态校验与金额异常判断下沉到 service,handler 不再散落业务判断。
func ValidateOrderPayable(orderNo string) (orderID int, totalCents int64, err error) {
	orderID, payStatus, orderStatus, totalCents, err := dao.GetOrderAmount(orderNo)
	if err != nil {
		return 0, 0, fmt.Errorf("订单不存在")
	}
	if payStatus == 1 {
		return 0, 0, fmt.Errorf("订单已支付")
	}
	if !ActiveStatus(orderStatus) {
		return 0, 0, fmt.Errorf("当前订单状态不可支付")
	}
	if totalCents <= 0 {
		logger.Warnf("[pay][告警] 订单金额异常 拒绝发起支付 订单%s 金额%d分", orderNo, totalCents)
		return 0, 0, fmt.Errorf("订单金额异常，请联系收银员")
	}
	return orderID, totalCents, nil
}

// CreatePayment 写入一笔支付流水(待支付)。同一订单同一渠道重复发起时复用旧流水。
func CreatePayment(orderNo, channel string, amountCents int64, prepayID string) error {
	return dao.InsertPayment(orderNo, channel, amountCents, prepayID)
}

// GetPayment 查询某订单某渠道最近一条支付流水。
func GetPayment(orderNo, channel string) (paymentID, status int, amountCents int64, err error) {
	return dao.GetPayment(orderNo, channel)
}

// LastPaymentChannel 查询订单最近一条支付流水的渠道。
func LastPaymentChannel(orderNo string) (string, error) {
	return dao.LastPaymentChannel(orderNo)
}

// GetOrderNoByID 按订单 ID 查询订单号。
func GetOrderNoByID(orderID int) (string, error) {
	return dao.GetOrderNoByID(orderID)
}

// ListPendingPayments 查询订单除 exceptChannel 外所有渠道的待支付流水。
func ListPendingPayments(orderNo, exceptChannel string) []PendingPayment {
	list := dao.ListPendingPayments(orderNo, exceptChannel)
	out := make([]PendingPayment, 0, len(list))
	for _, p := range list {
		out = append(out, PendingPayment{ID: p.ID, Channel: p.Channel})
	}
	return out
}

// MarkPaymentClosed 关闭支付流水(渠道关单成功后调用)。
func MarkPaymentClosed(paymentID int) error {
	return dao.MarkPaymentStatus(paymentID, dao.PayStatusClosed)
}

// MarkPaymentRefunded 将支付流水置为已退款。
func MarkPaymentRefunded(paymentID int) error {
	return dao.MarkPaymentStatus(paymentID, dao.PayStatusRefunded)
}

// PaymentSuccessResult 支付成功回写结果。
type PaymentSuccessResult struct {
	// Duplicate 表示订单已由其它渠道/线下收款,本笔为重复支付,
	// 由 handler 编排渠道原路退回。
	Duplicate bool
	// Failed 表示回写过程遇到 DB 故障(而非业务状态),handler 必须应答 FAIL
	// 让渠道重试 —— 否则顾客已付款但订单未入账,掉单不可恢复。
	Failed bool
}

// ApplyPaymentSuccess 支付成功回写(回调与主动查单补单共用):
// 幂等回写订单与流水;若订单已支付则为重复支付,返回 Duplicate 供 handler 原路退回;
// 若回写遭遇 DB 故障返回 Failed,handler 应答 FAIL 让渠道重试。
func ApplyPaymentSuccess(orderNo, channel, channelTradeNo, payType string, amountCents int64) PaymentSuccessResult {
	// 幂等:该渠道流水已置为已支付,说明是同一笔支付的重复回调,直接成功应答。
	if _, flowStatus, _, err := dao.GetPayment(orderNo, channel); err == nil && flowStatus == dao.PayStatusPaid {
		return PaymentSuccessResult{}
	}
	_, payStatus, _, _, err := dao.GetOrderAmount(orderNo)
	if err != nil {
		// DB 故障与「订单不存在」不同:后者由 handlePayNotify 首读区分,这里只可能是
		// 首读之后的瞬时故障,必须让渠道重试,不能折叠成成功应答。
		logger.Errorf("[pay][告警] 回写前查询订单失败,应答失败待渠道重试 订单%s: %v", orderNo, err)
		return PaymentSuccessResult{Failed: true}
	}
	// 订单已支付(其它渠道/线下先到):这笔是重复支付,由 handler 自动原路退回。
	if payStatus == 1 {
		// 先如实把本渠道流水置为已支付:同交易号通知重发时会命中上方的幂等短路,
		// 不会再次登记退款 —— 否则微信退款受理中 ack 丢失,重发的通知会触发第二笔退款。
		if _, e := dao.MarkPaymentPaid(orderNo, channel, channelTradeNo); e != nil {
			logger.Warnf("[pay][告警] 重复支付流水置已支付失败,应答失败待重试 订单%s 渠道%s: %v", orderNo, channel, e)
			return PaymentSuccessResult{Failed: true}
		}
		return PaymentSuccessResult{Duplicate: true}
	}
	changed, err := dao.MarkPaymentPaid(orderNo, channel, channelTradeNo)
	if err != nil {
		logger.Errorf("[pay][告警] 支付流水回写失败,应答失败待渠道重试 订单%s: %v", orderNo, err)
		return PaymentSuccessResult{Failed: true}
	}
	if changed {
		// 回写订单支付状态;条件 UPDATE WHERE pay_status=0 未命中(orderChanged=false)
		// 说明订单在 MarkPaymentPaid 之后的并发窗口内已被其它渠道/线下收款置为已支付,
		// 本笔是重复支付,交给 handler 自动原路退回 —— 否则会出现「钱收了但没入本笔账」。
		orderChanged, err := dao.MarkOrderPaidByChannel(orderNo, payType, channelTradeNo, channel)
		if err != nil {
			logger.Errorf("[pay][告警] 订单支付状态回写失败(流水已置已支付,订单可能未入账,请人工核对) 订单%s 渠道%s 流水号%s: %v", orderNo, channel, channelTradeNo, err)
			return PaymentSuccessResult{Failed: true}
		}
		if !orderChanged {
			logger.Warnf("[pay][告警] 订单已被其它渠道/线下收款,本笔为重复支付 订单%s 渠道%s 流水号%s", orderNo, channel, channelTradeNo)
			return PaymentSuccessResult{Duplicate: true}
		}
		logger.Infof("[pay] 支付成功 订单%s 渠道%s 金额%d分 渠道流水号%s", orderNo, channel, amountCents, channelTradeNo)
	} else {
		// 并发重复回调:同渠道两个通知同时到达,先到者已把流水置为已支付,后到者
		// 条件 UPDATE 命中 0 行 —— 复核流水状态后再告警,避免误导人工核对。
		if _, fs, _, e := dao.GetPayment(orderNo, channel); e == nil && fs == dao.PayStatusPaid {
			return PaymentSuccessResult{}
		}
		// 流水缺失/已关闭却收到了成功回调:钱到了但订单未入账,必须留痕人工核对。
		logger.Warnf("[pay][告警] 支付成功但无待支付流水可回写(请人工核对) 订单%s 渠道%s 流水号%s", orderNo, channel, channelTradeNo)
	}
	return PaymentSuccessResult{}
}

// RecordMismatchedPayment 金额不一致的支付留痕:写一条标记为失败的退款流水,
// 让商家在退款记录里能看到「渠道收了钱、系统没入账」,据此人工对账处理。
func RecordMismatchedPayment(orderID int, orderNo, channel string, paidCents, expectCents int64, channelTradeNo string) {
	reason := fmt.Sprintf("金额不一致:渠道实收¥%.2f/系统应收¥%.2f,未入账,请人工核对", po.ToYuan(paidCents), po.ToYuan(expectCents))
	if _, err := dao.InsertRefund(po.Refund{
		OrderID: orderID, OrderNo: orderNo, PaymentID: 0,
		RefundNo: "M" + GenOrderNo()[1:], Channel: channel,
		ChannelRefundNo: channelTradeNo, Amount: paidCents,
		Status: po.RefundStatusFail, Reason: reason, Operator: "system",
	}); err != nil {
		logger.Warnf("[pay][告警] 金额不一致留痕失败 订单%s: %v", orderNo, err)
	}
}

// HasPendingDuplicateRefund 该订单某渠道是否已存在未终态的重复支付退款单。
// 供 handler 在发起自动原路退回前查重,避免通知重发导致同一笔支付被退两次。
func HasPendingDuplicateRefund(orderNo, channel string) (refundID int, refundNo string) {
	refundID, refundNo, err := dao.GetPendingDuplicateRefund(orderNo, channel)
	if err != nil {
		logger.Warnf("[pay][告警] 重复退款查重失败,按无记录处理 订单%s 渠道%s: %v", orderNo, channel, err)
		return 0, ""
	}
	return refundID, refundNo
}

// RecordDuplicateRefund 登记重复支付自动原路退回的退款单(处理中)。
// 返回退款单号/ID 与订单金额,供 handler 调渠道退款;登记失败时告警并返回 error。
func RecordDuplicateRefund(orderID int, orderNo, channel string, amountCents int64, channelTradeNo string) (refundNo string, refundID, paymentID int, totalCents int64, err error) {
	paymentID, _, _, _ = dao.GetPayment(orderNo, channel)
	_, _, _, totalCents, _ = dao.GetOrderAmount(orderNo)
	refundNo = "R" + GenOrderNo()[1:]
	refundID, err = dao.InsertRefund(po.Refund{
		OrderID: orderID, OrderNo: orderNo, PaymentID: paymentID,
		RefundNo: refundNo, Channel: channel, ChannelRefundNo: channelTradeNo,
		Amount: amountCents, Status: po.RefundStatusProcessing,
		Reason: "重复支付自动原路退回", Operator: "system", IsDuplicate: 1,
	})
	if err != nil {
		logger.Warnf("[pay][告警] 重复支付退款单登记失败(请人工处理) 订单%s: %v", orderNo, err)
		return "", 0, paymentID, totalCents, err
	}
	return refundNo, refundID, paymentID, totalCents, nil
}

// GetRefundOrderInfo 查询退款所需的订单支付信息。
func GetRefundOrderInfo(orderID int) (orderNo string, payStatus int, totalCents int64, err error) {
	return dao.GetRefundOrderInfo(orderID)
}

// LastPaidPayment 查询订单最近一条已支付或(部分)已退款的支付流水。
func LastPaidPayment(orderNo string) (paymentID int, channel string, err error) {
	return dao.LastPaidPayment(orderNo)
}

// calcRefundCents 根据订单金额与已退(含处理中)额度计算本次退款金额(分)。
// 纯函数:额度校验规则只有一份,RefundAmount 与 CreateRefund 共用,避免两处口径漂移。
func calcRefundCents(totalCents, refunded int64, amountYuan float64) (int64, error) {
	remain := totalCents - refunded
	if remain <= 0 {
		return 0, fmt.Errorf("该订单已全额退款")
	}
	var refundCents int64
	if amountYuan <= 0 {
		refundCents = remain // 全额(剩余)退款
	} else {
		refundCents = po.ToCents(amountYuan)
	}
	if refundCents <= 0 {
		return 0, fmt.Errorf("退款金额必须大于 0")
	}
	if refundCents > remain {
		return 0, fmt.Errorf("退款金额超出可退金额 ¥%.2f", po.ToYuan(remain))
	}
	return refundCents, nil
}

// RefundAmount 校验可退额度并计算本次退款金额(分)。
// 可退额度 = 订单金额 - (已成功 + 处理中)退款金额;金额校验下沉到 service,
// 保证并发退款基于同一把锁时仍走同一套 fail-closed 校验。
func RefundAmount(orderNo string, totalCents int64, amountYuan float64) (int64, error) {
	refunded, err := dao.SumRefundedInclPending(orderNo)
	if err != nil {
		logger.Warnf("[pay] 可退额度查询失败,拒绝退款 订单%s: %v", orderNo, err)
		return 0, fmt.Errorf("退款额度查询失败，请稍后重试")
	}
	return calcRefundCents(totalCents, refunded, amountYuan)
}

// CreateRefund 在事务内完成「锁订单行 → 重查已退(含处理中) → 校验 → 落退款单」,
// 返回退款单号、退款单 ID 与本次退款金额(分)。
//
// 为什么需要 DB 事务 + 行锁,而不是只依赖 handler 的 refundMu:
// refundMu 是单实例进程锁,只能挡住同一进程内的并发;将来多实例部署时各实例锁互不相通。
// 这里用 dao.TouchOrderTx 对订单行做自赋值 UPDATE 取行写锁,把同一订单的额度校验与
// 退款单落库在 DB 行上串行化,消除 check-then-act 竞态 —— refundMu 仍保留为第一道防线。
func CreateRefund(orderID int, orderNo string, paymentID int, channel string, totalCents int64, amountYuan float64, reason, operator string) (refundNo string, refundID int, refundCents int64, err error) {
	refundNo = "R" + GenOrderNo()[1:]
	err = store.WithTx(func(tx *sql.Tx) error {
		// 先取订单行写锁,使后续「重查已退额度」读到的是串行化后的最新值。
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		refunded, err := dao.SumRefundedInclPendingTx(tx, orderNo)
		if err != nil {
			return err
		}
		refundCents, err = calcRefundCents(totalCents, refunded, amountYuan)
		if err != nil {
			return err
		}
		refundID, err = dao.InsertRefundTx(tx, po.Refund{
			OrderID:   orderID,
			OrderNo:   orderNo,
			PaymentID: paymentID,
			RefundNo:  refundNo,
			Channel:   channel,
			Amount:    refundCents,
			Status:    po.RefundStatusProcessing,
			Reason:    reason,
			Operator:  operator,
		})
		return err
	})
	if err != nil {
		logger.Warnf("[pay] 退款登记事务失败 订单%s: %v", orderNo, err)
		return refundNo, 0, 0, err
	}
	return refundNo, refundID, refundCents, nil
}

// InsertRefund 登记一笔退款单。
func InsertRefund(r po.Refund) (int, error) {
	return dao.InsertRefund(r)
}

// GetRefund 按主键读取退款单。
func GetRefund(refundID int) (po.Refund, error) {
	return dao.GetRefund(refundID)
}

// ListRefunds 查询某订单的退款记录。
func ListRefunds(orderID int) ([]po.Refund, error) {
	return dao.ListRefunds(orderID)
}

// MarkRefund 更新退款单状态(成功/失败)。
func MarkRefund(refundID, status int, channelRefundNo, failReason string) error {
	return dao.MarkRefund(refundID, status, channelRefundNo, failReason)
}

// ApplyRefundSuccess 退款成功回写:退款单置成功 + 订单累计退款金额 + 同步支付流水状态。
//
// 「置成功 + 累计退款额 + 扣实收」收进同一事务:退款单一旦置成功,渠道侧钱已退,
// 订单账务(退款额/实收)必须同生共死。任何一步失败都整体回滚,退款单回到非成功态,
// 渠道回调/查单重试时还能重新入账 —— 而不是留下 refund_amount/paid_amount 三者不一致。
//
// 幂等语义保持不变:置成功是条件更新(RowsAffected),并发回调只有第一次真正入账,
// 后续回调 changed=false 直接跳过,不重复累加。
//
// 返回 error 供调用方据实应答:事务失败(渠道钱已退但账务未跟上)时不能向商家
// 回复「退款成功/已到账」,应提示重试或人工核对。
func ApplyRefundSuccess(refundID, paymentID int, orderNo string, refundCents int64, channelRefundNo string) error {
	applied := false
	err := store.WithTx(func(tx *sql.Tx) error {
		changed, err := dao.MarkRefundSuccessOnceTx(tx, refundID, channelRefundNo)
		if err != nil {
			return err
		}
		if !changed {
			return nil // 已入账过,幂等跳过
		}
		if err := dao.AddOrderRefundedTx(tx, orderNo, refundCents); err != nil {
			return err
		}
		// 同步扣减实收金额(营业额口径),退款后不再算作营收;下限为 0。
		if err := dao.DeductPaidAmountTx(tx, orderNo, refundCents); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		// 事务失败已回滚,退款单未置成功,重试是安全的;但渠道钱已退,必须高可见度告警。
		logger.Errorf("[pay][告警] 退款入账事务失败(已回滚,渠道已退款,请关注重试/人工核对) 退款单%d 订单%s 金额%d分: %v", refundID, orderNo, refundCents, err)
		return err
	}
	if !applied {
		return nil // 已入账过,幂等跳过
	}
	// 订单全额退完时,支付流水标记为已退款(非账务关键,放事务外)。
	if _, _, _, totalCents, err := dao.GetOrderAmount(orderNo); err == nil {
		refunded, sumErr := dao.SumRefundedWithErr(orderNo)
		if sumErr != nil {
			// 查询失败时宁可跳过置位,也不能误按 0 判断导致全退标记永不置位;留告警等下次重试。
			logger.Warnf("[pay] 已退款金额查询失败,跳过流水全退置位 订单%s: %v", orderNo, sumErr)
		} else if refunded >= totalCents {
			_ = dao.MarkPaymentStatus(paymentID, dao.PayStatusRefunded)
		}
	}
	return nil
}

// ApplyDuplicateRefundSuccess 重复支付退回的到账回写:退款单置成功 + 支付流水置已退款。
// 重复退回的钱从未计入订单营收,因此只动退款单与支付流水,不累加订单退款额度。
func ApplyDuplicateRefundSuccess(refundID, paymentID int, channelRefundNo, refundNo string) {
	if changed, _ := dao.MarkRefundSuccessOnce(refundID, channelRefundNo); changed {
		_ = dao.MarkPaymentStatus(paymentID, dao.PayStatusRefunded)
		logger.Infof("[pay] 重复支付退回到账 退款单%s", refundNo)
	}
}
