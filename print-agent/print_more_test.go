package main

import (
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// 打印相关工具函数
// ============================================================================

func TestPrEffectiveJobPort(t *testing.T) {
	tests := []struct {
		name string
		port int
		want int
	}{
		{name: "零端口用默认 9100", port: 0, want: 9100},
		{name: "负数端口用默认 9100", port: -1, want: 9100},
		{name: "正数端口原样", port: 9101, want: 9101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveJobPort(agentJob{Port: tt.port}); got != tt.want {
				t.Fatalf("effectiveJobPort(%d)=%d, want %d", tt.port, got, tt.want)
			}
		})
	}
}

func TestPrPrinterAddr(t *testing.T) {
	if got := printerAddr(agentJob{IP: "   "}); got != "" {
		t.Fatalf("空 IP 应返回空串, got %q", got)
	}
	got := printerAddr(agentJob{IP: "192.168.1.8", Port: 0})
	if got != "192.168.1.8:9100" {
		t.Fatalf("printerAddr=%q, want 192.168.1.8:9100", got)
	}
}

func TestPrCeilSeconds(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want int
	}{
		{name: "零", d: 0, want: 0},
		{name: "负值", d: -time.Second, want: 0},
		{name: "整秒", d: 2 * time.Second, want: 2},
		{name: "亚秒进位", d: 1500 * time.Millisecond, want: 2},
		{name: "极小正值进位为 1", d: time.Nanosecond, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ceilSeconds(tt.d); got != tt.want {
				t.Fatalf("ceilSeconds(%s)=%d, want %d", tt.d, got, tt.want)
			}
		})
	}
}

func TestPrDocLabel(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "厨房单", doc: "kitchen", want: "厨房单"},
		{name: "食客小票", doc: "guest", want: "食客小票"},
		{name: "测试页", doc: "test", want: "测试页"},
		{name: "空类型", doc: "", want: "单据"},
		{name: "未知类型原样", doc: "custom", want: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := docLabel(agentJob{DocType: tt.doc}); got != tt.want {
				t.Fatalf("docLabel(%q)=%q, want %q", tt.doc, got, tt.want)
			}
		})
	}
}

func TestPrToAgentStatus(t *testing.T) {
	if got := toAgentStatus(nil); got != nil {
		t.Fatalf("nil 输入应返回 nil, got %+v", got)
	}
	st := &statusReply{
		queried:      true,
		raw:          "0200/0300/0400",
		paperOut:     true,
		paperNearEnd: true,
		coverOpen:    true,
		paused:       true,
		errFatal:     true,
	}
	got := toAgentStatus(st)
	if !got.Queried || got.Raw != "0200/0300/0400" ||
		!got.PaperOut || !got.PaperNearEnd || !got.CoverOpen ||
		!got.Paused || !got.Error {
		t.Fatalf("字段映射错误: %+v", got)
	}
}

// ============================================================================
// printJob:通过注入 dialTCP 走完整「校验 → 写字节」路径。
// ============================================================================

type prPipeMock struct {
	mu    sync.Mutex
	wg    sync.WaitGroup
	conns int
	data  [][]byte
}

func (p *prPipeMock) installDialHook(t *testing.T) {
	t.Helper()
	orig := dialTCP
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		server, client := net.Pipe()
		p.mu.Lock()
		p.conns++
		p.wg.Add(1)
		p.mu.Unlock()
		go func() {
			defer p.wg.Done()
			defer server.Close()
			data, _ := io.ReadAll(server)
			p.mu.Lock()
			p.data = append(p.data, data)
			p.mu.Unlock()
		}()
		return client, nil
	}
	t.Cleanup(func() { dialTCP = orig })
}

func (p *prPipeMock) waitIdle() { p.wg.Wait() }

func (p *prPipeMock) connCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns
}

func (p *prPipeMock) prints() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.data))
	copy(out, p.data)
	return out
}

