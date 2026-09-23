package print

// ============================================================================
// 示例模板预览测试(PreviewTicketSample)
//
// 验证三件事:
//   1. 示例数据渲染出完整票据(店名/标题/菜品/金额/页脚齐全),金额自洽;
//   2. 表单覆盖值生效(未保存的页脚/开关也能预览到,所见即所改);
//   3. 与实际打印同一条渲染路径 —— 覆盖为零值时与 RenderGuestTicket 完全一致。
// ============================================================================

import (
	"strings"
	"testing"

	"dining-system/internal/po"
)

func TestPreviewTicketSampleGuest(t *testing.T) {
	setupTestDB(t)

	chunks, lineWidth := sampleRender(t, po.PrintDocGuest, 48, nil)
	if lineWidth != 48 {
		t.Fatalf("80mm 行宽应为 48, got %d", lineWidth)
	}
	got := chunks[0]
	joined := strings.Join(got, "\n")
	// 完整票据要素:标题、示例菜、金额区、默认页脚(由 seed 补齐)。
	for _, want := range []string{"【食客小票】", "凉拌青瓜", "柴火土鸡", "菜品金额", "餐位费", "优惠", "合计", "谢谢惠顾"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("示例小票缺少 %q:\n%s", want, joined)
		}
	}
	// 金额自洽:8600+1800-600=9800(元显示为 98.00)。
	if !strings.Contains(joined, "98.00") {
		t.Fatalf("示例合计金额缺失(应为 98.00):\n%s", joined)
	}
	for _, l := range got {
		if displayWidth(l) > 48 {
			t.Fatalf("行超宽(%d): %q", displayWidth(l), l)
		}
	}
}

func TestPreviewTicketSampleOverrides(t *testing.T) {
	setupTestDB(t)

	footer := "未保存的口号"
	seatFee := false
	chunks, _ := sampleRender(t, po.PrintDocGuest, 48, &GuestTicketOverrides{Footer: &footer, ShowSeatFee: &seatFee})
	joined := strings.Join(chunks[0], "\n")

	if !strings.Contains(joined, "未保存的口号") {
		t.Fatalf("覆盖页脚未生效:\n%s", joined)
	}
	if strings.Contains(joined, "谢谢惠顾") {
		t.Fatalf("覆盖后不应出现库里的默认页脚:\n%s", joined)
	}
	if strings.Contains(joined, "餐位费") {
		t.Fatalf("覆盖关闭餐位费行未生效:\n%s", joined)
	}
	// 合计金额不因展示开关变化。
	if !strings.Contains(joined, "98.00") {
		t.Fatalf("合计金额不应受开关影响:\n%s", joined)
	}
}

func TestPreviewTicketSampleMatchesSavedConfig(t *testing.T) {
	setupTestDB(t)

	// 不传任何覆盖(全 nil):与实际打印路径(RenderGuestTicket 读库配置)完全一致。
	chunks, _ := sampleRender(t, po.PrintDocGuest, 48, &GuestTicketOverrides{})
	o, items := samplePreviewOrder()
	want := RenderGuestTicket(o, items, 48)
	if strings.Join(chunks[0], "\n") != strings.Join(want, "\n") {
		t.Fatalf("示例预览(无覆盖)应与实际打印渲染一致:\n预览:\n%s\n实际:\n%s",
			strings.Join(chunks[0], "\n"), strings.Join(want, "\n"))
	}
}

func TestPreviewTicketSampleKitchen(t *testing.T) {
	setupTestDB(t)

	chunks, lineWidth := sampleKitchen(t, 32, true)
	joined := strings.Join(chunks[0], "\n")
	if !strings.Contains(joined, "【厨房单】") || !strings.Contains(joined, "凉拌青瓜") {
		t.Fatalf("示例厨房单缺少关键内容:\n%s", joined)
	}
	// kitchenShowPrice=true 时带金额。
	if !strings.Contains(joined, "56.00") {
		t.Fatalf("厨房单带金额未生效:\n%s", joined)
	}
	if lineWidth != 32 {
		t.Fatalf("58mm 行宽应为 32, got %d", lineWidth)
	}
	for _, l := range chunks[0] {
		if displayWidth(l) > 32 {
			t.Fatalf("行超宽(%d): %q", displayWidth(l), l)
		}
	}
}

func sampleRender(t *testing.T, docType string, paperWidth int, ov *GuestTicketOverrides) ([][]string, int) {
	t.Helper()
	if ov == nil {
		ov = &GuestTicketOverrides{}
	}
	return PreviewTicketSample(docType, paperWidth, *ov, false)
}

func sampleKitchen(t *testing.T, paperWidth int, showPrice bool) ([][]string, int) {
	t.Helper()
	return PreviewTicketSample(po.PrintDocKitchen, paperWidth, GuestTicketOverrides{}, showPrice)
}
