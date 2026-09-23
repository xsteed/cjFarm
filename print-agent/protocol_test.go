package main

// ============================================================================
// 协议契约与端到端联动测试
//
// 这里用两个 mock 把「云后端 ⇄ 本程序 ⇄ 门店打印机」整条链路跑起来:
//   - cloudMock:扮演云端 /api/agent/print/pull|ack|ping 接口,并校验请求头;
//   - mockPrinter:扮演门店里的假打印机,通过 dialTCP 注入点以 net.Pipe 承接连接,
//     接收字节并应答 DLE EOT 状态(打印任务仍走真实的 validatePrinterAddr 校验)。
//
// 契约常量的含义:它们与后端 handler 测试(backend/internal/handler/agent_test.go)
// 中的同名常量是同一份清单的两份拷贝。任一端改动协议字段,对应测试立刻失败,
// 从而在 CI 层面锁死「pull 任务字段 / ack 结果字段 / 打印机状态字段」的兼容性。
// ============================================================================

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// contractPullJobKeys pull 响应里每条任务的字段契约(与后端 model.AgentJob 的 json tag 对齐)。
var contractPullJobKeys = []string{
	"jobId", "printerId", "printerName", "ip", "port",
	"copies", "docType", "orderNo", "tableNo", "deliveryId", "payload",
}

// contractAckResultKeys ack 请求 results[] 元素的字段契约(与后端 AgentAck 解析结构对齐)。
var contractAckResultKeys = []string{"jobId", "ok", "detail", "printerStatus"}

// contractAckResultSkipKeys 「未尝试」回执的字段契约。
//
// 比普通回执多 skipped/retryAfter,少 printerStatus(没打印就没有状态可报)。
// 后端不再镜像一份常量,改由 TestAgentAckSkippedReturnsAttempt 用同样的 JSON 形状
// 断言解析与 attempts 归还行为 —— 任一端改字段名,两端测试一起失败。
var contractAckResultSkipKeys = []string{"jobId", "ok", "detail", "skipped", "retryAfter"}

// contractPrinterStatusKeys ack 里 printerStatus 的字段契约(与后端 print.PrinterStatus 对齐)。
var contractPrinterStatusKeys = []string{
	"queried", "raw", "paperOut", "paperNearEnd", "coverOpen", "paused", "error",
}

// ============================================================================
// cloudMock:模拟云端接口
// ============================================================================

type cloudMock struct {
	t         *testing.T
	server    *httptest.Server
	jobsJSON  string // pull 返回的 data.jobs 原文
	pingData  string // ping 返回的 data 原文(缺省用标准样例)
	wantToken string // 每个请求都要求带上的代理令牌
	wantName  string // 期望的 X-Agent-Name(空=不校验)

	mu   sync.Mutex
	acks []map[string]interface{} // 收到的 ack 请求体

	ackFailures int           // 前 N 次 ack 请求返回 code 500(模拟回执丢失)
	pullDelay   time.Duration // pull 响应延迟(模拟云端长轮询 hold)
	caps        []string      // pull/ping 响应里声明的 serverCapabilities(缺省不含 ack-skipped)
}

// setCapabilities 让 mock 云端声明指定能力(默认只声明 v2 基础能力,即「老云端」)。
func (m *cloudMock) setCapabilities(caps ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.caps = append([]string(nil), caps...)
}

// capsJSON 渲染 serverCapabilities 字段;未显式设置时按「老云端」返回。
func (m *cloudMock) capsJSON() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.caps) == 0 {
		return `["printer-status","long-pull"]`
	}
	b, _ := json.Marshal(m.caps)
	return string(b)
}

