package dao

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

func initImageTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "image.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestImageSaveAndGetRoundTrip(t *testing.T) {
	initImageTestDB(t)

	wantData := []byte("png-bytes")
	if err := SaveImage("roundtrip.png", "image/png", wantData); err != nil {
		t.Fatalf("SaveImage 失败: %v", err)
	}

	gotData, gotMIME, ok := GetImage("roundtrip.png")
	if !ok {
		t.Fatal("GetImage 应命中刚写入的图片")
	}
	if gotMIME != "image/png" {
		t.Fatalf("MIME = %q, want image/png", gotMIME)
	}
	if !bytes.Equal(gotData, wantData) {
		t.Fatalf("图片内容不一致: got %q, want %q", gotData, wantData)
	}
}

func TestSaveImageDuplicateReturnsErrorAndKeepsOriginal(t *testing.T) {
	initImageTestDB(t)

	first := []byte("first")
	if err := SaveImage("dup.png", "image/png", first); err != nil {
		t.Fatalf("第一次 SaveImage 失败: %v", err)
	}
	if err := SaveImage("dup.png", "image/png", []byte("second")); err == nil {
		t.Fatal("同名 SaveImage 应返回冲突错误")
	}

	gotData, _, ok := GetImage("dup.png")
	if !ok {
		t.Fatal("冲突后原图片应仍可读取")
	}
	if !bytes.Equal(gotData, first) {
		t.Fatalf("同名冲突不应覆盖原内容: got %q, want %q", gotData, first)
	}
}

func TestImportDiskImages(t *testing.T) {
	initImageTestDB(t)

	dir := t.TempDir()
	preloaded := []byte("already-imported")
	if err := os.WriteFile(filepath.Join(dir, "already.png"), preloaded, 0644); err != nil {
		t.Fatalf("写入临时 png 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.jpg"), []byte("fresh-jpg"), 0644); err != nil {
		t.Fatalf("写入临时 jpg 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not image"), 0644); err != nil {
		t.Fatalf("写入临时 txt 失败: %v", err)
	}
	if err := SaveImage("already.png", "image/png", preloaded); err != nil {
		t.Fatalf("预置同名同大小图片失败: %v", err)
	}

	imported, skipped, mismatched := store.ImportDiskImages(dir)
	if imported != 1 || skipped != 1 || mismatched != 0 {
		t.Fatalf("首次导入计数 = (%d,%d,%d), want (1,1,0)", imported, skipped, mismatched)
	}
	gotData, gotMIME, ok := GetImage("fresh.jpg")
	if !ok || gotMIME != "image/jpeg" || !bytes.Equal(gotData, []byte("fresh-jpg")) {
		t.Fatalf("fresh.jpg 入库结果异常: ok=%v mime=%q data=%q", ok, gotMIME, gotData)
	}

	imported, skipped, mismatched = store.ImportDiskImages(dir)
	if imported != 0 || skipped != 2 || mismatched != 0 {
		t.Fatalf("重复导入计数 = (%d,%d,%d), want (0,2,0)", imported, skipped, mismatched)
	}

	imported, skipped, mismatched = store.ImportDiskImages(filepath.Join(t.TempDir(), "not-exists"))
	if imported != 0 || skipped != 0 || mismatched != 0 {
		t.Fatalf("不存在目录导入计数 = (%d,%d,%d), want (0,0,0)", imported, skipped, mismatched)
	}
}

func TestImportDiskImagesMismatchedKeepsDatabase(t *testing.T) {
	initImageTestDB(t)

	dir := t.TempDir()
	original := []byte("old")
	if err := SaveImage("mismatch.png", "image/png", original); err != nil {
		t.Fatalf("预置库图片失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mismatch.png"), []byte("newer-longer"), 0644); err != nil {
		t.Fatalf("写入临时图片失败: %v", err)
	}

	imported, skipped, mismatched := store.ImportDiskImages(dir)
	if imported != 0 || skipped != 0 || mismatched != 1 {
		t.Fatalf("大小不一致导入计数 = (%d,%d,%d), want (0,0,1)", imported, skipped, mismatched)
	}
	gotData, _, ok := GetImage("mismatch.png")
	if !ok {
		t.Fatal("大小不一致后库图片应仍可读取")
	}
	if !bytes.Equal(gotData, original) {
		t.Fatalf("大小不一致不应覆盖库内容: got %q, want %q", gotData, original)
	}
}

func TestImageAuthorityOrderDiskBeforeSeed(t *testing.T) {
	initImageTestDB(t)

	if _, err := store.DB.Exec(`DELETE FROM tb_image`); err != nil {
		t.Fatalf("清空图片表失败: %v", err)
	}
	dir := t.TempDir()
	fakeWX := []byte("fake-wx")
	if err := os.WriteFile(filepath.Join(dir, "pay_wx.png"), fakeWX, 0644); err != nil {
		t.Fatalf("写入临时收款码失败: %v", err)
	}

	imported, skipped, mismatched := store.ImportDiskImages(dir)
	if imported != 1 || skipped != 0 || mismatched != 0 {
		t.Fatalf("磁盘权威图导入计数 = (%d,%d,%d), want (1,0,0)", imported, skipped, mismatched)
	}
	inserted := store.EnsureSeedImages()
	if inserted != 22 {
		t.Fatalf("出厂图应只补缺 22 张, got %d", inserted)
	}
	gotData, gotMIME, ok := GetImage("pay_wx.png")
	if !ok {
		t.Fatal("pay_wx.png 应存在")
	}
	if gotMIME != "image/png" {
		t.Fatalf("pay_wx.png MIME = %q, want image/png", gotMIME)
	}
	if !bytes.Equal(gotData, fakeWX) {
		t.Fatalf("磁盘权威内容应胜过内嵌出厂图: got %q, want %q", gotData, fakeWX)
	}
}

func TestEnsureSeedImagesIdempotentAndFullInsert(t *testing.T) {
	initImageTestDB(t)

	if inserted := store.EnsureSeedImages(); inserted != 0 {
		t.Fatalf("Init 后重复补出厂图应为 0, got %d", inserted)
	}
	if _, err := store.DB.Exec(`DELETE FROM tb_image`); err != nil {
		t.Fatalf("清空图片表失败: %v", err)
	}
	if inserted := store.EnsureSeedImages(); inserted != 23 {
		t.Fatalf("空图片表补出厂图数量 = %d, want 23", inserted)
	}
}

func TestAuditDishImages(t *testing.T) {
	initImageTestDB(t)

	if missing := store.AuditDishImages(); len(missing) != 0 {
		t.Fatalf("初始库图片引用应齐全, missing=%v", missing)
	}

	const deletedName = "dining_20260918_001.jpeg"
	if _, err := store.DB.Exec(`DELETE FROM tb_image WHERE img_name=?`, deletedName); err != nil {
		t.Fatalf("删除被引用图片失败: %v", err)
	}
	missing := store.AuditDishImages()
	if !containsString(missing, deletedName) {
		t.Fatalf("missing 应包含 %s, got %v", deletedName, missing)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
