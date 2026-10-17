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

// sendAttempts 单次发送的总尝试次数(首次 + 2 次重试)。
// 网络抖动、打印机刚上电、飞鹅云瞬时故障这类问题,多给一两次机会就能救回来 ——
// 尤其「一菜一单」下一次下单要连发多张票,任何一张瞬时失败都意味着后厨少一道菜。
const sendAttempts = 3

// retryBackoff 第 attempt 次失败后的等待时长(400ms、1s)。
// 给「打印机刚上电」「云端瞬时不可用」留出恢复时间,又不至于把后台打印协程拖太久。
func retryBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 400 * time.Millisecond
	}
	return time.Second
}

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
//
//   - permError(本包判定的配置缺失/参数问题):重试不会有不同结果;
//   - 飞鹅 APIError:只有「瞬时故障」值得重试 —— 1003 打印机不在线、1004 云端添加任务失败
//     (官方文案就是「请稍后重试」)。其余(ret<0 的鉴权/参数类、1001/1002 设备绑定类、
//     1007 内容过大)重试无意义。**此前把飞鹅返回的所有 ret!=0 都当永久错误**,于是
//     1003/1004 这类一过性故障会让那一张票被直接丢掉,与「请稍后重试」的文案自相矛盾;
//   - 其余(网络/超时/data 异常等):重试。
func isRetryable(err error) bool {
	var pe *permError
	if errors.As(err, &pe) {
		return false
	}
	var fe *feie.APIError
	if errors.As(err, &fe) {
		return feieRetryableRet(fe.Ret)
	}
	return true
}

// feieRetryableRet 报告飞鹅返回码是否为「瞬时故障」(重试有机会成功)。
func feieRetryableRet(ret int) bool {
	switch ret {
	case 1003, 1004: // 打印机不在线 / 云端添加打印任务失败(请稍后重试)
		return true
	}
	return false
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
// 再把这部分菜逐道拆单。出纸 / 补打 / 预览三条路径同粒度(见 jobsForPrinter),
// 否则「预览看到的票」与「后厨手里的票」就是两回事。
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

// guestFooter 顾客小票页脚文案;留空表示不打印页脚。
// 出厂默认值由 seed 的 EnsureSettingDefaults 补齐(老库升级也会补),
// 用户在管理端清空后保存,即得到空串 → 页脚行整体省略。
func guestFooter() string { return strings.TrimSpace(service.GetSetting("print_guest_footer")) }

// guestShowDiscount 结账小票是否显示优惠行(默认显示;有优惠金额时才出现该行)。
// 点菜单不读它 —— 下单时的优惠是预估口径,优惠行只出现在结账小票上。
func guestShowDiscount() bool { return service.GetSetting("print_guest_show_discount") != "0" }

// guestPrintOnAppend 加菜后是否向小票机重出一张加菜小票(默认开)。
//
// 沿用 show_seat_fee 的「!= "0"」语义:管理端关掉时写入明确的 "0",
// 老库没有这个键时视为开启 —— 与 seed 的出厂默认一致。
func guestPrintOnAppend() bool { return service.GetSetting("print_guest_on_append") != "0" }

// guestPrintOnOrder 下单时是否向小票机出「点菜单」(默认开)。
//
// 下单即出的小票相当于点菜凭证(菜品上错、漏上当场能对),但与结账小票
// 内容高度重叠,不想要重复出纸的门店可关 —— 关闭后下单只打厨房单,
// 加菜小票(print_guest_on_append)与结账小票不受影响。
// 后付模式走 PrintOrder(厨房单+点菜单),先付模式走 PrintGuestOnOrder(只出点菜单),
// 两种模式都受本开关控制;先付模式的厨单等首次收款成功才出(见 printKitchenOnPaid)。
func guestPrintOnOrder() bool { return service.GetSetting("print_guest_on_order") != "0" }

// ============================================================================
// 打印机选取
// ============================================================================

// loadPrinters 查询启用的打印机。printerType 传 0 表示全部,否则按类型过滤(1=厨房单 2=顾客小票)。
func loadPrinters(printerType int) ([]po.Printer, error) {
	return service.ListEnabledPrinters(printerType)
}

// itemsForPrinter 按打印机的「分类分单」配置筛出它该打的菜品。
//
// 顾客小票不过滤 —— 小票必须含全部菜品,否则金额合计对不上。
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
		remoteID, raw, serr := sendViaFeie(client, p, lines)
		detail = feieRawDetail(remoteID, raw)
		return remoteID, detail, serr
	}
	detail, err = sendViaTCP(p, lines)
	if err != nil {
		return "", detail, err
	}
	return "", detail, nil
}

// feieRawDetail 台账/日志里的飞鹅返回摘要:云端单号 + 云端返回原文。
//
// 「返回原文」必须带上:排查「台账记成功、纸却没出来」时,这是唯一能确认飞鹅到底回了
// 什么的证据。此前日志只写自家拼的一句话,出了问题无从对账。
func feieRawDetail(orderID, raw string) string {
	raw = truncate(raw, 300)
	switch {
	case orderID == "" && raw == "":
		return ""
	case raw == "":
		return "飞鹅云端单号 " + orderID
	case orderID == "":
		return "飞鹅返回: " + raw
	default:
		return "飞鹅云端单号 " + orderID + ";返回: " + raw
	}
}