func TestPrPrintJob(t *testing.T) {
	prPayload := base64.StdEncoding.EncodeToString([]byte{0x1B, 0x40, 'o', 'k', '\n'})
	validJob := func() agentJob {
		return agentJob{IP: "192.168.1.100", Port: 9100, Copies: 1, Payload: prPayload}
	}

	t.Run("空 IP 报错", func(t *testing.T) {
		err, _ := printJob(agentJob{IP: "  ", Payload: prPayload})
		if err == nil || !strings.Contains(err.Error(), "任务未带打印机 IP") {
			t.Fatalf("err=%v, want 任务未带打印机 IP", err)
		}
	})

	t.Run("无效地址报错", func(t *testing.T) {
		j := validJob()
		j.IP = "abc"
		err, _ := printJob(j)
		if err == nil || !strings.Contains(err.Error(), "打印机 IP 非法") {
			t.Fatalf("err=%v, want 打印机 IP 非法", err)
		}
	})

	t.Run("payload 非 base64 报错", func(t *testing.T) {
		j := validJob()
		j.Payload = "!!!"
		err, _ := printJob(j)
		if err == nil || !strings.Contains(err.Error(), "打印内容解码失败") {
			t.Fatalf("err=%v, want 打印内容解码失败", err)
		}
	})

	t.Run("空内容报错", func(t *testing.T) {
		j := validJob()
		j.Payload = base64.StdEncoding.EncodeToString([]byte{})
		err, _ := printJob(j)
		if err == nil || !strings.Contains(err.Error(), "打印内容为空") {
			t.Fatalf("err=%v, want 打印内容为空", err)
		}
	})

	t.Run("copies<1 归一为 1", func(t *testing.T) {
		resetStatusBlacklistForTest()
		j := validJob()
		j.Copies = 0
		addr := net.JoinHostPort(j.IP, strconv.Itoa(effectiveJobPort(j)))
		if !markStatusBlacklisted(addr) {
			t.Fatal("预置状态黑名单失败")
		}
		t.Cleanup(resetStatusBlacklistForTest)

		p := &prPipeMock{}
		p.installDialHook(t)
		err, _ := printJob(j)
		if err != nil {
			t.Fatalf("printJob 失败: %v", err)
		}
		if got := p.connCount(); got != 1 {
			t.Fatalf("copies<1 应归一为 1 份, 连接数=%d", got)
		}
	})

	t.Run("成功打印多份", func(t *testing.T) {
		resetStatusBlacklistForTest()
		j := validJob()
		j.Copies = 2
		addr := net.JoinHostPort(j.IP, strconv.Itoa(effectiveJobPort(j)))
		if !markStatusBlacklisted(addr) {
			t.Fatal("预置状态黑名单失败")
		}
		t.Cleanup(resetStatusBlacklistForTest)

		p := &prPipeMock{}
		p.installDialHook(t)
		err, st := printJob(j)
		if err != nil {
			t.Fatalf("printJob 失败: %v", err)
		}
		if st != nil {
			t.Fatalf("跳过状态查询时应返回 nil 状态, got %+v", st)
		}
		p.waitIdle()
		if got := p.connCount(); got != 2 {
			t.Fatalf("copies=2 应建立 2 次连接, got %d", got)
		}
		want, _ := base64.StdEncoding.DecodeString(prPayload)
		prints := p.prints()
		if len(prints) != 2 {
			t.Fatalf("应记录 2 份数据, got %d", len(prints))
		}
		for i, data := range prints {
			if string(data) != string(want) {
				t.Fatalf("第 %d 份数据不一致: got %x want %x", i+1, data, want)
			}
		}
	})

	t.Run("dial 失败进入冷却", func(t *testing.T) {
		resetDialCooldownForTest()
		j := validJob()
		addr := net.JoinHostPort(j.IP, strconv.Itoa(effectiveJobPort(j)))
		orig := dialTCP
		dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
			return nil, errors.New("connection refused")
		}
		t.Cleanup(func() { dialTCP = orig })
		t.Cleanup(resetDialCooldownForTest)

		err, _ := printJob(j)
		if err == nil || !strings.Contains(err.Error(), "无法连接") {
			t.Fatalf("err=%v, want 无法连接", err)
		}
		if _, ok := isDialCooling(addr); !ok {
			t.Fatalf("拨号失败后地址 %s 应进入冷却", addr)
		}
	})

	t.Run("写失败报错", func(t *testing.T) {
		// 写失败也会把地址打进冷却(与拨号失败同策略),避免污染后续用例。
		resetDialCooldownForTest()
		t.Cleanup(resetDialCooldownForTest)
		j := validJob()
		orig := dialTCP
		dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
			server, client := net.Pipe()
			_ = server.Close() // 对端立即关闭,后续 Write 必然失败
			return client, nil
		}
		t.Cleanup(func() { dialTCP = orig })

		err, _ := printJob(j)
		if err == nil || !strings.Contains(err.Error(), "写入打印数据失败") {
			t.Fatalf("err=%v, want 写入打印数据失败", err)
		}
	})
}

