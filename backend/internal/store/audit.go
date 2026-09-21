package store

import (
	"dining-system/internal/logger"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"dining-system/internal/model"
)

// ============================================================================
// 操作日志(审计留痕)
//
// 定位:与 tb_print_log 同属「旁路数据」—— 写入失败只记 stderr,
// 绝不反向影响业务主流程(日志丢了可以补,下单/结账被拖垮不行)。
//
// 只有「按时间整段清理」一个出口,没有单条修改/删除接口:
// 审计记录本身必须不可篡改,否则留痕就没有意义。
// ============================================================================

// OperLogCols 操作日志完整列(与 ScanOperLog 顺序一一对应)。
const OperLogCols = `log_id, module, business_type, action, method, request_url,
	operator_id, operator, operator_role, oper_ip, target_type, target_id,
	oper_param, detail, status, error_msg, cost_ms, create_time`

// ScanOperLog 扫描一行操作日志。
func ScanOperLog(rows interface{ Scan(...interface{}) error }) (model.OperLog, error) {
	var l model.OperLog
	err := rows.Scan(&l.LogID, &l.Module, &l.BusinessType, &l.Action, &l.Method, &l.RequestURL,
		&l.OperatorID, &l.Operator, &l.OperatorRole, &l.OperIP, &l.TargetType, &l.TargetID,
		&l.OperParam, &l.Detail, &l.Status, &l.ErrorMsg, &l.CostMs, &l.CreateTime)
	return l, err
}

