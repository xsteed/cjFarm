package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// reviewOrder 造一笔「已付款且已上齐」的订单,返回订单号与桌台 ID(评价的合法前置态)。
func reviewOrder(t *testing.T) (orderNo string, tableID, orderID int) {
	t.Helper()
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	res, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	// 收款(置已支付)→ 依次推到「已上齐」:状态机不允许 1→3 跳级(orderTransitions)。
	if _, err := PayOrder(int(res.OrderID), "cash", "test"); err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res.OrderID), OrderStatusCooking, "", "test"); err != nil {
		t.Fatalf("置制作中失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res.OrderID), OrderStatusDining, "", "test"); err != nil {
		t.Fatalf("置已上齐失败: %v", err)
	}
	return res.OrderNo, 1, int(res.OrderID)
}

// TestReviewCreateAnonymous 匿名顾客提交评价:落库快照、身份为匿名、IP 只存哈希。
func TestReviewCreateAnonymous(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)

	st, err := CreateCustomerReview(0, tableID, orderNo, 2,
		[]string{"上菜慢", "味道一般"}, "等了半小时才上菜", nil, "dev-key", "203.0.113.7", "TestUA/1.0")
	if err != nil {
		t.Fatalf("匿名评价提交失败: %v", err)
	}
	if !st.Reviewed || st.Rating != 2 {
		t.Fatalf("返回状态不符: %+v", st)
	}
	r, err := dao.GetReviewByKey(orderNo, "dev-key")
	if err != nil || r.ReviewID == 0 {
		t.Fatalf("评价未落库: %v", err)
	}
	if r.CustomerID != 0 || r.Nickname != "" || r.Phone != "" {
		t.Fatalf("匿名评价不应有会员信息: %+v", r)
	}
	if r.IpHash == "" || strings.Contains(r.IpHash, "203") {
		t.Fatalf("IP 应存哈希且不含明文: %q", r.IpHash)
	}
	if r.TableNo == "" || r.PersonCount != 2 {
		t.Fatalf("桌台/人数快照缺失: %+v", r)
	}
	if r.BizDate == "" {
		t.Fatalf("营业日快照缺失: %+v", r)
	}
}

// TestReviewCreateMemberSnapshot 会员提交:昵称/手机号快照一次性写死,
// 之后改昵称/解绑手机都不影响已落库的评价(追溯靠快照,不靠联表)。
func TestReviewCreateMemberSnapshot(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	id := customerLoginByOpenidForTest(t, "review-member").Customer.CustomerID
	// 绑一条手机号身份(与 getPhoneNumber 绑定同一存储形态)。
	if _, err := store.DB.Exec(`INSERT INTO tb_customer_identity(customer_id, provider, external_id, credential, create_time)
		VALUES(?, 'phone', '13800001234', '', ?)`, id, store.Now()); err != nil {
		t.Fatalf("插手机号身份失败: %v", err)
	}
	if _, err := store.DB.Exec(`UPDATE tb_customer SET nickname='张顾客' WHERE customer_id=?`, id); err != nil {
		t.Fatalf("设昵称失败: %v", err)
	}

	if _, err := CreateCustomerReview(id, tableID, orderNo, 5, []string{"味道好"}, "", nil, "ignored-key", "192.0.2.1", "UA"); err != nil {
		t.Fatalf("会员评价提交失败: %v", err)
	}
	// 会员键由服务端派生 m:{id}(入参被忽略——防冒充他人占位)。
	r, err := dao.GetReviewByKey(orderNo, fmt.Sprintf("m:%d", id))
	if err != nil {
		t.Fatalf("查评价失败: %v", err)
	}
	if r.CustomerID != id || r.Nickname != "张顾客" || r.Phone != "13800001234" {
		t.Fatalf("快照不符: %+v", r)
	}
}

