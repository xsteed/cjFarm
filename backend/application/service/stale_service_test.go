package service

import (
	"context"
	"testing"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// stalePickupSeq 取餐号自增源:tb_order 上有 (biz_date, pickup_no) 唯一索引,
// 同一个营业日的测试订单必须错开取餐号,否则插第二笔就撞约束。
var stalePickupSeq int

// staleSeedOrder 直接种一笔订单,精确控制营业日、状态与下单时间。
// biz_date 传空串即模拟「手工补录 / 历史行」那种无从判定归属的行。
func staleSeedOrder(t *testing.T, orderNo string, tableID int, bizDate string, orderStatus, payStatus int, createTime string) {
	t.Helper()
	stalePickupSeq++
	if _, err := store.DB.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, biz_date, pickup_no, order_status,
		total_amount, pay_status, create_time, update_time) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, tableID, "01", "大厅01桌", bizDate, stalePickupSeq, orderStatus, 5000, payStatus, createTime, createTime); err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
}

// staleTable 建一张处于占用状态的桌台,返回桌台 ID 与桌号。
func staleTable(t *testing.T, no string) int {
	t.Helper()
	id, err := dao.InsertTable(po.Table{TableNo: no, TableName: "桌" + no, Capacity: 4, Status: 1})
	if err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	return int(id)
}

func staleSetBool(t *testing.T, key, v string) {
	t.Helper()
	if err := SetSetting(key, v); err != nil {
		t.Fatalf("写入配置 %s 失败: %v", key, err)
	}
}

// TestStaleGuardBizDateFollowsSwitch 隔离值由开关决定:关=不过滤,开=只认今天。
func TestStaleGuardBizDateFollowsSwitch(t *testing.T) {
	tableInitDB(t)

	staleSetBool(t, SettingStaleReleaseEnabled, "0")
	if got := StaleGuardBizDate(); got != "" {
		t.Fatalf("开关关闭时不应隔离任何订单, got %q", got)
	}

	staleSetBool(t, SettingStaleReleaseEnabled, "1")
	if got, want := StaleGuardBizDate(), store.Now()[:10]; got != want {
		t.Fatalf("开关开启时应隔离到今天的营业日, got %q want %q", got, want)
	}
}

// TestReleaseStaleTablesOnlyTouchesStaleTables 只清「本营业日无人的」桌台。
func TestReleaseStaleTablesOnlyTouchesStaleTables(t *testing.T) {
	tableInitDB(t)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	old := time.Now().Add(-30 * time.Hour).Format(conf.TimeLayout)
	today := store.Now()[:10]
	now := store.Now()

	staleOnly := staleTable(t, "01")  // 只有昨天的遗留单 → 应清
	inUseToday := staleTable(t, "02") // 只有今天的单 → 在用,不动
	mixed := staleTable(t, "03")      // 昨天 + 今天各一笔 → 在用,不动
	paidStale := staleTable(t, "04")  // 昨天已付款但出餐未完成 → 也是遗留,应清
	noDate := staleTable(t, "05")     // 无营业日的进行中单 → 无从判定,不动

	staleSeedOrder(t, "S-1", staleOnly, yesterday, 3, 0, old)
	staleSeedOrder(t, "S-2", inUseToday, today, 1, 0, now)
	staleSeedOrder(t, "S-3", mixed, yesterday, 2, 0, old)
	staleSeedOrder(t, "S-4", mixed, today, 1, 0, now)
	staleSeedOrder(t, "S-5", paidStale, yesterday, 2, 1, old)
	staleSeedOrder(t, "S-6", noDate, "", 1, 0, old)

	out, err := ReleaseStaleTables(context.Background())
	if err != nil {
		t.Fatalf("跨日清台失败: %v", err)
	}
	if out.Tables != 2 || out.Orders != 2 {
		t.Fatalf("应清 2 张桌台 / 2 笔遗留单, got %+v", out)
	}
	for _, tc := range []struct {
		id   int
		want int
		desc string
	}{
		{staleOnly, 0, "只有跨日遗留单的桌台应恢复空闲"},
		{inUseToday, 1, "本营业日在用的桌台不能被清"},
		{mixed, 1, "跨日单与今日单并存的桌台不能被清"},
		{paidStale, 0, "跨日已付款未出完的桌台同样应恢复空闲"},
		{noDate, 1, "无营业日的订单无从判定,保守不动桌台"},
	} {
		if got := relTableStatus(t, tc.id); got != tc.want {
			t.Fatalf("%s: status=%d want %d", tc.desc, got, tc.want)
		}
	}

	// 订单本身一律不改:补结账还是作废是账务判断,只能由店家做
	for _, no := range []string{"S-1", "S-5"} {
		var status, pay int
		if err := store.DB.QueryRow(`SELECT order_status, pay_status FROM tb_order WHERE order_no=?`, no).Scan(&status, &pay); err != nil {
			t.Fatalf("查询订单 %s 失败: %v", no, err)
		}
		if no == "S-1" && (status != 3 || pay != 0) {
			t.Fatalf("遗留单不应被改动, got status=%d pay=%d", status, pay)
		}
		if no == "S-5" && (status != 2 || pay != 1) {
			t.Fatalf("遗留单不应被改动, got status=%d pay=%d", status, pay)
		}
	}

	// 幂等:再跑一次不应重复计数
	if again, _ := ReleaseStaleTables(context.Background()); again.Tables != 0 {
		t.Fatalf("清台应幂等,第二次 got %+v", again)
	}
}

