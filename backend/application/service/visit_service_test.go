package service

import (
	"path/filepath"
	"testing"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// visitSvcInit 初始化独立测试库并启动批量写 worker(与 boot 阶段一致)。
func visitSvcInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "visit_svc.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// TestVisitVisitorKeyOf 访客键的四种前缀与优先级。
// u: 前缀是必须的:员工 ID 与会员 ID 各自从 1 开始,不加前缀会撞键。
func TestVisitVisitorKeyOf(t *testing.T) {
	cases := []struct {
		name string
		in   VisitInput
		want string
	}{
		{"会员优先", VisitInput{Channel: po.VisitChannelMiniprogram, CustomerID: 7, VisitorID: "abc"}, "m:7"},
		{"管理端员工独立空间", VisitInput{Channel: po.VisitChannelAdmin, CustomerID: 7, VisitorID: "abc"}, "u:7"},
		{"前端访客ID", VisitInput{Channel: po.VisitChannelH5, VisitorID: "abc-123"}, "c:abc-123"},
		{"兜底IP+UA", VisitInput{Channel: po.VisitChannelH5}, "a:iph:uah"},
	}
	for _, c := range cases {
		if got := visitorKeyOf(c.in, "iph", "uah"); got != c.want {
			t.Errorf("%s: visitorKeyOf = %q, 期望 %q", c.name, got, c.want)
		}
	}
}

// TestVisitSanitize 参数清洗:非法字符剥掉、超长截断,防任意文本入库。
func TestVisitSanitize(t *testing.T) {
	if got := sanitizeVisitorID(" abc-123_xyz "); got != "abc-123xyz" {
		t.Errorf("sanitizeVisitorID = %q, 期望 abc-123xyz", got)
	}
	if got := sanitizeVisitorID("12345678901234567890123456789012345678901234"); got != "1234567890123456789012345678901234567890" {
		t.Errorf("超长 visitorId 应截到 40 位, got %d 位", len(got))
	}
	if got := sanitizeEntry("K7M3PQ9X"); got != "K7M3PQ9X" {
		t.Errorf("sanitizeEntry = %q, 期望 K7M3PQ9X", got)
	}
	if got := sanitizeEntry("k7m3-pq9x!!"); got != "k7m3pq9x" {
		t.Errorf("sanitizeEntry 应剥掉非字母数字, got %q", got)
	}
	if got := sanitizeEntry("ABCDEFGHIJKL"); got != "ABCDEFGH" {
		t.Errorf("sanitizeEntry 应截到 8 位, got %q", got)
	}
}

// TestVisitNormalizeChannel 渠道白名单:未知值归 h5 且不炸。
func TestVisitNormalizeChannel(t *testing.T) {
	for in, want := range map[string]string{
		"h5": po.VisitChannelH5, "": po.VisitChannelH5, "H5": po.VisitChannelH5,
		"miniprogram": po.VisitChannelMiniprogram, "admin": po.VisitChannelAdmin,
		"evil": po.VisitChannelH5,
	} {
		if got := normalizeChannel(in); got != want {
			t.Errorf("normalizeChannel(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestVisitRecordAndStats 端到端:打点 → worker 落库 → rollup → 报表数字对得上。
// 这是整个特性最核心的一条链路,任何一环断了这里就红。
func TestVisitRecordAndStats(t *testing.T) {
	visitSvcInit(t)
	StartVisitWriter()

	today := time.Now().Format("2006-01-02")
	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "aaa", Entry: "K7M3PQ9X", IP: "1.1.1.1", UA: "ua-1"})
	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "aaa", Entry: "K7M3PQ9X", IP: "1.1.1.1", UA: "ua-1"})
	RecordVisit(VisitInput{Channel: po.VisitChannelMiniprogram, VisitorID: "bbb", IP: "2.2.2.2", UA: "ua-2"})
	// worker 是异步攒批的:等一个 flush 周期(500ms)再断言。
	time.Sleep(800 * time.Millisecond)

	var logCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log`).Scan(&logCount); err != nil {
		t.Fatalf("查询明细失败: %v", err)
	}
	if logCount != 3 {
		t.Fatalf("明细行数 = %d, 期望 3", logCount)
	}
	// IP/UA 不明文落库。
	var ipHash, uaHash string
	if err := store.DB.QueryRow(`SELECT COALESCE(ip_hash,''), COALESCE(ua_hash,'') FROM tb_visit_log LIMIT 1`).Scan(&ipHash, &uaHash); err != nil {
		t.Fatalf("查询哈希失败: %v", err)
	}
	if ipHash == "1.1.1.1" || ipHash == "" || uaHash == "ua-1" || uaHash == "" {
		t.Fatalf("IP/UA 必须哈希且非空: ip=%q ua=%q", ipHash, uaHash)
	}
	// entry 落库(桌台码,报表页要展示)。
	var entry string
	if err := store.DB.QueryRow(`SELECT entry FROM tb_visit_log WHERE channel='h5' LIMIT 1`).Scan(&entry); err != nil {
		t.Fatalf("查询 entry 失败: %v", err)
	}
	if entry != "K7M3PQ9X" {
		t.Fatalf("entry = %q, 期望 K7M3PQ9X", entry)
	}

	stats := VisitStatsOf("", "", "7")
	if stats.TodayPV != 3 {
		t.Fatalf("今日 PV = %d, 期望 3", stats.TodayPV)
	}
	if stats.TodayUV != 2 {
		t.Fatalf("今日 UV = %d, 期望 2(aaa/bbb)", stats.TodayUV)
	}
	if stats.TodayNewUV != 2 {
		t.Fatalf("今日新访客 = %d, 期望 2", stats.TodayNewUV)
	}
	if stats.PV != 3 || stats.UV != 2 {
		t.Fatalf("区间汇总 PV/UV = %d/%d, 期望 3/2", stats.PV, stats.UV)
	}
	if len(stats.Daily) != 7 {
		t.Fatalf("逐日行数 = %d, 期望 7(缺日补零)", len(stats.Daily))
	}
	last := stats.Daily[len(stats.Daily)-1]
	if last.Date != today || last.PV != 3 || last.UV != 2 {
		t.Fatalf("最后一天(今天) = %+v, 期望 pv=3 uv=2", last)
	}
	// 渠道构成:h5 2 次、小程序 1 次。
	chPV := map[string]int{}
	for _, c := range stats.Channels {
		chPV[c.Channel] = c.PV
	}
	if chPV[po.VisitChannelH5] != 2 || chPV[po.VisitChannelMiniprogram] != 1 {
		t.Fatalf("渠道 PV = %v, 期望 h5=2 mp=1", chPV)
	}
	// 累计访客与今日访问都写进了注册表。
	if stats.TotalVisitors != 2 {
		t.Fatalf("累计访客 = %d, 期望 2", stats.TotalVisitors)
	}
}

// TestVisitStatsDisabled 开关关闭时:打点不进库,报表接口仍可返回(空数据)。
func TestVisitStatsDisabled(t *testing.T) {
	visitSvcInit(t)
	t.Setenv("VISIT_STAT_ENABLED", "0")
	StartVisitWriter()

	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "aaa"})
	time.Sleep(300 * time.Millisecond)

	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log`).Scan(&n); err != nil {
		t.Fatalf("查询明细失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("关闭时不应落库, 实际 %d 行", n)
	}
	stats := VisitStatsOf("", "", "7")
	if stats.TodayPV != 0 || len(stats.Daily) != 7 {
		t.Fatalf("关闭时报表应为空数据: %+v", stats)
	}
}

// TestVisitRateLimit 每 IP 每分钟上限:前 N 次放行,之后拒绝。
func TestVisitRateLimit(t *testing.T) {
	t.Setenv("VISIT_RATE_PER_MIN", "3")
	ip := "10.0.0.9"
	for i := 0; i < 3; i++ {
		if !AllowVisitIP(ip) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if AllowVisitIP(ip) {
		t.Fatal("第 4 次应被限流")
	}
	// 换一个 IP 不受影响。
	if !AllowVisitIP("10.0.0.10") {
		t.Fatal("其它 IP 应放行")
	}
	// 空 IP(取不到来源)不限流:内网探测场景不该把统计自己打挂。
	if !AllowVisitIP("") {
		t.Fatal("空 IP 应放行")
	}
}

// TestVisitCleanExpired 按保留天数清理:过期明细删、当天的留。
func TestVisitCleanExpired(t *testing.T) {
	visitSvcInit(t)
	t.Setenv("VISIT_RETENTION_DAYS", "30")

	old := time.Now().AddDate(0, 0, -40).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")
	now := store.Now()
	for _, d := range []string{old, today} {
		if _, err := store.DB.Exec(`INSERT INTO tb_visit_log(visit_date, channel, visitor_key, create_time) VALUES(?,?,?,?)`,
			d, po.VisitChannelH5, "c:"+d, now); err != nil {
			t.Fatalf("插入明细失败: %v", err)
		}
	}
	if n := CleanExpiredVisits(); n != 1 {
		t.Fatalf("清理条数 = %d, 期望 1", n)
	}
	var left int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log`).Scan(&left); err != nil {
		t.Fatalf("查询剩余失败: %v", err)
	}
	if left != 1 {
		t.Fatalf("剩余 = %d, 期望 1(只留今天的)", left)
	}
}

// TestVisitEnsureRollupCatchUp 停机补跑:明细有天、台账缺天,补跑后补齐且幂等。
func TestVisitEnsureRollupCatchUp(t *testing.T) {
	visitSvcInit(t)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_visit_log(visit_date, channel, visitor_key, create_time) VALUES(?,?,?,?)`,
		yesterday, po.VisitChannelH5, "c:x", now); err != nil {
		t.Fatalf("插入明细失败: %v", err)
	}
	ensureRollup()
	rows, err := dao.VisitDailyRange(yesterday, yesterday)
	if err != nil {
		t.Fatalf("VisitDailyRange: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("补跑后台账应有昨天的行")
	}
	// 再跑一遍不报错、不翻倍(绝对重算)。
	ensureRollup()
	rows2, _ := dao.VisitDailyRange(yesterday, yesterday)
	if len(rows2) != len(rows) {
		t.Fatalf("重复补跑行数漂移: %d vs %d", len(rows2), len(rows))
	}
}

// TestVisitStatsRefreshesToday 报表必须反映「今天的实时数据」。
//
// 回归:ensureRollup 早先只补「晚于台账最大日期」的天,当天一旦被聚合过一次,
// 后续新访问就再也不会进入台账 —— 首页与报表会一直停在上次聚合的数字上。
// 这条用例先报一次看数,再报一次再看数,专治这种「看着有数、其实是旧的」。
func TestVisitStatsRefreshesToday(t *testing.T) {
	visitSvcInit(t)
	StartVisitWriter()

	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "first", IP: "1.1.1.1", UA: "ua"})
	time.Sleep(800 * time.Millisecond)
	if got := VisitStatsOf("", "", "7").TodayPV; got != 1 {
		t.Fatalf("首次查询今日 PV = %d, 期望 1", got)
	}

	// 第二次访问:台账已有今天这一行,必须靠「当天无条件重算」才能进来。
	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "second", IP: "2.2.2.2", UA: "ua"})
	time.Sleep(800 * time.Millisecond)
	stats := VisitStatsOf("", "", "7")
	if stats.TodayPV != 2 {
		t.Fatalf("新增访问后今日 PV = %d, 期望 2(台账必须重算当天)", stats.TodayPV)
	}
	if stats.TodayUV != 2 {
		t.Fatalf("新增访问后今日 UV = %d, 期望 2", stats.TodayUV)
	}
}

