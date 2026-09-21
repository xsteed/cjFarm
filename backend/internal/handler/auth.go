package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
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
// 令牌载荷: 用户名|uid|token_version|过期时间,用 HMAC-SHA256 签名。
//   用户名放在第一段是为了让既有 adminName() 的取值逻辑零改动;
//   token_version 用于「改密 / 强制下线」后让旧令牌立即失效。
// 密钥在进程启动时随机生成,重启后旧令牌全部失效(因此无需兼容历史令牌格式)。
// ============================================================================

// tokenTTL 令牌有效期,可通过 TOKEN_TTL_HOURS 环境变量覆盖(单位:小时),默认 24 小时。
// 设置较短有效期可缩小令牌泄露后的风险窗口。
func tokenTTL() time.Duration {
	if v, err := strconv.Atoi(store.Getenv("TOKEN_TTL_HOURS", "")); err == nil && v > 0 && v <= 24*30 {
		return time.Duration(v) * time.Hour
	}
	return 24 * time.Hour
}

var authSecret string

func init() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下回退为固定密钥(仅影响本进程生命周期)
		authSecret = hex.EncodeToString([]byte("dining-fallback-secret"))
		return
	}
	authSecret = hex.EncodeToString(b)
}

// AdminUser 返回初始管理员用户名(可用 ADMIN_USER 环境变量覆盖)。
//
// 升级到多员工后,该配置仅在「系统里没有启用的超级管理员」时用于生成引导账号,
// 之后一切以 tb_user 表为准(见 store.EnsureAdminUser)。
func AdminUser() string {
	return store.Getenv("ADMIN_USER", "admin")
}

// ---- 登录防暴力破解限流(内存级) ----
// 按来源 IP 记录连续登录失败次数,达到阈值后锁定一段时间,登录成功后清零。
// 真实 IP 由 gin 的 ClientIP() 依据 main 中 SetTrustedProxies 配置解析:
//   - 默认不信任任何代理(取 RemoteAddr),直连/无代理部署下即为真实 IP;
//   - 经 nginx 反代时,需设置 TRUSTED_PROXIES 环境变量(如 127.0.0.1 或 nginx 的 IP),
//     gin 才会信任 X-Forwarded-For 并取到真实客户端 IP,否则退化为全局粒度。
const (
	maxLoginFailures  = 5                // 连续失败次数阈值
	loginLockDuration = 15 * time.Minute // 触发阈值后的锁定时长
)

type loginEntry struct {
	failCount   int
	lockedUntil time.Time
}

type loginGuard struct {
	mu sync.Mutex
	m  map[string]*loginEntry
}

var guard = &loginGuard{m: make(map[string]*loginEntry)}

// locked 判断该 IP 是否处于锁定期;若锁定已过期则顺带清理。
func (g *loginGuard) locked(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.m[ip]
	if !ok {
		return false
	}
	if e.lockedUntil.IsZero() {
		return false // 尚未进入锁定期
	}
	if time.Now().After(e.lockedUntil) {
		delete(g.m, ip) // 锁定已过期
		return false
	}
	return true
}

// fail 记录一次失败;达到阈值则进入锁定期并重置计数。
func (g *loginGuard) fail(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.m[ip]
	if !ok {
		e = &loginEntry{}
		g.m[ip] = e
	}
	e.failCount++
	if e.failCount >= maxLoginFailures {
		e.lockedUntil = time.Now().Add(loginLockDuration)
		e.failCount = 0
	}
}

// clear 登录成功后清除该 IP 的失败记录。
func (g *loginGuard) clear(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, ip)
}

// ============================================================================
// 令牌
// ============================================================================

// tokenClaims 令牌载荷。
type tokenClaims struct {
	Username     string
	UserID       int
	TokenVersion int
}

