package po

// Wallet 对应 tb_wallet 表:会员的储值账户(1:1,主键即 customer_id)。
//
// 金额一律以「分」存(全库口径,见 migrations/README.md 第七节)。
// balance 只允许在 wallet DAO 的事务内增减(带 balance>=? 守卫);
// 任何字段对不上账,以 tb_wallet_txn 流水为准 —— 它是唯一事实来源。
//
// 两个余额桶(会员系统的地基):
//
//	balance      本金:顾客真金白银充进来的钱,可退、可退现金;
//	balance_gift 赠金:充值赠送/活动赠送,只能消费、不可退现。
//
// 分桶是为了「赠送」这门生意能被审计:赠金若与本金混在一个数里,
// 退余额时无法回答「这笔钱该不该退给顾客」,只能一刀切全退(可被套现)。
type Wallet struct {
	CustomerID    int
	Balance       int64 // 本金余额(分)
	BalanceGift   int64 // 赠金余额(分)
	TotalRecharge int64 // 累计充值(分)
	TotalConsume  int64 // 累计消费(分)
	TotalGift     int64 // 累计赠送(分)
	CreateTime    string
	UpdateTime    string
}

// 储值流水的业务类型(biz_type)。
const (
	WalletTxnRecharge = "recharge" // 充值(微信支付回调入账)
	WalletTxnConsume  = "consume"  // 消费(余额支付订单)
	WalletTxnRefund   = "refund"   // 退回(余额支付订单退款 / 系统回滚)
	WalletTxnGift     = "gift"     // 后台赠送(不收钱的储值,如开业赠送)
	WalletTxnAdjust   = "adjust"   // 后台扣减(纠错,需填原因)
	// WalletTxnGiftVoid 赠金作废(退余额时把赠金一并清零)。
	//
	// 为什么不复用 gift 的负数:复用会让 total_gift(累计赠送)被减掉,
	// 「一共送了多少」这个统计就变成了净额;顾客看到的流水也会是
	// 「赠送 -20.00」,读起来像扣错账。作废是独立事实,单列一类。
	WalletTxnGiftVoid = "gift_void"
	// WalletTxnGiftBack 赠金退回(撤销一次尚未完成的赠送/消费,把赠金原样放回赠金桶)。
	//
	// 只在「混合支付放弃后撤销」时出现:此时那笔消费根本没成立,
	// 赠金必须原样回桶。用 gift 的另一种说法会让 total_gift(累计赠送)被重复累加,
	// 顾客看到的「累计赠送」凭空变多。
	WalletTxnGiftBack = "gift_back"
	// WalletTxnWithdraw 退余额(注销/顾客要求退现,本金清零出账)。
	//
	// 与 adjust 的区别:adjust 是「纠错」,钱还在店里;withdraw 是「钱出店了」——
	// 门店要么线下退现金、要么走微信退款,这一笔必须与那次实际付款对得上。
	WalletTxnWithdraw = "withdraw"
)

// Recharge 对应 tb_recharge 表:顾客发起的一笔储值充值(微信 JSAPI 支付)。
//
// 单号固定以 R 开头(订单是 D 开头),支付回调 handler 靠前缀分流。
// status: 0 待支付 / 1 已入账 / 2 已关闭 —— 回调用条件更新做幂等,微信重发不会重复加钱。
//
// GiftAmount 是本笔充值附带的赠送金额(会员系统的「充 100 送 10」):
// 充值成功时本金入 balance、赠金入 balance_gift,两笔一起记在同一条流水里。
type Recharge struct {
	RechargeID     int
	RechargeNo     string
	CustomerID     int
	Amount         int64 // 本金(分)
	GiftAmount     int64 // 赠送(分)
	Status         int   // 0 待支付 / 1 已入账 / 2 已关闭
	Channel        string
	ChannelTradeNo string
	PayTime        string
	CreateTime     string
	UpdateTime     string
}

// 充值单状态。
const (
	RechargeStatusPending = 0
	RechargeStatusPaid    = 1
	// RechargeStatusClosed 已关闭:超过支付时限仍未到账(顾客放弃 / 渠道单已关)。
	// 关闭态是**终态**,回调再来也不入账 —— 否则一笔顾客早已放弃的充值,
	// 可能在他注销会员几个月后被渠道延迟通知复活,凭空多出一笔余额。
	RechargeStatusClosed = 2
)

// WalletAgreement 会员对《储值规则》的同意留痕(一人一行,customer_id 为主键)。
//
// 合规要求(详见 docs/compliance/prepaid-card.md):发卡企业须向购卡人明示
// 章程/协议。留痕的意义不是"防顾客",而是争议时能证明"当时展示的是哪一版规则"。
// rules_version 变更(门店改了规则文本)后,顾客需重新确认。
type WalletAgreement struct {
	CustomerID int
	RulesVer   string
	AgreedTime string
	CreateTime string
	UpdateTime string
}

// MaskPhone 手机号脱敏(139****0001):日志与列表展示用,完整号码不外发。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
