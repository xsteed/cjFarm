package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// 强制清台是唯一会「替店家改钱」的桌台操作,落库字段必须逐项核对:
// 账没记上等于钱丢了,记错了则是对账时更麻烦的口径问题。
//
// 这组用例走完整 handler 链路(含操作人留痕),断言到具体字段而不只是状态码。

// relPickupSeq tb_order 上有 (biz_date, pickup_no) 唯一索引,同一营业日的测试订单要错开取餐号。
var relPickupSeq int

// relSeedTable 建一张处于占用状态的桌台。
func relSeedTable(t *testing.T, tableNo string) int {
	t.Helper()
	id, err := dao.InsertTable(po.Table{TableNo: tableNo, TableName: "桌" + tableNo, Capacity: 4, Status: 1})
	if err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	return int(id)
}

// relSeedOrder 种一笔挂在指定桌台的订单(可带一条明细),返回 orderID。
func relSeedOrder(t *testing.T, orderNo string, tableID int, orderStatus, payStatus int, totalCents int64, specID, qty int) int {
	t.Helper()
	relPickupSeq++
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, biz_date, pickup_no, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, tableID, "01", "大厅01桌", 2, orderStatus,
		totalCents, 0, 0, totalCents, payStatus, now[:10], relPickupSeq, now, now); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
	var id int
	if err := store.DB.QueryRow(`SELECT order_id FROM tb_order WHERE order_no=?`, orderNo).Scan(&id); err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if specID > 0 {
		if _, err := store.DB.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
			VALUES(?,0,'测试菜',?,'',?,?,?, '')`, id, specID, totalCents/int64(qty), qty, totalCents); err != nil {
			t.Fatalf("插入订单明细失败: %v", err)
		}
	}
	return id
}

// relSeedSpec 建一条带库存的规格。
func relSeedSpec(t *testing.T, specID, stock int) {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO tb_spec(spec_id, dish_id, spec_name, price, stock) VALUES(?,0,'份',2800,?)`,
		specID, stock); err != nil {
		t.Fatalf("插入规格失败: %v", err)
	}
}

func relSpecStock(t *testing.T, specID int) int {
	t.Helper()
	var s int
	if err := store.DB.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&s); err != nil {
		t.Fatalf("查询库存失败: %v", err)
	}
	return s
}

// relCallRelease 以「张老板」的身份调用强制清台 handler。
func relCallRelease(t *testing.T, tableID int, action string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	payload, _ := json.Marshal(map[string]any{"tableId": tableID, "action": action})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/table/release", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	// 注入登录态:settle_operator / update_by 靠它留痕,空操作人等于事后查不到是谁点的
	permSetAuth(c, "table:edit", "order:view", "order:settle")
	TableRelease(c)
	return w
}

// relOrderFields 订单上「强制清台该写对」的全部字段。
type relOrderFields struct {
	OrderStatus  int
	PayStatus    int
	CreditStatus int
	PreSettle    int
	PayType      string
	SettleType   string
	SettleOper   string
	SettleRemark string
	PayTime      string
	SettleTime   string
	FinishTime   string
	UpdateBy     string
	PaidAmount   int64
	CreditAmount int64
	TotalAmount  int64
	CancelReason string
	ItemCount    int
}