func genToken(user string, uid, tokenVersion int) string {
	exp := time.Now().Add(tokenTTL()).Unix()
	payload := fmt.Sprintf("%s|%d|%d|%d", user, uid, tokenVersion, exp)
	mac := hmac.New(sha256.New, []byte(authSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// verifyToken 校验签名与有效期,返回载荷。用户名不允许含 '|'(建号时已校验)。
func verifyToken(tok string) (*tokenClaims, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(authSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, false
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 4 {
		return nil, false
	}
	uid, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, false
	}
	ver, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, false
	}
	exp, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return nil, false
	}
	if exp <= time.Now().Unix() {
		return nil, false
	}
	return &tokenClaims{Username: fields[0], UserID: uid, TokenVersion: ver}, true
}

// ============================================================================
// 中间件
// ============================================================================

// AdminAuth 管理端身份校验:验签 → 加载账号与角色 → 校验可用性 → 写入上下文。
//
// 每次请求按 uid 查一次库(单次主键查询),换取「改角色 / 停用账号 / 改密码
// 立刻生效」——不做内存缓存是有意为之,行为显然正确且易于测试。
func AdminAuth(c *gin.Context) {
	tok := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	claims, valid := verifyToken(tok)
	if !valid {
		unauthorized(c, "未登录或登录已过期")
		return
	}
	auth, err := store.GetAuthByID(claims.UserID)
	if err != nil {
		unauthorized(c, "登录状态已失效,请重新登录")
		return
	}
	if auth.Status != model.UserStatusEnabled {
		unauthorized(c, "账号已被停用,请联系管理员")
		return
	}
	if auth.TokenVersion != claims.TokenVersion {
		unauthorized(c, "登录状态已失效,请重新登录")
		return
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		forbidden(c, "账号角色已失效,请联系管理员")
		return
	}
	if auth.RoleStatus != model.UserStatusEnabled {
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
	Token         string   `json:"token"`
	UserID        int      `json:"userId"`
	Username      string   `json:"username"`
	RealName      string   `json:"realName"`
	RoleKey       string   `json:"roleKey"`
	RoleName      string   `json:"roleName"`
	Perms         []string `json:"perms"`
	// RememberToken 仅在「记住我」登录时返回;前端存本地,用于静默换发新登录态(免登录)。
	// 空串表示本次未启用记住我。
	RememberToken string `json:"rememberToken"`
}

// buildLoginResult 由账号信息构造登录成功返回体(普通登录与「记住我」静默登录共用)。
func buildLoginResult(auth *store.AuthInfo) loginResult {
	return loginResult{
		Token:    genToken(auth.Username, auth.UserID, auth.TokenVersion),
		UserID:   auth.UserID,
		Username: auth.Username,
		RealName: auth.DisplayName(),
		RoleKey:  auth.RoleKey,
		RoleName: auth.RoleName,
		Perms:    auth.Perms,
	}
}

// AdminLogin 管理端登录。
//
// 账号密码一律以 tb_user 表为准。老版本「用 ADMIN_USER / ADMIN_PASS 登录」的商户,
// 升级后首次启动会由 store.EnsureAdminUser 自动生成同名超级管理员账号
// (密码取库中已有的 admin_pass_hash 或 ADMIN_PASS),因此凭原有密码照常登录。
func AdminLogin(c *gin.Context) {
	ip := c.ClientIP()
	if guard.locked(ip) {
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

	auth, err := store.GetAuthByUsername(username)
	if err != nil {
		// 账号不存在:仍执行一次 bcrypt 校验以抹平响应时间差异,避免枚举用户名。
		store.WastePasswordVerify(p.Password)
		guard.fail(ip)
		WriteLoginLog(c, 0, username, false, "用户名或密码错误")
		fail(c, "用户名或密码错误")
		return
	}
	if !store.VerifyPassword(p.Password, store.GetUserPasswordHash(auth.UserID)) {
		guard.fail(ip)
		WriteLoginLog(c, auth.UserID, username, false, "用户名或密码错误")
		fail(c, "用户名或密码错误")
		return
	}
	if auth.Status != model.UserStatusEnabled {
		// 明确提示「已停用」会暴露账号存在性,但餐饮场景下「登不上去」比
		// 「账号存在性」重要得多(服务员要立刻知道该找谁),此处有意选择明确提示。
		guard.fail(ip)
		WriteLoginLog(c, auth.UserID, username, false, "账号已被停用")
		fail(c, "账号已被停用,请联系管理员")
		return
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		guard.fail(ip)
		WriteLoginLog(c, auth.UserID, username, false, "账号角色已失效")
		fail(c, "账号角色已失效,请联系管理员")
		return
	}
	if auth.RoleStatus != model.UserStatusEnabled {
		guard.fail(ip)
		WriteLoginLog(c, auth.UserID, username, false, "账号所属角色已停用")
		fail(c, "账号所属角色已停用,请联系管理员")
		return
	}

	guard.clear(ip)
	store.RecordLogin(auth.UserID, ip)
	WriteLoginLog(c, auth.UserID, username, true, "")
	// 历史旧格式哈希(单次 SHA-256)在校验通过后自动升级为 bcrypt。
	hash := store.GetUserPasswordHash(auth.UserID)
	if store.IsLegacyHash(hash) {
		store.SetUserPassword(auth.UserID, store.HashPassword(p.Password), auth.Username)
	}

	res := buildLoginResult(auth)
	// 「记住我」:签发不透明令牌存库,前端凭它静默换发新登录态(免登录 7/30 天)。
	// 过期与否完全由后端 tb_remember_token.expire_time 决定,前端无法篡改。
	// 未勾选则作废该账号已有的记住令牌,确保「取消记住」能真正生效、不残留。
	if p.Remember {
		days := 7
		if p.Days == 30 {
			days = 30
		}
		if tok, err := store.CreateRememberToken(auth.UserID, days); err == nil {
			res.RememberToken = tok
		}
	} else {
		store.DeleteRememberTokensByUser(auth.UserID)
	}
	ok(c, res)
}

// RememberLogin 用「记住我」令牌静默换取新登录态(免登录)。
//
// 与普通登录的区别:不发账号密码,只凭后端签发的随机令牌;令牌是否有效、是否过期,
// 完全由后端查 tb_remember_token 裁决(见 store.ConsumeRememberToken),
// 前端只持有这个不透明串,既不能伪造也无法自行决定有效期。令牌无效/过期时返回 401,
// 前端据此清掉本地令牌并退回登录页。
func RememberLogin(c *gin.Context) {
	var p struct {
		RememberToken string `json:"rememberToken"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.RememberToken == "" {
		unauthorized(c, "登录状态已过期,请重新登录")
		return
	}
	auth, err := store.ConsumeRememberToken(p.RememberToken)
	if err != nil {
		unauthorized(c, "登录状态已过期,请重新登录")
		return
	}
	ok(c, buildLoginResult(auth))
}

// Profile 返回当前登录者的身份与权限,供前端刷新登录态使用。
//
// 必要性:前端把权限缓存在 localStorage,商户改了某人角色后,该员工不重新登录
// 就仍带着旧权限(会显示不该显示的按钮)。刷新时重新拉一次即可收敛,
// 避免出现「看得见按钮但点了报 403」的迷惑体验。
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
//
// 写入目标从早期版本的 tb_config.admin_pass_hash 改为 tb_user.password_hash,
// 并把 token_version +1 —— 旧令牌立即失效,改密后需重新登录。
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
	if !store.VerifyPassword(p.OldPassword, store.GetUserPasswordHash(auth.UserID)) {
		fail(c, "原密码错误")
		return
	}
	if len(p.NewPassword) < 6 {
		fail(c, "新密码至少 6 位")
		return
	}
	if len(p.NewPassword) > 64 {
		fail(c, "新密码过长")
		return
	}
	if p.NewPassword == p.OldPassword {
		fail(c, "新密码不能与原密码相同")
		return
	}
	if err := store.SetUserPassword(auth.UserID, store.HashPassword(p.NewPassword), auth.DisplayName()); err != nil {
		fail(c, "密码保存失败，请稍后重试")
		return
	}
	// 改密后作废该账号的全部「记住我」会话,避免旧令牌继续免登录。
	store.DeleteRememberTokensByUser(auth.UserID)
	// 密码本身不进日志(请求体里的新旧密码已由 MaskParams 脱敏),只留动作。
	SetAuditDetail(c, "user", strconv.Itoa(auth.UserID), "修改本人登录密码")
	okMsg(c, "密码修改成功，请重新登录")
}
