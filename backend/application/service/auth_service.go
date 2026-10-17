// 认证与用户域用例:令牌签发/校验、登录限流、账号密码校验、
// 记住我会话、员工与角色管理。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// 以下别名让 handler 在调用 service 时无需再 import dao/store,
// 数据访问层类型仍然保持唯一权威定义在 dao 包。
type (
	AuthInfo           = dao.AuthInfo
	UserRow            = dao.UserRow
	RoleRow            = dao.RoleRow
	RememberSessionRow = dao.RememberSessionRow
)

// ============================================================================
// 访问令牌(管理端登录态)
// ============================================================================

// TokenClaims 令牌载荷。
type TokenClaims struct {
	Username     string
	UserID       int
	TokenVersion int
}

// TokenTTL 令牌有效期,可通过 TOKEN_TTL_HOURS 环境变量覆盖(单位:小时),默认 24 小时。
func TokenTTL() time.Duration {
	if v, err := strconv.Atoi(infrastructure.Getenv(conf.EnvTokenTTLHours, conf.DefaultTokenTTLHours)); err == nil && v > 0 && v <= 24*30 {
		return time.Duration(v) * time.Hour
	}
	return 24 * time.Hour
}

func init() {
	// 包加载期先做一次只读解析,保证测试与首个请求前密钥就绪(此阶段不落盘);
	// 生产启动在 main 装配完部署配置后还会调用 infrastructure.InitAuthKey() 重新解析并落盘,
	// 因此 TOKEN_SECRET / data/auth.key 会被正确识别为最终签名密钥。
	//
	// 解析不出密钥不能静默:签名密钥为空会让所有令牌验签失败,表现为「全员登录不上」,
	// 而日志里若无线索就只能靠猜。这里先告警,main 里的 InitAuthKey 还会再兜一次。
	if k := infrastructure.LoadAuthKey(); k == "" {
		logger.Warnf("[auth] 包加载期未能解析出登录令牌签名密钥,后续令牌验签将全部失败")
	}
}

// GenToken 签发访问令牌:用户名|uid|token_version|过期时间,HMAC-SHA256 签名。
func GenToken(username string, userID, tokenVersion int) string {
	exp := time.Now().Add(TokenTTL()).Unix()
	payload := fmt.Sprintf("%s|%d|%d|%d", username, userID, tokenVersion, exp)
	mac := hmac.New(sha256.New, []byte(infrastructure.LoadAuthKey()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// VerifyToken 校验签名与有效期,返回载荷。用户名不允许含 '|'(建号时已校验)。
func VerifyToken(tok string) (*TokenClaims, bool) {
	claims, exp, ok := parseToken(tok)
	if !ok || exp <= time.Now().Unix() {
		return nil, false
	}
	return claims, true
}

// VerifyTokenSignature 只校验签名与载荷结构,不校验有效期。
//
// 专供审计留痕(如 401/403 的越权记录,见 handler.writeDeniedLog):需要确认
// 「操作人姓名确实出自本系统签发的令牌」以防伪造,但令牌已过期并不代表它没有审计价值
// —— 恰恰相反,「谁在用过期/失效令牌访问」正是要留痕的线索。故此处不做过期判定。
func VerifyTokenSignature(tok string) (*TokenClaims, bool) {
	claims, _, ok := parseToken(tok)
	return claims, ok
}

// parseToken 校验 HMAC 签名与载荷结构,返回载荷与过期时间(Unix 秒)。
// 不做过期判断,由调用方决定语义:VerifyToken 判过期,VerifyTokenSignature 不判。
func parseToken(tok string) (*TokenClaims, int64, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return nil, 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, 0, false
	}
	mac := hmac.New(sha256.New, []byte(infrastructure.LoadAuthKey()))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, 0, false
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 4 {
		return nil, 0, false
	}
	uid, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, 0, false
	}
	ver, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, 0, false
	}
	exp, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return nil, 0, false
	}
	return &TokenClaims{Username: fields[0], UserID: uid, TokenVersion: ver}, exp, true
}

// ============================================================================
// 鉴权中间件支撑:验签 → 加载账号 → 校验可用性
// ============================================================================

// AuthzStatus 鉴权结果分类,由 handler 映射到 401/403 与具体文案。
type AuthzStatus int

