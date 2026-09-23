// Command print-agent 门店本地打印代理。
//
// 干什么用的:
//
//	后端部署在云服务器后,「直连 IP:9100」这条路径永远够不到门店内网的打印机。
//	本程序跑在门店内网任意一台常开机的设备上(收银电脑 / 小主机 / 树莓派 / 软路由),
//	主动**出站**轮询云端的打印队列,把指令转发给门店内网的打印机:
//
//	云后端 ──入队──> 本程序(HTTPS 出站拉单) ──TCP 192.168.x.x:9100──> 打印机
//
// 为什么不需要装驱动:
//
//	9100 是打印机的 RAW / JetDirect 端口,打印机在 TCP 层直接接收 ESC/POS 字节流,
//	不经过操作系统的打印队列。本程序做的只是「把云端下发的一串字节原样写进 socket」,
//	和浏览器打印(A4 单据)完全不同 —— 后者才需要厂商驱动。
//
// 本程序不依赖后端任何代码:票据的排版与 ESC/POS 编码(含 GBK、切纸)全部在后端完成,
// 下发的是 base64 后的字节流,这里只负责搬运。
//
// 用法(完整部署步骤见 docs/print-agent.md):
//
//	print-agent --server https://dining.example.com --token <系统配置里的代理令牌>
//	PRINT_AGENT_SERVER=... PRINT_AGENT_TOKEN=... print-agent     # 或用环境变量
//	print-agent --once                                          # 只跑一轮,用于排障自检
//	print-agent -v  (或 --version)                              # 查版本号/构建时间/提交
//
// 与 program 同目录下可放一个 agent.env(KEY=VALUE 逐行,与后端 .env 同格式),
// Windows 下把它和 exe 放一起双击即可运行,不必配环境变量。
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
)

// 构建时注入(Makefile LDFLAGS -X main.version 等)。
var version, buildTime, gitCommit = "dev", "", ""

// config 运行参数。
type config struct {
	server      string // 云端地址,如 https://dining.example.com(可带子路径,末尾斜杠可有可无)
	token       string // 代理令牌(系统配置 → 小票打印 → 代理令牌)
	interval    time.Duration
	limit       int
	wait        int
	name        string // 代理标识,多台代理时可区分是谁取的
	state       string // 本地状态文件路径(幂等去重用)
	logPath     string // 日志文件路径(空=仅控制台;Windows 任务计划运行必须设,否则日志无人接收)
	insecure    bool   // 跳过 TLS 证书校验(仅自签证书的内网环境)
	once        bool   // 只跑一轮后退出(排障自检,自动开启 verbose)
	verbose     bool   // 输出调试级日志(空轮次心跳、回执统计等)
	showVersion bool   // 输出版本信息后退出
	install     bool   // 一条命令安装:写 agent.env 并注册系统自启动
	uninstall   bool   // 卸载系统自启动(保留 agent.env)
	upgrade     bool   // 自助升级:下载最新产物替换自身(见 upgrade.go)
	// probe 打印机连通性自检的目标列表(逗号分隔的 ip[:port],见 probe.go)。
	// 纯本地探测:不连云端,故不需要 server/token,断网时也能查内网。
	probe        string
	probeTimeout time.Duration // 单台拨号超时(默认 5s)
	probePrint   bool          // 自检时真的吐一张 ASCII 自检页(默认只探端口 + 读状态)
	// printVia 打印通道:tcp(默认,直连 IP:9100)/ cups(一律走本机 CUPS 队列)/ auto
	// (先直连,失败且找得到队列时改投 CUPS)。macOS 15+ 的「本地网络隐私」会拦下由
	// LaunchAgent 托管的直连,cups/auto 是绕开它的办法,详见 cups.go 与部署手册。
	printVia string
	// cupsQueue 显式队列映射(PRINT_AGENT_CUPS_QUEUE)。形如 `192.168.1.133=厨房机`,
	// 只写队列名时作为所有打印机的默认队列。留空则只靠 lpstat -v 自动发现。
	cupsQueue string
	// noSleep 防睡眠(macOS 专属):安装时决定 launchd 是否用 caffeinate -i -s 包装代理,
	// 顶住「空闲睡眠」。它**挡不住合盖睡眠**(Clamshell Sleep 是电源管理层的独立触发器),
	// 合盖继续打单还需 sudo pmset -a disablesleep 1 —— 装完会核对并提示。
	noSleep         bool
	noSleepExplicit bool // 是否由 --no-sleep / PRINT_AGENT_NO_SLEEP 显式指定(未指定时安装交互询问)
	// serverExplicit/tokenExplicit:本次是否显式给出 --server/--token(含 shell export)。
	// 显式时 --install 覆盖 agent.env 里的旧值;值若来自已有的 agent.env 则不算显式,
	// 仍按合并语义保留旧值。
	serverExplicit bool
	tokenExplicit  bool
}

