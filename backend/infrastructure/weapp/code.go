// Package weapp 生成微信小程序码(桌牌点餐码)。
//
// 与 infrastructure/pay 的分工:微信支付走 api.mch.weixin.qq.com,需要商户证书签名;
// 小程序服务端接口走 api.weixin.qq.com,只认 access_token、不需要签名,
// 因此单独成包,不混进支付实现里。
//
// 配置项(存于 tb_config):
//
//	wxpay_appid         小程序 AppID(与微信支付共用同一项)
//	wx_app_secret       小程序 AppSecret(敏感项,落库前加密)
//	wx_code_env_version 出码指向的版本:release(默认)/ trial / develop
package weapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store/dao"
)

// apiBase 微信服务端接口根地址。声明为变量只为让单测能指向 httptest 服务器。
var apiBase = "https://api.weixin.qq.com"

// cacheDir 小程序码落盘目录。
//
// 用相对路径与 data/dining.db、data/master.key 保持同一基准(见 infrastructure/conf 的
// DefaultDBPath / DefaultMasterKeyPath):生产由 systemd 的 WorkingDirectory 固定
// 在 /opt/dining-system,三者一起正确解析。
var cacheDir = "data/weapp-code"

// PagePath 扫码后打开的小程序页面:点餐页(不是 app.json 的首页 —— 默认主页是
// pages/home/index,扫码仍应直达菜单,不让顾客多点一次)。
//
// 它必须存在于目标版本(且已随版本发布):check_path 为真时微信会校验,
// 写错会直接报 41030 invalid page。改动前先确认 src/app.json 的 pages 里仍有这一项。
const PagePath = "pages/index/index"

const (
	// 微信对 scene 的限制:≤32 个可见字符,只允许数字、英文字母与
	// !#$&'()*+,/:;=?@-._~ —— **不支持 %**,所以不能靠 urlencode 塞非 ASCII 内容。
	// 项目里的桌台稳定码是 8 位大写字母数字,天然满足;仍在此显式校验,
	// 免得把非法 scene 打到微信、只换回一个语焉不详的 40097。
	maxSceneLen = 32

	// width 合法区间与默认值(微信文档:280 ~ 1280,默认 430)。
	minWidth     = 280
	maxWidth     = 1280
	defaultWidth = 430

	// tokenRenewAhead 提前续期窗口:access_token 有效期 7200s,
	// 提前 5 分钟换新的,避免临界时刻拿到一个「马上过期」的 token。
	tokenRenewAhead = 5 * time.Minute

	// httpTimeout 单次调用微信接口的超时,与项目其它出站调用一致量级。
	httpTimeout = 10 * time.Second

	// maxRespBytes 响应体读取上限。1280px 的码图远小于此,
	// 设上限是为了防止异常响应把内存读爆。
	maxRespBytes = 4 << 20
)

// sceneCharset 微信允许出现在 scene 里的字符集合。
const sceneCharset = "0123456789" +
	"abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"!#$&'()*+,/:;=?@-._~"

// 出码参数里的固定部分:全部使用本接口、均已校验。使用 v2 版本路径(官方现行版本)。
const codeEndpoint = "/wxa/getwxacodeunlimit"

// stableTokenEndpoint 中控式 access_token 接口。
//
// 刻意不用老的 cgi-bin/token:后者「获取新 token 会让旧 token 立即失效」,
// 多进程/多实例部署时会互相踢,表现为随机 40001;
// stable_token 允许各实例各自缓存,互不影响。
const stableTokenEndpoint = "/cgi-bin/stable_token"

// CodeResult 一次出码的结果。
type CodeResult struct {
	// DataURL 形如 data:image/jpeg;base64,... 可直接塞进 <img src> 或打印页。
	//
	// 之所以回 dataURL 而不是图片二进制:项目前端请求层把响应统一解包成
	// {code,msg,data}(见 packages/api/src/http.ts),二进制响应过不了这层;
	// 而打印排版用的也正是 dataURL(见 apps/admin/src/utils/tableQr.ts 的
	// QrCard.dataUrl),JSON 里直接给字符串两端都省一次转换。
	DataURL string `json:"dataUrl"`
	// Cached 表示命中本地缓存,本次没有调用微信接口。
	Cached bool `json:"cached"`
	// EnvVersion 出码指向的版本(release / trial / develop),供前端提示。
	EnvVersion string `json:"envVersion"`
	// Width 实际生成的像素宽度(已按微信限制收敛)。
	Width int `json:"width"`
}

