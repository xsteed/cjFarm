package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stCloseState 关闭状态文件句柄,避免测试间文件占用。
func stCloseState(t *testing.T, s *jobState) {
	t.Helper()
	if s == nil {
		return
	}
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}

func TestStLoadJobState(t *testing.T) {
	t.Run("预写文件加载 seen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "print-agent.state")
		if err := os.WriteFile(path, []byte("id1\nid2\n\nid3\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := loadJobState(path)
		defer stCloseState(t, s)

		for _, id := range []string{"id1", "id2", "id3"} {
			if !s.has(id) {
				t.Fatalf("加载后应识别幂等号 %q", id)
			}
		}
		if s.has("id-missing") {
			t.Fatal("不应识别未写入的幂等号")
		}
	})

	t.Run("目录不存在路径降级为纯内存", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "no-such-dir", "print-agent.state")
		s := loadJobState(path)
		defer stCloseState(t, s)

		if s.f != nil {
			t.Fatal("目录不存在时 f 应为 nil(纯内存降级)")
		}
		// 纯内存态仍可正常工作:标记后本进程内能识别。
		s.markDone("mem-id")
		if !s.has("mem-id") {
			t.Fatal("纯内存态 markDone 后应可 has 识别")
		}
	})

	t.Run("空 id 行为", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "print-agent.state")
		s := loadJobState(path)
		defer stCloseState(t, s)

		if s.has("") {
			t.Fatal("空 id 不应命中")
		}
		s.markDone("") // 空 id 忽略,不 panic
		if s.has("") {
			t.Fatal("空 id markDone 后仍不应命中")
		}
		if len(s.order) != 0 {
			t.Fatalf("空 id 不应进入 order, got %v", s.order)
		}
	})
}

// TestStDropsTruncatedTail 断电把最后一行写了一半时,必须丢弃并把它从文件里摘掉。
//
// 危害不只是「多一条垃圾记录」:若把它当有效行留着,后续 append 会与它粘成一行
// (abcd<newid>\n),新记录反而没被独立记录 —— 该单在下次重启后失去去重、被重打一次。
func TestStDropsTruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-agent.state")
	// 末行 "id-part" 没有换行结尾 —— 正是断电写了一半的残行。
	if err := os.WriteFile(path, []byte("id1\nid2\nid-part"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadJobState(path)
	defer stCloseState(t, s)

	if !s.has("id1") || !s.has("id2") {
		t.Fatal("完整行应被加载")
	}
	if s.has("id-part") {
		t.Fatal("被截断的残行不该当成有效幂等号")
	}
	// 残行必须已从磁盘摘掉,否则下次加载又会把它的前缀读成有效记录。
	if data, err := os.ReadFile(path); err != nil || strings.Contains(string(data), "id-part") {
		t.Fatalf("残行应从文件里被摘掉, got %q err=%v", data, err)
	}

	s.markDone("id3")
	stCloseState(t, s)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "id1\nid2\nid3" {
		t.Fatalf("新记录应自成一行, got %q", got)
	}
}

// TestStCompactIsAtomic 压缩重写必须原子替换,不能先截断再写。
//
// 旧实现是「先 Close 并把句柄置 nil,再 os.WriteFile」——os.WriteFile 内部是 O_TRUNC,
// 一打开就把文件截成 0 字节:磁盘满 / I/O 出错时历史幂等号全部丢失,且句柄已被置 nil,
// 本进程此后再也不落盘(只剩一句告警)。去重记录一丢,重启后云端重发就是重复出纸。
func TestStCompactIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-agent.state")
	if err := os.WriteFile(path, []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadJobState(path)
	defer stCloseState(t, s)

	// 灌满到触发压缩。
	for i := 0; i <= jobStateMaxKeep; i++ {
		s.markDone("id-" + strconv.Itoa(i))
	}
	if len(s.order) > jobStateMaxKeep {
		t.Fatalf("压缩后条数应回落到上限内, got %d", len(s.order))
	}
	// 压缩后必须仍持有可追加的句柄(否则后续记录只留内存,重启即丢)。
	if s.f == nil {
		t.Fatal("压缩后必须仍能继续追加去重记录")
	}
	// 文件内容必须与内存一致且非空。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Fatal("压缩后状态文件被清空了(必须先写临时文件再 rename)")
	}
	last := s.order[len(s.order)-1]
	if !strings.Contains(string(data), last+"\n") {
		t.Fatalf("压缩后最新记录 %q 应落盘", last)
	}
	// 压缩后新记录仍要独立成行。
	s.markDone("after-compact")
	stCloseState(t, s)
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "after-compact\n") {
		t.Fatalf("压缩后新记录应独立成行, got %q", data)
	}
}

func TestStHasAndMarkDone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-agent.state")
	s := loadJobState(path)
	defer stCloseState(t, s)

	s.markDone("id1")
	if !s.has("id1") {
		t.Fatal("markDone 后应 has 识别")
	}
	// 幂等:同 id 二次 mark 不重复追加。
	s.markDone("id1")
	if !s.has("id1") {
		t.Fatal("重复 markDone 后仍应 has 识别")
	}
	if len(s.order) != 1 {
		t.Fatalf("同 id 二次 mark 不应重复追加, order=%v", s.order)
	}

	// 落盘内容断言:只应有一行 id1,无重复、无空行。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "id1\n" {
		t.Fatalf("状态文件内容=%q, want %q", data, "id1\n")
	}
}

func TestStMaybeCompactLocked(t *testing.T) {
	const total = 15000
	s := &jobState{
		path:  filepath.Join(t.TempDir(), "print-agent.state"),
		f:     nil, // 内存态,避免真实 fsync
		seen:  map[string]struct{}{},
		order: make([]string, 0, total),
	}
	for i := 0; i < total; i++ {
		id := "id" + strconv.Itoa(i)
		s.order = append(s.order, id)
		s.seen[id] = struct{}{}
	}

	s.maybeCompactLocked()

	if len(s.order) != jobStateMaxKeep/2 {
		t.Fatalf("截断后 order 长度=%d, want %d", len(s.order), jobStateMaxKeep/2)
	}
	if s.order[0] != "id10000" {
		t.Fatalf("应保留最新一半, 首条=%q, want id10000", s.order[0])
	}
	if s.order[len(s.order)-1] != "id14999" {
		t.Fatalf("末条=%q, want id14999", s.order[len(s.order)-1])
	}
	if len(s.seen) != jobStateMaxKeep/2 {
		t.Fatalf("seen 长度=%d, want %d", len(s.seen), jobStateMaxKeep/2)
	}
	if _, ok := s.seen["id9999"]; ok {
		t.Fatal("被截断的最老条目不应留在 seen 中")
	}
	if _, ok := s.seen["id10000"]; !ok {
		t.Fatal("保留的最新条目应在 seen 中")
	}
	// 实现会在压缩后重写文件并重开追加句柄(s.f 由 nil 变为打开的句柄,属预期行为),
	// 因此断言磁盘内容只保留最新一半。
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("读取压缩后的状态文件失败: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != jobStateMaxKeep/2 {
		t.Fatalf("落盘行数=%d, want %d", len(lines), jobStateMaxKeep/2)
	}
	if lines[0] != "id10000" || lines[len(lines)-1] != "id14999" {
		t.Fatalf("落盘应只保留最新一半, got 首=%q 末=%q", lines[0], lines[len(lines)-1])
	}
}
