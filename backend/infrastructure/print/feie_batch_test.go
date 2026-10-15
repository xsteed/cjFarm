package print

// ============================================================================
// 飞鹅通道批量派发(一菜一单合并推送)
//
// 背景:一张票一次 Open_printMsg 时,「一菜一单」会把一次下单变成 N 次独立推送。
// 飞鹅是厂商云端排队,对「短时间内连续多条」没有任何保证 —— 实测出现过飞鹅回 ret=0、
// 纸却没出来(日志显示成功),且同一秒的多条请求签名完全相同。
// 因此同一台飞鹅打印机的多张票必须合并成一次推送,用 <CUT> 在票之间切纸。
// ============================================================================

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"dining-system/domain/po"
	"dining-system/infrastructure/adapter/feie"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

// captureLogger 返回一个把日志写进内存缓冲的 logger。
// 打印路径的 logger 由 buildJobs 传入(job.log),因此测试可以注入它,
// 直接断言「日志里到底写了什么」,而不必去动全局 logger。
func captureLogger() (*zap.SugaredLogger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	return zap.New(core).Sugar(), buf
}

// feiePrintStub 起一个假的飞鹅开放平台,记录每条 Open_printMsg 的 content。
func feiePrintStub(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	contents := &[]string{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("apiname") == "Open_printMsg" {
			mu.Lock()
			*contents = append(*contents, r.Form.Get("content"))
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"ret":0,"msg":"ok","data":"FEIE-ORDER-1"}`))
	}))
	t.Cleanup(srv.Close)
	setPrintSetting(t, "feie_api_url", srv.URL)
	setPrintSetting(t, "feie_user", "u")
	setPrintSetting(t, "feie_ukey", "k")
	return srv, contents
}

func containsContents(t *testing.T, contents []string, want string) bool {
	t.Helper()
	for _, c := range contents {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

// TestDispatchFeieMergesTicketsIntoSinglePush 同一台飞鹅机的多张拆单票必须合并成一次推送:
// 只发一条 Open_printMsg,票与票之间用 <CUT> 切纸,内容含全部菜品。
func TestDispatchFeieMergesTicketsIntoSinglePush(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	_, contents := feiePrintStub(t)

	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 9, PrinterName: "后厨飞鹅打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48,
	}}
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("一菜一单应生成 2 张, got %d", len(jobs))
	}

	dispatchJobs(jobs)

	if len(*contents) != 1 {
		t.Fatalf("同一台飞鹅机的 2 张票必须合并成 1 次推送, got %d 次: %#v", len(*contents), *contents)
	}
	got := (*contents)[0]
	if !strings.Contains(got, "<CUT>") {
		t.Fatalf("票与票之间应有 <CUT> 切纸:\n%s", got)
	}
	if strings.ContainsRune(got, '\x03') {
		t.Fatalf("控制字符不能被当正文发出去:\n%q", got)
	}
	for _, want := range []string{"凉拌青瓜", "海鲜炒饭", "【厨房单】1/2", "【厨房单】2/2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("合并内容缺少 %q:\n%s", want, got)
		}
	}
	// 台账仍按「每张票一条」记录,粒度不因合并而丢失。
	var n int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_print_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("两张票应各记一条打印日志, got %d", n)
	}
	// 台账必须同时带上「第几张」与飞鹅返回原文:
	// 前者(label 列)让一菜一单的多条日志可区分 —— 独立成列而不是拼进 detail,
	// 因为代理通道的回执会整段改写 detail;后者是「记成功却没出纸」唯一的事后证据。
	var label, detail string
	if err := store.DB.QueryRow(
		`SELECT COALESCE(label,''), detail FROM tb_print_log WHERE label LIKE '%1/2%' LIMIT 1`).
		Scan(&label, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(label, "【厨房单】1/2") {
		t.Fatalf("台账 label 应记录票据标识, got %q", label)
	}
	if !strings.Contains(detail, "飞鹅云端单号 FEIE-ORDER-1") || !strings.Contains(detail, `"ret":0`) {
		t.Fatalf("台账 detail 应记录飞鹅返回原文, got %q", detail)
	}
}

// TestFeieResponseAppearsInLog 确认「飞鹅返回原文」确实出现在日志里,且汇总行带订单号与
// 第几张 —— 这是「台账记成功、纸却没出来」唯一能事后对账的线索。
func TestFeieResponseAppearsInLog(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	_, contents := feiePrintStub(t)

	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 9, PrinterName: "后厨飞鹅打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48,
	}}
	// 注入可捕获的 logger(buildJobs 的 log 会随 job 一起传给发送层)。
	log, buf := captureLogger()
	jobs := buildJobs(log, log, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("应生成 2 张, got %d", len(jobs))
	}

	dispatchJobs(jobs)

	if len(*contents) != 1 {
		t.Fatalf("应合并成 1 次推送, got %d", len(*contents))
	}
	logged := buf.String()
	for _, want := range []string{
		"合并推送",
		"【厨房单】1/2",
		"【厨房单】2/2",
		"飞鹅云端单号 FEIE-ORDER-1", // 云端单号
		`"ret":0`,             // 云端返回的 JSON 原文
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("日志缺少 %q, 实际:\n%s", want, logged)
		}
	}
}

// TestDispatchFeieKeepsPrintersSeparate 不同打印机(哪怕都是飞鹅)不能被合并到一条推送里。
func TestDispatchFeieKeepsPrintersSeparate(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	_, contents := feiePrintStub(t)

	o, _ := sampleOrder()
	printers := []po.Printer{
		{PrinterID: 1, PrinterName: "凉菜机", PrinterType: po.PrinterTypeKitchen,
			Provider: po.PrinterProviderFeie, FeieSN: "111111111", PaperWidth: 48, CategoryIDs: "1"},
		{PrinterID: 2, PrinterName: "热菜机", PrinterType: po.PrinterTypeKitchen,
			Provider: po.PrinterProviderFeie, FeieSN: "222222222", PaperWidth: 48, CategoryIDs: "6"},
	}
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("两台机器各 1 道菜应出 2 张, got %d", len(jobs))
	}

	dispatchJobs(jobs)

	if len(*contents) != 2 {
		t.Fatalf("两台打印机应各推 1 次, got %d", len(*contents))
	}
	for _, c := range *contents {
		if strings.Contains(c, "<CUT>") {
			t.Fatalf("不同打印机的票之间不应出现 <CUT>:\n%s", c)
		}
	}
	if !containsContents(t, *contents, "凉拌青瓜") || !containsContents(t, *contents, "海鲜炒饭") {
		t.Fatalf("两台机器应各收到自己负责的菜: %#v", *contents)
	}
}

// TestFeieContentRendersCutMarker 切纸控制行必须落地成 <CUT> 标签而不是被转义成
// 「&lt;CUT&gt;」打到纸上;且普通行不会被误判。
func TestFeieContentRendersCutMarker(t *testing.T) {
	setupTestDB(t)

	chunks := splitFeieContent([]string{"第一张", cutMarkLine(), "第二张"})
	if len(chunks) != 1 {
		t.Fatalf("短内容应只有一段, got %d", len(chunks))
	}
	if chunks[0] != "第一张<BR><CUT><BR>第二张" {
		t.Fatalf("切纸标签落地错误: %q", chunks[0])
	}
	if !SplitCutLine(cutMarkLine()) {
		t.Fatal("SplitCutLine 应识别切纸控制行")
	}
	if SplitCutLine("普通行") || SplitCutLine("") {
		t.Fatal("普通行不应被判为切纸行")
	}
}

// TestPackFeiePushKeepsTicketsWhole 装箱只在票与票之间切:一次推送绝不会把一张票切成两半
// (否则后厨会收到半张票),且每一次推送都能被 splitFeieContent 一次装下(不会二次切段)。
func TestPackFeiePushKeepsTicketsWhole(t *testing.T) {
	setupTestDB(t)
	big := func(tag string) []string {
		lines := make([]string, 0, 15)
		for i := 0; i < 15; i++ {
			lines = append(lines, tag+strings.Repeat("x", 100))
		}
		return lines
	}
	tickets := [][]string{big("A"), big("B"), big("C"), big("D"), big("E"), big("F")}

	pushed, pushes := 0, 0
	for start := 0; start < len(tickets); {
		end, lines := packFeiePush(tickets, start)
		if end <= start {
			t.Fatalf("packFeiePush 至少要装一张(start=%d)", start)
		}
		chunks := splitFeieContent(lines)
		if len(chunks) != 1 {
			t.Fatalf("第 %d 箱应能一次推送完(不得跨票切), got %d 段", pushes+1, len(chunks))
		}
		if len(chunks[0]) > feie.MaxBytes {
			t.Fatalf("第 %d 箱落地 %d 字节超上限 %d", pushes+1, len(chunks[0]), feie.MaxBytes)
		}
		cuts := 0
		for _, l := range lines {
			if SplitCutLine(l) {
				cuts++
			}
		}
		if cuts != end-start-1 {
			t.Fatalf("第 %d 箱装 %d 张票应有 %d 个切纸标记, got %d", pushes+1, end-start, end-start-1, cuts)
		}
		// 数量对了还不够:切纸必须只落在票与票之间,箱首/箱末不能是切纸行,
		// 否则一箱的开头或结尾会白切一刀(切出空纸头/把票切碎)。
		if SplitCutLine(lines[0]) || SplitCutLine(lines[len(lines)-1]) {
			t.Fatalf("第 %d 箱的首/尾行不应是切纸标记: %#v", pushes+1, lines)
		}
		pushed += end - start
		pushes++
		start = end
	}
	if pushed != len(tickets) {
		t.Fatalf("应覆盖全部票, got %d/%d", pushed, len(tickets))
	}
	if pushes < 2 {
		t.Fatalf("这批票应被拆成多次推送, got %d", pushes)
	}
}

// TestFeieCutMarksOnlyBetweenTickets 切纸标记的**位置**必须精确落在两张票之间:
// 单张票不能有、箱首箱末不能有、content 首尾不能有,且每个 <CUT> 都被 <BR> 前后包夹。
// 只校验数量不够 —— 位置错一格就是「白切一刀」或「把一张票切成两半」。
func TestFeieCutMarksOnlyBetweenTickets(t *testing.T) {
	setupTestDB(t)
	mk := func(tag string) []string {
		return []string{tag + "-头", tag + "-中", tag + "-尾"}
	}
	tickets := [][]string{mk("A"), mk("B"), mk("C")}

	// 1) 单张票:不产生任何切纸标记(单张票单独推送,末尾靠飞鹅自动切)。
	if end, single := packFeiePush(tickets[:1], 0); end != 1 {
		t.Fatalf("单张票应只有一箱, got end=%d", end)
	} else {
		for _, l := range single {
			if SplitCutLine(l) {
				t.Fatal("单张票不应产生切纸标记")
			}
		}
		if strings.Contains(splitFeieContent(single)[0], "<CUT>") {
			t.Fatal("单张票的 content 不应含 <CUT>")
		}
	}

	// 2) 多张票:逐行比对,切纸行精确位于两票之间。
	end, lines := packFeiePush(tickets, 0)
	if end != 3 {
		t.Fatalf("三张票应装成一箱, got end=%d", end)
	}
	want := []string{
		"A-头", "A-中", "A-尾", cutMarkLine(),
		"B-头", "B-中", "B-尾", cutMarkLine(),
		"C-头", "C-中", "C-尾",
	}
	if len(lines) != len(want) {
		t.Fatalf("合并行数应为 %d, got %d: %#v", len(want), len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("第 %d 行应为 %q, got %q", i+1, want[i], lines[i])
		}
	}
	if SplitCutLine(lines[0]) || SplitCutLine(lines[len(lines)-1]) {
		t.Fatal("切纸标记不得出现在箱首或箱末")
	}

	// 3) 落到 content 后:<CUT> 只在票之间,首尾都没有,且被 <BR> 前后包夹(标签要有分隔符)。
	content := splitFeieContent(lines)[0]
	if strings.HasPrefix(content, "<CUT>") || strings.HasSuffix(content, "<CUT>") {
		t.Fatalf("<CUT> 不得出现在 content 首尾: %q", content)
	}
	if strings.Contains(content, "<CUT><CUT>") {
		t.Fatalf("不应出现连续的切纸标记: %q", content)
	}
	if n := strings.Count(content, "<CUT>"); n != 2 {
		t.Fatalf("3 张票应有 2 个切纸点, got %d: %q", n, content)
	}
	rest := content
	for i := 0; i < 2; i++ {
		idx := strings.Index(rest, "<CUT>")
		if idx < len("<BR>") || !strings.HasPrefix(rest[idx-len("<BR>"):], "<BR>") ||
			!strings.HasPrefix(rest[idx+len("<CUT>"):], "<BR>") {
			t.Fatalf("第 %d 个 <CUT> 未被 <BR> 前后包夹: %q", i+1, rest)
		}
		rest = rest[idx+len("<CUT>"):]
	}

	// 4) 反证:用切纸点把 content 切开,每一段必须与「该票单独推送时的 content」完全一致。
	//    这是「切点精准落在票边界」的强等价:切点只要偏一格,某一段就会多/少一行。
	parts := strings.Split(content, "<BR><CUT><BR>")
	if len(parts) != 3 {
		t.Fatalf("按切纸点应恰好切成 3 张票, got %d 段", len(parts))
	}
	for i, tk := range tickets {
		if want := splitFeieContent(tk)[0]; parts[i] != want {
			t.Fatalf("第 %d 段应是完整的一张票:\n got=%s\nwant=%s", i+1, parts[i], want)
		}
	}

	// 5) 切纸行不得被当成放大/加粗/二维码行处理,否则会输出 <BOLD><CUT></BOLD> 这类怪标签。
	if _, ok := SplitBigLine(cutMarkLine()); ok {
		t.Fatal("切纸行不应被判为放大行")
	}
	if BoldLine(cutMarkLine()) {
		t.Fatal("切纸行不应被判为加粗行")
	}
	if _, ok := SplitQRLine(cutMarkLine()); ok {
		t.Fatal("切纸行不应被判为二维码行")
	}
}

// feieStateStub 起一个假的飞鹅开放平台:打印受理成功(云端单号固定),回执状态固定为 state。
func feieStateStub(t *testing.T, state string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		body := `{"ret":0,"msg":"ok","data":"FEIE-ORDER-1"}`
		if r.Form.Get("apiname") == "Open_queryOrderState" {
			body = `{"ret":0,"msg":"ok","data":"` + state + `"}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	setPrintSetting(t, "feie_api_url", srv.URL)
	setPrintSetting(t, "feie_user", "u")
	setPrintSetting(t, "feie_ukey", "k")
}

// confirmTestTiming 把出纸确认的等待压到毫秒级并注册恢复:
// 生产节奏是秒级(等真实打印机走纸),测试里等不起,也不能让毫秒级节奏泄漏到下个用例。
func confirmTestTiming(t *testing.T) {
	t.Helper()
	oldFirst, oldRetry, oldAttempts := feieConfirmFirstDelay, feieConfirmRetryDelay, feieConfirmAttempts
	feieConfirmFirstDelay, feieConfirmRetryDelay, feieConfirmAttempts = time.Millisecond, time.Millisecond, 2
	t.Cleanup(func() {
		feieConfirmFirstDelay, feieConfirmRetryDelay, feieConfirmAttempts = oldFirst, oldRetry, oldAttempts
	})
}

// successLogIDs 取当前库里所有「成功」台账的主键(dispatchJobs 刚写下的那批)。
func successLogIDs(t *testing.T) []int {
	t.Helper()
	rows, err := store.DB.Query(`SELECT print_id FROM tb_print_log WHERE status=?`, po.PrintStatusSuccess)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestFeieConfirmPrintedMarksLedgerFailed 出纸确认:回执始终「未打印」时,该次推送写下
// 的台账必须被回写成失败 —— 否则商户在「打印记录」里看到的永远是一条「成功」的假账。
// 复用代理通道的 UpdatePrintLogResult 回写,不另插一行:一票一条台账的粒度不因确认而改变。
func TestFeieConfirmPrintedMarksLedgerFailed(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	confirmTestTiming(t)
	feieStateStub(t, "未打印")

	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 9, PrinterName: "后厨飞鹅打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48,
	}}
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	if len(jobs) != 2 {
		t.Fatalf("应生成 2 张, got %d", len(jobs))
	}
	dispatchJobs(jobs)
	ids := successLogIDs(t)
	if len(ids) != 2 {
		t.Fatalf("推送后应有 2 条成功台账, got %d", len(ids))
	}

	client, err := NewFeieClient()
	if err != nil {
		t.Fatalf("构造飞鹅客户端失败: %v", err)
	}
	// 同步调用确认协程的执行体,避免对异步时序的脆弱等待。
	confirmFeiePrinted(jobs[0], []string{"【厨房单】1/2", "【厨房单】2/2"}, "FEIE-ORDER-1", ids, client)

	for _, id := range ids {
		var status int
		var detail string
		if err := store.DB.QueryRow(`SELECT status, detail FROM tb_print_log WHERE print_id=?`, id).
			Scan(&status, &detail); err != nil {
			t.Fatal(err)
		}
		if status != po.PrintStatusFailed || !strings.Contains(detail, "2 次确认均未打印") {
			t.Fatalf("台账应回写成失败, got status=%d detail=%q", status, detail)
		}
	}
}

