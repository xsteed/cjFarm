package main

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ============================================================================
// 一条命令安装 / 卸载
//
// 为什么要有这一块:门店店员没有能力手写 agent.env、改三平台自启动模板、再
// 手动注册服务。把「单文件拷过去 → 一条命令」做成内置能力后,安装门槛从
// 「照着文档改一堆文件」降到「双击/敲一行命令」,这才是门店能真正自助完成的交付。
// ============================================================================

//go:embed deploy/*
var deployFS embed.FS

// 三平台自启动的注册名/路径。集中定义便于测试与日后调整。
const (
	darwinPlistPath = "com.cjfarm.print-agent.plist"
	linuxUnitName   = "print-agent.service"
	windowsTaskName = "DiningPrintAgent"
)

// macOS 专属常量:安装形态与数据落点。
const (
	// darwinBundleName 部署到 ~/Applications 下的 bundle 名。
	darwinBundleName = "PrintAgent.app"
	// darwinBundleID bundle 标识。launchd plist 的 AssociatedBundleIdentifiers 与
	// Info.plist 的 CFBundleIdentifier 必须一致 —— 系统靠它把「本地网络」授权的
	// 责任主体定位到这个 App(原因见 deployDarwinBundle 注释)。
	darwinBundleID = "com.cjfarm.print-agent"
	// darwinBundleExecName bundle 内可执行文件的文件名(与 CFBundleExecutable 对齐)。
	darwinBundleExecName = "print-agent"
	// darwinDataDirName Application Support 下的数据目录名:agent.env / 状态 / 日志。
	// 刻意放在 bundle 之外 —— 见 dataDir 的注释。
	darwinDataDirName = "PrintAgent"
)

// stdinReader 复用同一个 bufio.Reader:避免管道一次喂入多行时,askServer 与
// askToken 各自新建 reader 导致前者预读缓冲里的后续行被丢掉。
var stdinReader = bufio.NewReader(os.Stdin)

// runInstall 写 agent.env 并注册系统自启动。
func runInstall(cfg config) {
	exe, err := executableAbsPath()
	if err != nil {
		fatalf("无法确定程序自身路径: %v", err)
	}

	// 优先级:--server/--token flag → 环境变量 → 交互询问。
	// parseFlags 已把 flag 与环境变量合并进 cfg,这里只补真正缺失的项。
	// serverExplicit/tokenExplicit 表示「本次显式给出」;交互补输的也算显式 —
	// 店员当场敲进去的值,当然是要它生效的那个值。
	server, token := cfg.server, cfg.token
	serverGiven, tokenGiven := cfg.serverExplicit, cfg.tokenExplicit
	if server == "" {
		server = askServer()
		serverGiven = true
	}
	if token == "" {
		token = askToken()
		tokenGiven = true
	}

	// 先定「实际会被拉起的程序路径」再定配置文件路径:macOS 上这一步会把自身部署进
	// ~/Applications/PrintAgent.app,程序路径随之改变,agent.env 也跟着落到该程序的
	// 数据目录 —— 顺序反了就会写到一个新程序根本不会去读的位置。
	runExe := prepareInstallTarget(exe)
	dataDir := dataDirFor(runExe)
	envPath := filepath.Join(dataDir, "agent.env")
	migrateFromOldDir(filepath.Dir(exe), dataDir)

	updated, err := writeAgentEnv(envPath, server, token, serverGiven, tokenGiven)
	if err != nil {
		fatalf("写 agent.env 失败: %v", err)
	}
	if len(updated) > 0 {
		logf("[提示] 已按本次输入更新 %s(旧值不再生效)", strings.Join(updated, "、"))
	}

	if err := installAutostart(runExe, cfg); err != nil {
		logf("[警告] 自启动注册未完全成功: %v", err)
	}

	platform := platformName(runtime.GOOS)
	logf("安装完成(%s):程序 %s,配置文件 %s", platform, runExe, envPath)
	logf("自检命令: %s --once", runExe)
	logf("卸载命令: %s --uninstall", runExe)
}

// runUninstall 移除系统自启动,但保留 agent.env(卸载≠重置配置)。
func runUninstall() {
	switch runtime.GOOS {
	case "darwin":
		uninstallDarwin()
	case "windows":
		uninstallWindows()
	case "linux":
		uninstallLinux()
	default:
		logf("[提示] 当前平台 %s 无内置卸载逻辑,请参考 deploy/ 模板手工移除", runtime.GOOS)
	}
	logf("卸载完成(%s);agent.env 已保留(卸载≠重置配置,重新安装请再次执行 --install)", platformName(runtime.GOOS))
}

// ============================================================================
// 安装目录与配置
// ============================================================================

// executableAbsPath 返回程序自身的绝对路径。
func executableAbsPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

