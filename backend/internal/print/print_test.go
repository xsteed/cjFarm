package print

import (
	"bytes"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

func TestValidatePrinterAddr(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		port    int
		wantErr bool
	}{
		{"局域网打印机正常", "192.168.1.100", 9100, false},
		{"私网10段正常", "10.0.0.5", 9100, false},
		{"公网IP正常", "8.8.8.8", 9100, false},
		{"IPv4回环拒绝", "127.0.0.1", 9100, true},
		{"IPv6回环拒绝", "::1", 9100, true},
		{"未指定0.0.0.0拒绝", "0.0.0.0", 9100, true},
		{"云元数据地址拒绝", "169.254.169.254", 80, true},
		{"链路本地IPv6拒绝", "fe80::1", 9100, true},
		{"非法IP拒绝", "not-an-ip", 9100, true},
		{"空IP拒绝", "", 9100, true},
		{"带空格IP拒绝", "  127.0.0.1  ", 9100, true},
		{"端口0拒绝", "192.168.1.1", 0, true},
		{"端口越界拒绝", "192.168.1.1", 65536, true},
		{"负端口拒绝", "192.168.1.1", -1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePrinterAddr(c.ip, c.port)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePrinterAddr(%q,%d) err=%v, wantErr=%v", c.ip, c.port, err, c.wantErr)
			}
		})
	}
}

func TestGBKEncodesChineseAndReplacesUnsupportedRunes(t *testing.T) {
	plain := "凉拌青瓜"
	want, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(plain))
	if err != nil {
		t.Fatalf("纯中文 GBK 编码失败: %v", err)
	}
	if got := gbk(plain); !bytes.Equal(got, want) {
		t.Fatalf("纯中文 GBK 编码不应改变, got %v want %v", got, want)
	}

	withUnsupported := "🍣 拼盘 𠮷"
	got := gbk(withUnsupported)
	if !bytes.Contains(got, []byte("?")) {
		t.Fatalf("不可映射字符应被替换为 ?, got %v", got)
	}
	if _, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), got); err != nil {
		t.Fatalf("兜底结果必须仍是可解码的 GBK 字节: %v, bytes=%v", err, got)
	}
}

func TestAgentJobTTLDefaultsAndDocTypeMapping(t *testing.T) {
	clearAgentTTLEnv(t)

	if got := agentJobTTL(po.PrintDocKitchen); got != 120*time.Minute {
		t.Fatalf("厨房单默认 TTL = %v, want %v", got, 120*time.Minute)
	}
	if got := agentJobTTL(po.PrintDocGuest); got != 24*time.Hour {
		t.Fatalf("食客小票默认 TTL = %v, want %v", got, 24*time.Hour)
	}
	if got := agentJobTTL("test"); got != 30*time.Minute {
		t.Fatalf("测试/其它单默认 TTL = %v, want %v", got, 30*time.Minute)
	}
	if got := agentJobTTL("unknown"); got != 30*time.Minute {
		t.Fatalf("未知单据默认 TTL = %v, want %v", got, 30*time.Minute)
	}
}

func TestAgentJobTTLEnvOverride(t *testing.T) {
	setEnvForTest(t, "AGENT_JOB_TTL_KITCHEN_MIN", "5")
	setEnvForTest(t, "AGENT_JOB_TTL_GUEST_MIN", "6")
	setEnvForTest(t, "AGENT_JOB_TTL_TEST_MIN", "7")

	if got := agentJobTTL(po.PrintDocKitchen); got != 5*time.Minute {
		t.Fatalf("厨房单 env TTL = %v, want %v", got, 5*time.Minute)
	}
	if got := agentJobTTL(po.PrintDocGuest); got != 6*time.Minute {
		t.Fatalf("食客小票 env TTL = %v, want %v", got, 6*time.Minute)
	}
	if got := agentJobTTL("test"); got != 7*time.Minute {
		t.Fatalf("测试单 env TTL = %v, want %v", got, 7*time.Minute)
	}
}