func relLoadOrder(t *testing.T, orderNo string) relOrderFields {
	t.Helper()
	var f relOrderFields
	if err := store.DB.QueryRow(`SELECT order_status, pay_status, COALESCE(credit_status,0), COALESCE(pre_settle_status,0),
		COALESCE(pay_type,''), COALESCE(settle_type,''), COALESCE(settle_operator,''), COALESCE(settle_remark,''),
		COALESCE(pay_time,''), COALESCE(settle_time,''), COALESCE(finish_time,''), COALESCE(update_by,''),
		COALESCE(paid_amount,0), COALESCE(credit_amount,0), COALESCE(total_amount,0), COALESCE(cancel_reason,'')
		FROM tb_order WHERE order_no=?`, orderNo).Scan(
		&f.OrderStatus, &f.PayStatus, &f.CreditStatus, &f.PreSettle,
		&f.PayType, &f.SettleType, &f.SettleOper, &f.SettleRemark,
		&f.PayTime, &f.SettleTime, &f.FinishTime, &f.UpdateBy,
		&f.PaidAmount, &f.CreditAmount, &f.TotalAmount, &f.CancelReason); err != nil {
		t.Fatalf("查询订单 %s 失败: %v", orderNo, err)
	}
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_item WHERE order_id=(SELECT order_id FROM tb_order WHERE order_no=?)`,
		orderNo).Scan(&f.ItemCount); err != nil {
		t.Fatalf("统计明细失败: %v", err)
	}
	return f
}

func relOK(t *testing.T, w *httptest.ResponseRecorder, wantCode int) {
	t.Helper()
	if w.Code != wantCode {
		t.Fatalf("HTTP 应为 %d, got %d body=%s", wantCode, w.Code, w.Body.String())
	}
}

// TestTableReleaseSettlePersistsOrderAndRevenue 补结账:订单按正常收款入账,
// 且这笔钱要能落进营收统计(pay_time 决定归属营业日,写漏了钱就不见了)。
func TestTableReleaseSettlePersistsOrderAndRevenue(t *testing.T) {
	tableInitDB(t)
	tableID := relSeedTable(t, "01")
	orderID := relSeedOrder(t, "REL-SETTLE", tableID, 3, 0, 12800, 0, 0)

	w := relCallRelease(t, tableID, "settle")
	relOK(t, w, http.StatusOK)

	f := relLoadOrder(t, "REL-SETTLE")
	if f.OrderStatus != 4 {
		t.Fatalf("补结账后应归档为已完成(4), got %d", f.OrderStatus)
	}
	if f.PayStatus != 1 {
		t.Fatalf("补结账后应为已支付, got %d", f.PayStatus)
	}
	if f.PayType != "现金" {
		t.Fatalf("未指定渠道时应记为现金, got %q", f.PayType)
	}
	if f.SettleType != "normal" {
		t.Fatalf("补结账应走正常收款, got %q", f.SettleType)
	}
	if f.PaidAmount != f.TotalAmount {
		t.Fatalf("实收应等于应收: paid=%d total=%d", f.PaidAmount, f.TotalAmount)
	}
	if f.CreditAmount != 0 || f.CreditStatus != 0 {
		t.Fatalf("补结账不应产生挂账: credit=%d status=%d", f.CreditAmount, f.CreditStatus)
	}
	if f.PreSettle != 3 {
		t.Fatalf("应记录结账前状态以便撤销, got %d", f.PreSettle)
	}
	if f.PayTime == "" || f.SettleTime == "" || f.FinishTime == "" {
		t.Fatalf("时间字段必须写全: pay=%q settle=%q finish=%q", f.PayTime, f.SettleTime, f.FinishTime)
	}
	if f.SettleOper != "张老板" || f.UpdateBy != "张老板" {
		t.Fatalf("操作人必须留痕: settle_operator=%q update_by=%q", f.SettleOper, f.UpdateBy)
	}
	if f.SettleRemark != "强制清台补结账" {
		t.Fatalf("结算备注应标明来源, got %q", f.SettleRemark)
	}

	// 桌台恢复空闲
	var status int
	if err := store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=?`, tableID).Scan(&status); err != nil {
		t.Fatalf("查询桌台失败: %v", err)
	}
	if status != 0 {
		t.Fatalf("清台后桌台应为空闲, got %d", status)
	}

	// 营收统计必须能统计到这笔:按 pay_time 归属当天
	day := store.Now()[:10]
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if got, err := dao.RangeAmount(day+" 00:00:00", next+" 00:00:00"); err != nil || got != 12800 {
		t.Fatalf("补结账金额应进当天营收, got %d want 12800 err=%v", got, err)
	}
	_ = orderID
}

