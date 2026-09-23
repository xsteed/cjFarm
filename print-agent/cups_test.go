package main

// ============================================================================
// CUPS 打印通道测试
//
// 与打印路径共用两个注入点(dialTCP 见 print.go、cupsSubmit/cupsListQueues 见
// cups.go):既能覆盖「选错通道」这类只在真机上才暴露的问题,又不会在 CI 里真的
// 调用 lpstat / lp。
// ============================================================================

import (
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// withPrintChannel 临时切换通道设置,测试结束自动还原。
func withPrintChannel(t *testing.T, via string, queues map[string]string) {
	t.Helper()
	origVia, origQueues := printChannelTarget.via, printChannelTarget.queueByAddr
	printChannelTarget.via, printChannelTarget.queueByAddr = via, queues
	t.Cleanup(func() {
		printChannelTarget.via, printChannelTarget.queueByAddr = origVia, origQueues
	})
}

// withCupsSubmit 替换作业提交实现,返回收集到的 argv 列表。
func withCupsSubmit(t *testing.T, out string, err error) *[]string {
	t.Helper()
	orig := cupsSubmit
	var calls []string
	cupsSubmit = func(argv []string, data []byte) ([]byte, error) {
		calls = append(calls, strings.Join(argv, " "))
		if err != nil {
			return []byte(out), err
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { cupsSubmit = orig })
	return &calls
}

// withCupsList 替换队列发现实现。
//
// 同时把 system_profiler 那条来源置空:否则走了自动发现的用例会去执行真实的
// system_profiler,让测试依赖运行环境的打印机配置。
func withCupsList(t *testing.T, out string, err error) {
	t.Helper()
	withCupsPrinters(t, `{"SPPrintersDataType":[]}`)
	orig := cupsListQueues
	cupsListQueues = func() ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() {
		cupsListQueues = orig
		cupsQueueCache.Lock()
		cupsQueueCache.byAddr, cupsQueueCache.fetched = nil, time.Time{}
		cupsQueueCache.Unlock()
	})
}

// withCupsPrinters 替换 system_profiler 那条发现来源。
func withCupsPrinters(t *testing.T, out string) {
	t.Helper()
	orig := cupsListPrinters
	cupsListPrinters = func() ([]byte, error) { return []byte(out), nil }
	t.Cleanup(func() {
		cupsListPrinters = orig
		cupsQueueCache.Lock()
		cupsQueueCache.byAddr, cupsQueueCache.fetched = nil, time.Time{}
		cupsQueueCache.Unlock()
	})
}

// clearDialCooldown 清掉某个地址的冷却记录(全局状态,会跨用例互相干扰)。
func clearDialCooldown(addr string) {
	dialCooldown.Lock()
	delete(dialCooldown.m, addr)
	dialCooldown.Unlock()
}

func testJob(ip string, copies int) agentJob {
	return agentJob{
		JobID:       1,
		IP:          ip,
		Port:        9100,
		Copies:      copies,
		Payload:     base64.StdEncoding.EncodeToString([]byte{0x1B, 0x40, 'h', 'i'}),
		PrinterName: "测试打印机",
	}
}

// ============================================================================
// lpstat 输出解析
// ============================================================================

func TestCupsParseCUPSQueues(t *testing.T) {
	out := strings.Join([]string{
		"device for Kitchen: socket://192.168.1.133:9100",
		"device for Front: socket://192.168.1.100",
		"device for Office: ipp://192.168.1.10:631/ipp/print",
		"device for Legacy: lpd://192.168.1.20/queue",
		"",
		"垃圾行",
		"device for NoUri: ",
	}, "\n")

	got := parseCUPSQueues(out)
	want := map[string]string{
		"192.168.1.133:9100": "Kitchen",
		"192.168.1.100:9100": "Front", // 缺端口按 9100 归一化
	}
	if len(got) != len(want) {
		t.Fatalf("应只认出 socket:// 队列, got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s 应映射到 %s, got %q", k, v, got[k])
		}
	}
}

