package handler

// ============================================================================
// 鉴权 / 登录态补充单元测试(auth.go)
//
// auth_test.go 已覆盖登录限流与令牌签名行为;本文件补齐 AdminAuth 各鉴权分支、
// 「记住我」登录 / 设备管理、退出登录、个人信息与修改密码等 HTTP 层分支。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// authMoreUserID 查询指定登录名的员工 ID。
func authMoreUserID(t *testing.T, username string) int {
	t.Helper()
	var id int
	if err := store.DB.QueryRow(`SELECT user_id FROM tb_user WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatalf("查询用户 %s 失败: %v", username, err)
	}
	return id
}

// authMoreSetAuth 直接注入当前登录者身份。
func authMoreSetAuth(c *gin.Context, uid int, username string) {
	setAuth(c, &service.AuthInfo{
		UserID:   uid,
		Username: username,
		RealName: "测试用户",
		Status:   po.UserStatusEnabled,
		RoleID:   1,
		RoleKey:  store.RoleKeyAdmin,
		RoleName: "超级管理员",
		Perms:    []string{},
	})
}

// authMorePost 以 JSON body 调用 POST handler,可携带 User-Agent,返回状态码与响应体。
func authMorePost(t *testing.T, h gin.HandlerFunc, body, ua string) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	req.RemoteAddr = "10.0.0.8:12345"
	c.Request = req
	h(c)
	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// authMoreAdminRequest 通过带 AdminAuth 中间件的最小路由发起请求。
func authMoreAdminRequest(t *testing.T, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", AdminAuth, func(c *gin.Context) { okMsg(c, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminAuthBranches(t *testing.T) {
	initAuthTestDB(t)

	// 无令牌 / 非法令牌 → 401。
	if w := authMoreAdminRequest(t, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("无令牌应返回 401, got %d", w.Code)
	}
	if w := authMoreAdminRequest(t, "bad-token"); w.Code != http.StatusUnauthorized {
		t.Fatalf("非法令牌应返回 401, got %d", w.Code)
	}
	// 合法签名但账号不存在 → 401。
	if w := authMoreAdminRequest(t, service.GenToken("ghost", 999999, 0)); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "登录状态已失效") {
		t.Fatalf("账号不存在应返回 401 登录状态已失效, got %d(%s)", w.Code, w.Body.String())
	}

	// 正常账号 + 正常令牌 → 放行。
	seedLoginUser(t, "authok", "pw123456")
	okUID := authMoreUserID(t, "authok")
	if w := authMoreAdminRequest(t, service.GenToken("authok", okUID, 0)); w.Code != http.StatusOK {
		t.Fatalf("正常账号应放行, got %d(%s)", w.Code, w.Body.String())
	}

	// 账号停用 → 401。
	seedLoginUser(t, "authdisabled", "pw123456")
	disabledUID := authMoreUserID(t, "authdisabled")
	if _, err := store.DB.Exec(`UPDATE tb_user SET status=0 WHERE user_id=?`, disabledUID); err != nil {
		t.Fatalf("停用账号失败: %v", err)
	}
	if w := authMoreAdminRequest(t, service.GenToken("authdisabled", disabledUID, 0)); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "已被停用") {
		t.Fatalf("停用账号应返回 401, got %d(%s)", w.Code, w.Body.String())
	}

	// 令牌版本不一致 → 401。
	seedLoginUser(t, "authversion", "pw123456")
	versionUID := authMoreUserID(t, "authversion")
	if _, err := store.DB.Exec(`UPDATE tb_user SET token_version=7 WHERE user_id=?`, versionUID); err != nil {
		t.Fatalf("更新令牌版本失败: %v", err)
	}
	if w := authMoreAdminRequest(t, service.GenToken("authversion", versionUID, 0)); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "登录状态已失效") {
		t.Fatalf("令牌版本不一致应返回 401, got %d(%s)", w.Code, w.Body.String())
	}

	// 角色缺失 → 403。
	seedLoginUser(t, "authrolemissing", "pw123456")
	roleMissingUID := authMoreUserID(t, "authrolemissing")
	if _, err := store.DB.Exec(`UPDATE tb_user SET role_id=0 WHERE user_id=?`, roleMissingUID); err != nil {
		t.Fatalf("清空角色失败: %v", err)
	}
	if w := authMoreAdminRequest(t, service.GenToken("authrolemissing", roleMissingUID, 0)); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "账号角色已失效") {
		t.Fatalf("角色缺失应返回 403, got %d(%s)", w.Code, w.Body.String())
	}

	// 角色停用 → 403。
	res, err := store.DB.Exec(`INSERT INTO tb_role(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES('disabled_role','停用角色','','all',0,99,0,'0',?,?,?)`, store.Now(), store.Now(), "")
	if err != nil {
		t.Fatalf("插入停用角色失败: %v", err)
	}
	disabledRoleID, _ := res.LastInsertId()
	if _, err := dao.InsertUser(po.User{Username: "authroledisabled", RealName: "停用角色用户", RoleID: int(disabledRoleID)}, store.HashPassword("pw123456"), "test"); err != nil {
		t.Fatalf("插入停用角色用户失败: %v", err)
	}
	roleDisabledUID := authMoreUserID(t, "authroledisabled")
	if w := authMoreAdminRequest(t, service.GenToken("authroledisabled", roleDisabledUID, 0)); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "角色已停用") {
		t.Fatalf("角色停用应返回 403, got %d(%s)", w.Code, w.Body.String())
	}
}

func TestRememberLoginBranches(t *testing.T) {
	initAuthTestDB(t)
	seedLoginUser(t, "rememberuser", "pw123456")
	uid := authMoreUserID(t, "rememberuser")
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0 Safari/537.36"
	tok, err := service.CreateRememberToken(uid, 7, ua)
	if err != nil {
		t.Fatalf("创建记住我令牌失败: %v", err)
	}

	if code, res := authMorePost(t, RememberLogin, `{}`, ua); code != http.StatusUnauthorized || res["code"] != float64(401) {
		t.Fatalf("缺令牌应返回 401, got %d(%v)", code, res)
	}
	if code, res := authMorePost(t, RememberLogin, `{"rememberToken":"bad"}`, ua); code != http.StatusUnauthorized {
		t.Fatalf("非法令牌应返回 401, got %d(%v)", code, res)
	}
	if code, res := authMorePost(t, RememberLogin, `{"rememberToken":"`+tok+`"}`, ua); code != http.StatusOK || res["data"].(map[string]interface{})["username"] != "rememberuser" {
		t.Fatalf("合法令牌应换发登录态, got %d(%v)", code, res)
	}
}

func TestLogoutBranches(t *testing.T) {
	initAuthTestDB(t)
	if code, res := authMorePost(t, Logout, `{}`, ""); code != http.StatusBadRequest || res["msg"] != "参数错误" {
		t.Fatalf("缺令牌应返回参数错误, got %d(%v)", code, res)
	}
	if code, res := authMorePost(t, Logout, `{"rememberToken":"whatever"}`, ""); code != http.StatusOK || res["msg"] != "已退出登录" {
		t.Fatalf("退出登录应成功, got %d(%v)", code, res)
	}
}

func TestRememberSessionsAndRevoke(t *testing.T) {
	initAuthTestDB(t)
	seedLoginUser(t, "sessionuser", "pw123456")
	uid := authMoreUserID(t, "sessionuser")
	tok, err := service.CreateRememberToken(uid, 7, "Chrome·macOS")
	if err != nil {
		t.Fatalf("创建记住我令牌失败: %v", err)
	}
	// 令牌已改为哈希存储,不能再按明文 token 反查;改为按设备识别前缀定位。
	var tokenID int
	if err := store.DB.QueryRow(`SELECT token_id FROM tb_remember_token WHERE user_id=? AND token_prefix=?`, uid, tok[:8]).Scan(&tokenID); err != nil {
		t.Fatalf("查询令牌失败: %v", err)
	}

	// 未登录 → 401。
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	RememberSessions(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", w.Code)
	}

	// 已登录 → 200 且至少 1 个会话。
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	authMoreSetAuth(c2, uid, "sessionuser")
	RememberSessions(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("会话列表应返回 200, got %d", w2.Code)
	}

	// 吊销:缺参 / 非法 / 不存在 / 成功。
	if _, res := authMorePost(t, RememberRevoke, `{}`, ""); res["code"] != float64(401) {
		t.Fatalf("未登录吊销应返回 401, got %v", res)
	}
	body := `{"tokenId":0}`
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c3.Request.Header.Set("Content-Type", "application/json")
	authMoreSetAuth(c3, uid, "sessionuser")
	RememberRevoke(c3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("缺 tokenId 应返回 400, got %d", w3.Code)
	}

	if code, res := authMorePostWithAuth(t, RememberRevoke, uid, `{"tokenId":99999}`); code != http.StatusBadRequest || res["msg"] != "登录设备不存在或已失效" {
		t.Fatalf("吊销不存在的设备应返回不存在, got %d(%v)", code, res)
	}
	if code, res := authMorePostWithAuth(t, RememberRevoke, uid, `{"tokenId":`+itoa(tokenID)+`}`); code != http.StatusOK || res["msg"] != "已吊销该设备" {
		t.Fatalf("吊销设备应成功, got %d(%v)", code, res)
	}
}

// authMorePostWithAuth 注入身份后调用 POST handler。
func authMorePostWithAuth(t *testing.T, h gin.HandlerFunc, uid int, body string) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	authMoreSetAuth(c, uid, "sessionuser")
	h(c)
	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestProfileBranches(t *testing.T) {
	initAuthTestDB(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	Profile(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", w.Code)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	authMoreSetAuth(c2, 7, "profileuser")
	Profile(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("个人信息应返回 200, got %d", w2.Code)
	}
}

func TestChangePasswordBranches(t *testing.T) {
	initAuthTestDB(t)
	seedLoginUser(t, "changepw", "oldpass1")
	uid := authMoreUserID(t, "changepw")

	if code, res := authMorePost(t, ChangePassword, `{"oldPassword":`, ""); code != http.StatusBadRequest || res["msg"] != "参数错误" {
		t.Fatalf("非法 JSON 应返回参数错误, got %d(%v)", code, res)
	}
	if code, res := authMorePost(t, ChangePassword, `{"oldPassword":"oldpass1","newPassword":"newpass2"}`, ""); code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d(%v)", code, res)
	}
	if code, res := authMorePostWithAuth(t, ChangePassword, uid, `{"oldPassword":"wrong","newPassword":"newpass2"}`); code != http.StatusBadRequest || res["msg"] != "原密码错误" {
		t.Fatalf("原密码错误应被拒绝, got %d(%v)", code, res)
	}
	if code, res := authMorePostWithAuth(t, ChangePassword, uid, `{"oldPassword":"oldpass1","newPassword":"123"}`); code != http.StatusBadRequest || res["msg"] != "新密码至少 6 位" {
		t.Fatalf("新密码过短应被拒绝, got %d(%v)", code, res)
	}
	if code, res := authMorePostWithAuth(t, ChangePassword, uid, `{"oldPassword":"oldpass1","newPassword":"newpass2"}`); code != http.StatusOK || res["msg"] != "密码修改成功，请重新登录" {
		t.Fatalf("修改密码应成功, got %d(%v)", code, res)
	}
}