func TestAgentJobTTLDisableWhenNonPositive(t *testing.T) {
	setEnvForTest(t, "AGENT_JOB_TTL_KITCHEN_MIN", "0")
	setEnvForTest(t, "AGENT_JOB_TTL_GUEST_MIN", "-1")
	setEnvForTest(t, "AGENT_JOB_TTL_TEST_MIN", "0")

	if got := agentJobTTL(po.PrintDocKitchen); got != 0 {
		t.Fatalf("厨房单 TTL=0 应关闭过期, got %v", got)
	}
	if got := agentJobTTL(po.PrintDocGuest); got != 0 {
		t.Fatalf("食客小票 TTL<=0 应关闭过期, got %v", got)
	}
	if got := agentJobTTL("test"); got != 0 {
		t.Fatalf("测试单 TTL=0 应关闭过期, got %v", got)
	}
}

func TestAgentExpireCutoffsFormatAndOffset(t *testing.T) {
	clearAgentTTLEnv(t)
	before := time.Now()
	kitchen, guest, other := agentExpireCutoffs()
	after := time.Now()

	kitchenTime := mustParseAgentTime(t, kitchen)
	if kitchenTime.Before(before.Add(-120*time.Minute-2*time.Second)) || kitchenTime.After(after.Add(-120*time.Minute+2*time.Second)) {
		t.Fatalf("厨房单 cutoff = %s, 不在 now-120min 的允许误差内", kitchen)
	}
	if guest == "" || other == "" {
		t.Fatalf("默认 TTL 开启时 cutoff 不应为空: guest=%q other=%q", guest, other)
	}
	mustParseAgentTime(t, guest)
	mustParseAgentTime(t, other)
}

func TestAgentExpireCutoffsEmptyWhenTTLDisabled(t *testing.T) {
	setEnvForTest(t, "AGENT_JOB_TTL_KITCHEN_MIN", "0")
	setEnvForTest(t, "AGENT_JOB_TTL_GUEST_MIN", "0")
	setEnvForTest(t, "AGENT_JOB_TTL_TEST_MIN", "0")

	kitchen, guest, other := agentExpireCutoffs()
	if kitchen != "" || guest != "" || other != "" {
		t.Fatalf("TTL=0 时 cutoff 应为空, got kitchen=%q guest=%q other=%q", kitchen, guest, other)
	}
}

func TestAgentSeenWithin(t *testing.T) {
	if !agentSeenWithin(time.Now().Add(-10*time.Second).Format(conf.TimeLayout), agentOnlineWindow) {
		t.Fatalf("90 秒窗口内的心跳应视为在线")
	}
	for _, seen := range []string{"", "not-time", time.Now().Add(-2 * agentOnlineWindow).Format(conf.TimeLayout)} {
		if agentSeenWithin(seen, agentOnlineWindow) {
			t.Fatalf("心跳 %q 应视为离线", seen)
		}
	}
}

func TestAnyAgentOnlineIncludesLegacyAndPerAgent(t *testing.T) {
	t.Run("legacy 在线", func(t *testing.T) {
		setupTestDB(t)
		resetAgentHeartbeatForTest(t)
		if err := dao.SetSetting("agent_last_seen", time.Now().Format(conf.TimeLayout)); err != nil {
			t.Fatalf("写入 legacy 心跳失败: %v", err)
		}
		if !AnyAgentOnline() {
			t.Fatalf("legacy 心跳在线时 AnyAgentOnline 应为 true")
		}
	})

	t.Run("per-agent 在线", func(t *testing.T) {
		setupTestDB(t)
		resetAgentHeartbeatForTest(t)
		if err := dao.SetSetting("agent_last_seen", ""); err != nil {
			t.Fatalf("清理 legacy 心跳失败: %v", err)
		}
		agent, _, err := dao.InsertPrintAgent("厨房代理", "")
		if err != nil {
			t.Fatalf("插入代理失败: %v", err)
		}
		if err := dao.TouchPrintAgent(agent.AgentID, "agent|test"); err != nil {
			t.Fatalf("写入 per-agent 心跳失败: %v", err)
		}
		if !AnyAgentOnline() {
			t.Fatalf("启用代理最近心跳在线时 AnyAgentOnline 应为 true")
		}
	})

	t.Run("远心跳或吊销均离线", func(t *testing.T) {
		setupTestDB(t)
		resetAgentHeartbeatForTest(t)
		if err := dao.SetSetting("agent_last_seen", ""); err != nil {
			t.Fatalf("清理 legacy 心跳失败: %v", err)
		}
		agent, _, err := dao.InsertPrintAgent("厨房代理", "")
		if err != nil {
			t.Fatalf("插入代理失败: %v", err)
		}
		oldSeen := time.Now().Add(-2 * agentOnlineWindow).Format(conf.TimeLayout)
		if _, err := store.DB.Exec(`UPDATE tb_print_agent SET last_seen=? WHERE agent_id=?`, oldSeen, agent.AgentID); err != nil {
			t.Fatalf("写入过期 per-agent 心跳失败: %v", err)
		}
		if AnyAgentOnline() {
			t.Fatalf("仅有过期 per-agent 心跳时 AnyAgentOnline 应为 false")
		}
		if err := dao.TouchPrintAgent(agent.AgentID, "agent|test"); err != nil {
			t.Fatalf("写入 per-agent 心跳失败: %v", err)
		}
		if err := dao.SetPrintAgentStatus(agent.AgentID, 0); err != nil {
			t.Fatalf("吊销代理失败: %v", err)
		}
		if AnyAgentOnline() {
			t.Fatalf("代理已吊销时即使最近心跳也应为 false")
		}
	})
}

