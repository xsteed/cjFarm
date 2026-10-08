// 顾客会员登录的第一步:wx.login 的 code → openid(code2session)。
//
// 与出码(getwxacodeunlimit,见 code.go)不同:code2session **不走 access_token**,
// 而是每次直接带 appid + secret 调用,因此无需任何缓存,天然幂等。
// code 由小程序端 wx.login 现取现用(只能用一次、5 分钟有效),后端不做任何存储。
package weapp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"dining-system/infra/logger"
)

// jscode2sessionEndpoint 微信登录凭证校验接口。
const jscode2sessionEndpoint = "/sns/jscode2session"

// ErrMemberDisabled 会员功能未开启(未配置小程序 AppID / AppSecret)。
// 用哨兵错误而非普通 error:service 层据此返回业务提示、前端隐藏「注册成为会员」入口,
// 而不是当成一次故障上报。
var ErrMemberDisabled = errors.New("会员功能未开启")

// MemberEnabled 顾客登录(会员)是否可用,与 Enabled() 同义,给 service 层一个语义化入口。
func MemberEnabled() bool { return Enabled() }

// Code2Session 用登录凭证 code 换小程序用户唯一标识 openid。
//
// 返回的 session_key 刻意丢弃:一期不解密手机号等敏感数据,按最小化原则不落库、不上传;
// 二期若需要,在这里补存储并同步补「用户隐私保护指引」声明。
func Code2Session(jsCode string) (string, error) {
	code := strings.TrimSpace(jsCode)
	if code == "" {
		return "", errors.New("缺少登录凭证")
	}
	if !Enabled() {
		return "", ErrMemberDisabled
	}
	q := url.Values{}
	q.Set("appid", cfg("wxpay_appid"))
	q.Set("secret", cfg("wx_app_secret"))
	q.Set("js_code", code)
	q.Set("grant_type", "authorization_code")

	resp, err := (&http.Client{Timeout: httpTimeout}).Get(apiBase + jscode2sessionEndpoint + "?" + q.Encode())
	if err != nil {
		return "", errors.New("微信服务繁忙，请稍后再试")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return "", errors.New("微信服务繁忙，请稍后再试")
	}
	var out struct {
		OpenID string `json:"openid"`
		wxErr
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		logger.Warnf("[weapp] code2session 响应解析失败: %s", snippet(raw))
		return "", errors.New("微信服务繁忙，请稍后再试")
	}
	if out.ErrCode != 0 {
		if isTokenErr(out.ErrCode) {
			// code2session 不用 access_token,理论上不会拿到 40001;
			// 一旦拿到(配置错乱/微信侧异常),顺手清掉出码用的 token 缓存,代价为零。
			invalidateToken()
		}
		return "", code2SessionErr(out.ErrCode, out.ErrMsg)
	}
	if strings.TrimSpace(out.OpenID) == "" {
		return "", errors.New("微信未返回用户标识，请稍后再试")
	}
	return out.OpenID, nil
}

// code2SessionErr 把 code2session 的错误码翻成可操作的中文提示。
// 与出码(codeErr)分开:两边的高频错误完全不同 —— 这里几乎都是「code 被用过/过期」。
func code2SessionErr(code int, msg string) error {
	switch code {
	case 40029, 40163:
		// code 无效/已被使用:小程序端会自动重试一次 wx.login,提示保持轻量。
		return errors.New("登录凭证无效，请重试")
	case 45011:
		return errors.New("请求过于频繁，请稍后再试")
	case 40226:
		return errors.New("当前微信账号存在风险，暂无法登录")
	case -1:
		return errors.New("微信服务繁忙，请稍后再试")
	}
	logger.Warnf("[weapp] code2session 失败(%d): %s", code, msg)
	return errors.New("微信登录失败，请稍后再试")
}