const (
	AuthzOK AuthzStatus = iota
	AuthzInvalidToken
	AuthzAccountGone
	AuthzDisabled
	AuthzTokenVersion
	AuthzRoleMissing
	AuthzRoleDisabled
)

// AuthenticateToken 校验访问令牌并加载账号/角色快照,按可用性分类返回状态。
//
// ctx 用于把 requestID 带进日志:这里的 Warn 几乎都是「不该发生的 401」,
// 排查时必须能定位到「是哪一个请求、带的哪个令牌」。
func AuthenticateToken(ctx context.Context, tok string) (*dao.AuthInfo, AuthzStatus) {
	claims, valid := VerifyToken(tok)
	if !valid {
		return nil, AuthzInvalidToken
	}
	auth, err := dao.GetAuthByID(claims.UserID)
	if err != nil {
		// 签名有效却加载不到账号:可能是账号已删(正常失效),也可能是数据库故障
		// (异常)。后者会被 401 完全掩盖 —— 用户只看到「请重新登录」,
		// 监控看到接口正常,只有日志能把这两者区分开,故必须留痕。
		logger.CtxWarnf(ctx, "[auth] 令牌签名有效但加载账号失败(账号已删除或数据库故障) uid=%d: %v", claims.UserID, err)
		return nil, AuthzAccountGone
	}
	if auth.Status != po.UserStatusEnabled {
		logger.CtxWarnf(ctx, "[auth] 令牌有效但账号已停用,拒绝访问 uid=%d 用户名=%s", auth.UserID, auth.Username)
		return nil, AuthzDisabled
	}
	if auth.TokenVersion != claims.TokenVersion {
		// 改密/重置后旧令牌仍在用:单独留痕,便于判断「是不是令牌没失效」类问题。
		logger.CtxWarnf(ctx, "[auth] 令牌版本不匹配,登录态已作废 uid=%d 用户名=%s 令牌版本=%d 当前版本=%d",
			claims.UserID, auth.Username, claims.TokenVersion, auth.TokenVersion)
		return nil, AuthzTokenVersion
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		logger.CtxWarnf(ctx, "[auth] 令牌有效但账号未分配有效角色,拒绝访问 uid=%d 用户名=%s", auth.UserID, auth.Username)
		return nil, AuthzRoleMissing
	}
	if auth.RoleStatus != po.UserStatusEnabled {
		logger.CtxWarnf(ctx, "[auth] 令牌有效但角色已停用,拒绝访问 uid=%d 用户名=%s 角色=%s",
			auth.UserID, auth.Username, auth.RoleKey)
		return nil, AuthzRoleDisabled
	}
	return auth, AuthzOK
}

// ============================================================================
// 顾客会员令牌(小程序登录态)
// ============================================================================

// customerTokenTTL 顾客令牌有效期。刻意比管理端短:顾客端有「静默重登」兜底
// (wx.login 免费且无感知,令牌过期/401 时小程序自动重新登录),短有效期换来
// 「停用会员」更快生效 —— 令牌无状态,不查库就无法感知 status 变化。
const customerTokenTTL = 2 * time.Hour

// customerTokenPrefix 顾客令牌载荷前缀。与管理端令牌(username|uid|ver|exp)形态不同,
// 两者互不可兑换:管理端令牌拿到顾客接口会被拒,反之亦然。
const customerTokenPrefix = "customer"

