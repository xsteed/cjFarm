package print

// ============================================================================
// 厨房单打印粒度(print_kitchen_split_mode)
//
// 三档粒度对齐成熟 POS 的「整单 / 一菜一单 / 按份数拆」:
//   - whole   整单一张(默认,与历史行为一致);
//   - item    一菜一单,同一道菜合并为一张(标 xN);
//   - portion 一菜一单且按份数拆,点 3 份出 3 张(每张 x1,金额折算单份)。
// 拆成多张时标题带序号,后厨据此确认出纸是否齐全;顾客小票任何档位下都整单一张。
// ============================================================================

import (
	"bytes"
	"strings"
	"testing"

	"dining-system/domain/po"
)

// TestKitchenSplitModeWhitelistAndDefault 粒度配置的读取语义:
// 默认(或键缺失)按整单;白名单内的取值原样返回;脏值一律回落整单。
func TestKitchenSplitModeWhitelistAndDefault(t *testing.T) {
	setupTestDB(t)

	if got := kitchenSplitMode(); got != po.KitchenSplitWhole {
		t.Fatalf("默认粒度应为整单, got %q", got)
	}
	for _, m := range []string{po.KitchenSplitItem, po.KitchenSplitPortion, po.KitchenSplitWhole} {
		setPrintSetting(t, "print_kitchen_split_mode", m)
		if got := kitchenSplitMode(); got != m {
			t.Fatalf("粒度 %q 应原样生效, got %q", m, got)
		}
	}
	// 脏值/老版本残留:不能因为一个坏配置把后厨单拆成没人预期的碎票。
	for _, bad := range []string{"1", "split", "item,portion", ""} {
		setPrintSetting(t, "print_kitchen_split_mode", bad)
		if got := kitchenSplitMode(); got != po.KitchenSplitWhole {
			t.Fatalf("非法粒度 %q 应回落整单, got %q", bad, got)
		}
	}
}

// TestKitchenSplitWholeKeepsSingleTicket 整单档:一台机器一张票、不带序号(序号 1/1 纯废纸)。
func TestKitchenSplitWholeKeepsSingleTicket(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitWhole)
	o, _ := sampleOrder()
	printers := []po.Printer{{PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48}}

	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 1 {
		t.Fatalf("整单档应只出 1 张, got %d", len(jobs))
	}
	got := strings.Join(jobs[0].render(), "\n")
	if strings.Contains(got, "1/1") {
		t.Fatalf("单张票据不应带序号:\n%s", got)
	}
	for _, want := range []string{"凉拌青瓜", "海鲜炒饭", "共 2 道 3 份"} {
		if !strings.Contains(got, want) {
			t.Fatalf("整单票应含全部菜品与份数汇总 %q:\n%s", want, got)
		}
	}
}