// TestReviewValidationChain 校验链:未付不能评、未上齐不能评、归属不符不能评、
// 星级越界拒绝、超长文字截断。
func TestReviewValidationChain(t *testing.T) {
	// 未付款订单。
	customerSvcInit(t)
	dishID, specID := customerSvcSpecID(t)
	res, err := CustomerCreateOrder(0, 1, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := CreateCustomerReview(0, 1, res.OrderNo, 5, nil, "", nil, "dev-key", "", ""); err == nil || !strings.Contains(err.Error(), "支付") {
		t.Fatalf("未付款订单应被拒绝, got %v", err)
	}
	// 已付款但未上齐。
	if _, err := PayOrder(int(res.OrderID), "cash", "test"); err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if _, err := CreateCustomerReview(0, 1, res.OrderNo, 5, nil, "", nil, "dev-key", "", ""); err == nil {
		t.Fatal("未上齐订单应被拒绝")
	}
	if err := ChangeOrderStatus(int(res.OrderID), OrderStatusCooking, "", "test"); err != nil {
		t.Fatalf("置制作中失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res.OrderID), OrderStatusDining, "", "test"); err != nil {
		t.Fatalf("置已上齐失败: %v", err)
	}
	// 桌台归属不符(单号可猜测,归属校验是防遍历的关键)。
	if _, err := CreateCustomerReview(0, 999, res.OrderNo, 5, nil, "", nil, "dev-key", "", ""); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("归属不符应被拒绝, got %v", err)
	}
	// 星级越界。
	if _, err := CreateCustomerReview(0, 1, res.OrderNo, 0, nil, "", nil, "dev-key", "", ""); err == nil {
		t.Fatal("rating=0 应被拒绝")
	}
	if _, err := CreateCustomerReview(0, 1, res.OrderNo, 6, nil, "", nil, "dev-key", "", ""); err == nil {
		t.Fatal("rating=6 应被拒绝")
	}
	// 文字超长截断 + 标签清洗(逗号替换/去重/限量)。
	long := strings.Repeat("好", 300)
	tags := []string{"味道好", "味道好", "a,b", strings.Repeat("长", 20), "份量足", "上菜快", "环境好", "性价比高", "第九个"}
	st, err := CreateCustomerReview(0, 1, res.OrderNo, 5, tags, long, nil, "dev-key", "", "")
	if err != nil {
		t.Fatalf("合法提交失败: %v", err)
	}
	if len([]rune(st.Content)) != 200 {
		t.Fatalf("文字应截断到 200 字, got %d", len([]rune(st.Content)))
	}
	if len(st.Tags) != 6 {
		t.Fatalf("标签应限量 6 个, got %v", st.Tags)
	}
	if st.Tags[1] != "a b" {
		t.Fatalf("标签内逗号应替换为空格(去重后 index=1), got %v", st.Tags)
	}
}

// TestReviewOncePerOrder 一单一评:重复提交返回 ErrReviewAlready(预检),
// 并发双击撞唯一索引后也转成同一语义(模拟:清掉预检查不到的路径直接再插)。
func TestReviewOncePerOrder(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	if _, err := CreateCustomerReview(0, tableID, orderNo, 4, nil, "", nil, "dev-key", "", ""); err != nil {
		t.Fatalf("首次提交失败: %v", err)
	}
	if _, err := CreateCustomerReview(0, tableID, orderNo, 5, nil, "", nil, "dev-key", "", ""); !errors.Is(err, ErrReviewAlready) {
		t.Fatalf("重复提交应返回 ErrReviewAlready, got %v", err)
	}
	// 查询状态接口同样返回已评。
	st, err := CustomerReviewStatus(0, tableID, orderNo, "dev-key")
	if err != nil || !st.Reviewed || st.Rating != 4 {
		t.Fatalf("状态查询不符: %+v err=%v", st, err)
	}
}

// TestReviewStatusOrderMissing 查询/提交对不存在订单与归属不符单的拒绝。
func TestReviewStatusOrderMissing(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	if _, err := CustomerReviewStatus(0, tableID, "no-such-order", "dev-key"); err == nil {
		t.Fatal("不存在订单应报错")
	}
	if _, err := CustomerReviewStatus(0, 999, orderNo, "dev-key"); err == nil {
		t.Fatal("归属不符应报错")
	}
}

// TestAdminReviewListSummary 管理端列表与汇总:同口径筛选、顾客三态标注、删除。
func TestAdminReviewListSummary(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	id := customerLoginByOpenidForTest(t, "review-admin").Customer.CustomerID
	if _, err := CreateCustomerReview(id, tableID, orderNo, 1, []string{"上菜慢"}, "太慢了", nil, "dev-key", "192.0.2.9", "UA"); err != nil {
		t.Fatalf("会员评价失败: %v", err)
	}
	// 再来一笔匿名的(换一桌下单,避免一单一评约束)。
	dishID, specID := customerSvcSpecID(t)
	res2, err := CustomerCreateOrder(0, 2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("第二单失败: %v", err)
	}
	if _, err := PayOrder(int(res2.OrderID), "cash", "test"); err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res2.OrderID), OrderStatusCooking, "", "test"); err != nil {
		t.Fatalf("置制作中失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res2.OrderID), OrderStatusDining, "", "test"); err != nil {
		t.Fatalf("置状态失败: %v", err)
	}
	if _, err := CreateCustomerReview(0, 2, res2.OrderNo, 5, []string{"味道好"}, "", nil, "dev-key", "", ""); err != nil {
		t.Fatalf("匿名评价失败: %v", err)
	}

	res, err := AdminReviewList(dao.ReviewQuery{}, 1, 20)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("应有 2 条评价, got total=%d items=%d", res.Total, len(res.Items))
	}
	if res.Summary.Total != 2 || res.Summary.AvgScore != 3 {
		t.Fatalf("汇总不符: %+v", res.Summary)
	}
	if res.Summary.BadCount != 1 || res.Summary.BadRate != 0.5 {
		t.Fatalf("差评口径不符: %+v", res.Summary)
	}
	// 顾客三态:会员在册=member,匿名=anonymous。
	byType := map[string]int{}
	for _, it := range res.Items {
		byType[it.CustomerType]++
	}
	if byType[dto.ReviewCustomerMember] != 1 || byType[dto.ReviewCustomerAnonymous] != 1 {
		t.Fatalf("顾客三态标注不符: %v", byType)
	}
	// 筛选:仅差评。
	resBad, err := AdminReviewList(dao.ReviewQuery{RatingMax: 2}, 1, 20)
	if err != nil || resBad.Total != 1 || resBad.Items[0].OrderNo != orderNo {
		t.Fatalf("差评筛选不符: total=%d err=%v", resBad.Total, err)
	}
	// 删除 + 删除后列表回零(该单的提交者是会员 id,键为 m:{id})。
	if _, err := dao.GetReviewByKey(orderNo, fmt.Sprintf("m:%d", id)); err != nil {
		t.Fatalf("评价应存在: %v", err)
	}
	r, _ := dao.GetReviewByKey(orderNo, fmt.Sprintf("m:%d", id))
	if _, err := AdminDeleteReview(r.ReviewID, "tester"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	resAfter, err := AdminReviewList(dao.ReviewQuery{}, 1, 20)
	if err != nil || resAfter.Total != 1 {
		t.Fatalf("删除后应剩 1 条, got %d err=%v", resAfter.Total, err)
	}
	if _, err := AdminDeleteReview(r.ReviewID, "tester"); err == nil {
		t.Fatal("重复删除应报「评价不存在」")
	}
}

