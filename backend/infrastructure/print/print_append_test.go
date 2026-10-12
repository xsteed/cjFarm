package print

// ============================================================================
// 加菜重出小票(print_guest_on_append)
//
// 加菜时后厨拿「加菜单」(只列新菜),小票机重出一张整单最新金额的食客小票。
// 这里验证两件事:配置开关的默认值与三态语义;加菜触发的作业渲染的是
// 整单口径(而非只列新菜的片段单 —— 那会和金额区对不上,也无法被补打重放)。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/domain/po"
)

// TestGuestAppendConfigGate 开关默认开;只有明确写 "0" 才关(与 seed 出厂默认一致)。
func TestGuestAppendConfigGate(t *testing.T) {
	setupTestDB(t)

	if !guestPrintOnAppend() {
		t.Fatal("默认应开启加菜重出小票")
	}
	setPrintSetting(t, "print_guest_on_append", "0")
	if guestPrintOnAppend() {
		t.Fatal("关闭后不应再打印")
	}
	setPrintSetting(t, "print_guest_on_append", "1")
	if !guestPrintOnAppend() {
		t.Fatal("重新开启失败")
	}
}

// TestGuestOnOrderConfigGate 下单点菜单开关:默认开,写 "0" 关,重新写 "1" 开。
func TestGuestOnOrderConfigGate(t *testing.T) {
	setupTestDB(t)

	if !guestPrintOnOrder() {
		t.Fatal("默认应开启下单点菜单")
	}
	setPrintSetting(t, "print_guest_on_order", "0")
	if guestPrintOnOrder() {
		t.Fatal("关闭后不应再打印")
	}
	setPrintSetting(t, "print_guest_on_order", "1")
	if !guestPrintOnOrder() {
		t.Fatal("重新开启失败")
	}
}

// TestGuestAppendJobRendersAppendOnly 加菜触发的作业按 docType=guest_append 渲染:
// 只列本次新增菜品 + 一行整单「当前合计」,不重放整单菜品与金额分项。
func TestGuestAppendJobRendersAppendOnly(t *testing.T) {
	setupTestDB(t)
	o, _ := settledOrder()
	printers := []po.Printer{
		{PrinterName: "前台小票机", PrinterType: po.PrinterTypeGuest, PaperWidth: 48},
		{PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48},
	}

	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocGuestAppend, "", po.PrintTriggerAppend, "")
	if len(jobs) != 1 {
		t.Fatalf("加菜小票只应发给小票机, got %d 个作业", len(jobs))
	}
	got := strings.Join(jobs[0].render(), "\n")
	for _, want := range []string{"【加菜小票】", "凉拌青瓜", "海鲜炒饭", "当前合计", "84.00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("加菜小票缺少要素 %q:\n%s", want, got)
		}
	}
	// 整单口径的内容不应出现在加菜小票上:标题、金额分项与「合计」措辞。
	for _, bad := range []string{"【食客小票】", "【加菜单】", "优惠", "赠送", " 合计"} {
		if strings.Contains(got, bad) {
			t.Fatalf("加菜小票不应包含整单要素 %q:\n%s", bad, got)
		}
	}
}

// TestDocTypeNormalizeKeepsAppendForm 补打/预览必须保留 guest_append 形态:
// 抹成 guest 会让重出来的票变成另一张(标题「【食客小票】」、金额区整单口径),
// 与打印记录里的「加菜小票」对不上。
func TestDocTypeNormalizeKeepsAppendForm(t *testing.T) {
	if got := normalizeDocType(po.PrintDocGuestAppend); got != po.PrintDocGuestAppend {
		t.Fatalf("guest_append 应保留原形态, got %q", got)
	}
	if got := normalizeDocType(po.PrintDocKitchen); got != po.PrintDocKitchen {
		t.Fatalf("kitchen 补打/预览应保持 kitchen, got %q", got)
	}
	if got := normalizeDocType("whatever"); got != po.PrintDocGuest {
		t.Fatalf("未知类型应回落为 guest, got %q", got)
	}
}

// TestGuestAppendPreviewKeepsAppendForm 加菜小票的预览必须仍是加菜小票形态
// (标题 + 一行「当前合计」),而不是整单食客小票。
func TestGuestAppendPreviewKeepsAppendForm(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuestAppend, po.PrintTriggerAppend)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	got := flattenPreview(chunks)
	for _, want := range []string{"【加菜小票】", "当前合计"} {
		if !strings.Contains(got, want) {
			t.Fatalf("加菜小票预览缺少 %q:\n%s", want, got)
		}
	}
	// 整单口径的要素不该出现:那是另一张票。
	for _, bad := range []string{"【食客小票】", "茶位费"} {
		if strings.Contains(got, bad) {
			t.Fatalf("加菜小票预览不应包含整单要素 %q:\n%s", bad, got)
		}
	}
}

// TestReprintKitchenTitleFollowsTrigger 厨房单抬头随触发场景变:
// 加菜那张出的是「【加菜单】」,补打必须沿用 —— 否则后厨会把重打的票
// 当成一张全新整单,已经上过的菜可能被重做一遍。
func TestReprintKitchenTitleFollowsTrigger(t *testing.T) {
	if got := kitchenTitleFor(po.PrintTriggerAppend); got != "【加菜单】" {
		t.Fatalf("加菜场景的厨房单抬头应为【加菜单】, got %q", got)
	}
	for _, tr := range []string{po.PrintTriggerOrder, po.PrintTriggerSettle, po.PrintTriggerReprint, po.PrintTriggerTest} {
		if got := kitchenTitleFor(tr); got != "【厨房单】" {
			t.Fatalf("触发场景 %s 的厨房单抬头应为【厨房单】, got %q", tr, got)
		}
	}
}
