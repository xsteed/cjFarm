package service

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// serialNoRe 单号格式:1 位前缀 + 12 位 yyMMddHHmmss + 7 位 Crockford Base32。
// 字符集剔除 I/L/O/U(易与 1/1/0/V 混淆),见 base32Alphabet 的说明。
var serialNoRe = regexp.MustCompile(`^[A-Za-z]\d{12}[0-9A-HJKMNP-TV-Z]{7}$`)

// TestGenOrderNo 订单号格式与不碰撞,同时把「总长 20 位」这条对外契约钉死
// (小票版面按此宽度排版,长度变了会影响打印)。
func TestGenOrderNo(t *testing.T) {
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		no := GenOrderNo()
		if len(no) != SerialNoLen {
			t.Fatalf("订单号长度应为 %d, got %d (%q)", SerialNoLen, len(no), no)
		}
		if !serialNoRe.MatchString(no) {
			t.Fatalf("订单号格式非法: %q", no)
		}
		if seen[no] {
			t.Fatalf("订单号发生碰撞: %q", no)
		}
		seen[no] = true
	}
}

// TestRandBase32 随机段的三个性质:长度严格等于 n、字符集限于 Crockford 表、非正长度安全。
func TestRandBase32(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]+$`)
	for _, n := range []int{1, 7, 32} {
		got := randBase32(n)
		if len(got) != n {
			t.Fatalf("randBase32(%d) 长度应为 %d, got %d (%q)", n, n, len(got), got)
		}
		if !re.MatchString(got) {
			t.Fatalf("randBase32(%d) 出现字母表外字符: %q", n, got)
		}
	}
	// 非正长度应安全返回空串。
	if got := randBase32(0); got != "" {
		t.Fatalf("randBase32(0) 应为空串, got %q", got)
	}
	if got := randBase32(-1); got != "" {
		t.Fatalf("randBase32(-1) 应为空串, got %q", got)
	}
}

// TestRandBase32CoversAlphabet 随机段应能覆盖整个字母表,否则实际熵低于标称的 35bit。
// 采样 1000×7=7000 个字符(每位期望出现约 219 次),缺席任一字符都说明映射有偏。
func TestRandBase32CoversAlphabet(t *testing.T) {
	seen := make(map[byte]bool, len(base32Alphabet))
	for i := 0; i < 1000; i++ {
		for _, c := range []byte(randBase32(serialNoRandomLen)) {
			seen[c] = true
		}
	}
	for i := 0; i < len(base32Alphabet); i++ {
		if !seen[base32Alphabet[i]] {
			t.Errorf("字母 %q 从未出现,随机段分布有偏", base32Alphabet[i])
		}
	}
}

// TestGenRefundNo 守护退款单号「与订单号结构同构、首位区分款项性质」这一契约。
//
// 历史实现写作 "R"+GenOrderNo()[1:],静默依赖「订单号首字母恰好是待裁掉的那一位」;
// 若有人改动订单号的前缀或长度,退款号会随之变形且编译期无任何提示,
// 直到 refund_no 唯一索引或人工对账时才发现。这里用格式与长度断言把契约锁死。
func TestGenRefundNo(t *testing.T) {
	cases := []struct {
		prefix string
		want   string
	}{
		{RefundNoRefund, RefundNoRefund},
		{RefundNoManual, RefundNoManual},
		{"", RefundNoRefund}, // 空前缀兜底:不得产生无前缀、无法辨识性质的单据号
	}
	for _, c := range cases {
		no := GenRefundNo(c.prefix)
		if !strings.HasPrefix(no, c.want) {
			t.Fatalf("GenRefundNo(%q) 应以 %q 开头, got %q", c.prefix, c.want, no)
		}
		if !serialNoRe.MatchString(no) {
			t.Fatalf("退款单号格式非法: prefix=%q got %q", c.prefix, no)
		}
	}

	// 与订单号等长,且 2000 次内不碰撞(代替已废弃的时间戳降级方案)。
	if len(GenRefundNo(RefundNoRefund)) != len(GenOrderNo()) {
		t.Fatalf("退款单号应与订单号等长")
	}
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		no := GenRefundNo(RefundNoRefund)
		if seen[no] {
			t.Fatalf("退款单号发生碰撞: %q", no)
		}
		seen[no] = true
	}
}

// TestSettingFlagDefaultOnMatchesRenderSemantics 守护「默认开」型开关的回显与渲染语义一致:
// 食客小票的优惠行等段落开关,渲染侧(print 包)按「!= "0"」判定,回显侧必须同语义——
// 否则配置键缺失时,配置页显示「关」而小票上仍在打印,两边自相矛盾。
func TestSettingFlagDefaultOnMatchesRenderSemantics(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "flagdefaulton.db"))
	defer store.DB.Close()

	// 三种取值逐一对照「渲染侧」的判定结果。
	cases := []struct {
		val      string // 库里的原始值("" 表示键不存在)
		want     string // SettingFlagDefaultOn 期望值
		renderOn bool   // 渲染侧(val != "0")的判定
	}{
		{val: "", want: "1", renderOn: true},   // 键缺失:默认开
		{val: "1", want: "1", renderOn: true},  // 明确开
		{val: "0", want: "0", renderOn: false}, // 明确关
		{val: "x", want: "1", renderOn: true},  // 脏值:按默认开处理
	}
	for i, c := range cases {
		key := "print_guest_show_discount"
		if c.val == "" {
			// 键不存在(直接删掉 seed 补的行,模拟极端缺失)。
			if _, err := store.DB.Exec(`DELETE FROM tb_config WHERE cfg_key=?`, key); err != nil {
				t.Fatalf("清理配置键失败: %v", err)
			}
		} else if err := dao.SetSetting(key, c.val); err != nil {
			t.Fatalf("写入配置失败: %v", err)
		}
		got := SettingFlagDefaultOn(key)
		if got != c.want || (got == "1") != c.renderOn {
			t.Fatalf("case %d(val=%q): got %q, want %q(渲染侧 %v)", i, c.val, got, c.want, c.renderOn)
		}
	}
}
