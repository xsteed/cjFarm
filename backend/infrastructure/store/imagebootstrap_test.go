package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store/seedimg"
)

// imgTestInitDB 初始化一个独立的临时库。
//
// Init 内部会跑完 migrate + seed,并顺带执行一次 ImportDiskImages(指向空目录)、
// EnsureSeedImages(补齐内嵌出厂图)—— 这正好也是本文件要验证的两条引导路径,
// 因此测试里再显式调用一次,断言其幂等/补齐语义。
func imgTestInitDB(t *testing.T) {
	t.Helper()
	t.Setenv(conf.EnvUploadDir, filepath.Join(t.TempDir(), "uploads"))
	Init(filepath.Join(t.TempDir(), "image.db"))
	t.Cleanup(func() { _ = DB.Close() })
}

// TestImageMIME 扩展名白名单映射:确定性、大小写不敏感、未知扩展名返回空串。
func TestImageMIME(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".JPG", "image/jpeg"},
		{".png", "image/png"},
		{".PNG", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".bmp", "image/bmp"},
		{".txt", ""},
		{"", ""},
		{".svg", ""},
		{".unknown", ""},
	}
	for _, c := range cases {
		if got := ImageMIME(c.ext); got != c.want {
			t.Errorf("ImageMIME(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

// TestInsertImageAndIgnore 「首个写入者胜」:冲突时忽略、不报错、不覆盖已有内容。
func TestInsertImageAndIgnore(t *testing.T) {
	imgTestInitDB(t)

	// 用不与出厂种子图冲突的名字。
	const name = "test-insert-image.png"
	data := []byte("fake-png-bytes")

	inserted, err := InsertImage(name, "image/png", data)
	if err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}
	if !inserted {
		t.Fatal("首次插入应返回 inserted=true")
	}

	// 冲突忽略:同一名字再插一次不算插入,也不报错。
	inserted2, err := InsertImage(name, "image/png", []byte("other"))
	if err != nil {
		t.Fatalf("重复插入不应报错: %v", err)
	}
	if inserted2 {
		t.Fatal("重复插入应返回 inserted=false")
	}

	// 库里内容必须是首次写入的,不能被第二次覆盖。
	var got []byte
	if err := DB.QueryRow(`SELECT img_data FROM tb_image WHERE img_name=?`, name).Scan(&got); err != nil {
		t.Fatalf("读取图片失败: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("图片内容被覆盖: got %q want %q", got, data)
	}
}

// TestImportDiskImages 存量磁盘图片导入的各种分支:
// 合法图片导入、非白名单扩展名跳过、子目录跳过、超大文件跳过、
// 超长文件名跳过、库已存在同名同大小跳过、同名异大小以库为准不覆盖。
func TestImportDiskImages(t *testing.T) {
	imgTestInitDB(t)

	dir := t.TempDir()
	writeFile := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("写文件 %s 失败: %v", name, err)
		}
	}

	writeFile("disk1.png", []byte("png-bytes"))  // 合法:导入
	writeFile("disk2.jpg", []byte("jpg-bytes"))  // 合法:导入
	writeFile("note.txt", []byte("text"))        // 非白名单扩展名:跳过
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile("sub/inside.png", []byte("ignored")) // 子目录:跳过
	writeFile("huge.png", make([]byte, 64<<20+1))   // 超过 64MB:跳过并告警
	writeFile(strings.Repeat("a", 130)+".png", []byte("x")) // 超长文件名(>128):跳过

	// 已在库中的同名同大小:跳过(skipped)。
	const dupName = "dup.png"
	dupData := []byte("dup-bytes-123")
	if _, err := InsertImage(dupName, "image/png", dupData); err != nil {
		t.Fatalf("预置图片失败: %v", err)
	}
	writeFile(dupName, dupData)

	// 同名但大小不同:以库为准不覆盖(mismatched)。
	const misName = "mis.png"
	if _, err := InsertImage(misName, "image/png", []byte("orig")); err != nil {
		t.Fatalf("预置图片失败: %v", err)
	}
	writeFile(misName, []byte("changed-size-content"))

	imported, skipped, mismatched := ImportDiskImages(dir)
	if imported != 2 {
		t.Fatalf("imported=%d, want 2 (disk1.png, disk2.jpg)", imported)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d, want 1 (dup.png)", skipped)
	}
	if mismatched != 1 {
		t.Fatalf("mismatched=%d, want 1 (mis.png)", mismatched)
	}

	// 不一致的文件必须以库内容为准,不被磁盘文件覆盖。
	var misGot []byte
	if err := DB.QueryRow(`SELECT img_data FROM tb_image WHERE img_name=?`, misName).Scan(&misGot); err != nil {
		t.Fatalf("读取 mis 失败: %v", err)
	}
	if string(misGot) != "orig" {
		t.Fatalf("mis 内容被覆盖: got %q", misGot)
	}
}

// TestImportDiskImagesMissingDir 目录不存在不阻断启动(幂等补齐源)。
func TestImportDiskImagesMissingDir(t *testing.T) {
	imgTestInitDB(t)
	imported, skipped, mismatched := ImportDiskImages(filepath.Join(t.TempDir(), "no-such-dir"))
	if imported != 0 || skipped != 0 || mismatched != 0 {
		t.Fatalf("缺失目录应返回全 0, got %d/%d/%d", imported, skipped, mismatched)
	}
}

// TestEnsureSeedImagesIdempotent 出厂图只补缺、从不覆盖,且重复补齐幂等。
func TestEnsureSeedImagesIdempotent(t *testing.T) {
	imgTestInitDB(t) // Init 内部已跑过一次 EnsureSeedImages

	// 出厂种子图应已随 Init 入库。
	for name := range seedimg.Files() {
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_image WHERE img_name=?`, name).Scan(&n); err != nil {
			t.Fatalf("查询种子图 %s 失败: %v", name, err)
		}
		if n != 1 {
			t.Fatalf("种子图 %s 应已入库, got count=%d", name, n)
		}
	}

	// 再次补齐必须幂等(均以库为准,不重复插入、不覆盖)。
	if inserted := EnsureSeedImages(); inserted != 0 {
		t.Fatalf("重复补齐种子图应插入 0 张, got %d", inserted)
	}
}

// TestAuditDishImages 菜品图片引用与库内内容的一致性审计。
func TestAuditDishImages(t *testing.T) {
	imgTestInitDB(t)

	// 取一张确实存在的出厂图作为「引用有效」用例。
	var validName string
	for name := range seedimg.Files() {
		validName = name
		break
	}

	missingName := "definitely-missing.png"
	_, _ = InsertImage(validName, "image/png", []byte("v"))

	now := Now()
	insertDish := func(img string) {
		if _, err := DB.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, del_flag, create_time, update_time)
			VALUES(1,'审计用例',?, '0',?,?)`, img, now, now); err != nil {
			t.Fatalf("插入菜品失败: %v", err)
		}
	}
	// 指向库内存在的图:不应出现在缺失列表。
	insertDish(UploadURLPrefix + validName)
	// 指向库内缺失的图:应出现在缺失列表。
	insertDish(UploadURLPrefix + missingName)
	// 已删除的菜品(del_flag='1')不参与审计。
	insertDish(UploadURLPrefix + "deleted-but-missing.png")
	_, _ = DB.Exec(`UPDATE tb_dish SET del_flag='1' WHERE dish_image=?`, UploadURLPrefix+"deleted-but-missing.png")

	missing := AuditDishImages()
	found := false
	for _, m := range missing {
		if m == missingName {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺失的菜品图片 %s 应被审计出来, missing=%v", missingName, missing)
	}
	// 有效引用与已删除记录不应误报。
	for _, m := range missing {
		if m == validName {
			t.Fatalf("库内存在的图片 %s 不应被报为缺失", validName)
		}
	}
}
