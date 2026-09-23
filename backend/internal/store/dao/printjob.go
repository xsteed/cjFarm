package dao

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ==================== 打印机 ====================

// PrinterCols 打印机表完整列(与 ScanPrinter 顺序一一对应)。
const PrinterCols = `printer_id, printer_name, printer_type, provider, ip, port,
	feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time`

// ScanPrinter 扫描一行打印机记录。
func ScanPrinter(rows interface{ Scan(...interface{}) error }) (po.Printer, error) {
	var p po.Printer
	err := rows.Scan(&p.PrinterID, &p.PrinterName, &p.PrinterType, &p.Provider, &p.IP, &p.Port,
		&p.FeieSN, &p.PaperWidth, &p.Copies, &p.CategoryIDs, &p.Status, &p.DelFlag,
		&p.CreateTime, &p.UpdateTime)
	if err != nil {
		return p, err
	}
	return p, nil
}

// ListPrinters 查询未删除的打印机列表。
func ListPrinters() ([]po.Printer, error) {
	list := []po.Printer{}
	rows, err := store.DB.Query(`SELECT ` + PrinterCols + ` FROM tb_printer WHERE del_flag='0' ORDER BY printer_id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		if p, err := ScanPrinter(rows); err == nil {
			list = append(list, p)
		}
	}
	return list, nil
}

// ListEnabledPrinters 查询启用的打印机;printerType 传 0 表示全部,
// 否则按类型过滤(1=厨房单 2=食客小票)。
func ListEnabledPrinters(printerType int) ([]po.Printer, error) {
	query := `SELECT ` + PrinterCols + ` FROM tb_printer WHERE del_flag='0' AND status=1`
	args := []interface{}{}
	if printerType != 0 {
		query += ` AND printer_type=?`
		args = append(args, printerType)
	}
	list := []po.Printer{}
	rows, err := store.DB.Query(query, args...)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		if p, err := ScanPrinter(rows); err == nil {
			list = append(list, p)
		}
	}
	return list, nil
}

// InsertPrinter 新增打印机并返回自增 ID。
func InsertPrinter(p po.Printer) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,'0',?,?)`,
		p.PrinterName, p.PrinterType, p.Provider, p.IP, p.Port,
		p.FeieSN, p.PaperWidth, p.Copies, p.CategoryIDs, p.Status, store.Now(), store.Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// UpdatePrinter 更新打印机信息。
func UpdatePrinter(p po.Printer) error {
	_, err := store.DB.Exec(`UPDATE tb_printer SET printer_name=?, printer_type=?, provider=?, ip=?, port=?,
		feie_sn=?, paper_width=?, copies=?, category_ids=?, status=?, update_time=? WHERE printer_id=?`,
		p.PrinterName, p.PrinterType, p.Provider, p.IP, p.Port,
		p.FeieSN, p.PaperWidth, p.Copies, p.CategoryIDs, p.Status, store.Now(), p.PrinterID)
	return err
}

// DeletePrinter 软删除打印机。
func DeletePrinter(id int) error {
	_, err := store.DB.Exec(`UPDATE tb_printer SET del_flag='1', update_time=? WHERE printer_id=?`, store.Now(), id)
	return err
}

// FilterAlivePrinterIDs 过滤掉不存在或已删除的打印机 id(保持入参顺序)。
//
// 打印机是软删除(del_flag),代理身份的 printer_ids 里会残留已删打印机的 id,
// 前端把它显示成「#12」这种找不到机器的授权项。编辑授权范围时用本函数把失效
// id 一并清掉。打印机数量级在几十台,逐个查询比拼 IN 占位符更省心。
func FilterAlivePrinterIDs(ids []int) []int {
	alive := make([]int, 0, len(ids))
	for _, id := range ids {
		var n int
		if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_printer WHERE printer_id=? AND del_flag='0'`, id).Scan(&n); err != nil {
			continue
		}
		if n > 0 {
			alive = append(alive, id)
		}
	}
	return alive
}

// CountPrintersByProvider 按接入方式统计未删除打印机数量。
func CountPrintersByProvider(provider string) int {
	var n int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_printer WHERE del_flag='0' AND provider=?`, provider).Scan(&n)
	return n
}

// FirstEnabledPrinterByType 查询指定类型的第一台启用打印机。
func FirstEnabledPrinterByType(printerType int) (int, error) {
	var printerID int
	err := store.DB.QueryRow(`SELECT printer_id FROM tb_printer WHERE del_flag='0' AND status=1 AND printer_type=? ORDER BY printer_id LIMIT 1`, printerType).Scan(&printerID)
	return printerID, err
}

// ==================== 本地打印代理任务队列 ====================
//
// 与 tb_print_log 的分工:
//   - tb_print_job 是「待办」:一张票据要送给哪台打印机、内容是什么,送完就结案;
//   - tb_print_log 是「台账」:给商家看的「这单打了没、成功了没」,入队时先记一条
//     「排队中」,代理回执后再回写成「已送出」或「失败原因」。
//
// 两者用 print_log_id 串起来。拆成两张表而不是一张,是因为它们的生命周期不同:
// 队列行可以清理,而打印日志要长期保留供商户翻账。

// PrintJobCols 任务队列完整列(与 scanPrintJob 顺序一一对应)。
const PrintJobCols = `job_id, printer_id, printer_name, printer_type, ip, port,
	doc_type, order_id, order_no, table_no, copies, print_log_id, delivery_id, payload,
	status, attempts, last_error, claimed_by, claim_time, next_try_time,
	trigger_by, operator, create_time, done_time`

// scanPrintJob 扫描一行任务记录。
func scanPrintJob(rows interface{ Scan(...interface{}) error }) (po.PrintJob, error) {
	var j po.PrintJob
	err := rows.Scan(&j.JobID, &j.PrinterID, &j.PrinterName, &j.PrinterType, &j.IP, &j.Port,
		&j.DocType, &j.OrderID, &j.OrderNo, &j.TableNo, &j.Copies, &j.PrintLogID, &j.DeliveryID, &j.Payload,
		&j.Status, &j.Attempts, &j.LastError, &j.ClaimedBy, &j.ClaimTime, &j.NextTryTime,
		&j.TriggerBy, &j.Operator, &j.CreateTime, &j.DoneTime)
	return j, err
}

// jobColsCoalesced 在查询里把可空列兜成空串。
// 队列行由本包 InsertPrintJob 写入时这几列都是显式空串,但「先升级代码、后跑迁移」
// 或手工插数据时可能为 NULL,直接 Scan 到 string 会报错,故统一在 SQL 侧兜底。
const jobColsCoalesced = `job_id, printer_id, printer_name, printer_type, ip, port,
	doc_type, order_id, order_no, table_no, copies, print_log_id, COALESCE(delivery_id,''), payload,
	status, attempts, last_error, COALESCE(claimed_by,''), COALESCE(claim_time,''),
	COALESCE(next_try_time,''), trigger_by, operator, create_time, COALESCE(done_time,'')`

// InsertPrintJob 入队一条打印任务,返回自增主键。
//
// delivery_id 在此生成(入队时一次性、永不变更):它是「幂等投递」的凭据 ——
// 代理打印成功后把它持久化到本地,若回执丢失导致任务被重新下发,代理凭它去重,
// 从根上消除「回执丢失 → 同一张票打两遍」。
func InsertPrintJob(j po.PrintJob) (int, error) {
	if j.DeliveryID == "" {
		j.DeliveryID = newDeliveryID()
	}
	res, err := store.DB.Exec(`INSERT INTO tb_print_job(printer_id, printer_name, printer_type, ip, port,
		doc_type, order_id, order_no, table_no, copies, print_log_id, delivery_id, payload,
		status, attempts, last_error, claimed_by, claim_time, next_try_time,
		trigger_by, operator, create_time, done_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.PrinterID, j.PrinterName, j.PrinterType, j.IP, j.Port,
		j.DocType, j.OrderID, j.OrderNo, j.TableNo, j.Copies, j.PrintLogID, j.DeliveryID, j.Payload,
		po.PrintJobPending, 0, "", "", "", "",
		j.TriggerBy, j.Operator, j.CreateTime, "")
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// newDeliveryID 生成 128 位随机数的 hex 串(32 字符)作为幂等投递号。
// 用 crypto/rand 而非时间戳/自增:它只要求全局唯一、不要求可排序,
// 随机保证两台后端实例并发入队时也不会撞号。
func newDeliveryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败几乎不可能;兜底用纳秒时间戳避免生成空号导致去重失效。
		return hex.EncodeToString([]byte(store.Now()))
	}
	return hex.EncodeToString(b)
}

// LoadPrintJob 按 ID 读取一条任务(回执时用它取 print_log_id 与打印机地址)。
func LoadPrintJob(jobID int) (po.PrintJob, error) {
	return scanPrintJob(store.DB.QueryRow(`SELECT `+jobColsCoalesced+` FROM tb_print_job WHERE job_id=?`, jobID))
}

// loadPrintJobStatus 只读任务当前状态。
//
// 条件更新未命中(受影响行数为 0)时,需要用它区分「已经是目标状态(幂等)」与
// 「状态非法,拒绝迁移」;它只查一列,避免把整行 payload 拉回来。
func loadPrintJobStatus(jobID int) (int, error) {
	var status int
	err := store.DB.QueryRow(`SELECT status FROM tb_print_job WHERE job_id=?`, jobID).Scan(&status)
	return status, err
}

// printJobExpiredCond 按单据类型判断任务是否已超过代理取单 TTL。
//
// cutoff 为空串表示该类型关闭过期判断。这里集中拼 SQL,保证「取单跳过」与
// 「后台作废」使用同一套判据,避免边界不一致导致过期单仍被下发。
const printJobExpiredCond = `(doc_type=? AND ?<>'' AND create_time<=?) OR
	(doc_type=? AND ?<>'' AND create_time<=?) OR
	(doc_type<>? AND doc_type<>? AND ?<>'' AND create_time<=?)`

func printJobExpiredArgs(kitchenCutoff, guestCutoff, otherCutoff string) []interface{} {
	return []interface{}{
		po.PrintDocKitchen, kitchenCutoff, kitchenCutoff,
		po.PrintDocGuest, guestCutoff, guestCutoff,
		po.PrintDocKitchen, po.PrintDocGuest, otherCutoff, otherCutoff,
	}
}

// ClaimPrintJobs 取单:挑出可执行的任务并原子地标记为「已被本代理取走」。
//
// 为什么要「选中再条件更新」而不是一把 UPDATE:
//   - 两种数据库都支持的写法里,「UPDATE ... RETURNING」只有 SQLite 支持,MySQL 不支持;
//   - 条件更新(UPDATE ... WHERE job_id=? AND status=?)靠受影响行数判断是否抢到,
//     两台代理同时轮询也不会重复打印同一条任务。
//
// 判据两类:
//   - 待取单(status=0)且已到重试时间;
//   - 已取单但租约超时(status=1 且 claim_time 早于 now-lease)
//     —— 代理进程被杀/断电时任务不会永远卡在「打印中」。
//
// 任务一旦被取走,attempts 即 +1(因此「取走 3 次都没成功」会被判为放弃)。
func ClaimPrintJobs(agentID string, limit, leaseSeconds int, kitchenCutoff, guestCutoff, otherCutoff string, printerIDs []int) ([]po.PrintJob, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	now := store.Now()
	leaseCutoff := shiftSeconds(now, -leaseSeconds)
	expiredArgs := printJobExpiredArgs(kitchenCutoff, guestCutoff, otherCutoff)

	// 授权域条件:per-agent 令牌限定打印机范围时,必须在 SQL 里过滤而不是取单后丢弃——
	// 取单后丢弃会让任务白白被 claim(耗 60s 租约、attempts+1),多代理互补分区时
	// 错配 claim 三次就能把任务打到 dead。
	scopeCond := ""
	var scopeArgs []interface{}
	if len(printerIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(printerIDs)), ",")
		scopeCond = " AND printer_id IN (" + ph + ")"
		for _, id := range printerIDs {
			scopeArgs = append(scopeArgs, id)
		}
	}

	candidates := []po.PrintJob{}
	pick := func(query string, args ...interface{}) error {
		rows, err := store.DB.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if j, err := scanPrintJob(rows); err == nil {
				candidates = append(candidates, j)
			}
		}
		return nil
	}

	// 待取单:从未取过,或上次失败后已过了退避时间。过期任务不能下发,
	// 否则代理离线多天后恢复会把陈旧厨房单重新打出来。
	pendingArgs := append([]interface{}{po.PrintJobPending, now}, expiredArgs...)
	pendingArgs = append(pendingArgs, scopeArgs...)
	pendingArgs = append(pendingArgs, limit)
	if err := pick(`SELECT `+jobColsCoalesced+` FROM tb_print_job
		WHERE status=? AND (next_try_time='' OR next_try_time<=?)
		AND NOT (`+printJobExpiredCond+`)`+scopeCond+`
		ORDER BY job_id LIMIT ?`, pendingArgs...); err != nil {
		return nil, err
	}
	// 租约超时:代理取走后失联,重新放回可执行队列。若已过 TTL,留给
	// 后台清理置 dead 并回写日志,不能再次交给代理打印。
	if len(candidates) < limit {
		claimedArgs := append([]interface{}{po.PrintJobClaimed, leaseCutoff}, expiredArgs...)
		claimedArgs = append(claimedArgs, scopeArgs...)
		claimedArgs = append(claimedArgs, limit-len(candidates))
		if err := pick(`SELECT `+jobColsCoalesced+` FROM tb_print_job
			WHERE status=? AND claim_time<>'' AND claim_time<=?
			AND NOT (`+printJobExpiredCond+`)`+scopeCond+`
			ORDER BY job_id LIMIT ?`, claimedArgs...); err != nil {
			return nil, err
		}
	}

	claimed := make([]po.PrintJob, 0, len(candidates))
	for _, j := range candidates {
		res, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, claimed_by=?, claim_time=?,
			attempts=attempts+1 WHERE job_id=? AND status=?`,
			po.PrintJobClaimed, agentID, now, j.JobID, j.Status)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // 被另一台代理抢先取走了
		}
		j.Status = po.PrintJobClaimed
		j.ClaimedBy = agentID
		j.ClaimTime = now
		j.Attempts++
		claimed = append(claimed, j)
	}
	return claimed, nil
}

