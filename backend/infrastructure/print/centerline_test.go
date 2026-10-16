package print

// ============================================================================
// 居中控制行(centerMarkLine)测试
//
// 单据名副标题(【顾客小票】等)用机器居中而非空格补位:空格居中建立在
// 「半角空格恰好是汉字一半宽」的假设上,且部分云通道会裁掉行首空格。
// 两条发送通道都必须消费该控制行并剥掉前缀,否则控制字符会被当正文打出来。
// ============================================================================

import (
	"bytes"
	"strings"
	"testing"
)

func TestCenterLineConsumedBySenders(t *testing.T) {
	setupTestDB(t)

	line := centerMarkLine("【顾客小票】")
	if text, ok := SplitCenterLine(line); !ok || text != "【顾客小票】" {
		t.Fatalf("SplitCenterLine 解析失败: %q %v", text, ok)
	}
	if _, ok := SplitCenterLine("【顾客小票】"); ok {
		t.Fatal("普通文本行不应被判为居中行")
	}

	// ESC/POS:ESC a 1 居中,副标题弱化不加粗,打完还原左对齐,控制前缀不得残留。
	raw := encodeESCPOS([]string{line})
	if !bytes.Contains(raw, []byte{0x1B, 0x61, 0x01}) {
		t.Fatalf("ESC/POS 应带居中指令 ESC a 1, bytes=%x", raw)
	}
	if bytes.Contains(raw, []byte{0x1B, 0x45, 0x01}) {
		t.Fatal("【顾客小票】副标题不应加粗")
	}
	if !bytes.Contains(raw, []byte{0x1B, 0x61, 0x00}) {
		t.Fatal("居中行打完应还原左对齐")
	}
	if bytes.ContainsAny(raw, "\x02\x03") {
		t.Fatalf("控制前缀不应出现在出纸字节里, bytes=%x", raw)
	}

	// 飞鹅:<C> 标签,不加粗。
	chunks := splitFeieContent([]string{line})
	if len(chunks) != 1 || chunks[0] != "<C>【顾客小票】</C>" {
		t.Fatalf("飞鹅应转成 <C> 标签, got %v", chunks)
	}

	// 【加菜小票】按标题规则加粗:<C> 与 <BOLD> 叠加 / ESC a 1 + ESC E。
	bold := centerMarkLine("【加菜小票】")
	content := strings.Join(splitFeieContent([]string{bold}), "")
	if !strings.Contains(content, "<C><BOLD>【加菜小票】</BOLD></C>") {
		t.Fatalf("加菜小票标题应居中并加粗: %q", content)
	}
	rawBold := encodeESCPOS([]string{bold})
	if !bytes.Contains(rawBold, []byte{0x1B, 0x45, 0x01}) {
		t.Fatal("加菜小票标题应加粗")
	}
}
