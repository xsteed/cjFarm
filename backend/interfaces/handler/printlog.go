package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/print"
	"dining-system/interfaces/dto"
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
	if err := print.Reprint(entry.PrinterID, entry.OrderID, entry.DocType, entry.TriggerBy, adminName(c)); err != nil {
		fail(c, "补打失败:"+err.Error())
		return
	}
	okMsg(c, "已重新发送到「"+entry.PrinterName+"」")
}

// previewLine 预览响应里的单行:文本 + 是否加粗强调(与 ESC/POS 出纸的 ESC E 重打、
// 飞鹅云出纸的 <BOLD> 标签一致,前端按 font-weight 渲染)。
type previewLine struct {
	Text string `json:"text"`
	Bold bool   `json:"bold,omitempty"`
	// QR 非空表示这一行实际出的是二维码:前端按该内容渲染真实二维码图形,
	// Text 只是占位提示。等宽文本网格画不出二维码,只能这样保住「预览即所得」。
	QR string `json:"qr,omitempty"`
	// Big 为真表示这一行按「放大居中」出纸(夹单桌号,见 print.SplitBigLine):
	// 前端用大字居中渲染,与 ESC/POS 的 GS ! 倍宽倍高、飞鹅的 <C><B> 一致。
	Big bool `json:"big,omitempty"`
}

// qrPlaceholder 预览里二维码行的占位文本。
const qrPlaceholder = "[二维码]"

// toPreviewLines 把文本行包装为带强调标记的预览行。
func toPreviewLines(lines []string) []previewLine {
	out := make([]previewLine, 0, len(lines))
	for _, l := range lines {
		if qr, ok := print.SplitQRLine(l); ok {
			out = append(out, previewLine{Text: qrPlaceholder, QR: qr})
			continue
		}
		// 放大行必须先剥掉控制前缀再下发:留着前缀前端会连控制字符一起显示。
		if big, ok := print.SplitBigLine(l); ok {
			out = append(out, previewLine{Text: big, Big: true})
			continue
		}
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
		DocType          string  `json:"docType"`          // guest(默认)/kitchen/guest_append
		PaperWidth       int     `json:"paperWidth"`       // 32(58mm)/48(80mm,默认)
		Footer           *string `json:"footer"`           // 页脚文案覆盖
		ShowDiscount     *bool   `json:"showDiscount"`     // 优惠行开关覆盖
		QR               *bool   `json:"qr"`               // 小票二维码开关覆盖
		KitchenShowPrice *bool   `json:"kitchenShowPrice"` // 厨房单带金额覆盖
		KitchenSplit     *string `json:"kitchenSplit"`     // 厨房单打印粒度覆盖(whole/item/portion)
	}
	_ = c.ShouldBindJSON(&p)
	chunks, lineWidth := print.PreviewTicketSample(p.DocType, p.PaperWidth, print.SampleOverrides{
		Footer:           p.Footer,
		ShowDiscount:     p.ShowDiscount,
		QR:               p.QR,
		KitchenShowPrice: p.KitchenShowPrice,
		KitchenSplit:     p.KitchenSplit,
	})
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
		fail(c, "该记录不是订单单据(如测试打印);测试页样式可到「打印机管理」的「票据预览」查看")
		return
	}
	chunks, lineWidth, err := print.PreviewTicket(entry.PrinterID, entry.OrderID, entry.DocType, entry.TriggerBy)
	if err != nil {
		fail(c, "生成预览失败:"+err.Error())
		return
	}
	previewChunks := make([][]previewLine, 0, len(chunks))
	for _, ck := range chunks {
		previewChunks = append(previewChunks, toPreviewLines(ck))
	}
	ok(c, gin.H{
		// chunks 与该打印机实际出纸的分段一一对应:受单条上限约束的通道
		// (代理/飞鹅)会把超长票据拆成多段依次送出,直连则是整张一次写完。
		// 每行带 bold 标记(与 ESC/POS 出纸的强调一致)。
		"chunks":      previewChunks,
		"lineWidth":   lineWidth,
		"docType":     entry.DocType,
		"orderNo":     entry.OrderNo,
		"printerName": entry.PrinterName,
		"copies":      entry.Copies,
		// 加菜小票的票据形态可以还原,但「当时新增了哪些菜」无从还原
		// (订单只增不减、日志不存渲染快照),菜品区退化为订单当前整单 ——
		// 由前端显式标注,避免商家把它当成原始加菜内容的复现。
		"approximate": entry.DocType == po.PrintDocGuestAppend,
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
	if err := print.Reprint(printerID, p.OrderID, p.DocType, po.PrintTriggerReprint, adminName(c)); err != nil {
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
