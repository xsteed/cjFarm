package handler

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/print"
	"dining-system/internal/service"
	"dining-system/internal/store"
)

// ============ 顾客端(公开接口) ============

// CustomerTable 顾客扫码进入桌台:路由参数同时接受「桌台稳定码」与「数字桌台ID」。
// 老二维码(/order/1)与新二维码(/order/XXXXXXXX)都可正常打开。
func CustomerTable(c *gin.Context) {
	t, err := store.GetTableByRef(c.Param("id"))
	if err != nil {
		fail(c, "桌台不存在")
		return
	}
	// 兜底补发稳定码,保证管理端始终能拿到可印制的码值。
	if t.TableCode == "" {
		if code, err := store.EnsureTableCode(t.TableID); err == nil {
			t.TableCode = code
		}
	}
	id := t.TableID

	// 该桌进行中的订单(1已下单/2制作中/3已上齐):顾客再次扫码时直接进入订单状态页。
	var currentOrder interface{}
	if t.Status == 1 {
		if o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order
			WHERE table_id=? AND order_status IN (1,2,3) ORDER BY order_id DESC LIMIT 1`, id)); err == nil {
			o.Items = store.LoadOrderItems(o.OrderID)
			currentOrder = o
		}
	}

	ok(c, gin.H{
		"tableId":      t.TableID,
		"tableNo":      t.TableNo,
		"tableName":    t.TableName,
		"tableCode":    t.TableCode,
		"capacity":     t.Capacity,
		"status":       t.Status,
		"currentOrder": currentOrder,
	})
}

func CustomerMenu(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT category_id, category_name, sort_order, del_flag, create_time, update_time
		FROM tb_category WHERE del_flag='0' ORDER BY sort_order, category_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	cats := []model.Category{}
	for rows.Next() {
		var ct model.Category
		rows.Scan(&ct.CategoryID, &ct.CategoryName, &ct.SortOrder, &ct.DelFlag, &ct.CreateTime, &ct.UpdateTime)
		cats = append(cats, ct)
	}
	rows.Close()

	drows, err := store.DB.Query(`SELECT ` + store.DishCols + `
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id
		WHERE d.del_flag='0' AND d.status=1 ORDER BY d.sort_order, d.dish_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	dishes := []model.Dish{}
	ids := []int{}
	for drows.Next() {
		if d, err := store.ScanDish(drows); err == nil {
			dishes = append(dishes, d)
			ids = append(ids, d.DishID)
		}
	}
	drows.Close()

	// 批量加载规格
	specMap := store.LoadSpecsByDishIDs(ids)
	for i := range dishes {
		dishes[i].Specs = specMap[dishes[i].DishID]
	}

	// 按分类组织(保持分类 sort_order 顺序)
	type catWithDishes struct {
		CategoryID   int          `json:"categoryId"`
		CategoryName string       `json:"categoryName"`
		Dishes       []model.Dish `json:"dishes"`
	}
	byCat := map[int]*catWithDishes{}
	for i := range cats {
		byCat[cats[i].CategoryID] = &catWithDishes{CategoryID: cats[i].CategoryID, CategoryName: cats[i].CategoryName, Dishes: []model.Dish{}}
	}
	for _, d := range dishes {
		if cw, ok := byCat[d.CategoryID]; ok {
			cw.Dishes = append(cw.Dishes, d)
		}
	}
	out := []catWithDishes{}
	for _, c := range cats {
		out = append(out, *byCat[c.CategoryID])
	}
	ok(c, out)
}

func CustomerRemarks(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT remark_id, option_name, sort_order, del_flag, create_time, update_time
		FROM tb_remark WHERE del_flag='0' ORDER BY sort_order, remark_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Remark{}
	for rows.Next() {
		var r model.Remark
		rows.Scan(&r.RemarkID, &r.OptionName, &r.SortOrder, &r.DelFlag, &r.CreateTime, &r.UpdateTime)
		list = append(list, r)
	}
	ok(c, list)
}

func CustomerCreateOrder(c *gin.Context) {
	var p struct {
		TableID     int               `json:"tableId"`
		PersonCount int               `json:"personCount"`
		OrderRemark string            `json:"orderRemark"`
		Items       []model.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if p.TableID == 0 || len(p.Items) == 0 {
		fail(c, "请选择桌台并点选菜品")
		return
	}
	if p.PersonCount < 1 {
		p.PersonCount = 1
	}
	if p.PersonCount > 100 {
		fail(c, "用餐人数超出限制")
		return
	}
	// 校验桌台存在且未删除
	var tableNo, tableName string
	if err := store.DB.QueryRow(`SELECT table_no, table_name FROM tb_table WHERE table_id=? AND del_flag='0'`, p.TableID).
		Scan(&tableNo, &tableName); err != nil {
		fail(c, "桌台不存在")
		return
	}

	items, err := service.ResolveOrderItems(p.Items)
	if err != nil {
		fail(c, err.Error())
		return
	}
	dishAmount, seatFee, discount, total := service.RecalcAmount(p.PersonCount, items)

	orderNo := service.GenOrderNo()
	var orderID int64
	err = store.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO tb_order(order_no, table_id, table_no, table_name, person_count, order_status,
			dish_amount, seat_fee, discount_amount, total_amount, pay_status, order_remark, create_by, create_time, update_by, update_time)
			VALUES(?,?,?,?,?,1,?,?,?,?,0,?,?,?,?,?)`,
			orderNo, p.TableID, tableNo, tableName, p.PersonCount,
			model.ToCents(dishAmount), model.ToCents(seatFee), model.ToCents(discount), model.ToCents(total), p.OrderRemark,
			"食客", store.Now(), "食客", store.Now())
		if err != nil {
			return err
		}
		orderID, err = res.LastInsertId()
		if err != nil {
			return err
		}
		for _, it := range items {
			if _, err := tx.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
				VALUES(?,?,?,?,?,?,?,?,?)`, orderID, it.DishID, it.DishName, it.SpecID, it.SpecName, model.ToCents(it.Price), it.Quantity, model.ToCents(it.Amount), it.Remark); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE tb_table SET status=1, update_time=? WHERE table_id=?`, store.Now(), p.TableID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		fail(c, "下单失败: "+err.Error())
		return
	}

	// 下单成功后异步触发打印机打印(厨房单 + 食客小票),失败不影响下单。
	if created, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_id=?`, orderID)); err == nil {
		created.Items = items
		print.PrintOrder(created)
	}

	ok(c, gin.H{
		"orderId":        orderID,
		"orderNo":        orderNo,
		"dishAmount":     dishAmount,
		"seatFee":        seatFee,
		"discountAmount": discount,
		"totalAmount":    total,
	})
}