// TestKitchenTicketMatureLayout 厨打单版式(成熟 POS 惯例):
// 票头短号 + 时间省到分;整单逐道编号 + 份数汇总;拆单不编号不汇总、菜名放大。
func TestKitchenTicketMatureLayout(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder() // OrderNo D20260921120000abcdef → 短号 ABCDEF

	t.Run("整单:编号、汇总、短号、分时间", func(t *testing.T) {
		got := strings.Join(RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"}), "\n")
		for _, want := range []string{
			"单号: ABCDEF", "下单: 09-21 12:00",
			"1. 凉拌青瓜 (份) x2", "2. 海鲜炒饭 x1", "共 2 道 3 份",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("整单版式缺少 %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "D20260921120000abcdef") {
			t.Fatalf("厨打单不应出现完整订单号:\n%s", got)
		}
	})

	t.Run("拆单:菜名放大、不编号不汇总", func(t *testing.T) {
		lines := RenderKitchenTicket(o, items[:1], 48, KitchenTicketOptions{Title: "【厨房单】1/2", SingleDish: true})
		if !hasBigLine(lines, "凉拌青瓜 (份) x2") {
			t.Fatalf("拆单应放大菜名行, got %v:\n%s", bigLinesOf(lines), strings.Join(lines, "\n"))
		}
		got := strings.Join(lines, "\n")
		if strings.Contains(got, "1. ") || strings.Contains(got, "共 1 道") {
			t.Fatalf("拆单不应编号、不应出份数汇总:\n%s", got)
		}
	})

	t.Run("拆单但开了金额:不放大菜名,金额与菜名同行", func(t *testing.T) {
		lines := RenderKitchenTicket(o, items[:1], 48, KitchenTicketOptions{
			Title: "【厨房单】1/2", SingleDish: true, ShowPrice: true,
		})
		if hasBigLine(lines, "凉拌青瓜 (份) x2") {
			t.Fatal("开启金额时菜名不放大(放大后金额无法同行对齐)")
		}
		if got := strings.Join(lines, "\n"); !strings.Contains(got, "56.00") {
			t.Fatalf("应带出小计:\n%s", got)
		}
	})

	t.Run("拆单且菜名过长:折成多行放大不折坏", func(t *testing.T) {
		long := []po.OrderItem{{DishName: "柴火土鸡炖野生菌汤配手工现磨米粉一份超大份量", SpecName: "半只", Quantity: 1, Price: 2000, Amount: 2000}}
		lines := RenderKitchenTicket(o, long, 32, KitchenTicketOptions{Title: "【厨房单】1/1", SingleDish: true})
		// 58mm 纸双倍宽只有 16 个半角位,长菜名放不下 → 主动折成多行放大,而不是回退正常字号
		// (夹单的意义就在菜名远远可认,回退等于整张票没有重点);只有桌号一行因自身短而整行放大。
		big := bigLinesOf(lines)
		if len(big) < 2 {
			t.Fatalf("长菜名应折成多行放大, got %v", big)
		}
		for _, l := range lines {
			if displayWidth(l) > 32 {
				t.Fatalf("行超宽(%d): %q", displayWidth(l), l)
			}
		}
		for _, b := range big {
			if displayWidth(b) > 16 {
				t.Fatalf("放大行超宽(放大后 %d 格 > 32): %q", displayWidth(b)*2, b)
			}
		}
	})
}

// TestKitchenTicketRemarkBold 做法/忌口备注是后厨最容易做错菜的地方,统一加粗强调。
func TestKitchenTicketRemarkBold(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	lines := RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"})

	bold := map[string]bool{}
	for _, l := range lines {
		if text, ok := BoldText(l); ok {
			bold[strings.TrimSpace(text)] = true
		}
	}
	if !bold["* 加辣"] {
		t.Fatalf("单菜做法备注应加粗:\n%v", bold)
	}
	if !bold["整单备注: 不要香菜"] {
		t.Fatalf("整单备注应加粗:\n%v", bold)
	}
	// 普通菜名行不应加粗(否则整张票全是重点,等于没有重点)。
	if bold["1. 凉拌青瓜 (份) x2"] || bold["共 2 道 3 份"] {
		t.Fatalf("菜名/汇总行不应加粗:\n%v", bold)
	}
}

// TestKitchenWholeTicketDishRowEnlarged 整单(一张清单)的菜品行要左对齐放大:
//   - 逐条编号是给后厨划单用的,居中了编号就不在行首,故用 bigL(左对齐放大)而非 big(居中放大);
//   - 放大后每字符占两格,宽度减半,放不下时必须回退成正常字号(与店名行同一套保护)。
func TestKitchenWholeTicketDishRowEnlarged(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()

	// 80mm(48 格):放大后可用 24 格,"1. 凉拌青瓜 (份) x2" 19 格放得下。
	lines := RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"})
	enlarged := map[string]bool{}
	for _, l := range lines {
		if text, ok := SplitBigLeftLine(l); ok {
			enlarged[text] = true
			if _, isCentered := SplitBigLine(l); isCentered {
				t.Fatalf("整单菜品行不应同时被判为居中放大行: %q", text)
			}
		}
	}
	if !enlarged["1. 凉拌青瓜 (份) x2"] || !enlarged["2. 海鲜炒饭 x1"] {
		t.Fatalf("80mm 下整单菜品行应左对齐放大, got %v:\n%s", enlarged, strings.Join(lines, "\n"))
	}
	// 每行仍不得超宽:放大行按「占两格」算,店名/桌号之外的正文行保持正常字号。
	for _, l := range lines {
		text, isBig := SplitBigLine(l)
		if bigL, ok := SplitBigLeftLine(l); ok {
			text, isBig = bigL, true
		}
		if isBig && displayWidth(text) > 24 {
			t.Fatalf("放大行超宽(放大后 %d 格 > 24): %q", displayWidth(text)*2, text)
		}
	}

	// 58mm(32 格):放大后只剩 16 格 —— 逐行判断,放得下照旧放大、放不下回退正常字号。
	small := RenderKitchenTicket(o, items, 32, KitchenTicketOptions{Title: "【厨房单】"})
	if !containsLine(small, "1. 凉拌青瓜 (份) x2") { // 19 格 > 16,放不下
		t.Fatalf("58mm 下放不下的菜品行应以正常字号整行输出(而非折行):\n%s", strings.Join(small, "\n"))
	}
	for _, l := range small {
		if bigL, ok := SplitBigLeftLine(l); ok && displayWidth(bigL) > 16 {
			t.Fatalf("58mm 放大行超宽(放大后 %d 格 > 32): %q", displayWidth(bigL)*2, bigL)
		}
	}
}

// containsLine 报告渲染结果里是否存在与 text 完全相同的正文行(用于确认回退成正常字号)。
func containsLine(lines []string, text string) bool {
	for _, l := range lines {
		if t, ok := BoldText(l); ok {
			l = t
		}
		if strings.TrimSpace(l) == text {
			return true
		}
	}
	return false
}

// TestKitchenSplitItemOnePerDishWithSeq 一菜一单:每道菜一张,标题带序号,每张只含自己那道菜。
func TestKitchenSplitItemOnePerDishWithSeq(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	o, _ := sampleOrder()
	printers := []po.Printer{{PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48}}

	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("两道菜应出 2 张, got %d", len(jobs))
	}
	if jobs[0].title != "【厨房单】1/2" || jobs[1].title != "【厨房单】2/2" {
		t.Fatalf("标题应带序号, got %q / %q", jobs[0].title, jobs[1].title)
	}
	first := strings.Join(jobs[0].render(), "\n")
	if !strings.Contains(first, "凉拌青瓜") || strings.Contains(first, "海鲜炒饭") {
		t.Fatalf("第 1 张只应含凉拌青瓜:\n%s", first)
	}
	// 同一道菜点 2 份仍是一张(标 x2),不按份数拆。
	if !strings.Contains(first, "x2") {
		t.Fatalf("一菜一单应保留份数标记 x2:\n%s", first)
	}
	// 单菜备注跟着自己那张走。
	if !strings.Contains(first, "加辣") {
		t.Fatalf("单菜备注应出现在该菜的小票上:\n%s", first)
	}
	// 每张都带桌号与整单备注(夹单场景核对用)。
	for i, j := range jobs {
		line := strings.Join(j.render(), "\n")
		if !hasBigLine(j.render(), "桌号 03") {
			t.Fatalf("第 %d 张缺少放大桌号行:\n%s", i+1, line)
		}
		if !strings.Contains(line, "整单备注: 不要香菜") {
			t.Fatalf("第 %d 张应带整单备注:\n%s", i+1, line)
		}
	}
}

// TestKitchenSplitPortionExpandsQuantity 按份数拆:点 3 份(2+1)出 3 张,每张 x1。
func TestKitchenSplitPortionExpandsQuantity(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)
	setPrintSetting(t, "print_kitchen_show_price", "1") // 顺带验证单份金额折算
	o, _ := sampleOrder()
	printers := []po.Printer{{PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48}}

	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 3 {
		t.Fatalf("2 份 + 1 份应出 3 张, got %d", len(jobs))
	}
	if jobs[0].title != "【厨房单】1/3" || jobs[2].title != "【厨房单】3/3" {
		t.Fatalf("序号应为 1/3 … 3/3, got %q … %q", jobs[0].title, jobs[2].title)
	}
	for i, j := range jobs[:2] {
		got := strings.Join(j.render(), "\n")
		if !strings.Contains(got, "凉拌青瓜 (份) x1") {
			t.Fatalf("第 %d 张应是单份青瓜:\n%s", i+1, got)
		}
		// 56.00 是两份合计;单份应折算为 28.00。
		if !strings.Contains(got, "28.00") || strings.Contains(got, "56.00") {
			t.Fatalf("第 %d 张金额应折算为单份 28.00:\n%s", i+1, got)
		}
	}
	if last := strings.Join(jobs[2].render(), "\n"); !strings.Contains(last, "海鲜炒饭 x1") {
		t.Fatalf("第 3 张应是海鲜炒饭:\n%s", last)
	}
}

