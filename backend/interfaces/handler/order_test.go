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

	"dining-system/application/service"
	"dining-system/infrastructure/store"
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

// intArgTestCtx 构造一个带指定 query 的测试上下文。
func intArgTestCtx(query string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test?"+query, nil)
	return c, w
}

func TestIntArg(t *testing.T) {
	// 空值 = 不限:(nil, true)。
	c, _ := intArgTestCtx("")
	if v, ok := intArg(c, "n"); !ok || v != nil {
		t.Fatalf("空查询值应返回 (nil, true), got %v ok=%v", v, ok)
	}
	// 合法整数(带空格)。
	c, _ = intArgTestCtx("n=%207%20")
	if v, ok := intArg(c, "n"); !ok || v == nil || *v != 7 {
		t.Fatalf("intArg(\" 7 \") 期望 7, got %v ok=%v", v, ok)
	}
	// 非法值:400 + (nil, false)。
	c, w := intArgTestCtx("n=abc")
	if v, ok := intArg(c, "n"); ok || v != nil {
		t.Fatalf("非法值应返回 (nil, false), got %v ok=%v", v, ok)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法值应写 400, got %d", w.Code)
	}
}

func TestOrderListAndFilters(t *testing.T) {
	initFlowTestDB(t)
	insertFlowOrder(t, "ORDLIST1", 1, 0, "settle_type='normal', credit_status=0")

	// 覆盖全部筛选条件,只要不报错且 total >= 1 即通过。
	res := ordGet(t, OrderList, "orderStatus=1&payStatus=0&orderNo=ORDLIST&tableNo=T1&tableName=测试桌&settleType=normal&creditStatus=0", nil)
	if res["code"] != float64(200) {
		t.Fatalf("订单列表应返回 200, got %v", res)
	}
	data := res["data"].(map[string]interface{})
	if data["total"].(float64) < 1 {
		t.Fatalf("订单列表应至少命中 1 条, got %v", data["total"])
	}
}

// TestOrderListBizDayAndHourFilters 报表下钻用的营业日区间 / 下单小时筛选。
// 口径与报表一致:区间按 create_time 折算营业日(受 biz_date_cutoff_hour 影响),
// 小时取 create_time 原始小时。
func TestOrderListBizDayAndHourFilters(t *testing.T) {
	initFlowTestDB(t)
	// 两单分别落在 10-23 深夜与 10-24 早间;营业日折算由 cutoff 配置决定归属。
	insertFlowOrder(t, "ORDBIZN", 1, 0, "create_time='2026-10-23 23:30:00'")
	insertFlowOrder(t, "ORDBIZD", 1, 0, "create_time='2026-10-24 08:00:00'")

	listTotal := func(query string) int {
		t.Helper()
		res := ordGet(t, OrderList, query, nil)
		if res["code"] != float64(200) {
			t.Fatalf("订单列表(%s)应返回 200, got %v", query, res)
		}
		return int(res["data"].(map[string]interface{})["total"].(float64))
	}

	// 默认 cutoff=0(自然日):两单分属 10-23 与 10-24。
	if got := listTotal("start=2026-10-23&end=2026-10-23"); got != 1 {
		t.Fatalf("自然日口径 10-23 应命中 1 单, got %d", got)
	}
	if got := listTotal("start=2026-10-24&end=2026-10-24"); got != 1 {
		t.Fatalf("自然日口径 10-24 应命中 1 单, got %d", got)
	}
	// 小时筛选与区间可叠加。
	if got := listTotal("start=2026-10-23&end=2026-10-24&hour=23"); got != 1 {
		t.Fatalf("hour=23 应只命中深夜单, got %d", got)
	}
	if got := listTotal("hour=8"); got != 1 {
		t.Fatalf("hour=8 应只命中早间单, got %d", got)
	}
	// 单边区间也可用。
	if got := listTotal("end=2026-10-23"); got != 1 {
		t.Fatalf("end=2026-10-23 单边应命中 1 单, got %d", got)
	}

	// cutoff=4:凌晨 0~4 点归前一营业日;23:30 不受影响。深夜单仍属 10-23,
	// 早间单不变;再造一条 10-24 凌晨 2 点的单验证它折算回 10-23。
	if err := service.SetSetting("biz_date_cutoff_hour", "4"); err != nil {
		t.Fatalf("设置营业日切换时刻失败: %v", err)
	}
	defer func() { _ = service.SetSetting("biz_date_cutoff_hour", "0") }()
	insertFlowOrder(t, "ORDBIZC", 1, 0, "create_time='2026-10-24 02:00:00'")
	if got := listTotal("start=2026-10-23&end=2026-10-23"); got != 2 {
		t.Fatalf("cutoff=4 时 10-23 营业日应含深夜单与凌晨单, got %d", got)
	}
	if got := listTotal("start=2026-10-24&end=2026-10-24"); got != 1 {
		t.Fatalf("cutoff=4 时 10-24 营业日应只含早间单, got %d", got)
	}

	// 非法参数:畸形日期与非 0-23 的 hour 都必须 400,而不是静默当「不限」。
	for _, bad := range []string{"start=10-23", "end=2026/10/23", "hour=24", "hour=abc"} {
		res := ordGet(t, OrderList, bad, nil)
		if res["code"] != float64(400) {
			t.Fatalf("非法参数(%s)应返回 400, got %v", bad, res)
		}
	}

	// hideCanceled=1:已取消订单不计入,与报表订单数同口径。
	insertFlowOrder(t, "ORDBIZX", 5, 0, "create_time='2026-10-24 09:00:00'")
	if got := listTotal("start=2026-10-24&end=2026-10-24"); got != 2 {
		t.Fatalf("含已取消时 10-24 应命中 2 单, got %d", got)
	}
	if got := listTotal("start=2026-10-24&end=2026-10-24&hideCanceled=1"); got != 1 {
		t.Fatalf("hideCanceled=1 时 10-24 应只命中 1 单, got %d", got)
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
