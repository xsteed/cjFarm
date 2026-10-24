package handler

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/print"
	"dining-system/interfaces/dto"
)

// intArg 将查询串解析为整数筛选值。v 为空返回 (nil, true) 表示「不限」;
// 非空但非法时写入 400 并返回 (nil, false) —— 静默忽略会让管理端看到
// 未过滤的全量列表,却以为是筛选结果(如 payStatus=abc)。
func intArg(c *gin.Context, name string) (*int, bool) {
	v := strings.TrimSpace(c.Query(name))
	if v == "" {
		return nil, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fail(c, "参数 "+name+" 必须是整数")
		return nil, false
	}
	return &n, true
}

// dateArg 解析 YYYY-MM-DD 查询参数(报表区间/下钻用)。空返回 ("", true) 表示不限;
// 非空但格式非法时写入 400 并返回 ("", false) —— 畸形日期放进 SQL 会静默落空,
// 看起来像「当天没数据」,必须在入口拦下。
func dateArg(c *gin.Context, name string) (string, bool) {
	v := strings.TrimSpace(c.Query(name))
	if v == "" {
		return "", true
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(v) {
		fail(c, "参数 "+name+" 格式应为 YYYY-MM-DD")
		return "", false
	}
	return v, true
}

// ============ 订单 ============

func OrderList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q service.OrderQuery
	if v := c.Query("orderStatus"); v != "" {
		q.OrderStatus = v
	}
	payStatus, ok := intArg(c, "payStatus")
	if !ok {
		return
	}
	q.PayStatus = payStatus
	if v := c.Query("orderNo"); v != "" {
		q.OrderNo = v
	}
	if v := c.Query("tableNo"); v != "" {
		q.TableNo = v
	}
	// 桌台名称模糊匹配(如「大厅01桌」):与桌号筛选并列,名称可随时改,
	// 历史单留的是改名前的快照,必须服务端过滤(同 settleRemark 的理由)。
	if v := c.Query("tableName"); v != "" {
		q.TableName = v
	}
	// 结算方式筛选:normal 正常收款 / free 免单 / credit 挂账
	if v := c.Query("settleType"); v != "" {
		q.SettleType = v
	}
	// 结算备注(免单原因/挂账人)模糊匹配。挂账人是自由文本,由服务端过滤,
	// 前端只过滤当前页会让第 2 页起的命中记录永远搜不到。
	if v := c.Query("settleRemark"); v != "" {
		q.SettleRemark = v
	}
	// 挂账状态筛选:0 非挂账 1 待收款 2 已结清
	creditStatus, ok := intArg(c, "creditStatus")
	if !ok {
		return
	}
	q.CreditStatus = creditStatus
	// 只看新单:已下单且商家从未操作过(viewed_at 为空),口径与列表「新」角标一致。
	if v := c.Query("newOnly"); v == "1" || v == "true" {
		q.NewOnly = true
	}
	// 营业日区间(YYYY-MM-DD,含首尾,单边可省):报表图表下钻用。
	// 口径与日报表「订单数」一致 —— 按 create_time 折算营业日(见 dao.bizDateExpr)。
	start, ok := dateArg(c, "start")
	if !ok {
		return
	}
	q.BizStart = start
	end, ok := dateArg(c, "end")
	if !ok {
		return
	}
	q.BizEnd = end
	// 下单小时(0-23):时段分布图下钻用。
	hour, ok := intArg(c, "hour")
	if !ok {
		return
	}
	if hour != nil && (*hour < 0 || *hour > 23) {
		fail(c, "参数 hour 需在 0-23 之间")
		return
	}
	q.Hour = hour
	// 下钻同口径:报表各图表的订单数都不含已取消,弹窗带 hideCanceled=1 时排除。
	if v := c.Query("hideCanceled"); v == "1" || v == "true" {
		q.ExcludeCanceled = true
	}
	total, list, pending, err := service.ListOrders(q, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 批量标记待处理催菜,供列表展示角标(一次查询覆盖整页,避免逐条查库)。
	out := make([]dto.Order, 0, len(list))
	// 批次计数同样一次 IN 查询覆盖整页:列表有「加菜×N」角标,商家不点详情
	// 也能看出哪桌加过菜(加菜高峰期的后厨优先级判断常以列表为准)。
	ids := make([]int, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.OrderID)
	}
	batchCounts := service.ListOrderBatchCounts(ids)
	// 处理中退款金额同样一次 IN 查询覆盖整页:refund_amount 只在退款成功时累加,
	// 不带上这个「退款中」口径,收银会看不出该单正在退款(按钮可点、点了必被后端拒)。
	refunding := service.ListOrderRefundingAmounts(ids)
	for _, o := range list {
		// 列表只返回订单主字段与催菜角标;明细仅在详情接口加载,
		// 逐单加载明细会退化成 N+1 查询(单页最多 500 次)。
		d := dto.FromOrder(o, nil, pending[o.OrderID])
		d.BatchCount = batchCounts[o.OrderID]
		d.RefundingAmount = po.ToYuan(refunding[o.OrderID])
		// 「新」角标:已下单且商家从未查看/操作过(viewed_at 为空)。
		// 只在列表接口算:详情一打开就会被标记为已查看,顾客端也不需要这个内部状态。
		// 每行都显式填充(true/false 恒二选一),前端与联调不用猜「缺字段= false」。
		isNew := o.OrderStatus == service.OrderStatusPlaced && o.ViewedAt == nil
		d.IsNew = &isNew
		// 「待完成」提示:已支付且订单仍是进行中(先付收款后到点「完成订单」归档之间的状态)。
		// 这期间桌台仍被占用,漏点「完成订单」桌台就一直释放不了 —— 给列表一个显式提示。
		needFinish := service.ActiveStatus(o.OrderStatus) && o.PayStatus == 1
		d.NeedFinish = &needFinish
		out = append(out, d)
	}
	tableResult(c, total, out)
}

