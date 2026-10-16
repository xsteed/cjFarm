package print

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/adapter/feie"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// setupTestDB 建一个临时 SQLite 库(含种子数据),供渲染层读取店铺名等配置。
//
// 关闭连接的回调必须在 t.TempDir() 之后注册 —— t.Cleanup 是后进先出,
// 先关连接再删目录才能通过(Windows 不允许删除仍被占用的文件)。
func setupTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	store.Init(filepath.Join(dir, "print_test.db"))
	t.Cleanup(func() {
		if store.DB != nil {
			_ = store.DB.Close()
		}
	})
	// 出纸确认的回执等待在生产是秒级(见 feieConfirmFirstDelay);这里拉到「不可能触发」:
	// 否则各用例派发出的确认协程会在用例结束后醒来,把失败回执写进**下一个用例**的
	// 临时库,造成与用例内容无关的偶发失败。专门的确认用例会把等待压到毫秒级并自行恢复。
	feieConfirmFirstDelay, feieConfirmRetryDelay = time.Hour, time.Hour
}

// ============================================================================
// 飞鹅内容切分(签名与错误码翻译已随接口层迁至 infrastructure/adapter/feie)
// ============================================================================

// TestEscapeFeie 菜名/备注里的尖括号必须转义,否则会被当成排版标签解析。
func TestEscapeFeie(t *testing.T) {
	cases := map[string]string{
		"普通菜名":         "普通菜名",
		"<BR>":         "&lt;BR&gt;",
		"a<b>c":        "a&lt;b&gt;c",
		"<C>居中</C>坏分子": "&lt;C&gt;居中&lt;/C&gt;坏分子",
	}
	for in, want := range cases {
		if got := escapeFeie(in); got != want {
			t.Errorf("escapeFeie(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSplitFeieContent 内容超上限时要按行切分,且每段都不超限。
func TestSplitFeieContent(t *testing.T) {
	// 每行 100 字节,共 100 行 => 必然超过单次上限,应切成多段。
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 100)
	}
	chunks := splitFeieContent(lines)
	if len(chunks) < 2 {
		t.Fatalf("超长内容应切成多段, got %d 段", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch) > feie.MaxBytes {
			t.Fatalf("第 %d 段长度 %d 超过上限 %d", i+1, len(ch), feie.MaxBytes)
		}
	}
	// 短内容只有一段。
	short := splitFeieContent([]string{"第一行", "第二行"})
	if len(short) != 1 || short[0] != "第一行<BR>第二行" {
		t.Fatalf("短内容拼接结果异常: %#v", short)
	}
	// 空内容也要有一段(避免空 content 被飞鹅判为参数错误)。
	if got := splitFeieContent(nil); len(got) != 1 {
		t.Fatalf("空内容应返回一段空串, got %#v", got)
	}
}

// ============================================================================
// 单据渲染
// ============================================================================

func sampleOrder() (po.Order, []po.OrderItem) {
	o := po.Order{
		OrderID: 7, OrderNo: "D20260921120000abcdef", TableNo: "03", TableName: "大厅03桌",
		PersonCount: 4, DishAmount: 6600, SeatFee: 2400, DiscountAmount: 600, TotalAmount: 8400,
		OrderRemark: "不要香菜",
		CreateTime:  "2026-09-21 12:00:00",
		SettleType:  "normal",
	}
	items := []po.OrderItem{
		{ItemID: 1, DishID: 1, DishName: "凉拌青瓜", SpecName: "份", Quantity: 2, Price: 2800, Amount: 5600, Remark: "加辣"},
		{ItemID: 2, DishID: 10, DishName: "海鲜炒饭", Quantity: 1, Price: 1000, Amount: 1000},
	}
	return o, items
}

// TestRenderTicketPickupNo 取餐号只印在厨房单上(后厨据此叫号);
// 顾客小票不打印 —— 取餐号是「当面对号」用的,顾客把票拿走后再报号无从对应。
// 未分配(历史订单/手工补录)时两票都不打印,避免露出无意义的 "0"。
func TestRenderTicketPickupNo(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()

	t.Run("已分配取餐号:厨房单打印、顾客小票不打印", func(t *testing.T) {
		o.PickupNo = 12
		kitchen := strings.Join(RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"}), "\n")
		if !strings.Contains(kitchen, "取餐号: 12") {
			t.Errorf("厨房单应包含取餐号行, 实际:\n%s", kitchen)
		}
		guest := strings.Join(RenderGuestTicket(o, items, 48), "\n")
		if strings.Contains(guest, "取餐号") {
			t.Errorf("顾客小票不应打印取餐号, 实际:\n%s", guest)
		}
	})

	t.Run("未分配则两票都不打印该行", func(t *testing.T) {
		o.PickupNo = 0
		for name, lines := range map[string][]string{
			"顾客小票": RenderGuestTicket(o, items, 48),
			"厨房单":  RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"}),
		} {
			if strings.Contains(strings.Join(lines, "\n"), "取餐号") {
				t.Errorf("%s 不应出现取餐号行, 实际:\n%s", name, strings.Join(lines, "\n"))
			}
		}
	})
}

// TestGuestTicketHeaderLayout 票头版式:各票种店名统一放大居中加粗;顾客小票单据名
// 居中弱化(不加粗)且不含取餐号,厨房单单据名保持左对齐、照常打印取餐号。
func TestGuestTicketHeaderLayout(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	o.PickupNo = 7
	shop := strings.TrimSpace(service.GetSetting("shop_name"))

	// 结账形态(trigger=settle):单据名是「【结账小票】」。
	guest := RenderGuestTicket(o, items, 48)
	// 80mm(48 格)下默认店名 23 格 ≤ 24,应作为放大行居中输出。
	if big := bigLinesOf(guest); len(big) != 1 || big[0] != shop {
		t.Fatalf("结账小票店名应为唯一放大行 %q, got %v:\n%s", shop, big, strings.Join(guest, "\n"))
	}
	// 单据名副标题:机器居中控制行(不再空格补位)且不加粗。
	titleLine := ""
	for _, l := range guest {
		if text, ok := SplitCenterLine(l); ok && strings.TrimSpace(text) == "【结账小票】" {
			titleLine = l
		}
	}
	if titleLine == "" {
		t.Fatalf("结账小票应保留单据名副标题(居中控制行):\n%s", strings.Join(guest, "\n"))
	}
	if text, _ := SplitCenterLine(titleLine); BoldLine(text) {
		t.Fatalf("单据名副标题应弱化(不加粗), got %q", text)
	}

	// 点菜单形态(trigger=order):单据名换成「【顾客小票】」,同样弱化居中。
	order := RenderGuestOrderTicket(o, items, 48)
	orderTitle := ""
	for _, l := range order {
		if text, ok := SplitCenterLine(l); ok && strings.TrimSpace(text) == "【顾客小票】" {
			orderTitle = l
		}
	}
	if orderTitle == "" {
		t.Fatalf("点菜单应保留单据名副标题:\n%s", strings.Join(order, "\n"))
	}
	if text, _ := SplitCenterLine(orderTitle); BoldLine(text) {
		t.Fatalf("点菜单单据名应弱化居中, got %q", orderTitle)
	}

	// 厨房单:店名同样放大居中(与顾客小票一致),单据名保持左对齐,取餐号照常打印。
	kitchen := RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"})
	if big := bigLinesOf(kitchen); len(big) != 2 || big[0] != shop {
		t.Fatalf("厨房单店名应为放大行(各票种统一), got %v:\n%s", big, strings.Join(kitchen, "\n"))
	}
	if len(kitchen) < 2 || kitchen[1] != pad("【厨房单】", 48) {
		t.Fatalf("厨房单单据名应保持左对齐原样, got %q", kitchen[1])
	}
	if !strings.Contains(strings.Join(kitchen, "\n"), "取餐号: 7") {
		t.Fatalf("厨房单应照常打印取餐号:\n%s", strings.Join(kitchen, "\n"))
	}
}

// sampleRows 返回带分类 ID 的订单明细(分单逻辑与 buildJobs 使用)。
func sampleRows() []dao.OrderItemRow {
	_, items := sampleOrder()
	return []dao.OrderItemRow{
		{OrderItem: items[0], CategoryID: 1},
		{OrderItem: items[1], CategoryID: 6},
	}
}

func TestRenderKitchenTicket(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()

	// 默认不带金额:后厨只看菜名与数量。
	lines := RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【厨房单】"})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"【厨房单】",
		// 厨打单票头精简:短号(与后台订单列表/打印记录同源) + 时间省到分。
		"单号: ABCDEF",
		"下单: 09-21 12:00",
		// 整单模式逐道编号,票尾给份数汇总。
		"1. 凉拌青瓜 (份) x2",
		"2. 海鲜炒饭 x1",
		"共 2 道 3 份",
		"加辣",
		"整单备注: 不要香菜",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("厨房单缺少 %q\n实际内容:\n%s", want, joined)
		}
	}
	// 台名「大厅03桌」已含桌号,放大桌号行之外不再重复打一行。
	if strings.Contains(joined, "大厅03桌") {
		t.Errorf("台名与桌号重复时不应另起一行,实际内容:\n%s", joined)
	}
	// 人数与下单时间并排一行(后厨单省纸),不该各占一行。
	merged := false
	for _, l := range lines {
		if strings.Contains(l, "人数: 4") && strings.Contains(l, "下单: 09-21 12:00") {
			merged = true
		}
	}
	if !merged {
		t.Errorf("人数与下单时间应并排同一行,实际内容:\n%s", joined)
	}
	if strings.Contains(joined, "D20260921120000abcdef") {
		t.Errorf("厨打单不应打印完整订单号(后厨读不出来),实际内容:\n%s", joined)
	}
	if strings.Contains(joined, "56.00") {
		t.Errorf("默认不应打印金额,实际内容:\n%s", joined)
	}
	// 每行不得超过纸宽(否则热敏机会乱折行)。
	for _, l := range lines {
		if displayWidth(l) > 48 {
			t.Errorf("行超宽(%d): %q", displayWidth(l), l)
		}
	}

	// 打开金额开关后必须带出小计。
	withPrice := strings.Join(RenderKitchenTicket(o, items, 48, KitchenTicketOptions{
		Title: "【厨房单】", ShowPrice: true,
	}), "\n")
	if !strings.Contains(withPrice, "56.00") {
		t.Errorf("开启金额后应带出小计,实际内容:\n%s", withPrice)
	}

	// 加菜单标题。
	if !strings.Contains(strings.Join(RenderKitchenTicket(o, items, 48, KitchenTicketOptions{Title: "【加菜单】"}), "\n"), "【加菜单】") {
		t.Error("加菜单标题未生效")
	}
}

func TestRenderGuestTicket(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	joined := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	for _, want := range []string{"【结账小票】", "订单号: D20260921120000abcdef", "凉拌青瓜 (份) x2", "茶位费(4人)", "优惠", "合计", "84.00", "谢谢惠顾"} {
		if !strings.Contains(joined, want) {
			t.Errorf("结账小票缺少 %q\n实际内容:\n%s", want, joined)
		}
	}
	for _, l := range RenderGuestTicket(o, items, 48) {
		if displayWidth(l) > 48 {
			t.Errorf("行超宽(%d): %q", displayWidth(l), l)
		}
	}
}

// TestRenderGuestTicketSettleTypes 免单/挂账必须在票面标注,避免对账歧义。
func TestRenderGuestTicketSettleTypes(t *testing.T) {
	setupTestDB(t)
	free, freeItems := sampleOrder()
	free.SettleType = "free"
	free.SettleRemark = "员工聚餐"
	if got := strings.Join(RenderGuestTicket(free, freeItems, 48), "\n"); !strings.Contains(got, "【免单】实收 0.00") || !strings.Contains(got, "免单原因: 员工聚餐") {
		t.Errorf("免单标注缺失:\n%s", got)
	}

	credit, creditItems := sampleOrder()
	credit.SettleType = "credit"
	credit.CreditStatus = 1
	credit.CreditAmount = 8400
	credit.SettleRemark = "张老板"
	got := strings.Join(RenderGuestTicket(credit, creditItems, 48), "\n")
	if !strings.Contains(got, "【挂账】待收 84.00") || !strings.Contains(got, "挂账人: 张老板") {
		t.Errorf("挂账标注缺失:\n%s", got)
	}

	credit.CreditStatus = 2
	if got := strings.Join(RenderGuestTicket(credit, creditItems, 48), "\n"); !strings.Contains(got, "【挂账已结】84.00") {
		t.Errorf("挂账已结标注缺失:\n%s", got)
	}
}

// TestTwoColWrap 两列对齐:放不下时左段折行、右段单独右对齐,不能挤成一条超宽行。
func TestTwoColWrap(t *testing.T) {
	lines := twoCol("一个很长很长的菜名加上规格说明", "1234.56", 32)
	for _, l := range lines {
		if displayWidth(l) > 32 {
			t.Errorf("行超宽(%d): %q", displayWidth(l), l)
		}
	}
	if lines[len(lines)-1] != strings.Repeat(" ", 32-len("1234.56"))+"1234.56" {
		t.Errorf("右段未右对齐: %q", lines[len(lines)-1])
	}
}

// ============================================================================
// 分单逻辑(含结账不再重复打厨房单的回归)
// ============================================================================

// TestItemsForPrinterCategorySplit 按分类分单:厨房机只收自己负责的分类。
func TestItemsForPrinterCategorySplit(t *testing.T) {
	kitchenCold := po.Printer{PrinterType: po.PrinterTypeKitchen, CategoryIDs: "1"}
	kitchenAll := po.Printer{PrinterType: po.PrinterTypeKitchen}
	guest := po.Printer{PrinterType: po.PrinterTypeGuest}

	items := sampleRows() // 分类 1 与 6 各一道

	if got := itemsForPrinter(kitchenCold, items); len(got) != 1 || got[0].DishName != "凉拌青瓜" {
		t.Errorf("凉菜机应只收到凉菜, got %#v", got)
	}
	if got := itemsForPrinter(kitchenAll, items); len(got) != 2 {
		t.Errorf("未配分类的厨房机应收全部菜品, got %d 条", len(got))
	}
	// 小票机即使误配了分类也不能过滤 —— 否则金额合计对不上。
	guestWithCats := po.Printer{PrinterType: po.PrinterTypeGuest, CategoryIDs: "1"}
	if got := itemsForPrinter(guestWithCats, items); len(got) != 2 {
		t.Errorf("小票机不得按分类过滤, got %d 条", len(got))
	}
	_ = guest
}

// TestBuildJobsSkipsEmptyCategoryPrinter 分类分单没匹配上菜品的机器必须被跳过,
// 否则会打出一张只有抬头没有菜品的空白厨房单。
func TestBuildJobsSkipsEmptyCategoryPrinter(t *testing.T) {
	printers := []po.Printer{
		{PrinterID: 1, PrinterName: "凉菜机", PrinterType: po.PrinterTypeKitchen, CategoryIDs: "99"},
		{PrinterID: 2, PrinterName: "热菜机", PrinterType: po.PrinterTypeKitchen},
	}
	o, _ := sampleOrder()
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 1 || jobs[0].p.PrinterID != 2 {
		t.Fatalf("应只给热菜机派单, got %#v", jobs)
	}
}

// TestBuildJobsGuestOnlyToGuestPrinters 回归:结账只该出小票机,
// 不能再给厨房机推一张厨房单(这是修复前的实际 bug)。
func TestBuildJobsGuestOnlyToGuestPrinters(t *testing.T) {
	printers := []po.Printer{
		{PrinterID: 1, PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen},
		{PrinterID: 2, PrinterName: "前台账", PrinterType: po.PrinterTypeGuest},
	}
	o, _ := sampleOrder()
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerSettle, "")
	if len(jobs) != 1 || jobs[0].p.PrinterID != 2 {
		t.Fatalf("结账单据只应派给小票机, got %#v", jobs)
	}
}

