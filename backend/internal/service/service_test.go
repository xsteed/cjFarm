package service

import (
	"path/filepath"
	"regexp"
	"testing"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

func TestGenOrderNo(t *testing.T) {
	re := regexp.MustCompile(`^D\d{14}[0-9a-f]{16}$`)
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		no := GenOrderNo()
		if !re.MatchString(no) {
			t.Fatalf("订单号格式非法: %q", no)
		}
		if seen[no] {
			t.Fatalf("订单号发生碰撞: %q", no)
		}
		seen[no] = true
	}
}

func TestRandHex(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{16}$`)
	if !re.MatchString(randHex(16)) {
		t.Fatalf("randHex(16) 应为 16 位十六进制, got %q", randHex(16))
	}
	// 不同长度
	if len(randHex(32)) != 32 {
		t.Fatalf("randHex(32) 长度应为 32")
	}
}

// TestSettingFlagDefaultOnMatchesRenderSemantics 守护「默认开」型开关的回显与渲染语义一致:
// 食客小票的餐位费/优惠行开关,渲染侧(print 包)按「!= "0"」判定,回显侧必须同语义——
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
		key := "print_guest_show_seat_fee"
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
