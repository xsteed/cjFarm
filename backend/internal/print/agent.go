// 本地打印代理通道(provider=agent)。
//
// 解决什么问题:后端部署在云服务器时,「直连 IP:9100」这条路径根本够不到门店内网的
// 打印机(见 escpos.go 顶部说明),换飞鹅云又要买新机器。本通道让门店里已有的
// 网络热敏机继续可用:门店内网常驻一个代理小程序,由它**出站**连云端拉单,
// 再往内网 `打印机IP:9100` 直发 ESC/POS 字节。
//
//	云后端 ──入队(tb_print_job)──> 门店代理 ──TCP 打印机IP:9100──> 打印机
//	        HTTPS 出站轮询,门店无需公网 IP、无需端口映射、无需 VPN
//
// 关键设计(为什么这么切):
//  1. 门店侧不需要任何驱动。9100 是 RAW/JetDirect 端口,打印机在 TCP 层直接收
//     ESC/POS 字节流,不经过操作系统打印队列 —— 这一点与直连通道完全一致,
//     只是「谁来连打印机」从云服务器换成了门店内网的代理;
//  2. 代理程序保持「傻管道」:ESC/POS 字节(含 GBK 编码与切纸指令)由后端在取单时
//     现场编码、以 base64 下发,代理只做 base64 解码 + 写 socket。协议一旦有变,
//     只改后端,不必逐台门店更新代理;
//  3. 出站轮询而非长连接:门店网络(NAT/4G/运营商)不需要任何入站放行,实现也简单,
//     断网恢复后自动续上——顺带获得「打印机离线不丢单」。
//
// 部署与排障见 docs/print-agent.md。
package print

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"dining-system/internal/logger"
	"dining-system/internal/model"
	"dining-system/internal/store"
)

// agentClaimLeaseSeconds 取单租约(秒)。
// 代理取走任务后若在租约内没有回执(进程被杀/断电/断网),任务会被重新放回队列。
// 60 秒足够覆盖「打印中卡纸」这类慢路径,又不会让积压任务等太久。
const agentClaimLeaseSeconds = 60

// agentRetryBackoffSeconds 单条任务失败后的退避时间(秒)。
const agentRetryBackoffSeconds = 10

// jobPayloadMaxChars 单条任务的文本行长度上限。
// 约合 80mm 小票 75 行;超出时按行拆成多条任务依次下发(与飞鹅云超长内容分段推送同一策略)。
const jobPayloadMaxChars = 3600

// ============================================================================
// 令牌与心跳
// ============================================================================

// AgentConfigured 报告代理通道是否可用(令牌已配置)。
func AgentConfigured() bool { return strings.TrimSpace(store.GetCfg("agent_token")) != "" }

