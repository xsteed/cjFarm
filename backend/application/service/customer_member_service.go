// 顾客会员域用例:一键注册/登录、会员卡、我的订单、下单归属维护。
//
// 设计见 docs/design-docs/customer-member-identity/spec.md 与
// docs/design-docs/customer-member/spec.md。四条不变式:
//  1. 匿名路径永不断 —— 登录是自愿的,未登录顾客的点餐/加菜/催菜行为与从前完全一致;
//  2. 归属只认令牌 —— 订单归属由服务端从顾客令牌解析,请求体里的任何字段都不可信;
//  3. 展示列尽力而为 —— order_count/last_order_time 只为会员卡展示,维护失败不影响下单;
//  4. 登录方式可插拔 —— 账号(customer_id)与登录方式(provider)分离,新增端只加一个凭据校验器。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store/dao"
	"dining-system/infrastructure/weapp"
)

// ============================================================================
// 登录方式(provider)
// ============================================================================

// ProviderPayload 一次登录请求里各 provider 需要的凭据。
//
// 目前只有微信小程序用 code;后续 phone 会用 phone+smsCode、password 会用
// account+password,都往这里加字段。响应体不变,前端按 provider 传对应的键。
type ProviderPayload struct {
	Code string
}

// memberSwitchOn 会员功能总开关(管理端「营业设置 → 会员功能」,tb_config 的 member_enabled)。
//
// 它是唯一的显隐/放行口径,与登录能力(AppSecret 是否配置)解耦 —— 显隐归
// 商家运营决定,不被技术配置顺手决定(spec 12.5 的预留设计)。
//
// 「默认关」语义(fail-closed):只有明确 "1" 才算开启,其余(含配置缺失、脏值)
// 一律按关闭处理,与 SettingFlag 同一口径 —— 宁可少一个会员入口,也不要在
// 未配 AppSecret 的部署上出现「入口在、点注册必失败」的哑火状态。
func memberSwitchOn() bool { return dao.GetSetting("member_enabled") == "1" }

// CustomerLoginResult 顾客登录/注册的结果:令牌 + 会员档案 + 是否首次(注册)。
type CustomerLoginResult struct {
	Token    string
	IsNew    bool
	Customer *po.Customer
}

// CustomerLogin 微信小程序一键注册/登录(向后兼容入口)。
//
// 等价于 CustomerLoginByProvider(ctx, wx_mp, {Code: code}, ip);
// 保留它是因为历史调用方与测试都按这个语义书写。
func CustomerLogin(ctx context.Context, code, ip string) (CustomerLoginResult, error) {
	return CustomerLoginByProvider(ctx, po.CustomerProviderWxMP, ProviderPayload{Code: code}, ip)
}

// CustomerLoginByProvider 按登录方式分派:校验凭据 → 查档/建档 → 签发令牌。
//
// provider 缺省为 wx_mp:老版本小程序的请求体只有 {code},加了这一维之后
// 它们一行不改也必须能继续登录(spec D4)。
func CustomerLoginByProvider(ctx context.Context, provider string, p ProviderPayload, ip string) (CustomerLoginResult, error) {
	// 总闸先于一切:管理端关闭会员后,连静默重登也不放行 —— 否则入口虽藏了,
	// 令牌过期触发的自动重登仍会绕过开关继续续命会员态。
	if !memberSwitchOn() {
		return CustomerLoginResult{}, errors.New("会员功能未开启")
	}
	provider = po.NormalizeCustomerProvider(provider)
	if !po.IsKnownCustomerProvider(provider) {
		return CustomerLoginResult{}, errors.New("暂不支持该登录方式")
	}
	// 限流按 provider 分开计数:手机号登录要防的是短信轰炸(与 code2session 的额度
	// 完全不是一回事),共用一份窗口会让其中一边被另一边误伤。
	if err := allowCustomerLogin(provider, ip); err != nil {
		return CustomerLoginResult{}, err
	}
	externalID, unionid, err := verifyCredential(ctx, provider, p)
	if err != nil {
		// verifyCredential 返回的都是「可直接给用户看」的文案(含会员功能未开启),
		// 这里不再包装,避免把可操作的提示换成笼统的「系统繁忙」。
		return CustomerLoginResult{}, err
	}
	return customerLoginByIdentity(ctx, provider, externalID, unionid)
}

// verifyCredential 把各 provider 的凭据换成「该方式下的用户标识」。
// 新增登录方式只在这里加一个 case,其余流程(查档/建档/令牌)完全复用。
func verifyCredential(ctx context.Context, provider string, p ProviderPayload) (externalID, unionid string, err error) {
	switch provider {
	case po.CustomerProviderWxMP:
		openid, e := weapp.Code2Session(ctx, p.Code)
		return openid, "", e
	default:
		// 枚举已登记但实现未落地(手机号 / 密码 / App / Apple):
		// 明确拒绝,不落库、不签发令牌。
		return "", "", errors.New("暂不支持该登录方式")
	}
}