// sendWithRetry 发送并在「值得重试」的错误上重试,退避 400ms / 1s。
func sendWithRetry(p po.Printer, lines []string) (remoteID, detail string, err error) {
	for attempt := 1; attempt <= sendAttempts; attempt++ {
		remoteID, detail, err = sendOnce(p, lines)
		if err == nil {
			return remoteID, detail, nil
		}
		if !isRetryable(err) || attempt == sendAttempts {
			return remoteID, detail, err
		}
		time.Sleep(retryBackoff(attempt))
	}
	return remoteID, detail, err
}

// ============================================================================
// 日志
// ============================================================================

// recordLog 把一次发送结果写入打印日志(成功与失败都记)。
//
// 入参是「这张票的作业描述 + 它的渲染结果」,而不是散着传 p/o/docType/label 一堆字段:
// 台账里的票据标识、订单号、票种、份数全都来自 job,少一个都不会记错归属。
//
// lines 是该票的完整文本行,随台账存成票面快照(见 snapshot.go):此后这条记录的
// 预览与补打都是回放这张票,而不是拿现在的订单与配置重算。
//
// 票据标识(job.label,如「【厨房单】1/4」)写进独立的 label 列,不拼进 detail ——
// 「一菜一单」一次下单会写多条台账,不写第几张就分不清丢的是哪一张;而 detail
// 会被后续回执整段改写(见 UpdatePrintLogResult),拼进去的标识留不住。
//
// 打印是异步且依赖打印机在线状态的,没有这张表时「小票没出来」无法追查;
// 日志写入本身失败只记后端日志,绝不向上抛 —— 它是旁路数据。
//
// 返回台账主键(写入失败为 0):飞鹅通道的出纸确认需要在事后把「云端受理成功」
// 回写成「回执未打印」,得靠它定位到具体哪几行(见 confirmFeiePrinted)。
func recordLog(log *zap.SugaredLogger, j job, lines []string, remoteID, detail string, costMs int, sendErr error) int {
	label := j.label()
	entry := po.PrintLog{
		OrderID:     j.o.OrderID,
		OrderNo:     j.o.OrderNo,
		TableNo:     j.o.TableNo,
		TableName:   j.o.TableName,
		PrinterID:   j.p.PrinterID,
		PrinterName: j.p.PrinterName,
		PrinterType: j.p.PrinterType,
		Provider:    j.p.Provider,
		DocType:     j.docType,
		Copies:      j.p.EffectiveCopies(),
		Status:      po.PrintStatusSuccess,
		RemoteID:    remoteID,
		Label:       label,
		Detail:      truncate(detail, 480),
		Content:     EncodeTicketContent(TicketContent{Label: label, Lines: lines}),
		PaperWidth:  j.p.PaperWidth,
		TriggerBy:   j.triggerBy,
		Operator:    j.operator,
		CostMs:      costMs,
		CreateTime:  service.Now(),
	}
	if sendErr != nil {
		entry.Status = po.PrintStatusFailed
		entry.Detail = truncate(sendErr.Error(), 480)
	}
	id, err := service.InsertPrintLogReturningID(entry)
	if err != nil {
		log.Warnf("打印: 写打印日志失败: %v", err)
	}
	return id
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
	p       po.Printer
	o       po.Order
	items   []service.PrintOrderItemRow
	docType string
	title   string
	// lines 非空时 render() 直接返回它,跳过模板渲染。
	//
	// 给「按台账补打」用:一条台账存了票面快照,补打要发的就必须是存下来的那张票,
	// 而不是拿现在的订单重渲染出来的另一张(见 ReprintLog)。渲染入口保持唯独一个,
	// 发送/分段/合并推送这些通道逻辑因此完全复用。
	lines     []string
	triggerBy string
	operator  string
	// singleDish 表示这张厨房单是拆出来的夹单(只含一道菜):
	// 渲染层据此放大菜名、且不编序号/不出份数汇总(见 KitchenTicketOptions.SingleDish)。
	singleDish bool
	// lastTicket 表示这张夹单是本台打印机本批拆单的最后一张:
	// 渲染层据此在票尾打粗线,后厨凭它确认整叠夹单到此为止(见 KitchenTicketOptions.LastTicket)。
	lastTicket bool
	// shopName / showPrice 是本批票共享的模板配置快照。
	//
	// 同一次下单所有票的店名与「厨房单带金额」开关完全一致,没有理由逐票重查配置表 ——
	// dao.GetSetting 每次调用都查库(靠这一点实现「改配置即刻生效」),而一菜一单会把
	// 「一道菜一次」放大成几十次单行查询。快照在 buildJobs 里取一次即可。
	shopName  string
	showPrice bool
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

// label 日志/台账里这张票的可读标识:拆单厨房单用带序号的标题(【厨房单】1/4),
// 其余用票种名。一菜一单一次下单会产出多条日志,没有这一列在日志里根本分不清是哪一张。
// 测试页返回空串 —— 它不是某张业务单据,不需要票据标识前缀。
func (j job) label() string {
	if t := strings.TrimSpace(j.title); t != "" {
		return t
	}
	switch j.docType {
	case po.PrintDocGuest:
		// guest 按触发分形态出两种票:结账(settle)出结账小票,其余出点菜单。
		// 台账标识要与票面标题对得上,排障时按这一列就能知道出的是哪张。
		if j.triggerBy == po.PrintTriggerSettle {
			return "结账小票"
		}
		return "顾客小票"
	case po.PrintDocGuestAppend:
		return "加菜小票"
	case docTest:
		return ""
	}
	return "厨房单"
}

// toPOItems 把带分类的订单明细还原为纯持久化明细(渲染层只关心 po.OrderItem 字段)。
func toPOItems(rows []service.PrintOrderItemRow) []po.OrderItem {
	out := make([]po.OrderItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.OrderItem)
	}
	return out
}

