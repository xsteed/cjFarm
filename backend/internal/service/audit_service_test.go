package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// auditInitDB 初始化 SQLite 临时库,供操作日志落库/查询/清理测试使用。
func auditInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "audit.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// TestAuditEnabled 校验操作日志总开关(默认开启)。
func TestAuditEnabled(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"默认开启", "", true},
		{"明确开启", "1", true},
		{"字符串开启", "true", true},
		{"关闭", "0", false},
		{"false关闭", "false", false},
		{"off关闭", "off", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.val == "" {
				t.Setenv(conf.EnvAuditLogEnabled, "")
			} else {
				t.Setenv(conf.EnvAuditLogEnabled, c.val)
			}
			if got := AuditEnabled(); got != c.want {
				t.Fatalf("AuditEnabled()=%v, want %v", got, c.want)
			}
		})
	}
}

// TestAuditLogGet 校验只读请求审计开关(默认关闭)。
func TestAuditLogGet(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"默认关闭", "", false},
		{"明确开启", "1", true},
		{"字符串开启", "true", true},
		{"on开启", "on", true},
		{"关闭", "0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.val == "" {
				t.Setenv(conf.EnvAuditLogGet, "")
			} else {
				t.Setenv(conf.EnvAuditLogGet, c.val)
			}
			if got := AuditLogGet(); got != c.want {
				t.Fatalf("AuditLogGet()=%v, want %v", got, c.want)
			}
		})
	}
}

// TestMaskParams 校验请求体 JSON 的敏感值脱敏。
func TestMaskParams(t *testing.T) {
	t.Run("敏感键脱敏", func(t *testing.T) {
		if got := MaskParams(`{"password":"secret123"}`); got != `{"password":"******"}` {
			t.Fatalf("脱敏结果 = %q", got)
		}
	})
	t.Run("多键保留结构", func(t *testing.T) {
		got := MaskParams(`{"name":"张三","password":"pw","age":18}`)
		if !strings.Contains(got, "张三") || !strings.Contains(got, "******") || strings.Contains(got, "pw") {
			t.Fatalf("多键脱敏结果 = %q", got)
		}
	})
	t.Run("嵌套对象递归脱敏", func(t *testing.T) {
		got := MaskParams(`{"a":{"token":"t"}}`)
		if !strings.Contains(got, "******") || strings.Contains(got, `"t"`) {
			t.Fatalf("嵌套脱敏结果 = %q", got)
		}
	})
	t.Run("数组元素递归脱敏", func(t *testing.T) {
		got := MaskParams(`{"list":[{"pwd":"x"}]}`)
		if !strings.Contains(got, "******") || strings.Contains(got, `"x"`) {
			t.Fatalf("数组脱敏结果 = %q", got)
		}
	})
	t.Run("路径后缀不脱敏", func(t *testing.T) {
		got := MaskParams(`{"wxpay_private_key_path":"/etc/key.pem"}`)
		if !strings.Contains(got, "/etc/key.pem") {
			t.Fatalf("_path 不应脱敏, got %q", got)
		}
	})
	t.Run("非JSON原样返回", func(t *testing.T) {
		if got := MaskParams("plain text"); got != "plain text" {
			t.Fatalf("非 JSON 应原样返回, got %q", got)
		}
	})
	t.Run("空串", func(t *testing.T) {
		if got := MaskParams("  "); got != "" {
			t.Fatalf("空串应返回空, got %q", got)
		}
	})
}

// TestNow 校验审计时间格式。
func TestNow(t *testing.T) {
	if _, err := time.Parse(conf.TimeLayout, Now()); err != nil {
		t.Fatalf("Now() 格式非法: %v", err)
	}
}

