package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// memberRouter 会员相关接口的最小路由(与 main.go 的注册方式保持一致):
// 会员接口挂必选身份,下单挂可选身份(匿名路径永不断)。
func memberRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.POST("/customer/order", CustomerAuthOptional, CustomerCreateOrder)
	api.POST("/customer/auth/login", CustomerMemberLogin)
	api.GET("/customer/auth/profile", CustomerAuthRequired, CustomerMemberProfile)
	api.GET("/customer/order/mine", CustomerAuthRequired, CustomerMyOrders)
	return r
}

// memberInitDB 建临时库并跑完迁移/种子。
func memberInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "member.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// memberGet 发起带可选 Authorization 头的 GET 请求。
func memberGet(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// memberPost 发起带 JSON 体与可选 Authorization 头的 POST 请求。
func memberPost(r *gin.Engine, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// memberSeed 建一个会员档案(绑 wx_mp 身份)并附两笔订单:一笔归属会员、一笔匿名。
// (不经 CustomerLogin:那需要打桩微信的 code2session,跨包做不到,也不必。)
// 返回会员档案/会员令牌/匿单单号。
func memberSeed(t *testing.T) (*po.Customer, string, string) {
	t.Helper()
	id, err := dao.CreateCustomerWithIdentity(po.CustomerProviderWxMP, "openid-handler", "")
	if err != nil {
		t.Fatalf("插入会员失败: %v", err)
	}
	cust, err := dao.GetCustomerByID(id)
	if err != nil || cust == nil {
		t.Fatalf("回读会员失败: %v", err)
	}
	now := store.Now()
	for _, tc := range []struct {
		orderNo    string
		customerID int
	}{
		{orderNo: "M2026100800001", customerID: cust.CustomerID},
		{orderNo: "M2026100800002", customerID: 0},
	} {
		if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
			dish_amount, seat_fee, discount_amount, total_amount, pay_status, customer_id, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			tc.orderNo, 1, "01", "大厅01桌", 2, 4, 2800, 0, 0, 2800, 1, tc.customerID, now, now); err != nil {
			t.Fatalf("插入订单失败: %v", err)
		}
	}
	return cust, service.GenCustomerToken(cust.CustomerID), "M2026100800002"
}

// TestMemberProfileRequiresToken 未登录/令牌不对访问会员接口应 401。
func TestMemberProfileRequiresToken(t *testing.T) {
	memberInitDB(t)
	r := memberRouter()

	if w := memberGet(r, "/api/customer/auth/profile", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401, got %d body=%s", w.Code, w.Body.String())
	}
	// 管理端令牌与顾客令牌互不通用;乱串同理。
	if w := memberGet(r, "/api/customer/auth/profile", service.GenToken("admin", 1, 1)); w.Code != http.StatusUnauthorized {
		t.Fatalf("管理端令牌应被顾客接口拒绝, got %d", w.Code)
	}
	if w := memberGet(r, "/api/customer/auth/profile", "garbage"); w.Code != http.StatusUnauthorized {
		t.Fatalf("非法令牌应 401, got %d", w.Code)
	}
}

// TestMemberProfileAndMyOrders 会员卡与「我的订单」:只看到自己的单,匿名单不出现,openid 不外发。
func TestMemberProfileAndMyOrders(t *testing.T) {
	memberInitDB(t)
	r := memberRouter()
	_, token, anonOrderNo := memberSeed(t)

	w := memberGet(r, "/api/customer/auth/profile", token)
	if w.Code != http.StatusOK {
		t.Fatalf("会员卡查询应成功, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "微信用户") {
		t.Fatalf("未填昵称应显示「微信用户」, got %s", w.Body.String())
	}
	// openid 是站内标识(现在存在 tb_customer_identity.external_id),绝不能出现在响应里。
	if strings.Contains(w.Body.String(), "openid-handler") {
		t.Fatal("openid 是站内标识,绝不能出现在接口响应里")
	}

	w = memberGet(r, "/api/customer/order/mine?pageNum=1&pageSize=10", token)
	if w.Code != http.StatusOK {
		t.Fatalf("我的订单应成功, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M2026100800001") {
		t.Fatalf("应包含会员自己的订单, got %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), anonOrderNo) {
		t.Fatalf("匿名单不应出现在会员的订单列表里: %s", w.Body.String())
	}
	// 金额单位必须是「元」:种子单 total_amount=2800(分)= ¥28.00。
	// 曾经直接下发分,小程序再按元格式化 → 显示 ¥2800.00,且与订单页的 ¥28.00 打架。
	if !strings.Contains(w.Body.String(), `"totalAmount":28`) {
		t.Fatalf("金额应以元下发(¥28.00), got %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "2800") {
		t.Fatalf("金额不应以分下发(¥28.00 的单不能出现 2800), got %s", w.Body.String())
	}
}

// TestMemberProfileDisabledMember 停用会员的令牌立即失效。
func TestMemberProfileDisabledMember(t *testing.T) {
	memberInitDB(t)
	r := memberRouter()
	cust, token, _ := memberSeed(t)

	if err := dao.SetCustomerStatus(cust.CustomerID, po.CustomerStatusDisabled); err != nil {
		t.Fatalf("停用会员失败: %v", err)
	}
	w := memberGet(r, "/api/customer/auth/profile", token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("停用会员应 401, got %d", w.Code)
	}
}

// TestMemberLoginParameterValidation 登录接口的参数与开关校验。
func TestMemberLoginParameterValidation(t *testing.T) {
	memberInitDB(t)
	r := memberRouter()

	// 缺 code。
	if w := memberPost(r, "/api/customer/auth/login", "", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("缺 code 应 400, got %d", w.Code)
	}
	// 功能未开启(AppSecret 未配置)时返回业务提示,而不是当成故障炸给用户。
	w := memberPost(r, "/api/customer/auth/login", "", `{"code":"any-code"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未配置时应 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "会员功能未开启") {
		t.Fatalf("应返回「会员功能未开启」, got %s", w.Body.String())
	}
}

// TestMemberLoginUnsupportedProvider 已登记但未实现的登录方式必须明确拒绝,
// 不能悄悄退化成小程序登录,也不能 500。
func TestMemberLoginUnsupportedProvider(t *testing.T) {
	memberInitDB(t)
	r := memberRouter()
	// 本测试只考察 provider 分派:会员总闸(出厂默认关)要先打开,
	// 否则请求在闸口就被拒,到不了分派逻辑。
	if err := dao.SetSetting("member_enabled", "1"); err != nil {
		t.Fatal(err)
	}

	for _, provider := range []string{"phone", "password", "apple", "wechat"} {
		w := memberPost(r, "/api/customer/auth/login", "", `{"provider":"`+provider+`","code":"c"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("provider=%s 应 400, got %d body=%s", provider, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "暂不支持该登录方式") {
			t.Fatalf("provider=%s 应提示「暂不支持该登录方式」, got %s", provider, w.Body.String())
		}
	}
}
