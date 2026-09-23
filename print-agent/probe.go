package main

// ============================================================================
// 打印机连通性自检(--probe)
//
// 门店排障最常问的一句是「代理到打印机这段到底通不通」。这段链路云端回答不了
// (云服务器够不到门店内网),打印日志要等队列里真有单才有结论,而门店机器上
// nc / Test-NetConnection 未必装了、店长也未必会用。--probe 把它变成一条命令:
// 直接拨 IP:9100,顺带回读打印机状态,可选吐一张自检页,一台机器一行结论。
//
// 四条设计约束:
//   - 纯本地:不连云端,所以 --server / --token 缺失(甚至断网)时照样能查内网;
//   - 目标地址照旧走 validatePrinterAddr —— 排障命令不能变成内网扫描器;
//   - 绕过拨号熔断冷却:排障要的就是真实再拨一次,而不是被上一次失败挡住 30 秒;
//   - 默认不吐纸:只探端口 + 读状态;真要出纸显式加 --probe-print。
// ============================================================================

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultProbePort 未写端口时的默认端口(9100 = 打印机 RAW / JetDirect 端口)。
	defaultProbePort = 9100
	// statusReadTimeout 单条状态回读的读超时。打印机应答状态是毫秒级的,
	// 等太久只会拖慢「连不上」的结论(三台机器各等 5 秒就没法用了)。
	statusReadTimeout = 500 * time.Millisecond
	// statusSettleDelay 写完字节到回读状态之间的静置:热敏机先收缓冲再走纸,
	// 立刻查会读到滞后状态。
	statusSettleDelay = 300 * time.Millisecond
)

// probeTarget 一个待探测的目标。
type probeTarget struct {
	ip   string
	port int
	raw  string // 命令行原文(错误回显用)
}

// addr 标准化后的 ip:port。
func (t probeTarget) addr() string {
	return net.JoinHostPort(t.ip, strconv.Itoa(t.port))
}

// probeResult 一台机器的探测结论。
type probeResult struct {
	target   probeTarget
	err      error         // 非空=没探通(地址被拦下或拨号失败)
	elapsed  time.Duration // 拨号耗时(地址被拦下时为 0)
	status   *statusReply  // 状态回读(未查到/不应答时为非 queried)
	printed  bool          // 是否真的送出了自检页字节
	rejected bool          // 地址未通过安全校验:与「网络不通」的排查方向完全不同
}

// ============================================================================
// 目标解析
// ============================================================================

// parseProbeTargets 解析 --probe 的目标列表:逗号/空格/顿号分隔,ip 或 ip:port。
func parseProbeTargets(s string) []probeTarget {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]probeTarget, 0, len(fields))
	for _, f := range fields {
		out = append(out, parseProbeTarget(f))
	}
	return out
}

// parseProbeTarget 解析单个目标:带端口用 net.SplitHostPort,不带端口补 9100。
//
// 不支持域名:代理侧的地址校验只放行 IP(见 validatePrinterAddr),自检也一样,
// 免得排障时被 DNS 解析结果误导。
func parseProbeTarget(s string) probeTarget {
	s = strings.TrimSpace(s)
	t := probeTarget{raw: s, port: defaultProbePort}
	if host, portStr, err := net.SplitHostPort(s); err == nil {
		t.ip = strings.TrimSpace(host)
		if p, err := strconv.Atoi(strings.TrimSpace(portStr)); err == nil {
			t.port = p
		}
		return t
	}
	t.ip = s
	return t
}

// ============================================================================
// 单台探测
// ============================================================================

