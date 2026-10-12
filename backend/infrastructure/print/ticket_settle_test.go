package print

// ============================================================================
// 结账小票:结算信息块与二维码
//
// 这两块都是「结账小票」区别于「下单时那张预估小票」的实质内容:
//   - 结算信息回答「怎么付的、什么时候付的、实际付了多少」;
//   - 二维码让顾客扫码自助查看/重打小票。
// 两者都必须可控(有值才打 / 开关默认关),否则会在不需要的场景里吐出废行与废码。
// ============================================================================

import (
	"bytes"
	"strings"
	"testing"

	"dining-system/domain/po"
)

// settledOrder 返回一份「已结账」的样例订单(支付方式/结账时间齐全)。
func settledOrder() (po.Order, []po.OrderItem) {
	o, items := sampleOrder()
	payType := "微信"
	settleTime := "2026-09-21 13:05:00"
	o.PayType = &payType
	o.SettleTime = &settleTime
	o.PaidAmount = o.TotalAmount
	return o, items
}

func renderSettled(t *testing.T) string {
	t.Helper()
	o, items := settledOrder()
	return strings.Join(RenderGuestTicket(o, items, 48), "\n")
}

// TestGuestTicketSettleInfo 已结账订单的小票必须带上支付方式与结账时间。
func TestGuestTicketSettleInfo(t *testing.T) {
	setupTestDB(t)
	got := renderSettled(t)
	for _, want := range []string{"支付方式", "微信", "结账时间", "2026-09-21 13:05:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("结账小票缺少结算信息 %q:\n%s", want, got)
		}
	}
}

// TestGuestTicketSettleInfoOmittedWhenUnsettled 未结算订单(下单即出票)不该留下空标签。
func TestGuestTicketSettleInfoOmittedWhenUnsettled(t *testing.T) {
	setupTestDB(t)
	o, items := sampleOrder() // 无 PayType / SettleTime
	got := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	for _, bad := range []string{"支付方式", "结账时间", "核销时间"} {
		if strings.Contains(got, bad) {
			t.Fatalf("未结算订单不应打印 %q:\n%s", bad, got)
		}
	}
}

// TestGuestTicketPaidLineOnlyWhenDiffers 实收行只在与合计不一致时出现
// (避免每张票都印两遍同一个金额)。
func TestGuestTicketPaidLineOnlyWhenDiffers(t *testing.T) {
	setupTestDB(t)

	// 1) 实收 = 合计:不补「实收」行。
	if got := renderSettled(t); strings.Contains(got, "实收") {
		t.Fatalf("实收等于合计时不该再印一行实收:\n%s", got)
	}

	// 2) 部分退款后实收 < 合计:必须补一行,且是顾客真正付的钱。
	o, items := settledOrder()
	o.PaidAmount = 6400
	o.RefundAmount = 2000
	got := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	if !strings.Contains(got, "实收") || !strings.Contains(got, "64.00") {
		t.Fatalf("实收与合计不一致时应打印实收行:\n%s", got)
	}
	if !strings.Contains(got, "已退款") || !strings.Contains(got, "-20.00") {
		t.Fatalf("有退款金额时应打印已退款行:\n%s", got)
	}
}

// TestGuestTicketCreditSettled 挂账核销后出的票要能看出「已结」与核销时间。
func TestGuestTicketCreditSettled(t *testing.T) {
	setupTestDB(t)
	o, items := settledOrder()
	o.SettleType = "credit"
	o.CreditStatus = 2
	o.CreditAmount = 8400
	creditTime := "2026-09-22 10:00:00"
	o.CreditSettleTime = &creditTime
	got := strings.Join(RenderGuestTicket(o, items, 48), "\n")
	if !strings.Contains(got, "【挂账已结】") {
		t.Fatalf("核销后小票应标注挂账已结:\n%s", got)
	}
	if !strings.Contains(got, "核销时间") {
		t.Fatalf("核销后小票应带上核销时间:\n%s", got)
	}
}

// TestGuestTicketQRDefaultOff 二维码默认关闭,开启后落在票据最末且带 H5 地址。
func TestGuestTicketQRDefaultOff(t *testing.T) {
	setupTestDB(t)

	// 1) 默认关:票上不应有任何二维码标记行。
	o, items := settledOrder()
	if got := RenderGuestTicket(o, items, 48); hasQRLine(got) {
		t.Fatalf("默认不应打印二维码:\n%s", strings.Join(got, "\n"))
	}

	// 2) 开启:末行为二维码标记,内容指向顾客端小票页。
	setPrintSetting(t, "h5_base_url", "https://dining.example.com")
	setPrintSetting(t, "print_guest_qr", "1")
	lines := RenderGuestTicket(o, items, 48)
	last := lines[len(lines)-1]
	content, ok := SplitQRLine(last)
	if !ok {
		t.Fatalf("开启后末行应为二维码标记行, got %q", last)
	}
	if want := "https://dining.example.com/printTicket?orderNo=D20260921120000abcdef"; content != want {
		t.Fatalf("二维码内容 = %q, want %q", content, want)
	}
}

