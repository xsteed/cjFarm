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
		{"【食客小票】", true},                            // 标题
		{"【厨房单】", true},                             // 标题
		{"【加菜单】", true},                             // 标题
		{"合计                          98.00", true}, // 合计行
		{"长健农场 · 柴火农家土菜", true},                     // 店名(seed 默认:「品牌 · 标语」)
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
func TestBoldLineRemarkLines(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder()
	o.OrderRemark = "【vip 包间】"
	lines := RenderGuestTicket(o, items, 48)
	boldCount := 0
	for _, l := range lines {
		if BoldLine(l) {
			boldCount++
		}
	}
	// 期望加粗:店名、标题、单菜备注、合计、整单备注 = 5 行。
	if boldCount != 5 {
		bolds := []string{}
		for _, l := range lines {
			if BoldLine(l) {
				bolds = append(bolds, l)
			}
		}
		t.Fatalf("样例小票应恰好 5 行加粗(店名/标题/单菜备注/合计/备注), got %d: %v", boldCount, bolds)
	}
}

// TestEncodeESCPOSBoldBytes 强调行必须被 ESC E 1 … ESC E 0 包裹,普通行不带指令。
func TestEncodeESCPOSBoldBytes(t *testing.T) {
	setupTestDB(t)

	raw := encodeESCPOS([]string{"【食客小票】", "凉拌青瓜 x2", "合计                          98.00"})
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

	lines := []string{"【食客小票】", "凉拌青瓜 (份) x2", "合计                          98.00"}
	content := strings.Join(splitFeieContent(lines), "")

	if !strings.Contains(content, "<BOLD>【食客小票】</BOLD>") {
		t.Fatalf("标题行应套 <BOLD>: %q", content)
	}
	if !strings.Contains(content, "<BOLD>合计") {
		t.Fatalf("合计行应套 <BOLD>: %q", content)
	}
	if strings.Contains(content, "<BOLD>凉拌青瓜") {
		t.Fatalf("普通菜名行不应加粗: %q", content)
	}
	// <B> 只能出现在放大行的 <C><B> 组合里;强调行绝不能用它。
	if strings.Contains(content, "<B>") {
		t.Fatalf("强调行不得使用 <B>(官方语义是放大一倍,会破坏列对齐): %q", content)
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
