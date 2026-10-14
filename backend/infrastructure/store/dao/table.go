package dao

import (
	"database/sql"
	"errors"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// ErrTableLocked 表示桌台被人工停用或预约,拒绝占用(新点餐)。
var ErrTableLocked = errors.New("table locked")

// TableCols 桌台表完整列(与 ScanTable 顺序一一对应)。
const TableCols = `table_id, table_no, table_name, capacity, status, lock_status, lock_remark, sort_order, del_flag,
	create_by, create_time, update_by, update_time, remark, table_code`

// ScanTable 扫描一行桌台记录。
func ScanTable(rows interface{ Scan(...interface{}) error }) (po.Table, error) {
	var t po.Table
	err := rows.Scan(&t.TableID, &t.TableNo, &t.TableName, &t.Capacity, &t.Status, &t.LockStatus, &t.LockRemark, &t.SortOrder, &t.DelFlag,
		&t.CreateBy, TS(&t.CreateTime), &t.UpdateBy, TS(&t.UpdateTime), &t.Remark, &t.TableCode)
	return t, err
}

// SetTableStatusTx 在事务中更新桌台状态。
//
// 带未删除守卫:并发删除桌台时不会把已删桌台的状态改回去;RowsAffected=0
// (桌台已删/不存在)视为无害的空操作 —— 订单流转不应因为桌台被删而整个回滚
// (订单仍需能完成/取消/结算)。
func SetTableStatusTx(tx *sql.Tx, tableID, status int) error {
	_, err := tx.Exec(`UPDATE tb_table SET status=?, update_time=? WHERE table_id=? AND del_flag='0'`, status, store.Now(), tableID)
	return err
}

// GetTableStatus 读取桌台占用状态(0 空闲 / 1 占用)。桌台不存在或已删除时返回错误。
func GetTableStatus(tableID int) (int, error) {
	var s int
	err := store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=? AND del_flag='0'`, tableID).Scan(&s)
	return s, err
}

// SetTableStatus 非事务版桌台状态更新,返回受影响行数(0 表示桌台不存在或已删除)。
//
// 仅供「强制清台」这类人工兜底操作使用:它跨多笔订单逐个结算/取消,无法整体放进
// 一个事务;常规流转一律走 SetTableStatusTx 以保证与订单变更原子完成。
func SetTableStatus(tableID, status int) (int64, error) {
	res, err := store.DB.Exec(`UPDATE tb_table SET status=?, update_time=? WHERE table_id=? AND del_flag='0'`, status, store.Now(), tableID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// OccupyTableTx 在事务中占用桌台,带未删除与「未被人工停用/预约」守卫;返回受影响行数
// (0 表示桌台不存在)。桌台被停用/预约时返回 ErrTableLocked。
//
// 与 SetTableStatusTx 的区别:并发下单场景要求「桌台存在、未删除且 lock_status=0」才允许占用,
// 同时利用该 UPDATE 在 MySQL/SQLite 上先取行写锁,把同一桌台的并发下单串行化。
//
// MySQL 未开启 clientFoundRows 时,no-op UPDATE 会返回 0 受影响行数(桌台已是占用态),
// 与「桌台不存在」无法区分;这里在 n==0 时回查存在性与 lock_status,据实区分三种结果。
func OccupyTableTx(tx *sql.Tx, tableID int) (int64, error) {
	res, err := tx.Exec(`UPDATE tb_table SET status=1, update_time=? WHERE table_id=? AND del_flag='0' AND lock_status=0`, store.Now(), tableID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return n, nil
	}
	var exists, lockStatus int
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(MAX(lock_status),0) FROM tb_table WHERE table_id=? AND del_flag='0'`, tableID).
		Scan(&exists, &lockStatus); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	if lockStatus != po.TableLockNormal {
		return 0, ErrTableLocked
	}
	return 1, nil
}

