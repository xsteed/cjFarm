package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// initAuditTestDB 建一个临时库跑完迁移(含 tb_oper_log),供审计中间件测试使用。
func initAuditTestDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "audit.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// newAuditRouter 搭一个只含「假登录 + 审计中间件 + 被测路由」的最小 gin 引擎。
//
// 不走真实 AdminAuth(那需要令牌与员工账号),直接注入身份快照 ——
// 这里要测的是审计中间件本身:拿到身份后有没有正确落库。
func newAuditRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		setAuth(c, &dao.AuthInfo{
			UserID: 1, Username: "boss", RealName: "张老板",
			RoleID: 1, RoleKey: "admin", RoleName: "超级管理员", Status: po.UserStatusEnabled,
		})
		c.Next()
	}, AuditLog())
	r.POST("/api/admin/order/settle", handler)
	return r
}

func doPost(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/order/settle", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.7:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// lastOperLog 取最新一条操作日志(经 service 查询,与 handler 共用同一数据访问路径)。
func lastOperLog(t *testing.T) po.OperLog {
	t.Helper()
	_, list, err := service.ListOperLogs(service.OperLogFilter{}, 1, 1)
	if err != nil {
		t.Fatalf("未读到操作日志: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("未读到操作日志")
	}
	return list[0]
}

// TestAuditLogRecordsWriteOperation 管理端写操作必须落一条日志,且参数已脱敏。
func TestAuditLogRecordsWriteOperation(t *testing.T) {
	initAuditTestDB(t)
	r := newAuditRouter(func(c *gin.Context) {
		// 业务 handler 正常读取请求体,验证中间件「读完塞回」没有破坏绑定。
		var p struct {
			OrderID int    `json:"orderId"`
			Secret  string `json:"wxpay_apiv3_key"`
			Key     string `json:"key"`
		}
		if err := c.ShouldBindJSON(&p); err != nil || p.OrderID != 88 {
			fail(c, "参数错误")
			return
		}
		SetAuditDetail(c, "order", "88", "免单 ¥128.00，原因：客户投诉")
		okMsg(c, "免单成功")
	})

	w := doPost(r, `{"orderId":88,"wxpay_apiv3_key":"deadbeef","key":"feie-key-456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("请求应成功(证明请求体被正确塞回), got %d: %s", w.Code, w.Body.String())
	}

	l := lastOperLog(t)
	if l.Module != "订单管理" || l.Action != "订单结账" {
		t.Errorf("模块/动作错误: %s / %s", l.Module, l.Action)
	}
	if l.Operator != "张老板" || l.OperatorRole != "超级管理员" || l.OperatorID != 1 {
		t.Errorf("操作人快照错误: %+v", l)
	}
	if l.TargetType != "order" || l.TargetID != "88" {
		t.Errorf("操作对象错误: %s #%s", l.TargetType, l.TargetID)
	}
	if l.Detail != "免单 ¥128.00，原因：客户投诉" {
		t.Errorf("语义摘要丢失: %q", l.Detail)
	}
	if l.Status != po.OperStatusSuccess {
		t.Errorf("成功请求应记为成功, got %d", l.Status)
	}
	if l.OperIP != "10.0.0.7" {
		t.Errorf("来源 IP 错误: %q", l.OperIP)
	}
	// 密钥进了日志就等于把支付密钥发给所有能看日志的人;
	// 打印机识别码 key 同理(配合 SN 就能把打印机绑到攻击者账号)。
	if strings.Contains(l.OperParam, "deadbeef") || strings.Contains(l.OperParam, "feie-key-456") ||
		!strings.Contains(l.OperParam, "******") {
		t.Errorf("请求参数未脱敏: %s", l.OperParam)
	}
	if !strings.Contains(l.Method, "POST /api/admin/order/settle") {
		t.Errorf("方法/路由记录错误: %q", l.Method)
	}
}

// TestAuditLogRecordsFailure 失败的操作也要记,并且带上失败原因。
//
// 「谁在什么时候试图免单但被拒」同样是审计线索;只记成功的话,
// 连续失败的操作(可能在试探系统边界)会完全消失。
func TestAuditLogRecordsFailure(t *testing.T) {
	initAuditTestDB(t)
	r := newAuditRouter(func(c *gin.Context) {
		fail(c, "免单必须填写原因")
	})

	w := doPost(r, `{"orderId":88}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("预期 400, got %d", w.Code)
	}
	l := lastOperLog(t)
	if l.Status != po.OperStatusFail {
		t.Errorf("失败请求应记为失败, got %d", l.Status)
	}
	if l.ErrorMsg != "免单必须填写原因" {
		t.Errorf("失败原因未记录: %q", l.ErrorMsg)
	}
}

// TestAuditLogSkipsGetByDefault 只读请求默认不记(量太大且无变更)。
func TestAuditLogSkipsGetByDefault(t *testing.T) {
	initAuditTestDB(t)
	r := newAuditRouter(func(c *gin.Context) { okMsg(c, "ok") })
	r.GET("/api/admin/order/list", func(c *gin.Context) { okMsg(c, "ok") })

	before := countOperLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/order/list", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	if got := countOperLogs(t); got != before {
		t.Errorf("默认不应记录 GET 请求, 日志从 %d 条变成 %d 条", before, got)
	}
}

// TestAuditLogDisabled 关闭开关后完全不落库(给不需要审计的部署留退路)。
func TestAuditLogDisabled(t *testing.T) {
	t.Setenv("AUDIT_LOG_ENABLED", "0")
	initAuditTestDB(t)
	r := newAuditRouter(func(c *gin.Context) { okMsg(c, "ok") })

	before := countOperLogs(t)
	doPost(r, `{"orderId":88}`)
	if got := countOperLogs(t); got != before {
		t.Errorf("关闭后不应写入日志, 日志从 %d 条变成 %d 条", before, got)
	}
}

func countOperLogs(t *testing.T) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_oper_log`).Scan(&n); err != nil {
		t.Fatalf("统计操作日志失败: %v", err)
	}
	return n
}
