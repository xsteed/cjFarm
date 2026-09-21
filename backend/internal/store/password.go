package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

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
func HashPassword(pw string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(normalizePassword(pw)), bcrypt.DefaultCost)
	if err != nil {
		// 极端失败时回退为旧格式,保证可用性。
		return legacyHashPassword(pw)
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

// legacyHashPassword 旧版哈希(仅用于 bcrypt 失败时的兜底,不推荐)。
func legacyHashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		copy(salt, []byte("dining-fixed-salt"))
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
