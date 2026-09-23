package handler

// ============================================================================
// 打印代理协议契约测试(云后端侧)
//
// 与门店侧 print-agent/protocol_test.go 的契约常量互为镜像:两边用同一份字段清单
// 锁定 pull/ack/ping 的 JSON 结构,任一端增删字段,测试立即失败,防止两端漂移。
// 另外这里走的是真实的 handler + SQLite 临时库,覆盖令牌鉴权、取单抢占、
// 回执状态机、租约超时与 per-agent 授权域。
// ============================================================================

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// contractPullJobKeys pull 响应里每条任务的字段契约(与 print-agent 端 agentJob 对齐)。
var contractPullJobKeys = []string{
	"jobId", "printerId", "printerName", "ip", "port",
	"copies", "docType", "orderNo", "tableNo", "deliveryId", "payload",
}

// contractAckResultKeys ack 请求 results[] 元素的字段契约(与 print-agent 端 result 对齐)。
var contractAckResultKeys = []string{"jobId", "ok", "detail", "printerStatus"}

// contractServerCapabilities 云端声明的能力(代理据此决定是否上报 skipped)。
//
// 删减这里等于「悄悄收回能力」:代理会立刻退回挂起不回执的兼容路径,
// 因此用断言锁住,避免与 print-agent 端的能力协商对不上。
var contractServerCapabilities = []string{"printer-status", "long-pull", "ack-skipped"}

// contractPrinterStatusKeys ack 里 printerStatus 的字段契约(与 print-agent 端 agentStatus 对齐)。
var contractPrinterStatusKeys = []string{
	"queried", "raw", "paperOut", "paperNearEnd", "coverOpen", "paused", "error",
}

// ============================================================================
// 基础设施
// ============================================================================

func initAgentTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store.Init(filepath.Join(t.TempDir(), "agent.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// callAgent 以代理身份调用一个 handler,返回 HTTP 状态码与解析后的响应体。
func callAgent(t *testing.T, h gin.HandlerFunc, token, body string) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Agent-Token", token)
	}
	req.Header.Set("X-Agent-Version", "2")
	c.Request = req
	h(c)
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return w.Code, out
}

// newTestAgent 创建一台 v2 打印代理身份,返回 (agentID, token)。
func newTestAgent(t *testing.T, printerIDs string) (int, string) {
	t.Helper()
	agent, token, err := dao.InsertPrintAgent("测试代理", printerIDs)
	if err != nil {
		t.Fatalf("创建打印代理失败: %v", err)
	}
	return agent.AgentID, token
}

// enqueueJob 入队一条打印任务,返回 jobID。
func enqueueJob(t *testing.T, printerID int, ip string, payload string) int {
	t.Helper()
	jobID, err := dao.InsertPrintJob(po.PrintJob{
		PrinterID:   printerID,
		PrinterName: "后厨打印机",
		PrinterType: 1,
		IP:          ip,
		Port:        0, // 0 → 取单时兜底 9100
		DocType:     "kitchen",
		OrderNo:     "NO20260922001",
		TableNo:     "T1",
		Copies:      2,
		Payload:     payload,
		TriggerBy:   "测试",
		CreateTime:  store.Now(),
	})
	if err != nil {
		t.Fatalf("入队打印任务失败: %v", err)
	}
	return jobID
}

func dataOf(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

func jobsOf(t *testing.T, resp map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := resp["data"].(map[string]interface{})["jobs"]
	if !ok {
		t.Fatalf("响应缺少 data.jobs: %v", resp)
	}
	list, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("data.jobs 不是数组: %T", raw)
	}
	jobs := make([]map[string]interface{}, 0, len(list))
	for _, it := range list {
		jobs = append(jobs, it.(map[string]interface{}))
	}
	return jobs
}