// GenCustomerToken 签发顾客令牌:customer|<customerID>|<exp>,HMAC-SHA256 签名。
func GenCustomerToken(customerID int) string {
	exp := time.Now().Add(customerTokenTTL).Unix()
	payload := fmt.Sprintf("%s|%d|%d", customerTokenPrefix, customerID, exp)
	mac := hmac.New(sha256.New, []byte(infrastructure.LoadAuthKey()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// VerifyCustomerToken 校验顾客令牌的签名与有效期,返回会员 ID。
// 与管理端 VerifyToken 的差异只有三点:前缀固定、无 tokenVersion(顾客无「改密下线」概念)、
// 载荷少一个字段。
func VerifyCustomerToken(tok string) (int, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, []byte(infrastructure.LoadAuthKey()))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return 0, false
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 3 || fields[0] != customerTokenPrefix {
		return 0, false
	}
	id, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || exp <= time.Now().Unix() {
		return 0, false
	}
	return id, true
}

// CustomerAuthzStatus 顾客登录态校验结果分类,由 handler 映射到 401 与文案。
type CustomerAuthzStatus int

const (
	CustomerAuthzOK CustomerAuthzStatus = iota
	CustomerAuthzInvalidToken
	CustomerAuthzGone
	CustomerAuthzDisabled
)

// AuthenticateCustomerToken 校验顾客令牌并加载会员档案,按可用性分类返回状态。
// 与管理端 AuthenticateToken 的差异:无角色/令牌版本概念,只看档案是否存在且启用。
func AuthenticateCustomerToken(tok string) (*po.Customer, CustomerAuthzStatus) {
	id, ok := VerifyCustomerToken(tok)
	if !ok {
		return nil, CustomerAuthzInvalidToken
	}
	c, err := dao.GetCustomerByID(id)
	if err != nil {
		return nil, CustomerAuthzGone
	}
	if c == nil {
		return nil, CustomerAuthzGone
	}
	if c.Status != po.CustomerStatusEnabled {
		return nil, CustomerAuthzDisabled
	}
	return c, CustomerAuthzOK
}

// ============================================================================
// 登录防暴力破解限流(内存级)
// ============================================================================

const (
	maxLoginFailures  = 5                // 连续失败次数阈值
	loginLockDuration = 15 * time.Minute // 触发阈值后的锁定时长
	loginIdleTTL      = 24 * time.Hour   // 失败记录的闲置回收时长
	loginGuardMaxKeys = 4096             // 触发闲置回收的条目上限
)

type loginEntry struct {
	failCount   int
	lockedUntil time.Time
	lastFail    time.Time
}

type loginGuard struct {
	mu        sync.Mutex
	m         map[string]*loginEntry
	lastPrune time.Time
}

var guard = &loginGuard{m: make(map[string]*loginEntry)}

// locked 判断该维度是否处于锁定期;若锁定已过期则顺带清理。
func (g *loginGuard) locked(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.m[key]
	if !ok {
		return false
	}
	if e.lockedUntil.IsZero() {
		return false
	}
	if time.Now().After(e.lockedUntil) {
		delete(g.m, key)
		return false
	}
	return true
}

// fail 记录一次失败;达到阈值则进入锁定期并重置计数。
func (g *loginGuard) fail(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.m[key]
	if !ok {
		e = &loginEntry{}
		g.m[key] = e
	}
	e.failCount++
	e.lastFail = time.Now()
	if e.failCount >= maxLoginFailures {
		e.lockedUntil = time.Now().Add(loginLockDuration)
		e.failCount = 0
	}
	g.pruneLocked()
}

// pruneLocked 闲置回收:条目过多时清掉「长期无失败」的记录。
// 回收只看 lastFail 是否超过 idle TTL——锁定期最长 15 分钟,远小于 idle TTL,
// 因此锁定中的条目不会在锁定期内被误删。扫描加 1 分钟节流,避免每次失败都全表扫描。
//
// 但只靠 idle TTL 回收并不能限制规模:攻击者轮换源 IP 持续打登录接口时,每个新条目的
// lastFail 都是新鲜的,一次也删不掉,map 会单调增长(登录接口无鉴权,可被无限打)。
// 因此这里再压一道**硬上限**:超额时按「最久未活动」淘汰一批,把规模钉死在
// loginGuardMaxKeys 量级 —— 这是防内存耗尽,不是防暴力破解(锁定语义见 locked/fail)。
func (g *loginGuard) pruneLocked() {
	if len(g.m) > loginGuardMaxKeys {
		g.evictOldestLocked()
	}
	if time.Since(g.lastPrune) < time.Minute {
		return
	}
	g.lastPrune = time.Now()
	if len(g.m) <= loginGuardMaxKeys {
		return
	}
	cutoff := time.Now().Add(-loginIdleTTL)
	for k, e := range g.m {
		if e.lastFail.Before(cutoff) {
			delete(g.m, k)
		}
	}
}

// evictOldestLocked 按「最久未活动」淘汰,把 map 收缩到硬上限的 3/4。
// 一次淘汰一批(而非一条)是为了摊薄 O(n) 扫描:持续撞库时每条失败请求都触发一次
// 全表扫描,本身就成了放大器。
//
// 排序键取 max(lastFail, lockedUntil):仍处于锁定期的条目排到最后,不会被优先淘汰,
// 否则攻击者可以用海量新 IP 把「已锁定的 IP」挤出去,反而提前解除锁定。
func (g *loginGuard) evictOldestLocked() {
	target := loginGuardMaxKeys / 4 * 3
	if target < 1 {
		target = 1
	}
	type kv struct {
		key    string
		active time.Time
	}
	all := make([]kv, 0, len(g.m))
	for k, e := range g.m {
		active := e.lastFail
		if e.lockedUntil.After(active) {
			active = e.lockedUntil
		}
		all = append(all, kv{k, active})
	}
	// 只要最旧的 len(all)-target 条:全量排序实现简单且条目数被硬上限约束(数千级)。
	sort.Slice(all, func(i, j int) bool { return all[i].active.Before(all[j].active) })
	for i := 0; i < len(all)-target; i++ {
		delete(g.m, all[i].key)
	}
}

// clear 登录成功后清除该维度的失败记录。
func (g *loginGuard) clear(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, key)
}

// LoginIPLocked 判断来源 IP 是否处于登录失败锁定期。
func LoginIPLocked(ip string) bool {
	return guard.locked("ip:" + ip)
}

// ============================================================================
// 登录 / 账号密码校验
// ============================================================================

// LoginFail 登录失败原因分类,由 handler 映射为审计文案与 HTTP 文案。
type LoginFail int

const (
	LoginOK LoginFail = iota
	LoginFailIPLocked
	LoginFailUserLocked
	LoginFailBadCredentials
	LoginFailDisabled
	LoginFailRoleMissing
	LoginFailRoleDisabled
)

// LoginResult 一次登录尝试的裁决结果。
type LoginResult struct {
	Auth     *dao.AuthInfo // 成功时非 nil
	Fail     LoginFail
	UserID   int    // 账号存在时的 uid(审计 OperatorID)
	Username string // trim 后的用户名(审计 Operator)
}

// Authenticate 执行账号密码登录:限流判定、账号查询、密码校验、可用性校验、
// 成功落库(登录时间/次数)与旧哈希升级。失败时已同步更新限流计数。
//
// ctx 用于把 requestID 带进日志:一次登录涉及多条日志(锁定/密码错误/成功),
// 靠 requestID 才能把「同一次尝试」串起来,也才能与访问日志、审计表相互印证。
func Authenticate(ctx context.Context, username, password, ip string) LoginResult {
	ipKey := "ip:" + ip

	// 本函数会连打多条日志,且都围绕同一个被尝试的账号与来源 IP:
	// 一次绑定、后续复用,避免每条日志重复拼这两段字符串。
	log := logger.WithContext(ctx, zap.String("username", username), zap.String("ip", ip))

	// 账号维度锁定:只对真实存在的账号计数,避免攻击者刷不存在的用户名把内存刷爆。
	// 查询失败时按「不存在」处理(不启用用户维度锁),但必须留痕 —— 静默跳过会让
	// 账号维度防爆破在 DB 故障时失效而无人知晓。
	var userKey string
	if taken, existsErr := dao.UsernameExists(username, 0); existsErr != nil {
		logger.CtxWarnf(ctx, "[auth] 用户名查重查询失败,本次不启用账号维度锁定: %v", existsErr)
	} else if taken {
		userKey = "user:" + username
		if guard.locked(userKey) {
			// 锁定分支也执行一次完整 bcrypt:与下方「账号不存在」分支保持同等耗时,
			// 否则攻击者可用响应时间差区分「账号存在且被锁定」(配合下方文案差异
			// 构成用户名枚举信道)。
			store.WastePasswordVerify(password)
			log.Warnf("[auth] 登录被拒:账号处于锁定期(连续失败过多)")
			return LoginResult{Fail: LoginFailUserLocked, Username: username}
		}
	}

	failBoth := func() {
		guard.fail(ipKey)
		if userKey != "" {
			guard.fail(userKey)
		}
	}

	// 登录成败一律留痕:审计表(AUDIT_ENABLED)可被关掉,而撞库、口令泄露这类
	// 安全事件恰恰需要在系统日志里被发现 —— 审计是「业务留痕」,日志是「运维告警」,
	// 两者不可互相替代。用户名可以打(它本就要进审计),密码任何情况下都不打。
	auth, err := dao.GetAuthByUsername(username)
	if err != nil {
		// 账号不存在:仍执行一次 bcrypt 校验以抹平响应时间差异,避免枚举用户名。
		store.WastePasswordVerify(password)
		guard.fail(ipKey)
		log.Warnf("[auth] 登录失败:账号不存在")
		return LoginResult{Fail: LoginFailBadCredentials, Username: username}
	}
	// 账号存在时补上 uid:它在后续每条日志里都有用(与审计表的 OperatorID 对齐)。
	log = log.With(zap.Int("uid", auth.UserID))
	if !store.VerifyPassword(password, dao.GetUserPasswordHash(auth.UserID)) {
		failBoth()
		log.Warnf("[auth] 登录失败:密码错误")
		return LoginResult{Fail: LoginFailBadCredentials, UserID: auth.UserID, Username: username}
	}
	if auth.Status != po.UserStatusEnabled {
		failBoth()
		log.Warnf("[auth] 登录失败:账号已停用")
		return LoginResult{Fail: LoginFailDisabled, UserID: auth.UserID, Username: username}
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		failBoth()
		log.Warnf("[auth] 登录失败:账号无有效角色")
		return LoginResult{Fail: LoginFailRoleMissing, UserID: auth.UserID, Username: username}
	}
	if auth.RoleStatus != po.UserStatusEnabled {
		failBoth()
		log.Warnf("[auth] 登录失败:账号所属角色已停用 角色=%s", auth.RoleKey)
		return LoginResult{Fail: LoginFailRoleDisabled, UserID: auth.UserID, Username: username}
	}

	guard.clear(ipKey)
	if userKey != "" {
		guard.clear(userKey)
	}
	log.Infof("[auth] 登录成功")
	dao.RecordLogin(auth.UserID, ip)
	// 历史旧格式哈希(单次 SHA-256)在校验通过后自动升级为 bcrypt。
	//
	// 注意:SetUserPassword 会把 token_version 自增(改密即作废该账号全部旧令牌)。
	// 这里必须重新读取账号快照,否则调用方仍用「升级前」的 token_version 签发令牌,
	// 令牌版本比库里少 1 → 该账号第一次登录拿到令牌后,所有管理端接口立刻 401,
	// 必须再登录一次才正常。bcrypt 持续失败(回退 legacy 哈希)时更会每次登录都自增
	// → 该账号永久拿不到有效令牌。
	hash := dao.GetUserPasswordHash(auth.UserID)
	if store.IsLegacyHash(hash) {
		if err := dao.SetUserPassword(auth.UserID, store.HashPassword(password), auth.Username); err != nil {
			log.Warnf("[auth] 旧哈希升级失败,本次按升级前的令牌版本签发: %v", err)
		} else if fresh, err := dao.GetAuthByUsername(username); err == nil && fresh != nil {
			auth = fresh
		} else {
			// 兜底:重读失败也要让令牌版本与库内一致(SetUserPassword 已自增)。
			auth.TokenVersion++
		}
	}
	return LoginResult{Auth: auth, UserID: auth.UserID, Username: username}
}

// ChangePassword 修改指定账号自己的密码,并在成功后作废其全部记住我会话。
func ChangePassword(userID int, oldPassword, newPassword, actor string) error {
	if !store.VerifyPassword(oldPassword, dao.GetUserPasswordHash(userID)) {
		return errors.New("原密码错误")
	}
	if len(newPassword) < 6 {
		return errors.New("新密码至少 6 位")
	}
	if len(newPassword) > 64 {
		return errors.New("新密码过长")
	}
	if newPassword == oldPassword {
		return errors.New("新密码不能与原密码相同")
	}
	if err := dao.SetUserPassword(userID, store.HashPassword(newPassword), actor); err != nil {
		return errors.New("密码保存失败，请稍后重试")
	}
	dao.DeleteRememberTokensByUser(userID)
	return nil
}

// ============================================================================
// 记住我(免登录)
// ============================================================================

// FingerprintUA 从 User-Agent 提取「浏览器·系统」指纹,用于「记住我」令牌的环境绑定。
func FingerprintUA(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	browser := "其他浏览器"
	switch {
	case strings.Contains(ua, "MicroMessenger"):
		browser = "微信"
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	osName := "未知系统"
	switch {
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		osName = "iOS"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		osName = "macOS"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		osName = "Linux"
	}
	return browser + "·" + osName
}

// rememberPermanentDays 「永久」档的天数(100 年,语义上等于不过期)。
// 与前端登录页 REMEMBER_PERMANENT_DAYS 保持一致。
const rememberPermanentDays = 36500

// CreateRememberToken 为账号生成一条「记住我」令牌(7/30 天或永久),返回令牌串。
// days 为 30 按 30 天处理;为 rememberPermanentDays 按「永久」处理;其余按 7 天处理。
func CreateRememberToken(userID, days int, ua string) (string, error) {
	d := 7
	switch days {
	case 30:
		d = 30
	case rememberPermanentDays:
		d = rememberPermanentDays
	}
	return dao.CreateRememberToken(userID, d, FingerprintUA(ua))
}

// ConsumeRememberToken 用「记住我」令牌换取账号信息,成功后刷新登录时间。
func ConsumeRememberToken(token, ua, ip string) (*dao.AuthInfo, error) {
	auth, err := dao.ConsumeRememberToken(token, FingerprintUA(ua))
	if err != nil {
		return nil, err
	}
	dao.RecordLogin(auth.UserID, ip)
	return auth, nil
}

// DeleteRememberToken 删除单条记住我令牌。
func DeleteRememberToken(token string) error {
	return dao.DeleteRememberToken(token)
}

// ListRememberSessions 列出某账号全部有效的「记住我」会话。
func ListRememberSessions(userID int) ([]dao.RememberSessionRow, error) {
	return dao.ListRememberSessions(userID)
}

// RevokeRememberSession 吊销指定的一条「记住我」会话。
func RevokeRememberSession(userID, tokenID int) (bool, error) {
	return dao.RevokeRememberSession(userID, tokenID)
}

// ============================================================================
// 员工管理
// ============================================================================

// usernamePattern 用户名允许的字符集(唯一来源在 store,引导建号与员工新增共用)。
var usernamePattern = store.UsernamePattern

// ListUsers 分页查询员工。
func ListUsers(keyword string, roleID int, status *int, pageNum, pageSize int) (int, []dao.UserRow, error) {
	return dao.ListUsers(dao.UserQuery{
		Keyword: strings.TrimSpace(keyword),
		RoleID:  roleID,
		Status:  status,
	}, pageNum, pageSize)
}

// CreateUser 新增员工(校验用户名/密码/角色,生成密码哈希)。
func CreateUser(username, password, realName string, roleID int, phone, remark, actor string) (int64, error) {
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return 0, errors.New("用户名只能包含字母、数字、下划线、点、@ 或连字符，长度 2~32 位")
	}
	if err := validatePassword(password); err != nil {
		return 0, err
	}
	if err := validateRole(roleID); err != nil {
		return 0, err
	}
	return dao.InsertUser(po.User{
		Username: username,
		RealName: strings.TrimSpace(realName),
		RoleID:   roleID,
		Phone:    strings.TrimSpace(phone),
		Remark:   strings.TrimSpace(remark),
	}, store.HashPassword(password), actor)
}

// UpdateUser 修改员工资料(姓名/角色/手机/备注)。
func UpdateUser(userID int, realName string, roleID int, phone, remark string, operatorID int, actor string) error {
	if err := validateRole(roleID); err != nil {
		return err
	}
	return dao.UpdateUserProfile(po.User{
		UserID:   userID,
		RealName: strings.TrimSpace(realName),
		RoleID:   roleID,
		Phone:    strings.TrimSpace(phone),
		Remark:   strings.TrimSpace(remark),
	}, operatorID, actor)
}

// ResetUserPassword 重置员工密码,并作废其全部记住我会话。
func ResetUserPassword(userID int, password, actor string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	if err := dao.SetUserPassword(userID, store.HashPassword(password), actor); err != nil {
		return err
	}
	dao.DeleteRememberTokensByUser(userID)
	return nil
}

// SetUserStatus 启用 / 停用账号,返回审计摘要与响应文案。
func SetUserStatus(userID, status, operatorID int, actor string) (detail, msg string, err error) {
	if err := dao.SetUserStatus(userID, status, operatorID, actor); err != nil {
		return "", "", err
	}
	if status == po.UserStatusEnabled {
		return "启用员工", "已启用", nil
	}
	return "停用员工", "已停用", nil
}

// DeleteUser 软删除账号。
func DeleteUser(userID, operatorID int, actor string) error {
	return dao.SoftDeleteUser(userID, operatorID, actor)
}

// validatePassword 密码长度校验(与修改密码保持一致)。
func validatePassword(pw string) error {
	if len(pw) < 6 {
		return errors.New("密码至少 6 位")
	}
	if len(pw) > 64 {
		return errors.New("密码过长")
	}
	return nil
}

// EnsureRoleAssignable 校验操作人有权把目标角色授予他人(新建员工 / 改员工角色)。
//
// 为什么必须有这道校验:员工管理(user:edit)与角色管理(role:edit)是两个独立权限点。
// 只持 user:edit 的自定义角色若能新建一个「超级管理员角色」的账号、或把已有员工改成
// 该角色,就等于绕过 role:edit 直接拿到全量权限 —— 而所有防锁死规则(不能改自己的
// 角色、不能动最后一个启用超管、内置角色不可删)都只约束「修改已有账号」,
// 对新建账号完全无效,拦不住这条提权路径。
//
// 规则:目标角色的权限集必须是操作人自身权限集的子集。超级管理员自身持全量权限,
// 因此永远满足子集关系,管理员不受任何影响;权限不足者只能授予自己不超出自身的角色。
func EnsureRoleAssignable(roleID int, operatorPerms []string) error {
	if roleID <= 0 {
		return errors.New("请选择角色")
	}
	r, err := dao.GetRoleByID(roleID)
	if err != nil || r == nil {
		return errors.New("所选角色不存在")
	}
	have := make(map[string]bool, len(operatorPerms))
	for _, p := range operatorPerms {
		have[p] = true
	}
	missing := []string{}
	for _, p := range store.ParsePerms(r.Perms) {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return errors.New("不能把权限高于自己的角色【" + r.RoleName + "】授予他人")
	}
	return nil
}

// EnsureUserPasswordResettable 校验操作人有权重置目标员工的密码。
//
// 与 EnsureRoleAssignable 同源:重置密码是比「改角色」更直接的提权路径 ——
// 只持 user:edit 的角色若能把超管的密码重置掉,就能直接用新密码登录超管账号
// 拿到全量权限,连「等对方下次登录」都不需要,完全绕过 role:edit。
// 因此这里复用同一套规则:目标员工的角色权限集不得高于操作人自身。
func EnsureUserPasswordResettable(userID int, operatorPerms []string) error {
	u, err := dao.GetUserByID(userID)
	if err != nil || u == nil {
		return errors.New("员工不存在")
	}
	return EnsureRoleAssignable(u.RoleID, operatorPerms)
}

// EnsurePermsWithinOperator 校验「待写入角色的权限集」不高于操作人自身。
//
// 为什么必须有:role:edit 的持有者能改任意非内置 admin 角色的权限。若不校验子集,
// 一个只给 role:edit 的自定义角色可以把自己所在角色的权限改成全量,下一次请求
// AuthenticateToken 读到的 perms 即全量,RequirePerm 全部放行 —— 与
// EnsureRoleAssignable 挡住的「授出高权限角色」是同一条提权路径的另一面。
// 超级管理员持全量权限,永远满足子集关系,不受影响;权限不足者只能维护自己不超出自身的角色。
func EnsurePermsWithinOperator(permList, operatorPerms []string) error {
	have := make(map[string]bool, len(operatorPerms))
	for _, p := range operatorPerms {
		have[p] = true
	}
	for _, p := range permList {
		if have[p] {
			continue
		}
		// 非法权限码交由 service.SaveRole/UpdateRole 的 UnknownPerms 统一报「无效的权限项」,
		// 这里只拦「合法但超出自身」的权限项,避免把两种错误混为一谈。
		if len(store.UnknownPerms([]string{p})) > 0 {
			continue
		}
		return errors.New("不能授予超出自身权限的权限项:" + p)
	}
	return nil
}

// validateRole 校验角色存在且启用。
func validateRole(roleID int) error {
	if roleID <= 0 {
		return errors.New("请选择角色")
	}
	r, err := dao.GetRoleByID(roleID)
	if err != nil || r == nil {
		return errors.New("所选角色不存在")
	}
	if r.Status != po.UserStatusEnabled {
		return errors.New("所选角色已停用")
	}
	return nil
}

// ============================================================================
// 角色与权限
// ============================================================================

// ListRoles 全部未删除角色(按排序号),并补算每个角色的员工数。
func ListRoles() ([]dao.RoleRow, error) {
	return dao.ListRoles()
}

// PermGroups 返回权限点目录(供 /perm/catalog 接口与前端渲染)。
func PermGroups() []store.PermGroup {
	return store.PermGroups()
}

// PermImplies 返回跨模块隐含依赖表。
func PermImplies() map[string]string {
	return store.PermImplies()
}

// AllPermCodes 返回全部权限码(按目录顺序)。
func AllPermCodes() []string {
	return store.AllPermCodes()
}

// SaveRole 新增自定义角色,返回新角色 ID 与审计摘要。
func SaveRole(roleKey, roleName string, permList []string, sortOrder int, remark, actor string) (int64, string, error) {
	roleKey = strings.TrimSpace(roleKey)
	roleName = strings.TrimSpace(roleName)
	if roleKey == "" || roleName == "" {
		return 0, "", errors.New("请填写角色标识与角色名称")
	}
	if bad := store.UnknownPerms(permList); len(bad) > 0 {
		return 0, "", errors.New("存在无效的权限项:" + strings.Join(bad, "、"))
	}
	id, err := dao.InsertRole(po.Role{
		RoleKey:   roleKey,
		RoleName:  roleName,
		SortOrder: sortOrder,
		Remark:    strings.TrimSpace(remark),
	}, permList, actor)
	if err != nil {
		return 0, "", err
	}
	return id, "新增角色 " + roleName + "，权限：" + permNames(permList), nil
}

// UpdateRole 修改角色(名称/权限/排序/备注),返回审计摘要。
func UpdateRole(roleID int, roleKey, roleName string, permList []string, sortOrder int, remark, actor string) (string, error) {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return "", errors.New("请填写角色名称")
	}
	if bad := store.UnknownPerms(permList); len(bad) > 0 {
		return "", errors.New("存在无效的权限项:" + strings.Join(bad, "、"))
	}
	oldPerms := ""
	if old, err := dao.GetRoleByID(roleID); err == nil && old != nil {
		oldPerms = old.Perms
	}
	err := dao.UpdateRole(po.Role{
		RoleID:    roleID,
		RoleKey:   strings.TrimSpace(roleKey),
		RoleName:  roleName,
		SortOrder: sortOrder,
		Remark:    strings.TrimSpace(remark),
	}, permList, actor)
	if err != nil {
		return "", err
	}
	return "修改角色 " + roleName + "：" + describePermChange(oldPerms, permList), nil
}