// TestTableReleaseCancelRestoresStockAndKeepsUnpaid 作废:订单取消、不计收入,占用的库存要还回去。
func TestTableReleaseCancelRestoresStockAndKeepsUnpaid(t *testing.T) {
	tableInitDB(t)
	const specID = 9001
	relSeedSpec(t, specID, 8) // 下了 2 份,库存已从 10 扣到 8
	tableID := relSeedTable(t, "02")
	relSeedOrder(t, "REL-CANCEL", tableID, 1, 0, 5600, specID, 2)

	w := relCallRelease(t, tableID, "cancel")
	relOK(t, w, http.StatusOK)

	f := relLoadOrder(t, "REL-CANCEL")
	if f.OrderStatus != 5 {
		t.Fatalf("作废后应为已取消(5), got %d", f.OrderStatus)
	}
	if f.PayStatus != 0 {
		t.Fatalf("作废不应产生收款, got pay_status=%d", f.PayStatus)
	}
	if f.PaidAmount != 0 {
		t.Fatalf("作废不应记实收, got %d", f.PaidAmount)
	}
	if f.CancelReason != "强制清台作废" {
		t.Fatalf("取消原因应标明来源, got %q", f.CancelReason)
	}
	if f.UpdateBy != "张老板" {
		t.Fatalf("操作人必须留痕, got %q", f.UpdateBy)
	}
	// 明细保留(取消不是删除),库存回补
	if f.ItemCount != 1 {
		t.Fatalf("作废不应删除明细, got %d 条", f.ItemCount)
	}
	if got := relSpecStock(t, specID); got != 10 {
		t.Fatalf("作废应回补库存到 10, got %d", got)
	}

	// 作废的单不进营收
	day := store.Now()[:10]
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if got, err := dao.RangeAmount(day+" 00:00:00", next+" 00:00:00"); err != nil || got != 0 {
		t.Fatalf("作废单不应计入营收, got %d err=%v", got, err)
	}
}

// TestTableReleaseArchivesPaidOrderWithoutTouchingMoney 已收款的单只归档,不动任何账务字段。
func TestTableReleaseArchivesPaidOrderWithoutTouchingMoney(t *testing.T) {
	tableInitDB(t)
	tableID := relSeedTable(t, "03")
	relSeedOrder(t, "REL-PAID", tableID, 2, 1, 9900, 0, 0)
	// 造出「已收款」的原始痕迹:收款方式与收款时间
	if _, err := store.DB.Exec(`UPDATE tb_order SET pay_type='微信', pay_time=?, paid_amount=9900 WHERE order_no='REL-PAID'`,
		store.Now()); err != nil {
		t.Fatalf("预置收款信息失败: %v", err)
	}

	w := relCallRelease(t, tableID, "settle")
	relOK(t, w, http.StatusOK)

	f := relLoadOrder(t, "REL-PAID")
	if f.OrderStatus != 4 {
		t.Fatalf("已收款订单应归档为已完成, got %d", f.OrderStatus)
	}
	if f.PayStatus != 1 {
		t.Fatalf("支付状态不应被改动, got %d", f.PayStatus)
	}
	if f.PayType != "微信" {
		t.Fatalf("原有收款方式不应被覆盖, got %q", f.PayType)
	}
	if f.SettleRemark != "" {
		t.Fatalf("归档不应伪装成一次结账, got settle_remark=%q", f.SettleRemark)
	}
	if f.FinishTime == "" {
		t.Fatal("归档应写 finish_time")
	}
	if f.UpdateBy != "张老板" {
		t.Fatalf("操作人必须留痕, got %q", f.UpdateBy)
	}
}

// TestTableReleaseHandlesMultipleOrdersAtOnce 一桌多单要一次收走,响应里的统计要对得上。
func TestTableReleaseHandlesMultipleOrdersAtOnce(t *testing.T) {
	tableInitDB(t)
	tableID := relSeedTable(t, "04")
	relSeedOrder(t, "REL-M1", tableID, 3, 0, 1000, 0, 0)
	relSeedOrder(t, "REL-M2", tableID, 1, 0, 2500, 0, 0)
	relSeedOrder(t, "REL-M3", tableID, 2, 1, 300, 0, 0) // 已收款未出完 → 归档

	w := relCallRelease(t, tableID, "settle")
	relOK(t, w, http.StatusOK)

	resp := tableJSON(t, w)
	msg, _ := resp["msg"].(string)
	if msg == "" {
		t.Fatalf("响应缺少提示文案: %v", resp)
	}

	for _, tc := range []struct {
		no   string
		want int
	}{
		{"REL-M1", 4}, {"REL-M2", 4}, {"REL-M3", 4},
	} {
		if got := relLoadOrder(t, tc.no).OrderStatus; got != tc.want {
			t.Fatalf("%s 应归档为 %d, got %d", tc.no, tc.want, got)
		}
	}
	if f := relLoadOrder(t, "REL-M3"); f.SettleRemark != "" {
		t.Fatalf("已收款的单不应被写成补结账, got %q", f.SettleRemark)
	}
	if f := relLoadOrder(t, "REL-M2"); f.SettleRemark != "强制清台补结账" {
		t.Fatalf("未收款的单应补结账, got %q", f.SettleRemark)
	}

	// 1 元 + 2.5 元两笔补结账进当天营收,已收款那笔不重复计入
	day := store.Now()[:10]
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if got, err := dao.RangeAmount(day+" 00:00:00", next+" 00:00:00"); err != nil || got != 3500 {
		t.Fatalf("当天营收应为 3500 分, got %d err=%v", got, err)
	}
}