// TestFeieConfirmPrintedKeepsSuccessOnPrinted 回执「已打印」时不回写:台账保持「成功」,
// 不产生任何多余动作。
func TestFeieConfirmPrintedKeepsSuccessOnPrinted(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	confirmTestTiming(t)
	feieStateStub(t, "已打印")

	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 9, PrinterName: "后厨飞鹅打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48,
	}}
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	dispatchJobs(jobs)
	ids := successLogIDs(t)
	if len(ids) != 2 {
		t.Fatalf("推送后应有 2 条成功台账, got %d", len(ids))
	}

	client, err := NewFeieClient()
	if err != nil {
		t.Fatalf("构造飞鹅客户端失败: %v", err)
	}
	confirmFeiePrinted(jobs[0], []string{"【厨房单】1/2", "【厨房单】2/2"}, "FEIE-ORDER-1", ids, client)

	for _, id := range ids {
		var status int
		if err := store.DB.QueryRow(`SELECT status FROM tb_print_log WHERE print_id=?`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != po.PrintStatusSuccess {
			t.Fatalf("回执已打印时台账应保持成功, got status=%d", status)
		}
	}
}

// TestSettingQueriesPerOrderBounded 一次下单的配置表查询次数必须有上界。
//
// dao.GetSetting 每次调用都真查库(「改配置即刻生效」正依赖这一点),所以绝不能在
// 「按行/按票」的循环里调用:历史上 BoldLine 在落地层逐行拿店名反推加粗,而每行在
// 装箱/切段/拼接里要落地 2~3 次,一次 15 道菜的一菜一单实测查了 150 次配置表。
// 现在店名/金额开关随作业快照下发、加粗判定不再读库 —— 这里用计数器把上界锁死,
// 防止以后有人把配置读取悄悄塞回渲染或落地层。
func TestSettingQueriesPerOrderBounded(t *testing.T) {
	setupTestDB(t)
	setPrintSetting(t, "print_kitchen_split_mode", po.KitchenSplitItem)
	feiePrintStub(t)

	o, _ := sampleOrder()
	printers := []po.Printer{{
		PrinterID: 9, PrinterName: "后厨飞鹅打印机", PrinterType: po.PrinterTypeKitchen,
		Provider: po.PrinterProviderFeie, FeieSN: "961828266", PaperWidth: 48,
	}}

	dao.ResetSettingQueries()
	jobs := buildJobs(nil, nil, printers, o, sampleRows(), po.PrintDocKitchen, "【厨房单】", po.PrintTriggerOrder, "")
	dispatchJobs(jobs)

	// 上界构成:拆单模式 1 + 店名快照 1 + 金额开关 1 + 发送路径构造客户端 3 +
	// 出纸确认客户端 3。再多的每一笔都意味着又有人在循环里读了配置。
	if n := dao.SettingQueries(); n > 10 {
		t.Fatalf("2 张票的一次下单配置查询应 ≤10 次, got %d(检查是否有行级/票级读取混了回来)", n)
	}
}

