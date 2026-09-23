package main

// ============================================================================
// 一条命令体检(--doctor)
//
// 门店现场排障最费时间的不是「修」,而是「问」:电话里让店员逐条执行命令、再把输出
// 念回来,一轮就是十几分钟,而且常常漏关键项。--doctor 把「代理自己身上所有会出问题
// 的地方」一次查完,并直接给出处置命令 —— 让门店只需要会两件事:
//
//	./print-agent --doctor                     只查代理自身(不碰打印机)
//	./print-agent --doctor --probe 192.168.1.100   连打印机那一段一起查
//
// 设计约束:
//   - 不要求 --server/--token:配置缺失本身就是要查出来的问题之一;
//   - 结论有退出码:有「[问题]」项返回 1,便于脚本判断(注意与 --probe 的退出码
//     语义一致:有不可达就是非零);
//   - 只读:不改配置、不重启服务、不建队列(要改就用 --install / --setup-cups)。
// ============================================================================

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// doctor 分级计数。区分「信息」与「注意」:把中性事实也算成警告会让结论失去意义
// (门店只会记住「一堆红字」)。
type doctor struct {
	ok    int
	info  int
	warn  int
	fail  int
	lines []string // 需要单独回看的处置提示(结论里再列一遍)
}

func (d *doctor) good(format string, a ...interface{}) { d.ok++; logf("[正常] "+format, a...) }
func (d *doctor) note(format string, a ...interface{}) { d.info++; logf("[信息] "+format, a...) }
func (d *doctor) hint(format string, a ...interface{}) {
	d.warn++
	logf("[注意] "+format, a...)
}
func (d *doctor) bad(format string, a ...interface{}) {
	d.fail++
	logf("[问题] "+format, a...)
}

// runDoctor 执行体检并返回进程退出码(有 [问题] → 1)。
func runDoctor(cfg config) int {
	d := &doctor{}
	logf("===== 打印代理体检 =====")

	checkIdentity(d)
	checkConfig(d, cfg)
	checkAutostart(d)
	checkChannel(d, cfg)
	checkSleep(d)
	checkCloud(d, cfg)

	// 打印机那一段交给 --probe 复用:排障要的就是同一套判据,不该有两份实现。
	probeCode := 0
	if cfg.probe != "" {
		logf("")
		probeCode = runProbe(cfg)
	}

	logf("")
	switch {
	case d.fail > 0:
		logf("===== 结论: 需处理 %d 项,注意 %d 项,正常 %d 项 =====", d.fail, d.warn, d.ok)
	case d.warn > 0:
		logf("===== 结论: 注意 %d 项,正常 %d 项(无阻塞性问题) =====", d.warn, d.ok)
	default:
		logf("===== 结论: %d 项全部正常 =====", d.ok)
	}
	if d.fail > 0 || probeCode != 0 {
		return 1
	}
	return 0
}

// checkIdentity 程序自身与运行环境。
func checkIdentity(d *doctor) {
	logf("--- 程序 ---")
	d.good("版本 v%s(构建 %s,提交 %s)", version, orDash(buildTime), orDash(gitCommit))
	exe, err := executableAbsPath()
	if err != nil {
		d.bad("无法确定程序自身路径: %v", err)
	} else {
		d.good("程序路径 %s", exe)
		// 「我在哪」与「真正在跑的是谁」在门店现场常常不是同一个:门店习惯站在解压目录里
		// 敲 ./print-agent --doctor,而开机自启跑的是 App 里那份、配置也在它的数据目录。
		// 不点破的话,下面会冒出一串「未配置云端地址」,把人引向完全错误的方向(实测踩到)。
		if running := darwinAgentBinary(); running != "" && !samePath(exe, running) {
			d.hint("当前执行的是解压目录里的产物,而开机自启跑的是 %s;本次体检读的是本目录的配置。"+
				"要看真实运行配置请改用: %s --doctor", running, running)
		}
	}
	d.note("平台 %s/%s", runtime.GOOS, runtime.GOARCH)

	dir := dataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.bad("运行数据目录不可用(%s): %v", dir, err)
		return
	}
	probe := filepath.Join(dir, ".doctor-write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		// 数据目录不可写是硬问题:状态文件写不进去 → 重复打印去重失效;日志写不进去 →
		// 出问题时没有任何现场。
		d.bad("运行数据目录不可写(%s): %v", dir, err)
	} else {
		_ = os.Remove(probe)
		d.good("运行数据目录可写 %s", dir)
	}
}