// MarkPrintJobDone 任务打印成功,结案。
//
// 只允许 claimed(打印中)→ done:条件更新 + RowsAffected 判定,与 ClaimPrintJobs
// 的抢占范式一致,避免迟到的成功回执把已放弃(dead)或尚未取走(pending)的任务结案。
// 未命中时读当前状态:已是 done 说明是重复回执,按幂等成功处理;其余状态返回错误。
func MarkPrintJobDone(jobID int) error {
	res, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, last_error='', done_time=?
		WHERE job_id=? AND status=?`, po.PrintJobDone, store.Now(), jobID, po.PrintJobClaimed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	status, err := loadPrintJobStatus(jobID)
	if err != nil {
		return err
	}
	if status == po.PrintJobDone {
		return nil
	}
	return fmt.Errorf("任务状态为 %d,不允许结案", status)
}

// RetryPrintJob 打印失败:重试次数未用尽则退回待取单(带退避),否则置为放弃。
// 返回是否仍会重试(供调用方决定日志文案:「已安排重试」还是「已放弃」)。
//
// 与 MarkPrintJobDone 一样只允许从 claimed(打印中)迁移:失败回执必须来自
// 「正在打印的任务」;done/dead 是终态禁止复活,pending 表示任务还没被取走、
// 也不该有失败回执。attempts 计数逻辑保持不变。
func RetryPrintJob(jobID int, errMsg string, backoffSeconds int) (bool, error) {
	errMsg = truncateRunes(errMsg, 480)
	now := store.Now()
	nextTry := shiftSeconds(now, backoffSeconds)

	// 单条 UPDATE 里按 attempts 分支,避免并发回执在 SELECT attempts 与 UPDATE 之间交错。
	res, err := store.DB.Exec(`UPDATE tb_print_job SET
		last_error=?,
		status=CASE WHEN attempts>=? THEN ? ELSE ? END,
		done_time=CASE WHEN attempts>=? THEN ? ELSE done_time END,
		next_try_time=CASE WHEN attempts>=? THEN next_try_time ELSE ? END,
		claimed_by=CASE WHEN attempts>=? THEN claimed_by ELSE '' END,
		claim_time=CASE WHEN attempts>=? THEN claim_time ELSE '' END
		WHERE job_id=? AND status=?`,
		errMsg,
		po.PrintJobMaxAttempts, po.PrintJobDead, po.PrintJobPending,
		po.PrintJobMaxAttempts, now,
		po.PrintJobMaxAttempts, nextTry,
		po.PrintJobMaxAttempts,
		po.PrintJobMaxAttempts,
		jobID, po.PrintJobClaimed)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		status, err := loadPrintJobStatus(jobID)
		if err != nil {
			return false, err
		}
		return false, fmt.Errorf("任务状态为 %d,不允许重试", status)
	}

	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_print_job WHERE job_id=?`, jobID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return false, err
		}
		return false, err
	}
	return status == po.PrintJobPending, nil
}

