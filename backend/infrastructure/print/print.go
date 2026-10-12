// Package print 负责把订单变成打印机上的一张小票。
//
// 分成三层,互不耦合:
//   - ticket.go            渲染层: 订单 -> []string 文本行(与厂商无关)
//   - escpos.go            发送层: 文本行 -> ESC/POS 指令 -> TCP 直连打印机
//   - feie.go              发送层: 文本行 -> <BR> 拼接的 content -> 飞鹅适配器
//     (协议本身在 infrastructure/adapter/feie:签名、接口调用、响应解码)
//   - print.go             编排层: 决定「谁该打哪张单」「失败怎么办」「日志怎么记」
//
// 打印一律在后台异步执行,失败只记日志、不影响下单/结账主流程;
// 但每次发送的结果(成功或失败)都会落到 tb_print_log,商家端可查、可补打。
//
// 依赖方向:print 依赖 po + dto + service(数据访问统一收口到 service)。
package print

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"go.uber.org/zap"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/adapter/feie"
	"dining-system/infrastructure/logger"
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
// 永久性错误有两类:本包的 permError(配置缺失/参数问题),以及飞鹅适配器的
// APIError(接口返回 ret!=0,重试不会有不同结果)。
func isRetryable(err error) bool {
	var pe *permError
	if errors.As(err, &pe) {
		return false
	}
	var fe *feie.APIError
	return !errors.As(err, &fe)
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

// kitchenSplitMode 厨房单打印粒度(po.KitchenSplitWhole/Item/Portion,默认整单)。
//
// item/portion 会把一张厨房单拆成「每道菜一张」的小票,供夹在菜上/传菜对口使用;
// 分类分单(Printer.CategoryIDs)仍然生效 —— 先按分类挑出某台机器负责的菜,
// 再把这部分菜逐道拆单。补打与预览不拆,始终整单出。
//
// 非法值(含老版本残留、被手工改脏)一律按整单处理:升级后行为与旧版一致,
// 绝不会因为一个坏配置把后厨单拆成一堆没人预期的碎票。
func kitchenSplitMode() string {
	v := strings.TrimSpace(service.GetSetting("print_kitchen_split_mode"))
	for _, m := range po.KitchenSplitModes {
		if v == m {
			return v
		}
	}
	return po.KitchenSplitWhole
}

// guestFooter 食客小票页脚文案;留空表示不打印页脚。
// 出厂默认值由 seed 的 EnsureSettingDefaults 补齐(老库升级也会补),
// 用户在管理端清空后保存,即得到空串 → 页脚行整体省略。
func guestFooter() string { return strings.TrimSpace(service.GetSetting("print_guest_footer")) }

// guestShowDiscount 食客小票是否显示优惠行(默认显示;有优惠金额时才出现该行)。
func guestShowDiscount() bool { return service.GetSetting("print_guest_show_discount") != "0" }

// guestQR 食客小票页脚后是否打印二维码(默认关闭)。
//
// 默认关的原因是它需要两个外部条件同时成立:H5 地址已配置成手机能访问的真实
// 地址、且打印机支持 GS ( k 二维码指令。出厂即开会让一部分机型吐出乱码,
// 让门店自己在「系统配置 → 小票打印」里确认后再开。
func guestQR() bool { return service.GetSetting("print_guest_qr") == "1" }

// guestPrintOnAppend 加菜后是否向小票机重出一张食客小票(默认开)。
//
// 沿用 show_seat_fee 的「!= "0"」语义:管理端关掉时写入明确的 "0",
// 老库没有这个键时视为开启 —— 与 seed 的出厂默认一致。
func guestPrintOnAppend() bool { return service.GetSetting("print_guest_on_append") != "0" }

// guestPrintOnOrder 下单时是否向小票机出「点菜单」(默认开)。
//
// 下单即出的小票相当于点菜凭证(菜品上错、漏上当场能对),但与结账小票
// 内容高度重叠,不想要重复出纸的门店可关 —— 关闭后下单只打厨房单,
// 加菜小票(print_guest_on_append)与结账小票不受影响。
func guestPrintOnOrder() bool { return service.GetSetting("print_guest_on_order") != "0" }

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
func recordLog(log *zap.SugaredLogger, p po.Printer, o po.Order, docType, triggerBy, operator, remoteID, detail string, costMs int, sendErr error) {
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
		log.Warnf("打印: 写打印日志失败: %v", err)
	}
}

