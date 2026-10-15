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
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/interfaces/dto"
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

// PrinterStatus 代理回读的打印机实时状态(DLE EOT)。
// Queried=false 表示机型不应答或代理未查询(不影响 ok 判定,仅观测数据)。
type PrinterStatus struct {
	Queried      bool   `json:"queried"`
	Raw          string `json:"raw"`
	PaperOut     bool   `json:"paperOut"`
	PaperNearEnd bool   `json:"paperNearEnd"`
	CoverOpen    bool   `json:"coverOpen"`
	Paused       bool   `json:"paused"`
	Error        bool   `json:"error"`
}

// Summary 返回人读摘要;nil 安全;全正常/未查询返回空串。
func (s *PrinterStatus) Summary() string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if s.PaperOut {
		parts = append(parts, "缺纸")
	}
	if s.PaperNearEnd {
		parts = append(parts, "纸将尽")
	}
	if s.CoverOpen {
		parts = append(parts, "盖板开")
	}
	if s.Paused {
		parts = append(parts, "已暂停")
	}
	if s.Error {
		parts = append(parts, "打印机错误")
	}
	if len(parts) == 0 {
		return ""
	}
	return "打印机报告:" + strings.Join(parts, "、")
}

// ============================================================================
// 令牌与心跳
// ============================================================================

// AgentConfigured 报告代理通道是否可用(令牌已配置)。
func AgentConfigured() bool { return strings.TrimSpace(service.GetSetting("agent_token")) != "" }

// VerifyAgentToken 校验代理提交的令牌(恒定时间比较,避免长度/前缀被逐字节探测)。
func VerifyAgentToken(tok string) bool {
	want := strings.TrimSpace(service.GetSetting("agent_token"))
	if want == "" {
		return false
	}
	got := strings.TrimSpace(tok)
	// 先各自 SHA-256 归一化长度再比较:直接比较原文时,长度不等会提前 return,
	// 攻击者可用不同长度的令牌探测出真实令牌长度(与「恒定时间」的注释不符)。
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
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
	perAgentSeenMu       sync.Mutex
	perAgentSeenNano     = map[int]int64{} // 每台 v2 代理上次落库心跳(纳秒)
	agentJobNotify       = make(chan struct{}, 1)
)

// perAgentSeenMaxEntries 心跳限流表的硬上限。
//
// 代理身份被删除后,它的条目不会被自动清掉 —— 长期运维(反复新增/吊销代理)
// 会让 map 单调增长。超过上限时整体重置:该表唯一的语义是「这台代理最近落库的
// 时间」,重置只会让下一拍心跳多写一次库,没有任何正确性影响。
const perAgentSeenMaxEntries = 512

// agentSeenPersistInterval 心跳落库间隔:15 秒写一次,兼顾「跨实例一致」与「不写爆库」。
const agentSeenPersistInterval = 15 * time.Second

// MarkAgentSeen 记录一次代理心跳(内存即时 + 限流落库)。
func MarkAgentSeen(ctx context.Context) {
	now := time.Now()
	nowNano := now.UnixNano()
	atomic.StoreInt64(&agentLastSeenNano, nowNano)

	last := atomic.LoadInt64(&agentSeenPersistNano)
	if nowNano-last >= int64(agentSeenPersistInterval) &&
		atomic.CompareAndSwapInt64(&agentSeenPersistNano, last, nowNano) {
		// 心跳是状态值不是配置项:AgentConfig 无关、不会回显、也不会被配置保存覆盖。
		if err := service.SetSetting("agent_last_seen", now.Format(conf.TimeLayout)); err != nil {
			logger.CtxAgentWarnf(ctx, "打印代理: 心跳落库失败: %v", err)
		}
	}
}

// MarkAgentSeenFor 记录 v2 per-agent 代理心跳。
//
// per-agent 心跳只落 tb_print_agent:管理端需要看每台代理自己的 last_seen/report,
// 不能再写全局 agent_last_seen,否则新旧代理的在线状态会互相覆盖。高频轮询仍沿用
// 15 秒限流,避免代理每次 pull/ack/ping 都写库。
func MarkAgentSeenFor(ctx context.Context, agentID int, report string) {
	if agentID <= 0 {
		return
	}
	nowNano := time.Now().UnixNano()
	perAgentSeenMu.Lock()
	if len(perAgentSeenNano) > perAgentSeenMaxEntries {
		perAgentSeenNano = map[int]int64{}
	}
	last := perAgentSeenNano[agentID]
	if nowNano-last < int64(agentSeenPersistInterval) {
		perAgentSeenMu.Unlock()
		return
	}
	perAgentSeenNano[agentID] = nowNano
	perAgentSeenMu.Unlock()

	if err := service.TouchPrintAgent(agentID, report); err != nil {
		logger.CtxAgentWarnf(ctx, "打印代理: 代理[%d]心跳落库失败: %v", agentID, err)
	}
}

