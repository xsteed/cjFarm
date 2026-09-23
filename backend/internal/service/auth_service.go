// 认证与用户域用例:令牌签发/校验、登录限流、账号密码校验、
// 记住我会话、员工与角色管理。
package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dining-system/infra"
	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
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
	if v, err := strconv.Atoi(infra.Getenv(conf.EnvTokenTTLHours, conf.DefaultTokenTTLHours)); err == nil && v > 0 && v <= 24*30 {
		return time.Duration(v) * time.Hour
	}
	return 24 * time.Hour
}

func init() {
	// 包加载期先做一次只读解析,保证测试与首个请求前密钥就绪(此阶段不落盘);
	// 生产启动在 main 装配完部署配置后还会调用 infra.InitAuthKey() 重新解析并落盘,
	// 因此 TOKEN_SECRET / data/auth.key 会被正确识别为最终签名密钥。
	_ = infra.LoadAuthKey()
}

// GenToken 签发访问令牌:用户名|uid|token_version|过期时间,HMAC-SHA256 签名。
func GenToken(username string, userID, tokenVersion int) string {
	exp := time.Now().Add(TokenTTL()).Unix()
	payload := fmt.Sprintf("%s|%d|%d|%d", username, userID, tokenVersion, exp)
	mac := hmac.New(sha256.New, []byte(infra.LoadAuthKey()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// VerifyToken 校验签名与有效期,返回载荷。用户名不允许含 '|'(建号时已校验)。
func VerifyToken(tok string) (*TokenClaims, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(infra.LoadAuthKey()))
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
	return &TokenClaims{Username: fields[0], UserID: uid, TokenVersion: ver}, true
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
func AuthenticateToken(tok string) (*dao.AuthInfo, AuthzStatus) {
	claims, valid := VerifyToken(tok)
	if !valid {
		return nil, AuthzInvalidToken
	}
	auth, err := dao.GetAuthByID(claims.UserID)
	if err != nil {
		return nil, AuthzAccountGone
	}
	if auth.Status != po.UserStatusEnabled {
		return nil, AuthzDisabled
	}
	if auth.TokenVersion != claims.TokenVersion {
		return nil, AuthzTokenVersion
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		return nil, AuthzRoleMissing
	}
	if auth.RoleStatus != po.UserStatusEnabled {
		return nil, AuthzRoleDisabled
	}
	return auth, AuthzOK
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
func (g *loginGuard) pruneLocked() {
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
func Authenticate(username, password, ip string) LoginResult {
	ipKey := "ip:" + ip

	// 账号维度锁定:只对真实存在的账号计数,避免攻击者刷不存在的用户名把内存刷爆。
	var userKey string
	if taken, _ := dao.UsernameExists(username, 0); taken {
		userKey = "user:" + username
		if guard.locked(userKey) {
			return LoginResult{Fail: LoginFailUserLocked, Username: username}
		}
	}

	failBoth := func() {
		guard.fail(ipKey)
		if userKey != "" {
			guard.fail(userKey)
		}
	}

	auth, err := dao.GetAuthByUsername(username)
	if err != nil {
		// 账号不存在:仍执行一次 bcrypt 校验以抹平响应时间差异,避免枚举用户名。
		store.WastePasswordVerify(password)
		guard.fail(ipKey)
		return LoginResult{Fail: LoginFailBadCredentials, Username: username}
	}
	if !store.VerifyPassword(password, dao.GetUserPasswordHash(auth.UserID)) {
		failBoth()
		return LoginResult{Fail: LoginFailBadCredentials, UserID: auth.UserID, Username: username}
	}
	if auth.Status != po.UserStatusEnabled {
		failBoth()
		return LoginResult{Fail: LoginFailDisabled, UserID: auth.UserID, Username: username}
	}
	if auth.RoleID == 0 || auth.RoleKey == "" {
		failBoth()
		return LoginResult{Fail: LoginFailRoleMissing, UserID: auth.UserID, Username: username}
	}
	if auth.RoleStatus != po.UserStatusEnabled {
		failBoth()
		return LoginResult{Fail: LoginFailRoleDisabled, UserID: auth.UserID, Username: username}
	}

	guard.clear(ipKey)
	if userKey != "" {
		guard.clear(userKey)
	}
	dao.RecordLogin(auth.UserID, ip)
	// 历史旧格式哈希(单次 SHA-256)在校验通过后自动升级为 bcrypt。
	hash := dao.GetUserPasswordHash(auth.UserID)
	if store.IsLegacyHash(hash) {
		dao.SetUserPassword(auth.UserID, store.HashPassword(password), auth.Username)
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

// CreateRememberToken 为账号生成一条「记住我」令牌(7/30 天),返回令牌串。
// days 仅 30 会按 30 天处理,其余按 7 天处理。
func CreateRememberToken(userID, days int, ua string) (string, error) {
	d := 7
	if days == 30 {
		d = 30
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

// usernamePattern 用户名允许的字符集。禁止 '|' 是因为令牌载荷用它做字段分隔符。
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,32}$`)

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
