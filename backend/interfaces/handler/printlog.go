package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
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
	status, ok := intArg(c, "status")
	if !ok {
		return
	}
	q.Status = status
	if v := c.Query("docType"); v != "" {
		q.DocType = v
	}
	if v := c.Query("provider"); v != "" {
		q.Provider = v
	}
	printerID, ok := intArg(c, "printerId")
	if !ok {
		return
	}
	q.PrinterID = printerID
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
// 走 PrintLog 而不是让前端自己拼参数,是因为日志里同时存了打印机、单据类型与票面快照,
// 一条记录就能唯一确定「重打哪一张纸」,前端不必回查订单明细。
// 带快照的记录发出的就是当时那张(内容、抬头、纸宽都不重算),与同一条记录的预览一致;
// 升级前没有快照的老记录回落到按当前订单重渲染。
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
	if err := print.ReprintLog(entry, adminName(c)); err != nil {
		fail(c, "补打失败:"+err.Error())
		return
	}
	okMsg(c, "已重新发送到「"+entry.PrinterName+"」")
}

// PrintLogRefresh 刷新失败记录:按云端单号回查飞鹅该次推送的最新打印结果。
//
// 「失败」的飞鹅记录多半来自出纸确认超时:推送当时打印机离线/缺纸,任务被缓存
// 在飞鹅云端;打印机恢复后飞鹅会自动补吐缓存任务 —— 纸可能已经出来了,但台账
// 还停在失败。本接口按记录里的云端单号(Open_queryOrderState)追问一次最新结果:
// 确认已打印则把台账回写为成功;仍为未打印则保持失败,是否补打由商户决定。
// 刷新是纯回查:不会向打印机重推任务,不存在打出重复小票的风险。
func PrintLogRefresh(c *gin.Context) {
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
	if entry.Provider != po.PrinterProviderFeie {
		fail(c, "仅飞鹅云通道的记录支持刷新")
		return
	}
	if entry.RemoteID == "" {
		fail(c, "该记录没有云端单号(推送当时未受理),无法回查")
		return
	}
	if entry.Status == po.PrintStatusSuccess {
		fail(c, "该记录已是成功状态,无需刷新")
		return
	}
	printer, perr := service.LoadPrinter(entry.PrinterID)
	if perr != nil {
		fail(c, "找不到该记录对应的打印机,无法回查")
		return
	}
	sn := po.NormalizeFeieSN(printer.FeieSN)
	if sn == "" {
		fail(c, "该打印机未填写飞鹅编号(SN),无法回查")
		return
	}
	client, cerr := print.NewFeieClient()
	if cerr != nil {
		fail(c, cerr.Error())
		return
	}
	printed, _, qerr := client.QueryOrderState(sn, entry.RemoteID)
	if qerr != nil {
		fail(c, "查询飞鹅云端状态失败:"+qerr.Error())
		return
	}
	if !printed {
		okMsg(c, "飞鹅回执:仍未打印(任务可能仍在云端队列,稍后可再刷新;长时间未出纸可人工补打)")
		return
	}
	detail := "人工刷新确认:飞鹅回执已打印(云端单号 " + entry.RemoteID + ")"
	if err := service.ConfirmPrintLogFeie(entry.PrintID, po.PrintStatusSuccess, detail); err != nil {
		fail(c, "回写打印记录失败:"+err.Error())
		return
	}
	okMsg(c, "飞鹅回执:已打印,已回写为成功")
}

// previewLine 预览响应里的单行:文本 + 是否加粗强调(与 ESC/POS 出纸的 ESC E 重打、
// 飞鹅云出纸的 <BOLD> 标签一致,前端按 font-weight 渲染)。
type previewLine struct {
	Text string `json:"text"`
	Bold bool   `json:"bold,omitempty"`
	// QR 非空表示这一行实际出的是二维码:前端按该内容渲染真实二维码图形,
	// Text 只是占位提示。等宽文本网格画不出二维码,只能这样保住「预览即所得」。
	QR string `json:"qr,omitempty"`
	// Big 为真表示这一行按「放大」出纸:飞鹅 <C><B>/<B>、ESC/POS GS ! 倍宽倍高,
	// 前端用大字渲染(见 print.SplitBigLine / SplitBigLeftLine)。
	Big bool `json:"big,omitempty"`
	// BigLeft 为真时那个放大行是左对齐的(整单菜品清单,见 print.SplitBigLeftLine):
	// 前端不能再居中渲染 —— 出纸上编号在行首对齐,预览居中了就失去「预览即所得」。
	BigLeft bool `json:"bigLeft,omitempty"`
	// Center 为真表示这一行按「机器居中」出纸(单据名副标题,见 print.SplitCenterLine):
	// 前端按 CSS 居中渲染,与放大行同一套观感,不再按字符网格左对齐。
	Center bool `json:"center,omitempty"`
}

// qrPlaceholder 预览里二维码行的占位文本。
const qrPlaceholder = "[二维码]"

