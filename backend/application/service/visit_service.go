package service

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure"
	"dining-system/infrastructure/geoip"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// ============ 站点访问统计(PV / UV / 新访客 / 渠道) ============
//
// 指标口径与表结构见 docs/design-docs/visit-analytics/spec.md。
// 与审计同一条哲学:统计是旁路数据,写失败只告警,绝不影响点餐/登录(fail-open)。

// ============================================================================
// 访客标识
// ============================================================================

// VisitInput 一次访问的原始信息(由 handler 从 HTTP 请求中提取)。
type VisitInput struct {
	Channel    string // h5 / miniprogram / admin
	VisitorID  string // 前端生成并持久化的随机访客 ID(可空)
	CustomerID int    // 登录会员 ID(0 = 匿名),只认服务端从令牌解析的值
	Entry      string // 入口标识:桌台点餐码(H5)
	IP         string // 来源 IP(哈希入库供去重;明文另存一列用于排查展示)
	UA         string // User-Agent(哈希入库供去重;另解析出 OS/浏览器/设备类型)

	// 端上直传的设备信息(仅小程序能精确提供:H5 拿不到机型,服务端 UA 兜底)。
	// system 形如 "iOS 16.6";platform 为 ios/android/windows/mac;model 为机型。
	ClientSystem   string
	ClientPlatform string
	ClientModel    string
}

// hashVisitValue 返回 sha256(登录令牌密钥|value) 的 hex 前 32 位。
//
// pepper 取 data/auth.key(与登录令牌同密钥体系):库文件泄露无法反推原始 IP/UA;
// 多实例部署共享同一密钥文件时,各节点算出的哈希才一致(UV 跨实例去重才有意义,
// 见 README「多实例部署必须共享 TOKEN_SECRET 或同一密钥文件」)。
func hashVisitValue(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(infrastructure.LoadAuthKey() + "|" + value))
	return hex.EncodeToString(sum[:])[:32]
}

// visitorKeyOf 计算 UV 去重键(优先级见 spec 第 1 节):
//
//	m:<customer_id> 小程序登录会员(最可靠,跨设备同一微信号恒定)
//	u:<user_id>     管理端登录员工(与会员 ID 空间隔离 —— 两边各自从 1 开始,
//	                 不加前缀会撞键,一个员工和一个会员会被误并成同一访客)
//	c:<visitorId>   前端随机访客 ID(同设备再次访问可识别)
//	a:<ip>:<ua>     存储被禁用时的兜底
func visitorKeyOf(in VisitInput, ipHash, uaHash string) string {
	if in.CustomerID > 0 {
		if in.Channel == po.VisitChannelAdmin {
			return "u:" + strconv.Itoa(in.CustomerID)
		}
		return "m:" + strconv.Itoa(in.CustomerID)
	}
	if id := sanitizeVisitorID(in.VisitorID); id != "" {
		return "c:" + id
	}
	return "a:" + ipHash + ":" + uaHash
}

