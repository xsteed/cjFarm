package main

// ============================================================================
// 打印机连通性自检(--probe)测试
//
// 与打印路径共用 dialTCP 注入点(见 protocol_test.go 的 mockPrinter):
// 拨号被重定向到内存管道,validatePrinterAddr 这条真实防护路径照常执行。
// ============================================================================

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// 目标解析
// ============================================================================

func TestProbeParseTargets(t *testing.T) {
	t.Run("缺省端口补 9100", func(t *testing.T) {
		got := parseProbeTargets("192.168.1.100")
		if len(got) != 1 {
			t.Fatalf("应解析出 1 个目标, got %d", len(got))
		}
		if got[0].ip != "192.168.1.100" || got[0].port != 9100 {
			t.Fatalf("目标解析错误: %+v", got[0])
		}
		if got[0].addr() != "192.168.1.100:9100" {
			t.Fatalf("addr()=%q, want 192.168.1.100:9100", got[0].addr())
		}
	})

	t.Run("显式端口优先", func(t *testing.T) {
		got := parseProbeTargets("192.168.1.101:9101")
		if len(got) != 1 || got[0].port != 9101 || got[0].addr() != "192.168.1.101:9101" {
			t.Fatalf("显式端口应保留, got %+v", got)
		}
	})

	t.Run("多目标混合分隔", func(t *testing.T) {
		// 逗号/空格/中文逗号都算分隔:命令行里贴一串 IP 是最常见用法。
		got := parseProbeTargets(" 192.168.1.100, 192.168.1.101:9101 ，192.168.1.102 ")
		if len(got) != 3 {
			t.Fatalf("应解析出 3 个目标, got %d(%+v)", len(got), got)
		}
		if got[1].port != 9101 || got[2].port != 9100 {
			t.Fatalf("分隔后端口解析错误: %+v", got)
		}
	})

	t.Run("空串无目标", func(t *testing.T) {
		if got := parseProbeTargets("  "); len(got) != 0 {
			t.Fatalf("空白输入不应产生目标, got %+v", got)
		}
	})
}

// ============================================================================
// 单台探测
// ============================================================================

func TestProbeOneReachable(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)

	r := probeOne(parseProbeTarget("192.168.1.100"), 2*time.Second, false)
	if r.err != nil {
		t.Fatalf("假打印机应可达, got %v", r.err)
	}
	if r.printed {
		t.Fatal("默认不应吐纸")
	}
	if r.status == nil || !r.status.queried {
		t.Fatalf("应回读到状态(假打印机应答三条 DLE EOT), got %+v", r.status)
	}
	if r.status.raw != "0200/0300/0400" {
		t.Fatalf("全正常时 raw 应为 0200/0300/0400, got %q", r.status.raw)
	}
	if got := describeStatus(r.status); !strings.Contains(got, "正常") {
		t.Fatalf("状态描述应含「正常」, got %q", got)
	}
}

func TestProbeOneUnreachable(t *testing.T) {
	installDialFailureHook(t)

	r := probeOne(parseProbeTarget("192.168.1.100"), time.Second, false)
	if r.err == nil {
		t.Fatal("拨号失败时应返回不可达")
	}
	if !strings.Contains(r.err.Error(), "无法连接打印机 192.168.1.100:9100") {
		t.Fatalf("错误信息应带地址且措辞与打印路径一致, got %v", r.err)
	}
	if r.status != nil {
		t.Fatal("不可达时不应有状态回读")
	}
}

// 自检命令不能变成内网扫描器:地址校验与打印任务走同一条防线。
func TestProbeOneRejectsIllegalAddr(t *testing.T) {
	installDialFailureHook(t) // 真拨号了就说明校验没生效(测试环境无打印机)

	for _, target := range []string{"127.0.0.1:9100", "169.254.169.254:9100", "0.0.0.0:9100", "not-an-ip:9100"} {
		r := probeOne(parseProbeTarget(target), time.Second, false)
		if r.err == nil {
			t.Fatalf("%s 应被地址校验拦下", target)
		}
		if !r.rejected {
			t.Fatalf("%s 应标记为地址被拦下(rejected), got %+v", target, r)
		}
		if strings.Contains(r.err.Error(), "无法连接打印机") {
			t.Fatalf("%s 应在拨号前被拦下, got %v", target, r.err)
		}
	}
}

func TestProbeOneSendsSelfTestTicket(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)

	r := probeOne(parseProbeTarget("192.168.1.100"), 2*time.Second, true)
	if r.err != nil {
		t.Fatalf("吐自检页不应失败: %v", r.err)
	}
	if !r.printed {
		t.Fatal("--probe-print 应标记已送出")
	}
	printer.waitIdle()
	if got := printer.connCount(); got != 1 {
		t.Fatalf("应建立 1 次连接, got %d", got)
	}
	if len(printer.prints) != 1 || !strings.Contains(string(printer.prints[0]), "print-agent self test") {
		t.Fatalf("假打印机应收到自检页文本, got %q", printer.prints)
	}
	if !strings.HasPrefix(string(printer.prints[0]), "\x1b@") {
		t.Fatalf("自检页应以 ESC @ 开头, got %x", printer.prints[0])
	}
}