// render 按单据类型渲染文本行;带 lines 的作业(按快照补打)直接用存下来的那几行。
func (j job) render() []string {
	if j.lines != nil {
		return j.lines
	}
	if j.docType == po.PrintDocGuest {
		// 与 label 同一判定:结账触发出结账小票,其余触发(下单/补放下单)出点菜单。
		// 快照补打走上面的 lines 直通,不受该判定影响。
		if j.triggerBy == po.PrintTriggerSettle {
			return RenderGuestTicket(j.o, toPOItems(j.items), j.p.PaperWidth)
		}
		return RenderGuestOrderTicket(j.o, toPOItems(j.items), j.p.PaperWidth)
	}
	if j.docType == po.PrintDocGuestAppend {
		return RenderGuestAppendTicket(j.o, toPOItems(j.items), j.p.PaperWidth)
	}
	return RenderKitchenTicket(j.o, toPOItems(j.items), j.p.PaperWidth, KitchenTicketOptions{
		Title:      j.title,
		ShowPrice:  j.showPrice,
		SingleDish: j.singleDish,
		LastTicket: j.lastTicket,
		ShopName:   j.shopName,
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
	logID := recordLog(log, j, lines, remoteID, detail, cost, err)

	if err != nil {
		log.Warnf("打印: 向打印机[%s](%s) 发送 %s 失败: %v%s",
			j.p.PrinterName, providerAddr(j.p), j.docType, err, feieDetailSuffix(j.p, detail))
		return err
	}
	log.Infof("打印: 已向打印机[%s](%s) 发送单据 %s(%s) [%s]%s",
		j.p.PrinterName, providerAddr(j.p), j.o.OrderNo, j.docType, j.label(), feieDetailSuffix(j.p, detail))

	// 飞鹅单张路径(补打等)同样要做出纸确认,机制见 confirmFeiePrinted。
	if j.p.IsFeie() && remoteID != "" {
		if client, cerr := NewFeieClient(); cerr == nil {
			goSafe("打印出纸确认", func() { confirmFeiePrinted(j, []string{j.label()}, remoteID, []int{logID}, client) })
		}
	}
	return nil
}

// feieDetailSuffix 日志行尾的飞鹅返回摘要。
//
// 只有飞鹅通道需要:它的 detail 是「云端单号 + 返回原文」,是排查「记成功却没出纸」的
// 唯一证据。直连/代理的 detail 就是目标地址,而日志行里已有 providerAddr,不重复。
func feieDetailSuffix(p po.Printer, detail string) string {
	if !p.IsFeie() || strings.TrimSpace(detail) == "" {
		return ""
	}
	return ";" + detail
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

// runJobRecovered 在 per-job recover 下执行单个打印作业,返回发送错误。
//
// 与 goSafe 的整批 recover 配合:单个作业 panic 被捕获后仍继续处理本批剩余作业,
// 避免一张坏单卡住同一订单的其它打印。
// 返回错误是为了让「补打」这类同步调用能把失败原因报给前端;异步调用方忽略它即可。
func runJobRecovered(j job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			j.jobLog().Errorf("打印: 单个打印作业 panic(已跳过,继续后续作业): %v\n%s", r, debug.Stack())
		}
	}()
	return run(j)
}

// runAsync 异步执行一批作业。打印失败只记日志,不影响调用方。
func runAsync(jobs []job) {
	if len(jobs) == 0 {
		return
	}
	goSafe("runAsync", func() {
		_ = dispatchJobs(jobs)
	})
}

// ============================================================================
// 作业派发
// ============================================================================

// dispatchJobs 顺序执行一批作业,并按通道决定「一次推送装几张票」;
// 返回本批第一次发送失败(全部成功时为 nil)。
//
// 飞鹅是厂商云端排队:一张票一次 Open_printMsg 时,「一菜一单」会把一次下单变成 N 次独立
// 推送,而飞鹅对「短时间内连续多条」没有任何保证(实测出现过飞鹅回 ret=0、纸却没出来),
// 且同一秒的多条请求签名完全相同。因此同一台飞鹅打印机的连续多张票在这里合并成一次推送,
// 票与票之间用 <CUT> 主动切纸 —— 纸面上仍是 N 张各自切开的票,但只发一条请求。
// 直连/本地代理每张票本就独立发送(各自带切纸),保持逐一执行。
//
// 拆单会把一批作业撑成多张票,因此「某一张失败不牵连其余张」:错误只汇总第一个,
// 其余照发 —— 补打时少一张也比一张都不出来强,且每张都有独立台账可查。
func dispatchJobs(jobs []job) error {
	var firstErr error
	for i := 0; i < len(jobs); {
		if jobs[i].p.IsFeie() {
			end := i + 1
			for end < len(jobs) && sameFeiePrinter(jobs[i], jobs[end]) {
				end++
			}
			if err := runFeieBatch(jobs[i:end]); err != nil && firstErr == nil {
				firstErr = err
			}
			i = end
			continue
		}
		if err := runJobRecovered(jobs[i]); err != nil && firstErr == nil {
			firstErr = err
		}
		i++
	}
	return firstErr
}

// sameFeiePrinter 报告两个作业是否属于「同一台飞鹅打印机」(可合并推送)。
// buildJobs 按打印机顺序连续生成作业,同一台机器的多张票因此天然相邻。
func sameFeiePrinter(a, b job) bool {
	return b.p.IsFeie() && b.p.PrinterID == a.p.PrinterID
}

// runFeieBatch 把同一台飞鹅打印机的一批票合并成尽量少的推送发送,并按「每张票一条」写打印日志。
//
// 合并的意义不只是少发请求:飞鹅的连发丢单一旦发生,代码收不到错误(飞鹅回 ret=0)、
// 也没法确认(未接状态回调),只能靠「减少推送次数」从源头规避。
// 一次推送装不下时按「票」为最小单位分箱(packFeiePush),绝不把一张票切成两半;
// 每张票仍各写一条台账,detail 里带上本次推送的云端单号与返回原文,事后可逐张对账。
func runFeieBatch(batch []job) error {
	if len(batch) == 0 {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			batch[0].jobLog().Errorf("打印: 飞鹅合并推送 panic(已跳过本批): %v\n%s", r, debug.Stack())
		}
	}()

	p := batch[0].p
	tickets := make([][]string, len(batch))
	labels := make([]string, len(batch))
	for i, j := range batch {
		tickets[i] = j.render()
		labels[i] = j.label()
	}

	var (
		pushed     int
		firstErr   error
		failDetail string // 第一次失败那次推送的返回原文
		lastDetail string // 最后一次推送的返回原文(全部成功时用)
		// 最后一次成功推送的云端单号、台账主键与票据标识:出纸确认只对成功的那次推送
		// 有意义(失败的那次云端没有单号可查),确认结果也只回写到它的台账行上。
		confirmID    string
		confirmLogs  []int
		confirmLabel []string
	)
	for start := 0; start < len(batch); {
		end, lines := packFeiePush(tickets, start)
		begin := time.Now()
		remoteID, detail, err := sendWithRetry(p, lines)
		cost := int(time.Since(begin).Milliseconds())
		if detail != "" {
			lastDetail = detail
		}
		// 一张票一条台账:合并推送不影响「哪一单打了没」的粒度,每条都带票据标识、
		// 票面快照与云端返回。
		ids := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			ids = append(ids, recordLog(batch[i].jobLog(), batch[i], tickets[i], remoteID, detail, cost, err))
		}
		// 某一箱失败不牵连后续箱:与合并前「一张票失败不影响其它票」的行为保持一致。
		if err != nil {
			if firstErr == nil {
				firstErr = err
				// 失败现场要用「失败那一次」的返回原文。lastDetail 可能还留着上一箱成功的
				// 云端单号,拿它当失败证据会误导排查(看上去像成功过才失败)。
				failDetail = detail
			}
		} else {
			pushed += end - start
			if remoteID != "" {
				confirmID = remoteID
				confirmLogs = ids
				confirmLabel = append([]string(nil), labels[start:end]...)
			}
		}
		start = end
	}

	log := batch[0].jobLog()
	if firstErr != nil {
		log.Warnf("打印: 向打印机[%s](%s) 合并推送 %s(%s) %d 张中 %d 张已送出,其余失败: %v%s",
			p.PrinterName, providerAddr(p), batch[0].o.OrderNo, batch[0].docType,
			len(batch), pushed, firstErr, feieDetailSuffix(p, failDetail))
		return firstErr
	}
	log.Infof("打印: 已向打印机[%s](%s) 合并推送 %s(%s) 共 %d 张[%s]%s",
		p.PrinterName, providerAddr(p), batch[0].o.OrderNo, batch[0].docType,
		len(batch), strings.Join(labels, "、"), feieDetailSuffix(p, lastDetail))

	// 异步回查云端状态(见 confirmFeiePrinted):受理不等于出纸,缺纸/离线的任务会被
	// 飞鹅缓存住,不回查就永远停在「成功却没出纸」。客户端在这里构造好传入,
	// 别让确认协程醒来后再去碰全局连接与配置。
	if confirmID != "" {
		if client, cerr := NewFeieClient(); cerr == nil {
			j, ids, labelsOf, order := batch[0], confirmLogs, confirmLabel, confirmID
			goSafe("打印出纸确认", func() { confirmFeiePrinted(j, labelsOf, order, ids, client) })
		}
	}
	return nil
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
// 顾客小票永远整单一张,金额合计对不上就是事故。
func buildJobs(log, agentLog *zap.SugaredLogger, printers []po.Printer, o po.Order, items []service.PrintOrderItemRow,
	docType, title, triggerBy, operator string) []job {

	jobs := []job{}
	for _, p := range printers {
		if (docType == po.PrintDocGuest || docType == po.PrintDocGuestAppend) &&
			p.PrinterType != po.PrinterTypeGuest {
			continue
		}
		if docType == po.PrintDocKitchen && p.PrinterType == po.PrinterTypeGuest {
			continue
		}
		jobs = append(jobs, jobsForPrinter(p, o, items, docType, title, triggerBy, operator, log, agentLog)...)
	}
	return jobs
}

