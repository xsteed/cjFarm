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
	res, err := PreviewPrinterTicket(printerID, po.PrintTriggerOrder)
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
	res, err = PreviewPrinterTicket(printerID, po.PrintTriggerOrder)
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
	res, err = PreviewPrinterTicket(printerID, po.PrintTriggerOrder)
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

	res, err := PreviewPrinterTicket(printerID, po.PrintTriggerOrder)
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
	if _, err := PreviewPrinterTicket(missing, po.PrintTriggerOrder); err == nil {
		t.Fatal("分类全不匹配时应报错(真实打印同样不会出纸)")
	}
}

// TestPreviewPrinterTicketGuest 顾客小票机:整单一张点菜单形态,不受打印粒度影响。
func TestPreviewPrinterTicketGuest(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)

	res, err := PreviewPrinterTicket(printerID, po.PrintTriggerOrder)
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

// TestPreviewPrinterTicketGuestSettle 顾客小票机的第二种形态:结账小票。
// 结账与下单出的是两张不同的票(标题、优惠行、支付方式/结账时间),
// 只预览点菜单会让商家以为结账时也只出那张。
func TestPreviewPrinterTicketGuestSettle(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	res, err := PreviewPrinterTicket(printerID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.DocType != po.PrintDocGuest || res.TriggerBy != po.PrintTriggerSettle {
		t.Fatalf("应返回 guest/settle, got %s/%s", res.DocType, res.TriggerBy)
	}
	if len(res.Chunks) != 1 {
		t.Fatalf("结账小票应整单一张, got %d", len(res.Chunks))
	}
	got := strings.Join(res.Chunks[0], "\n")
	for _, want := range []string{"【结账小票】", "优惠", "合计", "支付方式", "结账时间", "谢谢惠顾"} {
		if !strings.Contains(got, want) {
			t.Fatalf("结账小票预览缺少 %q:\n%s", want, got)
		}
	}
	// 两种形态不能串味:结账票不出「当前合计」,也不该退回点菜单标题。
	for _, bad := range []string{"当前合计", "【顾客小票】"} {
		if strings.Contains(got, bad) {
			t.Fatalf("结账小票预览不应包含 %q:\n%s", bad, got)
		}
	}
	// 直连通道不参与「小票字体放大」,预览不应声称放大了。
	if res.LargeFont {
		t.Fatal("直连打印机的结账预览不应标记为放大")
	}
}

// TestPreviewPrinterTicketSettleLargeFont 结账小票预览要如实反映「小票字体放大」:
// 飞鹅机标记放大且整张都是放大行;直连机保持原版式(该开关只对飞鹅云落地)。
func TestPreviewPrinterTicketSettleLargeFont(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_guest_large_font", "1")

	feieID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderFeie)
	res, err := PreviewPrinterTicket(feieID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if !res.LargeFont || !res.LargeFontEnabled || !res.LargeFontSupported {
		t.Fatalf("飞鹅结账小票应标记为放大(开关开+通道支持), got %v/%v/%v",
			res.LargeFont, res.LargeFontEnabled, res.LargeFontSupported)
	}
	for _, l := range res.Chunks[0] {
		if _, big := bigTextOf(l); !big {
			t.Fatalf("放大后的结账小票每行都应是放大行, got %q", l)
		}
	}

	tcpID := insertPreviewPrinterAs(t, po.PrinterTypeGuest, "", 48, po.PrinterProviderTCP)
	res, err = PreviewPrinterTicket(tcpID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.LargeFont {
		t.Fatal("直连通道不受放大开关影响")
	}
	// 前端靠这两个口径说明「为什么没放大」:开关是开的,但通道不支持。
	if !res.LargeFontEnabled || res.LargeFontSupported {
		t.Fatalf("直连应「开关开、通道不支持」, got enabled=%v supported=%v",
			res.LargeFontEnabled, res.LargeFontSupported)
	}
	for _, l := range res.Chunks[0] {
		if _, ok := SplitBigLeftLine(l); ok {
			t.Fatalf("直连结账小票不应整张放大, got %q", l)
		}
	}

	// 开关关掉时:飞鹅机也不放大,且三个口径合起来能区分「没开」与「通道不支持」。
	setPrintSetting(t, "print_guest_large_font", "0")
	res, err = PreviewPrinterTicket(feieID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.LargeFont || res.LargeFontEnabled || !res.LargeFontSupported {
		t.Fatalf("开关关闭时飞鹅机应是「未放大/未开启/支持」, got %v/%v/%v",
			res.LargeFont, res.LargeFontEnabled, res.LargeFontSupported)
	}
}

// TestTestPrintLinesMatchPreview 「测试打印」出的纸必须就是票据预览里看到的内容:
// 顾客小票机 = 点菜单 + 结账小票(两张,中间切纸),不再出通道信息页。
func TestTestPrintLinesMatchPreview(t *testing.T) {
	setupTestDB(t)
	guestID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)
	guest, err := service.LoadPrinter(guestID)
	if err != nil {
		t.Fatalf("加载打印机失败: %v", err)
	}

	lines, labels, err := testPrintLines(guest)
	if err != nil {
		t.Fatalf("testPrintLines 失败: %v", err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"【顾客小票】", "【结账小票】"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("测试打印缺少 %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "测试打印") || strings.Contains(joined, "通道: ") {
		t.Fatalf("测试打印不应再出通道信息页:\n%s", joined)
	}
	// 两张票之间必须切一刀,否则纸面上分不清是两张。
	if got := strings.Count(joined, cutMarkPrefix); got != 1 {
		t.Fatalf("点菜单与结账小票之间应有 1 个切纸行, got %d:\n%s", got, joined)
	}
	// 飞鹅通道落地后必须是 <CUT> 标签,而不是被转义成正文打出来。
	if content := strings.Join(splitFeieContent(lines), ""); !strings.Contains(content, feieCutTag) {
		t.Fatalf("飞鹅内容应在两张票之间落地 %s:\n%s", feieCutTag, content)
	}
	if len(labels) != 2 || labels[0] != "顾客小票" || labels[1] != "结账小票" {
		t.Fatalf("日志标识应与票面一致, got %v", labels)
	}

	// 逐行等于预览:两段预览拼起来就是出纸内容(预览即所得)。
	order, err := PreviewPrinterTicket(guestID, po.PrintTriggerOrder)
	if err != nil {
		t.Fatalf("预览点菜单失败: %v", err)
	}
	settle, err := PreviewPrinterTicket(guestID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("预览结账小票失败: %v", err)
	}
	want := append([]string{}, order.Chunks[0]...)
	want = append(want, cutMarkLine())
	want = append(want, settle.Chunks[0]...)
	if joined != strings.Join(want, "\n") {
		t.Fatalf("测试打印内容与预览不一致:\n打印:\n%s\n预览:\n%s", joined, strings.Join(want, "\n"))
	}
}

// TestTestPrintLinesKitchenFollowsSplitMode 厨房机的测试打印按打印粒度出纸:
// 一菜一单下 3 道菜 = 3 张,中间 2 个切纸行。
func TestTestPrintLinesKitchenFollowsSplitMode(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 48)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	p, err := service.LoadPrinter(printerID)
	if err != nil {
		t.Fatalf("加载打印机失败: %v", err)
	}

	lines, labels, err := testPrintLines(p)
	if err != nil {
		t.Fatalf("testPrintLines 失败: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if got := strings.Count(joined, cutMarkPrefix); got != 2 {
		t.Fatalf("3 张夹单之间应有 2 个切纸行, got %d:\n%s", got, joined)
	}
	if len(labels) != 3 || labels[0] != "【厨房单】1/3" {
		t.Fatalf("日志标识应带拆单序号, got %v", labels)
	}
	for _, want := range []string{"【厨房单】1/3", "【厨房单】3/3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("测试打印缺少 %q:\n%s", want, joined)
		}
	}
	// 结账不出厨房单:厨房机只有厨房单。
	if strings.Contains(joined, "结账小票") {
		t.Fatalf("厨房机不应出结账小票:\n%s", joined)
	}
}

// TestPreviewPrinterTicketKitchenIgnoresSettle 结账不出厨房单(见 PrintGuestTicket):
// 厨房机即使请求 settle 也按下单形态预览,不能展示一张真实永远不会出现的票。
func TestPreviewPrinterTicketKitchenIgnoresSettle(t *testing.T) {
	setupTestDB(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeKitchen, "", 48)

	res, err := PreviewPrinterTicket(printerID, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewPrinterTicket 失败: %v", err)
	}
	if res.DocType != po.PrintDocKitchen || res.TriggerBy != po.PrintTriggerOrder {
		t.Fatalf("厨房机应强制按下单形态, got %s/%s", res.DocType, res.TriggerBy)
	}
	if got := strings.Join(res.Chunks[0], "\n"); !strings.Contains(got, "【厨房单】") {
		t.Fatalf("厨房机预览应是厨房单:\n%s", got)
	}
}
