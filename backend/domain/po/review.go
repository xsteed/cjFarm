package po

// 评价相关常量。
const (
	// ReviewRatingMin / ReviewRatingMax 总分星级范围(1..5)。
	ReviewRatingMin = 1
	ReviewRatingMax = 5
	// ReviewTagsMax 快捷标签最多个数;ReviewTagMaxLen 单个标签最长字符数。
	ReviewTagsMax   = 6
	ReviewTagMaxLen = 12
	// ReviewContentMaxLen 文字评价最长字符数(rune);列宽 VARCHAR(500) 留余。
	ReviewContentMaxLen = 200
	// ReviewNicknameMaxLen 昵称快照最长字符数(与 tb_customer.nickname 列宽一致)。
	ReviewNicknameMaxLen = 64
)

// Review 对应 tb_review 表:一条订单评价(仅商家可见)。
//
// 追溯字段(nickname/phone/table 快照/person_count)在提交时一次性写死,不联表:
// 会员注销是物理删除档案且订单归属置 0,联表方案的追溯链会在注销后断裂;
// 快照让评价行自含「当时是谁」,与 tb_order 快照桌台名、tb_print_log 快照店名
// 同一设计哲学(见 docs/design-docs/order-review/spec.md §0.6)。
type Review struct {
	ReviewID    int
	OrderID     int
	OrderNo     string
	BizDate     string
	TableID     int
	TableNo     string
	TableName   string
	PersonCount int
	CustomerID  int    // 0 = 匿名顾客;会员注销后仅作历史编号
	Nickname    string // 会员昵称快照(匿名为空串)
	Phone       string // 会员手机号快照(provider='phone' 身份才有;匿名为空串)
	Rating      int    // 1..5
	Tags        string // 逗号分隔,≤ ReviewTagsMax 个
	Content     string // 文字评价(≤ ReviewContentMaxLen 字符)
	// ReviewerKey 评价者键(唯一索引 order_no+reviewer_key = 一人一评):
	// 会员由服务端派生「m:{customerID}」(不信前端,防顶替);匿名用设备端
	// 持久化的随机串(同设备恒定,跨设备无法识别——匿名本无稳定身份)。
	ReviewerKey string
	IpHash      string // sha256(auth.key|IP) 胡椒哈希,明文 IP 永不落库
	UaHash      string // 同上,User-Agent 哈希
	CreateTime  string
}
