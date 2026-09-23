// 审计域用例:操作日志(审计留痕)的落库、查询、脱敏与清理。
package service

import (
	"fmt"
	"strings"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============================================================================
// 操作日志(审计留痕)
// ============================================================================

// AuditEnabled 是否记录操作日志(AUDIT_LOG_ENABLED,默认开启)。
func AuditEnabled() bool {
	return dao.AuditEnabled()
}

// AuditLogGet 是否记录只读请求(AUDIT_LOG_GET,默认关闭)。
func AuditLogGet() bool {
	return dao.AuditLogGet()
}

// MaskParams 把请求体 JSON 脱敏后返回(保留结构,只替换敏感值)。
func MaskParams(raw string) string {
	return dao.MaskParams(raw)
}

// Now 返回当前时间字符串(数据库统一格式),供审计留痕时间列使用。
func Now() string {
	return store.Now()
}

// InsertOperLog 写入一条操作日志。
func InsertOperLog(l po.OperLog) error {
	return dao.InsertOperLog(l)
}

// ScanOperLog 扫描一行操作日志。
func ScanOperLog(rows interface{ Scan(...interface{}) error }) (po.OperLog, error) {
	return dao.ScanOperLog(rows)
}

// OperLogFilter 操作日志筛选条件。字段为 HTTP 查询参数原样值,
// 由 ListOperLogs 负责清洗(去空格),避免 handler 散落筛选逻辑。
//
// Status 用指针:0 表示「失败」,若用 int,零值会被误当成「只看失败」。
type OperLogFilter struct {
	Operator     string // 操作人显示名模糊匹配
	Module       string // 模块
	BusinessType string // 操作类型
	TargetType   string // 业务对象类型
	TargetID     string // 业务对象标识
	Status       *int   // nil 表示不限
	BeginTime    string // 起始时间(含),YYYY-MM-DD HH:MM:SS
	EndTime      string // 结束时间(含)
}

// ListOperLogs 分页查询操作日志(多条件筛选 + 按时间倒序)。
func ListOperLogs(f OperLogFilter, pageNum, pageSize int) (int, []po.OperLog, error) {
	return dao.ListOperLogs(dao.OperLogQuery{
		Operator:     strings.TrimSpace(f.Operator),
		Module:       strings.TrimSpace(f.Module),
		BusinessType: strings.TrimSpace(f.BusinessType),
		TargetType:   strings.TrimSpace(f.TargetType),
		TargetID:     strings.TrimSpace(f.TargetID),
		Status:       f.Status,
		BeginTime:    strings.TrimSpace(f.BeginTime),
		EndTime:      strings.TrimSpace(f.EndTime),
	}, pageNum, pageSize)
}

// CleanExpiredOperLogs 按配置保留天数清理过期日志。
//
// 只提供「按时间整段清理」:审计记录不允许挑着删,否则留痕可被定点抹掉。
// 返回 (保留天数, 截止时间, 清理条数);保留天数 <= 0 或清理失败时返回错误。
func CleanExpiredOperLogs() (days int, cutoff string, n int64, err error) {
	days = dao.AuditRetentionDays()
	if days <= 0 {
		return 0, "", 0, fmt.Errorf("未配置保留天数(AUDIT_RETENTION_DAYS),已禁用清理")
	}
	cutoff = time.Now().AddDate(0, 0, -days).Format(conf.TimeLayout)
	n, err = dao.CleanOperLogs(cutoff)
	if err != nil {
		return days, cutoff, 0, fmt.Errorf("清理失败: %w", err)
	}
	return days, cutoff, n, nil
}
