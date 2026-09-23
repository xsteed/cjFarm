package main

// ============================================================================
// CUPS 打印通道(--print-via cups / auto)
//
// 为什么需要它:macOS 15(Sequoia)起引入的「本地网络隐私」会拦截**既不来自 launchd
// daemon、也不来自 root、也不来自终端**的局域网出站连接。由 LaunchAgent 托管的代理
// 正落在被拦的那一类里:connect() 直接返回 EHOSTUNREACH,Go 里显示为
// "connect: no route to host" —— 与「打印机没开机 / 不在同一网段」表现完全一样,
// 日志上无法区分,极难排查(详见 docs/print-agent.md 的 macOS 一节)。
//
// CUPS 通道从根上绕开这道限制:
//   - 本进程只把字节交给本机 cupsd(127.0.0.1:631)。回环接口不支持广播,按 Apple
//     TN3179 对「本地网络」的定义,127.0.0.1 不是本地网络地址,不需要任何权限;
//   - 真正连 192.168.x.x:9100 的是 cupsd —— 一个由 launchd 启动的 root daemon。
//     TN3179 明确写着「launchd daemon」与「以 root 运行的程序」自动获得本地网络
//     访问权,无需授权。
//
// 代价:CUPS 通道回读不到 DLE EOT 实时状态(那条链路在 cupsd 手里,我们只有一个
// 作业提交口),因此缺纸/盖板开只能靠出纸结果间接判断。这是刻意取舍 —— 能出纸优先。
//
// 队列来源两条,先显式后自动:
//   1. 显式映射 PRINT_AGENT_CUPS_QUEUE,形如 `192.168.1.133=厨房打印机`,
//      或多个用逗号分隔;只写队列名时作为所有打印机的默认队列;
//   2. 自动发现:解析 `lpstat -v` 的 socket:// 设备 URI,建立「ip:port → 队列」映射。
//      门店只要在「系统设置 → 打印机」里添加过这台网络打印机(用「IP」方式、协议选
//      「行式打印机监控程序 - LPD」或 Socket/HP JetDirect),代理就能自己找到它,
//      不必再配任何东西。
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 打印通道取值(PRINT_AGENT_PRINT_VIA)。
const (
	// channelTCP 直连打印机 IP:9100 —— 默认,与所有既有部署行为一致。
	channelTCP = "tcp"
	// channelCUPS 一律交给本机 CUPS 队列(CUPS 连不到就报错,不退回直连)。
	channelCUPS = "cups"
	// channelAuto 先直连,直连失败且能找到对应 CUPS 队列时自动改投 CUPS。
	channelAuto = "auto"
)

// defaultCUPSQueueKey 显式映射里「只写队列名」时使用的通配键(对所有打印机生效)。
const defaultCUPSQueueKey = "*"

// cupsQueueTTL 自动发现的队列映射缓存时长。队列增删是低频操作,几分钟的滞后
// 无伤大雅;而每条任务都 exec 一次 lpstat 则纯属浪费。
const cupsQueueTTL = 5 * time.Minute

// cupsExecTimeout 单次 lpstat / lp 调用的兜底超时。lp 在 cupsd 无响应时会自己卡住,
// 这里再兜一层,避免代理主循环被一个挂死的子进程拖停。
const cupsExecTimeout = 30 * time.Second

// ============================================================================
// 外部命令注入点(与 print.go 的 dialTCP 同一套路:测试可替换,生产走真实命令)
// ============================================================================

// cupsListQueues 返回 `lpstat -v` 的输出。
var cupsListQueues = func() ([]byte, error) {
	return runExternal(cupsListArgs())
}

// cupsListPrinters 返回 `system_profiler -json SPPrintersDataType` 的输出。
var cupsListPrinters = func() ([]byte, error) {
	return runExternal(cupsPrintersArgs())
}

// cupsSubmit 提交一次打印作业,返回 lp 命令的输出。
var cupsSubmit = func(argv []string, data []byte) ([]byte, error) {
	// lp 只吃文件名,/dev/stdin 在部分 macOS 版本上不可用,故落一个临时文件再删。
	f, err := os.CreateTemp("", "print-agent-cups-*.bin")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return runExternal(append(argv, tmp))
}