// 机型不应答状态查询:不能算不可达,只是没有状态可报。
func TestProbeStatusNotQueriedIsStillReachable(t *testing.T) {
	orig := dialTCP
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			buf := make([]byte, 64)
			for {
				if _, err := server.Read(buf); err != nil {
					return
				}
			}
		}()
		return client, nil
	}
	t.Cleanup(func() { dialTCP = orig })

	r := probeOne(parseProbeTarget("192.168.1.100"), time.Second, false)
	if r.err != nil {
		t.Fatalf("不应答状态查询仍是可达, got %v", r.err)
	}
	if r.status != nil && r.status.queried {
		t.Fatal("不应答的机型 queried 应为 false")
	}
	if got := describeStatus(r.status); !strings.Contains(got, "不影响出纸") {
		t.Fatalf("未回读时要说明不影响出纸, got %q", got)
	}
}

// 异常状态位要翻译成人话(缺纸/盖板开),而不是甩一个 raw 给店长。
func TestProbeDescribeStatusAbnormal(t *testing.T) {
	st := &statusReply{queried: true, raw: "0220/--/--", coverOpen: true, paperOut: true}
	got := describeStatus(st)
	for _, want := range []string{"盖板开", "缺纸", "链路通"} {
		if !strings.Contains(got, want) {
			t.Fatalf("异常状态描述应含 %q, got %q", want, got)
		}
	}
}

// ============================================================================
// runProbe:汇总与退出码
// ============================================================================

func TestRunProbeAllReachable(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)
	logPath := captureLogToFile(t)

	code := runProbe(config{probe: "192.168.1.100,192.168.1.101", probeTimeout: 2 * time.Second})
	if code != 0 {
		t.Fatalf("全部可达时应返回 0, got %d", code)
	}
	log := readLogFile(t, logPath)
	if !strings.Contains(log, "汇总: 可达 2 / 不可达 0") {
		t.Fatalf("汇总应报 2 台可达,实际:\n%s", log)
	}
	if !strings.Contains(log, "192.168.1.100:9100 可达") || !strings.Contains(log, "192.168.1.101:9100 可达") {
		t.Fatalf("应逐台打印结论,实际:\n%s", log)
	}
}

func TestRunProbeUnreachableReturnsNonZero(t *testing.T) {
	installDialFailureHook(t)
	logPath := captureLogToFile(t)

	code := runProbe(config{probe: "192.168.1.100", probeTimeout: time.Second})
	if code != 1 {
		t.Fatalf("有不可达机器时应返回非 0(供脚本判断), got %d", code)
	}
	log := readLogFile(t, logPath)
	if !strings.Contains(log, "汇总: 可达 0 / 不可达 1") {
		t.Fatalf("汇总应报 1 台不可达,实际:\n%s", log)
	}
	if !strings.Contains(log, "排查:") {
		t.Fatalf("不可达时应给出排查提示,实际:\n%s", log)
	}
	// 自检不入队、不回执,自然也不消耗云端重试次数 —— 这句话必须写进日志,
	// 否则店长会以为自检跑一轮就把单子的重试机会用掉了。
	if !strings.Contains(log, "不会消耗云端重试次数") {
		t.Fatalf("应说明自检不消耗重试次数,实际:\n%s", log)
	}
}

// ============================================================================
// parseFlags:自检不需要 server/token
// ============================================================================

