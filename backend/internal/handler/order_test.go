package handler

// ============================================================================
// 订单 HTTP 层单元测试(order.go)
//
// 复用 orderflow_test.go 的 initFlowTestDB / insertFlowOrder / callHandler,
// 覆盖订单列表筛选、详情读取、看板、状态流转、催菜、收款、结账、挂账核销、
// 撤销结算、完成、取消、改单等正常与失败分支。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/service"
	"dining-system/internal/store"
)

// ordGet 以 GET 方式调用 handler,支持 query 与路径参数,返回解析后的响应体。
func ordGet(t *testing.T, h gin.HandlerFunc, query string, params map[string]string) map[string]interface{} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	target := "/test"
	if query != "" {
		target += "?" + query
	}
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range params {
		c.Params = append(c.Params, gin.Param{Key: k, Value: v})
	}
	h(c)
	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// ordDisablePrint 关闭小票打印,避免结账等路径触发异步打印网络连接。
func ordDisablePrint(t *testing.T) {
	t.Helper()
	if err := service.SetSetting("print_enabled", "0"); err != nil {
		t.Fatalf("关闭打印失败: %v", err)
	}
}

// ordInsertUrge 插入一条待处理催菜记录并返回主键。
func ordInsertUrge(t *testing.T, orderID int, orderNo, tableNo string) int {
	t.Helper()
	res, err := store.DB.Exec(`INSERT INTO tb_order_urge(order_id, order_no, table_id, table_no, table_name, urge_type, status, create_time)
		VALUES(?,?,?,?,?,?,?,?)`, orderID, orderNo, 1, tableNo, "测试桌", "urge", 0, store.Now())
	if err != nil {
		t.Fatalf("插入催菜失败: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestIntArg(t *testing.T) {
	if v := intArg("12"); v == nil || *v != 12 {
		t.Fatalf("intArg(\"12\") 期望 12, got %v", v)
	}
	if v := intArg(" 7 "); v == nil || *v != 7 {
		t.Fatalf("intArg(\" 7 \") 期望 7, got %v", v)
	}
	if v := intArg("abc"); v != nil {
		t.Fatalf("intArg(\"abc\") 应返回 nil, got %v", *v)
	}
	if v := intArg(""); v != nil {
		t.Fatalf("intArg(\"\") 应返回 nil, got %v", *v)
	}
}

func TestOrderListAndFilters(t *testing.T) {
	initFlowTestDB(t)
	insertFlowOrder(t, "ORDLIST1", 1, 0, "settle_type='normal', credit_status=0")

	// 覆盖全部筛选条件,只要不报错且 total >= 1 即通过。
	res := ordGet(t, OrderList, "orderStatus=1&payStatus=0&orderNo=ORDLIST&tableNo=T1&settleType=normal&creditStatus=0", nil)
	if res["code"] != float64(200) {
		t.Fatalf("订单列表应返回 200, got %v", res)
	}
	data := res["data"].(map[string]interface{})
	if data["total"].(float64) < 1 {
		t.Fatalf("订单列表应至少命中 1 条, got %v", data["total"])
	}
}

func TestOrderGetBranches(t *testing.T) {
	initFlowTestDB(t)

	// 非法路径参数。
	res := ordGet(t, OrderGet, "", map[string]string{"id": "abc"})
	if res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "无效的ID") {
		t.Fatalf("非法 ID 应返回参数错误, got %v", res)
	}

	// 订单不存在。
	res = ordGet(t, OrderGet, "", map[string]string{"id": "99999"})
	if res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}

	// 正常详情。
	id := insertFlowOrder(t, "ORDGET1", 3, 1)
	res = ordGet(t, OrderGet, "", map[string]string{"id": fmt.Sprintf("%d", id)})
	if res["code"] != float64(200) {
		t.Fatalf("订单详情应返回 200, got %v", res)
	}
	if res["data"].(map[string]interface{})["orderId"] != float64(id) {
		t.Fatalf("订单详情 ID 错误, got %v", res["data"])
	}
}

func TestOrderBoard(t *testing.T) {
	initFlowTestDB(t)
	res := ordGet(t, OrderBoard, "", nil)
	if res["code"] != float64(200) {
		t.Fatalf("看板应返回 200, got %v", res)
	}
	list, ok := res["data"].([]interface{})
	if !ok {
		t.Fatalf("看板 data 应为数组, got %T", res["data"])
	}
	if len(list) == 0 {
		t.Fatalf("看板应包含种子桌台")
	}
}

