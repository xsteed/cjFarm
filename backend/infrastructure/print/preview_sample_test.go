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

	"dining-system/domain/po"
)

func TestPreviewTicketSampleGuest(t *testing.T) {
	setupTestDB(t)

	chunks, lineWidth := sampleRender(t, po.PrintDocGuest, 48, nil)
	if lineWidth != 48 {
		t.Fatalf("80mm 行宽应为 48, got %d", lineWidth)
	}
	got := chunks[0]
	joined := strings.Join(got, "\n")
	// 完整票据要素:标题、示例菜、金额区(茶位费/优惠/合计)、默认页脚(由 seed 补齐)。
	for _, want := range []string{"【食客小票】", "凉拌青瓜", "柴火土鸡", "茶位费(3人)", "优惠", "合计", "谢谢惠顾"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("示例小票缺少 %q:\n%s", want, joined)
		}
	}
	// 菜品金额不单列:逐道菜已带金额,再列小计只会与合计对不上账。
	if strings.Contains(joined, "菜品金额") {
		t.Fatalf("金额区不应单列菜品金额:\n%s", joined)
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
	showDiscount := false
	chunks, _ := sampleRender(t, po.PrintDocGuest, 48, &SampleOverrides{Footer: &footer, ShowDiscount: &showDiscount})
	joined := strings.Join(chunks[0], "\n")

	if !strings.Contains(joined, "未保存的口号") {
		t.Fatalf("覆盖页脚未生效:\n%s", joined)
	}
	if strings.Contains(joined, "谢谢惠顾") {
		t.Fatalf("覆盖后不应出现库里的默认页脚:\n%s", joined)
	}
	if strings.Contains(joined, "优惠") {
		t.Fatalf("覆盖关闭优惠行未生效:\n%s", joined)
	}
	// 合计金额不因展示开关变化(优惠只影响展示,金额已在合计里算过)。
	if !strings.Contains(joined, "98.00") {
		t.Fatalf("合计金额不应受开关影响:\n%s", joined)
	}
}

func TestPreviewTicketSampleMatchesSavedConfig(t *testing.T) {
	setupTestDB(t)

	// 不传任何覆盖(全 nil):与实际打印路径(RenderGuestTicket 读库配置)完全一致。
	chunks, _ := sampleRender(t, po.PrintDocGuest, 48, &SampleOverrides{})
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

func sampleRender(t *testing.T, docType string, paperWidth int, ov *SampleOverrides) ([][]string, int) {
	t.Helper()
	if ov == nil {
		ov = &SampleOverrides{}
	}
	return PreviewTicketSample(docType, paperWidth, *ov)
}

func sampleKitchen(t *testing.T, paperWidth int, showPrice bool) ([][]string, int) {
	t.Helper()
	on := showPrice
	return PreviewTicketSample(po.PrintDocKitchen, paperWidth, SampleOverrides{KitchenShowPrice: &on})
}

// TestPreviewTicketSampleAppend 加菜小票示例:必须按加菜形态渲染,
// 而不是退化成一张整单食客小票(那样配置页就看不见这类票据长什么样)。
func TestPreviewTicketSampleAppend(t *testing.T) {
	setupTestDB(t)

	chunks, lineWidth := sampleRender(t, po.PrintDocGuestAppend, 48, nil)
	if lineWidth != 48 {
		t.Fatalf("80mm 行宽应为 48, got %d", lineWidth)
	}
	joined := strings.Join(chunks[0], "\n")
	for _, want := range []string{"【加菜小票】", "当前合计", "98.00"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("加菜小票示例缺少 %q:\n%s", want, joined)
		}
	}
	// 整单口径的内容不该出现在加菜小票示例上。
	for _, bad := range []string{"【食客小票】", "茶位费", "优惠"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("加菜小票示例不应包含整单要素 %q:\n%s", bad, joined)
		}
	}
}

// TestPreviewTicketSampleKitchenSplit 厨房单示例按打印粒度出多段:
// 一段就是后厨收到的一张纸,配了「一菜一单」却只预览一张整单的话,
// 商家判断不出后厨实际收到几张。
func TestPreviewTicketSampleKitchenSplit(t *testing.T) {
	setupTestDB(t)

	whole := po.KitchenSplitWhole
	chunks, _ := sampleRender(t, po.PrintDocKitchen, 48, &SampleOverrides{KitchenSplit: &whole})
	if len(chunks) != 1 {
		t.Fatalf("整单粒度应只有 1 段, got %d", len(chunks))
	}
	if !strings.Contains(strings.Join(chunks[0], "\n"), "共 3 道 4 份") {
		t.Fatalf("整单应带「共 N 道 M 份」汇总:\n%s", strings.Join(chunks[0], "\n"))
	}

	// 一菜一单:示例订单 3 道菜 → 3 张,各张标题带序号。
	item := po.KitchenSplitItem
	chunks, _ = sampleRender(t, po.PrintDocKitchen, 48, &SampleOverrides{KitchenSplit: &item})
	if len(chunks) != 3 {
		t.Fatalf("一菜一单应为每张菜一段, got %d 段", len(chunks))
	}
	for i, c := range chunks {
		want := ""
		switch i {
		case 0:
			want = "【厨房单】1/3"
		case 1:
			want = "【厨房单】2/3"
		default:
			want = "【厨房单】3/3"
		}
		if !strings.Contains(strings.Join(c, "\n"), want) {
			t.Fatalf("第 %d 段应带序号标题 %q:\n%s", i+1, want, strings.Join(c, "\n"))
		}
	}

	// 按份数拆:示例订单 2+1+1=4 份 → 4 张,每张 x1。
	portion := po.KitchenSplitPortion
	chunks, _ = sampleRender(t, po.PrintDocKitchen, 48, &SampleOverrides{KitchenSplit: &portion})
	if len(chunks) != 4 {
		t.Fatalf("按份数拆应为每份一段, got %d 段", len(chunks))
	}
	for _, c := range chunks {
		joined := strings.Join(c, "\n")
		if strings.Count(joined, "x1") == 0 {
			t.Fatalf("按份拆出的每张应是 x1:\n%s", joined)
		}
	}
}