// TestKitchenSplitGuestNeverSplit 顾客小票任何粒度下都整单一张 —— 金额合计必须完整。
func TestKitchenSplitGuestNeverSplit(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitPortion)
	o, _ := settledOrder()
	printers := []po.Printer{
		{PrinterName: "前台小票机", PrinterType: po.PrinterTypeGuest, PaperWidth: 48},
		{PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48},
	}

	for _, docType := range []string{po.PrintDocGuest, po.PrintDocGuestAppend} {
		jobs := buildJobs(nil, nil, printers, o, sampleRows(), docType, "", po.PrintTriggerOrder, "")
		if len(jobs) != 1 || jobs[0].p.PrinterType != po.PrinterTypeGuest {
			t.Fatalf("%s 应只发小票机且只 1 张, got %d", docType, len(jobs))
		}
	}
}

// TestKitchenSplitSeqScopedPerPrinter 分类分单 + 拆单叠加:序号按「本台机器实际张数」算。
// 两台机器各负责一道菜时,两边都应是 1/1(不出现「共 3 张却只收到 1 张」的错觉)。
func TestKitchenSplitSeqScopedPerPrinter(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	o, _ := sampleOrder()
	printers := []po.Printer{
		{PrinterName: "凉菜机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48, CategoryIDs: "1"},
		{PrinterName: "热菜机", PrinterType: po.PrinterTypeKitchen, PaperWidth: 48, CategoryIDs: "6"},
	}

	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("两台机器各 1 道菜应出 2 张, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.title != "【厨房单】" {
			t.Fatalf("每台机器只出 1 张时不应带序号, got %q", j.title)
		}
	}
}