func OrderGet(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	o, poItems, pendingUrge, err := service.GetOrder(id)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	// 注意:仅查看不消解「新」角标 —— 「新」的口径是「未操作过」,
	// 商家点开看一眼不代表处理了这单;只有实际操作(制作/收款/结账/取消/完成/改单/催办/补打)才标记。
	items := make([]dto.OrderItem, 0, len(poItems))
	for _, it := range poItems {
		items = append(items, dto.FromOrderItem(it))
	}
	// 详情同样带上催菜标记,商家可在弹窗内直接催办,无需回到列表。
	ok(c, dto.FromOrder(o, items, pendingUrge))
}

// OrderStale 未结账超时告警:当天开台超过配置阈值仍未收款的订单。
//
// 开关与阈值一并下发(而不是让前端去读系统配置):告警开关属 config:edit 侧的管理项,
// 而看板只要 order:view 就能进 —— 前端各自判断会出现「开关开了但界面不显示」的错配。
func OrderStale(c *gin.Context) {
	ok(c, service.ListStaleOrderAlerts())
}

// OrderBoard 看板:所有桌台 + 每桌当前进行中订单。
func OrderBoard(c *gin.Context) {
	tables, err := service.OrderBoard()
	if err != nil {
		fail(c, err.Error())
		return
	}
	list := []gin.H{}
	for _, t := range tables {
		orders := make([]interface{}, 0, len(t.Orders))
		for _, bo := range t.Orders {
			items := make([]dto.OrderItem, 0, len(bo.Items))
			for _, it := range bo.Items {
				items = append(items, dto.FromOrderItem(it))
			}
			orders = append(orders, dto.FromOrder(bo.Order, items, bo.PendingUrge))
		}
		list = append(list, gin.H{
			"tableId": t.TableID, "tableNo": t.TableNo, "tableName": t.TableName,
			"capacity": t.Capacity, "status": t.Status, "orders": orders,
		})
	}
	ok(c, list)
}

