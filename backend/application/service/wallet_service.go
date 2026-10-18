// 储值域用例:余额查询、充值(微信支付)、余额支付、退款退回、后台调账,以及手机号绑定。
//
// 钱相关的两条铁律:
//  1. 余额扣减必须在事务内带 balance>=? 守卫,并发下「先查再扣」必超扣;
//  2. 余额支付必须把「扣余额、落支付流水、订单入账」放进同一个事务 ——
//     渠道支付有回调重试兜底,储值余额扣掉之后没有任何重试机制,事务是唯一的保障。
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/infrastructure/weapp"
)

// PayChannelWallet 余额支付渠道标识。
// 与 pay 包的渠道常量同一命名空间,但刻意定义在本包:service 不得 import pay
// (见 payment_service.go 顶部的分层约束),而 handler 需要在调用渠道抽象前分流。
const PayChannelWallet = "wallet"

// ErrRechargeNotFound 充值单不存在(回调里的脏单号/历史单号)。
// notify handler 据此应答成功,避免渠道对一笔永远不存在的单无限重试。
var ErrRechargeNotFound = errors.New("充值单不存在")

// 充值金额边界(元)。只收整数元:充值没有找零需求,还能挡住一半的输入校验。
const (
	walletRechargeMinYuan = 1
	walletRechargeMaxYuan = 10000
)

// ============================================================================
// 查询
// ============================================================================

// WalletInfo 顾客端/后台共用的储值视图(金额一律以「元」下发,口径同订单)。
type WalletInfo struct {
	Balance       float64 `json:"balance"`
	TotalRecharge float64 `json:"totalRecharge"`
	TotalConsume  float64 `json:"totalConsume"`
	TotalGift     float64 `json:"totalGift"`
}

// WalletDetail 查询储值账户。从未充过值时返回全 0(不为每个会员预建账户行)。
func WalletDetail(customerID int) (*WalletInfo, error) {
	w, err := dao.GetWallet(customerID)
	if err != nil {
		return nil, errors.New("系统繁忙，请稍后再试")
	}
	if w == nil {
		return &WalletInfo{}, nil
	}
	return &WalletInfo{
		Balance:       po.ToYuan(w.Balance),
		TotalRecharge: po.ToYuan(w.TotalRecharge),
		TotalConsume:  po.ToYuan(w.TotalConsume),
		TotalGift:     po.ToYuan(w.TotalGift),
	}, nil
}

// ListWalletTxns 分页查询储值流水。
func ListWalletTxns(customerID, pageNum, pageSize int) (int, []dao.WalletTxnRow, error) {
	return dao.ListWalletTxns(customerID, pageNum, pageSize)
}

// ============================================================================
// 充值
// ============================================================================

// WalletRechargeEnabled 储值自助充值是否开放(管理端开关,默认关)。
// 只管「能否自助充值」:已有余额的消费与退款不受它影响 —— 那是顾客的钱。
func WalletRechargeEnabled() bool {
	return dao.GetSetting("wallet_recharge_enabled") == "1"
}

// CreateWalletRecharge 创建一笔待支付充值单,返回单号与金额(分)。
// 只收整数元:充值没有找零需求,整数元还能挡住一半的输入校验。
func CreateWalletRecharge(customerID int, amountYuan float64) (string, int64, error) {
	if !WalletRechargeEnabled() {
		return "", 0, errors.New("储值充值暂未开放，请联系商家")
	}
	if amountYuan != float64(int64(amountYuan)) {
		return "", 0, errors.New("充值金额须为整数元")
	}
	cents := po.ToCents(amountYuan)
	if cents < walletRechargeMinYuan*100 || cents > walletRechargeMaxYuan*100 {
		return "", 0, fmt.Errorf("单次充值金额须在 %d ~ %d 元之间", walletRechargeMinYuan, walletRechargeMaxYuan)
	}
	if _, err := dao.GetCustomerByID(customerID); err != nil {
		return "", 0, errors.New("系统繁忙，请稍后再试")
	}
	no, err := dao.InsertRecharge(customerID, cents)
	if err != nil {
		logger.Warnf("[wallet] 创建充值单失败(customer=%d): %v", customerID, err)
		return "", 0, errors.New("发起充值失败，请稍后再试")
	}
	return no, cents, nil
}

// GetWalletRecharge 查询充值单(前端轮询入账状态用)。不存在返回 (nil, nil)。
func GetWalletRecharge(rechargeNo string) (*po.Recharge, error) {
	return dao.GetRecharge(rechargeNo)
}