// TestKitchenSplitSecondTicketDishAlwaysBig 一菜一单的第二张票(菜名带规格,超过倍宽行宽)
// 菜名仍要放大:折成多行放大,而不是回退正常字号 —— 夹单的意义就是菜名远远可认,
// 一叠夹单里混着一张小字票,后厨根本注意不到。
// 同时验证每张厨房单票尾都垫一行空行:整单备注/分隔线不贴切纸刀口,不被拦腰截断。
func TestKitchenSplitSecondTicketDishAlwaysBig(t *testing.T) {
	setupTestDB(t)
	o, _ := sampleOrder()
	items := []po.OrderItem{
		{DishName: "凉拌青瓜", SpecName: "份", Quantity: 2, Price: 2800, Amount: 5600},
		{DishName: "香辣干锅牛肉", SpecName: "大份", Quantity: 1, Price: 5800, Amount: 5800},
	}
	// 58mm:第 1 张「凉拌青瓜 (份) x2」恰好 16 格整行放大;第 2 张 22 格超宽,折成多行放大。
	for i, it := range items {
		lines := RenderKitchenTicket(o, []po.OrderItem{it}, 32, KitchenTicketOptions{
			Title: seqTitle("【厨房单】", i, len(items)), SingleDish: true,
		})
		for _, piece := range splitBigLines(itemQtyLabel(it), 16) {
			if !hasBigLine(lines, piece) {
				t.Fatalf("第 %d 张菜名应放大(%q), got %v:\n%s", i+1, piece, bigLinesOf(lines), strings.Join(lines, "\n"))
			}
		}
		if last := lines[len(lines)-1]; last != "" {
			t.Fatalf("第 %d 张票尾应有一行空行, got last %q", i+1, last)
		}
	}
}

