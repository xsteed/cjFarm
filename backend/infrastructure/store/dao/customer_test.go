package dao

import (
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

// TestCustomerInsertAndGet 建档 + 按主键/按 openid 查询 + 不存在返回 (nil,nil)。
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

	byOpenid, err := GetCustomerByOpenid("openid-A")
	if err != nil || byOpenid == nil {
		t.Fatalf("按 openid 查询失败: %v", err)
	}
	if byOpenid.CustomerID != int(id) {
		t.Fatalf("openid 查询结果 ID 不符: %d", byOpenid.CustomerID)
	}

	// 不存在的记录:返回 (nil, nil),不报错(登录流程据此判定 isNew)。
	if none, err := GetCustomerByOpenid("no-such-openid"); err != nil || none != nil {
		t.Fatalf("查不存在的 openid 应返回 (nil,nil), got %v err=%v", none, err)
	}
	if none, err := GetCustomerByID(999999); err != nil || none != nil {
		t.Fatalf("查不存在的 ID 应返回 (nil,nil), got %v err=%v", none, err)
	}
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