// probeOne 探测一台打印机:校验地址 → 拨号 →(可选吐纸)→ 回读状态。
//
// 与 printJob 的区别:它不读冷却表(排障要真实拨一次),也不写冷却表(一次性命令,
// 进程退出即结束,没有冷却的必要)。
func probeOne(t probeTarget, timeout time.Duration, wantPrint bool) probeResult {
	r := probeResult{target: t}
	if err := validatePrinterAddr(t.ip, t.port); err != nil {
		r.rejected = true
		r.err = fmt.Errorf("目标 %q 未通过地址安全校验: %w", t.raw, err)
		return r
	}
	addr := t.addr()

	start := time.Now()
	conn, err := dialTCP("tcp", addr, timeout)
	r.elapsed = time.Since(start).Round(time.Millisecond)
	if err != nil {
		// 与打印路径同一句措辞,排障时在日志里检索「无法连接」能一次捞全。
		r.err = fmt.Errorf("无法连接打印机 %s: %w", addr, err)
		return r
	}
	defer conn.Close()

	if wantPrint {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		if _, werr := conn.Write(selfTestTicket()); werr != nil {
			r.err = fmt.Errorf("向 %s 写入自检页失败: %w", addr, werr)
			return r
		}
		r.printed = true
	}
	r.status = probeStatus(conn)
	return r
}

// probeStatus 回读 DLE EOT 实时状态(与打印路径同一组查询,但不带负面缓存:
// --probe 是一次性命令,没有「30 分钟跳过」的必要,也不应留下那条日志)。
func probeStatus(conn net.Conn) *statusReply {
	time.Sleep(statusSettleDelay)
	var replies [3][]byte
	for i, q := range statusQueries {
		if _, err := conn.Write(q); err != nil {
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(statusReadTimeout))
		var buf [4]byte
		if _, err := io.ReadFull(conn, buf[:]); err != nil {
			continue
		}
		replies[i] = append([]byte(nil), buf[:]...)
	}
	return buildStatusReply(replies)
}

// selfTestTicket 一张最小的自检页(纯 ASCII)。
//
// 为什么只有这么点内容:代理是纯标准库、且刻意不持有排版与编码逻辑
// (GBK 编码、切纸方式都在云端,改协议不用更新代理)。这里的目的只是
// 「确认这条链路能把字节变成纸」,真正的票据长相请用管理后台的「测试打印」。
func selfTestTicket() []byte {
	var b bytes.Buffer
	b.Write([]byte{0x1B, 0x40}) // ESC @ 初始化
	fmt.Fprintf(&b, "print-agent self test\n")
	fmt.Fprintf(&b, "%s\n", time.Now().Format("2006-01-02 15:04:05"))
	b.WriteString("\n\n")
	b.Write([]byte{0x1D, 0x56, 0x42, 0x00}) // GS V 切纸
	return b.Bytes()
}

// describeStatus 把状态回读翻译成一句人话。未查到(机型不支持)时明确说不影响出纸,
// 免得被当成故障。
func describeStatus(st *statusReply) string {
	if st == nil || !st.queried {
		return "状态未回读(该机型不支持 DLE EOT 查询,不影响出纸)"
	}
	var bad []string
	if st.paperOut {
		bad = append(bad, "缺纸")
	}
	if st.paperNearEnd {
		bad = append(bad, "纸将尽")
	}
	if st.coverOpen {
		bad = append(bad, "盖板开")
	}
	if st.paused {
		bad = append(bad, "暂停")
	}
	if st.errFatal {
		bad = append(bad, "不可恢复错误")
	}
	if len(bad) > 0 {
		return "状态 " + st.raw + " 异常:" + strings.Join(bad, "、") + "(链路通,是纸/盖板问题)"
	}
	return "状态 " + st.raw + " 正常"
}

// ============================================================================
// 失败归因
// ============================================================================

// instantRejectThreshold 「瞬时被拒」的判定阈值(毫秒级即为瞬时)。
//
// 这个阈值是 --probe 最有价值的一条判据:真实的网络不可达要么等 ARP 解析超时、
// 要么收到 ICMP host unreachable,都会花掉明显时间;而**本机策略**拦下连接是
// 立刻返回错误,耗时几乎为 0。
const instantRejectThreshold = 200 * time.Millisecond