// sanitizeVisitorID 过滤访客 ID:只保留 URL 安全字符并限长,防任意文本入库。
func sanitizeVisitorID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 40 {
		id = id[:40]
	}
	var b strings.Builder
	for _, r := range id {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeEntry 过滤入口标识(桌台点餐码):最多 8 位字母数字,其它字符剥掉。
func sanitizeEntry(entry string) string {
	entry = strings.TrimSpace(entry)
	var b strings.Builder
	for _, r := range entry {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
		if b.Len() >= 8 {
			break
		}
	}
	return b.String()
}

// ============================================================================
// 终端信息(UA 解析 + 小程序直传)
// ============================================================================

// clampText 文本入库前的兜底截断(按字符数,防 MySQL 严格模式拒收整条 INSERT)。
func clampText(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// parseUA 从 User-Agent 解析出设备类型 / 操作系统 / 浏览器 / 机型(尽力而为)。
//
// 手写轻量解析而非引入第三方库:这里只求「分布统计够用」的粗粒度结论
// (iOS / Android / 微信 / 手机…),机型仅在安卓 UA 给出时提取(iOS 的 UA
// 不含机型,见 uaAndroidModel)。判定顺序有讲究:
//   - 浏览器判定必须先于 Safari(Safari 的 UA 里含 Chrome 关键字的另有其人)、
//     先于 Chrome(Edge/Opera 的 UA 里也带 Chrome);
//   - 平板判定先于手机(Android 平板的 UA 不含 Mobile,手机才含);
//   - 鸿蒙判定必须先于 Android:鸿蒙 4.x 及以前的 UA 同时含 Android 与
//     HarmonyOS(如 "Linux; Android 10; HarmonyOS; NOH-AN00"),先判
//     Android 会让鸿蒙分支永远命中不到;鸿蒙 NEXT 的 UA 形如
//     "Mozilla/5.0 (Phone;OpenHarmony 6.0) … ArkWeb/6.0.0.42 Mobile",
//     既无 Android 也无 HarmonyOS,只能靠 OpenHarmony / ArkWeb 识别。
func parseUA(ua string) (device, osName, browser, model string) {
	u := strings.ToLower(ua)
	// ---- 设备类型 ----
	switch {
	case strings.Contains(u, "ipad"), strings.Contains(u, "tablet"):
		device = "平板"
	case strings.Contains(u, "android") && !strings.Contains(u, "mobile"):
		device = "平板"
	case strings.Contains(u, "mobile"), strings.Contains(u, "iphone"):
		device = "手机"
	case strings.Contains(u, "android"):
		device = "手机"
	case strings.Contains(u, "windows"), strings.Contains(u, "macintosh"),
		strings.Contains(u, "mac os x"), strings.Contains(u, "x11"), strings.Contains(u, "linux"):
		device = "电脑"
	default:
		device = "其他"
	}
	// ---- 操作系统 ----
	switch {
	case strings.Contains(u, "iphone"):
		osName = "iOS"
	case strings.Contains(u, "ipad"):
		osName = "iPadOS"
	// 鸿蒙必须先于 Android:鸿蒙 4.x 及以前的 UA 同时含 Android(见函数注释)。
	case strings.Contains(u, "harmonyos"), strings.Contains(u, "openharmony"),
		strings.Contains(u, "arkweb"):
		osName = "鸿蒙"
	case strings.Contains(u, "android"):
		osName = "Android"
	case strings.Contains(u, "windows"):
		osName = "Windows"
	case strings.Contains(u, "macintosh"), strings.Contains(u, "mac os x"):
		osName = "macOS"
	case strings.Contains(u, "linux"), strings.Contains(u, "x11"):
		osName = "Linux"
	default:
		osName = "其他"
	}
	// ---- 浏览器/宿主(顺序敏感,见函数注释) ----
	switch {
	case strings.Contains(u, "micromessenger"):
		browser = "微信"
	case strings.Contains(u, "alipayclient"):
		browser = "支付宝"
	case strings.Contains(u, "qq/"), strings.Contains(u, "mqqbrowser"):
		browser = "QQ浏览器"
	case strings.Contains(u, "edg/"):
		browser = "Edge"
	case strings.Contains(u, "opr/"), strings.Contains(u, "opera"):
		browser = "Opera"
	case strings.Contains(u, "firefox"), strings.Contains(u, "fxios"):
		browser = "Firefox"
	case strings.Contains(u, "crios"), strings.Contains(u, "chrome"):
		browser = "Chrome"
	case strings.Contains(u, "safari"):
		browser = "Safari"
	default:
		browser = "其他"
	}
	model = uaAndroidModel(ua)
	return device, osName, browser, model
}

// uaAndroidModel 尽力从安卓 UA 的括号设备段提取机型。安卓 UA 常形如
// "(Linux; Android 14; 2210132C Build/UKQ1.230917.001; wv)":机型在 Android
// 版本之后,可能跟着 Build/ 指纹与 WebView 标记,地区段(zh-CN)偶尔插在
// 中间。剔除版本段/标记段/地区段后取最后一段。iOS/iPadOS 的 UA 不含机型
// (Apple 隐私策略),桌面端无机型概念,统一返回空。
func uaAndroidModel(ua string) string {
	rest := ua
	for {
		open := strings.IndexByte(rest, '(')
		if open < 0 {
			return ""
		}
		rest = rest[open+1:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return ""
		}
		seg := rest[:end]
		rest = rest[end+1:]
		if !strings.Contains(strings.ToLower(seg), "android") {
			continue // 设备段不在本括号组,继续找
		}
		var model string
		for _, tok := range strings.Split(seg, ";") {
			tok = strings.TrimSpace(tok)
			low := strings.ToLower(tok)
			// 版本段(Android …)、兼容/WebView 标记、系统名、地区段
			// (zh-CN 形态)都不可能是机型。鸿蒙 4.x 的 UA 段形如
			// "Linux; Android 10; HarmonyOS; NOH-AN00; HMSCore 6.3.0.332",
			// 系统名与 HMSCore 版本都排在机型之后,不剔除会把机型记成
			// "HMSCore 6.3.0.332"。
			if tok == "" || strings.HasPrefix(low, "android") ||
				low == "u" || low == "wv" || low == "linux" ||
				low == "harmonyos" || strings.HasPrefix(low, "hmscore") ||
				(len(low) == 5 && low[2] == '-') {
				continue
			}
			model = tok
		}
		if i := strings.Index(model, "Build/"); i >= 0 {
			model = model[:i]
		}
		return strings.TrimSpace(model)
	}
}

// enrichDevice 汇总一次访问的终端信息(设备类型/OS/浏览器/机型)。
//
// 小程序端走 wx.getDeviceInfo 直传,比 UA 精确得多(尤其 iOS 机型,UA 根本
// 不含),因此小程序渠道逐项覆盖,UA 只做兜底;浏览器栏固定为「微信小程序」
// —— 小程序没有"浏览器"概念,真要看宿主版本,端上直传的 system 已足够区分。
// H5 渠道 UA 解出什么存什么(安卓机型可解,iOS 天生无机型存空):打点接口
// 公开无鉴权,机型可能被伪造,分布统计够用,不值得为此设防。
func enrichDevice(in VisitInput) (device, osName, browser, model string) {
	device, osName, browser, uaModel := parseUA(in.UA)
	model = clampText(uaModel, 64)
	if in.Channel != po.VisitChannelMiniprogram {
		return
	}
	if v := clampText(in.ClientModel, 64); v != "" {
		model = v
	}
	browser = "微信小程序"
	// system 形如 "iOS 16.6" / "Android 13":取首个空格前的词归一。
	if first := strings.Fields(strings.TrimSpace(in.ClientSystem)); len(first) > 0 {
		switch strings.ToLower(first[0]) {
		case "ios":
			osName = "iOS"
		case "android":
			osName = "Android"
		case "windows":
			osName = "Windows"
		case "mac":
			osName = "macOS"
		case "harmonyos", "ohos":
			osName = "鸿蒙"
		}
	}
	switch strings.ToLower(strings.TrimSpace(in.ClientPlatform)) {
	case "ios", "android":
		device = "手机"
	case "windows", "mac":
		device = "电脑"
	}
	return device, osName, browser, model
}

// normalizeChannel 归一化渠道:白名单之外的取值按 h5 记并告警
// (统计接口恒返回 200,这里不能把错误抛给用户)。
func normalizeChannel(ch string) string {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case po.VisitChannelMiniprogram:
		return po.VisitChannelMiniprogram
	case po.VisitChannelAdmin:
		return po.VisitChannelAdmin
	case po.VisitChannelH5, "":
		return po.VisitChannelH5
	default:
		logger.Warnf("[visit] 未知渠道 %q,已按 h5 记录", ch)
		return po.VisitChannelH5
	}
}

// VisitEnabled 访问统计总开关。
func VisitEnabled() bool { return dao.VisitEnabled() }

// ============================================================================
// 打点限流(进程内,每 IP 每分钟)
// ============================================================================

const (
	visitGuardIdleTTL = 10 * time.Minute // 闲置回收时长
	visitGuardMaxKeys = 4096             // 触发闲置回收的条目上限
	visitGuardWindow  = time.Minute      // 限流窗口
)

type visitEntry struct {
	count       int
	windowStart time.Time
}

type visitGuard struct {
	mu        sync.Mutex
	m         map[string]*visitEntry
	lastPrune time.Time
}

var visitIPGuard = &visitGuard{m: make(map[string]*visitEntry)}

// allowVisit 判断该 IP 在本分钟窗口内是否还能打点(超过上限返回 false)。
// 与登录限流同一形态:内存级、惰性回收、硬上限淘汰,防无限打把内存打爆。
func (g *visitGuard) allow(ip string) bool {
	if ip == "" {
		return true
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.m[ip]
	if !ok || now.Sub(e.windowStart) > visitGuardWindow {
		g.m[ip] = &visitEntry{count: 1, windowStart: now}
		g.pruneLocked(now)
		return true
	}
	e.count++
	return e.count <= dao.VisitRatePerMin()
}

// pruneLocked 惰性回收 + 硬上限淘汰(调用方须持锁)。
func (g *visitGuard) pruneLocked(now time.Time) {
	if len(g.m) > visitGuardMaxKeys {
		target := visitGuardMaxKeys / 4 * 3
		type kv struct {
			key   string
			start time.Time
		}
		all := make([]kv, 0, len(g.m))
		for k, e := range g.m {
			all = append(all, kv{k, e.windowStart})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].start.Before(all[j].start) })
		for i := 0; i < len(all)-target && i < len(all); i++ {
			delete(g.m, all[i].key)
		}
	}
	if now.Sub(g.lastPrune) < time.Minute {
		return
	}
	g.lastPrune = now
	for k, e := range g.m {
		if now.Sub(e.windowStart) > visitGuardIdleTTL {
			delete(g.m, k)
		}
	}
}

