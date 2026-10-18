// 储值域用例:余额查询、充值(微信支付)、余额支付、退款退回、退余额、后台调账,
// 以及手机号绑定与《储值规则》同意留痕。
//
// 钱相关的三条铁律:
//  1. 余额扣减必须在事务内带 balance>=? 守卫,并发下「先查再扣」必超扣;
//  2. 余额支付必须把「扣余额、落支付流水、订单入账」放进同一个事务 ——
//     渠道支付有回调重试兜底,储值余额扣掉之后没有任何重试机制,事务是唯一的保障;
//  3. 本金与赠金分桶:本金可退、赠金不退。赠金一旦与本金混在一个数里,
//     退余额时就无法回答「这笔钱该不该退给顾客」,只能一刀切全退(可被套现)。
//
// 合规口径(详见 docs/compliance/prepaid-card.md):本模块落地的每一项限制
// (单笔/余额上限、实名、规则明示、退余额通道)都能在那份文档里找到出处。
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
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

// ErrRechargeClosed 充值单已关闭(超过支付时限被关单,是终态)。
// 回调据此应答成功且**绝不入账**:顾客早已放弃的充值,不该被延迟通知复活。
var ErrRechargeClosed = errors.New("充值单已关闭")

// 储值配置项(默认值见 store.settingDefaults,门店可在管理端调整)。
const (
	SettingWalletRechargeEnabled = "wallet_recharge_enabled"
	SettingWalletRechargeMin     = "wallet_recharge_min"      // 单笔最小(元)
	SettingWalletRechargeMax     = "wallet_recharge_max"      // 单笔最大(元)
	SettingWalletBalanceMax      = "wallet_balance_max"       // 未记名余额上限(元)
	SettingWalletBalanceMaxNamed = "wallet_balance_max_named" // 已记名余额上限(元)
	SettingWalletRealnameReq     = "wallet_realname_required" // 充值是否强制实名
	SettingWalletAdjustMax       = "wallet_adjust_max"        // 后台单次调账上限(元)
	SettingWalletRulesVer        = "wallet_rules_version"     // 《储值规则》版本
	SettingWalletRulesText       = "wallet_rules_text"        // 《储值规则》正文
)

// 充值金额边界的兜底值(元):配置缺失/填错时的 fail-safe 取值,
// 宁可按最小额度放行,也不要因为读不到配置而放行一个天文数字。
const (
	walletRechargeMinDefault  = 1
	walletRechargeMaxDefault  = 1000
	walletBalanceMaxDefault   = 1000
	walletBalanceNamedDefault = 5000
	walletAdjustMaxDefault    = 1000
)

// 充值单保留时限:超过它仍未入账的待支付单被关闭(见 dao.ExpirePendingRecharges)。
const walletRechargeExpireHours = 24

