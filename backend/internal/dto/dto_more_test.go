package dto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dining-system/internal/po"
)

// TestDtoOrderItemMapping 校验订单明细字段映射与金额「分↔元」转换。
func TestDtoOrderItemMapping(t *testing.T) {
	p := po.OrderItem{ItemID: 1, DishID: 2, DishName: "宫保鸡丁", SpecID: 3, SpecName: "大份", Price: 2800, Quantity: 2, Amount: 5600, Remark: "少辣"}
	want := OrderItem{ItemID: 1, DishID: 2, DishName: "宫保鸡丁", SpecID: 3, SpecName: "大份", Price: 28, Quantity: 2, Amount: 56, Remark: "少辣"}
	if got := FromOrderItem(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromOrderItem() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoOrderMapping 校验订单字段映射:金额转换、短号派生与 Items/PendingUrge 组装。
func TestDtoOrderMapping(t *testing.T) {
	orderNo := "D20260921120000abcdef012345"
	payTime := "2024-01-01 00:00:01"
	remark := "订单备注"
	p := po.Order{
		OrderID: 1, OrderNo: orderNo, TableID: 2, TableNo: "A01", TableName: "大厅1号",
		PersonCount: 3, OrderStatus: 2,
		DishAmount: 1000, SeatFee: 200, DiscountAmount: 100, TotalAmount: 1100,
		PayStatus: 1, PayType: dmStr("wxpay"), PayTime: &payTime,
		TransactionID: "T123", PayChannel: "wxpay",
		RefundAmount: 0, RefundTime: nil,
		SettleType: "normal", SettleTime: &payTime, SettleOperator: "admin", SettleRemark: "",
		CreditStatus: 0, CreditAmount: 0, CreditSettleTime: nil, CreditSettleBy: "",
		PaidAmount: 1100, FinishTime: &payTime,
		OrderRemark: "", CancelReason: "",
		BeginTime: nil, EndTime: nil,
		CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-01 00:00:00",
		Remark: &remark,
	}
	items := []OrderItem{{ItemID: 1, DishName: "宫保鸡丁"}}
	got := FromOrder(p, items, true)
	if got.ShortNo != po.ShortOrderNo(orderNo) {
		t.Errorf("ShortNo = %q, want %q", got.ShortNo, po.ShortOrderNo(orderNo))
	}
	if got.DishAmount != 10 || got.SeatFee != 2 || got.DiscountAmount != 1 || got.TotalAmount != 11 {
		t.Errorf("金额字段转元错误: %+v", got)
	}
	if got.PaidAmount != 11 || got.RefundAmount != 0 || got.CreditAmount != 0 {
		t.Errorf("结算金额字段转元错误: %+v", got)
	}
	if got.PayType == nil || *got.PayType != "wxpay" {
		t.Errorf("PayType 指针未透传: %+v", got.PayType)
	}
	if got.Remark == nil || *got.Remark != remark {
		t.Errorf("Remark 指针未透传: %+v", got.Remark)
	}
	if !reflect.DeepEqual(got.Items, items) {
		t.Errorf("Items = %+v, want %+v", got.Items, items)
	}
	if !got.PendingUrge {
		t.Errorf("PendingUrge = false, want true")
	}
	// ToPO 金额转回分,Items/PendingUrge/ShortNo 不入库。
	if back := got.ToPO(); !reflect.DeepEqual(back, p) {
		t.Errorf("ToPO() = %+v, want %+v", back, p)
	}
}

// TestDtoOrderUrgeMapping 校验催菜记录映射:短号派生与 HandleTime 指针透传。
func TestDtoOrderUrgeMapping(t *testing.T) {
	orderNo := "D20260921120000abcdef012345"
	handleTime := "2024-01-01 00:00:05"
	p := po.OrderUrge{UrgeID: 1, OrderID: 2, OrderNo: orderNo, TableID: 3, TableNo: "A01", TableName: "大厅1号", UrgeType: "urge", Status: 1, Remark: "催菜", CreateTime: "2024-01-01 00:00:00", HandleTime: &handleTime, HandleBy: "admin"}
	want := OrderUrge{UrgeID: 1, OrderID: 2, OrderNo: orderNo, ShortNo: po.ShortOrderNo(orderNo), TableID: 3, TableNo: "A01", TableName: "大厅1号", UrgeType: "urge", Status: 1, Remark: "催菜", CreateTime: "2024-01-01 00:00:00", HandleTime: &handleTime, HandleBy: "admin"}
	if got := FromOrderUrge(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromOrderUrge() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoPrintLogMapping 校验打印日志映射:短号派生与完整字段透传。
func TestDtoPrintLogMapping(t *testing.T) {
	orderNo := "D20260921120000abcdef012345"
	p := po.PrintLog{PrintID: 1, OrderID: 2, OrderNo: orderNo, TableNo: "A01", TableName: "大厅1号", PrinterID: 3, PrinterName: "后厨", PrinterType: 1, Provider: "feie", DocType: "kitchen", Copies: 1, Status: 1, RemoteID: "R1", Detail: "ok", TriggerBy: "order", Operator: "admin", CostMs: 10, CreateTime: "2024-01-01 00:00:00"}
	want := PrintLog{PrintID: 1, OrderID: 2, OrderNo: orderNo, ShortNo: po.ShortOrderNo(orderNo), TableNo: "A01", TableName: "大厅1号", PrinterID: 3, PrinterName: "后厨", PrinterType: 1, Provider: "feie", DocType: "kitchen", Copies: 1, Status: 1, RemoteID: "R1", Detail: "ok", TriggerBy: "order", Operator: "admin", CostMs: 10, CreateTime: "2024-01-01 00:00:00"}
	if got := FromPrintLog(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPrintLog() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoZeroValueBoundaries 校验零值/空值边界不产生意外结果。
func TestDtoZeroValueBoundaries(t *testing.T) {
	d := FromOrder(po.Order{}, nil, false)
	if d.ShortNo != "" {
		t.Errorf("空订单 ShortNo = %q, want 空串", d.ShortNo)
	}
	if d.TotalAmount != 0 || d.DishAmount != 0 {
		t.Errorf("空订单金额应全为 0: %+v", d)
	}
	if got := (OrderItem{}).ToPO(); got.Price != 0 || got.Amount != 0 {
		t.Errorf("零值 OrderItem.ToPO() 金额应全为 0: %+v", got)
	}
	if got := FromSpec(po.Spec{}).Price; got != 0 {
		t.Errorf("零值 Spec 单价应为 0: %v", got)
	}
	if got := (Order{OrderNo: "D123"}).ToPO(); got.OrderNo != "D123" {
		t.Errorf("ToPO 应透传 OrderNo: %+v", got)
	}
}

// TestDtoSettingsJSONTagsMore 校验 Settings 的 snake_case 标签与 version 省略规则。
func TestDtoSettingsJSONTagsMore(t *testing.T) {
	src := Settings{ShopName: "测试门店", WxpayEnabled: "1", AgentLatestVersion: "1.2.3", Version: "abc123"}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if !strings.Contains(string(b), `"shop_name"`) || !strings.Contains(string(b), `"wxpay_enabled"`) {
		t.Errorf("序列化未使用 snake_case: %s", string(b))
	}
	var back Settings
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if back.ShopName != "测试门店" || back.WxpayEnabled != "1" || back.AgentLatestVersion != "1.2.3" || back.Version != "abc123" {
		t.Errorf("反序列化结果不符: %+v", back)
	}
	// Version 空值时因 omitempty 不出现在 JSON 中。
	b2, err := json.Marshal(Settings{ShopName: "x"})
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if strings.Contains(string(b2), `"version"`) {
		t.Errorf("Version 为空时不应序列化: %s", string(b2))
	}
}
