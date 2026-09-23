package dao

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// rememberDaoInitDB 初始化「记住我」测试用的临时库,并引导管理员账号。
func rememberDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "remember.db"))
	BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// rememberDaoAdminID 返回引导管理员的用户 ID。
func rememberDaoAdminID(t *testing.T) int {
	t.Helper()
	u, err := GetUserByUsername("admin")
	if err != nil || u == nil {
		t.Fatalf("查询引导管理员失败: %v", err)
	}
	return u.UserID
}

func TestRememberCreateAndConsume(t *testing.T) {
	rememberDaoInitDB(t)
	uid := rememberDaoAdminID(t)

	token, err := CreateRememberToken(uid, 7, "Chrome·macOS")
	if err != nil {
		t.Fatalf("创建记住令牌失败: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("令牌应为 64 位 hex, got %q", token)
	}

	auth, err := ConsumeRememberToken(token, "Chrome·macOS")
	if err != nil {
		t.Fatalf("消费令牌失败: %v", err)
	}
	if auth.UserID != uid {
		t.Fatalf("令牌应换回账号 %d, got %d", uid, auth.UserID)
	}

	// 再次消费同一令牌仍成功(不是一次性令牌)。
	if _, err := ConsumeRememberToken(token, "Chrome·macOS"); err != nil {
		t.Fatalf("重复消费令牌失败: %v", err)
	}
}

func TestRememberConsumeErrors(t *testing.T) {
	rememberDaoInitDB(t)
	uid := rememberDaoAdminID(t)

	// 无效令牌。
	if _, err := ConsumeRememberToken("no-such-token", "Chrome·macOS"); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("无效令牌应报「无效」, got %v", err)
	}

	// 过期令牌。
	expired, err := CreateRememberToken(uid, -1, "Chrome·macOS")
	if err != nil {
		t.Fatalf("创建过期令牌失败: %v", err)
	}
	if _, err := ConsumeRememberToken(expired, "Chrome·macOS"); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Fatalf("过期令牌应报「过期」, got %v", err)
	}

	// 环境不一致。
	token, err := CreateRememberToken(uid, 7, "Chrome·macOS")
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	if _, err := ConsumeRememberToken(token, "Safari·iOS"); err == nil || !strings.Contains(err.Error(), "环境") {
		t.Fatalf("环境不一致应报「环境」, got %v", err)
	}

	// 账号停用。
	token, err = CreateRememberToken(uid, 7, "Chrome·macOS")
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_user SET status=? WHERE user_id=?`, po.UserStatusDisabled, uid); err != nil {
		t.Fatalf("停用账号失败: %v", err)
	}
	if _, err := ConsumeRememberToken(token, "Chrome·macOS"); err == nil || !strings.Contains(err.Error(), "账号状态") {
		t.Fatalf("停用账号应报「账号状态」, got %v", err)
	}
}

func TestRememberConsumeLegacyUaBackfill(t *testing.T) {
	rememberDaoInitDB(t)
	uid := rememberDaoAdminID(t)

	token := "legacy-token-0000000000000000000000000000000000000000000000"
	future := shiftSeconds(store.Now(), 86400)
	if _, err := store.DB.Exec(`INSERT INTO tb_remember_token(user_id, token, expire_time, create_time, last_used_time, ua)
		VALUES(?,?,?,?,NULL,'')`, uid, token, future, store.Now()); err != nil {
		t.Fatalf("直插存量令牌失败: %v", err)
	}

	auth, err := ConsumeRememberToken(token, "Firefox·Win")
	if err != nil {
		t.Fatalf("消费存量令牌失败: %v", err)
	}
	if auth.UserID != uid {
		t.Fatalf("应换回账号 %d, got %d", uid, auth.UserID)
	}

	// 首次使用应补写环境指纹。
	sessions, err := ListRememberSessions(uid)
	if err != nil {
		t.Fatalf("查询会话失败: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.TokenPrefix == token[:8] {
			found = true
			if s.UA != "Firefox·Win" {
				t.Fatalf("存量会话应补写指纹, got %q", s.UA)
			}
		}
	}
	if !found {
		t.Fatalf("未找到补写后的会话: %+v", sessions)
	}
}

func TestListRememberSessions(t *testing.T) {
	rememberDaoInitDB(t)
	uid := rememberDaoAdminID(t)

	if _, err := CreateRememberToken(uid, 7, "Chrome·macOS"); err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	if _, err := CreateRememberToken(uid, 30, "Safari·iOS"); err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	if _, err := CreateRememberToken(uid, -1, "Firefox·Win"); err != nil {
		t.Fatalf("创建过期令牌失败: %v", err)
	}

	sessions, err := ListRememberSessions(uid)
	if err != nil {
		t.Fatalf("查询会话失败: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("应只返回 2 条未过期会话, got %d", len(sessions))
	}
	// 新的在前(token_id 降序)。
	if sessions[0].TokenID < sessions[1].TokenID {
		t.Fatalf("会话应按 token_id 降序, got %+v", sessions)
	}
	for _, s := range sessions {
		if len(s.TokenPrefix) != 8 {
			t.Fatalf("TokenPrefix 应为 8 位, got %q", s.TokenPrefix)
		}
	}
}

func TestRevokeAndDeleteRemember(t *testing.T) {
	rememberDaoInitDB(t)
	uid := rememberDaoAdminID(t)

	if _, err := CreateRememberToken(uid, 7, "Chrome·macOS"); err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	sessions, _ := ListRememberSessions(uid)
	if len(sessions) != 1 {
		t.Fatalf("应有一条会话, got %d", len(sessions))
	}
	tokenID := sessions[0].TokenID

	// 非属主吊销不生效且不报错。
	ok, err := RevokeRememberSession(uid+1, tokenID)
	if err != nil {
		t.Fatalf("吊销他人会话不应报错: %v", err)
	}
	if ok {
		t.Fatal("非属主吊销不应成功")
	}

	// 属主吊销成功。
	ok, err = RevokeRememberSession(uid, tokenID)
	if err != nil {
		t.Fatalf("吊销会话失败: %v", err)
	}
	if !ok {
		t.Fatal("属主吊销应成功")
	}

	// DeleteRememberToken 与批量删除。
	token2, _ := CreateRememberToken(uid, 7, "Chrome·macOS")
	if err := DeleteRememberToken(token2); err != nil {
		t.Fatalf("删除令牌失败: %v", err)
	}
	_, _ = CreateRememberToken(uid, 7, "Chrome·macOS")
	_, _ = CreateRememberToken(uid, 7, "Safari·iOS")
	if err := DeleteRememberTokensByUser(uid); err != nil {
		t.Fatalf("批量删除令牌失败: %v", err)
	}
	sessions, _ = ListRememberSessions(uid)
	if len(sessions) != 0 {
		t.Fatalf("批量删除后应无会话, got %d", len(sessions))
	}
}