// checkConfig 关键配置项(不打印令牌本身)。
func checkConfig(d *doctor, cfg config) {
	logf("--- 配置 ---")
	if cfg.server == "" {
		d.bad("未配置云端地址(PRINT_AGENT_SERVER);执行 --install 或在 agent.env 里补上")
	} else {
		d.good("云端地址 %s", cfg.server)
	}
	if cfg.token == "" {
		d.bad("未配置代理令牌(PRINT_AGENT_TOKEN);到管理端「系统配置 → 小票打印 → 代理令牌」签发")
	} else {
		d.good("代理令牌已配置(%s)", maskToken(cfg.token))
	}
	d.good("代理标识 %s", effectiveName(cfg))
	// interval 是 time.Duration,按 %d 打会得到纳秒数(3000000000s),必须按 Duration 格式化。
	d.note("轮询间隔 %s,长轮询 %ds,单次取单上限 %d 条", cfg.interval, cfg.wait, cfg.limit)
}

// checkAutostart 自启动是否真的挂上了。门店最常见的一类「装过但没生效」都出在这里。
func checkAutostart(d *doctor) {
	logf("--- 自启动 ---")
	switch runtime.GOOS {
	case "darwin":
		running, pid, detail := darwinAutostartStatus(darwinBundleID)
		switch {
		case running && pid != "":
			d.good("launchd 作业 %s 运行中(PID %s)", darwinBundleID, pid)
		case running:
			d.hint("launchd 作业 %s 状态为 running 但拿不到 PID,可稍后重查", darwinBundleID)
		default:
			d.bad("launchd 作业 %s 未运行%s;执行 --install 重新注册", darwinBundleID, parenIf(detail))
		}
		if app, err := darwinBundlePath(); err == nil {
			if _, err := os.Stat(app); err == nil {
				d.good("App 已部署 %s", app)
			} else {
				d.hint("未发现 %s;重跑 --install 会自动部署(它关系到「本地网络」权限能否授予)", app)
			}
		}
	case "linux":
		out, err := runExternal([]string{"systemctl", "is-active", linuxUnitName})
		state := strings.TrimSpace(string(out))
		if err != nil || state != "active" {
			d.bad("systemd 单元 %s 状态为 %q;执行 sudo systemctl restart %s", linuxUnitName, state, linuxUnitName)
		} else {
			d.good("systemd 单元 %s 运行中", linuxUnitName)
		}
	case "windows":
		// 只看任务是否存在:schtasks 的状态文本随系统语言变化,这里不做文本判断
		// (误判比不判更糟),运行细节交给 p.bat。
		if err := runCommand([]string{"schtasks", "/query", "/tn", windowsTaskName}); err != nil {
			d.bad("计划任务 %s 未注册;执行 install.bat 重新注册", windowsTaskName)
		} else {
			d.good("计划任务 %s 已注册(运行细节用 p.bat 查看)", windowsTaskName)
		}
	default:
		d.note("当前平台 %s 无自启动检查逻辑", runtime.GOOS)
	}
}

// checkChannel 打印通道与 CUPS 队列。
//
// 这里有一条**实测得出的经验判据**:如果本机明明有能连上打印机的 CUPS 队列,而通道
// 还是 tcp,那直连一旦被 macOS 的「本地网络」权限拦下就会全线打不出纸 —— 提示改 auto。
func checkChannel(d *doctor, cfg config) {
	logf("--- 打印通道 ---")
	// 本机见过的打印机地址:配置 CUPS 时门店/技术人员不必再去管理后台翻 IP。
	if known := knownPrinterAddrs(); len(known) > 0 {
		d.note("本机见过的打印机地址:%s(配系统打印通道时可直接用 --setup-cups auto)", strings.Join(known, "、"))
	}
	if printChannelTarget.via == channelTCP {
		d.note("通道 tcp(直连打印机 IP:9100)")
		if queues := discoveredCUPSQueues(); len(queues) > 0 {
			d.hint("本机有 %d 个可用 CUPS 队列但通道是 tcp:若直连被 macOS「本地网络」权限拦下,"+
				"会全部打不出纸。执行 --setup-cups <打印机IP> 或把 PRINT_AGENT_PRINT_VIA 设为 auto", len(queues))
		}
		return
	}
	d.note("通道 %s%s", printChannelTarget.via, printChannelNote(cfg))

	queues := discoveredCUPSQueues()
	if len(queues) == 0 {
		d.bad("通道是 %s 但没发现任何 CUPS socket 队列:打印会被直接判失败。"+
			"执行 --setup-cups <打印机IP> 一条命令建好队列", printChannelTarget.via)
		return
	}
	addrs := make([]string, 0, len(queues))
	for addr := range queues {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		d.good("CUPS 队列 %s → %s", addr, queues[addr])
	}
}

