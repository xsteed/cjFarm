package handler

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/print"
	"dining-system/internal/service"
	"dining-system/internal/store"
)

// intArg 将查询串解析为整数;非法时返回 nil,调用方跳过该筛选条件。
func intArg(v string) *int {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return &n
	}
	return nil
}

// ============ 订单 ============

// releaseTableTx / occupyTableTx 在事务中释放/占用桌台,与订单状态变更保持原子性。
func releaseTableTx(tx *sql.Tx, tableID int) error {
	_, err := tx.Exec(`UPDATE tb_table SET status=0, update_time=? WHERE table_id=?`, store.Now(), tableID)
	return err
}

func occupyTableTx(tx *sql.Tx, tableID int) error {
	_, err := tx.Exec(`UPDATE tb_table SET status=1, update_time=? WHERE table_id=?`, store.Now(), tableID)
	return err
}

func OrderList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	where := " WHERE 1=1"
	var args []interface{}
	if v := c.Query("orderStatus"); v != "" {
		where += " AND order_status=?"
		args = append(args, v)
	}
	if v := c.Query("payStatus"); v != "" {
		// 数值型字段必须以整数入参:SQLite 下 COALESCE(...) 表达式会丢失列亲和性,
		// 用字符串 "1" 去比整数列会恒不相等。
		if n := intArg(v); n != nil {
			where += " AND pay_status=?"
			args = append(args, *n)
		}
	}
	if v := c.Query("orderNo"); v != "" {
		where += " AND order_no LIKE ?"
		args = append(args, "%"+v+"%")
	}
	if v := c.Query("tableNo"); v != "" {
		where += " AND table_no=?"
		args = append(args, v)
	}
	// 结算方式筛选:normal 正常收款 / free 免单 / credit 挂账
	if v := c.Query("settleType"); v != "" {
		where += " AND COALESCE(settle_type,'normal')=?"
		args = append(args, v)
	}
	// 挂账状态筛选:0 非挂账 1 待收款 2 已结清
	if v := c.Query("creditStatus"); v != "" {
		if n := intArg(v); n != nil {
			where += " AND COALESCE(credit_status,0)=?"
			args = append(args, *n)
		}
	}
	var total int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order`+where, args...).Scan(&total)

	rows, err := store.DB.Query(`SELECT `+store.OrderCols+` FROM tb_order`+where+` ORDER BY order_id DESC LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Order{}
	for rows.Next() {
		o, err := store.ScanOrder(rows)
		if err == nil {
			list = append(list, o)
		}
	}
	// 批量标记待处理催菜,供列表展示角标(一次查询覆盖整页,避免逐条查库)。
	pendingUrges := store.PendingUrgeOrderIDs()
	for i := range list {
		list[i].PendingUrge = pendingUrges[list[i].OrderID]
	}
	tableResult(c, total, list)
}

