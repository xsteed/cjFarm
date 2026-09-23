package main

// ============================================================================
// macOS 自助配好系统打印通道(--setup-cups)
//
// 背景:macOS 15 起的「本地网络」隐私权限会拦下由 launchd 托管、且未获授权的代理,
// 报错与「打印机没开机」一模一样(见 docs/print-agent.md §3.5.1)。绕开它的确定性
// 办法是让打印改由系统打印服务发起 —— cupsd 是 root LaunchDaemon,不受该权限约束。
//
// 但要让门店自己走到这一步,文档得讲清「加打印机 → 查队列名 → 改配置 → 重启」四步,
// 每一步都能卡住人。--setup-cups 把它压成一条命令:
//
//	./print-agent --setup-cups 192.168.1.133
//
// 下列要点都是实测踩出来的,别再改回去:
//   - `lpadmin -m raw` 在 macOS 26 上被拒(「macOS不再支持原始队列」);
//   - `-m everywhere` 要求打印机支持 IPP Everywhere,而 ESC/POS 网口机通常只开 9100
//     (实测 631 连接被拒),所以也不行;
//   - **不带 -m** 直接建队列反而成功,且普通用户即可(实测不需要管理员密码);
//   - 内容靠 `lp -o raw` 原样透传,所以队列不带驱动也不影响出纸。
// ============================================================================

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cupsQueueNameFor 由打印机 IP 生成默认队列名。
//
// 刻意只用 ASCII(点除外):自动发现是从本地化输出里挑 ASCII 词来认队列名的
// (见 guessLocalizedQueue),中文队列名会让自动发现失效;而命令行建的队列名也不该
// 带空格之类需要转义的字符。
func cupsQueueNameFor(ip string) string {
	return "Cjfarm-" + strings.ReplaceAll(ip, ":", "-")
}

// runSetupCUPS 为一台或多台打印机配好 CUPS 通道,返回进程退出码。
//
// 支持一次给多个(逗号/空格分隔):门店常有多台打印机(后厨 + 前台),而每台都要重启
// 代理并等一次自动发现,分开做既慢又容易漏;一条命令全覆盖。
func runSetupCUPS(cfg config) int {
	if runtime.GOOS != "darwin" {
		logf("[提示] --setup-cups 只针对 macOS:其他平台没有「本地网络」隐私限制,直连打印机即可(tcp 通道),无需本命令")
		return 0
	}
	targets, reason := resolveSetupCUPSTargets(cfg.setupCUPS)
	if len(targets) == 0 {
		if reason == "" {
			reason = "未指定打印机地址;用法: --setup-cups 192.168.1.133[,192.168.1.134],或 --setup-cups auto"
		}
		logf("[错误] %s", reason)
		return 1
	}
	logf("===== 为 %d 台打印机配置 CUPS 打印通道 =====", len(targets))

	// 1) 逐台把队列准备好(已存在的直接复用,保证可重复执行)
	prepared := make([]probeTarget, 0, len(targets))
	for _, t := range targets {
		if err := validatePrinterAddr(t.ip, t.port); err != nil {
			logf("[错误] 目标 %q 不可用: %v", t.raw, err)
			continue
		}
		if ensureCUPSQueue(t) {
			prepared = append(prepared, t)
		}
	}
	if len(prepared) == 0 {
		logf("[问题] 没有任何一台配置成功;可走图形界面:「系统设置 → 打印机与扫描仪 → 添加打印机 → IP」,协议选 Socket / HP JetDirect")
		return 1
	}

	// 2) 打开 CUPS 通道(多台共用同一份设置,只改一次)
	if !ensureCUPSChannel() {
		return 1
	}

	// 3) 重启代理,让新配置真正生效(不重启就只是「改了文件」)
	restartAgent()

	// 4) 核对
	logf("")
	failed := 0
	for _, t := range prepared {
		if q, ok := findCUPSQueue(t.addr()); ok {
			logf("[完成] 核对通过:%s → 队列 %s", t.addr(), q)
			continue
		}
		logf("[问题] %s 的队列建了但自动发现读不到;请执行 --doctor 并把输出发给技术人员", t.addr())
		failed++
	}
	if failed > 0 {
		return 1
	}
	logf("[完成] 全部就绪:代理现在可以经系统打印服务出纸")
	logf("[提示] 想让纸真的出来验证一次: --probe %s --probe-print(会各出一张,直连那张可能失败)", prepared[0].addr())
	return 0
}

