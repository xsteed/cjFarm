package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/print"
	"dining-system/internal/store"
)

// ============ 本地打印代理(门店侧程序调用) ============
//
// 与其它接口的根本区别:调用方不是浏览器,而是门店内网常驻的代理程序,
// 它**出站**连云端(所以门店不需要公网 IP、不需要端口映射),鉴权用配置里的代理令牌。
//
// 三个接口就够跑起来:
//
//	POST /prod-api/agent/print/pull  取单(要打印什么、发到哪个 IP:9100)
//	POST /prod-api/agent/print/ack   回执(打成功了还是失败了、失败原因)
//	POST /prod-api/agent/print/ping  心跳(校验令牌 + 探活,启动时调一次即可)
//
// 协议细节与部署步骤见 docs/print-agent.md。

// agentTokenHeader 代理令牌请求头。
const agentTokenHeader = "X-Agent-Token"

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
	if !print.AgentConfigured() {
		agentErr(c, http.StatusForbidden,
			"服务端未启用本地打印代理:请在「系统配置 → 小票打印」填写代理令牌")
		return false
	}
	if !print.VerifyAgentToken(tok) {
		agentErr(c, http.StatusUnauthorized, "代理令牌不正确")
		return false
	}
	// 每次成功调用都算心跳:管理端据此显示「代理在线/离线」。
	print.MarkAgentSeen()
	return true
}

// agentErr 代理接口的错误响应(不写审计日志)。
func agentErr(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg})
}

// AgentPull 代理取单:抢占若干条待打印任务并返回已编码好的 ESC/POS 字节。
func AgentPull(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	// 请求体允许为空(代理可以不传任何参数);解析失败不视为错误。
	var p struct {
		AgentID string `json:"agentId"` // 代理自身标识(多台代理时可区分是谁取的)
		Limit   int    `json:"limit"`   // 本次最多取几条,缺省 10,上限 50
	}
	_ = c.ShouldBindJSON(&p)

	jobs, err := print.PullAgentJobs(strings.TrimSpace(p.AgentID), p.Limit)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "取单失败: "+err.Error())
		return
	}
	ok(c, gin.H{"jobs": jobs, "serverTime": store.Now()})
}

// AgentAck 代理回报打印结果(成功结案;失败按重试次数决定重试或放弃)。
func AgentAck(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	var p struct {
		Results []struct {
			JobID  int    `json:"jobId"`
			Ok     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"results"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	done, failed := 0, 0
	for _, r := range p.Results {
		if r.JobID <= 0 {
			continue
		}
		if err := print.AckAgentJob(r.JobID, r.Ok, r.Detail); err != nil {
			failed++
			continue
		}
		if r.Ok {
			done++
		}
	}
	ok(c, gin.H{"accepted": len(p.Results), "done": done, "rejected": failed})
}

// AgentPing 代理启动自检与心跳:令牌对不对、服务端时间、队列积压情况。
func AgentPing(c *gin.Context) {
	if !agentAuth(c) {
		return
	}
	pending, dead := store.PrintJobStats()
	ok(c, gin.H{
		"shopName":   store.GetCfg("shop_name"),
		"serverTime": store.Now(),
		"pending":    pending,
		"dead":       dead,
	})
}

// AgentInfo 管理端查看代理通道概况(不回显令牌)。供打印机管理页展示。
func AgentInfo(c *gin.Context) {
	pending, dead := store.PrintJobStats()
	var printerCount int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_printer WHERE del_flag='0' AND provider=?`,
		model.PrinterProviderAgent).Scan(&printerCount)

	lastSeen := ""
	if seen := print.AgentLastSeen(); !seen.IsZero() {
		lastSeen = seen.Format("2006-01-02 15:04:05")
	}
	ok(c, gin.H{
		"configured":   print.AgentConfigured(),
		"online":       print.AgentOnline(),
		"lastSeen":     lastSeen,
		"pending":      pending,
		"dead":         dead,
		"printerCount": printerCount,
	})
}
