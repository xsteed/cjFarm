// 访问统计(站点 PV/UV/新访客)的数据访问。
//
// 三张表的分工(见 docs/design-docs/visit-analytics/spec.md 第 3 节):
//   tb_visit_log   明细,「待办」性质 —— 日聚合落好后按保留天数清理;
//   tb_visitor     访客注册表,「台账」—— 永久保留,让「新访客」精确;
//   tb_visit_daily 日聚合台账 —— 永久保留,报表主读。
//
// 写入侧只有「批量插明细 + 访客 upsert」一个入口(WriteVisitBatch),由 service 的
// 批量写 worker 单线程调用:SQLite 单写者模式下不放大锁竞争,MySQL 亦合并往返。
package dao

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
)

// ============================================================================
// 开关(与审计同风格:env 读取,默认开启)
// ============================================================================

// VisitEnabled 是否记录访问统计(VISIT_STAT_ENABLED,默认开启)。
func VisitEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(infrastructure.Getenv(conf.EnvVisitStatEnabled, conf.DefaultVisitStatEnabled)))
	return v != "0" && v != "false" && v != "off"
}

// VisitRetentionDays 访问明细保留天数(VISIT_RETENTION_DAYS,默认 90,0 表示不清理)。
func VisitRetentionDays() int {
	n, err := strconv.Atoi(strings.TrimSpace(infrastructure.Getenv(conf.EnvVisitRetentionDays, conf.DefaultVisitRetentionDays)))
	if err != nil || n < 0 {
		return 90
	}
	return n
}

// VisitRatePerMin 单 IP 每分钟打点上限(VISIT_RATE_PER_MIN,默认 60)。
func VisitRatePerMin() int {
	n, err := strconv.Atoi(strings.TrimSpace(infrastructure.Getenv(conf.EnvVisitRatePerMin, conf.DefaultVisitRatePerMin)))
	if err != nil || n <= 0 {
		return 60
	}
	return n
}

// ============================================================================
// 写入(批量)
// ============================================================================

// visitLogInsertChunk 单条多行 INSERT 的最大行数。
// modernc.org/sqlite 默认 SQLITE_MAX_VARIABLE_NUMBER=32766,200 行 × 15 列 = 3000
// 参数,留一个数量级的余量;MySQL 的占位符上限(65535)同样宽裕。
const visitLogInsertChunk = 200

// WriteVisitBatch 一个事务内写入一批访问明细并维护访客注册表。
//
// 两者必须在同一事务:明细落了、访客没落,「新访客」就会少算;反之则会凭空
// 多出新访客。事务失败整体回滚,由调用方(service worker)决定重试或丢弃。
func WriteVisitBatch(logs []po.VisitLog, visitors []po.Visitor) error {
	// 只在「两者都为空」时短路:旧实现见 logs 为空就整体返回,若调用方只带
	// visitors(纯建档),访客档案会被静默丢弃、「新访客」少算。虽然当前唯一
	// 调用方(worker)不会构造这种入参,但契约上不该有这条隐形前提。
	if len(logs) == 0 && len(visitors) == 0 {
		return nil
	}
	return store.WithTx(func(tx *sql.Tx) error {
		if err := insertVisitLogsTx(tx, logs); err != nil {
			return err
		}
		return upsertVisitorsTx(tx, visitors)
	})
}

