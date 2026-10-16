package print

// ============================================================================
// 打印机票据预览(PreviewPrinterTicket)
//
// 打印机管理页的「票据预览」必须与真实出纸一致:
//   - 纸宽按该机配置;
//   - 厨房单按 print_kitchen_split_mode 拆张,一段 = 后厨收到的一张纸;
//   - 「负责分类」筛选生效,一道都不匹配时报错(真实打印同样不出纸);
//   - 顾客小票整单一张,渲染点菜单形态。
// ============================================================================

import (
	"strconv"
	"strings"
	"testing"

	"dining-system/application/service"
	"dining-system/domain/po"
)

// TestPreviewPrinterTicketKitchenFollowsSplitMode 厨房单预览按打印粒度拆张:
// 一菜一单下示例 3 道菜 = 3 段,各带序号标题与放大菜名,与真实出纸一致。
func TestPreviewPrinterTicketKitchenFollowsSplitMode(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 48)

	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	res, err := PreviewPrinterTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.DocType != po.PrintDocKitchen || res.SplitMode != po.KitchenSplitItem {
		t.Fatalf("应返回 kitchen/item, got %s/%s", res.DocType, res.SplitMode)
	}
	if res.LineWidth != 48 {
		t.Fatalf("80mm 行宽应为 48, got %d", res.LineWidth)
	}
	if len(res.Chunks) != 3 {
		t.Fatalf("一菜一单应预览 3 张, got %d", len(res.Chunks))
	}
	for i, want := range []string{"【厨房单】1/3", "【厨房单】2/3", "【厨房单】3/3"} {
		c := res.Chunks[i]
		if got := strings.Join(c, "\n"); !strings.Contains(got, want) {
			t.Fatalf("第 %d 张应带序号标题 %q:\n%s", i+1, want, got)
		}
		if len(bigLinesOf(c)) < 3 { // 店名 + 桌号 + 菜名至少三行放大
			t.Fatalf("第 %d 张菜名应放大, got %v:\n%s", i+1, bigLinesOf(c), strings.Join(c, "\n"))
		}
	}

	// 按份拆:示例 2+1+1=4 份 → 4 张,每张 x1。
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)
	res, err = PreviewPrinterTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if len(res.Chunks) != 4 || res.SplitMode != po.KitchenSplitPortion {
		t.Fatalf("按份拆应预览 4 张(split=%s), got %d", res.SplitMode, len(res.Chunks))
	}
	for i, c := range res.Chunks {
		if got := strings.Join(c, "\n"); !strings.Contains(got, "x1") {
			t.Fatalf("按份拆第 %d 张应是单份:\n%s", i+1, got)
		}
	}

	// 整单:1 张,带份数汇总,不带序号。
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitWhole)
	res, err = PreviewPrinterTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if len(res.Chunks) != 1 {
		t.Fatalf("整单应只预览 1 张, got %d", len(res.Chunks))
	}
	if got := strings.Join(res.Chunks[0], "\n"); !strings.Contains(got, "共 3 道 4 份") || strings.Contains(got, "1/1") {
		t.Fatalf("整单应带份数汇总且不带序号:\n%s", got)
	}
}

// TestPreviewPrinterTicketCategoryFilter 「负责分类」在预览里生效:
// 只授权主食机的示例预览只含海鲜炒饭;授权分类与示例菜品全不匹配时报错
// (真实下单时这台机器同样一道菜都分不到)。
func TestPreviewPrinterTicketCategoryFilter(t *testing.T) {
	setupTestDB(t)

	// 主食分类 = 种子分类第 5 个(凉拌青瓜=凉菜素菜、海鲜炒饭=主食、柴火土鸡=特色农家菜)。
	var stapleID int
	cats, err := service.ListCategories()
	if err != nil || len(cats) == 0 {
		t.Fatalf("测试库应已种子分类: %v", err)
	}
	for _, c := range cats {
		if c.CategoryName == "主食" {
			stapleID = c.CategoryID
		}
	}
	if stapleID == 0 {
		t.Fatalf("测试库缺少种子分类「主食」")
	}
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, strconv.Itoa(stapleID), 48)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)

	res, err := PreviewPrinterTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if len(res.Chunks) != 1 {
		t.Fatalf("主食机一菜一单应只预览 1 张(海鲜炒饭), got %d", len(res.Chunks))
	}
	got := strings.Join(res.Chunks[0], "\n")
	if !strings.Contains(got, "海鲜炒饭") || strings.Contains(got, "凉拌青瓜") || strings.Contains(got, "柴火土鸡") {
		t.Fatalf("主食机只应预览海鲜炒饭:\n%s", got)
	}

	// 授权一个示例菜品没有的分类:预览报错而不是出空白单。
	missing := insertPreviewPrinter(t, po.PrinterTypeKitchen, "999", 48)
	if _, err := PreviewPrinterTicket(missing); err == nil {
		t.Fatal("分类全不匹配时应报错(真实打印同样不会出纸)")
	}
}

// TestPreviewPrinterTicketGuest 顾客小票机:整单一张点菜单形态,不受打印粒度影响。
func TestPreviewPrinterTicketGuest(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)

	res, err := PreviewPrinterTicket(printerID)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.DocType != po.PrintDocGuest || res.SplitMode != "" {
		t.Fatalf("guest 预览应返回 guest/空粒度, got %s/%s", res.DocType, res.SplitMode)
	}
	if len(res.Chunks) != 1 {
		t.Fatalf("顾客小票应整单一张, got %d", len(res.Chunks))
	}
	got := strings.Join(res.Chunks[0], "\n")
	for _, want := range []string{"【顾客小票】", "凉拌青瓜", "茶位费(3人)", "当前合计", "备注: 少辣"} {
		if !strings.Contains(got, want) {
			t.Fatalf("点菜单预览缺少 %q:\n%s", want, got)
		}
	}
}
