package dao

import (
	"path/filepath"
	"testing"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

func svcAuditInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "audit-more.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func svcAuditInsert(t *testing.T, l po.OperLog) {
	t.Helper()
	if err := InsertOperLog(l); err != nil {
		t.Fatalf("InsertOperLog 失败: %v", err)
	}
}

func svcAuditInt(v int) *int { return &v }

func TestSvcAuditInsertAndScanOperLog(t *testing.T) {
	svcAuditInit(t)

	want := po.OperLog{
		Module:       "订单管理",
		BusinessType: po.OperTypeUpdate,
		Action:       "修改订单",
		Method:       "POST /api/admin/order/update",
		RequestURL:   "/api/admin/order/update?id=1",
		OperatorID:   7,
		Operator:     "张三",
		OperatorRole: "收银员",
		OperIP:       "127.0.0.1",
		TargetType:   "order",
		TargetID:     "O1",
		OperParam:    `{"orderId":1}`,
		Detail:       "改了数量",
		Status:       po.OperStatusSuccess,
		ErrorMsg:     "",
		CostMs:       12,
		CreateTime:   "2026-01-01 10:00:00",
	}
	svcAuditInsert(t, want)

	rows, err := store.DB.Query(`SELECT ` + OperLogCols + ` FROM tb_oper_log WHERE log_id=1`)
	if err != nil {
		t.Fatalf("查询操作日志失败: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("应有 1 行操作日志")
	}
	got, err := ScanOperLog(rows)
	if err != nil {
		t.Fatalf("ScanOperLog 失败: %v", err)
	}
	if got.LogID != 1 {
		t.Fatalf("LogID=%d, 期望 1", got.LogID)
	}
	if got.Module != want.Module || got.Operator != want.Operator || got.TargetID != want.TargetID ||
		got.Status != want.Status || got.CostMs != want.CostMs || got.CreateTime != want.CreateTime {
		t.Fatalf("字段不匹配: got %+v, want %+v", got, want)
	}
}

func TestSvcAuditScanOperLogColumnMismatch(t *testing.T) {
	svcAuditInit(t)
	svcAuditInsert(t, po.OperLog{Module: "登录", CreateTime: "2026-01-01 10:00:00"})

	rows, err := store.DB.Query(`SELECT log_id FROM tb_oper_log LIMIT 1`)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("应有 1 行")
	}
	if _, err := ScanOperLog(rows); err == nil {
		t.Fatal("列数不足时 ScanOperLog 应返回错误")
	}
}

func TestSvcAuditListOperLogs(t *testing.T) {
	svcAuditInit(t)

	seed := []po.OperLog{
		{Operator: "张三", Module: "订单管理", BusinessType: "update", TargetType: "order", TargetID: "O1", Status: po.OperStatusSuccess, CreateTime: "2026-01-01 10:00:00"},
		{Operator: "李四", Module: "员工管理", BusinessType: "grant", TargetType: "user", TargetID: "U1", Status: po.OperStatusFail, CreateTime: "2026-01-02 10:00:00"},
		{Operator: "张三丰", Module: "订单管理", BusinessType: "delete", TargetType: "order", TargetID: "O2", Status: po.OperStatusSuccess, CreateTime: "2026-01-03 10:00:00"},
	}
	for _, l := range seed {
		svcAuditInsert(t, l)
	}

	cases := []struct {
		name      string
		q         OperLogQuery
		pageNum   int
		pageSize  int
		wantTotal int
		wantLen   int
		wantFirst string
	}{
		{"无筛选第1页", OperLogQuery{}, 1, 2, 3, 2, "张三丰"},
		{"无筛选第2页", OperLogQuery{}, 2, 2, 3, 1, "张三"},
		{"操作人模糊", OperLogQuery{Operator: "张三"}, 1, 10, 2, 2, "张三丰"},
		{"操作人精确", OperLogQuery{Operator: "李四"}, 1, 10, 1, 1, "李四"},
		{"模块筛选", OperLogQuery{Module: "订单管理"}, 1, 10, 2, 2, "张三丰"},
		{"业务类型筛选", OperLogQuery{BusinessType: "grant"}, 1, 10, 1, 1, "李四"},
		{"对象筛选", OperLogQuery{TargetType: "order", TargetID: "O2"}, 1, 10, 1, 1, "张三丰"},
		{"只看失败", OperLogQuery{Status: svcAuditInt(po.OperStatusFail)}, 1, 10, 1, 1, "李四"},
		{"只看成功", OperLogQuery{Status: svcAuditInt(po.OperStatusSuccess)}, 1, 10, 2, 2, "张三丰"},
		{"时间下界", OperLogQuery{BeginTime: "2026-01-02 00:00:00"}, 1, 10, 2, 2, "张三丰"},
		{"时间上界", OperLogQuery{EndTime: "2026-01-01 23:59:59"}, 1, 10, 1, 1, "张三"},
		{"无匹配", OperLogQuery{Operator: "不存在的人"}, 1, 10, 0, 0, ""},
		{"超出页数", OperLogQuery{}, 3, 2, 3, 0, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			total, list, err := ListOperLogs(c.q, c.pageNum, c.pageSize)
			if err != nil {
				t.Fatalf("ListOperLogs 失败: %v", err)
			}
			if total != c.wantTotal {
				t.Errorf("total=%d, 期望 %d", total, c.wantTotal)
			}
			if len(list) != c.wantLen {
				t.Errorf("返回条数=%d, 期望 %d", len(list), c.wantLen)
			}
			if c.wantFirst != "" {
				if len(list) == 0 || list[0].Operator != c.wantFirst {
					t.Errorf("首条 operator=%q, 期望 %q", svcAuditFirstOperator(list), c.wantFirst)
				}
			}
		})
	}
}

