package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============ 顾客端用例 ============
//
// 本文件承载顾客端点餐流程的业务编排:扫码进入桌台、菜单浏览、下单、加菜、
// 催菜与订单查询。数据访问统一走 dao,事务用 store.WithTx,不依赖 handler/print/pay。
// 金额与明细校验复用 service.go 的 RecalcAmount/ResolveOrderItems 等纯函数,
// 状态机校验复用 ActiveStatus,读取订单供打印复用 LoadOrderForPrint。

// ErrActiveOrderDuplicate 一桌已存在进行中订单时拒绝重复开单。
// 用哨兵错误而不是「下单失败: %w」包装:这是明确的业务提示,顾客端应原样看到
// (继续点菜请走加菜接口),而不是被 handler 统一替换成「系统繁忙」。
//
// 文案注意引导扫新码:老二维码(纯数字桌台 ID)路径出于安全考虑不返回订单号,
// 桌台已有进行中订单时顾客既无法加菜也开不了新单 —— 唯一出路是扫桌上的新
// 8 位随机码(管理端桌台详情可打印),文案必须把这个出口讲清楚。
var ErrActiveOrderDuplicate = errors.New("该桌台已有进行中的订单，请扫描桌上的新二维码进入订单页继续点菜（或联系服务员）")

// orderItemsFromPO 将持久化订单明细批量转为 API 出参。
func orderItemsFromPO(poItems []po.OrderItem) []dto.OrderItem {
	items := make([]dto.OrderItem, 0, len(poItems))
	for _, it := range poItems {
		items = append(items, dto.FromOrderItem(it))
	}
	return items
}

// CustomerTable 顾客扫码进入桌台:按「桌台稳定码/数字桌台ID」解析桌台,
// 兜底补发稳定码,并返回该桌当前进行中的订单(无进行中订单时 currentOrder 为 nil)。
func CustomerTable(ref string) (dto.Table, *dto.Order, error) {
	t, byID, err := dao.GetTableByRef(ref)
	if err != nil {
		return dto.Table{}, nil, err
	}
	// 兜底补发稳定码,保证管理端始终能拿到可印制的码值。
	if t.TableCode == "" {
		if code, err := store.EnsureTableCode(t.TableID); err == nil {
			t.TableCode = code
		}
	}

	// 该桌进行中的订单(1已下单/2制作中/3已上齐):顾客再次扫码时直接进入订单状态页。
	// 纯数字 table_id(老二维码)可被遍历枚举,不能作为订单能力凭据 —— 该路径只返回桌台基础信息,
	// 不再附带进行中订单,避免攻击者遍历 1..N 批量拉取他人订单;桌台码(8 位随机码)才携带 currentOrder。
	var currentOrder *dto.Order
	if !byID && t.Status == 1 {
		if o, ok := dao.GetActiveOrderByTable(t.TableID); ok {
			order := dto.FromOrderCustomer(o, orderItemsFromPO(dao.LoadOrderItems(o.OrderID)), false)
			currentOrder = &order
		}
	}
	return dto.FromTable(t), currentOrder, nil
}

// CustomerMenuCategory 顾客端菜单按分类组织的出参结构。
type CustomerMenuCategory struct {
	CategoryID   int        `json:"categoryId"`
	CategoryName string     `json:"categoryName"`
	Dishes       []dto.Dish `json:"dishes"`
}

// CustomerMenu 返回顾客端菜单:分类保持 sort_order 顺序,菜品按分类组织。
func CustomerMenu() ([]CustomerMenuCategory, error) {
	cats, err := dao.ListCategories()
	if err != nil {
		return nil, err
	}

	dishes, err := dao.ListEnabledDishes()
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(dishes))
	for _, d := range dishes {
		ids = append(ids, d.DishID)
	}

	// 批量加载规格
	specMap := dao.LoadSpecsByDishIDs(ids)
	dishOut := make([]dto.Dish, 0, len(dishes))
	for _, d := range dishes {
		specs := make([]dto.Spec, 0, len(specMap[d.DishID]))
		for _, s := range specMap[d.DishID] {
			specs = append(specs, dto.FromSpec(s))
		}
		dishOut = append(dishOut, dto.FromDish(d.Dish, d.CategoryName, specs))
	}

	// 按分类组织(保持分类 sort_order 顺序)
	byCat := map[int]*CustomerMenuCategory{}
	for i := range cats {
		byCat[cats[i].CategoryID] = &CustomerMenuCategory{
			CategoryID:   cats[i].CategoryID,
			CategoryName: cats[i].CategoryName,
			Dishes:       []dto.Dish{},
		}
	}
	for _, d := range dishOut {
		if cw, ok := byCat[d.CategoryID]; ok {
			cw.Dishes = append(cw.Dishes, d)
		}
	}
	out := make([]CustomerMenuCategory, 0, len(cats))
	for _, c := range cats {
		out = append(out, *byCat[c.CategoryID])
	}
	return out, nil
}