func TestOrderStatusBranches(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "ORDSTAT1", 1, 0)

	if res := callHandler(t, OrderStatus, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderStatus, fmt.Sprintf(`{"orderId":%d,"orderStatus":9}`, id)); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "非法的订单状态") {
		t.Fatalf("非法状态应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderStatus, `{"orderId":99999,"orderStatus":2}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
}

func TestUrgeListAndHandle(t *testing.T) {
	initFlowTestDB(t)
	id := insertFlowOrder(t, "ORDURGE1", 1, 0)
	urgeID := ordInsertUrge(t, id, "ORDURGE1", "T1")

	res := ordGet(t, UrgeList, "status=0&tableNo=T1", nil)
	if res["code"] != float64(200) {
		t.Fatalf("催菜列表应返回 200, got %v", res)
	}

	// 缺参。
	if res := callHandler(t, UrgeHandle, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	// 按单批量处理。
	if res := callHandler(t, UrgeHandle, fmt.Sprintf(`{"orderId":%d}`, id)); res["code"] != float64(200) {
		t.Fatalf("按单处理催菜应成功, got %v", res)
	}
	// 已处理后再按单条处理应提示已处理。
	if res := callHandler(t, UrgeHandle, fmt.Sprintf(`{"urgeId":%d}`, urgeID)); res["code"] != float64(400) || res["msg"] != "该催菜已处理" {
		t.Fatalf("重复处理应提示已处理, got %v", res)
	}
}

func TestOrderPayBranches(t *testing.T) {
	initFlowTestDB(t)
	paid := insertFlowOrder(t, "ORDPAY1", 3, 1)
	unpaid := insertFlowOrder(t, "ORDPAY2", 3, 0)

	if res := callHandler(t, OrderPay, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderPay, `{"orderId":99999,"payType":"现金"}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderPay, fmt.Sprintf(`{"orderId":%d,"payType":"现金"}`, paid)); res["code"] != float64(400) || res["msg"] != "订单已支付" {
		t.Fatalf("已支付订单应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderPay, fmt.Sprintf(`{"orderId":%d,"payType":""}`, unpaid)); res["code"] != float64(200) || res["msg"] != "支付成功" {
		t.Fatalf("未支付订单收款应成功, got %v", res)
	}
}

func TestOrderSettleBranches(t *testing.T) {
	initFlowTestDB(t)
	ordDisablePrint(t)
	id := insertFlowOrder(t, "ORDSETTLE1", 3, 0)

	if res := callHandler(t, OrderSettle, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"bogus"}`, id)); res["code"] != float64(400) || res["msg"] != "非法的结算方式" {
		t.Fatalf("非法结算方式应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"free"}`, id)); res["code"] != float64(400) || res["msg"] != "免单必须填写原因" {
		t.Fatalf("免单缺原因应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"credit"}`, id)); res["code"] != float64(400) || res["msg"] != "挂账必须填写挂账单位/事由" {
		t.Fatalf("挂账缺事由应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"normal","payType":"现金"}`, id)); res["code"] != float64(200) || res["msg"] != "结账成功" {
		t.Fatalf("正常结账应成功, got %v", res)
	}

	freeID := insertFlowOrder(t, "ORDSETTLE2", 3, 0)
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"free","settleRemark":"客户投诉"}`, freeID)); res["code"] != float64(200) || !strings.Contains(res["msg"].(string), "免单成功") {
		t.Fatalf("免单应成功, got %v", res)
	}

	creditID := insertFlowOrder(t, "ORDSETTLE3", 3, 0)
	if res := callHandler(t, OrderSettle, fmt.Sprintf(`{"orderId":%d,"settleType":"credit","settleRemark":"某公司"}`, creditID)); res["code"] != float64(200) || !strings.Contains(res["msg"].(string), "挂账成功") {
		t.Fatalf("挂账应成功, got %v", res)
	}
}

func TestOrderCreditSettleBranches(t *testing.T) {
	initFlowTestDB(t)
	pending := insertFlowOrder(t, "ORDCREDIT1", 4, 1, "settle_type='credit', credit_status=1, credit_amount=10000, paid_amount=0, pre_settle_status=3")
	normal := insertFlowOrder(t, "ORDCREDIT2", 4, 1, "settle_type='normal', credit_status=0")

	if res := callHandler(t, OrderCreditSettle, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderCreditSettle, `{"orderId":99999,"payType":"现金"}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderCreditSettle, fmt.Sprintf(`{"orderId":%d,"payType":"现金"}`, normal)); res["code"] != float64(400) || res["msg"] != "该订单无需核销" {
		t.Fatalf("非挂账订单应返回无需核销, got %v", res)
	}
	if res := callHandler(t, OrderCreditSettle, fmt.Sprintf(`{"orderId":%d,"payType":"现金"}`, pending)); res["code"] != float64(200) || !strings.Contains(res["msg"].(string), "挂账已核销") {
		t.Fatalf("挂账核销应成功, got %v", res)
	}
}

func TestOrderSettleCancelMoreBranches(t *testing.T) {
	initFlowTestDB(t)
	unpaid := insertFlowOrder(t, "ORDCANCELSET1", 3, 0)

	if res := callHandler(t, OrderSettleCancel, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderSettleCancel, `{"orderId":99999,"reason":"录错"}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderSettleCancel, fmt.Sprintf(`{"orderId":%d,"reason":"录错"}`, unpaid)); res["code"] != float64(400) || res["msg"] != "订单未结算，无需撤销" {
		t.Fatalf("未结算订单应返回无需撤销, got %v", res)
	}
}

func TestOrderFinishBranches(t *testing.T) {
	initFlowTestDB(t)
	unpaid := insertFlowOrder(t, "ORDFIN1", 3, 0)
	paid := insertFlowOrder(t, "ORDFIN2", 3, 1)

	if res := callHandler(t, OrderFinish, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderFinish, `{"orderId":99999}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderFinish, fmt.Sprintf(`{"orderId":%d}`, unpaid)); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "未支付") {
		t.Fatalf("未支付订单应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderFinish, fmt.Sprintf(`{"orderId":%d}`, paid)); res["code"] != float64(200) || res["msg"] != "订单已完成并归档" {
		t.Fatalf("已支付订单完成应成功, got %v", res)
	}
}

func TestOrderCancelBranches(t *testing.T) {
	initFlowTestDB(t)
	unpaid := insertFlowOrder(t, "ORDCANCEL1", 1, 0)
	paid := insertFlowOrder(t, "ORDCANCEL2", 1, 1)

	if res := callHandler(t, OrderCancel, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderCancel, `{"orderId":99999,"cancelReason":"录错"}`); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderCancel, fmt.Sprintf(`{"orderId":%d,"cancelReason":"录错"}`, paid)); res["code"] != float64(400) || !strings.Contains(res["msg"].(string), "不能直接取消") {
		t.Fatalf("已支付订单取消应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderCancel, fmt.Sprintf(`{"orderId":%d,"cancelReason":"录错"}`, unpaid)); res["code"] != float64(200) || res["msg"] != "订单已取消" {
		t.Fatalf("未支付订单取消应成功, got %v", res)
	}
}

func TestOrderEditBranches(t *testing.T) {
	initFlowTestDB(t)
	unpaid := insertFlowOrder(t, "ORDEDIT1", 1, 0)
	paid := insertFlowOrder(t, "ORDEDIT2", 1, 1)

	var dishID, specID int
	if err := store.DB.QueryRow(`SELECT dish_id, spec_id FROM tb_spec LIMIT 1`).Scan(&dishID, &specID); err != nil {
		t.Fatalf("读取种子规格失败: %v", err)
	}
	validItems := fmt.Sprintf(`[{"dishId":%d,"specId":%d,"quantity":1}]`, dishID, specID)

	if res := callHandler(t, OrderEdit, `{}`); res["code"] != float64(400) || res["msg"] != "参数错误" {
		t.Fatalf("缺参应返回参数错误, got %v", res)
	}
	if res := callHandler(t, OrderEdit, fmt.Sprintf(`{"orderId":99999,"items":%s}`, validItems)); res["code"] != float64(400) || res["msg"] != "订单不存在" {
		t.Fatalf("不存在的订单应返回订单不存在, got %v", res)
	}
	if res := callHandler(t, OrderEdit, fmt.Sprintf(`{"orderId":%d,"items":[]}`, unpaid)); res["code"] != float64(400) || res["msg"] != "订单明细不能为空" {
		t.Fatalf("空明细应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderEdit, fmt.Sprintf(`{"orderId":%d,"items":%s}`, paid, validItems)); res["code"] != float64(400) || res["msg"] != "已支付订单不能改单" {
		t.Fatalf("已支付订单改单应被拒绝, got %v", res)
	}
	if res := callHandler(t, OrderEdit, fmt.Sprintf(`{"orderId":%d,"personCount":2,"items":%s}`, unpaid, validItems)); res["code"] != float64(200) || res["msg"] != "改单成功，金额已重算" {
		t.Fatalf("改单应成功, got %v", res)
	}
}
