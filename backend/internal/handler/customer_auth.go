// 顾客会员的 HTTP 层:一键注册/登录、会员卡、我的订单,以及顾客身份中间件。
//
// 与管理端鉴权(auth.go)的关键差异:
//   - 挂在公开组 /api/customer/* 下,无权限体系(routePerms 不涉及顾客组);
//   - 身份是「可选」的:下单/加菜带不带顾客令牌都放行 —— 匿名路径永不断(见 spec D1);
//   - 错误文案直接面向顾客,不做内部细节脱敏(service 层已保证只返回可读文案)。
package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/service"
)

// ctxKeyCustomerID 顾客会员 ID 在 gin context 里的键名。
const ctxKeyCustomerID = "customerID"

// bearerToken 提取 Authorization: Bearer <token> 里的令牌。
func bearerToken(c *gin.Context) string {
	return strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
}

// ============================================================================
// 中间件
// ============================================================================

// CustomerAuthOptional 可选顾客身份:带了合法顾客令牌就把会员 ID 写进上下文;
// 没带、令牌过期或无效一律放行(匿名路径永不断,会员归属只是「锦上添花」)。
func CustomerAuthOptional(c *gin.Context) {
	if tok := bearerToken(c); tok != "" {
		if cust, status := service.AuthenticateCustomerToken(tok); status == service.CustomerAuthzOK {
			c.Set(ctxKeyCustomerID, cust.CustomerID)
		}
	}
	c.Next()
}

// CustomerAuthRequired 必选顾客身份(会员专属接口):无效/过期/停用一律 401。
// 令牌过期是常态(2h TTL),小程序端会静默重登后重放,这里文案保持「请先登录」即可。
func CustomerAuthRequired(c *gin.Context) {
	cust, status := service.AuthenticateCustomerToken(bearerToken(c))
	switch status {
	case service.CustomerAuthzOK:
		c.Set(ctxKeyCustomerID, cust.CustomerID)
		c.Next()
		return
	case service.CustomerAuthzDisabled:
		unauthorized(c, "会员账号不可用")
	default:
		unauthorized(c, "请先登录")
	}
}

// customerIDFrom 取当前请求解析出的会员 ID(未登录为 0)。
func customerIDFrom(c *gin.Context) int {
	if v, ok := c.Get(ctxKeyCustomerID); ok {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}

// ============================================================================
// 接口
// ============================================================================

// memberView 会员卡的对外视图:只暴露展示字段,openid 永不出接口。
func memberView(c *po.Customer) gin.H {
	return gin.H{
		"customerId":  c.CustomerID,
		"nickname":    c.DisplayName(),
		"avatarUrl":   c.AvatarURL,
		"memberSince": c.CreateTime, // 入会日期
		"orderCount":  c.OrderCount,
	}
}

// CustomerMemberLogin 微信一键注册/登录。
// isNew 区分「🎉 注册成功，已加入会员」与「欢迎回来」两种前端反馈。
func CustomerMemberLogin(c *gin.Context) {
	var p struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.Code) == "" {
		fail(c, "参数错误")
		return
	}
	if !service.MemberEnabled() {
		fail(c, "会员功能未开启")
		return
	}
	res, err := service.CustomerLogin(p.Code, c.ClientIP())
	if err != nil {
		// service 层已保证错误文案可直接面向顾客(限流/微信错误码翻译/系统繁忙)。
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{
		"token":    res.Token,
		"isNew":    res.IsNew,
		"customer": memberView(res.Customer),
	})
}

// CustomerMemberProfile 会员卡信息(需顾客登录态)。
func CustomerMemberProfile(c *gin.Context) {
	cust, err := service.CustomerMemberProfile(customerIDFrom(c))
	if err != nil {
		unauthorized(c, err.Error())
		return
	}
	ok(c, memberView(cust))
}

// CustomerMyOrders 会员的历史订单(跨桌,按下单时间倒序分页)。
func CustomerMyOrders(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	total, rows, err := service.CustomerMyOrders(customerIDFrom(c), pageNum, pageSize)
	if err != nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, gin.H{"total": total, "items": rows})
}