// jobsForPrinter 单台打印机上这一次要出的全部作业(按打印粒度拆单后的每一张)。
//
// 出纸、补打、预览三条路径必须共用这一个函数:它们回答的是同一个问题
// 「这台机器这一次会吐出哪几张纸」。此前预览与补打各自构造一个整单 job,
// 开启「一菜一单 / 按份拆」的门店因此看到的是另一张票 —— 预览显示整单、
// 后厨手里是一叠夹单,「预览即所得」当场失效。
//
// 分类过滤后无菜可打时返回空(不打空白单),由调用方决定报错还是跳过。
func jobsForPrinter(p po.Printer, o po.Order, items []service.PrintOrderItemRow, docType, title, triggerBy, operator string,
	log, agentLog *zap.SugaredLogger) []job {

	sel := itemsForPrinter(p, items)
	if len(sel) == 0 {
		return nil
	}
	mode := po.KitchenSplitWhole
	if docType == po.PrintDocKitchen {
		mode = kitchenSplitMode()
	}
	// 模板配置快照:本台机器这一次要出的所有票共用一份。
	// 取在这里 ⇒ 每台打印机各取一次,而不是每张票各取一次 —— 一菜一单一次下单能出十几张,
	// 逐票重查配置表就是十几次多余的单行查询(见 job.shopName 的说明)。
	shopName, showPrice := "", false
	if docType == po.PrintDocKitchen {
		shopName = strings.TrimSpace(service.GetSetting("shop_name"))
		showPrice = kitchenShowPrice()
	}
	// 序号按「本台打印机实际要出的张数」计算:不同厨房机负责的分类不同,
	// 各机器上「第几张/共几张」必须自洽,否则后厨会以为漏打了。
	groups := kitchenTicketGroups(sel, mode)
	out := make([]job, 0, len(groups))
	for i, g := range groups {
		out = append(out, job{
			p: p, o: o, items: g,
			docType: docType, title: seqTitle(title, i, len(groups)),
			triggerBy: triggerBy, operator: operator,
			singleDish: docType == po.PrintDocKitchen && mode != po.KitchenSplitWhole,
			lastTicket: docType == po.PrintDocKitchen && mode != po.KitchenSplitWhole && i == len(groups)-1,
			shopName:   shopName, showPrice: showPrice,
			log: log, agentLog: agentLog,
		})
	}
	return out
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
	if it.Quantity <= 0 {
		// 老库/手工插入可能出现 quantity=0,直接返回空:除零会 panic,
		// 而打印链路的 recover 会把它吞成「该票静默丢失、无台账」。
		logger.Warnf("[print][告警] 打印明细数量为 %d,跳过该条", it.Quantity)
		return []service.PrintOrderItemRow{}
	}
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

// PrintOrder 后付模式下单后自动打印:厨房机出厨房单、小票机出「点菜单」。
//
// 小票机这张是点菜凭证(下单即出,金额只覆盖已点的菜),可由
// print_guest_on_order 单独关闭;加菜小票与结账小票不受它影响。
// 先付模式不下单即打整包 —— 厨房单要等收款成功,下单时走 PrintGuestOnOrder 只出点菜单。
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
		dispatchJobs(jobs)
	})
}