// assertExactKeys 断言 JSON 对象的字段集合与契约完全一致(不多不少)。
func assertExactKeys(t *testing.T, m map[string]interface{}, want []string) {
	t.Helper()
	got := map[string]bool{}
	for k := range m {
		got[k] = true
	}
	for _, k := range want {
		if !got[k] {
			t.Fatalf("缺少契约字段 %q,实际字段: %v", k, keysOf(m))
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

func keysOf(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// ============================================================================
// pull:下发任务的字段契约与字节编码
// ============================================================================

func TestAgentPullContract(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "凉拌青瓜\n米饭")

	code, resp := callAgent(t, AgentPull, token, `{"agentId":"测试代理","limit":10,"version":2,"capabilities":["printer-status","long-pull"],"wait":0}`)
	if code != 200 {
		t.Fatalf("pull 应返回 200, got %d(%v)", code, resp)
	}
	jobs := jobsOf(t, resp)
	if len(jobs) != 1 {
		t.Fatalf("应取到 1 条任务, got %d", len(jobs))
	}
	job := jobs[0]
	assertExactKeys(t, job, contractPullJobKeys)

	if job["jobId"] != float64(jobID) {
		t.Fatalf("jobId 错误: %v", job["jobId"])
	}
	if job["printerId"] != float64(7) || job["printerName"] != "后厨打印机" {
		t.Fatalf("打印机信息错误: %v", job)
	}
	if job["ip"] != "192.168.1.100" || job["port"] != float64(9100) {
		t.Fatalf("打印机地址错误(port 0 应兜底 9100): ip=%v port=%v", job["ip"], job["port"])
	}
	if job["copies"] != float64(2) || job["docType"] != "kitchen" ||
		job["orderNo"] != "NO20260922001" || job["tableNo"] != "T1" {
		t.Fatalf("单据字段错误: %v", job)
	}
	if len(job["deliveryId"].(string)) != 32 {
		t.Fatalf("deliveryId 应为 32 字符 hex: %v", job["deliveryId"])
	}

	// payload 是 base64 的 ESC/POS 字节:ESC @ 开头、GS V 切纸结尾(与 agent 端打印一致)。
	raw, err := base64.StdEncoding.DecodeString(job["payload"].(string))
	if err != nil {
		t.Fatalf("payload 不是合法 base64: %v", err)
	}
	if len(raw) < 6 || raw[0] != 0x1B || raw[1] != 0x40 {
		t.Fatalf("payload 应以 ESC @ 开头, got %x", raw[:min(len(raw), 8)])
	}
	if !strings.HasSuffix(string(raw), string([]byte{0x1D, 0x56, 0x42, 0x00})) {
		t.Fatalf("payload 应以 GS V 切纸结尾, got %x", raw[len(raw)-8:])
	}

	// data 层的附加字段(agent 端只依赖 jobs,其余字段为观测信息)。
	data := dataOf(t, resp)
	for _, k := range []string{"jobs", "serverTime", "protoVersion", "serverCapabilities"} {
		if _, ok := data[k]; !ok {
			t.Fatalf("data 缺少字段 %q: %v", k, data)
		}
	}
	assertServerCapabilities(t, data["serverCapabilities"])
}

// assertServerCapabilities 断言云端能力清单与契约一致。
func assertServerCapabilities(t *testing.T, v interface{}) {
	t.Helper()
	caps, ok := v.([]interface{})
	if !ok {
		t.Fatalf("serverCapabilities 类型错误: %T", v)
	}
	if len(caps) != len(contractServerCapabilities) {
		t.Fatalf("serverCapabilities=%v, want %v", caps, contractServerCapabilities)
	}
	for i, want := range contractServerCapabilities {
		if caps[i] != want {
			t.Fatalf("serverCapabilities=%v, want %v", caps, contractServerCapabilities)
		}
	}
}

// ============================================================================
// ack:成功结案 / 失败重试 / 未知任务拒绝
// ============================================================================

// agentStyleAckBody 用与 print-agent 端完全一致的 JSON 形状构造回执(含状态)。
func agentStyleAckBody(jobID int, ok bool, detail string, withStatus bool) string {
	status := ""
	if withStatus {
		status = `,"printerStatus":{"queried":true,"raw":"0200/0300/0400","paperOut":false,"paperNearEnd":false,"coverOpen":false,"paused":false,"error":false}`
	}
	return fmt.Sprintf(`{"results":[{"jobId":%d,"ok":%v,"detail":%q%s}]}`, jobID, ok, detail, status)
}

func TestAgentAckSuccessContract(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "测试单据")

	// 取单(抢占)后回执成功。
	if code, _ := callAgent(t, AgentPull, token, `{"wait":0}`); code != 200 {
		t.Fatalf("pull 失败: %d", code)
	}
	code, resp := callAgent(t, AgentAck, token, agentStyleAckBody(jobID, true, "", true))
	if code != 200 {
		t.Fatalf("ack 应返回 200, got %d(%v)", code, resp)
	}
	data := dataOf(t, resp)
	if data["accepted"] != float64(1) || data["done"] != float64(1) || data["rejected"] != float64(0) {
		t.Fatalf("ack 统计错误: %v", data)
	}

	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_print_job WHERE job_id=?`, jobID).Scan(&status); err != nil {
		t.Fatalf("查询任务状态失败: %v", err)
	}
	if status != po.PrintJobDone {
		t.Fatalf("回执成功后任务应结案(done), got status=%d", status)
	}
}

func TestAgentAckFailureRetryContract(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "测试单据")

	if code, _ := callAgent(t, AgentPull, token, `{"wait":0}`); code != 200 {
		t.Fatalf("pull 失败: %d", code)
	}
	code, resp := callAgent(t, AgentAck, token, agentStyleAckBody(jobID, false, "打印机缺纸", false))
	if code != 200 {
		t.Fatalf("ack 应返回 200, got %d(%v)", code, resp)
	}

	var status, attempts int
	var lastError, nextTryTime string
	if err := store.DB.QueryRow(`SELECT status, attempts, last_error, next_try_time FROM tb_print_job WHERE job_id=?`, jobID).
		Scan(&status, &attempts, &lastError, &nextTryTime); err != nil {
		t.Fatalf("查询任务状态失败: %v", err)
	}
	if status != po.PrintJobPending {
		t.Fatalf("失败回执后任务应退回待取单(pending), got status=%d", status)
	}
	if attempts != 1 || lastError != "打印机缺纸" || nextTryTime == "" {
		t.Fatalf("重试信息错误: attempts=%d lastError=%q nextTryTime=%q", attempts, lastError, nextTryTime)
	}
}

// 冷却挂起的回执:任务退回队列,且**归还**本次取单消耗的尝试次数 ——
// attempts 必须严格等于「真实尝试次数」,否则打印机短暂不可达就会被判成放弃。
func TestAgentAckSkippedReturnsAttempt(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "测试单据")

	if code, _ := callAgent(t, AgentPull, token, `{"wait":0}`); code != 200 {
		t.Fatalf("pull 失败: %d", code)
	}
	body := fmt.Sprintf(`{"results":[{"jobId":%d,"ok":false,"skipped":true,"retryAfter":25,"detail":"打印机连接熔断冷却中(约 25 秒后重试)"}]}`, jobID)
	code, resp := callAgent(t, AgentAck, token, body)
	if code != 200 {
		t.Fatalf("ack 应返回 200, got %d(%v)", code, resp)
	}
	data := dataOf(t, resp)
	if data["skipped"] != float64(1) || data["done"] != float64(0) || data["rejected"] != float64(0) {
		t.Fatalf("skipped 应单独计数: %v", data)
	}

	var status, attempts int
	var nextTryTime string
	if err := store.DB.QueryRow(`SELECT status, attempts, next_try_time FROM tb_print_job WHERE job_id=?`, jobID).
		Scan(&status, &attempts, &nextTryTime); err != nil {
		t.Fatalf("查询任务状态失败: %v", err)
	}
	if status != po.PrintJobPending {
		t.Fatalf("未尝试的任务应退回待取单(pending), got status=%d", status)
	}
	// 取单时 +1,上报 skipped 后归还 → 回到 0。
	if attempts != 0 {
		t.Fatalf("skipped 应归还尝试次数, attempts=%d, want 0", attempts)
	}
	if nextTryTime == "" {
		t.Fatalf("skipped 应按下发的 retryAfter 安排下次取单时间")
	}
}

// 老代理不传 skipped:行为必须与改动前一致(失败重试、attempts 累加)。
func TestAgentAckWithoutSkippedStillConsumesAttempt(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "测试单据")

	if code, _ := callAgent(t, AgentPull, token, `{"wait":0}`); code != 200 {
		t.Fatalf("pull 失败: %d", code)
	}
	code, _ := callAgent(t, AgentAck, token, agentStyleAckBody(jobID, false, "打印机缺纸", false))
	if code != 200 {
		t.Fatalf("ack 应返回 200, got %d", code)
	}
	var attempts int
	if err := store.DB.QueryRow(`SELECT attempts FROM tb_print_job WHERE job_id=?`, jobID).Scan(&attempts); err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("老报文不应归还尝试次数, attempts=%d, want 1", attempts)
	}
}

func TestAgentAckUnknownJobRejected(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	code, resp := callAgent(t, AgentAck, token, agentStyleAckBody(99999, true, "", false))
	if code != 200 {
		t.Fatalf("ack 应返回 200, got %d", code)
	}
	data := dataOf(t, resp)
	if data["rejected"] != float64(1) || data["done"] != float64(0) {
		t.Fatalf("未知任务应计入 rejected: %v", data)
	}
}

// ============================================================================
// ping:启动自检字段
// ============================================================================

func TestAgentPingContract(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	code, resp := callAgent(t, AgentPing, token, `{}`)
	if code != 200 {
		t.Fatalf("ping 应返回 200, got %d(%v)", code, resp)
	}
	data := dataOf(t, resp)
	// agent 端启动自检依赖这三个字段(其余字段为观测信息,agent 忽略)。
	for _, k := range []string{"shopName", "serverTime", "pending"} {
		if _, ok := data[k]; !ok {
			t.Fatalf("ping data 缺少字段 %q: %v", k, data)
		}
	}
}

// ============================================================================
// 鉴权:令牌正确 / 错误 / 吊销
// ============================================================================

func TestAgentAuthContract(t *testing.T) {
	initAgentTestDB(t)
	agentID, token := newTestAgent(t, "")

	if code, _ := callAgent(t, AgentPing, token, `{}`); code != 200 {
		t.Fatalf("正确令牌应放行, got %d", code)
	}
	if code, _ := callAgent(t, AgentPing, "wrong-token", `{}`); code != 403 {
		t.Fatalf("错误令牌应被拒绝(未配置全局令牌时返回 403), got %d", code)
	}
	if code, _ := callAgent(t, AgentPing, "", `{}`); code != 403 {
		t.Fatalf("空令牌应被拒绝, got %d", code)
	}

	// 吊销后原令牌立即失效。
	if err := dao.SetPrintAgentStatus(agentID, 0); err != nil {
		t.Fatalf("吊销代理失败: %v", err)
	}
	if code, _ := callAgent(t, AgentPing, token, `{}`); code != 403 {
		t.Fatalf("吊销后的令牌不应放行, got %d", code)
	}
}

// ============================================================================
// 租约超时:代理取走后失联,任务可被重新取走(不卡死在「打印中」)
// ============================================================================

func TestAgentPullLeaseExpiryReclaims(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	jobID := enqueueJob(t, 7, "192.168.1.100", "测试单据")

	// 第一次取单:任务被抢占。
	jobs := jobsOf(t, mustPull(t, token))
	if len(jobs) != 1 {
		t.Fatalf("应取到 1 条任务, got %d", len(jobs))
	}

	// 代理失联:把 claim_time 拨回 70 秒前(超过 60s 租约),且不给回执。
	oldClaim := time.Now().Add(-70 * time.Second).Format("2006-01-02 15:04:05")
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET claim_time=? WHERE job_id=?`, oldClaim, jobID); err != nil {
		t.Fatalf("回拨 claim_time 失败: %v", err)
	}

	// 再次取单:租约超时,同一任务应能被重新取走。
	jobs2 := jobsOf(t, mustPull(t, token))
	if len(jobs2) != 1 || jobs2[0]["jobId"] != float64(jobID) {
		t.Fatalf("租约超时后任务应被重新下发, got %v", jobs2)
	}
}

func mustPull(t *testing.T, token string) map[string]interface{} {
	t.Helper()
	code, resp := callAgent(t, AgentPull, token, `{"wait":0}`)
	if code != 200 {
		t.Fatalf("pull 应返回 200, got %d(%v)", code, resp)
	}
	return resp
}

// ============================================================================
// per-agent 授权域:代理只能取到自己名下的打印机任务
// ============================================================================

func TestAgentPullScopeContract(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "1") // 只授权打印机 1
	jobInScope := enqueueJob(t, 1, "192.168.1.101", "授权内单据")
	jobOutScope := enqueueJob(t, 2, "192.168.1.102", "授权外单据")

	jobs := jobsOf(t, mustPull(t, token))
	if len(jobs) != 1 {
		t.Fatalf("应只取到授权域内的 1 条任务, got %d 条", len(jobs))
	}
	if jobs[0]["jobId"] != float64(jobInScope) || jobs[0]["jobId"] == float64(jobOutScope) {
		t.Fatalf("取到的任务错误: got jobId=%v, want %d(授权内), 不应是 %d(授权外)",
			jobs[0]["jobId"], jobInScope, jobOutScope)
	}
}

