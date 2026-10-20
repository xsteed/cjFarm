package print

// ============================================================================
// 顾客小票模板配置测试(方案A)
//
// 可配置项:页脚文案(print_guest_footer)、优惠行(print_guest_show_discount)。
// 渲染函数实时读取配置,预览(PreviewTicket)与出纸走同一函数,改配置后预览立即反映
// ——这里同时验证该联动。金额区按「有才打」渲染(茶位费/优惠/赠送),故没有展示开关。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store/dao"
)

// setPrintSetting 写入一个打印配置项(走 dao 正规路径,兼容两种数据库方言)。
func setPrintSetting(t *testing.T, key, val string) {
	t.Helper()
	if err := dao.SetSetting(key, val); err != nil {
		t.Fatalf("写入配置 %s 失败: %v", key, err)
	}
}

// renderGuest 用样例订单渲染顾客小票(空分类打印机=不过滤菜品)。
func renderGuest(t *testing.T) string {
	t.Helper()
	o, items := sampleOrder()
	return strings.Join(RenderGuestTicket(o, items, 48), "\n")
}

func TestGuestTicketFooterConfig(t *testing.T) {
	setupTestDB(t)

	// 1) 默认页脚:出厂值由 seed 补齐(测试库走 store.Init 的同一条 seed 路径)。
	if got := renderGuest(t); !strings.Contains(got, "谢谢惠顾，欢迎再次光临！") {
		t.Fatalf("默认页脚缺失:\n%s", got)
	}

	// 2) 自定义页脚:改文案立即生效(渲染实时读配置,不需要重启)。
	setPrintSetting(t, "print_guest_footer", "老地方农家菜,好吃再来")
	if got := renderGuest(t); !strings.Contains(got, "老地方农家菜,好吃再来") || strings.Contains(got, "谢谢惠顾") {
		t.Fatalf("自定义页脚未生效:\n%s", got)
	}

	// 3) 清空页脚:整段省略,连分隔线也不打(空串是合法语义,不是回退默认)。
	setPrintSetting(t, "print_guest_footer", "")
	got := renderGuest(t)
	if strings.Contains(got, "谢谢惠顾") || strings.Contains(got, "老地方农家菜") {
		t.Fatalf("清空后不应再打印页脚:\n%s", got)
	}
	// 清空后最后一行不应是孤零零的分隔线。
	o2, items2 := sampleOrder()
	lines := RenderGuestTicket(o2, items2, 48)
	if len(lines) > 0 && lines[len(lines)-1] == strings.Repeat("-", 48) {
		t.Fatalf("清空页脚后不应残留孤立分隔线:\n%s", strings.Join(lines, "\n"))
	}
}

// TestGuestTicketAmountSection 茶位费与菜品同区(分隔线之上),金额区只留优惠与合计。
// 菜品金额不单列(逐道菜已带金额);赠送不再单列(赠菜在菜品区已用【赠】标注)。
func TestGuestTicketAmountSection(t *testing.T) {
	setupTestDB(t)

	o0, items0 := sampleOrder()
	lines := RenderGuestTicket(o0, items0, 48)
	got := strings.Join(lines, "\n")
	if strings.Contains(got, "菜品金额") {
		t.Fatalf("不应单列菜品金额:\n%s", got)
	}
	// 样例订单:茶位费 24 元(4 人×6 元)、优惠 6 元、无赠菜。
	if !strings.Contains(got, "茶位费(4人)") || !strings.Contains(got, "24.00") {
		t.Fatalf("有茶位费时应打印茶位费行:\n%s", got)
	}
	// 位置:茶位费在分隔线之上(与菜品同区),合计在分隔线之下,两者之间隔着那条横线。
	idxSeat, idxTotal := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "茶位费") {
			idxSeat = i
		}
		if strings.HasPrefix(strings.TrimSpace(l), "合计") {
			idxTotal = i
		}
	}
	if idxSeat < 0 || idxTotal < 0 || idxSeat >= idxTotal {
		t.Fatalf("茶位费应在合计之上:\n%s", got)
	}
	hrBelow := false
	for i := idxSeat + 1; i < idxTotal; i++ {
		if lines[i] == hr(48) {
			hrBelow = true
		}
	}
	if !hrBelow {
		t.Fatalf("茶位费与合计之间应有一条分隔线(茶位费在线上方):\n%s", got)
	}
	if !strings.Contains(got, "优惠") {
		t.Fatalf("有优惠时应打印优惠行:\n%s", got)
	}
	if strings.Contains(got, "赠送") {
		t.Fatalf("无赠送时不应打印赠送行:\n%s", got)
	}
	// 合计仍含茶位费与优惠:66 + 24 - 6 = 84。
	if !strings.Contains(got, "合计") || !strings.Contains(got, "84.00") {
		t.Fatalf("合计金额应完整(含茶位费与优惠):\n%s", got)
	}
}