func CustomerOrderByNo(c *gin.Context) {
	orderNo := c.Param("orderNo")
	o, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_no=?`, orderNo))
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	o.Items = store.LoadOrderItems(o.OrderID)
	ok(c, o)
}

func CustomerPayQr(c *gin.Context) {
	ok(c, gin.H{
		"pay_qr_wx":  store.GetCfg("pay_qr_wx"),
		"pay_qr_ali": store.GetCfg("pay_qr_ali"),
	})
}

// CustomerConfig 返回顾客端所需的公开配置(无需登录)。
// 之前顾客点餐页/打印页误用了需鉴权的 /dining/config/list,导致 401,现统一走此接口。
func CustomerConfig(c *gin.Context) {
	ok(c, gin.H{
		"shop_name":           store.GetCfg("shop_name"),
		"seat_fee_enabled":    cfgFlag("seat_fee_enabled"),
		"seat_fee":            store.GetCfg("seat_fee"),
		"promotion_enabled":   cfgFlag("promotion_enabled"),
		"promotion_threshold": store.GetCfg("promotion_threshold"),
		"promotion_discount":  store.GetCfg("promotion_discount"),
		"pay_qr_wx":           store.GetCfg("pay_qr_wx"),
		"pay_qr_ali":          store.GetCfg("pay_qr_ali"),
		// 在线支付开关(顾客端据此决定是否展示在线支付入口)
		"wxpay_enabled":  cfgFlag("wxpay_enabled"),
		"alipay_enabled": cfgFlag("alipay_enabled"),
	})
}

// CustomerAppendOrder 顾客加菜:向该桌进行中且未支付的订单追加菜品。
// 与首次下单不同,加菜复用已有订单(合并明细、重算金额),避免同一桌产生多笔订单。
func CustomerAppendOrder(c *gin.Context) {
	var p struct {
		OrderNo string            `json:"orderNo"`
		Items   []model.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.OrderNo == "" || len(p.Items) == 0 {
		fail(c, "参数错误")
		return
	}

	var orderID, tableID, personCount, orderStatus, payStatus int
	err := store.DB.QueryRow(`SELECT order_id, table_id, person_count, order_status, pay_status
		FROM tb_order WHERE order_no=?`, p.OrderNo).
		Scan(&orderID, &tableID, &personCount, &orderStatus, &payStatus)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.ActiveStatus(orderStatus) {
		fail(c, "当前订单状态不可加菜")
		return
	}
	if payStatus == 1 {
		fail(c, "订单已支付，不可加菜")
		return
	}

	newItems, err := service.ResolveOrderItems(p.Items)
	if err != nil {
		fail(c, err.Error())
		return
	}

	// 合并现有明细与新增明细,按最新人数/明细重算金额。
	existing := store.LoadOrderItems(orderID)
	allItems := append(existing, newItems...)
	dishAmount, seatFee, discount, total := service.RecalcAmount(personCount, allItems)

	err = store.WithTx(func(tx *sql.Tx) error {
		for _, it := range newItems {
			if _, err := tx.Exec(`INSERT INTO tb_order_item(order_id, dish_id, dish_name, spec_id, spec_name, price, quantity, amount, remark)
				VALUES(?,?,?,?,?,?,?,?,?)`, orderID, it.DishID, it.DishName, it.SpecID, it.SpecName, model.ToCents(it.Price), it.Quantity, model.ToCents(it.Amount), it.Remark); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE tb_order SET dish_amount=?, seat_fee=?, discount_amount=?, total_amount=?, update_time=? WHERE order_id=?`,
			model.ToCents(dishAmount), model.ToCents(seatFee), model.ToCents(discount), model.ToCents(total), store.Now(), orderID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		fail(c, "加菜失败: "+err.Error())
		return
	}

	// 打印新增菜品的厨房单(食客小票在结账时打印,此处仅通知后厨)。
	if created, err := store.ScanOrder(store.DB.QueryRow(`SELECT `+store.OrderCols+` FROM tb_order WHERE order_id=?`, orderID)); err == nil {
		created.Items = newItems
		print.PrintKitchen(created)
	}

	ok(c, gin.H{
		"orderId":     orderID,
		"orderNo":     p.OrderNo,
		"dishAmount":  dishAmount,
		"seatFee":     seatFee,
		"totalAmount": total,
	})
}

