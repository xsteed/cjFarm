package handler

import (
	"dining-system/internal/store/dao"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ============================================================================
// 订单资金链路守卫的回归测试
//
// 覆盖本轮修复的状态机/结算约束:
//   1. 未支付订单不可直接置「已完成」;已支付订单不可置「已取消」;
//   2. 撤销结算:恢复结账前状态(而非硬编码回 3);
//   3. 撤销结算:线下 normal 可撤销,在线支付 normal 必须走退款;
//   4. 顾客下单:同一桌台已有进行中订单时拒绝开新单;
//   5. 催菜冷却:事务化后连点不再穿透;
//   6. 重复支付退回不占订单退款额度(SumRefunded 排除 is_duplicate)。
// ============================================================================

func initFlowTestDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "orderflow.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// callHandler 用 JSON body 调一个 handler,返回响应体 map。
func callHandler(t *testing.T, h gin.HandlerFunc, body string) map[string]interface{} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	h(c)
	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// insertFlowOrder 直接种一条订单(绕过业务链路,精确控制状态)。
func insertFlowOrder(t *testing.T, orderNo string, orderStatus, payStatus int, extra ...string) int {
	t.Helper()
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, order_status,
		total_amount, pay_status, create_time, update_time) VALUES(?,?,?, ?,?,?,?, ?,?)`,
		orderNo, 1, "T1", "测试桌", orderStatus, 10000, payStatus, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&id); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	for _, stmt := range extra {
		if _, err := store.DB.Exec(fmt.Sprintf(`UPDATE tb_order SET %s WHERE order_id=%d`, stmt, id)); err != nil {
			t.Fatalf("更新订单失败(%s): %v", stmt, err)
		}
	}
	return id
}

func orderState(t *testing.T, orderID int) (status, payStatus int) {
	t.Helper()
	if err := store.DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_id=?`, orderID).
		Scan(&status, &payStatus); err != nil {
		t.Fatalf("查询订单状态失败: %v", err)
	}
	return
}

// TestOrderStatusTerminalGuards 终态守卫:未支付不可完成、已支付不可取消。
func TestOrderStatusTerminalGuards(t *testing.T) {
	initFlowTestDB(t)

	// 未支付订单置 4(完成) → 拒绝,防漏收。
	unpaid := insertFlowOrder(t, "FG1", 3, 0)
	res := callHandler(t, OrderStatus, fmt.Sprintf(`{"orderId":%d,"orderStatus":4}`, unpaid))
	if res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "未支付") {
		t.Fatalf("未支付订单置完成应被拒绝, got %v", res)
	}

	// 已支付订单置 5(取消) → 拒绝,须先退款。
	paid := insertFlowOrder(t, "FG2", 3, 1)
	res = callHandler(t, OrderStatus, fmt.Sprintf(`{"orderId":%d,"orderStatus":5}`, paid))
	if res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "不能取消") {
		t.Fatalf("已支付订单置取消应被拒绝, got %v", res)
	}

	// 已支付订单 3→4 正常归档。
	res = callHandler(t, OrderStatus, fmt.Sprintf(`{"orderId":%d,"orderStatus":4}`, paid))
	if res["code"] != float64(200) {
		t.Fatalf("已支付订单应可完成, got %v", res)
	}
	if s, _ := orderState(t, paid); s != 4 {
		t.Fatalf("完成后期望状态 4, got %d", s)
	}
}

// TestOrderSettleCancelRestoresPreStatus 撤销结算恢复结账前状态(而非硬编码回 3)。
func TestOrderSettleCancelRestoresPreStatus(t *testing.T) {
	initFlowTestDB(t)

	// 状态 1(已下单)被免单结账 → 撤销后应回到 1,而不是跳到 3。
	id := insertFlowOrder(t, "FG3", 4, 1, "pre_settle_status=1, settle_type='free', paid_amount=0")
	res := callHandler(t, OrderSettleCancel, fmt.Sprintf(`{"orderId":%d,"reason":"录错"}`, id))
	if res["code"] != float64(200) {
		t.Fatalf("免单撤销应成功, got %v", res)
	}
	if s, p := orderState(t, id); s != 1 || p != 0 {
		t.Fatalf("撤销后期望(状态1,未支付), got (%d,%d)", s, p)
	}

	// 历史订单(无快照,pre_settle_status=0)撤销回退为 3。
	legacy := insertFlowOrder(t, "FG4", 4, 1, "settle_type='free', paid_amount=0")
	res = callHandler(t, OrderSettleCancel, fmt.Sprintf(`{"orderId":%d,"reason":"录错"}`, legacy))
	if res["code"] != float64(200) {
		t.Fatalf("历史免单撤销应成功, got %v", res)
	}
	if s, _ := orderState(t, legacy); s != 3 {
		t.Fatalf("历史订单撤销后期望状态 3, got %d", s)
	}
}