func TestClaimPrintJobsSkipsExpiredJobs(t *testing.T) {
	clearAgentTTLEnv(t)
	setupTestDB(t)

	expiredID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, time.Now().Add(-3*time.Hour).Format(conf.TimeLayout))
	freshID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, time.Now().Add(-time.Minute).Format(conf.TimeLayout))
	kitchenCutoff, guestCutoff, otherCutoff := agentExpireCutoffs()

	jobs, err := dao.ClaimPrintJobs("agent-test", 10, agentClaimLeaseSeconds, kitchenCutoff, guestCutoff, otherCutoff, nil)
	if err != nil {
		t.Fatalf("ClaimPrintJobs 失败: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != freshID {
		t.Fatalf("应只取到未过期任务 fresh=%d, expired=%d, got %#v", freshID, expiredID, jobs)
	}
}

func TestClaimPrintJobsScopesByPrinterIDs(t *testing.T) {
	setupTestDB(t)
	ownedID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, store.Now())
	foreignID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, store.Now())
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET printer_id=2 WHERE job_id=?`, foreignID); err != nil {
		t.Fatalf("准备测试数据失败: %v", err)
	}

	// 授权域 [1] 的代理只能 claim 到打印机 1 的任务;域外任务必须保持 pending,
	// 否则错配 claim 会消耗租约与 attempts(3 次错配即可把任务打到 dead)。
	jobs, err := dao.ClaimPrintJobs("agent-test", 10, agentClaimLeaseSeconds, "", "", "", []int{1})
	if err != nil {
		t.Fatalf("ClaimPrintJobs 失败: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != ownedID {
		t.Fatalf("授权域 [1] 应只取到 owned=%d, got %#v", ownedID, jobs)
	}
	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_print_job WHERE job_id=?`, foreignID).Scan(&status); err != nil {
		t.Fatalf("查询域外任务失败: %v", err)
	}
	if status != po.PrintJobPending {
		t.Fatalf("域外任务不应被 claim, status=%d, want=%d", status, po.PrintJobPending)
	}
}