// TestReleaseStaleTablesNoopWhenNothingStale 没有遗留单时不产生任何副作用。
func TestReleaseStaleTablesNoopWhenNothingStale(t *testing.T) {
	tableInitDB(t)
	id := staleTable(t, "01")
	staleSeedOrder(t, "S-TODAY", id, store.Now()[:10], 1, 0, store.Now())

	out, err := ReleaseStaleTables(context.Background())
	if err != nil {
		t.Fatalf("跨日清台失败: %v", err)
	}
	if out.Tables != 0 || out.Orders != 0 {
		t.Fatalf("无遗留单时不应清台, got %+v", out)
	}
	if got := relTableStatus(t, id); got != 1 {
		t.Fatalf("在用桌台不应被动过, status=%d", got)
	}
}

// TestListStaleOrderAlerts 告警只看「当天、未支付、进行中且超过阈值」的订单。
func TestListStaleOrderAlerts(t *testing.T) {
	tableInitDB(t)
	staleSetBool(t, SettingStaleAlertEnabled, "1")
	staleSetBool(t, SettingStaleAlertHours, "4")
	// 跨日遗留那类由清台开关管(见 ListStaleOrderAlerts 的说明),这里一并打开
	staleSetBool(t, SettingStaleReleaseEnabled, "1")

	today := store.Now()[:10]
	before := time.Now().Add(-5 * time.Hour).Format(conf.TimeLayout)
	fresh := time.Now().Add(-1 * time.Hour).Format(conf.TimeLayout)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	staleSeedOrder(t, "A-HIT", 1, today, 3, 0, before)
	staleSeedOrder(t, "A-FRESH", 1, today, 3, 0, fresh)    // 未到阈值
	staleSeedOrder(t, "A-PAID", 1, today, 3, 1, before)    // 已收款,不算未结账
	staleSeedOrder(t, "A-DONE", 1, today, 4, 0, before)    // 已完成
	staleSeedOrder(t, "A-OLD", 1, yesterday, 3, 0, before) // 跨日遗留:桌台已被清空,但账还没了结

	res := ListStaleOrderAlerts()
	if !res.Enabled || res.Hours != 4 {
		t.Fatalf("告警开关/阈值下发异常: %+v", res)
	}
	// 两类一起下发:当天超时在前(桌还占着,最急),跨日遗留在后。
	// 遗留单必须出现 —— 它躺在订单列表最深处,不主动列出来就是「已清台=已遗忘」。
	if len(res.Items) != 2 {
		t.Fatalf("应命中当天超时 + 跨日遗留共 2 笔, got %+v", res.Items)
	}
	first, second := res.Items[0], res.Items[1]
	if first.OrderNo != "A-HIT" || first.Legacy {
		t.Fatalf("第一项应是当天超时的 A-HIT, got %+v", first)
	}
	if second.OrderNo != "A-OLD" || !second.Legacy {
		t.Fatalf("第二项应是跨日遗留的 A-OLD 且带 legacy 标记, got %+v", second)
	}
	if m := first.Minutes; m < 295 || m > 305 {
		t.Fatalf("开台时长应约 300 分钟, got %d", m)
	}
	if first.TotalAmount != 50 {
		t.Fatalf("金额应按元下发, got %v", first.TotalAmount)
	}

	// 只关「超时告警」:当天超时那类消失,但跨日遗留必须还在 ——
	// 它由清台开关负责,清台造成的账不能因为省了告警就被藏起来。
	staleSetBool(t, SettingStaleAlertEnabled, "0")
	onlyLegacy := ListStaleOrderAlerts()
	if !onlyLegacy.Enabled {
		t.Fatal("清台开关仍开着时,这块提醒整体应保持可见")
	}
	if len(onlyLegacy.Items) != 1 || onlyLegacy.Items[0].OrderNo != "A-OLD" || !onlyLegacy.Items[0].Legacy {
		t.Fatalf("只应剩跨日遗留那一笔, got %+v", onlyLegacy.Items)
	}

	// 两个开关都关:整块提醒消失
	staleSetBool(t, SettingStaleReleaseEnabled, "0")
	if off := ListStaleOrderAlerts(); off.Enabled || len(off.Items) != 0 {
		t.Fatalf("两个开关都关时不应返回任何条目, got %+v", off)
	}
}