func newCloudMock(t *testing.T, jobsJSON, token string) *cloudMock {
	t.Helper()
	m := &cloudMock{t: t, jobsJSON: jobsJSON, wantToken: token}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Agent-Token"); got != m.wantToken {
			t.Errorf("请求 %s 令牌错误: got %q want %q", r.URL.Path, got, m.wantToken)
		}
		if got := r.Header.Get("X-Agent-Version"); got != strconv.Itoa(agentProtocolVersion) {
			t.Errorf("请求 %s 协议版本错误: got %q want %q", r.URL.Path, got, strconv.Itoa(agentProtocolVersion))
		}
		if m.wantName != "" {
			if got := r.Header.Get("X-Agent-Name"); got != m.wantName {
				t.Errorf("请求 %s 代理标识错误: got %q want %q", r.URL.Path, got, m.wantName)
			}
		}
		switch r.URL.Path {
		case "/api/agent/print/pull":
			if m.pullDelay > 0 {
				time.Sleep(m.pullDelay)
			}
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":{"jobs":` + m.jobsJSON +
				`,"serverTime":"2026-09-22 10:00:00","protoVersion":2,"serverCapabilities":` + m.capsJSON() + `}}`))
		case "/api/agent/print/ack":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.mu.Lock()
			m.acks = append(m.acks, body)
			if m.ackFailures > 0 {
				m.ackFailures--
				m.mu.Unlock()
				_, _ = w.Write([]byte(`{"code":500,"msg":"回执暂不可用"}`))
				return
			}
			m.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
		case "/api/agent/print/ping":
			data := m.pingData
			if data == "" {
				data = `{"shopName":"长健农场 柴火农家土菜","serverTime":"2026-09-22 10:00:00","pending":3,` +
					`"serverCapabilities":` + m.capsJSON() + `}`
			}
			_, _ = w.Write([]byte(`{"code":200,"msg":"","data":` + data + `}`))
		default:
			t.Errorf("收到意外的请求路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *cloudMock) url() string { return m.server.URL }

// ackCount 返回云端累计收到的 ack 请求数。
//
// 熔断冷却期内代理「挂起不回执」,靠它断言云端确实没有收到新的回执 —— 每收到
// 一次失败回执,云端的重试额度(attempts,上限 3)就被消耗一次。
func (m *cloudMock) ackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.acks)
}

func (m *cloudMock) lastAck(t *testing.T) map[string]interface{} {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.acks) == 0 {
		t.Fatal("云端尚未收到任何 ack 请求")
	}
	return m.acks[len(m.acks)-1]
}

// ============================================================================
// mockPrinter:模拟门店里的打印机
//
// 通过替换包级 dialTCP 注入 net.Pipe 连接:打印任务里的 IP 用合法内网地址
// (192.168.1.100),因此 validatePrinterAddr 这条真实防护路径在测试中照常执行,
// 只是「拨号」这一步被重定向到内存管道。
// ============================================================================

type mockPrinter struct {
	t *testing.T

	mu     sync.Mutex
	wg     sync.WaitGroup
	conns  int
	prints [][]byte // 每个连接收到的纯打印字节(已剔除状态查询)
}

func newMockPrinter(t *testing.T) *mockPrinter {
	return &mockPrinter{t: t}
}

// installDialHook 让后续所有打印机拨号都落进这台假打印机。
func (p *mockPrinter) installDialHook(t *testing.T) {
	t.Helper()
	orig := dialTCP
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("打印任务应走 tcp 拨号, got %q", network)
		}
		server, client := net.Pipe()
		p.mu.Lock()
		p.conns++
		p.wg.Add(1)
		p.mu.Unlock()
		go func() {
			defer p.wg.Done()
			p.serve(server)
		}()
		return client, nil
	}
	t.Cleanup(func() { dialTCP = orig })
}

// installDialFailureHook 让拨号全部失败(模拟打印机断电/不在同一网段)。
//
// 返回拨号次数计数器,用于断言「熔断冷却期内确实一次都没拨号」—— 熔断的意义
// 就是不重拨,而不是「拨了但报个冷却的错」。
func installDialFailureHook(t *testing.T) *int32 {
	t.Helper()
	orig := dialTCP
	var calls int32
	dialTCP = func(network, address string, timeout time.Duration) (net.Conn, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { dialTCP = orig })
	return &calls
}

// serve 按 9100 语义处理一次连接:收字节流,遇到 DLE EOT 查询就回「一切正常」状态。
func (p *mockPrinter) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	var buf []byte
	dataEnd := -1 // 第一个 DLE EOT 查询出现的位置 = 纯打印数据的边界
	tmp := make([]byte, 1024)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) >= 3 {
				tail := buf[len(buf)-3:]
				if tail[0] == 0x1D && tail[1] == 0x04 {
					// 第一个查询的位置即纯打印数据边界;每条查询都要应答。
					if dataEnd < 0 {
						dataEnd = len(buf) - 3
					}
					_, _ = conn.Write([]byte{tail[0], tail[1], tail[2], 0x00})
				}
			}
		}
		if err != nil {
			break
		}
	}
	if dataEnd < 0 {
		dataEnd = len(buf)
	}
	p.mu.Lock()
	p.prints = append(p.prints, append([]byte(nil), buf[:dataEnd]...))
	p.mu.Unlock()
}