func TestCfgParseFlagsProbe(t *testing.T) {
	t.Run("--probe 无需 server/token", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--probe", "192.168.1.100")
		if err != nil {
			t.Fatalf("自检不应要求 --server/--token, got %v", err)
		}
		if cfg.probe != "192.168.1.100" {
			t.Fatalf("probe=%q", cfg.probe)
		}
		if cfg.probeTimeout != 5*time.Second {
			t.Fatalf("默认拨号超时应为 5s, got %s", cfg.probeTimeout)
		}
		if cfg.probePrint {
			t.Fatal("默认不应吐纸")
		}
	})

	t.Run("--probe-timeout 越界归一化", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--probe", "192.168.1.100", "--probe-timeout", "0")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.probeTimeout != time.Second {
			t.Fatalf("下限应归一化为 1s, got %s", cfg.probeTimeout)
		}

		cfg2, err := cfgParseFlags(t, "--probe", "192.168.1.100", "--probe-timeout", "999")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg2.probeTimeout != 30*time.Second {
			t.Fatalf("上限应归一化为 30s, got %s", cfg2.probeTimeout)
		}
	})

	t.Run("--probe-print 开关", func(t *testing.T) {
		cfg, err := cfgParseFlags(t, "--probe", "192.168.1.100", "--probe-print")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if !cfg.probePrint {
			t.Fatal("--probe-print 应开启吐纸")
		}
	})

	t.Run("PRINT_AGENT_PROBE_TIMEOUT 环境变量", func(t *testing.T) {
		cfg, err := cfgParseFlagsEnv(t, map[string]string{"PRINT_AGENT_PROBE_TIMEOUT": "2"},
			"--probe", "192.168.1.100")
		if err != nil {
			t.Fatalf("parseFlags 失败: %v", err)
		}
		if cfg.probeTimeout != 2*time.Second {
			t.Fatalf("环境变量应生效, got %s", cfg.probeTimeout)
		}
	})

	t.Run("不给 --probe 时照旧校验 server/token", func(t *testing.T) {
		_, err := cfgParseFlags(t, "--probe-timeout", "3")
		if err == nil || !strings.Contains(err.Error(), "缺少 --server") {
			t.Fatalf("未开启自检时应照常校验, got %v", err)
		}
	})

	t.Run("--probe-print 单独使用必须被拦下", func(t *testing.T) {
		// 少了 --probe 时它什么都不探测,却会照常进入常驻模式 —— 那等于在门店机器上
		// 再起一个代理实例,与开机自启的那个抢同一批任务(小票被打两遍)。
		cfg, err := cfgParseFlags(t, "--probe-print", "--server", "https://x.example.com", "--token", "t")
		if err == nil {
			t.Fatalf("应报错而不是进入常驻模式, got cfg.probe=%q probePrint=%v", cfg.probe, cfg.probePrint)
		}
		if !strings.Contains(err.Error(), "需要与 --probe") {
			t.Fatalf("报错应说明与 --probe 搭配使用, got %v", err)
		}
	})
}

// ============================================================================
// 失败归因:瞬时被拒 vs 网络不可达
//
// 这两者的报错文本可以完全一样(macOS 的「本地网络」权限拦下时也报 no route to
// host),唯一能分开它们的信号是「多快被拒」。判错方向会让门店去查打印机电源,
// 永远查不出来,因此这条判据必须钉住。
// ============================================================================

func TestProbeIsInstantHostUnreachable(t *testing.T) {
	noRoute := fmt.Errorf("无法连接打印机 192.168.1.133:9100: dial tcp 192.168.1.133:9100: connect: no route to host")

	tests := []struct {
		name string
		res  probeResult
		want bool
	}{
		{"瞬时 no route to host", probeResult{err: noRoute, elapsed: 2 * time.Millisecond}, true},
		{"瞬时 operation not permitted",
			probeResult{err: fmt.Errorf("connect: operation not permitted"), elapsed: time.Millisecond}, true},
		{"耗时很久的 no route to host(真实网络不可达)",
			probeResult{err: noRoute, elapsed: 3 * time.Second}, false},
		{"连接被拒是打印机侧的问题", probeResult{err: fmt.Errorf("connect: connection refused"), elapsed: time.Millisecond}, false},
		{"超时不是权限问题", probeResult{err: fmt.Errorf("dial tcp: i/o timeout"), elapsed: 5 * time.Second}, false},
		{"没有失败", probeResult{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInstantHostUnreachable(tt.res); got != tt.want {
				t.Fatalf("isInstantHostUnreachable=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestProbeHints(t *testing.T) {
	t.Run("普通不可达只给三条常规排查", func(t *testing.T) {
		hints := probeHints(probeResult{err: fmt.Errorf("connect: connection refused"), elapsed: time.Millisecond})
		if len(hints) != 1 {
			t.Fatalf("普通不可达应只有一条汇总提示, got %v", hints)
		}
		// probe_test.go 的其余用例按这个子串断言,不能改掉。
		if !strings.Contains(hints[0], "排查:") {
			t.Fatalf("首条提示应带「排查:」前缀, got %q", hints[0])
		}
	})

	t.Run("瞬时被拒额外给本地网络权限方向", func(t *testing.T) {
		hints := probeHints(probeResult{
			err:     fmt.Errorf("无法连接打印机 192.168.1.133:9100: connect: no route to host"),
			elapsed: time.Millisecond,
		})
		if len(hints) < 2 {
			t.Fatalf("瞬时被拒应追加处置方向, got %v", hints)
		}
		joined := strings.Join(hints, "\n")
		for _, want := range []string{"本地网络", "PRINT_AGENT_PRINT_VIA", "终端"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("处置方向应提到 %q: %s", want, joined)
			}
		}
	})
}
