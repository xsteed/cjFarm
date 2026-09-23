package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/conf"
	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/print"
	"dining-system/internal/service"
)

// ============ 本地打印代理(门店侧程序调用) ============
//
// 与其它接口的根本区别:调用方不是浏览器,而是门店内网常驻的代理程序,
// 它**出站**连云端(所以门店不需要公网 IP、不需要端口映射),鉴权用配置里的代理令牌。
//
// 四个接口:
//
//	POST /api/agent/print/pull      取单(要打印什么、发到哪个 IP:9100)
//	POST /api/agent/print/ack       回执(打成功了还是失败了、失败原因)
//	POST /api/agent/print/ping      心跳(校验令牌 + 探活,启动时调一次即可)
//	GET  /api/agent/print/download  自助升级(下载服务端部署的最新代理产物)
//
// 协议细节与部署步骤见 docs/print-agent.md。

// agentTokenHeader 代理令牌请求头。
const agentTokenHeader = "X-Agent-Token"

// agentServerCapabilities 云端支持的能力。
//
// "ack-skipped" 表示本端能处理 ack 里的 skipped(代理未真正尝试)回执:
// 收到后把任务退回队列并**归还**尝试次数,而不按失败计。代理据此决定是否发 skipped;
// 老代理不认识该能力,也就不会发这个字段,行为完全不变。
var agentServerCapabilities = []string{"printer-status", "long-pull", "ack-skipped"}

// agentAuth 校验代理令牌;失败时已写入响应并返回 false。
//
// 这里刻意不走 unauthorized/forbidden 这两个统一出口:它们会写审计日志(
// 越权尝试是有价值的线索),但代理是每几秒轮询一次的程序,令牌配错时会刷出
// 成千上万条「越权访问」记录,反而把真正需要追责的操作日志淹掉。
func agentAuth(c *gin.Context) bool {
	tok := strings.TrimSpace(c.GetHeader(agentTokenHeader))
	if tok == "" {
		// 兼容 Authorization: Bearer <token> —— 便于用现成的 HTTP 调试工具验证。
		tok = strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	}
	if agent, ok := service.FindPrintAgentByToken(tok); ok {
		// 只做鉴权 + 上下文写入;心跳/版本上报放到 AgentPull / AgentPing 里,
		// 因为 buildVersion 在请求体里,鉴权阶段还没有绑定请求体。
		c.Set("agentID", agent.AgentID)
		c.Set("agentName", agent.AgentName)
		logger.AgentDebugf("打印代理请求(v2): path=%s agentId=%d agentName=%s name=%s",
			c.Request.URL.Path, agent.AgentID, agent.AgentName, strings.TrimSpace(c.GetHeader("X-Agent-Name")))
		return true
	}
	if !print.AgentConfigured() {
		agentErr(c, http.StatusForbidden,
			"服务端未启用本地打印代理:请在「系统配置 → 小票打印」填写代理令牌")
		return false
	}
	if !print.VerifyAgentToken(tok) {
		agentErr(c, http.StatusUnauthorized, "代理令牌不正确")
		return false
	}
	logger.AgentDebugf("打印代理请求: path=%s version=%s name=%s",
		c.Request.URL.Path, strings.TrimSpace(c.GetHeader("X-Agent-Version")), strings.TrimSpace(c.GetHeader("X-Agent-Name")))
	// 每次成功调用都算心跳:管理端据此显示「代理在线/离线」。
	print.MarkAgentSeen()
	return true
}

// agentErr 代理接口的错误响应(不写审计日志)。
func agentErr(c *gin.Context, status int, msg string) {
	c.JSON(status, injectRequestID(c, &Rsp{Code: status, Msg: msg}))
}

func scopedPrintAgent(c *gin.Context) (dto.PrintAgent, bool, error) {
	v, ok := c.Get("agentID")
	if !ok {
		return dto.PrintAgent{}, false, nil
	}
	agentID, ok := v.(int)
	if !ok || agentID <= 0 {
		return dto.PrintAgent{}, false, nil
	}
	agent, err := service.LoadPrintAgent(agentID)
	if err != nil {
		return dto.PrintAgent{}, false, err
	}
	return dto.FromPrintAgent(agent, service.ParseIDList(agent.PrinterIDs)), true, nil
}