// TestVisitStatsRollsUpYesterdayTail 昨天的台账已定型后,零点前后补落库的明细
// 仍必须进入台账。
//
// 场景:23:00 巡检已把「昨天」聚合过一次;23:5x 的访问在 00:0x 才 flush 到明细。
// 若只重算「晚于台账最大日期」的天,这一天会永远少记尾部,且明细清理后无法补救。
func TestVisitStatsRollsUpYesterdayTail(t *testing.T) {
	visitSvcInit(t)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	now := store.Now()
	insert := func(key string) {
		t.Helper()
		if _, err := store.DB.Exec(`INSERT INTO tb_visit_log(visit_date, channel, visitor_key, create_time) VALUES(?,?,?,?)`,
			yesterday, po.VisitChannelH5, key, now); err != nil {
			t.Fatalf("插入明细失败: %v", err)
		}
	}

	// 第一步:先有一条明细,聚合出昨天这一行(模拟 23:00 的巡检)。
	insert("c:early")
	ensureRollup()

	// 第二步:零点后才落库的「昨天尾巴」。
	insert("c:late")

	// 报表查询时必须重算昨天,把尾巴算进去。
	stats := VisitStatsOf(yesterday, yesterday, "")
	if stats.PV != 2 {
		t.Fatalf("昨日 PV = %d, 期望 2(尾巴必须被重算)", stats.PV)
	}
	if stats.UV != 2 {
		t.Fatalf("昨日 UV = %d, 期望 2", stats.UV)
	}
}

