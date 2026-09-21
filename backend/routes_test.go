package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/handler"
	"dining-system/internal/store"
)

// TestAdminRoutePermCoverage 管理端路由权限覆盖率测试。
//
// 这是本次权限改造最关键的一道防线:handler.routePerms 是「集中式路由→权限映射表」,
// 中间件对未登记的路由是**拒绝访问**(fail-closed)。也就是说,新增一个管理端接口
// 却忘了在权限表里登记,表现不是「默认可访问」而是「线上直接 403」——
// 必须由测试在提交前拦住,而不是等用户点出来。
//
// 做法:复用生产代码的 setupAPI() 注册路由(而不是在测试里另抄一份路由表,
// 否则两边会各自漂移,断言就失去意义),再遍历 gin 的路由树逐条核对。
func TestAdminRoutePermCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	routes := r.Routes()
	if len(routes) == 0 {
		t.Fatal("未注册到任何路由,setupAPI 可能未生效")
	}

	adminCount, missing := 0, []string{}
	for _, rt := range routes {
		if !strings.HasPrefix(rt.Path, handler.AdminRoutePrefix) {
			continue
		}
		adminCount++
		if _, ok := handler.PermForRoute(rt.Method, rt.Path); !ok {
			missing = append(missing, rt.Method+" "+rt.Path)
		}
	}

	if adminCount == 0 {
		t.Fatalf("未发现 %s 下的管理端路由,断言无效", handler.AdminRoutePrefix)
	}
	if len(missing) > 0 {
		t.Errorf("以下管理端路由未在 handler.routePerms 中登记权限(会被 fail-closed 中间件拒绝):\n  %s\n"+
			"请在 internal/handler/perm.go 的 routePerms 中补上对应权限码;"+
			"「登录即可、无权限点」的路由请显式登记为 handler.PermFree。",
			strings.Join(missing, "\n  "))
	}
	t.Logf("已覆盖 %d 条管理端路由的权限登记", adminCount)
}

// TestRoutePermsHaveNoDeadEntries 反向校验:权限表里的条目必须真实存在于路由树。
//
// 防止「删了接口但忘了删权限表条目」导致权限表逐渐腐化,
// 也避免拼错路径时覆盖率测试恰好通过(把权限挂到了一条不存在的路由上)。
func TestRoutePermsHaveNoDeadEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	alive := map[string]bool{}
	for _, rt := range r.Routes() {
		alive[rt.Method+" "+rt.Path] = true
	}

	dead := []string{}
	for _, key := range handler.RegisteredRoutePermKeys() {
		if !alive[key] {
			dead = append(dead, key)
		}
	}
	if len(dead) > 0 {
		t.Errorf("handler.routePerms 中存在路由树里不存在的条目(接口已删或路径拼错):\n  %s",
			strings.Join(dead, "\n  "))
	}
}

// TestAdminWriteRoutesHaveAuditMeta 管理端「写操作」的审计覆盖率测试。
//
// 审计中间件是 fail-open 的(未登记就跳过记录,绝不能因为记日志拖垮业务),
// 这与权限中间件 fail-closed 相反 —— 于是「新增写接口忘了登记审计」的表现
// 不是报错,而是**静默不记录**,等到真出事才发现查不到人。
// 因此必须由测试在提交前拦住,和权限覆盖率测试同一套思路。
func TestAdminWriteRoutesHaveAuditMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	const auditPrefix = "/prod-api/dining"
	missing := []string{}
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, auditPrefix) {
			continue
		}
		switch rt.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue // GET 默认不记,由 AUDIT_LOG_GET 控制
		}
		if _, ok := handler.AuditMetaForRoute(rt.Method, rt.Path); !ok {
			missing = append(missing, rt.Method+" "+rt.Path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("以下管理端写操作路由未在 handler.routeAudit 中登记审计信息(将不会被记录):\n  %s\n"+
			"请在 internal/handler/audit.go 的 routeAudit 中补上模块/动作/操作类型。",
			strings.Join(missing, "\n  "))
	}
}