// perAgentReport 组装 per-agent 心跳的上报信息:「代理名|软件版本」。
// 代理名优先取鉴权时写入的 agentName,回退到请求体自报的 agentId;
// buildVersion 为空(v1 代理不传)时右侧留空,AgentInfo 解析时自然得到空版本。
func perAgentReport(c *gin.Context, requestAgentID, buildVersion string) string {
	name := ""
	if v, ok := c.Get("agentName"); ok {
		name, _ = v.(string)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(requestAgentID)
	}
	return name + "|" + strings.TrimSpace(buildVersion)
}

// agentReportVersion 从 per-agent 心跳上报「代理名|软件版本」中解析软件版本。
// 取最后一个 '|' 之后的部分;无 '|'、'|' 结尾或整体为空时返回空串,
// 兼容 v1 代理与老数据(它们没有 buildVersion,last_report 可能不是该格式)。
func agentReportVersion(report string) string {
	report = strings.TrimSpace(report)
	if report == "" {
		return ""
	}
	i := strings.LastIndex(report, "|")
	if i < 0 || i+1 >= len(report) {
		return ""
	}
	return report[i+1:]
}

// AgentPull 代理取单:抢占若干条待打印任务并返回已编码好的 ESC/POS 字节。
func AgentPull(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	// 长轮询返回前统一关 nginx 响应缓冲;wait=0 的短轮询加这个头也无害。
	c.Header("X-Accel-Buffering", "no")

	// 请求体允许为空(代理可以不传任何参数);解析失败不视为错误。
	var p struct {
		AgentID      string   `json:"agentId"`      // 代理自身标识(多台代理时可区分是谁取的)
		Limit        int      `json:"limit"`        // 本次最多取几条,缺省 10,上限 50
		Version      int      `json:"version"`      // v2 能力协商字段;当前只观测,不改变取单行为
		Capabilities []string `json:"capabilities"` // v2 能力声明;后续长轮询/状态能力按它灰度
		Wait         int      `json:"wait"`         // 长轮询等待秒数,0 为 v1 短轮询行为
		BuildVersion string   `json:"buildVersion"` // 代理软件版本(与协议 version 区分),v1 代理不传
	}
	_ = c.ShouldBindJSON(&p)
	if p.Wait < 0 {
		p.Wait = 0
	} else if p.Wait > 30 {
		p.Wait = 30
	}
	requestAgentID := strings.TrimSpace(p.AgentID)
	authAgent, scoped, err := scopedPrintAgent(c)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "读取代理授权范围失败: "+err.Error())
		return
	}
	claimAgentID := requestAgentID
	var scope []int
	if scoped {
		// v2 代理以已鉴权的 agent_id 作为领取标识,不再信任请求体里的自报 agentId;
		// 授权域直接传入取单 SQL(见 print.PullAgentJobs),错配代理不会 claim 到域外任务。
		claimAgentID = strconv.Itoa(authAgent.AgentID)
		scope = authAgent.PrinterIDList
		// per-agent 心跳:记录本次取单上报的软件版本,管理端据此判断门店是否可升级。
		// MarkAgentSeenFor 内部有 15s 落库限流,这里只负责调用,不依赖它是否真正写库。
		print.MarkAgentSeenFor(authAgent.AgentID, perAgentReport(c, requestAgentID, p.BuildVersion))
	}
	logger.AgentDebugf("打印代理取单: agentId=%s requestAgentId=%s version=%d capabilities=%v wait=%d scope=%v",
		claimAgentID, requestAgentID, p.Version, p.Capabilities, p.Wait, scope)

	jobs, err := print.PullAgentJobs(claimAgentID, p.Limit, scope)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "取单失败: "+err.Error())
		return
	}
	if p.Wait > 0 && len(jobs) == 0 {
		start := time.Now()
		deadline := start.Add(time.Duration(p.Wait) * time.Second)
		// 主服务 http.Server 未设置 WriteTimeout(见 backend/main.go),允许本 handler hold 最多 30s。
		// 多实例部署下他实例入队收不到本实例内存 notify,所以每 2s 仍做一次 DB 兜底扫描。
		for time.Now().Before(deadline) {
			remaining := time.Until(deadline)
			if remaining > 2*time.Second {
				remaining = 2 * time.Second
			}
			select {
			case <-print.AgentJobNotify():
			case <-time.After(remaining):
			case <-c.Request.Context().Done():
				logger.AgentDebugf("打印代理长轮询取消: agentId=%s wait=%d waited=%s", claimAgentID, p.Wait, time.Since(start).Truncate(time.Millisecond))
				return
			}

			jobs, err = print.PullAgentJobs(claimAgentID, p.Limit, scope)
			if err != nil {
				agentErr(c, http.StatusInternalServerError, "取单失败: "+err.Error())
				return
			}
			if len(jobs) > 0 {
				break
			}
		}
		logger.AgentDebugf("打印代理长轮询结束: agentId=%s wait=%d waited=%s jobs=%d",
			claimAgentID, p.Wait, time.Since(start).Truncate(time.Millisecond), len(jobs))
	}
	ok(c, gin.H{
		"jobs":               jobs,
		"serverTime":         service.Now(),
		"protoVersion":       2,
		"serverCapabilities": agentServerCapabilities,
	})
}