// ensureCUPSQueue 确保目标地址有一个可用的 CUPS socket 队列(已存在则复用)。
func ensureCUPSQueue(t probeTarget) bool {
	addr := t.addr()
	if queue, exists := findCUPSQueue(addr); exists {
		logf("[信息] 已有对应队列,直接使用:%s → %s", addr, queue)
		return true
	}
	queue := cupsQueueNameFor(t.ip)
	if out, err := runExternal([]string{"lpadmin", "-p", queue, "-E", "-v", "socket://" + addr}); err != nil {
		logf("[错误] 为 %s 创建打印队列失败: %v%s", addr, err, detailOf(out))
		return false
	}
	logf("[完成] 已创建打印队列 %s → socket://%s", queue, addr)
	// 队列刚建好,必须清掉发现缓存,否则后面的「核对」会读到建之前的空结果,
	// 门店看到的是「建了却还说没找到」。
	invalidateCUPSQueueCache()
	return true
}

// ensureCUPSChannel 把打印通道设为 auto(已就位则不动)。
//
// 用 auto 而不是 cups:直连若哪天能走通(例如门店改用 LaunchDaemon 或网段白名单),
// 就自动回到直连 —— 那条路能做 DLE EOT 状态回读,比 CUPS 信息更全。
func ensureCUPSChannel() bool {
	if printChannelTarget.via == channelCUPS || printChannelTarget.via == channelAuto {
		logf("[信息] 打印通道已是 %s,无需修改", printChannelTarget.via)
		return true
	}
	bin := darwinAgentBinary()
	envPath := filepath.Join(dataDirFor(bin), "agent.env")
	if err := upsertEnvKey(envPath, "PRINT_AGENT_PRINT_VIA", channelAuto); err != nil {
		logf("[错误] 写入 %s 失败: %v", envPath, err)
		return false
	}
	logf("[完成] 已把打印通道设为 auto(仍优先直连,直连失败再改投 CUPS);配置:%s", envPath)
	return true
}

// detailOf 把外部命令的输出浓缩成「(一行原因)」,没有内容时返回空串。
func detailOf(out []byte) string {
	if line := firstLine(string(out)); line != "" {
		return "(" + line + ")"
	}
	return ""
}

// restartAgent 重启自启动的代理;失败只给出手工命令,不算错误。
//
// 冲突提示:launchd 的 KeepAlive 会把进程拉起来,所以不能用 kill —— 必须走
// launchctl 的 kickstart(或重新 load),否则要么被立刻拉起、要么留下被禁用标记。
func restartAgent() {
	label := fmt.Sprintf("gui/%d/%s", os.Getuid(), darwinBundleID)
	if err := runCommand([]string{"launchctl", "kickstart", "-k", label}); err == nil {
		logf("[完成] 已重启自启动的代理,新配置即刻生效")
		return
	}
	// 作业还没注册(kickstart 找不到目标)时退回 load,与 --install 同一套。
	if home, err := os.UserHomeDir(); err == nil {
		plistPath := filepath.Join(home, "Library", "LaunchAgents", darwinPlistPath)
		if _, serr := os.Stat(plistPath); serr == nil {
			if lerr := runCommand(launchctlCommand("load", plistPath)); lerr == nil {
				logf("[完成] 已加载自启动配置并启动代理")
				return
			}
		}
	}
	logf("[注意] 未能自动重启代理;请手工执行 ./start.sh 或重新执行 --install,然后跑 --doctor 核对")
}