func TestRetryPrintJobReturnsPendingBeforeMaxAttempts(t *testing.T) {
	setupTestDB(t)
	jobID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, store.Now())
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, attempts=?, claimed_by=?, claim_time=? WHERE job_id=?`,
		po.PrintJobClaimed, po.PrintJobMaxAttempts-1, "agent-test", "2026-09-22 10:00:00", jobID); err != nil {
		t.Fatalf("准备测试任务失败: %v", err)
	}

	retrying, err := dao.RetryPrintJob(jobID, "打印机缺纸", 10)
	if err != nil {
		t.Fatalf("RetryPrintJob 失败: %v", err)
	}
	if !retrying {
		t.Fatalf("attempts=%d 时应回到 pending 并继续重试", po.PrintJobMaxAttempts-1)
	}

	var status int
	var lastError, claimedBy, claimTime, nextTryTime, doneTime string
	if err := store.DB.QueryRow(`SELECT status, last_error, COALESCE(claimed_by,''), COALESCE(claim_time,''), COALESCE(next_try_time,''), COALESCE(done_time,'')
		FROM tb_print_job WHERE job_id=?`, jobID).Scan(&status, &lastError, &claimedBy, &claimTime, &nextTryTime, &doneTime); err != nil {
		t.Fatalf("读取测试任务失败: %v", err)
	}
	if status != po.PrintJobPending {
		t.Fatalf("任务状态 = %d, want pending(%d)", status, po.PrintJobPending)
	}
	if lastError != "打印机缺纸" {
		t.Fatalf("last_error = %q, want %q", lastError, "打印机缺纸")
	}
	if nextTryTime == "" {
		t.Fatalf("pending 分支应设置 next_try_time")
	}
	if claimedBy != "" || claimTime != "" {
		t.Fatalf("pending 分支应清空 claim 信息, got claimed_by=%q claim_time=%q", claimedBy, claimTime)
	}
	if doneTime != "" {
		t.Fatalf("pending 分支不应设置 done_time, got %q", doneTime)
	}
}

func TestRetryPrintJobMarksDeadAtMaxAttempts(t *testing.T) {
	setupTestDB(t)
	jobID := insertAgentPrintJobForTest(t, po.PrintDocKitchen, store.Now())
	oldNextTry := "2026-09-22 10:01:00"
	oldClaimTime := "2026-09-22 10:00:00"
	if _, err := store.DB.Exec(`UPDATE tb_print_job SET status=?, attempts=?, claimed_by=?, claim_time=?, next_try_time=? WHERE job_id=?`,
		po.PrintJobClaimed, po.PrintJobMaxAttempts, "agent-test", oldClaimTime, oldNextTry, jobID); err != nil {
		t.Fatalf("准备测试任务失败: %v", err)
	}

	retrying, err := dao.RetryPrintJob(jobID, "持续离线", 10)
	if err != nil {
		t.Fatalf("RetryPrintJob 失败: %v", err)
	}
	if retrying {
		t.Fatalf("attempts=%d 时应置为 dead,不再重试", po.PrintJobMaxAttempts)
	}

	var status int
	var lastError, claimedBy, claimTime, nextTryTime, doneTime string
	if err := store.DB.QueryRow(`SELECT status, last_error, COALESCE(claimed_by,''), COALESCE(claim_time,''), COALESCE(next_try_time,''), COALESCE(done_time,'')
		FROM tb_print_job WHERE job_id=?`, jobID).Scan(&status, &lastError, &claimedBy, &claimTime, &nextTryTime, &doneTime); err != nil {
		t.Fatalf("读取测试任务失败: %v", err)
	}
	if status != po.PrintJobDead {
		t.Fatalf("任务状态 = %d, want dead(%d)", status, po.PrintJobDead)
	}
	if lastError != "持续离线" {
		t.Fatalf("last_error = %q, want %q", lastError, "持续离线")
	}
	if doneTime == "" {
		t.Fatalf("dead 分支应设置 done_time")
	}
	if nextTryTime != oldNextTry || claimedBy != "agent-test" || claimTime != oldClaimTime {
		t.Fatalf("dead 分支应保留重试与 claim 信息, got next_try_time=%q claimed_by=%q claim_time=%q", nextTryTime, claimedBy, claimTime)
	}
}

func TestPrinterStatusSummary(t *testing.T) {
	var nilStatus *PrinterStatus
	if got := nilStatus.Summary(); got != "" {
		t.Fatalf("nil Summary = %q, want empty", got)
	}
	if got := (&PrinterStatus{Queried: true}).Summary(); got != "" {
		t.Fatalf("正常状态 Summary = %q, want empty", got)
	}
	got := (&PrinterStatus{PaperOut: true, CoverOpen: true}).Summary()
	if !strings.Contains(got, "缺纸") || !strings.Contains(got, "盖板开") {
		t.Fatalf("异常摘要缺少状态: %q", got)
	}
	if strings.Index(got, "缺纸") > strings.Index(got, "盖板开") {
		t.Fatalf("异常摘要顺序错误: %q", got)
	}
}

// TestAckAgentJobRejectsForeignClaim 归属校验(高危回归):v2 代理不能回执他人
// 取走的任务,防止代理 A 把代理 B 的任务标记为已打印(商家看到成功、实际没出纸)。
func TestAckAgentJobRejectsForeignClaim(t *testing.T) {
	setupTestDB(t)
	logID := insertAgentPrintLogForTest(t, store.Now())
	jobID := insertAgentPrintJobWithLogForTest(t, po.PrintDocKitchen, store.Now(), logID)
	if claimed, err := dao.ClaimPrintJobs("agent-A", 10, agentClaimLeaseSeconds, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}

	err := AckAgentJob("agent-B", jobID, true, "", &PrinterStatus{})
	if err == nil || !strings.Contains(err.Error(), "任务由其他代理持有") {
		t.Fatalf("他人任务回执应被拒, got %v", err)
	}
	// 任务仍处于 claimed,不被越权结案。
	job, _ := service.LoadPrintJob(jobID)
	if job.Status != po.PrintJobClaimed {
		t.Fatalf("越权回执不应改变任务状态, got %d", job.Status)
	}
}

func TestAckAgentJobAppendsPrinterStatusAndCost(t *testing.T) {
	setupTestDB(t)
	createTime := time.Now().Add(-2 * time.Second).Format(conf.TimeLayout)
	logID := insertAgentPrintLogForTest(t, createTime)
	jobID := insertAgentPrintJobWithLogForTest(t, po.PrintDocKitchen, createTime, logID)
	if claimed, err := dao.ClaimPrintJobs("agent-test", 10, agentClaimLeaseSeconds, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}

	err := AckAgentJob("", jobID, true, "", &PrinterStatus{
		Queried:  true,
		Raw:      "0204/0400",
		PaperOut: true,
	})
	if err != nil {
		t.Fatalf("AckAgentJob 成功回执失败: %v", err)
	}

	var status, costMs int
	var detail string
	if err := store.DB.QueryRow(`SELECT status, detail, cost_ms FROM tb_print_log WHERE print_id=?`, logID).Scan(&status, &detail, &costMs); err != nil {
		t.Fatalf("读取打印日志失败: %v", err)
	}
	if status != po.PrintStatusSuccess {
		t.Fatalf("日志状态 = %d, want success(%d)", status, po.PrintStatusSuccess)
	}
	if !strings.Contains(detail, "打印机报告") || !strings.Contains(detail, "缺纸") || !strings.Contains(detail, "0204/0400") {
		t.Fatalf("日志 detail 未包含打印机状态与 raw: %q", detail)
	}
	if costMs <= 0 {
		t.Fatalf("成功回执应回写出纸耗时, got %d", costMs)
	}
}

func TestAckAgentJobFailureKeepsRetryBranch(t *testing.T) {
	setupTestDB(t)
	logID := insertAgentPrintLogForTest(t, store.Now())
	jobID := insertAgentPrintJobWithLogForTest(t, po.PrintDocKitchen, store.Now(), logID)
	if claimed, err := dao.ClaimPrintJobs("agent-test", 10, agentClaimLeaseSeconds, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}

	if err := AckAgentJob("", jobID, false, "打印机离线", &PrinterStatus{PaperOut: true}); err != nil {
		t.Fatalf("AckAgentJob 失败回执失败: %v", err)
	}

	var status, costMs int
	var detail string
	if err := store.DB.QueryRow(`SELECT status, detail, cost_ms FROM tb_print_log WHERE print_id=?`, logID).Scan(&status, &detail, &costMs); err != nil {
		t.Fatalf("读取打印日志失败: %v", err)
	}
	if status != po.PrintStatusQueued {
		t.Fatalf("失败未达上限时日志应保持排队中, got %d", status)
	}
	if !strings.Contains(detail, "已安排重试") || !strings.Contains(detail, "打印机离线") {
		t.Fatalf("失败回执应走重试文案, got %q", detail)
	}
	if costMs != 0 {
		t.Fatalf("失败分支不应回写 cost_ms, got %d", costMs)
	}
}

func TestAckAgentJobV1CompatibleNilPrinterStatus(t *testing.T) {
	setupTestDB(t)
	createTime := time.Now().Add(-2 * time.Second).Format(conf.TimeLayout)
	logID := insertAgentPrintLogForTest(t, createTime)
	jobID := insertAgentPrintJobWithLogForTest(t, po.PrintDocKitchen, createTime, logID)
	if claimed, err := dao.ClaimPrintJobs("agent-test", 10, agentClaimLeaseSeconds, "", "", "", nil); err != nil || len(claimed) != 1 {
		t.Fatalf("取单失败: %+v err=%v", claimed, err)
	}

	if err := AckAgentJob("", jobID, true, "", nil); err != nil {
		t.Fatalf("AckAgentJob v1 nil 状态不应失败: %v", err)
	}

	var detail string
	if err := store.DB.QueryRow(`SELECT detail FROM tb_print_log WHERE print_id=?`, logID).Scan(&detail); err != nil {
		t.Fatalf("读取打印日志失败: %v", err)
	}
	if strings.Contains(detail, "打印机报告") {
		t.Fatalf("v1 nil printerStatus 不应追加状态摘要, got %q", detail)
	}
}

func clearAgentTTLEnv(t *testing.T) {
	t.Helper()
	setEnvForTest(t, "AGENT_JOB_TTL_KITCHEN_MIN", "")
	setEnvForTest(t, "AGENT_JOB_TTL_GUEST_MIN", "")
	setEnvForTest(t, "AGENT_JOB_TTL_TEST_MIN", "")
}

func resetAgentHeartbeatForTest(t *testing.T) {
	t.Helper()
	atomic.StoreInt64(&agentLastSeenNano, 0)
	atomic.StoreInt64(&agentSeenPersistNano, 0)
	perAgentSeenMu.Lock()
	perAgentSeenNano = map[int]int64{}
	perAgentSeenMu.Unlock()
	t.Cleanup(func() {
		atomic.StoreInt64(&agentLastSeenNano, 0)
		atomic.StoreInt64(&agentSeenPersistNano, 0)
		perAgentSeenMu.Lock()
		perAgentSeenNano = map[int]int64{}
		perAgentSeenMu.Unlock()
	})
}

func setEnvForTest(t *testing.T, key, value string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if value == "" {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("清理环境变量 %s 失败: %v", key, err)
		}
	} else if err := os.Setenv(key, value); err != nil {
		t.Fatalf("设置环境变量 %s 失败: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func mustParseAgentTime(t *testing.T, s string) time.Time {
	t.Helper()
	got, err := time.ParseInLocation(conf.TimeLayout, s, time.Local)
	if err != nil {
		t.Fatalf("时间格式错误 %q: %v", s, err)
	}
	return got
}

func insertAgentPrintJobForTest(t *testing.T, docType, createTime string) int {
	t.Helper()
	return insertAgentPrintJobWithLogForTest(t, docType, createTime, 1)
}

func insertAgentPrintJobWithLogForTest(t *testing.T, docType, createTime string, logID int) int {
	t.Helper()
	id, err := dao.InsertPrintJob(po.PrintJob{
		PrinterID:   1,
		PrinterName: "测试打印机",
		PrinterType: po.PrinterTypeKitchen,
		IP:          "192.168.1.100",
		Port:        9100,
		DocType:     docType,
		OrderID:     1,
		OrderNo:     "T202609220001",
		TableNo:     "01",
		Copies:      1,
		PrintLogID:  logID,
		Payload:     "测试内容",
		TriggerBy:   po.PrintTriggerOrder,
		Operator:    "test",
		CreateTime:  createTime,
	})
	if err != nil {
		t.Fatalf("插入测试打印任务失败: %v", err)
	}
	return id
}

func insertAgentPrintLogForTest(t *testing.T, createTime string) int {
	t.Helper()
	id, err := dao.InsertPrintLogReturningID(po.PrintLog{
		OrderID:     1,
		OrderNo:     "T202609220001",
		TableNo:     "01",
		PrinterID:   1,
		PrinterName: "测试打印机",
		PrinterType: po.PrinterTypeKitchen,
		Provider:    po.PrinterProviderAgent,
		DocType:     po.PrintDocKitchen,
		Copies:      1,
		Status:      po.PrintStatusQueued,
		Detail:      "已入队,等待门店打印代理取单",
		TriggerBy:   po.PrintTriggerOrder,
		Operator:    "test",
		CreateTime:  createTime,
	})
	if err != nil {
		t.Fatalf("插入测试打印日志失败: %v", err)
	}
	return id
}
