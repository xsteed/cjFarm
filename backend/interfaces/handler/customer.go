package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"dining-system/application/service"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/print"
	"dining-system/interfaces/dto"
)

// ============ 顾客端(公开接口) ============

// customerFail 收敛顾客端公开接口的错误文案:匿名访客不应看到 dao 原始错误,
// 真实错误统一写日志;只有不含内部细节的业务提示才原样返回,其余返回固定文案。
func customerFail(c *gin.Context, err error) {
	if err == nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	msg := err.Error()
	// 顾客端错误只回固定文案,真实原因全部落在日志里 —— 若不带 requestID,
	// 「顾客说下单失败」就只能靠时间戳在大量匿名请求里碰运气。
	log := logger.WithContext(c.Request.Context(), zap.String("path", c.Request.URL.Path))
	// 订单不存在/已结束/不可加菜/已支付统一为同一文案,防止通过响应差异探测订单存在性。
	switch msg {
	case "订单不存在", "订单已结束，无需催菜", "当前订单状态不可加菜", "订单已支付，不可加菜",
		"当前订单状态不可修改人数", "订单已支付，不可修改人数":
		log.Warnf("[customer] 请求失败: %v", err)
		fail(c, "订单不存在或已结束")
		return
	}
	// 「下单失败/加菜失败/修改人数失败」包装了 dao 底层错误(可能是 SQL 错误),不能回显给访客。
	if strings.HasPrefix(msg, "下单失败:") || strings.HasPrefix(msg, "加菜失败:") ||
		strings.HasPrefix(msg, "修改人数失败:") {
		log.Warnf("[customer] 请求失败: %v", err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	fail(c, msg)
}

// CustomerTable 顾客扫码进入桌台:路由参数同时接受「桌台稳定码」与「数字桌台ID」。
// 老二维码(/order/1)与新二维码(/order/XXXXXXXX)都可正常打开。
func CustomerTable(c *gin.Context) {
	table, currentOrder, servingOrders, err := service.CustomerTable(c.Param("id"))
	if err != nil {
		fail(c, "桌台不存在")
		return
	}
	var co interface{}
	if currentOrder != nil {
		co = currentOrder
	}
	ok(c, gin.H{
		"tableId":       table.TableID,
		"tableNo":       table.TableNo,
		"tableName":     table.TableName,
		"tableCode":     table.TableCode,
		"capacity":      table.Capacity,
		"status":        table.Status,
		"currentOrder":  co,
		"servingOrders": servingOrders,
	})
}

func CustomerMenu(c *gin.Context) {
	out, err := service.CustomerMenu()
	if err != nil {
		logger.CtxWarnf(c.Request.Context(), "[customer] 菜单加载失败: %v", err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, out)
}

func CustomerRemarks(c *gin.Context) {
	list, err := service.RemarkList()
	if err != nil {
		logger.CtxWarnf(c.Request.Context(), "[customer] 备注加载失败: %v", err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, list)
}

func CustomerCreateOrder(c *gin.Context) {
	var p struct {
		TableID         int             `json:"tableId"`
		PersonCount     int             `json:"personCount"`
		OrderRemark     string          `json:"orderRemark"`
		Items           []dto.OrderItem `json:"items"`
		ClientRequestID string          `json:"clientRequestId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	// 会员归属只认服务端从顾客令牌解析出的值(可选身份,未登录为 0),请求体不带该字段。
	res, err := service.CustomerCreateOrder(customerIDFrom(c), p.TableID, p.PersonCount, p.OrderRemark, p.Items, p.ClientRequestID)
	if err != nil {
		customerFail(c, err)
		return
	}

	// 下单成功后异步触发打印机打印,失败不影响下单;幂等命中不重复打印。
	//
	// 后付模式:厨房单 + 点菜单一起出(print.PrintOrder)。
	// 先付模式(PayFirstEnabled):厨房单不能出 —— 订单还没收到钱,后厨提前做菜
	// 就违背了「先付」语义,改为在首次收款成功后补出(见 printKitchenOnPaid,
	// 线上支付与前台收款两条路都覆盖);点菜单只是点菜凭证、与后厨是否开工无关,
	// 下单时照常出(受 print_guest_on_order 控制),先有顾客小票、后有厨房单。
	if !res.Duplicated {
		if created, ok := service.LoadOrderForPrint(int(res.OrderID)); ok {
			if service.PayFirstEnabled() {
				print.PrintGuestOnOrder(c.Request.Context(), created, res.Items)
			} else {
				print.PrintOrder(c.Request.Context(), created, res.Items)
			}
		}
	}

	ok(c, gin.H{
		"orderId":        res.OrderID,
		"orderNo":        res.OrderNo,
		"dishAmount":     res.DishAmount,
		"seatFee":        res.SeatFee,
		"discountAmount": res.DiscountAmount,
		"totalAmount":    res.TotalAmount,
	})
}

func CustomerOrderByNo(c *gin.Context) {
	// 归属校验:调用方(扫码进来的桌台)带 tableId 时校验订单归属,防止凭单号
	// 遍历查询他人订单(单号会出现在小票/照片/群聊里)。老二维码链接未带桌台
	// 参数时退化为仅按单号查询,由本路由上的 IP 限流兜底(见 main.go)。
	tableID, _ := strconv.Atoi(c.Query("tableId"))
	order, err := service.CustomerOrderByNo(c.Param("orderNo"), tableID)
	if err != nil {
		customerFail(c, err)
		return
	}
	ok(c, order)
}

func CustomerPayQr(c *gin.Context) {
	ok(c, service.CustomerPayQr())
}

// CustomerSetting 返回顾客端所需的公开配置(无需登录)。
// 之前顾客点餐页/打印页误用了需鉴权的管理端 /api/admin/config/list,导致 401,现统一走此接口。
func CustomerSetting(c *gin.Context) {
	ok(c, service.CustomerSettings())
}

// CustomerAppendOrder 顾客加菜:向该桌进行中且未支付的订单追加菜品。
// 与首次下单不同,加菜复用已有订单(合并明细、重算金额),避免同一桌产生多笔订单。
func CustomerAppendOrder(c *gin.Context) {
	var p struct {
		TableID         int             `json:"tableId"`
		OrderNo         string          `json:"orderNo"`
		Items           []dto.OrderItem `json:"items"`
		PersonCount     int             `json:"personCount"`
		ClientRequestID string          `json:"clientRequestId"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}

	res, err := service.CustomerAppendOrder(p.TableID, p.OrderNo, p.Items, p.PersonCount, p.ClientRequestID)
	if err != nil {
		customerFail(c, err)
		return
	}

	// 加菜重算了应收金额:顾客此前若已生成收款码,那张码对应的还是旧金额。不关单的话
	// 顾客可以扫旧码按旧金额付款,回调金额与订单当前总额不符,会被金额防线拒之门外,
	// 留下一笔「顾客已付、订单未入账」要人工捞的挂起单。因此与改单/收款/取消同一口径:
	// 加菜后关掉待支付的在线渠道单。顾客回到支付页时 PayCreate 会按新金额重新出码
	// (InsertPayment 把关单占位行复活为待支付)。
	//
	// 幂等命中(Duplicated,同一 clientRequestId 的重放)金额没有再变,不动顾客手里的码。
	if !res.Duplicated {
		closeOnlinePayment(c.Request.Context(), res.OrderID)
	}

	// 打印:厨房机出「加菜单」(只列新菜),小票机重出一张整单最新金额的加菜小票
	// (print_guest_on_append 可关);幂等命中不重复打印。
	//
	// 先付模式下同样跳过:能加菜的订单必然是「未支付」的,此时后厨本就不该开火;
	// 加进来的菜会和整单一起在收款成功后出单(printKitchenOnPaid 出的是整单)。
	if !res.Duplicated && !service.PayFirstEnabled() {
		if created, ok := service.LoadOrderForPrint(res.OrderID); ok {
			print.PrintKitchen(c.Request.Context(), created, res.Items)
			print.PrintGuestAppend(c.Request.Context(), created, res.Items)
		}
	}

	ok(c, gin.H{
		"orderId":     res.OrderID,
		"orderNo":     res.OrderNo,
		"dishAmount":  res.DishAmount,
		"seatFee":     res.SeatFee,
		"totalAmount": res.TotalAmount,
	})
}

// CustomerUpdatePerson 顾客在订单页修改就餐人数(不改菜品)。
//
// 与加菜刻意分成两个接口:加菜要求 items 非空,拿它当「只改人数」用会重复扣库存、
// 重复出厨单。人数影响茶位费,改完应收金额就变了 —— 与加菜同一口径,必须关掉
// 顾客手里那张按旧金额生成的在线支付单,否则付款回调会撞上金额防线。
func CustomerUpdatePerson(c *gin.Context) {
	var p struct {
		TableID     int    `json:"tableId"`
		OrderNo     string `json:"orderNo"`
		PersonCount int    `json:"personCount"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}

	res, err := service.CustomerUpdatePerson(p.TableID, p.OrderNo, p.PersonCount)
	if err != nil {
		customerFail(c, err)
		return
	}

	// 人数真变了才关单:没变时金额没动,关单反而可能打断顾客正在进行的支付。
	if res.Changed {
		closeOnlinePayment(c.Request.Context(), res.Order.OrderID)
	}

	ok(c, res.Order)
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
		TableID int    `json:"tableId"`
		OrderNo string `json:"orderNo"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}

	res, err := service.CustomerUrgeOrder(p.TableID, p.OrderNo)
	if err != nil {
		// 冷却拒绝:结构化返回剩余秒数,前端据此展示倒计时,不再解析 msg 文案。
		var cd *service.UrgeCooldownError
		if errors.As(err, &cd) {
			failData(c, cd.Error(), gin.H{"cooldown": cd.Remain})
			return
		}
		customerFail(c, err)
		return
	}

	ok(c, gin.H{
		"orderId":  res.OrderID,
		"cooldown": res.Cooldown,
	})
}