// AllowVisitIP 该 IP 是否允许打点(供 handler 调用)。
func AllowVisitIP(ip string) bool { return visitIPGuard.allow(ip) }

// ============================================================================
// 异步批量写入
// ============================================================================

const (
	// visitChanSize 打点缓冲上限。单店日均数百次打点,4096 有两个数量级余量;
	// 缓冲满时丢弃(只告警)—— 统计绝不允许阻塞点餐请求。
	visitChanSize = 4096
	// visitBatchMax 单批最大条数(与 dao 的分块常数匹配)。
	visitBatchMax = 200
	// visitFlushInterval 攒批最长等待:再闲也要在此时长内落库,
	// 否则「最后一两条打点」会一直压在缓冲里,报表实时性差。
	visitFlushInterval = 500 * time.Millisecond
)

// visitJob 一条待写队列的访问(明细行 + 访客档案行)。
type visitJob struct {
	log     po.VisitLog
	visitor po.Visitor
}

// visitChan 在包初始化时创建,而不是在 worker 启动的 goroutine 里赋值:
// RecordVisit 对它是无锁读取,晚赋值会让「worker 尚未启动就到访」的打点
// 走进 nil 分支被静默丢弃(漏记),且从数据竞争角度也是不安全的写读模式。
// 包级初始化保证任何请求到达前 channel 已就绪;没有 worker 消费时打点只在
// 缓冲里排队(满后丢弃并告警),不会写库。
var visitChan = make(chan visitJob, visitChanSize)