// TestKitchenTableNoBigLine 桌号行按放大行输出;台名与桌号重复时不另起一行,
// 台名不含桌号时仍打印(信息不丢)。放大行同时含票头店名(各票种统一)。
func TestKitchenTableNoBigLine(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()

	lines := RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"})
	// 票头店名 + 桌号共两行放大,桌号必须是其中之一。
	big := bigLinesOf(lines)
	if len(big) != 2 {
		t.Fatalf("整单厨房单应有两行放大(店名 + 桌号), got %v:\n%s", big, strings.Join(lines, "\n"))
	}
	hasTable := false
	for _, b := range big {
		if b == "桌号 03" {
			hasTable = true
		}
	}
	if !hasTable {
		t.Fatalf("厨房单桌号行应为放大行, got %v:\n%s", big, strings.Join(lines, "\n"))
	}
	// 台名「大厅03桌」已含桌号,不再另起一行重复桌号。
	if got := strings.Join(lines, "\n"); strings.Contains(got, "大厅03桌") {
		t.Fatalf("台名与桌号重复时不应另起一行:\n%s", got)
	}
	// 台名不含桌号(包间名等)时仍要打印,信息不能丢。
	packed := o
	packed.TableName = "前台打包"
	if got := strings.Join(RenderKitchenTicket(packed, items, 48, KitchenTicketOptions{Title: "【厨房单】"}), "\n"); !strings.Contains(got, "前台打包") {
		t.Fatalf("台名不含桌号时应打印台名:\n%s", got)
	}
	// 顾客小票桌号保持原有等宽行、不放大(票头店名另行放大,不计入此断言)。
	guestLines := RenderGuestTicket(o, items, 48)
	for _, b := range bigLinesOf(guestLines) {
		if strings.Contains(b, "桌号") {
			t.Fatalf("顾客小票桌号行不应放大, got %v", bigLinesOf(guestLines))
		}
	}
	if guest := strings.Join(guestLines, "\n"); !strings.Contains(guest, "桌号: 03  大厅03桌") {
		t.Fatalf("顾客小票桌号行应保持原样:\n%s", guest)
	}
}

// TestBigLineConsumedBySenders 放大行是控制行:ESC/POS 与飞鹅通道都必须消费并剥掉前缀,
// 否则不可见控制字符会被当正文打到纸上。
func TestBigLineConsumedBySenders(t *testing.T) {
	setupTestDB(t)
	line := bigMarkLine("桌号 03")

	if text, ok := SplitBigLine(line); !ok || text != "桌号 03" {
		t.Fatalf("SplitBigLine 解析失败: %q %v", text, ok)
	}
	if _, ok := SplitBigLine("桌号 03"); ok {
		t.Fatal("普通文本行不应被判为放大行")
	}

	raw := encodeESCPOS([]string{line, "凉拌青瓜 x1"})
	if !bytes.Contains(raw, []byte{0x1D, 0x21, 0x11}) {
		t.Fatalf("ESC/POS 应带 GS ! 倍宽倍高指令, bytes=%v", raw)
	}
	if !bytes.Contains(raw, []byte{0x1D, 0x21, 0x00}) {
		t.Fatal("放大后必须还原字号,否则后续行全变双倍宽")
	}
	if bytes.Contains(raw, []byte{0x02}) {
		t.Fatalf("控制前缀不应出现在出纸字节里, bytes=%v", raw)
	}

	chunks := splitFeieContent([]string{line})
	if len(chunks) != 1 || chunks[0] != "<C><B>桌号 03</B></C>" {
		t.Fatalf("飞鹅通道应转成居中放大标签, got %v", chunks)
	}
}