// TestGuestTicketGiftLines 赠菜只在菜品区用「【赠】」标注:不打金额,
// 金额区也不再单列「赠送」行(赠菜不计入合计,且已在菜品区表明)。
func TestGuestTicketGiftLines(t *testing.T) {
	setupTestDB(t)

	o, items := sampleOrder()
	items[1].IsGift = 1
	items[1].Amount = 0
	o.GiftAmount = 1000 // 赠菜原价 10 元,仅用于让利口径统计
	lines := RenderGuestTicket(o, items, 48)
	got := strings.Join(lines, "\n")

	if !strings.Contains(got, "【赠】海鲜炒饭 x1") {
		t.Fatalf("赠菜行应保留【赠】标记:\n%s", got)
	}
	// 赠菜行不得出现金额(既不是 0.00,也不是原价)。
	for _, l := range lines {
		if strings.Contains(l, "【赠】") && strings.Contains(l, ".00") {
			t.Fatalf("赠菜行不应显示金额: %q", l)
		}
	}
	// 金额区不再单列赠送行:赠菜已在菜品区逐道标注,下面再写一遍是重复。
	for _, l := range lines {
		if strings.Contains(l, "赠送") && !strings.Contains(l, "【赠】") {
			t.Fatalf("金额区不应再出现赠送行: %q", l)
		}
	}
}

// TestGuestTicketSeatFeeZeroHidden 茶位费为 0(本单不收/门店未开启)时不打该行:
// 打一行「茶位费(4人) 0.00」会让客人以为被多收了一笔。
func TestGuestTicketSeatFeeZeroHidden(t *testing.T) {
	setupTestDB(t)

	o, items := sampleOrder()
	o.SeatFee = 0
	o.TotalAmount = 6600 - 600 // 菜品 66 - 优惠 6
	got := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	if strings.Contains(got, "茶位费") {
		t.Fatalf("茶位费为 0 时不应打印该行:\n%s", got)
	}
	if !strings.Contains(got, "合计") || !strings.Contains(got, "60.00") {
		t.Fatalf("合计应随茶位费为 0 变化(60.00):\n%s", got)
	}
}

func TestGuestTicketDiscountConfig(t *testing.T) {
	setupTestDB(t)

	// 默认显示优惠行(样例订单有 6 元优惠)。
	if got := renderGuest(t); !strings.Contains(got, "优惠") || !strings.Contains(got, "-6.00") {
		t.Fatalf("默认应显示优惠行:\n%s", got)
	}

	// 关闭后优惠行消失,合计不变。
	setPrintSetting(t, "print_guest_show_discount", "0")
	got := renderGuest(t)
	if strings.Contains(got, "优惠") {
		t.Fatalf("关闭后不应显示优惠行:\n%s", got)
	}
	if !strings.Contains(got, "84.00") {
		t.Fatalf("合计金额不应受影响:\n%s", got)
	}
}