// Enabled 判定出码所需配置是否齐备(小程序 AppID + AppSecret)。
func Enabled() bool {
	return cfg("wxpay_appid") != "" && cfg("wx_app_secret") != ""
}

// EnvVersion 返回出码指向的小程序版本。
//
// 未配置或取值非法时一律按 release —— 正式发布后这是唯一正确的值,
// 而配错成非法值的后果(出码指向不存在的版本)比回退到 release 更糟。
func EnvVersion() string {
	switch v := cfg("wx_code_env_version"); v {
	case "trial", "develop":
		return v
	default:
		return "release"
	}
}

// TableCode 生成指定桌台的小程序码。
//
// scene 传桌台稳定码本身。同一个 scene + 同一套参数,微信给出的码恒定不变且永久有效,
// 因此结果按「参数指纹」落盘缓存:批量导出几十张时只有第一次真正调用微信接口,
// 既省日额度也避开 45009 频控。
func TableCode(ctx context.Context, scene string, width int) (CodeResult, error) {
	scene = strings.TrimSpace(scene)
	if err := validateScene(scene); err != nil {
		return CodeResult{}, err
	}
	if !Enabled() {
		return CodeResult{}, fmt.Errorf("未配置小程序 AppID / AppSecret，请先到「系统配置 → 小程序桌牌码」填写")
	}
	width = normWidth(width)
	env := EnvVersion()

	path := cachePath(scene, width, env)
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		return CodeResult{DataURL: toDataURL(raw), Cached: true, EnvVersion: env, Width: width}, nil
	}

	token, err := accessToken(ctx)
	if err != nil {
		return CodeResult{}, err
	}
	raw, err := fetchCode(ctx, token, scene, width, env)
	if err != nil {
		return CodeResult{}, err
	}
	// 缓存写失败不影响本次出码:下次再问微信一次即可(仅多花一次额度),故只告警。
	if err := writeCache(path, raw); err != nil {
		logger.CtxWarnf(ctx, "[weapp] 小程序码缓存写入失败(scene=%s): %v", scene, err)
	}
	return CodeResult{DataURL: toDataURL(raw), EnvVersion: env, Width: width}, nil
}

// validateScene 校验 scene 是否符合微信限制。
func validateScene(scene string) error {
	if scene == "" {
		return fmt.Errorf("缺少桌台码")
	}
	// 按字符数而非字节数判定:微信限制的是「可见字符」个数。
	if len([]rune(scene)) > maxSceneLen {
		return fmt.Errorf("桌台码超长：微信要求进入小程序的 scene 不超过 %d 个字符", maxSceneLen)
	}
	for _, r := range scene {
		if !strings.ContainsRune(sceneCharset, r) {
			return fmt.Errorf("桌台码含微信不支持的字符 %q：scene 只允许数字、字母与 !#$&'()*+,/:;=?@-._~", r)
		}
	}
	return nil
}

// normWidth 把像素宽度收敛到微信允许的区间。
func normWidth(w int) int {
	switch {
	case w <= 0:
		return defaultWidth
	case w < minWidth:
		return minWidth
	case w > maxWidth:
		return maxWidth
	default:
		return w
	}
}

// cachePath 由「参数指纹 + 桌台码」拼出缓存文件名。
//
// 指纹涵盖 AppID / 版本 / 页面 / 宽度:任一项变化都会换新文件,
// 因此不需要额外的失效逻辑 —— 改完配置不会拿到按旧参数生成的图。
func cachePath(scene string, width int, env string) string {
	parts := []string{cfg("wxpay_appid"), env, PagePath, fmt.Sprint(width), scene}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	name := fmt.Sprintf("%s-%s.jpg", safeName(scene), hex.EncodeToString(sum[:])[:8])
	return filepath.Join(cacheDir, name)
}