// ============================================================================
// 状态黑名单 / 拨号冷却:补充 nil map 重建分支。
// ============================================================================

func TestPrStatusBlacklistNilMap(t *testing.T) {
	statusBlacklist.Lock()
	statusBlacklist.m = nil
	statusBlacklist.Unlock()
	t.Cleanup(resetStatusBlacklistForTest)

	addr := "192.0.2.20:9100"
	if !markStatusBlacklisted(addr) {
		t.Fatal("nil map 时首次标记应返回 true")
	}
	statusBlacklist.Lock()
	rebuilt := statusBlacklist.m != nil
	statusBlacklist.Unlock()
	if !rebuilt {
		t.Fatal("markStatusBlacklisted 应重建 nil map")
	}
}

func TestPrDialCooldownNilMap(t *testing.T) {
	dialCooldown.Lock()
	dialCooldown.m = nil
	dialCooldown.Unlock()
	t.Cleanup(resetDialCooldownForTest)

	addr := "192.0.2.21:9100"
	markDialCooldown(addr)
	if _, ok := isDialCooling(addr); !ok {
		t.Fatal("nil map 时标记后应处于冷却期")
	}
	dialCooldown.Lock()
	rebuilt := dialCooldown.m != nil
	dialCooldown.Unlock()
	if !rebuilt {
		t.Fatal("markDialCooldown 应重建 nil map")
	}
}

// ============================================================================
// 日志用任务摘要:订单号/桌号必须出现在日志里,便于按单检索
// ============================================================================

func TestPrOrderLabel(t *testing.T) {
	tests := []struct {
		name string
		job  agentJob
		want string
	}{
		{name: "订单号+桌号", job: agentJob{OrderNo: "NO42", TableNo: "T1"}, want: "订单NO42/桌T1"},
		{name: "只有订单号", job: agentJob{OrderNo: "NO42"}, want: "订单NO42"},
		{name: "只有桌号", job: agentJob{TableNo: "T1"}, want: "桌T1"},
		{name: "都没有", job: agentJob{}, want: ""},
		{name: "空白视为没有", job: agentJob{OrderNo: "  ", TableNo: ""}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orderLabel(tt.job); got != tt.want {
				t.Fatalf("orderLabel=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrJobBrief(t *testing.T) {
	got := jobBrief(agentJob{
		JobID: 42, DocType: "kitchen", OrderNo: "NO42", TableNo: "T1",
		PrinterName: "后厨打印机", IP: "192.168.1.100",
	})
	for _, want := range []string{"#42", "厨房单", "订单NO42/桌T1", "后厨打印机", "192.168.1.100:9100"} {
		if !strings.Contains(got, want) {
			t.Fatalf("jobBrief=%q 应包含 %q", got, want)
		}
	}

	// 端口缺省时日志里也应是实际生效的 9100,而不是 0。
	if !strings.Contains(jobBrief(agentJob{IP: "192.168.1.8"}), "192.168.1.8:9100") {
		t.Fatalf("缺省端口应展示 9100, got %q", jobBrief(agentJob{IP: "192.168.1.8"}))
	}
}

func TestPrJobBriefsTruncates(t *testing.T) {
	jobs := make([]agentJob, maxBriefJobs+3)
	for i := range jobs {
		jobs[i] = agentJob{JobID: i + 1, DocType: "guest", PrinterName: "前台", IP: "192.168.1.9"}
	}
	got := jobBriefs(jobs)
	if !strings.Contains(got, "...") && !strings.Contains(got, "…等共") {
		t.Fatalf("超出 %d 条时应省略, got %q", maxBriefJobs, got)
	}
	if !strings.Contains(got, "共 8 条") {
		t.Fatalf("省略部分应报总条数, got %q", got)
	}

	// 不超过上限时逐条展开,不出现省略号。
	few := jobBriefs(jobs[:2])
	if strings.Contains(few, "…") {
		t.Fatalf("未超上限不应省略, got %q", few)
	}
}