// TestSameFeiePrinterGrouping 合并只发生在「同一台飞鹅打印机」的相邻作业之间。
func TestSameFeiePrinterGrouping(t *testing.T) {
	feieA := job{p: po.Printer{PrinterID: 1, Provider: po.PrinterProviderFeie}}
	feieA2 := job{p: po.Printer{PrinterID: 1, Provider: po.PrinterProviderFeie}}
	feieB := job{p: po.Printer{PrinterID: 2, Provider: po.PrinterProviderFeie}}
	tcpA := job{p: po.Printer{PrinterID: 1, Provider: po.PrinterProviderTCP}}

	if !sameFeiePrinter(feieA, feieA2) {
		t.Fatal("同一台飞鹅机的作业应可合并")
	}
	if sameFeiePrinter(feieA, feieB) {
		t.Fatal("不同打印机的作业不应合并")
	}
	if sameFeiePrinter(feieA, tcpA) {
		t.Fatal("直连作业不应进入飞鹅合并")
	}
}

// TestIsRetryableFeieTransientCodes 飞鹅的瞬时故障(1003/1004)必须可重试,
// 配置/参数类错误仍不重试 —— 否则一过性故障会直接把那一张票丢掉。
func TestIsRetryableFeieTransientCodes(t *testing.T) {
	for _, ret := range []int{1003, 1004} {
		if !isRetryable(&feie.APIError{Ret: ret, Msg: "瞬时"}) {
			t.Fatalf("飞鹅 ret=%d 是瞬时故障,应重试", ret)
		}
	}
	for _, ret := range []int{-1, -2, -3, -4, 1001, 1002, 1007, 1009, 1010, 1011} {
		if isRetryable(&feie.APIError{Ret: ret, Msg: "永久"}) {
			t.Fatalf("飞鹅 ret=%d 是配置/参数类错误,不应重试", ret)
		}
	}
}