// ReleasePrintJob 把「代理未真正尝试」的任务退回队列,并归还本次取单消耗的尝试次数。
//
// 与 RetryPrintJob 的区别:attempts 是在 claim 取单时 +1 的,而代理可能因熔断冷却
// 根本没有拨过打印机(skipped)。此时若按失败处理,重试额度会被「空转」吃掉 ——
// 打印机只是短暂不可达就会被判成放弃、永久丢单。这里把额度还回去,使 attempts
// 严格等于「真实尝试次数」。
//
// 只允许 claimed → pending:done/dead 是终态,pending 说明任务根本没被取走。
// retryAfterSeconds 为 0 时按最小退避处理,避免任务立刻回到队列形成空转循环。
func ReleasePrintJob(jobID int, errMsg string, retryAfterSeconds int) error {
	errMsg = truncateRunes(errMsg, 480)
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	if retryAfterSeconds > 300 {
		retryAfterSeconds = 300
	}
	now := store.Now()
	res, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, last_error=?,
		attempts=CASE WHEN attempts>0 THEN attempts-1 ELSE 0 END,
		next_try_time=?, claimed_by='', claim_time=''
		WHERE job_id=? AND status=?`,
		po.PrintJobPending, errMsg, shiftSeconds(now, retryAfterSeconds), jobID, po.PrintJobClaimed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		status, err := loadPrintJobStatus(jobID)
		if err != nil {
			return err
		}
		return fmt.Errorf("任务状态为 %d,不允许退回队列", status)
	}
	return nil
}

// CancelPrintJobsByPrinter 取消某台打印机尚未送出的任务(待取单 + 打印中)。
//
// 返回取消条数与受影响的打印日志 ID —— 调用方据此把那些日志改为失败,
// 否则「清空队列」后日志会永远停在「排队中」,商家看不出这单其实没打。
func CancelPrintJobsByPrinter(printerID int) (int, []int, error) {
	rows, err := store.DB.Query(`SELECT print_log_id FROM tb_print_job
		WHERE printer_id=? AND status IN (?,?)`, printerID, po.PrintJobPending, po.PrintJobClaimed)
	if err != nil {
		return 0, nil, err
	}
	logIDs := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil && id > 0 {
			logIDs = append(logIDs, id)
		}
	}
	rows.Close()

	res, err := store.DB.Exec(`DELETE FROM tb_print_job WHERE printer_id=? AND status IN (?,?)`,
		printerID, po.PrintJobPending, po.PrintJobClaimed)
	if err != nil {
		return 0, nil, err
	}
	n, _ := res.RowsAffected()
	return int(n), logIDs, nil
}

// ExpireOverduePrintJobs 作废尚未送出的过期代理任务。
//
// 过期单据不能再交给代理「补打」:尤其是厨房单,离线数天后突然吐出会让后厨
// 照旧单做菜。这里把任务置 dead 并返回对应日志 ID,调用方负责把台账写成可人工补打。
//
// pending 与 claimed 的作废判据必须分开:
//   - pending 还没人取,按「创建时间超过 TTL」作废即可;
//   - claimed 正在被代理打印,不能因为创建时间过期就打断它,只有「租约已超时」
//     (代理取走后失联)才作废。租约超时但 TTL 未超时的任务由 ClaimPrintJobs 重新
//     下发,不能在这里置 dead;租约未超时则说明代理还在正常打印,更不能误伤。
func ExpireOverduePrintJobs(leaseSeconds int, kitchenCutoff, guestCutoff, otherCutoff string) (int, []int) {
	if kitchenCutoff == "" && guestCutoff == "" && otherCutoff == "" {
		return 0, nil
	}
	expiredArgs := printJobExpiredArgs(kitchenCutoff, guestCutoff, otherCutoff)
	now := store.Now()
	leaseCutoff := shiftSeconds(now, -leaseSeconds)

	logIDs := []int{}
	collect := func(query string, args ...interface{}) error {
		rows, err := store.DB.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err == nil && id > 0 {
				logIDs = append(logIDs, id)
			}
		}
		return rows.Err()
	}

	// 待取单:仅按 TTL 过期作废。
	pendingSelectArgs := append([]interface{}{po.PrintJobPending}, expiredArgs...)
	if err := collect(`SELECT print_log_id FROM tb_print_job
		WHERE status=? AND (`+printJobExpiredCond+`)`, pendingSelectArgs...); err != nil {
		return 0, nil
	}
	// 打印中:必须同时满足「TTL 过期」与「租约超时」,避免误伤正在打印的任务。
	claimedSelectArgs := append([]interface{}{po.PrintJobClaimed, leaseCutoff}, expiredArgs...)
	if err := collect(`SELECT print_log_id FROM tb_print_job
		WHERE status=? AND claim_time<>'' AND claim_time<=? AND (`+printJobExpiredCond+`)`, claimedSelectArgs...); err != nil {
		return 0, nil
	}

	deadText := "任务过期自动作废(TTL)"
	total := 0
	pendingUpdateArgs := append([]interface{}{po.PrintJobDead, deadText, now, po.PrintJobPending}, expiredArgs...)
	res, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, last_error=?, done_time=?
		WHERE status=? AND (`+printJobExpiredCond+`)`, pendingUpdateArgs...)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			total += int(n)
		}
	}
	claimedUpdateArgs := append([]interface{}{po.PrintJobDead, deadText, now, po.PrintJobClaimed, leaseCutoff}, expiredArgs...)
	res, err = store.DB.Exec(`UPDATE tb_print_job SET status=?, last_error=?, done_time=?
		WHERE status=? AND claim_time<>'' AND claim_time<=? AND (`+printJobExpiredCond+`)`, claimedUpdateArgs...)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			total += int(n)
		}
	}
	return total, logIDs
}