// visitOnce 保证 worker 进程内单例。
var visitOnce sync.Once

// StartVisitWriter 启动批量写 worker(进程内单例,由 StartVisitRollup 调用)。
// 所有访问明细只经由这一个 goroutine 写库:SQLite 单写者模式下不放大锁竞争。
func StartVisitWriter() {
	visitOnce.Do(func() {
		go visitWriterLoop()
	})
}

func visitWriterLoop() {
	batch := make([]visitJob, 0, visitBatchMax)
	timer := time.NewTimer(visitFlushInterval)
	defer timer.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		logs := make([]po.VisitLog, 0, len(batch))
		visitors := make([]po.Visitor, 0, len(batch))
		for _, j := range batch {
			logs = append(logs, j.log)
			visitors = append(visitors, j.visitor)
		}
		batch = batch[:0]
		if err := dao.WriteVisitBatch(logs, visitors); err != nil {
			// fail-open:这一批丢了(统计缺一小段),业务毫无感知。
			logger.Warnf("[visit] 批量写入访问统计失败(%d 条): %v", len(logs), err)
		}
	}
	for {
		select {
		case j := <-visitChan:
			batch = append(batch, j)
			if len(batch) >= visitBatchMax {
				flush()
				timer.Reset(visitFlushInterval)
			}
		case <-timer.C:
			flush()
			timer.Reset(visitFlushInterval)
		}
	}
}

