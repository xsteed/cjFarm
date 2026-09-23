package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dialTimeout        = 5 * time.Second
	statusBlacklistTTL = 30 * time.Minute
	// dialCooldownTTL 某 IP:port 拨号失败后的冷却时长。
	//
	// 必须**明显小于**云端的取单租约 agentClaimLeaseSeconds(60s):冷却期内代理
	// 挂起任务不回执,靠租约到期把任务放回队列。若冷却 ≥ 租约,任务回到队列时
	// 仍在冷却中,又会被挂起一整个租约 —— 空转一轮,出票被白白推迟一分钟。
	// 30s 留出足够余量:即使算上拨号超时(5s),租约到期时冷却早已结束。
	dialCooldownTTL = 30 * time.Second
)

// statusQueries 三条 DLE EOT 实时状态查询(打印机状态 n=2/3/4)。
var statusQueries = [3][]byte{
	{0x1D, 0x04, 0x02},
	{0x1D, 0x04, 0x03},
	{0x1D, 0x04, 0x04},
}

var statusBlacklist = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

var dialCooldown = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// ============================================================================
// 打印
// ============================================================================

// statusReply 是打印机对 DLE EOT 实时状态查询的回包解读。
type statusReply struct {
	queried      bool
	raw          string
	paperOut     bool
	paperNearEnd bool
	coverOpen    bool
	paused       bool
	errFatal     bool
}

// agentStatus 是回执给云端的 JSON 结构。字段名必须与云端 handler 对齐。
type agentStatus struct {
	Queried      bool   `json:"queried"`
	Raw          string `json:"raw"`
	PaperOut     bool   `json:"paperOut"`
	PaperNearEnd bool   `json:"paperNearEnd"`
	CoverOpen    bool   `json:"coverOpen"`
	Paused       bool   `json:"paused"`
	Error        bool   `json:"error"`
}

// dialTCP 建立到打印机的 TCP 连接。声明为变量是为了让测试注入假连接(net.Pipe):
// 打印机地址校验禁止回环 IP,单元测试无法真起 127.0.0.1 的打印机,注入后测试
// 仍走完整的「校验 → 写字节 → 查状态」路径。生产路径永远走 net.DialTimeout。
var dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, address, timeout)
}

// printJob 把一条任务写进打印机。
//
// 与后端直连通道完全一致:建立 TCP 连接 → 原样写入字节 → 关闭。
// 份数靠「重复发送同一份指令」实现(ESC/POS 没有联数指令,与后端 sendViaTCP 同策略)。
//
// 走哪条通道由 --print-via 决定(实现见 cups.go):默认直连打印机 IP:9100;macOS 上
// 直连被系统「本地网络隐私」拦下时,可改用 CUPS 队列(cups),或先直连、失败了再自动
// 改投(auto)。
func printJob(j agentJob) (error, *statusReply) {
	if strings.TrimSpace(j.IP) == "" {
		return errors.New("任务未带打印机 IP"), nil
	}
	port := effectiveJobPort(j)
	// 拨号前先校验目标地址:任务可能被云端数据库篡改注入,代理不能变成内网扫描/写入跳板。
	if err := validatePrinterAddr(j.IP, port); err != nil {
		return err, nil
	}
	data, err := base64.StdEncoding.DecodeString(j.Payload)
	if err != nil {
		return fmt.Errorf("打印内容解码失败: %w", err), nil
	}
	if len(data) == 0 {
		return errors.New("打印内容为空"), nil
	}
	copies := j.Copies
	if copies < 1 {
		copies = 1
	}

	addr := net.JoinHostPort(j.IP, strconv.Itoa(port))
	plan := planChannel(addr)

	if !plan.tcp {
		// 纯 CUPS 通道:没有队列就打不了。这里必须把「找不到队列」说清楚,
		// 否则员看到的是「连不上打印机」,会跑去查打印机电源 —— 方向完全错了。
		if !plan.cups {
			return fmt.Errorf("CUPS 打印通道已启用,但%s", cupsQueueHint(addr)), nil
		}
		if cerr := printViaCUPS(plan.queue, data, copies, addr); cerr != nil {
			return cerr, nil
		}
		logf("[通道] %s 经 CUPS 队列[%s] 送出 ×%d(该通道不回读 DLE EOT 状态)", addr, plan.queue, copies)
		return nil, nil
	}

	terr, st := writeViaTCP(addr, data, copies)
	if terr == nil {
		return nil, st
	}

	// auto:直连失败、且本机确实存在对应队列时改投 CUPS。这是 macOS 门店的救场路径 ——
	// 直连被本地网络隐私拦下时是**瞬间**返回 EHOSTUNREACH,CUPS 那条路不受该限制。
	if plan.cups {
		logf("[回退] %s 直连失败(%v),改用 CUPS 队列[%s] 重投", addr, terr, plan.queue)
		cerr := printViaCUPS(plan.queue, data, copies, addr)
		if cerr == nil {
			return nil, nil
		}
		// 两条路都断:先报直连的原因(门店最常要查的那条),再补 CUPS 的失败原因。
		markDialCooldown(addr)
		return fmt.Errorf("%v;改投 CUPS 队列[%s] 也未成功: %v", terr, plan.queue, cerr), nil
	}

	markDialCooldown(addr)
	return terr, st
}

