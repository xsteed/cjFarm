package main

// ============================================================================
// 打印机地址记忆(--setup-cups 自助配置的输入来源)
//
// 门店自助配置里最尴尬的一步是:--setup-cups 需要打印机 IP,而店员不知道 IP 是多少
// (IP 配在管理后台的打印机条目里,本机没有任何地方写着)。但云端下发的每个打印任务
// 都带着目标 IP —— 代理顺手记下来,后续 --setup-cups auto 就一台都不用问。
//
// 设计上的两个克制:
//   - **只记不猜**:文件里只有真在任务里出现过的地址,不会把猜测的网段写进去;
//   - **失败静默**:这只是便利功能,读写失败绝不该影响打印主线。
// ============================================================================

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// printerAddrsFileName 记忆文件名(与 state / log 同放在运行数据目录)。
const printerAddrsFileName = "print-agent.printers"

// cupsAutoTarget --setup-cups 的特殊取值:用本机见到过的全部打印机地址。
// 门店脚本默认就用它,门店本身不需要知道 IP。
const cupsAutoTarget = "auto"

var (
	printersMu   sync.Mutex
	printersSeen map[string]bool // 进程内去重,避免每条任务都去读写文件
)

// printerAddrsPath 记忆文件的绝对路径。
func printerAddrsPath() string {
	return filepath.Join(dataDir(), printerAddrsFileName)
}

// recordPrinterAddr 记下一个见过的打印机地址("ip:port")。
//
// 追加写 + 进程内去重:门店的打印机是个位数,文件永远很小,不值得为它引入更复杂的
// 结构。写失败直接放弃(见文件头「失败静默」)。
func recordPrinterAddr(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	printersMu.Lock()
	defer printersMu.Unlock()

	if printersSeen == nil {
		printersSeen = map[string]bool{}
		for _, a := range readPrinterAddrs() {
			printersSeen[a] = true
		}
	}
	if printersSeen[addr] {
		return
	}
	printersSeen[addr] = true

	f, err := os.OpenFile(printerAddrsPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(addr + "\n"); err != nil {
		return
	}
	logvf("已记下打印机地址 %s(以后 --setup-cups auto 可直接用它,不必手填)", addr)
}

// knownPrinterAddrs 返回历史上见过的打印机地址(排序后)。
//
// 排序是为了让日志与后续命令的输出稳定可比 —— map/文件顺序飘忽时,「两次运行对比」
// 这种最常用的排障手段就失效了。
func knownPrinterAddrs() []string {
	printersMu.Lock()
	defer printersMu.Unlock()
	return readPrinterAddrs()
}

// readPrinterAddrs 读记忆文件(调用方需自行持锁)。文件不存在时返回 nil。
func readPrinterAddrs() []string {
	data, err := os.ReadFile(printerAddrsPath())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		addr := strings.TrimSpace(line)
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	sort.Strings(out)
	return out
}

// resolveSetupCUPSTargets 把 --setup-cups 的取值解析成待处理的目标列表。
//
// 支持三形态:单个 IP、逗号分隔的多个 IP、以及特殊值 auto(用本机见过的地址)。
// auto 存在的理由见文件头 —— 它让门店不必知道打印机 IP。
func resolveSetupCUPSTargets(arg string) ([]probeTarget, string) {
	if !strings.EqualFold(strings.TrimSpace(arg), cupsAutoTarget) {
		return parseProbeTargets(arg), ""
	}
	known := knownPrinterAddrs()
	if len(known) == 0 {
		return nil, "本机还没见过任何打印机地址。请先到管理后台点一次「测试打印」(代理收到任务后就会记下地址)," +
			"然后重跑本命令;或者直接给出 IP: --setup-cups 192.168.1.133"
	}
	logf("[信息] 使用本机见过的 %d 个打印机地址:%s", len(known), strings.Join(known, "、"))
	return parseProbeTargets(strings.Join(known, ",")), ""
}
