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

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/conf"
	"dining-system/internal/handler"
	"dining-system/internal/middleware"
	"dining-system/internal/print"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// 构建信息,通过 -ldflags "-X main.version=... -X main.buildTime=... -X main.gitCommit=..." 注入。
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

// setupAPI 注册全部 API 路由。
//
// 抽成独立函数是为了让「管理端路由权限覆盖率测试」复用同一份注册逻辑:
// 测试遍历 gin 的 r.Routes() 断言每条 /api/admin/* 路由都已在
// handler.routePerms 中登记权限,新增接口忘登记会直接测试失败。
// 若路由注册散落在 main() 里,测试就得另抄一份路由表,覆盖率断言随即失去意义。
//
// 管理端中间件链: AdminAuth(401 身份) → RequirePerm(403 权限)。
func setupAPI(r *gin.Engine) {
	api := r.Group("/api")
	{
		// ---- 登录(公开) ----
		api.POST("/auth/login", handler.AdminLogin)
		// 记住我:用本地存储的令牌静默换发新登录态(免登录 7/30 天),过期由后端查库裁决。
		api.POST("/auth/remember-login", handler.RememberLogin)
		// 退出登录:吊销本设备持有的「记住我」令牌(凭令牌本身鉴权,幂等)。
		api.POST("/auth/logout", handler.Logout)

		// ---- 顾客端(公开) ----
		api.GET("/customer/table/:id", handler.CustomerTable)
		api.GET("/customer/menu", handler.CustomerMenu)
		api.GET("/customer/remarks", handler.CustomerRemarks)
		api.GET("/customer/config", handler.CustomerSetting)
		api.POST("/customer/order", handler.CustomerCreateOrder)
		api.POST("/customer/order/append", handler.CustomerAppendOrder)
		api.GET("/customer/order/no/:orderNo", handler.CustomerOrderByNo)
		api.POST("/customer/order/urge", handler.CustomerUrgeOrder)
		api.GET("/customer/pay/qr", handler.CustomerPayQr)
		api.POST("/customer/pay/create", handler.PayCreate)
		api.POST("/customer/pay/notify/wxpay", handler.PayNotifyWxpay)
		api.POST("/customer/pay/notify/alipay", handler.PayNotifyAlipay)
		api.GET("/customer/pay/query", handler.PayQuery)

		// ---- 本地打印代理(门店内网程序调用,令牌鉴权) ----
		// 不走 AdminAuth:调用方是门店里常驻的代理程序(不是浏览器会话),
		// 它出站轮询云端取单,再向门店内网 打印机IP:9100 直发。见 docs/print-agent.md。
		api.POST("/agent/print/pull", handler.AgentPull)
		api.POST("/agent/print/ack", handler.AgentAck)
		// ping 也用 POST:三个接口统一一种方法,代理程序与自研实现都不必区分动词。
		api.POST("/agent/print/ping", handler.AgentPing)
		// 自助升级:下载服务端部署的最新代理产物(响应二进制,sha256/版本在响应头)。
		api.GET("/agent/print/download", handler.AgentDownload)

		// ---- 管理端(需登录 + 按路由校验权限) ----
		// AuditLog 挂在最后:它要等 AdminAuth 拿到操作人身份、等 handler 跑完
		// 才知道成败,因此不能在最前面;写日志失败也不影响业务(旁路数据)。
		admin := api.Group("/admin", handler.AdminAuth, handler.RequirePerm(), handler.AuditLog())
		{
			// 登录者自身(无需权限点)
			admin.POST("/auth/password", handler.ChangePassword)
			admin.GET("/auth/profile", handler.Profile)
			// 记住登录会话(登录设备页):查看/吊销自己的「记住我」免登录设备。
			admin.GET("/auth/remember/sessions", handler.RememberSessions)
			admin.POST("/auth/remember/revoke", handler.RememberRevoke)

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
			admin.GET("/printer/agent/list", handler.AgentList)
			admin.POST("/printer/agent/save", handler.AgentSave)
			admin.POST("/printer/agent/status/:id", handler.AgentSetStatus)
			// 编辑授权范围 / 删除已吊销的代理身份(清理历史代理与其令牌哈希)
			admin.POST("/printer/agent/update/:id", handler.AgentUpdate)
			admin.DELETE("/printer/agent/:id", handler.AgentDelete)

			// 打印日志(排查「小票没出来」)与人工补打
			admin.GET("/print/log/list", handler.PrintLogList)
			admin.GET("/print/preview", handler.PrintPreview)
			admin.POST("/print/preview/sample", handler.PrintPreviewSample)
			admin.POST("/print/log/reprint", handler.PrintLogReprint)
			admin.POST("/order/reprint", handler.OrderReprint)

			admin.GET("/config/list", handler.SettingList)
			admin.POST("/config/save", handler.SettingSave)
			// 打印代理全局令牌:后端生成即落库(不随整份配置保存),明文只回显一次。
			admin.POST("/config/agent/token", handler.ConfigAgentTokenIssue)

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

		// ---- 上传(需登录 + 任一相关模块权限) ----
		// 图片上传的产物同时服务两个模块:菜品图(dish:edit)与店铺 Logo/收款码
		// (config:edit),因此校验「任一命中即放行」;店长/超管天然可用,
		// 收银员/员工(无这两个写权限)不需要也不应上传文件。
		api.POST("/common/upload", handler.AdminAuth, handler.RequireAnyPerm("dish:edit", "config:edit"), handler.Upload)
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
	deployStats, err := infra.LoadDeployConfig()
	if err != nil {
		logger.Fatalf("[deploy] %v", err)
	}
	if deployStats.YAMLFound {
		logger.Infof("[deploy] 已从 %s 载入 %d 项配置(其中 %d 项覆盖了 .env 同键)",
			deployStats.YAMLPath, deployStats.YAMLCount, deployStats.YAMLOverride)
	}
	if deployStats.DotEnvCount > 0 {
		logger.Infof("[deploy] 已从 %s 载入 %d 项配置(未覆盖更高优先级的来源)", deployStats.DotEnvPath, deployStats.DotEnvCount)
	}
	// 部署配置(config.yaml/.env)此时已写入环境变量,重建日志以生效其中的
	// LOG_LEVEL / LOG_PATH 等设置;此前的启动日志已按默认配置输出(默认同样落盘到
	// ./logs/app.log),旧文件句柄由 logger.Init 内部回收,不会泄漏。
	logger.Init()

	// 登录令牌签名密钥:service 包 init 阶段已做过只读解析(进程随机兜底),
	// 这里在部署配置(.env/config.yaml)写入环境后重新解析,必要时落盘 data/auth.key,
	// 并输出「进程随机密钥」告警。多实例部署必须共享 TOKEN_SECRET 或同一密钥文件。
	infra.InitAuthKey()

	// 跨域白名单须在部署配置装配后求值(见 middleware.LoadAllowedOrigins 的说明)。
	corsAllowedOrigins := middleware.LoadAllowedOrigins()

	// 数据库后端由环境变量决定:默认 SQLite(DB_PATH),设置 DB_DRIVER=mysql
	// 或提供 DB_DSN 即切换到 MySQL。业务代码与 SQL 均为两库通吃,无需改动。
	store.InitFromEnv()
	// 员工与权限体系引导:补齐内置角色 + 超级管理员 + 归一化存量角色权限。
	// 必须在 Init 之后单独调用(store 根包不得反向依赖 dao)。
	dao.BootstrapRoles()
	// 启动探测引导管理员是否仍是默认口令 admin123 —— 公网部署不改密等于裸奔。
	dao.WarnDefaultAdminPassword()
	// 加载/生成本机主密钥,并把历史明文敏感配置迁移为密文(须在建库之后)。
	infra.InitSecretKeys()
	if !infra.SecretsEncrypted() {
		logger.Warnf("[warn] 敏感配置未加密存储,建议设置 CONFIG_MASTER_KEY 或允许自动生成本机主密钥")
	} else {
		dao.MigrateSecrets()
		if old, ok := infra.OldMasterKey(); ok {
			dao.RotateSecrets(old)
		}
	}
	// 本地调试 / 容器部署便捷通道:配置里 agent_token 为空时,可用环境变量
	// AGENT_TOKEN 预置初始令牌(SetSetting 自动加密,幂等回填、绝不覆盖已有值)。
	// 管理后台「系统配置 → 小票打印」保存过的令牌始终优先;门店代理程序
	// 用同一个令牌跑 print-agent --install 即可连通。
	//
	// 命名提醒:conf.EnvAgentToken(AGENT_TOKEN)是「云端服务端」预置初始令牌读的环境变量,
	// 与代理程序侧 conf.EnvPrintAgentToken(PRINT_AGENT_TOKEN)是两个不同角色 ——
	// 前者在服务端启动时回填 tb_config.agent_token,后者写在门店 agent.env 里供代理读取;
	// 两者值相同才能连通,但变量名/所在位置不同,运维/脚本切勿混用。
	if tok := strings.TrimSpace(infra.Getenv(conf.EnvAgentToken, "")); tok != "" {
		if err := dao.SetSettingIfEmpty("agent_token", tok); err != nil {
			logger.Warnf("[warn] 从环境变量预置 agent_token 失败: %v", err)
		} else {
			logger.Infof("已从环境变量 AGENT_TOKEN 预置本地打印代理令牌")
		}
	}
	// 按保留天数清理过期操作日志(AUDIT_RETENTION_DAYS,默认 365 天,0=不清理)。
	if days, _, n, err := service.CleanExpiredOperLogs(); err != nil {
		// days <= 0 表示清理功能已禁用,原逻辑静默返回;只有真正清理失败才告警。
		if days > 0 {
			logger.Warnf("[audit] 清理过期操作日志失败: %v", err)
		}
	} else if n > 0 {
		logger.Infof("[audit] 已清理 %d 条过期操作日志", n)
	}
	// 后台定时清理已结案的本地打印代理任务(AGENT_JOB_RETENTION_DAYS,默认 7 天)。
	// 队列是「待办」,结案后没有长期保留价值;翻账看的是 tb_print_log,不受影响。
	print.StartAgentCleanup()

	// 前端静态目录支持环境变量覆盖,便于打包部署到不同工作目录。
	// 启动审计:校验菜品图片引用与图片表内容一一对应,缺失即 [warn](回答「不能少东西」)。
	store.AuditDishImages()
	staticDir := infra.Getenv(conf.EnvStaticDir, conf.DefaultStaticDir)

	r := gin.New()
	// RequestID 必须在最前:先生成 requestID 注入 context,后面的访问日志、
	// panic 恢复、业务 handler 才能读到同一个 ID 关联整条链路。
	r.Use(middleware.RequestID(), logger.GinLogger(), logger.GinRecovery(), middleware.NewCORS(corsAllowedOrigins))
	if err := r.SetTrustedProxies(middleware.LoadTrustedProxies()); err != nil {
		logger.Fatalf("配置可信代理失败: %v", err)
	}
	r.MaxMultipartMemory = 8 << 20 // 8MB 内存缓存,超限落盘

	// 静态资源:上传文件。用自定义 handler 替代 r.Static(见 handler.ServeImage):
	// 文件缺失时记录 [warn],避免「资源丢了却静默 404」。
	// 图片/收款码只有 /uploads/ 一个前缀(历史 /picture/ 已由 migrateUploadPrefix 改写),
	// 避免同一目录挂两个前缀造成「代码里写哪个才对」的歧义。
	r.GET("/uploads/*file", handler.ServeImage)

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
			if strings.HasPrefix(p, "/api/") || p == "/api" || strings.HasPrefix(p, "/uploads") {
				c.JSON(http.StatusNotFound, handler.NewRsp(c, 404, "接口不存在"))
				return
			}
			c.File(filepath.Join(staticDir, "index.html"))
		})
	} else {
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/api/") || p == "/api" {
				c.JSON(http.StatusNotFound, handler.NewRsp(c, 404, "接口不存在"))
				return
			}
			c.JSON(http.StatusNotFound, handler.NewRsp(c, 404, "前端未构建,请先 cd frontend && npm run build"))
		})
	}

	port := infra.Getenv(conf.EnvPort, conf.DefaultPort)

	// http.Server 超时配置:只收紧「读请求头」与「空闲连接」两个维度,显式不设
	// ReadTimeout / WriteTimeout —— /api/agent/print/pull 是门店代理的长轮询,
	// 需 hold 连接约 30 秒,一旦设 WriteTimeout 会把正常长轮询当超时掐断。
	// ReadHeaderTimeout 10s 足以挡住 Slowloris 这类慢速发请求头的攻击;
	// IdleTimeout 120s 用于回收 keep-alive 空闲连接,避免连接被无限占用。
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
