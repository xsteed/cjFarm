package store

import (
	"database/sql"
	"time"

	"dining-system/internal/model"
)

// ==================== 催菜 ====================

// UrgeCols 催菜表完整列(与 ScanUrge 顺序一一对应)。
const UrgeCols = `urge_id, order_id, order_no, table_id, table_no, table_name,
	urge_type, status, remark, create_time, handle_time, handle_by`

// ScanUrge 扫描一行催菜记录。handle_time 可为 NULL,转成指针避免前端空串分支。
func ScanUrge(rows interface{ Scan(...interface{}) error }) (model.OrderUrge, error) {
	var u model.OrderUrge
	var handleTime sql.NullString
	err := rows.Scan(&u.UrgeID, &u.OrderID, &u.OrderNo, &u.TableID, &u.TableNo, &u.TableName,
		&u.UrgeType, &u.Status, &u.Remark, &u.CreateTime, &handleTime, &u.HandleBy)
	if err != nil {
		return u, err
	}
	if handleTime.Valid {
		u.HandleTime = &handleTime.String
	}
	u.ShortNo = model.ShortOrderNo(u.OrderNo)
	return u, nil
}

// LastUrgeTime 返回该订单最近一次催菜时间;从未催过时 ok=false。
// 用于顾客端冷却判断,防止同一桌连点刷屏。
func LastUrgeTime(orderID int) (time.Time, bool) {
	var s string
	err := DB.QueryRow(`SELECT create_time FROM tb_order_urge
		WHERE order_id=? ORDER BY urge_id DESC LIMIT 1`, orderID).Scan(&s)
	if err != nil || s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// PendingUrgeOrderIDs 返回存在「待处理催菜」的订单 ID 集合。
// 订单列表/看板据此给对应订单打催菜角标,避免逐条查询。
func PendingUrgeOrderIDs() map[int]bool {
	out := map[int]bool{}
	rows, err := DB.Query(`SELECT DISTINCT order_id FROM tb_order_urge WHERE status=?`,
		model.UrgeStatusPending)
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
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_order_urge WHERE order_id=? AND status=?`,
		orderID, model.UrgeStatusPending).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// HandleUrgesByOrder 把某订单下所有待处理催菜标记为已处理。
// 与订单状态流转配合:上菜(状态3)或完成时自动消解,避免残留陈旧催菜。
func HandleUrgesByOrder(orderID int, operator string) {
	DB.Exec(`UPDATE tb_order_urge SET status=?, handle_time=?, handle_by=?
		WHERE order_id=? AND status=?`,
		model.UrgeStatusHandled, Now(), operator, orderID, model.UrgeStatusPending)
}