// TestPreviewReflectsTemplateConfig 改完配置,预览立即反映(预览与出纸同一条渲染路径)。
// 注意用结账小票样例:页脚只打在结账小票上,点菜单(trigger=order)永远不带页脚。
func TestPreviewReflectsTemplateConfig(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	setPrintSetting(t, "print_guest_footer", "预览联动验证")
	setPrintSetting(t, "print_guest_show_discount", "0")

	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuestSettle, po.PrintTriggerSettle)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	got := flattenPreview(chunks)
	if !strings.Contains(got, "预览联动验证") {
		t.Fatalf("预览应反映自定义页脚:\n%s", got)
	}
	// 该订单有优惠,关闭开关后预览里也不该出现优惠行。
	if strings.Contains(got, "优惠") {
		t.Fatalf("预览应反映优惠行关闭:\n%s", got)
	}
}

// ============================================================================
// 顾客小票整张放大(print_guest_large_font)
// ============================================================================

// bigTextOf 返回一行是否放大行及其文本;居中放大与左对齐放大都算。
func bigTextOf(line string) (string, bool) {
	if t, ok := SplitBigLine(line); ok {
		return t, true
	}
	if t, ok := SplitBigLeftLine(line); ok {
		return t, true
	}
	return "", false
}

// TestGuestTicketLargeFont 整张放大后:每行都是放大行,且按半个纸宽排版
// —— 倍宽倍高下每行只剩一半格数,再按 48 格排就会被机器硬折、两列散架。
// 点菜单与结账小票同受开关控制:同一位顾客拿到的一叠纸不能一半大一半小。
func TestGuestTicketLargeFont(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_guest_large_font", "1")

	o, items := settledOrder()
	lines := RenderGuestTicket(o, items, 48)
	if len(lines) == 0 {
		t.Fatal("结账小票不应为空")
	}
	for _, l := range lines {
		text, big := bigTextOf(l)
		if !big {
			t.Fatalf("整张放大后每一行都应是放大行, got %q", l)
		}
		if displayWidth(text) > 24 {
			t.Fatalf("放大行应按半纸宽(24 格)排版,超宽会被机器折行(%d 格): %q",
				displayWidth(text), text)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"【结账小票】", "凉拌青瓜 (份) x2", "56.00", "合计", "84.00", "支付方式", "谢谢惠顾"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("放大后的结账小票缺少 %q:\n%s", want, joined)
		}
	}

	// 点菜单(顾客小票)同样受这个开关影响:整张放大、按半纸宽 24 格重排。
	orderLines := RenderGuestOrderTicket(o, items, 48)
	for _, l := range orderLines {
		text, big := bigTextOf(l)
		if !big {
			t.Fatalf("点菜单整张放大后每一行都应是放大行, got %q", l)
		}
		if displayWidth(text) > 24 {
			t.Fatalf("点菜单放大行应按半纸宽(24 格)排版(%d 格): %q", displayWidth(text), text)
		}
	}
	orderJoined := strings.Join(orderLines, "\n")
	for _, want := range []string{"【顾客小票】", "凉拌青瓜 (份) x2", "56.00", "当前合计", "84.00"} {
		if !strings.Contains(orderJoined, want) {
			t.Fatalf("放大后的点菜单缺少 %q:\n%s", want, orderJoined)
		}
	}
	// 结账专属内容不能因为放大而渗进点菜单。
	for _, bad := range []string{"支付方式", "结账时间", "优惠"} {
		if strings.Contains(orderJoined, bad) {
			t.Fatalf("点菜单不应包含 %q:\n%s", bad, orderJoined)
		}
	}
}

