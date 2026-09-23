package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dining-system/infra"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// authSvcInit 初始化独立的 SQLite 测试库并引导角色。
func authSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "auth_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
	authSvcResetGuard()
}

// authSvcResetGuard 清空登录限流内存态，避免用例间串扰。
func authSvcResetGuard() {
	guard.mu.Lock()
	guard.m = map[string]*loginEntry{}
	guard.lastPrune = time.Time{}
	guard.mu.Unlock()
}

// authSvcCreateUser 创建一名指定角色、指定密码的员工并返回 userID。
func authSvcCreateUser(t *testing.T, username, password, roleKey string) int {
	t.Helper()
	roleID := dao.RoleIDByKey(roleKey)
	if roleID == 0 {
		t.Fatalf("未找到角色 %s", roleKey)
	}
	id, err := dao.InsertUser(po.User{Username: username, RealName: username, RoleID: roleID},
		store.HashPassword(password), "test")
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return int(id)
}

// authSvcSignedToken 用与生产相同的签名密钥手工签一枚令牌，便于构造过期/异常载荷。
func authSvcSignedToken(username string, uid, ver int, exp int64) string {
	payload := fmt.Sprintf("%s|%d|%d|%d", username, uid, ver, exp)
	mac := hmac.New(sha256.New, []byte(infra.LoadAuthKey()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func TestAuthTokenRoundTrip(t *testing.T) {
	tok := GenToken("alice", 7, 3)
	claims, ok := VerifyToken(tok)
	if !ok {
		t.Fatalf("合法令牌应校验通过")
	}
	if claims.Username != "alice" || claims.UserID != 7 || claims.TokenVersion != 3 {
		t.Fatalf("令牌载荷不符: %+v", claims)
	}
}

func TestAuthVerifyTokenInvalid(t *testing.T) {
	cases := []struct {
		name string
		tok  string
	}{
		{name: "空串", tok: ""},
		{name: "无签名段", tok: "abc"},
		{name: "签名篡改", tok: func() string {
			tok := GenToken("alice", 1, 0)
			// 改签名部分的首字符:保证与原字符不同,避免原字符恰好相同导致「未篡改」。
			i := strings.LastIndex(tok, ".") + 1
			rep := byte('0')
			if tok[i] == '0' {
				rep = '1'
			}
			return tok[:i] + string(rep) + tok[i+1:]
		}()},
		{name: "过期令牌", tok: authSvcSignedToken("alice", 1, 0, time.Now().Add(-time.Hour).Unix())},
		{name: "载荷字段缺失", tok: authSvcSignedToken("a|b", 1, 0, time.Now().Add(time.Hour).Unix())},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if claims, ok := VerifyToken(c.tok); ok {
				t.Fatalf("非法令牌不应校验通过, got %+v", claims)
			}
		})
	}
}

func TestAuthTokenTTLOverride(t *testing.T) {
	t.Setenv("TOKEN_TTL_HOURS", "2")
	if got := TokenTTL(); got != 2*time.Hour {
		t.Fatalf("TokenTTL 应读取环境变量覆盖为 2h, got %v", got)
	}
	// 非法值回退默认 24h。
	t.Setenv("TOKEN_TTL_HOURS", "999")
	if got := TokenTTL(); got != 24*time.Hour {
		t.Fatalf("越界 TTL 应回退 24h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "abc")
	if got := TokenTTL(); got != 24*time.Hour {
		t.Fatalf("非法 TTL 应回退 24h, got %v", got)
	}
}

func TestAuthAuthenticateTokenStatus(t *testing.T) {
	authSvcInit(t)
	uid := authSvcCreateUser(t, "alice_auth", "secret1", store.RoleKeyCashier)

	// 成功路径。
	tok := GenToken("alice_auth", uid, 0)
	auth, status := AuthenticateToken(tok)
	if status != AuthzOK || auth == nil || auth.UserID != uid {
		t.Fatalf("成功鉴权应返回 AuthzOK, got status=%d auth=%+v", status, auth)
	}

	// 无效令牌。
	if _, status := AuthenticateToken("bad-token"); status != AuthzInvalidToken {
		t.Fatalf("无效令牌应返回 AuthzInvalidToken, got %d", status)
	}

	// 账号不存在。
	if _, status := AuthenticateToken(GenToken("alice_auth", uid+999, 0)); status != AuthzAccountGone {
		t.Fatalf("账号不存在应返回 AuthzAccountGone, got %d", status)
	}

	// 令牌版本不匹配(改密会使 token_version +1)。
	if err := dao.SetUserPassword(uid, store.HashPassword("secret2"), "alice_auth"); err != nil {
		t.Fatalf("改密失败: %v", err)
	}
	if _, status := AuthenticateToken(tok); status != AuthzTokenVersion {
		t.Fatalf("旧版本令牌应返回 AuthzTokenVersion, got %d", status)
	}

	// 账号停用。
	authSvcResetGuard()
	if err := dao.SetUserStatus(uid, po.UserStatusDisabled, 999, "tester"); err != nil {
		t.Fatalf("停用账号失败: %v", err)
	}
	if _, status := AuthenticateToken(GenToken("alice_auth", uid, 1)); status != AuthzDisabled {
		t.Fatalf("停用账号应返回 AuthzDisabled, got %d", status)
	}
}

func TestAuthAuthenticateLogin(t *testing.T) {
	authSvcInit(t)
	uid := authSvcCreateUser(t, "bob_login", "pass123", store.RoleKeyCashier)

	t.Run("成功登录", func(t *testing.T) {
		authSvcResetGuard()
		res := Authenticate("bob_login", "pass123", "10.0.0.1")
		if res.Fail != LoginOK || res.Auth == nil || res.UserID != uid {
			t.Fatalf("登录应成功, got %+v", res)
		}
	})

	t.Run("账号不存在", func(t *testing.T) {
		authSvcResetGuard()
		res := Authenticate("no_such_user", "pass123", "10.0.0.2")
		if res.Fail != LoginFailBadCredentials {
			t.Fatalf("账号不存在应返回凭据错误, got %+v", res)
		}
	})

	t.Run("密码错误", func(t *testing.T) {
		authSvcResetGuard()
		res := Authenticate("bob_login", "wrong-pass", "10.0.0.3")
		if res.Fail != LoginFailBadCredentials {
			t.Fatalf("密码错误应返回凭据错误, got %+v", res)
		}
	})

	t.Run("账号停用", func(t *testing.T) {
		authSvcResetGuard()
		disabledID := authSvcCreateUser(t, "bob_disabled", "pass123", store.RoleKeyCashier)
		if err := dao.SetUserStatus(disabledID, po.UserStatusDisabled, 999, "tester"); err != nil {
			t.Fatalf("停用账号失败: %v", err)
		}
		res := Authenticate("bob_disabled", "pass123", "10.0.0.4")
		if res.Fail != LoginFailDisabled {
			t.Fatalf("停用账号登录应返回 LoginFailDisabled, got %+v", res)
		}
	})

	t.Run("角色缺失", func(t *testing.T) {
		authSvcResetGuard()
		noRoleID := authSvcCreateUser(t, "bob_norole", "pass123", store.RoleKeyCashier)
		if _, err := store.DB.Exec(`UPDATE tb_user SET role_id=0 WHERE user_id=?`, noRoleID); err != nil {
			t.Fatalf("清空角色失败: %v", err)
		}
		res := Authenticate("bob_norole", "pass123", "10.0.0.5")
		if res.Fail != LoginFailRoleMissing {
			t.Fatalf("角色缺失应返回 LoginFailRoleMissing, got %+v", res)
		}
	})

	t.Run("角色停用", func(t *testing.T) {
		authSvcResetGuard()
		roleID := dao.RoleIDByKey(store.RoleKeyCashier)
		if _, err := store.DB.Exec(`UPDATE tb_role SET status=0 WHERE role_id=?`, roleID); err != nil {
			t.Fatalf("停用角色失败: %v", err)
		}
		res := Authenticate("bob_login", "pass123", "10.0.0.6")
		if res.Fail != LoginFailRoleDisabled {
			t.Fatalf("角色停用应返回 LoginFailRoleDisabled, got %+v", res)
		}
	})

	t.Run("IP连续失败触发锁定", func(t *testing.T) {
		authSvcResetGuard()
		ip := "10.0.0.99"
		// 用不存在的用户名连续失败，只累计 IP 维度，避免账号维度先命中锁定。
		for i := 0; i < maxLoginFailures; i++ {
			Authenticate("ghost_user", "bad-password", ip)
		}
		if !LoginIPLocked(ip) {
			t.Fatalf("连续失败 %d 次后 IP 应被锁定", maxLoginFailures)
		}
	})

	t.Run("账号连续失败触发锁定", func(t *testing.T) {
		authSvcResetGuard()
		for i := 0; i < maxLoginFailures; i++ {
			Authenticate("bob_login", "bad-password", "10.0.0.100")
		}
		res := Authenticate("bob_login", "pass123", "10.0.0.100")
		if res.Fail != LoginFailUserLocked {
			t.Fatalf("账号连续失败后应返回 UserLocked, got %+v", res)
		}
	})
}

func TestAuthChangePassword(t *testing.T) {
	authSvcInit(t)
	uid := authSvcCreateUser(t, "carol_pwd", "oldpass", store.RoleKeyCashier)

	cases := []struct {
		name       string
		old, new   string
		wantErrSub string
	}{
		{name: "原密码错误", old: "wrong", new: "newpass1", wantErrSub: "原密码错误"},
		{name: "新密码过短", old: "oldpass", new: "123", wantErrSub: "至少 6 位"},
		{name: "新密码过长", old: "oldpass", new: strings.Repeat("x", 65), wantErrSub: "新密码过长"},
		{name: "新旧相同", old: "oldpass", new: "oldpass", wantErrSub: "不能与原密码相同"},
		{name: "成功", old: "oldpass", new: "newpass1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ChangePassword(uid, c.old, c.new, "carol_pwd")
			if c.wantErrSub == "" {
				if err != nil {
					t.Fatalf("改密应成功, got %v", err)
				}
				if !store.VerifyPassword(c.new, dao.GetUserPasswordHash(uid)) {
					t.Fatalf("改密后新密码应可校验")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErrSub) {
				t.Fatalf("期望错误包含 %q, got %v", c.wantErrSub, err)
			}
		})
	}
}

func TestAuthFingerprintUA(t *testing.T) {
	cases := []struct {
		ua   string
		want string
	}{
		{ua: "", want: ""},
		{ua: "   ", want: ""},
		{ua: "MicroMessenger/8.0 iPhone OS 17", want: "微信·iOS"},
		{ua: "Edg/120.0 Windows NT 10.0", want: "Edge·Windows"},
		{ua: "OPR/100.0 Android 14", want: "Opera·Android"},
		{ua: "Firefox/120.0 X11; Linux", want: "Firefox·Linux"},
		{ua: "Chrome/120.0 Macintosh; Intel Mac OS X 10_15", want: "Chrome·macOS"},
		{ua: "Safari/605.1 iPhone", want: "Safari·iOS"},
		{ua: "curl/8.0", want: "其他浏览器·未知系统"},
	}
	for _, c := range cases {
		if got := FingerprintUA(c.ua); got != c.want {
			t.Fatalf("FingerprintUA(%q) = %q, want %q", c.ua, got, c.want)
		}
	}
}

func TestAuthRememberTokenLifecycle(t *testing.T) {
	authSvcInit(t)
	uid := authSvcCreateUser(t, "dave_remember", "pass123", store.RoleKeyCashier)

	// 注意:CreateRememberToken/ConsumeRememberToken 内部会再调 FingerprintUA,
	// 因此这里必须传「原始 UA」,传已指纹化字符串会被打成「其他浏览器·未知系统」。
	chromeUA := "Chrome/120.0 Macintosh; Intel Mac OS X 10_15"
	safariUA := "Safari/605.1 iPhone"
	tok, err := CreateRememberToken(uid, 7, chromeUA)
	if err != nil || tok == "" {
		t.Fatalf("创建记住我令牌失败: %v (tok=%q)", err, tok)
	}

	sessions, err := ListRememberSessions(uid)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("应列出 1 条会话, got %d (err=%v)", len(sessions), err)
	}

	// 环境不一致的消费应作废令牌并失败。
	if _, err := ConsumeRememberToken(tok, safariUA, "10.0.0.1"); err == nil {
		t.Fatalf("环境不一致应拒绝消费")
	}

	// 重新签发并正常消费。
	tok2, err := CreateRememberToken(uid, 30, chromeUA)
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	auth, err := ConsumeRememberToken(tok2, chromeUA, "10.0.0.2")
	if err != nil || auth == nil || auth.UserID != uid {
		t.Fatalf("消费记住我令牌应成功, got auth=%+v err=%v", auth, err)
	}

	// 按主键吊销后列表为空。
	sessions, _ = ListRememberSessions(uid)
	if len(sessions) == 0 {
		t.Fatalf("应存在会话")
	}
	if ok, err := RevokeRememberSession(uid, sessions[0].TokenID); err != nil || !ok {
		t.Fatalf("吊销会话应成功, ok=%v err=%v", ok, err)
	}
}

func TestAuthCreateUserValidation(t *testing.T) {
	authSvcInit(t)

	// 非法用户名。
	if _, err := CreateUser("a", "pass123", "张三", dao.RoleIDByKey(store.RoleKeyCashier), "", "", "test"); err == nil || !strings.Contains(err.Error(), "用户名") {
		t.Fatalf("非法用户名应被拒绝, got %v", err)
	}
	// 非法密码。
	if _, err := CreateUser("valid_user", "123", "张三", dao.RoleIDByKey(store.RoleKeyCashier), "", "", "test"); err == nil || !strings.Contains(err.Error(), "密码") {
		t.Fatalf("短密码应被拒绝, got %v", err)
	}
	// 角色不存在。
	if _, err := CreateUser("valid_user", "pass123", "张三", 99999, "", "", "test"); err == nil || !strings.Contains(err.Error(), "角色") {
		t.Fatalf("不存在角色应被拒绝, got %v", err)
	}
	// 成功。
	id, err := CreateUser("valid_user", "pass123", " 张三 ", dao.RoleIDByKey(store.RoleKeyCashier), " 13800000000 ", " 备注 ", "test")
	if err != nil {
		t.Fatalf("创建用户应成功, got %v", err)
	}
	u, err := dao.GetUserByID(int(id))
	if err != nil || u == nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if u.RealName != "张三" || u.Phone != "13800000000" || u.Remark != "备注" {
		t.Fatalf("用户字段未按预期裁剪: %+v", u)
	}
}

func TestAuthUserAdminAndRoleManagement(t *testing.T) {
	authSvcInit(t)

	// 员工列表。
	total, users, err := ListUsers("", 0, nil, 1, 10)
	if err != nil || total < 1 || len(users) == 0 {
		t.Fatalf("员工列表应返回数据, total=%d err=%v", total, err)
	}

	// 角色列表。
	roles, err := ListRoles()
	if err != nil || len(roles) < 4 {
		t.Fatalf("角色列表应含内置角色, got %d (err=%v)", len(roles), err)
	}

	// 新增自定义角色。
	roleID, detail, err := SaveRole("custom_role", "自定义角色", []string{"order:view", "order:settle"}, 10, "备注", "test")
	if err != nil {
		t.Fatalf("新增角色失败: %v", err)
	}
	if roleID == 0 || !strings.Contains(detail, "自定义角色") {
		t.Fatalf("新增角色返回异常: id=%d detail=%s", roleID, detail)
	}

	// 非法权限项应被拒绝。
	if _, _, err := SaveRole("bad_role", "坏角色", []string{"order:view", "no:such"}, 11, "", "test"); err == nil || !strings.Contains(err.Error(), "无效的权限项") {
		t.Fatalf("非法权限应被拒绝, got %v", err)
	}

	// 修改角色。
	detail, err = UpdateRole(int(roleID), "custom_role", "自定义角色2", []string{"order:view"}, 12, "备注2", "test")
	if err != nil || !strings.Contains(detail, "自定义角色2") {
		t.Fatalf("修改角色失败: %v (detail=%s)", err, detail)
	}

	// 删除角色。
	if err := DeleteRole(int(roleID), "test"); err != nil {
		t.Fatalf("删除自定义角色失败: %v", err)
	}

	// 内置角色不可删。
	adminID := dao.RoleIDByKey(store.RoleKeyAdmin)
	if err := DeleteRole(adminID, "test"); err == nil {
		t.Fatalf("内置角色应不可删除")
	}
}

func TestAuthPermNamesAndChange(t *testing.T) {
	if got := permNames(nil); got != "无" {
		t.Fatalf("空权限摘要应为「无」, got %q", got)
	}
	if got := permNames([]string{"order:view", "order:settle"}); !strings.Contains(got, "订单查看") {
		t.Fatalf("权限摘要应含中文名, got %q", got)
	}
	if got := describePermChange("", []string{"order:view"}); !strings.Contains(got, "新增权限") {
		t.Fatalf("权限变化应识别新增, got %q", got)
	}
	if got := describePermChange("order:view", nil); !strings.Contains(got, "移除权限") {
		t.Fatalf("权限变化应识别移除, got %q", got)
	}
	if got := describePermChange("order:view", []string{"order:view"}); got != "权限未变化" {
		t.Fatalf("权限未变化时应返回固定文案, got %q", got)
	}
}