// cupsListArgs 返回列出队列的 argv。
func cupsListArgs() []string {
	return []string{"lpstat", "-v"}
}

// cupsPrintersArgs 返回「列出打印机」的 argv。
//
// 为什么用 system_profiler 而不是再调一次 lpstat:它的 JSON 键名是固定英文标识符
// (见系统里 SPPrintersReporter.spreporter 的 SPProperties.plist —— 设备 URI 是 uri,
// 打印机名是 _name),完全不受系统语言影响,而 macOS 的 lpstat 恰恰是随语言变化的
// (见 parseCUPSQueues 的说明)。代价是它比 lpstat 慢,故只作补充来源。
func cupsPrintersArgs() []string {
	return []string{"system_profiler", "-json", "SPPrintersDataType"}
}

// cupsSubmitArgs 返回提交作业的 argv。
//
// -o raw 是整个通道的关键:它让 CUPS 跳过一切过滤器/驱动,把字节原样投给设备 ——
// 与直连 IP:9100 的语义完全一致(票据的 ESC/POS 编码在云端完成)。
// -o job-sheets=none 关掉可能被队列默认值打开的封面页,否则每单都会多吐一张纸。
func cupsSubmitArgs(queue string) []string {
	return []string{"lp", "-d", queue, "-o", "raw", "-o", "job-sheets=none"}
}

// runExternal 执行外部命令并带超时。
//
// 用 CommandContext 而不是「另起 goroutine + 超时后 Kill」:后者会在
// cmd.Process 上制造数据竞争(写它的 Start 在 goroutine 里,读它的 Kill 在主 goroutine),
// 而 `go test -race` 之外很难发现。
//
// LC_ALL=C 保留着:虽然实测 macOS 的 lpstat 走 CoreFoundation 本地化、无视这些环境
// 变量(所以解析另有兜底,见 parseCUPSQueues),但对 Linux 上的 CUPS 它是有效的。
func runExternal(argv []string) ([]byte, error) {
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("未找到命令 %s(请确认系统打印服务可用)", argv[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), cupsExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s 执行超时(超过 %s)", argv[0], cupsExecTimeout)
	}
	return out, err
}

// ============================================================================
// 通道设置(启动时由 configurePrintChannel 写入,之后只读)
// ============================================================================

// printChannelTarget 全局打印通道设置。
//
// 用包级变量而不是给 printJob 加参数:printJob 的签名被 protocol_test.go 里
// 一整套「云后端 ⇄ 代理 ⇄ 打印机」契约测试固定住了,加参数会把这些测试全部翻新;
// 而通道是进程级一次性决定,与 cfg.verbose 同类,沿用 setVerbose 的做法。
var printChannelTarget = struct {
	via         string            // channelTCP / channelCUPS / channelAuto
	queueByAddr map[string]string // 显式 ip:port → 队列名;defaultCUPSQueueKey 为通配
}{via: channelTCP}

// configurePrintChannel 在进程启动时写入通道设置。
func configurePrintChannel(cfg config) {
	printChannelTarget.via = cfg.printVia
	printChannelTarget.queueByAddr = parseCUPSQueueConfig(cfg.cupsQueue)
}

// ============================================================================
// 显式映射:PRINT_AGENT_CUPS_QUEUE
// ============================================================================

