package main

// ============================================================================
// --doctor / --setup-cups 测试
//
// 这两个命令是「门店自助」的入口,共同特点是**不要求云端配置就能跑** ——
// 配置缺失正是要查出来的问题。所以参数解析那两条分支必须钉住:
// 一旦它们退回常规校验,门店在最需要体检的时候(配置坏了)反而跑不起来。
// ============================================================================

import (
	"runtime"
	"strings"
	"testing"
)

func TestCfgDoctorNeedsNoCloudConfig(t *testing.T) {
	cfg, err := cfgParseFlags(t, "--doctor")
	if err != nil {
		t.Fatalf("--doctor 不应要求 --server/--token: %v", err)
	}
	if !cfg.doctor {
		t.Fatal("doctor 标志未置位")
	}
	// 通道取值必须已归一化:体检要报告「当前走哪条通道」,拿到未归一化的原值会误导。
	if _, err := normalizePrintVia(cfg.printVia); err != nil {
		t.Fatalf("doctor 分支应把通道取值归一化, got %q", cfg.printVia)
	}
}

func TestCfgDoctorTakesPrecedenceOverProbe(t *testing.T) {
	// --doctor --probe x 必须是「体检 + 打印机那一段」。若被 --probe 分支先截走,
	// 配置/自启动/防睡眠就全被跳过了 —— 那就不是体检了。
	cfg, err := cfgParseFlags(t, "--doctor", "--probe", "192.168.1.133")
	if err != nil {
		t.Fatalf("parseFlags 失败: %v", err)
	}
	if !cfg.doctor {
		t.Fatal("同时给 --probe 时仍应保留 doctor")
	}
	if cfg.probe != "192.168.1.133" {
		t.Fatalf("目标应被保留, got %q", cfg.probe)
	}
	if cfg.probeTimeout <= 0 {
		t.Fatal("doctor 分支也要做探测超时归一化")
	}
}

func TestCfgDoctorRejectsLoneProbePrint(t *testing.T) {
	// --doctor 下 probePrint 单独出现没有意义,但也不该被当成「要起常驻实例」拦掉。
	cfg, err := cfgParseFlags(t, "--doctor", "--probe-print")
	if err != nil {
		t.Fatalf("--doctor 下不应因 probePrint 报错: %v", err)
	}
	if !cfg.doctor {
		t.Fatal("doctor 标志未置位")
	}
}

func TestCfgSetupCUPSNeedsNoCloudConfig(t *testing.T) {
	cfg, err := cfgParseFlags(t, "--setup-cups", "192.168.1.133")
	if err != nil {
		t.Fatalf("--setup-cups 不应要求 --server/--token(它正是打不出纸时才用): %v", err)
	}
	if cfg.setupCUPS != "192.168.1.133" {
		t.Fatalf("目标解析错误: %q", cfg.setupCUPS)
	}
}

func TestDoctorCountersAndExitCode(t *testing.T) {
	// 分级计数决定结论行与退出码,而门店只记「结论」那一行 —— 计数错了结论就是错的。
	d := &doctor{}
	d.good("a")
	d.note("b")
	d.hint("c")
	d.bad("d")
	d.bad("e")
	if d.ok != 1 || d.info != 1 || d.warn != 1 || d.fail != 2 {
		t.Fatalf("计数不符: ok=%d info=%d warn=%d fail=%d", d.ok, d.info, d.warn, d.fail)
	}
}

func TestDoctorRunsWithBrokenConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("体检的 macOS 检查项(lanuchctl/ioreg/lpstat)只在 macOS 上可跑")
	}
	// 设计承诺:配置坏了(缺 server/token)恰恰是最需要体检的时候,所以 --doctor
	// 必须能在没有云端配置的情况下跑完,并如实判为「有问题」(退出码 1)。
	// 这一条同时把 checkIdentity/checkConfig/checkAutostart/checkChannel/checkSleep/
	// checkCloud 的跳过分支都跑了一遍。
	// 会真实执行 lpstat 等命令,故把队列缓存清干净再走 —— 否则本机的真实队列会被
	// 留在全局缓存里,污染后面依赖该缓存的用例(实测踩到过)。
	t.Cleanup(invalidateCUPSQueueCache)
	code := runDoctor(config{printVia: channelTCP})
	if code != 1 {
		t.Fatalf("缺 server/token 时体检应判为有问题(退出码 1), got %d", code)
	}
}

func TestDoctorMaskToken(t *testing.T) {
	// 体检会把令牌「首尾露出」给门店核对是哪一条,中间必须遮住。
	for _, c := range []struct{ in, want string }{
		{"", "****"},
		{"12345678", "****"},
		{"123456789", "1234****6789"},
		{"vrQAyYnuVtCDrfDJbDQzVr4s7REEheUU", "vrQA****heUU"},
	} {
		if got := maskToken(c.in); got != c.want {
			t.Fatalf("maskToken(%q)=%q, want %q", c.in, got, c.want)
		}
	}
	if got := maskToken("vrQAyYnuVtCDrfDJbDQzVr4s7REEheUU"); strings.Contains(got, "VtCD") {
		t.Fatalf("中段不应泄露: %q", got)
	}
}

func TestDoctorValueAfterAndFirstLine(t *testing.T) {
	// launchctl print 的实际输出片段:状态与 PID 都靠这两个函数从里面抠出来。
	const sample = "\tstate = running\n\tpid = 81911\n\tprogram = /usr/bin/caffeinate\n"
	if got := valueAfter(sample, "pid = "); got != "81911" {
		t.Fatalf("valueAfter(pid)=%q, want 81911", got)
	}
	if got := valueAfter(sample, "state = "); got != "running" {
		t.Fatalf("valueAfter(state)=%q, want running", got)
	}
	if got := valueAfter(sample, "不存在的前缀"); got != "" {
		t.Fatalf("取不到时应返回空串, got %q", got)
	}
	if got := firstLine("\n\n  Could not find service  \n后面还有"); got != "Could not find service" {
		t.Fatalf("firstLine=%q", got)
	}
	if got := firstLine(""); got != "" {
		t.Fatalf("空输入应返回空串, got %q", got)
	}
}

func TestDoctorCupsQueueNameFor(t *testing.T) {
	// 队列名必须是纯 ASCII:自动发现是从本地化输出里挑 ASCII 词来认队列名的
	// (见 guessLocalizedQueue),中文名会让发现失效;也不能带空格等需要转义的字符。
	got := cupsQueueNameFor("192.168.1.133")
	if got != "Cjfarm-192.168.1.133" {
		t.Fatalf("队列名不符: %q", got)
	}
	for _, r := range got {
		if r > 127 || r == ' ' || r == '/' {
			t.Fatalf("队列名含非法字符 %q: %s", r, got)
		}
	}
}

func TestDoctorOrDashAndParenIf(t *testing.T) {
	if got := orDash("  "); got != "-" {
		t.Fatalf("空值应显示为短横线, got %q", got)
	}
	if got := orDash("v1.0.0"); got != "v1.0.0" {
		t.Fatalf("orDash 不应改动正常值, got %q", got)
	}
	if got := parenIf(""); got != "" {
		t.Fatalf("无附注时不应产生空括号, got %q", got)
	}
	if got := parenIf(" x "); got != "(x)" {
		t.Fatalf("parenIf=%q", got)
	}
}