// ============================================================================
// 显式映射与地址归一化
// ============================================================================

func TestCupsParseQueueConfig(t *testing.T) {
	t.Run("默认队列(只写队列名)", func(t *testing.T) {
		got := parseCUPSQueueConfig("厨房机")
		if got[defaultCUPSQueueKey] != "厨房机" {
			t.Fatalf("应写成通配键, got %v", got)
		}
	})
	t.Run("地址映射与端口补全", func(t *testing.T) {
		got := parseCUPSQueueConfig("192.168.1.133=厨房机, 192.168.1.100:9101=前台机")
		if got["192.168.1.133:9100"] != "厨房机" {
			t.Fatalf("缺端口应按 9100 补全, got %v", got)
		}
		if got["192.168.1.100:9101"] != "前台机" {
			t.Fatalf("显式端口应保留, got %v", got)
		}
	})
	t.Run("空值与非法项", func(t *testing.T) {
		if got := parseCUPSQueueConfig("   "); got != nil {
			t.Fatalf("空配置应返回 nil, got %v", got)
		}
		if got := parseCUPSQueueConfig("192.168.1.1="); got != nil {
			t.Fatalf("缺队列名的项应被丢弃, got %v", got)
		}
	})
}

func TestCupsNormalizeAddr(t *testing.T) {
	tests := map[string]string{
		"192.168.1.133":      "192.168.1.133:9100",
		"192.168.1.133:9101": "192.168.1.133:9101",
		"不是地址":               "不是地址", // 原样返回,免得把写错的键悄悄吞掉
		"":                   "",
	}
	for in, want := range tests {
		if got := normalizeCUPSAddr(in); got != want {
			t.Fatalf("normalizeCUPSAddr(%q)=%q, want %q", in, got, want)
		}
	}
}

// ============================================================================
// 通道归一化与决策
// ============================================================================

func TestCupsNormalizePrintVia(t *testing.T) {
	for _, v := range []string{"", "tcp", "TCP", " cups ", "auto"} {
		if _, err := normalizePrintVia(v); err != nil {
			t.Fatalf("normalizePrintVia(%q) 不应报错: %v", v, err)
		}
	}
	// 非法值必须报错:静默退回 tcp 会让门店在「明明配了 cups」的情况下继续看到
	// no route to host,比不配还难查。
	if _, err := normalizePrintVia("cup"); err == nil {
		t.Fatal("非法通道值应当报错")
	}
}

func TestCupsPlanChannel(t *testing.T) {
	addr := "192.168.1.133:9100"

	t.Run("tcp 模式不查队列", func(t *testing.T) {
		withPrintChannel(t, channelTCP, map[string]string{addr: "Kitchen"})
		plan := planChannel(addr)
		if !plan.tcp || plan.cups {
			t.Fatalf("tcp 模式应只走直连, got %+v", plan)
		}
	})

	t.Run("cups 模式只用队列", func(t *testing.T) {
		withPrintChannel(t, channelCUPS, map[string]string{addr: "Kitchen"})
		plan := planChannel(addr)
		if plan.tcp || !plan.cups || plan.queue != "Kitchen" {
			t.Fatalf("cups 模式应只走队列, got %+v", plan)
		}
	})

	t.Run("cups 模式无队列时给出明确信号", func(t *testing.T) {
		withPrintChannel(t, channelCUPS, map[string]string{})
		withCupsList(t, "", nil)
		plan := planChannel(addr)
		if plan.tcp || plan.cups {
			t.Fatalf("无队列时应既不直连也不 CUPS, got %+v", plan)
		}
	})

	t.Run("auto 模式先直连并保留兜底", func(t *testing.T) {
		withPrintChannel(t, channelAuto, map[string]string{addr: "Kitchen"})
		plan := planChannel(addr)
		if !plan.tcp || !plan.cups || !plan.fellBack {
			t.Fatalf("auto 模式应直连优先 + 队列兜底, got %+v", plan)
		}
	})

	t.Run("auto 模式无队列时退化为纯直连", func(t *testing.T) {
		withPrintChannel(t, channelAuto, map[string]string{})
		withCupsList(t, "", nil)
		plan := planChannel(addr)
		if !plan.tcp || plan.cups {
			t.Fatalf("无队列时 auto 应等价于 tcp, got %+v", plan)
		}
	})
}