// parseCUPSQueueConfig 解析显式队列映射。
//
// 每项两种写法:
//
//	192.168.1.133        → 通配队列:所有打印机都投给它(单打印机门店最省事);
//	192.168.1.133=厨房机  → 指定映射;左侧可带端口(192.168.1.133:9100=厨房机),
//	                       不带端口按 9100 归一化,好让 `lpstat -v` 的键能对上。
func parseCUPSQueueConfig(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := map[string]string{}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\t'
	})
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		left, right, ok := strings.Cut(f, "=")
		if !ok {
			out[defaultCUPSQueueKey] = f
			continue
		}
		queue := strings.TrimSpace(right)
		if queue == "" {
			continue
		}
		out[normalizeCUPSAddr(strings.TrimSpace(left))] = queue
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeCUPSAddr 把 ip 或 ip:port 归一化成 ip:port(缺端口按 9100)。
// 无法解析的输入原样返回,以免把用户写错的键悄悄吞掉(查不到队列时会报错提示)。
func normalizeCUPSAddr(s string) string {
	if s == "" {
		return s
	}
	if _, _, err := net.SplitHostPort(s); err == nil {
		return s
	}
	if net.ParseIP(s) != nil {
		return net.JoinHostPort(s, strconv.Itoa(defaultProbePort))
	}
	return s
}

// ============================================================================
// 自动发现:lpstat -v
// ============================================================================

var cupsQueueCache struct {
	sync.Mutex
	byAddr  map[string]string
	fetched time.Time
}

// invalidateCUPSQueueCache 清掉队列发现缓存。
//
// 刚建完队列必须清:发现结果带 5 分钟 TTL,不清的话「建好队列 → 立刻核对」会读到
// 建之前那份空结果,门店看到的是「建了却还是说没找到」。
func invalidateCUPSQueueCache() {
	cupsQueueCache.Lock()
	cupsQueueCache.byAddr, cupsQueueCache.fetched = nil, time.Time{}
	cupsQueueCache.Unlock()
}

// parseCUPSQueues 解析 `lpstat -v` 的输出为「ip:port → 队列名」。
//
// 只认 socket:// 设备:CUPS 里它就是 AppSocket/HP JetDirect,与直连 IP:9100 是同一条
// 协议。ipp:// 与 lpd:// 走的是另外的协议,lp -o raw 投过去要么被拒、要么打出乱码,
// 与其让它「看起来配好了却出不了纸」,不如在这里就不认,让上层明确报「找不到队列」。
//
// 英文环境下的输出样例:
//
//	device for Kitchen: socket://192.168.1.133:9100
//	device for Office: ipp://192.168.1.10:631/ipp/print
//
// 为什么要额外写一条「本地化兜底」:macOS 上的 lpstat 走 CoreFoundation 本地化,
// **完全无视 LC_ALL / LANG / LANGUAGE**(实测中文系统上设了这些仍然输出
// 「未添加目的位置。」),所以英文前缀 "device for " 在中文门店机器上一行都匹配不到,
// 而那恰好是本功能最需要工作的场景。中文环境下的实际输出形如:
//
//	用于CjfarmKitchen的设备：socket://192.168.1.133:9100
//
// 兜底只依赖语言无关的部分:设备 URI(socket:// 原样输出),以及「标签词几乎都是
// 非 ASCII、队列名通常是 ASCII」这一规律(见 guessLocalizedQueue)。**注意标签词与
// 队列名之间没有空格** —— 按空格分词在中文下会整行变成一个词,永远认不出队列名。
func parseCUPSQueues(out string) map[string]string {
	type entry struct{ addr, queue string }

	res := map[string]string{}
	var localized []entry

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		idx := strings.Index(line, "socket://")
		if idx < 0 {
			continue
		}
		uri := line[idx:]
		if f := strings.IndexAny(uri, " \t"); f >= 0 {
			uri = uri[:f]
		}
		// socket://192.168.1.133        → 补 9100
		// socket://192.168.1.133:9100   → 原样
		host := strings.TrimSuffix(strings.TrimPrefix(uri, "socket://"), "/")
		addr := normalizeCUPSAddr(host)

		// 首选:英文固定前缀。队列名里可能带冒号,故取第一个 ": " 作分隔。
		if rest, ok := strings.CutPrefix(line, "device for "); ok {
			if queue, _, ok := strings.Cut(rest, ": "); ok {
				if q := strings.TrimSpace(queue); q != "" {
					res[addr] = q
					continue
				}
			}
		}
		if q := guessLocalizedQueue(line[:idx]); q != "" {
			localized = append(localized, entry{addr: addr, queue: q})
		}
	}

	// 合并兜底结果,但不覆盖英文前缀认出来的 —— 一行只会走其中一条路,所以两者
	// 不会对同一个地址给出不同答案;混排输出(部分行带 "device for "、部分不带)也就能
	// 各认各的。
	for _, e := range localized {
		if _, exists := res[e.addr]; !exists {
			res[e.addr] = e.queue
		}
	}
	return res
}

