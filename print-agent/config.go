package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 默认参数。轮询间隔取 3 秒:订单到出纸的延迟让顾客察觉不到,又不会给云端太大压力。
const (
	defaultInterval = 3
	defaultLimit    = 10
	defaultWait     = 25
	httpTimeout     = 15 * time.Second
	// defaultProbeTimeoutSec --probe 单台拨号超时(秒)。与打印路径的 dialTimeout(5s)
	// 同量级:同网段内网的打印机应答是毫秒级,5 秒足够区分「关机」与「网段不通」。
	defaultProbeTimeoutSec = 5
)

// envSnapshot 记录 loadEnvFile 之前进程真实环境里的 SERVER/TOKEN。
//
// 为什么要在读文件之前快照:loadEnvFile 会把同目录 agent.env 的值 Setenv 进环境,
// 之后再 os.Getenv 就分不清这个值到底是本次命令/环境变量给的,还是旧配置文件里的。
// 装第二次时这个区分很关键——旧令牌必须能被 --token 覆盖掉。
var envSnapshot struct{ server, token string }

// snapshotEnv 必须在 loadEnvFile 之前调用,否则快照到的会是 agent.env 里的旧值。
func snapshotEnv() {
	envSnapshot.server = strings.TrimSpace(os.Getenv("PRINT_AGENT_SERVER"))
	envSnapshot.token = strings.TrimSpace(os.Getenv("PRINT_AGENT_TOKEN"))
}

// ============================================================================
// 参数与工具
// ============================================================================

