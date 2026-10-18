package dao

import (
	"path/filepath"
	"testing"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// visitInit 初始化独立 SQLite 测试库(建表含访问统计三张表)。
func visitInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "visit.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// visitLog 构造一条明细(日期/渠道/访客键可指定,其余用稳定值)。
func visitLog(date, channel, key string) po.VisitLog {
	return po.VisitLog{
		VisitDate:  date,
		Channel:    channel,
		VisitorKey: key,
		IPHash:     "iph",
		UAHash:     "uah",
		CreateTime: date + " 10:00:00",
	}
}

// visitLogMember 构造一条「登录会员」的明细(is_member=1,会员 UV 口径靠它)。
func visitLogMember(date, channel, key string, customerID int) po.VisitLog {
	l := visitLog(date, channel, key)
	l.IsMember = 1
	l.CustomerID = customerID
	return l
}

func visitVisitor(key, date string, member int) po.Visitor {
	return po.Visitor{
		VisitorKey:     key,
		FirstVisitDate: date,
		LastVisitDate:  date,
		VisitCount:     1,
		IsMember:       member,
		CreateTime:     date + " 10:00:00",
	}
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

// TestVisitWriteBatch 一批写入 = 明细全进 + 访客档案「新客建档」。
func TestVisitWriteBatch(t *testing.T) {
	visitInit(t)

	logs := []po.VisitLog{
		visitLog("2026-10-15", po.VisitChannelH5, "c:aaa"),
		visitLogMember("2026-10-15", po.VisitChannelMiniprogram, "m:7", 7),
		visitLog("2026-10-15", po.VisitChannelAdmin, "u:3"),
	}
	visitors := []po.Visitor{
		visitVisitor("c:aaa", "2026-10-15", 0),
		visitVisitor("m:7", "2026-10-15", 1),
		visitVisitor("u:3", "2026-10-15", 0),
	}
	if err := WriteVisitBatch(logs, visitors); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}
	if got := countRows(t, "tb_visit_log"); got != 3 {
		t.Fatalf("明细行数 = %d, 期望 3", got)
	}
	if got := countRows(t, "tb_visitor"); got != 3 {
		t.Fatalf("访客档案行数 = %d, 期望 3", got)
	}
	// 会员访客的 is_member 必须落对:会员 UV 口径靠它。
	var isMember, customerID int
	if err := store.DB.QueryRow(`SELECT is_member, customer_id FROM tb_visit_log WHERE visitor_key='m:7'`).Scan(&isMember, &customerID); err != nil {
		t.Fatalf("查询会员明细失败: %v", err)
	}
	if isMember != 1 {
		t.Fatalf("会员明细 is_member = %d, 期望 1", isMember)
	}
}

// TestVisitWriteBatchRepeatVisitor 同一访客再次来访:明细新增、档案不新增,
// 次数累加、最近日期推后(先 UPDATE 后 INSERT 的路径都走到)。
func TestVisitWriteBatchRepeatVisitor(t *testing.T) {
	visitInit(t)

	batch1 := []po.VisitLog{visitLog("2026-10-15", po.VisitChannelH5, "c:aaa")}
	if err := WriteVisitBatch(batch1, []po.Visitor{visitVisitor("c:aaa", "2026-10-15", 0)}); err != nil {
		t.Fatalf("首批写入: %v", err)
	}
	second := []po.VisitLog{visitLog("2026-10-16", po.VisitChannelH5, "c:aaa")}
	if err := WriteVisitBatch(second, []po.Visitor{visitVisitor("c:aaa", "2026-10-16", 0)}); err != nil {
		t.Fatalf("二批写入: %v", err)
	}
	if got := countRows(t, "tb_visit_log"); got != 2 {
		t.Fatalf("明细行数 = %d, 期望 2", got)
	}
	if got := countRows(t, "tb_visitor"); got != 1 {
		t.Fatalf("访客档案行数 = %d, 期望 1(老客不新建档)", got)
	}
	var cnt int
	var first, last string
	if err := store.DB.QueryRow(`SELECT visit_count, first_visit_date, last_visit_date FROM tb_visitor WHERE visitor_key='c:aaa'`).
		Scan(&cnt, &first, &last); err != nil {
		t.Fatalf("查询访客档案失败: %v", err)
	}
	if cnt != 2 || first != "2026-10-15" || last != "2026-10-16" {
		t.Fatalf("档案维护不正确: count=%d first=%s last=%s", cnt, first, last)
	}
}

// TestVisitWriteBatchSameKeyInBatch 同批同一访客多条:档案只写一行,次数按条数累加。
func TestVisitWriteBatchSameKeyInBatch(t *testing.T) {
	visitInit(t)

	logs := []po.VisitLog{
		visitLog("2026-10-15", po.VisitChannelH5, "c:aaa"),
		visitLog("2026-10-15", po.VisitChannelH5, "c:aaa"),
	}
	visitors := []po.Visitor{
		visitVisitor("c:aaa", "2026-10-15", 0),
		visitVisitor("c:aaa", "2026-10-15", 0),
	}
	if err := WriteVisitBatch(logs, visitors); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}
	if got := countRows(t, "tb_visitor"); got != 1 {
		t.Fatalf("访客档案行数 = %d, 期望 1", got)
	}
	var cnt int
	if err := store.DB.QueryRow(`SELECT visit_count FROM tb_visitor WHERE visitor_key='c:aaa'`).Scan(&cnt); err != nil {
		t.Fatalf("查询访客档案失败: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("visit_count = %d, 期望 2", cnt)
	}
}

// TestVisitWriteBatchEmpty 空明细直接返回,不建事务(worker 定时 flush 会调它)。
func TestVisitWriteBatchEmpty(t *testing.T) {
	visitInit(t)
	if err := WriteVisitBatch(nil, nil); err != nil {
		t.Fatalf("空批次应直接返回: %v", err)
	}
}

// TestVisitRollupDay rollup 的四个口径:全站 PV = 渠道之和、全站 UV 跨渠道去重、
// 新访客按注册表、会员 UV 只数登录会员;且重复执行结果一致(幂等重算)。
func TestVisitRollupDay(t *testing.T) {
	visitInit(t)

	const day = "2026-10-15"
	logs := []po.VisitLog{
		// 渠道 A:h5,两个访客(其中一个是会员)
		visitLog(day, po.VisitChannelH5, "c:aaa"),
		visitLogMember(day, po.VisitChannelH5, "m:7", 7),
		visitLog(day, po.VisitChannelH5, "c:aaa"), // 同一访客第二次,PV+1 但 UV 不变
		// 渠道 B:小程序,一个会员访客(与 h5 的 m:7 是同一人 → 全站 UV 去重)
		visitLogMember(day, po.VisitChannelMiniprogram, "m:7", 7),
		// 渠道 C:管理端,一个员工
		visitLog(day, po.VisitChannelAdmin, "u:3"),
	}
	visitors := []po.Visitor{
		visitVisitor("c:aaa", day, 0),
		visitVisitor("m:7", "2026-10-14", 1), // 首访是昨天 → 今天不算新访客
		visitVisitor("u:3", day, 0),          // 员工今天首访 → 计入新访客
	}
	if err := WriteVisitBatch(logs, visitors); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}

	if err := RollupDay(day); err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	rows := dailyRows(t, day)
	all := rows[po.VisitChannelAll]
	if all.PV != 5 {
		t.Fatalf("全站 PV = %d, 期望 5(渠道之和)", all.PV)
	}
	if all.UV != 3 {
		t.Fatalf("全站 UV = %d, 期望 3(c:aaa / m:7 / u:3)", all.UV)
	}
	if all.MemberUV != 1 {
		t.Fatalf("会员 UV = %d, 期望 1(仅 m:7)", all.MemberUV)
	}
	if all.NewUV != 2 {
		t.Fatalf("新访客 = %d, 期望 2(c:aaa 与 u:3 今天首访,m:7 是昨天)", all.NewUV)
	}
	if rows[po.VisitChannelH5].PV != 3 || rows[po.VisitChannelH5].UV != 2 {
		t.Fatalf("h5 渠道 = %+v, 期望 pv=3 uv=2", rows[po.VisitChannelH5])
	}
	if rows[po.VisitChannelMiniprogram].PV != 1 || rows[po.VisitChannelAdmin].PV != 1 {
		t.Fatalf("小程序/管理端渠道 PV 不对: %+v", rows)
	}

	// 幂等:再跑一次,数字不能翻倍(先 UPDATE 后 INSERT 的更新路径)。
	if err := RollupDay(day); err != nil {
		t.Fatalf("RollupDay(第二次): %v", err)
	}
	again := dailyRows(t, day)
	if again[po.VisitChannelAll] != all {
		t.Fatalf("重复 rollup 后数字漂移: %+v vs %+v", again[po.VisitChannelAll], all)
	}
	if got := countRows(t, "tb_visit_daily"); got != 4 {
		t.Fatalf("台账行数 = %d, 期望 4(all + 3 渠道)", got)
	}
}

