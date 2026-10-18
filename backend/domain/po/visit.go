package po

// 访问统计三张表的 PO:tb_visit_log(明细,待办性质,定期清理)、
// tb_visitor(访客注册表,台账)、tb_visit_daily(日聚合台账)。
//
// 指标口径(UV 去重键、新访客、渠道定义)见
// docs/design-docs/visit-analytics/spec.md 第 1 节。

// 访问渠道。h5 = 扫码点餐页,miniprogram = 小程序启动,admin = 管理端登录。
const (
	VisitChannelH5          = "h5"
	VisitChannelMiniprogram = "miniprogram"
	VisitChannelAdmin       = "admin"
	// VisitChannelAll 是日聚合台账里的「全站合计」行(跨渠道去重后的 UV)。
	VisitChannelAll = "all"
)

// VisitLog 一条访问明细(tb_visit_log)。只插不改,无 update_time。
type VisitLog struct {
	VisitID    int
	VisitDate  string // 'YYYY-MM-DD'(本地时间自然日)
	Channel    string // h5 / miniprogram / admin
	VisitorKey string // 访客去重键:m:<id> / c:<visitorId> / a:<ipHash>:<uaHash>
	IsMember   int    // 1 = 登录会员访客
	CustomerID int    // 会员访客的 customer_id,匿名为 0
	Entry      string // 入口标识:桌台点餐码(h5);小程序场景值一期留空
	IPHash     string // sha256(ip+pepper) 前 32 位,供 UV 去重;明文 IP 在 IP 列
	UAHash     string // sha256(ua+pepper) 前 32 位
	CreateTime string

	// 终端与来源(2026-10-18 增补)。IP 明文与操作日志 oper_ip 同一取舍:
	// 明细按保留天数清理,有限期暴露;城市来自 ip2region 离线库,缺库为空;
	// OS/浏览器/设备为归一展示值(iOS/微信/手机…);机型仅小程序精确,H5 留空。
	IP      string
	City    string
	OS      string
	Browser string
	Device  string
	Model   string
}

// Visitor 访客注册表一行(tb_visitor),主键即 VisitorKey。
// 「新访客」= first_visit_date 等于统计日的访客。
type Visitor struct {
	VisitorKey     string
	FirstVisitDate string
	LastVisitDate  string
	VisitCount     int
	IsMember       int
	CustomerID     int
	CreateTime     string
}

// VisitDaily 日聚合台账一行(tb_visit_daily)。
// channel='all' 行的 UV 是跨渠道去重后的全站值;各单渠道行之和不等于它。
type VisitDaily struct {
	ID         int
	VisitDate  string
	Channel    string
	PV         int
	UV         int
	NewUV      int
	MemberUV   int
}