// isInstantHostUnreachable 判断这次拨号失败是否属于「被本机策略拒绝」而非网络不可达。
//
// 最典型的场景是 macOS 15 起的「本地网络」隐私权限:由 launchd agent 托管的代理
// 连接局域网时被直接拒绝,Go 里报 connect: no route to host,与「打印机没开机」
// 完全同形。只看报错文本没法区分,必须结合「多快被拒」这一维。
func isInstantHostUnreachable(r probeResult) bool {
	if r.err == nil || r.elapsed > instantRejectThreshold {
		return false
	}
	msg := strings.ToLower(r.err.Error())
	return strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "operation not permitted")
}

// probeHints 返回不可达时的排查方向,一行一条。
//
// 第一条必须保留「排查:」前缀 —— probe_test.go 按这个子串断言。
func probeHints(r probeResult) []string {
	hints := []string{"排查:① 打印机是否开机/休眠 ② IP 是否变过(建议静态 IP 或 DHCP 保留) ③ 本机与打印机是否同一网段"}
	if !isInstantHostUnreachable(r) {
		return hints
	}
	return append(hints,
		"④ 连接是被本机**瞬间**拒绝的(不是网络不可达):macOS 15 及以上最常见的原因是「本地网络」隐私权限拦下了自启动的代理。",
		"   · 症状判别:在终端里手工跑同一条 --probe 能通、而开机自启的代理打不出来 —— 就是它。终端及其子进程不受该权限限制。",
		"   · 处置一(推荐):在「系统设置 → 打印机与扫描仪」以 IP 方式添加这台打印机,再设 PRINT_AGENT_PRINT_VIA=auto,让打印改由系统打印服务发起;",
		"   · 处置二:重新执行 --install(会把代理装进 .app),再到「系统设置 → 隐私与安全性 → 本地网络」里允许该 App。",
	)
}

// ============================================================================
// 通道预告
// ============================================================================