// CustomerPayQr 返回顾客端收款码配置。
func CustomerPayQr() map[string]string {
	return map[string]string{
		"pay_qr_wx":  dao.GetSetting("pay_qr_wx"),
		"pay_qr_ali": dao.GetSetting("pay_qr_ali"),
	}
}

// CustomerSettings 返回顾客端所需的公开配置(无需登录)。
//
// 一次性 LoadSettings 全表读入后内存映射,避免旧实现逐键 GetSetting
// (约 10 次 DB 查询)。开关项的归一化语义与 SettingFlag 保持一致:仅 "1" 视为开启。
func CustomerSettings() map[string]string {
	settings := dao.LoadSettings()
	flag := func(key string) string {
		if settings[key] == "1" {
			return "1"
		}
		return "0"
	}
	return map[string]string{
		"shop_name":           settings["shop_name"],
		"seat_fee_enabled":    flag("seat_fee_enabled"),
		"seat_fee":            settings["seat_fee"],
		"promotion_enabled":   flag("promotion_enabled"),
		"promotion_threshold": settings["promotion_threshold"],
		"promotion_discount":  settings["promotion_discount"],
		"pay_qr_wx":           settings["pay_qr_wx"],
		"pay_qr_ali":          settings["pay_qr_ali"],
		// 在线支付开关(顾客端据此决定是否展示在线支付入口)
		"wxpay_enabled":  flag("wxpay_enabled"),
		"alipay_enabled": flag("alipay_enabled"),
	}
}

// CustomerCreateOrderResult 顾客首次下单的结果,供 handler 组装响应与触发打印。
type CustomerCreateOrderResult struct {
	OrderID        int64
	OrderNo        string
	DishAmount     float64
	SeatFee        float64
	DiscountAmount float64
	TotalAmount    float64
	Items          []po.OrderItem // 本次下单明细(打印用)
}

