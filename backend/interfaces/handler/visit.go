// 站点访问统计的 HTTP 层:公开打点接口 + 管理端报表接口。
//
// 打点接口的三个「不合常理」之处都是有意的(见 spec 第 5 节):
//   1. 恒返回 200 —— 埋点失败/参数非法/被限流都不该打扰正在点餐的顾客,
//      真实原因只进日志(与 customerFail 的收敛哲学一致);
//   2. 挂在顾客公开组 + CustomerAuthOptional —— 带会员令牌就把访客升级为
//      「会员身份」(m:<id> 键),不带则匿名,两条路径都放行;
//   3. customer_id 只认服务端从令牌解析的值,请求体里的 customerId 一律忽略
//      (防伪造归属,与下单接口同一约定)。
package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/infrastructure/logger"
)

// VisitTrack 记录一次站点访问(公开接口,无需登录)。
//
// body: {visitorId?, channel, entry?}
//   - visitorId 前端生成并持久化的随机 ID(localStorage / wx.storage);
//   - channel   h5 / miniprogram / admin;
//   - entry     入口标识(桌台点餐码,仅 H5 有)。
func VisitTrack(c *gin.Context) {
	if !service.VisitEnabled() {
		ok(c, nil)
		return
	}
	// 限流不落库也不报错:统计接口的存在本身不该被用来探测服务状态。
	if !service.AllowVisitIP(c.ClientIP()) {
		logger.CtxDebugf(c.Request.Context(), "[visit] 打点被限流 IP=%s", c.ClientIP())
		ok(c, nil)
		return
	}
	var p struct {
		VisitorID string `json:"visitorId"`
		Channel   string `json:"channel"`
		Entry     string `json:"entry"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		// 埋点请求体非法:记一条 warn 供排查,对顾客仍返回成功。
		logger.CtxWarnf(c.Request.Context(), "[visit] 打点参数非法: %v", err)
		ok(c, nil)
		return
	}
	service.RecordVisit(service.VisitInput{
		Channel:    p.Channel,
		VisitorID:  p.VisitorID,
		CustomerID: customerIDFrom(c), // 0 = 匿名;带合法会员令牌时为会员 ID
		Entry:      p.Entry,
		IP:         c.ClientIP(),
		UA:         c.GetHeader("User-Agent"),
	})
	ok(c, nil)
}

// ReportVisits 访问统计报表(PV/UV/新访客/渠道构成),需 report:view。
//
// 入参与既有报表一致:start&end 或 days(默认 30,上限 366,见 resolveRange)。
func ReportVisits(c *gin.Context) {
	s := service.VisitStatsOf(c.Query("start"), c.Query("end"), c.Query("days"))
	ok(c, gin.H{
		"range": gin.H{"start": s.Start, "end": s.End},
		"summary": gin.H{
			"pv":            s.PV,
			"uv":            s.UV,
			"newUv":         s.NewUV,
			"memberUv":      s.MemberUV,
			"totalVisitors": s.TotalVisitors,
			"memberCount":   s.MemberCount,
			// 环比基准:后端只给绝对值,涨跌幅由前端统一渲染(同 report/summary)
			"todayPv":       s.TodayPV,
			"todayUv":       s.TodayUV,
			"todayNewUv":    s.TodayNewUV,
			"yesterdayPv":   s.YesterdayPV,
			"yesterdayUv":   s.YesterdayUV,
		},
		"daily":    s.Daily,
		"channels": s.Channels,
		// 扫码入口排行(桌台码):一期就要展示「哪张桌的码被扫得最多」。
		"entries": s.Entries,
	})
}