// TestGuestTicketQROffWithoutH5Base H5 地址未配置时不出码:打一个手机扫不通的码更糟。
func TestGuestTicketQROffWithoutH5Base(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "h5_base_url", "")
	setPrintSetting(t, "print_guest_qr", "1")
	o, items := settledOrder()
	if got := RenderGuestTicket(o, items, 48); hasQRLine(got) {
		t.Fatalf("H5 地址为空时不应出码:\n%s", strings.Join(got, "\n"))
	}
}

func hasQRLine(lines []string) bool {
	for _, l := range lines {
		if _, ok := SplitQRLine(l); ok {
			return true
		}
	}
	return false
}

// TestEncodeESCPOSQR 二维码标记行必须被 ESC/POS 编码器消费掉:
// 既不能把控制前缀当正文打出来,也要真的吐出 GS ( k 指令。
func TestEncodeESCPOSQR(t *testing.T) {
	setupTestDB(t)
	content := "https://dining.example.com/printTicket?orderNo=D1"
	data := encodeESCPOS([]string{"【食客小票】", qrMarkLine(content)})

	// 1) 标记前缀绝不能出现在字节流里(否则会打出一串乱码)。
	if bytes.Contains(data, []byte(qrMarkPrefix)) {
		t.Fatal("二维码标记前缀被打进了 ESC/POS 字节流")
	}
	if bytes.Contains(data, []byte("\x02")) {
		t.Fatal("ESC/POS 字节流里出现了不可见控制字符")
	}
	// 2) 必须包含二维码写入与打印指令(GS ( k 系列)。
	//    写入指令的长度字段 = cn + fn + m + 数据字节数,随内容长度变化,不能写死。
	n := len(content) + 3
	if !bytes.Contains(data, []byte{0x1D, 0x28, 0x6B, byte(n), byte(n >> 8), 0x31, 0x50, 0x30}) {
		t.Fatal("缺少 GS ( k 写入二维码数据指令")
	}
	if !bytes.Contains(data, []byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x51, 0x30}) {
		t.Fatal("缺少 GS ( k 打印二维码指令")
	}
	// 3) 二维码内容本身要原样进字节流。
	if !bytes.Contains(data, []byte(content)) {
		t.Fatal("二维码内容未写入字节流")
	}
	// 4) 切纸指令仍在(二维码不能把票尾的切纸吃掉)。
	if !bytes.Contains(data, []byte{0x1D, 0x56, 0x42, 0x00}) {
		t.Fatal("缺少切纸指令")
	}
}

// TestEncodeESCPOSQRTooLong 超长内容直接放弃出码,不把打不出来的指令发给打印机。
func TestEncodeESCPOSQRTooLong(t *testing.T) {
	setupTestDB(t)
	long := strings.Repeat("a", qrMaxDataBytes+1)
	if b := encodeQR(long); len(b) != 0 {
		t.Fatalf("超长内容不应生成二维码指令, got %d 字节", len(b))
	}
	if b := encodeQR(""); len(b) != 0 {
		t.Fatalf("空内容不应生成二维码指令, got %d 字节", len(b))
	}
}

// TestSplitFeieContentQR 飞鹅通道把二维码标记行转成 <QR> 标签,而不是原样文本。
func TestSplitFeieContentQR(t *testing.T) {
	setupTestDB(t)
	content := "https://dining.example.com/printTicket?orderNo=D1"
	chunks := splitFeieContent([]string{"【食客小票】", qrMarkLine(content)})
	got := strings.Join(chunks, "")
	if !strings.Contains(got, "<QR>"+content+"</QR>") {
		t.Fatalf("飞鹅内容缺少 <QR> 标签: %q", got)
	}
	if strings.Contains(got, qrMarkPrefix) {
		t.Fatalf("飞鹅内容不应残留二维码标记前缀: %q", got)
	}
}

// TestGuestTicketQROverride 示例预览能用未保存的表单值覆盖二维码开关(所见即所改)。
func TestGuestTicketQROverride(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "h5_base_url", "https://dining.example.com")

	on := true
	off := false

	// 库里是关的,但表单勾上了 → 预览里要出码。
	chunks, _ := PreviewTicketSample(po.PrintDocGuest, 48, SampleOverrides{QR: &on})
	if !hasQRLine(chunks[0]) {
		t.Fatal("覆盖为开启时预览应含二维码行")
	}
	// 库里是开的,但表单取消了 → 预览里不该出码。
	setPrintSetting(t, "print_guest_qr", "1")
	chunks, _ = PreviewTicketSample(po.PrintDocGuest, 48, SampleOverrides{QR: &off})
	if hasQRLine(chunks[0]) {
		t.Fatal("覆盖为关闭时预览不应含二维码行")
	}
}
