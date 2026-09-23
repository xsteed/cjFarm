// Package print 负责把订单变成打印机上的一张小票。
//
// 分成三层,互不耦合:
//   - ticket.go  渲染层: 订单 -> []string 文本行(与厂商无关)
//   - escpos.go  发送层: 文本行 -> ESC/POS 指令 -> TCP 直连打印机
//   - feie.go    发送层: 文本行 -> <BR> 拼接的 content -> 飞鹅云 HTTP 接口
//   - print.go   编排层: 决定「谁该打哪张单」「失败怎么办」「日志怎么记」
//
// 打印一律在后台异步执行,失败只记日志、不影响下单/结账主流程;
// 但每次发送的结果(成功或失败)都会落到 tb_print_log,商家端可查、可补打。
//
// 依赖方向:print 依赖 po + dto + service(数据访问统一收口到 service)。
package print

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"dining-system/infra/logger"
	"dining-system/internal/po"
	"dining-system/internal/service"
)

// sendAttempts 单次发送的总尝试次数(首次 + 1 次重试)。
// 网络抖动/打印机刚上电这类瞬时问题,重试一次就能救回来;
// 配置类错误(SN 填错、账号没配)会返回永久错误,不重试。
const sendAttempts = 2

// 测试单据类型(仅用于日志分类,不是业务单据)。
const docTest = "test"

// permError 标记「重试也没有意义」的错误(参数/权限/配置类)。
type permError struct{ msg string }

func (e *permError) Error() string { return e.msg }

// newPermError 构造永久性错误。
func newPermError(format string, a ...interface{}) error {
	return &permError{msg: fmt.Sprintf(format, a...)}
}

// isRetryable 判断错误是否值得重试。
func isRetryable(err error) bool {
	var pe *permError
	return !errors.As(err, &pe)
}

// ============================================================================
// 配置读取
// ============================================================================

// printEnabled 打印总开关。与 handler.settingFlag 同一套语义:只有明确的 "1" 才开启,
// 空值/脏值一律关闭——此前用「!= "0"」会把被清空的键也当作开启,与管理端
// 开关的显示状态相反。出厂默认 "1" 由 EnsureSettingDefaults 每次启动补齐,
// 正常部署不受影响。
func printEnabled() bool { return service.GetSetting("print_enabled") == "1" }

// kitchenShowPrice 厨房单是否带单价与金额(默认不带)。
func kitchenShowPrice() bool { return service.GetSetting("print_kitchen_show_price") == "1" }

// guestFooter 食客小票页脚文案;留空表示不打印页脚。
// 出厂默认值由 seed 的 EnsureSettingDefaults 补齐(老库升级也会补),
// 用户在管理端清空后保存,即得到空串 → 页脚行整体省略。
func guestFooter() string { return strings.TrimSpace(service.GetSetting("print_guest_footer")) }

// guestShowSeatFee 食客小票是否显示餐位费行(默认显示,对账透明)。
func guestShowSeatFee() bool { return service.GetSetting("print_guest_show_seat_fee") != "0" }

// guestShowDiscount 食客小票是否显示优惠行(默认显示;有优惠金额时才出现该行)。
func guestShowDiscount() bool { return service.GetSetting("print_guest_show_discount") != "0" }

// ============================================================================
// 打印机选取
// ============================================================================

// loadPrinters 查询启用的打印机。printerType 传 0 表示全部,否则按类型过滤(1=厨房单 2=食客小票)。
func loadPrinters(printerType int) ([]po.Printer, error) {
	return service.ListEnabledPrinters(printerType)
}

// itemsForPrinter 按打印机的「分类分单」配置筛出它该打的菜品。
//
// 食客小票不过滤 —— 小票必须含全部菜品,否则金额合计对不上。
// 未配置 category_ids 的厨房机也不过滤(传统的一台机器打全单)。
// 配了分类却一道菜都没匹配上时返回空,调用方据此跳过该机器:
// 否则凉菜机会打出一张只有抬头、没有菜品的空白单。
func itemsForPrinter(p po.Printer, items []service.PrintOrderItemRow) []service.PrintOrderItemRow {
	cats := service.ParseIDList(p.CategoryIDs)
	if p.PrinterType == po.PrinterTypeGuest || len(cats) == 0 {
		return items
	}
	want := map[int]bool{}
	for _, c := range cats {
		want[c] = true
	}
	out := make([]service.PrintOrderItemRow, 0, len(items))
	for _, it := range items {
		if want[it.CategoryID] {
			out = append(out, it)
		}
	}
	return out
}

