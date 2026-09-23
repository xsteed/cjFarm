package service

import (
	"database/sql"
	"fmt"
	"strings"

	"dining-system/infra/logger"
	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============ 订单域用例 ============
//
// 本文件承载订单/催菜的业务编排:状态机校验、金额重算、事务编排与催菜消解。
// 数据访问统一走 dao,事务用 store.WithTx;不依赖 handler/print/pay。
// 金额计算复用 service.go 的 CalcSettle/RecalcAmount/ResolveOrderItems 等纯函数。

// OrderQuery 订单列表筛选条件;零值字段表示不限。
// 与 dao.OrderQuery 解耦,handler 无需感知数据访问层类型。
type OrderQuery struct {
	OrderStatus  string // 空表示不限
	PayStatus    *int   // nil 表示不限
	OrderNo      string // 模糊匹配
	TableNo      string // 精确匹配
	SettleType   string // normal/free/credit
	CreditStatus *int   // nil 表示不限
}

// UrgeQuery 催菜列表筛选条件;零值字段表示不限。
type UrgeQuery struct {
	Status  *int   // nil 表示不限
	TableNo string // 精确匹配
}

// ListOrders 按条件分页查询订单列表,并返回待处理催菜角标集合。
// 催菜角标一次查询覆盖整页,避免逐单查库退化成 N+1。
func ListOrders(q OrderQuery, pageNum, pageSize int) (total int, list []po.Order, pending map[int]bool, err error) {
	total, list, err = dao.ListOrders(dao.OrderQuery{
		OrderStatus:  q.OrderStatus,
		PayStatus:    q.PayStatus,
		OrderNo:      q.OrderNo,
		TableNo:      q.TableNo,
		SettleType:   q.SettleType,
		CreditStatus: q.CreditStatus,
	}, pageNum, pageSize)
	if err != nil {
		return 0, nil, nil, err
	}
	return total, list, dao.PendingUrgeOrderIDs(), nil
}

// GetOrder 读取订单详情:返回订单、明细与待处理催菜标记。
func GetOrder(orderID int) (po.Order, []po.OrderItem, bool, error) {
	o, err := dao.GetOrderByID(orderID)
	if err != nil {
		return po.Order{}, nil, false, err
	}
	return o, dao.LoadOrderItems(o.OrderID), dao.HasPendingUrge(o.OrderID), nil
}

// BoardOrder 看板中某桌当前进行中的订单及其明细、催菜标记。
type BoardOrder struct {
	Order       po.Order
	Items       []po.OrderItem
	PendingUrge bool
}

// BoardTable 看板中的一桌;无进行中订单时 Order 为 nil。
type BoardTable struct {
	TableID   int
	TableNo   string
	TableName string
	Capacity  int
	Status    int
	Order     *BoardOrder
}

// OrderBoard 返回所有桌台及每桌当前进行中的订单。
//
// 旧实现逐桌 GetActiveOrderByTable + 逐单 LoadOrderItems,桌台越多查询越多(N+1);
// 这里改为:全部进行中订单一次查出、明细按 order_id 批量 IN 一次查出,内存按桌台
// 分组组装,总查询数从 O(桌台数×2) 降到常数条。
func OrderBoard() ([]BoardTable, error) {
	tables, err := dao.ListAllTables()
	if err != nil {
		return nil, err
	}
	// 一次取出待处理催菜集合,循环内直接查表标记,避免 N+1 查询。
	pendingUrges := dao.PendingUrgeOrderIDs()

	activeOrders, err := dao.ListActiveOrders()
	if err != nil {
		return nil, err
	}
	// 每桌只保留 order_id 最大(最近)的一笔进行中订单,与旧实现
	// GetActiveOrderByTable 的 ORDER BY order_id DESC LIMIT 1 语义一致。
	// 正常业务一桌同时仅一笔进行中订单(下单时已防重),这里仅是兜底。
	byTable := map[int]po.Order{}
	for _, o := range activeOrders {
		if cur, ok := byTable[o.TableID]; !ok || o.OrderID > cur.OrderID {
			byTable[o.TableID] = o
		}
	}
	orderIDs := make([]int, 0, len(byTable))
	for _, o := range byTable {
		orderIDs = append(orderIDs, o.OrderID)
	}
	itemsByOrder, err := dao.LoadOrderItemsByOrderIDs(orderIDs)
	if err != nil {
		return nil, err
	}

	out := make([]BoardTable, 0, len(tables))
	for _, t := range tables {
		bt := BoardTable{
			TableID:   t.TableID,
			TableNo:   t.TableNo,
			TableName: t.TableName,
			Capacity:  t.Capacity,
			Status:    t.Status,
		}
		if t.Status == 1 {
			if o, ok := byTable[t.TableID]; ok {
				items := itemsByOrder[o.OrderID]
				if items == nil {
					items = []po.OrderItem{} // 保持无明细时返回空数组而非 null
				}
				bt.Order = &BoardOrder{
					Order:       o,
					Items:       items,
					PendingUrge: pendingUrges[o.OrderID],
				}
			}
		}
		out = append(out, bt)
	}
	return out, nil
}

// ChangeOrderStatus 订单状态流转:校验状态机与终态资金约束后,
// 原子更新订单状态并同步桌台占用;上齐/完成/取消后自动消解待处理催菜。
func ChangeOrderStatus(orderID, target int, operator string) error {
	if !ValidOrderStatus(target) {
		return fmt.Errorf("非法的订单状态")
	}
	curStatus, payStatus, err := dao.GetOrderState(orderID)
	if err != nil {
		return fmt.Errorf("订单不存在")
	}
	if !CanTransition(curStatus, target) {
		return fmt.Errorf("非法的状态变更")
	}
	// 置为已完成(4)等同「归档收尾」,未收款订单必须先收款/结账,防止漏收。
	if target == OrderStatusFinished && payStatus != 1 {
		return fmt.Errorf("订单未支付，请先收款或结账")
	}
	// 置为已取消(5)同样要求未支付:已收款订单先退款,防止「钱已收、单已废」。
	if target == OrderStatusCanceled && payStatus == 1 {
		return fmt.Errorf("已支付订单不能取消，请先处理退款")
	}
	tableID, err := dao.GetOrderTable(orderID)
	if err != nil {
		return fmt.Errorf("订单不存在")
	}
	// 订单状态变更与桌台占用/释放需原子完成,避免中间态。
	err = store.WithTx(func(tx *sql.Tx) error {
		// 带当前状态守卫:并发「完成」与「取消」只有先到者生效。
		n, err := dao.UpdateOrderStatusTx(tx, orderID, target, operator, curStatus)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		if target == OrderStatusFinished || target == OrderStatusCanceled {
			return dao.SetTableStatusTx(tx, tableID, 0)
		} else if target >= OrderStatusPlaced && target <= OrderStatusDining {
			return dao.SetTableStatusTx(tx, tableID, 1)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// 上齐(3)/完成(4)/取消(5)后催菜诉求已无意义,自动消解。
	if target >= OrderStatusDining {
		dao.HandleUrgesByOrder(orderID, operator)
	}
	return nil
}

// ListUrges 分页查询催菜记录(待处理优先 + 时间倒序由 dao 保证)。
func ListUrges(q UrgeQuery, pageNum, pageSize int) (total int, list []po.OrderUrge, err error) {
	return dao.ListUrges(dao.UrgeQuery{Status: q.Status, TableNo: q.TableNo}, pageNum, pageSize)
}

// HandleUrges 处理催菜:传 orderID 批量处理该订单全部待处理催菜,
// 传 urgeID 处理单条。orderID 优先。
func HandleUrges(urgeID, orderID int, operator string) error {
	if orderID > 0 {
		dao.HandleUrgesByOrder(orderID, operator)
		return nil
	}
	affected, err := dao.HandleUrge(urgeID, operator)
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("该催菜已处理")
	}
	return nil
}

// PayOrder 收款:仅进行中且未支付的订单可全额收款。
// 返回归一化后的支付方式,供 handler 组装审计摘要。
func PayOrder(orderID int, payType, operator string) (string, error) {
	payType = NormalizePayType(payType)
	curStatus, payStatus, err := dao.GetOrderState(orderID)
	if err != nil {
		return payType, fmt.Errorf("订单不存在")
	}
	if !ActiveStatus(curStatus) {
		return payType, fmt.Errorf("当前订单状态不可收款")
	}
	if payStatus == 1 {
		return payType, fmt.Errorf("订单已支付")
	}
	// 收款即全额入账:实收金额 = 应收金额,结算方式为正常收款。
	// 带守卫条件:仅「进行中且未支付」可收款,并发收款/结账/取消只有先到者生效。
	n, err := dao.MarkOrderPaid(orderID, payType, operator)
	if err != nil {
		return payType, fmt.Errorf("收款失败: %w", err)
	}
	if n == 0 {
		return payType, fmt.Errorf("订单状态已变更，请刷新后重试")
	}
	return payType, nil
}

// SettleOutcome 一次结算的结果,供 handler 组装审计摘要与响应文案。
type SettleOutcome struct {
	SettleType  string // 结算方式(normal/free/credit)
	PayType     string // 展示用支付方式(免单/挂账时为对应文案)
	Remark      string // 裁剪后的结算备注(免单原因/挂账事由)
	TotalCents  int64  // 应收金额(分)
	PaidCents   int64  // 实收金额(分)
	CreditCents int64  // 挂账金额(分)
}

// SettleOrder 结账:normal 正常收款 / free 免单 / credit 挂账。
// 完成后订单归档、释放桌台并消解待处理催菜。
func SettleOrder(orderID int, settleTypeRaw, payTypeRaw, remark, operator string) (SettleOutcome, error) {
	if !ValidSettleType(settleTypeRaw) {
		return SettleOutcome{}, fmt.Errorf("非法的结算方式")
	}
	settleType := NormalizeSettleType(settleTypeRaw)
	// 免单/挂账属于让利与赊账,必须留痕;超长文本裁剪防污染。
	remark = strings.TrimSpace(remark)
	if r := []rune(remark); len(r) > 60 {
		remark = string(r[:60])
	}
	if settleType == SettleTypeFree && remark == "" {
		return SettleOutcome{}, fmt.Errorf("免单必须填写原因")
	}
	if settleType == SettleTypeCredit && remark == "" {
		return SettleOutcome{}, fmt.Errorf("挂账必须填写挂账单位/事由")
	}
	info, err := dao.GetOrderSettleInfo(orderID)
	if err != nil {
		return SettleOutcome{}, fmt.Errorf("订单不存在")
	}
	if !ActiveStatus(info.OrderStatus) {
		return SettleOutcome{}, fmt.Errorf("当前订单状态不可结账")
	}
	if info.PayStatus == 1 {
		return SettleOutcome{}, fmt.Errorf("订单已支付，请使用「完成订单」归档")
	}
	calc := CalcSettle(settleType, info.TotalCents)
	payType := NormalizePayType(payTypeRaw)
	if settleType == SettleTypeFree {
		payType = "免单"
	} else if settleType == SettleTypeCredit {
		payType = "挂账"
	}
	now := store.Now()
	err = store.WithTx(func(tx *sql.Tx) error {
		// 带守卫:仅「进行中且未支付」可结账。并发两次结账时只有先到者生效。
		// pre_settle_status 记录结账前的真实状态,撤销结算时按它恢复而非硬编码回 3。
		n, err := dao.SettleOrderTx(tx, orderID, dao.SettleOrderParams{
			SettleType:   settleType,
			PayType:      payType,
			SettleRemark: remark,
			Operator:     operator,
			Now:          now,
			CreditStatus: calc.CreditStatus,
			CreditCents:  calc.CreditCents,
			PaidCents:    calc.PaidCents,
			PreStatus:    info.OrderStatus,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return dao.SetTableStatusTx(tx, info.TableID, 0)
	})
	if err != nil {
		return SettleOutcome{}, err
	}
	// 已结账离开,未处理的催菜一并消解。
	dao.HandleUrgesByOrder(orderID, operator)
	return SettleOutcome{
		SettleType:  settleType,
		PayType:     payType,
		Remark:      remark,
		TotalCents:  info.TotalCents,
		PaidCents:   calc.PaidCents,
		CreditCents: calc.CreditCents,
	}, nil
}

// SettleCreditOrder 挂账核销:对挂账中的订单补收欠款。
// 返回归一化支付方式与实收金额(分)。
func SettleCreditOrder(orderID int, payTypeRaw, operator string) (string, int64, error) {
	payType := NormalizePayType(payTypeRaw)
	info, err := dao.GetOrderSettleInfo(orderID)
	if err != nil {
		return payType, 0, fmt.Errorf("订单不存在")
	}
	if info.SettleType != SettleTypeCredit || info.CreditStatus != CreditStatusPending {
		return payType, 0, fmt.Errorf("该订单无需核销")
	}
	// 已退款部分不再计入欠款。
	//
	// 口径说明:info.RefundCents 来自 tb_order.refund_amount,只在退款成功时累加;
	// 处理中的退款单已占用可退额度、渠道侧可能已划走钱,却尚未累加进 refund_amount。
	// 核销若只看 refund_amount,会把这部分处理中退款也算进欠款、向顾客多收。
	// 因此这里改用「已成功 + 处理中」口径(dao.SumRefundedInclPending)扣减;
	// 查询失败时 fail-closed 拒绝核销,避免拿 0 当已退金额放大欠款。
	refundedInclPending, err := dao.SumRefundedInclPending(info.OrderNo)
	if err != nil {
		logger.Warnf("[order] 挂账核销退款额度查询失败,拒绝核销 订单%s: %v", info.OrderNo, err)
		return payType, 0, fmt.Errorf("退款金额查询失败，请稍后重试")
	}
	due := info.TotalCents - refundedInclPending
	if due < 0 {
		due = 0
	}
	n, err := dao.SettleCreditOrder(orderID, due, payType, operator)
	if err != nil {
		return payType, 0, fmt.Errorf("核销失败: %w", err)
	}
	if n == 0 {
		return payType, 0, fmt.Errorf("挂账状态已变更，请刷新后重试")
	}
	return payType, due, nil
}

// CancelSettleOutcome 撤销结算的结果,供 handler 组装审计摘要。
type CancelSettleOutcome struct {
	SettleType string // 撤销前的结算方式
	Reason     string // 裁剪后的撤销原因
}

// CancelSettleOrder 撤销结算:把已免单/已挂账(或线下收款)的订单退回未支付,
// 恢复到结账前状态并重新占用桌台。
func CancelSettleOrder(orderID int, reason, operator string) (CancelSettleOutcome, error) {
	info, err := dao.GetOrderSettleInfo(orderID)
	if err != nil {
		return CancelSettleOutcome{}, fmt.Errorf("订单不存在")
	}
	if info.PayStatus != 1 {
		return CancelSettleOutcome{}, fmt.Errorf("订单未结算，无需撤销")
	}
	if info.SettleType == SettleTypeNormal && info.PayChannel != "" {
		// 在线支付订单必须走退款流程原路退回,不能简单回退支付状态。
		return CancelSettleOutcome{}, fmt.Errorf("在线支付订单请走退款流程撤销")
	}
	if info.RefundCents > 0 {
		return CancelSettleOutcome{}, fmt.Errorf("订单已发生退款，不能撤销结算")
	}
	if info.SettleType == SettleTypeCredit && info.CreditStatus == CreditStatusSettled {
		return CancelSettleOutcome{}, fmt.Errorf("挂账已核销，不能撤销")
	}
	if r := []rune(reason); len(r) > 60 {
		reason = string(r[:60])
	}
	// 恢复到结账前的真实状态;历史订单(未记录快照,值为 0)回退为已上齐(3)。
	restoreStatus := info.PreSettleStatus
	if restoreStatus < OrderStatusPlaced || restoreStatus > OrderStatusDining {
		restoreStatus = OrderStatusDining
	}
	now := store.Now()
	err = store.WithTx(func(tx *sql.Tx) error {
		// 守卫:仅「已收款」(pay_status=1)且 order_status IN (1,2,3,4) 的订单可撤销。
		// 1/2/3 对应现金收款后未归档(仍处进行中)的订单,4 对应已结算归档,与并发完成/重结互斥。
		n, err := dao.CancelSettleTx(tx, orderID, SettleTypeNormal, reason, restoreStatus, operator, now)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return dao.SetTableStatusTx(tx, info.TableID, 1)
	})
	if err != nil {
		return CancelSettleOutcome{}, fmt.Errorf("撤销失败: %w", err)
	}
	return CancelSettleOutcome{SettleType: info.SettleType, Reason: reason}, nil
}

// FinishOrder 完成订单归档:已支付订单置为已完成并释放桌台。
func FinishOrder(orderID int, operator string) error {
	curStatus, payStatus, err := dao.GetOrderState(orderID)
	if err != nil {
		return fmt.Errorf("订单不存在")
	}
	if !CanTransition(curStatus, OrderStatusFinished) {
		return fmt.Errorf("当前订单状态不可完成")
	}
	// 完成订单 = 对「已支付」订单的收尾归档,未支付订单必须先收款/结账。
	if payStatus != 1 {
		return fmt.Errorf("订单未支付，请先收款或结账")
	}
	tableID, err := dao.GetOrderTable(orderID)
	if err != nil {
		return fmt.Errorf("订单不存在")
	}
	err = store.WithTx(func(tx *sql.Tx) error {
		n, err := dao.FinishOrderTx(tx, orderID, operator, curStatus)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return dao.SetTableStatusTx(tx, tableID, 0)
	})
	if err != nil {
		return err
	}
	// 订单归档后催菜诉求已无意义,自动消解。
	dao.HandleUrgesByOrder(orderID, operator)
	return nil
}

// CancelOrder 取消未支付订单并释放桌台,返回原始取消原因。
func CancelOrder(orderID int, reason, operator string) (string, error) {
	curStatus, payStatus, err := dao.GetOrderState(orderID)
	if err != nil {
		return "", fmt.Errorf("订单不存在")
	}
	if !CanTransition(curStatus, OrderStatusCanceled) {
		return "", fmt.Errorf("当前订单状态不可取消")
	}
	if payStatus == 1 {
		return "", fmt.Errorf("已支付订单不能直接取消，请先处理退款")
	}
	tableID, err := dao.GetOrderTable(orderID)
	if err != nil {
		return "", fmt.Errorf("订单不存在")
	}
	err = store.WithTx(func(tx *sql.Tx) error {
		// 带守卫:防止「取消」与「收款/完成」并发时同时生效。
		n, err := dao.CancelOrderTx(tx, orderID, reason, operator, curStatus)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return dao.SetTableStatusTx(tx, tableID, 0)
	})
	if err != nil {
		return "", err
	}
	// 订单已作废,未处理的催菜一并消解。
	dao.HandleUrgesByOrder(orderID, operator)
	return reason, nil
}

// EditOrderOutcome 改单结果,供 handler 组装审计摘要。
type EditOrderOutcome struct {
	OldTotalCents int64   // 改单前应收金额(分)
	Total         float64 // 改单后应收金额(元)
	PersonCount   int     // 归一化后的用餐人数
	ItemCount     int     // 重建后的明细条数
}

// EditOrder 改单:以数据库为准校验并重建明细,重算金额后原子更新订单与明细。
func EditOrder(orderID, personCount int, orderRemark string, items []dto.OrderItem, operator string) (EditOrderOutcome, error) {
	curStatus, payStatus, err := dao.GetOrderState(orderID)
	if err != nil {
		return EditOrderOutcome{}, fmt.Errorf("订单不存在")
	}
	if !ActiveStatus(curStatus) {
		return EditOrderOutcome{}, fmt.Errorf("当前订单状态不可改单")
	}
	if payStatus == 1 {
		return EditOrderOutcome{}, fmt.Errorf("已支付订单不能改单")
	}
	if len(items) == 0 {
		return EditOrderOutcome{}, fmt.Errorf("订单明细不能为空")
	}
	if personCount < 1 {
		personCount = 1
	}
	resolved, err := ResolveOrderItems(items)
	if err != nil {
		return EditOrderOutcome{}, err
	}
	oldTotal := dao.GetOrderTotal(orderID)
	dishAmount, seatFee, discount, total := RecalcAmount(personCount, resolved)

	poItems := make([]po.OrderItem, 0, len(resolved))
	for _, it := range resolved {
		poItems = append(poItems, it.ToPO())
	}

	err = store.WithTx(func(tx *sql.Tx) error {
		// 先对订单行取写锁(自赋值 no-op):改单是「UPDATE tb_order → DELETE 明细重建」,
		// 若不锁行,与加菜(读明细→插新行→覆盖金额)并发时,加菜刚插入的明细可能被本事务
		// DELETE 掉而金额仍含它,或本事务写好的金额被加菜用旧明细算出的金额覆盖,产生坏账。
		// 锁行后同一订单的改单/加菜/收款/取消在行上串行化(两库语义见 dao.TouchOrderTx)。
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		n, err := dao.UpdateOrderForEditTx(tx, orderID, personCount, orderRemark,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(total), operator)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return dao.ReplaceOrderItemsTx(tx, orderID, poItems)
	})
	if err != nil {
		return EditOrderOutcome{}, fmt.Errorf("改单失败: %w", err)
	}
	return EditOrderOutcome{
		OldTotalCents: oldTotal,
		Total:         total,
		PersonCount:   personCount,
		ItemCount:     len(resolved),
	}, nil
}

// LoadOrderForPrint 读取订单,供结账小票打印使用。
// 打印失败不阻断主流程,故查询失败时返回 false 而非 error。
func LoadOrderForPrint(orderID int) (po.Order, bool) {
	o, err := dao.GetOrderByID(orderID)
	if err != nil {
		return po.Order{}, false
	}
	return o, true
}