func TestCupsFindCUPSQueueFallbackToDiscovery(t *testing.T) {
	withPrintChannel(t, channelCUPS, nil)
	withCupsList(t, "device for Kitchen: socket://192.168.1.133:9100\n", nil)

	q, ok := findCUPSQueue("192.168.1.133:9100")
	if !ok || q != "Kitchen" {
		t.Fatalf("应自动发现队列, got %q ok=%v", q, ok)
	}
	// 第二次调用命中缓存:把发现实现换成会失败的,结果应当不变。
	withCupsList(t, "", errors.New("boom"))
	if q, ok := findCUPSQueue("192.168.1.133:9100"); !ok || q != "Kitchen" {
		t.Fatalf("应命中缓存, got %q ok=%v", q, ok)
	}
}

// ============================================================================
// printJob 的通道行为
// ============================================================================

func TestPrintJobCUPSOnlyNeverDials(t *testing.T) {
	const addr = "192.168.1.133:9100"
	origDial := dialTCP
	t.Cleanup(func() { dialTCP = origDial })
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		t.Fatalf("CUPS 通道不应发起直连(%s)", address)
		return nil, nil
	}

	withPrintChannel(t, channelCUPS, map[string]string{addr: "Kitchen"})
	calls := withCupsSubmit(t, "request id is Kitchen-1 (1 file(s))", nil)

	err, st := printJob(testJob("192.168.1.133", 2))
	if err != nil {
		t.Fatalf("CUPS 通道不应失败: %v", err)
	}
	if st != nil {
		t.Fatalf("CUPS 通道回读不到 DLE EOT 状态,应为 nil, got %+v", st)
	}
	if len(*calls) != 2 {
		t.Fatalf("2 份应提交 2 次, got %d: %v", len(*calls), *calls)
	}
	for _, c := range *calls {
		// -o raw 是整个通道的关键:少了它 CUPS 会走过滤器,ESC/POS 字节被打散。
		if !strings.Contains(c, "-d Kitchen") || !strings.Contains(c, "-o raw") {
			t.Fatalf("lp 参数不正确: %s", c)
		}
	}
}

func TestPrintJobCUPSOnlyMissingQueueReportsClearly(t *testing.T) {
	origDial := dialTCP
	t.Cleanup(func() { dialTCP = origDial })
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		t.Fatalf("缺队列时不应退回直连")
		return nil, nil
	}
	withPrintChannel(t, channelCUPS, map[string]string{})
	withCupsList(t, "", nil)

	err, _ := printJob(testJob("192.168.1.133", 1))
	if err == nil {
		t.Fatal("缺队列应报错")
	}
	// 报错必须点明「缺队列」,否则门店会跑去查打印机电源,方向完全错了。
	if !strings.Contains(err.Error(), "CUPS") || !strings.Contains(err.Error(), "lpstat -v") {
		t.Fatalf("报错应指明缺 CUPS 队列并给出排查命令, got %v", err)
	}
}