// toPreviewLines 把文本行包装为带强调标记的预览行。
func toPreviewLines(lines []string) []previewLine {
	out := make([]previewLine, 0, len(lines))
	for _, l := range lines {
		// 切纸控制行只在飞鹅「同机多张合并推送」时产生,预览的是单张票、不会走到;
		// 防御性跳过,免得控制字符被当正文渲染出来。
		if print.SplitCutLine(l) {
			continue
		}
		if qr, ok := print.SplitQRLine(l); ok {
			out = append(out, previewLine{Text: qrPlaceholder, QR: qr})
			continue
		}
		// 放大行必须先剥掉控制前缀再下发:留着前缀前端会连控制字符一起显示。
		if big, ok := print.SplitBigLine(l); ok {
			out = append(out, previewLine{Text: big, Big: true})
			continue
		}
		if bigL, ok := print.SplitBigLeftLine(l); ok {
			out = append(out, previewLine{Text: bigL, Big: true, BigLeft: true})
			continue
		}
		if ctr, ok := print.SplitCenterLine(l); ok {
			// 是否加粗沿用形态反推(【加菜小票】是标题要加粗,【顾客小票】副标题不加粗)。
			_, bold := print.BoldText(ctr)
			out = append(out, previewLine{Text: ctr, Bold: bold, Center: true})
			continue
		}
		// 加粗判定走唯一入口:显式标记优先(备注折行的续行靠它),形态反推兜底。
		text, bold := print.BoldText(l)
		out = append(out, previewLine{Text: text, Bold: bold})
	}
	return out
}

// PrintPreviewSample 示例模板预览:用示例订单 + 当前配置渲染「预计打印模板」。
//
// 供管理端配置页即时查看效果:不依赖真实订单与打印机,表单里改的值
// (未保存)可通过请求体覆盖,所见即所改。不出纸、不记日志。
func PrintPreviewSample(c *gin.Context) {
	var p struct {
		DocType          string  `json:"docType"`          // guest(默认,点菜单)/guest_settle(结账小票)/kitchen/guest_append
		PaperWidth       int     `json:"paperWidth"`       // 32(58mm)/48(80mm,默认)
		Footer           *string `json:"footer"`           // 页脚文案覆盖
		ShowDiscount     *bool   `json:"showDiscount"`     // 优惠行开关覆盖
		LargeFont        *bool   `json:"largeFont"`        // 顾客小票整张放大覆盖
		KitchenShowPrice *bool   `json:"kitchenShowPrice"` // 厨房单带金额覆盖
		KitchenSplit     *string `json:"kitchenSplit"`     // 厨房单打印粒度覆盖(whole/item/portion)
	}
	// 绑定失败(如请求体为空/非法 JSON)时刻意忽略:本接口是「所见即所改」的
	// 覆盖预览,调用方可能不带体,此时全部覆盖项回落到默认值即正确行为。
	// 记一条 debug 日志,便于排查「改了没生效是不是请求体根本没发对」。
	if err := c.ShouldBindJSON(&p); err != nil {
		logger.CtxDebugf(c.Request.Context(), "[print] 示例预览未收到覆盖项(按默认渲染): %v", err)
	}
	chunks, lineWidth := print.PreviewTicketSample(p.DocType, p.PaperWidth, print.SampleOverrides{
		Footer:           p.Footer,
		ShowDiscount:     p.ShowDiscount,
		LargeFont:        p.LargeFont,
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

// PrintPreview 票据预览:优先回放台账里存的票面快照,没有快照才按当前数据重渲染。
// 不出纸、不记日志。
//
// 与「补打」对称 —— 一条日志记录唯一确定「预览哪一张纸」,前端拿文本行按等宽字体
// 渲染即可得到与 9100 出纸 1:1 的版式(渲染发生在后端,与出纸同一份代码)。
func PrintPreview(c *gin.Context) {
	printID, valid := intArg(c, "printId")
	if !valid {
		return
	}
	if printID == nil || *printID <= 0 {
		fail(c, "参数错误")
		return
	}
	entry, err := service.LoadPrintLog(*printID)
	if err != nil {
		fail(c, "打印记录不存在")
		return
	}
	if entry.OrderID == 0 {
		fail(c, "该记录不是订单单据(如测试打印);测试页样式可到「打印机管理」的「票据预览」查看")
		return
	}
	res, err := print.PreviewLog(entry)
	if err != nil {
		fail(c, "生成预览失败:"+err.Error())
		return
	}
	previewChunks := make([][]previewLine, 0, len(res.Chunks))
	for _, ck := range res.Chunks {
		previewChunks = append(previewChunks, toPreviewLines(ck))
	}
	ok(c, gin.H{
		// chunks 与该打印机实际出纸的分段一一对应:受单条上限约束的通道
		// (代理/飞鹅)会把超长票据拆成多段依次送出,直连则是整张一次写完。
		// 每行带 bold 标记(与 ESC/POS 出纸的强调一致)。
		"chunks":    previewChunks,
		"lineWidth": res.LineWidth,
		"docType":   entry.DocType,
		// triggerBy 供前端把 guest 的两种形态分开显示(顾客小票/结账小票),
		// 与 printDocLabel 同一套口径。
		"triggerBy":   entry.TriggerBy,
		"orderNo":     entry.OrderNo,
		"printerName": entry.PrinterName,
		"copies":      entry.Copies,
		// fromSnapshot:内容来自这张票当时存下来的快照(回放,一字不差),
		// 前端据此说明「与实际小票 1:1」。
		"fromSnapshot": res.FromSnapshot,
		// approximate:没有快照(升级前的老记录),按当前订单与配置重渲染 ——
		// 内容可能与当时不同,由前端显式标注,别让商家当成原始内容的复现。
		// 加菜小票尤其明显:当时新增了哪些菜无从追溯,菜品区退化为订单当前整单。
		"approximate": !res.FromSnapshot &&
			entry.DocType == po.PrintDocGuestAppend,
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
	// 补打同样说明商家已经处理过这张单(通常就是从详情里点的),「新」角标随之消解。
	service.MarkOrderViewed(p.OrderID)
	okMsg(c, "补打指令已发送")
}

// printerTypeLabel 打印机类型中文名(错误提示用)。
func printerTypeLabel(t int) string {
	if t == po.PrinterTypeGuest {
		return "顾客小票"
	}
	return "厨房单"
}