// writeViaTCP 直连打印机 IP:9100 写出数据。
//
// 只负责「连上 → 写字节 → 查状态」,刻意**不**碰熔断冷却表:冷却策略属于通道决策层
// (见 printJob)。否则 auto 模式下「直连失败、CUPS 成功」也会记上一次故障,下一条任务
// 被冷却挡住,永远走不到那个明明能成功的通道。
func writeViaTCP(addr string, data []byte, copies int) (error, *statusReply) {
	for i := 0; i < copies; i++ {
		conn, err := dialTCP("tcp", addr, dialTimeout)
		if err != nil {
			// 打印机没开机 / IP 填错 / 与代理不在同一网段,都会走到这里。
			return fmt.Errorf("无法连接打印机 %s: %w", addr, err), nil
		}
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		_, werr := conn.Write(data)
		if werr != nil {
			_ = conn.Close()
			// 写入失败同样是这条链路的问题(打印机假连、缓冲写满后撞上 deadline)。
			return fmt.Errorf("向 %s 写入打印数据失败: %w", addr, werr), nil
		}
		if i == copies-1 {
			var st *statusReply
			if !isStatusBlacklisted(addr) {
				st = queryPrinterStatus(conn)
			}
			_ = conn.Close()
			return nil, st
		}
		_ = conn.Close()
	}
	return nil, nil
}

// parseStatusReply 校验 DLE EOT 应答并返回第 4 字节状态位。
func parseStatusReply(query, reply []byte) (byte, bool) {
	if len(query) < 3 || len(reply) != 4 {
		return 0, false
	}
	if reply[0] != query[0] || reply[1] != query[1] || reply[2] != query[2] {
		return 0, false
	}
	return reply[3], true
}

// buildStatusReply 把三条 DLE EOT 回包解析成统一状态;缺失/乱码的回包只影响对应字段。
func buildStatusReply(replies [3][]byte) *statusReply {
	st := &statusReply{}
	parts := make([]string, len(statusQueries))
	var status [3]byte
	var ok [3]bool
	for i, q := range statusQueries {
		status[i], ok[i] = parseStatusReply(q, replies[i])
		if ok[i] {
			st.queried = true
			parts[i] = fmt.Sprintf("%02x%02x", q[2], status[i])
		} else {
			parts[i] = "--"
		}
	}
	st.raw = strings.Join(parts, "/")

	if ok[0] {
		st.coverOpen = status[0]&(1<<2) != 0
		st.paused = status[0]&(1<<3) != 0
		st.paperOut = st.paperOut || status[0]&(1<<5) != 0
	}
	if ok[1] {
		st.errFatal = status[1]&(1<<5) != 0
	}
	if ok[2] {
		st.paperNearEnd = status[2]&((1<<2)|(1<<3)) != 0
		st.paperOut = st.paperOut || status[2]&((1<<5)|(1<<6)) != 0
	}
	return st
}

// queryPrinterStatus 在打印数据送出后回读实时状态;失败不影响出票结果。
func queryPrinterStatus(conn net.Conn) *statusReply {
	// 热敏机通常先接收缓冲再走纸,稍等片刻能避开「刚写完就查」导致的状态滞后。
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
	st := buildStatusReply(replies)
	if !st.queried {
		if addr := conn.RemoteAddr().String(); addr != "" && markStatusBlacklisted(addr) {
			logf("[状态] 打印机 %s 不应答状态查询,30分钟内跳过查询", addr)
		}
	}
	return st
}

