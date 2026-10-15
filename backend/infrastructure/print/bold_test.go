package print

// ============================================================================
// 票据强调(加粗)测试
//
// BoldLine 是行级强调的唯一规则来源,三处消费方共用:
//   1. ESC/POS 编码(escpos.go):ESC E 重打,不占宽度、不破坏对齐;
//   2. 预览响应(handler):行带 bold 标记,前端 font-weight 渲染;
//   3. 飞鹅云(feie.go):<BOLD> 标签(官方「字体加粗」,不改变字符宽度);
//      不可用 <B> —— 它的官方语义是「放大一倍」,会破坏等宽对齐。
// ============================================================================

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoldLineRules(t *testing.T) {
	setupTestDB(t)

	cases := []struct {
		line string
		bold bool
	}{
		{"【顾客小票】", false},                           // 点菜单单据名:副标题弱化,不加粗
		{"【结账小票】", false},                           // 结账小票单据名:同为副标题,不加粗
		{"【厨房单】", true},                             // 标题
		{"【加菜单】", true},                             // 标题
		{"合计                          98.00", true}, // 合计行
		// 店名不再走形态反推:反推要拿店名比对,而本函数按行调用,那等于每行查一次
		// 配置表。店名行改由渲染层显式标记(见 shopNameLines)。
		{"长健农场 · 柴火农家土菜", false},                    // 店名(seed 默认:「品牌 · 标语」)
		{"凉拌青瓜 (份) x2", false},                      // 菜名
		{"  * 加辣", true},                            // 单菜备注(做法/忌口,必须醒目)
		{"菜品金额                      86.00", false},  // 金额行(仅合计加粗)
		{"备注: 少辣", true},                            // 整单备注
		{"整单备注: 少辣", true},                          // 厨房单整单备注
		{"1. 凉拌青瓜 (份) x2", false},                   // 编号后的菜名
		{"共 2 道 3 份", false},                        // 份数汇总
		{"--------------------------------", false}, // 分隔线
		{"", false}, // 空行
		{"【恰好整行都是书名号的文本】", true}, // 整行【】形态按标题规则加粗(误判后果仅多一行加粗,无害)
	}
	for _, c := range cases {
		if got := BoldLine(c.line); got != c.bold {
			t.Errorf("BoldLine(%q) = %v, want %v", c.line, got, c.bold)
		}
	}
}

// TestBoldLineRemarkLines 备注行按「备注强调」规则加粗,且不受备注内容形态影响:
// 备注文本恰好是【…】形态时仍只算一次强调(前缀「备注: 」已把它挡在标题规则之外)。
// 判定必须走消费方的唯一入口 BoldText(显式标记优先 + 形态反推兜底)。
func TestBoldLineRemarkLines(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	o.OrderRemark = "【vip 包间】"
	lines := RenderGuestTicket(o, items, 48)

	bolds := []string{}
	for _, l := range lines {
		if text, ok := BoldText(l); ok {
			bolds = append(bolds, text)
		}
	}
	// 期望加粗:单菜备注、合计、整单备注 = 3 行。
	// 店名改为放大行(不走进加粗判定)、单据名副标题弱化,都不计入加粗。
	if len(bolds) != 3 {
		t.Fatalf("样例小票应恰好 3 行加粗(单菜备注/合计/备注), got %d: %q", len(bolds), bolds)
	}
}

// TestLongRemarkBoldOnEveryLine 长备注按纸宽折行后,**每一行**都要加粗。
//
// 这是形态反推(BoldLine)的盲区:折行由 splitLines 完成,续行没有「* 」「整单备注: 」
// 前缀,反推不出来 —— 同一段备注会前半加粗、后半不加粗,后厨很容易把后半句当成普通说明。
func TestLongRemarkBoldOnEveryLine(t *testing.T) {
	setupTestDB(t)

	// 单元级:appendRemarkLines 是渲染层所有备注的唯一出口,逐行标记是它的契约,
	// 直接断言它最可靠(从成品票面反推「哪几行是续行」本身就是做不到的事)。
	long := "整单备注: 不要香菜不要葱不要蒜,鱼要清蒸不要红烧,米饭少放点"
	lines := appendRemarkLines(nil, long, 32)
	if len(lines) < 2 {
		t.Fatalf("这段备注在 32 格下应折成多行, got %d: %q", len(lines), lines)
	}
	for i, l := range lines {
		text, bold := BoldText(l)
		if !bold {
			t.Fatalf("第 %d 行未加粗: %q", i+1, text)
		}
		if strings.ContainsRune(text, '\x02') {
			t.Fatalf("控制前缀不得残留在正文里: %q", text)
		}
	}
	// 票面形态不变:前缀只在首行,续行不重复。
	if first, _ := BoldText(lines[0]); !strings.HasPrefix(first, "整单备注: ") {
		t.Fatalf("首行应保留原前缀: %q", first)
	}
	if second, _ := BoldText(lines[1]); strings.HasPrefix(second, "整单备注: ") {
		t.Fatalf("续行不应重复前缀: %q", second)
	}

	// 端到端:渲染层产出的控制行必须被发送层完全消费 —— 控制字符一旦漏剥就是直接
	// 打到纸上(或把标签当正文发出去)。渲染结果里有控制行是正常的,落地结果里没有才正常。
	o, items := sampleOrder()
	o.OrderRemark = "不要香菜不要葱不要蒜,鱼要清蒸不要红烧"
	items[0].Remark = "不要香菜不要葱不要蒜"
	for name, rendered := range map[string][]string{
		"厨房单":  RenderKitchenTicket(o, items, 32, KitchenTicketOptions{Title: "【厨房单】"}),
		"顾客小票": RenderGuestTicket(o, items, 32),
		"加菜小票": RenderGuestAppendTicket(o, items, 32),
	} {
		content := strings.Join(splitFeieContent(rendered), "")
		if strings.ContainsRune(content, '\x02') || strings.ContainsRune(content, '\x03') {
			t.Fatalf("%s 飞鹅 content 残留控制字符: %q", name, content)
		}
		if bytes.ContainsAny(encodeESCPOS(rendered), "\x02\x03") {
			t.Fatalf("%s ESC/POS 字节流残留控制字符", name)
		}
		// 备注文本本身必须留在票面上(前缀只被剥掉,内容不能丢)。
		if !strings.Contains(content, "不要香菜") {
			t.Fatalf("%s 备注内容丢失: %q", name, content)
		}
	}
}

