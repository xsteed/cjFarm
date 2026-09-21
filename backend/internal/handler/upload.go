package handler

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/store"
)

// ============ 上传 ============

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
}

// isValidImage 校验文件内容确为声明扩展名对应的真实图片,防止 polyglot 伪造。
// jpg/png/gif 用标准库完整解码并核对格式与扩展名一致;webp/bmp 用魔数校验(标准库无其解码器)。
func isValidImage(ext string, data []byte) bool {
	switch ext {
	case ".jpg", ".jpeg":
		_, format, err := image.Decode(bytes.NewReader(data))
		return err == nil && format == "jpeg"
	case ".png":
		_, format, err := image.Decode(bytes.NewReader(data))
		return err == nil && format == "png"
	case ".gif":
		_, format, err := image.Decode(bytes.NewReader(data))
		return err == nil && format == "gif"
	case ".webp":
		// WebP: "RIFF" + 4 字节长度 + "WEBP"
		return len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	case ".bmp":
		// BMP: "BM" 魔数
		return len(data) >= 2 && bytes.Equal(data[0:2], []byte("BM"))
	}
	return false
}

func Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		fail(c, "文件不能为空")
		return
	}
	// 大小限制 5MB
	if file.Size > 5<<20 {
		fail(c, "文件大小不能超过 5MB")
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedImageExt[ext] {
		fail(c, "仅支持上传图片文件(jpg/png/gif/webp/bmp)")
		return
	}

	// 内容校验:完整解码图片并核对格式与扩展名一致,防止仅靠扩展名/polyglot 伪造。
	src, err := file.Open()
	if err != nil {
		fail(c, "文件读取失败")
		return
	}
	data, err := io.ReadAll(src)
	src.Close()
	if err != nil || len(data) == 0 {
		fail(c, "文件读取失败")
		return
	}
	if !isValidImage(ext, data) {
		fail(c, "文件内容不是有效的图片")
		return
	}

	dir := store.Getenv("UPLOAD_DIR", "./uploads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		fail(c, "创建上传目录失败")
		return
	}
	name := fmt.Sprintf("dining_%s_%d%s", time.Now().Format("20060102_150405"), time.Now().Nanosecond(), ext)
	dst := filepath.Join(dir, name)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		fail(c, "上传失败")
		return
	}
	ok(c, gin.H{"fileName": file.Filename, "url": "/uploads/" + name})
}