func OrderStatus(c *gin.Context) {
	var p struct {
		OrderID      int    `json:"orderId"`
		OrderStatus  int    `json:"orderStatus"`
		CancelReason string `json:"cancelReason"` // 仅 orderStatus=5 时有意义
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	// 状态机校验、终态资金约束、订单与桌台原子流转、取消原因与库存回补、催菜消解全部下沉 service。
	if err := service.ChangeOrderStatus(p.OrderID, p.OrderStatus, p.CancelReason, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	// 列表上直接点「制作/上齐」也算处理过这单:消解「新」角标。
	service.MarkOrderViewed(p.OrderID)
	// 直接流转到终态(取消)时,同步关闭未支付的在线渠道单,防止旧二维码事后被扫码付款。
	if p.OrderStatus == 5 {
		closeOnlinePayment(c.Request.Context(), p.OrderID)
	}
	okMsg(c, "操作成功")
}

// ============ 催菜(商家端) ============

// UrgeList 催菜列表:默认返回全部,传 status=0 只看待处理。
// 按「待处理优先 + 时间倒序」排序,让 newest 的催促排在最前。
func UrgeList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q service.UrgeQuery
	status, ok := intArg(c, "status")
	if !ok {
		return
	}
	q.Status = status
	if v := c.Query("tableNo"); v != "" {
		q.TableNo = v
	}
	total, list, err := service.ListUrges(q, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	out := make([]dto.OrderUrge, 0, len(list))
	for _, u := range list {
		out = append(out, dto.FromOrderUrge(u))
	}
	tableResult(c, total, out)
}

// UrgeHandle 处理催菜:标记为已处理。
// 传 orderId 批量处理该订单的全部待处理催菜(适合一键消解),传 urgeId 处理单条。
func UrgeHandle(c *gin.Context) {
	var p struct {
		UrgeID  int `json:"urgeId"`
		OrderID int `json:"orderId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || (p.UrgeID == 0 && p.OrderID == 0) {
		fail(c, "参数错误")
		return
	}
	if err := service.HandleUrges(p.UrgeID, p.OrderID, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	// 处理催菜也是商家对这张单的明确操作,「新」角标一并消解。
	service.MarkOrderViewed(p.OrderID)
	okMsg(c, "已处理")
}

func OrderPay(c *gin.Context) {
	var p struct {
		OrderID int    `json:"orderId"`
		PayType string `json:"payType"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	// 仅进行中且未支付的订单可收款;校验与收款落库下沉 service。
	payType, err := service.PayOrder(p.OrderID, p.PayType, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 收款后订单可能仍是「已下单」(不收档),必须显式记已查看,否则「新」角标会一直挂着。
	service.MarkOrderViewed(p.OrderID)
	// 线下收款成功后,同步关闭该订单未支付的在线支付单:顾客若还开着此前的
	// 收款二维码,扫了会付第二笔(重复支付),关单后旧码失效。
	closeOnlinePayment(c.Request.Context(), p.OrderID)
	// 先付模式下,前台手工收款同样要触发后厨出单(否则「线上下单 + 前台付现金」的单没人做)。
	printKitchenOnPaid(c.Request.Context(), p.OrderID)
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), "订单收款，方式 "+payType)
	okMsg(c, "支付成功")
}

// OrderSettle 结账:支持三种结算方式,收款/免单/挂账后订单标记完成并释放桌台。
//   - normal 正常收款:实收 = 应收;
//   - free   免单:实收 0,必须填写免单原因(留痕用于对账);
//   - credit 挂账:实收 0,应收金额记入挂账,后续在「挂账管理」核销收款。
func OrderSettle(c *gin.Context) {
	var p struct {
		OrderID      int    `json:"orderId"`
		SettleType   string `json:"settleType"`
		PayType      string `json:"payType"`
		SettleRemark string `json:"settleRemark"`
		// PrintTicket 本次是否出小票。指针类型是为了区分「前端没传」与「显式 false」:
		// 老客户端不传该字段,必须维持「结账即打印」的既有行为。
		PrintTicket *bool `json:"printTicket"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	out, err := service.SettleOrder(p.OrderID, p.SettleType, p.PayType, p.SettleRemark, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	// 审计摘要:金额与原因必须写进日志 —— 免单与挂账是对账时最容易扯皮的两类动作,
	// 光看「调了 /order/settle」根本说不清让了多少利、记在谁头上。
	switch out.SettleType {
	case service.SettleTypeFree:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("免单 ¥%.2f，原因：%s", po.ToYuan(out.TotalCents), out.Remark))
	case service.SettleTypeCredit:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("挂账 ¥%.2f，事由：%s", po.ToYuan(out.CreditCents), out.Remark))
	default:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("结账收款 ¥%.2f，方式 %s", po.ToYuan(out.PaidCents), out.PayType))
	}
	// 打印结账单:免单/挂账小票会标注结算方式,便于给客人留底。
	// 免单/挂账常常并不需要给客人出票(留痕在审计日志里),因此由收银在弹窗里
	// 决定本次是否出纸;未传该字段时保持「结账即打印」的既有行为。
	if p.PrintTicket == nil || *p.PrintTicket {
		printOrderTicket(c.Request.Context(), p.OrderID)
	}
	// 结账(无论何种方式)后,关闭该订单未支付的在线渠道单:顾客还开着的收款码
	// 若再被扫码支付,回调会对不上账(挂账单尤其如此 —— 渠道那笔钱系统已无处安放)。
	closeOnlinePayment(c.Request.Context(), p.OrderID)
	if out.SettleType == service.SettleTypeFree {
		okMsg(c, "免单成功，订单已完成")
		return
	}
	if out.SettleType == service.SettleTypeCredit {
		okMsg(c, fmt.Sprintf("挂账成功，已记账 ¥%.2f，待收款", po.ToYuan(out.CreditCents)))
		return
	}
	okMsg(c, "结账成功")
}

// OrderCreditSettle 挂账核销:对挂账中的订单补收欠款。
func OrderCreditSettle(c *gin.Context) {
	var p struct {
		OrderID int    `json:"orderId"`
		PayType string `json:"payType"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	payType, due, err := service.SettleCreditOrder(c.Request.Context(), p.OrderID, p.PayType, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("挂账核销，收款 ¥%.2f（%s）", po.ToYuan(due), payType))
	// 补打一张「挂账已结」小票:客人此前拿到的那张写的是「待收」,欠款结清
	// 必须有一张书面凭证,否则口头核销日后对不上账。
	printOrderTicket(c.Request.Context(), p.OrderID)
	okMsg(c, fmt.Sprintf("挂账已核销，收款 ¥%.2f（%s）", po.ToYuan(due), payType))
}

// OrderSettleCancel 撤销结算:把已免单/已挂账的订单退回未支付,便于纠错重结。
// 仅限未发生退款、且挂账尚未核销的订单;撤销后订单回到「结账前状态」(历史订单
// 未记录结账前状态时回退为已上齐),桌台重新占用(顾客通常仍在场,继续用餐/重结)。
func OrderSettleCancel(c *gin.Context) {
	var p struct {
		OrderID int    `json:"orderId"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	out, err := service.CancelSettleOrder(p.OrderID, p.Reason, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	oldType := "正常收款"
	switch out.SettleType {
	case service.SettleTypeCredit:
		oldType = "挂账"
	case service.SettleTypeFree:
		oldType = "免单"
	}
	reason := out.Reason
	if reason == "" {
		reason = "未填写"
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), "撤销"+oldType+"结算，原因："+reason)
	okMsg(c, "已撤销结算，订单恢复为未支付")
}

// printOrderTicket 读取订单并异步打印结账小票(打印失败不影响结账结果)。
//
// 只发小票机 —— 这里曾经复用 PrintOrder(它会给所有启用的打印机都发单),
// 于是结账时厨房机又被打了一张一模一样的厨房单。结账与后厨无关,
// 后厨不该为了「客人买单了」再出一张纸。
func printOrderTicket(ctx context.Context, orderID int) {
	o, ok := service.LoadOrderForPrint(orderID)
	if !ok {
		return
	}
	// 明细由 PrintGuestTicket 内部重新加载:结账要的是整桌完整菜品与最终金额,
	// 而不是调用方手里可能只带部分明细的 o.Items。
	print.PrintGuestTicket(ctx, o)
}

// printKitchenOnPaid 先付模式专属:订单「首次收款成功」后,先把结账小票发给小票机、
// 再把厨单发给后厨。
//
// 为什么单独抽出来:先付模式下下单时不出单(customer.go 已跳过),出单时机后移到收款成功;
// 而收款有两条互不相通的路径 —— 线上支付回调/查单(pay.go)与前台手工收款(OrderPay),
// 两者都必须触发,否则「线上下单 + 前台收现金」的顾客会一单都吃不上。
//
// 这里刻意不再走 PrintOrder(它会连带出一张点菜单):先付模式收款成功那一刻,
// 订单就已经是「已支付」,后续不会再有独立的结账动作(SettleOrder 对已支付订单
// 直接拒绝)—— 收款时出的这张小票是该订单唯一的付款凭证,因此直接按
// PrintGuestTicket(trigger=settle)出【结账小票】,点菜单与付款凭证合一;
// 厨房单走 PrintKitchenNew(trigger=order):收款成功那一刻是该订单的第一张
// 厨房单,整单全部菜品都要现做,抬头必须是【厨房单】而非【加菜单】——
// 此前误用加菜专用的 PrintKitchen,整单被写死成【加菜单】+trigger=append。
// 后付模式下厨单已在下单时出过,这里直接返回,不会重复出纸。
// 打印在 print 包内异步执行且自带失败留痕,故本函数不返回错误,不影响收款结果。
func printKitchenOnPaid(ctx context.Context, orderID int) {
	if !service.PayFirstEnabled() {
		return
	}
	o, items, _, err := service.GetOrder(orderID)
	if err != nil {
		logger.CtxWarnf(ctx, "[print][告警] 先付模式收款后读取订单失败,厨单未出 订单%d: %v", orderID, err)
		return
	}
	// 先出结账小票、再发厨房单:两者的分发顺序按此固定。
	// 各自的发送在 print 包内仍异步执行,不互相等待 —— 这里的先后只是分发次序,
	// 实际出纸快慢取决于各打印机,无需也不能互相阻塞收款流程。
	printOrderTicket(ctx, orderID)
	print.PrintKitchenNew(ctx, o, items)
}

func OrderFinish(c *gin.Context) {
	var p struct {
		OrderID int `json:"orderId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.FinishOrder(p.OrderID, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	okMsg(c, "订单已完成并归档")
}

func OrderCancel(c *gin.Context) {
	var p struct {
		OrderID      int    `json:"orderId"`
		CancelReason string `json:"cancelReason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	reason, err := service.CancelOrder(p.OrderID, p.CancelReason, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	// 若该订单已生成在线支付单但尚未付款,同步关闭渠道订单,防止取消后仍被扫码支付。
	closeOnlinePayment(c.Request.Context(), p.OrderID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "未填写"
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), "取消订单，原因："+reason)
	okMsg(c, "订单已取消")
}

func OrderEdit(c *gin.Context) {
	var p struct {
		OrderID     int             `json:"orderId"`
		PersonCount int             `json:"personCount"`
		OrderRemark string          `json:"orderRemark"`
		Items       []dto.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	out, err := service.EditOrder(c.Request.Context(), p.OrderID, p.PersonCount, p.OrderRemark, p.Items, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	service.MarkOrderViewed(p.OrderID)
	// 改单重算了应收金额:若顾客此前已生成在线支付二维码,旧码对应旧金额,
	// 必须同步关闭渠道支付单(同 OrderCancel),防止按旧金额付款后回调因金额不符被拒导致掉单。
	closeOnlinePayment(c.Request.Context(), p.OrderID)
	// 改单前金额读取失败时写「未知」而不是「¥0.00」:审计记录事后要能据此对账,
	// 把查询失败折叠成 0 会凭空造出一个「应收 0 元」的假事实。
	oldTotalText := fmt.Sprintf("¥%.2f", po.ToYuan(out.OldTotalCents))
	if out.OldTotalUnknown {
		oldTotalText = "未知"
	}
	summary := fmt.Sprintf("改单：应收 %s → ¥%.2f，人数 %d，明细 %d 项",
		oldTotalText, out.Total, out.PersonCount, out.ItemCount)
	// 赠菜是让利动作,必须在审计摘要里单独说清赠了几道、让了多少利。
	if out.GiftCount > 0 {
		summary += fmt.Sprintf("，赠菜 %d 道（¥%.2f）", out.GiftCount, out.GiftAmount)
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), summary)
	okMsg(c, "改单成功，金额已重算")
}
