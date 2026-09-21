package handler

import (
	"dining-system/internal/logger"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/store"
)

// ============================================================================
// 接口级权限校验
//
// 采用「集中式路由 → 权限映射表 + 一个中间件」,而不是给 40 多个路由逐个挂中间件:
//   - 一屏即可审计全部接口的权限要求;
//   - 可以写覆盖率测试(遍历 gin 的 r.Routes() 断言每条路由都已登记),
//     新增路由忘登记权限会直接测试失败;
//   - fail-closed:未登记的路由一律 403 并打日志,而不是默认放行。
//
// 状态码约定(对「业务错误一律 400」的有意例外,鉴权属传输层语义):
//   401 未登录 / 令牌失效 → 前端清登录态并跳登录页
//   403 已登录但无权限   → 前端弹「没有操作权限」,不跳转
// ============================================================================

// PermFree 表示「登录即可,无需权限点」的已登记路由(显式登记,避免见谁都放行)。
const PermFree = ""

// routePerms 路由 → 权限码映射。
//
// key 为「HTTP 方法 + 空格 + 路由模板」,与 gin 的 c.FullPath() 严格一致
// (路由模板含 :id 之类的参数占位符,而非真实请求路径)。
var routePerms = map[string]string{
	// ---- 登录者自身(无需权限点) ----
	"POST /prod-api/dining/auth/password": PermFree,
	"GET /prod-api/dining/auth/profile":   PermFree,

	// ---- 员工管理 ----
	"GET /prod-api/dining/user/list":           "user:view",
	"POST /prod-api/dining/user/save":          "user:edit",
	"POST /prod-api/dining/user/update":        "user:edit",
	"POST /prod-api/dining/user/resetPassword": "user:edit",
	"POST /prod-api/dining/user/toggleStatus":  "user:edit",
	"DELETE /prod-api/dining/user/:id":         "user:edit",

	// ---- 角色与权限 ----
	"GET /prod-api/dining/role/list":    "role:view",
	"POST /prod-api/dining/role/save":   "role:edit",
	"POST /prod-api/dining/role/update": "role:edit",
	"DELETE /prod-api/dining/role/:id":  "role:edit",
	"GET /prod-api/dining/perm/catalog": "role:view",

	// ---- 桌台 ----
	"GET /prod-api/dining/table/list":    "table:view",
	"POST /prod-api/dining/table/save":   "table:edit",
	"POST /prod-api/dining/table/update": "table:edit",
	"DELETE /prod-api/dining/table/:id":  "table:edit",

	// ---- 分类 ----
	"GET /prod-api/dining/category/list":    "category:view",
	"POST /prod-api/dining/category/save":   "category:edit",
	"POST /prod-api/dining/category/update": "category:edit",
	"DELETE /prod-api/dining/category/:id":  "category:edit",

	// ---- 菜品 ----
	"GET /prod-api/dining/dish/list":    "dish:view",
	"GET /prod-api/dining/dish/:id":     "dish:view",
	"POST /prod-api/dining/dish/save":   "dish:edit",
	"POST /prod-api/dining/dish/update": "dish:edit",
	"DELETE /prod-api/dining/dish/:id":  "dish:edit",

	// ---- 备注 ----
	"GET /prod-api/dining/remark/list":    "remark:view",
	"POST /prod-api/dining/remark/save":   "remark:edit",
	"POST /prod-api/dining/remark/update": "remark:edit",
	"DELETE /prod-api/dining/remark/:id":  "remark:edit",

	// ---- 打印机 ----
	"GET /prod-api/dining/printer/list":      "printer:view",
	"POST /prod-api/dining/printer/save":     "printer:edit",
	"POST /prod-api/dining/printer/update":   "printer:edit",
	"DELETE /prod-api/dining/printer/:id":    "printer:edit",
	"POST /prod-api/dining/printer/test/:id": "printer:edit",

	// ---- 打印机(探测 / 绑定 / 云打印) ----
	// 「查看类」给 printer:view;「会触发打印或改动打印机状态」的动作一律给 printer:edit。
	"GET /prod-api/dining/printer/status/:id": "printer:view",
	"GET /prod-api/dining/printer/feie/info":  "printer:view",
	"POST /prod-api/dining/printer/probe/:id": "printer:edit",
	"POST /prod-api/dining/printer/bind":      "printer:edit",
	"POST /prod-api/dining/printer/clear/:id": "printer:edit",

	// ---- 打印记录与补打 ----
	// 补打会真的出纸(可能产生耗材成本),因此不归入只读的 printer:view。
	"GET /prod-api/dining/print/log/list":     "printer:view",
	"POST /prod-api/dining/print/log/reprint": "printer:edit",
	"POST /prod-api/dining/order/reprint":     "printer:edit",

	// ---- 系统配置 ----
	"GET /prod-api/dining/config/list":  "config:view",
	"POST /prod-api/dining/config/save": "config:edit",

	// ---- 订单 ----
	"GET /prod-api/dining/order/list":           "order:view",
	"GET /prod-api/dining/order/board":          "order:view",
	"GET /prod-api/dining/order/urge/list":      "order:view",
	"GET /prod-api/dining/order/:id":            "order:view",
	"POST /prod-api/dining/order/status":        "order:operate",
	"POST /prod-api/dining/order/finish":        "order:operate",
	"POST /prod-api/dining/order/urge/handle":   "order:operate",
	"POST /prod-api/dining/order/pay":           "order:settle",
	"POST /prod-api/dining/order/settle":        "order:settle",
	"POST /prod-api/dining/order/settle/cancel": "order:settle",
	"POST /prod-api/dining/order/credit/settle": "credit:settle",
	"POST /prod-api/dining/order/edit":          "order:edit",
	"POST /prod-api/dining/order/cancel":        "order:cancel",

	// ---- 退款 ----
	"POST /prod-api/dining/pay/refund":       "refund:operate",
	"POST /prod-api/dining/pay/refund/query": "refund:operate",
	"GET /prod-api/dining/pay/refund/list":   "refund:view",

	// ---- 报表 ----
	"GET /prod-api/dining/report/summary":      "report:view",
	"GET /prod-api/dining/report/dailyTrend":   "report:view",
	"GET /prod-api/dining/report/monthlyTrend": "report:view",
	"GET /prod-api/dining/report/dishRank":     "report:view",

	// ---- 操作日志(审计) ----
	// 查看只需 log:view;清理会真的删数据,单独收口到 log:manage。
	"GET /prod-api/dining/log/list":   "log:view",
	"POST /prod-api/dining/log/clean": "log:manage",
}

