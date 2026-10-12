package print

// ============================================================================
// 票据强调(加粗)测试
//
// BoldLine 是行级强调的唯一规则来源,三处消费方共用:
//   1. ESC/POS 编码(escpos.go):ESC E 重打,不占宽度、不破坏对齐;
//   2. 预览响应(handler):行带 bold 标记,前端 font-weight 渲染;
//   3. 飞鹅云(feie.go):刻意不放大(<B> 是倍宽标签,会破坏等宽对齐)。
// ============================================================================

import (
	"bytes"
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
		{"长健农场 柴火农家土菜", true},                       // 店名(seed 默认)
		{"凉拌青瓜 (份) x2", false},                      // 菜名
		{"  * 加辣", false},                           // 单菜备注
		{"菜品金额                      86.00", false},  // 金额行(仅合计加粗)
		{"备注: 少辣", false},                           // 整单备注
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

// TestBoldLineRemarkNotTitle 备注以【开头以】结尾的整行确实会被判为标题——
// 这是规则边界:模板生成的备注行恒有「备注: 」前缀,不会整行恰为【】形态,
// 用例固化该前提,防止有人改备注渲染时破坏强调判定。
func TestBoldLineRemarkNotTitle(t *testing.T) {
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
	// 期望加粗:店名、标题、合计 = 3 行(备注不应加粗)。
	if boldCount != 3 {
		bolds := []string{}
		for _, l := range lines {
			if BoldLine(l) {
				bolds = append(bolds, l)
			}
		}
		t.Fatalf("样例小票应恰好 3 行加粗(店名/标题/合计), got %d: %v", boldCount, bolds)
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