// ============================================================================
// 发送
// ============================================================================

// sendOnce 按厂商把文本行送出去。detail 是写进日志的补充信息。
func sendOnce(p po.Printer, lines []string) (remoteID, detail string, err error) {
	if p.IsFeie() {
		client, cerr := NewFeieClient()
		if cerr != nil {
			return "", "", &permError{msg: cerr.Error()}
		}
		remoteID, err = sendViaFeie(client, p, lines)
		if err != nil {
			return remoteID, "", err
		}
		return remoteID, "飞鹅云端单号 " + remoteID, nil
	}
	detail, err = sendViaTCP(p, lines)
	if err != nil {
		return "", detail, err
	}
	return "", detail, nil
}

// sendWithRetry 发送并在「值得重试」的错误上重试一次。
func sendWithRetry(p po.Printer, lines []string) (remoteID, detail string, err error) {
	for attempt := 1; attempt <= sendAttempts; attempt++ {
		remoteID, detail, err = sendOnce(p, lines)
		if err == nil {
			return remoteID, detail, nil
		}
		if !isRetryable(err) || attempt == sendAttempts {
			return remoteID, detail, err
		}
		time.Sleep(400 * time.Millisecond)
	}
	return remoteID, detail, err
}

// ============================================================================
// 日志
// ============================================================================

// recordLog 把一次发送结果写入打印日志(成功与失败都记)。
//
// 打印是异步且依赖打印机在线状态的,没有这张表时「小票没出来」无法追查;
// 日志写入本身失败只记后端日志,绝不向上抛 —— 它是旁路数据。
func recordLog(p po.Printer, o po.Order, docType, triggerBy, operator, remoteID, detail string, costMs int, sendErr error) {
	entry := po.PrintLog{
		OrderID:     o.OrderID,
		OrderNo:     o.OrderNo,
		TableNo:     o.TableNo,
		TableName:   o.TableName,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		Provider:    p.Provider,
		DocType:     docType,
		Copies:      p.EffectiveCopies(),
		Status:      po.PrintStatusSuccess,
		RemoteID:    remoteID,
		Detail:      detail,
		TriggerBy:   triggerBy,
		Operator:    operator,
		CostMs:      costMs,
		CreateTime:  service.Now(),
	}
	if sendErr != nil {
		entry.Status = po.PrintStatusFailed
		entry.Detail = truncate(sendErr.Error(), 480)
	}
	if err := service.InsertPrintLog(entry); err != nil {
		logger.Warnf("打印: 写打印日志失败: %v", err)
	}
}

// truncate 按字符数截断(避免超长错误信息撑爆 VARCHAR(500))。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ============================================================================
// 作业
// ============================================================================

// job 一次「向单台打印机送一张单据」的完整描述。
type job struct {
	p         po.Printer
	o         po.Order
	items     []service.PrintOrderItemRow
	docType   string
	title     string
	triggerBy string
	operator  string
}

// toPOItems 把带分类的订单明细还原为纯持久化明细(渲染层只关心 po.OrderItem 字段)。
func toPOItems(rows []service.PrintOrderItemRow) []po.OrderItem {
	out := make([]po.OrderItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.OrderItem)
	}
	return out
}

// render 按单据类型渲染文本行。
func (j job) render() []string {
	if j.docType == po.PrintDocGuest {
		return RenderGuestTicket(j.o, toPOItems(j.items), j.p.PaperWidth)
	}
	return RenderKitchenTicket(j.o, toPOItems(j.items), j.p.PaperWidth, kitchenShowPrice(), j.title)
}

// run 渲染 + 发送 + 记日志,返回发送错误(异步调用方通常会忽略)。
//
// agent 通道走单独的入队路径:它不做网络发送,只把票据写进任务队列等门店代理来取,
// 因此日志也要等代理回执后才回写结果(见 agent.go 的 enqueueAgentJob)。
func run(j job) error {
	if j.p.IsAgent() {
		return enqueueAgentJob(j)
	}
	lines := j.render()
	start := time.Now()
	remoteID, detail, err := sendWithRetry(j.p, lines)
	cost := int(time.Since(start).Milliseconds())
	recordLog(j.p, j.o, j.docType, j.triggerBy, j.operator, remoteID, detail, cost, err)

	if err != nil {
		logger.Warnf("打印: 向打印机[%s](%s) 发送 %s 失败: %v",
			j.p.PrinterName, providerAddr(j.p), j.docType, err)
		return err
	}
	logger.Infof("打印: 已向打印机[%s](%s) 发送单据 %s(%s)", j.p.PrinterName, providerAddr(j.p), j.o.OrderNo, j.docType)
	return nil
}