// logProbeChannels 打印「自启动的代理会走哪条通道、CUPS 队列找没找到」。
//
// 为什么放进 --probe:门店真正难答的问题往往不是「网络通不通」(探测本身已经答了),
// 而是「我配的 CUPS 通道到底生效了没有」。把自动发现的结果与每个目标解析出的通道
// 直接列出来,「找不到队列」就不再靠猜 —— 这也是 macOS 本地网络权限那套处置里最容易
// 卡住的一步。
func logProbeChannels(targets []probeTarget) {
	if printChannelTarget.via == channelTCP {
		return
	}
	queues := discoveredCUPSQueues()
	if len(queues) == 0 {
		logf("[自检] 打印通道=%s,但没有发现任何 CUPS socket 队列:"+
			"请在「系统设置 → 打印机与扫描仪」里以 IP 方式添加本店打印机(协议选 Socket/HP JetDirect),"+
			"或设 PRINT_AGENT_CUPS_QUEUE 显式指定队列名", printChannelTarget.via)
		return
	}
	// 按地址排序输出:map 遍历顺序随机,日志顺序飘忽会让「对比两次运行」变得没用。
	addrs := make([]string, 0, len(queues))
	for addr := range queues {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	logf("[自检] 打印通道=%s,CUPS 队列自动发现到 %d 个:", printChannelTarget.via, len(queues))
	for _, addr := range addrs {
		logf("[自检]   %s → 队列[%s]", addr, queues[addr])
	}
	for _, t := range targets {
		plan := planChannel(t.addr())
		switch {
		case plan.tcp && plan.cups:
			logf("[自检]   %s 会先直连,直连失败再改投队列[%s]", t.addr(), plan.queue)
		case plan.cups:
			logf("[自检]   %s 会走 CUPS 队列[%s](不再直连)", t.addr(), plan.queue)
		default:
			logf("[自检]   %s 没找到对应的 CUPS 队列,通道=%s 时会直接报错、不退回直连",
				t.addr(), printChannelTarget.via)
		}
	}
}

// probeCUPSPrint 额外从 CUPS 通道送一张自检页,验证「代理实际走的那条路」。
//
// 为什么单列一步:直连能通不代表代理实际用的通道也通。--print-via cups/auto 时真正
// 出纸的是 cupsd,而那条链路的两个常见坑(队列名认错、CUPS 里那台打印机被停用)在
// 直连探测里完全看不见。加 --probe-print 时顺手也跑一遍,门店一条命令就能把整条链路
// 验完,不必等真实订单来撞。
func probeCUPSPrint(addr string) {
	if printChannelTarget.via == channelTCP {
		return
	}
	plan := planChannel(addr)
	if !plan.cups {
		logf("[自检]   通道=%s 但没找到该目标对应的 CUPS 队列,未验证该通道", printChannelTarget.via)
		return
	}
	if err := printViaCUPS(plan.queue, selfTestTicket(), 1, addr); err != nil {
		logf("[自检]   经 CUPS 队列[%s] 送出自检页失败: %v", plan.queue, err)
		return
	}
	logf("[自检]   已额外经 CUPS 队列[%s] 送出自检页(代理实际通道验证通过)", plan.queue)
}

// ============================================================================
// 执行与汇总
// ============================================================================

// runProbe 执行自检并返回进程退出码:全部可达 0,任一不可达 1。
//
// 退出码做成返回值而不是内部 os.Exit,是为了让「有不可达机器 → 非零退出」这条
// 也能被测试覆盖(门店脚本可以据此判断要不要继续)。
func runProbe(cfg config) int {
	targets := parseProbeTargets(cfg.probe)
	if len(targets) == 0 {
		fatalf("--probe 未指定目标,例如: --probe 192.168.1.100 或 --probe 192.168.1.100,192.168.1.101:9100")
	}
	logf("[自检] 开始探测 %d 台打印机(拨号超时 %s,状态回读 开,自检页 %s)",
		len(targets), cfg.probeTimeout, onOff(cfg.probePrint))
	// 探测本身永远走直连(排障要的就是「本机到打印机」这段的真实结论),但把代理实际
	// 会走的通道一并交代清楚,免得「探测可达、打印却不行」时无从下手。
	logProbeChannels(targets)

	reachable, unreachable := 0, 0
	for _, t := range targets {
		r := probeOne(t, cfg.probeTimeout, cfg.probePrint)
		if r.err != nil {
			unreachable++
			if r.rejected {
				// 被安全校验拦下的不是网络问题,给网络排查方向只会误导。
				logf("[自检] %s 跳过: %v", t.raw, r.err)
				logf("[自检]   请填打印机在门店内网的 IP(不支持域名;回环/0.0.0.0/169.254.* 一律不放行)")
				continue
			}
			logf("[自检] %s 不可达(%s): %v", t.addr(), r.elapsed, r.err)
			for _, h := range probeHints(r) {
				logf("[自检]   %s", h)
			}
			// 直连不通时**更要**验证代理实际走的那条通道:macOS 本地网络权限场景下,
			// CUPS 正是唯一还能出纸的路。只在「可达」分支里验等于漏掉最该验的一半。
			if cfg.probePrint {
				probeCUPSPrint(t.addr())
			}
			continue
		}
		reachable++
		suffix := ""
		if r.printed {
			suffix = " 已送出自检页(直连)"
		}
		logf("[自检] %s 可达(%s)%s %s", t.addr(), r.elapsed, suffix, describeStatus(r.status))
		if cfg.probePrint {
			probeCUPSPrint(t.addr())
		}
	}

	logf("[自检] 汇总: 可达 %d / 不可达 %d", reachable, unreachable)
	if unreachable > 0 {
		// 自检只是本机探测:不入队、不回执,也就不会消耗云端的重试次数。
		logf("[自检] 不可达的机器不会消耗云端重试次数;修好后下一单会自动补打(积压任务按时间顺序出)")
		return 1
	}
	return 0
}

// onOff 布尔值的人读形式(日志里的中文开关)。
func onOff(b bool) string {
	if b {
		return "开(会真的出纸)"
	}
	return "关(不吐纸)"
}
