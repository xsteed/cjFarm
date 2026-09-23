package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/print"
	"dining-system/internal/service"
)

// ============ 打印日志与补打 ============

// PrintLogList 打印日志列表。
//
// 打印是异步的且依赖打印机在线状态,「小票没出来」是餐饮门店最常见的报障之一。
// 这个列表回答三个问题: 这一单打了没? 打到哪台机器? 失败原因是什么?
// 支持按状态(只看失败)、单据类型、单号关键词筛选。
func PrintLogList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q service.PrintLogQuery
	if v := c.Query("status"); v != "" {
		q.Status = intArg(v)
	}
	if v := c.Query("docType"); v != "" {
		q.DocType = v
	}
	if v := c.Query("provider"); v != "" {
		q.Provider = v
	}
	if v := c.Query("printerId"); v != "" {
		q.PrinterID = intArg(v)
	}
	if v := c.Query("orderNo"); v != "" {
		q.OrderNo = v
	}

	total, list, err := service.ListPrintLogs(q, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	items := make([]dto.PrintLog, 0, len(list))
	for _, l := range list {
		items = append(items, dto.FromPrintLog(l))
	}
	tableResult(c, total, items)
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
	entry, err := service.LoadPrintLog(p.PrintID)
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

// previewLine 预览响应里的单行:文本 + 是否加粗强调(与 ESC/POS 出纸的 ESC E 强调
// 行一致,前端按 font-weight 渲染;飞鹅通道保持纯文本,但预览统一展示强调效果)。
type previewLine struct {
	Text string `json:"text"`
	Bold bool   `json:"bold,omitempty"`
}

// toPreviewLines 把文本行包装为带强调标记的预览行。
func toPreviewLines(lines []string) []previewLine {
	out := make([]previewLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, previewLine{Text: l, Bold: print.BoldLine(l)})
	}
	return out
}

// PrintPreviewSample 示例模板预览:用示例订单 + 当前配置渲染「预计打印模板」。
//
// 供管理端配置页即时查看效果:不依赖真实订单与打印机,表单里改的值
// (未保存)可通过请求体覆盖,所见即所改。不出纸、不记日志。
func PrintPreviewSample(c *gin.Context) {
	var p struct {
		DocType          string  `json:"docType"`          // guest(默认)/kitchen
		PaperWidth       int     `json:"paperWidth"`       // 32(58mm)/48(80mm,默认)
		Footer           *string `json:"footer"`           // 页脚文案覆盖
		ShowSeatFee      *bool   `json:"showSeatFee"`      // 餐位费行开关覆盖
		ShowDiscount     *bool   `json:"showDiscount"`     // 优惠行开关覆盖
		KitchenShowPrice *bool   `json:"kitchenShowPrice"` // 厨房单带金额覆盖
	}
	_ = c.ShouldBindJSON(&p)
	chunks, lineWidth := print.PreviewTicketSample(p.DocType, p.PaperWidth, print.GuestTicketOverrides{
		Footer:       p.Footer,
		ShowSeatFee:  p.ShowSeatFee,
		ShowDiscount: p.ShowDiscount,
	}, p.KitchenShowPrice != nil && *p.KitchenShowPrice)
	previewChunks := make([][]previewLine, 0, len(chunks))
	for _, ck := range chunks {
		previewChunks = append(previewChunks, toPreviewLines(ck))
	}
	ok(c, gin.H{
		"chunks":    previewChunks,
		"lineWidth": lineWidth,
		"docType":   p.DocType,
	})
}

// PrintPreview 票据预览:按打印日志重放渲染,不出纸、不记日志。
//
// 与「补打」对称 —— 一条日志记录唯一确定「预览什么」,前端拿文本行按等宽字体
// 渲染即可得到与 9100 出纸 1:1 的版式(渲染发生在后端,与出纸同一份代码)。
func PrintPreview(c *gin.Context) {
	printID := 0
	if v := intArg(c.Query("printId")); v != nil {
		printID = *v
	}
	if printID == 0 {
		fail(c, "参数错误")
		return
	}
	entry, err := service.LoadPrintLog(printID)
	if err != nil {
		fail(c, "打印记录不存在")
		return
	}
	if entry.OrderID == 0 {
		fail(c, "该记录不是订单单据(如测试打印),无法预览")
		return
	}
	chunks, lineWidth, err := print.PreviewTicket(entry.PrinterID, entry.OrderID, entry.DocType)
	if err != nil {
		fail(c, "生成预览失败:"+err.Error())
		return
	}
	previewChunks := make([][]previewLine, 0, len(chunks))
	for _, ck := range chunks {
		previewChunks = append(previewChunks, toPreviewLines(ck))
	}
	ok(c, gin.H{
		// chunks 与实际入队的任务段一一对应:超长票据会拆成多段依次送出,
		// 前端逐段渲染即与出纸 1:1;每行带 bold 标记(与 ESC/POS 出纸的强调一致)。
		"chunks":      previewChunks,
		"lineWidth":   lineWidth,
		"docType":     entry.DocType,
		"orderNo":     entry.OrderNo,
		"printerName": entry.PrinterName,
		"copies":      entry.Copies,
	})
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
	if p.DocType != po.PrintDocKitchen {
		p.DocType = po.PrintDocGuest
	}
	printerID := p.PrinterID
	if printerID == 0 {
		printerType := po.PrinterTypeGuest
		if p.DocType == po.PrintDocKitchen {
			printerType = po.PrinterTypeKitchen
		}
		var err error
		printerID, err = service.FirstEnabledPrinterByType(printerType)
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
	if t == po.PrinterTypeGuest {
		return "食客小票"
	}
	return "厨房单"
}