// providerAddr 日志用的打印机定位串。
func providerAddr(p po.Printer) string {
	if p.IsFeie() {
		return "飞鹅SN:" + p.FeieSN
	}
	if p.IsAgent() {
		return "本地代理→" + addrOf(p.IP, effectivePort(p.Port))
	}
	return addrOf(p.IP, p.Port)
}

// goSafe 以带 panic recover 的方式启动后台 goroutine。
//
// 打印/清理都是旁路任务,任何一次渲染或数据异常都不该把整个进程带崩;
// recover 后记 error 日志(含 panic 值与堆栈),既保留现场又不影响主流程。
func goSafe(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("打印: 后台任务 %s panic: %v\n%s", name, r, debug.Stack())
			}
		}()
		fn()
	}()
}

// runJobRecovered 在 per-job recover 下执行单个打印作业。
//
// 与 goSafe 的整批 recover 配合:单个作业 panic 被捕获后仍继续处理本批剩余作业,
// 避免一张坏单卡住同一订单的其它打印。
func runJobRecovered(j job) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("打印: 单个打印作业 panic(已跳过,继续后续作业): %v\n%s", r, debug.Stack())
		}
	}()
	_ = run(j)
}

// runAsync 异步执行一批作业。打印失败只记日志,不影响调用方。
func runAsync(jobs []job) {
	if len(jobs) == 0 {
		return
	}
	goSafe("runAsync", func() {
		for _, j := range jobs {
			runJobRecovered(j)
		}
	})
}

// buildJobs 按「单据类型 + 分类分单」为一批启用的打印机生成作业。
//
// docType 为 guest 时只挑小票机;kitchen 时只挑厨房机并做分类过滤。
// 分类过滤后无菜可打的机器直接跳过(不打空白单)。
func buildJobs(printers []po.Printer, o po.Order, items []service.PrintOrderItemRow,
	docType, title, triggerBy, operator string) []job {

	jobs := []job{}
	for _, p := range printers {
		if docType == po.PrintDocGuest && p.PrinterType != po.PrinterTypeGuest {
			continue
		}
		if docType == po.PrintDocKitchen && p.PrinterType == po.PrinterTypeGuest {
			continue
		}
		sel := itemsForPrinter(p, items)
		if len(sel) == 0 {
			continue
		}
		jobs = append(jobs, job{
			p: p, o: o, items: sel,
			docType: docType, title: title,
			triggerBy: triggerBy, operator: operator,
		})
	}
	return jobs
}

// ============================================================================
// 对外入口
// ============================================================================

// PrintOrder 下单后自动打印:厨房机出厨房单、小票机出食客小票。
// items 应为本次下单的菜品(未补分类也可,内部会补)。
func PrintOrder(o po.Order, items []po.OrderItem) {
	if !printEnabled() {
		return
	}
	goSafe("PrintOrder", func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(0)
		if err != nil {
			logger.Warnf("打印: 查询打印机失败: %v", err)
			return
		}
		if len(printers) == 0 {
			logger.Warnf("打印: 没有启用中的打印机,跳过订单 %s", o.OrderNo)
			return
		}
		jobs := buildJobs(printers, o, attached, po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
		jobs = append(jobs, buildJobs(printers, o, attached, po.PrintDocGuest, "", po.PrintTriggerOrder, "")...)
		for _, j := range jobs {
			runJobRecovered(j)
		}
	})
}