func parseFlags() (config, error) {
	var (
		// server/token 刻意**不**把配置值写进 flag 默认值:flag 会把默认值回显在用法
		// 输出里,于是门店敲错一个参数(甚至只是 --help)就会把代理令牌明文打到终端、
		// 连同截屏一起进工单。取值改在 Parse 之后从环境补齐(见下方 cmdLine 那段)。
		server       = flag.String("server", "", "云端地址,如 https://dining.example.com")
		token        = flag.String("token", "", "代理令牌(系统配置 → 小票打印 → 代理令牌)")
		interval     = flag.Int("interval", envInt("PRINT_AGENT_INTERVAL", defaultInterval), "轮询间隔(秒)")
		limit        = flag.Int("limit", envInt("PRINT_AGENT_LIMIT", defaultLimit), "单次最多取几条任务(1-50)")
		wait         = flag.Int("wait", envInt("PRINT_AGENT_WAIT", defaultWait), "长轮询等待秒数(0-30,默认25;--once 强制0;环境变量 PRINT_AGENT_WAIT)")
		name         = flag.String("name", envOr("PRINT_AGENT_NAME", ""), "代理标识(默认取主机名)")
		state        = flag.String("state", envOr("PRINT_AGENT_STATE", ""), "本地状态文件(记录已成功打印的幂等号,用于去重;默认落在运行数据目录 print-agent.state)")
		logPath      = flag.String("log", envOr("PRINT_AGENT_LOG", ""), "日志文件路径(空=仅控制台;Windows 任务计划/服务方式运行必须设,否则日志无人接收,如 --log print-agent.log)")
		insecure     = flag.Bool("insecure", envOr("PRINT_AGENT_INSECURE", "") == "1", "跳过 TLS 证书校验(自签证书时使用)")
		once         = flag.Bool("once", false, "只跑一轮后退出(排障自检,强制 wait=0 并自动开启调试日志)")
		verbose      = flag.Bool("verbose", envOr("PRINT_AGENT_VERBOSE", "") == "1", "输出调试级日志(含空轮次心跳与回执统计;常驻模式下较刷屏,排障时开启)")
		showVersion  = flag.Bool("version", false, "输出版本、构建时间、Git 提交后退出")
		install      = flag.Bool("install", false, "一条命令安装:生成 agent.env 并注册系统自启动")
		uninstall    = flag.Bool("uninstall", false, "卸载系统自启动(保留 agent.env)")
		upgrade      = flag.Bool("upgrade", false, "自助升级:从云端下载最新代理产物并替换自身(替换后需重启生效)")
		noSleep      = flag.Bool("no-sleep", envOr("PRINT_AGENT_NO_SLEEP", "") == "1", "macOS 防睡眠:代理运行期间用 caffeinate 顶住空闲睡眠(需插电;仅 --install 时写入自启动配置)。注意 caffeinate 挡不住合盖睡眠,合盖继续打单还需另设 sudo pmset -a disablesleep 1,装完会提示")
		probe        = flag.String("probe", "", "只做打印机连通性自检:直接拨 ip[:port](多个用逗号分隔,端口缺省 9100),打印「可达/不可达 + 状态回读」;不连云端、不入队、不消耗重试次数")
		probeTimeout = flag.Int("probe-timeout", envInt("PRINT_AGENT_PROBE_TIMEOUT", defaultProbeTimeoutSec), "自检时单台打印机的拨号超时(秒,1-30)")
		probePrint   = flag.Bool("probe-print", false, "自检时吐一张纯 ASCII 自检页(默认只探端口 + 读状态,不吐纸)")
		doctor       = flag.Bool("doctor", false, "一条命令体检:一次查完配置/自启动/打印通道/防睡眠与合盖/云端连通,并给出处置命令;可再带 --probe <打印机IP> 把打印机那一段一起查")
		setupCUPS    = flag.String("setup-cups", "", "macOS:为指定打印机建好系统打印队列并打开 CUPS 通道(直连被系统「本地网络」权限拦下时用),形如 192.168.1.133 或 192.168.1.133:9100")
		printVia     = flag.String("print-via", envOr("PRINT_AGENT_PRINT_VIA", channelTCP), "打印通道: tcp=直连打印机 IP:9100(默认);cups=一律走本机 CUPS 打印队列;auto=先直连、失败且找得到队列时改投 CUPS(macOS 15+ 被「本地网络」权限拦下时用 cups/auto)")
		cupsQueue    = flag.String("cups-queue", envOr("PRINT_AGENT_CUPS_QUEUE", ""), "CUPS 队列映射(仅 cups/auto 通道): 形如 192.168.1.133=厨房打印机,多个用逗号分隔;只写队列名则作为所有打印机的默认队列。留空时用 lpstat -v 自动发现")
		// 声明但不在此处使用:--env 已在 main() 开头(loadEnvFile)按 os.Args 处理,
		// 这里登记一下,免得 flag.Parse 报「flag provided but not defined」。
		_ = flag.String("env", "", "agent.env 配置文件路径(默认取程序同目录)")
	)
	// -v 是 --version 的短别名:门店/运维习惯敲短参数,两个名字绑定同一个变量。
	flag.BoolVar(showVersion, "v", false, "输出版本、构建时间、Git 提交后退出(等价 --version)")
	flag.Parse()

	// --no-sleep 是否被显式指定(--no-sleep / --no-sleep=false 都算,环境变量也算):
	// 显式时安装不再交互询问,直接按该值注册自启动;未显式时 macOS 安装会问一句。
	noSleepExplicit := os.Getenv("PRINT_AGENT_NO_SLEEP") != ""
	// --server/--token 同理:只在本次命令行或 shell 环境变量显式给出时才算,
	// 值若来自已存在的 agent.env(见 snapshotEnv)则不算,以免重装时误改配置。
	serverExplicit := envSnapshot.server != ""
	tokenExplicit := envSnapshot.token != ""
	cmdLine := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		cmdLine[f.Name] = true
		switch f.Name {
		case "no-sleep":
			noSleepExplicit = true
		case "server":
			serverExplicit = true
		case "token":
			tokenExplicit = true
		}
	})

	// 命令行没给时,回退到环境变量(含已加载的 agent.env)。放在这里而不是 flag 默认值,
	// 就是为了不让它们出现在用法输出里 —— 见上面 server/token 的声明处。
	if !cmdLine["server"] {
		*server = envOr("PRINT_AGENT_SERVER", "")
	}
	if !cmdLine["token"] {
		*token = envOr("PRINT_AGENT_TOKEN", "")
	}

	cfg := config{
		server:          strings.TrimSpace(*server),
		token:           strings.TrimSpace(*token),
		interval:        time.Duration(*interval) * time.Second,
		limit:           *limit,
		wait:            *wait,
		name:            strings.TrimSpace(*name),
		state:           strings.TrimSpace(*state),
		logPath:         resolveLogPath(normalizeLogPath(*logPath)),
		insecure:        *insecure,
		once:            *once,
		verbose:         *verbose,
		showVersion:     *showVersion,
		install:         *install,
		uninstall:       *uninstall,
		upgrade:         *upgrade,
		probe:           strings.TrimSpace(*probe),
		probeTimeout:    time.Duration(*probeTimeout) * time.Second,
		probePrint:      *probePrint,
		printVia:        strings.TrimSpace(*printVia),
		cupsQueue:       strings.TrimSpace(*cupsQueue),
		noSleep:         *noSleep,
		noSleepExplicit: noSleepExplicit,
		serverExplicit:  serverExplicit,
		tokenExplicit:   tokenExplicit,
		doctor:          *doctor,
		setupCUPS:       *setupCUPS,
	}
	if cfg.showVersion {
		return cfg, nil
	}
	// --doctor 必须排在 --probe 之前:体检允许顺带带上 --probe,若先命中下面那个分支
	// 就只剩打印机那一段,代理自身的配置/自启动/防睡眠全被跳过 —— 那就不是体检了。
	if cfg.doctor {
		// 体检不要求 --server/--token:配置缺失正是要查出来的问题之一。
		normalizeChannelOrDefault(&cfg)
		normalizeProbeTimeout(&cfg)
		return cfg, nil
	}
	// --setup-cups 同理:它只在「打不出纸」时才用,那时配置可能正好是坏的。
	if cfg.setupCUPS != "" {
		normalizeChannelOrDefault(&cfg)
		return cfg, nil
	}
	// --probe-print 是 --probe 的修饰开关,单独给出来会「什么都不探测」却照常进入
	// 常驻模式 —— 那等于在门店机器上又起一个代理实例,与开机自启的那个抢同一批任务,
	// 表现为小票被打两遍。这里直接拦掉,不给这个手滑的机会。
	if cfg.probePrint && cfg.probe == "" {
		return cfg, errors.New("--probe-print 需要与 --probe <打印机IP> 一起使用(单独使用会进入常驻模式,再起一个代理实例)")
	}
	if cfg.probe != "" {
		// 连通性自检是纯本地操作(拨 IP:9100),与云端地址/令牌无关:
		// 断网时更要能查内网这一段,故跳过 server/token 校验。
		normalizeProbeTimeout(&cfg)
		normalizeChannelOrDefault(&cfg)
		return cfg, nil
	}
	if cfg.install || cfg.uninstall {
		// 安装/卸载阶段 server/token 尚未从命令行或环境变量给出属正常:
		// install 会在 runInstall 里交互补齐,uninstall 根本用不到。这里直接放行,
		// 否则「缺少 --server」的常规校验会挡住一条命令安装。
		return cfg, nil
	}
	if cfg.once {
		cfg.wait = 0
		cfg.verbose = true
	}
	if cfg.wait < 0 {
		cfg.wait = 0
	}
	if cfg.wait > 30 {
		cfg.wait = 30
	}
	if cfg.state == "" {
		// 默认落在运行数据目录,便于「一个文件夹打包部署」:exe + agent.env + print-agent.state。
		cfg.state = filepath.Join(dataDir(), "print-agent.state")
	}
	if cfg.server == "" {
		return cfg, errors.New("缺少 --server(云端地址),例如 --server https://dining.example.com")
	}
	if u, err := url.Parse(cfg.server); err != nil || u.Scheme == "" || u.Host == "" {
		return cfg, fmt.Errorf("--server 不是合法地址: %q(应形如 https://dining.example.com)", cfg.server)
	}
	if cfg.token == "" {
		return cfg, errors.New("缺少 --token(代理令牌),请在管理端「系统配置 → 小票打印」生成并填入")
	}
	if cfg.interval < time.Second {
		cfg.interval = time.Second
	}
	if cfg.limit < 1 {
		cfg.limit = 1
	}
	if cfg.limit > 50 {
		cfg.limit = 50
	}
	if cfg.name == "" {
		if h, err := os.Hostname(); err == nil {
			cfg.name = h
		} else {
			cfg.name = "print-agent"
		}
	}
	// 通道校验放在最后:--version / --probe / --install / --uninstall 都在上面提前返回了,
	// 它们用不到通道配置,不该因为一个拼错的 print-via 而被挡住。
	via, err := normalizePrintVia(cfg.printVia)
	if err != nil {
		return cfg, err
	}
	cfg.printVia = via
	return cfg, nil
}