// truncate 按字符数截断(避免超长错误信息撑爆 VARCHAR(500))。
// 必须按 rune 截断:按字节切会把多字节 UTF-8 字符切一半,写入 utf8mb4 列时
// 轻则乱码、重则整行 INSERT 被拒(诊断信息反而丢失)。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
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
	// singleDish 表示这张厨房单是拆出来的夹单(只含一道菜):
	// 渲染层据此放大菜名、且不编序号/不出份数汇总(见 KitchenTicketOptions.SingleDish)。
	singleDish bool
	// log / agentLog 是入口处就已绑定 requestID 与订单号的 logger。
	//
	// 为什么不带 ctx 进 goroutine:打印是异步的,handler 返回后请求 ctx 随即被
	// cancel,goroutine 里再读 ctx 语义上已不属于该请求。因此在入口(仍在请求内)
	// 就把 requestID 取出来固化成字段交给 goroutine —— 值已落定,不受 ctx 生命周期影响,
	// 「小票没出来」时仍能凭响应头的 X-Request-ID 反查到这次打印。
	//
	// 两个 logger 分别对应两条输出通道:log → 主日志;agentLog → 代理专属
	// agent.log(直连打印机与代理通道的日志不该混在一起,见 logger 包注释)。
	log      *zap.SugaredLogger
	agentLog *zap.SugaredLogger
}

// jobLog 取本作业的主日志 logger;极端情况下(手工构造 job)回落到包级 logger,不丢日志。
func (j job) jobLog() *zap.SugaredLogger {
	if j.log != nil {
		return j.log
	}
	return logger.With(zap.String("orderNo", j.o.OrderNo))
}