// PrintKitchen 加菜:只向厨房机推「加菜单」。
//
// 食客小票不在这里打 —— 加菜后金额会变,小票统一留到结账时出一张准确的。
// items 应为本次新增的菜品(不是整桌的全部菜品)。
func PrintKitchen(o po.Order, items []po.OrderItem) {
	if !printEnabled() {
		return
	}
	goSafe("PrintKitchen", func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(po.PrinterTypeKitchen)
		if err != nil {
			logger.Warnf("打印: 查询厨房打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(printers, o, attached, po.PrintDocKitchen, "【加菜单】", po.PrintTriggerAppend, ""))
	})
}

// PrintGuestTicket 结账后打印食客小票(仅小票机)。
//
// 这里曾经复用 PrintOrder,导致结账时厨房机又被打了一张一模一样的厨房单;
// 结账单只该出给小票机,厨房不需要知道客人什么时候结的账。
func PrintGuestTicket(o po.Order) {
	if !printEnabled() {
		return
	}
	goSafe("PrintGuestTicket", func() {
		attached := service.LoadOrderItemsForPrint(o.OrderID)
		printers, err := loadPrinters(po.PrinterTypeGuest)
		if err != nil {
			logger.Warnf("打印: 查询小票打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(printers, o, attached, po.PrintDocGuest, "", po.PrintTriggerSettle, ""))
	})
}

// Reprint 人工补打:把指定订单的单据重新送到指定打印机(同步返回结果)。
//
// 与自动打印的区别:这是商家明确要求的动作,失败必须让前端看到原因,
// 所以同步执行并把错误原样返回;同时日志里 trigger_by=reprint 便于区分。
func Reprint(printerID, orderID int, docType, operator string) error {
	if docType != po.PrintDocKitchen {
		docType = po.PrintDocGuest
	}
	p, err := service.LoadPrinter(printerID)
	if err != nil {
		return errors.New("打印机不存在")
	}
	o, items, err := service.LoadOrderAndItemsForPrint(orderID)
	if err != nil {
		return errors.New("订单不存在")
	}
	sel := itemsForPrinter(p, items)
	if len(sel) == 0 {
		if docType == po.PrintDocKitchen && len(service.ParseIDList(p.CategoryIDs)) > 0 {
			return errors.New("该打印机只负责部分菜品分类,本单没有它负责的菜")
		}
		return errors.New("订单没有可打印的菜品")
	}
	title := "【厨房单】"
	j := job{p: p, o: o, items: sel, docType: docType, title: title,
		triggerBy: po.PrintTriggerReprint, operator: operator}
	return run(j)
}

// PreviewTicket 生成某订单在某台打印机上的票据文本行(预览用)。
//
// 与 Reprint 走完全相同的取数与渲染路径(同一份 job.render),保证「预览即所得」:
// 前端按等宽字体渲染这些行,就是这张票在 9100 热敏机上吐出来的样子。
// 只渲染,不发送、不出纸、不记日志。
//
// 返回按实际入队规则(splitJobPayload)切好的段:超长票据在实际打印时会拆成多条
// 任务依次送出,预览按同样规则分段返回,前端逐段渲染即与出纸 1:1。
// 第二个返回值是行宽(半角字符数,前端排版用)。
func PreviewTicket(printerID, orderID int, docType string) ([][]string, int, error) {
	if docType != po.PrintDocKitchen {
		docType = po.PrintDocGuest
	}
	p, err := service.LoadPrinter(printerID)
	if err != nil {
		return nil, 0, errors.New("打印机不存在")
	}
	o, items, err := service.LoadOrderAndItemsForPrint(orderID)
	if err != nil {
		return nil, 0, errors.New("订单不存在")
	}
	sel := itemsForPrinter(p, items)
	if len(sel) == 0 {
		if docType == po.PrintDocKitchen && len(service.ParseIDList(p.CategoryIDs)) > 0 {
			return nil, 0, errors.New("该打印机只负责部分菜品分类,本单没有它负责的菜")
		}
		return nil, 0, errors.New("订单没有可打印的菜品")
	}
	j := job{p: p, o: o, items: sel, docType: docType, title: "【厨房单】"}
	chunks := make([][]string, 0, 1)
	for _, c := range splitJobPayload(j.render()) {
		chunks = append(chunks, strings.Split(c, "\n"))
	}
	return chunks, LineWidth(p.PaperWidth), nil
}

// GuestTicketOverrides 示例预览的模板配置覆盖;nil 字段表示「用当前库配置」,
// 非 nil 字段以请求值优先 —— 让管理端在保存前就能看到「表单里改的值」的效果。
type GuestTicketOverrides struct {
	Footer       *string
	ShowSeatFee  *bool
	ShowDiscount *bool
}

// PreviewTicketSample 用示例订单渲染一版「预计打印模板」。
//
// 不依赖任何真实订单与打印机:管理端配置页即时查看模板效果(所见即所改,
// 表单值通过 overrides 注入,未保存也能预览)。示例金额自洽(菜品+餐位费-优惠=合计)。
// kitchenShowPrice 仅厨房单生效;paperWidth 取 32(58mm)/48(80mm),其余值按 48。
func PreviewTicketSample(docType string, paperWidth int, ov GuestTicketOverrides, kitchenShowPrice bool) ([][]string, int) {
	if paperWidth != 32 {
		paperWidth = 48
	}
	o, items := samplePreviewOrder()
	if docType == po.PrintDocKitchen {
		lines := RenderKitchenTicket(o, items, paperWidth, kitchenShowPrice, "【厨房单】")
		return [][]string{lines}, LineWidth(paperWidth)
	}
	cfg := currentGuestTicketConfig()
	if ov.Footer != nil {
		cfg.footer = strings.TrimSpace(*ov.Footer)
	}
	if ov.ShowSeatFee != nil {
		cfg.showSeatFee = *ov.ShowSeatFee
	}
	if ov.ShowDiscount != nil {
		cfg.showDiscount = *ov.ShowDiscount
	}
	return [][]string{renderGuestTicketWith(o, items, paperWidth, cfg)}, LineWidth(paperWidth)
}

// samplePreviewOrder 构造示例订单:金额自洽,菜名贴近真实门店,便于看排版效果。
func samplePreviewOrder() (po.Order, []po.OrderItem) {
	o := po.Order{
		OrderNo:        "D20260922SAMPLE001",
		TableNo:        "08",
		TableName:      "大厅08桌",
		PersonCount:    3,
		DishAmount:     8600,
		SeatFee:        1800,
		DiscountAmount: 600,
		TotalAmount:    9800,
		OrderRemark:    "少辣",
		CreateTime:     service.Now(),
		SettleType:     "normal",
	}
	items := []po.OrderItem{
		{DishName: "凉拌青瓜", SpecName: "份", Quantity: 2, Price: 2800, Amount: 5600, Remark: "加辣"},
		{DishName: "海鲜炒饭", Quantity: 1, Price: 1000, Amount: 1000},
		{DishName: "柴火土鸡", SpecName: "半只", Quantity: 1, Price: 2000, Amount: 2000},
	}
	return o, items
}

// ProbePrinter 测试打印机连通性,不落地任何纸:
//   - tcp   只探一次 TCP 连接(不写数据,避免又吐一张测试页);
//   - feie  查云端在线状态与设备类型,顺带验证账号/SN 是否配对;
//   - agent 云端够不到门店内网,只能看「代理是否还在轮询」与队列积压。
func ProbePrinter(p po.Printer) (string, error) {
	if p.IsAgent() {
		return probeAgent(p)
	}
	if p.IsFeie() {
		client, err := NewFeieClient()
		if err != nil {
			return "", err
		}
		sn := strings.TrimSpace(p.FeieSN)
		if sn == "" {
			return "", errors.New("未填写飞鹅打印机编号(SN)")
		}
		status, err := client.PrinterStatus(sn)
		if err != nil {
			return "", err
		}
		return "飞鹅云状态:" + status, nil
	}
	if err := ProbeTCP(p); err != nil {
		return "", err
	}
	return "TCP 连接正常(" + addrOf(p.IP, effectivePort(p.Port)) + ")", nil
}

// effectivePort 端口兜底 9100。
func effectivePort(port int) int {
	if port <= 0 {
		return 9100
	}
	return port
}

// SendTestPrint 发送测试打印页(同步,失败原因原样返回给前端)。
// agent 通道下「同步」的含义变为「已同步入队」——代理在线时几乎立刻出纸,
// 离线则排队等它上线,与前端的交互仍是「点一下、马上有结果」。
func SendTestPrint(p po.Printer) error {
	if p.IsAgent() {
		return enqueueAgentTicket(p, RenderTestTicket(p), docTest, po.Order{}, po.PrintTriggerTest, "")
	}
	lines := RenderTestTicket(p)
	start := time.Now()
	remoteID, detail, err := sendWithRetry(p, lines)
	cost := int(time.Since(start).Milliseconds())
	recordLog(p, po.Order{}, docTest, po.PrintTriggerTest, "", remoteID, detail, cost, err)
	return err
}