// TestBigLeftLineConsumedBySenders 左对齐放大行(整单菜品清单)也是控制行,两通道都要消费:
// 飞鹅落地成 <B> 而非 <C><B>(<C> 是居中,居中了编号就不在行首),ESC/POS 同样倍宽倍高
// 但必须保持左对齐。两种放大前缀还不能互相误判。
func TestBigLeftLineConsumedBySenders(t *testing.T) {
	setupTestDB(t)
	line := bigLeftMarkLine("1. 凉拌青瓜 (份) x2")

	if text, ok := SplitBigLeftLine(line); !ok || text != "1. 凉拌青瓜 (份) x2" {
		t.Fatalf("SplitBigLeftLine 解析失败: %q %v", text, ok)
	}
	if _, ok := SplitBigLeftLine("1. 凉拌青瓜 (份) x2"); ok {
		t.Fatal("普通文本行不应被判为左对齐放大行")
	}
	if _, ok := SplitBigLine(line); ok {
		t.Fatal("左对齐放大行不应被判为居中放大行")
	}
	if _, ok := SplitBigLeftLine(bigMarkLine("桌号 03")); ok {
		t.Fatal("居中放大行不应被判为左对齐放大行")
	}

	raw := encodeESCPOS([]string{line})
	if !bytes.Contains(raw, []byte{0x1D, 0x21, 0x11}) {
		t.Fatalf("ESC/POS 应带 GS ! 倍宽倍高指令, bytes=%v", raw)
	}
	if bytes.Contains(raw, []byte{0x1B, 0x61, 0x01}) {
		t.Fatal("左对齐放大不得发居中指令 ESC a 1")
	}
	if !bytes.Contains(raw, []byte{0x1B, 0x61, 0x00}) {
		t.Fatal("左对齐放大应显式声明 ESC a 0,不依赖上一条指令留下的对齐状态")
	}
	if !bytes.Contains(raw, []byte{0x1D, 0x21, 0x00}) {
		t.Fatal("放大后必须还原字号,否则后续行全变双倍宽")
	}
	if bytes.Contains(raw, []byte{0x02}) {
		t.Fatalf("控制前缀不应出现在出纸字节里, bytes=%v", raw)
	}

	chunks := splitFeieContent([]string{line})
	if len(chunks) != 1 || chunks[0] != "<B>1. 凉拌青瓜 (份) x2</B>" {
		t.Fatalf("飞鹅通道应转成左对齐放大标签 <B>, got %v", chunks)
	}
	if strings.Contains(chunks[0], "<C>") {
		t.Fatalf("左对齐放大不得带居中标签: %q", chunks[0])
	}
}

// TestBoldLineSeqTitle 拆单序号标题仍按标题加粗;正文里带序号的说明不该误判。
func TestBoldLineSeqTitle(t *testing.T) {
	setupTestDB(t)

	if !BoldLine("【厨房单】2/5") || !BoldLine("【加菜单】1/3") {
		t.Fatal("带序号的标题行应加粗(与 ESC E 强调一致)")
	}
	for _, notTitle := range []string{"【厨房单】说明", "【厨房单】0/3", "【厨房单】3/2"} {
		if BoldLine(notTitle) {
			t.Fatalf("%q 不应判为标题行", notTitle)
		}
	}
	// 备注行按「备注强调」规则加粗(见 BoldLine),不属于标题规则。
	if !BoldLine("备注: 2/5") || !BoldLine("整单备注: 2/5") || !BoldLine("  * 2/5") {
		t.Fatal("备注行应加粗")
	}
}

// bigLinesOf 取出渲染结果里全部放大行的文本(按出现顺序)。
// 一张厨房单上放大行不止一条:桌号固定放大,夹单模式下菜名也放大。
func bigLinesOf(lines []string) []string {
	out := []string{}
	for _, l := range lines {
		if text, ok := SplitBigLine(l); ok {
			out = append(out, text)
		}
	}
	return out
}

// hasBigLine 判断放大行里是否有指定文本。
func hasBigLine(lines []string, want string) bool {
	for _, t := range bigLinesOf(lines) {
		if t == want {
			return true
		}
	}
	return false
}