// TestGuestTicketLargeFontOnlyOnFeie 放大只在飞鹅云落地:
// 同一份结账作业,飞鹅打印机拿到倍宽行(<B>/<C><B>),直连打印机仍是原版式。
// 这是刻意的不对称 —— 飞鹅 content 的 <B> 与 ESC/POS 的字库假设不同,不做跨通道联动。
func TestGuestTicketLargeFontOnlyOnFeie(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_guest_large_font", "1")
	o, _ := settledOrder()

	feieP := po.Printer{
		PrinterName: "前台飞鹅", PrinterType: po.PrinterTypeGuest,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48, Copies: 1,
	}
	jobs := buildJobs(nil, nil, []po.Printer{feieP}, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerSettle, "")
	if len(jobs) != 1 {
		t.Fatalf("应只给飞鹅小票机派一张, got %d", len(jobs))
	}
	lines := jobs[0].render()
	for _, l := range lines {
		if _, big := bigTextOf(l); !big {
			t.Fatalf("飞鹅结账小票应整张放大, got %q", l)
		}
	}
	// 落地到 content 后每行都必须带倍宽标签,不能有漏网的纯文本行
	// (漏一行就是「整张票里突然夹一行小字」)。
	content := strings.Join(splitFeieContent(lines), "")
	for _, piece := range strings.Split(content, feieSep) {
		if !strings.HasPrefix(piece, "<B>") && !strings.HasPrefix(piece, "<C><B>") {
			t.Fatalf("放大票的每一行都应落地为倍宽标签, got %q", piece)
		}
	}
	if !strings.Contains(content, "<C><B>【结账小票】</B></C>") {
		t.Fatalf("单据名副标题应居中放大:\n%s", content)
	}

	tcpP := feieP
	tcpP.Provider, tcpP.FeieSN, tcpP.IP, tcpP.Port = po.PrinterProviderTCP, "", "192.168.1.20", 9100
	jobs = buildJobs(nil, nil, []po.Printer{tcpP}, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerSettle, "")
	if len(jobs) != 1 {
		t.Fatalf("应只给直连小票机派一张, got %d", len(jobs))
	}
	tcpLines := jobs[0].render()
	for _, l := range tcpLines {
		if _, ok := SplitBigLeftLine(l); ok {
			t.Fatalf("直连打印不应整张放大, got %q", l)
		}
	}
	if !strings.Contains(strings.Join(tcpLines, "\n"), strings.Repeat("-", 48)) {
		t.Fatalf("直连结账小票仍是 48 格版式:\n%s", strings.Join(tcpLines, "\n"))
	}

	// 点菜单(trigger=order)与结账小票同一套判定:飞鹅放大、直连保持原版式。
	jobs = buildJobs(nil, nil, []po.Printer{feieP}, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerOrder, "")
	if len(jobs) != 1 {
		t.Fatalf("应只给飞鹅小票机派一张点菜单, got %d", len(jobs))
	}
	for _, l := range jobs[0].render() {
		if _, big := bigTextOf(l); !big {
			t.Fatalf("飞鹅点菜单应整张放大, got %q", l)
		}
	}
	jobs = buildJobs(nil, nil, []po.Printer{tcpP}, o, sampleRows(), po.PrintDocGuest, "", po.PrintTriggerOrder, "")
	if len(jobs) != 1 {
		t.Fatalf("应只给直连小票机派一张点菜单, got %d", len(jobs))
	}
	orderLines := jobs[0].render()
	for _, l := range orderLines {
		if _, ok := SplitBigLeftLine(l); ok {
			t.Fatalf("直连点菜单不应整张放大, got %q", l)
		}
	}
	if !strings.Contains(strings.Join(orderLines, "\n"), strings.Repeat("-", 48)) {
		t.Fatalf("直连点菜单仍是 48 格版式:\n%s", strings.Join(orderLines, "\n"))
	}
}

// TestPreviewTicketSampleLargeFont 示例预览要能显示「未保存的放大开关」:
// 门店勾上开关就能先看效果,不必真出一张纸。
func TestPreviewTicketSampleLargeFont(t *testing.T) {
	setupTestDB(t)
	on := true
	chunks, lineWidth := PreviewTicketSample(po.PrintDocGuestSettle, 48, SampleOverrides{LargeFont: &on})
	if lineWidth != 48 {
		t.Fatalf("纸宽口径不应因放大而变(仍按物理纸宽), got %d", lineWidth)
	}
	for _, l := range chunks[0] {
		if _, big := bigTextOf(l); !big {
			t.Fatalf("示例预览应整张放大, got %q", l)
		}
	}

	// 点菜单样例同样消费这个开关:两张顾客票的字号必须同步。
	orderChunks, _ := PreviewTicketSample(po.PrintDocGuest, 48, SampleOverrides{LargeFont: &on})
	for _, l := range orderChunks[0] {
		if _, big := bigTextOf(l); !big {
			t.Fatalf("点菜单示例预览也应整张放大, got %q", l)
		}
	}
}
