package po

// Wallet 对应 tb_wallet 表:会员的储值账户(1:1,主键即 customer_id)。
//
// 金额一律以「分」存(全库口径,见 migrations/README.md 第七节)。
// balance 只允许在 wallet DAO 的事务内增减(带 balance>=? 守卫);
// 任何字段对不上账,以 tb_wallet_txn 流水为准 —— 它是唯一事实来源。
type Wallet struct {
	CustomerID    int
	Balance       int64 // 当前余额(分)
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
)

// Recharge 对应 tb_recharge 表:顾客发起的一笔储值充值(微信 JSAPI 支付)。
//
// 单号固定以 R 开头(订单是 D 开头),支付回调 handler 靠前缀分流。
// status: 0 待支付 / 1 已入账 —— 回调用条件更新做幂等,微信重发不会重复加钱。
type Recharge struct {
	RechargeID     int
	RechargeNo     string
	CustomerID     int
	Amount         int64
	Status         int // 0 待支付 / 1 已入账
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
)

// MaskPhone 手机号脱敏(139****0001):日志与列表展示用,完整号码不外发。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
