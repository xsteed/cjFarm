package weapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// wcStub 记录假微信服务被调用的次数。
type wcStub struct {
	CodeCalls  int32
	TokenCalls int32
	// lastPayload 是最后一次出码请求体,用于断言真正发出去的参数。
	lastPayload map[string]interface{}
}

// jpegBytes 一段带 JPEG 文件头的假图片(内容无需是合法 JPEG,本包只按文件头判类型)。
var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("fake-jpeg-body")...)

// wcInit 准备测试环境:临时库 + 临时缓存目录 + 假微信服务。
//
// 缓存目录与微信根地址都是包级变量,必须显式还原,否则会污染同包其它用例
// (它们拿到的可能是一个已被关掉的 httptest 地址)。
func wcInit(t *testing.T, codeResp http.HandlerFunc) *wcStub {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "uploads"))
	store.Init(filepath.Join(t.TempDir(), "weapp.db"))
	t.Cleanup(func() { _ = store.DB.Close() })

	oldDir, oldBase := cacheDir, apiBase
	cacheDir = t.TempDir()
	invalidateToken()
	t.Cleanup(func() {
		cacheDir, apiBase = oldDir, oldBase
		invalidateToken()
	})

	stub := &wcStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case stableTokenEndpoint:
			atomic.AddInt32(&stub.TokenCalls, 1)
			_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":7200}`))
		case codeEndpoint:
			atomic.AddInt32(&stub.CodeCalls, 1)
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			stub.lastPayload = payload
			codeResp(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	apiBase = srv.URL

	for k, v := range map[string]string{
		"wxpay_appid":         "wx-test-appid",
		"wx_app_secret":       "test-secret",
		"wx_code_env_version": "release",
	} {
		if err := dao.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%q) 失败: %v", k, err)
		}
	}
	return stub
}

// TestTableCodeSuccessAndCache 出码成功,且第二次命中本地缓存。
//
// 缓存是硬需求而非优化:批量导出几十张桌牌时,没有缓存就会对微信打几十次接口
// (而同一个 scene 的码恒定不变,多打纯属浪费额度)。
func TestTableCodeSuccessAndCache(t *testing.T) {
	stub := wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})

	first, err := TableCode("K7M3PQ9X", 900)
	if err != nil {
		t.Fatalf("首次出码应成功: %v", err)
	}
	if !strings.HasPrefix(first.DataURL, "data:image/jpeg;base64,") {
		t.Fatalf("应返回 jpeg dataURL, got %q", first.DataURL[:40])
	}
	if first.Cached {
		t.Fatal("首次出码不应标记为缓存命中")
	}
	if first.Width != 900 || first.EnvVersion != "release" {
		t.Fatalf("宽度/版本回传不符: width=%d env=%s", first.Width, first.EnvVersion)
	}

	// 真正发给微信的参数:scene 用裸桌台码,release 下必须开 check_path。
	if got := stub.lastPayload["scene"]; got != "K7M3PQ9X" {
		t.Fatalf("scene = %v, want K7M3PQ9X", got)
	}
	if got := stub.lastPayload["page"]; got != PagePath {
		t.Fatalf("page = %v, want %s", got, PagePath)
	}
	if got := stub.lastPayload["check_path"]; got != true {
		t.Fatalf("release 下 check_path 应为 true, got %v", got)
	}

	second, err := TableCode("K7M3PQ9X", 900)
	if err != nil {
		t.Fatalf("二次出码应成功: %v", err)
	}
	if !second.Cached || second.DataURL != first.DataURL {
		t.Fatal("相同参数第二次应命中缓存并返回同一张图")
	}
	if n := atomic.LoadInt32(&stub.CodeCalls); n != 1 {
		t.Fatalf("命中缓存后不应再打微信出码接口, 出码调用 = %d", n)
	}
	if n := atomic.LoadInt32(&stub.TokenCalls); n != 1 {
		t.Fatalf("access_token 应在进程内复用, 取 token 调用 = %d", n)
	}
}

// TestTableCodeWechatErrorBodyIsNotCachedAsImage 微信返回错误 JSON 时不能存成图片。
//
// 这是本接口最容易踩的坑:成功回图片二进制、失败回 JSON,而两者 HTTP 状态码都是 200。
// 一旦只看状态码,{"errcode":41030} 会被当成码图落盘,前端只显示一个破图,
// 而且缓存还在,后续所有请求都会拿到这张「坏图」。
func TestTableCodeWechatErrorBodyIsNotCachedAsImage(t *testing.T) {
	wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		// 注意:状态码 200,响应体却是 JSON —— 与微信的真实行为一致。
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":41030,"errmsg":"invalid page"}`))
	})

	_, err := TableCode("K7M3PQ9X", 0)
	if err == nil {
		t.Fatal("微信返回错误码时应失败")
	}
	// 错误文案要能指向可操作项(页面未发布是这个码最常见的原因)。
	if !strings.Contains(err.Error(), "页面") || !strings.Contains(err.Error(), PagePath) {
		t.Fatalf("错误提示应指明页面问题, got %q", err.Error())
	}
	entries, err2 := os.ReadDir(cacheDir)
	if err2 != nil {
		t.Fatalf("读取缓存目录失败: %v", err2)
	}
	if len(entries) != 0 {
		t.Fatalf("错误响应绝不能落盘为图片, got %v", entries)
	}
}

