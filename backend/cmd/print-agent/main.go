// Command print-agent 门店本地打印代理。
//
// 干什么用的:
//
//	后端部署在云服务器后,「直连 IP:9100」这条路径永远够不到门店内网的打印机。
//	本程序跑在门店内网任意一台常开机的设备上(收银电脑 / 小主机 / 树莓派 / 软路由),
//	主动**出站**轮询云端的打印队列,把指令转发给门店内网的打印机:
//
//	  云后端 ──入队──> 本程序(HTTPS 出站拉单) ──TCP 192.168.x.x:9100──> 打印机
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
//
// 与 program 同目录下可放一个 agent.env(KEY=VALUE 逐行,与后端 .env 同格式),
// Windows 下把它和 exe 放一起双击即可运行,不必配环境变量。
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 默认参数。轮询间隔取 3 秒:订单到出纸的延迟让顾客察觉不到,又不会给云端太大压力。
const (
	defaultInterval = 3
	defaultLimit    = 10
	dialTimeout     = 5 * time.Second
	httpTimeout     = 15 * time.Second
	ackAttempts     = 3
)

// config 运行参数。
type config struct {
	server   string // 云端地址,如 https://dining.example.com(可带子路径,末尾斜杠可有可无)
	token    string // 代理令牌(系统配置 → 小票打印 → 代理令牌)
	interval time.Duration
	limit    int
	name     string // 代理标识,多台代理时可区分是谁取的
	state    string // 本地状态文件路径(幂等去重用)
	insecure bool   // 跳过 TLS 证书校验(仅自签证书的内网环境)
	once     bool   // 只跑一轮后退出(排障自检)
}

