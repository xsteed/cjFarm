package print

// ============================================================================
// 食客小票模板配置测试(方案A)
//
// 三个可配置项:页脚文案(print_guest_footer)、餐位费行(print_guest_show_seat_fee)、
// 优惠行(print_guest_show_discount)。渲染函数实时读取配置,预览(PreviewTicket)
// 与出纸走同一函数,改配置后预览立即反映——这里同时验证该联动。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// setPrintSetting 写入一个打印配置项(走 dao 正规路径,兼容两种数据库方言)。
func setPrintSetting(t *testing.T, key, val string) {
	t.Helper()
	if err := dao.SetSetting(key, val); err != nil {
		t.Fatalf("写入配置 %s 失败: %v", key, err)
	}
}

// renderGuest 用样例订单渲染食客小票(空分类打印机=不过滤菜品)。
func renderGuest(t *testing.T) string {
	t.Helper()
	o, items := sampleOrder()
	return strings.Join(RenderGuestTicket(o, items, 48), "\n")
}

func TestGuestTicketFooterConfig(t *testing.T) {
	setupTestDB(t)

	// 1) 默认页脚:出厂值由 seed 补齐(测试库走 store.Init 的同一条 seed 路径)。
	if got := renderGuest(t); !strings.Contains(got, "谢谢惠顾,欢迎再次光临") {
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

func TestGuestTicketSeatFeeConfig(t *testing.T) {
	setupTestDB(t)

	// 默认显示餐位费。
	if got := renderGuest(t); !strings.Contains(got, "餐位费") {
		t.Fatalf("默认应显示餐位费行:\n%s", got)
	}

	// 关闭后餐位费行消失,但合计金额不变(只是展示开关,不影响金额计算)。
	setPrintSetting(t, "print_guest_show_seat_fee", "0")
	got := renderGuest(t)
	if strings.Contains(got, "餐位费") {
		t.Fatalf("关闭后不应显示餐位费行:\n%s", got)
	}
	if !strings.Contains(got, "合计") || !strings.Contains(got, "84.00") {
		t.Fatalf("合计行与金额不应受影响:\n%s", got)
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
	setPrintSetting(t, "print_guest_show_seat_fee", "0")

	chunks, _, err := PreviewTicket(printerID, orderID, po.PrintDocGuest)
	if err != nil {
		t.Fatalf("PreviewTicket 失败: %v", err)
	}
	got := flattenPreview(chunks)
	if !strings.Contains(got, "预览联动验证") {
		t.Fatalf("预览应反映自定义页脚:\n%s", got)
	}
	if strings.Contains(got, "餐位费") {
		t.Fatalf("预览应反映餐位费行关闭:\n%s", got)
	}
}