func OrderGet(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_id=?`, id))
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	o.Items = store.LoadOrderItems(o.OrderID)
	// 详情同样带上催菜标记,商家可在弹窗内直接催办,无需回到列表。
	o.PendingUrge = store.HasPendingUrge(o.OrderID)
	ok(c, o)
}

// OrderBoard 看板:所有桌台 + 每桌当前进行中订单。
func OrderBoard(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT ` + store.TableCols + ` FROM tb_table WHERE del_flag='0' ORDER BY sort_order, table_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	// 一次取出待处理催菜集合,循环内直接查表标记,避免 N+1 查询。
	pendingUrges := store.PendingUrgeOrderIDs()
	list := []gin.H{}
	for rows.Next() {
		t, err := store.ScanTable(rows)
		if err != nil {
			continue
		}
		var order interface{}
		if t.Status == 1 {
			o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE table_id=? AND order_status IN (1,2,3) ORDER BY order_id DESC LIMIT 1`, t.TableID))
			if err == nil {
				o.Items = store.LoadOrderItems(o.OrderID)
				o.PendingUrge = pendingUrges[o.OrderID]
				order = o
			}
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
	if !service.ValidOrderStatus(p.OrderStatus) {
		fail(c, "非法的订单状态")
		return
	}
	// 状态机校验:仅允许合法跳转,防止已完成/已取消订单被非法改回。
	curStatus, _, err := store.GetOrderState(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.CanTransition(curStatus, p.OrderStatus) {
		fail(c, "非法的状态变更")
		return
	}
	tableID, err := store.GetOrderTable(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	// 订单状态变更与桌台占用/释放需原子完成,避免"订单已更新但桌台未同步"的中间态。
	operator := adminName(c)
	err = store.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE tb_order SET order_status=?, update_by=?, update_time=? WHERE order_id=?`, p.OrderStatus, operator, store.Now(), p.OrderID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("订单不存在")
		}
		if p.OrderStatus == 4 || p.OrderStatus == 5 {
			return releaseTableTx(tx, tableID)
		} else if p.OrderStatus >= 1 && p.OrderStatus <= 3 {
			return occupyTableTx(tx, tableID)
		}
		return nil
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 上齐(3)/完成(4)/取消(5)后催菜诉求已无意义,自动消解,避免商家端残留角标。
	if p.OrderStatus >= 3 {
		store.HandleUrgesByOrder(p.OrderID, adminName(c))
	}
	okMsg(c, "操作成功")
}

// ============ 催菜(商家端) ============

// UrgeList 催菜列表:默认返回全部,传 status=0 只看待处理。
// 按「待处理优先 + 时间倒序」排序,让 newest 的催促排在最前。
func UrgeList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	where := " WHERE 1=1"
	var args []interface{}
	if v := c.Query("status"); v != "" {
		if n := intArg(v); n != nil {
			where += " AND status=?"
			args = append(args, *n)
		}
	}
	if v := c.Query("tableNo"); v != "" {
		where += " AND table_no=?"
		args = append(args, v)
	}
	var total int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order_urge`+where, args...).Scan(&total)

	rows, err := store.DB.Query(`SELECT `+store.UrgeCols+` FROM tb_order_urge`+where+
		` ORDER BY status, urge_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.OrderUrge{}
	for rows.Next() {
		if u, err := store.ScanUrge(rows); err == nil {
			list = append(list, u)
		}
	}
	tableResult(c, total, list)
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
	operator := adminName(c)
	if p.OrderID > 0 {
		store.HandleUrgesByOrder(p.OrderID, operator)
		okMsg(c, "已处理")
		return
	}
	res, err := store.DB.Exec(`UPDATE tb_order_urge SET status=?, handle_time=?, handle_by=?
		WHERE urge_id=? AND status=?`,
		model.UrgeStatusHandled, store.Now(), operator, p.UrgeID, model.UrgeStatusPending)
	if err != nil {
		fail(c, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(c, "该催菜已处理")
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
	// 仅进行中且未支付的订单可收款,防止对已完成/已取消订单重复收款。
	curStatus, payStatus, err := store.GetOrderState(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.ActiveStatus(curStatus) {
		fail(c, "当前订单状态不可收款")
		return
	}
	if payStatus == 1 {
		fail(c, "订单已支付")
		return
	}
	// 收款即全额入账:实收金额 = 应收金额,结算方式为正常收款。
	store.DB.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?, settle_type=?, settle_time=?,
		settle_operator=?, paid_amount=total_amount-COALESCE(refund_amount,0), credit_status=0, credit_amount=0,
		update_by=?, update_time=? WHERE order_id=?`,
		service.NormalizePayType(p.PayType), store.Now(), service.SettleTypeNormal, store.Now(),
		adminName(c), adminName(c), store.Now(), p.OrderID)
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), "订单收款，方式 "+service.NormalizePayType(p.PayType))
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
	if !service.ValidSettleType(p.SettleType) {
		fail(c, "非法的结算方式")
		return
	}
	settleType := service.NormalizeSettleType(p.SettleType)
	// 免单/挂账属于让利与赊账,必须留痕:免单强制填写原因,挂账强制填写挂账单位/事由
	// (否则后续「挂账管理」无法对账催收);超长文本裁剪防污染。
	p.SettleRemark = strings.TrimSpace(p.SettleRemark)
	if r := []rune(p.SettleRemark); len(r) > 60 {
		p.SettleRemark = string(r[:60])
	}
	if settleType == service.SettleTypeFree && p.SettleRemark == "" {
		fail(c, "免单必须填写原因")
		return
	}
	if settleType == service.SettleTypeCredit && p.SettleRemark == "" {
		fail(c, "挂账必须填写挂账单位/事由")
		return
	}
	info, err := store.GetOrderSettleInfo(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.ActiveStatus(info.OrderStatus) {
		fail(c, "当前订单状态不可结账")
		return
	}
	if info.PayStatus == 1 {
		fail(c, "订单已支付，请使用「完成订单」归档")
		return
	}
	calc := service.CalcSettle(settleType, info.TotalCents)
	payType := service.NormalizePayType(p.PayType)
	if settleType == service.SettleTypeFree {
		payType = "免单"
	} else if settleType == service.SettleTypeCredit {
		payType = "挂账"
	}
	operator := adminName(c)
	now := store.Now()

	err = store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET pay_status=1, pay_type=?, pay_time=?,
			settle_type=?, settle_time=?, settle_operator=?, settle_remark=?,
			credit_status=?, credit_amount=?, paid_amount=?,
			order_status=4, finish_time=?, update_by=?, update_time=? WHERE order_id=?`,
			payType, now, settleType, now, operator, p.SettleRemark,
			calc.CreditStatus, calc.CreditCents, calc.PaidCents,
			now, operator, now, p.OrderID); err != nil {
			return err
		}
		return releaseTableTx(tx, info.TableID)
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 审计摘要:金额与原因必须写进日志 —— 免单与挂账是对账时最容易扯皮的两类动作,
	// 光看「调了 /order/settle」根本说不清让了多少利、记在谁头上。
	switch settleType {
	case service.SettleTypeFree:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("免单 ¥%.2f，原因：%s", model.ToYuan(info.TotalCents), p.SettleRemark))
	case service.SettleTypeCredit:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("挂账 ¥%.2f，事由：%s", model.ToYuan(calc.CreditCents), p.SettleRemark))
	default:
		SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
			fmt.Sprintf("结账收款 ¥%.2f，方式 %s", model.ToYuan(calc.PaidCents), payType))
	}
	// 打印结账单:免单/挂账小票会标注结算方式,便于给客人留底。
	printOrderTicket(p.OrderID)
	// 已结账离开,未处理的催菜一并消解。
	store.HandleUrgesByOrder(p.OrderID, adminName(c))
	if settleType == service.SettleTypeFree {
		okMsg(c, "免单成功，订单已完成")
		return
	}
	if settleType == service.SettleTypeCredit {
		okMsg(c, fmt.Sprintf("挂账成功，已记账 ¥%.2f，待收款", model.ToYuan(calc.CreditCents)))
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
	info, err := store.GetOrderSettleInfo(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if info.SettleType != service.SettleTypeCredit || info.CreditStatus != service.CreditStatusPending {
		fail(c, "该订单无需核销")
		return
	}
	// 已退款部分不再计入欠款。
	due := info.TotalCents - info.RefundCents
	if due < 0 {
		due = 0
	}
	operator := adminName(c)
	now := store.Now()
	res, err := store.DB.Exec(`UPDATE tb_order SET credit_status=?, credit_settle_time=?, credit_settle_by=?,
		pay_type=?, paid_amount=?, update_by=?, update_time=? WHERE order_id=? AND credit_status=?`,
		service.CreditStatusSettled, now, operator, service.NormalizePayType(p.PayType), due,
		operator, now, p.OrderID, service.CreditStatusPending)
	if err != nil {
		fail(c, "核销失败: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(c, "挂账状态已变更，请刷新后重试")
		return
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("挂账核销，收款 ¥%.2f（%s）", model.ToYuan(due), service.NormalizePayType(p.PayType)))
	okMsg(c, fmt.Sprintf("挂账已核销，收款 ¥%.2f（%s）", model.ToYuan(due), service.NormalizePayType(p.PayType)))
}

// OrderSettleCancel 撤销结算:把已免单/已挂账的订单退回未支付,便于纠错重结。
// 仅限未发生退款、且挂账尚未核销的订单;撤销后订单回到「已上齐」,桌台重新占用。
func OrderSettleCancel(c *gin.Context) {
	var p struct {
		OrderID int    `json:"orderId"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	info, err := store.GetOrderSettleInfo(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if info.PayStatus != 1 {
		fail(c, "订单未结算，无需撤销")
		return
	}
	if info.SettleType == service.SettleTypeNormal {
		fail(c, "正常收款订单请走退款流程撤销")
		return
	}
	if info.RefundCents > 0 {
		fail(c, "订单已发生退款，不能撤销结算")
		return
	}
	if info.SettleType == service.SettleTypeCredit && info.CreditStatus == service.CreditStatusSettled {
		fail(c, "挂账已核销，不能撤销")
		return
	}
	if r := []rune(p.Reason); len(r) > 60 {
		p.Reason = string(r[:60])
	}
	operator := adminName(c)
	now := store.Now()
	err = store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET pay_status=0, pay_type=NULL, pay_time=NULL,
			settle_type=?, settle_time=NULL, settle_operator='', settle_remark=?,
			credit_status=0, credit_amount=0, credit_settle_time=NULL, credit_settle_by='',
			paid_amount=0, finish_time=NULL, order_status=?, update_by=?, update_time=? WHERE order_id=?`,
			service.SettleTypeNormal, p.Reason, service.OrderStatusDining, operator, now, p.OrderID); err != nil {
			return err
		}
		return occupyTableTx(tx, info.TableID)
	})
	if err != nil {
		fail(c, "撤销失败: "+err.Error())
		return
	}
	oldType := "免单"
	if info.SettleType == service.SettleTypeCredit {
		oldType = "挂账"
	}
	reason := p.Reason
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
	o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_id=?`, orderID))
	if err != nil {
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
	curStatus, payStatus, err := store.GetOrderState(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.CanTransition(curStatus, service.OrderStatusFinished) {
		fail(c, "当前订单状态不可完成")
		return
	}
	// 完成订单 = 对「已支付」订单的收尾归档,未支付订单必须先收款/结账,避免漏收。
	if payStatus != 1 {
		fail(c, "订单未支付，请先收款或结账")
		return
	}
	tableID, err := store.GetOrderTable(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	err = store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET order_status=4, finish_time=?, update_by=?, update_time=? WHERE order_id=?`, store.Now(), adminName(c), store.Now(), p.OrderID); err != nil {
			return err
		}
		return releaseTableTx(tx, tableID)
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 订单归档后催菜诉求已无意义,自动消解,避免商家端残留角标。
	store.HandleUrgesByOrder(p.OrderID, adminName(c))
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
	curStatus, payStatus, err := store.GetOrderState(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.CanTransition(curStatus, service.OrderStatusCanceled) {
		fail(c, "当前订单状态不可取消")
		return
	}
	if payStatus == 1 {
		fail(c, "已支付订单不能直接取消，请先处理退款")
		return
	}
	tableID, err := store.GetOrderTable(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	err = store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET order_status=5, cancel_reason=?, update_by=?, update_time=? WHERE order_id=?`,
			p.CancelReason, adminName(c), store.Now(), p.OrderID); err != nil {
			return err
		}
		return releaseTableTx(tx, tableID)
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 若该订单已生成在线支付单但尚未付款,同步关闭渠道订单,防止取消后仍被扫码支付。
	closeOnlinePayment(p.OrderID)
	// 订单已作废,未处理的催菜一并消解。
	store.HandleUrgesByOrder(p.OrderID, adminName(c))
	reason := strings.TrimSpace(p.CancelReason)
	if reason == "" {
		reason = "未填写"
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID), "取消订单，原因："+reason)
	okMsg(c, "订单已取消")
}

