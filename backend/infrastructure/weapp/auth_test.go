package weapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// c2sInit 准备 Code2Session 测试环境:临时库 + 假微信 jscode2session 服务。
//
// apiBase 是包级变量(为了让单测能指向 httptest 服务器),必须显式还原,
// 否则会污染同包其它用例(它们可能拿到一个已关闭的地址)。
func c2sInit(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp-c2s.db"))
	t.Cleanup(func() { _ = store.DB.Close() })

	oldBase := apiBase
	t.Cleanup(func() { apiBase = oldBase })

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	apiBase = srv.URL

	for k, v := range map[string]string{
		"wxpay_appid":   "wx-test-appid",
		"wx_app_secret": "test-secret",
	} {
		if err := dao.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%q) 失败: %v", k, err)
		}
	}
}

// TestCode2SessionSuccess code 换 openid 成功,且请求带上了 appid/code。
func TestCode2SessionSuccess(t *testing.T) {
	c2sInit(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != jscode2sessionEndpoint {
			t.Errorf("意外路径 %s", r.URL.Path)
		}
		// query 里带 secret,只验证关键参数存在、不把 secret 回显进断言信息。
		if r.URL.Query().Get("js_code") == "" {
			t.Error("应携带 js_code")
		}
		if r.URL.Query().Get("appid") != "wx-test-appid" {
			t.Error("应携带 appid")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openid":"oOpenID123"}`))
	})

	// code 前后空白应被裁剪。
	openid, err := Code2Session(context.Background(), "  valid-code  ")
	if err != nil {
		t.Fatalf("Code2Session 应成功: %v", err)
	}
	if openid != "oOpenID123" {
		t.Fatalf("openid = %q, want oOpenID123", openid)
	}
}

// TestCode2SessionEmptyCode 空 code 直接报错(不发起网络请求)。
func TestCode2SessionEmptyCode(t *testing.T) {
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp-c2s-empty.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
	if err := dao.SetSetting("wxpay_appid", "wx"); err != nil {
		t.Fatal(err)
	}
	if err := dao.SetSetting("wx_app_secret", "secret"); err != nil {
		t.Fatal(err)
	}

	if _, err := Code2Session(context.Background(), "   "); err == nil {
		t.Fatal("空 code 应报错")
	}
}

// TestCode2SessionDisabled 未配置小程序 AppID/Secret 时返回哨兵错误 ErrMemberDisabled。
func TestCode2SessionDisabled(t *testing.T) {
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp-c2s-disabled.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
	if err := dao.SetSetting("wxpay_appid", ""); err != nil {
		t.Fatal(err)
	}
	if err := dao.SetSetting("wx_app_secret", ""); err != nil {
		t.Fatal(err)
	}

	_, err := Code2Session(context.Background(), "code")
	if !errors.Is(err, ErrMemberDisabled) {
		t.Fatalf("未配置时应返回 ErrMemberDisabled, got %v", err)
	}
}

// TestCode2SessionNetworkError 网络失败只回「微信服务繁忙」(不泄露 query 里的 secret)。
func TestCode2SessionNetworkError(t *testing.T) {
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp-c2s-net.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
	if err := dao.SetSetting("wxpay_appid", "wx"); err != nil {
		t.Fatal(err)
	}
	if err := dao.SetSetting("wx_app_secret", "secret"); err != nil {
		t.Fatal(err)
	}

	// 指向一个已关闭的服务,强制连接失败。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	_, err := Code2Session(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "微信服务繁忙") {
		t.Fatalf("网络失败应返回「微信服务繁忙」, got %v", err)
	}
}

// TestCode2SessionWechatErrorCode 微信返回错误码时应翻成中文提示。
func TestCode2SessionWechatErrorCode(t *testing.T) {
	c2sInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":40029,"errmsg":"invalid code"}`))
	})
	_, err := Code2Session(context.Background(), "used-code")
	if err == nil || !strings.Contains(err.Error(), "登录凭证无效") {
		t.Fatalf("errcode 40029 应提示「登录凭证无效」, got %v", err)
	}
}

// TestCode2SessionEmptyOpenid 微信没回 openid 时应报错(不把空标识当成功)。
func TestCode2SessionEmptyOpenid(t *testing.T) {
	c2sInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openid":""}`))
	})
	_, err := Code2Session(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "未返回用户标识") {
		t.Fatalf("空 openid 应报错, got %v", err)
	}
}

// TestCode2SessionMalformedJSON 响应体无法解析时不泄露原文,统一回「微信服务繁忙」。
func TestCode2SessionMalformedJSON(t *testing.T) {
	c2sInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	})
	_, err := Code2Session(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "微信服务繁忙") {
		t.Fatalf("解析失败应返回「微信服务繁忙」, got %v", err)
	}
}

// TestMemberEnabled 与 Enabled 同义:配置齐备为 true,缺 AppSecret 为 false。
func TestMemberEnabled(t *testing.T) {
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp-c2s-member.db"))
	t.Cleanup(func() { _ = store.DB.Close() })

	if err := dao.SetSetting("wxpay_appid", "wx"); err != nil {
		t.Fatal(err)
	}
	if err := dao.SetSetting("wx_app_secret", "secret"); err != nil {
		t.Fatal(err)
	}
	if !MemberEnabled() {
		t.Fatal("配置齐备时 MemberEnabled 应为 true")
	}
	if err := dao.SetSetting("wx_app_secret", ""); err != nil {
		t.Fatal(err)
	}
	if MemberEnabled() {
		t.Fatal("缺 AppSecret 时 MemberEnabled 应为 false")
	}
}