// insertVisitLogsTx 分批多行 INSERT 明细。
func insertVisitLogsTx(tx *sql.Tx, logs []po.VisitLog) error {
	for start := 0; start < len(logs); start += visitLogInsertChunk {
		end := start + visitLogInsertChunk
		if end > len(logs) {
			end = len(logs)
		}
		chunk := logs[start:end]
		var b strings.Builder
		b.WriteString(`INSERT INTO tb_visit_log(visit_date, channel, visitor_key, is_member, customer_id, entry,
			ip_hash, ua_hash, ip, city, os, browser, device, model, create_time) VALUES `)
		args := make([]interface{}, 0, len(chunk)*15)
		for i, l := range chunk {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
			args = append(args, l.VisitDate, l.Channel, l.VisitorKey, l.IsMember, l.CustomerID, l.Entry,
				l.IPHash, l.UAHash, l.IP, l.City, l.OS, l.Browser, l.Device, l.Model, l.CreateTime)
		}
		if _, err := tx.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// upsertVisitorsTx 维护访客注册表:同批同键先合并(累加次数、取最大日期),
// 再按「先 UPDATE 后 INSERT」写入 —— 与 NextPickupNoTx 同一模式,两库语义一致,
// 不依赖 MySQL 的 ON DUPLICATE KEY / SQLite 的 ON CONFLICT。
//
// 并发下两个 worker 可能同时 UPDATE 同一访客(影响 0 行)又同时 INSERT:
// visitor_key 是主键,后到者撞唯一约束即视为「已建档」,UPDATE 路径下一次批次
// 会把它补上;统计口径只要求「档案存在且次数单调增»,不要求严格精确。
func upsertVisitorsTx(tx *sql.Tx, visitors []po.Visitor) error {
	if len(visitors) == 0 {
		return nil
	}
	merged := make(map[string]*po.Visitor, len(visitors))
	order := make([]string, 0, len(visitors))
	for i := range visitors {
		v := visitors[i]
		if v.VisitorKey == "" {
			continue
		}
		if m, ok := merged[v.VisitorKey]; ok {
			m.VisitCount += v.VisitCount
			if v.FirstVisitDate < m.FirstVisitDate {
				m.FirstVisitDate = v.FirstVisitDate
			}
			if v.LastVisitDate > m.LastVisitDate {
				m.LastVisitDate = v.LastVisitDate
			}
			continue
		}
		cp := v
		merged[v.VisitorKey] = &cp
		order = append(order, v.VisitorKey)
	}
	for _, key := range order {
		v := merged[key]
		res, err := tx.Exec(`UPDATE tb_visitor SET last_visit_date=?, visit_count=visit_count+? WHERE visitor_key=?`,
			v.LastVisitDate, v.VisitCount, v.VisitorKey)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO tb_visitor(visitor_key, first_visit_date, last_visit_date, visit_count, is_member, customer_id, create_time)
			VALUES(?,?,?,?,?,?,?)`,
			v.VisitorKey, v.FirstVisitDate, v.LastVisitDate, v.VisitCount, v.IsMember, v.CustomerID, v.CreateTime); err != nil {
			// 并发首建档:另一路已插入同样主键。主键冲突可安全忽略(档案已在),
			// 其余错误(连接断等)上抛让整批回滚。
			if isUniqueConflict(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// ============================================================================
// 日聚合(rollup)
// ============================================================================

// RollupDay 从明细绝对重算某一天的全站与分渠道指标,写入日聚合台账。
//
// 幂等:同一 (visit_date, channel) 重复执行结果一致(先 UPDATE 后 INSERT,
// 写的是绝对值而非累加),崩溃后可放心重跑 —— 启动补跑靠的正是这一点。
// channel='all' 行的 UV 是跨渠道去重值;单渠道行的 UV 之和会大于它
// (同一访客可能当天两个渠道都来过),报表展示时已注明。
func RollupDay(date string) error {
	if date == "" {
		return nil
	}
	pv := map[string]int{}
	uv := map[string]int{}
	rows, err := store.DB.Query(`SELECT channel, COUNT(*), COUNT(DISTINCT visitor_key) FROM tb_visit_log
		WHERE visit_date=? GROUP BY channel`, date)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ch string
		var p, u int
		if err := rows.Scan(&ch, &p, &u); err != nil {
			rows.Close()
			return err
		}
		pv[ch], uv[ch] = p, u
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	var allUV, memberUV, newUV int
	if err := store.DB.QueryRow(`SELECT COUNT(DISTINCT visitor_key) FROM tb_visit_log WHERE visit_date=?`, date).Scan(&allUV); err != nil {
		return err
	}
	if err := store.DB.QueryRow(`SELECT COUNT(DISTINCT visitor_key) FROM tb_visit_log WHERE visit_date=? AND is_member=1`, date).Scan(&memberUV); err != nil {
		return err
	}
	// 新访客以访客注册表为准(精确):首访日期 = 当日的档案数。
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visitor WHERE first_visit_date=?`, date).Scan(&newUV); err != nil {
		return err
	}

	now := store.Now()
	return store.WithTx(func(tx *sql.Tx) error {
		write := func(channel string, p, u, nu, mu int) error {
			res, err := tx.Exec(`UPDATE tb_visit_daily SET pv=?, uv=?, new_uv=?, member_uv=? WHERE visit_date=? AND channel=?`,
				p, u, nu, mu, date, channel)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				return nil
			}
			if _, err := tx.Exec(`INSERT INTO tb_visit_daily(visit_date, channel, pv, uv, new_uv, member_uv, create_time)
				VALUES(?,?,?,?,?,?,?)`, date, channel, p, u, nu, mu, now); err != nil {
				// 并发 rollup(多实例 / 多标签页同时查报表)可能两路都走了
				// 「UPDATE 0 行 → INSERT」:后到者撞 (visit_date, channel) 唯一索引。
				// 另一路已经写了同一份绝对数值,忽略即可。
				if isUniqueConflict(err) {
					return nil
				}
				return err
			}
			return nil
		}
		if err := write(po.VisitChannelAll, sumInts(pv), allUV, newUV, memberUV); err != nil {
			return err
		}
		for ch, p := range pv {
			if err := write(ch, p, uv[ch], 0, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// sumInts 汇总各渠道 PV(全站 PV = 各渠道之和,PV 无跨渠道去重问题)。
func sumInts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// isUniqueConflict 判断错误是否为唯一索引冲突。
//
// 两库文案不同:SQLite 是 "UNIQUE constraint failed",MySQL 是 "Duplicate entry"。
// 这类冲突在「先 UPDATE 后 INSERT」的幂等写入(访客建档、日聚合)里意味着
// 「另一路已经写好了同一份数据」,可以安全忽略;其它错误必须上抛。
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

// VisitLogDates 返回明细中出现的全部日期(升序),供启动补跑判断缺口。
func VisitLogDates() ([]string, error) {
	rows, err := store.DB.Query(`SELECT DISTINCT visit_date FROM tb_visit_log ORDER BY visit_date`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MaxRolledUpDate 返回日聚合台账中已有的最大日期(无数据返回空串)。
func MaxRolledUpDate() (string, error) {
	var d sql.NullString
	if err := store.DB.QueryRow(`SELECT MAX(visit_date) FROM tb_visit_daily`).Scan(&d); err != nil {
		return "", err
	}
	return d.String, nil
}

// CleanVisitLogsBefore 删除 cutoff 日期之前的访问明细(保留期到期)。
// 只删明细;访客注册表与日聚合台账是长期账本,不受影响。
func CleanVisitLogsBefore(cutoff string) (int64, error) {
	res, err := store.DB.Exec(`DELETE FROM tb_visit_log WHERE visit_date < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ============================================================================
// 查询
// ============================================================================

// VisitDailyRange 查询区间内的日聚合台账(含首尾日期)。
func VisitDailyRange(start, end string) ([]po.VisitDaily, error) {
	rows, err := store.DB.Query(`SELECT id, visit_date, channel, pv, uv, new_uv, member_uv
		FROM tb_visit_daily WHERE visit_date>=? AND visit_date<=? ORDER BY visit_date, channel`, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []po.VisitDaily{}
	for rows.Next() {
		var d po.VisitDaily
		if err := rows.Scan(&d.ID, &d.VisitDate, &d.Channel, &d.PV, &d.UV, &d.NewUV, &d.MemberUV); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ChannelUVRow 单个渠道在窗口内的 UV(COUNT DISTINCT 近似值)。
type ChannelUVRow struct {
	Channel string
	UV      int
}

// ChannelUVInRange 从明细统计区间内各渠道 UV(含首尾日期)。
// 受明细保留期约束:早于保留期的日期 UV 归零(台账里的 pv 仍完整)。
func ChannelUVInRange(start, end string) ([]ChannelUVRow, error) {
	rows, err := store.DB.Query(`SELECT channel, COUNT(DISTINCT visitor_key) FROM tb_visit_log
		WHERE visit_date>=? AND visit_date<=? GROUP BY channel`, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ChannelUVRow{}
	for rows.Next() {
		var r ChannelUVRow
		if err := rows.Scan(&r.Channel, &r.UV); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DistinctVisitorsInRange 区间独立访客数(跨渠道去重,含首尾日期)。
// 读明细,受保留期约束:区间早于保留期时结果为下限。
func DistinctVisitorsInRange(start, end string) (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(DISTINCT visitor_key) FROM tb_visit_log
		WHERE visit_date>=? AND visit_date<=?`, start, end).Scan(&n)
	return n, err
}

// VisitDistRow 终端构成分布的一行(设备类型/操作系统/浏览器共用)。
type VisitDistRow struct {
	Name string
	PV   int
}

// visitDistColumns 分布维度白名单:GROUP BY 的列名不能来自用户输入,
// 这里显式枚举,枚举之外的维度一律拒绝。
var visitDistColumns = map[string]bool{
	"os":      true,
	"browser": true,
	"device":  true,
}

// VisitDistribution 区间内某个终端维度的分布(含首尾日期,按 PV 倒序)。
// 空值行原样返回(名称为空串),由上层归一为「未知」展示 —— 不在 SQL 里过滤:
// 分布各项之和恒等于区间总 PV,「未知」占比本身就是采集覆盖率的直观呈现。
func VisitDistribution(start, end, column string) ([]VisitDistRow, error) {
	if !visitDistColumns[column] {
		return nil, fmt.Errorf("不支持的分布维度: %s", column)
	}
	// 列名来自上面的白名单,并非用户输入拼接。
	rows, err := store.DB.Query(`SELECT `+column+`, COUNT(*) AS pv FROM tb_visit_log
		WHERE visit_date>=? AND visit_date<=?
		GROUP BY `+column+` ORDER BY pv DESC, `+column, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []VisitDistRow{}
	for rows.Next() {
		var r VisitDistRow
		if err := rows.Scan(&r.Name, &r.PV); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DistinctMemberVisitorsInRange 区间会员访客数(登录会员的去重访客,含首尾日期)。
func DistinctMemberVisitorsInRange(start, end string) (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(DISTINCT visitor_key) FROM tb_visit_log
		WHERE visit_date>=? AND visit_date<=? AND is_member=1`, start, end).Scan(&n)
	return n, err
}

// NewVisitorsInRange 区间新访客数:访客注册表中首访日期落在区间内的档案数。
// 读台账(tb_visitor),不受明细保留期影响,精确。
func NewVisitorsInRange(start, end string) (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visitor
		WHERE first_visit_date>=? AND first_visit_date<=?`, start, end).Scan(&n)
	return n, err
}

// VisitEntryCount 单个扫码入口(桌台码)的访问次数。
type VisitEntryCount struct {
	Entry string // 落库的入口标识:桌台点餐码,历史二维码则是数字桌台 ID
	PV    int
}

// TopVisitEntryCodes 区间内「扫码入口」访问量排行(最多 limit 条)。
//
// 刻意只查 tb_visit_log 单表、返回纯「码 → 次数」,与桌台的关联放在 Go 侧
// (见 ListTableIdentities):跨表比较 `CAST(table_id AS CHAR) = entry` 在
// MySQL 8 下会因排序规则混用直接报 Error 1267(CAST 结果携带连接默认的
// utf8mb4_0900_ai_ci,与表列的 utf8mb4_general_ci 冲突;SQLite 没有这个
// 概念,单测拦不住)。数字桌台 ID 与码两种形态由调用方统一匹配。
func TopVisitEntryCodes(start, end string, limit int) ([]VisitEntryCount, error) {
	// 夹紧而不是静默改写:调用方传 100 时只拿到 10 条、却完全不知道被截断,
	// 会以为是「只有 10 个入口」。<=0 用默认值,过大则夹到上限(仍返回前 N 名)。
	const entryRankMax = 50
	if limit <= 0 {
		limit = 10
	}
	if limit > entryRankMax {
		limit = entryRankMax
	}
	rows, err := store.DB.Query(`SELECT entry, COUNT(*) AS pv FROM tb_visit_log
		WHERE visit_date>=? AND visit_date<=? AND entry<>''
		GROUP BY entry ORDER BY pv DESC, entry
		LIMIT ?`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []VisitEntryCount{}
	for rows.Next() {
		var r VisitEntryCount
		if err := rows.Scan(&r.Entry, &r.PV); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TableIdentity 入口榜关联桌台所需的展示字段。
type TableIdentity struct {
	TableID   int
	TableNo   string
	TableName string
	TableCode string
}

// ListTableIdentities 返回全部未删除桌台(单店几十行,供入口榜做「码 → 桌」关联)。
// 纯单表查询,无 JOIN/CAST,两库行为完全一致。
func ListTableIdentities() ([]TableIdentity, error) {
	rows, err := store.DB.Query(`SELECT table_id, COALESCE(table_no,''), COALESCE(table_name,''), COALESCE(table_code,'')
		FROM tb_table WHERE del_flag='0'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []TableIdentity{}
	for rows.Next() {
		var r TableIdentity
		if err := rows.Scan(&r.TableID, &r.TableNo, &r.TableName, &r.TableCode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountVisitors 累计独立访客数(访客注册表行数)。
func CountVisitors() (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visitor`).Scan(&n)
	return n, err
}

// CountMembers 启用中的会员数(tb_customer)—— 「用户量」的精确口径。
func CountMembers() (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_customer WHERE status=?`, po.CustomerStatusEnabled).Scan(&n)
	return n, err
}