// menuOnlyPerms 声明「不直接对应任何后端路由、只用于管理端菜单/入口显隐」的权限码。
//
// 用途:让「每个权限码都必须有归属」成为可测试的不变式(见 routes_test.go)。
// 没有这份显式声明时,漏挂权限与「有意做成菜单级」在代码上无法区分 ——
// `credit:view` 就是这样被漏掉的:它在权限目录里存在、能出现在角色勾选框里,
// 但没有任何一条路由引用它。
//
// 目前唯一的成员是 `credit:view`:
//
//	挂账页展示的其实就是「settle_type='credit' 的订单」,数据来自
//	`GET /prod-api/dining/order/list`(需要 `order:view`)。因此 `credit:view`
//	只决定左侧菜单里「挂账管理」是否出现,真正的数据读取仍由 `order:view` 把守。
//
// ⚠️ 由此带来一个约束:单纯授予 `credit:view` 而不给 `order:view`,用户会看到
//
//	「挂账管理」菜单却在进页时被 403。内置 4 个角色都同时持有这两个权限,
//	不会触发;自定义角色需要注意(设计文档 4.3 已记这一条)。
var menuOnlyPerms = map[string]bool{
	"credit:view": true,
}

// IsMenuOnlyPerm 判断某权限码是否只做菜单/入口显隐(不参与后端接口校验)。
func IsMenuOnlyPerm(code string) bool { return menuOnlyPerms[code] }