// InsertOperLog 写入一条操作日志。
func InsertOperLog(l model.OperLog) error {
	_, err := DB.Exec(`INSERT INTO tb_oper_log(module, business_type, action, method, request_url,
		operator_id, operator, operator_role, oper_ip, target_type, target_id,
		oper_param, detail, status, error_msg, cost_ms, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.Module, l.BusinessType, l.Action, l.Method, l.RequestURL,
		l.OperatorID, l.Operator, l.OperatorRole, l.OperIP, l.TargetType, l.TargetID,
		l.OperParam, l.Detail, l.Status, l.ErrorMsg, l.CostMs, l.CreateTime)
	return err
}

// OperLogQuery 操作日志筛选条件。
//
// Status 用指针:0 表示「失败」,若用 int,零值会被误当成「只看失败」
// (与 UserQuery.Status 同一个坑)。
type OperLogQuery struct {
	Operator     string // 操作人显示名模糊匹配
	Module       string // 模块
	BusinessType string // 操作类型
	TargetType   string // 业务对象类型
	TargetID     string // 业务对象标识
	Status       *int   // nil 表示不限
	BeginTime    string // 起始时间(含),YYYY-MM-DD HH:MM:SS
	EndTime      string // 结束时间(含)
}

// ListOperLogs 分页查询操作日志(按时间倒序)。
func ListOperLogs(q OperLogQuery, pageNum, pageSize int) (int, []model.OperLog, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if kw := strings.TrimSpace(q.Operator); kw != "" {
		where = append(where, "operator LIKE ?")
		args = append(args, "%"+kw+"%")
	}
	if v := strings.TrimSpace(q.Module); v != "" {
		where = append(where, "module=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.BusinessType); v != "" {
		where = append(where, "business_type=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.TargetType); v != "" {
		where = append(where, "target_type=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.TargetID); v != "" {
		where = append(where, "target_id=?")
		args = append(args, v)
	}
	if q.Status != nil {
		where = append(where, "status=?")
		args = append(args, *q.Status)
	}
	if v := strings.TrimSpace(q.BeginTime); v != "" {
		where = append(where, "create_time>=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.EndTime); v != "" {
		where = append(where, "create_time<=?")
		args = append(args, v)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_oper_log`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := DB.Query(`SELECT `+OperLogCols+` FROM tb_oper_log`+cond+
		` ORDER BY log_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	list := []model.OperLog{}
	for rows.Next() {
		if l, err := ScanOperLog(rows); err == nil {
			list = append(list, l)
		}
	}
	return total, list, nil
}

// CleanOperLogs 删除指定时间之前的全部操作日志,返回删除行数。
//
// 只允许「按时间整段清理」,不允许删单条 —— 否则审计留痕可被逐条抹掉。
func CleanOperLogs(beforeTime string) (int64, error) {
	res, err := DB.Exec(`DELETE FROM tb_oper_log WHERE create_time < ?`, beforeTime)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CleanExpiredOperLogs 按保留天数清理过期日志(启动时调用),返回删除行数。
// 保留天数 <= 0 表示不清理。
func CleanExpiredOperLogs() int64 {
	days := AuditRetentionDays()
	if days <= 0 {
		return 0
	}
	n, err := CleanOperLogs(time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05"))
	if err != nil {
		logger.Warnf("[audit] 清理过期操作日志失败: %v", err)
		return 0
	}
	return n
}

// ============================================================================
// 开关
// ============================================================================

// AuditEnabled 是否记录操作日志(AUDIT_LOG_ENABLED,默认开启)。
func AuditEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(Getenv("AUDIT_LOG_ENABLED", "1")))
	return v != "0" && v != "false" && v != "off"
}

// AuditLogGet 是否记录只读请求(AUDIT_LOG_GET,默认关闭)。
//
// GET 量级远大于写操作且审计价值低(不产生变更),默认只记写操作;
// 需要排查「谁导出了报表」这类场景时再打开。
func AuditLogGet() bool {
	v := strings.TrimSpace(strings.ToLower(Getenv("AUDIT_LOG_GET", "0")))
	return v == "1" || v == "true" || v == "on"
}

// AuditRetentionDays 操作日志保留天数(AUDIT_RETENTION_DAYS,默认 365,0 表示不清理)。
func AuditRetentionDays() int {
	n, err := strconv.Atoi(strings.TrimSpace(Getenv("AUDIT_RETENTION_DAYS", "365")))
	if err != nil || n < 0 {
		return 365
	}
	return n
}

// ============================================================================
// 参数脱敏
// ============================================================================
//
// 审计日志里最危险的不是「记了什么动作」,而是「把密码和密钥也记了进来」。
// 典型反例:config/save 提交体里带着 wxpay_apiv3_key、feie_ukey;
// user/save 带着明文密码。这些值一旦落库,操作日志页就成了泄密入口
// (它的读者比「能改系统配置」的人多得多)。
//
// 因此落库前统一走 MaskParams:按键名规则把敏感值替换为 ******。

// sensitiveKeyParts 命中任意一个即视为敏感字段(对键名做小写包含匹配)。
var sensitiveKeyParts = []string{
	"password", "passwd", "pwd",
	"secret", "token", "ukey", "apiv3",
	"privatekey", "private_key", "cert", "credential",
}

// maskedValue 脱敏占位符。
const maskedValue = "******"

// MaskParams 把请求体 JSON 脱敏后返回(保留结构,只替换敏感值)。
//
// 入参非 JSON 对象时(如表单、纯文本)不做解析,原样截断返回,
// 由调用方决定如何展示;这样至少不会把内容悄悄丢掉。
func MaskParams(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return truncateRunes(raw, 2000)
	}
	maskMap(m)
	out, err := json.Marshal(m)
	if err != nil {
		return truncateRunes(raw, 2000)
	}
	return truncateRunes(string(out), 2000)
}

// maskMap 递归脱敏:敏感键的值整体替换,嵌套对象与数组继续下探。
func maskMap(m map[string]interface{}) {
	for k, v := range m {
		if isSensitiveKey(k) {
			m[k] = maskedValue
			continue
		}
		m[k] = maskValue(v)
	}
}

func maskValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		maskMap(t)
		return t
	case []interface{}:
		for i := range t {
			t[i] = maskValue(t[i])
		}
		return t
	default:
		return v
	}
}

// isSensitiveKey 判断键名是否敏感。
//
// 排除 *_path:保存的是文件路径而非密钥内容(如 wxpay_private_key_path),
// 脱敏会让「改了哪个文件」这条信息也一起丢掉。
func isSensitiveKey(key string) bool {
	if strings.HasSuffix(key, "_path") {
		return false
	}
	k := strings.ToLower(key)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// truncateRunes 按字符(而非字节)截断,避免中文被切断产生乱码。
//
// 截断标记计入总长:列宽是硬上限(MySQL STRICT 模式超长会拒绝整条 INSERT,
// 审计记录将静默丢失),所以最终返回值必须保证不超过 max 个字符。
func truncateRunes(s string, max int) string {
	const suffix = "…(已截断)"
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= len([]rune(suffix)) {
		return string(r[:max])
	}
	return string(r[:max-len([]rune(suffix))]) + suffix
}