func TestPrintJobAutoFallsBackToCUPS(t *testing.T) {
	const addr = "192.168.1.133:9100"
	clearDialCooldown(addr)

	origDial := dialTCP
	t.Cleanup(func() { dialTCP = origDial })
	dials := 0
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		dials++
		return nil, errors.New("dial tcp " + addr + ": connect: no route to host")
	}

	withPrintChannel(t, channelAuto, map[string]string{addr: "Kitchen"})
	calls := withCupsSubmit(t, "request id is Kitchen-2 (1 file(s))", nil)

	err, st := printJob(testJob("192.168.1.133", 1))
	if err != nil {
		t.Fatalf("auto 模式应在 CUPS 兜底下成功: %v", err)
	}
	if st != nil {
		t.Fatalf("CUPS 兜底时状态应为 nil, got %+v", st)
	}
	if dials != 1 || len(*calls) != 1 {
		t.Fatalf("应直连 1 次后改投 CUPS 1 次, got dials=%d cups=%d", dials, len(*calls))
	}
	// 关键:直连失败但 CUPS 成功,绝不能记冷却 —— 否则下一条任务被冷却挡住,
	// 永远走不到这个明明能成功的通道。
	if _, cooling := isDialCooling(addr); cooling {
		t.Fatal("CUPS 兜底成功时不应触发熔断冷却")
	}
}

func TestPrintJobAutoBothFailMarksCooldown(t *testing.T) {
	const addr = "192.168.1.133:9100"
	clearDialCooldown(addr)

	origDial := dialTCP
	t.Cleanup(func() { dialTCP = origDial })
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, errors.New("dial tcp " + addr + ": connect: no route to host")
	}
	withPrintChannel(t, channelAuto, map[string]string{addr: "Kitchen"})
	withCupsSubmit(t, "lp: Error - unable to access printer", errors.New("exit status 1"))

	err, _ := printJob(testJob("192.168.1.133", 1))
	if err == nil {
		t.Fatal("两条路都断应报错")
	}
	// 报错要同时带上两条通道的原因,否则只能看到一半。
	if !strings.Contains(err.Error(), "no route to host") || !strings.Contains(err.Error(), "CUPS") {
		t.Fatalf("应同时给出直连与 CUPS 的失败原因, got %v", err)
	}
	if _, cooling := isDialCooling(addr); !cooling {
		t.Fatal("两条路都断时应触发熔断冷却")
	}
	clearDialCooldown(addr)
}

func TestPrintJobTCPModeDoesNotTouchCUPS(t *testing.T) {
	const addr = "192.168.1.133:9100"
	clearDialCooldown(addr)

	origDial, origList := dialTCP, cupsListQueues
	t.Cleanup(func() { dialTCP, cupsListQueues = origDial, origList })
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, errors.New("dial tcp " + addr + ": connect: connection refused")
	}
	cupsListQueues = func() ([]byte, error) {
		t.Fatal("默认 tcp 通道不应执行 lpstat")
		return nil, nil
	}
	withPrintChannel(t, channelTCP, nil)

	// 注意 printJob 的返回顺序是 (error, *statusReply),error 在前。
	if err, _ := printJob(testJob("192.168.1.133", 1)); err == nil {
		t.Fatal("连接被拒应报错")
	}
	// tcp 模式必须保留原有的冷却语义(与改造前一致)。
	if _, cooling := isDialCooling(addr); !cooling {
		t.Fatal("tcp 模式拨号失败应触发熔断冷却")
	}
	clearDialCooldown(addr)
}

// ============================================================================
// 失败归因提示
// ============================================================================

func TestCupsDialFailureHint(t *testing.T) {
	if got := dialFailureHint(nil); got != "" {
		t.Fatalf("无错误不应提示, got %q", got)
	}
	if got := dialFailureHint(errors.New("i/o timeout")); got != "" {
		// 超时是打印机侧的真实故障,提示改通道只会把门店带偏。
		t.Fatalf("超时不应提示本地网络权限, got %q", got)
	}

	// sync.Once 语义:同一个提示只输出一次(测试进程内共享同一个 Once,故只能
	// 断言「首次非空、之后为空」这一顺序性质)。
	first := dialFailureHint(errors.New("dial tcp 192.168.1.133:9100: connect: no route to host"))
	if first == "" {
		t.Fatal("no route to host 应给出本地网络权限提示")
	}
	if !strings.Contains(first, "本地网络") {
		t.Fatalf("提示应点明「本地网络」权限, got %q", first)
	}
	if second := dialFailureHint(errors.New("connect: no route to host")); second != "" {
		t.Fatalf("同一提示不应重复输出, got %q", second)
	}
}

