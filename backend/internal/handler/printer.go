package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/print"
	"dining-system/internal/store"
)

// ============ 打印机 ============

// printerCols 打印机表完整列(与 scanPrinter 顺序一一对应)。
const printerCols = `printer_id, printer_name, printer_type, provider, ip, port,
	feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time`

// scanPrinter 扫描一行打印机记录,并把分类 CSV 解析成 ID 列表。
func scanPrinter(rows interface{ Scan(...interface{}) error }) (model.Printer, error) {
	var p model.Printer
	err := rows.Scan(&p.PrinterID, &p.PrinterName, &p.PrinterType, &p.Provider, &p.IP, &p.Port,
		&p.FeieSN, &p.PaperWidth, &p.Copies, &p.CategoryIDs, &p.Status, &p.DelFlag,
		&p.CreateTime, &p.UpdateTime)
	if err != nil {
		return p, err
	}
	p.CategoryIDList = store.ParseIDList(p.CategoryIDs)
	p.CategoryNames = categoryNamesOf(p.CategoryIDList)
	return p, nil
}

// categoryNamesOf 把分类 ID 列表翻成中文名,拼成展示串(供列表页直接显示)。
func categoryNamesOf(ids []int) *string {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT category_name FROM tb_category WHERE category_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	s := strings.Join(names, "、")
	if s == "" {
		return nil
	}
	return &s
}

// normalizePrinter 规范化打印机的可枚举字段,避免非法值入库。
//
// 开关型字段(此处是 status)必须归一化成 0/1:前端 t-switch 收到其它值会直接
// 抛异常并中断整棵路由树的渲染(历史上踩过配置页开关脏值的坑)。
func normalizePrinter(p *model.Printer) {
	if p.PrinterType != model.PrinterTypeKitchen && p.PrinterType != model.PrinterTypeGuest {
		p.PrinterType = model.PrinterTypeKitchen
	}
	if p.Provider != model.PrinterProviderFeie {
		p.Provider = model.PrinterProviderTCP
	}
	if p.Port == 0 {
		p.Port = 9100
	}
	if p.PaperWidth != 32 && p.PaperWidth != 48 {
		p.PaperWidth = 48
	}
	if p.Copies < 1 {
		p.Copies = 1
	}
	if p.Copies > 5 {
		p.Copies = 5
	}
	if p.Status != 0 {
		p.Status = 1
	}
	// 分类分单只对厨房单有意义 —— 食客小票必须含全部菜品,否则金额合计对不上。
	if p.PrinterType == model.PrinterTypeGuest {
		p.CategoryIDList = nil
		p.CategoryIDs = ""
		return
	}
	if len(p.CategoryIDList) > 0 {
		p.CategoryIDs = store.JoinIDList(p.CategoryIDList)
	} else {
		p.CategoryIDs = store.JoinIDList(store.ParseIDList(p.CategoryIDs))
	}
}

// validatePrinter 按接入方式分别校验,失败时已写入错误响应。
//
// 两条通道的必填项不同:直连必须填 IP(且要做防 SSRF 校验),
// 云打印必须填 SN(不发起任何对内网的连接,故不做 IP 校验)。
func validatePrinter(c *gin.Context, p *model.Printer) bool {
	if strings.TrimSpace(p.PrinterName) == "" {
		fail(c, "请填写打印机名称")
		return false
	}
	if p.IsFeie() {
		if strings.TrimSpace(p.FeieSN) == "" {
			fail(c, "请填写飞鹅打印机编号(SN),机身标签上有")
			return false
		}
		return true
	}
	if strings.TrimSpace(p.IP) == "" {
		fail(c, "请填写打印机 IP 地址")
		return false
	}
	if err := print.ValidatePrinterAddr(p.IP, p.Port); err != nil {
		fail(c, err.Error())
		return false
	}
	return true
}

func PrinterList(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT ` + printerCols + ` FROM tb_printer WHERE del_flag='0' ORDER BY printer_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Printer{}
	for rows.Next() {
		if p, err := scanPrinter(rows); err == nil {
			list = append(list, p)
		}
	}
	tableResult(c, len(list), list)
}