func (p *mockPrinter) waitIdle() { p.wg.Wait() }

func (p *mockPrinter) connCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns
}

// ============================================================================
// 工具
// ============================================================================

func testClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// testConfig 构造一份指向 mock 云端的运行配置。
func testConfig(cloud *cloudMock) config {
	return config{server: cloud.url(), token: "test-token", name: "test-agent", limit: 10, wait: 0}
}

func testState(t *testing.T) *jobState {
	t.Helper()
	return loadJobState(filepath.Join(t.TempDir(), "print-agent.state"))
}

// assertExactKeys 断言 JSON 对象的字段集合与契约完全一致(不多不少)。
func assertExactKeys(t *testing.T, m map[string]json.RawMessage, want []string) {
	t.Helper()
	got := map[string]bool{}
	for k := range m {
		got[k] = true
	}
	for _, k := range want {
		if !got[k] {
			t.Fatalf("缺少契约字段 %q,实际字段: %v", k, keysOfRaw(m))
		}
	}
	for k := range got {
		found := false
		for _, w := range want {
			if w == k {
				found = true
			}
		}
		if !found {
			t.Fatalf("出现契约外字段 %q,契约字段: %v", k, want)
		}
	}
}

func keysOfRaw(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// jobsJSONFor 构造一条完整的契约任务 JSON(IP 用合法内网地址,拨号由测试注入)。
func jobsJSONFor(deliveryID, payload string, copies int) string {
	return `[{"jobId":42,"printerId":7,"printerName":"后厨打印机","ip":"192.168.1.100",` +
		`"port":9100,"copies":` + strconv.Itoa(copies) +
		`,"docType":"kitchen","orderNo":"NO20260922042","tableNo":"T1",` +
		`"deliveryId":"` + deliveryID + `","payload":"` + payload + `"}]`
}

const testDeliveryID = "0123456789abcdef0123456789abcdef"

// testPayload 是一份合法 ESC/POS 字节的 base64(ESC @ + 文本行 + GS V 切纸),
// 程序生成以保证与后端下发格式一致(后端也是 base64.StdEncoding)。
var testPayload = base64.StdEncoding.EncodeToString([]byte{
	0x1B, 0x40, // ESC @
	't', 'e', 's', 't', ' ', '1', '2', '3', '\n',
	0x1D, 0x56, 0x42, 0x00, // GS V 切纸
})

// ackResults 解析 ack 请求体里的 results 数组。
func ackResults(t *testing.T, ack map[string]interface{}) []map[string]json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(ack["results"])
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("ack.results 解析失败: %v(%s)", err, raw)
	}
	return results
}

// ============================================================================
// 端到端:取单 → 打印(2 份)→ 回执,全链路契约断言
// ============================================================================

func TestRunCycleEndToEndContract(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)
	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 2), "test-token")

	cfg := testConfig(cloud)
	st := testState(t)

	n, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st)
	if err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("本轮应处理 1 条任务, got %d", n)
	}

	// 1) 假打印机:copies=2 → 建立 2 次连接,每次都收到与 payload 完全一致的字节。
	printer.waitIdle()
	if got := printer.connCount(); got != 2 {
		t.Fatalf("打印机应收到 2 次连接(2 份), got %d", got)
	}
	wantData, err := base64.StdEncoding.DecodeString(testPayload)
	if err != nil {
		t.Fatalf("测试样例 payload 不是合法 base64: %v", err)
	}
	if len(printer.prints) != 2 {
		t.Fatalf("应记录 2 份打印数据, got %d", len(printer.prints))
	}
	for i, data := range printer.prints {
		if string(data) != string(wantData) {
			t.Fatalf("第 %d 份打印字节与 payload 解码结果不一致:\n got  %x\n want %x", i+1, data, wantData)
		}
	}

	// 2) ack 请求体:results[0] 字段集合与契约一致,且值正确。
	ack := cloud.lastAck(t)
	results := ackResults(t, ack)
	if len(results) != 1 {
		t.Fatalf("ack.results 应只有 1 条, got %d", len(results))
	}
	assertExactKeys(t, results[0], contractAckResultKeys)
	if string(results[0]["jobId"]) != "42" {
		t.Fatalf("ack jobId 错误: %s", results[0]["jobId"])
	}
	if string(results[0]["ok"]) != "true" {
		t.Fatalf("ack ok 应为 true: %s", results[0]["ok"])
	}
	// printerStatus 字段集合与值:假打印机应答了三条 DLE EOT 全正常。
	stRaw, err := json.Marshal(results[0]["printerStatus"])
	if err != nil {
		t.Fatalf("printerStatus 缺失: %v", err)
	}
	var stMap map[string]json.RawMessage
	if err := json.Unmarshal(stRaw, &stMap); err != nil {
		t.Fatalf("printerStatus 解析失败: %v", err)
	}
	assertExactKeys(t, stMap, contractPrinterStatusKeys)
	if string(stMap["queried"]) != "true" {
		t.Fatalf("假打印机应应答状态查询, queried=%s", stMap["queried"])
	}
	if string(stMap["raw"]) != `"0200/0300/0400"` {
		t.Fatalf("状态原文应为全正常 0200/0300/0400, got %s", stMap["raw"])
	}
}

