package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/print"
	"dining-system/internal/store"
)

// ============ 打印日志与补打 ============

// PrintLogList 打印日志列表。
//
// 打印是异步的且依赖打印机在线状态,「小票没出来」是餐饮门店最常见的报障之一。
// 这个列表回答三个问题: 这一单打了没? 打到哪台机器? 失败原因是什么?
// 支持按状态(只看失败)、单据类型、单号关键词筛选。
func PrintLogList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	where := " WHERE 1=1"
	args := []interface{}{}
	if v := c.Query("status"); v != "" {
		if n := intArg(v); n != nil {
			where += " AND status=?"
			args = append(args, *n)
		}
	}
	if v := c.Query("docType"); v != "" {
		where += " AND doc_type=?"
		args = append(args, v)
	}
	if v := c.Query("provider"); v != "" {
		where += " AND provider=?"
		args = append(args, v)
	}
	if v := c.Query("printerId"); v != "" {
		if n := intArg(v); n != nil {
			where += " AND printer_id=?"
			args = append(args, *n)
		}
	}
	if v := c.Query("orderNo"); v != "" {
		where += " AND order_no LIKE ?"
		args = append(args, "%"+v+"%")
	}

	var total int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_log`+where, args...).Scan(&total)

	rows, err := store.DB.Query(`SELECT `+store.PrintLogCols+` FROM tb_print_log`+where+
		` ORDER BY print_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.PrintLog{}
	for rows.Next() {
		if l, err := store.ScanPrintLog(rows); err == nil {
			list = append(list, l)
		}
	}
	tableResult(c, total, list)
}

// PrintLogReprint 按日志重打:同订单、同打印机、同单据类型再送一次。
//
// 走 PrintLog 而不是让前端自己拼参数,是因为日志里同时存了打印机与单据类型,
// 一条记录就能唯一确定「重打什么」,前端不必回查订单明细。
func PrintLogReprint(c *gin.Context) {
	var p struct {
		PrintID int `json:"printId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.PrintID == 0 {
		fail(c, "参数错误")
		return
	}
	entry, err := store.LoadPrintLog(p.PrintID)
	if err != nil {
		fail(c, "打印记录不存在")
		return
	}
	if entry.OrderID == 0 {
		fail(c, "该记录不是订单单据(如测试打印),无法重打")
		return
	}
	if err := print.Reprint(entry.PrinterID, entry.OrderID, entry.DocType, adminName(c)); err != nil {
		fail(c, "补打失败:"+err.Error())
		return
	}
	okMsg(c, "已重新发送到「"+entry.PrinterName+"」")
}

// OrderReprint 按订单补打:可在订单详情里指定打印机与单据类型。
// 没有传 printerId 时,自动选一台该单据类型的启用打印机,省去先查打印机列表。
func OrderReprint(c *gin.Context) {
	var p struct {
		OrderID   int    `json:"orderId"`
		PrinterID int    `json:"printerId"`
		DocType   string `json:"docType"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	if p.DocType != model.PrintDocKitchen {
		p.DocType = model.PrintDocGuest
	}
	printerID := p.PrinterID
	if printerID == 0 {
		printerType := model.PrinterTypeGuest
		if p.DocType == model.PrintDocKitchen {
			printerType = model.PrinterTypeKitchen
		}
		err := store.DB.QueryRow(`SELECT printer_id FROM tb_printer
			WHERE del_flag='0' AND status=1 AND printer_type=? ORDER BY printer_id LIMIT 1`, printerType).
			Scan(&printerID)
		if err != nil {
			fail(c, "没有启用中的"+printerTypeLabel(printerType)+"打印机,请先到「打印机管理」启用一台")
			return
		}
	}
	if err := print.Reprint(printerID, p.OrderID, p.DocType, adminName(c)); err != nil {
		fail(c, "补打失败:"+err.Error())
		return
	}
	okMsg(c, "补打指令已发送")
}

// printerTypeLabel 打印机类型中文名(错误提示用)。
func printerTypeLabel(t int) string {
	if t == model.PrinterTypeGuest {
		return "食客小票"
	}
	return "厨房单"
}