// guessLocalizedQueue 从「设备 URI 之前的那段文字」里猜出队列名。
//
// 猜不准时返回空串而不是硬猜:猜错会去投一个不存在的队列,而显式配置
// PRINT_AGENT_CUPS_QUEUE 与 system_profiler 那条路永远是对的 —— 宁可留一句明确的
// 「找不到队列」,也不要拿着标签词去投递。
func guessLocalizedQueue(prefix string) string {
	// 队列名与 URI 之间由本地化文案分隔,可能带半角/全角冒号。
	prefix = strings.TrimRight(strings.TrimRight(prefix, " \t"), ":：")

	// 各语言句式不同。实测中文版 CUPS 输出的是(注意标签与队列名之间 **没有空格**):
	//
	//	用于CjfarmKitchen的设备：socket://192.168.1.133:9100
	//
	// 所以不能靠「空格分词后取某一段」——那一条在中文下整行是一个词,永远取不到。
	// 可用的规律是两条:
	//  1. 本地化标签词几乎都是非 ASCII,而队列名通常是 ASCII —— 把非 ASCII 字符全丢掉,
	//     剩下的就是队列名(前头可能还夹着 ASCII 的标签词,如法语的 "apparaat voor");
	//  2. 各语言里队列名都排在标签词之后(「用于 X 的设备」/「device for X」),
	//     故取去除非 ASCII 之后的**最后一个词**。
	fields := strings.Fields(dropNonASCII(prefix))
	if len(fields) == 0 {
		// 队列名本身是中文时必然走到这里(去除非 ASCII 后什么都不剩)。
		// 不硬猜:由 PRINT_AGENT_CUPS_QUEUE 显式指定。
		return ""
	}
	return fields[len(fields)-1]
}

// dropNonASCII 剔除不可打印 ASCII 与斜杠,但保留空格作为词边界。
//
// 斜杠要去掉:CUPS 不允许队列名里含它,留着只会在后面拼 argv 时添乱。
func dropNonASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteRune(' ') // 词边界必须留下,否则 "apparaat voor Kitchen" 会粘连成一个词
		case r >= 33 && r < 127 && r != '/':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ============================================================================
// 队列发现(语言无关来源):system_profiler -json
// ============================================================================

