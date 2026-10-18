// 手机号快速验证(getPhoneNumber 按钮)的服务端换取。
//
// 前置:小程序必须是**企业认证主体**且已开通「手机号快速验证组件」(微信按次计费),
// 未开通时本接口返回的 errcode 会被翻成可操作的中文提示。
// code 由前端 button open-type="getPhoneNumber" 的回调现取现用(一次性,5 分钟有效),
// 与 wx.login 的 code 是**两种凭证**,不可混用。
//
// 只落 purePhoneNumber(不带国际区号的纯号码);countryCode/水印等字段按最小化原则不存。
package weapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"dining-system/infrastructure/logger"
)

// phoneEndpoint 手机号快速验证组件的服务端换取接口。
const phoneEndpoint = "/wxa/business/getuserphonenumber?access_token="

// PhoneNumber 换取结果(只保留业务需要的字段)。
type PhoneNumber struct {
	// PurePhoneNumber 不带国际区号的纯手机号(11 位),落库与展示都用它。
	PurePhoneNumber string
}

// GetPhoneNumber 用 getPhoneNumber 回调的 code 换用户手机号。
//
// 走 access_token(stable token 缓存与出码共用);拿到 40001/42001 等令牌类错误时
// 顺手失效缓存,下次重取 —— 与 Code2Session 的处理一致。
func GetPhoneNumber(ctx context.Context, jsCode string) (PhoneNumber, error) {
	code := strings.TrimSpace(jsCode)
	if code == "" {
		return PhoneNumber{}, errors.New("缺少手机号授权凭证")
	}
	if !Enabled() {
		return PhoneNumber{}, ErrMemberDisabled
	}
	token, err := accessToken(ctx)
	if err != nil {
		return PhoneNumber{}, err
	}
	body, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return PhoneNumber{}, err
	}
	raw, _, err := doPost(apiBase+phoneEndpoint+token, body)
	if err != nil {
		logger.CtxWarnf(ctx, "[weapp] 手机号换取请求失败(网络/超时): %v", err)
		return PhoneNumber{}, errors.New("微信服务繁忙，请稍后再试")
	}

	var out struct {
		PhoneInfo struct {
			PurePhoneNumber string `json:"purePhoneNumber"`
		} `json:"phone_info"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// 响应体含手机号(个人信息),解析失败也不回显原文。
		logger.CtxWarnf(ctx, "[weapp] 手机号响应解析失败: %v", err)
		return PhoneNumber{}, errors.New("微信服务繁忙，请稍后再试")
	}
	if out.ErrCode != 0 {
		if isTokenErr(out.ErrCode) {
			invalidateToken()
		}
		return PhoneNumber{}, phoneErr(ctx, out.ErrCode, out.ErrMsg)
	}
	phone := strings.TrimSpace(out.PhoneInfo.PurePhoneNumber)
	if phone == "" {
		return PhoneNumber{}, errors.New("微信未返回手机号，请稍后再试")
	}
	return PhoneNumber{PurePhoneNumber: phone}, nil
}

// phoneErr 把手机号组件的高频错误码翻成可操作的中文提示。
// 40029/40163 = code 无效或已被用(前端重新拉起授权即可);45009 = 调用配额不足。
func phoneErr(ctx context.Context, code int, msg string) error {
	switch code {
	case 40029, 40163:
		return errors.New("授权已过期，请重新点击获取")
	case 45009:
		return errors.New("获取手机号次数已达上限，请稍后再试")
	case 43101:
		// 用户拒绝授权:微信在用户取消时也会回这个码,文案不要吓人。
		return errors.New("未授权手机号")
	}
	logger.CtxWarnf(ctx, "[weapp] 手机号换取失败(%d): %s", code, msg)
	return fmt.Errorf("获取手机号失败（%d），请稍后再试", code)
}