// TestAuditMetasHaveNoDeadEntries 反向校验:审计表里的条目必须真实存在于路由树。
// 防止删接口后条目腐化,也避免路径拼错时覆盖率恰好「通过」。
func TestAuditMetasHaveNoDeadEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	alive := map[string]bool{}
	for _, rt := range r.Routes() {
		alive[rt.Method+" "+rt.Path] = true
	}
	dead := []string{}
	for _, key := range handler.RegisteredAuditKeys() {
		if !alive[key] {
			dead = append(dead, key)
		}
	}
	if len(dead) > 0 {
		t.Errorf("handler.routeAudit 中存在路由树里不存在的条目(接口已删或路径拼错):\n  %s",
			strings.Join(dead, "\n  "))
	}
}

// TestCustomerRoutesStayPublic 回归确认权限改造没有误伤顾客端与登录接口。
//
// 顾客端必须是零鉴权的(扫码即用),登录接口也必须公开 —— 这些路由一旦被
// 误挂上 AdminAuth/RequirePerm,整个点餐流程会直接不可用。
func TestCustomerRoutesStayPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	mustExist := []string{
		"POST /prod-api/auth/login",
		"GET /prod-api/api/dining/menu",
		"GET /prod-api/api/dining/table/:id",
		"POST /prod-api/api/dining/order",
		"POST /prod-api/api/dining/order/append",
		"GET /prod-api/api/dining/order/no/:orderNo",
		"POST /prod-api/api/dining/order/urge",
		"GET /prod-api/api/dining/config",
		"GET /prod-api/api/dining/remarks",
		"GET /prod-api/api/dining/pay/qr",
		"POST /prod-api/api/dining/pay/create",
		"POST /prod-api/api/dining/pay/notify/wxpay",
		"POST /prod-api/api/dining/pay/notify/alipay",
		"GET /prod-api/api/dining/pay/query",
	}
	alive := map[string]bool{}
	for _, rt := range r.Routes() {
		alive[rt.Method+" "+rt.Path] = true
	}
	for _, key := range mustExist {
		if !alive[key] {
			t.Errorf("公开路由缺失: %s", key)
		}
		if _, registered := handler.PermForRoute(strings.SplitN(key, " ", 2)[0], strings.SplitN(key, " ", 2)[1]); registered {
			t.Errorf("公开路由 %s 不应出现在管理端权限表中(会被要求登录)", key)
		}
	}
}

// TestEveryPermCodeIsUsedOrDeclaredMenuOnly 每个权限码都必须「有归属」。
//
// 前两个测试分别保证「路由都登记了权限」与「权限表的条目都真实存在」,
// 但都管不到第三种腐化:某个权限码**从未被任何路由引用**。
// `credit:view` 就是这么漏掉的 —— 它在权限目录里、能在角色勾选框里被勾上,
// 却没有任何一条路由引用它,于是「授予挂账查看」在后端等于什么都没发生。
//
// 断言:目录里的每个权限码,要么被至少一条已登记路由引用,要么在
// handler.menuOnlyPerms 里显式声明为「只做菜单显隐」。后者是一份有意为之的
// 清单,新增成员要写清理由,不能默默塞进去。
func TestEveryPermCodeIsUsedOrDeclaredMenuOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupAPI(r)

	used := map[string]bool{}
	for _, rt := range r.Routes() {
		perm, ok := handler.PermForRoute(rt.Method, rt.Path)
		if ok && perm != handler.PermFree {
			used[perm] = true
		}
	}

	menuOnly := map[string]bool{}
	for _, code := range handler.MenuOnlyPerms() {
		menuOnly[code] = true
		if !store.IsPermCode(code) {
			t.Errorf("handler.menuOnlyPerms 里的 %q 不是合法权限码(拼错了?)", code)
		}
		if used[code] {
			t.Errorf("%q 已被路由引用,不应再声明为菜单级(menuOnlyPerms 里这项多余)", code)
		}
	}

	orphan := []string{}
	for _, code := range store.AllPermCodes() {
		if !used[code] && !menuOnly[code] {
			orphan = append(orphan, code)
		}
	}
	if len(orphan) > 0 {
		t.Errorf("以下权限码未被任何管理端路由引用,也没声明为菜单级:\n  %s\n"+
			"→ 若该权限本就该校验接口,请在 perm.go 的 routePerms 里挂上它;\n"+
			"→ 若它只用于菜单/入口显隐,请加入 handler.menuOnlyPerms 并写明理由。",
			strings.Join(orphan, "\n  "))
	}

	t.Logf("权限码归属核对通过:路由引用 %d 个、菜单级 %d 个、目录共 %d 个",
		len(used), len(menuOnly), len(store.AllPermCodes()))
}