// TestTableReleaseSkipsStaleOrdersWhenGuardOn 跨日清台开启时,强制清台只收本营业日的单。
//
// 这是防「误结正在用餐那桌」的关键:开启清台后,一桌可能同时挂着
// 「昨日遗留单(已被读取层隔离)」和「今日新单」。前者不动账务是对的,
// 但后者要是被一起补结账,正在吃饭的客人就被记成已付款了。
func TestTableReleaseSkipsStaleOrdersWhenGuardOn(t *testing.T) {
	tableInitDB(t)
	if err := service.SetSetting("stale_release_enabled", "1"); err != nil {
		t.Fatalf("开启跨日清台失败: %v", err)
	}
	defer func() { _ = service.SetSetting("stale_release_enabled", "0") }()

	tableID := relSeedTable(t, "06")
	// 昨天遗留的未结账单
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
		dish_amount, seat_fee, discount_amount, total_amount, pay_status, biz_date, pickup_no, create_time, update_time)
		VALUES('REL-OLD', ?, '06', '桌06', 2, 3, 1000, 0, 0, 1000, 0, ?, 101, '2026-01-01 12:00:00', '2026-01-01 12:00:00')`,
		tableID, time.Now().AddDate(0, 0, -1).Format("2006-01-02")); err != nil {
		t.Fatalf("插入跨日遗留单失败: %v", err)
	}
	// 今天新客人的单
	relSeedOrder(t, "REL-TODAY", tableID, 1, 0, 2500, 0, 0)

	w := relCallRelease(t, tableID, "settle")
	relOK(t, w, http.StatusOK)

	if f := relLoadOrder(t, "REL-OLD"); f.OrderStatus != 3 || f.PayStatus != 0 {
		t.Fatalf("跨日遗留单不该被这次强制清台改动: status=%d pay=%d", f.OrderStatus, f.PayStatus)
	}
	if f := relLoadOrder(t, "REL-TODAY"); f.OrderStatus != 4 || f.PayStatus != 1 {
		t.Fatalf("本营业日的单应被补结账: status=%d pay=%d", f.OrderStatus, f.PayStatus)
	}

	// 只有今日那笔进营收:遗留单没被偷偷入账
	day := store.Now()[:10]
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if got, err := dao.RangeAmount(day+" 00:00:00", next+" 00:00:00"); err != nil || got != 2500 {
		t.Fatalf("当天营收应为 2500 分(仅今日单), got %d err=%v", got, err)
	}

	// 桌台照样置空闲
	var status int
	store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=?`, tableID).Scan(&status)
	if status != 0 {
		t.Fatalf("清台后桌台应为空闲, got %d", status)
	}
}

// TestTableReleaseRejectsBadInputWithoutSideEffect 非法入参不得改动任何数据。
func TestTableReleaseRejectsBadInputWithoutSideEffect(t *testing.T) {
	tableInitDB(t)
	tableID := relSeedTable(t, "05")
	relSeedOrder(t, "REL-BAD", tableID, 3, 0, 1000, 0, 0)

	// 未指定处理方式(空 action):必须拒绝 —— 补结账与作废的对账含义相反,
	// 静默默认一个等于替店家做了账务决策。
	w := relCallRelease(t, tableID, "")
	if w.Code == http.StatusOK {
		t.Fatalf("空 action 应被拒绝, body=%s", w.Body.String())
	}
	if f := relLoadOrder(t, "REL-BAD"); f.OrderStatus != 3 || f.PayStatus != 0 {
		t.Fatalf("被拒绝的请求不应改动订单: status=%d pay=%d", f.OrderStatus, f.PayStatus)
	}
	var status int
	store.DB.QueryRow(`SELECT status FROM tb_table WHERE table_id=?`, tableID).Scan(&status)
	if status != 1 {
		t.Fatalf("被拒绝的请求不应改动桌台, got %d", status)
	}

	// 桌台不存在
	w = relCallRelease(t, 999999, "settle")
	if w.Code == http.StatusOK {
		t.Fatalf("不存在的桌台应被拒绝, body=%s", w.Body.String())
	}
}