func svcAuditFirstOperator(list []po.OperLog) string {
	if len(list) == 0 {
		return ""
	}
	return list[0].Operator
}

func TestSvcAuditCleanOperLogs(t *testing.T) {
	svcAuditInit(t)

	svcAuditInsert(t, po.OperLog{Module: "老日志", CreateTime: "2026-01-01 10:00:00"})
	svcAuditInsert(t, po.OperLog{Module: "老日志2", CreateTime: "2026-01-02 10:00:00"})
	svcAuditInsert(t, po.OperLog{Module: "新日志", CreateTime: "2026-01-03 10:00:00"})

	n, err := CleanOperLogs("2026-01-02 00:00:00")
	if err != nil {
		t.Fatalf("CleanOperLogs 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应删除 1 条(严格早于阈值), got %d", n)
	}

	var left int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_oper_log`).Scan(&left); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 2 {
		t.Fatalf("剩余条数=%d, 期望 2", left)
	}
}

func TestSvcAuditCleanExpiredOperLogs(t *testing.T) {
	svcAuditInit(t)
	t.Setenv(conf.EnvAuditRetentionDays, "1")

	old := time.Now().AddDate(0, 0, -10).Format(conf.TimeLayout)
	fresh := time.Now().Format(conf.TimeLayout)
	svcAuditInsert(t, po.OperLog{Module: "过期", CreateTime: old})
	svcAuditInsert(t, po.OperLog{Module: "新鲜", CreateTime: fresh})

	n := CleanExpiredOperLogs()
	if n != 1 {
		t.Fatalf("应清理 1 条过期日志, got %d", n)
	}
	var module string
	if err := store.DB.QueryRow(`SELECT module FROM tb_oper_log`).Scan(&module); err != nil {
		t.Fatalf("查询剩余日志失败: %v", err)
	}
	if module != "新鲜" {
		t.Fatalf("剩余日志应为「新鲜」, got %q", module)
	}
}

func TestSvcAuditCleanExpiredOperLogsDisabled(t *testing.T) {
	svcAuditInit(t)
	t.Setenv(conf.EnvAuditRetentionDays, "0")
	svcAuditInsert(t, po.OperLog{Module: "永不清理", CreateTime: time.Now().AddDate(0, 0, -100).Format(conf.TimeLayout)})

	if n := CleanExpiredOperLogs(); n != 0 {
		t.Fatalf("保留天数<=0 时不应清理, got %d", n)
	}
	var left int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_oper_log`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("保留天数<=0 时日志应原样保留: left=%d err=%v", left, err)
	}
}

func TestSvcAuditEnabled(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"默认", "", true},
		{"0", "0", false},
		{"false", "false", false},
		{"off", "off", false},
		{"1", "1", true},
		{"true", "true", true},
		{"on", "on", true},
		{"大写FALSE", "FALSE", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(conf.EnvAuditLogEnabled, c.val)
			if got := AuditEnabled(); got != c.want {
				t.Errorf("AuditEnabled()=%v, 期望 %v", got, c.want)
			}
		})
	}
}

func TestSvcAuditLogGet(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"默认", "", false},
		{"1", "1", true},
		{"true", "true", true},
		{"on", "on", true},
		{"0", "0", false},
		{"false", "false", false},
		{"off", "off", false},
		{"大写ON", "ON", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(conf.EnvAuditLogGet, c.val)
			if got := AuditLogGet(); got != c.want {
				t.Errorf("AuditLogGet()=%v, 期望 %v", got, c.want)
			}
		})
	}
}

func TestSvcAuditRetentionDays(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want int
	}{
		{"默认", "", 365},
		{"零", "0", 0},
		{"正常", "30", 30},
		{"负数", "-5", 365},
		{"非法", "abc", 365},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(conf.EnvAuditRetentionDays, c.val)
			if got := AuditRetentionDays(); got != c.want {
				t.Errorf("AuditRetentionDays()=%d, 期望 %d", got, c.want)
			}
		})
	}
}