// safeName 把 scene 变成安全的文件名片段:scene 允许出现 / : ? 等在文件名里有含义的字符。
func safeName(scene string) string {
	var b strings.Builder
	for _, r := range scene {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "code"
	}
	return b.String()
}

func writeCache(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 0644:只是码图,不含敏感信息。
	return os.WriteFile(path, raw, 0o644)
}

// toDataURL 把图片字节转成 dataURL(类型按文件头判断)。
func toDataURL(raw []byte) string {
	return "data:" + sniffMIME(raw) + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// sniffMIME 判断图片类型:微信默认回 jpeg,is_hyaline 为真时回 png。
func sniffMIME(raw []byte) string {
	if bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	return "image/jpeg"
}

// fetchCode 调用 wxa/getwxacodeunlimit 取码图。
//
// 这里最大的坑:成功返回**图片二进制**、失败返回 **JSON**,而两者的 HTTP 状态码都是 200。
// 所以必须先看 Content-Type,再退回「能解析成 errcode 就按错误处理」,
// 绝不能只看状态码 —— 否则会把 {"errcode":41030} 当成一张图存下来,前端只显示一个破图。
func fetchCode(ctx context.Context, token, scene string, width int, env string) ([]byte, error) {
	payload := map[string]interface{}{
		"scene":       scene,
		"page":        PagePath,
		"width":       width,
		"env_version": env,
		"is_hyaline":  false,
		// check_path 只在 release 下为真:未发布的体验版/开发版页面过不了
		// page 存在性校验,会被直接拒成 41030。微信要求 env_version 非 release 时
		// 必须同时关掉校验,否则这个「先出码自测」的流程根本走不通。
		"check_path": env == "release",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	raw, ctype, err := doPost(apiBase+codeEndpoint+"?access_token="+url.QueryEscape(token), body)
	if err != nil {
		// 出码失败直接表现为「桌牌码生成不出来」,而网络层原因(DNS/超时/TLS)
		// 只以一句文案抛给用户,不留痕就只能靠猜。不打 URL:query 里带 access_token。
		logger.CtxWarnf(ctx, "[weapp] 调用微信出码接口失败 scene=%s env=%s: %v", scene, env, err)
		return nil, err
	}
	if strings.HasPrefix(ctype, "image/") {
		return raw, nil
	}
	var e wxErr
	if json.Unmarshal(raw, &e) == nil && e.ErrCode != 0 {
		if isTokenErr(e.ErrCode) {
			// token 失效:丢掉缓存,下一次出码自动换新的重试。
			invalidateToken()
		}
		logger.CtxWarnf(ctx, "[weapp] 微信出码返回错误 scene=%s errcode=%d errmsg=%s", scene, e.ErrCode, e.ErrMsg)
		return nil, codeErr(e.ErrCode, e.ErrMsg)
	}
	// 既不是图片也解析不出 errcode:通常是网关/代理返回了 HTML 错误页,
	// 这类「微信侧或网络侧异常」必须留痕,否则只会看到一个破图。
	logger.CtxWarnf(ctx, "[weapp] 微信出码返回非图片内容 scene=%s contentType=%s: %s", scene, ctype, snippet(raw))
	return nil, fmt.Errorf("微信出码返回了非图片内容：%s", snippet(raw))
}

// wxErr 是微信接口的通用错误响应体。
type wxErr struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// isTokenErr 判定是否为「access_token 不可用」类错误。
func isTokenErr(code int) bool {
	switch code {
	case 40001, 40014, 42001:
		return true
	}
	return false
}

// codeErr 把微信错误码翻成可操作的中文提示。
//
// 出码失败几乎都落在这几个码上,直接告诉使用者「去改哪一项」,
// 比把 errmsg 原样抛出来有意义得多(原先是英文短语,店员看不懂)。
func codeErr(code int, msg string) error {
	switch code {
	case 40001, 40014, 42001:
		return fmt.Errorf("微信拒绝访问（access_token 无效）：请检查「系统配置」里的小程序 AppID 与 AppSecret 是否填错")
	case 40013:
		return fmt.Errorf("小程序 AppID 无效：请检查「系统配置」里的小程序 AppID")
	case 40125:
		return fmt.Errorf("小程序 AppSecret 无效：请检查「系统配置」里的小程序 AppSecret")
	case 41030:
		return fmt.Errorf("出码失败：小程序页面 %s 不存在。请确认该页面已在目标版本（%s）中发布，或把「出码版本」先改成体验版", PagePath, EnvVersion())
	case 45009, 45011:
		return fmt.Errorf("微信接口调用频率超限，请稍后重试")
	case 40097:
		return fmt.Errorf("出码参数不合法（桌台码或尺寸超出微信限制）")
	}
	return fmt.Errorf("微信出码失败（%d）：%s", code, msg)
}

var (
	tokenMu     sync.Mutex
	tokenValue  string
	tokenExpiry time.Time
)

// accessToken 取 access_token,进程内缓存,并在到期前留出续期窗口。
//
// 取 token 期间持有互斥锁:出码是低频的管理端操作(且结果已落盘缓存),
// 让并发请求串行等一次取 token,比引入并发去重更简单,也不会对微信打出并发风暴。
func accessToken(ctx context.Context) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if tokenValue != "" && time.Now().Before(tokenExpiry) {
		return tokenValue, nil
	}
	body, err := json.Marshal(map[string]interface{}{
		"grant_type": "client_credential",
		"appid":      cfg("wxpay_appid"),
		"secret":     cfg("wx_app_secret"),
	})
	if err != nil {
		return "", err
	}
	raw, _, err := doPost(apiBase+stableTokenEndpoint, body)
	if err != nil {
		logger.CtxWarnf(ctx, "[weapp] 获取微信 access_token 请求失败(网络/超时): %v", err)
		return "", fmt.Errorf("获取微信 access_token 失败：%w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		wxErr
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// 不把 raw 回显到错误消息(会一路抛到管理端页面)也不写进日志:
		// 正常响应体里就是 access_token 明文,属敏感凭据。
		logger.CtxWarnf(ctx, "[weapp] 解析微信 access_token 响应失败: %v", err)
		return "", fmt.Errorf("解析微信 access_token 响应失败")
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		// 注意:此处**不能**调用 invalidateToken —— 我们正持有 tokenMu,会自锁。
		// 取 token 失败本就没有可失效的缓存值。
		// 这里是最该留痕的一处:appid/secret 配错、IP 不在白名单都表现为
		// 「全部桌牌码生成失败」,而用户看到的只有一句中文文案。
		logger.CtxWarnf(ctx, "[weapp] 微信拒绝下发 access_token errcode=%d errmsg=%s(请检查 wxpay_appid / wx_app_secret 配置)",
			out.ErrCode, out.ErrMsg)
		return "", codeErr(out.ErrCode, out.ErrMsg)
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= tokenRenewAhead {
		// 微信固定给 7200s;响应缺字段时按同值兜底。
		ttl = 2 * time.Hour
	}
	tokenValue = out.AccessToken
	tokenExpiry = time.Now().Add(ttl - tokenRenewAhead)
	return tokenValue, nil
}

// invalidateToken 丢弃缓存的 access_token,让下一次出码重新向微信换取。
// 拿到 token 类错误码时调用;内部自行加锁,故不可在持有 tokenMu 时调用。
func invalidateToken() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	tokenValue = ""
	tokenExpiry = time.Time{}
}

// doPost 发送 JSON 请求,回传响应体与 Content-Type。
func doPost(endpoint string, body []byte) (raw []byte, contentType string, err error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("微信接口返回 HTTP %d：%s", resp.StatusCode, snippet(raw))
	}
	return raw, resp.Header.Get("Content-Type"), nil
}

// snippet 截取响应片段用于报错,避免把整段响应体塞进日志与界面。
func snippet(raw []byte) string {
	runes := []rune(strings.TrimSpace(string(raw)))
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return string(runes)
}

// cfg 便捷读取配置项(空值返回空串)。
//
// 刻意直接走 dao 而不是 service.GetSetting(两者等价):本包已被 service 层反向使用
// (会员登录要调 Code2Session),若再依赖 service 会构成 import 环。
func cfg(key string) string {
	return dao.GetSetting(key)
}