// TestReviewImageLifecycle 评价图片生命周期:上传(独立表)→ 提交绑定 →
// 管理端列表带图 → 删除级联清图;GetImage 两级回落先查评价表。
func TestReviewImageLifecycle(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	// 上传两张图(PNG 最小图,过魔数校验)。
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	u1, err := SaveCustomerReviewImage("a.png", png)
	if err != nil {
		t.Fatalf("上传图1失败: %v", err)
	}
	u2, err := SaveCustomerReviewImage("b.png", png)
	if err != nil {
		t.Fatalf("上传图2失败: %v", err)
	}
	n1 := strings.TrimPrefix(u1, store.UploadURLPrefix)
	n2 := strings.TrimPrefix(u2, store.UploadURLPrefix)
	// 落在独立表且未绑定。
	if _, ct, ok := dao.GetReviewImage(n1); !ok || ct == "" {
		t.Fatalf("图1应在独立表: ok=%v ct=%q", ok, ct)
	}
	if r, _ := dao.GetReviewByKey(orderNo, "dev-key"); r.ReviewID != 0 {
		t.Fatal("前提不成立: 提交前不应已有评价")
	}
	// 非图片内容被拒。
	if _, err := SaveCustomerReviewImage("c.png", []byte("<script>alert(1)</script>")); err == nil {
		t.Fatal("伪装图片应被魔数校验拒绝")
	}
	// 提交评价并绑定两张图。
	st, err := CreateCustomerReview(0, tableID, orderNo, 4, []string{"味道好"}, "带图评价", []string{n1, n2}, "dev-key", "192.0.2.5", "UA")
	if err != nil {
		t.Fatalf("带图评价提交失败: %v", err)
	}
	if !st.Reviewed {
		t.Fatal("应提交成功")
	}
	// GetImage 两级回落:评价图名可读出(未命中 tb_image)。
	if d, _, ok := GetImage(n1); !ok || len(d) != len(png) {
		t.Fatal("GetImage 应回落读到评价图")
	}
	// 管理端列表带图。
	res, err := AdminReviewList(dao.ReviewQuery{}, 1, 10)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	var found *dto.Review
	for i := range res.Items {
		if res.Items[i].OrderNo == orderNo {
			found = &res.Items[i]
		}
	}
	if found == nil || len(found.Images) != 2 {
		t.Fatalf("列表应带 2 张图: %+v", found)
	}
	// 同桌另一位顾客(dev-key2)仍可评价;但其引用的图名 n1 已被第一位绑定,
	// 绑定数不足整体回滚 —— 「先传后评」的图不能引用他人已占用的行。
	if _, err := CreateCustomerReview(0, tableID, orderNo, 5, nil, "x", []string{n1}, "dev-key2", "", ""); err == nil || !strings.Contains(err.Error(), "已失效") {
		t.Fatalf("引用他人已绑定图应报已失效, got %v", err)
	}
	// 删除评价 → 图片级联清理。
	r, err := dao.GetReviewByKey(orderNo, "dev-key")
	if err != nil || r.ReviewID == 0 {
		t.Fatalf("首位顾客的评价应存在: %+v err=%v", r, err)
	}
	if _, err := AdminDeleteReview(r.ReviewID, "tester"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, _, ok := dao.GetReviewImage(n1); ok {
		t.Fatal("删除评价后图片应级联清理")
	}
}

// TestReviewImageOrphanClean 孤儿清理:review_id=0 且超过 cutoff 的行被回收。
func TestReviewImageOrphanClean(t *testing.T) {
	customerSvcInit(t)
	// 手工插一条两天前的孤儿图。
	old := time.Now().Add(-48 * time.Hour).Format(conf.TimeLayout)
	if _, err := store.DB.Exec(`INSERT INTO tb_review_image(img_name, content_type, file_size, create_time)
		VALUES('review_orphan.png','image/png',1,?)`, old); err != nil {
		t.Fatalf("插孤儿图失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_review_image(img_name, content_type, file_size, create_time)
		VALUES('review_fresh.png','image/png',1,?)`, store.Now()); err != nil {
		t.Fatalf("插新图失败: %v", err)
	}
	n, err := dao.CleanOrphanReviewImages(time.Now().Add(-24 * time.Hour).Format(conf.TimeLayout))
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应清理 1 张, got %d", n)
	}
	if _, _, ok := dao.GetReviewImage("review_orphan.png"); ok {
		t.Fatal("过期孤儿应被删除")
	}
	if _, _, ok := dao.GetReviewImage("review_fresh.png"); !ok {
		t.Fatal("未到期的图不应被删")
	}
}

// TestReviewImageFilter 有图/无图筛选:与列表同一 cond,口径一致。
func TestReviewImageFilter(t *testing.T) {
	// 一笔带图(t1) + 一笔无图(t2)。
	orderNo, tableID, _ := reviewOrder(t)
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	u, err := SaveCustomerReviewImage("f.png", png)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	n := strings.TrimPrefix(u, store.UploadURLPrefix)
	if _, err := CreateCustomerReview(0, tableID, orderNo, 5, nil, "带图", []string{n}, "dev-key", "", ""); err != nil {
		t.Fatalf("带图评价失败: %v", err)
	}
	dishID, specID := customerSvcSpecID(t)
	res2, err := CustomerCreateOrder(0, 2, 2, "", []dto.OrderItem{{DishID: dishID, SpecID: specID, Quantity: 1}})
	if err != nil {
		t.Fatalf("第二单失败: %v", err)
	}
	if _, err := PayOrder(int(res2.OrderID), "cash", "test"); err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if err := ChangeOrderStatus(int(res2.OrderID), OrderStatusCooking, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := ChangeOrderStatus(int(res2.OrderID), OrderStatusDining, "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCustomerReview(0, 2, res2.OrderNo, 3, nil, "无图", nil, "dev-key", "", ""); err != nil {
		t.Fatalf("无图评价失败: %v", err)
	}

	with, err := AdminReviewList(dao.ReviewQuery{HasImages: true}, 1, 10)
	if err != nil || with.Total != 1 || with.Items[0].OrderNo != orderNo {
		t.Fatalf("有图筛选不符: total=%d err=%v", with.Total, err)
	}
	without, err := AdminReviewList(dao.ReviewQuery{HasNoImages: true}, 1, 10)
	if err != nil || without.Total != 1 || without.Items[0].OrderNo != res2.OrderNo {
		t.Fatalf("无图筛选不符: total=%d err=%v", without.Total, err)
	}
	all, err := AdminReviewList(dao.ReviewQuery{}, 1, 10)
	if err != nil || all.Total != 2 {
		t.Fatalf("全部应 2 条: total=%d err=%v", all.Total, err)
	}
}

// TestReviewPerReviewer 一人一评:同桌两位匿名顾客各自评价互不影响,
// 同一人(同键)重复提交被拒;「已评」查询按键隔离。
func TestReviewPerReviewer(t *testing.T) {
	orderNo, tableID, _ := reviewOrder(t)
	// 顾客 A(device-a)提交。
	if _, err := CreateCustomerReview(0, tableID, orderNo, 5, nil, "A 的评价", nil, "device-a", "", ""); err != nil {
		t.Fatalf("A 提交失败: %v", err)
	}
	// A 查询:已评;B 查询:未评。
	if st, err := CustomerReviewStatus(0, tableID, orderNo, "device-a"); err != nil || !st.Reviewed {
		t.Fatalf("A 应显示已评: %+v err=%v", st, err)
	}
	if st, err := CustomerReviewStatus(0, tableID, orderNo, "device-b"); err != nil || st.Reviewed {
		t.Fatalf("B 应显示未评: %+v err=%v", st, err)
	}
	// B(device-b)仍可提交同一订单。
	if _, err := CreateCustomerReview(0, tableID, orderNo, 2, nil, "B 的评价", nil, "device-b", "", ""); err != nil {
		t.Fatalf("B 提交失败: %v", err)
	}
	// A 再提交被拒(一人一次)。
	if _, err := CreateCustomerReview(0, tableID, orderNo, 1, nil, "A 再评", nil, "device-a", "", ""); !errors.Is(err, ErrReviewAlready) {
		t.Fatalf("A 重复提交应被拒, got %v", err)
	}
	// 管理端能看到同单两条。
	res, err := AdminReviewList(dao.ReviewQuery{}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range res.Items {
		if it.OrderNo == orderNo {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("同单应有 2 条评价, got %d", n)
	}
	// 匿名不带键提交被拒(防占位)。
	if _, err := CreateCustomerReview(0, tableID, orderNo, 3, nil, "", nil, "", "", ""); err == nil {
		t.Fatal("匿名缺评价者键应被拒")
	}
}
