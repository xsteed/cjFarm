package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/handler"
	"dining-system/internal/logger"
	"dining-system/internal/print"
	"dining-system/internal/store"
)

// allowedOrigins 解析 CORS_ORIGINS 环境变量(逗号分隔)得到跨域白名单。
// 未配置时不放开任何跨域——本项目前后端同源(经 Nginx 反代),生产无需跨域;
// 仅当 H5 点餐页单独部署在其他域名时,才需要配置具体 origin。
var allowedOrigins = loadAllowedOrigins()

func loadAllowedOrigins() map[string]bool {
	m := map[string]bool{}
	for _, o := range strings.Split(store.Getenv("CORS_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			m[o] = true
		}
	}
	return m
}

// loadTrustedProxies 解析 TRUSTED_PROXIES 环境变量(逗号分隔的 IP/CIDR)作为可信代理列表。
// 默认返回 nil(不信任任何代理),此时 gin 的 ClientIP() 取 RemoteAddr,可防止客户端伪造 XFF。
// 经 nginx 反代部署时,需设置 TRUSTED_PROXIES(如 127.0.0.1 或 nginx 的 IP),
// 登录限流才能取到真实客户端 IP(否则所有请求共享 nginx 来源,退化为全局粒度)。
func loadTrustedProxies() []string {
	var out []string
	for _, p := range strings.Split(store.Getenv("TRUSTED_PROXIES", ""), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// corsMiddleware:仅对白名单内的 Origin 放开跨域;不携带凭据,降低 CSRF 面。
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// 构建信息,通过 -ldflags "-X main.version=... -X main.buildTime=... -X main.gitCommit=..." 注入。
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

// setupAPI 注册全部 API 路由。
//
// 抽成独立函数是为了让「管理端路由权限覆盖率测试」复用同一份注册逻辑:
// 测试遍历 gin 的 r.Routes() 断言每条 /prod-api/dining/* 路由都已在
// handler.routePerms 中登记权限,新增接口忘登记会直接测试失败。
// 若路由注册散落在 main() 里,测试就得另抄一份路由表,覆盖率断言随即失去意义。
//
// 管理端中间件链: AdminAuth(401 身份) → RequirePerm(403 权限)。
func setupAPI(r *gin.Engine) {
	prod := r.Group("/prod-api")
	{
		// ---- 登录(公开) ----
		prod.POST("/auth/login", handler.AdminLogin)
		// 记住我:用本地存储的令牌静默换发新登录态(免登录 7/30 天),过期由后端查库裁决。
		prod.POST("/auth/remember-login", handler.RememberLogin)

		// ---- 顾客端(公开) ----
		prod.GET("/api/dining/table/:id", handler.CustomerTable)
		prod.GET("/api/dining/menu", handler.CustomerMenu)
		prod.GET("/api/dining/remarks", handler.CustomerRemarks)
		prod.GET("/api/dining/config", handler.CustomerConfig)
		prod.POST("/api/dining/order", handler.CustomerCreateOrder)
		prod.POST("/api/dining/order/append", handler.CustomerAppendOrder)
		prod.GET("/api/dining/order/no/:orderNo", handler.CustomerOrderByNo)
		prod.POST("/api/dining/order/urge", handler.CustomerUrgeOrder)
		prod.GET("/api/dining/pay/qr", handler.CustomerPayQr)
		prod.POST("/api/dining/pay/create", handler.PayCreate)
		prod.POST("/api/dining/pay/notify/wxpay", handler.PayNotifyWxpay)
		prod.POST("/api/dining/pay/notify/alipay", handler.PayNotifyAlipay)
		prod.GET("/api/dining/pay/query", handler.PayQuery)

		// ---- 本地打印代理(门店内网程序调用,令牌鉴权) ----
		// 不走 AdminAuth:调用方是门店里常驻的代理程序(不是浏览器会话),
		// 它出站轮询云端取单,再向门店内网 打印机IP:9100 直发。见 docs/print-agent.md。
		prod.POST("/agent/print/pull", handler.AgentPull)
		prod.POST("/agent/print/ack", handler.AgentAck)
		// ping 也用 POST:三个接口统一一种方法,代理程序与自研实现都不必区分动词。
		prod.POST("/agent/print/ping", handler.AgentPing)

		// ---- 管理端(需登录 + 按路由校验权限) ----
		// AuditLog 挂在最后:它要等 AdminAuth 拿到操作人身份、等 handler 跑完
		// 才知道成败,因此不能在最前面;写日志失败也不影响业务(旁路数据)。
		admin := prod.Group("/dining", handler.AdminAuth, handler.RequirePerm(), handler.AuditLog())
		{
			// 登录者自身(无需权限点)
			admin.POST("/auth/password", handler.ChangePassword)
			admin.GET("/auth/profile", handler.Profile)

			// 员工管理
			admin.GET("/user/list", handler.UserList)
			admin.POST("/user/save", handler.UserSave)
			admin.POST("/user/update", handler.UserUpdate)
			admin.POST("/user/resetPassword", handler.UserResetPassword)
			admin.POST("/user/toggleStatus", handler.UserToggleStatus)
			admin.DELETE("/user/:id", handler.UserDelete)

			// 角色与权限
			admin.GET("/role/list", handler.RoleList)
			admin.POST("/role/save", handler.RoleSave)
			admin.POST("/role/update", handler.RoleUpdate)
			admin.DELETE("/role/:id", handler.RoleDelete)
			admin.GET("/perm/catalog", handler.PermCatalog)

			admin.GET("/table/list", handler.TableList)
			admin.POST("/table/save", handler.TableSave)
			admin.POST("/table/update", handler.TableUpdate)
			admin.DELETE("/table/:id", handler.TableDelete)

			admin.GET("/category/list", handler.CategoryList)
			admin.POST("/category/save", handler.CategorySave)
			admin.POST("/category/update", handler.CategoryUpdate)
			admin.DELETE("/category/:id", handler.CategoryDelete)

			admin.GET("/dish/list", handler.DishList)
			admin.GET("/dish/:id", handler.DishGet)
			admin.POST("/dish/save", handler.DishSave)
			admin.POST("/dish/update", handler.DishUpdate)
			admin.DELETE("/dish/:id", handler.DishDelete)

			admin.GET("/remark/list", handler.RemarkList)
			admin.POST("/remark/save", handler.RemarkSave)
			admin.POST("/remark/update", handler.RemarkUpdate)
			admin.DELETE("/remark/:id", handler.RemarkDelete)

			admin.GET("/printer/list", handler.PrinterList)
			admin.POST("/printer/save", handler.PrinterSave)
			admin.POST("/printer/update", handler.PrinterUpdate)
			admin.DELETE("/printer/:id", handler.PrinterDelete)
			admin.POST("/printer/test/:id", handler.PrinterTest)
			// 只测连通性不吐纸 / 查实时状态 / 绑定到飞鹅账号 / 清空飞鹅云端队列
			admin.POST("/printer/probe/:id", handler.PrinterProbe)
			admin.GET("/printer/status/:id", handler.PrinterStatus)
			admin.POST("/printer/bind", handler.PrinterBind)
			admin.POST("/printer/clear/:id", handler.PrinterClear)
			admin.GET("/printer/feie/info", handler.FeieInfo)
			// 本地打印代理概况(令牌是否已配、代理是否在线、队列积压)
			admin.GET("/printer/agent/info", handler.AgentInfo)

			// 打印日志(排查「小票没出来」)与人工补打
			admin.GET("/print/log/list", handler.PrintLogList)
			admin.POST("/print/log/reprint", handler.PrintLogReprint)
			admin.POST("/order/reprint", handler.OrderReprint)

			admin.GET("/config/list", handler.ConfigList)
			admin.POST("/config/save", handler.ConfigSave)

			admin.GET("/order/list", handler.OrderList)
			admin.GET("/order/board", handler.OrderBoard)
			admin.GET("/order/urge/list", handler.UrgeList)
			admin.POST("/order/urge/handle", handler.UrgeHandle)
			admin.GET("/order/:id", handler.OrderGet)
			admin.POST("/order/status", handler.OrderStatus)
			admin.POST("/order/pay", handler.OrderPay)
			admin.POST("/order/settle", handler.OrderSettle)
			admin.POST("/order/credit/settle", handler.OrderCreditSettle)
			admin.POST("/order/settle/cancel", handler.OrderSettleCancel)
			admin.POST("/order/finish", handler.OrderFinish)
			admin.POST("/order/cancel", handler.OrderCancel)
			admin.POST("/order/edit", handler.OrderEdit)

			admin.POST("/pay/refund", handler.PayRefund)
			admin.POST("/pay/refund/query", handler.PayRefundQuery)
			admin.GET("/pay/refund/list", handler.PayRefundList)

			admin.GET("/report/summary", handler.ReportSummary)
			admin.GET("/report/dailyTrend", handler.ReportDailyTrend)
			admin.GET("/report/monthlyTrend", handler.ReportMonthlyTrend)
			admin.GET("/report/dishRank", handler.ReportDishRank)
			// 时段分布(排班/备货参考)与结算方式构成
			admin.GET("/report/hourly", handler.ReportHourly)
			admin.GET("/report/settleMix", handler.ReportSettleMix)

			// ---- 操作日志(审计留痕) ----
			admin.GET("/log/list", handler.LogList)
			admin.POST("/log/clean", handler.LogClean)
		}

		// ---- 上传(需登录) ----
		// 仅要求登录、不额外收紧:图片上传目前只服务于菜品管理,
		// 而菜品写操作本身已受 dish:edit 约束。
		prod.POST("/common/upload", handler.AdminAuth, handler.Upload)
	}
}

func main() {
	// 日志最先初始化:后续启动日志统一走 zap(控制台可读 + 文件 JSON 可采集)。
	logger.Init()
	defer logger.Sync()

	gin.SetMode(gin.ReleaseMode)

	// 装配部署配置,优先级:真实环境变量 > config.yaml > .env > 代码默认值。
	// config.yaml 解析失败(键名拼错 / 缩进错)会让启动直接失败 —— 静默跑起来
	// 更难排查;其它来源缺失都属正常,单机 SQLite 零配置即可启动。
	cfgStats, err := store.LoadDeployConfig()
	if err != nil {
		logger.Fatalf("[config] %v", err)
	}
	if cfgStats.YAMLFound {
		logger.Infof("[config] 已从 %s 载入 %d 项配置(其中 %d 项覆盖了 .env 同键)",
			cfgStats.YAMLPath, cfgStats.YAMLCount, cfgStats.YAMLOverride)
	}
	if cfgStats.DotEnvCount > 0 {
		logger.Infof("[config] 已从 %s 载入 %d 项配置(未覆盖更高优先级的来源)", cfgStats.DotEnvPath, cfgStats.DotEnvCount)
	}
	// 部署配置(config.yaml/.env)此时已写入环境变量,重建日志以生效其中的
	// LOG_LEVEL / LOG_PATH 等设置;此前的启动日志已按默认配置输出(默认同样落盘到 ./logs/app.log)。
	logger.Init()

	// 数据库后端由环境变量决定:默认 SQLite(DB_PATH),设置 DB_DRIVER=mysql
	// 或提供 DB_DSN 即切换到 MySQL。业务代码与 SQL 均为两库通吃,无需改动。
	store.InitFromEnv()
	// 加载/生成本机主密钥,并把历史明文敏感配置迁移为密文(须在建库之后)。
	store.InitSecret()
	if !store.SecretsEncrypted() {
		logger.Warnf("[warn] 敏感配置未加密存储,建议设置 CONFIG_MASTER_KEY 或允许自动生成本机主密钥")
	}
	// 按保留天数清理过期操作日志(AUDIT_RETENTION_DAYS,默认 365 天,0=不清理)。
	if n := store.CleanExpiredOperLogs(); n > 0 {
		logger.Infof("[audit] 已清理 %d 条过期操作日志", n)
	}
	// 后台定时清理已结案的本地打印代理任务(AGENT_JOB_RETENTION_DAYS,默认 7 天)。
	// 队列是「待办」,结案后没有长期保留价值;翻账看的是 tb_print_log,不受影响。
	print.StartAgentCleanup()

	// 上传目录与前端静态目录均支持环境变量覆盖,便于打包部署到不同工作目录。
	uploadDir := store.Getenv("UPLOAD_DIR", "./uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		logger.Fatalf("创建上传目录失败: %v", err)
	}
	// 启动审计:校验菜品图片引用与磁盘文件一一对应,缺失即 [warn](回答「不能少东西」)。
	store.AuditDishImages(uploadDir)
	staticDir := store.Getenv("STATIC_DIR", "../frontend/dist")

	r := gin.New()
	r.Use(logger.GinLogger(), logger.GinRecovery(), corsMiddleware())
	if err := r.SetTrustedProxies(loadTrustedProxies()); err != nil {
		logger.Fatalf("配置可信代理失败: %v", err)
	}
	r.MaxMultipartMemory = 8 << 20 // 8MB 内存缓存,超限落盘

	// 静态资源:上传文件。
	// 用自定义 handler 替代 r.Static:文件缺失时记录 [warn],避免「资源丢了却静默 404」。
	// 此前收款码、菜品图均出现过「文件在、配置空」或「引用在、磁盘无」的情况。
	serveUpload := func(c *gin.Context) {
		name := filepath.Base(c.Request.URL.Path) // 防目录穿越
		p := filepath.Join(uploadDir, name)
		if _, err := os.Stat(p); err != nil {
			logger.Warnf("[static][告警] 缺失资源 %s (来自 %s)", c.Request.URL.Path, c.ClientIP())
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "资源不存在"})
			return
		}
		c.File(p)
	}
	// 图片/收款码只有 /uploads/ 一个前缀(历史 /picture/ 已由 migrateUploadPrefix 改写),
	// 避免同一目录挂两个前缀造成「代码里写哪个才对」的歧义。
	r.GET("/uploads/*file", serveUpload)

	// 后端 API 前缀
	setupAPI(r)

	// 前端静态资源(若已构建)。Vite 默认把产物放到 dist/assets,但这里统一约定
	// assetsDir 为 "static"(见 frontend/vite.config.js),因此只需托管 /static 即可。
	// 同时兼容 /assets 以覆盖未改配置的旧构建产物。
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		r.Static("/static", filepath.Join(staticDir, "static"))
		r.Static("/assets", filepath.Join(staticDir, "assets"))
		// 站点图标(vite public 目录产物)。
		// 注意:必须逐个登记 —— 未登记的图标路径会被下面 NoRoute 的 SPA 兜底吃掉,
		// 浏览器拿到的是 index.html(前端 index.html 里声明了多个尺寸的 favicon)。
		r.StaticFile("/favicon.ico", filepath.Join(staticDir, "favicon.ico"))
		r.StaticFile("/favicon.svg", filepath.Join(staticDir, "favicon.svg"))
		r.StaticFile("/favicon-16x16.png", filepath.Join(staticDir, "favicon-16x16.png"))
		r.StaticFile("/favicon-32x32.png", filepath.Join(staticDir, "favicon-32x32.png"))
		r.StaticFile("/favicon-48x48.png", filepath.Join(staticDir, "favicon-48x48.png"))
		r.StaticFile("/favicon-192x192.png", filepath.Join(staticDir, "favicon-192x192.png"))
		r.StaticFile("/favicon-512x512.png", filepath.Join(staticDir, "favicon-512x512.png"))
		r.StaticFile("/apple-touch-icon.png", filepath.Join(staticDir, "apple-touch-icon.png"))
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			// API 与静态资源路径返回 JSON 404,不落入 SPA 兜底
			if strings.HasPrefix(p, "/prod-api") || strings.HasPrefix(p, "/uploads") {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "接口不存在"})
				return
			}
			c.File(filepath.Join(staticDir, "index.html"))
		})
	} else {
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/prod-api") {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "接口不存在"})
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "前端未构建,请先 cd frontend && npm run build"})
		})
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		logger.Infof("扫码点餐管理系统已启动 v%s (build %s, commit %s): http://localhost:%s (管理端账号 %s)", version, buildTime, gitCommit, port, handler.AdminUser())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Infof("正在关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Warnf("关闭异常: %v", err)
	}
	if store.DB != nil {
		_ = store.DB.Close()
	}
	logger.Infof("服务已退出")
}