// PrintGuestOnOrder 先付模式下单时专用:只向小票机出「点菜单」(受 print_guest_on_order 控制)。
//
// 后付模式的下单打印走 PrintOrder(厨房单 + 点菜单一起);先付模式下厨单必须等
// 首次收款成功才出(printKitchenOnPaid),但点菜单只是点菜凭证、与后厨是否开工无关,
// 下单时照常出 —— 顾客手里先有一张纸,菜上错/漏上当场就能对。
// items 应为本次下单的菜品(未补分类也可,内部会补)。
func PrintGuestOnOrder(ctx context.Context, o po.Order, items []po.OrderItem) {
	if !printEnabled() || !guestPrintOnOrder() {
		return
	}
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe("PrintGuestOnOrder", func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(po.PrinterTypeGuest)
		if err != nil {
			log.Warnf("打印: 查询小票打印机失败: %v", err)
			return
		}
		if len(printers) == 0 {
			log.Warnf("打印: 没有启用中的小票打印机,跳过点菜单")
			return
		}
		dispatchJobs(buildJobs(log, agentLog, printers, o, attached, po.PrintDocGuest, "", po.PrintTriggerOrder, ""))
	})
}

// PrintKitchen 加菜:只向厨房机推「加菜单」。
//
// 加菜小票由 PrintGuestAppend 负责 —— 厨房单只列本次新增的菜品(后厨只补做
// 这几道),小票则重出整单最新金额;两者口径不同,不要合并。
// items 应为本次新增的菜品(不是整桌的全部菜品)。
func PrintKitchen(ctx context.Context, o po.Order, items []po.OrderItem) {
	printKitchenDoc(ctx, o, items, "PrintKitchen", "【加菜单】", po.PrintTriggerAppend)
}

