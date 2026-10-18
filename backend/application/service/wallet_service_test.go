package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// walletInit 建库 + 注册一个会员 + 充值,返回会员 ID(供储值用例直接用)。
//
// 充值开关默认是关的(动钱的入口 fail-closed),用例里显式打开;
// 同时关掉「强制实名」(用例里没有真实手机号可绑)并代会员同意《储值规则》——
// 这两道闸门是本轮合规改造新增的,不开就建不了单。
func walletInit(t *testing.T, openid string, rechargeYuan int64) int {
	t.Helper()
	customerSvcInit(t)
	if err := SetSetting("wallet_recharge_enabled", "1"); err != nil {
		t.Fatalf("打开充值开关失败: %v", err)
	}
	if err := SetSetting("wallet_realname_required", "0"); err != nil {
		t.Fatalf("关闭强制实名失败: %v", err)
	}
	id := customerLoginByOpenidForTest(t, openid).Customer.CustomerID
	if err := AgreeWalletRules(id, WalletRulesVersion()); err != nil {
		t.Fatalf("同意储值规则失败: %v", err)
	}
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
			_, _, err := dao.AdjustWallet(id, -6000, po.WalletTxnConsume, "", "", "并发扣款", "")
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

// TestWalletConcurrentConsumeUsesFreshGift 并发消费必须按「锁定后的真实赠金」拆分。
//
// 赠金 100 + 本金 100,两笔并发各扣 80:总余额 200 够付,两笔都该成功。
// 若拆分用的是锁外快照(两笔都以为赠金还有 100),第二笔会尝试扣 100 赠金、
// 被赠金桶守卫拒绝 —— 明明有钱却报「余额不足」。这是 v1 的已知边界。
func TestWalletConcurrentConsumeUsesFreshGift(t *testing.T) {
	id := walletInit(t, "wallet-race-gift", 100)
	if err := AdminWalletAdjust(id, 100, "测试赠送", "店长"); err != nil {
		t.Fatalf("赠送失败: %v", err)
	}

	const workers = 2
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, _, err := dao.AdjustWallet(id, -8000, po.WalletTxnConsume, "", "", "并发扣款", "")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != workers {
		t.Fatalf("200 元余额并发扣 80 元两次应全部成功, got %d", success)
	}
	w, _ := WalletDetail(id)
	if w.Balance != 40 || w.BalanceGift != 0 {
		t.Fatalf("并发后应为本金 40 / 赠金 0, got %v / %v", w.Balance, w.BalanceGift)
	}
}

// ============================================================================
// 合规改造新增的用例(本金/赠金分桶、闸门、退余额、注销拦截)
// ============================================================================

// TestWalletRechargeCreditConcurrent 并发回调只能入账一次。
//
// 这是「先加钱、后置已入账」这类写法的经典事故:两个回调同时读到 status=0,
// 各自加一次钱,条件更新只有一个成功 —— 钱却加了两遍,凭空多出一笔余额。
// 修法是把条件更新提到加钱之前(抢占),本用例守住这个顺序。
func TestWalletRechargeCreditConcurrent(t *testing.T) {
	id := walletInit(t, "wallet-credit-race", 0)
	no, cents, err := CreateWalletRecharge(id, 100)
	if err != nil {
		t.Fatalf("建充值单失败: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			// 并发下其中一个可能因库锁失败(SQLite 单写者),这属正常;
			// 真正要守住的是「余额不多不少」。
			_ = WalletRechargeCredit(no, "wxpay", "T-"+no, cents)
		}()
	}
	wg.Wait()

	if w, _ := WalletDetail(id); w.Balance != 100 {
		t.Fatalf("并发回调只能入账一次, 余额应为 100 元, got %v", w.Balance)
	}
	// 入账后再补一次(串行),仍然只能是一次。
	if err := WalletRechargeCredit(no, "wxpay", "T-"+no, cents); err != nil {
		t.Fatalf("重复回调应按幂等成功: %v", err)
	}
	if w, _ := WalletDetail(id); w.Balance != 100 {
		t.Fatalf("重复回调不得重复加钱, got %v", w.Balance)
	}
}

// TestWalletGiftBucketConsumeFirst 赠金优先消耗,且退款只退本金。
//
// 「赠金不可套现」是本轮分桶的核心:若消费时不区分桶、退款时全额退本金,
// 「充 100 送 10 → 用赠金买单 → 退款拿回本金」就能无限套现。
func TestWalletGiftBucketConsumeFirst(t *testing.T) {
	id := walletInit(t, "wallet-gift", 100)
	dishID, specID := customerSvcSpecID(t)

	// 后台赠送 30 元 → 落赠金桶(不收钱的储值不该变成可退现金)。
	if err := AdminWalletAdjust(id, 30, "开业赠送", "店长"); err != nil {
		t.Fatalf("赠送失败: %v", err)
	}
	if w, _ := WalletDetail(id); w.Balance != 100 || w.BalanceGift != 30 {
		t.Fatalf("赠送后应为本金 100 / 赠金 30, got %v / %v", w.Balance, w.BalanceGift)
	}

	order, err := CustomerCreateOrder(id, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := PayOrderByWallet(context.Background(), id, order.OrderNo); err != nil {
		t.Fatalf("余额支付失败: %v", err)
	}

	after, _ := WalletDetail(id)
	giftUsed := int64(30 - after.BalanceGift)
	principalUsed := int64(100 - after.Balance)
	if giftUsed <= 0 {
		t.Fatalf("消费应优先扣赠金, got 赠金余额 %v", after.BalanceGift)
	}
	if giftUsed != 30 && after.BalanceGift != 0 {
		t.Fatalf("赠金未被优先扣完: 用掉 %d, 剩 %v", giftUsed, after.BalanceGift)
	}

	// 全额退款必须被拒:总额里含赠金,赠金不退。
	if err := WalletRefundForOrder(order.OrderNo, (giftUsed+principalUsed)*100, "全额退款"); err == nil {
		t.Fatal("含赠金的消费不得全额退成本金")
	} else if !strings.Contains(err.Error(), "赠金") {
		t.Fatalf("拒绝原因应说明赠金不退, got %v", err)
	}
	// 只退本金部分应成功。
	if err := WalletRefundForOrder(order.OrderNo, principalUsed*100, "退本金"); err != nil {
		t.Fatalf("退本金失败: %v", err)
	}
	if w, _ := WalletDetail(id); w.Balance != 100 {
		t.Fatalf("退本金后本金应回到 100 元, got %v", w.Balance)
	}
	if w, _ := WalletDetail(id); w.BalanceGift != after.BalanceGift {
		t.Fatalf("退款不应改变赠金余额, got %v 期望 %v", w.BalanceGift, after.BalanceGift)
	}
}

// TestWalletRechargeComplianceGates 充值的合规闸门:
// 规则未同意 / 未实名 / 超单笔上限 / 超余额上限,任一不过都不建单。
func TestWalletRechargeComplianceGates(t *testing.T) {
	// 规则版本变更 → 必须重新确认。
	id := walletInit(t, "wallet-gate-rules", 0)
	if err := SetSetting("wallet_rules_version", "2026.2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateWalletRecharge(id, 100); err == nil || !strings.Contains(err.Error(), "储值规则") {
		t.Fatalf("规则版本变更未重新确认时应拒绝, got %v", err)
	}
	if err := AgreeWalletRules(id, "2026.1"); err == nil {
		t.Fatal("同意过期版本应被拒绝")
	}
	if err := AgreeWalletRules(id, "2026.2"); err != nil {
		t.Fatalf("同意当前版本失败: %v", err)
	}

	// 强制实名但未绑手机号:拒绝(实名与否决定余额上限是 1000 还是 5000)。
	if err := SetSetting("wallet_realname_required", "1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateWalletRecharge(id, 100); err == nil || !strings.Contains(err.Error(), "手机号") {
		t.Fatalf("强制实名未绑手机号时应拒绝, got %v", err)
	}
	if err := SetSetting("wallet_realname_required", "0"); err != nil {
		t.Fatal(err)
	}

	// 单笔上限。
	if err := SetSetting("wallet_recharge_max", "100"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateWalletRecharge(id, 500); err == nil {
		t.Fatal("超过单笔上限应被拒绝")
	}
	if err := SetSetting("wallet_recharge_max", "1000"); err != nil {
		t.Fatal(err)
	}

	// 余额上限(本金 + 赠金合并计算)。
	if err := SetSetting("wallet_balance_max", "150"); err != nil {
		t.Fatal(err)
	}
	no, cents, err := CreateWalletRecharge(id, 100)
	if err != nil {
		t.Fatalf("首次充值应成功: %v", err)
	}
	if err := WalletRechargeCredit(no, "wxpay", "TG", cents); err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if _, _, err := CreateWalletRecharge(id, 100); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("充值后余额将超上限时应拒绝, got %v", err)
	}
}

// TestWalletClosedRechargeNeverCredits 已关闭的充值单是终态:延迟回调也不得入账。
//
// 「关了还入账」会让顾客早已放弃的钱凭空出现在余额里;反过来关早了就是资损,
// 所以关闭时限定在 24 小时(远超 prepay_id 的 2 小时有效期)。
func TestWalletClosedRechargeNeverCredits(t *testing.T) {
	id := walletInit(t, "wallet-close", 0)
	no, cents, err := CreateWalletRecharge(id, 100)
	if err != nil {
		t.Fatalf("建充值单失败: %v", err)
	}
	// 用「未来」的截止时刻强制关闭,绕开 24 小时等待。
	if _, err := dao.ExpirePendingRecharges("2999-01-01 00:00:00"); err != nil {
		t.Fatalf("关闭充值单失败: %v", err)
	}
	if err := WalletRechargeCredit(no, "wxpay", "TC", cents); !errors.Is(err, ErrRechargeClosed) {
		t.Fatalf("已关闭的充值单应返回 ErrRechargeClosed, got %v", err)
	}
	if w, _ := WalletDetail(id); w.Balance != 0 {
		t.Fatalf("已关闭的单不得入账, 余额应为 0, got %v", w.Balance)
	}
}

// TestAdminWalletWithdrawAndDeleteAccount 退余额与注销拦截:
// 带余额注销必须被拒;退余额清零本金与赠金后才能注销。
func TestAdminWalletWithdrawAndDeleteAccount(t *testing.T) {
	id := walletInit(t, "wallet-withdraw", 100)
	if err := AdminWalletAdjust(id, 20, "赠送", "店长"); err != nil {
		t.Fatalf("赠送失败: %v", err)
	}

	// 带余额注销:拒绝,并明确告知还剩多少、该怎么办。
	if err := CustomerDeleteAccount(id); err == nil || !strings.Contains(err.Error(), "余额未清零") {
		t.Fatalf("带余额注销应被拒绝, got %v", err)
	}
	if c, _ := dao.GetCustomerByID(id); c == nil {
		t.Fatal("被拒绝的注销不得删除档案")
	}

	// 退余额:本金清零(真要退给顾客的钱),赠金作废(不产生现金流出)。
	res, err := AdminWalletWithdraw(id, "顾客要求退余额", "店长")
	if err != nil {
		t.Fatalf("退余额失败: %v", err)
	}
	if res.Principal != 10000 || res.Gift != 2000 {
		t.Fatalf("退余额应清零本金 100 元 / 赠金 20 元, got %+v", res)
	}
	if w, _ := WalletDetail(id); w.Balance != 0 || w.BalanceGift != 0 {
		t.Fatalf("退余额后两个桶都应归零, got %v / %v", w.Balance, w.BalanceGift)
	}

	// 余额已清零:可以注销,且不留孤儿账户行。
	if err := CustomerDeleteAccount(id); err != nil {
		t.Fatalf("余额清零后应能注销: %v", err)
	}
	if w, err := dao.GetWallet(id); err != nil || w != nil {
		t.Fatal("注销应连储值账户一起删除,不留孤儿行")
	}

	// 未填原因不得退余额(钱出店必须有据可查)。
	if _, err := AdminWalletWithdraw(id, "", "店长"); err == nil {
		t.Fatal("未填原因不得退余额")
	}
}