// TestBuildJobsKitchenOnlyToKitchenPrinters 反向下单时小票机不拿厨房单。
func TestBuildJobsKitchenOnlyToKitchenPrinters(t *testing.T) {
	printers := []po.Printer{
		{PrinterID: 1, PrinterName: "后厨机", PrinterType: po.PrinterTypeKitchen},
		{PrinterID: 2, PrinterName: "前台账", PrinterType: po.PrinterTypeGuest},
	}
	o, _ := sampleOrder()
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 1 || jobs[0].p.PrinterID != 1 {
		t.Fatalf("厨房单据只应派给厨房机, got %#v", jobs)
	}
}

// ============================================================================
// 重试判定
// ============================================================================

// TestIsRetryable 配置类错误不该重试(重试也不会有不同结果,只是白等)。
func TestIsRetryable(t *testing.T) {
	if isRetryable(newPermError("SN 填错")) {
		t.Error("永久性错误不应重试")
	}
	if !isRetryable(errString("connection refused")) {
		t.Error("网络类错误应重试")
	}
}

// errString 一个普通错误(模拟网络错误)。
type errString string

func (e errString) Error() string { return string(e) }

// TestGuestTicketFormFollowsTrigger 顾客小票的形态跟着触发场景走:
//   - 下单触发(trigger=order)→ 点菜单形态「【顾客小票】」:金额一行「当前合计」,
//     不出优惠与结算块;
//   - 结账触发(trigger=settle)→ 结账形态「【结账小票】」:优惠行与结算块齐全。
//
// 先付模式的「首次收款成功」走的就是 settle 触发(见 handler.printKitchenOnPaid),
// 那张纸点菜单与付款凭证合一,不会再出第二张点菜单。
func TestGuestTicketFormFollowsTrigger(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()

	order := strings.Join(RenderGuestOrderTicket(o, items, 48), "\n")
	if !strings.Contains(order, "【顾客小票】") || !strings.Contains(order, "当前合计") {
		t.Fatalf("点菜单形态缺要素:\n%s", order)
	}
	// 「\n合计」匹配顶格的金额行,不会撞上「当前合计」。
	for _, bad := range []string{"【结账小票】", "优惠", "支付方式", "结账时间", "\n合计"} {
		if strings.Contains(order, bad) {
			t.Fatalf("点菜单不应包含结账要素 %q:\n%s", bad, order)
		}
	}

	settle := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	if !strings.Contains(settle, "【结账小票】") || !strings.Contains(settle, "优惠") || !strings.Contains(settle, "\n合计") {
		t.Fatalf("结账小票形态缺要素:\n%s", settle)
	}
	if strings.Contains(settle, "当前合计") {
		t.Fatalf("结账小票金额行应为「合计」:\n%s", settle)
	}
}