// writeAgentEnv 以「合并语义」写 agent.env:文件不存在时按示例配置风格新生成;
// 已存在时只补齐缺失的 SERVER/TOKEN/LOG,已有键值默认保留。
//
// overrideServer/overrideToken 为 true 表示本次安装显式给出了 SERVER/TOKEN,
// 此时就地改写对应行,并返回被改写的键名(仅在值真的变化时算);未显式时沿用旧值。
// 权限 0600(内含令牌)。
func writeAgentEnv(path, server, token string, overrideServer, overrideToken bool) ([]string, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	content, updated := buildAgentEnv(string(existing), server, token, overrideServer, overrideToken)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, err
	}
	return updated, nil
}

// buildAgentEnv 合并/生成 agent.env 内容。existing 为空表示新文件。
//
// 返回 (内容, 被本次覆盖的键名)。为什么要在「保留旧值」的默认上开一个覆盖口子:
// 门店第二次安装(换服务器、重发令牌)时,若命令行明确传了却被旧配置静默吞掉,
// 表现是「安装完成、日志却一直报令牌不正确」,隔着电话根本排不出来。反过来,
// 不带参数重装就是想沿用旧配置,这时一个字都不许改。
func buildAgentEnv(existing, server, token string, overrideServer, overrideToken bool) (string, []string) {
	if strings.TrimSpace(existing) == "" {
		return newAgentEnv(server, token), nil
	}

	content := existing
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	// 只识别「未注释的 KEY=」行:被 # 注释掉的 SERVER/TOKEN 视为缺失,仍会补齐。
	var updated []string
	hasServer, hasToken, hasLog := false, false, false
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PRINT_AGENT_SERVER":
			hasServer = true
			if overrideServer && setEnvLine(lines, i, "PRINT_AGENT_SERVER", server) {
				updated = append(updated, "PRINT_AGENT_SERVER")
			}
		case "PRINT_AGENT_TOKEN":
			hasToken = true
			if overrideToken && setEnvLine(lines, i, "PRINT_AGENT_TOKEN", token) {
				updated = append(updated, "PRINT_AGENT_TOKEN")
			}
		case "PRINT_AGENT_LOG":
			hasLog = true
		}
	}

	var sb strings.Builder
	sb.WriteString(strings.Join(lines, "\n"))
	if !hasServer || !hasToken || !hasLog {
		sb.WriteString("\n# 以下由 --install 补齐\n")
	}
	if !hasServer {
		sb.WriteString("PRINT_AGENT_SERVER=")
		sb.WriteString(server)
		sb.WriteString("\n")
	}
	if !hasToken {
		sb.WriteString("PRINT_AGENT_TOKEN=")
		sb.WriteString(token)
		sb.WriteString("\n")
	}
	if !hasLog {
		// 日志落盘是排障底线(Windows 任务计划运行没有它日志无人接收),
		// 旧版生成的 agent.env 在重新 --install 时自动补上。
		sb.WriteString("PRINT_AGENT_LOG=print-agent.log\n")
	}
	return sb.String(), updated
}

// setEnvLine 就地改写某一行的值;值与目标相同时返回 false(不算「已更新」)。
func setEnvLine(lines []string, i int, key, value string) bool {
	want := key + "=" + value
	if strings.TrimSpace(lines[i]) == want {
		return false
	}
	lines[i] = want
	return true
}

// newAgentEnv 按 agent.env.example 的注释风格生成新文件:
// SERVER/TOKEN 写实际值,其余可选变量注释掉。
func newAgentEnv(server, token string) string {
	return "# 门店本地打印代理 — 配置文件\n" +
		"# 由 print-agent --install 自动生成;修改后重启代理(卸载后再 --install,或重启系统服务)生效。\n" +
		"# 完整部署手册见 docs/print-agent.md。\n\n" +
		"# 云端地址:浏览器里打开管理后台的地址(不要带 /prod-api 和结尾斜杠)\n" +
		"PRINT_AGENT_SERVER=" + server + "\n\n" +
		"# 代理令牌:管理端「系统配置 → 小票打印 → 本地打印代理 → 代理令牌」生成。\n" +
		"PRINT_AGENT_TOKEN=" + token + "\n\n" +
		"# 以下为可选配置,默认值已能满足大多数门店,需要时取消注释修改。\n" +
		"# 轮询/长轮询等待秒数(默认 25):门店网络差时可设为 0 退回纯轮询。\n" +
		"# PRINT_AGENT_WAIT=25\n\n" +
		"# 轮询间隔(秒,默认 3):长轮询模式下仅作为兜底节奏。\n" +
		"# PRINT_AGENT_INTERVAL=3\n\n" +
		"# 单次最多取几条任务(默认 10,上限 50),一般无需修改。\n" +
		"# PRINT_AGENT_LIMIT=10\n\n" +
		"# 代理标识:多台代理时可区分是谁取的;默认取主机名。\n" +
		"# PRINT_AGENT_NAME=门店收银电脑\n\n" +
		"# 仅当云端用自签证书(浏览器提示不安全)时才需要打开\n" +
		"# PRINT_AGENT_INSECURE=1\n\n" +
		"# 本地状态文件(幂等去重):默认取运行数据目录 print-agent.state,一般无需修改。\n" +
		"# PRINT_AGENT_STATE=print-agent.state\n\n" +
		"# 日志文件:排障时看这个文件。Windows 任务计划运行必须有它(否则日志无人接收);\n" +
		"# 设为 off 可关闭落盘(仅控制台)。\n" +
		"PRINT_AGENT_LOG=print-agent.log\n\n" +
		"# 打印通道(默认 tcp=直连打印机 IP:9100,一般不用改)。\n" +
		"# 仅当 macOS 15+ 报「无法连接打印机 ... no route to host」、而手工执行 --probe 却能通时\n" +
		"# 才需要它 —— 那是系统的「本地网络」隐私权限拦下了自启动的代理。\n" +
		"# 处置:在「系统设置 → 打印机与扫描仪」以 IP 方式添加本店打印机后,把下面一行\n" +
		"# 改成 auto(仍优先直连,被拦时自动改投系统打印服务):\n" +
		"# PRINT_AGENT_PRINT_VIA=auto\n" +
		"# 队列名能自动发现;需要手填时:\n" +
		"# PRINT_AGENT_CUPS_QUEUE=192.168.1.133=厨房打印机\n\n" +
		"# 运行数据目录(agent.env / 状态 / 日志的落点),macOS 装成 App 后默认在\n" +
		"# ~/Library/Application Support/PrintAgent,一般无需修改。\n" +
		"# PRINT_AGENT_DATA_DIR=/path/to/data\n\n" +
		"# macOS 盒盖防睡眠(可选项,默认关):=1 时下次 --install 用 caffeinate 顶住空闲睡眠。\n" +
		"# 注意:仅此**不足以**合盖继续打单 —— caffeinate 挡不住 Clamshell Sleep,\n" +
		"# 还要另设一个系统开关(需管理员密码,一次性):\n" +
		"#   sudo pmset -a disablesleep 1\n" +
		"# 不填时 --install 会交互询问。Windows/Linux 忽略本项。\n" +
		"# PRINT_AGENT_NO_SLEEP=1\n"
}