// RecordVisit 记录一次访问(异步,恒不返回错误)。
//
// 顺序:参数清洗 → 算访客键 → 投递到写队列。队列满则丢弃并告警。
// channel 在包初始化时创建(见上),此处无需判空;未启动 worker 时打点只进缓冲。
func RecordVisit(in VisitInput) {
	if !VisitEnabled() {
		return
	}
	ipHash, uaHash := hashVisitValue(strings.TrimSpace(in.IP)), hashVisitValue(strings.TrimSpace(in.UA))
	channel := normalizeChannel(in.Channel)
	key := visitorKeyOf(in, ipHash, uaHash)
	now := store.Now()
	visitDate := now[:10]
	// is_member / customer_id 只对「顾客会员」有意义:管理端员工的 ID 存在
	// 访客键(u: 前缀)里即可,不占会员口径 —— 否则员工登录会把会员 UV 撑高。
	isMember, customerID := 0, 0
	if in.CustomerID > 0 && channel != po.VisitChannelAdmin {
		isMember, customerID = 1, in.CustomerID
	}
	device, osName, browser, model := enrichDevice(in)
	j := visitJob{
		log: po.VisitLog{
			VisitDate:  visitDate,
			Channel:    channel,
			VisitorKey: key,
			IsMember:   isMember,
			CustomerID: customerID,
			Entry:      sanitizeEntry(in.Entry),
			IPHash:     ipHash,
			UAHash:     uaHash,
			CreateTime: now,

			IP:      clampText(in.IP, 45),
			City:    geoip.CityOf(in.IP),
			OS:      osName,
			Browser: browser,
			Device:  device,
			Model:   model,
		},
		visitor: po.Visitor{
			VisitorKey:     key,
			FirstVisitDate: visitDate,
			LastVisitDate:  visitDate,
			VisitCount:     1,
			IsMember:       isMember,
			CustomerID:     customerID,
			CreateTime:     now,
		},
	}
	select {
	case visitChan <- j:
	default:
		// 缓冲满:丢弃。正常量级永远到不了这里,真到了说明在被刷,
		// 此时保业务可用优先于统计完整。
		logger.Warnf("[visit] 打点缓冲已满,本次访问未记录(channel=%s)", channel)
	}
}

// ============================================================================
// 日聚合与清理(后台任务)
// ============================================================================

// visitJobInterval 后台任务间隔:补跑漏掉的 rollup + 清理过期明细。
// 30 分钟足够 —— 报表查询前也会 ensureRollup,定时任务只是兜底。
const visitJobInterval = 30 * time.Minute

// StartVisitRollup 启动访问统计后台任务:先补跑一轮(进程刚起来时正该把
// 停机期间累积的明细聚合掉),再按间隔循环。照 StartStaleOrderGuard 的范本
// (单轮 panic 只跳过本轮,不让协程退出)。
func StartVisitRollup() {
	StartVisitWriter()
	go func() {
		for {
			runVisitJobOnce()
			time.Sleep(visitJobInterval)
		}
	}()
}

// runVisitJobOnce 执行一轮:补跑 rollup + 清理过期明细。开关关闭时直接返回。
func runVisitJobOnce() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[visit] 后台任务单轮 panic(已跳过本轮): %v", r)
		}
	}()
	if !VisitEnabled() {
		return
	}
	ensureRollup()
	CleanExpiredVisits()
}