// PrintKitchenNew 首次出厨:向厨房机推「【厨房单】」(trigger=order)。
//
// 先付模式下下单时不出厨单(customer.go 已跳过),统一后移到「首次收款成功」;
// 那一刻出的是该订单的第一张厨房单,整单全部菜品都是要做的菜 —— 抬头必须是
// 【厨房单】、trigger 必须落 order。此前这里复用 PrintKitchen,整单被打成
// 【加菜单】+trigger=append,后厨无从分辨是不是新单,补打/预览(kitchenTitleFor
// 按 trigger 推导抬头)也跟着错。
func PrintKitchenNew(ctx context.Context, o po.Order, items []po.OrderItem) {
	printKitchenDoc(ctx, o, items, "PrintKitchenNew", "【厨房单】", po.PrintTriggerOrder)
}

func printKitchenDoc(ctx context.Context, o po.Order, items []po.OrderItem, fn, title, trigger string) {
	if !printEnabled() {
		return
	}
	log := logger.WithContext(ctx, zap.String("orderNo", o.OrderNo))
	agentLog := logger.AgentWithContext(ctx, zap.String("orderNo", o.OrderNo))
	goSafe(fn, func() {
		attached := service.AttachItemCategories(items)
		printers, err := loadPrinters(po.PrinterTypeKitchen)
		if err != nil {
			log.Warnf("打印: 查询厨房打印机失败: %v", err)
			return
		}
		runAsync(buildJobs(log, agentLog, printers, o, attached, po.PrintDocKitchen, title, trigger, ""))
	})
}

// PrintGuestAppend 加菜后向小票机出一张「加菜小票」(仅小票机)。
//
// 只列本次新增的菜品,金额只给一行整单「当前合计」—— 客人刚加完菜,
// 关心的是加了什么、现在多少钱,不必再重放一遍此前已点的全部菜品。
// 结账时仍会出完整结账小票,优惠等整单口径的信息在那张票上呈现。
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

// PrintGuestTicket 结账后打印结账小票(仅小票机,trigger=settle)。
//
// 这里曾经复用 PrintOrder,导致结账时厨房机又被打了一张一模一样的厨房单;
// 结账单只该出给小票机,厨房不需要知道客人什么时候结的账。
// 先付模式的「首次收款成功」也走这里(见 print.printKitchenOnPaid):
// 那一刻钱已收到,这张纸就是该订单唯一的付款凭证 —— 点菜单与结账小票合一。
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
		// 未知/脏值兜底成顾客小票:它内容最完整(明细 + 金额项齐全),
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

// ticketTitleFor 重放(补打/预览)时该用的抬头。
//
// 只有厨房单有抬头:顾客小票/加菜小票的名字由 RenderGuestTicket 系列自己生成,
// 给它塞厨房单抬头会让台账标识(label)变成「【厨房单】」—— 补打一张顾客小票,
// 记录页上却显示成厨房单,排障时是灾难(此前 kitchenTitleFor 被无条件用在了
// 所有单据类型上,靠 RenderGuestTicket 忽略抬头才没打到纸上)。
func ticketTitleFor(docType, triggerBy string) string {
	if docType != po.PrintDocKitchen {
		return ""
	}
	return kitchenTitleFor(triggerBy)
}

// Reprint 人工补打:把指定订单的单据重新送到指定打印机(同步返回结果)。
//
// 与自动打印的区别:这是商家明确要求的动作,失败必须让前端看到原因,
// 所以同步执行并把错误原样返回;同时日志里 trigger_by=reprint 便于区分。
//
// 补打与出纸同粒度:厨房单按 print_kitchen_split_mode 同样拆单(见 jobsForPrinter),
// 后厨手里原本就是一叠夹单,补打吐出一张从没见过的整单票反而无从核对 ——
// 且它会与记录页的预览对不上。
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
	jobs := jobsForPrinter(p, o, items, docType, ticketTitleFor(docType, triggerBy), po.PrintTriggerReprint, operator, nil, nil)
	return dispatchJobs(jobs)
}

// ReprintLog 按一条打印台账补打:有票面快照就照发当时那张,没有才按当前数据重渲染。
//
// 为什么优先走快照:商家点「补打」要的是**刚才那张**。一条台账唯一确定的是一张纸,
// 而不是「这道菜现在的样子」—— 订单后来加过菜、店名页脚改过、纸宽改过,
// 重渲染出来的都是另一张票。走快照时补打与记录页的预览必然一致(同一份存下来的行)。
//
// 发送链完全复用:快照塞进 job.lines,分段、飞鹅合并推送、代理入队照旧。
func ReprintLog(entry po.PrintLog, operator string) error {
	snap, ok := DecodeTicketContent(entry.Content)
	if !ok {
		// 升级前的老记录(或内容损坏):回落到按当前数据重渲染,行为与历史一致。
		return Reprint(entry.PrinterID, entry.OrderID, entry.DocType, entry.TriggerBy, operator)
	}
	p, err := service.LoadPrinter(entry.PrinterID)
	if err != nil {
		return errors.New("打印机不存在")
	}
	// 纸宽以快照为准:这些行是按当时的纸宽 pad 出来的,若这台机器后来改过纸宽,
	// 新台账若记成当前值,那条记录的预览又会用错行宽。发送本身不读纸宽。
	if entry.PaperWidth > 0 {
		p.PaperWidth = entry.PaperWidth
	}
	// 订单头只为台账留痕(单号/桌台)与代理任务行;渲染不再需要它,
	// 所以订单后来被改过、加过菜,都不影响补出来的这张票。
	o, found := service.LoadOrderForPrint(entry.OrderID)
	if !found {
		return errors.New("订单不存在")
	}
	return dispatchJobs([]job{{
		p: p, o: o, lines: snap.Lines,
		docType: normalizeDocType(entry.DocType), title: snap.Label,
		triggerBy: po.PrintTriggerReprint, operator: operator,
	}})
}

