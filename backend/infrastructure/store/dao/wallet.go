// 储值账户(tb_wallet)、储值流水(tb_wallet_txn)、充值单(tb_recharge)与
// 《储值规则》同意留痕(tb_wallet_agreement)的数据访问。
//
// 四条不变式:
//  1. 余额只在事务内增减,UPDATE 必须带 balance >= ? / balance_gift >= ? 守卫 ——
//     并发消费/退款下「先查再扣」必超扣,守卫行不命中(affected=0)即余额不足;
//  2. 每一次余额变动都必须落一条 tb_wallet_txn,balance_after/gift_after 由同一
//     事务回读算出,逐行累加必须等于当前余额(对账脚本靠这条不变式);
//  3. 充值入账用条件更新(tb_recharge.status 0→1)做幂等,微信重发不会重复加钱;
//     已关闭(2)的单是终态,回调再来也绝不入账;
//  4. 本金与赠金分桶:本金可退、赠金不可退。消费先扣赠金再扣本金,
//     退款只退本金 —— 混在一个数里就无法回答「这笔钱该不该退给顾客」。
package dao

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// walletCols 与 scanWallet 的扫描顺序是唯一对应关系,改一边必须改另一边。
const walletCols = `customer_id, balance, balance_gift, total_recharge, total_consume, total_gift, create_time, update_time`

