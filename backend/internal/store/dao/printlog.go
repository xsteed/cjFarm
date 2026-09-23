package dao

import (
	"database/sql"
	"strconv"
	"strings"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ==================== 打印日志 ====================

// PrintLogCols 打印日志完整列(与 ScanPrintLog 顺序一一对应)。
const PrintLogCols = `print_id, order_id, order_no, table_no, table_name,
	printer_id, printer_name, printer_type, provider, doc_type, copies,
	status, remote_id, detail, trigger_by, operator, cost_ms, create_time`

// ScanPrintLog 扫描一行打印日志记录。
func ScanPrintLog(rows interface{ Scan(...interface{}) error }) (po.PrintLog, error) {
	var l po.PrintLog
	err := rows.Scan(&l.PrintID, &l.OrderID, &l.OrderNo, &l.TableNo, &l.TableName,
		&l.PrinterID, &l.PrinterName, &l.PrinterType, &l.Provider, &l.DocType, &l.Copies,
		&l.Status, &l.RemoteID, &l.Detail, &l.TriggerBy, &l.Operator, &l.CostMs, &l.CreateTime)
	if err != nil {
		return l, err
	}
	return l, nil
}

// InsertPrintLog 写入一条打印日志。
//
// 打印日志是「排查用」的旁路数据,写入失败绝不能反向影响下单/结账主流程,
// 故这里只返回 error 由调用方决定是否记后端日志,不做任何重试或 panic。
func InsertPrintLog(l po.PrintLog) error {
	_, err := insertPrintLog(l)
	return err
}

// InsertPrintLogReturningID 写入一条打印日志并返回主键。
//
// 本地打印代理通道需要它:入队时先落一条「排队中」日志,并把这个 log_id 记在
// 任务行上,等代理回执时再回写成「已送出 / 失败原因」——一单一条日志,
// 而不是入队、送出各记一条(否则打印日志页会出现两行,商家反而看不清这单到底打没打)。
func InsertPrintLogReturningID(l po.PrintLog) (int, error) {
	res, err := insertPrintLog(l)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// insertPrintLog 公共写入实现。
func insertPrintLog(l po.PrintLog) (sql.Result, error) {
	return store.DB.Exec(`INSERT INTO tb_print_log(order_id, order_no, table_no, table_name,
		printer_id, printer_name, printer_type, provider, doc_type, copies,
		status, remote_id, detail, trigger_by, operator, cost_ms, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.OrderID, l.OrderNo, l.TableNo, l.TableName,
		l.PrinterID, l.PrinterName, l.PrinterType, l.Provider, l.DocType, l.Copies,
		l.Status, l.RemoteID, l.Detail, l.TriggerBy, l.Operator, l.CostMs, l.CreateTime)
}

// LoadPrinter 按 ID 读取一台打印机(含已停用/已删除 — 补打历史单据时仍需要它)。
func LoadPrinter(printerID int) (po.Printer, error) {
	var p po.Printer
	err := store.DB.QueryRow(`SELECT printer_id, printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time
		FROM tb_printer WHERE printer_id=?`, printerID).
		Scan(&p.PrinterID, &p.PrinterName, &p.PrinterType, &p.Provider, &p.IP, &p.Port,
			&p.FeieSN, &p.PaperWidth, &p.Copies, &p.CategoryIDs, &p.Status, &p.DelFlag, &p.CreateTime, &p.UpdateTime)
	if err != nil {
		return p, err
	}
	return p, nil
}

// LoadPrintLog 按 ID 读取一条打印日志。
func LoadPrintLog(printID int) (po.PrintLog, error) {
	return ScanPrintLog(store.DB.QueryRow(`SELECT `+PrintLogCols+` FROM tb_print_log WHERE print_id=?`, printID))
}

// PrintLogQuery 打印日志列表筛选条件;零值字段表示不限。
type PrintLogQuery struct {
	Status    *int   // nil 表示不限
	DocType   string // 精确匹配
	Provider  string // 精确匹配
	PrinterID *int   // nil 表示不限
	OrderNo   string // 模糊匹配
}

// ListPrintLogs 按条件分页查询打印日志。
func ListPrintLogs(q PrintLogQuery, pageNum, pageSize int) (total int, list []po.PrintLog, err error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if q.Status != nil {
		where = append(where, "status=?")
		args = append(args, *q.Status)
	}
	if q.DocType != "" {
		where = append(where, "doc_type=?")
		args = append(args, q.DocType)
	}
	if q.Provider != "" {
		where = append(where, "provider=?")
		args = append(args, q.Provider)
	}
	if q.PrinterID != nil {
		where = append(where, "printer_id=?")
		args = append(args, *q.PrinterID)
	}
	if q.OrderNo != "" {
		where = append(where, "order_no LIKE ?")
		args = append(args, "%"+q.OrderNo+"%")
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	list = []po.PrintLog{}
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_log`+cond, args...).Scan(&total); err != nil {
		return 0, list, err
	}

	rows, err := store.DB.Query(`SELECT `+PrintLogCols+` FROM tb_print_log`+cond+
		` ORDER BY print_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, list, err
	}
	defer rows.Close()
	for rows.Next() {
		l, scanErr := ScanPrintLog(rows)
		if scanErr != nil {
			return 0, list, scanErr
		}
		list = append(list, l)
	}
	if err = rows.Err(); err != nil {
		return 0, list, err
	}
	return total, list, nil
}

// ParseIDList 把 CSV ID 串解析为整数切片(忽略空项与非法项)。
// 分类分单的 category_ids 存在一个 VARCHAR 里而不是独立映射表:
// 单台打印机的分类数是个位数量级、且永远整读整写,没有反查需求,
// 与 tb_role.perms 的处理方式保持一致。
func ParseIDList(csv string) []int {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	out := []int{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n := 0
		ok := true
		for _, r := range part {
			if r < '0' || r > '9' {
				ok = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if ok && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// JoinIDList 把整数 ID 切片拼成 CSV(去重、保持首次出现顺序)。
func JoinIDList(ids []int) string {
	seen := map[int]bool{}
	parts := []string{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

// OrderItemRow 订单明细+打印分类投影;CategoryID 不入 tb_order_item 表,由 dish 关联补齐。
type OrderItemRow struct {
	po.OrderItem
	CategoryID int
}

// AttachItemCategories 按 dish_id 补齐订单明细的分类 ID。
//
// 打印层需要「按菜品分类分单」(凉菜机只收凉菜),而 tb_order_item 只冗余了
// dish_name / spec_name,没有 category_id —— 与其为打印这一个场景改订单明细表结构,
// 不如在打印前用一次 IN 查询补齐(下单明细数量是十位级,代价可忽略)。
func AttachItemCategories(items []po.OrderItem) []OrderItemRow {
	if len(items) == 0 {
		return nil
	}
	seen := map[int]bool{}
	ids := []interface{}{}
	for _, it := range items {
		if it.DishID > 0 && !seen[it.DishID] {
			seen[it.DishID] = true
			ids = append(ids, it.DishID)
		}
	}
	out := make([]OrderItemRow, 0, len(items))
	if len(ids) == 0 {
		for _, it := range items {
			out = append(out, OrderItemRow{OrderItem: it})
		}
		return out
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := store.DB.Query(`SELECT dish_id, category_id FROM tb_dish WHERE dish_id IN (`+placeholders+`)`, ids...)
	if err != nil {
		for _, it := range items {
			out = append(out, OrderItemRow{OrderItem: it})
		}
		return out
	}
	defer rows.Close()
	cat := map[int]int{}
	for rows.Next() {
		var dishID, categoryID int
		if rows.Scan(&dishID, &categoryID) == nil {
			cat[dishID] = categoryID
		}
	}
	for _, it := range items {
		row := OrderItemRow{OrderItem: it}
		if c, ok := cat[it.DishID]; ok {
			row.CategoryID = c
		}
		out = append(out, row)
	}
	return out
}