// PreviewTicket 生成某订单在某台打印机上的票据文本行(预览用:按当前数据重算)。
//
// 与 Reprint 走完全相同的取数与渲染路径(同一份 jobsForPrinter + job.render),
// 保证「预览即所得」:前端按等宽字体渲染这些行,就是这几张票在 9100 热敏机上
// 吐出来的样子。只渲染,不发送、不出纸、不记日志。
//
// 「打印记录」页走的是 PreviewLog(优先回放台账快照,没有快照才落到这里);
// 本函数按「现在的订单 + 现在的配置」渲染,是配置页示例预览与老记录回落的底座。
//
// 一段 = 一张纸,顺序与实际出纸一致,来自两层切分:
//   - 单据层:厨房单按 print_kitchen_split_mode 拆成「一菜一单 / 按份拆」的若干张,
//     每张带序号标题(【厨房单】2/5)与放大的菜名 —— 与后厨实际收到的一叠一致;
//   - 通道层:单张票据再按该通道的发送上限切段(lineChunksFor),代理/飞鹅
//     受单条上限约束会把超长票据拆成多次送出;直连一次写完,不再分段。
//
// 第二个返回值是行宽(半角字符数,前端排版用)。
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
	jobs := jobsForPrinter(p, o, items, docType, ticketTitleFor(docType, triggerBy), triggerBy, "", nil, nil)
	chunks := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		lines := j.render()
		chunks = append(chunks, sliceChunks(lines, lineChunksFor(p, lines))...)
	}
	return chunks, LineWidth(p.PaperWidth), nil
}

// PreviewResult 一次票据预览的结果。
type PreviewResult struct {
	// Chunks 分段后的文本行,一段就是一张纸,顺序与实际出纸一致。
	Chunks [][]string
	// LineWidth 每行可容纳的半角字符数(58mm=32,80mm=48),前端按它画纸面网格。
	LineWidth int
	// FromSnapshot 为真:内容来自台账存的票面快照(回放,一字不差);
	// 为假:这条记录没有快照(升级前的老记录),是按当前订单与配置重渲染的(重演),
	// 管理端据此提示商家「内容可能已与当时不同」。
	FromSnapshot bool
}

// PreviewLog 按一条打印台账生成预览:有票面快照就回放,没有才按当前数据重渲染。
//
// 与 ReprintLog 对称 —— 同一条记录,预览看到什么,补打就发什么。
func PreviewLog(entry po.PrintLog) (PreviewResult, error) {
	if snap, ok := DecodeTicketContent(entry.Content); ok {
		return replaySnapshot(entry, snap), nil
	}
	chunks, lineWidth, err := PreviewTicket(entry.PrinterID, entry.OrderID, entry.DocType, entry.TriggerBy)
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Chunks: chunks, LineWidth: lineWidth}, nil
}

// replaySnapshot 回放票面快照:纸面内容一个字都不重算,只按该打印机当前走的通道
// 把长票切段(「一次请求装不下就分多次送出」是传输细节,与票上写什么无关)。
func replaySnapshot(entry po.PrintLog, snap TicketContent) PreviewResult {
	// 打印机可能已被删除,通道无从得知:退回直连语义(一次写完,只有一段),
	// 至少把票面内容完整呈现出来,而不是因为一台机器没了就看不到历史票。
	p, err := service.LoadPrinter(entry.PrinterID)
	if err != nil {
		p = po.Printer{}
	}
	width := entry.PaperWidth
	if width == 0 {
		width = p.PaperWidth // 快照列缺省(手工插入的历史行)时退回打印机配置
	}
	return PreviewResult{
		Chunks:       sliceChunks(snap.Lines, lineChunksFor(p, snap.Lines)),
		LineWidth:    LineWidth(width),
		FromSnapshot: true,
	}
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

// previewSampleCategoryNames 示例菜品对应的种子分类名(见 store.seedCategoryRows)。
// 预览时按「当前库里的实际分类 ID」回填:门店改过分类名/重建过分类后,
// 按名字解析不到了该菜就不带分类 —— 被分类分单过滤属预期行为,与真实订单一致。
var previewSampleCategoryNames = []string{"凉菜素菜", "主食", "特色农家菜"}

// previewSampleRows 示例订单的带分类明细:给三道示例菜配上当前库里的真实分类 ID,
// 让「负责分类」筛选在预览里与真实打印走同一条 itemsForPrinter 路径。
func previewSampleRows() (po.Order, []service.PrintOrderItemRow) {
	o, items := samplePreviewOrder()
	rows := make([]service.PrintOrderItemRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, service.PrintOrderItemRow{OrderItem: it})
	}
	nameToID := map[string]int{}
	if cats, err := service.ListCategories(); err == nil {
		for _, c := range cats {
			nameToID[strings.TrimSpace(c.CategoryName)] = c.CategoryID
		}
	}
	for i, name := range previewSampleCategoryNames {
		if i < len(rows) {
			rows[i].CategoryID = nameToID[name]
		}
	}
	return o, rows
}