// TestBizDateOfCutoff 营业日切换时刻的边界:切换那一刻起才算新的一天。
//
// 这条规则决定「跨夜正在用餐的客人会不会被当成上一营业日的遗留单」——
// 晚市 23:50 下单、00:30 还在吃的客人,只有把切换时刻后移到打烊后才是安全的。
func TestBizDateOfCutoff(t *testing.T) {
	at := func(h, m, s int) time.Time {
		return time.Date(2026, 10, 14, h, m, s, 0, time.Local)
	}
	for _, tc := range []struct {
		name   string
		when   time.Time
		cutoff int
		want   string
	}{
		{"自然日:凌晨仍算当天", at(2, 30, 0), 0, "2026-10-14"},
		{"cutoff=4:凌晨 2:30 属前一营业日", at(2, 30, 0), 4, "2026-10-13"},
		{"cutoff=4:3:59:59 仍属前一营业日", at(3, 59, 59), 4, "2026-10-13"},
		{"cutoff=4:4:00:00 进入新营业日", at(4, 0, 0), 4, "2026-10-14"},
		{"cutoff=4:晚市不受影响", at(20, 0, 0), 4, "2026-10-14"},
		{"cutoff=4:23:50 下单仍属当天营业日", at(23, 50, 0), 4, "2026-10-14"},
	} {
		if got := BizDateOf(tc.when, tc.cutoff); got != tc.want {
			t.Fatalf("%s: BizDateOf(%s, %d) = %q, want %q",
				tc.name, tc.when.Format("2006-01-02 15:04:05"), tc.cutoff, got, tc.want)
		}
	}
}

// TestCurrentBizDateFollowsCutoff 当前营业日由配置决定,脏值回落自然日。
func TestCurrentBizDateFollowsCutoff(t *testing.T) {
	tableInitDB(t)
	natural := time.Now().Format("2006-01-02")

	// 默认 0:与自然日一致,行为与改动前完全相同
	staleSetBool(t, SettingBizDateCutoffHour, "0")
	if got := CurrentBizDate(); got != natural {
		t.Fatalf("切换时刻为 0 时应等于自然日, got %q want %q", got, natural)
	}

	// 脏值:回落 0,不能让库里的越界值把营业日推到明天
	for _, bad := range []string{"99", "-3", "abc", ""} {
		staleSetBool(t, SettingBizDateCutoffHour, bad)
		if got := CurrentBizDate(); got != natural {
			t.Fatalf("切换时刻为 %q 时应回落自然日, got %q", bad, got)
		}
	}

	// 合法值:与纯函数口径一致
	staleSetBool(t, SettingBizDateCutoffHour, "4")
	if got, want := CurrentBizDate(), BizDateOf(time.Now(), 4); got != want {
		t.Fatalf("切换时刻为 4 时应与 BizDateOf(now,4) 一致, got %q want %q", got, want)
	}

	for _, tc := range []struct{ in, want string }{
		{"0", "0"}, {" 6 ", "6"}, {"12", "12"},
		{"13", "0"}, {"-1", "0"}, {"x", "0"}, {"", "0"},
	} {
		if got := NormalizeBizDateCutoffHour(tc.in); got != tc.want {
			t.Fatalf("NormalizeBizDateCutoffHour(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStaleAlertHoursNormalization 阈值必须是 1~72 的合法值,脏值回落默认。
func TestStaleAlertHoursNormalization(t *testing.T) {
	tableInitDB(t)

	for _, tc := range []struct{ in, want string }{
		{"8", "8"},
		{" 12 ", "12"},
		{"0", "4"},
		{"73", "4"},
		{"-3", "4"},
		{"abc", "4"},
		{"", "4"},
	} {
		if got := NormalizeStaleAlertHours(tc.in); got != tc.want {
			t.Fatalf("NormalizeStaleAlertHours(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// 读取侧同样兜底:直接往库里塞脏值也不会让看板刷满或一条不报
	staleSetBool(t, SettingStaleAlertHours, "999")
	if got := StaleAlertHours(); got != StaleAlertHoursDef {
		t.Fatalf("库中脏值应回落默认 %d, got %d", StaleAlertHoursDef, got)
	}
}
