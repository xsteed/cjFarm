package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
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
var ErrActiveOrderDuplicate = errors.New("该桌台已有未结账的订单，请扫描桌上的新二维码进入订单页继续点菜（或联系服务员）")

// errDuplicateRequest 幂等键已处理过:同一 clientRequestId 的重复请求(双击/超时重试)
// 直接返回首次结果,不再重复下单/加菜。仅在事务内部使用,不对外透出。
var errDuplicateRequest = errors.New("重复请求")

// ErrTableUnavailable 桌台被店员人工停用/预约时,拒绝顾客新点餐。
// 与 ErrActiveOrderDuplicate 一样是面向顾客的业务提示,应原样透出(提示联系店员)。
var ErrTableUnavailable = errors.New("该桌台暂不可用或已被预订，如需用餐请联系店员")

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
func CustomerTable(ref string) (dto.Table, *dto.Order, []dto.Order, error) {
	t, byID, err := dao.GetTableByRef(ref)
	if err != nil {
		return dto.Table{}, nil, nil, err
	}
	// 兜底补发稳定码,保证管理端始终能拿到可印制的码值。
	if t.TableCode == "" {
		if code, err := store.EnsureTableCode(t.TableID); err == nil {
			t.TableCode = code
		}
	}

	// 该桌「未结账」的订单(付款前,1/2/3 且 pay_status=0):顾客再次扫码时直接进入订单页,
	// 可继续加菜或付款;已付款但出餐未完成的订单不算未结账,顾客可开新单。
	// 纯数字 table_id(老二维码)可被遍历枚举,不能作为订单能力凭据 —— 该路径只返回桌台基础信息,
	// 不再附带订单,避免攻击者遍历 1..N 批量拉取他人订单;桌台码(8 位随机码)才携带 currentOrder。
	var currentOrder *dto.Order
	servingOrders := make([]dto.Order, 0)
	if !byID {
		// 跨日自动清台开启时,上一营业日的遗留单不再返回给新客人 —— 店家漏点收款
		// 时它会把上一桌的账单直接摊到这一桌头上。开关关闭时为空串,行为与历史一致。
		guard := StaleGuardBizDate()
		if o, ok := dao.GetUnpaidOrderByTable(t.TableID, guard); ok {
			order := dto.FromOrderCustomer(o, orderItemsFromPO(dao.LoadOrderItems(o.OrderID)), false)
			currentOrder = &order
		}
		// 已付款但出餐未完成的订单:顾客据此查看出餐进度。
		// 明细批量 IN 查询加载(与 CustomerMenu 同款),避免一桌多笔订单时
		// 查询数随订单数线性增长(扫码是匿名公开接口,放大面最大)。
		serving := dao.ListServingOrdersByTable(t.TableID, guard)
		if len(serving) > 0 {
			ids := make([]int, 0, len(serving))
			for _, o := range serving {
				ids = append(ids, o.OrderID)
			}
			itemsByOrder, err := dao.LoadOrderItemsByOrderIDs(ids)
			if err != nil {
				return dto.Table{}, nil, nil, err
			}
			for _, o := range serving {
				servingOrders = append(servingOrders,
					dto.FromOrderCustomer(o, orderItemsFromPO(itemsByOrder[o.OrderID]), false))
			}
		}
	}
	return dto.FromTable(t), currentOrder, servingOrders, nil
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
	specMap, err := dao.LoadSpecsByDishIDs(ids)
	if err != nil {
		return nil, err
	}
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
		"shop_slogan":         settings["shop_slogan"],
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
		// 小程序内支付(JSAPI)开关:小程序结账页据此决定是否出现「微信支付」按钮。
		// fail-closed:只有明确为 "1" 才下发 "1",避免「按钮出现了却调不起收银台」。
		"wxpay_jsapi_enabled": flag("wxpay_jsapi_enabled"),
		// 先付/后付模式:顾客端据此决定「下单后是否直达支付页/是否提示先付款」
		"pay_first_enabled": flag("pay_first_enabled"),
		// 会员功能开关:管理端独立总闸(「营业设置 → 会员功能」),不再由
		// AppSecret 等登录能力推导 —— 显隐归商家运营决定,与技术配置解耦
		// (闸口见 memberSwitchOn,登录与静默重登同受它约束)。
		"member_enabled": memberEnabledFlag(),
		// 储值自助充值开关(默认 0):只管「能否自助充值」,不影响已有余额的消费与退款。
		"wallet_recharge_enabled": flag("wallet_recharge_enabled"),
	}
}