// ============================================================================
// 长轮询:wait>0 且无任务时,后端 hold 到 deadline 再返回空队列
// ============================================================================

func TestAgentPullLongPollHoldsUntilDeadline(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")

	start := time.Now()
	code, resp := callAgent(t, AgentPull, token, `{"wait":1}`)
	elapsed := time.Since(start)

	if code != 200 {
		t.Fatalf("长轮询 pull 应返回 200, got %d(%v)", code, resp)
	}
	if len(jobsOf(t, resp)) != 0 {
		t.Fatalf("无任务时长轮询应返回空队列: %v", resp)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("wait=1 应 hold 约 1 秒后返回, 实际 %s", elapsed)
	}
}

// ============================================================================
// 请求体参数:limit 超出上限时按上限截断(agent 端依赖这个语义控制单轮流量)
// ============================================================================

func TestAgentPullLimitClamp(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")
	for i := 0; i < 12; i++ {
		enqueueJob(t, 7, "192.168.1.100", fmt.Sprintf("单据 %d", i))
	}

	// 请求体 limit=100(协议允许 agent 传任意值),后端应 clamp 到上限 50 以内;
	// ClaimPrintJobs 的默认上限策略保证不会无界取单。
	code, resp := callAgent(t, AgentPull, token, `{"limit":100,"wait":0}`)
	if code != 200 {
		t.Fatalf("pull 应返回 200, got %d(%v)", code, resp)
	}
	jobs := jobsOf(t, resp)
	if len(jobs) != 10 {
		t.Fatalf("limit=100 应被 clamp 到 10, got %d 条", len(jobs))
	}
}