func PrinterSave(c *gin.Context) {
	var p model.Printer
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	normalizePrinter(&p)
	if !validatePrinter(c, &p) {
		return
	}
	res, err := store.DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, provider, ip, port,
		feie_sn, paper_width, copies, category_ids, status, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,'0',?,?)`,
		p.PrinterName, p.PrinterType, p.Provider, p.IP, p.Port,
		p.FeieSN, p.PaperWidth, p.Copies, p.CategoryIDs, p.Status, store.Now(), store.Now())
	if err != nil {
		fail(c, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	ok(c, gin.H{"printerId": id})
}

func PrinterUpdate(c *gin.Context) {
	var p model.Printer
	if err := c.ShouldBindJSON(&p); err != nil || p.PrinterID == 0 {
		fail(c, "参数错误")
		return
	}
	normalizePrinter(&p)
	if !validatePrinter(c, &p) {
		return
	}
	store.DB.Exec(`UPDATE tb_printer SET printer_name=?, printer_type=?, provider=?, ip=?, port=?,
		feie_sn=?, paper_width=?, copies=?, category_ids=?, status=?, update_time=? WHERE printer_id=?`,
		p.PrinterName, p.PrinterType, p.Provider, p.IP, p.Port,
		p.FeieSN, p.PaperWidth, p.Copies, p.CategoryIDs, p.Status, store.Now(), p.PrinterID)
	okMsg(c, "修改成功")
}

func PrinterDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	store.DB.Exec(`UPDATE tb_printer SET del_flag='1', update_time=? WHERE printer_id=?`, store.Now(), id)
	okMsg(c, "删除成功")
}

// PrinterTest 打一张测试页(会真的出纸)。
// 内容含当前通道信息(IP 或飞鹅 SN),便于确认改对了是哪台机器。
func PrinterTest(c *gin.Context) {
	p, okp := loadPrinterParam(c)
	if !okp {
		return
	}
	if err := print.SendTestPrint(p); err != nil {
		fail(c, "测试打印失败:"+err.Error())
		return
	}
	okMsg(c, "已向「"+p.PrinterName+"」("+printerTarget(p)+") 发送测试打印指令")
}

// PrinterProbe 只测连通性,不吐纸。
// 与「测试打印」的区别:想确认「网络/账号通不通」而不想白白浪费一张纸时用它。
func PrinterProbe(c *gin.Context) {
	p, okp := loadPrinterParam(c)
	if !okp {
		return
	}
	msg, err := print.ProbePrinter(p)
	if err != nil {
		fail(c, "连接失败:"+err.Error())
		return
	}
	ok(c, gin.H{"message": msg})
}

// PrinterStatus 查询打印机实时状态。
//   - 飞鹅: 走 Open_queryPrinterStatus 拿在线/缺纸状态,并附带当日打印统计;
//   - TCP:  只能探一次端口连通性(直连打印机没有可查询的状态接口)。
func PrinterStatus(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	p, err := store.LoadPrinter(id)
	if err != nil {
		fail(c, "打印机不存在")
		return
	}
	if !p.IsFeie() {
		if _, err := print.ProbePrinter(p); err != nil {
			ok(c, gin.H{"provider": "tcp", "online": false, "status": "连接失败:" + err.Error()})
			return
		}
		ok(c, gin.H{"provider": "tcp", "online": true, "status": "端口可达(直连打印机无状态查询接口)"})
		return
	}
	client, err := print.NewFeieClient()
	if err != nil {
		fail(c, err.Error())
		return
	}
	if strings.TrimSpace(p.FeieSN) == "" {
		fail(c, "该打印机未填写飞鹅 SN")
		return
	}
	status, err := client.PrinterStatus(p.FeieSN)
	if err != nil {
		fail(c, err.Error())
		return
	}
	res := gin.H{
		"provider": "feie",
		"status":   status,
		"online":   strings.Contains(status, "在线"),
	}
	// 当日打印统计是辅助信息,查不到不影响状态展示。
	if printed, waiting, err := client.QueryOrderInfoByDate(p.FeieSN, time.Now().Format("2006-01-02")); err == nil {
		res["todayPrinted"] = printed
		res["todayWaiting"] = waiting
	}
	ok(c, res)
}

// PrinterBind 把打印机绑定到当前飞鹅账号(Open_printerAddlist)。
//
// 打印机出厂后必须先在飞鹅后台(或通过该接口)绑定到开发者账号,才能被推单。
// 这里让商户在系统内一步完成,免得两头切换填错。
func PrinterBind(c *gin.Context) {
	var p struct {
		SN    string `json:"sn"`
		Key   string `json:"key"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	p.SN = strings.TrimSpace(p.SN)
	p.Key = strings.TrimSpace(p.Key)
	if p.SN == "" || p.Key == "" {
		fail(c, "请填写打印机编号(SN)与识别码(KEY)")
		return
	}
	client, err := print.NewFeieClient()
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 格式: 打印机编号#识别码#备注名称#流量卡号(后两项可省)。
	parts := []string{p.SN, p.Key}
	if n := strings.TrimSpace(p.Name); n != "" {
		parts = append(parts, n)
	}
	if ph := strings.TrimSpace(p.Phone); ph != "" {
		parts = append(parts, ph)
	}
	okList, noList, err := client.AddPrinter(strings.Join(parts, "#"))
	if err != nil {
		fail(c, err.Error())
		return
	}
	if len(noList) > 0 {
		fail(c, "绑定未成功:"+strings.Join(noList, " "))
		return
	}
	ok(c, gin.H{"ok": okList})
}

// PrinterClear 清空该打印机的飞鹅云端待打印队列。
// 打错单/打了一半卡住时,先清队列再补打,否则积压任务会一起吐出来。
func PrinterClear(c *gin.Context) {
	p, okp := loadPrinterParam(c)
	if !okp {
		return
	}
	if !p.IsFeie() {
		fail(c, "清空队列仅支持飞鹅云打印机")
		return
	}
	client, err := print.NewFeieClient()
	if err != nil {
		fail(c, err.Error())
		return
	}
	if err := client.ClearQueue(strings.TrimSpace(p.FeieSN)); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "已清空「"+p.PrinterName+"」的待打印队列")
}

// FeieInfo 返回飞鹅账号配置概况(不回显 UKEY)。
func FeieInfo(c *gin.Context) {
	ok(c, gin.H{
		"user":       store.GetCfg("feie_user"),
		"apiUrl":     store.GetCfg("feie_api_url"),
		"configured": print.FeieConfigured(),
	})
}

// loadPrinterParam 按路径 :id 读取打印机,失败时已写入错误响应。
func loadPrinterParam(c *gin.Context) (model.Printer, bool) {
	id, okid := idParam(c)
	if !okid {
		return model.Printer{}, false
	}
	p, err := store.LoadPrinter(id)
	if err != nil {
		fail(c, "打印机不存在")
		return model.Printer{}, false
	}
	return p, true
}

// printerTarget 日志/提示里用的打印机定位串。
func printerTarget(p model.Printer) string {
	if p.IsFeie() {
		return "飞鹅 SN " + p.FeieSN
	}
	port := p.Port
	if port <= 0 {
		port = 9100
	}
	return p.IP + ":" + strconv.Itoa(port)
}