// TestTableCodeRetriesAfterTokenExpired token 失效后自动丢弃缓存并重取。
func TestTableCodeRetriesAfterTokenExpired(t *testing.T) {
	var codeCalls int32
	stub := wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		// 第一次说 token 无效,第二次正常出图(模拟微信踢掉旧 token 的场景)。
		if atomic.AddInt32(&codeCalls, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"invalid credential"}`))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})

	_, err := TableCode("K7M3PQ9X", 0)
	if err == nil {
		t.Fatal("第一次应因 token 失效而失败")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Fatalf("错误提示应指出 access_token 问题, got %q", err.Error())
	}

	res, err := TableCode("K7M3PQ9X", 0)
	if err != nil {
		t.Fatalf("token 失效后重试应成功: %v", err)
	}
	if res.Cached {
		t.Fatal("重试成功得到的是新图,不应标记为缓存命中")
	}
	if n := atomic.LoadInt32(&stub.TokenCalls); n != 2 {
		t.Fatalf("token 失效后应重取一次 access_token, 取 token 调用 = %d", n)
	}
}

// TestTableCodeNonReleaseEnvSkipsPathCheck 非正式版必须关掉 page 存在性校验。
//
// 微信要求:env_version 为 trial/develop 时,page 可能尚未发布,
// 若 check_path 仍为 true 会被直接拒成 41030 —— 「先出体验版码自测」这条路就走不通。
func TestTableCodeNonReleaseEnvSkipsPathCheck(t *testing.T) {
	stub := wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})
	if err := dao.SetSetting("wx_code_env_version", "trial"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}

	res, err := TableCode("K7M3PQ9X", 0)
	if err != nil {
		t.Fatalf("体验版出码应成功: %v", err)
	}
	if res.EnvVersion != "trial" {
		t.Fatalf("EnvVersion = %q, want trial", res.EnvVersion)
	}
	if got := stub.lastPayload["check_path"]; got != false {
		t.Fatalf("trial 下 check_path 应为 false, got %v", got)
	}
	if got := stub.lastPayload["env_version"]; got != "trial" {
		t.Fatalf("env_version = %v, want trial", got)
	}
}

// TestTableCodeCacheKeyCoversParams 参数变化必须换缓存文件,否则改完配置还会拿到旧图。
func TestTableCodeCacheKeyCoversParams(t *testing.T) {
	wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})

	base := cachePath("K7M3PQ9X", 900, "release")
	if base == cachePath("K7M3PQ9X", 640, "release") {
		t.Fatal("宽度变化应换缓存文件")
	}
	if base == cachePath("K7M3PQ9X", 900, "trial") {
		t.Fatal("出码版本变化应换缓存文件")
	}
	if base == cachePath("K7M3PQ9Y", 900, "release") {
		t.Fatal("不同桌台码不应共用缓存文件")
	}
	if base != cachePath("K7M3PQ9X", 900, "release") {
		t.Fatal("相同参数应得到同一缓存文件")
	}
}

func TestTableCodeRequiresConfig(t *testing.T) {
	wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})
	if err := dao.SetSetting("wx_app_secret", ""); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	if Enabled() {
		t.Fatal("缺 AppSecret 时不应判定为可用")
	}
	_, err := TableCode("K7M3PQ9X", 0)
	if err == nil || !strings.Contains(err.Error(), "AppSecret") {
		t.Fatalf("未配置时应给出指向系统配置的错误, got %v", err)
	}
}

func TestValidateScene(t *testing.T) {
	if err := validateScene("K7M3PQ9X"); err != nil {
		t.Fatalf("合法桌台码不应报错: %v", err)
	}
	if err := validateScene(""); err == nil {
		t.Fatal("空桌台码应报错")
	}
	// 33 个字符:微信上限是 32。
	if err := validateScene(strings.Repeat("A", maxSceneLen+1)); err == nil {
		t.Fatal("超长 scene 应报错")
	}
	if err := validateScene(strings.Repeat("A", maxSceneLen)); err != nil {
		t.Fatalf("恰好 %d 字符应合法: %v", maxSceneLen, err)
	}
	// % 不在微信允许的字符集里(因此不能用 urlencode 处理 scene)。
	if err := validateScene("A%3D1"); err == nil {
		t.Fatal("含 % 的 scene 应报错")
	}
}

func TestNormWidth(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, defaultWidth},
		{-1, defaultWidth},
		{100, minWidth},
		{900, 900},
		{2000, maxWidth},
	}
	for _, c := range cases {
		if got := normWidth(c.in); got != c.want {
			t.Fatalf("normWidth(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestSafeName 桌台码以外的 scene(允许出现 / : ? 等字符)不能拼出非法文件名。
func TestSafeName(t *testing.T) {
	if got := safeName("K7M3PQ9X"); got != "K7M3PQ9X" {
		t.Fatalf("安全字符应原样保留, got %q", got)
	}
	if got := safeName("a/b:c?d"); got != "a_b_c_d" {
		t.Fatalf("路径字符应被替换, got %q", got)
	}
	if got := safeName("///"); strings.ContainsAny(got, "/\\") {
		t.Fatalf("文件名不应含路径分隔符, got %q", got)
	}
}

func TestEnvVersionFallback(t *testing.T) {
	wcInit(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	})
	// 非法取值回退为 release:宁可指向正式版,也不指向一个不存在的版本。
	if err := dao.SetSetting("wx_code_env_version", "production"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	if got := EnvVersion(); got != "release" {
		t.Fatalf("非法取值应回退 release, got %q", got)
	}
	if err := dao.SetSetting("wx_code_env_version", "develop"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	if got := EnvVersion(); got != "develop" {
		t.Fatalf("develop 应被接受, got %q", got)
	}
}
