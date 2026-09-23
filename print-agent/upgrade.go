package main

// ============================================================================
// 自助升级(--upgrade)
//
// 流程:ping 取云端「代理最新版本号」→ 版本落后时下载新产物 → sha256 校验 →
// 原子替换自身 → 提示/触发重启。
//
// 安全要点:
//   - 下载走与取单相同的令牌鉴权,产物由服务端部署(非第三方地址);
//   - 下载先落临时文件,sha256(服务端响应头给出)校验通过才碰现有程序文件,
//     任何一步失败都保持旧程序原样可运行;
//   - Unix 下 rename 原子替换,正在运行的旧进程不受影响,重启后生效;
//   - Windows 下运行中的 exe 不可覆盖但可改名:旧文件先改名 .old,新文件就位,
//     再触发任务计划重启;改名失败立即回滚。
// ============================================================================

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// upgradeDownloadTimeout 下载超时:产物约 6~8MB,给门店慢网络留足余量。
const upgradeDownloadTimeout = 5 * time.Minute

// runUpgrade 执行自助升级;任何一步失败都 fatalf 退出并保持旧程序原样。
func runUpgrade(client *http.Client, base string, cfg config) {
	latest, err := fetchLatestVersion(client, base, cfg)
	if err != nil {
		fatalf("查询云端最新版本失败: %v", err)
	}
	if latest == "" {
		fatalf("云端未配置「代理最新版本号」(管理后台「系统配置 → 本地打印代理」),配置后才能自助升级")
	}
	switch compareVersion(version, latest) {
	case 0:
		logf("已是最新版本 v%s,无需升级", version)
		return
	case 1:
		logf("[提示] 本地 v%s 比云端配置的 v%s 还新(开发/灰度场景),仍按云端版本下载", version, latest)
	default:
		logf("发现新版本: v%s → v%s", version, latest)
	}

	exe, err := executableAbsPath()
	if err != nil {
		fatalf("无法确定程序自身路径: %v", err)
	}
	tmp := upgradeTmpPath(exe)
	defer os.Remove(tmp) // 成功后临时文件已改名,此处仅清理异常残留

	logf("下载新版本中(来自 %s)...", base)
	sha, err := downloadAgent(client, base, cfg, tmp)
	if err != nil {
		fatalf("下载新版本失败: %v", err)
	}
	if sha == "" {
		fatalf("云端响应缺少 sha256(后端版本过旧),无法校验升级包,已中止")
	}
	if err := verifyFileSHA256(tmp, sha); err != nil {
		fatalf("升级包校验失败(下载损坏或传输被篡改,请重试): %v", err)
	}
	if err := replaceBinary(exe, tmp); err != nil {
		fatalf("替换程序失败(旧程序未受影响): %v", err)
	}
	syncDarwinBundleAfterUpgrade(exe)
	logf("已替换为 v%s", latest)
	restartHint()
}

// syncDarwinBundleAfterUpgrade 让 ~/Applications 里的 App 跟着自助升级一起更新。
//
// 为什么需要它:macOS 上真正被 launchd 拉起的是 .app 里的那份拷贝,而门店执行自助
// 升级时用的多半是解压目录里的产物(upgrade.sh 找的就是它)。只替换解压目录里的文件,
// 重启后跑的还是 App 里的旧版本 —— 「提示升级成功、版本却没变」,最难查的那类问题。
//
// 升级完必须重新签名:替换 App 内文件会让原有签名失效,而签名正是系统识别本 App、
// 进而授予「本地网络」权限的依据。写 Info.plist 也必须排在签名之前,同理。
func syncDarwinBundleAfterUpgrade(exe string) {
	if runtime.GOOS != "darwin" {
		return
	}
	app, err := darwinBundlePath()
	if err != nil {
		return
	}
	if _, err := os.Stat(app); err != nil {
		return // 没做过 bundle 部署(旧版安装 / 非 macOS 门店),无事可做
	}
	target := darwinBundleExePath(app)
	if !samePath(exe, target) {
		if err := copyFile(exe, target, 0o755); err != nil {
			logf("[警告] 同步更新 App 失败: %v(下次 --install 会重新部署)", err)
			return
		}
		logf("[提示] 已同步更新 App: %s", app)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"),
		[]byte(renderDarwinInfoPlist(deployTemplate("deploy/PrintAgent-Info.plist"))), 0o644); err != nil {
		logvf("刷新 App 版本号失败(不影响运行): %v", err)
	}
	signDarwinBundle(app)
}