// CustomerCreateOrder 顾客首次下单:校验参数、以数据库为准重建明细、占用桌台、
// 在同一桌台并发下单校验后原子写入订单与明细。
func CustomerCreateOrder(tableID, personCount int, orderRemark string, items []dto.OrderItem) (CustomerCreateOrderResult, error) {
	if tableID == 0 || len(items) == 0 {
		return CustomerCreateOrderResult{}, errors.New("请选择桌台并点选菜品")
	}
	if personCount < 1 {
		personCount = 1
	}
	if personCount > 100 {
		return CustomerCreateOrderResult{}, errors.New("用餐人数超出限制")
	}
	// 校验桌台存在且未删除
	tableNo, tableName, err := dao.GetTableNoName(tableID)
	if err != nil {
		return CustomerCreateOrderResult{}, errors.New("桌台不存在")
	}

	resolved, err := ResolveOrderItems(items)
	if err != nil {
		return CustomerCreateOrderResult{}, err
	}
	dishAmount, seatFee, discount, total := RecalcAmount(personCount, resolved)

	poItems := make([]po.OrderItem, 0, len(resolved))
	for _, it := range resolved {
		poItems = append(poItems, it.ToPO())
	}

	orderNo := GenOrderNo()
	var orderID int64
	err = store.WithTx(func(tx *sql.Tx) error {
		// 先写桌台行(置占用)再查进行中订单:这条 UPDATE 在 MySQL/SQLite 上都会
		// 先取到该行的写锁,把「同一桌台的并发下单」在本行上串行化,
		// 使下面的「已有进行中订单」检查不再有 check-then-act 竞态窗口。
		n, err := dao.OccupyTableTx(tx, tableID)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("桌台不存在")
		}
		// 一桌同一时间只允许一笔进行中订单:重复下单(双击/并发提交/重复扫码)
		// 会造成「看板只显示最新一单、旧单仍可被收款」的资损口径混乱,
		// 继续点菜应走加菜接口。
		active, err := dao.CountActiveOrdersTx(tx, tableID)
		if err != nil {
			return err
		}
		if active > 0 {
			return ErrActiveOrderDuplicate
		}
		orderID, err = dao.InsertOrderTx(tx, orderNo, tableID, personCount, tableNo, tableName, orderRemark,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(total))
		if err != nil {
			return err
		}
		for _, it := range poItems {
			if err := dao.InsertOrderItemTx(tx, orderID, it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrActiveOrderDuplicate) {
			return CustomerCreateOrderResult{}, err
		}
		return CustomerCreateOrderResult{}, fmt.Errorf("下单失败: %w", err)
	}

	return CustomerCreateOrderResult{
		OrderID:        orderID,
		OrderNo:        orderNo,
		DishAmount:     dishAmount,
		SeatFee:        seatFee,
		DiscountAmount: discount,
		TotalAmount:    total,
		Items:          poItems,
	}, nil
}

// CustomerOrderByNo 按订单号查询订单详情(顾客端)。
func CustomerOrderByNo(orderNo string) (dto.Order, error) {
	o, err := dao.GetOrderByNo(orderNo)
	if err != nil {
		return dto.Order{}, errors.New("订单不存在")
	}
	return dto.FromOrderCustomer(o, orderItemsFromPO(dao.LoadOrderItems(o.OrderID)), false), nil
}

// CustomerAppendOrderResult 顾客加菜的结果,供 handler 组装响应与触发打印。
type CustomerAppendOrderResult struct {
	OrderID     int
	OrderNo     string
	DishAmount  float64
	SeatFee     float64
	TotalAmount float64
	Items       []po.OrderItem // 本次新增明细(打印用)
}

// CustomerAppendOrder 顾客加菜:向该桌进行中且未支付的订单追加菜品。
// 与首次下单不同,加菜复用已有订单(合并明细、重算金额),避免同一桌产生多笔订单。
func CustomerAppendOrder(tableID int, orderNo string, items []dto.OrderItem) (CustomerAppendOrderResult, error) {
	if tableID == 0 || orderNo == "" || len(items) == 0 {
		return CustomerAppendOrderResult{}, errors.New("参数错误")
	}

	orderID, orderTableID, _, orderStatus, payStatus, err := dao.GetOrderAppendInfo(orderNo)
	if err != nil {
		return CustomerAppendOrderResult{}, errors.New("订单不存在")
	}
	// 归属校验:orderNo 可被猜测/泄露,不能单独作为加菜凭据;
	// 只有「订单所属桌台 == 请求方声称的桌台」才允许改动他人账单。
	if orderTableID != tableID {
		return CustomerAppendOrderResult{}, errors.New("订单不属于当前桌台")
	}
	if !ActiveStatus(orderStatus) {
		return CustomerAppendOrderResult{}, errors.New("当前订单状态不可加菜")
	}
	if payStatus == 1 {
		return CustomerAppendOrderResult{}, errors.New("订单已支付，不可加菜")
	}

	newItems, err := ResolveOrderItems(items)
	if err != nil {
		return CustomerAppendOrderResult{}, err
	}

	var dishAmount, seatFee, discount, total float64
	err = store.WithTx(func(tx *sql.Tx) error {
		// 先对订单行取写锁(自赋值 no-op):加菜是「读明细→插新行→覆盖金额」,
		// 若不锁行,与改单(DELETE 明细重建)/收款/取消并发时,双方基于各自的旧明细
		// 重算金额,后写者覆盖先写者,出现「明细多了金额没加」或「明细少了金额没减」的坏账。
		// 锁行后同一订单的加菜/改单/收款/取消在行上串行化(两库语义见 dao.TouchOrderTx)。
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		// 事务内重读状态:并发收款/改单/取消时,基于事务外旧快照重算金额会互相覆盖,
		// 出现「明细多了、金额没加上」的坏账。
		curStatus, curPay, curPerson, err := dao.GetOrderStateTx(tx, orderID)
		if err != nil {
			return err
		}
		if !ActiveStatus(curStatus) || curPay == 1 {
			return errors.New("订单状态已变更，请刷新后重试")
		}
		// 合并现有明细与新增明细,按最新人数/明细重算金额。
		// 明细读失败必须终止事务:否则会基于不完整的旧明细重算金额并落库,产生坏账。
		existing, err := dao.LoadOrderItemsTx(tx, orderID)
		if err != nil {
			return err
		}
		allItems := make([]dto.OrderItem, 0, len(existing)+len(newItems))
		for _, it := range existing {
			allItems = append(allItems, dto.FromOrderItem(it))
		}
		allItems = append(allItems, newItems...)
		dishAmount, seatFee, discount, total = RecalcAmount(curPerson, allItems)
		for _, it := range newItems {
			if err := dao.InsertOrderItemTx(tx, int64(orderID), it.ToPO()); err != nil {
				return err
			}
		}
		if err := dao.UpdateOrderAmountTx(tx, orderID,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(total)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return CustomerAppendOrderResult{}, fmt.Errorf("加菜失败: %w", err)
	}

	poItems := make([]po.OrderItem, 0, len(newItems))
	for _, it := range newItems {
		poItems = append(poItems, it.ToPO())
	}
	return CustomerAppendOrderResult{
		OrderID:     orderID,
		OrderNo:     orderNo,
		DishAmount:  dishAmount,
		SeatFee:     seatFee,
		TotalAmount: total,
		Items:       poItems,
	}, nil
}

// CustomerUrgeOrderResult 顾客催菜的结果,供 handler 组装响应。
type CustomerUrgeOrderResult struct {
	OrderID  int
	Cooldown int
}

// UrgeCooldownError 催菜冷却拒绝错误,携带剩余冷却秒数(Remain)。
// handler 识别该类型后把 Remain 放进响应体 data,前端据此展示倒计时,
// 不再依赖解析 msg 文案(文案调整不会破坏前端)。
type UrgeCooldownError struct {
	Remain int
}

func (e *UrgeCooldownError) Error() string {
	if e.Remain > 0 {
		return fmt.Sprintf("已通知后厨，请 %d 秒后再催", e.Remain)
	}
	return "已通知后厨，请稍后再催"
}

// CustomerUrgeOrder 顾客催菜:请求后厨加急处理当前订单。
//
// 约束:
//   - 仅进行中的订单可催(已完成/已取消无意义);
//   - 同一订单 180 秒内只受理一次,防止顾客连点把后厨刷屏,
//     超频时返回剩余等待秒数,前端据此展示倒计时。
//
// 催菜会生成一条待处理记录,商家端订单列表与桌台看板据此打角标。
func CustomerUrgeOrder(tableID int, orderNo string) (CustomerUrgeOrderResult, error) {
	orderNo = strings.TrimSpace(orderNo)
	if tableID == 0 || orderNo == "" {
		return CustomerUrgeOrderResult{}, errors.New("参数错误")
	}

	orderID, orderTableID, orderStatus, tableNo, tableName, err := dao.GetUrgeTarget(orderNo)
	if err != nil {
		return CustomerUrgeOrderResult{}, errors.New("订单不存在")
	}
	// 归属校验:与加菜同理,orderNo 不能单独作为催单凭据,须与请求桌台匹配。
	if orderTableID != tableID {
		return CustomerUrgeOrderResult{}, errors.New("订单不属于当前桌台")
	}
	if !ActiveStatus(orderStatus) {
		return CustomerUrgeOrderResult{}, errors.New("订单已结束，无需催菜")
	}

	// 冷却校验与插入放进同一事务,并对订单行做一次写锁(自赋值 no-op):
	// 「查上次催菜→插入」的间隔正是 check-then-act 竞态窗口,并发连点可各自
	// 通过校验刷出一排催菜;先锁行再校验即可把同一订单的催菜串行化(两库都成立)。
	errCooldown := errors.New("urge cooldown")
	cutoff := time.Now().Add(-po.UrgeCooldownSecond * time.Second).Format(conf.TimeLayout)
	err = store.WithTx(func(tx *sql.Tx) error {
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		recent, err := dao.CountRecentUrgesTx(tx, orderID, cutoff)
		if err != nil {
			return err
		}
		if recent > 0 {
			return errCooldown
		}
		return dao.InsertUrgeTx(tx, orderID, orderNo, orderTableID, tableNo, tableName,
			po.UrgeTypeUrge, po.UrgeStatusPending)
	})
	if err != nil {
		if errors.Is(err, errCooldown) {
			// 冷却中:算出剩余秒数给前端展示倒计时。
			remain := 0
			if last, okLast := dao.LastUrgeTime(orderID); okLast {
				if r := po.UrgeCooldownSecond - int(time.Since(last).Seconds()); r > 0 {
					remain = r
				}
			}
			return CustomerUrgeOrderResult{}, &UrgeCooldownError{Remain: remain}
		}
		return CustomerUrgeOrderResult{}, errors.New("催菜失败，请稍后重试")
	}

	return CustomerUrgeOrderResult{OrderID: orderID, Cooldown: po.UrgeCooldownSecond}, nil
}
