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

	"dining-system/application/service"
	"dining-system/infrastructure"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/print"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/handler"
	"dining-system/interfaces/middleware"
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

		// 运行期版本查询(公开,无需登录):部署后 curl /api/version 即可确认
		// 正在运行的进程版本/commit,不依赖回翻启动日志或读安装目录 VERSION 文件。
		api.GET("/version", handler.Version)

		// ---- 顾客端(公开) ----
		api.GET("/customer/table/:id", handler.CustomerTable)
		// 入座选人数上报（茶位费门店）：同桌共享，别人扫码进来不再重复选。
		// 公开接口 + 幂等（同值重复上报无副作用），见 handler.CustomerSetTablePerson。
		api.POST("/customer/table/person", handler.CustomerSetTablePerson)
		api.GET("/customer/menu", handler.CustomerMenu)
		api.GET("/customer/remarks", handler.CustomerRemarks)
		api.GET("/customer/config", handler.CustomerSetting)
		api.POST("/customer/order", handler.CustomerAuthOptional, handler.CustomerCreateOrder)
		api.POST("/customer/order/append", handler.CustomerAuthOptional, handler.CustomerAppendOrder)
		// 改就餐人数(不改菜品):人数决定茶位费,加菜接口要求明细非空,不能复用
		// (见 service.CustomerUpdatePerson 的说明)。
		api.POST("/customer/order/person", handler.CustomerAuthOptional, handler.CustomerUpdatePerson)
		// 站点访问统计打点(公开):H5 点餐页与小程序启动时调用。带会员令牌则
		// 访客升级为「会员身份」,不带则匿名;恒返回 200,失败不影响点餐
		// (见 handler.VisitTrack 与 docs/design-docs/visit-analytics/spec.md)。
		api.POST("/customer/visit", handler.CustomerAuthOptional, handler.VisitTrack)
		// 顾客会员:一键注册/登录(自愿,未登录不影响点餐);登录态为 2h 短令牌,
		// 过期由小程序端静默重登(见 docs/design-docs/customer-member/spec.md)。
		api.POST("/customer/auth/login", handler.CustomerMemberLogin)
		api.GET("/customer/auth/profile", handler.CustomerAuthRequired, handler.CustomerMemberProfile)
		// 完善资料(昵称)与注销会员:均为顾客自助,必须带会员令牌。
		api.POST("/customer/auth/profile", handler.CustomerAuthRequired, handler.CustomerMemberUpdateProfile)
		api.DELETE("/customer/auth/account", handler.CustomerAuthRequired, handler.CustomerMemberDeleteAccount)
		// 手机号快速绑定(getPhoneNumber 按钮):手机号作为一种登录方式存身份表。
		api.POST("/customer/auth/phone", handler.CustomerAuthRequired, handler.BindPhone)
		// 储值:余额/流水/充值(微信 JSAPI)/余额支付,全部必须带会员令牌。
		api.GET("/customer/wallet", handler.CustomerAuthRequired, handler.WalletInfo)
		api.GET("/customer/wallet/txns", handler.CustomerAuthRequired, handler.WalletTxns)
		api.POST("/customer/wallet/recharge", handler.CustomerAuthRequired, handler.WalletRecharge)
		api.GET("/customer/wallet/recharge/status", handler.CustomerAuthRequired, handler.WalletRechargeStatus)
		// 充值补账:回调丢失时前端主动触发后端向渠道查单(顾客已付款却没入账的自救通道)。
		api.POST("/customer/wallet/recharge/reconcile", handler.CustomerAuthRequired, handler.WalletRechargeReconcile)
		// 《储值规则》同意留痕(合规:发卡须明示章程)。
		api.POST("/customer/wallet/rules/agree", handler.CustomerAuthRequired, handler.WalletAgreeRules)
		api.POST("/customer/wallet/pay", handler.CustomerAuthRequired, handler.WalletPay)
		// 混合支付:余额抵扣 + 微信补差(余额不足时也能用上储值)。
		api.POST("/customer/wallet/pay-mixed", handler.CustomerAuthRequired, handler.WalletMixedPay)
		// 混合支付取消:顾客点了取消就立刻退回已扣的余额,不让钱白等超时释放。
		api.POST("/customer/wallet/pay-cancel", handler.CustomerAuthRequired, handler.WalletCancelMixedPay)
		api.GET("/customer/order/mine", handler.CustomerAuthRequired, handler.CustomerMyOrders)
		// 历史订单详情:跨桌,归属只认会员身份(customer_id),不能用 tableId 校验
		// —— 顾客早已离开那张桌(见 service.CustomerMyOrderDetail)。
		api.GET("/customer/order/mine/:orderNo", handler.CustomerAuthRequired, handler.CustomerMyOrderDetail)
		// 按单号查订单是无鉴权公开接口,且单号会出现在小票/照片/群聊里:
		// 挂单 IP 限流兜底遍历探测(归属校验见 handler.CustomerOrderByNo)。
		api.GET("/customer/order/no/:orderNo",
			middleware.NewIPRateLimit(time.Minute, middleware.LoadCustomerOrderQueryRateLimit()),
			handler.CustomerOrderByNo)
		api.POST("/customer/order/urge", handler.CustomerUrgeOrder)
		// 订单评价(仅商家可见的内部反馈,匿名与会员同入口):已付款且上齐后可评,
		// 一单一评。匿名可评 = 无鉴权写接口,读写都挂 IP 限流兜底垃圾内容
		// (见 docs/design-docs/order-review/spec.md)。
		reviewLimiter := middleware.NewIPRateLimit(time.Minute, middleware.LoadCustomerReviewRateLimit())
		api.GET("/customer/review", reviewLimiter, handler.CustomerReviewStatus)
		api.POST("/customer/review", reviewLimiter, handler.CustomerAuthOptional, handler.CustomerReviewCreate)
		// 评价图片上传(先传后评):独立表 tb_review_image,提交时事务绑定,孤儿 24h 清理。
		api.POST("/customer/review/upload", reviewLimiter, handler.CustomerAuthOptional, handler.CustomerReviewUpload)
		api.GET("/customer/pay/qr", handler.CustomerPayQr)
		api.POST("/customer/pay/create", handler.PayCreate)
		// 小程序内支付(JSAPI):服务端用 wx.login 的 code 换 openid 并签好调起参数,
		// 前端拿参数直接调 wx.requestPayment。未开通时返回明确文案,前端降级到收款码。
		api.POST("/customer/pay/jsapi", handler.PayJsapi)
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

			// 会员管理(顾客会员,与上面的员工账号是两套体系):只读列表 + 启停用。
			// 不提供后台新建/删除 —— 档案由顾客自助注册与注销,后台代建会留下
			// 没有登录方式的孤儿档案(见 handler/member.go 顶部说明)。
			admin.GET("/member/list", handler.MemberList)
			admin.POST("/member/status", handler.MemberToggleStatus)
			// 储值:某会员的余额与流水 / 后台调账(赠送或扣减) / 退余额(钱出店)。
			admin.GET("/member/wallet", handler.MemberWalletDetail)
			admin.POST("/member/wallet/adjust", handler.MemberWalletAdjust)
			admin.POST("/member/wallet/withdraw", handler.MemberWalletWithdraw)

			// 角色与权限
			admin.GET("/role/list", handler.RoleList)
			admin.POST("/role/save", handler.RoleSave)
			admin.POST("/role/update", handler.RoleUpdate)
			admin.DELETE("/role/:id", handler.RoleDelete)
			admin.GET("/perm/catalog", handler.PermCatalog)

			admin.GET("/table/list", handler.TableList)
			// 桌台小程序码(顾客扫它进小程序点餐);配置见「系统配置 → 小程序桌牌码」
			admin.GET("/table/weapp-code", handler.TableWeappCode)
			admin.POST("/table/save", handler.TableSave)
			admin.POST("/table/update", handler.TableUpdate)
			// 桌台人工可用性状态:停用 / 预约 / 恢复正常(与订单占用状态正交)。
			admin.POST("/table/lock", handler.TableLock)
			// 强制清台:店家漏点收款导致桌台长期占用时的唯一人工补救入口(会动钱,见 handler 注释)
			admin.POST("/table/release", handler.TableRelease)
			admin.DELETE("/table/:id", handler.TableDelete)

			admin.GET("/category/list", handler.CategoryList)
			admin.POST("/category/save", handler.CategorySave)
			admin.POST("/category/update", handler.CategoryUpdate)
			admin.DELETE("/category/:id", handler.CategoryDelete)

			admin.GET("/dish/list", handler.DishList)
			admin.GET("/dish/:id", handler.DishGet)
			admin.POST("/dish/save", handler.DishSave)
			admin.POST("/dish/update", handler.DishUpdate)
			// 拖拽调整菜品在分类内的顺序(不接 :id,避免与 GET /dish/:id 的路由语义混淆)
			admin.POST("/dish/sort", handler.DishSort)
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
			// 预览该打印机的票据(不出纸):默认按当前设置模拟真实出纸,kind=test 出测试页
			admin.GET("/printer/preview/:id", handler.PrinterPreview)
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
			// 刷新失败的飞鹅记录:按云端单号回查最新打印结果(不出纸、不重推)
			admin.POST("/print/log/refresh", handler.PrintLogRefresh)
			admin.POST("/order/reprint", handler.OrderReprint)

			admin.GET("/config/list", handler.SettingList)
			admin.POST("/config/save", handler.SettingSave)
			// 打印代理全局令牌:后端生成即落库(不随整份配置保存),明文只回显一次。
			admin.POST("/config/agent/token", handler.ConfigAgentTokenIssue)

			admin.GET("/order/list", handler.OrderList)
			admin.GET("/order/board", handler.OrderBoard)
			// 未结账超时告警(开关与阈值随配置下发,见 handler.OrderStale)
			admin.GET("/order/stale", handler.OrderStale)
			admin.GET("/order/urge/list", handler.UrgeList)
			admin.POST("/order/urge/handle", handler.UrgeHandle)
			// 订单评价(仅商家可见):列表+汇总只读,删除不当内容。
			admin.GET("/review/list", handler.AdminReviewList)
			admin.DELETE("/review/:id", handler.AdminReviewDelete)
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
			// 用户用餐时长分析(翻台/排台参考)
			admin.GET("/report/diningDuration", handler.ReportDiningDuration)
			// 站点访问统计(PV/UV/新访客/渠道构成),同属 report:view
			admin.GET("/report/visits", handler.ReportVisits)
			// 访问明细下钻(访问统计弹窗),同属 report:view
			admin.GET("/report/visitLogs", handler.ReportVisitLogs)

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