// ============================================================================
// 交互询问
// ============================================================================

// askServer 交互询问云端地址,直到输入合法;无法交互(如管道已结束)时直接失败退出。
func askServer() string {
	for {
		s, err := prompt("请输入云端地址(如 https://dining.example.com): ")
		if err != nil {
			fatalf("无法交互读取云端地址: %v;请改用 --server 指定", err)
		}
		if s == "" {
			fmt.Println("地址不能为空,请重新输入")
			continue
		}
		if u, err := url.Parse(s); err != nil || u.Scheme == "" || u.Host == "" {
			fmt.Println("地址不合法,请重新输入(应形如 https://dining.example.com)")
			continue
		}
		return s
	}
}

// askToken 交互询问代理令牌,直到输入非空。
func askToken() string {
	for {
		tok, err := readTokenHidden()
		if err != nil {
			fatalf("无法交互读取代理令牌: %v;请改用 --token 指定", err)
		}
		if tok != "" {
			return tok
		}
		fmt.Println("令牌不能为空,请重新输入")
	}
}

// readTokenHidden 读取令牌;终端环境下临时关闭回显,避免令牌被旁人瞟见。
func readTokenHidden() (string, error) {
	echoOff := false
	if runtime.GOOS != "windows" && isTerminal(os.Stdin) {
		if err := sttyMode("-echo"); err == nil {
			echoOff = true
		} else {
			fmt.Println("提示: 无法关闭终端回显,令牌将在屏幕上可见")
		}
	}
	fmt.Print("请输入代理令牌: ")
	line, err := stdinReader.ReadString('\n')
	if echoOff {
		_ = sttyMode("echo")
	}
	fmt.Println()
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// prompt 输出提示并读取一行;读到 EOF 且无内容时返回错误,避免非交互环境下死循环。
func prompt(text string) (string, error) {
	fmt.Print(text)
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// isTerminal 判断文件是否为终端(用于决定是否调用 stty 关回显)。
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// sttyMode 包装 stty 命令;必须把 stdin 指到终端,否则 stty 拿不到终端设置。
func sttyMode(mode string) error {
	cmd := exec.Command("stty", mode)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// ============================================================================
// macOS .app bundle 部署
// ============================================================================

// darwinBundlePath 返回 bundle 的安装位置(~/Applications/PrintAgent.app)。
//
// 选 ~/Applications 而不是 /Applications:门店电脑不一定要有管理员密码,
// 装在用户目录下一条命令就能完成,也不需要 sudo。
func darwinBundlePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Applications", darwinBundleName), nil
}

// darwinBundleExePath 返回 bundle 内可执行文件的路径。
func darwinBundleExePath(app string) string {
	return filepath.Join(app, "Contents", "MacOS", darwinBundleExecName)
}

// prepareInstallTarget 返回「实际会被自启动拉起的程序路径」。
//
// 除 macOS 外都是原样返回(单文件部署形态不变);macOS 上会先把自身部署进
// ~/Applications/PrintAgent.app,失败则退回裸二进制 —— 后者在 macOS 15+ 上可能被
// 「本地网络」权限拦下,但至少代理还在跑,不会因为一个可选的安装优化而装不上。
func prepareInstallTarget(exe string) string {
	if runtime.GOOS != "darwin" {
		return exe
	}
	appExe, err := deployDarwinBundle(exe)
	if err != nil {
		logf("[警告] 部署 macOS App(macOS 本地网络权限需要它)失败: %v", err)
		logf("[警告] 已退回直接运行二进制;若日志出现「无法连接打印机 ... no route to host」," +
			"请按部署手册 macOS 一节处置(改用 CUPS 通道或给本地网络授权)")
		return exe
	}
	return appExe
}

// deployDarwinBundle 把当前可执行文件部署成 ~/Applications/PrintAgent.app,
// 返回 bundle 内可执行文件的绝对路径。
//
// 为什么必须做成 .app(而不是把二进制直接丢给 LaunchAgent):
//
//	macOS 15 起,连接局域网地址需要「本地网络」授权,而授权是**按代码签名识别
//	程序身份**记录的。Apple TN3179 写明:launchd daemon 自动放行,但 launchd agent
//	不在此列 —— 不使用 SMAppService 安装的 agent,必须在自己 launchd plist 里声明
//	AssociatedBundleIdentifiers,系统才能定位 responsible code。没有 bundle 时系统
//	找不到可授权的责任主体:既不弹授权窗口,也不会在「系统设置 → 隐私与安全性 →
//	本地网络」里留下条目,连接被直接拒绝,报错为
//
//	    connect: no route to host
//
//	与「打印机没开机」完全同形,门店只能靠猜。装进 bundle 后这一条才有解。
//
// 部署流程刻意把「写文件」全部做完再签名:签名之后再动 bundle 里任何文件都会让
// 签名失效,而身份识别正依赖它。
func deployDarwinBundle(exe string) (string, error) {
	app, err := darwinBundlePath()
	if err != nil {
		return "", err
	}
	target := darwinBundleExePath(app)

	// 已经在 bundle 内(由 launchd 拉起的实例再执行 --install):原地不动,只重签一次
	// 以防上一次被升级流程改过文件。
	if samePath(exe, target) {
		signDarwinBundle(app)
		return target, nil
	}

	for _, d := range []string{filepath.Join(app, "Contents", "MacOS"), filepath.Join(app, "Contents", "Resources")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	if err := copyFile(exe, target, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"),
		[]byte(renderDarwinInfoPlist(deployTemplate("deploy/PrintAgent-Info.plist"))), 0o644); err != nil {
		return "", err
	}
	signDarwinBundle(app)
	return target, nil
}

// renderDarwinInfoPlist 把模板里的 __VERSION__ 换成当前版本号。
//
// CFBundleShortVersionString 只接受数字与点(如 1.0.0),开发态构建的 version 是
// "dev",直接写进去会让系统认不出这个 App,故过滤成兜底版本号。
func renderDarwinInfoPlist(tpl string) string {
	v := version
	if !isNumericVersion(v) {
		v = "0.0.0"
	}
	return strings.ReplaceAll(tpl, "__VERSION__", v)
}

// isNumericVersion 判断版本号是否是「数字与点」组成(cf. CFBundleShortVersionString)。
func isNumericVersion(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// signDarwinBundle 对 bundle 做 ad-hoc 签名,失败只告警。
//
// 为什么即便没有付费证书也要签:本地网络隐私依赖代码签名追踪程序身份,完全未签名的
// bundle 在这条链路上行为不确定。ad-hoc(--sign -)至少让 bundle 有一份完整的
// code directory,系统能把它当成一个可识别的 App。签名失败不影响代理运行 ——
// 只是本地网络授权可能仍然拿不到,那时按手册改用 CUPS 通道。
func signDarwinBundle(app string) {
	if _, err := os.Stat(app); err != nil {
		return
	}
	cmd := exec.Command("codesign", "--force", "--sign", "-", "--identifier", darwinBundleID, app)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			logf("[提示] App 签名未完成(不影响运行,但 macOS 可能不显示「本地网络」授权项): %v(%s)", err, msg)
		} else {
			logf("[提示] App 签名未完成(不影响运行,但 macOS 可能不显示「本地网络」授权项): %v", err)
		}
		return
	}
	logvf("已对 %s 完成 ad-hoc 签名", app)
}

// migrateFromOldDir 把旧数据目录(程序同目录)里的配置与去重状态搬到新数据目录。
//
// 两个文件都要搬,而且 **state 必须搬**:
//   - agent.env:门店重装时用的往往就是「换个目录解压再 install」,旧目录里的令牌
//     没有任何理由让店员再填一遍;
//   - print-agent.state:它记录「哪些任务已经打过了」。只换目录不搬它,历史就断了 ——
//     云端若重发同一条任务(租约到期重试、人工补打),小票会被重复打出。这是门店
//     当场就能看见的故障,比多填一次令牌严重得多。
//
// 只在新位置尚无对应文件时搬:新位置那份才是权威副本(见 writeAgentEnv 的合并语义)。
func migrateFromOldDir(oldDir, newDir string) {
	if oldDir == "" || newDir == "" || samePath(oldDir, newDir) {
		return
	}
	migrateFile(filepath.Join(oldDir, "agent.env"), filepath.Join(newDir, "agent.env"), "配置文件")
	migrateFile(filepath.Join(oldDir, "print-agent.state"), filepath.Join(newDir, "print-agent.state"), "去重状态")
}

// migrateFile 把 oldPath 复制到 newPath(源不存在或目标已存在时什么都不做)。
func migrateFile(oldPath, newPath, what string) {
	if samePath(oldPath, newPath) {
		return
	}
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return
	}
	// 0600:两个文件都可能含敏感内容(令牌 / 已打单据号)。
	if err := os.WriteFile(newPath, data, 0o600); err != nil {
		return
	}
	logf("[提示] 已沿用原有%s:%s → %s", what, oldPath, newPath)
}

// copyFile 复制文件并设置权限。先写临时文件再 rename,避免 KeepAlive 的 launchd
// 在文件只写了一半时把新进程拉起来(读到半个可执行文件会直接崩溃)。
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// samePath 判断两个路径是否指向同一个文件(解析符号链接后比较)。
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

// ============================================================================
// 自启动注册
// ============================================================================

// installAutostart 按平台分派自启动注册。
func installAutostart(exe string, cfg config) error {
	switch runtime.GOOS {
	case "darwin":
		return installDarwin(exe, cfg)
	case "windows":
		return installWindows(exe)
	case "linux":
		return installLinux(exe)
	default:
		return fmt.Errorf("当前平台 %s 暂不支持自动注册自启动,请参考 deploy/ 模板手工部署", runtime.GOOS)
	}
}

// installDarwin 渲染 LaunchAgent 并交给 launchctl 加载。
//
// 盒盖防睡眠是安装时的可选项(noSleep):门店选「是」时用 caffeinate -i -s 包装代理,
// 顶住空闲睡眠;选「否」时与原有单进程形态一致,下班合盖睡觉省电。未显式指定
// (--no-sleep / PRINT_AGENT_NO_SLEEP)时交互询问,便于门店店员自助决定。
//
// 这里刻意**不**把「合盖继续运行」说成选「是」就够了:caffeinate 挡不住 Clamshell
// Sleep,还得关掉系统的合盖睡眠开关(需 root)。装完由 reportDarwinLidClosedReadiness
// 核对并如实告知缺口。
func installDarwin(exe string, cfg config) error {
	tpl := deployTemplate("deploy/print-agent.plist")
	if tpl == "" {
		return errors.New("内嵌的 macOS LaunchAgent 模板缺失")
	}

	noSleep := cfg.noSleep
	if !cfg.noSleepExplicit {
		noSleep = askNoSleep()
	}
	// AssociatedBundleIdentifiers 必须补上:它是「本地网络」授权能被授予的前提
	// (launchd agent 不在 daemon 的自动放行范围内,详见 deployDarwinBundle)。
	content := withAssociatedBundleIdentifiers(renderDarwinPlist(tpl, exe, noSleep), darwinBundleID)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", darwinPlistPath)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return err
	}
	// 重复安装时旧 plist 可能已在运行:先卸载再加载,避免 launchd 报重复注册。
	// 首次安装时这个 job 还不在 launchd 注册列表里,legacy launchctl 会把「找不到目标」
	// 报成 「Unload failed: 5: Input/output error」(退出码仍是 0),纯粹是历史遗留的
	// 兜底错误码。先把话说在前头,免得门店盯着这行字,漏掉后面真正的报错。
	logf("[提示] 正在清理同名旧自启动项;若紧接着出现 \"Unload failed: 5: Input/output error\",表示此前未注册过该任务(首次安装常见),属正常现象")
	_ = runCommand(launchctlCommand("unload", plistPath))
	if err := runCommand(launchctlCommand("load", plistPath)); err != nil {
		return err
	}
	// 装完立刻核对「合盖继续运行」是否真的成立。caffeinate 单独做不到这件事,
	// 必须把缺口和补法明确说出来(见 reportDarwinLidClosedReadiness)。
	reportDarwinLidClosedReadiness(noSleep)
	return nil
}

