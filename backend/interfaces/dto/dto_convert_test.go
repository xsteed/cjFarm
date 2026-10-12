package dto

import (
	"reflect"
	"strings"
	"testing"

	"dining-system/domain/po"
)

// dmStr 返回字符串指针,便于构造含 *string 字段的出入参。
func dmStr(s string) *string { return &s }

// TestDtoCategoryMapping 校验分类字段映射与往返。
func TestDtoCategoryMapping(t *testing.T) {
	p := po.Category{CategoryID: 1, CategoryName: "热菜", SortOrder: 2, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-02 00:00:00"}
	want := Category{CategoryID: 1, CategoryName: "热菜", SortOrder: 2, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-02 00:00:00"}
	if got := FromCategory(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromCategory() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoTableMapping 校验桌台字段映射与 Remark 指针透传。
func TestDtoTableMapping(t *testing.T) {
	p := po.Table{TableID: 3, TableNo: "A01", TableName: "大厅1号", Capacity: 4, Status: 1, SortOrder: 1, DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-02 00:00:00", Remark: dmStr("靠窗"), TableCode: "TCODE01"}
	want := Table{TableID: 3, TableNo: "A01", TableName: "大厅1号", Capacity: 4, Status: 1, SortOrder: 1, DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-02 00:00:00", Remark: dmStr("靠窗"), TableCode: "TCODE01"}
	if got := FromTable(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromTable() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoRemarkMapping 校验备注选项字段映射。
func TestDtoRemarkMapping(t *testing.T) {
	p := po.Remark{RemarkID: 7, OptionName: "少辣", SortOrder: 1, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-02 00:00:00"}
	want := Remark{RemarkID: 7, OptionName: "少辣", SortOrder: 1, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-02 00:00:00"}
	if got := FromRemark(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromRemark() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoSpecMapping 校验规格字段映射与单价「分↔元」转换。
func TestDtoSpecMapping(t *testing.T) {
	p := po.Spec{SpecID: 1, DishID: 2, SpecName: "大份", Price: 1880}
	want := Spec{SpecID: 1, DishID: 2, SpecName: "大份", Price: 18.8}
	if got := FromSpec(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromSpec() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestFromOrderCustomerCancelReason 校验顾客端取消原因的「可展示性」收口。
//
// 背景:顾客端 H5 与小程序都有「订单已取消：<原因>」这一行,但出参层曾把 CancelReason 一律清空,
// 于是这行永远是空的(顾客只看到「已取消」)。修复后按三条规则处理:
//  1. 空 / 通用标签「商家取消」 → 置空,由前端给标准提示(未支付、不产生费用);
//  2. 真实原因(如「菜品售罄」) → 去空白后原样下发;
//  3. 超长文本 → 按字符截断,避免整段内部备注出现在顾客手机上。
func TestFromOrderCustomerCancelReason(t *testing.T) {
	long := strings.Repeat("备注", 30) // 60 字符
	cases := []struct {
		name     string
		in       string
		want     string
		wantFull string
	}{
		{"空原因", "", "", ""},
		{"通用标签不含信息", "商家取消", "", "商家取消"},
		{"真实原因原样下发", "菜品售罄", "菜品售罄", "菜品售罄"},
		{"去首尾空白", "  顾客要求取消  ", "顾客要求取消", "  顾客要求取消  "},
		{"超长截断到 40 字符", long, string([]rune(long)[:40]), long},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := po.Order{OrderID: 1, OrderNo: "D001", OrderStatus: 5, CancelReason: c.in, CreateBy: "internal", UpdateBy: "internal"}
			got := FromOrderCustomer(p, nil, false)
			if got.CancelReason != c.want {
				t.Errorf("顾客端 CancelReason = %q, want %q", got.CancelReason, c.want)
			}
			// 敏感字段仍然剥离
			if got.CreateBy != "" || got.UpdateBy != "" || got.TransactionID != "" || got.SettleOperator != "" {
				t.Errorf("内部字段应被剥离, got %+v", got)
			}
			// 管理端口径不受影响:原因原样保留
			if admin := FromOrder(p, nil, false); admin.CancelReason != c.wantFull {
				t.Errorf("管理端 CancelReason = %q, want %q", admin.CancelReason, c.wantFull)
			}
		})
	}
}

// TestDtoPaymentMapping 校验支付流水映射:金额单位为分,不做单位转换。
func TestDtoPaymentMapping(t *testing.T) {
	p := po.Payment{PaymentID: 9, OrderNo: "D123", Channel: "wxpay", ChannelTradeNo: "T456", Amount: 1234, Status: 1, PrepayID: "P789", NotifyTime: "2024-01-01 00:00:01", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:02"}
	want := Payment{PaymentID: 9, OrderNo: "D123", Channel: "wxpay", ChannelTradeNo: "T456", Amount: 1234, Status: 1, PrepayID: "P789", NotifyTime: "2024-01-01 00:00:01", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:02"}
	if got := FromPayment(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPayment() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoRefundMapping 校验退款记录映射:金额单位为分,不做单位转换。
func TestDtoRefundMapping(t *testing.T) {
	p := po.Refund{RefundID: 5, OrderID: 8, OrderNo: "D123", PaymentID: 9, RefundNo: "R1", Channel: "wxpay", ChannelRefundNo: "CR1", Amount: 500, Status: 1, Reason: "退菜", Operator: "admin", FailReason: "", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:01", IsDuplicate: 1}
	want := Refund{RefundID: 5, OrderID: 8, OrderNo: "D123", PaymentID: 9, RefundNo: "R1", Channel: "wxpay", ChannelRefundNo: "CR1", Amount: 500, Status: 1, Reason: "退菜", Operator: "admin", FailReason: "", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:01", IsDuplicate: 1}
	if got := FromRefund(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromRefund() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoOperLogMapping 校验操作日志字段映射。
func TestDtoOperLogMapping(t *testing.T) {
	p := po.OperLog{LogID: 1, Module: "订单管理", BusinessType: "update", Action: "修改菜品", Method: "POST /api/admin/dish/update", RequestURL: "/api/admin/dish/update?id=1", OperatorID: 2, Operator: "张三", OperatorRole: "管理员", OperIP: "127.0.0.1", TargetType: "dish", TargetID: "1", OperParam: `{"dishName":"x"}`, Detail: "修改菜品名称", Status: 1, ErrorMsg: "", CostMs: 12, CreateTime: "2024-01-01 00:00:00"}
	want := OperLog{LogID: 1, Module: "订单管理", BusinessType: "update", Action: "修改菜品", Method: "POST /api/admin/dish/update", RequestURL: "/api/admin/dish/update?id=1", OperatorID: 2, Operator: "张三", OperatorRole: "管理员", OperIP: "127.0.0.1", TargetType: "dish", TargetID: "1", OperParam: `{"dishName":"x"}`, Detail: "修改菜品名称", Status: 1, ErrorMsg: "", CostMs: 12, CreateTime: "2024-01-01 00:00:00"}
	if got := FromOperLog(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromOperLog() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoPrintJobMapping 校验打印任务字段映射与 payload 透传。
func TestDtoPrintJobMapping(t *testing.T) {
	p := po.PrintJob{JobID: 1, PrinterID: 2, PrinterName: "后厨", PrinterType: 1, IP: "192.168.1.10", Port: 9100, DocType: "kitchen", OrderID: 3, OrderNo: "D123", TableNo: "A01", Copies: 2, PrintLogID: 4, DeliveryID: "DLV1", Payload: "line1\nline2", Status: 0, Attempts: 1, LastError: "", ClaimedBy: "", ClaimTime: "", NextTryTime: "2024-01-01 00:00:00", TriggerBy: "order", Operator: "admin", CreateTime: "2024-01-01 00:00:00", DoneTime: ""}
	want := PrintJob{JobID: 1, PrinterID: 2, PrinterName: "后厨", PrinterType: 1, IP: "192.168.1.10", Port: 9100, DocType: "kitchen", OrderID: 3, OrderNo: "D123", TableNo: "A01", Copies: 2, PrintLogID: 4, DeliveryID: "DLV1", Payload: "line1\nline2", Status: 0, Attempts: 1, LastError: "", ClaimedBy: "", ClaimTime: "", NextTryTime: "2024-01-01 00:00:00", TriggerBy: "order", Operator: "admin", CreateTime: "2024-01-01 00:00:00", DoneTime: ""}
	if got := FromPrintJob(p); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPrintJob() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoPrintJobWithPayload 校验代理取单出参组装:payload 由调用方传入。
func TestDtoPrintJobWithPayload(t *testing.T) {
	p := po.PrintJob{JobID: 1, PrinterID: 2, PrinterName: "后厨", IP: "192.168.1.10", Port: 9100, Copies: 2, DocType: "kitchen", OrderNo: "D123", TableNo: "A01", DeliveryID: "DLV1"}
	want := AgentJob{JobID: 1, PrinterID: 2, PrinterName: "后厨", IP: "192.168.1.10", Port: 9100, Copies: 2, DocType: "kitchen", OrderNo: "D123", TableNo: "A01", DeliveryID: "DLV1", Payload: "base64payload"}
	if got := FromPrintJobWithPayload(p, "base64payload"); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPrintJobWithPayload() = %+v, want %+v", got, want)
	}
}

// TestDtoPrintAgentMapping 校验打印代理映射:PrinterIDList 为运行时补算,不入库。
func TestDtoPrintAgentMapping(t *testing.T) {
	p := po.PrintAgent{AgentID: 1, AgentName: "门店代理", TokenHash: "hash", TokenHint: "abcd", PrinterIDs: "1,2", Status: 1, LastSeen: "2024-01-01 00:00:00", LastReport: "", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00"}
	want := PrintAgent{AgentID: 1, AgentName: "门店代理", TokenHash: "hash", TokenHint: "abcd", PrinterIDs: "1,2", Status: 1, LastSeen: "2024-01-01 00:00:00", LastReport: "", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00", PrinterIDList: []int{1, 2}}
	if got := FromPrintAgent(p, []int{1, 2}); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPrintAgent() = %+v, want %+v", got, want)
	}
	// PrinterIDList 不入库,ToPO 后该字段消失。
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoPrinterMapping 校验打印机映射:CategoryIDList/OnlineStatus/CategoryNames 不入库。
func TestDtoPrinterMapping(t *testing.T) {
	p := po.Printer{PrinterID: 1, PrinterName: "后厨打印机", PrinterType: 1, Provider: "feie", IP: "192.168.1.10", Port: 9100, FeieSN: "SN1", PaperWidth: 48, Copies: 2, CategoryIDs: "1,2", Status: 1, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00"}
	names := dmStr("热菜,凉菜")
	want := Printer{PrinterID: 1, PrinterName: "后厨打印机", PrinterType: 1, Provider: "feie", IP: "192.168.1.10", Port: 9100, FeieSN: "SN1", PaperWidth: 48, Copies: 2, CategoryIDs: "1,2", Status: 1, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00", CategoryIDList: []int{1, 2}, OnlineStatus: "在线", CategoryNames: names}
	if got := FromPrinter(p, []int{1, 2}, "在线", names); !reflect.DeepEqual(got, want) {
		t.Errorf("FromPrinter() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoRoleMapping 校验角色映射:PermList/UserCount 不入库。
func TestDtoRoleMapping(t *testing.T) {
	p := po.Role{RoleID: 1, RoleKey: "admin", RoleName: "管理员", Perms: "dish:list,dish:edit", DataScope: "all", IsBuiltin: 1, SortOrder: 1, Status: 1, DelFlag: "0", CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00", Remark: "内置"}
	want := Role{RoleID: 1, RoleKey: "admin", RoleName: "管理员", Perms: "dish:list,dish:edit", DataScope: "all", IsBuiltin: 1, SortOrder: 1, Status: 1, DelFlag: "0", PermList: []string{"dish:list", "dish:edit"}, CreateTime: "2024-01-01 00:00:00", UpdateTime: "2024-01-01 00:00:00", Remark: "内置", UserCount: 3}
	if got := FromRole(p, []string{"dish:list", "dish:edit"}, 3); !reflect.DeepEqual(got, want) {
		t.Errorf("FromRole() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoUserMapping 校验员工映射:RoleKey/RoleName/Perms 为联表字段,不入库。
func TestDtoUserMapping(t *testing.T) {
	p := po.User{UserID: 1, Username: "zhangsan", RealName: "张三", RoleID: 1, Phone: "13800138000", Status: 1, LastLoginTime: "2024-01-01 00:00:00", LastLoginIP: "127.0.0.1", LoginCount: 5, PwdUpdateTime: "2024-01-01 00:00:00", DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-01 00:00:00", Remark: "备注"}
	want := User{UserID: 1, Username: "zhangsan", RealName: "张三", RoleID: 1, Phone: "13800138000", Status: 1, LastLoginTime: "2024-01-01 00:00:00", LastLoginIP: "127.0.0.1", LoginCount: 5, PwdUpdateTime: "2024-01-01 00:00:00", DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-01 00:00:00", Remark: "备注", RoleKey: "admin", RoleName: "管理员", Perms: []string{"dish:list"}}
	if got := FromUser(p, "admin", "管理员", []string{"dish:list"}); !reflect.DeepEqual(got, want) {
		t.Errorf("FromUser() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}

// TestDtoDishMapping 校验菜品映射:CategoryName/Specs 为运行时补算,不入库。
func TestDtoDishMapping(t *testing.T) {
	p := po.Dish{DishID: 1, CategoryID: 2, DishName: "宫保鸡丁", DishImage: "a.png", Description: "微辣", Status: 1, SortOrder: 1, DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-01 00:00:00", Remark: dmStr("招牌")}
	want := Dish{DishID: 1, CategoryID: 2, CategoryName: "热菜", DishName: "宫保鸡丁", DishImage: "a.png", Description: "微辣", Status: 1, SortOrder: 1, DelFlag: "0", CreateBy: "admin", CreateTime: "2024-01-01 00:00:00", UpdateBy: "admin", UpdateTime: "2024-01-01 00:00:00", Remark: dmStr("招牌"), Specs: []Spec{{SpecID: 1, DishID: 1, SpecName: "大份", Price: 28}}}
	if got := FromDish(p, "热菜", []Spec{{SpecID: 1, DishID: 1, SpecName: "大份", Price: 28}}); !reflect.DeepEqual(got, want) {
		t.Errorf("FromDish() = %+v, want %+v", got, want)
	}
	if got := want.ToPO(); !reflect.DeepEqual(got, p) {
		t.Errorf("ToPO() = %+v, want %+v", got, p)
	}
}
