package handler

import (
	"bytes"
	"dining-system/internal/store/dao"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/store"
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

func TestUploadImageValidation(t *testing.T) {
	initUploadTestDB(t)
	pngData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) })
	jpgData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return jpeg.Encode(b, m, nil) })
	gifData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return gif.Encode(b, m, nil) })

	cases := []struct {
		name     string
		filename string
		data     []byte
		wantCode int
	}{
		{"png正确", "ok.png", pngData, http.StatusOK},
		{"jpg正确", "ok.jpg", jpgData, http.StatusOK},
		{"jpeg正确", "ok.jpeg", jpgData, http.StatusOK},
		{"gif正确", "ok.gif", gifData, http.StatusOK},
		{"webp魔数正确", "ok.webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("VP8 ")...), http.StatusOK},
		{"bmp魔数正确", "ok.bmp", []byte("BM\x00\x00\x00\x00\x00\x00"), http.StatusOK},
		{"png内容声明为jpg(格式不符)", "bad.jpg", pngData, http.StatusBadRequest},
		{"webp魔数错误", "bad.webp", []byte("XXXX\x00\x00\x00\x00WEBPxxxx"), http.StatusBadRequest},
		{"bmp魔数错误", "bad.bmp", []byte("XXrest"), http.StatusBadRequest},
		{"伪造成png的文本", "bad.png", []byte("not an image at all, just plain text"), http.StatusBadRequest},
		{"空内容", "empty.png", []byte{}, http.StatusBadRequest},
		{"未知扩展名", "bad.txt", pngData, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doUpload(t, newUploadRequest(t, "file", c.filename, c.data))
			if w.Code != c.wantCode {
				t.Fatalf("上传 %s 应返回 %d, got %d body=%s", c.filename, c.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

// ============================================================================
// 上传接口集成测试:上传写库,展示链路按文件名读回。
// ============================================================================

// initUploadTestDB 建一个临时库跑完迁移(含 tb_image),供上传集成测试使用。
func initUploadTestDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "upload.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// newUploadRequest 构造一个携带单个文件字段的 multipart 请求。
//
// 走真实 gin 路由而不直接造 gin.Context:只有经过路由的请求才会触发 gin 对
// multipart body 的解析,Upload 里的 c.FormFile 才能拿到与线上一致的 *multipart.FileHeader。
func newUploadRequest(t *testing.T, field, filename string, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("创建 multipart 表单字段失败: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("写入文件内容失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// doUpload 在最小 gin 引擎上注册 Upload 并执行请求。
func doUpload(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/upload", Upload)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestUploadSavesImageToDB 合法 PNG 上传应落入 tb_image,且响应 url 能被展示链路反查。
//
// 这是「上传→数据库→展示」链路的端到端校验:落库后字节必须与上传一致、MIME 必须与
// 扩展名映射一致,而不是只验证 HTTP 200 就放过(旧实现落磁盘的字节校验已在此被替换)。
func TestUploadSavesImageToDB(t *testing.T) {
	initUploadTestDB(t)
	pngData := encodeTestImage(t, func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) })

	w := doUpload(t, newUploadRequest(t, "file", "菜品图.png", pngData))
	if w.Code != http.StatusOK {
		t.Fatalf("合法 PNG 上传应成功, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			FileName string `json:"fileName"`
			URL      string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析上传响应失败: %v", err)
	}
	if !strings.HasPrefix(resp.Data.URL, store.UploadURLPrefix+"dining_") {
		t.Fatalf("响应 url 前缀错误: %q", resp.Data.URL)
	}

	name := strings.TrimPrefix(resp.Data.URL, store.UploadURLPrefix)
	got, mime, ok := dao.GetImage(name)
	if !ok {
		t.Fatalf("上传后应能在 tb_image 查到 %s", name)
	}
	if !bytes.Equal(got, pngData) {
		t.Fatalf("库中字节与上传内容不一致")
	}
	if mime != "image/png" {
		t.Fatalf("库中 MIME 错误: got %q, want image/png", mime)
	}
}

// TestUploadRejectsFakeImage 扩展名合法但内容不是图片的伪造文件必须被拦下。
//
// 上传链路的内容真实性校验先于落库:否则攻击者可借 .png 扩展名把任意字节写进
// 图片表,展示链路原样吐回时会变成内容与 Content-Type 不符的脏数据。
func TestUploadRejectsFakeImage(t *testing.T) {
	initUploadTestDB(t)
	fake := []byte("not an image at all, just plain text")
	w := doUpload(t, newUploadRequest(t, "file", "fake.png", fake))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("伪造内容应 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestUploadRejectsOversize 超过 5MB 的上传必须被拦下。
//
// file.Size 是 multipart 头声明值,构造超过 5MB 的真实 body 即可触发第一道大小防线,
// 避免超大实体继续进入解码与落库路径耗尽内存/磁盘。
func TestUploadRejectsOversize(t *testing.T) {
	initUploadTestDB(t)
	big := bytes.Repeat([]byte("x"), 5<<20+1)
	w := doUpload(t, newUploadRequest(t, "file", "big.png", big))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超限上传应 400, got %d body=%s", w.Code, w.Body.String())
	}
}