// AgentAck 代理回报打印结果(成功结案;失败按重试次数决定重试或放弃)。
func AgentAck(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	// v2 令牌在鉴权阶段已写入 agentID;legacy 令牌没有 per-agent 身份,ackAgentID 留空,
	// 由 print.AckAgentJob 只做状态守卫(历史兼容,详见其注释)。
	authAgent, scoped, err := scopedPrintAgent(c)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "读取代理授权范围失败: "+err.Error())
		return
	}
	ackAgentID := ""
	if scoped {
		ackAgentID = strconv.Itoa(authAgent.AgentID)
	}
	var p struct {
		Results []struct {
			JobID  int    `json:"jobId"`
			Ok     bool   `json:"ok"`
			Detail string `json:"detail"`
			// Skipped 代理因熔断冷却未真正尝试打印(未拨过打印机),请求退回队列且不计失败。
			// 老代理不传,默认 false → 走原有失败重试逻辑。
			Skipped bool `json:"skipped"`
			// RetryAfter 代理建议多少秒后再下发(通常是冷却剩余秒数)。0 表示由云端决定。
			RetryAfter int `json:"retryAfter"`
			PrinterStatus *struct {
				Queried      bool   `json:"queried"`
				Raw          string `json:"raw"`
				PaperOut     bool   `json:"paperOut"`
				PaperNearEnd bool   `json:"paperNearEnd"`
				CoverOpen    bool   `json:"coverOpen"`
				Paused       bool   `json:"paused"`
				Error        bool   `json:"error"`
			} `json:"printerStatus"`
		} `json:"results"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	done, failed, skipped := 0, 0, 0
	for _, r := range p.Results {
		if r.JobID <= 0 {
			continue
		}
		// 未真正尝试:退回队列并归还尝试次数,不走失败重试。
		if r.Skipped {
			if err := print.ReleaseAgentJob(ackAgentID, r.JobID, r.Detail, r.RetryAfter); err != nil {
				failed++
				continue
			}
			skipped++
			continue
		}
		var st *print.PrinterStatus
		if r.PrinterStatus != nil {
			st = &print.PrinterStatus{
				Queried:      r.PrinterStatus.Queried,
				Raw:          r.PrinterStatus.Raw,
				PaperOut:     r.PrinterStatus.PaperOut,
				PaperNearEnd: r.PrinterStatus.PaperNearEnd,
				CoverOpen:    r.PrinterStatus.CoverOpen,
				Paused:       r.PrinterStatus.Paused,
				Error:        r.PrinterStatus.Error,
			}
		}
		if err := print.AckAgentJob(ackAgentID, r.JobID, r.Ok, r.Detail, st); err != nil {
			failed++
			continue
		}
		if r.Ok {
			done++
		}
	}
	ok(c, gin.H{
		"accepted":           len(p.Results),
		"done":               done,
		"rejected":           failed,
		"skipped":            skipped,
		"protoVersion":       2,
		"serverCapabilities": agentServerCapabilities,
	})
}

// AgentPing 代理启动自检与心跳:令牌对不对、服务端时间、队列积压情况。
func AgentPing(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	// 请求体允许为空(老代理不传 buildVersion);解析失败不视为错误。
	var p struct {
		AgentID      string `json:"agentId"`      // 代理自身标识(与 pull 一致,便于上报归属)
		BuildVersion string `json:"buildVersion"` // 代理软件版本;v1 代理不传,留空即可
	}
	_ = c.ShouldBindJSON(&p)
	authAgent, scoped, err := scopedPrintAgent(c)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "读取代理授权范围失败: "+err.Error())
		return
	}
	if scoped {
		// per-agent 心跳:ping 是代理启动/周期性的稳定请求,在这里记录软件版本供管理端展示。
		print.MarkAgentSeenFor(authAgent.AgentID, perAgentReport(c, p.AgentID, p.BuildVersion))
	}
	// agent 日志统一走专属 agent.log(见 infra/logger 的 Agent* 系列),
	// 控制台不会被高频轮询刷屏,文件里完整现场不缺;「代理连接成功」这一条
	// 是排查「代理为什么拉不到单」的关键线索,保留 info 级别落盘。
	name := strings.TrimSpace(p.AgentID)
	if name == "" {
		name = "未知代理"
	}
	logger.AgentInfof("打印代理[%s]已连接云端(版本 %s)", name, strings.TrimSpace(p.BuildVersion))
	pending, dead := service.PrintJobStats()
	ok(c, gin.H{
		"shopName":           service.GetSetting("shop_name"),
		"serverTime":         service.Now(),
		"pending":            pending,
		"dead":               dead,
		"protoVersion":       2,
		"serverCapabilities": agentServerCapabilities,
		"oldestPendingSec":   service.OldestPendingSec(),
		"latestAgentVersion": service.GetSetting("agent_latest_version"),
	})
}

// AgentDownload 代理自助升级:按平台下载服务端部署的最新代理产物。
//
// 调用方是门店侧的 print-agent --upgrade;鉴权与取单一致(代理令牌)。
// 产物来自服务器上的 AGENT_BIN_DIR(默认发布包内 print-agent/bin),
// sha256 与版本号放响应头,供客户端校验完整性、核对版本。
func AgentDownload(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	goos := strings.ToLower(strings.TrimSpace(c.Query("goos")))
	goarch := strings.ToLower(strings.TrimSpace(c.Query("goarch")))
	if !validAgentPlatform(goos, goarch) {
		agentErr(c, http.StatusBadRequest, "不支持的目标平台(仅 linux/darwin/windows + amd64/arm64)")
		return
	}

	name := "print-agent-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(infra.Getenv(conf.EnvAgentBinDir, conf.DefaultAgentBinDir), name)
	data, err := os.ReadFile(path)
	if err != nil {
		agentErr(c, http.StatusNotFound,
			"服务端未部署该平台的最新代理产物(缺少 "+path+"):请把发布包 print-agent/bin 下的产物随服务一起部署到服务器")
		return
	}
	sum := sha256.Sum256(data)
	c.Header("X-Agent-Sha256", hex.EncodeToString(sum[:]))
	// 版本号与 ping 下发的 latestAgentVersion 同源(管理端「代理最新版本号」配置),
	// 客户端据此核对下载产物与云端宣称的版本一致。
	c.Header("X-Agent-Version", strings.TrimSpace(service.GetSetting("agent_latest_version")))
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// validAgentPlatform 目标平台白名单:防止经 query 参数做目录穿越/任意文件读取。
func validAgentPlatform(goos, goarch string) bool {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return false
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return false
	}
	// 目前只发布 windows-amd64 产物,先按实际发布矩阵收紧。
	return goos != "windows" || goarch == "amd64"
}

// AgentInfo 管理端查看代理通道概况(不回显令牌)。供打印机管理页展示。
func AgentInfo(c *gin.Context) {
	pending, dead := service.PrintJobStats()
	printerCount := service.CountPrintersByProvider(po.PrinterProviderAgent)
	agents := service.ListPrintAgents()
	agentItems := []gin.H{}
	for _, a := range agents {
		pa := dto.FromPrintAgent(a, service.ParseIDList(a.PrinterIDs))
		agentItems = append(agentItems, gin.H{
			"agentId":       pa.AgentID,
			"agentName":     pa.AgentName,
			"tokenHint":     pa.TokenHint,
			"printerIds":    pa.PrinterIDs,
			"printerIdList": pa.PrinterIDList,
			"status":        pa.Status,
			"lastSeen":      pa.LastSeen,
			"lastReport":    pa.LastReport,
			// 从「代理名|软件版本」里解析出软件版本;老数据/解析失败时为空串。
			"version": agentReportVersion(pa.LastReport),
		})
	}

	lastSeen := ""
	if seen := print.AgentLastSeen(); !seen.IsZero() {
		lastSeen = seen.Format(conf.TimeLayout)
	}
	ok(c, gin.H{
		"configured":       print.AgentConfigured(),
		"online":           print.AnyAgentOnline(),
		"lastSeen":         lastSeen,
		"pending":          pending,
		"dead":             dead,
		"printerCount":     printerCount,
		"agents":           agentItems,
		"oldestPendingSec": service.OldestPendingSec(),
		// 与 ping 下发的字段同名:管理端据此判断每台代理是否落后于云端最新版本。
		"latestAgentVersion": service.GetSetting("agent_latest_version"),
	})
}

// AgentList 返回 v2 打印代理身份列表;令牌明文不会再次回显。
func AgentList(c *gin.Context) {
	agents := service.ListPrintAgents()
	items := make([]dto.PrintAgent, 0, len(agents))
	for _, a := range agents {
		items = append(items, dto.FromPrintAgent(a, service.ParseIDList(a.PrinterIDs)))
	}
	tableResult(c, len(items), items)
}

// AgentSave 新增一台 v2 打印代理身份。令牌明文仅在本次响应中回显一次。
func AgentSave(c *gin.Context) {
	var p struct {
		AgentName  string `json:"agentName"`
		PrinterIDs string `json:"printerIds"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	name := strings.TrimSpace(p.AgentName)
	if name == "" {
		fail(c, "请填写代理名称")
		return
	}
	agent, token, err := service.InsertPrintAgent(name, strings.TrimSpace(p.PrinterIDs))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{
		"token":     token,
		"tokenHint": agent.TokenHint,
		"agentId":   agent.AgentID,
	})
}

