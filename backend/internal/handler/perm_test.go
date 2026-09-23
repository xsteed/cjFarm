package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// permSetAuth 注入一份管理员身份快照(含指定权限点)。
func permSetAuth(c *gin.Context, perms ...string) {
	setAuth(c, &dao.AuthInfo{
		UserID: 1, Username: "boss", RealName: "张老板",
		RoleID: 1, RoleKey: "admin", RoleName: "超级管理员",
		Status: po.UserStatusEnabled, Perms: perms,
	})
}

// permAuthMW 生成一个注入身份的中间件。
func permAuthMW(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		permSetAuth(c, perms...)
		c.Next()
	}
}

// permRouter 用真实 gin 路由执行一组 handler,验证中间件的放行/拒绝。
func permRouter(method, path string, handlers ...gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, handlers...)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// permOK 放行后的占位 handler。
func permOK(c *gin.Context) { okMsg(c, "ok") }

// TestPermForRoute 权限表查询:已登记路由返回权限码,未登记返回 false。
func TestPermForRoute(t *testing.T) {
	if perm, ok := PermForRoute("POST", "/api/admin/user/save"); !ok || perm != "user:edit" {
		t.Fatalf("user/save 应要求 user:edit, got %q ok=%v", perm, ok)
	}
	if perm, ok := PermForRoute("GET", "/api/admin/user/list"); !ok || perm != "user:view" {
		t.Fatalf("user/list 应要求 user:view, got %q ok=%v", perm, ok)
	}
	if perm, ok := PermForRoute("GET", "/api/admin/auth/profile"); !ok || perm != PermFree {
		t.Fatalf("auth/profile 应为登录即可, got %q ok=%v", perm, ok)
	}
	if _, ok := PermForRoute("GET", "/api/admin/not-registered"); ok {
		t.Fatal("未登记路由不应返回 ok")
	}
}

// TestRegisteredRoutePermKeysSorted 登记的权限键有序且格式合法。
func TestRegisteredRoutePermKeysSorted(t *testing.T) {
	keys := RegisteredRoutePermKeys()
	if len(keys) < 10 {
		t.Fatalf("登记的权限键应不少于 10 条, got %d", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("权限键应有序: %q >= %q", keys[i-1], keys[i])
		}
		if !strings.Contains(keys[i], " ") {
			t.Fatalf("权限键格式错误(应为 方法 路由): %q", keys[i])
		}
	}
}

// TestMenuOnlyPerms 菜单级权限声明。
func TestMenuOnlyPerms(t *testing.T) {
	got := MenuOnlyPerms()
	if len(got) != 1 || got[0] != "credit:view" {
		t.Fatalf("菜单级权限应为 [credit:view], got %v", got)
	}
	if !IsMenuOnlyPerm("credit:view") {
		t.Fatal("credit:view 应判定为菜单级权限")
	}
	if IsMenuOnlyPerm("order:view") {
		t.Fatal("order:view 不应判定为菜单级权限")
	}
}

// TestRequirePermAllowsFreeRoute 登录即可路由放行。
func TestRequirePermAllowsFreeRoute(t *testing.T) {
	w := permRouter(http.MethodPost, "/api/admin/auth/password", permAuthMW(), RequirePerm(), permOK)
	if w.Code != http.StatusOK {
		t.Fatalf("登录即可路由应放行, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePermAllowsMatchingPerm 具备对应权限点时放行。
func TestRequirePermAllowsMatchingPerm(t *testing.T) {
	w := permRouter(http.MethodPost, "/api/admin/user/save", permAuthMW("user:edit"), RequirePerm(), permOK)
	if w.Code != http.StatusOK {
		t.Fatalf("具备权限应放行, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePermForbidsMissingPerm 已登录但无权限时拒绝。
func TestRequirePermForbidsMissingPerm(t *testing.T) {
	t.Setenv("AUDIT_LOG_ENABLED", "0") // 拒绝分支会写安全审计,测试里关闭避免依赖 DB。
	w := permRouter(http.MethodPost, "/api/admin/user/save", permAuthMW("table:view"), RequirePerm(), permOK)
	if w.Code != http.StatusForbidden {
		t.Fatalf("缺少权限应返回 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePermForbidsUnregistered 未登记路由 fail-closed 拒绝。
func TestRequirePermForbidsUnregistered(t *testing.T) {
	t.Setenv("AUDIT_LOG_ENABLED", "0")
	w := permRouter(http.MethodGet, "/api/admin/not-registered", permAuthMW("user:edit"), RequirePerm(), permOK)
	if w.Code != http.StatusForbidden {
		t.Fatalf("未登记路由应返回 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestRequireAnyPerm 任一权限命中即放行。
func TestRequireAnyPerm(t *testing.T) {
	w := permRouter(http.MethodPost, "/api/admin/upload", permAuthMW("dish:edit"), RequireAnyPerm("config:edit", "dish:edit"), permOK)
	if w.Code != http.StatusOK {
		t.Fatalf("命中任一权限应放行, got %d body=%s", w.Code, w.Body.String())
	}

	t.Setenv("AUDIT_LOG_ENABLED", "0")
	w2 := permRouter(http.MethodPost, "/api/admin/upload", permAuthMW("table:view"), RequireAnyPerm("config:edit", "dish:edit"), permOK)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("无任一权限应返回 403, got %d body=%s", w2.Code, w2.Body.String())
	}
}

// TestHasPermCurrentAuthAdminName 上下文身份工具的行为。
func TestHasPermCurrentAuthAdminName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if HasPerm(c, "table:view") {
		t.Fatal("未登录时 HasPerm 应返回 false")
	}
	if currentUID(c) != 0 {
		t.Fatalf("未登录时 currentUID 应为 0, got %d", currentUID(c))
	}

	permSetAuth(c, "table:view", "dish:view")
	if !HasPerm(c, "table:view") {
		t.Fatal("具备 table:view 时 HasPerm 应返回 true")
	}
	if HasPerm(c, "user:edit") {
		t.Fatal("不具备 user:edit 时 HasPerm 应返回 false")
	}
	if currentUID(c) != 1 {
		t.Fatalf("currentUID 应为 1, got %d", currentUID(c))
	}
	if adminName(c) != "张老板" {
		t.Fatalf("adminName 应优先取中文姓名, got %q", adminName(c))
	}
}

// TestUsernameFromToken 从令牌载荷兜底解析用户名。
func TestUsernameFromToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := base64.RawURLEncoding.EncodeToString([]byte("boss|7|0|9999999999"))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+payload+".sig")
	if got := usernameFromToken(c); got != "boss" {
		t.Fatalf("应解析出用户名 boss, got %q", got)
	}

	// 载荷格式错误时返回空串。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c2.Request.Header.Set("Authorization", "Bearer not-base64.sig")
	if got := usernameFromToken(c2); got != "" {
		t.Fatalf("非法载荷应返回空串, got %q", got)
	}
}

// TestForbiddenUnauthorizedCodes 鉴权响应状态码与业务码。
func TestForbiddenUnauthorizedCodes(t *testing.T) {
	t.Setenv("AUDIT_LOG_ENABLED", "0")
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	forbidden(c, "没有操作权限")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "没有操作权限") {
		t.Fatalf("forbidden 应返回 403, got %d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	unauthorized(c2, "未登录")
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized 应返回 401, got %d body=%s", w2.Code, w2.Body.String())
	}
}