// WalletRechargeCredit 充值入账(支付回调调用):幂等 —— 条件更新 tb_recharge 0→1,
// 微信重发通知不会重复加钱。单号不存在返回 ErrRechargeNotFound(调用方应答成功)。
func WalletRechargeCredit(rechargeNo, channel, channelTradeNo string, amountCents int64) error {
	err := store.WithTx(func(tx *sql.Tx) error {
		r, err := dao.GetRechargeTx(tx, rechargeNo)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrRechargeNotFound
		}
		if r.Status == po.RechargeStatusPaid {
			return nil // 已入账,重复通知直接成功
		}
		// 金额防线与订单回调同一口径:不一致绝不入账。
		if r.Amount != amountCents {
			logger.Warnf("[wallet][告警] 充值回调金额不一致 单%s 期望%d分 实际%d分", rechargeNo, r.Amount, amountCents)
			return fmt.Errorf("充值金额不一致")
		}
		if _, err := dao.AdjustWalletTx(tx, r.CustomerID, r.Amount, po.WalletTxnRecharge, "", channelTradeNo, "储值充值", ""); err != nil {
			return err
		}
		_, err = dao.MarkRechargePaidTx(tx, rechargeNo, channel, channelTradeNo)
		return err
	})
	if err != nil && !errors.Is(err, ErrRechargeNotFound) {
		logger.Warnf("[wallet] 充值入账失败 单%s: %v", rechargeNo, err)
	}
	return err
}

// ============================================================================
// 余额支付
// ============================================================================

// PayOrderByWallet 用储值余额支付订单,返回是否首次入账(先付模式据此出厨单)。
//
// 扣余额、落支付流水、订单入账在**同一个事务**里:渠道支付有回调重试兜底,
// 余额扣掉之后没有任何重试机制 —— 若拆成多步,中间任何一步失败都会出现
// 「钱扣了、单没付」且无法自愈,只能人工对账。
// 匿名单(同桌他人下的单)会被本次支付认领归属;他人订单拒绝。
func PayOrderByWallet(ctx context.Context, customerID int, orderNo string) (bool, error) {
	firstTime := false
	err := store.WithTx(func(tx *sql.Tx) error {
		// 订单行锁 + 状态/金额复核(与渠道支付同一套防线)。
		o, err := dao.GetOrderAmountTx(tx, orderNo)
		if err != nil {
			return errors.New("订单不存在")
		}
		if o.PayStatus == 1 {
			return errors.New("订单已支付")
		}
		if !ActiveStatus(o.OrderStatus) {
			return errors.New("当前订单状态不可支付")
		}
		if o.TotalAmount <= 0 {
			return errors.New("订单金额异常，请联系收银员")
		}
		// 归属:只能付自己的单;匿名单(同桌他人下的)由本次支付认领,
		// 否则「我的订单」里查不到这单,对账口径也断了。
		owner, err := dao.OrderCustomerTx(tx, orderNo)
		if err != nil {
			return err
		}
		if owner != 0 && owner != customerID {
			return errors.New("只能使用自己的储值余额支付自己的订单")
		}
		if owner == 0 {
			if _, err := dao.ClaimOrderCustomerTx(tx, orderNo, customerID); err != nil {
				return err
			}
		}
		// 支付流水:先落待支付占位(订单行锁内复核金额,与渠道支付同一防线),
		// 扣款成功后置为已支付 —— 复用渠道流水的全部既有语义(退款/关单/列表)。
		if err := dao.InsertPaymentTx(tx, orderNo, PayChannelWallet, o.TotalAmount, ""); err != nil {
			return err
		}
		tradeNo := "W" + orderNo
		if _, err := dao.AdjustWalletTx(tx, customerID, -o.TotalAmount, po.WalletTxnConsume, orderNo, tradeNo, "", ""); err != nil {
			if errors.Is(err, dao.ErrWalletInsufficient) {
				return errors.New("储值余额不足，请先充值")
			}
			return err
		}
		if _, err := dao.MarkPaymentPaidTx(tx, orderNo, PayChannelWallet, tradeNo); err != nil {
			return err
		}
		changed, err := dao.MarkOrderPaidByChannelTx(tx, orderNo, "储值余额", tradeNo, PayChannelWallet)
		if err != nil {
			return err
		}
		if !changed {
			// 订单在事务内已被并发置为已支付:整笔回滚,一分钱都不会动。
			return errors.New("订单已支付")
		}
		firstTime = true
		logger.CtxInfof(ctx, "[wallet] 余额支付成功 订单%s 会员%d 金额%d分", orderNo, customerID, o.TotalAmount)
		return nil
	})
	return firstTime, err
}

