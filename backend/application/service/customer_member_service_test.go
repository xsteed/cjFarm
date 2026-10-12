package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// craftCustomerToken 用真实签名密钥构造任意载荷的顾客令牌(测试过期场景用)。
func craftCustomerToken(t *testing.T, payload string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(infrastructure.LoadAuthKey()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// TestCustomerLoginNewThenReturn 一键注册/登录:首次建档 isNew=true,再次是「欢迎回来」。
func TestCustomerLoginNewThenReturn(t *testing.T) {
	customerSvcInit(t)

	first, err := customerLoginByOpenid(context.Background(), "openid-member-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("首次登录应成功: %v", err)
	}
	if !first.IsNew {
		t.Fatal("首次登录应标记为注册(isNew=true)")
	}
	if first.Customer.OpenID != "openid-member-1" {
		t.Fatalf("档案 openid 不符: %+v", first.Customer)
	}
	if first.Token == "" {
		t.Fatal("应签发顾客令牌")
	}
	if first.Customer.OrderCount != 0 {
		t.Fatalf("新会员订单数应为 0, got %d", first.Customer.OrderCount)
	}

	second, err := customerLoginByOpenid(context.Background(), "openid-member-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("再次登录应成功: %v", err)
	}
	if second.IsNew {
		t.Fatal("再次登录不应标记为注册")
	}
	if second.Customer.CustomerID != first.Customer.CustomerID {
		t.Fatalf("应是同一份档案: %d vs %d", second.Customer.CustomerID, first.Customer.CustomerID)
	}
}

// TestCustomerLoginDisabledMember 停用会员登录被拒。
func TestCustomerLoginDisabledMember(t *testing.T) {
	customerSvcInit(t)

	first, err := customerLoginByOpenid(context.Background(), "openid-disabled", "1.2.3.4")
	if err != nil {
		t.Fatalf("首次登录应成功: %v", err)
	}
	if err := dao.SetCustomerStatus(first.Customer.CustomerID, po.CustomerStatusDisabled); err != nil {
		t.Fatalf("停用会员失败: %v", err)
	}

	if _, err := customerLoginByOpenid(context.Background(), "openid-disabled", "1.2.3.4"); err == nil {
		t.Fatal("停用会员登录应被拒绝")
	}
}

// TestCustomerTokenLifecycle 顾客令牌:有效载荷可验、过期/篡改被拒、与管理端令牌互拒。
func TestCustomerTokenLifecycle(t *testing.T) {
	customerSvcInit(t)

	tok := GenCustomerToken(42)
	id, ok := VerifyCustomerToken(tok)
	if !ok || id != 42 {
		t.Fatalf("有效令牌应能验出会员 ID, got id=%d ok=%v", id, ok)
	}

	// 过期:直接构造一个 exp 在过去的载荷。
	if _, ok := VerifyCustomerToken(craftCustomerToken(t, "customer|42|1")); ok {
		t.Fatal("过期令牌应被拒")
	}
	// 篡改签名(截断/追加都会让 HMAC 对不上)。
	if _, ok := VerifyCustomerToken(strings.Replace(GenCustomerToken(42), ".", ".x", 1)); ok {
		t.Fatal("篡改签名的令牌应被拒")
	}
	if _, ok := VerifyCustomerToken("not-a-token"); ok {
		t.Fatal("非法令牌应被拒")
	}

	// 与管理端令牌互拒:载荷形态不同,谁也不能冒充谁。
	adminTok := GenToken("admin", 1, 1)
	if _, ok := VerifyCustomerToken(adminTok); ok {
		t.Fatal("管理端令牌不应通过顾客令牌校验")
	}
	if _, ok := VerifyToken(GenCustomerToken(42)); ok {
		t.Fatal("顾客令牌不应通过管理端令牌校验")
	}
}

// TestCustomerLoginRateLimit 同 IP 限流:超过每分钟上限后被拦,换 IP 不受影响。
func TestCustomerLoginRateLimit(t *testing.T) {
	customerSvcInit(t)

	const ip = "9.9.9.9"
	for i := 0; i < customerLoginMaxPerMin; i++ {
		if err := allowCustomerLogin(ip); err != nil {
			t.Fatalf("第 %d 次不应被限流: %v", i+1, err)
		}
	}
	if err := allowCustomerLogin(ip); err == nil {
		t.Fatal("超过每分钟上限应被限流")
	}
	if err := allowCustomerLogin("8.8.8.8"); err != nil {
		t.Fatalf("其它 IP 不应受影响: %v", err)
	}
}

// TestCustomerCreateOrderAttribution 下单归属:会员单写 customer_id,匿名单为 0;
// 匿名链路(不登录)行为与从前完全一致。
func TestCustomerCreateOrderAttribution(t *testing.T) {
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)

	// 匿名单(会员功能上线前的既有路径,必须保持 customer_id=0)。
	anon, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("匿名下单应成功: %v", err)
	}
	var anonOwner int
	if err := store.DB.QueryRow(`SELECT customer_id FROM tb_order WHERE order_id=?`, anon.OrderID).Scan(&anonOwner); err != nil {
		t.Fatalf("读取匿名订单归属失败: %v", err)
	}
	if anonOwner != 0 {
		t.Fatalf("匿名订单归属应为 0, got %d", anonOwner)
	}

	// 会员单:归属写入,并维护会员卡展示列。
	member := customerLoginByOpenidForTest(t, "openid-owner")
	memberOrder, err := CustomerCreateOrder(member.Customer.CustomerID, 2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("会员下单应成功: %v", err)
	}
	var owner, count int
	var lastOrder string
	if err := store.DB.QueryRow(`SELECT customer_id FROM tb_order WHERE order_id=?`, memberOrder.OrderID).Scan(&owner); err != nil {
		t.Fatalf("读取会员订单归属失败: %v", err)
	}
	if owner != member.Customer.CustomerID {
		t.Fatalf("会员订单归属应为 %d, got %d", member.Customer.CustomerID, owner)
	}
	if err := store.DB.QueryRow(`SELECT order_count, COALESCE(last_order_time,'') FROM tb_customer WHERE customer_id=?`,
		member.Customer.CustomerID).Scan(&count, &lastOrder); err != nil {
		t.Fatalf("读取会员展示列失败: %v", err)
	}
	if count != 1 || lastOrder == "" {
		t.Fatalf("下单后应维护会员展示列, got count=%d lastOrder=%q", count, lastOrder)
	}

	// 「我的订单」只看到自己的单:跨桌、且不含匿名单。
	total, rows, err := CustomerMyOrders(member.Customer.CustomerID, 1, 10)
	if err != nil {
		t.Fatalf("我的订单查询失败: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].OrderNo != memberOrder.OrderNo {
		t.Fatalf("我的订单应只含会员自己的 1 单, got total=%d rows=%+v", total, rows)
	}

	// 加菜不改归属,也不重复计数。
	if _, err := CustomerAppendOrder(2, memberOrder.OrderNo, []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}}, 2, "append-m-1"); err != nil {
		t.Fatalf("会员加菜应成功: %v", err)
	}
	if err := store.DB.QueryRow(`SELECT order_count FROM tb_customer WHERE customer_id=?`, member.Customer.CustomerID).Scan(&count); err != nil {
		t.Fatalf("读取会员订单计数失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("加菜属于同一单,不应重复计数, got %d", count)
	}
}

// customerLoginByOpenidForTest 测试辅助:注册一个会员并返回登录结果。
func customerLoginByOpenidForTest(t *testing.T, openid string) CustomerLoginResult {
	t.Helper()
	res, err := customerLoginByOpenid(context.Background(), openid, "127.0.0.1")
	if err != nil {
		t.Fatalf("注册会员失败: %v", err)
	}
	return res
}