// dailyRows 取某一天的台账,按渠道索引。
func dailyRows(t *testing.T, date string) map[string]po.VisitDaily {
	t.Helper()
	rows, err := VisitDailyRange(date, date)
	if err != nil {
		t.Fatalf("VisitDailyRange: %v", err)
	}
	out := map[string]po.VisitDaily{}
	for _, r := range rows {
		out[r.Channel] = r
	}
	if len(out) == 0 {
		t.Fatalf("%s 没有台账行", date)
	}
	return out
}

// TestVisitCleanLogsBefore 清理按「日期」整段删,边界日期(等于 cutoff)保留。
func TestVisitCleanLogsBefore(t *testing.T) {
	visitInit(t)

	dates := []string{"2026-07-01", "2026-07-02", "2026-07-03"}
	for _, d := range dates {
		if err := WriteVisitBatch([]po.VisitLog{visitLog(d, po.VisitChannelH5, "c:"+d)}, nil); err != nil {
			t.Fatalf("写入 %s 明细: %v", d, err)
		}
	}
	// cutoff = 07-02:删 07-01,保留 07-02 与 07-03。
	n, err := CleanVisitLogsBefore("2026-07-02")
	if err != nil {
		t.Fatalf("CleanVisitLogsBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("清理条数 = %d, 期望 1", n)
	}
	left := countRows(t, "tb_visit_log")
	if left != 2 {
		t.Fatalf("剩余明细 = %d, 期望 2", left)
	}
}

// TestVisitRangeQueries 区间查询:UV 去重、新访客按注册表、渠道 UV。
func TestVisitRangeQueries(t *testing.T) {
	visitInit(t)

	logs := []po.VisitLog{
		visitLog("2026-10-14", po.VisitChannelH5, "c:aaa"),
		visitLog("2026-10-15", po.VisitChannelH5, "c:aaa"),
		visitLog("2026-10-15", po.VisitChannelMiniprogram, "c:aaa"), // 同一访客跨渠道
		visitLogMember("2026-10-15", po.VisitChannelMiniprogram, "m:7", 7),
	}
	visitors := []po.Visitor{
		visitVisitor("c:aaa", "2026-10-14", 0),
		visitVisitor("m:7", "2026-10-15", 1),
	}
	if err := WriteVisitBatch(logs, visitors); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}

	if n, err := DistinctVisitorsInRange("2026-10-14", "2026-10-15"); err != nil || n != 2 {
		t.Fatalf("区间 UV = %d(err=%v), 期望 2", n, err)
	}
	if n, err := DistinctMemberVisitorsInRange("2026-10-14", "2026-10-15"); err != nil || n != 1 {
		t.Fatalf("会员 UV = %d(err=%v), 期望 1", n, err)
	}
	if n, err := NewVisitorsInRange("2026-10-14", "2026-10-15"); err != nil || n != 2 {
		t.Fatalf("新访客 = %d(err=%v), 期望 2", n, err)
	}
	chans, err := ChannelUVInRange("2026-10-14", "2026-10-15")
	if err != nil {
		t.Fatalf("ChannelUVInRange: %v", err)
	}
	got := map[string]int{}
	for _, c := range chans {
		got[c.Channel] = c.UV
	}
	if got[po.VisitChannelH5] != 1 || got[po.VisitChannelMiniprogram] != 2 {
		t.Fatalf("渠道 UV = %v, 期望 h5=1 miniprogram=2", got)
	}
	if n, err := CountVisitors(); err != nil || n != 2 {
		t.Fatalf("累计访客 = %d(err=%v), 期望 2", n, err)
	}
	if n, err := CountMembers(); err != nil || n != 0 {
		t.Fatalf("会员数 = %d(err=%v), 期望 0(空 tb_customer)", n, err)
	}
	// 会员数种一条:CountMembers 只数启用中的。
	if _, err := store.DB.Exec(`INSERT INTO tb_customer(nickname, status, create_time) VALUES('x',1,'2026-10-01 00:00:00')`); err != nil {
		t.Fatalf("插入会员失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_customer(nickname, status, create_time) VALUES('y',0,'2026-10-01 00:00:00')`); err != nil {
		t.Fatalf("插入停用会员失败: %v", err)
	}
	if n, err := CountMembers(); err != nil || n != 1 {
		t.Fatalf("会员数 = %d(err=%v), 期望 1(停用不计)", n, err)
	}
}

// TestVisitLogDatesAndMaxRolledUp 启动补跑依赖的两个查询:明细日期清单 + 台账最大日期。
func TestVisitLogDatesAndMaxRolledUp(t *testing.T) {
	visitInit(t)

	for _, d := range []string{"2026-10-15", "2026-10-16"} {
		if err := WriteVisitBatch([]po.VisitLog{visitLog(d, po.VisitChannelH5, "c:x")}, nil); err != nil {
			t.Fatalf("写入 %s: %v", d, err)
		}
	}
	dates, err := VisitLogDates()
	if err != nil {
		t.Fatalf("VisitLogDates: %v", err)
	}
	if len(dates) != 2 || dates[0] != "2026-10-15" || dates[1] != "2026-10-16" {
		t.Fatalf("明细日期清单 = %v, 期望升序两天", dates)
	}
	maxRolled, err := MaxRolledUpDate()
	if err != nil || maxRolled != "" {
		t.Fatalf("空台账 MaxRolledUpDate = %q(err=%v), 期望空串", maxRolled, err)
	}
	if err := RollupDay("2026-10-15"); err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	maxRolled, err = MaxRolledUpDate()
	if err != nil || maxRolled != "2026-10-15" {
		t.Fatalf("MaxRolledUpDate = %q(err=%v), 期望 2026-10-15", maxRolled, err)
	}
}

// TestVisitConfigDefaults 三个配置读取的非法值回落(不依赖真实环境变量)。
func TestVisitConfigDefaults(t *testing.T) {
	visitInit(t)
	if !VisitEnabled() {
		t.Fatal("默认应开启访问统计")
	}
	if got := VisitRetentionDays(); got != 90 {
		t.Fatalf("默认保留天数 = %d, 期望 90", got)
	}
	if got := VisitRatePerMin(); got != 60 {
		t.Fatalf("默认限流 = %d, 期望 60", got)
	}
	t.Setenv("VISIT_RETENTION_DAYS", "abc")
	if got := VisitRetentionDays(); got != 90 {
		t.Fatalf("非法保留天数应回落 90, got %d", got)
	}
	t.Setenv("VISIT_STAT_ENABLED", "0")
	if VisitEnabled() {
		t.Fatal("VISIT_STAT_ENABLED=0 应关闭")
	}
	t.Setenv("VISIT_RATE_PER_MIN", "0")
	if got := VisitRatePerMin(); got != 60 {
		t.Fatalf("非法限流值应回落 60, got %d", got)
	}
}

// TestVisitTopEntryCodes 入口榜只做单表聚合:按码计数、倒序、限条数。
// 与桌台的关联在 Go 侧(service),SQL 里不做 JOIN —— MySQL 8 下
// CAST(table_id AS CHAR) 与表列排序规则混用会直接报 Error 1267。
func TestVisitTopEntryCodes(t *testing.T) {
	visitInit(t)

	const day = "2026-10-15"
	logs := []po.VisitLog{
		visitLog(day, po.VisitChannelH5, "c:a"),
		visitLog(day, po.VisitChannelH5, "c:b"),
		visitLog(day, po.VisitChannelH5, "c:c"),
	}
	for i := range logs {
		logs[i].Entry = "K7M3PQ9X" // 3 次
	}
	legacy := visitLog(day, po.VisitChannelH5, "c:d")
	legacy.Entry = "1" // 历史数字桌号 1 次
	logs = append(logs, legacy)
	orphan := visitLog(day, po.VisitChannelH5, "c:e")
	orphan.Entry = "ZZZZ9999" // 关联不到任何桌台,照常出数
	logs = append(logs, orphan)
	blank := visitLog(day, po.VisitChannelH5, "c:f") // entry 为空:不进榜
	logs = append(logs, blank)
	if err := WriteVisitBatch(logs, nil); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}

	rows, err := TopVisitEntryCodes(day, day, 10)
	if err != nil {
		t.Fatalf("TopVisitEntryCodes: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("入口行数 = %d, 期望 3: %+v", len(rows), rows)
	}
	if rows[0].Entry != "K7M3PQ9X" || rows[0].PV != 3 {
		t.Fatalf("首行 = %+v, 期望 K7M3PQ9X pv=3", rows[0])
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.Entry] = r.PV
	}
	if got["1"] != 1 || got["ZZZZ9999"] != 1 {
		t.Fatalf("入口计数不正确: %v", got)
	}
}

