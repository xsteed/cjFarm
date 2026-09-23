package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/infra"
	"dining-system/internal/conf"
	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/service"
)

// ============================================================================
// 管理端鉴权
//
// 三条链路(见 docs/user-permission-design.md):
//   1. AdminLogin   —— 查 tb_user 校验账号密码,签发令牌;
//   2. AdminAuth    —— 验签 + 按 uid 加载账号与角色(每次请求一次主键查询),
//                      校验账号启用、角色有效、令牌版本一致 → 401;
//   3. RequirePerm  —— 按「方法 + 路由模板」查权限表判定 → 403(见 perm.go)。
//
// 令牌签发/校验、账号/角色可用性判定、登录限流与密码校验等业务逻辑已下沉到
// service(auth_service.go),本文件只保留 HTTP 机制与出参组装。
// ============================================================================

// AdminUser 返回初始管理员用户名(可用 ADMIN_USER 环境变量覆盖)。
//
// 升级到多员工后,该配置仅在「系统里没有启用的超级管理员」时用于生成引导账号,
// 之后一切以 tb_user 表为准(见 dao.EnsureAdminUser)。
func AdminUser() string {
	return infra.Getenv(conf.EnvAdminUser, conf.DefaultAdminUser)
}

// ============================================================================
// 中间件
// ============================================================================

// AdminAuth 管理端身份校验:验签 → 加载账号与角色 → 校验可用性 → 写入上下文。
func AdminAuth(c *gin.Context) {
	tok := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	auth, status := service.AuthenticateToken(tok)
	switch status {
	case service.AuthzInvalidToken:
		unauthorized(c, "未登录或登录已过期")
		return
	case service.AuthzAccountGone, service.AuthzTokenVersion:
		unauthorized(c, "登录状态已失效,请重新登录")
		return
	case service.AuthzDisabled:
		unauthorized(c, "账号已被停用,请联系管理员")
		return
	case service.AuthzRoleMissing:
		forbidden(c, "账号角色已失效,请联系管理员")
		return
	case service.AuthzRoleDisabled:
		forbidden(c, "账号所属角色已停用,请联系管理员")
		return
	}
	setAuth(c, auth)
	c.Next()
}

// ============================================================================
// 登录 / 改密 / 我的信息
// ============================================================================

// loginResult 登录成功返回的登录态(前端据此渲染菜单与按钮)。
type loginResult struct {
	Token    string   `json:"token"`
	UserID   int      `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	RoleKey  string   `json:"roleKey"`
	RoleName string   `json:"roleName"`
	Perms    []string `json:"perms"`
	// RememberToken 仅在「记住我」登录时返回;前端存本地,用于静默换发新登录态(免登录)。
	// 空串表示本次未启用记住我。
	RememberToken string `json:"rememberToken"`
}

// buildLoginResult 由账号信息构造登录成功返回体(普通登录与「记住我」静默登录共用)。
func buildLoginResult(auth *service.AuthInfo) loginResult {
	return loginResult{
		Token:    service.GenToken(auth.Username, auth.UserID, auth.TokenVersion),
		UserID:   auth.UserID,
		Username: auth.Username,
		RealName: auth.DisplayName(),
		RoleKey:  auth.RoleKey,
		RoleName: auth.RoleName,
		Perms:    auth.Perms,
	}
}

// AdminLogin 管理端登录。
func AdminLogin(c *gin.Context) {
	ip := c.ClientIP()
	if service.LoginIPLocked(ip) {
		WriteLoginLog(c, 0, "", false, "登录失败次数过多，已被临时锁定")
		fail(c, "登录失败次数过多，请稍后再试")
		return
	}
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// 记住我(免登录):勾选后后端签发一条 7/30 天有效的令牌返回给前端。
		Remember bool `json:"remember"`
		Days     int  `json:"days"` // 仅当 Remember 为 true 时生效,1=7天 2=30天(下方归一为 7/30)
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	username := strings.TrimSpace(p.Username)
	if username == "" || p.Password == "" {
		fail(c, "请输入用户名和密码")
		return
	}

	res := service.Authenticate(username, p.Password, ip)
	switch res.Fail {
	case service.LoginFailUserLocked:
		WriteLoginLog(c, 0, res.Username, false, "登录失败次数过多，已被临时锁定")
		fail(c, "登录失败次数过多，请稍后再试")
		return
	case service.LoginFailBadCredentials:
		WriteLoginLog(c, res.UserID, res.Username, false, "用户名或密码错误")
		fail(c, "用户名或密码错误")
		return
	case service.LoginFailDisabled:
		WriteLoginLog(c, res.UserID, res.Username, false, "账号已被停用")
		fail(c, "账号已被停用,请联系管理员")
		return
	case service.LoginFailRoleMissing:
		WriteLoginLog(c, res.UserID, res.Username, false, "账号角色已失效")
		fail(c, "账号角色已失效,请联系管理员")
		return
	case service.LoginFailRoleDisabled:
		WriteLoginLog(c, res.UserID, res.Username, false, "账号所属角色已停用")
		fail(c, "账号所属角色已停用,请联系管理员")
		return
	}

	WriteLoginLog(c, res.Auth.UserID, res.Username, true, "")

	out := buildLoginResult(res.Auth)
	// 「记住我」:签发不透明令牌存库,前端凭它静默换发新登录态(免登录 7/30 天)。
	if p.Remember {
		if tok, err := service.CreateRememberToken(res.Auth.UserID, p.Days, c.GetHeader("User-Agent")); err == nil {
			out.RememberToken = tok
		}
	}
	ok(c, out)
}

// RememberLogin 用「记住我」令牌静默换取新登录态(免登录)。
func RememberLogin(c *gin.Context) {
	var p struct {
		RememberToken string `json:"rememberToken"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.RememberToken == "" {
		unauthorized(c, "登录状态已过期,请重新登录")
		return
	}
	auth, err := service.ConsumeRememberToken(p.RememberToken, c.GetHeader("User-Agent"), c.ClientIP())
	if err != nil {
		unauthorized(c, "登录状态已过期,请重新登录")
		return
	}
	// 与普通登录对齐:免登录进入系统同样要留审计痕迹、刷新最后登录时间。
	WriteOperLog(c, po.OperLog{
		Module:       "登录账号",
		BusinessType: po.OperTypeLogin,
		Action:       "记住我免登录",
		Method:       "POST /api/auth/remember-login",
		OperatorID:   auth.UserID,
		Operator:     auth.Username,
		Status:       po.OperStatusSuccess,
	})
	ok(c, buildLoginResult(auth))
}

