package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// UsernamePattern 管理端用户名允许的字符集。
//
// 禁止 '|' 是因为登录令牌的载荷用它做字段分隔符(见 service.GenToken/parseToken):
// 用户名里出现 '|' 会让载荷被拆成 5 段,该账号签发的令牌永远校验失败 —— 表现为
// 「登录返回 200、之后每个接口都 401」,极难排查。
//
// 放在 store 而不是 service:启动引导建号(dao.EnsureAdminUser,读 ADMIN_USER)
// 与员工新增(service.CreateUser)都要用,而 dao 不能反向依赖 service。
var UsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,32}$`)

// ValidUsername 用户名是否符合字符集与长度要求。
func ValidUsername(name string) bool { return UsernamePattern.MatchString(name) }

// ============================================================================
// 密码哈希
//
// 放在 store 而不是 handler:员工 CRUD(建号、重置密码)与登录校验都要用,
// 属于「账号数据」的组成部分,由 store 统一持有可避免 handler 之间互相依赖。
//
// 方案:bcrypt(自适应成本 + 内置随机盐),抗暴力破解能力强于早期的
// 单次 SHA-256+盐。bcrypt 密码长度上限 72 字节,超长密码先做一次 SHA-256 摘要。
// ============================================================================

// normalizePassword 对超长密码做 SHA-256 摘要,规避 bcrypt 的 72 字节限制。
func normalizePassword(pw string) string {
	if len(pw) <= 72 {
		return pw
	}
	sum := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(sum[:])
}

// HashPassword 生成密码哈希(bcrypt)。
//
// 失败时直接 panic,不回退旧格式:旧格式是无自适应的 SHA-256+盐,一旦回退,
// 新设的密码会以弱哈希落库且**无任何日志**,事后无从发现。password 已被
// normalizePassword 摘要到 72 字节内,bcrypt 在此条件下不会失败,panic 只可能
// 出现在实现异常/成本被改坏的场景,属于必须立刻暴露的故障。
func HashPassword(pw string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(normalizePassword(pw)), bcrypt.DefaultCost)
	if err != nil {
		panic("store: bcrypt 生成哈希失败,拒绝降级为弱哈希: " + err.Error())
	}
	return string(h)
}

// VerifyPassword 校验密码:支持 bcrypt 与历史旧格式(hex(盐)$hex(SHA256(盐+密码)))。
func VerifyPassword(pw, stored string) bool {
	if strings.HasPrefix(stored, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(normalizePassword(pw))) == nil
	}
	if stored == "" {
		return false
	}
	return legacyVerifyPassword(pw, stored)
}

// IsLegacyHash 判断是否为旧格式哈希(登录成功后可自动升级为 bcrypt)。
func IsLegacyHash(stored string) bool {
	return stored != "" && !strings.HasPrefix(stored, "$2")
}

// dummyHash 用于「账号不存在」时仍执行一次 bcrypt 校验,抹平响应时间差异,
// 避免通过响应时间枚举出系统里存在哪些用户名。包加载时生成一次。
var dummyHash = HashPassword("dining-anti-enumeration-dummy")

// WastePasswordVerify 执行一次无意义的密码校验,仅用于抹平「账号不存在」分支的时间差异。
func WastePasswordVerify(pw string) {
	_ = VerifyPassword(pw, dummyHash)
}

// legacyHashPassword 旧版哈希。仅保留给兼容测试与历史数据工具使用,
// 新密码一律走 HashPassword(bcrypt),不得再调用本函数落库。
func legacyHashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		// 固定盐等于「所有账号同一个盐」,离线爆破成本骤降且无任何信号,
		// 宁可显式失败也不降级。
		panic("store: crypto/rand 不可用,拒绝生成使用固定盐的密码哈希: " + err.Error())
	}
	sum := sha256.Sum256(append(salt, []byte(pw)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:])
}

// legacyVerifyPassword 校验旧格式哈希,用于兼容历史数据。
func legacyVerifyPassword(pw, stored string) bool {
	parts := strings.SplitN(stored, "$", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(pw)...))
	expected := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) == 1
}
