package dao

import (
	"database/sql"
	"strings"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ==================== 催菜 ====================

// UrgeCols 催菜表完整列(与 ScanUrge 顺序一一对应)。
const UrgeCols = `urge_id, order_id, order_no, table_id, table_no, table_name,
	urge_type, status, remark, create_time, handle_time, handle_by`

// ScanUrge 扫描一行催菜记录。handle_time 可为 NULL,转成指针避免前端空串分支。
func ScanUrge(rows interface{ Scan(...interface{}) error }) (po.OrderUrge, error) {
	var u po.OrderUrge
	var handleTime sql.NullString
	err := rows.Scan(&u.UrgeID, &u.OrderID, &u.OrderNo, &u.TableID, &u.TableNo, &u.TableName,
		&u.UrgeType, &u.Status, &u.Remark, &u.CreateTime, &handleTime, &u.HandleBy)
	if err != nil {
		return u, err
	}
	if handleTime.Valid {
		u.HandleTime = &handleTime.String
	}
	return u, nil
}

// LastUrgeTime 返回该订单最近一次催菜时间;从未催过时 ok=false。
// 用于顾客端冷却判断,防止同一桌连点刷屏。
func LastUrgeTime(orderID int) (time.Time, bool) {
	var s string
	err := store.DB.QueryRow(`SELECT create_time FROM tb_order_urge
		WHERE order_id=? ORDER BY urge_id DESC LIMIT 1`, orderID).Scan(&s)
	if err != nil || s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(conf.TimeLayout, s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// PendingUrgeOrderIDs 返回存在「待处理催菜」的订单 ID 集合。
// 订单列表/看板据此给对应订单打催菜角标,避免逐条查询。
func PendingUrgeOrderIDs() map[int]bool {
	out := map[int]bool{}
	rows, err := store.DB.Query(`SELECT DISTINCT order_id FROM tb_order_urge WHERE status=?`,
		po.UrgeStatusPending)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// HasPendingUrge 判断指定订单是否存在未处理的催菜。单条详情查询用。
func HasPendingUrge(orderID int) bool {
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_urge WHERE order_id=? AND status=?`,
		orderID, po.UrgeStatusPending).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// HandleUrgesByOrder 把某订单下所有待处理催菜标记为已处理。
// 与订单状态流转配合:上菜(状态3)或完成时自动消解,避免残留陈旧催菜。
func HandleUrgesByOrder(orderID int, operator string) {
	store.DB.Exec(`UPDATE tb_order_urge SET status=?, handle_time=?, handle_by=?
		WHERE order_id=? AND status=?`,
		po.UrgeStatusHandled, store.Now(), operator, orderID, po.UrgeStatusPending)
}

// UrgeQuery 催菜列表筛选条件;零值字段表示不限。
type UrgeQuery struct {
	Status  *int   // nil 表示不限
	TableNo string // 精确匹配
}

// ListUrges 按条件分页查询催菜记录。
func ListUrges(q UrgeQuery, pageNum, pageSize int) (total int, list []po.OrderUrge, err error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if q.Status != nil {
		where = append(where, "status=?")
		args = append(args, *q.Status)
	}
	if q.TableNo != "" {
		where = append(where, "table_no=?")
		args = append(args, q.TableNo)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_urge`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+UrgeCols+` FROM tb_order_urge`+cond+
		` ORDER BY status, urge_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, nil, err
	}
	defer rows.Close()
	list = []po.OrderUrge{}
	for rows.Next() {
		u, scanErr := ScanUrge(rows)
		if scanErr != nil {
			return 0, nil, scanErr
		}
		list = append(list, u)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// HandleUrge 处理单条待处理催菜,返回影响行数。
func HandleUrge(urgeID int, operator string) (int64, error) {
	res, err := store.DB.Exec(`UPDATE tb_order_urge SET status=?, handle_time=?, handle_by=? WHERE urge_id=? AND status=?`,
		po.UrgeStatusHandled, store.Now(), operator, urgeID, po.UrgeStatusPending)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// InsertUrge 新增一条催菜记录。
func InsertUrge(orderID int, orderNo string, tableID int, tableNo, tableName string, urgeType string, status int) error {
	_, err := store.DB.Exec(`INSERT INTO tb_order_urge(order_id, order_no, table_id, table_no, table_name, urge_type, status, create_time) VALUES(?,?,?,?,?,?,?,?)`,
		orderID, orderNo, tableID, tableNo, tableName, urgeType, status, store.Now())
	return err
}

// CountRecentUrgesTx 统计订单在 cutoff 之后产生的催菜记录数(事务内冷却校验用)。
func CountRecentUrgesTx(tx *sql.Tx, orderID int, cutoff string) (int, error) {
	var recent int
	err := tx.QueryRow(`SELECT COUNT(*) FROM tb_order_urge WHERE order_id=? AND create_time>?`, orderID, cutoff).Scan(&recent)
	return recent, err
}

// InsertUrgeTx 在事务中新增一条催菜记录。
func InsertUrgeTx(tx *sql.Tx, orderID int, orderNo string, tableID int, tableNo, tableName string, urgeType string, status int) error {
	_, err := tx.Exec(`INSERT INTO tb_order_urge(order_id, order_no, table_id, table_no, table_name, urge_type, status, create_time) VALUES(?,?,?,?,?,?,?,?)`,
		orderID, orderNo, tableID, tableNo, tableName, urgeType, status, store.Now())
	return err
}
