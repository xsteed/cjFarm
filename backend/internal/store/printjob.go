package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"dining-system/internal/model"
)

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
func scanPrintJob(rows interface{ Scan(...interface{}) error }) (model.PrintJob, error) {
	var j model.PrintJob
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
func InsertPrintJob(j model.PrintJob) (int, error) {
	if j.DeliveryID == "" {
		j.DeliveryID = newDeliveryID()
	}
	res, err := DB.Exec(`INSERT INTO tb_print_job(printer_id, printer_name, printer_type, ip, port,
		doc_type, order_id, order_no, table_no, copies, print_log_id, delivery_id, payload,
		status, attempts, last_error, claimed_by, claim_time, next_try_time,
		trigger_by, operator, create_time, done_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.PrinterID, j.PrinterName, j.PrinterType, j.IP, j.Port,
		j.DocType, j.OrderID, j.OrderNo, j.TableNo, j.Copies, j.PrintLogID, j.DeliveryID, j.Payload,
		model.PrintJobPending, 0, "", "", "", "",
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
		return hex.EncodeToString([]byte(Now()))
	}
	return hex.EncodeToString(b)
}

// LoadPrintJob 按 ID 读取一条任务(回执时用它取 print_log_id 与打印机地址)。
func LoadPrintJob(jobID int) (model.PrintJob, error) {
	return scanPrintJob(DB.QueryRow(`SELECT `+jobColsCoalesced+` FROM tb_print_job WHERE job_id=?`, jobID))
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
func ClaimPrintJobs(agentID string, limit, leaseSeconds int) ([]model.PrintJob, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	now := Now()
	leaseCutoff := shiftSeconds(now, -leaseSeconds)

	candidates := []model.PrintJob{}
	pick := func(query string, args ...interface{}) error {
		rows, err := DB.Query(query, args...)
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

	// 待取单:从未取过,或上次失败后已过了退避时间。
	if err := pick(`SELECT `+jobColsCoalesced+` FROM tb_print_job
		WHERE status=? AND (next_try_time='' OR next_try_time<=?)
		ORDER BY job_id LIMIT ?`, model.PrintJobPending, now, limit); err != nil {
		return nil, err
	}
	// 租约超时:代理取走后失联,重新放回可执行队列。
	if len(candidates) < limit {
		if err := pick(`SELECT `+jobColsCoalesced+` FROM tb_print_job
			WHERE status=? AND claim_time<>'' AND claim_time<=?
			ORDER BY job_id LIMIT ?`, model.PrintJobClaimed, leaseCutoff, limit-len(candidates)); err != nil {
			return nil, err
		}
	}

	claimed := make([]model.PrintJob, 0, len(candidates))
	for _, j := range candidates {
		res, err := DB.Exec(`UPDATE tb_print_job SET status=?, claimed_by=?, claim_time=?,
			attempts=attempts+1 WHERE job_id=? AND status=?`,
			model.PrintJobClaimed, agentID, now, j.JobID, j.Status)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // 被另一台代理抢先取走了
		}
		j.Status = model.PrintJobClaimed
		j.ClaimedBy = agentID
		j.ClaimTime = now
		j.Attempts++
		claimed = append(claimed, j)
	}
	return claimed, nil
}

// MarkPrintJobDone 任务打印成功,结案。
func MarkPrintJobDone(jobID int) error {
	_, err := DB.Exec(`UPDATE tb_print_job SET status=?, last_error='', done_time=?
		WHERE job_id=?`, model.PrintJobDone, Now(), jobID)
	return err
}

// RetryPrintJob 打印失败:重试次数未用尽则退回待取单(带退避),否则置为放弃。
// 返回是否仍会重试(供调用方决定日志文案:「已安排重试」还是「已放弃」)。
func RetryPrintJob(jobID int, errMsg string, backoffSeconds int) (bool, error) {
	var attempts int
	if err := DB.QueryRow(`SELECT attempts FROM tb_print_job WHERE job_id=?`, jobID).Scan(&attempts); err != nil {
		return false, err
	}
	errMsg = truncateRunes(errMsg, 480)
	if attempts >= model.PrintJobMaxAttempts {
		_, err := DB.Exec(`UPDATE tb_print_job SET status=?, last_error=?, done_time=?
			WHERE job_id=?`, model.PrintJobDead, errMsg, Now(), jobID)
		return false, err
	}
	_, err := DB.Exec(`UPDATE tb_print_job SET status=?, last_error=?, next_try_time=?,
		claimed_by='', claim_time='' WHERE job_id=?`,
		model.PrintJobPending, errMsg, shiftSeconds(Now(), backoffSeconds), jobID)
	return true, err
}

// CancelPrintJobsByPrinter 取消某台打印机尚未送出的任务(待取单 + 打印中)。
//
// 返回取消条数与受影响的打印日志 ID —— 调用方据此把那些日志改为失败,
// 否则「清空队列」后日志会永远停在「排队中」,商家看不出这单其实没打。
func CancelPrintJobsByPrinter(printerID int) (int, []int, error) {
	rows, err := DB.Query(`SELECT print_log_id FROM tb_print_job
		WHERE printer_id=? AND status IN (?,?)`, printerID, model.PrintJobPending, model.PrintJobClaimed)
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

	res, err := DB.Exec(`DELETE FROM tb_print_job WHERE printer_id=? AND status IN (?,?)`,
		printerID, model.PrintJobPending, model.PrintJobClaimed)
	if err != nil {
		return 0, nil, err
	}
	n, _ := res.RowsAffected()
	return int(n), logIDs, nil
}

// PrintJobStats 队列概况(供打印机管理页展示「待送 / 卡住」)。
func PrintJobStats() (pending, dead int) {
	DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE status IN (?,?)`,
		model.PrintJobPending, model.PrintJobClaimed).Scan(&pending)
	DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE status=?`, model.PrintJobDead).Scan(&dead)
	return pending, dead
}

// PendingJobCountByPrinter 某台打印机的积压任务数。
func PendingJobCountByPrinter(printerID int) int {
	var n int
	DB.QueryRow(`SELECT COUNT(*) FROM tb_print_job WHERE printer_id=? AND status IN (?,?)`,
		printerID, model.PrintJobPending, model.PrintJobClaimed).Scan(&n)
	return n
}

// CleanDonePrintJobs 清理已结案的老任务(队列是「待办」,没有长期保留价值)。
// keepDays<=0 时不清理。返回清理行数。
func CleanDonePrintJobs(keepDays int) int {
	if keepDays <= 0 {
		return 0
	}
	cutoff := shiftSeconds(Now(), -keepDays*86400)
	res, err := DB.Exec(`DELETE FROM tb_print_job WHERE status IN (?,?) AND create_time<>'' AND create_time<?`,
		model.PrintJobDone, model.PrintJobDead, cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// UpdatePrintLogResult 回写打印日志的结果(代理通道专用:入队时先记「排队中」,
// 代理回执后再改成成功/失败)。仅当该日志行仍是「排队中」时才改写,
// 避免人工补打已经写出的结果被迟到的回执覆盖。
func UpdatePrintLogResult(printID int, status int, detail string) error {
	if printID <= 0 {
		return nil
	}
	_, err := DB.Exec(`UPDATE tb_print_log SET status=?, detail=? WHERE print_id=? AND status=?`,
		status, truncateRunes(detail, 480), printID, model.PrintStatusQueued)
	return err
}

// shiftSeconds 返回 base 时间字符串偏移 deltaSeconds 秒后的同格式字符串。
// 时间统一以 'YYYY-MM-DD HH:MM:SS' 字符串存储,定宽格式下字典序即时间序,
// 因此可以直接用字符串比较做「到期」「超时」判断。
func shiftSeconds(base string, deltaSeconds int) string {
	t, err := time.Parse("2006-01-02 15:04:05", base)
	if err != nil {
		return base
	}
	return t.Add(time.Duration(deltaSeconds) * time.Second).Format("2006-01-02 15:04:05")
}

// 注:按字符截断的 truncateRunes 复用 audit.go 中的同名实现(那里已有同样的需求)。