// PreviewPrinterTicketResult 打印机票据预览的结果。
type PreviewPrinterTicketResult struct {
	// Chunks 分段后的文本行,一段就是一张纸:厨房单按打印粒度拆出的每张、
	// 以及通道发送上限切出的每段,顺序与实际出纸一致。
	Chunks [][]string
	// LineWidth 每行可容纳的半角字符数(58mm=32,80mm=48)。
	LineWidth int
	// DocType 模拟的单据类型:kitchen(厨房单)/ guest(顾客点菜单)。
	DocType string
	// SplitMode 厨房单打印粒度(whole/item/portion);guest 为空串。
	SplitMode string
}

// PreviewPrinterTicket 按某台打印机的当前设置,用示例订单模拟「真实下单时它会收到什么」。
//
// 与出纸走完全同一份代码(jobsForPrinter + job.render + lineChunksFor):
//   - 纸宽按该机配置(58/80mm)渲染;
//   - 厨房单按 print_kitchen_split_mode 拆张:整单一张 / 一菜一单 / 按份数拆,
//     每张带序号标题与放大菜名,与后厨实际收到的一叠一致;
//   - 「负责分类」筛选生效:配了分类的厨房机只预览分类匹配的菜品,
//     一道都不匹配时报错 —— 真实下单时这台机器同样不会出纸;
//   - 「厨房单显示金额」、店名等模板配置同样生效;
//   - 通道发送上限(飞鹅字节/代理字符)对超长票的分段同样模拟。
//
// 只渲染,不发送、不出纸、不记日志。
func PreviewPrinterTicket(printerID int) (PreviewPrinterTicketResult, error) {
	p, err := service.LoadPrinter(printerID)
	if err != nil {
		return PreviewPrinterTicketResult{}, errors.New("打印机不存在")
	}
	docType, title := po.PrintDocGuest, ""
	if p.PrinterType == po.PrinterTypeKitchen {
		docType, title = po.PrintDocKitchen, "【厨房单】"
	}
	o, rows := previewSampleRows()
	jobs := jobsForPrinter(p, o, rows, docType, title, po.PrintTriggerOrder, "", nil, nil)
	if len(jobs) == 0 {
		return PreviewPrinterTicketResult{}, errors.New(
			"该打印机设置了「负责分类」,示例菜品都不在其负责的分类内 —— 真实下单时这台机器同样不会出纸;请调整分类或到订单详情按单补打")
	}
	chunks := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		lines := j.render()
		chunks = append(chunks, sliceChunks(lines, lineChunksFor(p, lines))...)
	}
	split := ""
	if docType == po.PrintDocKitchen {
		split = kitchenSplitMode()
	}
	return PreviewPrinterTicketResult{
		Chunks:    chunks,
		LineWidth: LineWidth(p.PaperWidth),
		DocType:   docType,
		SplitMode: split,
	}, nil
}

// SampleOverrides 示例预览的模板配置覆盖;nil 字段表示「用当前库配置」,
// 非 nil 字段以请求值优先 —— 让管理端在保存前就能看到「表单里改的值」的效果。
type SampleOverrides struct {
	Footer       *string
	ShowDiscount *bool
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
//
// 顾客小票两种形态都可预览:docType=guest 出点菜单(未结算样例),
// docType=guest_settle 出结账小票(已结算样例),与真实出纸的两种形态一一对应。
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
		// 点菜单样例把支付信息清掉:下单那一刻订单还没结账,样张要如实反映
		// 「点菜单上没有支付方式/结账时间」,否则门店会以为出纸顺序出了问题。
		settled := docType == po.PrintDocGuestSettle
		if !settled {
			o.PayType, o.SettleTime = nil, nil
		}
		return [][]string{renderGuestTicketWith(o, items, paperWidth, cfg, settled)}, w
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
			LastTicket: len(groups) > 1 && i == len(groups)-1,
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
			// 与 splitItemPortions 同一口径:数量为 0 的历史脏数据直接跳过,避免除零 panic。
			if it.Quantity <= 0 {
				continue
			}
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
			job{p: p, docType: docTest, triggerBy: po.PrintTriggerTest})
	}
	lines := RenderTestTicket(p)
	start := time.Now()
	remoteID, detail, err := sendWithRetry(p, lines)
	cost := int(time.Since(start).Milliseconds())
	recordLog(log, job{p: p, docType: docTest, triggerBy: po.PrintTriggerTest}, lines, remoteID, detail, cost, err)
	if err != nil {
		log.Warnf("打印: 测试页发送失败 打印机[%s](%s): %v%s",
			p.PrinterName, providerAddr(p), err, feieDetailSuffix(p, detail))
		return err
	}
	log.Infof("打印: 已向打印机[%s](%s) 发送测试页%s",
		p.PrinterName, providerAddr(p), feieDetailSuffix(p, detail))
	return nil
}