// memberEnabledFlag 会员功能开关的归一化("1"/"0"):「默认关」语义(fail-closed),
// 与 memberSwitchOn 同一口径 —— 只有库里明确 "1" 才下发 "1"。
func memberEnabledFlag() string {
	if memberSwitchOn() {
		return "1"
	}
	return "0"
}

// CustomerCreateOrderResult 顾客首次下单的结果,供 handler 组装响应与触发打印。
type CustomerCreateOrderResult struct {
	OrderID        int64
	OrderNo        string
	PickupNo       int // 当日取餐号(顾客口头报号用),0 表示未分配
	DishAmount     float64
	SeatFee        float64
	DiscountAmount float64
	TotalAmount    float64
	Items          []po.OrderItem // 本次下单明细(打印用)
	Duplicated     bool           // 幂等命中:重复请求返回的首次结果,无需再打印
}

// maxPersonCount 用餐人数上限。茶位费按人数计,一次误填就会算出巨额应收;
// 下单/改人数/入座共享记忆三处共用同一上限,避免口径漂移。
const maxPersonCount = 100

// CustomerCreateOrder 顾客首次下单:校验参数、以数据库为准重建明细、占用桌台、
// 在同一桌台并发下单校验后原子写入订单与明细。
// clientRequestID 为可选幂等键:同一键的重复请求(双击/超时重试)返回首次结果。
// customerID 为下单会员的归属(0 = 匿名),只认服务端从顾客令牌解析出的值。
func CustomerCreateOrder(customerID, tableID, personCount int, orderRemark string, items []dto.OrderItem, clientRequestIDs ...string) (CustomerCreateOrderResult, error) {
	clientRequestID := ""
	if len(clientRequestIDs) > 0 {
		clientRequestID = clientRequestIDs[0]
	}
	if tableID == 0 || len(items) == 0 {
		return CustomerCreateOrderResult{}, errors.New("请选择桌台并点选菜品")
	}
	// order_remark 列宽 500:顾客端自由文本不截断会在 MySQL 严格模式下
	// 让整个下单事务失败(前端只看到「系统繁忙」)。
	orderRemark = truncateRunes(strings.TrimSpace(orderRemark), orderRemarkMaxLen)
	if personCount < 1 {
		personCount = 1
	}
	if personCount > maxPersonCount {
		return CustomerCreateOrderResult{}, errors.New("用餐人数超出限制")
	}
	// 校验桌台存在且未删除,并同时取到人工状态:
	// 「桌台存在」与「人工状态可用」是对同一行的两个判断,一次读齐,不做两遍查询。
	table, err := dao.GetTableByID(tableID)
	if err != nil {
		return CustomerCreateOrderResult{}, errors.New("桌台不存在")
	}
	tableNo, tableName := table.TableNo, table.TableName
	// 人工停用/预约的桌台拒绝新点餐(事务内 OccupyTableTx 还会再兜底校验一次,防并发漏判)。
	if table.LockStatus != po.TableLockNormal {
		return CustomerCreateOrderResult{}, ErrTableUnavailable
	}

	// 安全:赠送(is_gift)是商家改单行为,顾客端一律强制清零后再计价,
	// 否则任意人可把菜品标成赠送实现零元下单(见 stripGiftFlagsForCustomer)。
	stripGiftFlagsForCustomer(items)
	resolved, err := ResolveOrderItems(items)
	if err != nil {
		return CustomerCreateOrderResult{}, err
	}
	dishAmount, seatFee, discount, giftAmount, total := RecalcAmount(personCount, resolved)

	poItems := make([]po.OrderItem, 0, len(resolved))
	for _, it := range resolved {
		p := it.ToPO()
		p.ClientRequestID = clientRequestID
		poItems = append(poItems, p)
	}

	orderNo := GenOrderNo()
	var orderID int64
	var existingOrderID int
	var pickup int
	// 营业日的两个派生值都在事务外取好。两条理由:
	//   1. 它们都要读 tb_config —— 事务已经持有 tb_table 的写锁,事务内再多一次
	//      跨表 SELECT 会拉长持锁时间,并发下单时更容易撞上 SQLITE_BUSY;
	//   2. 同一单里「隔离口径」与「写进订单的营业日」必须来自同一次读,
	//      否则管理员恰好在这一刻改了切换时刻,两边就会各算各的。
	// 营业日不是自然日:切换时刻见 biz_date_cutoff_hour。它同时决定取餐号
	// 在哪一刻重置回 1 —— 晚市跨过午夜时,号不该在 00:00 突然跳回 1。
	guard := StaleGuardBizDate()
	bizDate := CurrentBizDate()
	err = store.WithTx(func(tx *sql.Tx) error {
		// 先写桌台行(置占用)再查进行中订单:这条 UPDATE 在 MySQL/SQLite 上都会
		// 先取到该行的写锁,把「同一桌台的并发下单」在本行上串行化,
		// 使下面的「已有进行中订单」检查不再有 check-then-act 竞态窗口。
		n, err := dao.OccupyTableTx(tx, tableID)
		if err != nil {
			if errors.Is(err, dao.ErrTableLocked) {
				return ErrTableUnavailable
			}
			return err
		}
		if n == 0 {
			return errors.New("桌台不存在")
		}
		// 幂等:同一 clientRequestId 只处理一次,重复请求返回首次结果(不重复下单)。
		if clientRequestID != "" {
			if id, ok := dao.GetOrderIDByRequestIDTx(tx, clientRequestID); ok {
				existingOrderID = id
				return errDuplicateRequest
			}
		}
		// 一桌同一时间最多一笔「未结账」订单:重复下单(双击/并发提交/重复扫码)
		// 应走加菜接口,而不是再开一单;已付款但出餐未完成的订单不阻断新单
		// (顾客付款后本单即结束,再点即为新的一单)。
		unpaid, err := dao.CountUnpaidOrdersTx(tx, tableID, guard)
		if err != nil {
			return err
		}
		if unpaid > 0 {
			return ErrActiveOrderDuplicate
		}
		// 取餐号必须在事务内取:UPDATE 计数行的锁把并发下单串行化,
		// 且其自增随事务回滚,不会出现「号被占用却没有对应订单」的空洞。
		pickupNo, err := dao.NextPickupNoTx(tx, bizDate)
		if err != nil {
			return err
		}
		orderID, err = dao.InsertOrderTx(tx, orderNo, bizDate, pickupNo, tableID, personCount, customerID, tableNo, tableName, orderRemark,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(giftAmount), po.ToCents(total))
		if err != nil {
			return err
		}
		pickup = pickupNo
		for _, it := range poItems {
			// 先扣库存再写明细：扣失败（并发售罄）整个事务回滚，明细也不会留下。
			if err := dao.DecrementSpecStockTx(tx, it.SpecID, it.Quantity); err != nil {
				return err
			}
			if err := dao.InsertOrderItemTx(tx, orderID, it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDuplicateRequest) {
			return customerCreateOrderFromExistingID(existingOrderID)
		}
		if errors.Is(err, ErrActiveOrderDuplicate) {
			return CustomerCreateOrderResult{}, err
		}
		if errors.Is(err, ErrTableUnavailable) {
			return CustomerCreateOrderResult{}, err
		}
		if errors.Is(err, dao.ErrSoldOut) {
			return CustomerCreateOrderResult{}, errors.New("部分菜品已售罄，请返回菜单重新选择")
		}
		return CustomerCreateOrderResult{}, fmt.Errorf("下单失败: %w", err)
	}

	// 幂等命中走上面的 errDuplicateRequest 分支提前返回,走到这里的一定是「本次真实插入」,
	// 据此维护会员卡展示列,不会重复计数。
	NotifyCustomerOrder(customerID)
	// 人数已固化进订单：清掉「入座选人数」的桌级共享值（见 table_person.go），
	// 避免残留的预填默认被后续进桌的客人沿用。
	ClearTablePendingPerson(tableID)

	return CustomerCreateOrderResult{
		OrderID:        orderID,
		OrderNo:        orderNo,
		PickupNo:       pickup,
		DishAmount:     dishAmount,
		SeatFee:        seatFee,
		DiscountAmount: discount,
		TotalAmount:    total,
		Items:          poItems,
	}, nil
}

// customerCreateOrderFromExistingID 幂等命中时按订单 ID 还原首次下单结果(标记 Duplicated,handler 跳过打印)。
func customerCreateOrderFromExistingID(orderID int) (CustomerCreateOrderResult, error) {
	o, err := dao.GetOrderByID(orderID)
	if err != nil {
		return CustomerCreateOrderResult{}, fmt.Errorf("下单失败: %w", err)
	}
	return CustomerCreateOrderResult{
		OrderID:        int64(o.OrderID),
		OrderNo:        o.OrderNo,
		PickupNo:       o.PickupNo,
		DishAmount:     po.ToYuan(o.DishAmount),
		SeatFee:        po.ToYuan(o.SeatFee),
		DiscountAmount: po.ToYuan(o.DiscountAmount),
		TotalAmount:    po.ToYuan(o.TotalAmount),
		Items:          dao.LoadOrderItems(o.OrderID),
		Duplicated:     true,
	}, nil
}

// CustomerOrderByNo 按订单号查询订单(顾客端订单页轮询/小票页使用)。
//
// tableID > 0 时校验订单归属:调用方(扫码进来的桌台)必须证明这个单是自己的 ——
// 单号会出现在小票/照片/群聊里,无鉴权接口不做归属校验等于把全店订单明细
// (菜品/金额/挂账人)敞开给匿名访客遍历。与加菜/催菜同一口径(tableId 一并提交)。
// tableID <= 0 时(老二维码链接未带桌台)退化为仅按单号查询,由顾客端限流
// 中间件兜底(见 main.go 路由上的 NewIPRateLimit)。
func CustomerOrderByNo(orderNo string, tableID int) (dto.Order, error) {
	o, err := dao.GetOrderByNo(orderNo)
	if err != nil {
		return dto.Order{}, errors.New("订单不存在")
	}
	if tableID > 0 && o.TableID != tableID {
		// 与「订单不存在」同一文案:不向调用方泄露「单号存在但不属于你」。
		return dto.Order{}, errors.New("订单不存在或已结束")
	}
	return dto.FromOrderCustomer(o, orderItemsFromPO(dao.LoadOrderItems(o.OrderID)), false), nil
}

// CustomerMyOrderDetail 会员查看自己的某一笔历史订单(跨桌,含明细)。
//
// 与 CustomerOrderByNo 的区别:后者靠 tableId 证明「这单是这张桌的」,而历史订单
// 是**跨桌**的 —— 顾客早就离开那张桌了,只能靠会员身份证明。所以这里不走 tableId,
// 而是把 customer_id 直接写进查询条件。
//
// 「查不到」与「不是你的」返回同一文案:不向调用方泄露某个单号是否存在。
func CustomerMyOrderDetail(customerID int, orderNo string) (dto.Order, error) {
	if customerID <= 0 || orderNo == "" {
		return dto.Order{}, errors.New("订单不存在")
	}
	o, err := dao.GetOrderByNoAndCustomer(orderNo, customerID)
	if err != nil {
		return dto.Order{}, errors.New("订单不存在")
	}
	return dto.FromOrderCustomer(o, orderItemsFromPO(dao.LoadOrderItems(o.OrderID)), false), nil
}

// CustomerUpdatePersonResult 顾客改就餐人数的结果。
//
// Changed=false 表示人数与库中现值一致(重复提交 / 连点),调用方无需关旧收款码。
type CustomerUpdatePersonResult struct {
	Order   dto.Order
	Changed bool
}

// CustomerUpdatePerson 顾客在订单页修改就餐人数(不改菜品)。
//
// 为什么单独开一个接口:人数直接决定茶位费,「人没到齐 / 有人先走」时需要能改;
// 而加菜接口的语义是「追加菜品 + 顺带更新人数」,它强制 items 非空(见
// CustomerAppendOrder)。拿它只改人数,要么被参数校验拒掉,要么得把现有明细原样
// 回传 —— 后者会重复扣库存、重复出厨单(整单被再做一遍),绝对不能这么用。
//
// 约束:仅「进行中且未支付」的订单可改。改人数就是在改应收金额,已支付的单改了
// 会与实收金额对不上,只能走退款/改单的人工流程。
func CustomerUpdatePerson(tableID int, orderNo string, personCount int) (CustomerUpdatePersonResult, error) {
	if tableID == 0 || orderNo == "" {
		return CustomerUpdatePersonResult{}, errors.New("参数错误")
	}
	if personCount < 1 || personCount > maxPersonCount {
		// 上限与加菜/下单接口保持一致(maxPersonCount)
		return CustomerUpdatePersonResult{}, errors.New("用餐人数需在 1~100 之间")
	}

	o, err := dao.GetOrderAppendInfo(orderNo)
	if err != nil {
		return CustomerUpdatePersonResult{}, errors.New("订单不存在")
	}
	orderID := o.OrderID
	// 归属校验:与加菜同一口径 —— 单号可被猜测/泄露,只有「订单所属桌台 ==
	// 请求方声称的桌台」才允许改动这张账单。
	if o.TableID != tableID {
		return CustomerUpdatePersonResult{}, errors.New("订单不属于当前桌台")
	}
	if !ActiveStatus(o.OrderStatus) {
		return CustomerUpdatePersonResult{}, errors.New("当前订单状态不可修改人数")
	}
	if o.PayStatus == 1 {
		return CustomerUpdatePersonResult{}, errors.New("订单已支付，不可修改人数")
	}

	changed := false
	// 事务内并发校验失败时保留原错误文案:包成「修改人数失败: ...」会被
	// customerFail 当成底层错误收敛成「系统繁忙」,顾客反而不知道该刷新重试。
	var bizErr error
	err = store.WithTx(func(tx *sql.Tx) error {
		// 与加菜/改单/收款/取消同一把行锁:金额必须基于事务内的最新明细重算,
		// 否则并发改单时会出现「人数是新的、明细是旧的」的错账。
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		cur, err := dao.GetOrderStateTx(tx, orderID)
		if err != nil {
			return err
		}
		if !ActiveStatus(cur.OrderStatus) || cur.PayStatus == 1 {
			bizErr = errors.New("订单状态已变更，请刷新后重试")
			return bizErr
		}
		// 人数没变:不写库(避免无谓的 update_time 变化与关单动作)
		if cur.PersonCount == personCount {
			return nil
		}
		items, err := dao.LoadOrderItemsTx(tx, orderID)
		if err != nil {
			return err
		}
		dtoItems := make([]dto.OrderItem, 0, len(items))
		for _, it := range items {
			dtoItems = append(dtoItems, dto.FromOrderItem(it))
		}
		// 只重算金额,不碰明细:改人数不是加菜 —— 不产生新批次、不扣库存、不出厨单
		dishAmount, seatFee, discount, giftAmount, total := RecalcAmount(personCount, dtoItems)
		if err := dao.UpdateOrderAmountAndPersonTx(tx, orderID, personCount,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(giftAmount), po.ToCents(total)); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		if bizErr != nil {
			return CustomerUpdatePersonResult{}, bizErr
		}
		return CustomerUpdatePersonResult{}, fmt.Errorf("修改人数失败: %w", err)
	}

	// 返回最新订单:顾客端要按新金额刷新账单。价格口径只保留后端这一份,
	// 不让前端「自己加一下茶位费」—— 两端各算一次必然漂移。
	order, err := CustomerOrderByNo(orderNo, tableID)
	if err != nil {
		return CustomerUpdatePersonResult{}, err
	}
	return CustomerUpdatePersonResult{Order: order, Changed: changed}, nil
}

// CustomerAppendOrderResult 顾客加菜的结果,供 handler 组装响应与触发打印。
type CustomerAppendOrderResult struct {
	OrderID     int
	OrderNo     string
	DishAmount  float64
	SeatFee     float64
	TotalAmount float64
	Items       []po.OrderItem // 本次新增明细(打印用)
	Duplicated  bool           // 幂等命中:重复请求返回的首次结果,无需再打印
}

// CustomerAppendOrder 顾客加菜:向该桌进行中且未支付的订单追加菜品。
// 与首次下单不同,加菜复用已有订单(合并明细、重算金额),避免同一桌产生多笔未结账单。
// personCount 允许随加菜一并修改用餐人数:<=0 表示沿用订单现值。
// clientRequestID 为可选幂等键:同一键的重复请求(双击/超时重试)返回首次结果。
func CustomerAppendOrder(tableID int, orderNo string, items []dto.OrderItem, personCount int, clientRequestIDs ...string) (CustomerAppendOrderResult, error) {
	clientRequestID := ""
	if len(clientRequestIDs) > 0 {
		clientRequestID = clientRequestIDs[0]
	}
	if tableID == 0 || orderNo == "" || len(items) == 0 {
		return CustomerAppendOrderResult{}, errors.New("参数错误")
	}
	if personCount > maxPersonCount {
		return CustomerAppendOrderResult{}, errors.New("用餐人数超出限制")
	}

	o, err := dao.GetOrderAppendInfo(orderNo)
	if err != nil {
		return CustomerAppendOrderResult{}, errors.New("订单不存在")
	}
	orderID, orderStatus, payStatus := o.OrderID, o.OrderStatus, o.PayStatus
	// 归属校验:orderNo 可被猜测/泄露,不能单独作为加菜凭据;
	// 只有「订单所属桌台 == 请求方声称的桌台」才允许改动他人账单。
	if o.TableID != tableID {
		return CustomerAppendOrderResult{}, errors.New("订单不属于当前桌台")
	}
	if !ActiveStatus(orderStatus) {
		return CustomerAppendOrderResult{}, errors.New("当前订单状态不可加菜")
	}
	if payStatus == 1 {
		return CustomerAppendOrderResult{}, errors.New("订单已支付，不可加菜")
	}

	// 安全:同上,顾客加菜同样不接受赠送标记(只清新增项,不影响本单已有的商家赠送菜)。
	stripGiftFlagsForCustomer(items)
	newItems, err := ResolveOrderItems(items)
	if err != nil {
		return CustomerAppendOrderResult{}, err
	}

	var dishAmount, seatFee, discount, giftAmount, total float64
	var nextBatch int
	err = store.WithTx(func(tx *sql.Tx) error {
		// 先对订单行取写锁(自赋值 no-op):加菜是「读明细→插新行→覆盖金额」,
		// 若不锁行,与改单(DELETE 明细重建)/收款/取消并发时,双方基于各自的旧明细
		// 重算金额,后写者覆盖先写者,出现「明细多了金额没加」或「明细少了金额没减」的坏账。
		// 锁行后同一订单的加菜/改单/收款/取消在行上串行化(两库语义见 dao.TouchOrderTx)。
		if err := dao.TouchOrderTx(tx, orderID); err != nil {
			return err
		}
		// 幂等:同一 clientRequestId 只处理一次,重复请求返回首次结果(不重复加菜)。
		if clientRequestID != "" {
			dup, err := dao.HasOrderItemRequestTx(tx, orderID, clientRequestID)
			if err != nil {
				return err
			}
			if dup {
				return errDuplicateRequest
			}
		}
		// 事务内重读状态:并发收款/改单/取消时,基于事务外旧快照重算金额会互相覆盖,
		// 出现「明细多了、金额没加上」的坏账。
		cur, err := dao.GetOrderStateTx(tx, orderID)
		if err != nil {
			return err
		}
		if !ActiveStatus(cur.OrderStatus) || cur.PayStatus == 1 {
			return errors.New("订单状态已变更，请刷新后重试")
		}
		// 用餐人数可随加菜一并更新:请求未传(<=0)则沿用订单现值。
		effectivePerson := cur.PersonCount
		if personCount > 0 {
			effectivePerson = personCount
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
		// 累计明细条数上限:ResolveOrderItems 只限制「本次提交」的条数,若不加总上限,
		// 可反复加菜把单笔订单明细无限撑大,拖慢金额重算与整单重写(ReplaceOrderItemsTx)。
		if len(allItems) > maxOrderItems {
			return fmt.Errorf("单笔订单明细过多(最多 %d 项),请分单处理", maxOrderItems)
		}
		dishAmount, seatFee, discount, giftAmount, total = RecalcAmount(effectivePerson, allItems)
		// 加菜批次:首次下单批次为 1,每次加菜在上次最大批次基础上 +1。
		nextBatch = 1
		for _, it := range existing {
			if it.BatchNo >= nextBatch {
				nextBatch = it.BatchNo + 1
			}
		}
		for _, it := range newItems {
			p := it.ToPO()
			p.BatchNo = nextBatch
			p.ClientRequestID = clientRequestID
			if err := dao.DecrementSpecStockTx(tx, p.SpecID, p.Quantity); err != nil {
				return err
			}
			if err := dao.InsertOrderItemTx(tx, int64(orderID), p); err != nil {
				return err
			}
		}
		if err := dao.UpdateOrderAmountAndPersonTx(tx, orderID, effectivePerson,
			po.ToCents(dishAmount), po.ToCents(seatFee), po.ToCents(discount), po.ToCents(giftAmount), po.ToCents(total)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDuplicateRequest) {
			return customerAppendFromExisting(orderNo)
		}
		if errors.Is(err, dao.ErrSoldOut) {
			return CustomerAppendOrderResult{}, errors.New("部分菜品已售罄，请返回菜单重新选择")
		}
		return CustomerAppendOrderResult{}, fmt.Errorf("加菜失败: %w", err)
	}

	poItems := make([]po.OrderItem, 0, len(newItems))
	for _, it := range newItems {
		p := it.ToPO()
		p.BatchNo = nextBatch
		p.ClientRequestID = clientRequestID
		poItems = append(poItems, p)
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

// customerAppendFromExisting 幂等命中时按订单号还原加菜后的订单金额(标记 Duplicated,handler 跳过打印)。
func customerAppendFromExisting(orderNo string) (CustomerAppendOrderResult, error) {
	o, err := dao.GetOrderByNo(orderNo)
	if err != nil {
		return CustomerAppendOrderResult{}, fmt.Errorf("加菜失败: %w", err)
	}
	return CustomerAppendOrderResult{
		OrderID:     o.OrderID,
		OrderNo:     o.OrderNo,
		DishAmount:  po.ToYuan(o.DishAmount),
		SeatFee:     po.ToYuan(o.SeatFee),
		TotalAmount: po.ToYuan(o.TotalAmount),
		Items:       nil,
		Duplicated:  true,
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

	o, err := dao.GetUrgeTarget(orderNo)
	if err != nil {
		return CustomerUrgeOrderResult{}, errors.New("订单不存在")
	}
	orderID, tableNo, tableName := o.OrderID, o.TableNo, o.TableName
	// 归属校验:与加菜同理,orderNo 不能单独作为催单凭据,须与请求桌台匹配。
	if o.TableID != tableID {
		return CustomerUrgeOrderResult{}, errors.New("订单不属于当前桌台")
	}
	if !ActiveStatus(o.OrderStatus) {
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
		return dao.InsertUrgeTx(tx, orderID, orderNo, o.TableID, tableNo, tableName,
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