// ============================================================================
// macOS 合盖继续运行:状态核对
// ============================================================================

// darwinSleepState 与「合盖后代理能否继续运行」直接相关的电源状态。
type darwinSleepState struct {
	// known 表示成功读到硬部件状态。读不到时一律不报警 —— 把「查不到」说成「没配」
	// 会让门店白跑一趟。
	known bool
	// sleepDisabled 对应 pmset 的 disablesleep(即 IOPMrootDomain 的 SleepDisabled)。
	// 只有它为 true,合盖才不触发 Clamshell Sleep。
	sleepDisabled bool
	// onBattery 当前是否靠电池供电。
	onBattery bool
}

// readDarwinSleepState 免 sudo 读取电源状态。
//
// 用 ioreg 而不是 pmset 读:读 disablesleep 需要 root,而 IOPMrootDomain 的
// SleepDisabled 属性普通用户就能读 —— 而安装与排障都是由门店用户执行的,拿不到 sudo。
func readDarwinSleepState() darwinSleepState {
	var st darwinSleepState
	if out, err := runExternal([]string{"ioreg", "-r", "-c", "IOPMrootDomain", "-d", "1"}); err == nil {
		st.known = true
		st.sleepDisabled = strings.Contains(string(out), `"SleepDisabled" = Yes`)
	}
	if out, err := runExternal([]string{"pmset", "-g", "batt"}); err == nil {
		st.onBattery = strings.Contains(string(out), "Battery Power")
	}
	return st
}