// AgentLastSeen 返回最近心跳时间;从未收到过返回零值。
//
// 优先读内存(即时);内存为空时回退读库 —— 覆盖「后端刚重启 / 多实例下心跳在别的实例」。
func AgentLastSeen() time.Time {
	if v := atomic.LoadInt64(&agentLastSeenNano); v != 0 {
		return time.Unix(0, v)
	}
	if s := strings.TrimSpace(service.GetSetting("agent_last_seen")); s != "" {
		if t, err := time.ParseInLocation(conf.TimeLayout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// agentOnlineWindow 判定「代理在线」的时间窗。
// 取轮询间隔(默认 3 秒)的数十倍,允许门店网络抖动几次不误报离线。
const agentOnlineWindow = 90 * time.Second

// AgentOnline 报告 legacy 全局代理是否仍在正常轮询。
func AgentOnline() bool {
	seen := AgentLastSeen()
	return !seen.IsZero() && time.Since(seen) < agentOnlineWindow
}

// AnyAgentOnline 报告 legacy 或任一启用中的 v2 per-agent 代理是否在线。
func AnyAgentOnline() bool {
	if AgentOnline() {
		return true
	}
	for _, a := range service.ListPrintAgents() {
		if a.Status == 1 && agentSeenWithin(a.LastSeen, agentOnlineWindow) {
			return true
		}
	}
	return false
}

func agentSeenWithin(seen string, window time.Duration) bool {
	seen = strings.TrimSpace(seen)
	if seen == "" {
		return false
	}
	t, err := time.ParseInLocation(conf.TimeLayout, seen, time.Local)
	if err != nil {
		return false
	}
	return time.Since(t) < window
}

// AgentSeenOnline 报告某台 v2 代理的 last_seen 是否落在在线窗口内。
//
// 由服务端判定而不是交给前端:`last_seen` 是**服务端本地时区的墙钟字符串**
// (store.Now()),前端用 new Date(seen.replace(' ','T')) 会按**浏览器**时区解析。
// 容器通常跑 UTC、门店浏览器在东八区,两者差 8 小时 ≫ 90 秒窗口 ——
// 结果是代理明明在正常心跳,页面却恒定显示「离线」。
func AgentSeenOnline(lastSeen string) bool {
	return agentSeenWithin(lastSeen, agentOnlineWindow)
}

// agentProbeSeen 返回探活展示用的最近心跳:legacy 全局心跳与 v2 per-agent
// 心跳取较新者。只用于文案展示,在线判定仍走 AnyAgentOnline(同一套窗口)。
func agentProbeSeen() time.Time {
	latest := AgentLastSeen()
	for _, a := range service.ListPrintAgents() {
		if a.Status != 1 {
			continue
		}
		if t, err := time.ParseInLocation(conf.TimeLayout, strings.TrimSpace(a.LastSeen), time.Local); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

// probeAgent 检查代理通道是否可用。
//
// 云端永远连不到门店内网,因此这里能回答的不是「打印机通不通」,而是
// 「代理还在不在轮询 + 有多少单积压」——这已经足够区分「没人跑代理」与「打印机坏了」。
//
// 判定必须覆盖 v2 per-agent 体系:门店可能只签发 v2 代理令牌(legacy 全局
// agent_token 已清空),此时 AgentConfigured()=false、AgentLastSeen() 为零,
// 若只看 legacy 轨会恒报「未启用/未收到心跳」,而管理端概况用的是 AnyAgentOnline。
func probeAgent(p po.Printer) (string, error) {
	if !AgentConfigured() && len(service.ListPrintAgents()) == 0 {
		return "", errors.New("本地打印代理未启用:请到「系统配置 → 小票打印」填写代理令牌,并在门店内网运行代理程序")
	}
	pending := service.PendingJobCountByPrinter(p.PrinterID)
	if !AnyAgentOnline() {
		seen := agentProbeSeen()
		if seen.IsZero() {
			return "", fmt.Errorf("云端尚未收到任何代理心跳(门店内需运行 print-agent);本机当前积压 %d 单", pending)
		}
		return "", fmt.Errorf("打印代理已离线(最近心跳 %s);本机当前积压 %d 单",
			seen.Format(conf.TimeLayout), pending)
	}
	return fmt.Sprintf("本地打印代理在线(最近心跳 %s);本机积压 %d 单",
		agentProbeSeen().Format("15:04:05"), pending), nil
}

// ============================================================================
// 入队
// ============================================================================

// AgentJobNotify 返回本实例的新任务通知通道,供长轮询 handler 等待。
// 多实例部署下他实例入队不会写到本实例内存通道,因此 handler 侧仍需 2s DB 兜底扫描。
func AgentJobNotify() <-chan struct{} { return agentJobNotify }

// notifyAgentJob 非阻塞投递新任务通知;已有 pending 通知时无需重复投递。
func notifyAgentJob() {
	select {
	case agentJobNotify <- struct{}{}:
	default:
	}
}

// enqueueAgentJob 把一张订单票据写入代理队列(替代直连的「立刻发出去」)。
func enqueueAgentJob(j job) error {
	return enqueueAgentTicket(j.jobAgentLog(), j)
}

// enqueueAgentTicket 单张票据入队的公共实现(订单票据与测试页共用)。
//
// 顺序很重要:先记打印日志拿到 log_id,再带着 log_id 入队。
// 这样即便入队那步失败,商家在「打印日志」里也能看到一条失败记录,
// 而不是「下单了、什么都没发生、日志里也查不到」。
func enqueueAgentTicket(log *zap.SugaredLogger, j job) error {
	p, o := j.p, j.o
	lines := j.render()

	copies := p.EffectiveCopies()
	label := j.label()
	logID, err := service.InsertPrintLogReturningID(po.PrintLog{
		OrderID:     o.OrderID,
		OrderNo:     o.OrderNo,
		TableNo:     o.TableNo,
		TableName:   o.TableName,
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		Provider:    po.PrinterProviderAgent,
		DocType:     j.docType,
		Copies:      copies,
		Status:      po.PrintStatusQueued,
		// 票据标识走独立列:后续的回执/重试/作废都会整段改写 detail
		// (见 dao.UpdatePrintLogResult),拼进 detail 的标识留不住。
		Label:  label,
		Detail: "已入队,等待门店打印代理取单",
		// 票面快照与其它通道一致:入队时内容就已定稿(见 snapshot.go),
		// 存下来后这条记录的预览/补打才是回放,而不是拿现在的订单重算。
		Content:    EncodeTicketContent(TicketContent{Label: label, Lines: lines}),
		PaperWidth: p.PaperWidth,
		TriggerBy:  j.triggerBy,
		Operator:   j.operator,
		CreateTime: service.Now(),
	})
	if err != nil {
		log.Warnf("打印代理: 写打印日志失败(仍继续入队): %v", err)
	}

	chunks := splitJobPayload(lines)
	for i, chunk := range chunks {
		if _, err := service.InsertPrintJob(po.PrintJob{
			PrinterID:   p.PrinterID,
			PrinterName: p.PrinterName,
			PrinterType: p.PrinterType,
			IP:          strings.TrimSpace(p.IP),
			Port:        effectivePort(p.Port),
			DocType:     j.docType,
			OrderID:     o.OrderID,
			OrderNo:     o.OrderNo,
			TableNo:     o.TableNo,
			Copies:      copies,
			PrintLogID:  logID,
			Payload:     chunk,
			TriggerBy:   j.triggerBy,
			Operator:    j.operator,
			CreateTime:  service.Now(),
		}); err != nil {
			log.Warnf("打印代理: 入队失败(第 %d/%d 段): %v", i+1, len(chunks), err)
			// 分段入队失败时,前面已成功的段可能已经被代理取走打印,不能只写一句
			// 「入队失败」让台账误以为整单都没送出去 —— 明确标注哪些段可能已打、哪段失败。
			detail := "入队失败: " + err.Error()
			if i > 0 {
				detail = fmt.Sprintf("第 %d 段入队失败: %v;第 1..%d 段已入队,可能已被代理取走打印", i+1, err, i)
			}
			service.UpdatePrintLogResult(logID, po.PrintStatusFailed, detail, -1)
			return fmt.Errorf("第 %d/%d 段入队失败: %w", i+1, len(chunks), err)
		}
	}
	notifyAgentJob()
	log.Infof("打印代理: 已入队 %d 段,等待代理取单 -> 打印机[%s](%s) 单据 %s(%s)",
		len(chunks), p.PrinterName, providerAddr(p), o.OrderNo, j.docType)
	return nil
}

// agentLineChunks 本地代理通道的行分段区间:单条任务的字符上限(文本 + 换行符)。
func agentLineChunks(lines []string) []chunkSpan {
	return lineChunks(lines, jobPayloadMaxChars, len("\n"), func(l string) int { return len(l) })
}

// splitJobPayload 把文本行按长度上限切成多段(每段是一份可直接打印的连续票据)。
//
// 区间由 agentLineChunks 计算,与预览共用同一份切段逻辑 —— 预览与实际发送
// 一旦各算各的,长单「预览几张纸 / 实际吐几张」就对不上了。
func splitJobPayload(lines []string) []string {
	out := make([]string, 0, 1)
	for _, sp := range agentLineChunks(lines) {
		out = append(out, strings.Join(lines[sp.Start:sp.End], "\n"))
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
	n, logIDs, err := service.CancelPrintJobsByPrinter(printerID)
	if err != nil {
		return 0, err
	}
	for _, id := range logIDs {
		service.UpdatePrintLogResult(id, po.PrintStatusFailed, "队列已被人工清空,该单据未送出", -1)
	}
	return n, nil
}

// ============================================================================
// 取单 / 回执
// ============================================================================

// PullAgentJobs 代理取单:抢占任务并把文本行编码成 ESC/POS 字节下发。
// printerIDs 为 per-agent 授权域(空=全部):授权域在 SQL 层过滤,错配代理
// 根本不会 claim 到域外任务,避免「claim 后丢弃」白白消耗租约与 attempts。
func PullAgentJobs(agentID string, limit int, printerIDs []int) ([]dto.AgentJob, error) {
	if agentID == "" {
		agentID = "unknown"
	}
	kitchenCutoff, guestCutoff, otherCutoff := agentExpireCutoffs()
	jobs, err := service.ClaimPrintJobs(agentID, limit, agentClaimLeaseSeconds, kitchenCutoff, guestCutoff, otherCutoff, printerIDs)
	if err != nil {
		return nil, err
	}
	out := make([]dto.AgentJob, 0, len(jobs))
	for _, j := range jobs {
		payload := base64.StdEncoding.EncodeToString(EncodeTicket(strings.Split(j.Payload, "\n")))
		job := dto.FromPrintJobWithPayload(j, payload)
		job.Port = effectivePort(job.Port)
		out = append(out, job)
	}
	return out, nil
}

// AckAgentJob 代理回报一条任务的打印结果。
//
// 失败不是终点:未用尽重试次数的任务会被重新放回队列(带退避),
// 打印机缺纸/关机这类瞬时问题在换纸开机后会自己补上;次数用尽才置为放弃。
//
// agentID 是鉴权后的代理身份(v2 令牌);legacy 令牌没有 per-agent 身份,传入空串,
// 只依赖下方 dao 层的条件更新做状态守卫 —— 历史兼容:老代理共用一把全局令牌,
// claimed_by 只是取单时自报的 agentId,不能当作强身份校验。
func AckAgentJob(ctx context.Context, agentID string, jobID int, ok bool, detail string, st *PrinterStatus) error {
	j, err := service.LoadPrintJob(jobID)
	if err != nil {
		return err
	}
	// 归属校验:v2 代理只能回执自己取走的任务,防止代理 A 把代理 B 的任务改状态。
	if agentID != "" && j.ClaimedBy != agentID {
		return errors.New("任务由其他代理持有")
	}
	detail = strings.TrimSpace(detail)
	if ok {
		if err := service.MarkPrintJobDone(jobID); err != nil {
			return err
		}
		logDetail := "本地打印代理已送出(" + addrOf(j.IP, effectivePort(j.Port)) + ")"
		if summary := st.Summary(); summary != "" {
			logDetail += "，但" + summary
			if raw := strings.TrimSpace(st.Raw); raw != "" {
				logDetail += "(raw " + raw + ")"
			}
		}
		service.UpdatePrintLogResult(j.PrintLogID, po.PrintStatusSuccess, logDetail, agentJobCostMs(j.CreateTime))
		logger.CtxAgentInfof(ctx, "打印代理: 代理已送出任务 #%d → 打印机[%s](%s) 单据 %s",
			jobID, j.PrinterName, addrOf(j.IP, effectivePort(j.Port)), j.OrderNo)
		return nil
	}

	if detail == "" {
		detail = "代理上报失败(未提供原因)"
	}
	retrying, err := service.RetryPrintJob(jobID, detail, agentRetryBackoffSeconds)
	if err != nil {
		return err
	}
	if retrying {
		service.UpdatePrintLogResult(j.PrintLogID, po.PrintStatusQueued,
			"第 "+strconv.Itoa(j.Attempts)+" 次失败,已安排重试: "+detail, -1)
	} else {
		service.UpdatePrintLogResult(j.PrintLogID, po.PrintStatusFailed,
			"已放弃("+strconv.Itoa(j.Attempts)+" 次失败): "+detail, -1)
	}
	// 这条是「转述代理上报」:真正拨打印机失败的是门店侧代理进程,本进程只是收到
	// ack(ok=false) 后代为记一笔并安排重试。文案必须写明失败方,否则排障时会被
	// 误读成「云端后端直连打印机失败」,从而怀疑链路接错。
	logger.CtxAgentWarnf(ctx, "打印代理: 收到代理上报,任务 #%d 在代理侧打印失败(第 %d 次,仍会重试=%v): %s",
		jobID, j.Attempts, retrying, detail)
	return nil
}

// ReleaseAgentJob 代理回报「本机未真正尝试」(熔断冷却中,没有拨过打印机)。
//
// 与 AckAgentJob(ok=false) 的关键区别:**不消耗重试次数**。
// attempts 是在 claim 取单时就 +1 的,冷却期内若按失败处理,打印机只是短暂不可达
// (刚上电 / WiFi 未就绪)也会被判成「已放弃」而永久丢单。这里把额度还回去,
// 使 attempts 严格等于真实尝试次数。
//
// 归属校验与 AckAgentJob 一致:v2 代理只能操作自己取走的任务。
func ReleaseAgentJob(ctx context.Context, agentID string, jobID int, detail string, retryAfterSeconds int) error {
	j, err := service.LoadPrintJob(jobID)
	if err != nil {
		return err
	}
	if agentID != "" && j.ClaimedBy != agentID {
		return errors.New("任务由其他代理持有")
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "代理未尝试打印(打印机连接冷却中)"
	}
	if err := service.ReleasePrintJob(jobID, detail, retryAfterSeconds); err != nil {
		return err
	}
	// 打印日志维持「排队中」:任务没被放弃,商家看到的是「还在等打印机恢复」。
	service.UpdatePrintLogResult(j.PrintLogID, po.PrintStatusQueued,
		"打印机连接冷却中,约 "+strconv.Itoa(retryAfterSeconds)+
			" 秒后重试(尚未尝试打印,不消耗重试次数)", -1)
	logger.CtxAgentDebugf(ctx, "打印代理: 任务 #%d 在代理侧未尝试(熔断冷却中),%d 秒后重新下发", jobID, retryAfterSeconds)
	return nil
}

func agentJobCostMs(createTime string) int {
	created, err := time.ParseInLocation(conf.TimeLayout, createTime, time.Local)
	if err != nil {
		return -1
	}
	now, err := time.ParseInLocation(conf.TimeLayout, service.Now(), time.Local)
	if err != nil {
		return -1
	}
	return int(now.Sub(created).Milliseconds())
}

// ============================================================================
// 后台清理
// ============================================================================

// agentCleanupInterval 任务清理周期。
const agentCleanupInterval = 10 * time.Minute

// StartAgentCleanup 启动后台定时清理代理任务(供 main 启动一次)。
//
// tb_print_job 是「待办」,done/dead 的行只对「事后翻账」有用,而翻账看的是
// tb_print_log(长期保留),队列行本身没有长期价值。不清理的话,日积月累这张表会
// 越滚越大,拖慢取单与报表。每轮先把超过 TTL 的未送出任务置 dead,再按
// AGENT_JOB_RETENTION_DAYS(默认 7 天)清理已结案任务。
func StartAgentCleanup() {
	goSafe("agentCleanup", func() {
		for {
			time.Sleep(agentCleanupInterval)
			// 逐轮 recover:外层 goSafe 的 recover 在 goroutine 最外层,若不在循环内
			// 兜住,单轮 panic 会穿透 for 直接终止协程,此后清理永久停摆(过期任务
			// 不再作废、队列越滚越大)。
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("打印: agentCleanup 单轮清理 panic(已跳过本轮): %v", r)
					}
				}()
				kitchenCutoff, guestCutoff, otherCutoff := agentExpireCutoffs()
				expired, logIDs := service.ExpireOverduePrintJobs(agentClaimLeaseSeconds, kitchenCutoff, guestCutoff, otherCutoff)
				for _, id := range logIDs {
					service.UpdatePrintLogResult(id, po.PrintStatusFailed, "任务未在有效期内送出,已自动作废,可人工补打", -1)
				}
				if expired > 0 {
					logger.AgentInfof("打印代理: 已作废 %d 条过期代理任务", expired)
				}

				n := service.CleanDonePrintJobs(agentJobRetentionDays())
				if n > 0 {
					logger.AgentInfof("打印代理: 已清理 %d 条超过保留期的代理任务", n)
				}
			}()
		}
	})
}

// agentJobRetentionDays 返回代理任务保留天数(AGENT_JOB_RETENTION_DAYS,默认 7,<=0 视为不清理)。
//
// 上限钳制:保留天数最终会换算成秒并参与 time.Duration(纳秒)运算,极大值会溢出成
// 负数 —— 那会让 cutoff 挪到未来、把**全部**已结案任务一次性删掉。钳到十年。
func agentJobRetentionDays() int {
	const maxRetentionDays = 3650 // 10 年
	v, err := strconv.Atoi(strings.TrimSpace(infrastructure.Getenv(conf.EnvAgentJobRetentionDays, conf.DefaultAgentJobRetentionDays)))
	if err != nil {
		return 7
	}
	if v > maxRetentionDays {
		logger.AgentWarnf("打印代理: %s=%d 天超出上限,已按 %d 天处理", conf.EnvAgentJobRetentionDays, v, maxRetentionDays)
		return maxRetentionDays
	}
	return v
}

// agentJobTTL 返回不同单据类型允许滞留在代理队列中的时长。
// <=0 表示该类型不过期:用于极少数门店临时关闭 TTL,避免需要改代码或迁移表结构。
func agentJobTTL(docType string) time.Duration {
	key, def := conf.EnvAgentJobTTLTestMin, conf.DefaultAgentJobTTLTestMin
	switch docType {
	case po.PrintDocKitchen:
		key, def = conf.EnvAgentJobTTLKitchenMin, conf.DefaultAgentJobTTLKitchenMin
	case po.PrintDocGuest:
		key, def = conf.EnvAgentJobTTLGuestMin, conf.DefaultAgentJobTTLGuestMin
	}
	mins, err := strconv.Atoi(strings.TrimSpace(infrastructure.Getenv(key, def)))
	if err != nil {
		mins, _ = strconv.Atoi(def)
	}
	if mins <= 0 {
		return 0
	}
	// 上限钳制:time.Duration 是 int64 纳秒,换算成分钟约 1.5e8 就会溢出为负,
	// 而 ttl<=0 的语义是「该类型不过期」—— 一个笔误的超大值会静默关掉 TTL,
	// 让过期厨房单被无限期重发。这里钳到一年并显式告警。
	const maxTTLMinutes = 525600 // 365 天
	if mins > maxTTLMinutes {
		logger.AgentWarnf("打印代理: %s=%d 分钟超出上限,已按 %d 分钟(一年)处理", key, mins, maxTTLMinutes)
		mins = maxTTLMinutes
	}
	return time.Duration(mins) * time.Minute
}

// agentExpireCutoffs 计算三类单据的过期边界。
//
// create_time 以固定宽度字符串存储,因此 cutoff 也必须使用同一格式,才能在 SQL 中
// 直接做字典序比较。空串表示该类型关闭 TTL 过滤。
func agentExpireCutoffs() (kitchen, guest, other string) {
	now := service.Now()
	base, err := time.ParseInLocation(conf.TimeLayout, now, time.Local)
	if err != nil {
		base = time.Now()
	}
	cutoff := func(ttl time.Duration) string {
		if ttl <= 0 {
			return ""
		}
		return base.Add(-ttl).Format(conf.TimeLayout)
	}
	return cutoff(agentJobTTL(po.PrintDocKitchen)),
		cutoff(agentJobTTL(po.PrintDocGuest)),
		cutoff(agentJobTTL(""))
}