// TestVisitListTableIdentities 桌台清单(入口榜标签用):未删除的全部返回,
// 字段齐全即可 —— 关联逻辑在 service 测。
func TestVisitListTableIdentities(t *testing.T) {
	visitInit(t)

	now := store.Now()
	if _, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, del_flag, table_code, create_time, update_time)
		VALUES('01','大厅01桌',4,0,'0','K7M3PQ9X',?,?)`, now, now); err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, del_flag, table_code, create_time, update_time)
		VALUES('99','已删桌',4,0,'1','DELETE99',?,?)`, now, now); err != nil {
		t.Fatalf("插入已删桌台失败: %v", err)
	}

	ids, err := ListTableIdentities()
	if err != nil {
		t.Fatalf("ListTableIdentities: %v", err)
	}
	// 种子数据自带 8 张桌,这里只断言两条关键性质:
	// 本次插入的桌台在列,已删除的不在列(del_flag='1' 必须被过滤)。
	var found, deletedFound bool
	for _, r := range ids {
		if r.TableCode == "K7M3PQ9X" {
			found = true
			if r.TableNo != "01" || r.TableName != "大厅01桌" {
				t.Fatalf("桌台字段不正确: %+v", r)
			}
		}
		if r.TableCode == "DELETE99" {
			deletedFound = true
		}
	}
	if !found {
		t.Fatalf("未找到新插入的桌台: %+v", ids)
	}
	if deletedFound {
		t.Fatalf("已删除桌台不应返回: %+v", ids)
	}
}

