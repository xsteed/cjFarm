package store

import (
	"strconv"
	"strings"

	"dining-system/internal/model"
)

// ==================== 打印日志 ====================

// PrintLogCols 打印日志完整列(与 ScanPrintLog 顺序一一对应)。
const PrintLogCols = `print_id, order_id, order_no, table_no, table_name,
	printer_id, printer_name, printer_type, provider, doc_type, copies,
	status, remote_id, detail, trigger_by, operator, cost_ms, create_time`

// ScanPrintLog 扫描一行打印日志记录。
func ScanPrintLog(rows interface{ Scan(...interface{}) error }) (model.PrintLog, error) {
	var l model.PrintLog
	err := rows.Scan(&l.PrintID, &l.OrderID, &l.OrderNo, &l.TableNo, &l.TableName,
		&l.PrinterID, &l.PrinterName, &l.PrinterType, &l.Provider, &l.DocType, &l.Copies,
		&l.Status, &l.RemoteID, &l.Detail, &l.TriggerBy, &l.Operator, &l.CostMs, &l.CreateTime)
	if err != nil {
		return l, err
	}
	l.ShortNo = model.ShortOrderNo(l.OrderNo)
	return l, nil
}

// InsertPrintLog 写入一条打印日志。
//
// 打印日志是「排查用」的旁路数据,写入失败绝不能反向影响下单/结账主流程,
// 故这里只返回 error 由调用方决定是否记后端日志,不做任何重试或 panic。
func InsertPrintLog(l model.PrintLog) error {
	_, err := DB.Exec(`INSERT INTO tb_print_log(order_id, order_no, table_no, table_name,
		printer_id, printer_name, printer_type, provider, doc_type, copies,
		status, remote_id, detail, trigger_by, operator, cost_ms, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.OrderID, l.OrderNo, l.TableNo, l.TableName,
		l.PrinterID, l.PrinterName, l.PrinterType, l.Provider, l.DocType, l.Copies,
		l.Status, l.RemoteID, l.Detail, l.TriggerBy, l.Operator, l.CostMs, l.CreateTime)
	return err
}

// LoadPrinter 按 ID 读取一台打印机(含已停用/已删除 — 补打历史单据时仍需要它)。
func LoadPrinter(printerID int) (model.Printer, error) {
	var p model.Printer
	err := DB.QueryRow(`SELECT printer_id, printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time
		FROM tb_printer WHERE printer_id=?`, printerID).
		Scan(&p.PrinterID, &p.PrinterName, &p.PrinterType, &p.Provider, &p.IP, &p.Port,
			&p.FeieSN, &p.PaperWidth, &p.Copies, &p.CategoryIDs, &p.Status, &p.DelFlag, &p.CreateTime, &p.UpdateTime)
	if err != nil {
		return p, err
	}
	p.CategoryIDList = ParseIDList(p.CategoryIDs)
	return p, nil
}

// LoadPrintLog 按 ID 读取一条打印日志。
func LoadPrintLog(printID int) (model.PrintLog, error) {
	return ScanPrintLog(DB.QueryRow(`SELECT ` + PrintLogCols + ` FROM tb_print_log WHERE print_id=?`))
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

// AttachItemCategories 按 dish_id 补齐订单明细的分类 ID。
//
// 打印层需要「按菜品分类分单」(凉菜机只收凉菜),而 tb_order_item 只冗余了
// dish_name / spec_name,没有 category_id —— 与其为打印这一个场景改订单明细表结构,
// 不如在打印前用一次 IN 查询补齐(下单明细数量是十位级,代价可忽略)。
func AttachItemCategories(items []model.OrderItem) []model.OrderItem {
	if len(items) == 0 {
		return items
	}
	seen := map[int]bool{}
	ids := []interface{}{}
	for _, it := range items {
		if it.DishID > 0 && !seen[it.DishID] {
			seen[it.DishID] = true
			ids = append(ids, it.DishID)
		}
	}
	if len(ids) == 0 {
		return items
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := DB.Query(`SELECT dish_id, category_id FROM tb_dish WHERE dish_id IN (`+placeholders+`)`, ids...)
	if err != nil {
		return items
	}
	defer rows.Close()
	cat := map[int]int{}
	for rows.Next() {
		var dishID, categoryID int
		if rows.Scan(&dishID, &categoryID) == nil {
			cat[dishID] = categoryID
		}
	}
	for i := range items {
		if c, ok := cat[items[i].DishID]; ok {
			items[i].CategoryID = c
		}
	}
	return items
}
