package service

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"time"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============ 上传 ============
//
// 图片上传的业务逻辑:扩展名白名单、内容真实性校验、文件名生成、落库与 URL 前缀。
// handler 只负责从 multipart 取文件、读入字节与响应组装。

// MaxImageBytes 上传图片大小上限(5MB),handler 与服务共用同一份定义。
const MaxImageBytes = 5 << 20

// 图片尺寸上限,防止解压炸弹:压缩后字节数小不代表解码后内存占用小,
// 一张声明 50000×50000 像素的 PNG/JPEG 压缩后可能远小于 5MB,但解码器会按声明
// 尺寸整帧分配像素缓冲(50000×50000×4 字节约 10GB)直接触发 OOM。
//   - 单边 ≤ 8192:覆盖 8K 及常见宣传图/海报的显示与打印需求;
//   - 总像素 ≤ 4000 万:即使单边都未超限,总像素仍被钳制(如 8192×8192 会被拒),
//     按 RGBA 4 字节/像素估算峰值解码内存约 160MB,对上传体量与业务图片量级留有充足余量。
const (
	maxImageDimension = 8192
	maxImagePixels    = 40_000_000
)

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
}

var (
	// errInvalidImage 内容不是声明扩展名对应的真实图片(含无法解码/格式不符/魔数不符)。
	errInvalidImage = errors.New("文件内容不是有效的图片")
	// errImageTooLarge 图片声明尺寸超出上限,单独提示便于前端区分「格式不对」与「太大了」。
	errImageTooLarge = errors.New("图片尺寸过大")
)

// isValidImage 校验文件内容确为声明扩展名对应的真实图片,防止 polyglot 伪造与解压炸弹。
// jpg/png/gif 先 DecodeConfig 读声明尺寸(只解析头部、不分配像素缓冲)做上限校验,
// 通过后再完整 Decode 核对格式与扩展名;webp/bmp 用魔数校验(标准库无其解码器)。
func isValidImage(ext string, data []byte) error {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return errInvalidImage
		}
		// 先校验单边、再算总像素:&&/|| 短路保证乘法只在宽高已非负时执行,避免整数溢出。
		if cfg.Width <= 0 || cfg.Height <= 0 ||
			cfg.Width > maxImageDimension || cfg.Height > maxImageDimension ||
			cfg.Width*cfg.Height > maxImagePixels {
			return errImageTooLarge
		}
		// 期望格式:jpg/jpeg→jpeg,png→png,gif→gif。扩展名是白名单里的,这里必能命中。
		want := map[string]string{".jpg": "jpeg", ".jpeg": "jpeg", ".png": "png", ".gif": "gif"}[ext]
		_, format, err := image.Decode(bytes.NewReader(data))
		if err != nil || format != want {
			return errInvalidImage
		}
		return nil
	case ".webp":
		// WebP: "RIFF" + 4 字节长度 + "WEBP"
		if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
			return nil
		}
		return errInvalidImage
	case ".bmp":
		// BMP: "BM" 魔数
		if len(data) >= 2 && bytes.Equal(data[0:2], []byte("BM")) {
			return nil
		}
		return errInvalidImage
	}
	return errInvalidImage
}

// SaveUpload 校验图片并落库,返回可对外访问的 URL(前缀取自 store.UploadURLPrefix)。
func SaveUpload(fileName string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if !allowedImageExt[ext] {
		return "", errors.New("仅支持上传图片文件(jpg/png/gif/webp/bmp)")
	}
	if len(data) > MaxImageBytes {
		return "", errors.New("文件大小不能超过 5MB")
	}
	if err := isValidImage(ext, data); err != nil {
		return "", err
	}

	name := fmt.Sprintf("dining_%s_%d%s", time.Now().Format("20060102_150405"), time.Now().Nanosecond(), ext)
	// 图片内容落库:展示链路按文件名查 tb_image,磁盘不再作为运行时数据源;
	// 文件名一旦写入即不可变,若撞唯一索引会显式返回错误,交由调用方重试。
	if err := dao.SaveImage(name, store.ImageMIME(ext), data); err != nil {
		return "", errors.New("上传失败")
	}
	// 返回的路径前缀必须与后端静态路由、Nginx 反代一致,统一取自 store.UploadURLPrefix。
	return store.UploadURLPrefix + name, nil
}

// GetImage 按文件名读取图片内容与 Content-Type。
// 库中 Content-Type 为空时按扩展名兜底(理论不应发生),仍为空用通用二进制。
func GetImage(name string) (data []byte, contentType string, ok bool) {
	data, contentType, ok = dao.GetImage(name)
	if !ok {
		return nil, "", false
	}
	if contentType == "" {
		contentType = store.ImageMIME(filepath.Ext(name))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return data, contentType, true
}
