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
func TestPreviewReflectsTemplateConfig(t *testing.T) {
	setupTestDB(t)
	orderID := insertPreviewOrder(t)
	printerID := insertPreviewPrinter(t, po.PrinterTypeGuest, "", 48)

	setPrintSetting(t, "print_guest_footer", "预览联动验证")
	setPrintSetting(t, "print_guest_show_discount", "0")

	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuest, po.PrintTriggerOrder)
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
