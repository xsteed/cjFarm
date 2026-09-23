package print

import (
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
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
}

// ============================================================================
// 飞鹅签名与内容切分
// ============================================================================

// TestFeieSign 校验签名算法: sha1(user + UKEY + stime) 的 40 位小写十六进制。
// 期望值是外部独立算出来的(sha1sum),不是用同一段代码生成的 —— 否则测不出错。
func TestFeieSign(t *testing.T) {
	got := feieSign("demo_user", "UKEY", "1234567890")
	want := "92bd75ff96974fbba84590de3bc1f101f8ff56c1"
	if got != want {
		t.Fatalf("feieSign = %q, want %q", got, want)
	}
	if len(got) != 40 || strings.ToLower(got) != got {
		t.Fatalf("签名必须是 40 位小写十六进制, got %q", got)
	}
}

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
		if len(ch) > feieMaxBytes {
			t.Fatalf("第 %d 段长度 %d 超过上限 %d", i+1, len(ch), feieMaxBytes)
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
	lines := RenderKitchenTicket(o, items, 48, false, "【厨房单】")
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"【厨房单】", "单号: D20260921120000abcdef", "大厅03桌", "凉拌青瓜 (份) x2", "加辣", "整单备注: 不要香菜"} {
		if !strings.Contains(joined, want) {
			t.Errorf("厨房单缺少 %q\n实际内容:\n%s", want, joined)
		}
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
	withPrice := strings.Join(RenderKitchenTicket(o, items, 48, true, "【厨房单】"), "\n")
	if !strings.Contains(withPrice, "56.00") {
		t.Errorf("开启金额后应带出小计,实际内容:\n%s", withPrice)
	}

	// 加菜单标题。
	if !strings.Contains(strings.Join(RenderKitchenTicket(o, items, 48, false, "【加菜单】"), "\n"), "【加菜单】") {
		t.Error("加菜单标题未生效")
	}
}

func TestRenderGuestTicket(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	joined := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	for _, want := range []string{"【食客小票】", "订单号: D20260921120000abcdef", "凉拌青瓜 (份) x2", "菜品金额", "餐位费", "优惠", "合计", "84.00", "谢谢惠顾"} {
		if !strings.Contains(joined, want) {
			t.Errorf("食客小票缺少 %q\n实际内容:\n%s", want, joined)
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
	jobs := buildJobs(printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
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
	jobs := buildJobs(printers, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerSettle, "")
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
	jobs := buildJobs(printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
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