// TestEncodeESCPOSBoldBytes 强调行必须被 ESC E 1 … ESC E 0 包裹,普通行不带指令。
func TestEncodeESCPOSBoldBytes(t *testing.T) {
	setupTestDB(t)

	raw := encodeESCPOS([]string{"【厨房单】", "凉拌青瓜 x2", "合计                          98.00"})
	escEOn := []byte{0x1B, 0x45, 0x01}
	escEOff := []byte{0x1B, 0x45, 0x00}

	if bytes.Count(raw, escEOn) != 2 {
		t.Fatalf("两行强调(标题+合计)应有 2 个 ESC E 1,字节流: %x", raw)
	}
	if bytes.Count(raw, escEOff) != 2 {
		t.Fatalf("两行强调应有 2 个 ESC E 0 复位,字节流: %x", raw)
	}
	// 普通行(菜名)前后不应有加粗指令:菜名的 GBK 字节前直接是上一行的换行。
	idx := bytes.Index(raw, []byte("\n"))
	next := bytes.Index(raw[idx+1:], escEOn)
	if next == 0 {
		t.Fatalf("普通行不应紧跟加粗指令: %x", raw)
	}
	// 指令顺序:先 ESC E 1,再是行内容;行结束换行后 ESC E 0。
	firstOn := bytes.Index(raw, escEOn)
	if firstOn != 2 { // ESC @ 初始化(2 字节)之后
		t.Fatalf("首个 ESC E 1 应紧跟初始化指令(偏移 2), got %d", firstOn)
	}
}

// TestFeieBoldTag 飞鹅通道的强调行必须套官方 <BOLD> 标签。
//
// 官方 content 标签表里:<B> 是「放大一倍」,<BOLD> 才是「字体加粗」。
// <BOLD> 不改变字符宽度,与 ESC/POS 的 ESC E 重打等价;用 <B> 会把
// ticket.go 算好的等宽列对齐撑坏。
func TestFeieBoldTag(t *testing.T) {
	setupTestDB(t)

	lines := []string{"【厨房单】", "凉拌青瓜 (份) x2", "合计                          98.00"}
	content := strings.Join(splitFeieContent(lines), "")

	if !strings.Contains(content, "<BOLD>【厨房单】</BOLD>") {
		t.Fatalf("厨房单标题行应套 <BOLD>: %q", content)
	}
	if !strings.Contains(content, "<BOLD>合计") {
		t.Fatalf("合计行应套 <BOLD>: %q", content)
	}
	if strings.Contains(content, "<BOLD>凉拌青瓜") {
		t.Fatalf("普通菜名行不应加粗: %q", content)
	}
	// 纯文本行(非放大行)绝不能被套上 <B>:它会把 ticket.go 算好的等宽列对齐撑坏。
	// 放大行另走 SplitBigLine / SplitBigLeftLine 分支,输入里没有它们,所以这里
	// 一旦出现 <B> 就说明强调行用错了标签。
	if strings.Contains(content, "<B>") {
		t.Fatalf("强调行不得使用 <B>(官方语义是「放大一倍」,会破坏列对齐): %q", content)
	}
}

// TestFeieBoldKeepsWidth 加粗标签不得改动正文本身:
// 标签之外的字符(尤其是 pad 出来的对齐空格)必须逐字节保留。
func TestFeieBoldKeepsWidth(t *testing.T) {
	setupTestDB(t)

	const line = "合计                          98.00"
	got := feieLinePiece(line)
	if got != "<BOLD>"+line+"</BOLD>" {
		t.Fatalf("加粗行落地形态错误: %q", got)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(got, "<BOLD>"), "</BOLD>")
	if inner != line {
		t.Fatalf("加粗标签不得改动正文(含对齐空格): %q", inner)
	}

	// 普通行保持纯文本,不带任何标签。
	const plain = "凉拌青瓜 (份) x2"
	if got := feieLinePiece(plain); got != plain {
		t.Fatalf("普通行不应带标签: %q", got)
	}
}
