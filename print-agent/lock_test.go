package main

import (
	"path/filepath"
	"testing"
)

// TestAcquireInstanceLockExclusive 单实例互斥:同一锁文件同时只允许一个持有者,
// 释放后可再次获取。钉住「两个实例同时跑 → 租约过期后同一张小票被重复打印」这条路径。
func TestAcquireInstanceLockExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-agent.lock")

	f1, ok, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("首次加锁失败: %v", err)
	}
	if !ok {
		t.Fatal("首次加锁应成功")
	}
	defer func() { _ = f1.Close() }()

	// 第二个实例(不同 fd)必须拿不到锁。
	f2, ok2, err2 := acquireInstanceLock(path)
	if err2 != nil {
		t.Fatalf("第二次加锁返回了错误(应为竞争失败): %v", err2)
	}
	if ok2 {
		_ = f2.Close()
		t.Fatal("同一锁文件的第二次加锁不应成功")
	}

	// 释放后应能重新获取(进程退出时内核自动释放,不需要清理陈旧锁)。
	if err := f1.Close(); err != nil {
		t.Fatalf("释放锁失败: %v", err)
	}
	f3, ok3, err3 := acquireInstanceLock(path)
	if err3 != nil || !ok3 {
		t.Fatalf("释放后应能重新加锁, ok=%v err=%v", ok3, err3)
	}
	_ = f3.Close()
}