// Logout 吊销「记住我」令牌(退出登录 / 取消记住时由前端调用)。
func Logout(c *gin.Context) {
	var p struct {
		RememberToken string `json:"rememberToken"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.RememberToken == "" {
		fail(c, "参数错误")
		return
	}
	service.DeleteRememberToken(p.RememberToken)
	okMsg(c, "已退出登录")
}

// RememberSessions 列出当前账号全部有效的「记住我」会话(登录设备管理页)。
func RememberSessions(c *gin.Context) {
	auth := currentAuth(c)
	if auth == nil {
		unauthorized(c, "未登录或登录已过期")
		return
	}
	sessions, err := service.ListRememberSessions(auth.UserID)
	if err != nil {
		fail(c, "查询登录设备失败")
		return
	}
	out := make([]dto.RememberSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, dto.RememberSession{
			TokenID:      s.TokenID,
			TokenPrefix:  s.TokenPrefix,
			UA:           s.UA,
			CreateTime:   s.CreateTime,
			LastUsedTime: s.LastUsedTime,
			ExpireTime:   s.ExpireTime,
		})
	}
	ok(c, gin.H{"sessions": out})
}

// RememberRevoke 吊销指定的一条「记住我」会话(登录设备管理页)。
func RememberRevoke(c *gin.Context) {
	auth := currentAuth(c)
	if auth == nil {
		unauthorized(c, "未登录或登录已过期")
		return
	}
	var p struct {
		TokenID int `json:"tokenId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.TokenID <= 0 {
		fail(c, "参数错误")
		return
	}
	deleted, err := service.RevokeRememberSession(auth.UserID, p.TokenID)
	if err != nil || !deleted {
		fail(c, "登录设备不存在或已失效")
		return
	}
	okMsg(c, "已吊销该设备")
}

// Profile 返回当前登录者的身份与权限,供前端刷新登录态使用。
func Profile(c *gin.Context) {
	auth := currentAuth(c)
	if auth == nil {
		unauthorized(c, "未登录或登录已过期")
		return
	}
	ok(c, gin.H{
		"userId":   auth.UserID,
		"username": auth.Username,
		"realName": auth.DisplayName(),
		"roleKey":  auth.RoleKey,
		"roleName": auth.RoleName,
		"perms":    auth.Perms,
	})
}

// ChangePassword 修改「当前登录者自己」的密码(多员工语义:只影响本人)。
func ChangePassword(c *gin.Context) {
	var p struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	auth := currentAuth(c)
	if auth == nil {
		unauthorized(c, "未登录或登录已过期")
		return
	}
	if err := service.ChangePassword(auth.UserID, p.OldPassword, p.NewPassword, auth.DisplayName()); err != nil {
		fail(c, err.Error())
		return
	}
	// 密码本身不进日志(请求体里的新旧密码已由 MaskParams 脱敏),只留动作。
	SetAuditDetail(c, "user", strconv.Itoa(auth.UserID), "修改本人登录密码")
	okMsg(c, "密码修改成功，请重新登录")
}