// MenuOnlyPerms 返回全部「菜单级」权限码,供测试与文档核对。
func MenuOnlyPerms() []string {
	out := make([]string, 0, len(menuOnlyPerms))
	for code := range menuOnlyPerms {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// PermForRoute 查询某路由所需的权限码,第二个返回值表示该路由是否已登记。
// 供权限中间件与「路由覆盖率测试」共用。
func PermForRoute(method, fullPath string) (string, bool) {
	perm, ok := routePerms[method+" "+fullPath]
	return perm, ok
}

// RegisteredRoutePermKeys 返回权限表里登记的全部键(形如 "GET /prod-api/dining/table/list")。
// 供测试做反向校验:权限表里的条目必须在真实路由树中存在,避免删接口后条目腐化,
// 也避免路径拼错时覆盖率恰好「通过」(把权限挂到了一条不存在的路由上)。
func RegisteredRoutePermKeys() []string {
	keys := make([]string, 0, len(routePerms))
	for k := range routePerms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AdminRoutePrefix 管理端路由前缀(覆盖率测试据此筛选需要登记权限的路由)。
const AdminRoutePrefix = "/prod-api/dining"

// RequirePerm 权限校验中间件,必须挂在 AdminAuth 之后。
//
// 未登记的路由:记录日志并拒绝(fail-closed)—— 新增接口时忘记登记权限会被测试拦下,
// 万一漏网也不会变成「默认可访问」。
func RequirePerm() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Request.Method + " " + c.FullPath()
		perm, ok := PermForRoute(c.Request.Method, c.FullPath())
		if !ok {
			logger.Warnf("[perm] 路由未登记权限,已拒绝访问: %s", key)
			forbidden(c, "没有操作权限")
			return
		}
		if perm != PermFree && !HasPerm(c, perm) {
			forbidden(c, "没有操作权限:"+store.PermName(perm))
			return
		}
		c.Next()
	}
}

// ============================================================================
// 上下文工具
// ============================================================================

const ctxAuthKey = "dining.auth"

// setAuth 由 AdminAuth 写入当前登录者信息。
func setAuth(c *gin.Context, auth *store.AuthInfo) {
	c.Set(ctxAuthKey, auth)
}

// currentAuth 取当前登录者信息(未登录返回 nil)。
func currentAuth(c *gin.Context) *store.AuthInfo {
	if v, ok := c.Get(ctxAuthKey); ok {
		if a, ok := v.(*store.AuthInfo); ok {
			return a
		}
	}
	return nil
}

// currentUID 当前登录者 ID(未登录返回 0)。
func currentUID(c *gin.Context) int {
	if a := currentAuth(c); a != nil {
		return a.UserID
	}
	return 0
}

// HasPerm 判断当前登录者是否拥有指定权限点。
func HasPerm(c *gin.Context, code string) bool {
	a := currentAuth(c)
	if a == nil {
		return false
	}
	return store.HasPermCode(a.Perms, code)
}

// adminName 当前操作人的留痕显示名:优先中文姓名,未填则回退登录名。
//
// 订单 / 催菜 / 退款等表的 create_by / update_by / settle_operator / handle_by
// 都写这个值。实现放在这里统一提供,业务 handler 直接调用。
func adminName(c *gin.Context) string {
	if a := currentAuth(c); a != nil {
		return a.DisplayName()
	}
	// 兜底:上下文缺失(理论上不会发生,所有调用点都在 AdminAuth 之后)
	// 时退回从令牌载荷解析,保证留痕不丢。
	return usernameFromToken(c)
}

// usernameFromToken 从 Bearer 令牌的载荷第一段取用户名(兜底路径)。
// 载荷格式: 用户名|uid|token_version|过期时间(见 auth.go)。
func usernameFromToken(c *gin.Context) string {
	tok := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	fields := strings.SplitN(string(payload), "|", 2)
	if len(fields) != 2 {
		return ""
	}
	return fields[0]
}

// ============================================================================
// 响应
// ============================================================================

// unauthorized 未登录 / 登录态失效。前端拦截器据此清登录态并跳登录页。
//
// 顺带留一条审计:「谁在用失效令牌访问」同样是安全线索。
// 匿名请求(无令牌)不记,免得日志被端口扫描灌满。
func unauthorized(c *gin.Context, msg string) {
	writeDeniedLog(c, "未登录访问被拒绝", msg)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": msg})
}

// forbidden 已登录但无权限(或角色失效)。前端只弹提示,不跳转。
//
// 越权尝试是审计里最有价值的一类:它说明有人碰了不该碰的按钮,
// 而这类请求在 AuditLog 之前就被 Abort,中间件看不到,因此在此埋点。
func forbidden(c *gin.Context, msg string) {
	writeDeniedLog(c, "无权限访问被拒绝", msg)
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": msg})
}
