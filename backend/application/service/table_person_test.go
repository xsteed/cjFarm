package service

import (
	"testing"
	"time"
)

// 桌级共享入座人数（table_person.go）的单元测试。
// 重点验证三件事：越界输入被丢弃、后写覆盖先写、TTL 过期与下单清除。
func TestTablePendingPerson(t *testing.T) {
	t.Run("设置后可读，同桌共享", func(t *testing.T) {
		ClearTablePendingPerson(9001)
		SetTablePendingPerson(9001, 4)
		if got := GetTablePendingPerson(9001); got != 4 {
			t.Fatalf("want 4, got %d", got)
		}
	})

	t.Run("后写覆盖先写（并发语义：共享值只是预填默认，不是锁）", func(t *testing.T) {
		ClearTablePendingPerson(9002)
		SetTablePendingPerson(9002, 6)
		SetTablePendingPerson(9002, 2)
		if got := GetTablePendingPerson(9002); got != 2 {
			t.Fatalf("want 2 (last write wins), got %d", got)
		}
	})

	t.Run("越界与非法输入静默丢弃", func(t *testing.T) {
		ClearTablePendingPerson(9003)
		for _, bad := range [][2]int{{0, 4}, {-1, 4}, {9003, 0}, {9003, 21}, {9003, -3}} {
			SetTablePendingPerson(bad[0], bad[1])
		}
		if got := GetTablePendingPerson(9003); got != 0 {
			t.Fatalf("want 0 for invalid inputs, got %d", got)
		}
	})

	t.Run("未记录的桌台返回 0", func(t *testing.T) {
		if got := GetTablePendingPerson(999999); got != 0 {
			t.Fatalf("want 0, got %d", got)
		}
	})

	t.Run("超过 TTL 视为没选过", func(t *testing.T) {
		origTTL := tablePersonTTL
		tablePersonTTL = 10 * time.Millisecond
		defer func() { tablePersonTTL = origTTL }()

		ClearTablePendingPerson(9004)
		SetTablePendingPerson(9004, 5)
		if got := GetTablePendingPerson(9004); got != 5 {
			t.Fatalf("want 5 before TTL, got %d", got)
		}
		time.Sleep(20 * time.Millisecond)
		if got := GetTablePendingPerson(9004); got != 0 {
			t.Fatalf("want 0 after TTL, got %d", got)
		}
	})

	t.Run("下单清除后不再沿用", func(t *testing.T) {
		ClearTablePendingPerson(9005)
		SetTablePendingPerson(9005, 8)
		ClearTablePendingPerson(9005)
		if got := GetTablePendingPerson(9005); got != 0 {
			t.Fatalf("want 0 after clear, got %d", got)
		}
	})
}