// newHTTPClient 构造 HTTP 客户端。
// normalizeChannelOrDefault 归一化打印通道;取值非法时退回 tcp 且不报错。
//
// 专给排障类命令(--probe / --doctor / --setup-cups)用:它们本身不该因为一个配错的
// 通道取值而失败,何况「当前通道是什么、队列找到没有」正是它们要报告的结论。
func normalizeChannelOrDefault(cfg *config) {
	if via, err := normalizePrintVia(cfg.printVia); err == nil {
		cfg.printVia = via
	} else {
		cfg.printVia = channelTCP
	}
}

// normalizeProbeTimeout 把自检拨号超时收敛到 1-30 秒。
func normalizeProbeTimeout(cfg *config) {
	if cfg.probeTimeout < time.Second {
		cfg.probeTimeout = time.Second
	}
	if cfg.probeTimeout > 30*time.Second {
		cfg.probeTimeout = 30 * time.Second
	}
}

func newHTTPClient(insecure bool) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 由 --insecure 显式开启
	}
	return &http.Client{Timeout: httpTimeout, Transport: tr}
}

// envFilePath 返回 agent.env 的查找路径:优先 --env 指定的,否则取运行数据目录。
// 在 flag.Parse 之前调用,故这里手写一遍参数扫描(不能依赖 flag 包已解析)。
func envFilePath() string {
	for i, a := range os.Args {
		if (a == "--env" || a == "-env") && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--env="); ok {
			return v
		}
	}
	return filepath.Join(dataDir(), "agent.env")
}