// customerLoginByIdentity 由「登录方式 + 用户标识」完成建档或复登。
// 与具体 provider 解耦成独立函数,是为了单测可以不经 code2session 直接验证登录语义
// (weapp 的 apiBase 是包内变量,跨包测试无法打桩)。
func customerLoginByIdentity(ctx context.Context, provider, externalID, unionid string) (CustomerLoginResult, error) {
	cust, err := dao.GetCustomerByIdentity(provider, externalID)
	if err != nil {
		// 顾客说「登录不上」时,这类建档/查档失败是唯一线索;带上 requestID
		// 才能把它与同一次请求的其它日志、以及访问日志关联起来。
		logger.CtxWarnf(ctx, "[member] 按身份查档失败(%s): %v", provider, err)
		return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
	}

	isNew := false
	if cust == nil {
		id, insErr := dao.CreateCustomerWithIdentity(provider, externalID, unionid)
		if insErr != nil {
			// 并发首登:另一请求刚绑定同一身份(唯一索引冲突)。建档与绑定在同一个
			// 事务里,冲突时档案已整体回滚,不会留下没有登录方式的孤儿行。
			cust, err = dao.GetCustomerByIdentity(provider, externalID)
			if err != nil || cust == nil {
				logger.CtxWarnf(ctx, "[member] 并发建档后重查失败(%s): %v", provider, insErr)
				return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
			}
		} else {
			isNew = true
			if cust, err = dao.GetCustomerByID(id); err != nil || cust == nil {
				logger.CtxWarnf(ctx, "[member] 建档后回读失败: %v", err)
				return CustomerLoginResult{}, errors.New("系统繁忙，请稍后再试")
			}
		}
	}

	if cust.Status != po.CustomerStatusEnabled {
		return CustomerLoginResult{}, errors.New("会员账号不可用")
	}
	dao.TouchCustomerLogin(cust.CustomerID)
	dao.TouchIdentityLogin(provider, externalID)
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
// 顾客自助:完善资料 / 注销
// ============================================================================

// maxNicknameRunes 昵称长度上限。tb_customer.nickname 是 VARCHAR(64),
// 取 32 个字符留余量(后台列表/会员卡都要能一行放下)。
const maxNicknameRunes = 32

// CustomerUpdateProfile 会员自助完善资料(目前只有昵称)。
//
// 头像不支持顾客自助修改:上传接口 /api/common/upload 要求管理端权限,
// 给顾客开一条上传通道是另一个功能(还要防滥用),本期不做。这里读原值回写,
// 避免「只改昵称」把头像清空。
func CustomerUpdateProfile(customerID int, nickname string) (*po.Customer, error) {
	nickname = strings.TrimSpace(nickname)
	if utf8.RuneCountInString(nickname) > maxNicknameRunes {
		return nil, fmt.Errorf("昵称最多 %d 个字", maxNicknameRunes)
	}
	c, err := dao.GetCustomerByID(customerID)
	if err != nil {
		return nil, errors.New("系统繁忙，请稍后再试")
	}
	if c == nil {
		return nil, errors.New("请先登录")
	}
	if err := dao.UpdateCustomerProfile(customerID, nickname, c.AvatarURL); err != nil {
		logger.Warnf("[member] 更新会员资料失败(id=%d): %v", customerID, err)
		return nil, errors.New("保存失败，请稍后再试")
	}
	return dao.GetCustomerByID(customerID)
}

// CustomerDeleteAccount 注销会员(顾客自助)。
//
// 语义见 spec §2.6:档案与登录方式物理删除,历史订单保留但归属置 0(变回匿名单)。
// 令牌无状态,注销后下一次请求会因为查不到档案而 401,不需要额外的吊销逻辑。
func CustomerDeleteAccount(customerID int) error {
	if err := dao.DeleteCustomerAccount(customerID); err != nil {
		logger.Warnf("[member] 注销会员失败(id=%d): %v", customerID, err)
		return errors.New("注销失败，请稍后再试")
	}
	return nil
}

// ============================================================================
// 管理端:会员管理
// ============================================================================

// ListMembers 分页查询会员(后台会员页)。
func ListMembers(keyword string, status *int, pageNum, pageSize int) (int, []dao.MemberRow, error) {
	return dao.ListMembers(dao.MemberQuery{
		Keyword: strings.TrimSpace(keyword),
		Status:  status,
	}, pageNum, pageSize)
}

// SetMemberStatus 启用/停用会员。停用后现有令牌在下一次校验时立即失效(每次都查 status)。
func SetMemberStatus(customerID, status int) error {
	if status != po.CustomerStatusEnabled && status != po.CustomerStatusDisabled {
		return errors.New("会员状态不合法")
	}
	c, err := dao.GetCustomerByID(customerID)
	if err != nil {
		return errors.New("系统繁忙，请稍后再试")
	}
	if c == nil {
		return errors.New("会员不存在")
	}
	if c.Status == status {
		return nil
	}
	return dao.SetCustomerStatus(customerID, status)
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

// allowCustomerLogin 同 IP 固定窗口限流:每分钟最多 30 次登录尝试。
//
// 按 provider 分开计数:微信小程序登录是 IP 维度(微信侧另有 45011 频控,
// 这里的作用是「在打到微信之前先拦住」,保护门店自身的额度与响应时间);
// 后续接手机号登录时必须换成「手机号 + IP」双维度并单独配阈值 ——
// 短信是按条计费的,30 次/分的 IP 窗口挡不住短信轰炸,届时应另起一套,
// 而不是复用本函数的额度。
func allowCustomerLogin(provider, ip string) error {
	if strings.TrimSpace(ip) == "" {
		return nil
	}
	key := provider + "|" + ip
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
	w := customerLoginWindows[key]
	if w == nil || now.Sub(w.start) >= customerLoginWindow {
		w = &customerRateWindow{start: now}
		customerLoginWindows[key] = w
	}
	w.count++
	if w.count > customerLoginMaxPerMin {
		return errors.New("请求过于频繁，请稍后再试")
	}
	return nil
}
