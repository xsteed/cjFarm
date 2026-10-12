package snowflake

import (
	"sync"
	"testing"
	"time"
)

// TestNewValidatesNodeID 节点号越界必须报错而非静默截断。
func TestNewValidatesNodeID(t *testing.T) {
	for _, id := range []int64{-1, 1024, 1 << 20} {
		if _, err := New(id); err == nil {
			t.Errorf("New(%d) 应报错,实际通过", id)
		}
	}
	for _, id := range []int64{0, 1, 1023} {
		if _, err := New(id); err != nil {
			t.Errorf("New(%d) 不应报错: %v", id, err)
		}
	}
}

// TestGenerateUniqueAndMonotonic 并发生成大量 ID:全局唯一、同节点单调递增。
func TestGenerateUniqueAndMonotonic(t *testing.T) {
	n, err := New(1)
	if err != nil {
		t.Fatal(err)
	}

	const (
		workers = 32
		per     = 5000
	)
	// 每个 worker 只往自己的切片写,不共享收集顺序 —— 跨线程观测不到
	// 真实生成先后,只能断言「全局唯一 + 各 worker 内部单调」。
	ids := make([][]int64, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ids[w] = make([]int64, 0, per)
			for i := 0; i < per; i++ {
				ids[w] = append(ids[w], n.Generate())
			}
		}(w)
	}
	wg.Wait()

	seen := make(map[int64]struct{}, workers*per)
	for w, seq := range ids {
		prev := int64(0)
		for _, id := range seq {
			if id <= 0 {
				t.Fatalf("ID 必须为正数,实际 %d", id)
			}
			if _, dup := seen[id]; dup {
				t.Fatalf("并发生成出现重复 ID: %d", id)
			}
			seen[id] = struct{}{}
			if id <= prev {
				t.Fatalf("worker %d 内 ID 必须单调递增: %d 出现在 %d 之后", w, id, prev)
			}
			prev = id
		}
	}
}

// TestGenerateNodeBits 节点号应编码在 ID 的第 12~21 位。
func TestGenerateNodeBits(t *testing.T) {
	for _, node := range []int64{0, 1, 5, 1023} {
		n, err := New(node)
		if err != nil {
			t.Fatal(err)
		}
		got := (n.Generate() >> nodeShift) & MaxNodeID
		if got != node {
			t.Errorf("节点号 %d 编码后读出 %d", node, got)
		}
	}
}

// TestTimeRoundTrip 从 ID 还原的生成时间应落在「生成前后 2 秒」窗口内。
func TestTimeRoundTrip(t *testing.T) {
	n, err := New(1)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-2 * time.Second)
	id := n.Generate()
	after := time.Now().Add(2 * time.Second)

	got := Time(id)
	if got.Before(before) || got.After(after) {
		t.Errorf("Time(%d) = %v,应落在 [%v, %v] 内", id, got, before, after)
	}
}
