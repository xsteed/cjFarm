package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// authRegLegacyHash 构造旧格式口令哈希(hex(盐)$hex(SHA256(盐+密码))),
// 与 store 的历史实现一致,用于模拟「存量老账号」。
func authRegLegacyHash(salt, password string) string {
	sum := sha256.Sum256(append([]byte(salt), []byte(password)...))
	return hex.EncodeToString([]byte(salt)) + "$" + hex.EncodeToString(sum[:])
}

// TestAuthenticateLegacyHashUpgradeTokenMatches 旧哈希自动升级后签发的令牌必须立即可用。
//
// SetUserPassword 会把 token_version +1(改密即作废该账号的全部旧令牌)。如果登录时
// 拿的是「升级前」的账号快照去签发令牌,令牌版本就会比库里少 1 —— 登录接口返回 200,
// 但随后每一个管理端接口都 401,该账号必须再登录一次才能正常使用。
func TestAuthenticateLegacyHashUpgradeTokenMatches(t *testing.T) {
	authSvcInit(t)
	const username, password = "legacy_user", "secret123"
	uid := authSvcCreateUser(t, username, password, store.RoleKeyCashier)
	// 把口令换成旧格式,模拟历史数据。
	if _, err := store.DB.Exec(`UPDATE tb_user SET password_hash=? WHERE user_id=?`,
		authRegLegacyHash("0123456789abcdef", password), uid); err != nil {
		t.Fatalf("写入旧哈希失败: %v", err)
	}

	res := Authenticate(context.Background(), username, password, "10.0.0.1")
	if res.Auth == nil {
		t.Fatalf("旧哈希账号登录应成功, got fail=%d", res.Fail)
	}
	if !strings.HasPrefix(dao.GetUserPasswordHash(uid), "$2") {
		t.Fatalf("登录成功后旧哈希应自动升级为 bcrypt")
	}
	// 关键断言:用登录返回的版本号签发令牌,必须能直接通过鉴权。
	tok := GenToken(res.Auth.Username, res.Auth.UserID, res.Auth.TokenVersion)
	if _, status := AuthenticateToken(context.Background(), tok); status != AuthzOK {
		t.Fatalf("升级后签发的令牌应立即有效, got status=%d", status)
	}
}

// TestEnsureRoleAssignable 员工管理入口的提权防护:
// 只持 user:edit 的角色不能把权限高于自身的角色(如超级管理员)授予他人,
// 否则等于绕过 role:edit 拿到全量权限。
func TestEnsureRoleAssignable(t *testing.T) {
	authSvcInit(t)

	adminRoleID := dao.RoleIDByKey(store.RoleKeyAdmin)
	if adminRoleID == 0 {
		t.Fatal("未找到超级管理员角色")
	}
	if err := EnsureRoleAssignable(adminRoleID, AllPermCodes()); err != nil {
		t.Fatalf("超管授予超管角色应放行, got %v", err)
	}
	if err := EnsureRoleAssignable(adminRoleID, []string{"user:view", "user:edit"}); err == nil {
		t.Fatal("权限不足者授予超级管理员角色应被拒绝")
	}
	if err := EnsureRoleAssignable(dao.RoleIDByKey(store.RoleKeyCashier), AllPermCodes()); err != nil {
		t.Fatalf("超管授予收银员角色应放行, got %v", err)
	}
	if err := EnsureRoleAssignable(0, AllPermCodes()); err == nil || !strings.Contains(err.Error(), "请选择角色") {
		t.Fatalf("未选择角色应提示「请选择角色」, got %v", err)
	}
}

// TestLoginGuardHardCap 登录限流的条目表必须有硬上限。
//
// 登录接口无鉴权:攻击者轮换源 IP 持续打时,每个新条目的 lastFail 都是新鲜的,
// 只靠 idle TTL 回收一条也删不掉 —— map 会单调增长直到内存耗尽。
func TestLoginGuardHardCap(t *testing.T) {
	authSvcInit(t)
	for i := 0; i < loginGuardMaxKeys*2; i++ {
		guard.fail(fmt.Sprintf("ip:10.%d.%d.%d", i/65536%256, i/256%256, i%256))
	}
	guard.mu.Lock()
	n := len(guard.m)
	guard.mu.Unlock()
	if n > loginGuardMaxKeys {
		t.Fatalf("限流条目应被硬上限收敛到 %d 以内, got %d", loginGuardMaxKeys, n)
	}
}
