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
