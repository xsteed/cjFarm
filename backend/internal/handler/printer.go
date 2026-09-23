package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/print"
	"dining-system/internal/service"
)

// ============ 打印机 ============

// normalizePrinter 规范化打印机的可枚举字段,避免非法值入库。
//
// 开关型字段(此处是 status)必须归一化成 0/1:前端 t-switch 收到其它值会直接
// 抛异常并中断整棵路由树的渲染(历史上踩过配置页开关脏值的坑)。
func normalizePrinter(p *dto.Printer) {
	if p.PrinterType != po.PrinterTypeKitchen && p.PrinterType != po.PrinterTypeGuest {
		p.PrinterType = po.PrinterTypeKitchen
	}
	// 接入方式只接受三种已知取值,其余(含历史上可能出现的中文/空值)一律回落到网络直连 ——
	// 存进库的枚举值会直接决定「走哪条发送路径」,不能让脏值漏进去。
	if p.Provider != po.PrinterProviderFeie && p.Provider != po.PrinterProviderAgent {
		p.Provider = po.PrinterProviderTCP
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
	if p.PrinterType == po.PrinterTypeGuest {
		p.CategoryIDList = nil
		p.CategoryIDs = ""
		return
	}
	if len(p.CategoryIDList) > 0 {
		p.CategoryIDs = service.JoinIDList(p.CategoryIDList)
	} else {
		p.CategoryIDs = service.JoinIDList(service.ParseIDList(p.CategoryIDs))
	}
}

// validatePrinter 按接入方式分别校验,失败时已写入错误响应。
//
// 三条通道的必填项不同:
//   - tcp   必须填 IP(后端要直连,且要做防 SSRF 校验);
//   - feie  必须填 SN(不发起任何对内网的连接,故不做 IP 校验);
//   - agent 必须填 IP —— 它是「打印机在门店内网的地址」,由门店代理使用。
//     虽然云后端自己不会去连它(SSRF 面不存在),仍按同一规则校验:
//     地址写错时在保存阶段就报错,比让代理一遍遍连不上、堆一队列任务要好得多。
func validatePrinter(c *gin.Context, p *dto.Printer) bool {
	if strings.TrimSpace(p.PrinterName) == "" {
		fail(c, "请填写打印机名称")
		return false
	}
	if p.Provider == po.PrinterProviderFeie {
		if strings.TrimSpace(p.FeieSN) == "" {
			fail(c, "请填写飞鹅打印机编号(SN),机身标签上有")
			return false
		}
		return true
	}
	if strings.TrimSpace(p.IP) == "" {
		if p.Provider == po.PrinterProviderAgent {
			fail(c, "请填写打印机在门店内网的 IP 地址(格式如 192.168.1.8)")
			return false
		}
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
	list, err := service.ListPrinters()
	if err != nil {
		fail(c, err.Error())
		return
	}
	cats, _ := service.ListCategories()
	nameMap := map[int]string{}
	for _, ct := range cats {
		nameMap[ct.CategoryID] = ct.CategoryName
	}
	items := make([]dto.Printer, 0, len(list))
	for _, p := range list {
		ids := service.ParseIDList(p.CategoryIDs)
		var names *string
		if len(ids) > 0 {
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				if n, ok := nameMap[id]; ok {
					parts = append(parts, n)
				}
			}
			if len(parts) > 0 {
				s := strings.Join(parts, "、")
				names = &s
			}
		}
		items = append(items, dto.FromPrinter(p, ids, "", names))
	}
	tableResult(c, len(items), items)
}

func PrinterSave(c *gin.Context) {
	var p dto.Printer
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	normalizePrinter(&p)
	if !validatePrinter(c, &p) {
		return
	}
	id, err := service.InsertPrinter(p.ToPO())
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"printerId": id})
}