// CustomerUrgeOrder 顾客催菜:请求后厨加急处理当前订单。
//
// 约束:
//   - 仅进行中的订单可催(已完成/已取消无意义);
//   - 同一订单 180 秒内只受理一次,防止顾客连点把后厨刷屏,
//     超频时返回剩余等待秒数,前端据此展示倒计时。
//
// 催菜会生成一条待处理记录,商家端订单列表与桌台看板据此打角标。
func CustomerUrgeOrder(c *gin.Context) {
	var p struct {
		OrderNo string `json:"orderNo"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || strings.TrimSpace(p.OrderNo) == "" {
		fail(c, "参数错误")
		return
	}
	p.OrderNo = strings.TrimSpace(p.OrderNo)

	var orderID, tableID, orderStatus int
	var tableNo, tableName string
	err := store.DB.QueryRow(`SELECT order_id, table_id, order_status, table_no, table_name
		FROM tb_order WHERE order_no=?`, p.OrderNo).
		Scan(&orderID, &tableID, &orderStatus, &tableNo, &tableName)
	if err != nil {
		fail(c, "订单不存在")
		return
	}
	if !service.ActiveStatus(orderStatus) {
		fail(c, "订单已结束，无需催菜")
		return
	}

	// 冷却校验:同订单两次催菜间隔不得小于 UrgeCooldownSecond。
	if last, okLast := store.LastUrgeTime(orderID); okLast {
		if remain := model.UrgeCooldownSecond - int(time.Since(last).Seconds()); remain > 0 {
			fail(c, fmt.Sprintf("已通知后厨，请 %d 秒后再催", remain))
			return
		}
	}

	if _, err := store.DB.Exec(`INSERT INTO tb_order_urge(order_id, order_no, table_id, table_no, table_name,
		urge_type, status, create_time) VALUES(?,?,?,?,?,?,?,?)`,
		orderID, p.OrderNo, tableID, tableNo, tableName,
		model.UrgeTypeUrge, model.UrgeStatusPending, store.Now()); err != nil {
		fail(c, "催菜失败，请稍后重试")
		return
	}

	ok(c, gin.H{
		"orderId":  orderID,
		"cooldown": model.UrgeCooldownSecond,
	})
}