// settingInt 读整数配置,缺失或非数字时回落默认值。
// 配置是门店手填的,不能被一条脏数据挡住整个充值入口 —— 但要朝「更安全」的方向回落。
func settingInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(dao.GetSetting(key)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// WalletRulesVersion 当前《储值规则》版本(门店改了正文就必须改版本号,
// 版本号变化会强制顾客重新确认 —— 这是「当时展示的是哪一版」的留痕依据)。
func WalletRulesVersion() string {
	v := strings.TrimSpace(dao.GetSetting(SettingWalletRulesVer))
	if v == "" {
		return "2026.1"
	}
	return v
}

// WalletRulesText 《储值规则》正文(顾客端展示,门店可在管理端编辑)。
func WalletRulesText() string {
	t := strings.TrimSpace(dao.GetSetting(SettingWalletRulesText))
	if t == "" {
		return store.SettingDefault(SettingWalletRulesText)
	}
	return t
}

// WalletRealnameRequired 充值前是否强制实名(绑手机号)。
// 合规:未记名单用途卡单张限额 1000 元,记名卡 5000 元 —— 实名与否直接决定余额上限。
func WalletRealnameRequired() bool {
	return strings.TrimSpace(dao.GetSetting(SettingWalletRealnameReq)) != "0"
}

// walletBalanceMaxCents 该会员的余额上限(分)。已实名(绑手机号)走记名卡额度。
func walletBalanceMaxCents(customerID int) int64 {
	def := int64(walletBalanceMaxDefault) * 100
	if dao.HasCustomerPhone(customerID) {
		def = int64(walletBalanceNamedDefault) * 100
		return int64(settingInt(SettingWalletBalanceMaxNamed, int(def/100))) * 100
	}
	return int64(settingInt(SettingWalletBalanceMax, int(def/100))) * 100
}

// ============================================================================
// 查询
// ============================================================================

// WalletInfo 顾客端/后台共用的储值视图(金额一律以「元」下发,口径同订单)。
type WalletInfo struct {
	Balance       float64 `json:"balance"`     // 本金余额(可退)
	BalanceGift   float64 `json:"balanceGift"` // 赠金余额(不可退)
	TotalRecharge float64 `json:"totalRecharge"`
	TotalConsume  float64 `json:"totalConsume"`
	TotalGift     float64 `json:"totalGift"`
	// ---- 合规与交互(顾客端据此渲染充值区与规则确认) ----
	Named            bool    `json:"named"`            // 是否已实名(绑手机号)
	RulesVer         string  `json:"rulesVer"`         // 当前《储值规则》版本
	RulesText        string  `json:"rulesText"`        // 规则正文
	Agreed           bool    `json:"agreed"`           // 是否已同意当前版本
	CanRecharge      bool    `json:"canRecharge"`      // 是否可自助充值
	RealnameRequired bool    `json:"realnameRequired"` // 充值是否强制实名
	RechargeMin      float64 `json:"rechargeMin"`      // 单笔最小(元)
	RechargeMax      float64 `json:"rechargeMax"`      // 单笔最大(元)
	BalanceMax       float64 `json:"balanceMax"`       // 余额上限(元)
}

// WalletDetail 查询储值账户。从未充过值时返回全 0(不为每个会员预建账户行)。
func WalletDetail(customerID int) (*WalletInfo, error) {
	w, err := dao.GetWallet(customerID)
	if err != nil {
		return nil, errors.New("系统繁忙，请稍后再试")
	}
	var balance, gift, totalRecharge, totalConsume, totalGift int64
	if w != nil {
		balance, gift = w.Balance, w.BalanceGift
		totalRecharge, totalConsume, totalGift = w.TotalRecharge, w.TotalConsume, w.TotalGift
	}
	ver := WalletRulesVersion()
	return &WalletInfo{
		Balance:          po.ToYuan(balance),
		BalanceGift:      po.ToYuan(gift),
		TotalRecharge:    po.ToYuan(totalRecharge),
		TotalConsume:     po.ToYuan(totalConsume),
		TotalGift:        po.ToYuan(totalGift),
		Named:            dao.HasCustomerPhone(customerID),
		RulesVer:         ver,
		RulesText:        WalletRulesText(),
		Agreed:           dao.GetWalletAgreementVer(customerID) == ver,
		CanRecharge:      WalletRechargeEnabled(),
		RealnameRequired: WalletRealnameRequired(),
		RechargeMin:      float64(settingInt(SettingWalletRechargeMin, walletRechargeMinDefault)),
		RechargeMax:      float64(settingInt(SettingWalletRechargeMax, walletRechargeMaxDefault)),
		BalanceMax:       po.ToYuan(walletBalanceMaxCents(customerID)),
	}, nil
}

// ListWalletTxns 分页查询储值流水。
func ListWalletTxns(customerID, pageNum, pageSize int) (int, []dao.WalletTxnRow, error) {
	return dao.ListWalletTxns(customerID, pageNum, pageSize)
}

// WalletBalanceCents 取某会员的本金与赠金余额(分)。不存在账户返回 (0,0)。
// 供注销校验与退余额使用 —— 它们必须看到两个桶,漏掉赠金会留下无法消费也无法退的悬空钱。
func WalletBalanceCents(customerID int) (int64, int64, error) {
	w, err := dao.GetWallet(customerID)
	if err != nil {
		return 0, 0, err
	}
	if w == nil {
		return 0, 0, nil
	}
	return w.Balance, w.BalanceGift, nil
}

// ============================================================================
// 《储值规则》同意
// ============================================================================

// AgreeWalletRules 记录会员同意了当前版本的《储值规则》。
//
// 合规:发卡企业须向购卡人明示章程/协议。留痕不是为了「防顾客」,而是争议发生时
// 能证明「他充值时看到的是哪一版规则」。重复同意幂等。
func AgreeWalletRules(customerID int, rulesVer string) error {
	if strings.TrimSpace(rulesVer) == "" || rulesVer != WalletRulesVersion() {
		return errors.New("规则已更新，请重新阅读并同意")
	}
	if err := dao.SaveWalletAgreement(customerID, rulesVer); err != nil {
		logger.Warnf("[wallet] 记录储值规则同意失败(customer=%d): %v", customerID, err)
		return errors.New("保存失败，请稍后再试")
	}
	return nil
}

// ============================================================================
// 充值
// ============================================================================

// WalletRechargeEnabled 储值自助充值是否开放(管理端开关,默认关)。
// 只管「能否自助充值」:已有余额的消费与退款不受它影响 —— 那是顾客的钱。
func WalletRechargeEnabled() bool {
	return dao.GetSetting(SettingWalletRechargeEnabled) == "1"
}

// rechargeGiftCents 充值赠送规则(会员系统接入点)。
//
// 目前不赠送,恒返回 0。后续接「充 100 送 10」时**只改这一个函数**:
// 赠送额在建单时算好并冻结在 tb_recharge.gift_amount 上,回调入账只认单上的数,
// 不重新计算 —— 否则活动一改,已支付未入账的单会入成另一个数。
func rechargeGiftCents(amountCents int64) int64 {
	_ = amountCents
	return 0
}

// CreateWalletRecharge 创建一笔待支付充值单,返回单号与金额(分)。
//
// 只收整数元:充值没有找零需求,整数元还能挡住一半的输入校验。
//
// 四道合规闸门(任一不过都拒绝建单,顺序按「顾客能自己解决」排):
//  1. 已同意当前版《储值规则》;
//  2. 强制实名时已绑手机号(决定余额上限是 1000 还是 5000);
//  3. 单笔金额在区间内;
//  4. 入账后余额不超过该会员的余额上限。
func CreateWalletRecharge(customerID int, amountYuan float64) (string, int64, error) {
	if !WalletRechargeEnabled() {
		return "", 0, errors.New("储值充值暂未开放，请联系商家")
	}
	if amountYuan != float64(int64(amountYuan)) {
		return "", 0, errors.New("充值金额须为整数元")
	}
	cents := po.ToCents(amountYuan)
	minCents := int64(settingInt(SettingWalletRechargeMin, walletRechargeMinDefault)) * 100
	maxCents := int64(settingInt(SettingWalletRechargeMax, walletRechargeMaxDefault)) * 100
	if minCents > maxCents {
		minCents = maxCents
	}
	if cents < minCents || cents > maxCents {
		return "", 0, fmt.Errorf("单次充值金额须在 %d ~ %d 元之间", minCents/100, maxCents/100)
	}
	if dao.GetWalletAgreementVer(customerID) != WalletRulesVersion() {
		return "", 0, errors.New("请先阅读并同意《储值规则》")
	}
	if WalletRealnameRequired() && !dao.HasCustomerPhone(customerID) {
		return "", 0, errors.New("请先绑定手机号完成实名登记")
	}
	if _, err := dao.GetCustomerByID(customerID); err != nil {
		return "", 0, errors.New("系统繁忙，请稍后再试")
	}
	// 余额上限:本金 + 赠金 + 本次充值不得超过上限。
	// 两个桶都算进去 —— 只算本金会让「赠金多的会员」突破监管上限。
	balance, gift, err := WalletBalanceCents(customerID)
	if err != nil {
		return "", 0, errors.New("系统繁忙，请稍后再试")
	}
	limit := walletBalanceMaxCents(customerID)
	if balance+gift+cents > limit {
		return "", 0, fmt.Errorf("充值后余额将超过上限 %s 元，请先消费或联系门店调整", po.FormatYuan(limit))
	}
	no, err := dao.InsertRecharge(customerID, cents, rechargeGiftCents(cents))
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

// ListPendingRecharges 取会员近期未入账的充值单(handler 据此向渠道主动查单补账)。
//
// 补账入口必须在 service 之外(handler 调 pay.Provider.Query):service 不依赖
// pay 包(分层约束)。这里只提供「哪些单值得去查」。
func ListPendingRecharges(customerID int, withinHours int) ([]po.Recharge, error) {
	if withinHours <= 0 {
		withinHours = walletRechargeExpireHours
	}
	since := hoursAgo(withinHours)
	return dao.ListPendingRecharges(customerID, since)
}

// hoursAgo 返回 n 小时前的时间串(数据库统一格式),用于时间窗比较。
// 时间一律按字符串字典序比较(全库 create_time 同格式),不引入时区换算的额外分支。
func hoursAgo(n int) string {
	return time.Now().Add(-time.Duration(n) * time.Hour).Format(conf.TimeLayout)
}

// walletExpireInterval 关单巡检间隔。废单只是多留几行,不是需要争分夺秒的巡检,
// 取 6 小时:既不会让长时间运行的进程堆出成千上万行待支付单,也不至于频繁扫表。
const walletExpireInterval = 6 * time.Hour

// StartWalletRechargeExpirer 后台周期关闭超时未支付的充值单。
//
// 与 ExpireWalletRecharges 的关系:后者是「现在做一次」(启动时立刻清掉停机期间
// 累积的废单),这个是「以后每 6 小时做一次」。没有它,常年不重启的门店会在
// tb_recharge 里堆满顾客早已放弃的待支付单,补账扫描也会越扫越慢。
func StartWalletRechargeExpirer() {
	go func() {
		for {
			time.Sleep(walletExpireInterval)
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[wallet] 关闭超时充值单单轮 panic(已跳过本轮): %v", r)
					}
				}()
				ExpireWalletRecharges()
			}()
		}
	}()
}