// parseSystemProfilerPrinters 解析 `system_profiler -json SPPrintersDataType` 的输出。
//
// 键名取自系统自带的 SPPrintersReporter.spreporter/Contents/Resources/SPProperties.plist:
// 设备 URI 是 `uri`,打印机名(队列名)是 `_name`。两者都是固定的英文标识符,所以这条
// 路在任何系统语言下都成立 —— 中文 macOS 上它是唯一可靠的自助发现来源。
//
// 键名做了容错(同时接受 uri/device_uri 与 _name/name,并兜底扫描任何 socket:// 值):
// 这些是系统的私有格式,升级后改名不该让门店直接失去发现能力。
func parseSystemProfilerPrinters(out []byte) map[string]string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil
	}
	raw, ok := doc["SPPrintersDataType"]
	if !ok {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	res := map[string]string{}
	for _, it := range items {
		queue := firstStringField(it, "_name", "name")
		if queue == "" {
			continue // 没有队列名就没法投递(例如无打印机时的 status=no_info_found 项)
		}
		uri := firstStringField(it, "uri", "device_uri")
		if uri == "" {
			uri = anySocketURI(it)
		}
		host, ok := strings.CutPrefix(uri, "socket://")
		if !ok {
			continue // 同样只认 socket://,理由见 parseCUPSQueues
		}
		res[normalizeCUPSAddr(strings.TrimSuffix(host, "/"))] = queue
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// firstStringField 取多个候选键里第一个非空字符串值。
func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// anySocketURI 在对象的所有字符串值里找出 socket:// 设备 URI(键名兜底)。
func anySocketURI(m map[string]any) string {
	for _, v := range m {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "socket://") {
			return s
		}
	}
	return ""
}

// discoveredCUPSQueues 返回自动发现的队列映射(带 TTL 缓存)。
//
// 两个来源并用:lpstat 快但输出随系统语言变化;system_profiler 慢但语言无关。
// 谁先认出来以谁为准,另一个只补缺口 —— 这样英文机器上几乎不花额外代价,
// 中文机器上也能自动发现(那才是主流场景)。
func discoveredCUPSQueues() map[string]string {
	cupsQueueCache.Lock()
	defer cupsQueueCache.Unlock()
	if cupsQueueCache.byAddr != nil && time.Since(cupsQueueCache.fetched) < cupsQueueTTL {
		return cupsQueueCache.byAddr
	}

	queues := map[string]string{}
	if out, err := cupsListQueues(); err != nil {
		logvf("lpstat 队列发现未成功(不影响直连通道): %v", err)
	} else {
		for addr, q := range parseCUPSQueues(string(out)) {
			queues[addr] = q
		}
	}
	if out, err := cupsListPrinters(); err != nil {
		logvf("system_profiler 打印机发现未成功(不影响直连通道): %v", err)
	} else {
		for addr, q := range parseSystemProfilerPrinters(out) {
			if _, exists := queues[addr]; !exists {
				queues[addr] = q
			}
		}
	}

	if len(queues) == 0 {
		// 「没发现」不等于「没有」:本机可能真没配 CUPS,也可能只是两种来源都没读出来。
		// 保留上一轮结果(空时返回 nil),让上层给出「找不到队列」的明确报错。
		return cupsQueueCache.byAddr
	}
	cupsQueueCache.byAddr = queues
	cupsQueueCache.fetched = time.Now()
	logvf("CUPS 队列自动发现: 共 %d 个可用 socket 队列", len(queues))
	return queues
}

// ============================================================================
// 队列匹配与投递
// ============================================================================

// findCUPSQueue 为一条打印目标找到可用的 CUPS 队列。
func findCUPSQueue(addr string) (string, bool) {
	if m := printChannelTarget.queueByAddr; m != nil {
		if q := m[addr]; q != "" {
			return q, true
		}
		if q := m[defaultCUPSQueueKey]; q != "" {
			return q, true
		}
	}
	if q := discoveredCUPSQueues()[addr]; q != "" {
		return q, true
	}
	return "", false
}

// cupsQueueHint 找不到队列时给出的排查方向。
func cupsQueueHint(addr string) string {
	return fmt.Sprintf("未找到 %s 对应的 CUPS 打印队列;"+
		"请在「系统设置 → 打印机与扫描仪」里用 IP 方式添加这台打印机(协议选 Socket/HP JetDirect/LPD),"+
		"或用 PRINT_AGENT_CUPS_QUEUE 显式指定队列名(用 `lpstat -v` 查看现有队列)", addr)
}

// printViaCUPS 把一份字节经 CUPS 队列送出,按份数重复提交。
//
// 为什么重复提交而不是 lp -n:lp 的 -n 在走 raw 时会交给 CUPS 的 job 复制机制,
// 部分机型/后端下不生效。与直连通道「重复发送同一份指令」保持同一策略,行为可预期。
func printViaCUPS(queue string, data []byte, copies int, addr string) error {
	for i := 0; i < copies; i++ {
		out, err := cupsSubmit(cupsSubmitArgs(queue), data)
		msg := strings.TrimSpace(string(out))
		if err != nil {
			if msg != "" {
				return fmt.Errorf("CUPS 队列[%s] 提交失败: %v(%s)", queue, err, msg)
			}
			return fmt.Errorf("CUPS 队列[%s] 提交失败: %w", queue, err)
		}
		logvf("CUPS 队列[%s] 已受理 %s 的第 %d/%d 份(%.60s)", queue, addr, i+1, copies, msg)
	}
	return nil
}

// ============================================================================
// 通道决策
// ============================================================================

// channelPlan 一条任务最终走哪条通道。
type channelPlan struct {
	tcp      bool   // 是否先尝试直连
	cups     bool   // 是否可用 CUPS 兜底
	queue    string // CUPS 队列名(cups=true 时有效)
	fellBack bool   // CUPS 是「直连失败后的回退」而非首选
}

// planChannel 按当前设置与目标地址决定通道。
//
// 关键设计:tcp 模式完全不去碰 lpstat —— 默认部署(Windows/Linux 及未开启的 macOS)
// 的行为与改造前一模一样,不让一个新增的可选能力给既有门店引入任何新变量。
func planChannel(addr string) channelPlan {
	switch printChannelTarget.via {
	case channelCUPS:
		queue, ok := findCUPSQueue(addr)
		return channelPlan{cups: ok, queue: queue}
	case channelAuto:
		queue, ok := findCUPSQueue(addr)
		return channelPlan{tcp: true, cups: ok, queue: queue, fellBack: ok}
	default:
		return channelPlan{tcp: true}
	}
}

// ============================================================================
// 失败归因提示
// ============================================================================

// dialFailureHintShown 保证该提示每个进程只输出一次。
//
// 用 atomic.Bool 而不是 sync.Once:Once 没有 Reset,测试在多轮运行(go test -count=2)
// 之间无法复位,断言「首次非空、之后为空」的用例会莫名失败。语义上我们只需要「标记过
// 没有」,不需要 Once 的阻塞保证。
var dialFailureHintShown atomic.Bool

// dialFailureHint 针对「连接被本机策略瞬间拒绝」给出一次性处置提示。
//
// 为什么只提示一次:这类失败会随熔断冷却反复出现(每 30 秒一轮),一条任务刷一遍
// 会把日志冲垮;而处置办法始终是同一句话,看一次就够。
//
// 只认 no route to host,不认超时:超时/拒绝连接是打印机侧的真实故障,提示改通道
// 只会把门店带偏。
func dialFailureHint(err error) string {
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "no route to host") {
		return ""
	}
	if !dialFailureHintShown.CompareAndSwap(false, true) {
		return ""
	}
	return "连接被瞬间拒绝(no route to host)且耗时极短:若本机是 macOS 15 及以上," +
		"这通常是系统的「本地网络」隐私权限拦下了自启动的代理,而不是打印机故障 —— " +
		"在终端里手工执行 --probe 能通即可确认(终端不受该权限限制)。" +
		"处置:优先改用 CUPS 通道(执行 --setup-cups <打印机IP> 一条命令即可)," +
		"或按部署手册 macOS 一节给本 App 授予「本地网络」权限。"
}

// ============================================================================
// 参数解析辅助
// ============================================================================

// printChannelNote 通道日志的补充说明:让门店在日志第一屏就能看出当前配置走哪条路。
func printChannelNote(cfg config) string {
	switch cfg.printVia {
	case channelCUPS:
		return "(任务一律交给本机 CUPS 队列;CUPS 不可用时不会退回直连)"
	case channelAuto:
		return "(优先直连打印机;直连失败且本机有对应 CUPS 队列时自动改投 CUPS)"
	default:
		return "(直连打印机 IP:9100)"
	}
}

// normalizePrintVia 归一化通道配置。
//
// 非法值直接报错而不是悄悄退回 tcp:配错的字(tcps / cup)如果被静默忽略,门店看到
// 的仍是 "no route to host",而配置文件里明明写着「改用 CUPS」—— 比不配还难查。
func normalizePrintVia(s string) (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "", channelTCP:
		return channelTCP, nil
	case channelCUPS:
		return channelCUPS, nil
	case channelAuto:
		return channelAuto, nil
	default:
		return "", fmt.Errorf("PRINT_AGENT_PRINT_VIA/--print-via 取值非法: %q(只支持 %s / %s / %s)",
			s, channelTCP, channelCUPS, channelAuto)
	}
}
