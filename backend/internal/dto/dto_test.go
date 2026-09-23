package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"dining-system/internal/po"
)

// TestSpecPriceRoundTrip 校验 Spec 金额往返:元↔分转换只在 FromPO/ToPO 内发生。
func TestSpecPriceRoundTrip(t *testing.T) {
	if got := po.ToCents(66.66); got != 6666 {
		t.Fatalf("ToCents(66.66) = %d, want 6666", got)
	}
	if got := (Spec{Price: 66.66}).ToPO().Price; got != 6666 {
		t.Errorf("Spec{Price:66.66}.ToPO().Price = %d, want 6666", got)
	}
	if got := FromSpec(po.Spec{Price: 6666}).Price; got != 66.66 {
		t.Errorf("FromSpec(po.Spec{Price:6666}).Price = %v, want 66.66", got)
	}
}

// TestOrderTotalAmountRoundTrip 校验 Order 金额字段(元↔分)往返。
func TestOrderTotalAmountRoundTrip(t *testing.T) {
	if got := (Order{TotalAmount: 66.66}).ToPO().TotalAmount; got != 6666 {
		t.Errorf("Order{TotalAmount:66.66}.ToPO().TotalAmount = %d, want 6666", got)
	}
	if got := FromOrder(po.Order{TotalAmount: 6666}, nil, false).TotalAmount; got != 66.66 {
		t.Errorf("FromOrder(po.Order{TotalAmount:6666}).TotalAmount = %v, want 66.66", got)
	}
}

// TestFromOrderDerivesAndAssembles 校验 FromOrder 的短号派生与 Items/PendingUrge 组装。
func TestFromOrderDerivesAndAssembles(t *testing.T) {
	orderNo := "D20260921120000abcdef012345"
	items := []OrderItem{{ItemID: 1, DishName: "测试菜"}}
	got := FromOrder(po.Order{OrderNo: orderNo}, items, true)
	if got.ShortNo != po.ShortOrderNo(orderNo) {
		t.Errorf("ShortNo = %q, want %q", got.ShortNo, po.ShortOrderNo(orderNo))
	}
	if len(got.Items) != 1 || got.Items[0].DishName != "测试菜" {
		t.Errorf("Items 未透传形参: %+v", got.Items)
	}
	if !got.PendingUrge {
		t.Errorf("PendingUrge = false, want true")
	}
}

// TestFromPrintLogDerivesShortNo 校验 PrintLog 的短号派生。
func TestFromPrintLogDerivesShortNo(t *testing.T) {
	orderNo := "D20260921120000abcdef012345"
	got := FromPrintLog(po.PrintLog{OrderNo: orderNo})
	if got.ShortNo != po.ShortOrderNo(orderNo) {
		t.Errorf("ShortNo = %q, want %q", got.ShortNo, po.ShortOrderNo(orderNo))
	}
}

// TestFromUserJoinsPassthrough 校验 User 联表字段透传。
func TestFromUserJoinsPassthrough(t *testing.T) {
	got := FromUser(po.User{Username: "zhangsan"}, "admin", "管理员", []string{"dish:list"})
	if got.RoleKey != "admin" || got.RoleName != "管理员" {
		t.Errorf("roleKey/roleName 未透传: %+v", got)
	}
	if len(got.Perms) != 1 || got.Perms[0] != "dish:list" {
		t.Errorf("perms 未透传: %+v", got.Perms)
	}
	if got.Username != "zhangsan" {
		t.Errorf("Username = %q, want zhangsan", got.Username)
	}
}

// TestSettingsJSONContainsAgentLatestVersion 校验 Settings 序列化含 agent_latest_version。
func TestSettingsJSONContainsAgentLatestVersion(t *testing.T) {
	b, err := json.Marshal(Settings{AgentLatestVersion: "1.2.3"})
	if err != nil {
		t.Fatalf("json.Marshal(Settings) error: %v", err)
	}
	if !strings.Contains(string(b), `"agent_latest_version"`) {
		t.Errorf("序列化结果未包含 agent_latest_version: %s", string(b))
	}
}
