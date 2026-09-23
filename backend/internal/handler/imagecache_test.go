package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// imgInitDB 建临时库(含 tb_image),供图片展示 handler 测试使用。
func imgInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "imagecache.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// imgServe 以指定请求路径调用 ServeImage。
func imgServe(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	ServeImage(c)
	return w
}

// TestServeImageFromDB 未命中缓存时从 tb_image 读回并回填缓存。
func TestServeImageFromDB(t *testing.T) {
	imgInitDB(t)
	data := []byte("img-db-bytes")
	if err := dao.SaveImage("img_db_test.png", "image/png", data); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}

	w := imgServe(t, "/uploads/img_db_test.png")
	if w.Code != http.StatusOK {
		t.Fatalf("展示图片应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("展示图片字节不一致: got %q, want %q", w.Body.Bytes(), data)
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type 错误: %q", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("Cache-Control 缺少 immutable: %q", w.Header().Get("Cache-Control"))
	}

	// 命中后应回填进程内缓存。
	got, ok := getImageCache("img_db_test.png")
	if !ok || !bytes.Equal(got.data, data) || got.contentType != "image/png" {
		t.Fatalf("展示后应回填缓存, got %v ok=%v", got, ok)
	}
}

// TestServeImageFromCache 命中进程内缓存时不再查库。
func TestServeImageFromCache(t *testing.T) {
	// 无需数据库:缓存命中分支直接返回。
	putImageCache("img_cache_hit.png", imageCacheEntry{data: []byte("cached-bytes"), contentType: "image/png"})

	w := imgServe(t, "/uploads/img_cache_hit.png")
	if w.Code != http.StatusOK {
		t.Fatalf("缓存命中应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	if string(w.Body.Bytes()) != "cached-bytes" {
		t.Fatalf("缓存命中返回内容错误: %q", w.Body.String())
	}
}

// TestServeImageNotFound 资源不存在时返回 404。
func TestServeImageNotFound(t *testing.T) {
	imgInitDB(t)

	w := imgServe(t, "/uploads/img_missing.png")
	if w.Code != http.StatusNotFound {
		t.Fatalf("缺失资源应返回 404, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "资源不存在") {
		t.Fatalf("缺失资源响应体错误: %s", w.Body.String())
	}
}

// TestServeImagePathTraversal 目录穿越被 filepath.Base 防住(按文件名查库后 404)。
func TestServeImagePathTraversal(t *testing.T) {
	imgInitDB(t)

	w := imgServe(t, "/uploads/../../etc/passwd")
	if w.Code != http.StatusNotFound {
		t.Fatalf("目录穿越请求应被拦成 404, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestImageCachePutGet 缓存写入/读取基本语义,重复写入不覆盖。
func TestImageCachePutGet(t *testing.T) {
	putImageCache("img_cache_roundtrip.png", imageCacheEntry{data: []byte("first"), contentType: "image/png"})

	e, ok := getImageCache("img_cache_roundtrip.png")
	if !ok || string(e.data) != "first" || e.contentType != "image/png" {
		t.Fatalf("缓存应命中首次写入内容, got %v ok=%v", e, ok)
	}

	// 内容一旦入库即不可变,重复写入应保持原值。
	putImageCache("img_cache_roundtrip.png", imageCacheEntry{data: []byte("second"), contentType: "image/png"})
	e2, _ := getImageCache("img_cache_roundtrip.png")
	if string(e2.data) != "first" {
		t.Fatalf("重复写入不应覆盖已有缓存, got %q", e2.data)
	}
}
