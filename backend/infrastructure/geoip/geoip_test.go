package geoip

// 城市解析的真值验证依赖 data/ip2region.xdb(gitignored 运行期资产):
// 文件缺失时整组用例跳过,而不是失败 —— 没有库文件的城市功能本就降级为空。

import (
	"path/filepath"
	"sync"
	"testing"

	"dining-system/infrastructure/conf"
)

func geoIPInit(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "data", "ip2region.xdb"))
	if err != nil {
		t.Skipf("解析路径失败: %v", err)
	}
	t.Setenv(conf.EnvVisitIP2RegionPath, path)
}

func TestCityOf(t *testing.T) {
	geoIPInit(t)

	cases := []struct {
		ip     string
		want   string
		exact  bool // true=精确相等,false=非空即可(库更新后城市措辞可能变化)
	}{
		{"114.114.114.114", "中国 江苏省 南京市", false},
		{"223.5.5.5", "中国 浙江省 杭州市", false},
		{"192.168.1.100", "内网", true},
		{"127.0.0.1", "内网", true},
		{"", "", true},
		{"not-an-ip", "", true},
	}
	for _, c := range cases {
		got := CityOf(c.ip)
		if c.exact {
			if got != c.want {
				t.Errorf("CityOf(%q) = %q, 期望 %q", c.ip, got, c.want)
			}
			continue
		}
		if c.want == "" && got != "" {
			t.Errorf("CityOf(%q) = %q, 期望空", c.ip, got)
		}
		if c.want != "" && got == "" {
			t.Errorf("CityOf(%q) = 空, 期望非空(形如 %q)", c.ip, c.want)
		}
	}
}

// TestCityOfWithoutDB 缺库降级:返回空串,绝不能 panic 或影响调用方。
func TestCityOfWithoutDB(t *testing.T) {
	t.Setenv(conf.EnvVisitIP2RegionPath, filepath.Join(t.TempDir(), "not-exist.xdb"))
	// once 已在上面用例触发过;这里直接重置加载状态验证降级路径
	once = sync.Once{}
	searcher = nil
	if got := CityOf("114.114.114.114"); got != "" {
		t.Fatalf("缺库时应返回空串, got %q", got)
	}
	once = sync.Once{}
	searcher = nil
}