// fetchLatestVersion ping 云端,取「代理最新版本号」。
func fetchLatestVersion(client *http.Client, base string, cfg config) (string, error) {
	var out struct {
		LatestAgentVersion string `json:"latestAgentVersion"`
	}
	if err := post(client, base, "/api/agent/print/ping", cfg, pingBody(cfg.name), &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.LatestAgentVersion), nil
}

// downloadAgent 下载对应平台的产物到 path,返回服务端给出的 sha256(hex)。
func downloadAgent(client *http.Client, base string, cfg config, path string) (string, error) {
	u := fmt.Sprintf("%s/api/agent/print/download?goos=%s&goarch=%s", base, runtime.GOOS, runtime.GOARCH)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Agent-Token", cfg.token)
	req.Header.Set("X-Agent-Version", strconv.Itoa(agentProtocolVersion))

	// 下载时长不受常规 httpTimeout(15s)限制,复用连接池。
	dl := &http.Client{Timeout: upgradeDownloadTimeout, Transport: client.Transport}
	resp, err := dl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var r apiResp
		if json.Unmarshal(body, &r) == nil && r.Msg != "" {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, r.Msg)
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}

	// 产物约 6~8MB,按 10 倍上限防异常响应撑爆磁盘。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20)); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Header.Get("X-Agent-Sha256")), nil
}

// verifyFileSHA256 校验下载产物的 sha256(与响应头给出的值比对)。
func verifyFileSHA256(path, want string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.ToLower(strings.TrimSpace(want)) {
		return fmt.Errorf("sha256 不匹配(期望 %s)", want)
	}
	return nil
}

// upgradeTmpPath 下载临时文件路径;Windows 下保持 .exe 结尾,便于就地改名。
func upgradeTmpPath(exe string) string {
	if runtime.GOOS == "windows" {
		return exe + ".upgrade.exe"
	}
	return exe + ".upgrade"
}

// replaceBinary 用新产物替换正在运行的程序文件。
//
// Unix:rename 原子替换,运行中的旧 inode 继续跑,重启后新版本生效。
// Windows:运行中的 exe 可改名不可覆盖 —— 旧文件先改名 .old,新文件就位,
// 失败则回滚恢复旧文件名。
func replaceBinary(exe, tmp string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmp, exe)
	}
	backup := exe + ".old"
	_ = os.Remove(backup) // 上次升级残留;删除失败(仍被占用)不影响后续覆盖语义
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("重命名旧程序失败: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(backup, exe) // 回滚,保证现场旧版本完整可运行
		return fmt.Errorf("新程序就位失败: %w", err)
	}
	return nil
}

// restartHint 提示/触发重启让新版本生效。
func restartHint() {
	switch runtime.GOOS {
	case "darwin":
		logf("重启代理让新版本生效: launchctl kickstart -k gui/$(id -u)/com.cjfarm.print-agent")
		logf("(装了 op 脚本的目录里也可执行 ./start.sh)")
	case "linux":
		logf("重启代理让新版本生效: sudo systemctl restart print-agent")
	case "windows":
		// 任务计划重启失败只提示:下次开机/登录也会自然拉到新版本。
		if err := runCommand([]string{"schtasks", "/run", "/tn", windowsTaskName}); err != nil {
			logf("[提示] 自动触发任务计划重启失败: %v(可双击 start.bat,或重启电脑生效)", err)
			return
		}
		logf("已触发任务计划 %s 重启,新版本即刻生效", windowsTaskName)
	default:
		logf("请重启代理程序让新版本生效")
	}
}

// compareVersion 比较两个 x.y.z 版本,返回 -1(a<b)/0/1(a>b)。
// 无法解析的版本(dev/空/格式异常)视为最旧 —— 开发构建总能升级到正式版本。
func compareVersion(a, b string) int {
	pa, pb := parseVersion(a), parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parseVersion 解析 x.y.z(容忍 v 前缀);任一段非法时整段按 0 处理。
func parseVersion(v string) [3]int {
	out := [3]int{}
	for i, part := range strings.SplitN(strings.TrimSpace(v), ".", 3) {
		part = strings.TrimPrefix(strings.TrimSpace(part), "v")
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}
