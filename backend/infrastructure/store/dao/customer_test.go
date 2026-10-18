package dao

import (
	"database/sql"
	"path/filepath"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

func custDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "customer.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// TestCustomerInsertAndGet 建档 + 按主键/按登录方式查询 + 不存在返回 (nil,nil)。
func TestCustomerInsertAndGet(t *testing.T) {
	custDaoInitDB(t)

	id, err := InsertCustomer(po.Customer{
		OpenID:    "openid-A",
		Nickname:  "小红",
		AvatarURL: "http://a.png",
		Remark:    "vip",
	})
	if err != nil {
		t.Fatalf("InsertCustomer 失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("应返回自增 ID, got %d", id)
	}

	byID, err := GetCustomerByID(int(id))
	if err != nil || byID == nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if byID.OpenID != "openid-A" || byID.Nickname != "小红" || byID.Remark != "vip" {
		t.Fatalf("字段不符: %+v", byID)
	}
	// 默认值:status 应为启用、order_count 应为 0。
	if byID.Status != po.CustomerStatusEnabled {
		t.Fatalf("默认 status 应为启用, got %d", byID.Status)
	}
	if byID.OrderCount != 0 {
		t.Fatalf("默认 order_count 应为 0, got %d", byID.OrderCount)
	}

	// 不存在的记录:返回 (nil, nil),不报错(登录流程据此判定 isNew)。
	if none, err := GetCustomerByIdentity(po.CustomerProviderWxMP, "no-such-openid"); err != nil || none != nil {
		t.Fatalf("查不存在的身份应返回 (nil,nil), got %v err=%v", none, err)
	}
	if none, err := GetCustomerByID(999999); err != nil || none != nil {
		t.Fatalf("查不存在的 ID 应返回 (nil,nil), got %v err=%v", none, err)
	}
}

// TestCreateCustomerWithIdentity 建档与绑定在同一个事务里,且按身份可查回同一份档案。
func TestCreateCustomerWithIdentity(t *testing.T) {
	custDaoInitDB(t)

	id, err := CreateCustomerWithIdentity(po.CustomerProviderWxMP, "openid-B", "")
	if err != nil {
		t.Fatalf("建档失败: %v", err)
	}
	c, err := GetCustomerByIdentity(po.CustomerProviderWxMP, "openid-B")
	if err != nil || c == nil {
		t.Fatalf("按身份查档失败: %v", err)
	}
	if c.CustomerID != id {
		t.Fatalf("身份指向的档案不符: want %d got %d", id, c.CustomerID)
	}

	// 同一个 key 重复绑定必须失败(唯一索引),且失败后不留孤儿档案 ——
	// 并发首登靠的就是这个语义。
	before := customerCount(t)
	if _, err := CreateCustomerWithIdentity(po.CustomerProviderWxMP, "openid-B", ""); err == nil {
		t.Fatal("同一身份重复建档应被唯一索引拒绝")
	}
	if after := customerCount(t); after != before {
		t.Fatalf("冲突后档案必须整体回滚, 期望 %d 行, got %d", before, after)
	}
}

// TestCustomerIdentityUnique (provider, external_id) 是登录凭据的唯一约束。
//
// 注意:这里直接插 tb_customer_identity,而不走 CreateCustomerWithIdentity ——
// P0 过渡期 tb_customer.openid 仍是 NOT NULL + 唯一索引,非微信 provider 建档
// 会先撞 openid 的唯一约束(这正是 P2 必须放开可空的原因,见 spec §7.2)。
// 身份表本身的约束与那里无关,单独验。
func TestCustomerIdentityUnique(t *testing.T) {
	custDaoInitDB(t)

	id, err := CreateCustomerWithIdentity(po.CustomerProviderWxMP, "openid-D", "")
	if err != nil {
		t.Fatal(err)
	}
	// 同一 provider 换个 external_id:允许(一个会员可以绑多种凭据,一个凭据只能绑一个会员)。
	if err := InsertIdentityTxForTest(id, po.CustomerProviderWxOA, "openid-D-oa"); err != nil {
		t.Fatalf("不同 external_id 应允许: %v", err)
	}
	// 完全相同的 (provider, external_id):必须被唯一索引拒绝。
	if err := InsertIdentityTxForTest(id, po.CustomerProviderWxOA, "openid-D-oa"); err == nil {
		t.Fatal("同一 (provider, external_id) 重复绑定应被拒绝")
	}
}

// TestCustomerIdentityUnionID 未绑定开放平台时 unionid 存 NULL 而不是空串;
// 同一个人的多个 openid 可共享同一个 unionid(索引非唯一)。
func TestCustomerIdentityUnionID(t *testing.T) {
	custDaoInitDB(t)

	id, err := CreateCustomerWithIdentity(po.CustomerProviderWxMP, "openid-C1", "")
	if err != nil {
		t.Fatal(err)
	}
	var uid *string
	if err := store.DB.QueryRow(`SELECT unionid FROM tb_customer_identity WHERE external_id=?`, "openid-C1").Scan(&uid); err != nil {
		t.Fatalf("读取 unionid 失败: %v", err)
	}
	if uid != nil {
		t.Fatalf("未绑定开放平台时 unionid 应为 NULL, got %q", *uid)
	}

	// 两个不同 openid 共享同一个 unionid:不能撞唯一约束,否则同一人接第二端就失败。
	for _, ext := range []string{"openid-C2", "openid-C3"} {
		if err := InsertIdentityTxForTest(id, po.CustomerProviderWxApp, ext, "union-1"); err != nil {
			t.Fatalf("共享 unionid 应允许(%s): %v", ext, err)
		}
	}
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_customer_identity WHERE unionid='union-1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("应有 2 条共享 unionid 的身份, got %d", n)
	}
}

// InsertIdentityTxForTest 测试辅助:单条绑定(不建档案)。
func InsertIdentityTxForTest(customerID int, provider, externalID string, unionids ...string) error {
	unionid := ""
	if len(unionids) > 0 {
		unionid = unionids[0]
	}
	return store.WithTx(func(tx *sql.Tx) error {
		return InsertIdentityTx(tx, customerID, provider, externalID, unionid)
	})
}

// customerCount 当前会员档案总数(并发/回滚类断言用)。
func customerCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_customer`).Scan(&n); err != nil {
		t.Fatalf("统计会员失败: %v", err)
	}
	return n
}

// TestCustomerTimestampColumns 通用记录时间的填写口径:
// create_time 仅 INSERT 时写一次,update_time 仅 UPDATE 时推进 —— INSERT 时为 NULL。
// 「从未被改过」与「刚创建」因此可区分,插入时把两者填成同一时刻就丢掉了这份信息。
func TestCustomerTimestampColumns(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-T"})
	if err != nil {
		t.Fatal(err)
	}
	var ct, ut *string
	if err := store.DB.QueryRow(`SELECT create_time, update_time FROM tb_customer WHERE customer_id=?`, id).Scan(&ct, &ut); err != nil {
		t.Fatalf("读取时间列失败: %v", err)
	}
	if ct == nil || *ct == "" {
		t.Fatal("INSERT 应写 create_time")
	}
	if ut != nil {
		t.Fatalf("INSERT 不应写 update_time, got %q", *ut)
	}

	// UPDATE 后:update_time 被推进,create_time 必须原地不动。
	if err := UpdateCustomerProfile(int(id), "改名", "http://x.png"); err != nil {
		t.Fatal(err)
	}
	var ct2, ut2 *string
	if err := store.DB.QueryRow(`SELECT create_time, update_time FROM tb_customer WHERE customer_id=?`, id).Scan(&ct2, &ut2); err != nil {
		t.Fatalf("读取时间列失败: %v", err)
	}
	if ct2 == nil || *ct2 != *ct {
		t.Fatalf("UPDATE 不得改动 create_time, got %v(原 %v)", ct2, *ct)
	}
	if ut2 == nil || *ut2 == "" {
		t.Fatal("UPDATE 应推进 update_time")
	}
}

// TestCustomerUpdateProfile 完善资料(昵称/头像)生效。
func TestCustomerUpdateProfile(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-U"})
	if err != nil {
		t.Fatal(err)
	}

	if err := UpdateCustomerProfile(int(id), "新昵称", "http://new.png"); err != nil {
		t.Fatalf("UpdateCustomerProfile 失败: %v", err)
	}
	c, err := GetCustomerByID(int(id))
	if err != nil || c == nil {
		t.Fatalf("查询失败: %v", err)
	}
	if c.Nickname != "新昵称" || c.AvatarURL != "http://new.png" {
		t.Fatalf("资料未更新: %+v", c)
	}
}

// TestCustomerSetStatus 停用后立即生效(status 翻转)。
func TestCustomerSetStatus(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-S"})
	if err != nil {
		t.Fatal(err)
	}

	if err := SetCustomerStatus(int(id), po.CustomerStatusDisabled); err != nil {
		t.Fatalf("SetCustomerStatus 失败: %v", err)
	}
	c, err := GetCustomerByID(int(id))
	if err != nil || c == nil || c.Status != po.CustomerStatusDisabled {
		t.Fatalf("状态未翻转: %+v err=%v", c, err)
	}
}

// TestCustomerTouchLogin 记录登录时间。
func TestCustomerTouchLogin(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-L"})
	if err != nil {
		t.Fatal(err)
	}

	// 建档时 last_login_time 已由 InsertCustomer 写入(非空),TouchCustomerLogin 应刷新它。
	c0, _ := GetCustomerByID(int(id))
	if c0.LastLoginTime == "" {
		t.Fatal("建档后 last_login_time 应已写入")
	}
	TouchCustomerLogin(int(id))
	c1, _ := GetCustomerByID(int(id))
	if c1.LastLoginTime == "" {
		t.Fatal("TouchCustomerLogin 后应记录登录时间")
	}
}

// TestCustomerTouchOrder 下单成功后累计订单数 +1。
func TestCustomerTouchOrder(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-O"})
	if err != nil {
		t.Fatal(err)
	}

	TouchCustomerOrder(int(id))
	TouchCustomerOrder(int(id))
	c, _ := GetCustomerByID(int(id))
	if c.OrderCount != 2 {
		t.Fatalf("下单两次后 order_count 应为 2, got %d", c.OrderCount)
	}
	if c.LastOrderTime == "" {
		t.Fatal("TouchCustomerOrder 应刷新最近下单时间")
	}
}

// TestListOrdersByCustomer 会员「我的订单」分页与分→元换算;
// customerID<=0 防御性返回空。
func TestListOrdersByCustomer(t *testing.T) {
	custDaoInitDB(t)
	id, err := InsertCustomer(po.Customer{OpenID: "openid-M"})
	if err != nil {
		t.Fatal(err)
	}

	// 防御:非法会员 ID 直接返回空结果,不应报错。
	if total, rows, err := ListOrdersByCustomer(0, 1, 10); err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("customerID<=0 应返回空, got total=%d rows=%d err=%v", total, len(rows), err)
	}

	// 该会员暂无订单。
	if total, rows, err := ListOrdersByCustomer(int(id), 1, 10); err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("无订单应返回空, got total=%d rows=%d err=%v", total, len(rows), err)
	}

	// 插一条该会员的订单:库里存分,出参必须是元。
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count,
		order_status, dish_amount, seat_fee, discount_amount, total_amount, pay_status, customer_id, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"OC1", 1, "T1", "测试桌", 2, 2, 5000, 0, 0, 5000, 1, id, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}

	total, rows, err := ListOrdersByCustomer(int(id), 1, 10)
	if err != nil {
		t.Fatalf("查询会员订单失败: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("应返回 1 条订单, got total=%d rows=%d", total, len(rows))
	}
	r := rows[0]
	if r.OrderNo != "OC1" || r.TableName != "测试桌" {
		t.Fatalf("订单字段不符: %+v", r)
	}
	// 库里 5000 分 → 出参 50.00 元。
	if r.TotalAmount != po.ToYuan(5000) {
		t.Fatalf("金额应换算为元(50.00), got %v", r.TotalAmount)
	}

	// 分页越界返回空,不报错。
	if _, rows, err := ListOrdersByCustomer(int(id), 2, 10); err != nil || len(rows) != 0 {
		t.Fatalf("第二页应返回空, got rows=%d err=%v", len(rows), err)
	}
}