func main() {
	// 先把同目录下的 agent.env 灌进环境变量,再解析命令行 —— 这样「文件配置」
	// 与「环境变量/命令行」都能用,且后者优先级更高。
	loadEnvFile(envFilePath())

	cfg, err := parseFlags()
	if err != nil {
		fatalf("%v", err)
	}
	if cfg.once {
		cfg.limit = 1
	}

	client := newHTTPClient(cfg.insecure)
	base := strings.TrimSuffix(cfg.server, "/")

	// 启动自检:令牌/地址有问题立刻失败退出(systemd 会重试并让失败可见),
	// 而不是静默循环 —— 「代理跑着但一单也没打出来」是最难排查的状态。
	if err := ping(client, base, cfg); err != nil {
		fatalf("连接云端失败: %v\n请检查 --server / --token,以及系统配置里的「代理令牌」是否一致", err)
	}
	logf("已连接云端 %s(代理标识 %s,轮询间隔 %s)", base, cfg.name, cfg.interval)

	// 本地去重状态:记录「已经成功打印过的幂等号」。
	// 打印成功后先落盘、再回执 —— 即便回执因断网丢失、任务被云端重发,
	// 下次取到同一 deliveryId 也会被跳过,从而避免同一张票重复出纸。
	st := loadJobState(cfg.state)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	for {
		n, err := runCycle(client, base, cfg, st)
		if err != nil {
			// 网络类错误不退出:门店断网、云端重启都属正常,恢复后自动续上。
			logf("[警告] 本轮取单失败(稍后自动重试): %v", err)
		} else if n > 0 {
			logf("本轮已处理 %d 条打印任务", n)
		}
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

// runCycle 执行一轮完整流程,返回本轮处理的任务数。
func runCycle(client *http.Client, base string, cfg config, st *jobState) (int, error) {
	jobs, err := pull(client, base, cfg)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}

	type result struct {
		JobID  int    `json:"jobId"`
		Ok     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	results := make([]result, 0, len(jobs))
	for _, j := range jobs {
		// 幂等去重:这条任务在本机已经成功打印过(上一次回执丢失被重发)。
		// 跳过真正打印、直接回执成功,避免重复出票。
		if st.has(j.DeliveryID) {
			logf("[去重] 任务 #%d 已在本机打印过(deliveryId %s),跳过并回执", j.JobID, j.DeliveryID)
			results = append(results, result{JobID: j.JobID, Ok: true})
			continue
		}

		err := printJob(j)
		r := result{JobID: j.JobID, Ok: err == nil}
		if err != nil {
			r.Detail = err.Error()
			logf("[失败] 任务 #%d → 打印机[%s](%s:%d) 单据 %s: %v",
				j.JobID, j.PrinterName, j.IP, j.Port, docLabel(j), err)
		} else {
			// 先落盘再回执:把「重复打印」的窗口压到「打印成功 → 落盘」之间的毫秒级。
			st.markDone(j.DeliveryID)
			logf("[成功] 任务 #%d → 打印机[%s](%s:%d) 单据 %s ×%d",
				j.JobID, j.PrinterName, j.IP, j.Port, docLabel(j), j.Copies)
		}
		results = append(results, r)
	}

	ack(client, base, cfg, results)
	return len(jobs), nil
}

// ============================================================================
// 打印
// ============================================================================

// printJob 把一条任务写进打印机。
//
// 与后端直连通道完全一致:建立 TCP 连接 → 原样写入字节 → 关闭。
// 份数靠「重复发送同一份指令」实现(ESC/POS 没有联数指令,与后端 sendViaTCP 同策略)。
func printJob(j agentJob) error {
	if strings.TrimSpace(j.IP) == "" {
		return errors.New("任务未带打印机 IP")
	}
	port := j.Port
	if port <= 0 {
		port = 9100
	}
	data, err := base64.StdEncoding.DecodeString(j.Payload)
	if err != nil {
		return fmt.Errorf("打印内容解码失败: %w", err)
	}
	if len(data) == 0 {
		return errors.New("打印内容为空")
	}
	copies := j.Copies
	if copies < 1 {
		copies = 1
	}

	addr := net.JoinHostPort(j.IP, strconv.Itoa(port))
	for i := 0; i < copies; i++ {
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err != nil {
			// 打印机没开机 / IP 填错 / 与代理不在同一网段,都会走到这里。
			return fmt.Errorf("无法连接打印机 %s: %w", addr, err)
		}
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		_, werr := conn.Write(data)
		_ = conn.Close()
		if werr != nil {
			return fmt.Errorf("向 %s 写入打印数据失败: %w", addr, werr)
		}
	}
	return nil
}

// ============================================================================
// 接口调用
// ============================================================================

type agentJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Copies      int    `json:"copies"`
	DocType     string `json:"docType"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	// DeliveryID 幂等投递号:打印成功后落盘,重发时据此去重(见 runCycle)。
	DeliveryID string `json:"deliveryId"`
	Payload    string `json:"payload"`
}

// apiResp 后端统一响应体(与浏览器端共用同一套 {code,msg,data} 约定)。
type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// post 发起一次带令牌的 POST 请求并解析统一响应体。
func post(client *http.Client, base, path, token string, body interface{}, out interface{}) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	var r apiResp
	if err := json.Unmarshal(payload, &r); err != nil {
		return fmt.Errorf("云端返回格式异常(HTTP %d): %s", resp.StatusCode, snippet(payload))
	}
	if r.Code != 200 {
		if r.Msg != "" {
			return errors.New(r.Msg)
		}
		return fmt.Errorf("云端返回 HTTP %d", resp.StatusCode)
	}
	if out != nil && len(r.Data) > 0 {
		if err := json.Unmarshal(r.Data, out); err != nil {
			return fmt.Errorf("解析云端数据失败: %w", err)
		}
	}
	return nil
}

// pull 取一批待打印任务。
func pull(client *http.Client, base string, cfg config) ([]agentJob, error) {
	var out struct {
		Jobs []agentJob `json:"jobs"`
	}
	err := post(client, base, "/prod-api/agent/print/pull", cfg.token,
		map[string]interface{}{"agentId": cfg.name, "limit": cfg.limit}, &out)
	return out.Jobs, err
}

// ack 回报结果。
//
// 失败会重试几次:打印本身已经成功、只是回执丢了的话,任务会因租约超时被重新下发,
// 导致同一张票打两遍。重试能把这个窗口压到很小,但网络彻底中断时仍可能重复
// (宁可重打一张,也不能丢单)—— 这一点在 docs/print-agent.md 里已说明。
func ack(client *http.Client, base string, cfg config, results interface{}) {
	body := map[string]interface{}{"results": results}
	var lastErr error
	for i := 1; i <= ackAttempts; i++ {
		if err := post(client, base, "/prod-api/agent/print/ack", cfg.token, body, nil); err == nil {
			if i > 1 {
				logf("回执已补送成功(第 %d 次尝试)", i)
			}
			return
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(i) * time.Second)
	}
	logf("[警告] 回执发送失败(任务已被云端标记为打印中,租约超时后会重新下发,可能重复打印): %v", lastErr)
}

// ping 启动自检:校验地址与令牌。
func ping(client *http.Client, base string, cfg config) error {
	var out struct {
		ShopName   string `json:"shopName"`
		ServerTime string `json:"serverTime"`
		Pending    int    `json:"pending"`
	}
	if err := post(client, base, "/prod-api/agent/print/ping", cfg.token,
		map[string]interface{}{"agentId": cfg.name}, &out); err != nil {
		return err
	}
	logf("云端自检通过: 店铺=%q 服务器时间=%s 待送任务=%d 条", out.ShopName, out.ServerTime, out.Pending)
	return nil
}

// ============================================================================
// 参数与工具
// ============================================================================

func parseFlags() (config, error) {
	var (
		server   = flag.String("server", envOr("PRINT_AGENT_SERVER", ""), "云端地址,如 https://dining.example.com")
		token    = flag.String("token", envOr("PRINT_AGENT_TOKEN", ""), "代理令牌(系统配置 → 小票打印 → 代理令牌)")
		interval = flag.Int("interval", envInt("PRINT_AGENT_INTERVAL", defaultInterval), "轮询间隔(秒)")
		limit    = flag.Int("limit", envInt("PRINT_AGENT_LIMIT", defaultLimit), "单次最多取几条任务(1-50)")
		name     = flag.String("name", envOr("PRINT_AGENT_NAME", ""), "代理标识(默认取主机名)")
		state    = flag.String("state", envOr("PRINT_AGENT_STATE", ""), "本地状态文件(记录已成功打印的幂等号,用于去重;默认取程序同目录 print-agent.state)")
		insecure = flag.Bool("insecure", envOr("PRINT_AGENT_INSECURE", "") == "1", "跳过 TLS 证书校验(自签证书时使用)")
		once     = flag.Bool("once", false, "只跑一轮后退出(排障自检)")
		// 声明但不在此处使用:--env 已在 main() 开头(loadEnvFile)按 os.Args 处理,
		// 这里登记一下,免得 flag.Parse 报「flag provided but not defined」。
		_ = flag.String("env", "", "agent.env 配置文件路径(默认取程序同目录)")
	)
	flag.Parse()

	cfg := config{
		server:   strings.TrimSpace(*server),
		token:    strings.TrimSpace(*token),
		interval: time.Duration(*interval) * time.Second,
		limit:    *limit,
		name:     strings.TrimSpace(*name),
		state:    strings.TrimSpace(*state),
		insecure: *insecure,
		once:     *once,
	}
	if cfg.state == "" {
		// 默认与程序同目录,便于「一个文件夹打包部署」:exe + agent.env + print-agent.state。
		if exe, err := os.Executable(); err == nil {
			cfg.state = filepath.Join(filepath.Dir(exe), "print-agent.state")
		} else {
			cfg.state = "print-agent.state"
		}
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
	return cfg, nil
}

// newHTTPClient 构造 HTTP 客户端。
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

// envFilePath 返回 agent.env 的查找路径:优先 --env 指定的,否则取可执行文件同目录。
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
	exe, err := os.Executable()
	if err != nil {
		return "agent.env"
	}
	return filepath.Join(filepath.Dir(exe), "agent.env")
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

// docLabel 单据类型的中文名(日志可读性)。
func docLabel(j agentJob) string {
	switch j.DocType {
	case "kitchen":
		return "厨房单"
	case "guest":
		return "食客小票"
	case "test":
		return "测试页"
	}
	if j.DocType == "" {
		return "单据"
	}
	return j.DocType
}

// snippet 截取响应片段用于报错展示。
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// logf 输出带时间戳的日志(stdout,便于 systemd / 任务计划程序采集)。
func logf(format string, a ...interface{}) {
	fmt.Printf("%s "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, a...)...)
}

// fatalf 打印错误并退出(非 0 退出码让 systemd 视为失败并触发重启)。
func fatalf(format string, a ...interface{}) {
	logf("[错误] "+format, a...)
	os.Exit(1)
}

// ============================================================================
// 本地去重状态(幂等投递)
// ============================================================================
//
// 目的:把「回执丢失 → 云端重发 → 同一张票打两遍」从「可能发生」变成「基本不可能」。
// 原理:每条任务带一个永不变更的 deliveryId,代理打印成功后立刻把它写进本地状态文件
// 并 fsync,之后才回执。下次取到相同 deliveryId(说明上次回执丢了、任务被重发),
// 直接跳过打印、回执成功即可。
//
// 为什么落盘而不是放内存:代理进程可能被重启/断电,内存记录会丢;落盘才能在重启后
// 依然去重。残余窗口只剩「打印成功 → 写盘」之间的毫秒级断电,这已是无法在应用层
// 进一步消除的极限(再往下需要打印机端的事务回执,不现实)。

// jobStateMaxKeep 状态文件最多保留的幂等号条数。超出后截断最老的一半,
// 避免门店长期运行后文件无界增长;10000 条足够覆盖「一周内的回执重试」。
const jobStateMaxKeep = 10000

// jobState 已成功打印的幂等号集合(带持久化)。
type jobState struct {
	mu    sync.Mutex
	path  string
	f     *os.File
	seen  map[string]struct{}
	order []string // 按写入顺序,用于超限时截断最老的
}

// loadJobState 从文件加载去重状态并保持追加句柄。
// 文件不可写时降级为纯内存(仍然工作,只是重启后失去去重能力),并打一条告警。
func loadJobState(path string) *jobState {
	s := &jobState{path: path, seen: map[string]struct{}{}}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				s.seen[line] = struct{}{}
				s.order = append(s.order, line)
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		logf("[警告] 状态文件不可写(%v),重启后将失去重复打印去重能力", err)
		return s
	}
	s.f = f
	return s
}

// has 报告某幂等号是否已在本机成功打印过。
func (s *jobState) has(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[id]
	return ok
}

// markDone 记录一次成功打印:追加写文件并 fsync(先落盘,调用方才回执)。
func (s *jobState) markDone(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return
	}
	s.seen[id] = struct{}{}
	s.order = append(s.order, id)
	if s.f != nil {
		_, _ = s.f.WriteString(id + "\n")
		_ = s.f.Sync() // 落盘后再回执,断电也不丢
	}
	s.maybeCompactLocked()
}

// maybeCompactLocked 超出上限时截断最老的一半并整体重写(调用方须持有锁)。
func (s *jobState) maybeCompactLocked() {
	if len(s.order) <= jobStateMaxKeep {
		return
	}
	keep := s.order[len(s.order)-jobStateMaxKeep/2:]
	s.order = keep
	s.seen = make(map[string]struct{}, len(keep))
	for _, k := range keep {
		s.seen[k] = struct{}{}
	}
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	if err := os.WriteFile(s.path, []byte(strings.Join(keep, "\n")+"\n"), 0o600); err == nil {
		if f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			s.f = f
		}
	}
}