func scanWallet(row *sql.Row) (*po.Wallet, error) {
	var w po.Wallet
	err := row.Scan(&w.CustomerID, &w.Balance, &w.BalanceGift, &w.TotalRecharge, &w.TotalConsume, &w.TotalGift, TS(&w.CreateTime), TS(&w.UpdateTime))
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// GetWallet 取储值账户。不存在返回 (nil, nil) —— 会员从未充过值就没有账户,
// 读侧按余额 0 展示,不为每个会员预建一行。
func GetWallet(customerID int) (*po.Wallet, error) {
	w, err := scanWallet(store.DB.QueryRow(`SELECT `+walletCols+` FROM tb_wallet WHERE customer_id=?`, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// EnsureWalletTx 事务内确保账户存在(首次充值/消费/调账时建),返回当前本金与赠金余额。
//
// 用 INSERT IGNORE 而不是「先查再插」:并发首充时两个请求都查不到、都会插入,
// 唯一主键兜底只让一个成功,另一个被忽略 —— 行为两库一致且天然并发安全。
//
// 回读余额而不是「按 0 推导」:首次建账户也可能撞上并发的首充/首赠,
// 回读拿到的一定是本事务可见的权威值。
func EnsureWalletTx(tx *sql.Tx, customerID int) (int64, int64, error) {
	stmt := store.InsertIgnoreInto("tb_wallet",
		"customer_id", "balance", "balance_gift", "create_time")
	if _, err := tx.Exec(stmt, customerID, 0, 0, store.Now()); err != nil {
		return 0, 0, err
	}
	var balance, gift int64
	if err := tx.QueryRow(`SELECT balance, balance_gift FROM tb_wallet WHERE customer_id=?`, customerID).
		Scan(&balance, &gift); err != nil {
		return 0, 0, err
	}
	return balance, gift, nil
}

// LockWalletTx 事务内确保账户存在并**取行写锁**,返回锁定后的本金与赠金余额。
//
// 为什么必须在锁内读:消费要按「当前赠金」决定先扣多少赠金、再扣多少本金。
// 若在锁外读(EnsureWalletTx 是普通 SELECT),MySQL 下读的是快照,并发两笔消费
// 会拿到同一个赠金数、各自按它拆分:先提交的那笔顺利扣掉,后一笔因赠金桶守卫
// 不命中而失败 —— 明明总余额够付,却报「余额不足」。
//
// 取锁手法用自赋值 UPDATE(两库通用):SQLite 没有 SELECT ... FOR UPDATE 语法,
// 而 MySQL 的 UPDATE 会锁住命中的行,等价于加锁读。
func LockWalletTx(tx *sql.Tx, customerID int) (int64, int64, error) {
	if _, _, err := EnsureWalletTx(tx, customerID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(`UPDATE tb_wallet SET update_time=update_time WHERE customer_id=?`, customerID); err != nil {
		return 0, 0, err
	}
	var balance, gift int64
	if err := tx.QueryRow(`SELECT balance, balance_gift FROM tb_wallet WHERE customer_id=?`, customerID).
		Scan(&balance, &gift); err != nil {
		return 0, 0, err
	}
	return balance, gift, nil
}

// ErrWalletInsufficient 余额不足。用哨兵错误而非文案:service 层据此返回可读提示,
// 调用方(如余额支付)不需要解析字符串。
var ErrWalletInsufficient = errors.New("储值余额不足")

// walletSplit 一次余额变动在两个桶之间的拆分结果(单位分,带符号)。
//
// deltaBalance / deltaGift 分别是要加减到本金桶与赠金桶的量;
// bucketAffected 之外的桶传 0 —— UPDATE 语句对两桶统一带守卫,传 0 的桶不受影响。
type walletSplit struct {
	deltaBalance int64 // 本金变动(带符号)
	deltaGift    int64 // 赠金变动(带符号)
	// totalForStat 计入累计列的量(只取正向业务口径,见 AdjustWalletTx 的说明)。
	totalForStat int64
}

// splitDelta 按业务类型决定「这笔钱动哪个桶」。
//
// 规则(唯一定义处,service 与对账脚本都以它为准):
//   - gift / gift_void / gift_back → 赠金桶(赠送 / 作废 / 撤销后原样放回)
//   - consume          → 先扣赠金、再扣本金(赠金先消耗,门店成本最低且顾客感知一致)
//   - 其余类型         → 本金桶
//
// curGift 是当前赠金余额,用于 consume 时判断能扣多少赠金。
func splitDelta(bizType string, deltaCents, curGift int64) walletSplit {
	if bizType == po.WalletTxnGift || bizType == po.WalletTxnGiftVoid || bizType == po.WalletTxnGiftBack {
		return walletSplit{deltaGift: deltaCents}
	}
	if bizType != po.WalletTxnConsume || deltaCents >= 0 {
		return walletSplit{deltaBalance: deltaCents}
	}
	// 消费:先扣赠金、再扣本金。
	need := -deltaCents
	useGift := curGift
	if useGift > need {
		useGift = need
	}
	return walletSplit{
		deltaBalance: -(need - useGift),
		deltaGift:    -useGift,
		totalForStat: need,
	}
}

// AdjustWalletTx 事务内调整余额并落一条流水,返回变动后的本金余额与赠金余额。
//
// deltaCents 带符号:正数入账、负数出账;出账时 UPDATE 的守卫不命中即余额不足,
// 返回 ErrWalletInsufficient(事务由调用方回滚)。
//
// 累计列只记三类正向业务口径:充值/消费/赠送;退回与纠错性扣减不动累计,
// 否则「退回一笔消费」会把 total_consume 减成负数,报表口径就乱了。
//
// 先 UPDATE(带守卫),再在同一事务里回读真实余额作为 balance_after / gift_after。
//
// 不用「先 SELECT 算好再写」:普通 SELECT 在 MySQL 下读的是快照,并发扣款时
// 算出的「变动后余额」可能基于旧值,流水与余额就会对不上。UPDATE 的 WHERE
// 守卫在行锁下用**当前值**判定,之后回读的一定是本事务写入后的权威值。
func AdjustWalletTx(tx *sql.Tx, customerID int, deltaCents int64, bizType, orderNo, paymentNo, reason, operator string) (int64, int64, error) {
	if deltaCents == 0 {
		return 0, 0, errors.New("变动金额不能为 0")
	}
	// 锁内读两桶再拆分:锁外的普通 SELECT 在 MySQL 下是快照读,
	// 并发消费会按同一份旧赠金各自拆分,后一笔被赠金桶守卫误拒(见 LockWalletTx)。
	_, curGift, err := LockWalletTx(tx, customerID)
	if err != nil {
		return 0, 0, err
	}

	sp := splitDelta(bizType, deltaCents, curGift)

	var totalSQL string
	switch bizType {
	case po.WalletTxnRecharge:
		totalSQL = `, total_recharge=total_recharge+?`
	case po.WalletTxnConsume:
		totalSQL = `, total_consume=total_consume+?`
	case po.WalletTxnGift:
		totalSQL = `, total_gift=total_gift+?`
	default:
		totalSQL = ``
	}

	stmt := `UPDATE tb_wallet SET balance=balance+?, balance_gift=balance_gift+?` + totalSQL +
		`, update_time=? WHERE customer_id=? AND balance+? >= 0 AND balance_gift+? >= 0`
	args := []interface{}{sp.deltaBalance, sp.deltaGift}
	if totalSQL != "" {
		if bizType == po.WalletTxnConsume {
			// 消费的累计口径记「整单消费额」,不区分桶:
			// total_consume 回答的是「这位会员一共消费了多少」,拆桶会让它对不上订单额。
			args = append(args, sp.totalForStat)
		} else {
			args = append(args, deltaCents)
		}
	}
	args = append(args, store.Now(), customerID, sp.deltaBalance, sp.deltaGift)
	res, err := tx.Exec(stmt, args...)
	if err != nil {
		return 0, 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, 0, ErrWalletInsufficient
	}
	var newBalance, newGift int64
	if err := tx.QueryRow(`SELECT balance, balance_gift FROM tb_wallet WHERE customer_id=?`, customerID).
		Scan(&newBalance, &newGift); err != nil {
		return 0, 0, err
	}

	_, err = tx.Exec(`INSERT INTO tb_wallet_txn(customer_id, biz_type, amount, balance_after, gift_amount, gift_after,
		order_no, payment_no, reason, operator, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		customerID, bizType, deltaCents, newBalance, sp.deltaGift, newGift, orderNo, paymentNo, reason, operator, store.Now())
	if err != nil {
		return 0, 0, err
	}
	return newBalance, newGift, nil
}

// AdjustWallet 自带事务版本的 AdjustWalletTx(后台调账/退余额用)。
// 参数与返回值同 AdjustWalletTx。
func AdjustWallet(customerID int, deltaCents int64, bizType, orderNo, paymentNo, reason, operator string) (int64, int64, error) {
	var balance, gift int64
	err := store.WithTx(func(tx *sql.Tx) error {
		var e error
		balance, gift, e = AdjustWalletTx(tx, customerID, deltaCents, bizType, orderNo, paymentNo, reason, operator)
		return e
	})
	return balance, gift, err
}

// ============================================================================
// 订单消费构成(退款只能退本金,靠它算上限)
// ============================================================================

// ConsumeComposition 某订单用储值支付时的消费构成(单位分,均为正数)。
type ConsumeComposition struct {
	Total     int64 // 该单从储值扣掉的总额
	Gift      int64 // 其中来自赠金桶的部分(不退)
	Principal int64 // 其中来自本金桶的部分(可退上限)
	Refunded  int64 // 已退回本金的累计额
}

// OrderConsumeComposition 事务内统计某订单的储值消费构成与已退金额。
//
// 赠金不可退现:顾客用赠金消费的部分退款时不返还本金,否则
// 「充 100 送 10 → 用赠金买单 → 退款拿回本金」可以无限套现。
// 查不到消费流水(历史数据或非常规路径)时 Found=false,调用方按「全额可退」处理。
func OrderConsumeComposition(tx *sql.Tx, orderNo string) (ConsumeComposition, bool, error) {
	var c ConsumeComposition
	var total, gift, cnt int64
	// 一次查出「总额/赠金额/流水条数」:cnt 用于区分「没有消费流水」与
	// 「消费额恰好为 0」,后者不能当成查不到(否则会绕过赠金不退的约束)。
	err := tx.QueryRow(`SELECT COALESCE(SUM(-amount),0), COALESCE(SUM(-gift_amount),0), COUNT(*)
		FROM tb_wallet_txn WHERE order_no=? AND biz_type=? AND amount<0`,
		orderNo, po.WalletTxnConsume).Scan(&total, &gift, &cnt)
	if err != nil {
		return c, false, err
	}
	if cnt == 0 {
		return c, false, nil
	}
	c.Total = total
	c.Gift = gift
	c.Principal = total - gift
	if c.Principal < 0 {
		c.Principal = 0
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM tb_wallet_txn
		WHERE order_no=? AND biz_type=?`, orderNo, po.WalletTxnRefund).Scan(&c.Refunded); err != nil {
		return c, false, err
	}
	return c, true, nil
}

// ============================================================================
// 《储值规则》同意留痕
// ============================================================================

// GetWalletAgreementVer 取会员当前同意的规则版本(未同意返回空串)。
func GetWalletAgreementVer(customerID int) string {
	var ver string
	_ = store.DB.QueryRow(`SELECT rules_ver FROM tb_wallet_agreement WHERE customer_id=?`, customerID).Scan(&ver)
	return ver
}

// SaveWalletAgreement 记录会员同意了某一版规则(一人一行,重复同意按整行替换)。
func SaveWalletAgreement(customerID int, rulesVer string) error {
	_, err := store.DB.Exec(store.InsertReplaceInto("tb_wallet_agreement",
		"customer_id", "rules_ver", "agreed_time", "create_time", "update_time"),
		customerID, rulesVer, store.Now(), store.Now(), store.Now())
	return err
}

// DeleteWalletAgreementTx 事务内删除同意留痕(会员注销时随档案一起清)。
func DeleteWalletAgreementTx(tx *sql.Tx, customerID int) error {
	_, err := tx.Exec(`DELETE FROM tb_wallet_agreement WHERE customer_id=?`, customerID)
	return err
}

// ============================================================================
// 手机号(实名判定)
// ============================================================================

// GetCustomerPhone 取会员绑定的手机号(未绑定/查询失败返回空串)。
// 只用于顾客端「我的」页展示脱敏号,完整号码不外发由调用方负责脱敏。
func GetCustomerPhone(customerID int) string {
	var phone string
	_ = store.DB.QueryRow(`SELECT external_id FROM tb_customer_identity WHERE customer_id=? AND provider=? LIMIT 1`,
		customerID, po.CustomerProviderPhone).Scan(&phone)
	return phone
}

// HasCustomerPhone 是否已绑定手机号 —— 储值的「记名」判定口径
// (未记名卡受单张 1000 元上限约束,见 docs/compliance/prepaid-card.md)。
func HasCustomerPhone(customerID int) bool {
	return GetCustomerPhone(customerID) != ""
}

// ============================================================================
// 流水查询
// ============================================================================

// WalletTxnRow 储值流水的对外视图(顾客端与后台共用;金额以「元」下发,口径同订单)。
type WalletTxnRow struct {
	TxnID        int64   `json:"txnId"`
	BizType      string  `json:"bizType"`
	Amount       float64 `json:"amount"`
	BalanceAfter float64 `json:"balanceAfter"`
	// GiftAmount 本笔变动中属于赠金桶的部分(元,带符号)。
	// 顾客端据此把「-50.00(含赠金 10.00)」说清楚,避免顾客以为本金少了。
	GiftAmount float64 `json:"giftAmount"`
	OrderNo    string  `json:"orderNo"`
	Reason     string  `json:"reason"`
	Operator   string  `json:"operator"`
	CreateTime string  `json:"createTime"`
}

// ListWalletTxns 分页取某会员的储值流水(按时间倒序)。
func ListWalletTxns(customerID, pageNum, pageSize int) (int, []WalletTxnRow, error) {
	if customerID <= 0 {
		return 0, []WalletTxnRow{}, nil
	}
	var total int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_wallet_txn WHERE customer_id=?`, customerID).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := store.DB.Query(`SELECT txn_id, biz_type, amount, balance_after, COALESCE(gift_amount,0), COALESCE(order_no,''),
		COALESCE(reason,''), COALESCE(operator,''), COALESCE(create_time,'')
		FROM tb_wallet_txn WHERE customer_id=? ORDER BY txn_id DESC LIMIT ? OFFSET ?`,
		customerID, pageSize, (pageNum-1)*pageSize)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]WalletTxnRow, 0, pageSize)
	for rows.Next() {
		var r WalletTxnRow
		var amount, balanceAfter, gift int64
		if err := rows.Scan(&r.TxnID, &r.BizType, &amount, &balanceAfter, &gift, &r.OrderNo, &r.Reason, &r.Operator, TS(&r.CreateTime)); err != nil {
			return 0, nil, err
		}
		// 库里存「分」,出参一律「元」(与订单金额同一口径)。
		r.Amount = po.ToYuan(amount)
		r.BalanceAfter = po.ToYuan(balanceAfter)
		r.GiftAmount = po.ToYuan(gift)
		out = append(out, r)
	}
	return total, out, rows.Err()
}

// ============================================================================
// 充值单
// ============================================================================

// newRechargeNo 生成充值单号:R + 时间戳 + 6 位随机数。
// 唯一索引兜底撞号(同秒内 1/10^6 概率,撞上由调用方重试一次)。
func newRechargeNo() string {
	return fmt.Sprintf("R%s%06d", time.Now().Format("20060102150405"), rand.Intn(1000000))
}

// InsertRecharge 建一笔待支付充值单,返回单号。撞号(唯一索引冲突)自动重试一次。
//
// giftCents 是本笔充值附带的赠送额,**建单时就冻结在单上**:回调入账只认单上的
// 金额,不重新算活动规则 —— 否则活动一改,已支付未入账的单会入成另一个数。
func InsertRecharge(customerID int, amountCents, giftCents int64) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		no := newRechargeNo()
		_, err := store.DB.Exec(`INSERT INTO tb_recharge(recharge_no, customer_id, amount, gift_amount, status, create_time)
			VALUES(?,?,?,?,0,?)`, no, customerID, amountCents, giftCents, store.Now())
		if err == nil {
			return no, nil
		}
		if attempt == 1 {
			return "", err
		}
	}
	return "", errors.New("生成充值单失败")
}

// scanRechargeFull 与 rechargeFullCols 的扫描顺序一一对应。
const rechargeFullCols = `recharge_id, recharge_no, customer_id, amount, gift_amount, status, COALESCE(channel,''),
	COALESCE(channel_trade_no,''), COALESCE(pay_time,''), COALESCE(create_time,'')`

// GetRecharge 按单号取充值单。不存在返回 (nil, nil)。
func GetRecharge(rechargeNo string) (*po.Recharge, error) {
	var r po.Recharge
	err := store.DB.QueryRow(`SELECT `+rechargeFullCols+` FROM tb_recharge WHERE recharge_no=?`, rechargeNo).
		Scan(&r.RechargeID, &r.RechargeNo, &r.CustomerID, &r.Amount, &r.GiftAmount, &r.Status, &r.Channel, &r.ChannelTradeNo, TS(&r.PayTime), TS(&r.CreateTime))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// MarkRechargePaidTx 事务内把充值单置为已入账(条件更新做幂等)。
// 返回 changed=false 表示已被本次或之前的回调入过账,调用方直接按成功应答。
func MarkRechargePaidTx(tx *sql.Tx, rechargeNo, channel, channelTradeNo string) (bool, error) {
	res, err := tx.Exec(`UPDATE tb_recharge SET status=1, channel=?, channel_trade_no=?, pay_time=?, update_time=?
		WHERE recharge_no=? AND status=0`,
		channel, channelTradeNo, store.Now(), store.Now(), rechargeNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetRechargeTx 事务内按单号取充值单。不存在返回 (nil, nil)。
func GetRechargeTx(tx *sql.Tx, rechargeNo string) (*po.Recharge, error) {
	var r po.Recharge
	err := tx.QueryRow(`SELECT recharge_id, recharge_no, customer_id, amount, gift_amount, status FROM tb_recharge WHERE recharge_no=?`,
		rechargeNo).Scan(&r.RechargeID, &r.RechargeNo, &r.CustomerID, &r.Amount, &r.GiftAmount, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListPendingRecharges 取某会员「近期仍未入账」的充值单(主动查单补账用)。
//
// 只取 status=0 的待支付单:已入账(1)无需再查,已关闭(2)是终态,
// 渠道延迟通知也不会让它复活(见 po.RechargeStatusClosed)。
func ListPendingRecharges(customerID int, since string) ([]po.Recharge, error) {
	rows, err := store.DB.Query(`SELECT `+rechargeFullCols+` FROM tb_recharge
		WHERE customer_id=? AND status=0 AND create_time>=? ORDER BY recharge_id DESC LIMIT 20`,
		customerID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]po.Recharge, 0, 8)
	for rows.Next() {
		var r po.Recharge
		if err := rows.Scan(&r.RechargeID, &r.RechargeNo, &r.CustomerID, &r.Amount, &r.GiftAmount, &r.Status,
			&r.Channel, &r.ChannelTradeNo, TS(&r.PayTime), TS(&r.CreateTime)); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExpirePendingRecharges 关闭超过时限仍未入账的充值单,返回关闭条数。
//
// 为什么必须关:微信 JSAPI 的 prepay_id 只有 2 小时有效期,过期后这单永远不可能
// 被支付;留着 status=0 既不释放(顾客可能反复看到「待入账」),也会让补账扫描
// 每次都去查渠道、白耗配额。关闭是终态,后续延迟回调一律不入账。
//
// 时限取 24 小时而不是 2 小时:给「回调比支付晚到」留出足够余量,
// 绝不为了清理而把一笔真实支付关掉 —— 关错了就是顾客的资损。
func ExpirePendingRecharges(cutoff string) (int, error) {
	res, err := store.DB.Exec(`UPDATE tb_recharge SET status=?, update_time=? WHERE status=0 AND create_time<?`,
		po.RechargeStatusClosed, store.Now(), cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ============================================================================
// 订单归属
// ============================================================================

// OrderCustomer 读订单归属(0 = 匿名单/订单不存在)。
//
// 供「顾客撤销自己的支付」这类只读校验使用:它不该为了一次归属判断开事务。
func OrderCustomer(orderNo string) int {
	var cid int
	_ = store.DB.QueryRow(`SELECT COALESCE(customer_id,0) FROM tb_order WHERE order_no=?`, orderNo).Scan(&cid)
	return cid
}

// OrderCustomerTx 事务内读订单归属(0 = 匿名单)。
func OrderCustomerTx(tx *sql.Tx, orderNo string) (int, error) {
	var cid int
	err := tx.QueryRow(`SELECT COALESCE(customer_id,0) FROM tb_order WHERE order_no=?`, orderNo).Scan(&cid)
	return cid, err
}

// ClaimOrderCustomerTx 匿名单被会员用储值余额支付时认领归属(条件更新,只认领匿名单)。
// 返回 false 表示该单已被其它会员认领,调用方应拒绝本次支付。
func ClaimOrderCustomerTx(tx *sql.Tx, orderNo string, customerID int) (bool, error) {
	res, err := tx.Exec(`UPDATE tb_order SET customer_id=?, update_time=? WHERE order_no=? AND COALESCE(customer_id,0)=0`,
		customerID, store.Now(), orderNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
