// 储值账户(tb_wallet)、储值流水(tb_wallet_txn)与充值单(tb_recharge)的数据访问。
//
// 三条不变式:
//  1. 余额只在事务内增减,UPDATE 必须带 balance >= ? 守卫 —— 并发消费/退款下
//     「先查再扣」必超扣,守卫行不命中(affected=0)即余额不足;
//  2. 每一次余额变动都必须落一条 tb_wallet_txn,balance_after 由同一事务算出,
//     逐行累加必须等于当前余额(对账脚本靠这条不变式);
//  3. 充值入账用条件更新(tb_recharge.status 0→1)做幂等,微信重发通知不会重复加钱。
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
const walletCols = `customer_id, balance, total_recharge, total_consume, total_gift, create_time, update_time`

func scanWallet(row *sql.Row) (*po.Wallet, error) {
	var w po.Wallet
	err := row.Scan(&w.CustomerID, &w.Balance, &w.TotalRecharge, &w.TotalConsume, &w.TotalGift, TS(&w.CreateTime), TS(&w.UpdateTime))
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

// EnsureWalletTx 事务内确保账户存在(首次充值/消费/调账时建),返回当前余额。
//
// 用 INSERT IGNORE 而不是「先查再插」:并发首充时两个请求都查不到、都会插入,
// 唯一主键兜底只让一个成功,另一个被忽略 —— 行为两库一致且天然并发安全。
func EnsureWalletTx(tx *sql.Tx, customerID int) (int64, error) {
	stmt := store.InsertIgnoreInto("tb_wallet",
		"customer_id", "balance", "create_time")
	if _, err := tx.Exec(stmt, customerID, 0, store.Now()); err != nil {
		return 0, err
	}
	var balance int64
	if err := tx.QueryRow(`SELECT balance FROM tb_wallet WHERE customer_id=?`, customerID).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}

// ErrWalletInsufficient 余额不足。用哨兵错误而非文案:service 层据此返回可读提示,
// 调用方(如余额支付)不需要解析字符串。
var ErrWalletInsufficient = errors.New("储值余额不足")

// AdjustWalletTx 事务内调整余额并落一条流水,返回变动后的余额。
//
// deltaCents 带符号:正数入账、负数出账;出账时 UPDATE 的 balance >= ? 守卫
// 不命中即余额不足,返回 ErrWalletInsufficient(事务由调用方回滚)。
// 累计列只记三类正向业务口径:充值/消费/赠送;退回与纠错性扣减不动累计,
// 否则「退回一笔消费」会把 total_consume 减成负数,报表口径就乱了。
func AdjustWalletTx(tx *sql.Tx, customerID int, deltaCents int64, bizType, orderNo, paymentNo, reason, operator string) (int64, error) {
	if deltaCents == 0 {
		return 0, errors.New("变动金额不能为 0")
	}
	if _, err := EnsureWalletTx(tx, customerID); err != nil {
		return 0, err
	}

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

	// 先 UPDATE(带守卫),再在同一事务里回读真实余额作为 balance_after。
	//
	// 不用「先 SELECT 算好再写」:普通 SELECT 在 MySQL 下读的是快照,并发扣款时
	// 算出的「变动后余额」可能基于旧值,流水与余额就会对不上。UPDATE 的 WHERE
	// 守卫在行锁下用**当前值**判定,之后回读的一定是本事务写入后的权威值。
	stmt := `UPDATE tb_wallet SET balance=balance+?` + totalSQL + `, update_time=? WHERE customer_id=? AND balance+? >= 0`
	args := []interface{}{deltaCents}
	if totalSQL != "" {
		args = append(args, deltaCents)
	}
	args = append(args, store.Now(), customerID, deltaCents)
	res, err := tx.Exec(stmt, args...)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrWalletInsufficient
	}
	var newBalance int64
	if err := tx.QueryRow(`SELECT balance FROM tb_wallet WHERE customer_id=?`, customerID).Scan(&newBalance); err != nil {
		return 0, err
	}

	_, err = tx.Exec(`INSERT INTO tb_wallet_txn(customer_id, biz_type, amount, balance_after, order_no, payment_no, reason, operator, create_time)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		customerID, bizType, deltaCents, newBalance, orderNo, paymentNo, reason, operator, store.Now())
	if err != nil {
		return 0, err
	}
	return newBalance, nil
}

// AdjustWallet 事务内调整余额并落一条流水(自带事务的非事务版本,后台调账用)。
// 参数与返回值同 AdjustWalletTx。
func AdjustWallet(customerID int, deltaCents int64, bizType, orderNo, paymentNo, reason, operator string) (int64, error) {
	var balance int64
	err := store.WithTx(func(tx *sql.Tx) error {
		var e error
		balance, e = AdjustWalletTx(tx, customerID, deltaCents, bizType, orderNo, paymentNo, reason, operator)
		return e
	})
	return balance, err
}

// GetCustomerPhone 取会员绑定的手机号(未绑定/查询失败返回空串)。
// 只用于顾客端「我的」页展示脱敏号,完整号码不外发由调用方负责脱敏。
func GetCustomerPhone(customerID int) string {
	var phone string
	_ = store.DB.QueryRow(`SELECT external_id FROM tb_customer_identity WHERE customer_id=? AND provider=? LIMIT 1`,
		customerID, po.CustomerProviderPhone).Scan(&phone)
	return phone
}

// WalletTxnRow 储值流水的对外视图(顾客端与后台共用;金额以「元」下发,口径同订单)。
type WalletTxnRow struct {
	TxnID        int64   `json:"txnId"`
	BizType      string  `json:"bizType"`
	Amount       float64 `json:"amount"`
	BalanceAfter float64 `json:"balanceAfter"`
	OrderNo      string  `json:"orderNo"`
	Reason       string  `json:"reason"`
	Operator     string  `json:"operator"`
	CreateTime   string  `json:"createTime"`
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
	rows, err := store.DB.Query(`SELECT txn_id, biz_type, amount, balance_after, COALESCE(order_no,''), COALESCE(reason,''), COALESCE(operator,''), COALESCE(create_time,'')
		FROM tb_wallet_txn WHERE customer_id=? ORDER BY txn_id DESC LIMIT ? OFFSET ?`,
		customerID, pageSize, (pageNum-1)*pageSize)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]WalletTxnRow, 0, pageSize)
	for rows.Next() {
		var r WalletTxnRow
		var amount, balanceAfter int64
		if err := rows.Scan(&r.TxnID, &r.BizType, &amount, &balanceAfter, &r.OrderNo, &r.Reason, &r.Operator, TS(&r.CreateTime)); err != nil {
			return 0, nil, err
		}
		// 库里存「分」,出参一律「元」(与订单金额同一口径)。
		r.Amount = po.ToYuan(amount)
		r.BalanceAfter = po.ToYuan(balanceAfter)
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
func InsertRecharge(customerID int, amountCents int64) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		no := newRechargeNo()
		_, err := store.DB.Exec(`INSERT INTO tb_recharge(recharge_no, customer_id, amount, status, create_time)
			VALUES(?,?,?,0,?)`, no, customerID, amountCents, store.Now())
		if err == nil {
			return no, nil
		}
		if attempt == 1 {
			return "", err
		}
	}
	return "", errors.New("生成充值单失败")
}

// GetRecharge 按单号取充值单。不存在返回 (nil, nil)。
func GetRecharge(rechargeNo string) (*po.Recharge, error) {
	var r po.Recharge
	err := store.DB.QueryRow(`SELECT recharge_id, recharge_no, customer_id, amount, status, COALESCE(channel,''),
		COALESCE(channel_trade_no,''), COALESCE(pay_time,''), COALESCE(create_time,'')
		FROM tb_recharge WHERE recharge_no=?`, rechargeNo).
		Scan(&r.RechargeID, &r.RechargeNo, &r.CustomerID, &r.Amount, &r.Status, &r.Channel, &r.ChannelTradeNo, TS(&r.PayTime), TS(&r.CreateTime))
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
	err := tx.QueryRow(`SELECT recharge_id, recharge_no, customer_id, amount, status FROM tb_recharge WHERE recharge_no=?`,
		rechargeNo).Scan(&r.RechargeID, &r.RechargeNo, &r.CustomerID, &r.Amount, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
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