func toAgentStatus(st *statusReply) *agentStatus {
	if st == nil {
		return nil
	}
	return &agentStatus{
		Queried:      st.queried,
		Raw:          st.raw,
		PaperOut:     st.paperOut,
		PaperNearEnd: st.paperNearEnd,
		CoverOpen:    st.coverOpen,
		Paused:       st.paused,
		Error:        st.errFatal,
	}
}

// ============================================================================
// 状态查询黑名单 / 拨号熔断冷却
// ============================================================================

func markStatusBlacklisted(addr string) bool {
	statusBlacklist.Lock()
	defer statusBlacklist.Unlock()
	if statusBlacklist.m == nil {
		statusBlacklist.m = map[string]time.Time{}
	}
	now := time.Now()
	if until, ok := statusBlacklist.m[addr]; ok && now.Before(until) {
		return false
	}
	statusBlacklist.m[addr] = now.Add(statusBlacklistTTL)
	return true
}

func isStatusBlacklisted(addr string) bool {
	statusBlacklist.Lock()
	defer statusBlacklist.Unlock()
	until, ok := statusBlacklist.m[addr]
	if !ok {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	delete(statusBlacklist.m, addr)
	return false
}

func markDialCooldown(addr string) {
	dialCooldown.Lock()
	defer dialCooldown.Unlock()
	if dialCooldown.m == nil {
		dialCooldown.m = map[string]time.Time{}
	}
	dialCooldown.m[addr] = time.Now().Add(dialCooldownTTL)
}

func isDialCooling(addr string) (time.Duration, bool) {
	dialCooldown.Lock()
	defer dialCooldown.Unlock()
	until, ok := dialCooldown.m[addr]
	if !ok {
		return 0, false
	}
	remain := time.Until(until)
	if remain > 0 {
		return remain, true
	}
	delete(dialCooldown.m, addr)
	return 0, false
}

// ============================================================================
// 打印相关工具
// ============================================================================

func effectiveJobPort(j agentJob) int {
	if j.Port > 0 {
		return j.Port
	}
	return 9100
}

func printerAddr(j agentJob) string {
	if strings.TrimSpace(j.IP) == "" {
		return ""
	}
	return net.JoinHostPort(j.IP, strconv.Itoa(effectiveJobPort(j)))
}

func ceilSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - time.Nanosecond) / time.Second)
}

// maxBriefJobs 入口日志最多逐条展开的任务数。一次最多取 50 条,全展开会把
// 日志文件冲垮 —— 超出部分只报条数。
const maxBriefJobs = 5

// jobBriefs 把一批任务拼成一行摘要,供「本轮收到 N 条」入口日志使用。
func jobBriefs(jobs []agentJob) string {
	var b strings.Builder
	for i, j := range jobs {
		if i == maxBriefJobs {
			b.WriteString(fmt.Sprintf(" …等共 %d 条", len(jobs)))
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(jobBrief(j))
	}
	return b.String()
}

// jobBrief 一条任务的一行摘要,入口日志与逐条结果日志共用同一份描述:
// 「#42 厨房单 订单NO20260922042/桌T1 → 打印机[后厨打印机](192.168.1.100:9100)」。
// 带上订单号/桌号是为了能直接按「哪一单」在日志里检索,不必回云端查 jobId。
func jobBrief(j agentJob) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s", j.JobID, docLabel(j))
	if s := orderLabel(j); s != "" {
		b.WriteString(" " + s)
	}
	fmt.Fprintf(&b, " → 打印机[%s](%s:%d)", j.PrinterName, j.IP, effectiveJobPort(j))
	return b.String()
}

// orderLabel 任务的业务定位信息(订单号/桌号),两者都为空时返回空串。
func orderLabel(j agentJob) string {
	var parts []string
	if o := strings.TrimSpace(j.OrderNo); o != "" {
		parts = append(parts, "订单"+o)
	}
	if t := strings.TrimSpace(j.TableNo); t != "" {
		parts = append(parts, "桌"+t)
	}
	return strings.Join(parts, "/")
}

// docLabel 单据类型的中文名(日志可读性)。
func docLabel(j agentJob) string {
	switch j.DocType {
	case "kitchen":
		return "厨房单"
	case "guest":
		return "食客小票"
	case "test":
		return "测试页"
	}
	if j.DocType == "" {
		return "单据"
	}
	return j.DocType
}