// TestVisitDistribution 终端分布:按维度白名单聚合、空值行返回(上层归一「未知」)、
// 白名单之外的维度拒绝(防注入)。
func TestVisitDistribution(t *testing.T) {
	visitInit(t)

	const day = "2026-10-15"
	logs := []po.VisitLog{
		func() po.VisitLog { l := visitLog(day, po.VisitChannelH5, "c:a"); l.OS = "iOS"; l.Browser = "微信"; l.Device = "手机"; return l }(),
		func() po.VisitLog { l := visitLog(day, po.VisitChannelH5, "c:b"); l.OS = "iOS"; l.Browser = "微信"; l.Device = "手机"; return l }(),
		func() po.VisitLog { l := visitLog(day, po.VisitChannelH5, "c:c"); l.OS = "Android"; l.Browser = "Chrome"; l.Device = "手机"; return l }(),
		func() po.VisitLog { l := visitLog(day, po.VisitChannelAdmin, "c:d"); l.OS = "Windows"; l.Browser = "Edge"; l.Device = "电脑"; return l }(),
		visitLog(day, po.VisitChannelH5, "c:e"), // 三列全空:各维度都归一行
	}
	if err := WriteVisitBatch(logs, nil); err != nil {
		t.Fatalf("WriteVisitBatch: %v", err)
	}

	rows, err := VisitDistribution(day, day, "os")
	if err != nil {
		t.Fatalf("VisitDistribution(os): %v", err)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.Name] = r.PV
	}
	if got["iOS"] != 2 || got["Android"] != 1 || got["Windows"] != 1 || got[""] != 1 {
		t.Fatalf("OS 分布 = %v", got)
	}
	if len(rows) == 0 || rows[0].Name != "iOS" || rows[0].PV != 2 {
		t.Fatalf("分布应按 PV 倒序: %+v", rows)
	}

	rows, err = VisitDistribution(day, day, "browser")
	if err != nil {
		t.Fatalf("VisitDistribution(browser): %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("浏览器分布行数 = %d, 期望 4", len(rows))
	}

	if _, err := VisitDistribution(day, day, "entry; DROP TABLE tb_visit_log"); err == nil {
		t.Fatal("白名单之外的维度应被拒绝")
	}
}