// ExpireWalletRecharges 关闭超过时限仍未入账的充值单(启动时与巡检周期调用)。
func ExpireWalletRecharges() int {
	n, err := dao.ExpirePendingRecharges(hoursAgo(walletRechargeExpireHours))
	if err != nil {
		logger.Warnf("[wallet] 关闭超时充值单失败: %v", err)
		return 0
	}
	if n > 0 {
		logger.Infof("[wallet] 已关闭超时未支付充值单 %d 笔", n)
	}
	return n
}

// WalletRechargeCredit 充值入账(支付回调/主动查单补账调用):幂等 —— 条件更新
// tb_recharge 0→1,微信重发通知不会重复加钱。
//
// 单号不存在返回 ErrRechargeNotFound、已关闭返回 ErrRechargeClosed,
// 调用方据此应答成功(避免渠道对一笔永远不会成功的单无限重试)。
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
		if r.Status == po.RechargeStatusClosed {
			// 终态:顾客早已放弃的充值,不能被延迟通知复活。
			logger.Warnf("[wallet][告警] 充值单已关闭却收到回调,拒绝入账 单%s 金额%d分", rechargeNo, amountCents)
			return ErrRechargeClosed
		}
		// 金额防线与订单回调同一口径:不一致绝不入账。
		if r.Amount != amountCents {
			logger.Warnf("[wallet][告警] 充值回调金额不一致 单%s 期望%d分 实际%d分", rechargeNo, r.Amount, amountCents)
			return fmt.Errorf("充值金额不一致")
		}
		// 先「抢占」这条充值单,再动钱 —— 顺序至关重要。
		//
		// 条件更新(status 0→1)必须在加钱**之前**执行:否则两个并发回调会同时
		// 读到 status=0、各自加一次钱,最后条件更新只有一个成功,但钱已经加了两遍。
		// 把抢占放在前面,UPDATE 会持有该行的写锁,后来的回调等它提交后
		// affected=0,直接按幂等成功返回 —— 一分钱都不会多加。
		changed, err := dao.MarkRechargePaidTx(tx, rechargeNo, channel, channelTradeNo)
		if err != nil {
			return err
		}
		if !changed {
			// 另一个并发回调已抢占并入账(行锁已释放 ⇒ 对方已提交)。
			logger.Infof("[wallet] 充值单已由并发回调入账,按幂等处理 单%s", rechargeNo)
			return nil
		}
		if _, _, err := dao.AdjustWalletTx(tx, r.CustomerID, r.Amount, po.WalletTxnRecharge, "", channelTradeNo, "储值充值", ""); err != nil {
			return err
		}
		// 赠送额入赠金桶(独立于本金:赠金不可退现,混进本金就分不清了)。
		if r.GiftAmount > 0 {
			if _, _, err := dao.AdjustWalletTx(tx, r.CustomerID, r.GiftAmount, po.WalletTxnGift, "", channelTradeNo, "充值赠送", ""); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrRechargeNotFound) && !errors.Is(err, ErrRechargeClosed) {
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
//
// 扣减顺序:先赠金、再本金(见 dao.splitDelta),流水里记下赠金占比,
// 退款时据此判断「有多少本金可退」。
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
		if _, _, err := dao.AdjustWalletTx(tx, customerID, -o.TotalAmount, po.WalletTxnConsume, orderNo, tradeNo, "", ""); err != nil {
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

// OrderOwner 读订单归属的会员 ID(0 = 匿名单/订单不存在)。
// 供「顾客撤销自己发起的支付」这类一次性归属判断使用。
func OrderOwner(orderNo string) int {
	return dao.OrderCustomer(orderNo)
}

// ============================================================================
// 混合支付(余额抵扣 + 渠道支付剩余)
// ============================================================================

// MixedPayPreparation 混合支付第一步(扣余额)的结果。
//
// PayCents > 0 时,调用方(handler)需要再向渠道发起一笔「差额」支付;
// 发起失败必须调 AbortMixedPay 把余额退回 —— 钱先扣了却没有收款单,是资损。
type MixedPayPreparation struct {
	OrderID     int
	WalletCents int64 // 本次实际扣掉的余额(分)
	PayCents    int64 // 还需渠道支付的金额(分);0 表示余额已付清
	// FirstTime 余额已把订单付清(先付模式据此出厨单)。
	FirstTime bool
}

// PrepareMixedPay 混合支付第一步:在事务内扣余额并落一条已支付的储值流水。
//
// 为什么先扣余额再拉起渠道:渠道支付是异步的,如果先付渠道再扣余额,
// 余额不足时顾客的钱已经进了商户号,只能再发起一笔退款 —— 体验与对账都更差。
// 先扣余额的代价是「顾客放弃渠道支付时余额被占用」,由 AbortMixedPay(即时)
// 与后台超时释放(ReleaseMixedHoldsOnce)兜底。
//
// 幂等/并发:订单行条件更新(MarkOrderPaidByChannelTx)决定谁真正把订单付清;
// 未付清时由 remaining = 总额 - 已入账额 保证不会多扣。
func PrepareMixedPay(ctx context.Context, customerID int, orderNo string, walletCents int64) (MixedPayPreparation, error) {
	var prep MixedPayPreparation
	if walletCents <= 0 {
		return prep, errors.New("请选择要使用的储值金额")
	}
	err := store.WithTx(func(tx *sql.Tx) error {
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
		// 归属:只能付自己的单;匿名单由本次支付认领(与纯余额支付同一口径)。
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
		paid, err := dao.SumPaidPaymentsTx(tx, orderNo)
		if err != nil {
			return err
		}
		remaining := o.TotalAmount - paid
		if remaining <= 0 {
			return errors.New("订单已支付")
		}
		// 顾客要用的额度大于还差的金额时收敛到差额:多扣的余额对顾客没有任何好处,
		// 反而要再走一次退款。
		if walletCents > remaining {
			walletCents = remaining
		}
		tradeNo := mixedWalletTradeNo(orderNo)
		if _, _, err := dao.AdjustWalletTx(tx, customerID, -walletCents, po.WalletTxnConsume, orderNo, tradeNo, "", ""); err != nil {
			if errors.Is(err, dao.ErrWalletInsufficient) {
				return errors.New("储值余额不足，请调整使用金额")
			}
			return err
		}
		if err := dao.InsertPaymentTx(tx, orderNo, PayChannelWallet, walletCents, ""); err != nil {
			return err
		}
		if _, err := dao.MarkPaymentPaidTx(tx, orderNo, PayChannelWallet, tradeNo); err != nil {
			return err
		}
		prep.OrderID = o.OrderID
		prep.WalletCents = walletCents
		prep.PayCents = remaining - walletCents
		if prep.PayCents == 0 {
			changed, err := dao.MarkOrderPaidByChannelTx(tx, orderNo, "储值余额", tradeNo, PayChannelWallet)
			if err != nil {
				return err
			}
			if !changed {
				return errors.New("订单已支付")
			}
			prep.FirstTime = true
		}
		return nil
	})
	if err != nil {
		return MixedPayPreparation{}, err
	}
	logger.CtxInfof(ctx, "[wallet] 混合支付扣余额 订单%s 会员%d 余额%d分 待渠道%d分",
		orderNo, customerID, prep.WalletCents, prep.PayCents)
	return prep, nil
}

// mixedWalletTradeNo 生成混合支付储值流水的本地交易号。
//
// 不能再用 "W"+订单号:同一订单可能先后发起多次混合支付(放弃后重来),
// 而 (channel, channel_trade_no) 是唯一索引 —— 复用会撞约束,第二次扣款直接失败。
func mixedWalletTradeNo(orderNo string) string {
	return fmt.Sprintf("W%s-%d", orderNo, time.Now().UnixNano())
}

// AbortMixedPay 撤销一次未完成的混合支付:把已扣的余额原路退回,并关闭待支付的渠道流水。
//
// 调用时机:渠道下单失败、落支付流水失败、后台超时释放。
// 订单已付清时直接返回(钱不该退);余额退回按**本金/赠金原样还原**,
// 不能一律退进本金桶 —— 那会把门店赠送的赠金洗成可退现金。
func AbortMixedPay(ctx context.Context, orderNo, reason string) error {
	released := int64(0)
	err := store.WithTx(func(tx *sql.Tx) error {
		o, err := dao.GetOrderAmountTx(tx, orderNo)
		if err != nil {
			return err
		}
		if o.PayStatus == 1 {
			return nil // 已付清:渠道的钱到了,不能退余额
		}
		cid, err := dao.OrderCustomerTx(tx, orderNo)
		if err != nil {
			return err
		}
		if cid <= 0 {
			return errors.New("订单无会员归属，无法退回储值余额")
		}
		payments, err := dao.ListPaidPaymentsTx(tx, orderNo)
		if err != nil {
			return err
		}
		var walletPaid int64
		var walletIDs []int
		for _, p := range payments {
			if p.Channel == PayChannelWallet {
				walletPaid += p.Amount
				walletIDs = append(walletIDs, p.PaymentID)
			}
		}
		if walletPaid > 0 {
			principal, gift := walletPaid, int64(0)
			if comp, found, e := dao.OrderConsumeComposition(tx, orderNo); e != nil {
				return e
			} else if found {
				// 原样还原两个桶:赠金退回赠金桶,本金退回本金桶。
				principal, gift = comp.Principal-comp.Refunded, comp.Gift
				if principal < 0 {
					principal = 0
				}
				if principal > walletPaid {
					principal = walletPaid
				}
				if gift > walletPaid-principal {
					gift = walletPaid - principal
				}
			}
			if principal > 0 {
				if _, _, e := dao.AdjustWalletTx(tx, cid, principal, po.WalletTxnRefund, orderNo, "", reason, "system"); e != nil {
					return e
				}
			}
			if gift > 0 {
				if _, _, e := dao.AdjustWalletTx(tx, cid, gift, po.WalletTxnGiftBack, orderNo, "", reason+"（赠金原路返回）", "system"); e != nil {
					return e
				}
			}
			for _, id := range walletIDs {
				if e := dao.MarkPaymentStatusTx(tx, id, dao.PayStatusRefunded); e != nil {
					return e
				}
			}
			released = walletPaid
		}
		// 关闭本单所有待支付的渠道流水:它们对应的渠道单由调用方(handler)负责关。
		if _, err := tx.Exec(`UPDATE tb_payment SET status=?, update_time=? WHERE order_no=? AND status=?`,
			dao.PayStatusClosed, store.Now(), orderNo, dao.PayStatusPending); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logger.CtxErrorf(ctx, "[wallet][告警] 撤销混合支付失败(余额可能被占用,请人工核对) 订单%s: %v", orderNo, err)
		return err
	}
	if released > 0 {
		logger.CtxInfof(ctx, "[wallet] 已撤销混合支付并退回余额 订单%s 金额%d分 原因:%s", orderNo, released, reason)
	}
	return nil
}

// ListMixedHoldCandidates 列出「余额已扣、渠道仍未支付」且已超时的订单(后台释放用)。
//
// 判定三件同时成立:订单未支付 + 有已支付的储值流水 + 有超时未支付的渠道流水。
func ListMixedHoldCandidates(before string, limit int) ([]string, error) {
	rows, err := store.DB.Query(`SELECT DISTINCT o.order_no FROM tb_order o
		JOIN tb_payment w ON w.order_no=o.order_no AND w.channel=? AND w.status=?
		JOIN tb_payment p ON p.order_no=o.order_no AND p.channel<>? AND p.status=?
		WHERE o.pay_status=0 AND p.create_time<? LIMIT ?`,
		PayChannelWallet, dao.PayStatusPaid, PayChannelWallet, dao.PayStatusPending, before, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return nil, err
		}
		out = append(out, no)
	}
	return out, rows.Err()
}

// WalletRefundForOrder 余额支付订单的退款:原路退回 = 退回储值本金(同步完成)。
// 渠道退款不需要 —— 余额本来就在店里,不必向微信/支付宝发起退款。
//
// 只退本金,且不超过该单消费时实际从本金扣掉的部分:赠金是白送的,退成现金
// 就等于「充 100 送 10 → 用赠金买单 → 退款拿回本金」的无限套现。
func WalletRefundForOrder(orderNo string, refundCents int64, reason string) error {
	err := store.WithTx(func(tx *sql.Tx) error {
		cid, err := dao.OrderCustomerTx(tx, orderNo)
		if err != nil {
			return err
		}
		if cid <= 0 {
			return errors.New("订单无会员归属，无法退回储值余额")
		}
		// 可退本金上限:该单消费的本金部分 - 已退本金。
		// 查不到消费流水(历史数据)时按「全额可退」放行 —— 那时的余额全是本金。
		if comp, found, err := dao.OrderConsumeComposition(tx, orderNo); err != nil {
			return err
		} else if found {
			remaining := comp.Principal - comp.Refunded
			if refundCents > remaining {
				if comp.Gift > 0 {
					return fmt.Errorf("该单储值消费含赠金 %s 元（赠金不退），最多可退本金 %s 元",
						po.FormatYuan(comp.Gift), po.FormatYuan(remaining))
				}
				return fmt.Errorf("最多可退 %s 元", po.FormatYuan(remaining))
			}
		}
		_, _, err = dao.AdjustWalletTx(tx, cid, refundCents, po.WalletTxnRefund, orderNo, "", reason, "")
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
		_, _, err = dao.AdjustWalletTx(tx, cid, -refundCents, po.WalletTxnAdjust, orderNo, "", "退款入账失败自动回滚", "system")
		return err
	})
	if err != nil {
		logger.Errorf("[wallet][告警] 退款回滚失败(余额与退款单可能不一致,请人工核对) 订单%s 金额%d分: %v", orderNo, refundCents, err)
	}
	return err
}

// ============================================================================
// 退余额(钱出店)
// ============================================================================

// WalletWithdrawResult 退余额结果(供审计与响应文案)。
type WalletWithdrawResult struct {
	Principal int64 // 退回的本金(分)
	Gift      int64 // 一并清零的赠金(分,不退给顾客)
}

// AdminWalletWithdraw 退余额:把会员的储值余额清零,门店据此线下退款(现金/微信)。
//
// 这是「顾客的预付款能拿回来」这条合规要求的落点(见 docs/compliance/prepaid-card.md):
// 没有这个通道,会员一旦不想来了,钱就永久锁在店里 —— 法律上站不住。
//
// 本金与赠金必须分别清零:赠金是门店白送的,作废即可(不产生现金流出),
// 本金才是真要退给顾客的钱。返回的本金金额写进审计,与门店实际付款对得上。
//
// 会员注销前的余额清零也走这里(或后台调账),保证「注销时余额必为 0」。
func AdminWalletWithdraw(customerID int, reason, operator string) (WalletWithdrawResult, error) {
	var res WalletWithdrawResult
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return res, errors.New("请填写退余额原因")
	}
	if _, err := dao.GetCustomerByID(customerID); err != nil {
		return res, errors.New("会员不存在")
	}
	err := store.WithTx(func(tx *sql.Tx) error {
		balance, gift, err := walletBalanceTx(tx, customerID)
		if err != nil {
			return err
		}
		if balance <= 0 && gift <= 0 {
			return errors.New("该会员储值余额为 0，无需退款")
		}
		if balance > 0 {
			if _, _, err := dao.AdjustWalletTx(tx, customerID, -balance, po.WalletTxnWithdraw, "", "", reason, operator); err != nil {
				return err
			}
			res.Principal = balance
		}
		if gift > 0 {
			// 赠金作废用独立类型 gift_void,不复用 gift 的负数:
			// 复用会让 total_gift(累计赠送)变小,「一共送了多少」变成净额,
			// 顾客端也会看到「赠送 -20.00」这种像扣错账的流水。
			if _, _, err := dao.AdjustWalletTx(tx, customerID, -gift, po.WalletTxnGiftVoid, "", "", "退余额，赠金作废："+reason, operator); err != nil {
				return err
			}
			res.Gift = gift
		}
		return nil
	})
	if err != nil {
		logger.Warnf("[wallet] 退余额失败(customer=%d): %v", customerID, err)
		return WalletWithdrawResult{}, err
	}
	logger.Infof("[wallet] 退余额 会员%d 本金%.2f元 赠金作废%.2f元 原因:%s 操作人:%s",
		customerID, po.ToYuan(res.Principal), po.ToYuan(res.Gift), reason, operator)
	return res, nil
}

// walletBalanceTx 事务内读会员的本金与赠金余额(无账户返回 0,0)。
// 与 EnsureWalletTx 的区别:它不建账户 —— 退余额不该给一个从没充过值的会员
// 凭空造一个账户行。
func walletBalanceTx(tx *sql.Tx, customerID int) (int64, int64, error) {
	var balance, gift int64
	err := tx.QueryRow(`SELECT COALESCE(balance,0), COALESCE(balance_gift,0) FROM tb_wallet WHERE customer_id=?`,
		customerID).Scan(&balance, &gift)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return balance, gift, err
}

// ============================================================================
// 手机号绑定
// ============================================================================

// BindCustomerPhone 用 getPhoneNumber 的 code 换手机号并绑到当前会员。
// 返回脱敏后的手机号(完整号码不外发)。同一手机号全店只能属于一个会员。
//
// 手机号就是一种登录方式(provider=phone),只存 tb_customer_identity,
// 不在档案表再加一列 —— 身份层当初就是为这一步设计的。
// 对储值而言它还有第二重意义:绑定即记名,余额上限从 1000 提到 5000。
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
//
// 正数一律进**赠金桶**(biz_type=gift):调账是门店不收钱的补偿/赠送,
// 走赠金桶才不会凭空造出「可退现金」的本金 —— 那是能被套现的。
// 负数走本金桶的 adjust(纠错:把多记的钱扣回去)。
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
	maxYuan := settingInt(SettingWalletAdjustMax, walletAdjustMaxDefault)
	if amountYuan > float64(maxYuan) || amountYuan < -float64(maxYuan) {
		return fmt.Errorf("单次调账金额不得超过 %d 元", maxYuan)
	}
	if _, err := dao.GetCustomerByID(customerID); err != nil {
		return errors.New("系统繁忙，请稍后再试")
	}
	cents := po.ToCents(amountYuan)
	bizType := po.WalletTxnGift
	if cents < 0 {
		bizType = po.WalletTxnAdjust
	}
	if _, _, err := dao.AdjustWallet(customerID, cents, bizType, "", "", reason, operator); err != nil {
		if errors.Is(err, dao.ErrWalletInsufficient) {
			return errors.New("扣减后余额将为负，请核对金额")
		}
		logger.Warnf("[wallet] 后台调账失败(customer=%d): %v", customerID, err)
		return errors.New("调账失败，请稍后再试")
	}
	logger.Infof("[wallet] 后台调账 会员%d %+.2f元 原因:%s 操作人:%s", customerID, amountYuan, reason, operator)
	return nil
}