// ListAllTables 查询全部未删除桌台。
func ListAllTables() ([]po.Table, error) {
	rows, err := store.DB.Query(`SELECT ` + TableCols + ` FROM tb_table WHERE del_flag='0' ORDER BY sort_order, table_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []po.Table{}
	for rows.Next() {
		t, scanErr := ScanTable(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// TableQuery 桌台列表筛选条件;零值字段表示不限。
type TableQuery struct {
	Keyword string // 桌台名称模糊匹配
}

// ListTables 按条件分页查询桌台。
func ListTables(q TableQuery, pageNum, pageSize int) (total int, list []po.Table, err error) {
	where := []string{"del_flag='0'"}
	args := []interface{}{}
	if q.Keyword != "" {
		where = append(where, "table_name LIKE ?")
		args = append(args, "%"+q.Keyword+"%")
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+TableCols+` FROM tb_table`+cond+` ORDER BY sort_order, table_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return
	}
	defer rows.Close()
	list = []po.Table{}
	for rows.Next() {
		t, scanErr := ScanTable(rows)
		if scanErr != nil {
			// 单行扫描失败(列对齐/类型不符)必须报错,静默丢弃会表现为「莫名少数据」。
			return 0, nil, scanErr
		}
		list = append(list, t)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// GetTableByID 按主键读取整张桌台记录(已删除视为不存在,返回 sql.ErrNoRows)。
//
// 需要同时用上桌号/桌名/人工状态等多个字段的调用方走这里,避免对同一行连查数次
// (如顾客下单前的「桌台存在 + 人工状态可用」校验)。
func GetTableByID(tableID int) (po.Table, error) {
	return ScanTable(store.DB.QueryRow(`SELECT `+TableCols+` FROM tb_table WHERE table_id=? AND del_flag='0'`, tableID))
}

// InsertTable 新增桌台并返回主键 ID。
func InsertTable(t po.Table) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, lock_status, lock_remark, sort_order, del_flag, table_code, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, t.TableNo, t.TableName, t.Capacity, t.Status, t.LockStatus, t.LockRemark, t.SortOrder, "0", store.NewUniqueTableCode(), store.Now(), store.Now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		// 取不到自增 ID 必须报错:调用方拿 0 去补发桌台码会静默失败,新桌台无二维码。
		return 0, err
	}
	return id, nil
}

// UpdateTable 更新桌台基础信息(桌号/桌名/容纳人数/排序)。
//
// 刻意不更新 status:占用状态由订单流转自动维护,后台编辑表单并不提交该字段,
// 若在此覆盖会把正在用餐的桌台「改」成空闲,导致看板丢单。
// 人工可用性状态(lock_status/lock_remark)统一走 SetTableLock,便于单独加业务守卫。
func UpdateTable(t po.Table) error {
	_, err := store.DB.Exec(`UPDATE tb_table SET table_no=?, table_name=?, capacity=?, sort_order=?, update_time=? WHERE table_id=?`,
		t.TableNo, t.TableName, t.Capacity, t.SortOrder, store.Now(), t.TableID)
	return err
}

// SetTableLock 更新桌台人工状态(lock_status/lock_remark),幂等。
func SetTableLock(id, lockStatus int, lockRemark string) error {
	_, err := store.DB.Exec(`UPDATE tb_table SET lock_status=?, lock_remark=?, update_time=? WHERE table_id=?`,
		lockStatus, lockRemark, store.Now(), id)
	return err
}

// DeleteTable 逻辑删除桌台。
func DeleteTable(id int) error {
	_, err := store.DB.Exec(`UPDATE tb_table SET del_flag='1', update_time=? WHERE table_id=?`, store.Now(), id)
	return err
}

// GetTableByRef 按「桌台码」或「数字桌台ID」解析桌台,兼容老二维码。
// 先按桌台码解析(大小写不敏感),失败再按纯数字 ID 解析,
// 这样既支持新印制的随机码,也不影响历史已印制的 /order/1 链接。
//
// 第二个返回值 byID 表示本次命中走的是「纯数字 table_id」路径(老二维码)。
// 数字 ID 可被攻击者遍历 1..N 枚举,不能作为订单能力凭据;顾客端据此跳过 currentOrder 的查询,
// 避免把进行中订单(订单号/菜品/金额/渠道交易号)批量泄露给匿名访客。
func GetTableByRef(ref string) (po.Table, bool, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		row := store.DB.QueryRow(`SELECT `+TableCols+` FROM tb_table WHERE table_code=? AND del_flag='0'`, strings.ToUpper(ref))
		if t, err := ScanTable(row); err == nil {
			return t, false, nil
		}
	}
	id, ok := parseRefID(ref)
	if !ok {
		return po.Table{}, false, sql.ErrNoRows
	}
	t, err := ScanTable(store.DB.QueryRow(`SELECT `+TableCols+` FROM tb_table WHERE table_id=? AND del_flag='0'`, id))
	return t, true, err
}

// parseRefID 仅接受纯数字的正整数,拒绝 "1abc" / "-1" / 空串 / 超长串。
func parseRefID(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}
