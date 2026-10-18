package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// walletInit 建库 + 注册一个会员 + 充值,返回会员 ID(供储值用例直接用)。
// 充值开关默认是关的(动钱的入口 fail-closed),用例里显式打开。
func walletInit(t *testing.T, openid string, rechargeYuan int64) int {
	t.Helper()
	customerSvcInit(t)
	if err := SetSetting("wallet_recharge_enabled", "1"); err != nil {
		t.Fatalf("打开充值开关失败: %v", err)
	}
	id := customerLoginByOpenidForTest(t, openid).Customer.CustomerID
	if rechargeYuan > 0 {
		no, cents, err := CreateWalletRecharge(id, float64(rechargeYuan))
		if err != nil {
			t.Fatalf("创建充值单失败: %v", err)
		}
		if cents != rechargeYuan*100 {
			t.Fatalf("充值金额不符: %d", cents)
		}
		if err := WalletRechargeCredit(no, "wxpay", "T-"+no, cents); err != nil {
			t.Fatalf("充值入账失败: %v", err)
		}
	}
	return id
}

// TestWalletRechargeCredit 幂等:同一单号重复回调只加一次钱;金额不一致绝不入账。
func TestWalletRechargeCredit(t *testing.T) {
	id := walletInit(t, "wallet-r1", 0)

	no, cents, err := CreateWalletRecharge(id, 100)
	if err != nil || cents != 10000 {
		t.Fatalf("建充值单失败: %v %d", err, cents)
	}
	for i := 0; i < 3; i++ { // 微信重发通知 → 三次回调
		if err := WalletRechargeCredit(no, "wxpay", "T1", cents); err != nil {
			t.Fatalf("第 %d 次回调应成功: %v", i+1, err)
		}
	}
	w, _ := WalletDetail(id)
	if w.Balance != 100 {
		t.Fatalf("重复回调不得重复加钱, 余额应为 100 元, got %v", w.Balance)
	}

	// 金额不一致:绝不入账。
	no2, cents2, err2 := CreateWalletRecharge(id, 50)
	if err2 != nil {
		t.Fatalf("建充值单失败: %v", err2)
	}
	if err := WalletRechargeCredit(no2, "wxpay", "T2", cents2-100); err == nil {
		t.Fatal("金额不一致应被拒绝")
	}
	// 不存在的单号:返回哨兵错误(handler 应答成功,渠道不必重试),但不加钱。
	if err := WalletRechargeCredit("R0000000000000", "wxpay", "T3", 100); !errors.Is(err, ErrRechargeNotFound) {
		t.Fatalf("不存在的充值单应返回 ErrRechargeNotFound, got %v", err)
	}
	if w, _ = WalletDetail(id); w.Balance != 100 {
		t.Fatalf("拒绝入账后余额不应变化, got %v", w.Balance)
	}

	// 充值开关关闭:自助充值被拒(余额消费/退款不受影响,那是顾客的钱)。
	if err := SetSetting("wallet_recharge_enabled", "0"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateWalletRecharge(id, 100); err == nil {
		t.Fatal("充值开关关闭时应被拒绝")
	}
}

// TestWalletPayAndRefund 余额支付与退款:
// 扣款/流水/订单入账同一事务;退款原路退回余额;余额支付的单能在「我的订单」里查到。
func TestWalletPayAndRefund(t *testing.T) {
	id := walletInit(t, "wallet-pay", 100)
	dishID, specID := customerSvcSpecID(t)

	order, err := CustomerCreateOrder(id, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}

	// 匿名单认领:归属在支付时落到本会员,「我的订单」才查得到。
	total, rows, err := CustomerMyOrders(id, 1, 10)
	if err != nil || total != 1 || len(rows) == 0 {
		t.Fatalf("下单后应能查到自己的订单, total=%d err=%v", total, err)
	}

	first, err := PayOrderByWallet(context.Background(), id, order.OrderNo)
	if err != nil {
		t.Fatalf("余额支付失败: %v", err)
	}
	if !first {
		t.Fatal("首次支付应返回 firstTime=true(先付模式据此出厨单)")
	}

	// 订单已入账、支付流水已置已支付(渠道语义完全复用,退款/列表不用另写一套)。
	o, err := GetOrderAmount(order.OrderNo)
	if err != nil || o.PayStatus != 1 {
		t.Fatalf("订单应已支付: %+v err=%v", o, err)
	}
	p, err := dao.GetPayment(order.OrderNo, PayChannelWallet)
	if err != nil || p.Status != dao.PayStatusPaid {
		t.Fatalf("支付流水应已置已支付: %+v err=%v", p, err)
	}

	// 余额被扣掉:100 - 订单金额。
	w, _ := WalletDetail(id)
	if w.Balance < 0 || w.Balance > 100 {
		t.Fatalf("余额应在 0~100 之间, got %v", w.Balance)
	}
	used := 100 - w.Balance

	// 幂等:再付一次必须被拒(订单已支付),余额不动。
	if _, err := PayOrderByWallet(context.Background(), id, order.OrderNo); err == nil {
		t.Fatal("重复支付应被拒绝")
	}
	if w2, _ := WalletDetail(id); w2.Balance != w.Balance {
		t.Fatal("重复支付被拒后余额不应变化")
	}

	// 退款:原路退回储值余额。
	if err := WalletRefundForOrder(order.OrderNo, po.ToCents(used), "测试退款"); err != nil {
		t.Fatalf("退回余额失败: %v", err)
	}
	if w3, _ := WalletDetail(id); w3.Balance != 100 {
		t.Fatalf("退款后余额应回到 100 元, got %v", w3.Balance)
	}

	// 他人订单不能用我的余额支付。
	other := customerLoginByOpenidForTest(t, "wallet-other").Customer.CustomerID
	order2, err := CustomerCreateOrder(other, 2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("他人下单失败: %v", err)
	}
	if _, err := PayOrderByWallet(context.Background(), id, order2.OrderNo); err == nil {
		t.Fatal("不能用余额支付他人的订单")
	}
}

// TestWalletInsufficientAndConcurrency 并发消费不超扣:
// 100 元余额、两个并发扣 60 元的请求,必须恰好成功一个 ——
// 「先查再扣」在这里必然超扣,守卫是唯一防线。
func TestWalletInsufficientAndConcurrency(t *testing.T) {
	id := walletInit(t, "wallet-race", 100)

	const workers = 2
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := dao.AdjustWallet(id, -6000, po.WalletTxnConsume, "", "", "并发扣款", "")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	success := 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
	}
	if success != 1 {
		t.Fatalf("100 元余额并发扣 60 元应恰好成功 1 次, got %d", success)
	}
	w, _ := WalletDetail(id)
	if w.Balance != 40 {
		t.Fatalf("并发后余额应为 40 元, got %v", w.Balance)
	}
}