// PrintJobStats 队列概况(供打印机管理页展示「待送 / 卡住」)。
func PrintJobStats() (pending, dead int) {
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE status IN (?,?)`,
		po.PrintJobPending, po.PrintJobClaimed).Scan(&pending)
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE status=?`, po.PrintJobDead).Scan(&dead)
	return pending, dead
}

// PendingJobCountByPrinter 某台打印机的积压任务数。
func PendingJobCountByPrinter(printerID int) int {
	var n int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE printer_id=? AND status IN (?,?)`,
		printerID, po.PrintJobPending, po.PrintJobClaimed).Scan(&n)
	return n
}

// CleanDonePrintJobs 清理已结案的老任务(队列是「待办」,没有长期保留价值)。
// keepDays<=0 时不清理。返回清理行数。
func CleanDonePrintJobs(keepDays int) int {
	if keepDays <= 0 {
		return 0
	}
	cutoff := shiftSeconds(store.Now(), -keepDays*86400)
	res, err := store.DB.Exec(`DELETE FROM tb_print_job WHERE status IN (?,?) AND create_time<>'' AND create_time<?`,
		po.PrintJobDone, po.PrintJobDead, cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// UpdatePrintLogResult 回写打印日志的结果(代理通道专用:入队时先记「排队中」,
// 代理回执后再改成成功/失败)。仅当该日志行仍是「排队中」时才改写,
// 避免人工补打已经写出的结果被迟到的回执覆盖。
func UpdatePrintLogResult(printID int, status int, detail string, costMs int) error {
	if printID <= 0 {
		return nil
	}
	if costMs >= 0 {
		_, err := store.DB.Exec(`UPDATE tb_print_log SET status=?, detail=?, cost_ms=? WHERE print_id=? AND status=?`,
			status, truncateRunes(detail, 480), costMs, printID, po.PrintStatusQueued)
		return err
	}
	_, err := store.DB.Exec(`UPDATE tb_print_log SET status=?, detail=? WHERE print_id=? AND status=?`,
		status, truncateRunes(detail, 480), printID, po.PrintStatusQueued)
	return err
}

// OldestPendingSec 返回最老未结案代理任务已等待的秒数;无积压或时间异常返回 0。
func OldestPendingSec() int {
	var oldest sql.NullString
	if err := store.DB.QueryRow(`SELECT MIN(create_time) FROM tb_print_job WHERE status IN (?,?)`,
		po.PrintJobPending, po.PrintJobClaimed).Scan(&oldest); err != nil || !oldest.Valid || strings.TrimSpace(oldest.String) == "" {
		return 0
	}
	created, err := time.ParseInLocation(conf.TimeLayout, oldest.String, time.Local)
	if err != nil {
		return 0
	}
	now, err := time.ParseInLocation(conf.TimeLayout, store.Now(), time.Local)
	if err != nil {
		return 0
	}
	sec := int(now.Sub(created).Seconds())
	if sec < 0 {
		return 0
	}
	return sec
}

// shiftSeconds 返回 base 时间字符串偏移 deltaSeconds 秒后的同格式字符串。
// 时间统一以 'YYYY-MM-DD HH:MM:SS' 字符串存储,定宽格式下字典序即时间序,
// 因此可以直接用字符串比较做「到期」「超时」判断。
func shiftSeconds(base string, deltaSeconds int) string {
	t, err := time.Parse(conf.TimeLayout, base)
	if err != nil {
		return base
	}
	return t.Add(time.Duration(deltaSeconds) * time.Second).Format(conf.TimeLayout)
}

// 注:按字符截断的 truncateRunes 复用 audit.go 中的同名实现(那里已有同样的需求)。