// checkSleep 防睡眠与合盖(macOS)。这两件事互相独立,少做一件合盖后就是会停摆。
func checkSleep(d *doctor) {
	if runtime.GOOS != "darwin" {
		return
	}
	logf("--- 防睡眠 / 合盖 ---")
	st := readDarwinSleepState()

	if pid := caffeinatePIDFor(darwinAgentBinary()); pid > 0 {
		d.good("caffeinate 防睡眠在位(PID %d)", pid)
	} else {
		d.hint("未发现 caffeinate 进程:合盖后空闲计时器也会让系统睡眠。" +
			"重跑 --install 并在询问「合盖后继续工作」时选 y")
	}

	if !st.known {
		d.hint("读不到合盖睡眠开关,请手工核对: %s", ioregSleepDisabledHint)
	} else if st.sleepDisabled {
		d.good("合盖睡眠已关闭(SleepDisabled=Yes)")
	} else {
		d.hint("合盖睡眠未关闭,合盖后系统会睡、代理停摆(caffeinate 挡不住它)。执行: %s",
			darwinDisableSleepCommand())
	}

	if st.onBattery {
		d.hint("当前靠电池供电:合盖长时间运行会持续耗电并发热,请接交流电源")
	} else {
		d.good("已接交流电源")
	}
}

// checkCloud 云端连通(配置齐全时才查)。
func checkCloud(d *doctor, cfg config) {
	logf("--- 云端 ---")
	if cfg.server == "" || cfg.token == "" {
		d.note("云端地址或令牌缺失,跳过连通性检查")
		return
	}
	client := newHTTPClient(cfg.insecure)
	if err := ping(client, cfg.server, cfg); err != nil {
		d.bad("连接云端失败: %v", err)
		return
	}
	d.good("云端连通正常")
}

// ============================================================================
// 小工具
// ============================================================================

// orDash 空值显示为短横线,避免日志里出现空白的字段。
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// parenIf 把附注包进括号;没有附注时返回空串。
func parenIf(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return "(" + strings.TrimSpace(s) + ")"
}

// maskToken 只露出令牌首尾,便于门店核对「填的是哪一条」而不泄露内容。
func maskToken(t string) string {
	if len(t) <= 8 {
		return "****"
	}
	return t[:4] + "****" + t[len(t)-4:]
}

// effectiveName 代理标识(未显式配置时取主机名,与运行期一致)。
func effectiveName(cfg config) string {
	if strings.TrimSpace(cfg.name) != "" {
		return cfg.name
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "-"
}

// darwinAgentBinary 被 launchd 拉起的那个可执行文件路径。
//
// 已按 .app 形态部署时是 App 内的那一份;否则退回当前进程自己 —— 判据是「自启动拉起
// 的是谁」,而不是「我现在是谁」,两者在门店现场经常不是同一个。
func darwinAgentBinary() string {
	if app, err := darwinBundlePath(); err == nil {
		p := darwinBundleExePath(app)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if exe, err := executableAbsPath(); err == nil {
		return exe
	}
	return ""
}

// darwinAutostartStatus 查询 launchd 作业状态。
//
// 用 launchctl print 而不是 list:list 的输出列宽固定、且不区分「已注册但退出」与
// 「从未注册」,排障时正是这两种要分开。
func darwinAutostartStatus(label string) (running bool, pid, detail string) {
	out, err := runExternal([]string{"launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)})
	s := string(out)
	if err != nil && !strings.Contains(s, "state = ") {
		// 未注册时 launchctl 以非零码退出并把原因写在 stderr(已合并到 out)。
		return false, "", firstLine(s)
	}
	running = strings.Contains(s, "state = running")
	return running, valueAfter(s, "pid = "), ""
}

// valueAfter 取 s 中第一个 prefix 之后到行尾的内容。
func valueAfter(s, prefix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	if j := strings.IndexAny(rest, "\n\r"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// firstLine 取第一行非空内容,用于把外部命令的报错浓缩成一行。
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// caffeinatePIDFor 找到「为指定代理进程持有防睡眠断言」的 caffeinate 进程。
//
// 用 ps 全量扫描而不是 pgrep -f:路径里有 `.` 之类的正则元字符,丢给 pgrep 匹配
// 会有歧义;而且我们要的是「命令行里同时出现 caffeinate 与该路径」,自己比对最直白。
func caffeinatePIDFor(agentPath string) int {
	if agentPath == "" {
		return 0
	}
	out, err := runExternal([]string{"ps", "-o", "pid=", "-o", "command=", "-ax"})
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "caffeinate") || !strings.Contains(line, agentPath) {
			continue
		}
		if f := strings.Fields(line); len(f) > 0 {
			if pid, err := strconv.Atoi(f[0]); err == nil {
				return pid
			}
		}
	}
	return 0
}
