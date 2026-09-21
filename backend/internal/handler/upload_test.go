package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodeTestImage(t *testing.T, enc func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := enc(&buf, img); err != nil {
		t.Fatalf("编码测试图片失败: %v", err)
	}
	return buf.Bytes()
}

func TestIsValidImage(t *testing.T) {
	pngData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) })
	jpgData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return jpeg.Encode(b, m, nil) })
	gifData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return gif.Encode(b, m, nil) })

	cases := []struct {
		name string
		ext  string
		data []byte
		want bool
	}{
		{"png正确", ".png", pngData, true},
		{"jpg正确", ".jpg", jpgData, true},
		{"jpeg正确", ".jpeg", jpgData, true},
		{"gif正确", ".gif", gifData, true},
		{"png内容声明为jpg(格式不符)", ".jpg", pngData, false},
		{"webp魔数正确", ".webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("VP8 ")...), true},
		{"webp魔数错误", ".webp", []byte("XXXX\x00\x00\x00\x00WEBPxxxx"), false},
		{"bmp魔数正确", ".bmp", []byte("BM\x00\x00\x00\x00\x00\x00"), true},
		{"bmp魔数错误", ".bmp", []byte("XXrest"), false},
		{"伪造成png的文本", ".png", []byte("not an image at all, just plain text"), false},
		{"空内容", ".png", []byte{}, false},
		{"未知扩展名", ".txt", pngData, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isValidImage(c.ext, c.data); got != c.want {
				t.Fatalf("isValidImage(%q) = %v, want %v", c.ext, got, c.want)
			}
		})
	}
}
