package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// 密码哈希的实现已移到 store(员工 CRUD 与登录校验都要用),这里验证其行为。
func TestHashAndVerifyPassword(t *testing.T) {
	hash := store.HashPassword("s3cret-p@ss")
	if hash == "" {
		t.Fatal("HashPassword 返回空")
	}
	if !store.VerifyPassword("s3cret-p@ss", hash) {
		t.Fatal("正确密码应校验通过")
	}
	if store.VerifyPassword("wrong-password", hash) {
		t.Fatal("错误密码不应通过")
	}
	if store.HashPassword("same") == store.HashPassword("same") {
		t.Fatal("两次哈希应因随机盐而不同")
	}
	if store.VerifyPassword("anything", "") {
		t.Fatal("空哈希不应通过任何密码")
	}
	// 超过 bcrypt 72 字节上限的长密码也要能正常工作(内部先做 SHA-256 摘要)。
	long := strings.Repeat("a", 200)
	if !store.VerifyPassword(long, store.HashPassword(long)) {
		t.Fatal("超长密码应校验通过")
	}
}

// 令牌签发/校验逻辑已下沉到 service,这里直接验证 service 行为。
func TestTokenRoundTrip(t *testing.T) {
	claims, ok := service.VerifyToken(service.GenToken("admin", 7, 3))
	if !ok {
		t.Fatal("合法 token 应通过校验")
	}
	if claims.Username != "admin" || claims.UserID != 7 || claims.TokenVersion != 3 {
		t.Fatalf("载荷解析错误: %+v", claims)
	}
}

func TestTokenTampered(t *testing.T) {
	parts := strings.SplitN(service.GenToken("admin", 1, 0), ".", 2)
	if len(parts) != 2 {
		t.Fatal("token 格式应为 payload.sig")
	}
	if _, ok := service.VerifyToken(parts[0] + ".deadbeef"); ok {
		t.Fatal("签名被篡改的 token 不应通过")
	}
	// 篡改载荷(提升 uid)后签名不匹配,同样必须失败。
	forged := base64.RawURLEncoding.EncodeToString([]byte("admin|99|0|99999999999"))
	if _, ok := service.VerifyToken(forged + "." + parts[1]); ok {
		t.Fatal("篡改载荷的 token 不应通过")
	}
}

func TestTokenExpired(t *testing.T) {
	// 手工构造一个过期(exp=1, 即 1970 年)的 token。签名密钥已随业务下沉私有化到
	// service,测试无法伪造合法签名;这里同时具备「过期 + 签名非法」,VerifyToken
	// 必须拒绝 —— 过期时间校验与签名校验都不应放行。
	payload := "admin|1|0|1"
	tok := base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".deadbeef"
	if _, ok := service.VerifyToken(tok); ok {
		t.Fatal("已过期或签名非法的 token 不应通过")
	}
}

// TestTokenPayloadShape 锁定载荷格式:第一段必须是用户名。
//
// adminName() 的兜底路径(不走鉴权上下文时)直接从载荷第一段取操作人,
// 若把 uid 提到第一段,订单的操作人留痕会静默变成数字。
func TestTokenPayloadShape(t *testing.T) {
	tok := service.GenToken("zhangsan", 42, 0)
	raw, err := base64.RawURLEncoding.DecodeString(strings.SplitN(tok, ".", 2)[0])
	if err != nil {
		t.Fatalf("载荷不是合法 base64: %v", err)
	}
	fields := strings.Split(string(raw), "|")
	if len(fields) != 4 {
		t.Fatalf("载荷应为 4 段(用户名|uid|token_version|过期时间), got %d 段: %q", len(fields), raw)
	}
	if fields[0] != "zhangsan" {
		t.Fatalf("第一段必须是用户名, got %q", fields[0])
	}
	if fields[1] != "42" {
		t.Fatalf("第二段应为 uid, got %q", fields[1])
	}
}

func TestTokenTTL(t *testing.T) {
	t.Setenv("TOKEN_TTL_HOURS", "")
	if got := service.TokenTTL(); got != 24*time.Hour {
		t.Fatalf("未设置时默认应为 24h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "12")
	if got := service.TokenTTL(); got != 12*time.Hour {
		t.Fatalf("应读取 12h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "invalid")
	if got := service.TokenTTL(); got != 24*time.Hour {
		t.Fatalf("非法值应回退 24h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "99999")
	if got := service.TokenTTL(); got != 24*time.Hour {
		t.Fatalf("超上限应回退 24h, got %v", got)
	}
}

// TestLoginGuard 通过 AdminLogin handler 断言登录失败限流:
// 连续错误达到阈值后,即使密码正确也被来源 IP 维度临时锁定。
func TestLoginGuard(t *testing.T) {
	initAuthTestDB(t)
	gin.SetMode(gin.TestMode)

	// 登录限流 guard 是 service 包级内存态(跨 -count 轮次共享,且无导出重置接口),
	// 因此用户名与 IP 每次运行取唯一值,避免上一轮触发的锁定残留干扰本轮断言。
	username := fmt.Sprintf("guarduser_%d", time.Now().UnixNano())
	password := "correct-password"
	ip := fmt.Sprintf("203.0.113.%d", time.Now().UnixNano()%254+1) // TEST-NET-3,避免与真实环境冲突
	seedLoginUser(t, username, password)

	const threshold = 5
	for i := 0; i < threshold; i++ {
		w, c := newLoginContext(t, ip, map[string]interface{}{"username": username, "password": "wrong-password"})
		AdminLogin(c)
		if w.Code != http.StatusBadRequest || loginMsg(t, w) != "用户名或密码错误" {
			t.Fatalf("第 %d 次错误密码应返回「用户名或密码错误」, got %d %q", i+1, w.Code, loginMsg(t, w))
		}
	}

	// 达到阈值后锁定,即使密码正确也不放行。
	w, c := newLoginContext(t, ip, map[string]interface{}{"username": username, "password": password})
	AdminLogin(c)
	if w.Code != http.StatusBadRequest || loginMsg(t, w) != "登录失败次数过多，请稍后再试" {
		t.Fatalf("触发阈值后应锁定, got %d %q", w.Code, loginMsg(t, w))
	}
}

func initAuthTestDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "auth.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

func seedLoginUser(t *testing.T, username, password string) {
	t.Helper()
	roleID := dao.RoleIDByKey(store.RoleKeyAdmin)
	if roleID == 0 {
		t.Fatal("超级管理员角色不存在")
	}
	if _, err := dao.InsertUser(po.User{
		Username: username,
		RealName: username,
		RoleID:   roleID,
	}, store.HashPassword(password), "test"); err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
}

func newLoginContext(t *testing.T, ip string, body map[string]interface{}) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw, _ := json.Marshal(body)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	if ip != "" {
		c.Request.RemoteAddr = ip + ":1234"
	}
	return w, c
}

func loginMsg(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var rsp struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rsp); err != nil {
		t.Fatalf("登录响应非法 JSON: %v\n%s", err, w.Body.String())
	}
	return rsp.Msg
}