// AgentSetStatus 启用或吊销一台 v2 打印代理身份。
func AgentSetStatus(c *gin.Context) {
	agentID, okid := idParam(c)
	if !okid {
		return
	}
	var p struct {
		Status int `json:"status"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if p.Status != 0 && p.Status != 1 {
		fail(c, "状态参数错误")
		return
	}
	if err := service.SetPrintAgentStatus(agentID, p.Status); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "操作成功")
}

// AgentUpdate 修改一台代理身份的名称与授权范围(不重新签发令牌)。
//
// agentName 留空表示不改名;printerIds 留空表示授权全部打印机 ——
// 保存时后端会顺带剔除已删除打印机的残留 id。
func AgentUpdate(c *gin.Context) {
	agentID, okid := idParam(c)
	if !okid {
		return
	}
	var p struct {
		AgentName  string `json:"agentName"`
		PrinterIDs string `json:"printerIds"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if err := service.UpdatePrintAgent(agentID, strings.TrimSpace(p.AgentName), strings.TrimSpace(p.PrinterIDs)); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "保存成功")
}

// AgentDelete 删除一台已吊销的代理身份(清理历史记录与其令牌哈希)。
//
// 仍启用(status=1)的代理会被 dao 拒绝:先吊销再删,避免把正在出纸的代理凭空抹掉。
func AgentDelete(c *gin.Context) {
	agentID, okid := idParam(c)
	if !okid {
		return
	}
	if err := service.DeletePrintAgent(agentID); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}