// jobAgentLog 取本作业的代理日志 logger;同上,回落时也不丢日志。
func (j job) jobAgentLog() *zap.SugaredLogger {
	if j.agentLog != nil {
		return j.agentLog
	}
	return logger.AgentWithContext(nil, zap.String("orderNo", j.o.OrderNo))
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
	if j.docType == po.PrintDocGuestAppend {
		return RenderGuestAppendTicket(j.o, toPOItems(j.items), j.p.PaperWidth)
	}
	return RenderKitchenTicket(j.o, toPOItems(j.items), j.p.PaperWidth, KitchenTicketOptions{
		Title:      j.title,
		ShowPrice:  kitchenShowPrice(),
		SingleDish: j.singleDish,
	})
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
	log := j.jobLog()
	recordLog(log, j.p, j.o, j.docType, j.triggerBy, j.operator, remoteID, detail, cost, err)

	if err != nil {
		log.Warnf("打印: 向打印机[%s](%s) 发送 %s 失败: %v",
			j.p.PrinterName, providerAddr(j.p), j.docType, err)
		return err
	}
	log.Infof("打印: 已向打印机[%s](%s) 发送单据 %s(%s)", j.p.PrinterName, providerAddr(j.p), j.o.OrderNo, j.docType)
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
			j.jobLog().Errorf("打印: 单个打印作业 panic(已跳过,继续后续作业): %v\n%s", r, debug.Stack())
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

// buildJobs 按「单据类型 + 分类分单 + 打印粒度」为一批启用的打印机生成作业。
//
// docType 为 guest / guest_append 时只挑小票机(两者都不做分类过滤);
// kitchen 时只挑厨房机并做分类过滤。分类过滤后无菜可打的机器直接跳过
// (不打空白单)。
//
// 厨房单按 print_kitchen_split_mode 决定粒度:整单一张 / 一菜一单 / 按份数拆。
// 一旦拆成多张,标题会带上序号(【厨房单】2/5),后厨据此确认出纸是否齐全。
// 整单备注会随每张小票重复打印 —— 拆单本来就是为了逐道对菜,备注跟着菜走才有效。
// 食客小票永远整单一张,金额合计对不上就是事故。
func buildJobs(log, agentLog *zap.SugaredLogger, printers []po.Printer, o po.Order, items []service.PrintOrderItemRow,
	docType, title, triggerBy, operator string) []job {

	mode := po.KitchenSplitWhole
	if docType == po.PrintDocKitchen {
		mode = kitchenSplitMode()
	}
	jobs := []job{}
	for _, p := range printers {
		if (docType == po.PrintDocGuest || docType == po.PrintDocGuestAppend) &&
			p.PrinterType != po.PrinterTypeGuest {
			continue
		}
		if docType == po.PrintDocKitchen && p.PrinterType == po.PrinterTypeGuest {
			continue
		}
		sel := itemsForPrinter(p, items)
		if len(sel) == 0 {
			continue
		}
		// 序号按「本台打印机实际要出的张数」计算:不同厨房机负责的分类不同,
		// 各机器上「第几张/共几张」必须自洽,否则后厨会以为漏打了。
		groups := kitchenTicketGroups(sel, mode)
		for i, g := range groups {
			jobs = append(jobs, job{
				p: p, o: o, items: g,
				docType: docType, title: seqTitle(title, i, len(groups)),
				triggerBy: triggerBy, operator: operator,
				singleDish: docType == po.PrintDocKitchen && mode != po.KitchenSplitWhole,
				log:        log, agentLog: agentLog,
			})
		}
	}
	return jobs
}

// kitchenTicketGroups 把一台打印机负责的菜品切成若干张小票的内容。
//
//   - whole(默认):整份就是一张,与历史行为一致;
//   - item:每道菜一张(同一道菜点 2 份仍是一张,标 x2);
//   - portion:每份一张 —— 点 3 份出 3 张、每张 x1,便于分盘出菜。
//
// 一菜一单与按份数拆都是成熟 POS 的常规粒度,门店按出菜方式二选一即可。
func kitchenTicketGroups(items []service.PrintOrderItemRow, mode string) [][]service.PrintOrderItemRow {
	if mode != po.KitchenSplitItem && mode != po.KitchenSplitPortion {
		return [][]service.PrintOrderItemRow{items}
	}
	groups := make([][]service.PrintOrderItemRow, 0, len(items))
	for _, it := range items {
		if mode == po.KitchenSplitPortion && it.Quantity > 1 {
			for _, one := range splitItemPortions(it) {
				groups = append(groups, []service.PrintOrderItemRow{one})
			}
			continue
		}
		groups = append(groups, []service.PrintOrderItemRow{it})
	}
	return groups
}

// splitItemPortions 把一道菜按份数拆成 N 份(每份一张小票的内容)。
//
// 金额取「小计 ÷ 份数」而不是直接用单价:赠送菜的小计为 0(原价记在订单赠送额里),
// 用单价反而会在票面上露出一个没在收的金额。份数不能整除时向下取整,误差 < 1 分,
// 且只在开了「厨房单显示金额」时可见 —— 分盘出菜看的始终是菜名与数量。
func splitItemPortions(it service.PrintOrderItemRow) []service.PrintOrderItemRow {
	unit := it.Amount / int64(it.Quantity)
	out := make([]service.PrintOrderItemRow, 0, it.Quantity)
	for i := 0; i < it.Quantity; i++ {
		one := it
		one.Quantity = 1
		one.Amount = unit
		out = append(out, one)
	}
	return out
}

// seqTitle 给拆单小票的标题拼上序号(如「【厨房单】2/5」)。
//
// 只有一张时不加序号:整单场景序号恒为 1/1,纯粹浪费纸面注意力。
func seqTitle(title string, idx, total int) string {
	if total <= 1 {
		return title
	}
	if strings.TrimSpace(title) == "" {
		return fmt.Sprintf("%d/%d", idx+1, total)
	}
	return fmt.Sprintf("%s%d/%d", title, idx+1, total)
}

// ============================================================================
// 票据分段
// ============================================================================

// chunkSpan 一段票据在文本行序列中的行下标区间 [Start, End)。
type chunkSpan struct {
	Start, End int
}

// lineChunks 把渲染好的文本行按某条通道的发送上限切成若干段。
//
// 预览与实际发送必须共用这一份计算 —— 预览的全部价值就在于「这几段就是那几张纸」。
// 此前预览统一按代理通道的字符上限切段,而三条通道的实际约束各不相同,
// 于是直连机器从不分段却被预览显示成多张、飞鹅机器按字节上限切也切在别的位置。
//
// cost 给出这一行落到该通道后的长度(飞鹅要算上 <BR> 与 <QR>/<C><B>/<BOLD> 标签的膨胀),
// sepLen 是相邻两行之间的分隔符长度,max 是单段上限。
// 单行自身超限时不强拆:票据纸宽决定了它不会发生,强拆只会把一行撕成两半。
func lineChunks(lines []string, max, sepLen int, cost func(line string) int) []chunkSpan {
	out := make([]chunkSpan, 0, 1)
	start, size := 0, 0
	for i, l := range lines {
		c := cost(l)
		if size > 0 {
			c += sepLen
		}
		if size > 0 && size+c > max {
			out = append(out, chunkSpan{Start: start, End: i})
			start = i
			size = cost(l) // 新段的首行不带分隔符
			continue
		}
		size += c
	}
	out = append(out, chunkSpan{Start: start, End: len(lines)})
	return out
}

// lineChunksFor 按打印机实际走的通道选择分段规则。
//
//   - tcp   一次连接把整张票连续写完,从不做分段(预览也只有一段纸);
//   - agent 受单条任务的字符上限约束,见 agentLineChunks;
//   - feie  受单次推送的字节上限约束,见 feieLineChunks。
func lineChunksFor(p po.Printer, lines []string) []chunkSpan {
	switch {
	case p.IsFeie():
		return feieLineChunks(lines)
	case p.IsAgent():
		return agentLineChunks(lines)
	default:
		return []chunkSpan{{Start: 0, End: len(lines)}}
	}
}

// sliceChunks 按分段区间取回各段的文本行(预览取用;发送侧按同样区间拼 content)。
func sliceChunks(lines []string, spans []chunkSpan) [][]string {
	out := make([][]string, 0, len(spans))
	for _, sp := range spans {
		out = append(out, lines[sp.Start:sp.End])
	}
	return out
}

// ============================================================================
// 对外入口
// ============================================================================

// PrintOrder 下单后自动打印:厨房机出厨房单、小票机出「点菜单」。
//
// 小票机这张是点菜凭证(下单即出,金额只覆盖已点的菜),可由
// print_guest_on_order 单独关闭;加菜小票与结账小票不受它影响。
// items 应为本次下单的菜品(未补分类也可,内部会补)。
func PrintOrder(ctx context.Context, o po.Order, items []po.OrderItem) {
	if !printEnabled() {
		return
	}
	// 在入口(仍处于请求上下文内)绑定 requestID;goroutine 内只使用这个已固化的
	// logger,不再触碰可能已 cancel 的 ctx。
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe("PrintOrder", func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(0)
		if err != nil {
			log.Warnf("打印: 查询打印机失败: %v", err)
			return
		}
		if len(printers) == 0 {
			log.Warnf("打印: 没有启用中的打印机,跳过订单")
			return
		}
		jobs := buildJobs(log, agentLog, printers, o, attached, po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
		if guestPrintOnOrder() {
			jobs = append(jobs, buildJobs(log, agentLog, printers, o, attached, po.PrintDocGuest, "", po.PrintTriggerOrder, "")...)
		}
		for _, j := range jobs {
			runJobRecovered(j)
		}
	})
}

// PrintKitchen 加菜:只向厨房机推「加菜单」。
//
// 食客小票由 PrintGuestAppend 负责 —— 厨房单只列本次新增的菜品(后厨只补做
// 这几道),小票则重出整单最新金额;两者口径不同,不要合并。
// items 应为本次新增的菜品(不是整桌的全部菜品)。
func PrintKitchen(ctx context.Context, o po.Order, items []po.OrderItem) {
	if !printEnabled() {
		return
	}
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe("PrintKitchen", func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(po.PrinterTypeKitchen)
		if err != nil {
			log.Warnf("打印: 查询厨房打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(log, agentLog, printers, o, attached, po.PrintDocKitchen, "【加菜单】", po.PrintTriggerAppend, ""))
	})
}

// PrintGuestAppend 加菜后向小票机出一张「加菜小票」(仅小票机)。
//
// 只列本次新增的菜品,金额只给一行整单「当前合计」—— 客人刚加完菜,
// 关心的是加了什么、现在多少钱,不必再重放一遍此前已点的全部菜品。
// 结账时仍会出完整食客小票,优惠等整单口径的信息在那张票上呈现。
//
// items 应为本次新增的菜品(与 PrintKitchen 同一份,不是整单明细)。
// 打印开关由 print_guest_on_append 单独控制(默认开):加菜频繁的门店可关掉省纸。
//
// 补打/预览说明:该单据在日志里落 doc_type=guest_append,但补打与预览无法
// 还原「当时新增了哪些菜」(订单只增不减、日志不存渲染快照),统一按
// docType=guest 重放当前整单 —— 见 po.PrintDocGuestAppend 的注释。
func PrintGuestAppend(ctx context.Context, o po.Order, items []po.OrderItem) {
	if !printEnabled() || !guestPrintOnAppend() {
		return
	}
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe("PrintGuestAppend", func() {
		printers, err := loadPrinters(po.PrinterTypeGuest)
		if err != nil {
			log.Warnf("打印: 查询小票打印机失败: %v", err)
			return
		}
		if len(printers) == 0 {
			log.Warnf("打印: 没有启用中的小票打印机,跳过加菜小票")
			return
		}
		attached := service.AttachItemCategories(items)
		runAsync(buildJobs(log, agentLog, printers, o, attached, po.PrintDocGuestAppend, "", po.PrintTriggerAppend, ""))
	})
}

// PrintGuestTicket 结账后打印食客小票(仅小票机)。
//
// 这里曾经复用 PrintOrder,导致结账时厨房机又被打了一张一模一样的厨房单;
// 结账单只该出给小票机,厨房不需要知道客人什么时候结的账。
func PrintGuestTicket(ctx context.Context, o po.Order) {
	if !printEnabled() {
		return
	}
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe("PrintGuestTicket", func() {
		attached := service.LoadOrderItemsForPrint(o.OrderID)
		printers, err := loadPrinters(po.PrinterTypeGuest)
		if err != nil {
			log.Warnf("打印: 查询小票打印机失败: %v", err)
			return
		}
		// 没有启用中的小票机时必须留一条日志:结账后客人等着拿小票却始终不出纸,
		// 若此处静默返回,排查时既没有打印记录也没有任何线索(与 PrintOrder 同款兜底)。
		if len(printers) == 0 {
			log.Warnf("打印: 没有启用中的小票打印机,跳过结账小票")
			return
		}
		runAsync(buildJobs(log, agentLog, printers, o, attached, po.PrintDocGuest, "", po.PrintTriggerSettle, ""))
	})
}

// normalizeDocType 单据类型归一化:只放行三种已知单据,其余一律回落 guest。
//
// guest_append(加菜小票)不再被抹成 guest。重放时必须走 RenderGuestAppendTicket
// 才能还原那张票的形态(标题「【加菜小票】」、金额只有一行「当前合计」);
// 抹成 guest 会让预览/补打吐出一张标题与金额口径都变了的另一张票,与纪录不符。
// 代价是「当时新增了哪些菜」无从还原(订单只增不减、日志不存渲染快照),
// 菜品区退化为当前整单 —— 由管理端在预览里显式说明,见 handler.PrintPreview。
func normalizeDocType(docType string) string {
	switch docType {
	case po.PrintDocKitchen, po.PrintDocGuestAppend:
		return docType
	default:
		// 未知/脏值兜底成食客小票:它内容最完整(明细 + 金额项齐全),
		// 兜底成厨房单会让重出来的票上没有金额。
		return po.PrintDocGuest
	}
}

// kitchenTitleFor 补打/预览时厨房单的抬头。
//
// 加菜触发的那张厨房单出纸时打的是「【加菜单】」(见 PrintKitchen),后厨据此知道
// 只需补做这几道;一律写死「【厨房单】」会让重出来的票被当成一张全新整单,
// 已经上过的菜可能被重做一遍。
func kitchenTitleFor(triggerBy string) string {
	if triggerBy == po.PrintTriggerAppend {
		return "【加菜单】"
	}
	return "【厨房单】"
}

// Reprint 人工补打:把指定订单的单据重新送到指定打印机(同步返回结果)。
//
// 与自动打印的区别:这是商家明确要求的动作,失败必须让前端看到原因,
// 所以同步执行并把错误原样返回;同时日志里 trigger_by=reprint 便于区分。
// 补打始终整单一张,不受 print_kitchen_split_mode 拆单影响 —— 逐道菜
// 补打无法从界面表达,商家要的也是核对全单。
//
// triggerBy 是原纪录的触发场景,只用于推导厨房单抬头(【厨房单】/【加菜单】)。
func Reprint(printerID, orderID int, docType, triggerBy, operator string) error {
	docType = normalizeDocType(docType)
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
	j := job{p: p, o: o, items: sel, docType: docType, title: kitchenTitleFor(triggerBy),
		triggerBy: po.PrintTriggerReprint, operator: operator}
	return run(j)
}

// PreviewTicket 生成某订单在某台打印机上的票据文本行(预览用)。
//
// 与 Reprint 走完全相同的取数与渲染路径(同一份 job.render),保证「预览即所得」:
// 前端按等宽字体渲染这些行,就是这张票在 9100 热敏机上吐出来的样子。
// 只渲染,不发送、不出纸、不记日志。
//
// 分段按「该打印机实际走的通道」计算(lineChunksFor):代理/飞鹅通道受单条
// 上限约束会把超长票据拆成多次送出,预览走同一份规则;直连通道一次写完,
// 预览也只返回一段。第二个返回值是行宽(半角字符数,前端排版用)。
func PreviewTicket(printerID, orderID int, docType, triggerBy string) ([][]string, int, error) {
	docType = normalizeDocType(docType)
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
	j := job{p: p, o: o, items: sel, docType: docType, title: kitchenTitleFor(triggerBy)}
	lines := j.render()
	return sliceChunks(lines, lineChunksFor(p, lines)), LineWidth(p.PaperWidth), nil
}

// PreviewTestTicket 预览某台打印机的测试页(不出纸)。
//
// 与 SendTestPrint 走同一个 RenderTestTicket,只是不发送:新装一台机器或想核对
// 通道参数时先看预览,确认无误再决定要不要真出一张纸。此前只有「测试打印」,
// 每核一次版式就白扔一张纸。
func PreviewTestTicket(printerID int) ([][]string, int, error) {
	p, err := service.LoadPrinter(printerID)
	if err != nil {
		return nil, 0, errors.New("打印机不存在")
	}
	lines := RenderTestTicket(p)
	return sliceChunks(lines, lineChunksFor(p, lines)), LineWidth(p.PaperWidth), nil
}

// SampleOverrides 示例预览的模板配置覆盖;nil 字段表示「用当前库配置」,
// 非 nil 字段以请求值优先 —— 让管理端在保存前就能看到「表单里改的值」的效果。
type SampleOverrides struct {
	Footer       *string
	ShowDiscount *bool
	QR           *bool
	// KitchenShowPrice 厨房单是否带单价与金额;nil 表示读当前配置。
	KitchenShowPrice *bool
	// KitchenSplit 厨房单的打印粒度(whole/item/portion);nil 表示读当前配置。
	// item/portion 下按「一段一张纸」返回多段,与实际出纸张数一致。
	KitchenSplit *string
}

// PreviewTicketSample 用示例订单渲染一版「预计打印模板」。
//
// 不依赖任何真实订单与打印机:管理端配置页即时查看模板效果(所见即所改,
// 表单值通过 ov 注入,未保存也能预览)。示例金额自洽(菜品+茶位费-优惠=合计)。
// paperWidth 取 32(58mm)/48(80mm),其余值按 48。
func PreviewTicketSample(docType string, paperWidth int, ov SampleOverrides) ([][]string, int) {
	if paperWidth != 32 {
		paperWidth = 48
	}
	w := LineWidth(paperWidth)
	o, items := samplePreviewOrder()
	switch docType {
	case po.PrintDocKitchen:
		return kitchenSampleChunks(o, items, paperWidth, ov), w
	case po.PrintDocGuestAppend:
		// 加菜小票示例:只列「本次新增」的部分菜品 + 一行整单当前合计。
		// 真实场景里 items 是本次新增的菜,示例挑前两道充当新增部分。
		return [][]string{RenderGuestAppendTicket(o, items[:2], paperWidth)}, w
	default:
		cfg := currentGuestTicketConfig()
		if ov.Footer != nil {
			cfg.footer = strings.TrimSpace(*ov.Footer)
		}
		if ov.ShowDiscount != nil {
			cfg.showDiscount = *ov.ShowDiscount
		}
		if ov.QR != nil {
			cfg.qr = *ov.QR
		}
		return [][]string{renderGuestTicketWith(o, items, paperWidth, cfg)}, w
	}
}

// kitchenSampleChunks 按厨房单打印粒度渲染示例:item/portion 下一段就是一张纸。
//
// 只看一张整单预览判断不出「后厨到底收几张纸、每张上面是什么」—— 而这是
// print_kitchen_split_mode 唯一要回答的问题,这里必须让它看得见。
func kitchenSampleChunks(o po.Order, items []po.OrderItem, paperWidth int, ov SampleOverrides) [][]string {
	showPrice := false
	if ov.KitchenShowPrice != nil {
		showPrice = *ov.KitchenShowPrice
	}
	mode := kitchenSplitMode()
	if ov.KitchenSplit != nil {
		if v := strings.TrimSpace(*ov.KitchenSplit); v != "" {
			for _, m := range po.KitchenSplitModes {
				if v == m {
					mode = m
					break
				}
			}
		}
	}
	groups := sampleDishGroups(items, mode)
	out := make([][]string, 0, len(groups))
	for i, g := range groups {
		out = append(out, RenderKitchenTicket(o, g, paperWidth, KitchenTicketOptions{
			Title:      seqTitle("【厨房单】", i, len(groups)),
			ShowPrice:  showPrice,
			SingleDish: len(groups) > 1,
		}))
	}
	return out
}

// sampleDishGroups 按粒度把示例菜品切成若干张小票的内容(与 kitchenTicketGroups
// 同一套切法:item 一菜一单、portion 一份一张,whole 整单一张)。
func sampleDishGroups(items []po.OrderItem, mode string) [][]po.OrderItem {
	if mode == po.KitchenSplitItem || mode == po.KitchenSplitPortion {
		groups := make([][]po.OrderItem, 0, len(items))
		for _, it := range items {
			n := 1
			if mode == po.KitchenSplitPortion {
				n = it.Quantity
			}
			for i := 0; i < n; i++ {
				one := it
				one.Quantity = 1
				one.Amount = it.Amount / int64(it.Quantity)
				groups = append(groups, []po.OrderItem{one})
			}
		}
		return groups
	}
	return [][]po.OrderItem{items}
}

// samplePreviewOrder 构造示例订单:金额自洽,菜名贴近真实门店,便于看排版效果。
//
// 带上支付方式与结账时间,是为了让「示例预览」能看到结账小票的那几行结算信息
// —— 下单即出的小票这几行为空,只看下单模板会误以为小票上没有支付方式。
func samplePreviewOrder() (po.Order, []po.OrderItem) {
	payType := "微信"
	settleTime := service.Now()
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
		PayType:        &payType,
		SettleTime:     &settleTime,
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
func SendTestPrint(ctx context.Context, p po.Printer) error {
	log := logger.WithContext(ctx, zap.String("printer", p.PrinterName))
	if p.IsAgent() {
		return enqueueAgentTicket(
			logger.AgentWithContext(ctx, zap.String("printer", p.PrinterName)),
			p, RenderTestTicket(p), docTest, po.Order{}, po.PrintTriggerTest, "")
	}
	lines := RenderTestTicket(p)
	start := time.Now()
	remoteID, detail, err := sendWithRetry(p, lines)
	cost := int(time.Since(start).Milliseconds())
	recordLog(log, p, po.Order{}, docTest, po.PrintTriggerTest, "", remoteID, detail, cost, err)
	return err
}
