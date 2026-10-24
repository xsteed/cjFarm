package service

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"time"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// ============ 上传 ============
//
// 图片上传的业务逻辑:扩展名白名单、内容真实性校验、文件名生成、落库与 URL 前缀。
// handler 只负责从 multipart 取文件、读入字节与响应组装。

// MaxImageBytes 上传图片大小上限(64MB),handler 与服务共用同一份定义。
const MaxImageBytes = 64 << 20

// 图片尺寸上限,防止解压炸弹:压缩后字节数小不代表解码后内存占用小,
// 一张声明 50000×50000 像素的 PNG/JPEG 压缩后可能远小于 64MB,但解码器会按声明
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

// checkImageDimension 校验画布尺寸在上限内。
// 先校验单边、再算总像素:&&/|| 短路保证乘法只在宽高已非负时执行,避免整数溢出。
func checkImageDimension(w, h int) error {
	if w <= 0 || h <= 0 ||
		w > maxImageDimension || h > maxImageDimension || w*h > maxImagePixels {
		return errImageTooLarge
	}
	return nil
}

// webpDimension 从 WebP 头部解析画布尺寸(只解析头部,不分配像素缓冲)。
//
// 标准库没有 WebP 解码器,若只比对 "RIFF....WEBP" 魔数,任意以这 12 字节开头的
// 字节流(可以整段是 HTML/JS)都能落库并以 image/webp 对外提供 —— 既绕过了尺寸/
// 解压炸弹防护,也会在会做 MIME 嗅探的旧客户端上退化成存储型 XSS。
// 这里按三种 chunk 头手写解析,拿到真实尺寸后套用与 jpg/png 相同的上限。
func webpDimension(data []byte) (w, h int, ok bool) {
	// 最少 20 字节 = RIFF(4) + 长度(4) + WEBP(4) + chunk fourcc(4) + chunk 长度(4),
	// 之后再按各 chunk 的最小长度校验;不要卡更高,否则会误拒体积很小的合法 WebP。
	if len(data) < 20 || !bytes.Equal(data[0:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return 0, 0, false
	}
	chunk := string(data[12:16])
	size := int(binary.LittleEndian.Uint32(data[16:20]))
	body := data[20:]
	// chunk 声明长度可能被伪造:越界时收敛到实际剩余字节,避免切片 panic。
	if size <= 0 || size > len(body) {
		size = len(body)
	}
	body = body[:size]
	switch chunk {
	case "VP8 ": // 有损:3 字节 frame tag + 3 字节起始码,随后 14+14 位宽高
		if len(body) < 10 || !bytes.Equal(body[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(body[6:10])
		return int(v & 0x3FFF), int((v >> 16) & 0x3FFF), true
	case "VP8L": // 无损:1 字节签名 + 14+14 位「宽-1 / 高-1」
		if len(body) < 5 || body[0] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(body[1:5])
		return int(bits&0x3FFF) + 1, int((bits>>14)&0x3FFF) + 1, true
	case "VP8X": // 扩展:4 字节标志位 + 24 位「宽-1」+ 24 位「高-1」
		if len(body) < 10 {
			return 0, 0, false
		}
		w = int(body[4]) | int(body[5])<<8 | int(body[6])<<16
		h = int(body[7]) | int(body[8])<<8 | int(body[9])<<16
		return w + 1, h + 1, true
	}
	return 0, 0, false
}

// bmpDimension 从 BMP 头部解析尺寸(兼容 BITMAPCOREHEADER 与 BITMAPINFOHEADER+)。
// 只比对 "BM" 两个字节时,任意以 BM 开头的字节流都会被当作合法 BMP 落库。
func bmpDimension(data []byte) (w, h int, ok bool) {
	if len(data) < 26 || !bytes.Equal(data[0:2], []byte("BM")) {
		return 0, 0, false
	}
	// 偏移 14 的 biSize 决定后续布局:12 = BITMAPCOREHEADER(16 位宽高)。
	if binary.LittleEndian.Uint32(data[14:18]) == 12 {
		w = int(binary.LittleEndian.Uint16(data[18:20]))
		h = int(binary.LittleEndian.Uint16(data[20:22]))
		return w, h, w > 0 && h > 0
	}
	// BITMAPINFOHEADER 及以上:32 位宽高;高度为负表示自上而下存储,取绝对值。
	w = int(int32(binary.LittleEndian.Uint32(data[18:22])))
	h = int(int32(binary.LittleEndian.Uint32(data[22:26])))
	if h < 0 {
		h = -h
	}
	return w, h, w > 0 && h > 0
}

// isValidImage 校验文件内容确为声明扩展名对应的真实图片,防止 polyglot 伪造与解压炸弹。
// jpg/png/gif 先 DecodeConfig 读声明尺寸(只解析头部、不分配像素缓冲)做上限校验,
// 通过后再完整 Decode 核对格式与扩展名;webp/bmp 标准库无解码器,改用手工解析头部
// 拿到真实尺寸 —— 两者都必须过同一套尺寸上限,不能只做魔数比对。
func isValidImage(ext string, data []byte) error {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return errInvalidImage
		}
		if err := checkImageDimension(cfg.Width, cfg.Height); err != nil {
			return err
		}
		// 期望格式:jpg/jpeg→jpeg,png→png,gif→gif。扩展名是白名单里的,这里必能命中。
		want := map[string]string{".jpg": "jpeg", ".jpeg": "jpeg", ".png": "png", ".gif": "gif"}[ext]
		_, format, err := image.Decode(bytes.NewReader(data))
		if err != nil || format != want {
			return errInvalidImage
		}
		return nil
	case ".webp":
		w, h, ok := webpDimension(data)
		if !ok {
			return errInvalidImage
		}
		return checkImageDimension(w, h)
	case ".bmp":
		w, h, ok := bmpDimension(data)
		if !ok {
			return errInvalidImage
		}
		return checkImageDimension(w, h)
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
		return "", errors.New("文件大小不能超过 64MB")
	}
	if err := isValidImage(ext, data); err != nil {
		return "", err
	}

	// 时间戳 + 随机段:只有时间戳时,同纳秒并发上传会撞 tb_image.img_name 唯一索引
	// (靠上层重试兜底,但用户白等一次);随机段让冲突概率降到可忽略,也让文件名
	// 不可被预测。time.Now() 只取一次,避免「日期段与纳秒段」跨秒不一致。
	now := time.Now()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成文件名失败: %w", err)
	}
	name := fmt.Sprintf("dining_%s_%d_%x%s", now.Format("20060102_150405"), now.Nanosecond(), b, ext)
	// 图片内容落库:展示链路按文件名查 tb_image,磁盘不再作为运行时数据源;
	// 文件名一旦写入即不可变,若撞唯一索引会显式返回错误,交由调用方重试。
	// 错误必须原样包装上抛(不是笼统的「上传失败」):dao 注释明确要求把
	// 「文件名冲突」暴露给上层重试,吞掉后 handler 无法区分冲突与其它故障。
	if err := dao.SaveImage(name, store.ImageMIME(ext), data); err != nil {
		return "", fmt.Errorf("上传失败: %w", err)
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