// TestAuditOperLogCRUD 校验操作日志写入、查询与筛选。
func TestAuditOperLogCRUD(t *testing.T) {
	auditInitDB(t)

	for i := 0; i < 3; i++ {
		if err := InsertOperLog(po.OperLog{
			Module: "订单管理", BusinessType: "insert", Action: "新增订单", Operator: "张三",
			TargetType: "order", TargetID: "D100", Status: 1, CreateTime: store.Now(),
		}); err != nil {
			t.Fatalf("InsertOperLog 失败: %v", err)
		}
	}
	// 一条失败日志。
	if err := InsertOperLog(po.OperLog{
		Module: "员工管理", BusinessType: "login", Operator: "李四", Status: 0, CreateTime: store.Now(),
	}); err != nil {
		t.Fatalf("InsertOperLog 失败: %v", err)
	}

	total, list, err := ListOperLogs(OperLogFilter{}, 1, 10)
	if err != nil {
		t.Fatalf("ListOperLogs 失败: %v", err)
	}
	if total != 4 || len(list) != 4 {
		t.Fatalf("total=%d len=%d, want 4/4", total, len(list))
	}

	// 操作人筛选。
	total, list, err = ListOperLogs(OperLogFilter{Operator: "张三"}, 1, 10)
	if err != nil || total != 3 || len(list) != 3 {
		t.Fatalf("操作人筛选异常: total=%d len=%d err=%v", total, len(list), err)
	}
	// 模块筛选。
	total, _, err = ListOperLogs(OperLogFilter{Module: "员工管理"}, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("模块筛选异常: total=%d err=%v", total, err)
	}
	// 状态筛选(0 表示失败,须用指针避免零值歧义)。
	status := 0
	total, _, err = ListOperLogs(OperLogFilter{Status: &status}, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("状态筛选异常: total=%d err=%v", total, err)
	}
	// 分页。
	total, list, err = ListOperLogs(OperLogFilter{}, 1, 2)
	if err != nil || total != 4 || len(list) != 2 {
		t.Fatalf("分页异常: total=%d len=%d err=%v", total, len(list), err)
	}
}

// TestAuditScanOperLog 校验单行扫描。
func TestAuditScanOperLog(t *testing.T) {
	auditInitDB(t)
	if err := InsertOperLog(po.OperLog{Module: "订单", Operator: "王五", Status: 1, CreateTime: store.Now()}); err != nil {
		t.Fatalf("InsertOperLog 失败: %v", err)
	}
	row := store.DB.QueryRow(`SELECT `+dao.OperLogCols+` FROM tb_oper_log WHERE operator=?`, "王五")
	log, err := ScanOperLog(row)
	if err != nil {
		t.Fatalf("ScanOperLog 失败: %v", err)
	}
	if log.Operator != "王五" || log.Module != "订单" {
		t.Fatalf("扫描结果异常: %+v", log)
	}
}

// TestCleanExpiredOperLogs 校验按保留天数清理过期日志。
func TestCleanExpiredOperLogs(t *testing.T) {
	t.Setenv(conf.EnvAuditRetentionDays, "1")
	auditInitDB(t)

	// 一条远古日志、一条当前日志。
	if err := InsertOperLog(po.OperLog{Module: "订单", Status: 1, CreateTime: "2000-01-01 00:00:00"}); err != nil {
		t.Fatalf("InsertOperLog 失败: %v", err)
	}
	if err := InsertOperLog(po.OperLog{Module: "订单", Status: 1, CreateTime: store.Now()}); err != nil {
		t.Fatalf("InsertOperLog 失败: %v", err)
	}

	days, cutoff, n, err := CleanExpiredOperLogs()
	if err != nil {
		t.Fatalf("CleanExpiredOperLogs 失败: %v", err)
	}
	if days != 1 || cutoff == "" || n != 1 {
		t.Fatalf("清理结果异常: days=%d cutoff=%q n=%d", days, cutoff, n)
	}
	total, _, err := ListOperLogs(OperLogFilter{}, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("清理后应剩 1 条, total=%d err=%v", total, err)
	}
}

// TestCleanExpiredOperLogsDisabled 校验未配置保留天数时拒绝清理。
func TestCleanExpiredOperLogsDisabled(t *testing.T) {
	t.Setenv(conf.EnvAuditRetentionDays, "0")
	auditInitDB(t)

	days, _, n, err := CleanExpiredOperLogs()
	if err == nil {
		t.Fatal("保留天数<=0 应返回错误")
	}
	if days != 0 || n != 0 {
		t.Fatalf("禁用清理时 days=%d n=%d, want 0/0", days, n)
	}
}
