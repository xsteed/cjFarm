package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"dining-system/internal/store"
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

func TestTokenRoundTrip(t *testing.T) {
	claims, ok := verifyToken(genToken("admin", 7, 3))
	if !ok {
		t.Fatal("合法 token 应通过校验")
	}
	if claims.Username != "admin" || claims.UserID != 7 || claims.TokenVersion != 3 {
		t.Fatalf("载荷解析错误: %+v", claims)
	}
}

func TestTokenTampered(t *testing.T) {
	parts := strings.SplitN(genToken("admin", 1, 0), ".", 2)
	if len(parts) != 2 {
		t.Fatal("token 格式应为 payload.sig")
	}
	if _, ok := verifyToken(parts[0] + ".deadbeef"); ok {
		t.Fatal("签名被篡改的 token 不应通过")
	}
	// 篡改载荷(提升 uid)后签名不匹配,同样必须失败。
	forged := base64.RawURLEncoding.EncodeToString([]byte("admin|99|0|99999999999"))
	if _, ok := verifyToken(forged + "." + parts[1]); ok {
		t.Fatal("篡改载荷的 token 不应通过")
	}
}

func TestTokenExpired(t *testing.T) {
	// 手工构造一个过期(exp=1, 即 1970 年)但签名合法的 token。
	payload := "admin|1|0|1"
	mac := hmac.New(sha256.New, []byte(authSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	tok := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
	if _, ok := verifyToken(tok); ok {
		t.Fatal("已过期 token 不应通过")
	}
}

// TestTokenPayloadShape 锁定载荷格式:第一段必须是用户名。
//
// adminName() 的兜底路径(不走鉴权上下文时)直接从载荷第一段取操作人,
// 若把 uid 提到第一段,订单的操作人留痕会静默变成数字。
func TestTokenPayloadShape(t *testing.T) {
	tok := genToken("zhangsan", 42, 0)
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
	if got := tokenTTL(); got != 24*time.Hour {
		t.Fatalf("未设置时默认应为 24h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "12")
	if got := tokenTTL(); got != 12*time.Hour {
		t.Fatalf("应读取 12h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "invalid")
	if got := tokenTTL(); got != 24*time.Hour {
		t.Fatalf("非法值应回退 24h, got %v", got)
	}
	t.Setenv("TOKEN_TTL_HOURS", "99999")
	if got := tokenTTL(); got != 24*time.Hour {
		t.Fatalf("超上限应回退 24h, got %v", got)
	}
}

func TestLoginGuard(t *testing.T) {
	g := &loginGuard{m: make(map[string]*loginEntry)}
	ip := "1.2.3.4"

	for i := 0; i < maxLoginFailures-1; i++ {
		g.fail(ip)
		if g.locked(ip) {
			t.Fatalf("第 %d 次失败不应触发锁定", i+1)
		}
	}
	g.fail(ip) // 达到阈值
	if !g.locked(ip) {
		t.Fatal("达到失败阈值应锁定")
	}
	g.clear(ip)
	if g.locked(ip) {
		t.Fatal("清除后不应再锁定")
	}
}