// darwinDisableSleepCommand / darwinEnableSleepCommand 合盖睡眠开关的命令行。
func darwinDisableSleepCommand() string { return "sudo pmset -a disablesleep 1" }
func darwinEnableSleepCommand() string  { return "sudo pmset -a disablesleep 0" }

// ioregSleepDisabledHint 免 sudo 核对合盖睡眠开关的一行命令(背景见 readDarwinSleepState)。
const ioregSleepDisabledHint = `ioreg -r -c IOPMrootDomain -d 1 | grep SleepDisabled   # 期望 "SleepDisabled" = Yes`

// reportDarwinLidClosedReadiness 安装后核对「合盖继续运行」到底成不成立。
//
// 这是刻意加的一道「说实话」环节:门店勾了防睡眠就会以为合盖也能打单,而 caffeinate
// 的断言**挡不住合盖睡眠** —— 合盖触发的是 Clamshell Sleep,属于电源管理层的独立触发器。
// 真正的开关只有 `pmset disablesleep`,它需要 root,代理自己装不了,所以只能把缺口
// 明确交出去,而不是让门店在「以为配好了」的状态下丢单。
func reportDarwinLidClosedReadiness(noSleep bool) {
	st := readDarwinSleepState()

	if !noSleep {
		logf("[提示] 未启用防睡眠:合盖后系统会睡眠,代理停摆(唤醒后自动续上,不会丢单)")
		if st.known && !st.sleepDisabled {
			logf("[提示] 若需要「合盖后继续打单」,两件事都要做:")
			logf("[提示]   ① 关掉合盖睡眠(需管理员密码):%s", darwinDisableSleepCommand())
			logf("[提示]   ② 重新执行 --install,在询问「合盖后继续工作」时选 y(caffeinate 顶住空闲睡眠)")
		}
		return
	}

	logf("[提示] 已启用防睡眠:caffeinate -i -s 会顶住空闲睡眠,合盖后代理继续轮询")
	if !st.known {
		logf("[提示] 未能读取合盖睡眠开关,请手工核对:%s", ioregSleepDisabledHint)
	} else if st.sleepDisabled {
		logf("[提示] 合盖睡眠已关闭(SleepDisabled=Yes):合盖后系统不睡,配置完整")
	} else {
		logf("[警告] 合盖睡眠**还没关掉** —— caffeinate 挡不住它,现在合盖系统照样睡、代理停摆")
		logf("[警告] 请执行(需管理员密码,一次性):%s", darwinDisableSleepCommand())
		logf("[警告] 以后想恢复「合盖即睡」:%s", darwinEnableSleepCommand())
	}
	if st.onBattery {
		logf("[警告] 当前靠电池供电:合盖长时间运行会持续耗电并发热,门店请接交流电源")
	}
}