// ============================================================================
// 通道日志说明
// ============================================================================

func TestCupsPrintChannelNote(t *testing.T) {
	for _, via := range []string{channelTCP, channelCUPS, channelAuto} {
		if got := printChannelNote(config{printVia: via}); got == "" {
			t.Fatalf("%s 应有一句说明", via)
		}
	}
	if got := printChannelNote(config{printVia: channelCUPS}); !strings.Contains(got, "CUPS") {
		t.Fatalf("CUPS 通道说明应提到 CUPS, got %q", got)
	}
}

func TestCupsParseCUPSQueuesEmpty(t *testing.T) {
	if got := parseCUPSQueues(""); len(got) != 0 {
		t.Fatalf("空输出应得到空映射, got %v", got)
	}
}

// ============================================================================
// 本地化 lpstat 输出(macOS 上 lpstat 无视 LC_ALL,中文系统必然走到这里)
// ============================================================================

func TestCupsParseCUPSQueuesLocalized(t *testing.T) {
	// 本地化文案里标签词在前、队列名在后。
	got := parseCUPSQueues("设备 Kitchen：socket://192.168.1.133:9100\n")
	if got["192.168.1.133:9100"] != "Kitchen" {
		t.Fatalf("「设备 X:」语序应能认出队列名, got %v", got)
	}

	// 反过来:队列名在前、标签词在后。
	got = parseCUPSQueues("Kitchen 的设备：socket://192.168.1.133:9100\n")
	if got["192.168.1.133:9100"] != "Kitchen" {
		t.Fatalf("「X 的设备:」语序应能认出队列名, got %v", got)
	}

	// 英文行照旧优先,且不该被本地化兜底覆盖。
	got = parseCUPSQueues("device for Front: socket://192.168.1.100:9100\n" +
		"设备 Kitchen：socket://192.168.1.133:9100\n")
	if got["192.168.1.100:9100"] != "Front" || got["192.168.1.133:9100"] != "Kitchen" {
		t.Fatalf("混排时应各自认对, got %v", got)
	}
}

func TestCupsParseCUPSQueuesLocalizedErrorLine(t *testing.T) {
	// 中文系统上没配打印机时 lpstat 的输出:一个 URI 都没有,应得到空映射而不是乱认。
	got := parseCUPSQueues("lpstat: 未添加目的位置。\n")
	if len(got) != 0 {
		t.Fatalf("无队列时不应认出任一映射, got %v", got)
	}
}

func TestCupsGuessLocalizedQueue(t *testing.T) {
	tests := map[string]string{
		"设备 ":                     "",
		"设备 Kitchen：":             "Kitchen",
		"Kitchen 的设备：":            "Kitchen",
		"apparaat voor Kitchen: ": "Kitchen",
		"Кириллица Kitchen: ":     "Kitchen",
		// 队列名本身是中文且标签词也是中文:无从判断语序,宁可放弃 ——
		// 由 system_profiler 那条语言无关的路兜住。
		"设备 厨房打印机：": "",
	}
	for in, want := range tests {
		if got := guessLocalizedQueue(in); got != want {
			t.Fatalf("guessLocalizedQueue(%q)=%q, want %q", in, got, want)
		}
	}
}

// ============================================================================
// system_profiler 来源(语言无关)
// ============================================================================

