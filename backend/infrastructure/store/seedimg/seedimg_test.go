package seedimg

import "testing"

func TestSeedImagesEmbedded(t *testing.T) {
	if got, want := Count(), 23; got != want {
		t.Fatalf("内嵌种子图数量 = %d, want %d", got, want)
	}

	files := Files()
	for _, name := range []string{"pay_wx.png", "dining_20260918_001.jpeg"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("内嵌种子图缺少 %s", name)
		}
	}
}