func main() {
	// 全局 panic 兜底:崩溃先落日志(含堆栈)再以非零码退出 —— 门店设备上无人值守,
	// 进程级守护(launchd KeepAlive / 任务计划 RestartOnFailure / systemd Restart)
	// 会把进程拉起,但拉起前必须有现场可查。
	defer func() {
		if r := recover(); r != nil {
			logf("[崩溃] panic: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()

	enableUTF8Console()

	// 先把同目录下的 agent.env 灌进环境变量,再解析命令行 —— 这样「文件配置」
	// 与「环境变量/命令行」都能用,且后者优先级更高。
	// 灌之前先快照真实环境:之后就无法区分「本次传的」与「文件里的旧值」了。
	snapshotEnv()
	loadEnvFile(envFilePath())

	cfg, err := parseFlags()
	if err != nil {
		fatalf("%v", err)
	}
	if cfg.logPath != "" {
		if err := initLogFile(cfg.logPath); err != nil {
			logf("[警告] 日志文件不可写(%v),仅输出到控制台", err)
		}
	}
	if cfg.showVersion {
		printVersion()
		return
	}
	if cfg.probe != "" {
		// 连通性自检是纯本地操作,不需要(也不应要求)云端地址与令牌 ——
		// 云端不通时,恰恰更需要确认「本机到打印机」这一段。
		// 通道设置也要就位:自检会一并报告「代理实际会走哪条通道、队列找到没有」。
		configurePrintChannel(cfg)
		os.Exit(runProbe(cfg))
	}
	if cfg.install {
		runInstall(cfg)
		return
	}
	if cfg.uninstall {
		runUninstall()
		return
	}
	if cfg.once {
		cfg.limit = 1
		cfg.wait = 0
		cfg.verbose = true // 排障自检:连空闲轮次也如实记录,便于判断「到底有没有取到单」
	}
	setVerbose(cfg.verbose)
	if cfg.verbose {
		logf("调试日志已开启,空轮次与回执统计也会记录")
	}
	configurePrintChannel(cfg)
	logf("打印通道: %s%s", cfg.printVia, printChannelNote(cfg))

	client := newHTTPClient(cfg.insecure)
	base := strings.TrimSuffix(cfg.server, "/")

	// 自助升级:与常驻轮询互斥的一次性命令(升级后提示重启,由守护拉起新版本)。
	if cfg.upgrade {
		runUpgrade(client, base, cfg)
		return
	}

	// 启动自检:令牌/地址有问题立刻失败退出(systemd 会重试并让失败可见),
	// 而不是静默循环 —— 「代理跑着但一单也没打出来」是最难排查的状态。
	if err := ping(client, base, cfg); err != nil {
		fatalf("连接云端失败: %v\n请检查 --server / --token,以及系统配置里的「代理令牌」是否一致", err)
	}
	logf("已连接云端 %s(代理标识 %s,代理版本 %s,轮询间隔 %s,长轮询 %ds)",
		base, cfg.name, version, cfg.interval, cfg.wait)

	// 本地去重状态:记录「已经成功打印过的幂等号」。
	// 打印成功后先落盘、再回执 —— 即便回执因断网丢失、任务被云端重发,
	// 下次取到同一 deliveryId 也会被跳过,从而避免同一张票重复出纸。
	st := loadJobState(cfg.state)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// 常驻模式下启用假死看门狗:主循环每轮打活动戳,卡死超过阈值即自杀退出,
	// 由 launchd KeepAlive / 任务计划 RestartOnFailure / systemd Restart=always
	// 自动拉起(见 watchdog.go)。--once 单轮自检不需要。
	if !cfg.once {
		markActive()
		startWatchdog(stop)
	}

	for {
		// 成功/失败的逐条日志与每轮汇总都由 runCycle 负责输出 —— 这里只处理
		// 「取单都没成功」的网络类错误,避免两处各说一半导致口径不一致。
		if _, err := runCycle(client, base, cfg, st); err != nil {
			// 网络类错误不退出:门店断网、云端重启都属正常,恢复后自动续上。
			logf("[警告] 本轮取单失败(稍后自动重试): %v", err)
		}
		markActive()
		if cfg.once {
			return
		}
		select {
		case <-stop:
			logf("收到退出信号,已停止")
			return
		case <-time.After(cfg.interval):
		}
	}
}

// ============================================================================
// 一轮:取单 → 逐条打印 → 回执
// ============================================================================

// runCycle 执行一轮完整流程,返回本轮回执的任务数。
//
// 注意与「取到的任务数」区分:熔断冷却中的任务会被挂起(不回执、不拨号),
// 不计入返回值,以便主循环日志如实反映本轮真正处理了几条。
func runCycle(client *http.Client, base string, cfg config, st *jobState) (int, error) {
	start := time.Now()
	jobs, err := pull(client, base, cfg)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		// 空闲是长轮询下的常态(每几秒一次),默认不记,只在排障时开 --verbose。
		logvf("本轮无待打印任务(pull 耗时 %s)", sinceMS(start))
		return 0, nil
	}
	// 入口日志:云端下发了什么、要发去哪台打印机,先落一行再逐条打印。
	// 排查「代理在线但没出纸」时,这一行是判断任务到底有没有到本机的分水岭。
	logf("[取单] 本轮收到 %d 条任务(pull 耗时 %s): %s", len(jobs), sinceMS(start), jobBriefs(jobs))

	type result struct {
		JobID  int    `json:"jobId"`
		Ok     bool   `json:"ok"`
		Detail string `json:"detail"`
		// Skipped 本机未真正尝试打印(熔断冷却中,没拨过打印机),请求云端退回队列
		// 并归还尝试次数。老云端忽略该字段,按失败处理(代理仅在云端声明
		// ack-skipped 能力时才发,见 noteServerCapabilities)。
		Skipped bool `json:"skipped,omitempty"`
		// RetryAfter 建议多少秒后再下发(冷却剩余秒数),避免任务立刻回队列形成空转。
		RetryAfter    int          `json:"retryAfter,omitempty"`
		PrinterStatus *agentStatus `json:"printerStatus,omitempty"`
	}
	results := make([]result, 0, len(jobs))
	for _, j := range jobs {
		// 幂等去重:这条任务在本机已经成功打印过(上一次回执丢失被重发)。
		// 跳过真正打印、直接回执成功,避免重复出票。
		if st.has(j.DeliveryID) {
			logf("[去重] %s 已在本机打印过(deliveryId %s),跳过并回执", jobBrief(j), j.DeliveryID)
			results = append(results, result{JobID: j.JobID, Ok: true})
			continue
		}

		// 熔断冷却期内绝不能按「打印失败」上报:
		//
		// 云端在 claim 取单时就已经 attempts+1(上限 po.PrintJobMaxAttempts=3),
		// 失败回执会让任务退回队列并被再次取走 —— 也就是每回执一次失败就花掉一次额度。
		// 若在冷却期内照常回执失败,3 次额度会被「根本没拨过打印机的空转」吃光:
		// 打印机只是短暂不可达(no route to host 常见于刚上电 / WiFi 尚未拿到 IP),
		// 单据却在几十秒内被判成「已放弃」,再也补不回来,反倒是丢单。
		if addr := printerAddr(j); addr != "" {
			if remain, ok := isDialCooling(addr); ok {
				detail := fmt.Sprintf("打印机连接熔断冷却中(约 %d 秒后重试)", ceilSeconds(remain))
				if serverSupportsSkipped.Load() {
					// 云端支持 skipped:明确上报「未尝试」,任务立刻退回队列并归还尝试次数,
					// 冷却结束后马上重新下发,不必等一个租约。
					logf("[等待] %s: %s,已上报「未尝试」——不消耗云端重试次数", jobBrief(j), detail)
					results = append(results, result{
						JobID: j.JobID, Ok: false, Detail: detail,
						Skipped: true, RetryAfter: ceilSeconds(remain) + 1,
					})
					continue
				}
				// 老云端:挂起不回执。任务保持 claimed,等 60 秒租约到期自动回到队列;
				// 冷却只有 dialCooldownTTL(30s,必须小于租约),届时取到它一定是真实拨号。
				logf("[等待] %s: %s,本轮不回执 —— 冷却等待不消耗云端重试次数", jobBrief(j), detail)
				continue
			}
		}

		err, printerStatus := printJob(j)
		r := result{JobID: j.JobID, Ok: err == nil, PrinterStatus: toAgentStatus(printerStatus)}
		if err != nil {
			r.Detail = err.Error()
			logf("[失败] %s: %v", jobBrief(j), err)
			if hint := dialFailureHint(err); hint != "" {
				logf("[提示] %s", hint)
			}
		} else {
			if cfg.once {
				if printerStatus != nil {
					logf("[状态] %s DLE EOT raw=%s queried=%v", jobBrief(j), printerStatus.raw, printerStatus.queried)
				} else {
					logf("[状态] %s DLE EOT raw=<未查询>", jobBrief(j))
				}
				// 状态回读是附加观测:有异常(缺纸/盖板开)时单独提示,仍按成功回执。
				if printerStatus != nil && (printerStatus.paperOut || printerStatus.coverOpen ||
					printerStatus.paperNearEnd || printerStatus.paused || printerStatus.errFatal) {
					logf("[提醒] %s 打印机状态异常: %s", jobBrief(j), printerStatus.raw)
				}
			}
			// 先落盘再回执:把「重复打印」的窗口压到「打印成功 → 落盘」之间的毫秒级。
			st.markDone(j.DeliveryID)
			logf("[成功] %s 已送出 ×%d", jobBrief(j), j.Copies)
		}
		results = append(results, r)
	}

	// results 为空只可能是「全部任务都在熔断冷却中且云端不支持 skipped」:
	// 一条都不回执(回执会让云端 attempts+1,把重试额度花在没拨号的空转上),
	// 任务留在 claimed 等租约到期自动回到队列。
	if len(results) == 0 {
		logvf("本轮无回执上报(%d 条任务均在熔断冷却等待中)", len(jobs))
		return 0, nil
	}
	ok, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch {
		case r.Skipped:
			// 冷却挂起不是失败:云端不会扣重试次数,也不会把任务判成放弃。
			skipped++
		case r.Ok:
			ok++
		default:
			failed++
		}
	}
	// 汇总口径把「未尝试」单独拆出来:混进失败会让人以为这单已经判死要补打。
	summary := fmt.Sprintf("成功 %d / 失败 %d", ok, failed)
	if skipped > 0 {
		summary += fmt.Sprintf(" / 未尝试(冷却等待)%d", skipped)
	}
	// verbose 下先记一笔「打算上报什么」,与汇总对照即可看出回执有没有丢。
	logvf("准备回执上报: 共 %d 条(%s)", len(results), summary)
	ack(client, base, cfg, results)
	// 汇总用「如实口径」而不是「已处理 N 条」:失败也算处理过了,但写上会让排障的人
	// 误以为纸出来了。这里明确拆出成功/失败,一眼看出本轮到底成没成。
	if failed > 0 || skipped > 0 {
		logf("[汇总] 本轮回执 %d 条(%s)", len(results), summary)
	} else {
		logf("[汇总] 本轮回执 %d 条,全部成功", len(results))
	}
	return len(results), nil
}

// sinceMS 取 start 至今的耗时,毫秒精度(日志可读性,不追求精确计时)。
func sinceMS(start time.Time) time.Duration {
	return time.Since(start).Round(time.Millisecond)
}
