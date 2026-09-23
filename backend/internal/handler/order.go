package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/print"
	"dining-system/internal/service"
)

// intArg 将查询串解析为整数;非法时返回 nil,调用方跳过该筛选条件。
func intArg(v string) *int {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return &n
	}
	return nil
}

// ============ 订单 ============

func OrderList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q service.OrderQuery
	if v := c.Query("orderStatus"); v != "" {
		q.OrderStatus = v
	}
	if v := c.Query("payStatus"); v != "" {
		q.PayStatus = intArg(v)
	}
	if v := c.Query("orderNo"); v != "" {
		q.OrderNo = v
	}
	if v := c.Query("tableNo"); v != "" {
		q.TableNo = v
	}
	// 结算方式筛选:normal 正常收款 / free 免单 / credit 挂账
	if v := c.Query("settleType"); v != "" {
		q.SettleType = v
	}
	// 挂账状态筛选:0 非挂账 1 待收款 2 已结清
	if v := c.Query("creditStatus"); v != "" {
		q.CreditStatus = intArg(v)
	}
	total, list, pending, err := service.ListOrders(q, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 批量标记待处理催菜,供列表展示角标(一次查询覆盖整页,避免逐条查库)。
	out := make([]dto.Order, 0, len(list))
	for _, o := range list {
		// 列表只返回订单主字段与催菜角标;明细仅在详情接口加载,
		// 逐单加载明细会退化成 N+1 查询(单页最多 500 次)。
		out = append(out, dto.FromOrder(o, nil, pending[o.OrderID]))
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
	items := make([]dto.OrderItem, 0, len(poItems))
	for _, it := range poItems {
		items = append(items, dto.FromOrderItem(it))
	}
	// 详情同样带上催菜标记,商家可在弹窗内直接催办,无需回到列表。
	ok(c, dto.FromOrder(o, items, pendingUrge))
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
		var order interface{}
		if t.Order != nil {
			items := make([]dto.OrderItem, 0, len(t.Order.Items))
			for _, it := range t.Order.Items {
				items = append(items, dto.FromOrderItem(it))
			}
			order = dto.FromOrder(t.Order.Order, items, t.Order.PendingUrge)
		}
		list = append(list, gin.H{
			"tableId": t.TableID, "tableNo": t.TableNo, "tableName": t.TableName,
			"capacity": t.Capacity, "status": t.Status, "order": order,
		})
	}
	ok(c, list)
}

func OrderStatus(c *gin.Context) {
	var p struct {
		OrderID     int `json:"orderId"`
		OrderStatus int `json:"orderStatus"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	// 状态机校验、终态资金约束、订单与桌台原子流转、催菜消解全部下沉 service。
	if err := service.ChangeOrderStatus(p.OrderID, p.OrderStatus, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	// 直接流转到终态(取消)时,同步关闭未支付的在线渠道单,防止旧二维码事后被扫码付款。
	if p.OrderStatus == 5 {
		closeOnlinePayment(p.OrderID)
	}
	okMsg(c, "操作成功")
}

// ============ 催菜(商家端) ============

// UrgeList 催菜列表:默认返回全部,传 status=0 只看待处理。
// 按「待处理优先 + 时间倒序」排序,让 newest 的催促排在最前。
func UrgeList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q service.UrgeQuery
	if v := c.Query("status"); v != "" {
		q.Status = intArg(v)
	}
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
	// 线下收款成功后,同步关闭该订单未支付的在线支付单:顾客若还开着此前的
	// 收款二维码,扫了会付第二笔(重复支付),关单后旧码失效。
	closeOnlinePayment(p.OrderID)
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
	printOrderTicket(p.OrderID)
	// 结账(无论何种方式)后,关闭该订单未支付的在线渠道单:顾客还开着的收款码
	// 若再被扫码支付,回调会对不上账(挂账单尤其如此 —— 渠道那笔钱系统已无处安放)。
	closeOnlinePayment(p.OrderID)
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
	payType, due, err := service.SettleCreditOrder(p.OrderID, p.PayType, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("挂账核销，收款 ¥%.2f（%s）", po.ToYuan(due), payType))
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

// printOrderTicket 读取订单并异步打印食客小票(打印失败不影响结账结果)。
//
// 只发小票机 —— 这里曾经复用 PrintOrder(它会给所有启用的打印机都发单),
// 于是结账时厨房机又被打了一张一模一样的厨房单。结账与后厨无关,
// 后厨不该为了「客人买单了」再出一张纸。
func printOrderTicket(orderID int) {
	o, ok := service.LoadOrderForPrint(orderID)
	if !ok {
		return
	}
	// 明细由 PrintGuestTicket 内部重新加载:结账要的是整桌完整菜品与最终金额,
	// 而不是调用方手里可能只带部分明细的 o.Items。
	print.PrintGuestTicket(o)
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
	// 若该订单已生成在线支付单但尚未付款,同步关闭渠道订单,防止取消后仍被扫码支付。
	closeOnlinePayment(p.OrderID)
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
	out, err := service.EditOrder(p.OrderID, p.PersonCount, p.OrderRemark, p.Items, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 改单重算了应收金额:若顾客此前已生成在线支付二维码,旧码对应旧金额,
	// 必须同步关闭渠道支付单(同 OrderCancel),防止按旧金额付款后回调因金额不符被拒导致掉单。
	closeOnlinePayment(p.OrderID)
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("改单：应收 ¥%.2f → ¥%.2f，人数 %d，明细 %d 项",
			po.ToYuan(out.OldTotalCents), out.Total, out.PersonCount, out.ItemCount))
	okMsg(c, "改单成功，金额已重算")
}