// ============================================================================
// 打印机不可达:回执失败 + 熔断冷却期挂起(不回执、不消耗云端重试次数)
// ============================================================================

func TestRunCyclePrinterUnreachableThenCoolingDown(t *testing.T) {
	resetDialCooldownForTest()
	dials := installDialFailureHook(t)

	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	// 第一轮:拨号失败 → ok=false,detail 带原因。
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	results1 := ackResults(t, cloud.lastAck(t))
	if len(results1) != 1 || string(results1[0]["ok"]) != "false" {
		t.Fatalf("第一轮回执应为失败, got %v", results1)
	}
	if !strings.Contains(string(results1[0]["detail"]), "无法连接") {
		t.Fatalf("失败原因应包含「无法连接」, got %s", results1[0]["detail"])
	}
	if got := atomic.LoadInt32(dials); got != 1 {
		t.Fatalf("第一轮应只拨号 1 次, got %d", got)
	}

	// 第二轮(同一地址):处于熔断冷却 → **挂起不回执**。
	//
	// 这个断言是本次修复的核心回归点:云端在 claim 取单时就 attempts+1(上限 3),
	// 失败回执会让任务退回队列再被取走,也就是每回执一次失败就消耗一次额度。
	// 冷却期内若照常回执失败,3 次额度会被「根本没拨过打印机的空转」吃光 ——
	// 打印机只是短暂不可达(刚上电 / WiFi 未就绪)也会被判成「已放弃」而丢单。
	// 挂起后任务保持 claimed,60 秒租约到期自动回到队列,届时冷却早已结束。
	n, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st)
	if err != nil {
		t.Fatalf("第二轮 runCycle 失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("冷却期内不应回执任何任务, got n=%d", n)
	}
	if got := cloud.ackCount(); got != 1 {
		t.Fatalf("冷却期内的任务不应产生新 ack(否则白白消耗云端重试次数), 累计应仍为 1, got %d", got)
	}
	if got := atomic.LoadInt32(dials); got != 1 {
		t.Fatalf("冷却期内不应再拨号, 累计拨号应仍为 1, got %d", got)
	}

	// 冷却结束后:同一任务恢复真实拨号并正常回执 —— 挂起必须是暂时的,不能永久卡住。
	resetDialCooldownForTest()
	n, err = runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st)
	if err != nil {
		t.Fatalf("冷却结束后 runCycle 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("冷却结束后应恢复回执, got n=%d", n)
	}
	if got := atomic.LoadInt32(dials); got != 2 {
		t.Fatalf("冷却结束后应重新拨号, 累计拨号应为 2, got %d", got)
	}
}

// ============================================================================
// 熔断冷却 + 云端支持 skipped:上报「未尝试」而不是「失败」
// ============================================================================