// TestOrderSettleCancelNormalChannelRules normal 结算的撤销规则:
// 线下收款可撤销纠错;在线支付必须走退款流程。
func TestOrderSettleCancelNormalChannelRules(t *testing.T) {
	initFlowTestDB(t)

	// 线下(现金)normal:允许撤销,用于收银误操作纠错。
	offline := insertFlowOrder(t, "FG5", 4, 1,
		"pre_settle_status=3, settle_type='normal', paid_amount=10000, pay_time='"+store.Now()+"'")
	res := callHandler(t, OrderSettleCancel, fmt.Sprintf(`{"orderId":%d,"reason":"现金收错"}`, offline))
	if res["code"] != float64(200) {
		t.Fatalf("线下 normal 撤销应成功, got %v", res)
	}
	if s, p := orderState(t, offline); s != 3 || p != 0 {
		t.Fatalf("线下 normal 撤销后期望(状态3,未支付), got (%d,%d)", s, p)
	}

	// 在线支付 normal:拒绝 —— 渠道资金必须原路退回。
	online := insertFlowOrder(t, "FG6", 4, 1,
		"pre_settle_status=3, settle_type='normal', paid_amount=10000, pay_channel='wxpay', pay_time='"+store.Now()+"'")
	res = callHandler(t, OrderSettleCancel, fmt.Sprintf(`{"orderId":%d,"reason":""}`, online))
	if res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "退款") {
		t.Fatalf("在线 normal 撤销应被拒绝并提示走退款, got %v", res)
	}
}

// TestCustomerCreateOrderRejectsDuplicateActiveOrder 一桌一单:已有进行中订单时拒绝开新单。
func TestCustomerCreateOrderRejectsDuplicateActiveOrder(t *testing.T) {
	initFlowTestDB(t)

	// 取一对真实存在的 (dish_id, spec_id) 组装合法明细。
	var dishID, specID int
	if err := store.DB.QueryRow(`SELECT dish_id, spec_id FROM tb_spec LIMIT 1`).Scan(&dishID, &specID); err != nil {
		t.Fatalf("读取种子规格失败: %v", err)
	}
	body := fmt.Sprintf(`{"tableId":1,"personCount":2,"items":[{"dishId":%d,"specId":%d,"quantity":1}]}`, dishID, specID)

	// 第一单成功。
	res := callHandler(t, CustomerCreateOrder, body)
	if res["code"] != float64(200) {
		t.Fatalf("首单应成功, got %v", res)
	}
	// 同桌第二单(模拟双击/重复提交)被拒。
	res = callHandler(t, CustomerCreateOrder, body)
	if res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "进行中的订单") {
		t.Fatalf("重复开单应被拒绝, got %v", res)
	}
}

// TestCustomerUrgeCooldownSerial 催菜冷却:冷却期内第二次催菜被拒。
func TestCustomerUrgeCooldownSerial(t *testing.T) {
	initFlowTestDB(t)

	id := insertFlowOrder(t, "FG7", 1, 0)
	orderNo := "FG7"
	// 催菜需携带桌台归属(tableId),insertFlowOrder 写入的订单属于桌台 1。
	res := callHandler(t, CustomerUrgeOrder, fmt.Sprintf(`{"orderNo":%q,"tableId":1}`, orderNo))
	if res["code"] != float64(200) {
		t.Fatalf("首次催菜应成功, got %v", res)
	}
	res = callHandler(t, CustomerUrgeOrder, fmt.Sprintf(`{"orderNo":%q,"tableId":1}`, orderNo))
	if res["code"] != float64(400) {
		t.Fatalf("冷却期内催菜应被拒绝, got %v", res)
	}
	// 冷却期内只有一条催菜记录(并发连点不再穿透)。
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_urge WHERE order_id=?`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("期望仅 1 条催菜记录, got %d (err=%v)", n, err)
	}
}

// TestSumRefundedExcludesDuplicate 重复支付退回不计入订单退款额度。
func TestSumRefundedExcludesDuplicate(t *testing.T) {
	initFlowTestDB(t)

	id := insertFlowOrder(t, "FG8", 4, 1)
	now := store.Now()
	// 普通退款 30 元(已成功)。
	if _, err := dao.InsertRefund(po.Refund{
		OrderID: id, OrderNo: "FG8", RefundNo: "FR1", Channel: "wxpay",
		Amount: 3000, Status: po.RefundStatusSuccess,
	}); err != nil {
		t.Fatalf("登记普通退款失败: %v", err)
	}
	// 重复支付退回 100 元(已成功,is_duplicate=1)。
	if _, err := dao.InsertRefund(po.Refund{
		OrderID: id, OrderNo: "FG8", RefundNo: "FR2", Channel: "alipay",
		Amount: 10000, Status: po.RefundStatusSuccess, IsDuplicate: 1, Reason: "重复支付自动原路退回",
	}); err != nil {
		t.Fatalf("登记重复退回失败: %v", err)
	}
	_ = now
	if got := dao.SumRefunded("FG8"); got != 3000 {
		t.Fatalf("SumRefunded 应排除重复退回(期望 3000), got %d", got)
	}
	if got, err := dao.SumRefundedInclPending("FG8"); err != nil || got != 3000 {
		t.Fatalf("SumRefundedInclPending 应排除重复退回(期望 3000), got %d (err=%v)", got, err)
	}
	// 时间参数仅用于按 update_time 聚合的报表口径,这里不依赖。
	_ = time.Now()
}
