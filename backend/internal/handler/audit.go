package handler

import (
	"bytes"
	"dining-system/internal/logger"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============================================================================
// 操作日志(审计留痕)
//
// 三条采集路径,缺一不可:
//   1. AuditLog 中间件 —— 管理端写操作自动落库(兜底,覆盖全部接口);
//   2. WriteOperLog   —— 登录等「不在管理端中间件链上」的场景手工埋点;
//   3. SetAuditDetail —— 高风险动作在 handler 里补一句人话摘要
//                        (中间件只知道「调了 /order/settle」,不知道「免了 128 块」)。
//
// 与权限体系的关系:权限是 fail-closed(未登记 = 拒绝),审计是 fail-open
// (未登记 = 不记这条),因为「记不下来」不应让业务不可用。
// fail-open 的漏洞由 routes_test.go 的覆盖率测试补上:新增写操作却忘了登记,
// 测试直接失败。
// ============================================================================

// auditMeta 一条路由的审计元信息。
type auditMeta struct {
	Module string // 模块中文名
	Action string // 动作中文名
	Type   string // 操作类型(见 model.OperType*)
	Target string // 业务对象类型:order / user / dish ...
	IDKey  string // 从请求体取对象标识的键名(如 orderId);路径 :id 优先
}

// routeAudit 路由 → 审计元信息。key 与 gin 的 c.FullPath() 一致。
//
// 只登记「会产生变更」的写操作;GET 默认不记(量大且无变更),
// 打开 AUDIT_LOG_GET 时才记,未登记的 GET 走默认元信息。
var routeAudit = map[string]auditMeta{
	// ---- 登录者自身 ----
	"POST /prod-api/dining/auth/password": {Module: "登录账号", Action: "修改密码", Type: model.OperTypeUpdate, Target: "user", IDKey: "userId"},

	// ---- 员工与权限(最高危:权限被谁放大过必须能查) ----
	"POST /prod-api/dining/user/save":          {Module: "员工管理", Action: "新增员工", Type: model.OperTypeInsert, Target: "user"},
	"POST /prod-api/dining/user/update":        {Module: "员工管理", Action: "修改员工", Type: model.OperTypeUpdate, Target: "user", IDKey: "userId"},
	"POST /prod-api/dining/user/resetPassword": {Module: "员工管理", Action: "重置员工密码", Type: model.OperTypeGrant, Target: "user", IDKey: "userId"},
	"POST /prod-api/dining/user/toggleStatus":  {Module: "员工管理", Action: "启停用员工", Type: model.OperTypeGrant, Target: "user", IDKey: "userId"},
	"DELETE /prod-api/dining/user/:id":         {Module: "员工管理", Action: "删除员工", Type: model.OperTypeDelete, Target: "user", IDKey: "id"},
	"POST /prod-api/dining/role/save":          {Module: "角色权限", Action: "新增角色", Type: model.OperTypeInsert, Target: "role"},
	"POST /prod-api/dining/role/update":        {Module: "角色权限", Action: "修改角色权限", Type: model.OperTypeGrant, Target: "role", IDKey: "roleId"},
	"DELETE /prod-api/dining/role/:id":         {Module: "角色权限", Action: "删除角色", Type: model.OperTypeDelete, Target: "role", IDKey: "id"},

	// ---- 基础资料 ----
	"POST /prod-api/dining/table/save":      {Module: "桌台管理", Action: "新增桌台", Type: model.OperTypeInsert, Target: "table"},
	"POST /prod-api/dining/table/update":    {Module: "桌台管理", Action: "修改桌台", Type: model.OperTypeUpdate, Target: "table", IDKey: "tableId"},
	"DELETE /prod-api/dining/table/:id":     {Module: "桌台管理", Action: "删除桌台", Type: model.OperTypeDelete, Target: "table", IDKey: "id"},
	"POST /prod-api/dining/category/save":   {Module: "分类管理", Action: "新增分类", Type: model.OperTypeInsert, Target: "category"},
	"POST /prod-api/dining/category/update": {Module: "分类管理", Action: "修改分类", Type: model.OperTypeUpdate, Target: "category", IDKey: "categoryId"},
	"DELETE /prod-api/dining/category/:id":  {Module: "分类管理", Action: "删除分类", Type: model.OperTypeDelete, Target: "category", IDKey: "id"},
	"POST /prod-api/dining/dish/save":       {Module: "菜品管理", Action: "新增菜品", Type: model.OperTypeInsert, Target: "dish"},
	"POST /prod-api/dining/dish/update":     {Module: "菜品管理", Action: "修改菜品", Type: model.OperTypeUpdate, Target: "dish", IDKey: "dishId"},
	"DELETE /prod-api/dining/dish/:id":      {Module: "菜品管理", Action: "删除菜品", Type: model.OperTypeDelete, Target: "dish", IDKey: "id"},
	"POST /prod-api/dining/remark/save":     {Module: "备注管理", Action: "新增备注", Type: model.OperTypeInsert, Target: "remark"},
	"POST /prod-api/dining/remark/update":   {Module: "备注管理", Action: "修改备注", Type: model.OperTypeUpdate, Target: "remark", IDKey: "remarkId"},
	"DELETE /prod-api/dining/remark/:id":    {Module: "备注管理", Action: "删除备注", Type: model.OperTypeDelete, Target: "remark", IDKey: "id"},

	// ---- 打印机(注意 test/probe/bind/clear 会改动打印机或出纸,同样留痕) ----
	"POST /prod-api/dining/printer/save":      {Module: "打印机管理", Action: "新增打印机", Type: model.OperTypeInsert, Target: "printer"},
	"POST /prod-api/dining/printer/update":    {Module: "打印机管理", Action: "修改打印机", Type: model.OperTypeUpdate, Target: "printer", IDKey: "printerId"},
	"DELETE /prod-api/dining/printer/:id":     {Module: "打印机管理", Action: "删除打印机", Type: model.OperTypeDelete, Target: "printer", IDKey: "id"},
	"POST /prod-api/dining/printer/test/:id":  {Module: "打印机管理", Action: "打印测试页", Type: model.OperTypePrint, Target: "printer", IDKey: "id"},
	"POST /prod-api/dining/printer/probe/:id": {Module: "打印机管理", Action: "探测打印机", Type: model.OperTypeOther, Target: "printer", IDKey: "id"},
	"POST /prod-api/dining/printer/bind":      {Module: "打印机管理", Action: "绑定飞鹅账号", Type: model.OperTypeUpdate, Target: "printer"},
	"POST /prod-api/dining/printer/clear/:id": {Module: "打印机管理", Action: "清空云端队列", Type: model.OperTypeUpdate, Target: "printer", IDKey: "id"},

	// ---- 打印留痕与补打 ----
	"POST /prod-api/dining/print/log/reprint": {Module: "打印记录", Action: "补打小票", Type: model.OperTypePrint, Target: "print", IDKey: "printId"},
	"POST /prod-api/dining/order/reprint":     {Module: "打印记录", Action: "补打订单小票", Type: model.OperTypePrint, Target: "order", IDKey: "orderId"},

	// ---- 系统配置 ----
	"POST /prod-api/dining/config/save": {Module: "系统配置", Action: "修改系统配置", Type: model.OperTypeUpdate, Target: "config"},

	// ---- 订单(金额相关,必须留痕) ----
	"POST /prod-api/dining/order/status":        {Module: "订单管理", Action: "订单流转", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/finish":        {Module: "订单管理", Action: "完成订单", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/urge/handle":   {Module: "订单管理", Action: "处理催菜", Type: model.OperTypeUpdate, Target: "urge", IDKey: "urgeId"},
	"POST /prod-api/dining/order/pay":           {Module: "订单管理", Action: "订单收款", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/settle":        {Module: "订单管理", Action: "订单结账", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/credit/settle": {Module: "挂账管理", Action: "挂账核销", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/settle/cancel": {Module: "订单管理", Action: "撤销结算", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/edit":          {Module: "订单管理", Action: "订单改单", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/order/cancel":        {Module: "订单管理", Action: "取消订单", Type: model.OperTypeDelete, Target: "order", IDKey: "orderId"},

	// ---- 退款 ----
	"POST /prod-api/dining/pay/refund":       {Module: "退款管理", Action: "发起退款", Type: model.OperTypeUpdate, Target: "order", IDKey: "orderId"},
	"POST /prod-api/dining/pay/refund/query": {Module: "退款管理", Action: "同步退款状态", Type: model.OperTypeOther, Target: "refund", IDKey: "refundId"},

	// ---- 操作日志自身 ----
	"POST /prod-api/dining/log/clean": {Module: "操作日志", Action: "清理操作日志", Type: model.OperTypeDelete, Target: "operlog"},
}

// AuditMetaForRoute 查询某路由的审计元信息,第二个返回值表示是否已登记。
func AuditMetaForRoute(method, fullPath string) (auditMeta, bool) {
	m, ok := routeAudit[method+" "+fullPath]
	return m, ok
}

// RegisteredAuditKeys 返回审计表里登记的全部键(供覆盖率测试核对)。
func RegisteredAuditKeys() []string {
	keys := make([]string, 0, len(routeAudit))
	for k := range routeAudit {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// 语义摘要
// ============================================================================

const ctxAuditDetailKey = "dining.audit.detail"

// auditExtra 业务 handler 补充的审计信息。
type auditExtra struct {
	targetType string
	targetID   string
	detail     string
}

// SetAuditDetail 由业务 handler 补充「人话摘要」与操作对象。
//
// 中间件只能看到「调了哪个接口 + 传了什么参数」,看不出业务含义;
// 涉及金额与权限的动作必须在 handler 里显式说清楚,事后才对得上账。
// 同一请求多次调用以最后一次为准(一个请求只做一件事)。
func SetAuditDetail(c *gin.Context, targetType, targetID, detail string) {
	c.Set(ctxAuditDetailKey, &auditExtra{targetType: targetType, targetID: targetID, detail: detail})
}

func currentAuditExtra(c *gin.Context) *auditExtra {
	if v, ok := c.Get(ctxAuditDetailKey); ok {
		if e, ok := v.(*auditExtra); ok {
			return e
		}
	}
	return nil
}

// ============================================================================
// 中间件
// ============================================================================

// maxAuditBody 允许读取请求体的上限。超过则不记参数(但仍完整放行请求)。
const maxAuditBody = 256 << 10

// AuditLog 操作日志中间件,必须挂在 AdminAuth 之后(需要操作人身份)。
//
// 流程:读请求体(读完塞回去,handler 才能正常绑定) → 包一层 ResponseWriter
// 抓响应(取失败原因)→ c.Next() → 按 HTTP 状态判定成败 → 落库。
// 写入失败只打 stderr:审计是旁路数据,绝不能因为记日志把业务搞挂。
func AuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !store.AuditEnabled() {
			c.Next()
			return
		}
		method, path := c.Request.Method, c.FullPath()
		meta, ok := AuditMetaForRoute(method, path)
		if method == http.MethodGet && !store.AuditLogGet() {
			c.Next()
			return
		}
		if !ok {
			// 未登记:用路由本身兜底记一条,保证「不漏」优先于「好看」。
			meta = auditMeta{Module: moduleOfRoute(path), Action: method, Type: model.OperTypeOther}
		}

		start := time.Now()
		raw := readAuditBody(c)
		bw := &auditBodyWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
		c.Writer = bw

		c.Next()

		status := c.Writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		success := status < http.StatusBadRequest
		errMsg := ""
		if !success {
			errMsg = responseMsg(bw.buf.String())
		}

		targetType, targetID, detail := meta.Target, resolveTargetID(c, meta, raw), ""
		if e := currentAuditExtra(c); e != nil {
			detail = e.detail
			if e.targetType != "" {
				targetType = e.targetType
			}
			if e.targetID != "" {
				targetID = e.targetID
			}
		}
		auth := currentAuth(c)
		entry := model.OperLog{
			Module:       meta.Module,
			BusinessType: meta.Type,
			Action:       meta.Action,
			Method:       method + " " + path,
			RequestURL:   auditRequestURL(c),
			OperIP:       c.ClientIP(),
			TargetType:   targetType,
			TargetID:     targetID,
			OperParam:    store.MaskParams(raw),
			Detail:       detail,
			Status:       model.OperStatusSuccess,
			ErrorMsg:     errMsg,
			CostMs:       int(time.Since(start).Milliseconds()),
			CreateTime:   store.Now(),
		}
		if !success {
			entry.Status = model.OperStatusFail
		}
		if auth != nil {
			entry.OperatorID = auth.UserID
			entry.Operator = auth.DisplayName()
			entry.OperatorRole = auth.RoleName
		} else {
			// 理论上不会发生(中间件在 AdminAuth 之后),兜底从令牌取用户名。
			entry.Operator = usernameFromToken(c)
		}
		if err := store.InsertOperLog(entry); err != nil {
			logger.Warnf("[audit] 写入操作日志失败(%s): %v", entry.Method, err)
		}
	}
}

// auditBodyWriter 在透传响应的同时留一份副本,用于取失败原因。
//
// 之所以要抓响应:审计不只是「做了什么」,还得记「为什么没做成」——
// 「免单失败:未填写原因」比一行冷冰冰的 status=0 有用得多。
type auditBodyWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *auditBodyWriter) Write(b []byte) (int, error) {
	if w.buf.Len() < maxAuditBody {
		w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// WriteString 与 Write 同理:gin 的部分响应路径(c.String 等)走这里,
// 不覆盖的话失败原因会漏抓。
func (w *auditBodyWriter) WriteString(s string) (int, error) {
	if w.buf.Len() < maxAuditBody {
		w.buf.WriteString(s)
	}
	return w.ResponseWriter.WriteString(s)
}

// readAuditBody 读取请求体并原样塞回,保证后续 ShouldBindJSON 不受影响。
//
// 跳过 multipart:上传是文件流,读进来既占内存又没法脱敏,
// 「传了什么文件」由 upload handler 自己决定要不要记。
func readAuditBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	ct := c.Request.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		return ""
	}
	if c.Request.ContentLength > maxAuditBody {
		return ""
	}
	raw, err := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	return string(raw)
}

// resolveTargetID 确定操作对象标识:路径 :id 优先,其次请求体里的 ID 字段。
func resolveTargetID(c *gin.Context, meta auditMeta, raw string) string {
	if v := clampStr(strings.TrimSpace(c.Param("id")), 64); v != "" {
		return v
	}
	if meta.IDKey == "" || strings.TrimSpace(raw) == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	switch v := m[meta.IDKey].(type) {
	case float64:
		// JSON 数字统一映射为 float64:用 FormatFloat 而非强转 int,
		// 避免超大数溢出产生负值;NaN/Inf 视为无效。
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}
		return clampStr(strconv.FormatFloat(v, 'f', -1, 64), 64)
	case string:
		return clampStr(strings.TrimSpace(v), 64)
	}
	return ""
}

// clampStr 按字符截断到 max 长。列宽是硬上限(MySQL STRICT 超长会拒绝整条
// INSERT,导致这条审计静默丢失),所以宁可截断也不能让它把记录带崩。
func clampStr(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// auditRequestURL 取含 query 的请求路径(截断防超长)。
func auditRequestURL(c *gin.Context) string {
	u := c.Request.URL.RequestURI()
	if len([]rune(u)) > 255 {
		return string([]rune(u)[:255])
	}
	return u
}

// responseMsg 从响应 JSON 里取 msg 字段(失败原因)。
func responseMsg(body string) string {
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil || r.Msg == "" {
		if len([]rune(body)) > 500 {
			return string([]rune(body)[:500])
		}
		return body
	}
	if len([]rune(r.Msg)) > 500 {
		return string([]rune(r.Msg)[:500])
	}
	return r.Msg
}

// moduleOfRoute 未登记路由兜底:取路径第二段作为模块名。
func moduleOfRoute(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for _, p := range parts {
		if p == "dining" || p == "prod-api" {
			continue
		}
		return p
	}
	return "其它"
}

// ============================================================================
// 手工埋点:登录 / 改密 / 越权尝试
// ============================================================================

// WriteOperLog 写入一条操作日志(补全 IP / URL / 时间)。
//
// 用于「不在管理端中间件链上」的场景:登录是公开接口,走不到 AuditLog。
func WriteOperLog(c *gin.Context, l model.OperLog) {
	if !store.AuditEnabled() {
		return
	}
	if l.RequestURL == "" {
		l.RequestURL = auditRequestURL(c)
	}
	if l.OperIP == "" {
		l.OperIP = c.ClientIP()
	}
	if l.CreateTime == "" {
		l.CreateTime = store.Now()
	}
	if err := store.InsertOperLog(l); err != nil {
		logger.Warnf("[audit] 写入操作日志失败(%s): %v", l.Action, err)
	}
}

// WriteLoginLog 记录一次登录尝试(成功与失败都记)。
//
// 失败尝试是安全审计的关键证据:连续密码错误可能是撞库,
// 而 tb_user 上的 last_login_time 只保留最后一次成功,看不出这些。
func WriteLoginLog(c *gin.Context, userID int, username string, success bool, msg string) {
	status := model.OperStatusFail
	if success {
		status = model.OperStatusSuccess
	}
	WriteOperLog(c, model.OperLog{
		Module:       "登录账号",
		BusinessType: model.OperTypeLogin,
		Action:       "登录系统",
		Method:       "POST /prod-api/auth/login",
		OperatorID:   userID,
		Operator:     username,
		Status:       status,
		ErrorMsg:     msg,
	})
}

// writeDeniedLog 记录被拒绝的访问(401 未登录 / 403 越权)。
//
// 这两类请求发生在 AdminAuth / RequirePerm 里,在 AuditLog 之前就被 Abort 了,
// 中间件看不到。「谁在尝试越权」恰恰是最该留痕的一类,故在此单独埋点。
// 匿名请求(没有令牌)不记 —— 否则任何人扫一下端口就能灌满日志。
func writeDeniedLog(c *gin.Context, action, msg string) {
	if !store.AuditEnabled() {
		return
	}
	operator, operatorID := "", 0
	if a := currentAuth(c); a != nil {
		operator, operatorID = a.DisplayName(), a.UserID
	} else {
		operator = usernameFromToken(c)
	}
	if operator == "" {
		return
	}
	WriteOperLog(c, model.OperLog{
		Module:       "系统安全",
		BusinessType: model.OperTypeOther,
		Action:       action,
		Method:       c.Request.Method + " " + c.FullPath(),
		OperatorID:   operatorID,
		Operator:     operator,
		Status:       model.OperStatusFail,
		ErrorMsg:     msg,
	})
}

// ============================================================================
// 查询与清理接口
// ============================================================================

// LogList 操作日志列表(分页 + 多条件筛选)。
func LogList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var status *int
	if s := c.Query("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			status = &v
		}
	}
	total, list, err := store.ListOperLogs(store.OperLogQuery{
		Operator:     strings.TrimSpace(c.Query("operator")),
		Module:       strings.TrimSpace(c.Query("module")),
		BusinessType: strings.TrimSpace(c.Query("businessType")),
		TargetType:   strings.TrimSpace(c.Query("targetType")),
		TargetID:     strings.TrimSpace(c.Query("targetId")),
		Status:       status,
		BeginTime:    strings.TrimSpace(c.Query("beginTime")),
		EndTime:      strings.TrimSpace(c.Query("endTime")),
	}, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, total, list)
}

// LogClean 按保留天数清理过期日志。
//
// 只提供「按时间整段清理」:审计记录不允许挑着删,否则留痕可被定点抹掉。
// 天数只来自 AUDIT_RETENTION_DAYS 配置,**忽略请求里传入的任何天数** ——
// 否则持有 log:manage 的人可以传 days=99999 一次抹掉全部历史,
// 「清理动作本身有留痕」救不回被删掉的数据。
// 清理动作本身会被 AuditLog 中间件记一条,谁清的、清了多少仍有据可查。
func LogClean(c *gin.Context) {
	days := store.AuditRetentionDays()
	if days <= 0 {
		fail(c, "未配置保留天数(AUDIT_RETENTION_DAYS),已禁用清理")
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	n, err := store.CleanOperLogs(cutoff)
	if err != nil {
		fail(c, "清理失败: "+err.Error())
		return
	}
	SetAuditDetail(c, "operlog", "", fmt.Sprintf("清理 %s 之前的记录，共 %d 条（保留 %d 天）", cutoff, n, days))
	okMsg(c, fmt.Sprintf("已清理 %d 条过期操作日志", n))
}