func TestRunCycleCoolingReportsSkippedWhenSupported(t *testing.T) {
	resetDialCooldownForTest()
	t.Cleanup(func() { serverSupportsSkipped.Store(false) })
	dials := installDialFailureHook(t)

	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cloud.setCapabilities("printer-status", "long-pull", capabilityAckSkipped)
	cfg := testConfig(cloud)
	st := testState(t)

	// 第一轮:真实拨号失败 → 该地址进入冷却。
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	if got := atomic.LoadInt32(dials); got != 1 {
		t.Fatalf("第一轮应只拨号 1 次, got %d", got)
	}

	// 第二轮:冷却中 → 云端支持 skipped,应上报「未尝试」(不是失败、也不是挂起不回执)。
	// 云端收到后把任务退回队列并**归还**尝试次数,冷却结束即可重新下发,
	// 而不必像兼容路径那样等满 60 秒租约。
	n, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st)
	if err != nil {
		t.Fatalf("第二轮 runCycle 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("skipped 也是一条回执, 应返回 1, got %d", n)
	}
	results := ackResults(t, cloud.lastAck(t))
	if len(results) != 1 {
		t.Fatalf("ack.results 应有 1 条, got %d", len(results))
	}
	assertExactKeys(t, results[0], contractAckResultSkipKeys)
	if string(results[0]["ok"]) != "false" {
		t.Fatalf("未尝试不是成功, ok 应为 false: %s", results[0]["ok"])
	}
	if string(results[0]["skipped"]) != "true" {
		t.Fatalf("应上报 skipped=true: %s", results[0]["skipped"])
	}
	retryAfter, err := strconv.Atoi(string(results[0]["retryAfter"]))
	if err != nil || retryAfter <= 0 {
		t.Fatalf("retryAfter 应为正数秒数, got %s", results[0]["retryAfter"])
	}
	// 熔断的意义就是不重拨:冷却期内一次都没拨。
	if got := atomic.LoadInt32(dials); got != 1 {
		t.Fatalf("冷却期内不应再拨号, 累计拨号应仍为 1, got %d", got)
	}
}

// ============================================================================
// 幂等去重:回执丢失 → 云端重发同一 deliveryId → 不重复出纸
// ============================================================================

func TestRunCycleIdempotentDedup(t *testing.T) {
	resetDialCooldownForTest()
	printer := newMockPrinter(t)
	printer.installDialHook(t)
	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	// 第一轮:正常打印。
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("第一轮 runCycle 失败: %v", err)
	}
	printer.waitIdle()
	if got := printer.connCount(); got != 1 {
		t.Fatalf("第一轮应打印 1 份, got %d", got)
	}

	// 第二轮:云端重发同一 deliveryId(模拟回执丢失),应跳过真实打印、直接回执成功。
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("第二轮 runCycle 失败: %v", err)
	}
	printer.waitIdle()
	if got := printer.connCount(); got != 1 {
		t.Fatalf("重发任务不应重复出纸,打印连接数应从 1 保持为 1, got %d", got)
	}
	results := ackResults(t, cloud.lastAck(t))
	if len(results) != 1 || string(results[0]["ok"]) != "true" {
		t.Fatalf("去重跳过的任务应直接回执成功, got %v", results)
	}

	// 状态文件应已落盘(重启后仍能去重)。
	st2 := loadJobState(st.path)
	if !st2.has(testDeliveryID) {
		t.Fatal("幂等号应已持久化到状态文件,重载后仍能识别")
	}
}

// ============================================================================
// pull 反序列化契约:云端下发的完整字段一个不少地进到 agentJob
// ============================================================================

func TestPullUnmarshalContract(t *testing.T) {
	jobs := `[{"jobId":42,"printerId":7,"printerName":"后厨打印机","ip":"192.168.1.100","port":9100,` +
		`"copies":2,"docType":"kitchen","orderNo":"NO42","tableNo":"T1",` +
		`"deliveryId":"` + testDeliveryID + `","payload":"` + testPayload + `"}]`
	cloud := newCloudMock(t, jobs, "test-token")
	cfg := testConfig(cloud)

	got, err := pull(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg)
	if err != nil {
		t.Fatalf("pull 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应取到 1 条任务, got %d", len(got))
	}
	j := got[0]
	if j.JobID != 42 || j.PrinterID != 7 || j.PrinterName != "后厨打印机" ||
		j.IP != "192.168.1.100" || j.Port != 9100 || j.Copies != 2 ||
		j.DocType != "kitchen" || j.OrderNo != "NO42" || j.TableNo != "T1" ||
		j.DeliveryID != testDeliveryID || j.Payload != testPayload {
		t.Fatalf("任务字段解析错误: %+v", j)
	}
}

