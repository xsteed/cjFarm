// 顾客会员域用例:一键注册/登录、会员卡、我的订单、下单归属维护。
//
// 设计见 docs/design-docs/customer-member/spec.md。三条不变式:
//  1. 匿名路径永不断 —— 登录是自愿的,未登录顾客的点餐/加菜/催菜行为与从前完全一致;
//  2. 归属只认令牌 —— 订单归属由服务端从顾客令牌解析,请求体里的任何字段都不可信;
//  3. 展示列尽力而为 —— order_count/last_order_time 只为会员卡展示,维护失败不影响下单。
package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store/dao"
	"dining-system/infrastructure/weapp"
)

// MemberEnabled 会员功能是否开启(小程序 AppID / AppSecret 已配置)。
// 顾客端公开配置里带上该标记,前端据此显隐「注册成为会员」入口。
func MemberEnabled() bool {
	return weapp.MemberEnabled()
}

// CustomerLoginResult 顾客登录/注册的结果:令牌 + 会员档案 + 是否首次(注册)。
type CustomerLoginResult struct {
	Token    string
	IsNew    bool
	Customer *po.Customer
}

// CustomerLogin 微信一键注册/登录:
//
//	code → code2session 换 openid → 首次出现建档(isNew=true)或复登 → 签发顾客令牌。
//
// code 由小程序端 wx.login 现取现用;无效时返回可重试的提示,由前端自动重试一次。
func CustomerLogin(ctx context.Context, code, ip string) (CustomerLoginResult, error) {
	if err := allowCustomerLogin(ip); err != nil {
		return CustomerLoginResult{}, err
	}
	openid, err := weapp.Code2Session(ctx, code)
	if err != nil {
		// weapp 返回的都是「可直接给用户看」的文案(含会员功能未开启),
		// 这里不再包装,避免把可操作的提示换成笼统的「系统繁忙」。
		return CustomerLoginResult{}, err
	}
	return customerLoginByOpenid(ctx, openid, ip)
}

// customerLoginByOpenid 由 openid 完成建档或复登。
// 与微信交互解耦成独立函数,是为了单测可以不经 code2session 直接验证登录语义
// (weapp 的 apiBase 是包内变量,跨包测试无法打桩)。
func customerLoginByOpenid(ctx context.Context, openid, ip string) (CustomerLoginResult, error) {
	cust, err := dao.GetCustomerByOpenid(openid)
	if err != nil {
		// 顾客说「登录不上」时,这类建档/查档失败是唯一线索;带上 requestID
		// 才能把它与同一次请求的其它日志、以及访问日志关联起来。
		logger.CtxWarnf(ctx, "[member] 按 openid 查档失败: %v", err)
		return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
	}

	isNew := false
	if cust == nil {
		id, insErr := dao.InsertCustomer(po.Customer{OpenID: openid})
		if insErr != nil {
			// 并发首登:另一请求刚建档(唯一索引冲突)。转按 openid 重查走登录分支。
			cust, err = dao.GetCustomerByOpenid(openid)
			if err != nil || cust == nil {
				logger.CtxWarnf(ctx, "[member] 并发建档后重查失败: %v", insErr)
				return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
			}
		} else {
			isNew = true
			if cust, err = dao.GetCustomerByID(int(id)); err != nil || cust == nil {
				logger.CtxWarnf(ctx, "[member] 建档后回读失败: %v", err)
				return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
			}
		}
	}

	if cust.Status != po.CustomerStatusEnabled {
		return CustomerLoginResult{}, errors.New("会员账号不可用")
	}
	dao.TouchCustomerLogin(cust.CustomerID)
	return CustomerLoginResult{
		Token:    GenCustomerToken(cust.CustomerID),
		IsNew:    isNew,
		Customer: cust,
	}, nil
}

// CustomerMemberProfile 会员卡信息。
func CustomerMemberProfile(customerID int) (*po.Customer, error) {
	c, err := dao.GetCustomerByID(customerID)
	if err != nil {
		return nil, errors.New("系统繁忙，请稍后再试")
	}
	if c == nil {
		// 令牌有效但档案没了(如手工清库):按未登录处理,让前端走重新登录。
		return nil, errors.New("请先登录")
	}
	return c, nil
}

// CustomerMyOrders 会员的历史订单(跨桌,分页)。
func CustomerMyOrders(customerID, pageNum, pageSize int) (int, []dao.CustomerOrderRow, error) {
	return dao.ListOrdersByCustomer(customerID, pageNum, pageSize)
}

// NotifyCustomerOrder 下单成功后维护会员卡展示列。展示数据,失败仅告警。
// 只在「真正插入新单」的路径调用;幂等命中(重复请求回放)不调用,避免重复计数。
func NotifyCustomerOrder(customerID int) {
	if customerID <= 0 {
		return
	}
	dao.TouchCustomerOrder(customerID)
}

// ============================================================================
// 登录限流:防滥用 / 保护门店在微信侧的调用额度
// ============================================================================

const (
	customerLoginWindow    = time.Minute
	customerLoginMaxPerMin = 30
	customerLoginMaxKeys   = 4096
)

type customerRateWindow struct {
	start time.Time
	count int
}

var (
	customerLoginMu      sync.Mutex
	customerLoginWindows = map[string]*customerRateWindow{}
	customerLoginPruneAt time.Time
)

// allowCustomerLogin 同 IP 固定窗口限流:每分钟最多 30 次 code2session。
// 微信侧另有 45011 频控,这里的作用是「在打到微信之前先拦住」,保护门店自身的额度与响应时间。
func allowCustomerLogin(ip string) error {
	if strings.TrimSpace(ip) == "" {
		return nil
	}
	customerLoginMu.Lock()
	defer customerLoginMu.Unlock()
	now := time.Now()
	if len(customerLoginWindows) > customerLoginMaxKeys && now.After(customerLoginPruneAt) {
		customerLoginPruneAt = now.Add(time.Minute)
		for k, w := range customerLoginWindows {
			if now.Sub(w.start) > 2*customerLoginWindow {
				delete(customerLoginWindows, k)
			}
		}
	}
	w := customerLoginWindows[ip]
	if w == nil || now.Sub(w.start) >= customerLoginWindow {
		w = &customerRateWindow{start: now}
		customerLoginWindows[ip] = w
	}
	w.count++
	if w.count > customerLoginMaxPerMin {
		return errors.New("请求过于频繁，请稍后再试")
	}
	return nil
}