// DeleteRole 删除角色(内置角色与仍被员工引用的角色不可删)。
func DeleteRole(roleID int, actor string) error {
	return dao.SoftDeleteRole(roleID, actor)
}

// permNames 把权限码列表转成中文名串(摘要用;超长时截断)。
func permNames(codes []string) string {
	names := []string{}
	for _, c := range store.NormalizePerms(codes) {
		if n := store.PermName(c); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "无"
	}
	s := strings.Join(names, "、")
	if len([]rune(s)) > 200 {
		return string([]rune(s)[:200]) + "…"
	}
	return s
}

// describePermChange 描述权限差异:新增了哪些、去掉了哪些。
func describePermChange(oldCSV string, newCodes []string) string {
	oldSet, newSet := map[string]bool{}, map[string]bool{}
	for _, c := range store.ParsePerms(oldCSV) {
		oldSet[c] = true
	}
	for _, c := range store.NormalizePerms(newCodes) {
		newSet[c] = true
	}
	added, removed := []string{}, []string{}
	for _, c := range store.AllPermCodes() {
		switch {
		case newSet[c] && !oldSet[c]:
			added = append(added, store.PermName(c))
		case oldSet[c] && !newSet[c]:
			removed = append(removed, store.PermName(c))
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return "权限未变化"
	}
	parts := []string{}
	if len(added) > 0 {
		parts = append(parts, "新增权限 "+strings.Join(added, "、"))
	}
	if len(removed) > 0 {
		parts = append(parts, "移除权限 "+strings.Join(removed, "、"))
	}
	s := strings.Join(parts, "；")
	if len([]rune(s)) > 400 {
		return string([]rune(s)[:400]) + "…"
	}
	return s
}