// sinceBoot 返回从进程启动到此刻的时长(毫秒取整),供启动各阶段的日志打点使用。
// 启动慢时(建库/迁移/图片导入)靠它区分「哪一步吃掉了时间」,不必掐表猜。
func sinceBoot(start time.Time) time.Duration { return time.Since(start).Round(time.Millisecond) }

func main() {
	// 日志最先初始化:后续启动日志统一走 zap(控制台可读 + 文件 JSON 可采集)。
	logger.Init()
	defer logger.Sync()
	// bootStart 是启动打点的统一起点(见下面各条 [boot] 日志)。
	bootStart := time.Now()

	gin.SetMode(gin.ReleaseMode)

	// 装配部署配置,优先级:真实环境变量 > config.yaml > .env > 代码默认值。
	// config.yaml 解析失败(键名拼错 / 缩进错)会让启动直接失败 —— 静默跑起来
	// 更难排查;其它来源缺失都属正常,单机 SQLite 零配置即可启动。
	deployStats, err := infrastructure.LoadDeployConfig()
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
	// 下面这条走的是「重建之前」的默认日志:即使 config.yaml 把 log.level 设成
	// warn/error,它也一定会出现在终端上,标出启动进度到哪一步了。
	logger.Infof("[boot] 部署配置就绪(%s, 共 %d 项), 按配置重建日志 ...",
		sinceBoot(bootStart), deployStats.YAMLCount+deployStats.DotEnvCount)
	logger.Init()

	// 登录令牌签名密钥:service 包 init 阶段已做过只读解析(进程随机兜底),
	// 这里在部署配置(.env/config.yaml)写入环境后重新解析,必要时落盘 data/auth.key,
	// 并输出「进程随机密钥」告警。多实例部署必须共享 TOKEN_SECRET 或同一密钥文件。
	// ⚠️ 从这一步起日志已被 config.yaml 的 log.level 过滤:若设成 warn/error,
	// 下面所有 [boot]/[db] 打点都不会出现在终端,要看 <安装目录>/logs/app.log。
	infrastructure.InitAuthKey()
	logger.Infof("[boot] 登录令牌签名密钥就绪(%s)", sinceBoot(bootStart))

	// 跨域白名单须在部署配置装配后求值(见 middleware.LoadAllowedOrigins 的说明)。
	corsAllowedOrigins := middleware.LoadAllowedOrigins()

	// 数据库后端由环境变量决定:默认 SQLite(DB_PATH),设置 DB_DRIVER=mysql
	// 或提供 DB_DSN 即切换到 MySQL。业务代码与 SQL 均为两库通吃,无需改动。
	// 这一步是最容易「静默卡住」的地方(建连/握手没有超时),store.Init 内部
	// 另有逐步打点,卡住时看最后一条 [db] 日志即可定位。
	logger.Infof("[boot] 初始化数据库连接(%s) ...", sinceBoot(bootStart))
	store.InitFromEnv()
	logger.Infof("[boot] 数据库初始化完成(%s)", sinceBoot(bootStart))
	// 员工与权限体系引导:补齐内置角色 + 超级管理员 + 归一化存量角色权限。
	// 必须在 Init 之后单独调用(store 根包不得反向依赖 dao)。
	dao.BootstrapRoles()
	logger.Infof("[boot] 内置角色与管理员账号就绪(%s)", sinceBoot(bootStart))
	// 启动探测引导管理员是否仍是默认口令 admin123 —— 公网部署不改密等于裸奔。
	dao.WarnDefaultAdminPassword()
	// 加载/生成本机主密钥,并把历史明文敏感配置迁移为密文(须在建库之后)。
	logger.Infof("[boot] 装配主密钥与敏感配置(%s) ...", sinceBoot(bootStart))
	infrastructure.InitSecretKeys()
	if !infrastructure.SecretsEncrypted() {
		logger.Warnf("[warn] 敏感配置未加密存储,建议设置 CONFIG_MASTER_KEY 或允许自动生成本机主密钥")
	} else {
		dao.MigrateSecrets()
		if old, ok := infrastructure.OldMasterKey(); ok {
			dao.RotateSecrets(old)
		}
	}
	logger.Infof("[boot] 主密钥与敏感配置就绪(%s)", sinceBoot(bootStart))
	// 后台任务(打印代理任务回收、遗留订单巡检)在协程里周期执行,
	// 单独打点:它们各自的首次查询可能较慢,与本进程「起来没有」要区分开。
	logger.Infof("[boot] 启动后台巡检与自检任务(%s) ...", sinceBoot(bootStart))
	// 本地调试 / 容器部署便捷通道:配置里 agent_token 为空时,可用环境变量
	// AGENT_TOKEN 预置初始令牌(SetSetting 自动加密,幂等回填、绝不覆盖已有值)。
	// 管理后台「系统配置 → 小票打印」保存过的令牌始终优先;门店代理程序
	// 用同一个令牌跑 print-agent --install 即可连通。
	//
	// 命名提醒:conf.EnvAgentToken(AGENT_TOKEN)是「云端服务端」预置初始令牌读的环境变量,
	// 与代理程序侧 conf.EnvPrintAgentToken(PRINT_AGENT_TOKEN)是两个不同角色 ——
	// 前者在服务端启动时回填 tb_config.agent_token,后者写在门店 agent.env 里供代理读取;
	// 两者值相同才能连通,但变量名/所在位置不同,运维/脚本切勿混用。
	if tok := strings.TrimSpace(infrastructure.Getenv(conf.EnvAgentToken, "")); tok != "" {
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
	// 桌台遗留订单防护:按「跨日自动清台」配置开关后台巡检,把过期遗留单所在桌台
	// 恢复空闲(订单与账务不动)。开关关闭时每轮直接跳过,不做查询。
	service.StartStaleOrderGuard()
	// 储值:关闭超过 24 小时仍未入账的充值单(顾客早已放弃、渠道也不会再受理)。
	// 启动时先清一轮(停机期间累积的废单),之后由后台任务每 6 小时一次 ——
	// 常年不重启的门店否则会在 tb_recharge 里堆满顾客早已放弃的待支付单。
	service.ExpireWalletRecharges()
	service.StartWalletRechargeExpirer()
	// 混合支付超时释放:余额已扣、微信迟迟未付的订单,15 分钟后自动退回余额并关单。
	// 放在 handler 层:释放前要用 pay 包关渠道单,service 不得 import pay。
	handler.StartMixedPaySweeper()
	// 评价图片孤儿清理:上传后未绑定评价的图 24h 后回收(用户内容不进 tb_image,见 tb_review_image)。
	service.StartReviewImageOrphanCleaner()
	// 站点访问统计:批量写 worker + 日聚合补跑 + 明细清理(开关关闭时不起 worker)。
	// 放在巡检任务之后:两者互不依赖,但日志上保持「后台任务」在同一段。
	service.StartVisitRollup()
	logger.Infof("[boot] 后台巡检与自检任务已就绪(%s)", sinceBoot(bootStart))

	// 前端静态目录支持环境变量覆盖,便于打包部署到不同工作目录。
	// 启动审计:校验菜品图片引用与图片表内容一一对应,缺失即 [warn](回答「不能少东西」)。
	store.AuditDishImages()
	staticDir := infrastructure.Getenv(conf.EnvStaticDir, conf.DefaultStaticDir)
	logger.Infof("[boot] 启动审计完成, static_dir=%s(%s)", staticDir, sinceBoot(bootStart))

	logger.Infof("[boot] 注册路由与中间件(%s) ...", sinceBoot(bootStart))
	r := gin.New()
	// RequestID 必须在最前:先生成 requestID 注入 context,后面的访问日志、
	// panic 恢复、业务 handler 才能读到同一个 ID 关联整条链路。
	r.Use(middleware.RequestID(), logger.GinLogger(), logger.GinRecovery(), middleware.NewCORS(corsAllowedOrigins))
	if err := r.SetTrustedProxies(middleware.LoadTrustedProxies()); err != nil {
		logger.Fatalf("配置可信代理失败: %v", err)
	}
	r.MaxMultipartMemory = 128 << 20 // 128MB 内存缓存,须大于上传上限(64MB),超限落盘

	// 静态资源:上传文件。用自定义 handler 替代 r.Static(见 handler.ServeImage):
	// 文件缺失时记录 [warn],避免「资源丢了却静默 404」。
	// 图片/收款码只有 /uploads/ 一个前缀(历史 /picture/ 已由 migrateUploadPrefix 改写),
	// 避免同一目录挂两个前缀造成「代码里写哪个才对」的歧义。
	r.GET("/uploads/*file", handler.ServeImage)

	// 运行期版本信息:把编译期注入的 main.version/gitCommit/buildTime 交给 handler 包,
	// 供 GET /api/version 在进程中即时读取(与内存态一致,不依赖启动日志或安装目录文件)。
	handler.InitBuildInfo(version, gitCommit, buildTime)

	// 后端 API 前缀
	setupAPI(r)

	// 前端静态资源(若已构建)。Vite 默认把产物放到 dist/assets,但这里统一约定
	// assetsDir 为 "static"(见 frontend/vite.config.ts),因此只需托管 /static 即可。
	// 同时兼容 /assets 以覆盖未改配置的旧构建产物。
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		logger.Infof("[boot] 已挂载前端静态资源目录 %s", staticDir)
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
		logger.Warnf("[boot][告警] 前端产物目录不存在或不是目录(%s), 页面请求将返回 404;"+
			"请确认 server.static_dir 指向 <安装目录>/frontend/dist", staticDir)
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/api/") || p == "/api" {
				c.JSON(http.StatusNotFound, handler.NewRsp(c, 404, "接口不存在"))
				return
			}
			c.JSON(http.StatusNotFound, handler.NewRsp(c, 404, "前端未构建,请先 cd frontend && npm run build"))
		})
	}

	port := infrastructure.Getenv(conf.EnvPort, conf.DefaultPort)

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
		logger.Infof("点餐管理系统已启动 v%s (build %s, commit %s): http://localhost:%s (管理端账号 %s, 启动耗时 %s)",
			version, buildTime, gitCommit, port, handler.AdminUser(), sinceBoot(bootStart))
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