// TestGuestJobFormFollowsTrigger 作业层同源:docType=guest 的作业按 triggerBy
// 分发到对应渲染函数,label 也与票面标题对得上 —— 台账里「顾客小票/结账小票」
// 要能一眼分清,排障时才不用点开快照猜。
func TestGuestJobFormFollowsTrigger(t *testing.T) {
	setupTestDB(t)
	o, _ := settledOrder()
	printers := []po.Printer{
		{PrinterName: "前台小票机", PrinterType: po.PrinterTypeGuest, PaperWidth: 48},
	}

	orderJobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerOrder, "")
	if len(orderJobs) != 1 {
		t.Fatalf("点菜单只应发给小票机, got %d 个作业", len(orderJobs))
	}
	if got := strings.Join(orderJobs[0].render(), "\n"); !strings.Contains(got, "【顾客小票】") {
		t.Fatalf("下单作业应渲染点菜单形态:\n%s", got)
	}
	if got := orderJobs[0].label(); got != "顾客小票" {
		t.Fatalf("下单作业标识应为「顾客小票」, got %q", got)
	}

	settleJobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerSettle, "")
	if len(settleJobs) != 1 {
		t.Fatalf("结账小票只应发给小票机, got %d 个作业", len(settleJobs))
	}
	if got := strings.Join(settleJobs[0].render(), "\n"); !strings.Contains(got, "【结账小票】") {
		t.Fatalf("结账作业应渲染结账小票形态:\n%s", got)
	}
	if got := settleJobs[0].label(); got != "结账小票" {
		t.Fatalf("结账作业标识应为「结账小票」, got %q", got)
	}
}