// askNoSleep 交互询问是否启用盒盖防睡眠;非交互环境(管道/重定向)默认关闭。
func askNoSleep() bool {
	ans, err := prompt("是否需要在 Mac 合盖后继续工作(代理运行期间顶住空闲睡眠,需插电源;合盖睡眠开关还要另行设置,装完会提示)?[y/N]: ")
	if err != nil {
		return false // 无法交互时按最保守的「不防睡眠」处理
	}
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// installWindows 渲染任务计划 XML,经临时文件导入 schtasks。
func installWindows(exe string) error {
	tpl := deployTemplate("deploy/print-agent-windows-task.xml")
	if tpl == "" {
		return errors.New("内嵌的 Windows 任务计划模板缺失")
	}
	content := renderWindowsTask(tpl, exe)

	// schtasks 的 /xml 只吃文件路径,必须落一个临时文件再导入。
	f, err := os.CreateTemp("", "print-agent-task-*.xml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return runCommand(schtasksCommand(tmp))
}

// installLinux 渲染 systemd 单元并安装;无 root 时不静默失败,打印等价 sudo 命令。
func installLinux(exe string) error {
	tpl := deployTemplate("deploy/print-agent.service")
	if tpl == "" {
		return errors.New("内嵌的 systemd 单元模板缺失")
	}
	content := renderSystemdUnit(tpl, exe)

	unitPath := filepath.Join("/etc/systemd/system", linuxUnitName)
	if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
		// 门店设备往往没有 root:把命令交给用户执行,而不是留下一句「权限不足」。
		logf("[提示] 写 %s 需要 root 权限,请手动执行以下三条命令:", unitPath)
		fmt.Println("sudo tee " + unitPath + " >/dev/null <<'EOF'")
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		fmt.Println("EOF")
		fmt.Println("sudo systemctl daemon-reload")
		fmt.Println("sudo systemctl enable --now print-agent")
		return fmt.Errorf("写 %s 失败(无 root),已打印手动安装命令", unitPath)
	}

	// 逐个尝试:daemon-reload 失败也要继续 enable,最后再汇总,便于一次看出全部问题。
	var firstErr error
	if err := runCommand(systemctlCommand("daemon-reload")); err != nil {
		firstErr = err
	}
	if err := runCommand(systemctlCommand("enable", "--now", "print-agent")); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// ============================================================================
// 卸载
// ============================================================================

// uninstallDarwin 卸载 LaunchAgent;重复卸载不报错。
func uninstallDarwin() {
	home, err := os.UserHomeDir()
	if err != nil {
		logf("[警告] 无法确定用户目录: %v", err)
		return
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", darwinPlistPath)
	_ = runCommand(launchctlCommand("unload", plistPath)) // 未加载时失败属正常,忽略
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		logf("[警告] 删除 %s 失败: %v", plistPath, err)
	}

	// 顺带移除部署到 ~/Applications 的 App:留着它只会让「系统设置 → 隐私与安全性 →
	// 本地网络」里残存一条指向已卸载程序的授权项,下次重装还得先去手工删掉才能重新授权。
	// agent.env(含令牌)留在 Application Support 里不动 —— 卸载 ≠ 重置配置。
	if app, aerr := darwinBundlePath(); aerr == nil {
		if _, serr := os.Stat(app); serr == nil {
			if rerr := os.RemoveAll(app); rerr != nil {
				logf("[警告] 删除 %s 失败: %v", app, rerr)
			} else {
				logf("[提示] 已移除 App: %s", app)
			}
		}
	}

	// 合盖睡眠开关是系统级设置,不是本程序的注册项,卸载时**不该**顺手改回去(可能
	// 门店另有用途),但必须把「它还开着」这件事交代清楚,否则这台 Mac 会一直不睡。
	if st := readDarwinSleepState(); st.known && st.sleepDisabled {
		logf("[提示] 合盖睡眠开关仍是关闭状态(SleepDisabled=Yes);如需恢复「合盖即睡」:%s",
			darwinEnableSleepCommand())
	}
}

// uninstallWindows 删除任务计划;任务不存在时提示即可,不算错误。
func uninstallWindows() {
	if err := runCommand([]string{"schtasks", "/delete", "/tn", windowsTaskName, "/f"}); err != nil {
		logf("[提示] 删除 Windows 计划任务失败(若任务已不存在可忽略): %v", err)
	}
}

// uninstallLinux 停用并删除 systemd 单元;无 root 时打印等价 sudo 命令。
func uninstallLinux() {
	_ = runCommand(systemctlCommand("disable", "--now", "print-agent")) // 服务未启用时失败属正常

	unitPath := filepath.Join("/etc/systemd/system", linuxUnitName)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		logf("[提示] 删除 %s 需要 root 权限,请手动执行: sudo rm %s", unitPath, unitPath)
	}
	if err := runCommand(systemctlCommand("daemon-reload")); err != nil {
		logf("[提示] daemon-reload 需要 root 权限,请手动执行: sudo systemctl daemon-reload")
	}
}

// ============================================================================
// 模板渲染
// ============================================================================

// renderDarwinPlist 把 ProgramArguments 整块替换掉:
//
//   - noSleep=false:替换为「仅 exe 绝对路径」单元素,与原有行为一致;
//   - noSleep=true :替换为「caffeinate -i -s 包装代理」四元素。
//
// 为什么用 caffeinate 包装:睡眠期间进程全停、网络断开,代理不再轮询,云端很快判离线。
// 但必须明确一点 —— caffeinate **挡不住合盖睡眠**:
//
//	合盖触发的是 Clamshell Sleep,属于电源管理层的独立触发器,IOPMAssertion
//	(caffeinate 用的就是它)抑制不了。实测电源日志里能直接看到接了电源照样睡:
//
//	  Entering Sleep state due to 'Clamshell Sleep':TCPKeepAlive=active Using AC (Charge:100%)
//
//	合盖后没有任何输入,空闲睡眠计时器也会到点,所以 caffeinate 仍然必要 —— 它负责
//	挡住「空闲睡眠」这一路。真正关掉合盖触发器的是 `pmset disablesleep`(需 root,
//	由安装脚本设置;见 reportDarwinLidClosedReadiness 的核对逻辑)。
//
// 用 -i -s 两个开关而不是只有 -s:
//   - -s 的断言 **仅在交流电源下有效**(man caffeinate 原文),只加 -s 时电池供电
//     仍然会空闲睡眠;
//   - -i 不限制电源,所以在电池下也拦得住空闲睡眠。
//
// 代理崩溃退出时 caffeinate 同步退出、断言释放,由 launchd KeepAlive 整体拉起,
// 与原有单进程形态的守护语义一致。
//
// agent.env 与程序同目录会被自动读取,无需再传 --server/--token。
func renderDarwinPlist(tpl, exe string, noSleep bool) string {
	const key = "<key>ProgramArguments</key>"
	idx := strings.Index(tpl, key)
	if idx < 0 {
		return tpl
	}
	rest := tpl[idx+len(key):]
	arrStart := strings.Index(rest, "<array>")
	if arrStart < 0 {
		return tpl
	}
	relEnd := strings.Index(rest[arrStart:], "</array>")
	if relEnd < 0 {
		return tpl
	}
	arrEnd := arrStart + relEnd + len("</array>")

	newArray := "<array>\n        <string>" + escapeXML(exe) + "</string>\n    </array>"
	if noSleep {
		newArray = "<array>\n" +
			"        <string>/usr/bin/caffeinate</string>\n" +
			"        <string>-i</string>\n" +
			"        <string>-s</string>\n" +
			"        <string>" + escapeXML(exe) + "</string>\n" +
			"    </array>"
	}
	return tpl[:idx+len(key)] + rest[:arrStart] + newArray + rest[arrEnd:]
}

// withAssociatedBundleIdentifiers 往 LaunchAgent plist 里补 AssociatedBundleIdentifiers。
//
// 这不是可选装饰。Apple TN3179 对 macOS 的本地网络隐私写得很明确:
//
//   - 由 launchd 启动的 **daemon** 自动获得本地网络访问权;
//   - 但 **agent** 不在豁免之列 —— 若不是用 SMAppService 安装的,就必须在 launchd
//     plist 里声明 AssociatedBundleIdentifiers,macOS 才能定位到 responsible code。
//
// 少了这一段,系统找不到可授权的责任主体:既不弹授权窗口,「系统设置 → 隐私与安全性
// → 本地网络」里也不会出现本 App,局域网连接会被直接拒绝。
//
// 插入位置选在 Label 的值之后(plist 的 dict 对键顺序无要求)。
//
// 幂等判定必须找**真正的键**而不是子串「AssociatedBundleIdentifiers」:模板顶部的
// 注释里就有这个词,按子串判会误认为已经写过,结果是永远不注入 —— 正是这个功能
// 要修的那个 bug 的翻版。
func withAssociatedBundleIdentifiers(tpl, bundleID string) string {
	if strings.Contains(tpl, "<key>AssociatedBundleIdentifiers</key>") {
		return tpl
	}
	const key = "<key>Label</key>"
	idx := strings.Index(tpl, key)
	if idx < 0 {
		return tpl
	}
	rest := tpl[idx+len(key):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		return tpl
	}
	at := idx + len(key) + end + len("</string>")
	block := "\n\n    <key>AssociatedBundleIdentifiers</key>\n" +
		"    <array>\n" +
		"        <string>" + escapeXML(bundleID) + "</string>\n" +
		"    </array>"
	return tpl[:at] + block + tpl[at:]
}

// renderSystemdUnit 替换 ExecStart 行。其余行(含 EnvironmentFile 的可选读取)保持原样。
func renderSystemdUnit(tpl, exe string) string {
	lines := strings.Split(tpl, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "ExecStart=") {
			lines[i] = "ExecStart=" + exe
		}
	}
	return strings.Join(lines, "\n")
}

