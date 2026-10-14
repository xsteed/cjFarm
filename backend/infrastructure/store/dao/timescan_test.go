package dao

import (
	"database/sql"
	"testing"
	"time"
)

// TestTSAcceptsDialectValues 时间列扫描必须同时吃得下两种方言交回来的值:
// MySQL(DATETIME + parseTime=true)给 time.Time,SQLite(VARCHAR)给 string/[]byte。
func TestTSAcceptsDialectValues(t *testing.T) {
	when := time.Date(2026, 10, 15, 12, 34, 56, 0, time.Local)
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"MySQL time.Time", when, "2026-10-15 12:34:56"},
		{"SQLite string", "2026-10-15 12:34:56", "2026-10-15 12:34:56"},
		{"SQLite []byte", []byte("2026-10-15 12:34:56"), "2026-10-15 12:34:56"},
		{"NULL", nil, ""},
		{"空串", "", ""},
		{"MySQL 零值", "0000-00-00 00:00:00", ""},
		{"零值时间", time.Time{}, ""},
		// 外部导入的 ISO 形态统一折算成本地时间文本,避免同列里混两种格式
		{"ISO 带时区", "2026-10-15T12:34:56+08:00", "2026-10-15 12:34:56"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			if err := TS(&got).Scan(c.val); err != nil {
				t.Fatalf("TS(*string) 扫描 %T 失败: %v", c.val, err)
			}
			if got != c.want {
				t.Fatalf("TS(*string) = %q, 期望 %q", got, c.want)
			}
		})
	}
}

// TestTSPointerAndNullString 可空字段的两种载体都要保持「没值就是没值」的语义。
func TestTSPointerAndNullString(t *testing.T) {
	var ptr *string
	if err := TS(&ptr).Scan(nil); err != nil {
		t.Fatal(err)
	}
	if ptr != nil {
		t.Fatalf("NULL 应落 nil, got %q", *ptr)
	}
	if err := TS(&ptr).Scan("2026-10-15 08:00:00"); err != nil {
		t.Fatal(err)
	}
	if ptr == nil || *ptr != "2026-10-15 08:00:00" {
		t.Fatalf("非空值应落指针, got %v", ptr)
	}
	// 空串同样视为没值:*string 落 nil,与既有 nsPtr() 语义一致
	if err := TS(&ptr).Scan(""); err != nil {
		t.Fatal(err)
	}
	if ptr != nil {
		t.Fatalf("空串应落 nil, got %q", *ptr)
	}

	var ns sql.NullString
	if err := TS(&ns).Scan(nil); err != nil {
		t.Fatal(err)
	}
	if ns.Valid {
		t.Fatal("NULL 应保持 Valid=false")
	}
	if err := TS(&ns).Scan("2026-10-15 08:00:00"); err != nil {
		t.Fatal(err)
	}
	if !ns.Valid || ns.String != "2026-10-15 08:00:00" {
		t.Fatalf("非空值应 Valid=true, got %+v", ns)
	}
}

// TestTSRejectsUnsupportedTarget 目标类型写错时必须显式报错,而不是静默丢数据。
func TestTSRejectsUnsupportedTarget(t *testing.T) {
	var n int
	if err := TS(&n).Scan("2026-10-15 08:00:00"); err == nil {
		t.Fatal("不支持的扫描目标应返回错误")
	}
}