// ensureRollup 把明细中存在、但台账还没有的日期全部补算(绝对重算,幂等)。
//
// 两个调用场景:启动补跑(停机跨过了零点)与报表查询前(保证当天数据最新)。
//
// ⚠️ **今天与昨天必须无条件重算**,其它历史日期只在缺口时补:
//   - 今天:台账是绝对重算的结果,一旦今天已有一行,新访问不会自动进入 ——
//     只补「晚于台账最大日期」的天会让首页与报表停在上次聚合的数字上;
//   - 昨天:零点前后存在必然的竞态 —— 23:5x 的访问在 00:0x 才落库,而昨天的
//     台账早在 23:00 或零点前最后一次巡检时就定型了。若不重算昨天,这一天
//     会永远少记尾部(且明细清理后无法补救)。
//
// 重算一天是几条聚合查询,两天一起也不过十几条;相比「数字少了却无从解释」,
// 这点代价完全值得。
func ensureRollup() {
	dates, err := dao.VisitLogDates()
	if err != nil {
		logger.Warnf("[visit] 读取访问明细日期失败,跳过 rollup: %v", err)
		return
	}
	if len(dates) == 0 {
		return
	}
	maxRolled, err := dao.MaxRolledUpDate()
	if err != nil {
		logger.Warnf("[visit] 读取日聚合台账失败,跳过 rollup: %v", err)
		return
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	for _, d := range dates {
		if d <= maxRolled && d != today && d != yesterday {
			continue // 历史日期已落台账;不清空明细就无需重算
		}
		if err := dao.RollupDay(d); err != nil {
			logger.Warnf("[visit] 聚合 %s 的访问统计失败: %v", d, err)
			return
		}
	}
}

// CleanExpiredVisits 按保留天数清理过期访问明细(0 = 不清理)。
// 返回清理条数;失败只告警(清理是维护动作,不该影响任何查询)。
func CleanExpiredVisits() int {
	days := dao.VisitRetentionDays()
	if days <= 0 {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	n, err := dao.CleanVisitLogsBefore(cutoff)
	if err != nil {
		logger.Warnf("[visit] 清理过期访问明细失败(保留 %d 天): %v", days, err)
		return 0
	}
	if n > 0 {
		logger.Infof("[visit] 已清理 %s 之前的访问明细 %d 条(保留 %d 天)", cutoff, n, days)
	}
	return int(n)
}

// ============================================================================
// 报表查询
// ============================================================================

// VisitStats 访问统计的用例级出参(全是计数,没有金额)。
type VisitStats struct {
	Start string
	End   string

	PV       int // 区间 PV(台账逐日之和,无跨日重复问题)
	UV       int // 区间独立访客(明细去重;受保留期约束,见 VisitStatsOf 注释)
	NewUV    int // 区间新访客(访客注册表首访日期,精确)
	MemberUV int // 区间会员访客(明细去重,受保留期约束)

	TodayPV     int
	TodayUV     int
	TodayNewUV  int
	YesterdayPV int
	YesterdayUV int

	TotalVisitors int // 累计独立访客(访客注册表)
	MemberCount   int // 启用中会员数(tb_customer,「用户量」精确口径)

	Daily    []VisitDailyRow
	Channels []VisitChannelRow
	// Entries 扫码入口排行:哪张桌的二维码被扫得最多(仅 H5,一期就要展示)。
	Entries []VisitEntryRow
	// 终端构成(设备类型/操作系统/浏览器),来自明细,受保留期约束。
	Devices  []VisitDistRow
	Oses     []VisitDistRow
	Browsers []VisitDistRow
}

// VisitDistRow 终端构成分布的一行(报表出参)。
type VisitDistRow struct {
	Name string `json:"name"`
	PV   int    `json:"pv"`
}

// VisitEntryRow 扫码入口排行的一行。
type VisitEntryRow struct {
	Entry string `json:"entry"` // 点餐码 / 历史数字桌台 ID
	Label string `json:"label"` // 供前端直接渲染的名称(桌名 · 桌号;关联不到时回退点餐码)
	PV    int    `json:"pv"`
}

// VisitDailyRow 报表里一天的访问数据(pv/uv/newUv + 分渠道 PV)。
type VisitDailyRow struct {
	Date    string `json:"date"`
	PV      int    `json:"pv"`
	UV      int    `json:"uv"`
	NewUV   int    `json:"newUv"`
	H5PV    int    `json:"h5Pv"`
	MPPV    int    `json:"mpPv"`
	AdminPV int    `json:"adminPv"`
}

// VisitChannelRow 报表里一个渠道的访问数据。
type VisitChannelRow struct {
	Channel string `json:"channel"`
	Label   string `json:"label"`
	PV      int    `json:"pv"`
	UV      int    `json:"uv"`
}

// visitChannelLabels 渠道中文名(与后端枚举一一对应)。
var visitChannelLabels = map[string]string{
	po.VisitChannelH5:          "扫码点餐",
	po.VisitChannelMiniprogram: "小程序",
	po.VisitChannelAdmin:       "管理后台",
}

// VisitStatsOf 汇总区间访问统计。
//
// 口径:
//   - PV / 逐日数据全读日聚合台账(长期保留,快);
//   - 区间 UV 与会员 UV 读明细 COUNT(DISTINCT)(精确),但明细只保留
//     VISIT_RETENTION_DAYS 天 —— 区间早于保留期时结果为下限,报表页已注明;
//   - 区间新访客读访客注册表(精确,不受保留期影响);
//   - 「今日/昨日」来自台账;查询前 ensureRollup 会把当天重算一遍
//     (绝对重算幂等),因此台账永远最新。
func VisitStatsOf(startStr, endStr, daysStr string) VisitStats {
	start, end := resolveRange(startStr, endStr, daysStr, 30, maxRangeDays)
	startDay, endDay := start.Format("2006-01-02"), end.Format("2006-01-02")
	// 查询前保证台账连续(当天 + 停机缺口)。失败不阻断:退回读旧台账。
	ensureRollup()

	rows, err := dao.VisitDailyRange(startDay, endDay)
	if err != nil {
		logger.Warnf("[visit] 读取日聚合台账失败: %v", err)
		rows = nil
	}

	// 按日期归拢:all 行进汇总,分渠道行进各自渠道的 PV。
	type dayAgg struct {
		pv, uv, newUV int
		byChannel     map[string]int
	}
	days := map[string]*dayAgg{}
	for _, r := range rows {
		d := days[r.VisitDate]
		if d == nil {
			d = &dayAgg{byChannel: map[string]int{}}
			days[r.VisitDate] = d
		}
		if r.Channel == po.VisitChannelAll {
			d.pv, d.uv, d.newUV = r.PV, r.UV, r.NewUV
			continue
		}
		d.byChannel[r.Channel] += r.PV
	}

	out := VisitStats{Start: startDay, End: endDay}
	// 逐日输出:区间内缺失日期补零(与 DailyTrend 同一约定)。
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		row := VisitDailyRow{Date: key}
		if agg := days[key]; agg != nil {
			row.PV, row.UV, row.NewUV = agg.pv, agg.uv, agg.newUV
			row.H5PV = agg.byChannel[po.VisitChannelH5]
			row.MPPV = agg.byChannel[po.VisitChannelMiniprogram]
			row.AdminPV = agg.byChannel[po.VisitChannelAdmin]
			out.PV += row.PV
		}
		out.Daily = append(out.Daily, row)
	}

	// 今日/昨日:从台账直接取(查询前已 ensureRollup,当天数据是最新的)。
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if agg := days[today]; agg != nil {
		out.TodayPV, out.TodayUV, out.TodayNewUV = agg.pv, agg.uv, agg.newUV
	}
	if agg := days[yesterday]; agg != nil {
		out.YesterdayPV, out.YesterdayUV = agg.pv, agg.uv
	}

	// 区间 UV / 会员 UV:明细窗口内精确去重。
	if n, err := dao.DistinctVisitorsInRange(startDay, endDay); err == nil {
		out.UV = n
	} else {
		logger.Warnf("[visit] 统计区间 UV 失败: %v", err)
	}
	if n, err := dao.DistinctMemberVisitorsInRange(startDay, endDay); err == nil {
		out.MemberUV = n
	}
	// 区间新访客:访客注册表首访日期落在区间内(精确)。
	if n, err := dao.NewVisitorsInRange(startDay, endDay); err == nil {
		out.NewUV = n
	}

	// 渠道构成:PV 来自台账(长期完整),UV 来自明细窗口(COUNT DISTINCT)。
	channelPV := map[string]int{}
	for _, agg := range days {
		for ch, pv := range agg.byChannel {
			channelPV[ch] += pv
		}
	}
	channelUV := map[string]int{}
	if uvRows, err := dao.ChannelUVInRange(startDay, endDay); err == nil {
		for _, r := range uvRows {
			channelUV[r.Channel] = r.UV
		}
	}
	for _, ch := range []string{po.VisitChannelH5, po.VisitChannelMiniprogram, po.VisitChannelAdmin} {
		if channelPV[ch] == 0 && channelUV[ch] == 0 {
			continue
		}
		out.Channels = append(out.Channels, VisitChannelRow{
			Channel: ch,
			Label:   visitChannelLabels[ch],
			PV:      channelPV[ch],
			UV:      channelUV[ch],
		})
	}

	// 扫码入口排行(桌台码 Top 10):先取「码 → 次数」(单表查询,无方言坑),
	// 再在 Go 侧关联桌台名 —— H5 与小程序都带桌台码,两种关联形态:
	//   8 位点餐码 → table_code;老二维码的数字桌台 ID → table_id。
	// 关联不到的(桌台已删除)回退显示码值本身。
	codes, err := dao.TopVisitEntryCodes(startDay, endDay, 10)
	if err != nil {
		// 入口榜失败不影响主指标(PV/UV 已算完),但必须留痕:
		// 「榜是空的」和「榜查不动了」在界面上看不出区别。
		logger.Warnf("[visit] 统计扫码入口排行失败: %v", err)
	}
	if tables, terr := dao.ListTableIdentities(); terr != nil {
		logger.Warnf("[visit] 读取桌台清单(入口榜标签用)失败: %v", terr)
	} else {
		byKey := make(map[string]dao.TableIdentity, len(tables)*2)
		for _, tb := range tables {
			if tb.TableCode != "" {
				byKey[tb.TableCode] = tb
			}
			byKey[strconv.Itoa(tb.TableID)] = tb
		}
		for _, c := range codes {
			label := c.Entry
			if tb, ok := byKey[c.Entry]; ok {
				label = tb.TableNo + " 号桌"
				if tb.TableName != "" && tb.TableName != tb.TableNo {
					label = tb.TableName + " · " + tb.TableNo + " 号桌"
				}
			}
			out.Entries = append(out.Entries, VisitEntryRow{Entry: c.Entry, Label: label, PV: c.PV})
		}
	}

	// 终端构成:三个维度各自 GROUP BY(列名走 dao 白名单);空值归一为「未知」。
	for dim, dst := range map[string]*[]VisitDistRow{
		"device": &out.Devices, "os": &out.Oses, "browser": &out.Browsers,
	} {
		rows, err := dao.VisitDistribution(startDay, endDay, dim)
		if err != nil {
			logger.Warnf("[visit] 统计终端分布(%s)失败: %v", dim, err)
			continue
		}
		out2 := make([]VisitDistRow, 0, len(rows))
		for _, r := range rows {
			name := r.Name
			if strings.TrimSpace(name) == "" {
				name = "未知"
			}
			out2 = append(out2, VisitDistRow{Name: name, PV: r.PV})
		}
		*dst = out2
	}

	if n, err := dao.CountVisitors(); err == nil {
		out.TotalVisitors = n
	}
	if n, err := dao.CountMembers(); err == nil {
		out.MemberCount = n
	}
	return out
}
