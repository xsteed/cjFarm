package service

import (
	"context"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// payChannelWxpay 测试用的微信渠道标识:与 pay.ChannelWxpay 同值,
// 但 service 包不得 import pay(分层约束),故在测试内声明。
const payChannelWxpay = "wxpay"

// mixedOrder 造一个「余额不够付全款」的订单,返回会员 ID、订单与订单总额(分)。
func mixedOrder(t *testing.T, openid string, walletYuan int64) (int, po.Order, int64) {
	t.Helper()
	id := walletInit(t, openid, walletYuan)
	dishID, specID := customerSvcSpecID(t)
	order, err := CustomerCreateOrder(id, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	o, err := GetOrderAmount(order.OrderNo)
	if err != nil {
		t.Fatalf("查订单失败: %v", err)
	}
	// GetOrderAmount 只填充四个字段,单号要自己补上(下游按 OrderNo 查单)。
	o.OrderNo = order.OrderNo
	if o.TotalAmount <= walletYuan*100 {
		t.Fatalf("用例前提不成立:订单总额 %d 分应大于余额 %d 分", o.TotalAmount, walletYuan*100)
	}
	return id, o, o.TotalAmount
}

// TestMixedPayWalletPlusChannel 混合支付:先扣余额,差额走渠道,回调补齐后订单入账。
func TestMixedPayWalletPlusChannel(t *testing.T) {
	ctx := context.Background()
	id, o, total := mixedOrder(t, "mixed-plus", 10)
	const walletCents = int64(1000)

	prep, err := PrepareMixedPay(ctx, id, o.OrderNo, walletCents)
	if err != nil {
		t.Fatalf("混合支付扣余额失败: %v", err)
	}
	if prep.WalletCents != walletCents || prep.PayCents != total-walletCents {
		t.Fatalf("拆分不符: 余额%d 待付%d(总额%d)", prep.WalletCents, prep.PayCents, total)
	}
	if prep.FirstTime {
		t.Fatal("余额未付清时不应标记订单已支付")
	}
	// 余额已扣、订单仍未支付、钱包流水已入账。
	if w, _ := WalletDetail(id); w.Balance != 0 {
		t.Fatalf("余额应已扣光, got %v", w.Balance)
	}
	if cur, _ := GetOrderAmount(o.OrderNo); cur.PayStatus != 0 {
		t.Fatal("订单在渠道支付完成前不应置为已支付")
	}
	wp, err := dao.GetPayment(o.OrderNo, PayChannelWallet)
	if err != nil || wp.Status != dao.PayStatusPaid || wp.Amount != walletCents {
		t.Fatalf("钱包流水异常: %+v err=%v", wp, err)
	}

	// 渠道侧落流水 + 模拟微信回调(金额 = 差额)。
	if err := CreatePayment(prep.OrderID, o.OrderNo, payChannelWxpay, prep.PayCents, "prepay-mixed"); err != nil {
		t.Fatalf("落渠道流水失败(混合金额校验应放行): %v", err)
	}
	res := ApplyPaymentSuccess(ctx, o.OrderNo, payChannelWxpay, "WX-MIX-1", "微信支付", prep.PayCents)
	if res.Failed || res.Duplicate {
		t.Fatalf("回调入账异常: %+v", res)
	}
	cur, _ := GetOrderAmount(o.OrderNo)
	if cur.PayStatus != 1 {
		t.Fatal("余额 + 渠道补齐后订单应为已支付")
	}
	// 实收必须是订单总额(而不是渠道那一笔的金额)。
	var paid int64
	if err := store.DB.QueryRow(`SELECT COALESCE(paid_amount,0) FROM tb_order WHERE order_no=?`, o.OrderNo).Scan(&paid); err != nil {
		t.Fatalf("查实收失败: %v", err)
	}
	if paid != total {
		t.Fatalf("实收应为订单总额 %d 分, got %d", total, paid)
	}
}

// TestMixedPayWalletCoversAll 余额够付全款时,混合支付 = 一次性付清,不需要渠道。
func TestMixedPayWalletCoversAll(t *testing.T) {
	ctx := context.Background()
	id := walletInit(t, "mixed-all", 100)
	dishID, specID := customerSvcSpecID(t)
	order, err := CustomerCreateOrder(id, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	// 传一个明显超过订单金额的额度:应被收敛到「还差的金额」,不会多扣。
	prep, err := PrepareMixedPay(ctx, id, order.OrderNo, 10000)
	if err != nil {
		t.Fatalf("混合支付失败: %v", err)
	}
	if prep.PayCents != 0 || !prep.FirstTime {
		t.Fatalf("余额足够时应直接付清: payCents=%d firstTime=%v", prep.PayCents, prep.FirstTime)
	}
	cur, _ := GetOrderAmount(order.OrderNo)
	if cur.PayStatus != 1 {
		t.Fatal("余额付清后订单应为已支付")
	}
	if w, _ := WalletDetail(id); w.Balance != 100-float64(prep.WalletCents)/100 {
		t.Fatalf("余额扣减不符: 剩 %v, 实扣 %d 分", w.Balance, prep.WalletCents)
	}
}

// TestAbortMixedPayRestoresBuckets 放弃渠道支付后撤销:本金与赠金原样回到各自的桶。
//
// 若一律退进本金桶,门店白送的赠金就被洗成了可退现金。
func TestAbortMixedPayRestoresBuckets(t *testing.T) {
	ctx := context.Background()
	id, o, _ := mixedOrder(t, "mixed-abort", 10)
	// 造出「本金 10 + 赠金 5」的结构,让拆分一定会碰到赠金桶。
	if err := AdminWalletAdjust(id, 5, "测试赠送", "店长"); err != nil {
		t.Fatalf("赠送失败: %v", err)
	}
	before, _ := WalletDetail(id)

	if _, err := PrepareMixedPay(ctx, id, o.OrderNo, 1500); err != nil {
		t.Fatalf("混合支付失败: %v", err)
	}
	if after, _ := WalletDetail(id); after.Balance+after.BalanceGift != before.Balance+before.BalanceGift-15 {
		t.Fatalf("扣款金额不符: before=%v/%v after=%v/%v", before.Balance, before.BalanceGift, after.Balance, after.BalanceGift)
	}

	if err := AbortMixedPay(ctx, o.OrderNo, "用例撤销"); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	restored, _ := WalletDetail(id)
	if restored.TotalGift != before.TotalGift {
		t.Fatalf("撤销不得重复累加「累计赠送」: 期望 %v, got %v", before.TotalGift, restored.TotalGift)
	}
	if restored.Balance != before.Balance || restored.BalanceGift != before.BalanceGift {
		t.Fatalf("撤销后应原样还原: 期望 %v/%v, got %v/%v",
			before.Balance, before.BalanceGift, restored.Balance, restored.BalanceGift)
	}
	// 钱包流水置为已退款、订单仍是待支付。
	wp, _ := dao.GetPayment(o.OrderNo, PayChannelWallet)
	if wp.Status != dao.PayStatusRefunded {
		t.Fatalf("撤销后钱包流水应置已退款, got %d", wp.Status)
	}
	if cur, _ := GetOrderAmount(o.OrderNo); cur.PayStatus != 0 {
		t.Fatal("撤销后订单应保持未支付")
	}
}

// TestRefundPartsSplitAcrossChannels 混合支付订单退款:按支付流水分摊,钱各回各家。
func TestRefundPartsSplitAcrossChannels(t *testing.T) {
	ctx := context.Background()
	id, o, total := mixedOrder(t, "mixed-refund", 10)
	const walletCents = int64(1000)

	prep, err := PrepareMixedPay(ctx, id, o.OrderNo, walletCents)
	if err != nil {
		t.Fatalf("混合支付失败: %v", err)
	}
	if err := CreatePayment(prep.OrderID, o.OrderNo, payChannelWxpay, prep.PayCents, "prepay-refund"); err != nil {
		t.Fatalf("落渠道流水失败: %v", err)
	}
	if res := ApplyPaymentSuccess(ctx, o.OrderNo, payChannelWxpay, "WX-MIX-2", "微信支付", prep.PayCents); res.Failed || res.Duplicate {
		t.Fatalf("回调入账异常: %+v", res)
	}

	// 全额退款应拆成两笔:最近付的微信先退,再退储值。
	parts, err := CreateRefundParts(ctx, o.OrderID, o.OrderNo, total, 0, "测试退款", "tester")
	if err != nil {
		t.Fatalf("退款分摊失败: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("混合支付退款应拆成 2 笔, got %d: %+v", len(parts), parts)
	}
	var sum int64
	byChannel := map[string]int64{}
	for _, p := range parts {
		sum += p.Amount
		byChannel[p.Channel] += p.Amount
	}
	if sum != total {
		t.Fatalf("分摊总额应等于订单金额 %d, got %d", total, sum)
	}
	if byChannel[payChannelWxpay] != prep.PayCents || byChannel[PayChannelWallet] != walletCents {
		t.Fatalf("分摊应各回各家: 微信%d(期望%d) 储值%d(期望%d)",
			byChannel[payChannelWxpay], prep.PayCents, byChannel[PayChannelWallet], walletCents)
	}
	// 微信那笔的 total 参数必须是该笔支付的原始金额,不是订单总额。
	for _, p := range parts {
		if p.Channel == payChannelWxpay && p.PaymentTotal != prep.PayCents {
			t.Fatalf("渠道退款的 total 应为该笔支付金额 %d, got %d", prep.PayCents, p.PaymentTotal)
		}
	}
}