// renderWindowsTask 替换 <Command> 的内容。
func renderWindowsTask(tpl, exe string) string {
	start := strings.Index(tpl, "<Command>")
	if start < 0 {
		return tpl
	}
	end := strings.Index(tpl, "</Command>")
	if end < 0 || end < start {
		return tpl
	}
	return tpl[:start+len("<Command>")] + escapeXML(exe) + tpl[end:]
}

// escapeXML 转义 XML 特殊字符。exe 路径一般不含这些字符,但多一层保护更稳。
func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// deployTemplate 读取内嵌模板;缺失时返回空串由调用方报错。
func deployTemplate(name string) string {
	data, err := deployFS.ReadFile(name)
	if err != nil {
		return ""
	}
	return string(data)
}

// ============================================================================
// 命令构造与执行
// ============================================================================

// schtasksCommand 返回创建 Windows 计划任务的 argv。
func schtasksCommand(xmlPath string) []string {
	return []string{"schtasks", "/create", "/tn", windowsTaskName, "/xml", xmlPath, "/f"}
}

// launchctlCommand 返回 launchctl load/unload 的 argv。
func launchctlCommand(verb, plist string) []string {
	return []string{"launchctl", verb, plist}
}

// systemctlCommand 返回 systemctl 的 argv。
func systemctlCommand(args ...string) []string {
	return append([]string{"systemctl"}, args...)
}

// runCommand 执行外部命令,结果落日志、失败不中断(由调用方决定是否汇总)。
func runCommand(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg != "" {
			logf("[命令] %s 失败: %v(%s)", strings.Join(argv, " "), err, msg)
		} else {
			logf("[命令] %s 失败: %v", strings.Join(argv, " "), err)
		}
		return err
	}
	if msg != "" {
		logf("[命令] %s 输出: %s", strings.Join(argv, " "), msg)
	}
	return nil
}

// platformName 把 GOOS 换成面向用户的中文平台名。
func platformName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}