// WalletRefundForOrder 余额支付订单的退款:原路退回 = 退回储值余额(同步完成)。
// 渠道退款不需要 —— 余额本来就在店里,不必向微信/支付宝发起退款。
func WalletRefundForOrder(orderNo string, refundCents int64, reason string) error {
	err := store.WithTx(func(tx *sql.Tx) error {
		cid, err := dao.OrderCustomerTx(tx, orderNo)
		if err != nil {
			return err
		}
		if cid <= 0 {
			return errors.New("订单无会员归属，无法退回储值余额")
		}
		_, err = dao.AdjustWalletTx(tx, cid, refundCents, po.WalletTxnRefund, orderNo, "", reason, "")
		return err
	})
	if err != nil {
		logger.Warnf("[wallet] 余额退回失败 订单%s 金额%d分: %v", orderNo, refundCents, err)
	}
	return err
}

// WalletRefundRollback 退款入账失败时的补偿:把刚退回余额的钱立即扣回。
//
// 退款是「先退钱、后记账」两步,第二步失败会留下「余额已加、退款单停在处理中」的
// 不一致,且处理中的退款单会占用可退额度挡住重试 —— 必须立刻对冲。
// 扣回用的是 adjust 类型;若顾客已把刚退的钱花掉导致扣成负数被守卫拒绝,
// 只能留痕人工处理(这是唯一无法自动修复的路径,Error 级别告警)。
func WalletRefundRollback(orderNo string, refundCents int64) error {
	err := store.WithTx(func(tx *sql.Tx) error {
		cid, err := dao.OrderCustomerTx(tx, orderNo)
		if err != nil {
			return err
		}
		if cid <= 0 {
			return errors.New("订单无会员归属，无法回滚退款")
		}
		_, err = dao.AdjustWalletTx(tx, cid, -refundCents, po.WalletTxnAdjust, orderNo, "", "退款入账失败自动回滚", "system")
		return err
	})
	if err != nil {
		logger.Errorf("[wallet][告警] 退款回滚失败(余额与退款单可能不一致,请人工核对) 订单%s 金额%d分: %v", orderNo, refundCents, err)
	}
	return err
}

// ============================================================================
// 手机号绑定
// ============================================================================

// BindCustomerPhone 用 getPhoneNumber 的 code 换手机号并绑到当前会员。
// 返回脱敏后的手机号(完整号码不外发)。同一手机号全店只能属于一个会员。
//
// 手机号就是一种登录方式(provider=phone),只存 tb_customer_identity,
// 不在档案表再加一列 —— 身份层当初就是为这一步设计的。
func BindCustomerPhone(ctx context.Context, customerID int, phoneCode string) (string, error) {
	phone, err := weapp.GetPhoneNumber(ctx, phoneCode)
	if err != nil {
		return "", err
	}
	no := strings.TrimSpace(phone.PurePhoneNumber)

	err = store.WithTx(func(tx *sql.Tx) error {
		// 已被绑定?指向同一会员则幂等成功,指向别人则拒绝 ——
		// 否则「一个手机号两个会员」会让按手机号查人永远有歧义。
		existing, err := dao.GetCustomerByIdentity(po.CustomerProviderPhone, no)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.CustomerID != customerID {
				return errors.New("该手机号已绑定其它会员")
			}
			return nil
		}
		return dao.InsertIdentityTx(tx, customerID, po.CustomerProviderPhone, no, "")
	})
	if err != nil {
		return "", err
	}
	logger.CtxInfof(ctx, "[member] 会员%d 已绑定手机号 %s", customerID, po.MaskPhone(no))
	return po.MaskPhone(no), nil
}

// ============================================================================
// 后台调账
// ============================================================================

// AdminWalletAdjust 后台调账:正数=赠送(入账),负数=扣减(纠错)。
// 必须填原因;操作人写入流水,审计靠它对得上人。
func AdminWalletAdjust(customerID int, amountYuan float64, reason, operator string) error {
	if amountYuan == 0 {
		return errors.New("变动金额不能为 0")
	}
	if amountYuan != float64(int64(amountYuan)) {
		return errors.New("变动金额须为整数元")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("请填写变动原因")
	}
	if _, err := dao.GetCustomerByID(customerID); err != nil {
		return errors.New("系统繁忙，请稍后再试")
	}
	cents := po.ToCents(amountYuan)
	bizType := po.WalletTxnGift
	if cents < 0 {
		bizType = po.WalletTxnAdjust
	}
	if _, err := dao.AdjustWallet(customerID, cents, bizType, "", "", reason, operator); err != nil {
		if errors.Is(err, dao.ErrWalletInsufficient) {
			return errors.New("扣减后余额将为负，请核对金额")
		}
		logger.Warnf("[wallet] 后台调账失败(customer=%d): %v", customerID, err)
		return errors.New("调账失败，请稍后再试")
	}
	logger.Infof("[wallet] 后台调账 会员%d %+.2f元 原因:%s 操作人:%s", customerID, amountYuan, reason, operator)
	return nil
}