// TestVisitStatsEntries 打点带的桌台码要一路走到报表:落库 → 入口榜 → 出参标签。
//
// 两个渠道都会带桌台码(H5 路由参数 / 小程序码 scene),榜上看的是「哪张桌引来的
// 客人」,所以 entry 不能在中途丢掉 —— 丢了的表现正是「趋势有数、入口榜空」。
func TestVisitStatsEntries(t *testing.T) {
	visitSvcInit(t)
	StartVisitWriter()

	RecordVisit(VisitInput{Channel: po.VisitChannelH5, VisitorID: "a", Entry: "K7M3PQ9X", IP: "1.1.1.1", UA: "ua"})
	RecordVisit(VisitInput{Channel: po.VisitChannelMiniprogram, VisitorID: "b", Entry: "8YG3Q335", IP: "2.2.2.2", UA: "ua"})
	time.Sleep(800 * time.Millisecond)

	var stored string
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_visit_log WHERE entry<>''`).Scan(&stored); err != nil {
		t.Fatalf("查询明细失败: %v", err)
	}
	if stored != "2" {
		t.Fatalf("带桌台码的明细 = %s 条, 期望 2(entry 必须在落库时不丢)", stored)
	}

	// 种一张桌台:K7M3PQ9X 应被关联成「桌名 · 桌号」;关联不到的 8YG3Q335
	// 回退为码值本身(历史数字桌号同理,走 strconv.Itoa(table_id) 匹配)。
	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, del_flag, table_code, create_time, update_time)
		VALUES('01','大厅01桌',4,0,'0','K7M3PQ9X',?,?)`, now, now); err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}

	stats := VisitStatsOf("", "", "7")
	if len(stats.Entries) != 2 {
		t.Fatalf("入口榜 = %+v, 期望 2 条", stats.Entries)
	}
	byEntry := map[string]VisitEntryRow{}
	for _, e := range stats.Entries {
		byEntry[e.Entry] = e
	}
	if byEntry["K7M3PQ9X"].PV != 1 {
		t.Fatalf("入口榜按码聚合不正确: %+v", stats.Entries)
	}
	if got := byEntry["K7M3PQ9X"].Label; got != "大厅01桌 · 01 号桌" {
		t.Fatalf("K7M3PQ9X 标签 = %q, 期望 大厅01桌 · 01 号桌", got)
	}
	if got := byEntry["8YG3Q335"].Label; got != "8YG3Q335" {
		t.Fatalf("关联不到桌台时应回退码值, got %q", got)
	}
}
