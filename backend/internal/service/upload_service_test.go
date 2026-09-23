package service

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// uploadInitDB 初始化 SQLite 临时库,上传目录指向不存在的位置避免误导入。
func uploadInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "upload.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// uploadPNG 生成一张真实可解码的 PNG 图片字节。
func uploadPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

// uploadJPEG 生成一张真实可解码的 JPEG 图片字节。
func uploadJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("生成 JPEG 失败: %v", err)
	}
	return buf.Bytes()
}

// uploadGIF 生成一张真实可解码的 GIF 图片字节。
func uploadGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("生成 GIF 失败: %v", err)
	}
	return buf.Bytes()
}

// uploadPNGOfSize 生成指定尺寸(宽 w、高 h)的真实可解码 PNG。
// 纯色图压缩率极高,声明超大尺寸时字节数仍很小,可模拟「解压炸弹」(压缩小、解码大)。
func uploadPNGOfSize(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

// TestUploadIsValidImage 校验图片内容真实性:解码格式与扩展名一致,webp/bmp 走魔数。
func TestUploadIsValidImage(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		data []byte
		want bool
	}{
		{"真实png", ".png", uploadPNG(t), true},
		{"伪造png", ".png", []byte("not an image"), false},
		{"真实jpg", ".jpg", uploadJPEG(t), true},
		{"伪造jpg", ".jpeg", []byte("not an image"), false},
		{"真实gif", ".gif", uploadGIF(t), true},
		{"伪造gif", ".gif", []byte("not an image"), false},
		{"webp魔数正确", ".webp", []byte("RIFF1234WEBP"), true},
		{"webp魔数错误", ".webp", []byte("RIFX1234WEBP"), false},
		{"webp过短", ".webp", []byte("RIFF"), false},
		{"bmp魔数正确", ".bmp", []byte("BMxxxx"), true},
		{"bmp魔数错误", ".bmp", []byte("xx"), false},
		{"未知扩展名", ".txt", []byte("anything"), false},
		{"超宽解压炸弹", ".png", uploadPNGOfSize(t, 8193, 1), false},
		{"超高解压炸弹", ".png", uploadPNGOfSize(t, 1, 8193), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isValidImage(c.ext, c.data)
			if (got == nil) != c.want {
				t.Fatalf("isValidImage(%q)=%v, want 通过=%v", c.ext, got, c.want)
			}
		})
	}
}

// TestUploadSaveUpload 校验上传的扩展名白名单、大小上限、内容校验与落库。
func TestUploadSaveUpload(t *testing.T) {
	t.Run("非法扩展名", func(t *testing.T) {
		uploadInitDB(t)
		_, err := SaveUpload("file.txt", []byte("hello"))
		if err == nil || !strings.Contains(err.Error(), "仅支持上传图片文件") {
			t.Fatalf("非法扩展名应报错, got %v", err)
		}
	})
	t.Run("超过大小上限", func(t *testing.T) {
		uploadInitDB(t)
		_, err := SaveUpload("big.png", make([]byte, MaxImageBytes+1))
		if err == nil || !strings.Contains(err.Error(), "文件大小不能超过 5MB") {
			t.Fatalf("超限应报错, got %v", err)
		}
	})
	t.Run("内容不是有效图片", func(t *testing.T) {
		uploadInitDB(t)
		_, err := SaveUpload("fake.png", []byte("polyglot-fake"))
		if err == nil || !strings.Contains(err.Error(), "文件内容不是有效的图片") {
			t.Fatalf("伪造内容应报错, got %v", err)
		}
	})
	t.Run("正常上传返回URL并入库", func(t *testing.T) {
		uploadInitDB(t)
		wantData := uploadPNG(t)
		url, err := SaveUpload("dish.png", wantData)
		if err != nil {
			t.Fatalf("SaveUpload 失败: %v", err)
		}
		if !strings.HasPrefix(url, store.UploadURLPrefix) {
			t.Fatalf("返回 URL 前缀错误: %q", url)
		}
		name := strings.TrimPrefix(url, store.UploadURLPrefix)
		gotData, gotMIME, ok := dao.GetImage(name)
		if !ok {
			t.Fatal("上传后应能按文件名读到图片")
		}
		if gotMIME != "image/png" {
			t.Fatalf("MIME = %q, want image/png", gotMIME)
		}
		if !bytes.Equal(gotData, wantData) {
			t.Fatal("图片内容与上传不一致")
		}
	})
}

// TestUploadGetImage 校验按名读取与 Content-Type 兜底链。
func TestUploadGetImage(t *testing.T) {
	uploadInitDB(t)

	t.Run("图片不存在", func(t *testing.T) {
		if _, _, ok := GetImage("not-exist.png"); ok {
			t.Fatal("不存在的图片应返回 ok=false")
		}
	})
	t.Run("正常读取", func(t *testing.T) {
		if err := dao.SaveImage("normal.png", "image/png", []byte("png-bytes")); err != nil {
			t.Fatalf("SaveImage 失败: %v", err)
		}
		data, ct, ok := GetImage("normal.png")
		if !ok || ct != "image/png" || !bytes.Equal(data, []byte("png-bytes")) {
			t.Fatalf("读取结果异常: ok=%v ct=%q data=%q", ok, ct, data)
		}
	})
	t.Run("ContentType为空按扩展名兜底", func(t *testing.T) {
		if _, err := store.DB.Exec(`INSERT INTO tb_image(img_name, content_type, img_data, file_size, create_time) VALUES(?,?,?,?,?)`,
			"nulltype.gif", nil, []byte("gif-bytes"), 9, store.Now()); err != nil {
			t.Fatalf("插入图片失败: %v", err)
		}
		data, ct, ok := GetImage("nulltype.gif")
		if !ok || ct != "image/gif" || !bytes.Equal(data, []byte("gif-bytes")) {
			t.Fatalf("扩展名兜底异常: ok=%v ct=%q data=%q", ok, ct, data)
		}
	})
	t.Run("未知扩展名兜底通用二进制", func(t *testing.T) {
		if _, err := store.DB.Exec(`INSERT INTO tb_image(img_name, content_type, img_data, file_size, create_time) VALUES(?,?,?,?,?)`,
			"weird.xyz", nil, []byte("xyz"), 3, store.Now()); err != nil {
			t.Fatalf("插入图片失败: %v", err)
		}
		_, ct, ok := GetImage("weird.xyz")
		if !ok || ct != "application/octet-stream" {
			t.Fatalf("未知扩展名应兜底 application/octet-stream, ok=%v ct=%q", ok, ct)
		}
	})
}
