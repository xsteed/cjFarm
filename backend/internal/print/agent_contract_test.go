package print

// ============================================================================
// 打印代理下发契约测试(云后端侧,print 包层)
//
// 锁死两件事:
//   1. PullAgentJobs 下发的每条任务 JSON 字段集合,与门店侧 print-agent 的
//      agentJob 结构(print-agent/protocol_test.go 中的同名契约常量)完全一致;
//   2. deliveryId 在「租约超时重新下发」时保持不变 —— 这是 agent 端幂等去重的地基。
// 任一端改动协议字段,对应测试立即失败,防止两端漂移。
// ============================================================================

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// contractPullJobKeys 与 print-agent/protocol_test.go 中的同名常量互为镜像:
// 这是 pull 响应里每条任务的字段契约。
var contractPullJobKeys = []string{
	"jobId", "printerId", "printerName", "ip", "port",
	"copies", "docType", "orderNo", "tableNo", "deliveryId", "payload",
}

// enqueueContractJob 入队一条待打印任务,返回 jobID。
func enqueueContractJob(t *testing.T, printerID int, ip string, copies int, payload string) int {
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
		Copies:      copies,
		Payload:     payload,
		TriggerBy:   "测试",
		CreateTime:  store.Now(),
	})
	if err != nil {
		t.Fatalf("入队打印任务失败: %v", err)
	}
	return jobID
}

func assertJobKeysExact(t *testing.T, m map[string]interface{}, want []string) {
	t.Helper()
	got := map[string]bool{}
	for k := range m {
		got[k] = true
	}
	for _, k := range want {
		if !got[k] {
			t.Fatalf("缺少契约字段 %q,实际字段: %v", k, keysOfAgent(m))
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

func keysOfAgent(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// pullJSON 取单并序列化,返回任务 JSON 对象列表。
func pullJSON(t *testing.T, agentID string, limit int) []map[string]interface{} {
	t.Helper()
	jobs, err := PullAgentJobs(agentID, limit, nil)
	if err != nil {
		t.Fatalf("PullAgentJobs 失败: %v", err)
	}
	raw, err := json.Marshal(jobs)
	if err != nil {
		t.Fatalf("序列化任务失败: %v", err)
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("反序列化任务失败: %v", err)
	}
	return list
}

// TestPullAgentJobsJSONContract 下发任务的字段集合与字节编码与 agent 端完全对齐。
func TestPullAgentJobsJSONContract(t *testing.T) {
	setupTestDB(t)
	jobID := enqueueContractJob(t, 7, "192.168.1.100", 2, "凉拌青瓜\n米饭")

	jobs := pullJSON(t, "测试代理", 10)
	if len(jobs) != 1 {
		t.Fatalf("应取到 1 条任务, got %d", len(jobs))
	}
	job := jobs[0]
	assertJobKeysExact(t, job, contractPullJobKeys)

	if job["jobId"] != float64(jobID) {
		t.Fatalf("jobId 错误: %v", job["jobId"])
	}
	if job["printerId"] != float64(7) || job["printerName"] != "后厨打印机" {
		t.Fatalf("打印机信息错误: %v", job)
	}
	if job["ip"] != "192.168.1.100" || job["port"] != float64(9100) {
		t.Fatalf("地址错误(port 0 应兜底 9100): ip=%v port=%v", job["ip"], job["port"])
	}
	if job["copies"] != float64(2) || job["docType"] != "kitchen" ||
		job["orderNo"] != "NO20260922001" || job["tableNo"] != "T1" {
		t.Fatalf("单据字段错误: %v", job)
	}
	if len(job["deliveryId"].(string)) != 32 {
		t.Fatalf("deliveryId 应为 32 字符 hex: %v", job["deliveryId"])
	}

	// payload 是 base64 的 ESC/POS:ESC @ 开头、GS V 切纸结尾(agent 端原样写入打印机)。
	raw, err := base64.StdEncoding.DecodeString(job["payload"].(string))
	if err != nil {
		t.Fatalf("payload 不是合法 base64: %v", err)
	}
	if len(raw) < 6 || raw[0] != 0x1B || raw[1] != 0x40 {
		t.Fatalf("payload 应以 ESC @ 开头, got %x", raw)
	}
	if len(raw) < 4 || raw[len(raw)-4] != 0x1D || raw[len(raw)-3] != 0x56 ||
		raw[len(raw)-2] != 0x42 || raw[len(raw)-1] != 0x00 {
		t.Fatalf("payload 应以 GS V 切纸结尾, got %x", raw[len(raw)-8:])
	}
}

// TestPullAgentJobsDeliveryIDStable 租约超时重发时 deliveryId 保持不变,
// 否则 agent 端的幂等去重会失效(同一张票打两遍)。
func TestPullAgentJobsDeliveryIDStable(t *testing.T) {
	setupTestDB(t)
	jobID := enqueueContractJob(t, 7, "192.168.1.100", 1, "幂等测试")

	first := pullJSON(t, "测试代理", 10)
	if len(first) != 1 {
		t.Fatalf("第一次取单应拿到任务, got %d", len(first))
	}

	// 模拟代理失联:把 claim_time 拨回 70 秒前(超过 60s 租约),任务重新可抢。
	oldClaim := time.Now().Add(-70 * time.Second).Format("2006-01-02 15:04:05")
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET claim_time=? WHERE job_id=?`, oldClaim, jobID); err != nil {
		t.Fatalf("回拨 claim_time 失败: %v", err)
	}

	second := pullJSON(t, "测试代理", 10)
	if len(second) != 1 {
		t.Fatalf("租约超时后任务应被重新下发, got %d", len(second))
	}
	if second[0]["deliveryId"] != first[0]["deliveryId"] {
		t.Fatalf("重发任务 deliveryId 必须保持不变(幂等凭据):\n first  %v\n second %v",
			first[0]["deliveryId"], second[0]["deliveryId"])
	}
	if second[0]["jobId"] != first[0]["jobId"] {
		t.Fatalf("重发任务 jobId 必须不变: %v vs %v", first[0]["jobId"], second[0]["jobId"])
	}
}