func TestCupsParseSystemProfilerPrinters(t *testing.T) {
	out := `{
	  "SPPrintersDataType" : [
	    { "cupsversion" : "CUPS/2.3.4", "status" : "no_info_found" },
	    { "_name" : "Kitchen", "uri" : "socket://192.168.1.133:9100", "status" : "idle" },
	    { "_name" : "Office", "uri" : "ipp://192.168.1.10:631/ipp/print" },
	    { "_name" : "Front", "uri" : "socket://192.168.1.100" },
	    { "uri" : "socket://192.168.1.9:9100" },
	    { "_name" : "Weird", "device_uri" : "socket://192.168.1.8:9100" },
	    { "_name" : "Renamed", "something_else" : "socket://192.168.1.7:9100" }
	  ]
	}`
	got := parseSystemProfilerPrinters([]byte(out))

	want := map[string]string{
		"192.168.1.133:9100": "Kitchen",
		"192.168.1.100:9100": "Front",   // 缺端口补 9100
		"192.168.1.8:9100":   "Weird",   // device_uri 兼容
		"192.168.1.7:9100":   "Renamed", // 键名变了也能兜住
	}
	if len(got) != len(want) {
		t.Fatalf("结果数量不符, got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s 应映射到 %s, got %q", k, v, got[k])
		}
	}
	// ipp:// 与「没有 _name」的项都必须被丢掉:前者协议不对,后者无法投递。
	if _, ok := got["192.168.1.10:631"]; ok {
		t.Fatalf("ipp:// 不应被认出, got %v", got)
	}
	if _, ok := got["192.168.1.9:9100"]; ok {
		t.Fatalf("没有队列名的项应被丢弃, got %v", got)
	}
}

func TestCupsParseSystemProfilerPrintersBadInput(t *testing.T) {
	for _, in := range []string{"", "不是 JSON", "{}", `{"SPPrintersDataType": "不是数组"}`, `{"Other":[]}`} {
		if got := parseSystemProfilerPrinters([]byte(in)); got != nil {
			t.Fatalf("输入 %q 应返回 nil, got %v", in, got)
		}
	}
}

// ============================================================================
// --probe-print 时额外验证「代理实际走的那条通道」
// ============================================================================

func TestCupsProbeCUPSPrint(t *testing.T) {
	const addr = "192.168.1.133:9100"

	t.Run("tcp 通道不做额外投递", func(t *testing.T) {
		withPrintChannel(t, channelTCP, map[string]string{addr: "Kitchen"})
		calls := withCupsSubmit(t, "request id is Kitchen-1 (1 file(s))", nil)
		probeCUPSPrint(addr)
		if len(*calls) != 0 {
			t.Fatalf("tcp 通道不应经 CUPS 投递, got %v", *calls)
		}
	})

	t.Run("auto 通道经队列投递自检页", func(t *testing.T) {
		withPrintChannel(t, channelAuto, map[string]string{addr: "Kitchen"})
		calls := withCupsSubmit(t, "request id is Kitchen-1 (1 file(s))", nil)
		probeCUPSPrint(addr)
		if len(*calls) != 1 {
			t.Fatalf("应经 CUPS 投递 1 次, got %v", *calls)
		}
	})

	t.Run("无队列时不投递", func(t *testing.T) {
		withPrintChannel(t, channelCUPS, map[string]string{})
		withCupsList(t, "", nil)
		calls := withCupsSubmit(t, "", nil)
		probeCUPSPrint(addr)
		if len(*calls) != 0 {
			t.Fatalf("找不到队列时不应投递, got %v", *calls)
		}
	})
}

func TestCupsDiscoveryMergesSources(t *testing.T) {
	// 中文系统上 lpstat 一行都认不出,system_profiler 顶上 —— 这是主流门店场景。
	withCupsList(t, "lpstat: 未添加目的位置。\n", nil)
	withCupsPrinters(t, `{"SPPrintersDataType":[{"_name":"厨房打印机","uri":"socket://192.168.1.133:9100"}]}`)

	q, ok := findCUPSQueue("192.168.1.133:9100")
	if !ok || q != "厨房打印机" {
		t.Fatalf("应由 system_profiler 兜住(含中文队列名), got %q ok=%v", q, ok)
	}
}