// ============================================================================
// 运行数据目录
// ============================================================================

// dataDir 返回运行数据目录 —— agent.env / 状态文件 / 日志的默认落点。
//
// 三种情形:
//   - PRINT_AGENT_DATA_DIR 显式指定 → 用它;
//   - macOS .app bundle 安装(见 install.go)→ ~/Library/Application Support/PrintAgent。
//     可变数据绝不能写进 bundle:macOS 的代码签名会校验 bundle 内容,往里写日志
//     等于自毁签名,而本地网络权限正是按代码签名识别程序身份的(TN3179);
//   - 其余(Windows / Linux / 直接解压运行)→ 程序同目录,与既有部署形态完全一致。
func dataDir() string {
	exe, err := os.Executable()
	if err != nil {
		if d := strings.TrimSpace(os.Getenv("PRINT_AGENT_DATA_DIR")); d != "" {
			return d
		}
		return "."
	}
	return dataDirFor(exe)
}

// dataDirFor 是 dataDir 的显式入参版本:安装时程序路径会变(部署进 bundle),
// 要在写入 agent.env 之前先按「未来的程序路径」算出目录,故必须能指定 exe。
func dataDirFor(exe string) string {
	if d := strings.TrimSpace(os.Getenv("PRINT_AGENT_DATA_DIR")); d != "" {
		return d
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	if d, ok := bundleDataDir(exe); ok {
		return d
	}
	return filepath.Dir(exe)
}

// bundleDataDir 判断可执行文件是否位于 macOS .app bundle 内,是则返回它的数据目录。
//
// 判定条件刻意收得很紧(必须是 .../X.app/Contents/MacOS/<exe>),免得把某个恰好叫
// Contents/MacOS 的普通目录误判成 bundle,把门店的 agent.env 挪到别处找不到。
func bundleDataDir(exe string) (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	macosDir := filepath.Dir(exe)
	contentsDir := filepath.Dir(macosDir)
	if filepath.Base(macosDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" {
		return "", false
	}
	if !strings.HasSuffix(filepath.Base(filepath.Dir(contentsDir)), ".app") {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, "Library", "Application Support", darwinDataDirName), true
}

// agentEnvPath 返回安装时写入 agent.env 的路径(与 envFilePath 的默认值一致)。
func agentEnvPath() string {
	return filepath.Join(dataDir(), "agent.env")
}

// loadEnvFile 读取 KEY=VALUE 配置文件并写入环境变量(已存在的变量不覆盖)。
//
// 为什么要有它:Windows 下把 exe 和 agent.env 放同一个文件夹、双击就能跑,
// 不必去配系统环境变量或改服务的命令行参数 —— 门店店长自己就能维护。
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // 文件不存在属正常情况,静默跳过
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// 去掉可能的引号(路径/令牌里带空格时用得上)
		v = strings.Trim(v, `"'`)
		if k == "" || os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
}

// normalizeLogPath 把「off/none/false/0/-」等关闭标志统一为空串(不落盘),
// 其余原样返回 —— 与后端 LOG_PATH 的关闭语义保持一致。
func normalizeLogPath(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "off", "none", "false", "0", "-":
		return ""
	}
	return strings.TrimSpace(p)
}

// resolveLogPath 把相对日志路径锚定到运行数据目录(绝对路径原样返回)。
//
// 为什么需要它:守护进程的当前工作目录不由我们决定 —— launchd 是 /、
// Windows 任务计划是 System32、systemd 取决于 WorkingDirectory。而
// --install 默认写入的 PRINT_AGENT_LOG=print-agent.log 是相对路径,在
// 这些环境下会解析到只读目录而打开失败,日志全部丢失(只剩一句告警)。
// 锚定到数据目录(见 dataDir:普通部署就是 exe 同目录,bundle 部署是
// Application Support)后,「程序 + agent.env + print-agent.log 一个文件夹」
// 的默认部署形态在任何守护方式下都成立,与 state 文件的默认策略一致。
func resolveLogPath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dataDir(), p)
}

// envOr 取环境变量,为空时返回默认值。
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// envInt 取整型环境变量,非法或缺失时返回默认值。
func envInt(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return v
	}
	return def
}

func printVersion() {
	fmt.Printf("version=%s\nbuildTime=%s\ngitCommit=%s\n", version, buildTime, gitCommit)
}