// ============================================================================
// ack 序列化契约:本程序发出的 JSON 字段集合与后端解析结构一致
// ============================================================================

func TestAckJSONShapeContract(t *testing.T) {
	// 与 main.go runCycle 里 result 局部类型保持同一组 json tag(它无法从测试中引用,
	// 故在此镜像定义;协议字段以契约常量为唯一权威)。
	r := struct {
		JobID         int          `json:"jobId"`
		Ok            bool         `json:"ok"`
		Detail        string       `json:"detail"`
		PrinterStatus *agentStatus `json:"printerStatus,omitempty"`
	}{
		JobID: 42,
		Ok:    true,
		PrinterStatus: &agentStatus{
			Queried: true, Raw: "0200/0300/0400",
		},
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("序列化 result 失败: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	assertExactKeys(t, m, contractAckResultKeys)
	var st map[string]json.RawMessage
	if err := json.Unmarshal(m["printerStatus"], &st); err != nil {
		t.Fatalf("printerStatus 缺失: %v", err)
	}
	assertExactKeys(t, st, contractPrinterStatusKeys)

	// 失败且无状态时的形状:printerStatus 应被省略(omitempty),后端按 nil 处理。
	r2 := struct {
		JobID         int          `json:"jobId"`
		Ok            bool         `json:"ok"`
		Detail        string       `json:"detail"`
		PrinterStatus *agentStatus `json:"printerStatus,omitempty"`
	}{JobID: 1, Ok: false, Detail: "无法连接"}
	raw2, _ := json.Marshal(r2)
	var m2 map[string]json.RawMessage
	_ = json.Unmarshal(raw2, &m2)
	if _, exists := m2["printerStatus"]; exists {
		t.Fatalf("printerStatus 为空时应省略, got %s", raw2)
	}
}

// ============================================================================
// ping 契约:启动自检的三个数据字段必须能从云端响应中解析
// ============================================================================

func TestPingContract(t *testing.T) {
	cloud := newCloudMock(t, `[]`, "test-token")
	cfg := testConfig(cloud)
	if err := ping(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg); err != nil {
		t.Fatalf("ping 应成功, got %v", err)
	}

	// data 缺字段(如老版本后端)时不致命:ping 只校验令牌与地址,字段缺失补零值。
	cloud2 := newCloudMock(t, `[]`, "test-token")
	cloud2.pingData = `{"shopName":"","pending":0}` // 缺 serverTime
	if err := ping(testClient(), strings.TrimSuffix(cloud2.url(), "/"), cfg); err != nil {
		t.Fatalf("缺字段的 ping 响应不应导致失败, got %v", err)
	}
}

// ============================================================================
// 回执重试:云端回执接口抖动时,结果最终必达(打印只发生一次)
// ============================================================================

func TestAckRetriesUntilDelivered(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)
	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cloud.ackFailures = 2 // 前两次回执返回 code 500,第三次成功

	cfg := testConfig(cloud)
	st := testState(t)

	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}

	// 打印只发生一次:回执重试不会导致重复出纸。
	printer.waitIdle()
	if got := printer.connCount(); got != 1 {
		t.Fatalf("打印应只发生 1 次, got %d", got)
	}

	// 回执经历 3 次尝试(2 次失败 + 1 次成功),最终送达。
	cloud.mu.Lock()
	n := len(cloud.acks)
	last := cloud.acks[len(cloud.acks)-1]
	cloud.mu.Unlock()
	if n != 3 {
		t.Fatalf("回执应尝试 3 次(2 失败 + 1 成功), got %d", n)
	}
	results := ackResults(t, last)
	if len(results) != 1 || string(results[0]["ok"]) != "true" {
		t.Fatalf("最终送达的回执内容错误: %v", results)
	}
}

// ============================================================================
// 长轮询:云端 hold 住 pull 响应时,agent 的超时必须覆盖服务端等待时间
// ============================================================================