func PrinterUpdate(c *gin.Context) {
	var p dto.Printer
	if err := c.ShouldBindJSON(&p); err != nil || p.PrinterID == 0 {
		fail(c, "参数错误")
		return
	}
	normalizePrinter(&p)
	if !validatePrinter(c, &p) {
		return
	}
	if err := service.UpdatePrinter(p.ToPO()); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func PrinterDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	if err := service.DeletePrinter(id); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}

// PrinterTest 打一张测试页(会真的出纸)。
// 内容含当前通道信息(IP 或飞鹅 SN),便于确认改对了是哪台机器。
func PrinterTest(c *gin.Context) {
	p, okp := loadPrinterParam(c)
	if !okp {
		return
	}
	if err := print.SendTestPrint(p.ToPO()); err != nil {
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
	msg, err := print.ProbePrinter(p.ToPO())
	if err != nil {
		fail(c, "连接失败:"+err.Error())
		return
	}
	ok(c, gin.H{"message": msg})
}

// PrinterStatus 查询打印机实时状态。
//   - 飞鹅:  走 Open_queryPrinterStatus 拿在线/缺纸状态,并附带当日打印统计;
//   - TCP:   只能探一次端口连通性(直连打印机没有可查询的状态接口);
//   - agent: 云后端够不到门店内网,状态的含义是「门店代理是否还在轮询」+ 本机积压。
func PrinterStatus(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	p, err := service.LoadPrinter(id)
	if err != nil {
		fail(c, "打印机不存在")
		return
	}
	if !p.IsFeie() {
		provider := "tcp"
		if p.IsAgent() {
			provider = "agent"
		}
		msg, err := print.ProbePrinter(p)
		if err != nil {
			res := gin.H{"provider": provider, "online": false, "status": err.Error()}
			if p.IsAgent() {
				res["pending"] = service.PendingJobCountByPrinter(p.PrinterID)
			}
			ok(c, res)
			return
		}
		res := gin.H{"provider": provider, "online": true, "status": "端口可达(直连打印机无状态查询接口)"}
		if p.IsAgent() {
			res["status"] = msg
			res["pending"] = service.PendingJobCountByPrinter(p.PrinterID)
		}
		ok(c, res)
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
	if p.Provider == po.PrinterProviderAgent {
		// 代理掉线 / IP 填错时队列会一直堆积,必须给商家一个止损手段。
		n, err := print.ClearAgentQueue(p.PrinterID)
		if err != nil {
			fail(c, err.Error())
			return
		}
		okMsg(c, "已清空「"+p.PrinterName+"」的待打印队列(丢弃 "+strconv.Itoa(n)+" 条未送出任务)")
		return
	}
	if p.Provider != po.PrinterProviderFeie {
		fail(c, "清空队列仅支持飞鹅云 / 本地打印代理打印机")
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
		"user":       service.GetSetting("feie_user"),
		"apiUrl":     service.GetSetting("feie_api_url"),
		"configured": print.FeieConfigured(),
	})
}

// loadPrinterParam 按路径 :id 读取打印机,失败时已写入错误响应。
func loadPrinterParam(c *gin.Context) (dto.Printer, bool) {
	id, okid := idParam(c)
	if !okid {
		return dto.Printer{}, false
	}
	p, err := service.LoadPrinter(id)
	if err != nil {
		fail(c, "打印机不存在")
		return dto.Printer{}, false
	}
	return dto.FromPrinter(p, service.ParseIDList(p.CategoryIDs), "", nil), true
}

// printerTarget 日志/提示里用的打印机定位串。
func printerTarget(p dto.Printer) string {
	if p.Provider == po.PrinterProviderFeie {
		return "飞鹅 SN " + p.FeieSN
	}
	port := p.Port
	if port <= 0 {
		port = 9100
	}
	addr := p.IP + ":" + strconv.Itoa(port)
	if p.Provider == po.PrinterProviderAgent {
		return "门店内网 " + addr + "(由本地打印代理转发)"
	}
	return addr
}