func OrderEdit(c *gin.Context) {
	var p struct {
		OrderID     int               `json:"orderId"`
		PersonCount int               `json:"personCount"`
		OrderRemark string            `json:"orderRemark"`
		Items       []model.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderID == 0 {
		fail(c, "参数错误")
		return
	}
	// 仅进行中且未支付的订单可改单,避免改单改变已结算金额造成账目不清。
	curStatus, payStatus, err := store.GetOrderState(p.OrderID)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.ActiveStatus(curStatus) {
		fail(c, "当前订单状态不可改单")
		return
	}
	if payStatus == 1 {
		fail(c, "已支付订单不能改单")
		return
	}
	if len(p.Items) == 0 {
		fail(c, "订单明细不能为空")
		return
	}
	if p.PersonCount < 1 {
		p.PersonCount = 1
	}
	// 以数据库为准校验并重建明细(规格价格/名称),防止改单时价格被篡改或手滑写错。
	items, err := service.ResolveOrderItems(p.Items)
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 改单前的应收金额:审计要记「从多少改成多少」,只记结果等于没记。
	var oldTotal int64
	_ = store.DB.QueryRow(`SELECT COALESCE(total_amount,0) FROM tb_order WHERE order_id=?`, p.OrderID).Scan(&oldTotal)
	dishAmount, seatFee, discount, total := service.RecalcAmount(p.PersonCount, items)

	err = store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET person_count=?, order_remark=?, dish_amount=?, seat_fee=?, discount_amount=?, total_amount=?, update_by=?, update_time=? WHERE order_id=?`,
			p.PersonCount, p.OrderRemark, model.ToCents(dishAmount), model.ToCents(seatFee), model.ToCents(discount), model.ToCents(total), adminName(c), store.Now(), p.OrderID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tb_order_item WHERE order_id=?`, p.OrderID); err != nil {
			return err
		}
		for _, it := range items {
			if _, err := tx.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
				VALUES(?,?,?,?,?,?,?,?,?)`, p.OrderID, it.DishID, it.DishName, it.SpecID, it.SpecName, model.ToCents(it.Price), it.Quantity, model.ToCents(it.Amount), it.Remark); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(c, "改单失败: "+err.Error())
		return
	}
	SetAuditDetail(c, "order", strconv.Itoa(p.OrderID),
		fmt.Sprintf("改单：应收 ¥%.2f → ¥%.2f，人数 %d，明细 %d 项",
			model.ToYuan(oldTotal), total, p.PersonCount, len(items)))
	okMsg(c, "改单成功，金额已重算")
}