func TestPullLongPollWait(t *testing.T) {
	cloud := newCloudMock(t, `[]`, "test-token")
	cloud.pullDelay = 1200 * time.Millisecond // 模拟云端 hold 1.2 秒后返回空队列

	cfg := testConfig(cloud)
	cfg.wait = 1 // 长轮询:客户端超时 = 1s + 10s = 11s,应覆盖 1.2s 的 hold

	start := time.Now()
	jobs, err := pull(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("长轮询 pull 不应超时失败: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("空队列应返回 0 条任务, got %d", len(jobs))
	}
	if elapsed < 1200*time.Millisecond {
		t.Fatalf("测试自身问题:mock 延迟未生效(%s)", elapsed)
	}
}

// ============================================================================
// 取单日志:入口那一行必须能回答「收到几条、哪一单、发去哪台打印机」
// ============================================================================

func TestRunCycleLogsPulledJobs(t *testing.T) {
	printer := newMockPrinter(t)
	printer.installDialHook(t)
	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	logPath := captureLogToFile(t)

	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	printer.waitIdle()

	log := readLogFile(t, logPath)
	for _, want := range []string{
		"[取单] 本轮收到 1 条任务",
		"订单NO20260922042",
		"桌T1",
		"192.168.1.100:9100",
		"[成功]",
		"[汇总] 本轮回执 1 条,全部成功",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("日志应包含 %q,实际:\n%s", want, log)
		}
	}
}

// 汇总必须如实拆出成功/失败 —— 「已处理 N 条」这种口径在全是失败时会误导排障。
func TestRunCycleSummaryCountsFailures(t *testing.T) {
	resetDialCooldownForTest()
	installDialFailureHook(t)

	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	logPath := captureLogToFile(t)
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}

	log := readLogFile(t, logPath)
	if !strings.Contains(log, "[汇总] 本轮回执 1 条(成功 0 / 失败 1)") {
		t.Fatalf("失败轮次的汇总应写「成功 0 / 失败 1」,实际:\n%s", log)
	}
}

// 冷却挂起(skipped)不是失败:汇总必须单独列出,否则排障时会误以为这单已经判死要补打。
func TestRunCycleSummaryCountsSkipped(t *testing.T) {
	resetDialCooldownForTest()
	t.Cleanup(func() { serverSupportsSkipped.Store(false) })
	installDialFailureHook(t)

	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cloud.setCapabilities("printer-status", "long-pull", capabilityAckSkipped)
	cfg := testConfig(cloud)
	st := testState(t)

	// 第一轮:真实拨号失败 → 该地址进入冷却。
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("第一轮 runCycle 失败: %v", err)
	}

	logPath := captureLogToFile(t)
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("第二轮 runCycle 失败: %v", err)
	}
	log := readLogFile(t, logPath)
	if !strings.Contains(log, "[汇总] 本轮回执 1 条(成功 0 / 失败 0 / 未尝试(冷却等待)1)") {
		t.Fatalf("冷却轮次的汇总应把「未尝试」单列,实际:\n%s", log)
	}
}

// 失败日志同样要带上订单/打印机定位信息,否则排障时只能拿 jobId 回云端反查。
func TestRunCycleLogsFailureDetail(t *testing.T) {
	resetDialCooldownForTest()
	installDialFailureHook(t)

	cloud := newCloudMock(t, jobsJSONFor(testDeliveryID, testPayload, 1), "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	logPath := captureLogToFile(t)
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}

	log := readLogFile(t, logPath)
	if !strings.Contains(log, "[失败]") {
		t.Fatalf("失败轮次应记录 [失败] 日志,实际:\n%s", log)
	}
	if !strings.Contains(log, "订单NO20260922042") {
		t.Fatalf("失败日志应带订单号,实际:\n%s", log)
	}
}

// 空闲轮次默认静默(常驻模式每几秒一轮),仅 --verbose 时记录。
func TestRunCycleIdleLogOnlyWhenVerbose(t *testing.T) {
	cloud := newCloudMock(t, `[]`, "test-token")
	cfg := testConfig(cloud)
	st := testState(t)

	old := verbose
	t.Cleanup(func() { setVerbose(old) })

	setVerbose(false)
	logPath := captureLogToFile(t)
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	if log := readLogFile(t, logPath); log != "" {
		t.Fatalf("非 verbose 时空轮次不应写日志,实际:\n%s", log)
	}

	setVerbose(true)
	logPath2 := captureLogToFile(t)
	if _, err := runCycle(testClient(), strings.TrimSuffix(cfg.server, "/"), cfg, st); err != nil {
		t.Fatalf("runCycle 失败: %v", err)
	}
	if log := readLogFile(t, logPath2); !strings.Contains(log, "本轮无待打印任务") {
		t.Fatalf("verbose 时空轮次应记录空闲,实际:\n%s", log)
	}
}