// ============================================================================
// 自助升级:download 端点
// ============================================================================

// callAgentGetFull 以代理身份 GET 调用 handler,返回状态码、响应体与响应头。
func callAgentGetFull(t *testing.T, h gin.HandlerFunc, token, query string) (int, []byte, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/test?"+query, nil)
	if token != "" {
		req.Header.Set("X-Agent-Token", token)
	}
	req.Header.Set("X-Agent-Version", "2")
	c.Request = req
	h(c)
	return w.Code, w.Body.Bytes(), w.Header()
}

func TestAgentDownload(t *testing.T) {
	initAgentTestDB(t)
	_, token := newTestAgent(t, "")

	// 统一把产物目录指到临时目录,保证「无产物 → 404」的判定不受机器环境影响。
	dir := t.TempDir()
	t.Setenv(conf.EnvAgentBinDir, dir)

	// 1. 非法平台 / 目录穿越尝试 → 400(白名单挡在文件读取之前)
	for _, q := range []string{
		"goos=freebsd&goarch=amd64",
		"goos=linux&goarch=386",
		"goos=..%2F..&goarch=amd64",
		"goos=windows&goarch=arm64", // 发布矩阵目前没有 windows-arm64
	} {
		if code, _, _ := callAgentGetFull(t, AgentDownload, token, q); code != http.StatusBadRequest {
			t.Fatalf("query %q 应 400, got %d", q, code)
		}
	}

	// 2. 合法平台但服务端无产物 → 404
	if code, _, _ := callAgentGetFull(t, AgentDownload, token, "goos=linux&goarch=amd64"); code != http.StatusNotFound {
		t.Fatalf("无产物应 404, got %d", code)
	}

	// 3. 有产物 → 200 + 内容一致 + sha256 头匹配
	payload := []byte("fake print-agent binary v1.2.3")
	if err := os.WriteFile(filepath.Join(dir, "print-agent-linux-amd64"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	code, body, headers := callAgentGetFull(t, AgentDownload, token, "goos=linux&goarch=amd64")
	if code != http.StatusOK {
		t.Fatalf("有产物应 200, got %d", code)
	}
	if string(body) != string(payload) {
		t.Fatalf("响应体与产物不一致: got %q", snippetBytes(body))
	}
	if headers.Get("X-Agent-Sha256") != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 头不匹配: got %q", headers.Get("X-Agent-Sha256"))
	}
	if headers.Get("Content-Disposition") == "" {
		t.Fatal("缺少 Content-Disposition 头")
	}

	// 4. 无令牌 → 拒绝(未配置全局令牌时 403,与 ping/pull 一致)
	if code, _, _ := callAgentGetFull(t, AgentDownload, "", "goos=linux&goarch=amd64"); code != http.StatusForbidden {
		t.Fatalf("无令牌应 403, got %d", code)
	}
}

// snippetBytes 截取响应片段用于报错展示。
func snippetBytes(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
