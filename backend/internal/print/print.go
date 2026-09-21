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
// 依赖方向:print 依赖 model + store。
package print

import (
	"dining-system/internal/logger"
	"errors"
	"fmt"
	"strings"
	"time"

	"dining-system/internal/model"
	"dining-system/internal/store"
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

// printEnabled 打印总开关。缺省视为开启(缺配置项时不该静默不打印)。
func printEnabled() bool { return store.GetCfg("print_enabled") != "0" }

// kitchenShowPrice 厨房单是否带单价与金额(默认不带)。
func kitchenShowPrice() bool { return store.GetCfg("print_kitchen_show_price") == "1" }

// ============================================================================
// 打印机选取
// ============================================================================

// loadPrinters 查询启用的打印机。printerType 传 0 表示全部,否则按类型过滤(1=厨房单 2=食客小票)。
func loadPrinters(printerType int) ([]model.Printer, error) {
	query := `SELECT printer_id, printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time
		FROM tb_printer WHERE del_flag='0' AND status=1`
	args := []interface{}{}
	if printerType != 0 {
		query += ` AND printer_type=?`
		args = append(args, printerType)
	}
	rows, err := store.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var printers []model.Printer
	for rows.Next() {
		var p model.Printer
		if err := rows.Scan(&p.PrinterID, &p.PrinterName, &p.PrinterType, &p.Provider, &p.IP, &p.Port,
			&p.FeieSN, &p.PaperWidth, &p.Copies, &p.CategoryIDs, &p.Status, &p.DelFlag,
			&p.CreateTime, &p.UpdateTime); err != nil {
			continue
		}
		p.CategoryIDList = store.ParseIDList(p.CategoryIDs)
		printers = append(printers, p)
	}
	return printers, nil
}

// itemsForPrinter 按打印机的「分类分单」配置筛出它该打的菜品。
//
// 食客小票不过滤 —— 小票必须含全部菜品,否则金额合计对不上。
// 未配置 category_ids 的厨房机也不过滤(传统的一台机器打全单)。
// 配了分类却一道菜都没匹配上时返回空,调用方据此跳过该机器:
// 否则凉菜机会打出一张只有抬头、没有菜品的空白单。
func itemsForPrinter(p model.Printer, items []model.OrderItem) []model.OrderItem {
	if p.PrinterType == model.PrinterTypeGuest || len(p.CategoryIDList) == 0 {
		return items
	}
	want := map[int]bool{}
	for _, c := range p.CategoryIDList {
		want[c] = true
	}
	out := make([]model.OrderItem, 0, len(items))
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
func sendOnce(p model.Printer, lines []string) (remoteID, detail string, err error) {
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
func sendWithRetry(p model.Printer, lines []string) (remoteID, detail string, err error) {
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
func recordLog(p model.Printer, o model.Order, docType, triggerBy, operator, remoteID, detail string, costMs int, sendErr error) {
	entry := model.PrintLog{
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
		Status:      model.PrintStatusSuccess,
		RemoteID:    remoteID,
		Detail:      detail,
		TriggerBy:   triggerBy,
		Operator:    operator,
		CostMs:      costMs,
		CreateTime:  store.Now(),
	}
	if sendErr != nil {
		entry.Status = model.PrintStatusFailed
		entry.Detail = truncate(sendErr.Error(), 480)
	}
	if err := store.InsertPrintLog(entry); err != nil {
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
	p         model.Printer
	o         model.Order
	items     []model.OrderItem
	docType   string
	title     string
	triggerBy string
	operator  string
}

// render 按单据类型渲染文本行。
func (j job) render() []string {
	if j.docType == model.PrintDocGuest {
		return RenderGuestTicket(j.o, j.p.PaperWidth)
	}
	return RenderKitchenTicket(j.o, j.items, j.p.PaperWidth, kitchenShowPrice(), j.title)
}

// run 渲染 + 发送 + 记日志,返回发送错误(异步调用方通常会忽略)。
func run(j job) error {
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
func providerAddr(p model.Printer) string {
	if p.IsFeie() {
		return "飞鹅SN:" + p.FeieSN
	}
	return addrOf(p.IP, p.Port)
}

// runAsync 异步执行一批作业。打印失败只记日志,不影响调用方。
func runAsync(jobs []job) {
	if len(jobs) == 0 {
		return
	}
	go func() {
		for _, j := range jobs {
			_ = run(j)
		}
	}()
}

// buildJobs 按「单据类型 + 分类分单」为一批启用的打印机生成作业。
//
// docType 为 guest 时只挑小票机;kitchen 时只挑厨房机并做分类过滤。
// 分类过滤后无菜可打的机器直接跳过(不打空白单)。
func buildJobs(printers []model.Printer, o model.Order, items []model.OrderItem,
	docType, title, triggerBy, operator string) []job {

	jobs := []job{}
	for _, p := range printers {
		if docType == model.PrintDocGuest && p.PrinterType != model.PrinterTypeGuest {
			continue
		}
		if docType == model.PrintDocKitchen && p.PrinterType == model.PrinterTypeGuest {
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
// o.Items 应为本次下单的菜品(未补分类也可,内部会补)。
func PrintOrder(o model.Order) {
	if !printEnabled() {
		return
	}
	go func() {
		o.Items = store.AttachItemCategories(o.Items)
		printers, err := loadPrinters(0)
		if err != nil {
			logger.Warnf("打印: 查询打印机失败: %v", err)
			return
		}
		if len(printers) == 0 {
			logger.Warnf("打印: 没有启用中的打印机,跳过订单 %s", o.OrderNo)
			return
		}
		jobs := buildJobs(printers, o, o.Items, model.PrintDocKitchen, "【厨房单】", model.PrintTriggerOrder, "")
		jobs = append(jobs, buildJobs(printers, o, o.Items, model.PrintDocGuest, "", model.PrintTriggerOrder, "")...)
		for _, j := range jobs {
			_ = run(j)
		}
	}()
}

// PrintKitchen 加菜:只向厨房机推「加菜单」。
//
// 食客小票不在这里打 —— 加菜后金额会变,小票统一留到结账时出一张准确的。
// o.Items 应为本次新增的菜品(不是整桌的全部菜品)。
func PrintKitchen(o model.Order) {
	if !printEnabled() {
		return
	}
	go func() {
		items := store.AttachItemCategories(o.Items)
		printers, err := loadPrinters(model.PrinterTypeKitchen)
		if err != nil {
			logger.Warnf("打印: 查询厨房打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(printers, o, items, model.PrintDocKitchen, "【加菜单】", model.PrintTriggerAppend, ""))
	}()
}

// PrintGuestTicket 结账后打印食客小票(仅小票机)。
//
// 这里曾经复用 PrintOrder,导致结账时厨房机又被打了一张一模一样的厨房单;
// 结账单只该出给小票机,厨房不需要知道客人什么时候结的账。
func PrintGuestTicket(o model.Order) {
	if !printEnabled() {
		return
	}
	go func() {
		items := store.LoadOrderItems(o.OrderID)
		o.Items = store.AttachItemCategories(items)
		printers, err := loadPrinters(model.PrinterTypeGuest)
		if err != nil {
			logger.Warnf("打印: 查询小票打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(printers, o, o.Items, model.PrintDocGuest, "", model.PrintTriggerSettle, ""))
	}()
}

// Reprint 人工补打:把指定订单的单据重新送到指定打印机(同步返回结果)。
//
// 与自动打印的区别:这是商家明确要求的动作,失败必须让前端看到原因,
// 所以同步执行并把错误原样返回;同时日志里 trigger_by=reprint 便于区分。
func Reprint(printerID, orderID int, docType, operator string) error {
	if docType != model.PrintDocKitchen {
		docType = model.PrintDocGuest
	}
	p, err := store.LoadPrinter(printerID)
	if err != nil {
		return errors.New("打印机不存在")
	}
	o, err := loadOrderForPrint(orderID)
	if err != nil {
		return errors.New("订单不存在")
	}
	items := store.AttachItemCategories(o.Items)
	sel := itemsForPrinter(p, items)
	if len(sel) == 0 {
		if docType == model.PrintDocKitchen && len(p.CategoryIDList) > 0 {
			return errors.New("该打印机只负责部分菜品分类,本单没有它负责的菜")
		}
		return errors.New("订单没有可打印的菜品")
	}
	title := "【厨房单】"
	j := job{p: p, o: o, items: sel, docType: docType, title: title,
		triggerBy: model.PrintTriggerReprint, operator: operator}
	return run(j)
}

// ProbePrinter 测试打印机连通性,不落地任何纸:
//   - tcp  只探一次 TCP 连接(不写数据,避免又吐一张测试页);
//   - feie 查云端在线状态与设备类型,顺带验证账号/SN 是否配对。
func ProbePrinter(p model.Printer) (string, error) {
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
func SendTestPrint(p model.Printer) error {
	lines := RenderTestTicket(p)
	start := time.Now()
	remoteID, detail, err := sendWithRetry(p, lines)
	cost := int(time.Since(start).Milliseconds())
	recordLog(p, model.Order{}, docTest, model.PrintTriggerTest, "", remoteID, detail, cost, err)
	return err
}

// loadOrderForPrint 读取订单及其明细(补打历史订单用,不校验订单状态)。
func loadOrderForPrint(orderID int) (model.Order, error) {
	o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_id=?`, orderID))
	if err != nil {
		return o, err
	}
	o.Items = store.LoadOrderItems(orderID)
	return o, nil
}