// VerifyAgentToken 校验代理提交的令牌(恒定时间比较,避免长度/前缀被逐字节探测)。
func VerifyAgentToken(tok string) bool {
	want := strings.TrimSpace(store.GetCfg("agent_token"))
	if want == "" {
		return false
	}
	got := strings.TrimSpace(tok)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// 代理心跳:内存即时值 + 限流落库双轨。
//
// 内存值保证「在线判定」零延迟(管理端每次查都走这里);落库值解决两个内存方案
// 解决不了的问题:
//   - 后端重启后内存清零,代理没来得及再轮询的那几秒会误报离线;
//   - 多实例部署(MySQL + 负载均衡)下代理可能轮询到不同实例,纯内存会导致
//     「同一时刻在 A 实例在线、在 B 实例离线」的错乱。
//
// 为避免高频写库(代理每 3 秒轮询一次),落库做 15 秒限流。
var (
	agentLastSeenNano    int64 // 内存中的最近心跳(纳秒)
	agentSeenPersistNano int64 // 上次落库的纳秒时间,用于 15s 限流
)

// agentSeenPersistInterval 心跳落库间隔:15 秒写一次,兼顾「跨实例一致」与「不写爆库」。
const agentSeenPersistInterval = 15 * time.Second

// MarkAgentSeen 记录一次代理心跳(内存即时 + 限流落库)。
func MarkAgentSeen() {
	now := time.Now()
	nowNano := now.UnixNano()
	atomic.StoreInt64(&agentLastSeenNano, nowNano)

	last := atomic.LoadInt64(&agentSeenPersistNano)
	if nowNano-last >= int64(agentSeenPersistInterval) &&
		atomic.CompareAndSwapInt64(&agentSeenPersistNano, last, nowNano) {
		// 心跳是状态值不是配置项:AgentConfig 无关、不会回显、也不会被配置保存覆盖。
		if err := store.SetCfg("agent_last_seen", now.Format("2006-01-02 15:04:05")); err != nil {
			logger.Warnf("打印: 心跳落库失败: %v", err)
		}
	}
}

// AgentLastSeen 返回最近心跳时间;从未收到过返回零值。
//
// 优先读内存(即时);内存为空时回退读库 —— 覆盖「后端刚重启 / 多实例下心跳在别的实例」。
func AgentLastSeen() time.Time {
	if v := atomic.LoadInt64(&agentLastSeenNano); v != 0 {
		return time.Unix(0, v)
	}
	if s := strings.TrimSpace(store.GetCfg("agent_last_seen")); s != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// agentOnlineWindow 判定「代理在线」的时间窗。
// 取轮询间隔(默认 3 秒)的数十倍,允许门店网络抖动几次不误报离线。
const agentOnlineWindow = 90 * time.Second

// AgentOnline 报告代理是否仍在正常轮询。
func AgentOnline() bool {
	seen := AgentLastSeen()
	return !seen.IsZero() && time.Since(seen) < agentOnlineWindow
}

// probeAgent 检查代理通道是否可用。
//
// 云端永远连不到门店内网,因此这里能回答的不是「打印机通不通」,而是
// 「代理还在不在轮询 + 有多少单积压」——这已经足够区分「没人跑代理」与「打印机坏了」。
func probeAgent(p model.Printer) (string, error) {
	if !AgentConfigured() {
		return "", errors.New("本地打印代理未启用:请到「系统配置 → 小票打印」填写代理令牌,并在门店内网运行代理程序")
	}
	pending := store.PendingJobCountByPrinter(p.PrinterID)
	seen := AgentLastSeen()
	if seen.IsZero() {
		return "", fmt.Errorf("云端尚未收到任何代理心跳(门店内需运行 print-agent);本机当前积压 %d 单", pending)
	}
	if !AgentOnline() {
		return "", fmt.Errorf("打印代理已离线(最近心跳 %s);本机当前积压 %d 单",
			seen.Format("2006-01-02 15:04:05"), pending)
	}
	return fmt.Sprintf("本地打印代理在线(最近心跳 %s);本机积压 %d 单",
		seen.Format("15:04:05"), pending), nil
}

// ============================================================================
// 入队
// ============================================================================

// enqueueAgentJob 把一张订单票据写入代理队列(替代直连的「立刻发出去」)。
func enqueueAgentJob(j job) error {
	return enqueueAgentTicket(j.p, j.render(), j.docType, j.o, j.triggerBy, j.operator)
}

// enqueueAgentTicket 渲染结果入队的公共实现(订单票据与测试页共用)。
//
// 顺序很重要:先记打印日志拿到 log_id,再带着 log_id 入队。
// 这样即便入队那步失败,商家在「打印日志」里也能看到一条失败记录,
// 而不是「下单了、什么都没发生、日志里也查不到」。
func enqueueAgentTicket(p model.Printer, lines []string, docType string,
	o model.Order, triggerBy, operator string) error {

	copies := p.EffectiveCopies()
	logID, err := store.InsertPrintLogReturningID(model.PrintLog{
		OrderID:     o.OrderID,
		OrderNo:     o.OrderNo,
		TableNo:     o.TableNo,
		TableName:   o.TableName,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		Provider:    model.PrinterProviderAgent,
		DocType:     docType,
		Copies:      copies,
		Status:      model.PrintStatusQueued,
		Detail:      "已入队,等待门店打印代理取单",
		TriggerBy:   triggerBy,
		Operator:    operator,
		CreateTime:  store.Now(),
	})
	if err != nil {
		logger.Warnf("打印: 写打印日志失败(仍继续入队): %v", err)
	}

	chunks := splitJobPayload(lines)
	for i, chunk := range chunks {
		if _, err := store.InsertPrintJob(model.PrintJob{
			PrinterID:   p.PrinterID,
			PrinterName: p.PrinterName,
			PrinterType: p.PrinterType,
			IP:          strings.TrimSpace(p.IP),
			Port:        effectivePort(p.Port),
			DocType:     docType,
			OrderID:     o.OrderID,
			OrderNo:     o.OrderNo,
			TableNo:     o.TableNo,
			Copies:      copies,
			PrintLogID:  logID,
			Payload:     chunk,
			TriggerBy:   triggerBy,
			Operator:    operator,
			CreateTime:  store.Now(),
		}); err != nil {
			logger.Warnf("打印: 入队失败(第 %d/%d 段): %v", i+1, len(chunks), err)
			store.UpdatePrintLogResult(logID, model.PrintStatusFailed, "入队失败: "+err.Error())
			return err
		}
	}
	logger.Infof("打印: 已入队 %d 段,等待代理取单 -> 打印机[%s](%s) 单据 %s(%s)",
		len(chunks), p.PrinterName, providerAddr(p), o.OrderNo, docType)
	return nil
}

// splitJobPayload 把文本行按长度上限切成多段(每段是一份可直接打印的连续票据)。
// 单行本身超限时无法再切(票据宽度决定了它不会发生),按原样放行。
func splitJobPayload(lines []string) []string {
	out := []string{}
	cur := ""
	for _, l := range lines {
		next := l
		if cur != "" {
			next = cur + "\n" + l
		}
		if len(next) > jobPayloadMaxChars && cur != "" {
			out = append(out, cur)
			cur = l
			continue
		}
		cur = next
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// ClearAgentQueue 清空某台打印机的代理队列(代理掉线、IP 填错时止损)。
//
// 与飞鹅云的「清空队列」语义对齐:已打印出来的单据不受影响,只丢弃还没送出的。
// 同时把对应的打印日志改为失败,否则日志会永远停在「排队中」。
func ClearAgentQueue(printerID int) (int, error) {
	n, logIDs, err := store.CancelPrintJobsByPrinter(printerID)
	if err != nil {
		return 0, err
	}
	for _, id := range logIDs {
		store.UpdatePrintLogResult(id, model.PrintStatusFailed, "队列已被人工清空,该单据未送出")
	}
	return n, nil
}

// ============================================================================
// 取单 / 回执
// ============================================================================

// PullAgentJobs 代理取单:抢占任务并把文本行编码成 ESC/POS 字节下发。
func PullAgentJobs(agentID string, limit int) ([]model.AgentJob, error) {
	if agentID == "" {
		agentID = "unknown"
	}
	jobs, err := store.ClaimPrintJobs(agentID, limit, agentClaimLeaseSeconds)
	if err != nil {
		return nil, err
	}
	out := make([]model.AgentJob, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, model.AgentJob{
			JobID:       j.JobID,
			PrinterID:   j.PrinterID,
			PrinterName: j.PrinterName,
			IP:          j.IP,
			Port:        effectivePort(j.Port),
			Copies:      j.Copies,
			DocType:     j.DocType,
			OrderNo:     j.OrderNo,
			TableNo:     j.TableNo,
			DeliveryID:  j.DeliveryID,
			Payload:     base64.StdEncoding.EncodeToString(EncodeTicket(strings.Split(j.Payload, "\n"))),
		})
	}
	return out, nil
}

// AckAgentJob 代理回报一条任务的打印结果。
//
// 失败不是终点:未用尽重试次数的任务会被重新放回队列(带退避),
// 打印机缺纸/关机这类瞬时问题在换纸开机后会自己补上;次数用尽才置为放弃。
func AckAgentJob(jobID int, ok bool, detail string) error {
	j, err := store.LoadPrintJob(jobID)
	if err != nil {
		return err
	}
	detail = strings.TrimSpace(detail)
	if ok {
		if err := store.MarkPrintJobDone(jobID); err != nil {
			return err
		}
		store.UpdatePrintLogResult(j.PrintLogID, model.PrintStatusSuccess,
			"本地打印代理已送出("+addrOf(j.IP, effectivePort(j.Port))+")")
		logger.Infof("打印: 代理已送出任务 #%d → 打印机[%s](%s) 单据 %s",
			jobID, j.PrinterName, addrOf(j.IP, effectivePort(j.Port)), j.OrderNo)
		return nil
	}

	if detail == "" {
		detail = "代理上报失败(未提供原因)"
	}
	retrying, err := store.RetryPrintJob(jobID, detail, agentRetryBackoffSeconds)
	if err != nil {
		return err
	}
	if retrying {
		store.UpdatePrintLogResult(j.PrintLogID, model.PrintStatusQueued,
			"第 "+strconv.Itoa(j.Attempts)+" 次失败,已安排重试: "+detail)
	} else {
		store.UpdatePrintLogResult(j.PrintLogID, model.PrintStatusFailed,
			"已放弃("+strconv.Itoa(j.Attempts)+" 次失败): "+detail)
	}
	logger.Warnf("打印: 代理上报任务 #%d 打印失败(第 %d 次,仍会重试=%v): %s",
		jobID, j.Attempts, retrying, detail)
	return nil
}

// ============================================================================
// 后台清理
// ============================================================================

// agentCleanupInterval 任务清理周期。
const agentCleanupInterval = 10 * time.Minute

// StartAgentCleanup 启动后台定时清理已结案的老任务(供 main 启动一次)。
//
// tb_print_job 是「待办」,done/dead 的行只对「事后翻账」有用,而翻账看的是
// tb_print_log(长期保留),队列行本身没有长期价值。不清理的话,日积月累这张表会
// 越滚越大,拖慢取单与报表。保留天数由 AGENT_JOB_RETENTION_DAYS 控制(默认 7 天)。
func StartAgentCleanup() {
	go func() {
		for {
			time.Sleep(agentCleanupInterval)
			n := store.CleanDonePrintJobs(agentJobRetentionDays())
			if n > 0 {
				logger.Infof("打印: 已清理 %d 条超过保留期的代理任务", n)
			}
		}
	}()
}

// agentJobRetentionDays 返回代理任务保留天数(AGENT_JOB_RETENTION_DAYS,默认 7,<=0 视为不清理)。
func agentJobRetentionDays() int {
	if v, err := strconv.Atoi(strings.TrimSpace(store.Getenv("AGENT_JOB_RETENTION_DAYS", "7"))); err == nil {
		return v
	}
	return 7
}
